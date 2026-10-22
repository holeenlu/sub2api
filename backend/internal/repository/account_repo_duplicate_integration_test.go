//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountAuthorizationImportPostgresRollbackAndSnapshot(t *testing.T) {
	ctx := t.Context()
	client := testEntClient(t)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
		verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) {
			return new(service.VerifiedSupplyIdentity), nil
		},
		claim: func(context.Context, service.VendorProxySQL, *service.Account, *service.VerifiedSupplyIdentity) error {
			return nil
		},
	})
	name := fmt.Sprintf("authorization-import-%d", time.Now().UnixNano())
	group, err := client.Group.Create().SetName(name).SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "ALTER TABLE openai_siwc_grants DROP CONSTRAINT IF EXISTS test_authorization_import_reject")
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id IN (SELECT id FROM accounts WHERE name=$1)", name)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE name=$1", name)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", group.ID)
	})
	newAccount := func() *service.Account {
		return &service.Account{Name: name, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusDisabled, Schedulable: false, Credentials: map[string]any{"access_token": "native", "chatgpt_account_id": "workspace", "plan_type": "plus"}, Extra: map[string]any{service.OpenAISIWCEnabledKey: true}}
	}
	grants := &service.AccountAuthorizationCiphertexts{Excel: "encrypted-excel", SIWCRegistration: "encrypted-registration", SIWCCredentials: "encrypted-siwc"}
	account := newAccount()
	require.NoError(t, repo.CreateWithAccountAuthorizations(ctx, account, []service.AccountGroup{{GroupID: group.ID, Priority: 19}}, grants))
	account.Credentials = map[string]any{"stale": "must-be-replaced"}
	account.Extra = map[string]any{"stale": true}
	snapshot, err := repo.AccountAuthorizationSnapshot(ctx, account)
	require.NoError(t, err)
	require.Equal(t, grants, snapshot)
	require.Equal(t, "native", account.Credentials["access_token"])
	require.NotContains(t, account.Credentials, "stale")
	require.NotContains(t, account.Extra, "stale")
	var priority int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT priority FROM account_groups WHERE account_id=$1 AND group_id=$2", account.ID, group.ID).Scan(&priority))
	require.Equal(t, 19, priority)
	_, err = integrationDB.ExecContext(ctx, "ALTER TABLE openai_siwc_grants ADD CONSTRAINT test_authorization_import_reject CHECK (registration_ciphertext <> 'reject-this-import')")
	require.NoError(t, err)
	failure := newAccount()
	grants.SIWCRegistration = "reject-this-import"
	require.Error(t, repo.CreateWithAccountAuthorizations(ctx, failure, []service.AccountGroup{{GroupID: group.ID, Priority: 3}}, grants))
	for _, table := range []string{"accounts", "account_groups", "openai_excel_grants", "openai_siwc_grants", "scheduler_outbox"} {
		column := "account_id"
		if table == "accounts" {
			column = "id"
		}
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+column+"=$1", failure.ID).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestCreateWithAccountGroupsPersistsPausedCopyAtomically(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	suffix := time.Now().UnixNano()

	group, err := client.Group.Create().
		SetName(fmt.Sprintf("duplicate-atomic-%d", suffix)).
		SetPlatform(service.PlatformAnthropic).
		Save(ctx)
	require.NoError(t, err)

	success := &service.Account{
		Name:        fmt.Sprintf("duplicate-success-%d", suffix),
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: false,
		Credentials: map[string]any{"api_key": "secret"},
		Extra:       map[string]any{},
	}
	require.NoError(t, repo.CreateWithAccountGroups(ctx, success, []service.AccountGroup{{GroupID: group.ID, Priority: 37}}))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id = $1", success.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_groups WHERE account_id = $1", success.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", success.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id = $1", group.ID)
	})

	var schedulable bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT schedulable FROM accounts WHERE id = $1", success.ID).Scan(&schedulable))
	require.False(t, schedulable)
	var priority int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT priority FROM account_groups WHERE account_id = $1 AND group_id = $2", success.ID, group.ID).Scan(&priority))
	require.Equal(t, 37, priority)
	var outboxCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", success.ID).Scan(&outboxCount))
	require.Equal(t, 1, outboxCount)

	failure := &service.Account{
		Name:        fmt.Sprintf("duplicate-failure-%d", suffix),
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		Schedulable: false,
		Credentials: map[string]any{"api_key": "secret"},
		Extra:       map[string]any{},
	}
	err = repo.CreateWithAccountGroups(ctx, failure, []service.AccountGroup{{GroupID: int64(^uint64(0) >> 1), Priority: 1}})
	require.Error(t, err)

	var accountCount, groupCount, failedOutboxCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts WHERE name = $1", failure.Name).Scan(&accountCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM account_groups WHERE account_id = $1", failure.ID).Scan(&groupCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id = $1", failure.ID).Scan(&failedOutboxCount))
	require.Zero(t, accountCount)
	require.Zero(t, groupCount)
	require.Zero(t, failedOutboxCount)
}
