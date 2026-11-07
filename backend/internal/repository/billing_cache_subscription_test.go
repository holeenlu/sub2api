//go:build unit

package repository

import (
	"context"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestBillingSubscriptionCloseCancelsCallbackAndJoins(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &billingCache{rdb: client}
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handle, err := cache.SubscribeSubscriptionCacheInvalidation(context.Background(), func(ctx context.Context, key string) { close(entered); <-ctx.Done(); close(canceled); <-release })
	require.NoError(t, err)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		_ = handle.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = handle.Wait(ctx)
	})
	require.NoError(t, cache.PublishSubscriptionCacheInvalidation(context.Background(), "test-key"))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("callback did not start")
	}
	require.NoError(t, handle.Close())
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("callback context not canceled")
	}
	bounded, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, handle.Wait(bounded), context.DeadlineExceeded)
	releaseOnce.Do(func() { close(release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, handle.Wait(join))
	require.NoError(t, handle.Close())
	require.Eventually(t, func() bool {
		subscribers, err := client.PubSubNumSub(join, subCacheInvalidateChannel).Result()
		return err == nil && subscribers[subCacheInvalidateChannel] == 0
	}, time.Second, 10*time.Millisecond)
	require.Zero(t, client.Publish(join, subCacheInvalidateChannel, "after-close").Val())
}
func TestBillingSubscriptionParentCancellationJoinsBlockedReceiver(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	parent, cancel := context.WithCancel(context.Background())
	handle, err := (&billingCache{rdb: client}).SubscribeSubscriptionCacheInvalidation(parent, func(context.Context, string) { t.Error("unexpected callback") })
	require.NoError(t, err)
	cancel()
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, handle.Wait(join))
}
func TestBillingSubscriptionCallbackPanicIsObservableAfterJoin(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	cache := &billingCache{rdb: client}
	handle, err := cache.SubscribeSubscriptionCacheInvalidation(context.Background(), func(context.Context, string) { panic("callback failure") })
	require.NoError(t, err)
	require.NoError(t, cache.PublishSubscriptionCacheInvalidation(context.Background(), "test-key"))
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.ErrorContains(t, handle.Wait(join), "callback panicked")
}
