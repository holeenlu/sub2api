//go:build integration

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSCooldownPostgresConcurrentInstances(t *testing.T) {
	client := testEntClient(t)
	account := mustCreateAccount(t, client, &service.Account{
		Name: "bps-cooldown-concurrent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-token"},
		Extra:       map[string]any{"openai_excel_bps": true, "unrelated_state": "preserve"},
	})
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		require.NoError(t, err)
		require.NoError(t, client.Account.DeleteOneID(account.ID).Exec(context.Background()))
	})
	repos := []*accountRepository{
		newAccountRepositoryWithSQL(client, integrationDB, nil),
		newAccountRepositoryWithSQL(client, integrationDB, nil),
	}
	const scope = service.ExcelBPSCooldownScopePrefix + "429:model:Z3B0LTYtYXN0cmE"
	base := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Microsecond)
	start := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := repos[i%len(repos)].ExtendExcelBPSCooldown(t.Context(), account.ID, scope, base.Add(time.Duration(i)*time.Hour))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	reloaded := newAccountRepositoryWithSQL(client, integrationDB, nil)
	until, err := reloaded.GetExcelBPSCooldown(t.Context(), account.ID, []string{scope})
	require.NoError(t, err)
	require.Equal(t, base.Add(15*time.Hour), until)
	until, err = reloaded.ExtendExcelBPSCooldown(t.Context(), account.ID, scope, base)
	require.NoError(t, err)
	require.Equal(t, base.Add(15*time.Hour), until, "committed shorter signals cannot replace the maximum")
	other, err := reloaded.GetExcelBPSCooldown(t.Context(), account.ID, []string{service.ExcelBPSCooldownScopePrefix + "429"})
	require.NoError(t, err)
	require.True(t, other.IsZero(), "model state must not become an account-wide BPS cooldown")

	stored, err := reloaded.GetByID(t.Context(), account.ID)
	require.NoError(t, err)
	require.True(t, stored.IsExcelBPSEnabled())
	require.Nil(t, stored.RateLimitResetAt)
	require.Equal(t, "preserve", stored.Extra["unrelated_state"])
	require.Equal(t, account.Credentials, stored.Credentials)
	require.NotContains(t, stored.Extra, service.ExcelBPS403DisabledAtKey)
}
