//go:build integration

package repository

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Point only at a disposable local Redis. No database flush is used: the test
// owns a unique account counter and removes only its own minute keys.
func TestStrictRPMLiveRedisConcurrentAndMinuteBoundary(t *testing.T) {
	addr := os.Getenv("SUB2API_RPM_REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("SUB2API_RPM_REDIS_TEST_ADDR not set")
	}
	ctx := context.Background()
	clients := make([]*redis.Client, 4)
	for i := range clients {
		clients[i] = redis.NewClient(&redis.Options{Addr: addr})
		t.Cleanup(func() { clients[i].Close() })
	}
	require.NoError(t, clients[0].Ping(ctx).Err())
	owner := time.Now().UnixNano()
	t.Cleanup(func() {
		keys, _ := clients[0].Keys(ctx, fmt.Sprintf("%s%d:*", rpmKeyPrefix, owner)).Result()
		if len(keys) > 0 {
			clients[0].Del(ctx, keys...)
		}
	})
	now, err := clients[0].Time(ctx).Result()
	require.NoError(t, err)
	// Start away from a boundary so the concurrent burst belongs to one minute.
	if left := time.Until(now.Truncate(time.Minute).Add(time.Minute)); left < 5*time.Second {
		time.Sleep(left + 50*time.Millisecond)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	const limit = 17
	for i := 0; i < 120; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache := &RPMCacheImpl{rdb: clients[i%len(clients)]}
			ok, count, _, err := cache.TryAcquireRPM(ctx, owner, limit)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			if count > limit {
				t.Errorf("count=%d", count)
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, limit, accepted.Load())
	cache := &RPMCacheImpl{rdb: clients[0]}
	ok, count, resetAt, err := cache.TryAcquireRPM(ctx, owner, limit)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, limit, count)
	keys, err := clients[0].Keys(ctx, fmt.Sprintf("%s%d:*", rpmKeyPrefix, owner)).Result()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	ttl, err := clients[0].TTL(ctx, keys[0]).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, 120*time.Second)
	serverNow, err := clients[0].Time(ctx).Result()
	require.NoError(t, err)
	oldMinute := serverNow.Unix()/60 - 1
	staleKey := fmt.Sprintf("%s%d:%d", rpmKeyPrefix, owner, oldMinute)
	result, err := tryRPMScript.Run(ctx, clients[0], []string{staleKey}, limit, 120, oldMinute).Slice()
	require.NoError(t, err)
	require.Equal(t, int64(-1), result[0])
	require.EqualValues(t, 0, clients[0].Exists(ctx, staleKey).Val())
	time.Sleep(time.Until(resetAt) + 75*time.Millisecond)
	ok, count, _, err = cache.TryAcquireRPM(ctx, owner, limit)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, count)
}
