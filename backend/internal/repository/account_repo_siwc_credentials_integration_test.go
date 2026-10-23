//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// StoreSIWCRecord owns its transaction. Use the shared Ent client, never an Ent
// transaction; every fixture is unique and its committed rows are cleaned up.
func newSIWCPostgresFixture(t *testing.T) (*accountRepository, *service.Account) {
	t.Helper()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{
		Name: "siwc-integration-" + uuid.NewString(), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "native-access", "refresh_token": "native-refresh", "chatgpt_account_id": "workspace-" + uuid.NewString(), "chatgpt_user_id": "native-user", "email": "native@example.test", "client_id": "codex-client", "auth_mode": "oauth", "openai_auth_mode": "oauth"},
		Extra:       map[string]any{service.OpenAISIWCEnabledKey: true},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
		require.NoError(t, err)
	})
	return newAccountRepositoryWithSQL(client, integrationDB, nil), account
}

func TestSIWCPostgresInstallationStableSingleton(t *testing.T) {
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	first, err := repo.SIWCInstallationID(t.Context(), "urn:uuid:"+uuid.NewString())
	require.NoError(t, err)
	require.NotEmpty(t, first)
	again, err := repo.SIWCInstallationID(t.Context(), "urn:uuid:"+uuid.NewString())
	require.NoError(t, err)
	require.Equal(t, first, again)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM openai_siwc_installation").Scan(&count))
	require.Equal(t, 1, count)
	_, err = integrationDB.ExecContext(t.Context(), "INSERT INTO openai_siwc_installation(singleton,host_id) VALUES(FALSE,$1)", "invalid-second-host")
	var pgError *pq.Error
	require.ErrorAs(t, err, &pgError)
	require.Equal(t, pq.ErrorCode("23514"), pgError.Code)
	// Singleton is installation-wide, so keep an already-created row for other
	// tests instead of deleting shared state. The ephemeral harness owns its DB.
}

func TestSIWCPostgresConcurrentCASAndRevocation(t *testing.T) {
	repo, account := newSIWCPostgresFixture(t)
	registration, grant, err := repo.GetSIWCRecord(t.Context(), account)
	require.NoError(t, err)
	require.Empty(t, registration)
	require.Empty(t, grant)
	saved, err := repo.StoreSIWCRecord(t.Context(), account, "", "", "encrypted-registration-0", "encrypted-grant-0")
	require.NoError(t, err)
	require.True(t, saved)
	registration, grant, err = repo.GetSIWCRecord(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, "encrypted-registration-0", registration)
	require.Equal(t, "encrypted-grant-0", grant)
	// Simulate native Codex refresh completing after SIWC loaded its parent
	// snapshot but before saving the upstream-rotated SIWC grant.
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE accounts SET credentials=credentials || jsonb_build_object('access_token','rotated-native-access','refresh_token','rotated-native-refresh') WHERE id=$1", account.ID)
	require.NoError(t, err)
	type outcome struct {
		saved bool
		err   error
		index int
	}
	start := make(chan struct{})
	results := make(chan outcome, 16)
	for i := 0; i < 16; i++ {
		go func(index int) {
			<-start
			saved, err := repo.StoreSIWCRecord(t.Context(), account, registration, grant, fmt.Sprintf("encrypted-registration-%d", index+1), fmt.Sprintf("encrypted-grant-%d", index+1))
			results <- outcome{saved, err, index + 1}
		}(i)
	}
	close(start)
	winner, winners := 0, 0
	for range 16 {
		result := <-results
		require.NoError(t, result.err)
		if result.saved {
			winner = result.index
			winners++
		}
	}
	require.Equal(t, 1, winners, "exactly one registration+grant CAS may win")
	currentReg, currentGrant, err := repo.GetSIWCRecord(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("encrypted-registration-%d", winner), currentReg)
	require.Equal(t, fmt.Sprintf("encrypted-grant-%d", winner), currentGrant)
	saved, err = repo.StoreSIWCRecord(t.Context(), account, currentReg, currentGrant, currentReg, "")
	require.NoError(t, err)
	require.True(t, saved)
	// Both an already-running refresh and an old initial callback must lose after signout.
	saved, err = repo.StoreSIWCRecord(t.Context(), account, currentReg, currentGrant, currentReg, "late-refresh")
	require.NoError(t, err)
	require.False(t, saved)
	saved, err = repo.StoreSIWCRecord(t.Context(), account, "", "", "late-registration", "late-callback")
	require.NoError(t, err)
	require.False(t, saved)
	registration, grant, err = repo.GetSIWCRecord(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, currentReg, registration)
	require.Empty(t, grant)
}

func TestSIWCPostgresParentIdentityAndNativeTokenRotation(t *testing.T) {
	for _, key := range []string{"chatgpt_account_id", "chatgpt_user_id", "email", "client_id", "auth_mode", "openai_auth_mode", "access_token", "refresh_token", "expires_at"} {
		t.Run(key, func(t *testing.T) {
			repo, account := newSIWCPostgresFixture(t)
			saved, err := repo.StoreSIWCRecord(t.Context(), account, "", "", "encrypted-registration", "encrypted-grant")
			require.NoError(t, err)
			require.True(t, saved)
			_, err = integrationDB.ExecContext(t.Context(), "UPDATE accounts SET credentials=jsonb_set(credentials,ARRAY[$2]::text[],to_jsonb($3::text)) WHERE id=$1", account.ID, key, "replaced-value")
			require.NoError(t, err)
			registration, grant, err := repo.GetSIWCRecord(t.Context(), account)
			require.NoError(t, err)
			rotating := key == "access_token" || key == "refresh_token" || key == "expires_at"
			if rotating {
				// A native token rotation alone must not destroy independently stored SIWC
				// identity or prevent saving its independently rotated refresh token.
				require.Equal(t, "encrypted-registration", registration)
				require.Equal(t, "encrypted-grant", grant)
			} else {
				require.Empty(t, registration)
				require.Empty(t, grant)
			}
			saved, err = repo.StoreSIWCRecord(t.Context(), account, "encrypted-registration", "encrypted-grant", "updated-registration", "updated-grant")
			require.NoError(t, err)
			require.Equal(t, rotating, saved)
			var storedReg, storedGrant string
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT registration_ciphertext,credentials_ciphertext FROM openai_siwc_grants WHERE account_id=$1", account.ID).Scan(&storedReg, &storedGrant))
			if rotating {
				require.Equal(t, "updated-registration", storedReg)
				require.Equal(t, "updated-grant", storedGrant)
			} else {
				require.Equal(t, "encrypted-registration", storedReg)
				require.Equal(t, "encrypted-grant", storedGrant)
			}
			var liveValue string
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT credentials->>$2 FROM accounts WHERE id=$1", account.ID, key).Scan(&liveValue))
			require.Equal(t, "replaced-value", liveValue, "SIWC writes never restore stale native credentials")
		})
	}
}

func TestSIWCPostgresParentDeletionCascade(t *testing.T) {
	repo, account := newSIWCPostgresFixture(t)
	saved, err := repo.StoreSIWCRecord(t.Context(), account, "", "", "encrypted-registration", "encrypted-grant")
	require.NoError(t, err)
	require.True(t, saved)
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE accounts SET deleted_at=NOW() WHERE id=$1", account.ID)
	require.NoError(t, err)
	registration, grant, err := repo.GetSIWCRecord(t.Context(), account)
	require.NoError(t, err)
	require.Empty(t, registration)
	require.Empty(t, grant)
	saved, err = repo.StoreSIWCRecord(t.Context(), account, "encrypted-registration", "encrypted-grant", "stale-registration", "stale-grant")
	require.NoError(t, err)
	require.False(t, saved)
	_, err = integrationDB.ExecContext(t.Context(), "DELETE FROM accounts WHERE id=$1", account.ID)
	require.NoError(t, err)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM openai_siwc_grants WHERE account_id=$1", account.ID).Scan(&count))
	require.Zero(t, count)
}

func TestSIWCPostgresUsageAuthorizationConstraintAndImmutability(t *testing.T) {
	_, account := newSIWCPostgresFixture(t)
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{Email: "siwc-usage-" + uuid.NewString() + "@example.test"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "siwc-usage-" + uuid.NewString(), Name: "SIWC integration"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM usage_logs WHERE account_id=$1", account.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id=$1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1", user.ID)
		require.NoError(t, err)
	})
	usageRepo := newUsageLogRepositoryWithSQL(client, integrationDB)
	for _, mode := range []string{service.UsageAuthorizationModeNative, service.UsageAuthorizationModeBPS, service.UsageAuthorizationModeSIWC, ""} {
		t.Run("mode_"+mode, func(t *testing.T) {
			entry := &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, RequestID: uuid.NewString(), Model: "siwc-test-model", AuthorizationMode: mode, CreatedAt: time.Now().UTC()}
			created, err := usageRepo.Create(t.Context(), entry)
			require.NoError(t, err)
			require.True(t, created)
			var persisted sql.NullString
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT authorization_mode FROM usage_logs WHERE id=$1", entry.ID).Scan(&persisted))
			if mode == "" {
				require.False(t, persisted.Valid, "historical unknown mode stays NULL")
			} else {
				require.True(t, persisted.Valid)
				require.Equal(t, mode, persisted.String)
			}
			_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_logs SET authorization_mode=authorization_mode WHERE id=$1", entry.ID)
			require.NoError(t, err)
			replacement := "siwc"
			if mode == "siwc" {
				replacement = "native"
			}
			_, err = integrationDB.ExecContext(t.Context(), "UPDATE usage_logs SET authorization_mode=$2 WHERE id=$1", entry.ID, replacement)
			var pgError *pq.Error
			require.ErrorAs(t, err, &pgError)
			require.Equal(t, pq.ErrorCode("P0001"), pgError.Code)
			require.True(t, strings.Contains(pgError.Message, "usage authorization mode is immutable"))
		})
	}
	_, err := integrationDB.ExecContext(t.Context(), "INSERT INTO usage_logs(user_id,api_key_id,account_id,request_id,model,authorization_mode) VALUES($1,$2,$3,$4,'siwc-invalid-mode','other')", user.ID, key.ID, account.ID, uuid.NewString())
	var pgError *pq.Error
	require.ErrorAs(t, err, &pgError)
	require.Equal(t, pq.ErrorCode("23514"), pgError.Code)
}
