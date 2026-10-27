//go:build integration

package repository

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func bpsCooldownRecoveryAccount(t *testing.T, extra map[string]any) (*accountRepository, *service.Account) {
	t.Helper()
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{
		Name: "bps-cooldown-successful-test-recovery", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "synthetic-recovery-token"}, Extra: extra,
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		require.NoError(t, err)
		require.NoError(t, client.Account.DeleteOneID(account.ID).Exec(context.Background()))
	})
	return newAccountRepositoryWithSQL(client, integrationDB, nil), account
}

func bpsRecoveryModelScope(model string) string {
	return service.ExcelBPSCooldownScopePrefix + "429:model:" + base64.RawURLEncoding.EncodeToString([]byte(model))
}

// Exercise the real service -> repository -> PostgreSQL chain used after the
// runner has observed model B succeeding. No live provider probe is simulated.
func TestBPSCooldownRecoveryPostgresSuccessfulModelBPreservesModelA(t *testing.T) {
	until := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Microsecond)
	repo, account := bpsCooldownRecoveryAccount(t, map[string]any{
		"openai_excel_bps": true, "openai_excel_bps_models": []any{"gpt-6-astra", "gpt-5.6-sol"},
		"unrelated_state":   "preserve",
		"model_rate_limits": map[string]any{"openai:image_generation": map[string]any{"rate_limit_reset_at": until.Format(time.RFC3339Nano)}},
	})
	modelA := bpsRecoveryModelScope("gpt-6-astra")
	endpoint := ":endpoint:" + base64.RawURLEncoding.EncodeToString([]byte("/basispoints/api/attachments"))
	scopes := []string{
		modelA,
		service.ExcelBPSCooldownScopePrefix + "429" + endpoint,
		modelA + endpoint,
		service.ExcelBPSCooldownScopePrefix + "model_permission:" + base64.RawURLEncoding.EncodeToString([]byte("gpt-6-astra")),
	}
	for _, scope := range scopes {
		_, err := repo.ExtendExcelBPSCooldown(t.Context(), account.ID, scope, until)
		require.NoError(t, err)
	}
	modelB, err := repo.GetExcelBPSCooldown(t.Context(), account.ID, []string{bpsRecoveryModelScope("gpt-5.6-sol")})
	require.NoError(t, err)
	require.True(t, modelB.IsZero(), "model B's text test is outside A and attachment scopes")
	before, err := repo.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	beforeLimits := before.Extra["model_rate_limits"].(map[string]any)

	svc := service.NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	result, err := svc.RecoverAccountAfterSuccessfulTest(t.Context(), account.ID)
	require.NoError(t, err)
	require.True(t, result.ClearedRateLimit)
	reloaded := newAccountRepositoryWithSQL(repo.client, integrationDB, nil)
	stored, err := reloaded.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	limits := stored.Extra["model_rate_limits"].(map[string]any)
	require.Len(t, limits, len(scopes))
	for _, scope := range scopes {
		require.Equal(t, beforeLimits[scope], limits[scope], "retain the complete BPS entry")
		deadline, err := reloaded.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
		require.NoError(t, err)
		require.True(t, deadline.Equal(until), "a fresh consumer still observes the full Retry-After")
	}
	require.NotContains(t, limits, "openai:image_generation")
	require.Equal(t, true, stored.Extra["openai_excel_bps"])
	require.Equal(t, "preserve", stored.Extra["unrelated_state"])
	require.Equal(t, before.Credentials, stored.Credentials)

	result, err = svc.RecoverAccountAfterSuccessfulTest(t.Context(), account.ID)
	require.NoError(t, err)
	require.False(t, result.ClearedRateLimit, "BPS-only state is not falsely reported recovered")
}

func TestBPSCooldownRecoveryPostgresSuccessfulTestStillClearsNativeState(t *testing.T) {
	until := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	repo, account := bpsCooldownRecoveryAccount(t, map[string]any{
		"unrelated_state":   "preserve",
		"model_rate_limits": map[string]any{"gpt-5.6-sol": map[string]any{"rate_limit_reset_at": until.Format(time.RFC3339Nano)}},
	})
	_, err := integrationDB.ExecContext(t.Context(), "UPDATE accounts SET status='error', error_message='test recovery', rate_limited_at=NOW(), rate_limit_reset_at=$2, overload_until=$2, temp_unschedulable_until=$2, temp_unschedulable_reason='test recovery' WHERE id=$1", account.ID, until)
	require.NoError(t, err)
	svc := service.NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	result, err := svc.RecoverAccountAfterSuccessfulTest(t.Context(), account.ID)
	require.NoError(t, err)
	require.True(t, result.ClearedError)
	require.True(t, result.ClearedRateLimit)
	stored, err := repo.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	require.Equal(t, service.StatusActive, stored.Status)
	require.Empty(t, stored.ErrorMessage)
	require.Nil(t, stored.RateLimitedAt)
	require.Nil(t, stored.RateLimitResetAt)
	require.Nil(t, stored.OverloadUntil)
	require.Nil(t, stored.TempUnschedulableUntil)
	require.NotContains(t, stored.Extra, "model_rate_limits")
	require.Equal(t, "preserve", stored.Extra["unrelated_state"])
}

func TestBPSCooldownRecoveryPostgresExplicitAdminClearIsUnchanged(t *testing.T) {
	for _, entry := range []string{"clear_rate_limit", "recover_state"} {
		t.Run(entry, func(t *testing.T) {
			repo, account := bpsCooldownRecoveryAccount(t, map[string]any{"openai_excel_bps": true})
			scope := bpsRecoveryModelScope("gpt-6-astra")
			_, err := repo.ExtendExcelBPSCooldown(t.Context(), account.ID, scope, time.Now().Add(24*time.Hour))
			require.NoError(t, err)
			svc := service.NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			if entry == "clear_rate_limit" {
				require.NoError(t, svc.ClearRateLimit(t.Context(), account.ID))
			} else {
				result, err := svc.RecoverAccountState(t.Context(), account.ID, service.AccountRecoveryOptions{})
				require.NoError(t, err)
				require.True(t, result.ClearedRateLimit)
			}
			until, err := repo.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
			require.NoError(t, err)
			require.True(t, until.IsZero())
			stored, err := repo.GetByID(t.Context(), account.ID)
			require.NoError(t, err)
			require.NotContains(t, stored.Extra, "model_rate_limits")
			require.True(t, stored.IsExcelBPSEnabled())
		})
	}
}

type bpsRecoveryAfterReadRepository struct {
	*accountRepository
	afterRead func(context.Context) error
}

func (r *bpsRecoveryAfterReadRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	account, err := r.accountRepository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if r.afterRead != nil {
		hook := r.afterRead
		r.afterRead = nil
		if err := hook(ctx); err != nil {
			return nil, err
		}
	}
	return account, nil
}

func TestBPSCooldownRecoveryPostgresRetainsExtensionAfterRecoverySnapshot(t *testing.T) {
	until := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Microsecond)
	repo, account := bpsCooldownRecoveryAccount(t, map[string]any{
		"openai_excel_bps":  true,
		"model_rate_limits": map[string]any{"gpt-5.6-sol": map[string]any{"rate_limit_reset_at": until.Format(time.RFC3339Nano)}},
	})
	scope := bpsRecoveryModelScope("gpt-6-astra")
	writer := newAccountRepositoryWithSQL(repo.client, integrationDB, nil)
	wrapped := &bpsRecoveryAfterReadRepository{accountRepository: repo, afterRead: func(ctx context.Context) error {
		_, err := writer.ExtendExcelBPSCooldown(ctx, account.ID, scope, until)
		return err
	}}
	svc := service.NewRateLimitService(wrapped, nil, &config.Config{}, nil, nil)
	result, err := svc.RecoverAccountAfterSuccessfulTest(t.Context(), account.ID)
	require.NoError(t, err)
	require.True(t, result.ClearedRateLimit)
	saved, err := writer.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
	require.NoError(t, err)
	require.True(t, saved.Equal(until), "cleanup must filter the current row, not the pre-write snapshot")
	stored, err := writer.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	limits := stored.Extra["model_rate_limits"].(map[string]any)
	require.Len(t, limits, 1)
	require.Contains(t, limits, scope)
}
