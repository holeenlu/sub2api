//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSImagePolicyRealRedis(t *testing.T) {
	ctx := context.Background()
	cache := &gatewayCache{rdb: integrationRedis}
	scope := fmt.Sprintf("image-policy-integration:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		require.NoError(t, integrationRedis.Del(ctx,
			imagePolicyKey(scope, "progress"), imagePolicyKey(scope, "warning"),
			imagePolicyKey(scope+"-other", "warning")).Err())
	})

	require.NoError(t, cache.SaveBPSImageProgress(ctx, scope, "", "first"))
	require.Error(t, cache.SaveBPSImageProgress(ctx, scope, "", "stale"))
	require.NoError(t, cache.SaveBPSImageProgress(ctx, scope, "first", "next"))
	require.NoError(t, integrationRedis.Expire(ctx, imagePolicyKey(scope, "progress"), time.Minute).Err())
	value, err := cache.LoadBPSImageProgress(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, "next", value)
	ttl, err := integrationRedis.TTL(ctx, imagePolicyKey(scope, "progress")).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 119*time.Minute)
	require.LessOrEqual(t, ttl, 2*time.Hour)

	var first atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := cache.ClaimBPSImageWarning(ctx, scope)
			if err != nil {
				t.Error(err)
				return
			}
			if claimed {
				first.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, first.Load())
	claimed, err := cache.ClaimBPSImageWarning(ctx, scope+"-other")
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, cache.ResetBPSImageWarning(ctx, scope))
	claimed, err = cache.ClaimBPSImageWarning(ctx, scope)
	require.NoError(t, err)
	require.True(t, claimed)
}

// The protocol's existing miniredis tests exercise expiry/LRU boundaries. This
// complementary test executes the real Lua/CAS path through two Redis clients.
func TestBPSSharedStateRealRedisConcurrentCAS(t *testing.T) {
	ctx := context.Background()
	second := redis.NewClient(integrationRedis.Options())
	t.Cleanup(func() { _ = second.Close() })
	// Use the real shared-key layout with a unique scope. No flush is allowed.
	a, b := &gatewayCache{rdb: integrationRedis}, &gatewayCache{rdb: second}
	scope := fmt.Sprintf("bps-real-state:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		key := bpsStateKey(scope, "catalog")
		weight, _ := integrationRedis.HGet(ctx, bpsStatePrefix+"sizes", key).Int64()
		pipe := integrationRedis.TxPipeline()
		pipe.Del(ctx, key)
		pipe.ZRem(ctx, bpsStatePrefix+"lru", key)
		pipe.HDel(ctx, bpsStatePrefix+"sizes", key)
		pipe.HIncrBy(ctx, bpsStatePrefix+"sizes", "__bytes", -weight)
		_, err := pipe.Exec(ctx)
		require.NoError(t, err)
	})
	zero := uint64(0)
	ok, err := a.SaveBPSState(ctx, scope, "catalog", &zero, []byte("original"))
	require.NoError(t, err)
	require.True(t, ok)
	raw, version, err := b.LoadBPSState(ctx, scope, "catalog")
	require.NoError(t, err)
	require.Equal(t, "original", string(raw))
	require.NotZero(t, version)
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cache := a
			if i%2 == 1 {
				cache = b
			}
			saved, e := cache.SaveBPSState(ctx, scope, "catalog", &version, []byte(fmt.Sprint(i)))
			if e != nil {
				t.Error(e)
			}
			if saved {
				winners.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 1, winners.Load())
	raw, _, err = b.LoadBPSState(ctx, scope+"/other-credential", "catalog")
	require.NoError(t, err)
	require.Empty(t, raw)
	require.NoError(t, integrationRedis.PExpire(ctx, bpsStateKey(scope, "catalog"), time.Millisecond).Err())
	require.Eventually(t, func() bool { return integrationRedis.Exists(ctx, bpsStateKey(scope, "catalog")).Val() == 0 }, time.Second, time.Millisecond)
	raw, version, err = a.LoadBPSState(ctx, scope, "catalog")
	require.NoError(t, err)
	require.Empty(t, raw)
	require.Zero(t, version)
}
