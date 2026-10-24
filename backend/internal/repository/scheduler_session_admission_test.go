package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionAdmissionReadOnlyBatchAndAtomicOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	server.SetTime(time.Unix(1800000000, 0))
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(serverTimingRedisHook{})
	cache := NewSessionLimitCache(client, 5)
	ctx := context.Background()
	require.NoError(t, client.ZAdd(ctx, sessionLimitKey(1), redis.Z{Score: 1799999900, Member: "existing"}, redis.Z{Score: 1799999000, Member: "expired"}).Err())
	require.NoError(t, client.Set(ctx, sessionLimitKey(2), "wrong_type", 0).Err())
	budgetCtx, b, cancel := selectionbudget.Start(ctx)
	defer cancel()
	entries, err := cache.GetSessionAdmissionBatch(budgetCtx, []int64{1, 2, 3}, "existing", map[int64]time.Duration{1: 2 * time.Minute})
	require.Error(t, err)
	require.True(t, entries[1].Known)
	require.Equal(t, 1, entries[1].ActiveCount)
	require.True(t, entries[1].SessionActive)
	require.False(t, entries[2].Known)
	require.True(t, entries[3].Known)
	require.EqualValues(t, 2, client.ZCard(ctx, sessionLimitKey(1)).Val(), "preflight must not remove expired members")
	require.Equal(t, 2, b.Snapshot().Operations)
	require.Equal(t, 7, b.Snapshot().Commands)
	existing, err := cache.RegisterSession(ctx, 1, "existing", 1, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, existing.Allowed)
	require.False(t, existing.Created)
	denied, err := cache.RegisterSession(ctx, 1, "new", 1, 2*time.Minute)
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	created, err := cache.RegisterSession(ctx, 3, "new", 1, 2*time.Minute)
	require.NoError(t, err)
	require.True(t, created.Allowed)
	require.True(t, created.Created)
}

func TestNativeLargePoolPrefetchPreservesFinalQualificationBudget(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(serverTimingRedisHook{})
	cache := &sessionLimitCache{rdb: client, defaultIdleTimeout: 5 * time.Minute}
	ids := make([]int64, 5000)
	accounts := make([]service.AccountWithConcurrency, len(ids))
	costs := make(map[int64]float64, len(ids))
	for i := range ids {
		ids[i] = int64(i + 1)
		accounts[i] = service.AccountWithConcurrency{ID: ids[i], MaxConcurrency: 10}
		costs[ids[i]] = 1
	}
	ctx, budget, cancel := selectionbudget.Start(context.Background())
	defer cancel()
	_, err := cache.GetSessionAdmissionBatch(ctx, ids, "existing", nil)
	require.NoError(t, err)
	_, err = cache.GetWindowCostBatch(ctx, ids)
	require.NoError(t, err)
	require.NoError(t, selectionbudget.Take(ctx, "sql", 0), "one cold window-cost batch query")
	warmCtx := selectionbudget.Reserve(ctx, 12+1+(len(ids)+255)/256+2)
	require.ErrorIs(t, cache.SetWindowCostBatch(warmCtx, costs), selectionbudget.ErrExhausted)
	counts, err := NewRPMCache(client).GetRPMBatch(ctx, ids)
	require.NoError(t, err)
	require.Len(t, counts, len(ids))
	_, err = NewConcurrencyCache(client, 5, 30).GetAccountsLoadBatch(ctx, accounts)
	require.NoError(t, err)
	require.GreaterOrEqual(t, budget.Remaining(), 12, "cold caches must leave room for the selected account")
	require.False(t, budget.Snapshot().Exhausted, "optional warming hitting its local ceiling does not exhaust selection")
}
func TestSessionAdmissionLargePoolAndExhaustedPipeline(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	client.AddHook(serverTimingRedisHook{})
	cache := NewSessionLimitCache(client, 5)
	ids := make([]int64, 5000)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	ctx, b, cancel := selectionbudget.Start(context.Background())
	defer cancel()
	result, err := cache.GetSessionAdmissionBatch(ctx, ids, "new", nil)
	require.NoError(t, err)
	require.Len(t, result, 5000)
	require.Equal(t, 21, b.Snapshot().Operations)
	require.Equal(t, 10001, b.Snapshot().Commands)
	for b.Remaining() > 1 {
		require.NoError(t, selectionbudget.Take(ctx, "sql", 0))
	}
	entries, err := cache.GetSessionAdmissionBatch(ctx, []int64{1, 2}, "new", nil)
	require.ErrorIs(t, err, selectionbudget.ErrExhausted)
	for _, id := range []int64{1, 2} {
		require.False(t, entries[id].Known, fmt.Sprint(id))
	}
	registration, err := cache.RegisterSession(ctx, 1, "new", 1, time.Minute)
	require.ErrorIs(t, err, selectionbudget.ErrExhausted)
	require.False(t, registration.Allowed)
}
