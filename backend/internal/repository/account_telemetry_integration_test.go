//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAccountTelemetryPostgresCreationAndConcurrentPersistence(t *testing.T) {
	ctx := t.Context()
	repo := newAccountRepositoryWithSQL(testEntClient(t), integrationDB, nil)
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
		verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) {
			return new(service.VerifiedSupplyIdentity), nil
		},
		claim: func(context.Context, service.VendorProxySQL, *service.Account, *service.VerifiedSupplyIdentity) error {
			return nil
		},
	})
	account := &service.Account{Name: "telemetry-" + uuid.NewString(), Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "fixture", "account_uuid": "account-fixture"}, Extra: map[string]any{"ordinary": true}, Concurrency: 1, Priority: 1, Schedulable: true}
	require.NoError(t, repo.Create(ctx, account))
	current, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	original := service.ReadAccountTelemetry(current)
	require.Equal(t, "pending", original.Status)
	require.NotEmpty(t, original.Revision)
	require.Nil(t, original.Profile)
	credentials, _ := json.Marshal(account.Credentials)
	var wins atomic.Int32
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state := original
			state.Revision = uuid.NewString()
			saved, err := repo.SaveAccountTelemetry(ctx, account.ID, string(credentials), original.Revision, state)
			if err != nil {
				errors <- err
			}
			if saved {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), wins.Load())
	current, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	winner := service.ReadAccountTelemetry(current)
	saved, err := repo.SaveAccountTelemetry(ctx, account.ID, `{"access_token":"another-account"}`, winner.Revision, winner)
	require.NoError(t, err)
	require.False(t, saved)
	fake := winner
	fake.Revision = "stale-editor"
	require.NoError(t, repo.UpdateExtra(ctx, account.ID, map[string]any{service.AccountTelemetryExtraKey: fake, "ordinary": false}))
	current, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, winner.Revision, service.ReadAccountTelemetry(current).Revision)
	require.Equal(t, false, current.Extra["ordinary"])
	_, err = repo.BulkUpdate(ctx, []int64{account.ID}, service.AccountBulkUpdate{Extra: map[string]any{service.AccountTelemetryExtraKey: fake, "bulk": true}})
	require.NoError(t, err)
	current, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, winner.Revision, service.ReadAccountTelemetry(current).Revision)
	current.Extra[service.AccountTelemetryExtraKey] = fake
	current.Name += "-edited"
	require.NoError(t, repo.Update(ctx, current))
	current, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, winner.Revision, service.ReadAccountTelemetry(current).Revision)
	accounts, err := repo.ListAccountTelemetry(ctx, 0, 100, true)
	require.NoError(t, err)
	found := false
	for _, item := range accounts {
		found = found || item.ID == account.ID
	}
	require.True(t, found)
}
