package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRecoveryProbeCrossInstanceFencingAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	a, ok := NewConcurrencyCache(clientA, 15, 900).(*concurrencyCache)
	require.True(t, ok)
	b, ok := NewConcurrencyCache(clientB, 15, 900).(*concurrencyCache)
	require.True(t, ok)
	ctx := context.Background()
	now := time.Now()
	server.SetTime(now)
	require.NoError(t, a.BlockRecoveryScope(ctx, "proxy:1", time.Second))
	allowed, _, err := b.AcquireRecoveryProbe(ctx, "proxy:1", 0, "early")
	require.NoError(t, err)
	require.False(t, allowed)
	server.SetTime(now.Add(2 * time.Second))
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i, cache := range []*concurrencyCache{a, b} {
		wg.Add(1)
		go func(i int, cache *concurrencyCache) {
			defer wg.Done()
			owner := []string{"a", "b"}[i]
			allowed, probe, err := cache.AcquireRecoveryProbe(ctx, "proxy:1", 0, owner)
			if err == nil && allowed && probe {
				winners.Add(1)
			}
		}(i, cache)
	}
	wg.Wait()
	require.Equal(t, int32(1), winners.Load())
	// Simulate a crashed holder. Reclaim only after the lease deadline.
	server.SetTime(now.Add(63 * time.Second))
	allowed, probe, err := b.AcquireRecoveryProbe(ctx, "proxy:1", 0, "new")
	require.NoError(t, err)
	require.True(t, allowed && probe)
	require.NoError(t, a.FinishRecoveryProbe(ctx, "proxy:1", "a", 0, true))
	require.NoError(t, a.FinishRecoveryProbe(ctx, "proxy:1", "b", 0, true))
	allowed, _, err = b.AcquireRecoveryProbe(ctx, "proxy:1", 0, "third")
	require.NoError(t, err)
	require.False(t, allowed, "stale owners cannot release a newer probe")
	require.NoError(t, b.FinishRecoveryProbe(ctx, "proxy:1", "new", 0, true))
	allowed, probe, err = a.AcquireRecoveryProbe(ctx, "proxy:1", 0, "normal")
	require.NoError(t, err)
	require.True(t, allowed)
	require.False(t, probe)
	// A new incident fences a late completion from the previous epoch.
	require.NoError(t, a.BlockRecoveryScope(ctx, "proxy:1", time.Minute))
	require.NoError(t, b.FinishRecoveryProbe(ctx, "proxy:1", "new", 0, true))
	allowed, _, err = b.AcquireRecoveryProbe(ctx, "proxy:1", 0, "blocked")
	require.NoError(t, err)
	require.False(t, allowed)
	// Persisted account cooldowns do not reopen after every successful request.
	until := now.Add(-time.Second).UnixMilli()
	allowed, probe, err = a.AcquireRecoveryProbe(ctx, "account:2", until, "owner")
	require.NoError(t, err)
	require.True(t, allowed && probe)
	require.NoError(t, a.FinishRecoveryProbe(ctx, "account:2", "owner", 0, true))
	allowed, probe, err = b.AcquireRecoveryProbe(ctx, "account:2", until, "next")
	require.NoError(t, err)
	require.True(t, allowed)
	require.False(t, probe)
}
