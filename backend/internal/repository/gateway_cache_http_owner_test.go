package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newHTTPResponseOwnerTestCache(t *testing.T) (*gatewayCache, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return &gatewayCache{rdb: client}, mr
}

func TestHTTPResponseOwnerSharedExpiryAndLegacyReaders(t *testing.T) {
	cache, mr := newHTTPResponseOwnerTestCache(t)
	ctx := context.Background()
	writer := service.NewOpenAIWSStateStore(cache)
	reader := service.NewOpenAIWSStateStore(cache)
	require.NoError(t, writer.BindHTTPResponseOwner(ctx, 17, "resp_fixture", 101, 201, time.Second))
	for _, store := range []service.OpenAIWSStateStore{writer, reader} {
		user, key, found, err := store.GetHTTPResponseOwner(ctx, 17, "resp_fixture")
		require.NoError(t, err)
		require.True(t, found)
		require.EqualValues(t, 101, user)
		require.EqualValues(t, 201, key)
	}
	keys := httpResponseOwnerKeys(17, "resp_fixture")
	for _, key := range keys {
		require.NotContains(t, key, "resp_fixture", "Redis keys must only contain a response digest")
		require.Equal(t, time.Second, mr.TTL(key))
	}
	user, err := mr.Get(keys[1])
	require.NoError(t, err)
	require.Equal(t, "101", user, "old readers retain their integer wire format")
	apiKey, err := mr.Get(keys[2])
	require.NoError(t, err)
	require.Equal(t, "201", apiKey)
	_, _, found, err := reader.GetHTTPResponseOwner(ctx, 18, "resp_fixture")
	require.NoError(t, err)
	require.False(t, found, "groups must not share owner grants")
	mr.FastForward(2 * time.Second)
	for _, store := range []service.OpenAIWSStateStore{writer, reader, service.NewOpenAIWSStateStore(cache)} {
		_, _, found, err = store.GetHTTPResponseOwner(ctx, 17, "resp_fixture")
		require.NoError(t, err)
		require.False(t, found, "neither a warm node nor a restarted node may extend shared expiry")
	}
}

func TestHTTPResponseOwnerLegacyAndInvalidRecords(t *testing.T) {
	cache, mr := newHTTPResponseOwnerTestCache(t)
	ctx := context.Background()
	keys := httpResponseOwnerKeys(17, "resp_legacy")
	// Simulate the existing old writer; both compatibility keys are required.
	require.NoError(t, mr.Set(keys[1], "101"))
	_, _, found, err := cache.GetHTTPResponseOwner(ctx, 17, "resp_legacy")
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, mr.Set(keys[2], "201"))
	user, key, found, err := cache.GetHTTPResponseOwner(ctx, 17, "resp_legacy")
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 101, user)
	require.EqualValues(t, 201, key)
	require.NoError(t, cache.SetHTTPResponseOwner(ctx, 17, "resp_legacy", 101, 201, time.Minute))
	require.NoError(t, mr.Set(keys[1], "999"))
	_, _, found, err = cache.GetHTTPResponseOwner(ctx, 17, "resp_legacy")
	require.ErrorIs(t, err, errHTTPResponseOwnerUnavailable)
	require.False(t, found, "an old writer must not silently contradict the v1 owner")
	require.NoError(t, mr.Set(keys[1], "101"))
	for _, invalid := range []string{"v2:101:201", "v1:101", "v1:0:201", "v1:101:bad", "v1:101:201:extra", ""} {
		require.NoError(t, mr.Set(keys[0], invalid))
		_, _, found, err = cache.GetHTTPResponseOwner(ctx, 17, "resp_legacy")
		require.ErrorIs(t, err, errHTTPResponseOwnerUnavailable)
		require.False(t, found, "invalid v1 must not silently fall back to legacy grants")
	}
	mr.Del(keys[0])
	mr.HSet(keys[0], "wrong", "type")
	_, _, found, err = cache.GetHTTPResponseOwner(ctx, 17, "resp_legacy")
	require.ErrorIs(t, err, errHTTPResponseOwnerUnavailable)
	require.False(t, found)
}

func TestHTTPResponseOwnerFailedPublicationAndSharedDeletion(t *testing.T) {
	cache, mr := newHTTPResponseOwnerTestCache(t)
	ctx := context.Background()
	store := service.NewOpenAIWSStateStore(cache)
	mr.SetError("synthetic-private-topology")
	err := store.BindHTTPResponseOwner(ctx, 17, "resp_failed", 101, 201, time.Minute)
	require.ErrorIs(t, err, errHTTPResponseOwnerUnavailable)
	require.NotContains(t, err.Error(), "synthetic-private")
	mr.SetError("")
	_, _, found, err := store.GetHTTPResponseOwner(ctx, 17, "resp_failed")
	require.NoError(t, err)
	require.False(t, found, "failed shared publication must not grant from local memory")
	require.NoError(t, store.BindHTTPResponseOwner(ctx, 17, "resp_failed", 101, 201, time.Minute))
	_, _, found, err = store.GetHTTPResponseOwner(ctx, 17, "resp_failed")
	require.NoError(t, err)
	require.True(t, found)
	mr.SetError("synthetic-private-topology")
	_, _, found, err = store.GetHTTPResponseOwner(ctx, 17, "resp_failed")
	require.ErrorIs(t, err, errHTTPResponseOwnerUnavailable)
	require.False(t, found, "a warm process must not bypass shared owner availability")
	mr.SetError("")
	for _, key := range httpResponseOwnerKeys(17, "resp_failed") {
		require.True(t, mr.Del(key))
	}
	_, _, found, err = store.GetHTTPResponseOwner(ctx, 17, "resp_failed")
	require.NoError(t, err)
	require.False(t, found, "deleting all shared formats must revoke warm process grants")
}

func TestHTTPResponseOwnerRealRedisAtomicPairs(t *testing.T) {
	raw := os.Getenv("TEST_REDIS_URL")
	if raw == "" {
		t.Skip("TEST_REDIS_URL is required for isolated real Redis")
	}
	opts, err := redis.ParseURL(raw)
	require.NoError(t, err)
	client := redis.NewClient(opts)
	defer client.Close()
	ctx := context.Background()
	require.NoError(t, client.Ping(ctx).Err())
	cache := &gatewayCache{rdb: client}
	response := fmt.Sprintf("owner-test-%d", time.Now().UnixNano())
	keys := httpResponseOwnerKeys(713, response)
	defer client.Del(ctx, keys...)
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	for i := int64(1); i <= 20; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			for n := 0; n < 10; n++ {
				if err := cache.SetHTTPResponseOwner(ctx, 713, response, id, id+100, time.Minute); err != nil {
					failures <- err
					return
				}
				user, key, found, err := cache.GetHTTPResponseOwner(ctx, 713, response)
				if err != nil || !found || key != user+100 {
					failures <- fmt.Errorf("mixed owner pair %d/%d found=%t err=%v", user, key, found, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	warm := service.NewOpenAIWSStateStore(cache)
	_, _, found, err := warm.GetHTTPResponseOwner(ctx, 713, response)
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, client.Del(ctx, keys...).Err())
	_, _, found, err = warm.GetHTTPResponseOwner(ctx, 713, response)
	require.NoError(t, err)
	require.False(t, found)
}
