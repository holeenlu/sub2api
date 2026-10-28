package repository

import (
	"context"
	"flag"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// This test-only flag selects a task-owned local Redis; normal unit runs do not
// start Docker or implicitly connect to an application's configured database.
var capacityRedisTestAddr = flag.String("capacity-redis-addr", "", "isolated loopback Redis for capacity lease contract tests")

func TestRealRedisCapacityLeaseAuthority(t *testing.T) {
	if *capacityRedisTestAddr == "" {
		t.Skip("requires an explicitly supplied isolated local Redis")
	}
	host, _, err := net.SplitHostPort(*capacityRedisTestAddr)
	require.NoError(t, err)
	require.True(t, net.ParseIP(host).IsLoopback())
	client := redis.NewClient(&redis.Options{Addr: *capacityRedisTestAddr})
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Ping(t.Context()).Err())
	raw := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	raw.slotTTLSeconds = 2 // test-only shortened version of the same configured TTL
	id := time.Now().UnixNano()%1000000000 + 1000000000
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = client.Del(ctx, accountSlotKey(id), userSlotKey(id), liveAccountSlotKey(id), liveUserSlotKey(id), liveAPIKeySlotKey(id)).Err()
		_ = client.ZRem(ctx, accountActiveIndexKey, strconv.FormatInt(id, 10)).Err()
		_ = client.ZRem(ctx, userActiveIndexKey, strconv.FormatInt(id, 10)).Err()
	}
	t.Cleanup(cleanup)

	t.Run("long request renews original member", func(t *testing.T) {
		result, err := routing.AcquireCapacity(t.Context(), raw, routing.CapacityRequest{ID: id, Token: "long-request", Limit: 1}, nil)
		require.NoError(t, err)
		require.True(t, result.Acquired)
		require.False(t, result.Unlimited)
		require.NotNil(t, result.Lease)
		defer result.ReleaseFunc()
		ctx, cancel := context.WithCancel(t.Context())
		bound, detach := result.Lease.Bind(ctx)
		defer detach()
		time.Sleep(2500 * time.Millisecond)
		require.NoError(t, result.Lease.Validate(t.Context()))
		require.NoError(t, bound.Err())
		count, err := raw.GetAccountConcurrency(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		cancel()
		result.ReleaseFunc()
		require.ErrorIs(t, result.Lease.Validate(t.Context()), context.Canceled)
		count, err = raw.GetAccountConcurrency(t.Context(), id)
		require.NoError(t, err)
		require.Zero(t, count)
	})

	t.Run("user service carries verified identity and token", func(t *testing.T) {
		svc := service.NewConcurrencyService(raw)
		result, err := svc.AcquireUserSlot(t.Context(), id, 1)
		require.NoError(t, err)
		require.True(t, result.Acquired)
		require.NotNil(t, result.Lease)
		defer result.ReleaseFunc()
		identity := result.Lease.Snapshot().Identity
		require.Equal(t, id, identity.UserID)
		require.Zero(t, identity.AccountID)
		require.NotEmpty(t, identity.Token)
		proof, err := result.Verify(t.Context(), routing.CapacityPolicy{ResourceID: id, User: true, Limit: 1})
		require.NoError(t, err)
		require.NotEqual(t, [32]byte{}, proof.Fingerprint())
		require.True(t, proof.ValidUntil().After(time.Now()))
		count, err := raw.GetUserConcurrency(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		result.ReleaseFunc()
		count, err = raw.GetUserConcurrency(t.Context(), id)
		require.NoError(t, err)
		require.Zero(t, count)
	})

	t.Run("replacement cannot be cleared by old owner", func(t *testing.T) {
		result, err := routing.AcquireCapacity(t.Context(), raw, routing.CapacityRequest{ID: id, Token: "old", Limit: 1}, nil)
		require.NoError(t, err)
		defer result.ReleaseFunc()
		require.NoError(t, raw.ReleaseAccountSlot(t.Context(), id, "old"))
		ok, err := raw.AcquireAccountSlot(t.Context(), id, 1, "new")
		require.NoError(t, err)
		require.True(t, ok)
		require.ErrorIs(t, result.Lease.Validate(t.Context()), routing.ErrCapacityLeaseLost)
		result.ReleaseFunc()
		proof, err := raw.CheckCapacityLease(t.Context(), routing.CapacityRequest{ID: id, Token: "new"})
		require.NoError(t, err)
		require.Positive(t, proof)
		require.NoError(t, raw.ReleaseAccountSlot(t.Context(), id, "new"))
	})

	t.Run("expired user member cannot be renewed", func(t *testing.T) {
		now, err := client.Time(t.Context()).Result()
		require.NoError(t, err)
		require.NoError(t, client.ZAdd(t.Context(), userSlotKey(id), redis.Z{Member: "expired", Score: float64(now.Unix() - 3)}).Err())
		req := routing.CapacityRequest{ID: id, Token: "expired", User: true, Limit: 1}
		proof, err := raw.RefreshCapacityLease(t.Context(), req)
		require.NoError(t, err)
		require.Zero(t, proof)
		require.ErrorIs(t, client.ZScore(t.Context(), userSlotKey(id), "expired").Err(), redis.Nil)
		proof, err = raw.CheckCapacityLease(t.Context(), req)
		require.NoError(t, err)
		require.Zero(t, proof)
	})

	t.Run("live handoff retains token and missing scope cannot revive", func(t *testing.T) {
		identity := routing.LeaseIdentity{AccountID: id, UserID: id, APIKeyID: id, Token: "call-owner"}
		lease, ok, err := routing.AcquireLiveCapacity(t.Context(), raw, routing.LiveCapacityRequest{Identity: identity, AccountLimit: 1, UserLimit: 1})
		require.NoError(t, err)
		require.True(t, ok)
		lease.Handoff()
		require.ErrorIs(t, lease.Validate(t.Context()), context.Canceled)
		successor, err := routing.AdoptLiveLease(t.Context(), raw, identity)
		require.NoError(t, err)
		defer successor.Release()
		require.NoError(t, successor.Validate(t.Context()))
		require.NoError(t, client.ZRem(t.Context(), liveUserSlotKey(id), identity.Token).Err())
		remaining, err := raw.RefreshLiveLeaseProof(t.Context(), id, id, id, identity.Token)
		require.NoError(t, err)
		require.Zero(t, remaining)
		require.ErrorIs(t, successor.Validate(t.Context()), routing.ErrCapacityLeaseLost)
		require.ErrorIs(t, client.ZScore(t.Context(), liveUserSlotKey(id), identity.Token).Err(), redis.Nil)
	})
	t.Run("startup preserves live foreign owners and waiters", func(t *testing.T) {
		previousTTL := raw.slotTTLSeconds
		raw.slotTTLSeconds = 15
		defer func() { raw.slotTTLSeconds = previousTTL }()
		ok, err := raw.AcquireAccountSlot(t.Context(), id, 1, "foreign-process-live")
		require.NoError(t, err)
		require.True(t, ok)
		defer raw.ReleaseAccountSlot(context.Background(), id, "foreign-process-live")
		user, err := service.NewConcurrencyService(raw).AcquireUserSlot(t.Context(), id, 1)
		require.NoError(t, err)
		defer user.ReleaseFunc()
		now, err := client.Time(t.Context()).Result()
		require.NoError(t, err)
		require.NoError(t, client.ZAdd(t.Context(), accountSlotKey(id), redis.Z{Member: "expired-process", Score: float64(now.Unix() - 16)}).Err())
		accountWait, userWait, legacyWait := accountWaitKeyPrefix+strconv.FormatInt(id, 10), waitQueueKeyPrefix+strconv.FormatInt(id, 10), accountWaitKeyPrefix+strconv.FormatInt(id+1, 10)
		defer client.Del(context.Background(), accountWait, userWait, legacyWait)
		require.NoError(t, client.Set(t.Context(), accountWait, 3, time.Minute).Err())
		require.NoError(t, client.Set(t.Context(), userWait, 2, time.Minute).Err())
		require.NoError(t, client.Set(t.Context(), legacyWait, 5, 0).Err())
		// This flag is explicitly for an isolated task-owned Redis. Exercise the
		// existing one-time marker without touching any deployment's state.
		require.NoError(t, client.Del(t.Context(), legacyWaitSweepMarkerKey).Err())
		require.NoError(t, raw.CleanupStaleProcessSlots(t.Context(), "new-process-"))
		require.NoError(t, client.ZScore(t.Context(), accountSlotKey(id), "foreign-process-live").Err())
		require.ErrorIs(t, client.ZScore(t.Context(), accountSlotKey(id), "expired-process").Err(), redis.Nil)
		require.NoError(t, user.Lease.Validate(t.Context()))
		wait, err := client.Get(t.Context(), accountWait).Int()
		require.NoError(t, err)
		require.Equal(t, 3, wait)
		wait, err = client.Get(t.Context(), userWait).Int()
		require.NoError(t, err)
		require.Equal(t, 2, wait)
		wait, err = client.Get(t.Context(), legacyWait).Int()
		require.NoError(t, err)
		require.Equal(t, 5, wait)
		ttl, err := client.TTL(t.Context(), legacyWait).Result()
		require.NoError(t, err)
		require.Positive(t, ttl)
	})

}
