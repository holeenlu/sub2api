//go:build integration

package repository

import (
	"context"
	"github.com/stretchr/testify/require"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBillingSubscriptionRedisCloseAndJoin(t *testing.T) {
	cache := &billingCache{rdb: integrationRedis}
	entered := make(chan struct{})
	canceled := make(chan struct{})
	h, err := cache.SubscribeSubscriptionCacheInvalidation(context.Background(), func(ctx context.Context, _ string) { close(entered); <-ctx.Done(); close(canceled) })
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = h.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, h.Wait(ctx))
	})
	require.NoError(t, cache.PublishSubscriptionCacheInvalidation(context.Background(), "close-join"))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Redis callback not admitted")
	}
	require.NoError(t, h.Close())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, h.Wait(ctx))
	select {
	case <-canceled:
	default:
		t.Fatal("callback not canceled before join")
	}
	// Local join completes before Redis necessarily consumes the TCP close.
	// Require bounded server-side convergence, not cross-connection ordering.
	require.Eventually(t, func() bool {
		subscribers, err := integrationRedis.PubSubNumSub(ctx, subCacheInvalidateChannel).Result()
		return err == nil && subscribers[subCacheInvalidateChannel] == 0
	}, time.Second, 10*time.Millisecond)
}
func TestBillingSubscriptionRedisParentCancellationUnblocksReceiver(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	h, err := (&billingCache{rdb: integrationRedis}).SubscribeSubscriptionCacheInvalidation(parent, func(context.Context, string) { t.Error("unexpected callback") })
	require.NoError(t, err)
	cancel()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, h.Wait(ctx))
	require.NoError(t, h.Close())
}
func TestBillingSubscriptionRedisReconnectKeepsHandleOwnership(t *testing.T) {
	cache := &billingCache{rdb: integrationRedis}
	messages := make(chan string, 16)
	h, err := cache.SubscribeSubscriptionCacheInvalidation(context.Background(), func(_ context.Context, key string) {
		select {
		case messages <- key:
		default:
		}
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = h.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, h.Wait(ctx))
	})
	killed, err := integrationRedis.Do(context.Background(), "CLIENT", "KILL", "TYPE", "pubsub").Int64()
	require.NoError(t, err)
	require.Positive(t, killed)
	require.Eventually(t, func() bool {
		_ = cache.PublishSubscriptionCacheInvalidation(context.Background(), "after-reconnect")
		select {
		case key := <-messages:
			return key == "after-reconnect"
		default:
			return false
		}
	}, 3*time.Second, 20*time.Millisecond)
}

func TestBillingSubscriptionRedisHeartbeatIsOwned(t *testing.T) {
	pingCalls := func() int64 {
		stats, err := integrationRedis.Info(context.Background(), "commandstats").Result()
		require.NoError(t, err)
		for _, line := range strings.Split(stats, "\n") {
			if strings.HasPrefix(line, "cmdstat_ping:calls=") {
				raw := strings.SplitN(strings.TrimPrefix(line, "cmdstat_ping:calls="), ",", 2)[0]
				n, err := strconv.ParseInt(raw, 10, 64)
				require.NoError(t, err)
				return n
			}
		}
		return 0
	}
	before := pingCalls()
	h, err := (&billingCache{rdb: integrationRedis}).SubscribeSubscriptionCacheInvalidation(context.Background(), func(context.Context, string) {})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = h.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, h.Wait(ctx))
	})
	require.Eventually(t, func() bool { return pingCalls() > before }, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, h.Close())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, h.Wait(ctx))
	select {
	case <-h.(*billingCacheSubscription).healthDone:
	default:
		t.Fatal("heartbeat not joined")
	}
}
