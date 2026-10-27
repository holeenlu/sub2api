//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func bpsCooldownReviewAccount(t *testing.T) *service.Account {
	t.Helper()
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: "bps-cooldown-independent-review", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "synthetic-bps-review-token"},
		Extra:       map[string]any{"openai_excel_bps": true},
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", account.ID)
	})
	return account
}

// Two repository instances exercise the real PostgreSQL row lock. This is R
// evidence, not a claim that two independent gateway processes were exercised.
func TestExcelBPSCooldownReviewPostgresConcurrentWritersAndScopeIsolation(t *testing.T) {
	account := bpsCooldownReviewAccount(t)
	other := bpsCooldownReviewAccount(t)
	a := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	b := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	const scope = service.ExcelBPSCooldownScopePrefix + "429:model:review-a"
	const otherScope = service.ExcelBPSCooldownScopePrefix + "429:model:review-b"
	longest := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Microsecond)
	_, err := a.ExtendExcelBPSCooldown(t.Context(), account.ID, scope, longest)
	require.NoError(t, err)
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for i := 1; i <= 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			repo := a
			if n%2 == 0 {
				repo = b
			}
			_, err := repo.ExtendExcelBPSCooldown(t.Context(), account.ID, scope, longest.Add(-time.Duration(n)*time.Hour))
			failures <- err
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	// Recreate the consumer: the deadline must come from PG, not an instance map.
	restarted := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	got, err := restarted.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
	require.NoError(t, err)
	require.True(t, got.Equal(longest), "shorter signals must not truncate a 72-hour upstream limit")
	absent, err := restarted.GetExcelBPSCooldown(t.Context(), account.ID, []string{otherScope})
	require.NoError(t, err)
	require.True(t, absent.IsZero(), "another model is not limited")
	absent, err = restarted.GetExcelBPSCooldown(t.Context(), other.ID, []string{scope})
	require.NoError(t, err)
	require.True(t, absent.IsZero(), "another account is not limited")
	stored, err := restarted.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	require.Equal(t, true, stored.Extra["openai_excel_bps"])
	require.Nil(t, stored.RateLimitedAt)
	require.Nil(t, stored.RateLimitResetAt)
	require.NotContains(t, stored.Extra, service.ExcelBPS403DisabledAtKey)
	var events int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&events))
	require.Equal(t, 1, events, "only the actual extension publishes an account change")
}

func TestExcelBPSCooldownPostgresSurvivesStaleFullAccountEdit(t *testing.T) {
	account := bpsCooldownReviewAccount(t)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	stale, err := repo.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	const scope = service.ExcelBPSCooldownScopePrefix + "429"
	until := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Microsecond)
	_, err = repo.ExtendExcelBPSCooldown(t.Context(), account.ID, scope, until)
	require.NoError(t, err)
	stale.Name = "ordinary edit after cooldown"
	require.NoError(t, repo.Update(t.Context(), stale))
	actual, err := repo.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
	require.NoError(t, err)
	require.True(t, actual.Equal(until), "an old editor snapshot cannot erase server-owned cooldown")
}

func TestExcelBPSCooldownReviewPostgresTransactionRollback(t *testing.T) {
	account := bpsCooldownReviewAccount(t)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	tx, err := integrationEntClient.Tx(t.Context())
	require.NoError(t, err)
	defer tx.Rollback()
	const scope = service.ExcelBPSCooldownScopePrefix + "429:endpoint:review"
	until := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	saved, err := repo.ExtendExcelBPSCooldown(dbent.NewTxContext(t.Context(), tx), account.ID, scope, until)
	require.NoError(t, err)
	require.True(t, saved.Equal(until))
	require.NoError(t, tx.Rollback())
	missing, err := repo.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
	require.NoError(t, err)
	require.True(t, missing.IsZero(), "rolled-back cooldown cannot become authoritative")
	var events int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", account.ID).Scan(&events))
	require.Zero(t, events, "rolled-back cooldown must not publish an outbox event")
}
