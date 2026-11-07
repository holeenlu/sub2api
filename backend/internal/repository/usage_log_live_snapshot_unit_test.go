//go:build unit

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestUsageLogLiveVendorSnapshotSurvivesCacheRoundTrip(t *testing.T) {
	mini := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	defer rdb.Close()
	cache := &gatewayCache{rdb: rdb}
	id := int64(12)
	name, platform, groupName := "selected", "openai", "served"
	r := &service.LiveCallRecord{CallID: "call", CallHash: "callhash", AccountID: 4, VendorIDSnapshot: &id, AccountNameSnapshot: &name, PlatformSnapshot: &platform, GroupNameSnapshot: &groupName, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	require.NoError(t, cache.SaveLiveUsageSnapshot(context.Background(), r, time.Hour))
	require.NoError(t, cache.SaveLiveCall(context.Background(), r, time.Hour))
	loaded, e := cache.GetLiveCall(context.Background(), r.CallHash)
	require.NoError(t, e)
	require.NoError(t, cache.LoadLiveUsageSnapshot(context.Background(), loaded))
	require.Equal(t, id, *loaded.VendorIDSnapshot)
	require.Equal(t, name, *loaded.AccountNameSnapshot)
	require.Equal(t, groupName, *loaded.GroupNameSnapshot)
	loaded.AccountID = 5
	require.Error(t, cache.LoadLiveUsageSnapshot(context.Background(), loaded), "snapshot bound to selected account")
	mini.FastForward(2 * time.Hour)
	require.False(t, mini.Exists(liveCallKey(r.CallHash)))
}
