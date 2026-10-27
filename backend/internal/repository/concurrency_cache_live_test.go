package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOrdinarySlotRenewalPreservesActiveMembersWithoutRevivingLostSlots(t *testing.T) {
	for _, scope := range []string{"account", "user", "api_key"} {
		t.Run(scope, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			ctx := context.Background()
			cache := NewConcurrencyCache(client, 1, 60).(*concurrencyCache)
			now := time.Now().Truncate(time.Second)
			server.SetTime(now)
			const id int64 = 17
			key, index := accountSlotKey(id), accountActiveIndexKey
			acquire, renew, release := cache.AcquireAccountSlot, cache.RefreshAccountSlot, cache.ReleaseAccountSlot
			if scope == "user" {
				key, index = userSlotKey(id), userActiveIndexKey
				acquire, renew, release = cache.AcquireUserSlot, cache.RefreshUserSlot, cache.ReleaseUserSlot
			} else if scope == "api_key" {
				key, index = apiKeySlotKey(id), ""
				acquire = func(ctx context.Context, id int64, _ int, requestID string) (bool, error) {
					return true, cache.TrackAPIKeySlot(ctx, id, requestID)
				}
				renew, release = cache.RefreshAPIKeySlot, cache.ReleaseAPIKeySlot
			}
			acquired, err := acquire(ctx, id, 1, "active")
			require.NoError(t, err)
			require.True(t, acquired)
			for range 4 {
				now = now.Add(40 * time.Second)
				server.FastForward(40 * time.Second)
				server.SetTime(now)
				owned, err := renew(ctx, id, "active")
				require.NoError(t, err)
				require.True(t, owned, "active ownership must survive multiple original TTLs")
				score, err := client.ZScore(ctx, key, "active").Result()
				require.NoError(t, err)
				require.Equal(t, float64(now.Unix()), score)
				if index != "" {
					until, err := client.ZScore(ctx, index, "17").Result()
					require.NoError(t, err)
					require.Equal(t, float64(now.Unix()+60), until, "the active cleanup index must track renewal")
					acquired, err := acquire(ctx, id, 1, "competing")
					require.NoError(t, err)
					require.False(t, acquired, "renewal must preserve the original admission limit")
				}
			}
			require.NoError(t, release(ctx, id, "active"))
			owned, err := renew(ctx, id, "active")
			require.NoError(t, err)
			require.False(t, owned, "released ownership must not be recreated")
			require.NoError(t, client.ZAdd(ctx, key,
				redis.Z{Score: float64(now.Unix() - 61), Member: "expired"},
				redis.Z{Score: float64(now.Unix()), Member: "other-instance"},
			).Err())
			owned, err = renew(ctx, id, "expired")
			require.NoError(t, err)
			require.False(t, owned, "an expired member must not be revived even while its key exists")
			_, err = client.ZScore(ctx, key, "expired").Result()
			require.ErrorIs(t, err, redis.Nil)
			_, err = client.ZScore(ctx, key, "other-instance").Result()
			require.NoError(t, err, "a failed renewal must preserve another instance's live slot")
		})
	}
}

func TestConcurrencyStartupPreservesOtherInstanceSlotsAndWaiters(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	first := NewConcurrencyCache(client, 15, 900)
	acquired, err := first.AcquireAccountSlot(ctx, 10, 1, "other-instance-account")
	require.NoError(t, err)
	require.True(t, acquired)
	acquired, err = first.AcquireUserSlot(ctx, 20, 1, "other-instance-user")
	require.NoError(t, err)
	require.True(t, acquired)
	allowed, err := first.IncrementAccountWaitCount(ctx, 10, 2)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = first.IncrementWaitCount(ctx, 20, 2)
	require.NoError(t, err)
	require.True(t, allowed)
	secondClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = secondClient.Close() })
	second := NewConcurrencyCache(secondClient, 15, 900)
	clock, err := client.Time(ctx).Result()
	require.NoError(t, err)
	for _, item := range []struct{ key, index, id string }{
		{accountSlotKeyPrefix + "11", accountActiveIndexKey, "11"},
		{userSlotKeyPrefix + "21", userActiveIndexKey, "21"},
	} {
		require.NoError(t, client.ZAdd(ctx, item.key, redis.Z{Score: float64(clock.Unix() - 901), Member: "expired-instance"}).Err())
		require.NoError(t, client.ZAdd(ctx, item.index, redis.Z{Score: float64(clock.Unix() - 1), Member: item.id}).Err())
	}
	service.ProvideConcurrencyService(second, nil, nil)
	require.False(t, server.Exists(accountSlotKeyPrefix+"11"))
	require.False(t, server.Exists(userSlotKeyPrefix+"21"))

	acquired, err = second.AcquireAccountSlot(ctx, 10, 1, "new-instance-account")
	require.NoError(t, err)
	require.False(t, acquired, "starting a peer must not reclaim another instance's capacity")
	acquired, err = second.AcquireUserSlot(ctx, 20, 1, "new-instance-user")
	require.NoError(t, err)
	require.False(t, acquired)
	waiting, err := server.Get(accountWaitKeyPrefix + "10")
	require.NoError(t, err)
	require.Equal(t, "1", waiting)
	waiting, err = server.Get(waitQueueKeyPrefix + "20")
	require.NoError(t, err)
	require.Equal(t, "1", waiting)
}

func TestLiveLeaseReplacesRegularSlotsAndCountsTowardLimits(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	live, ok := regular.(service.LiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	accountAcquired, err := regular.AcquireAccountSlot(ctx, 10, 1, "regular-account")
	require.NoError(t, err)
	require.True(t, accountAcquired)
	userAcquired, err := regular.AcquireUserSlot(ctx, 20, 1, "regular-user")
	require.NoError(t, err)
	require.True(t, userAcquired)

	acquired, err := live.AcquireLiveLease(ctx, 10, 1, 20, 1, 30, "live-lease", true)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, regular.ReleaseAccountSlot(ctx, 10, "regular-account"))
	require.NoError(t, regular.ReleaseUserSlot(ctx, 20, "regular-user"))

	accountCount, err := regular.GetAccountConcurrency(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount)
	userCount, err := regular.GetUserConcurrency(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, userCount)
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-blocked")
	require.NoError(t, err)
	require.False(t, accountAcquired)

	refreshed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "live-lease")
	require.NoError(t, err)
	require.True(t, refreshed)
	require.NoError(t, live.ReleaseLiveLease(ctx, 10, 20, 30, "live-lease"))
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-allowed")
	require.NoError(t, err)
	require.True(t, accountAcquired)
}

func TestLiveLeaseExpiresWithoutRefresh(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	live, ok := regular.(service.LiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := live.AcquireLiveLease(ctx, 10, 1, 20, 1, 30, "expired-live", false)
	require.NoError(t, err)
	require.True(t, acquired)

	redisServer.FastForward(61 * time.Second)
	acquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-after-expiry")
	require.NoError(t, err)
	require.True(t, acquired)
	refreshed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "expired-live")
	require.NoError(t, err)
	require.False(t, refreshed)
}
