//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Catalog scope includes credential identity and proxy selection. The compact
// scheduler projection deliberately omits both; it is not discovery evidence.
func TestSchedulerCatalogSnapshotRestoresCompleteAccountScope(t *testing.T) {
	for _, platform := range []string{service.PlatformAnthropic, service.PlatformOpenAI} {
		for _, mode := range []string{"fixed", "follow"} {
			t.Run(platform+"/"+mode, func(t *testing.T) {
				ctx := context.Background()
				cache := newSchedulerCacheUnit(t)
				proxyID := int64(8)
				account := service.Account{ID: 19, Platform: platform, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true,
					ProxyID: &proxyID, Credentials: map[string]any{"access_token": "test-token", "base_url": "https://provider.example/v1"},
					Extra: map[string]any{service.ModelCatalogPolicyExtraKey: service.ModelCatalogPolicy{Models: []string{"selected-model"}}}}
				bucket := service.SchedulerBucket{GroupID: 3, Platform: platform, Mode: service.SchedulerModeSingle}
				token, err := cache.CaptureBucketWriteToken(ctx, bucket)
				require.NoError(t, err)
				require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{account}))
				usedAt := time.Now().UTC().Truncate(time.Millisecond)
				require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{account.ID: usedAt}))
				accounts, hit, err := cache.GetSnapshot(ctx, bucket)
				require.NoError(t, err)
				require.True(t, hit)
				require.Len(t, accounts, 1)
				require.Equal(t, account.Credentials, accounts[0].Credentials)
				require.Equal(t, account.ProxyID, accounts[0].ProxyID)
				require.Equal(t, usedAt, *accounts[0].LastUsedAt)
				require.False(t, accounts[0].SchedulerTicketProjection, "full accounts use their real ticket state")
				// Never validate an incomplete scope if the paired full snapshot
				// is absent. The caller can reload from the database on a miss.
				require.NoError(t, cache.rdb.Del(ctx, schedulerAccountKey("19")).Err())
				_, hit, err = cache.GetSnapshot(ctx, bucket)
				require.NoError(t, err)
				require.False(t, hit)
			})
		}
	}
}
