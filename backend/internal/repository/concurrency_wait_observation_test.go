package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestQueueObservationMeasuresBypassAndReapsCanceledWaiters(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, ok := NewConcurrencyCache(client, 15, 900).(*concurrencyCache)
	require.True(t, ok)
	ctx := context.Background()
	now := time.Now()
	server.SetTime(now)
	require.NoError(t, cache.StartWaitObservation(ctx, "account", 1, "first", 10*time.Second))
	server.SetTime(now.Add(2 * time.Second))
	require.NoError(t, cache.StartWaitObservation(ctx, "account", 1, "second", 20*time.Second))
	server.SetTime(now.Add(3 * time.Second))
	age, older, err := cache.ObserveWaitAdmission(ctx, "account", 1, "")
	require.NoError(t, err)
	require.Equal(t, int64(3000), age)
	require.Equal(t, 2, older)
	_, older, err = cache.ObserveWaitAdmission(ctx, "account", 1, "second")
	require.NoError(t, err)
	require.Equal(t, 1, older)
	_, older, err = cache.ObserveWaitAdmission(ctx, "account", 1, "first")
	require.NoError(t, err)
	require.Zero(t, older)
	require.NoError(t, cache.EndWaitObservation(ctx, "account", 1, "second"))
	server.SetTime(now.Add(11 * time.Second))
	age, older, err = cache.ObserveWaitAdmission(ctx, "account", 1, "")
	require.NoError(t, err)
	require.Zero(t, older)
	require.Zero(t, age)
	// Observation never changes the admission semaphore or another scope.
	allowed, err := cache.AcquireAccountSlot(ctx, 1, 1, "real-request")
	require.NoError(t, err)
	require.True(t, allowed)
	require.NoError(t, cache.StartWaitObservation(ctx, "user", 1, "user-wait", time.Minute))
	_, older, err = cache.ObserveWaitAdmission(ctx, "account", 1, "")
	require.NoError(t, err)
	require.Zero(t, older)
}
