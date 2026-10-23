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

func TestVendorProxyCacheRejectsLateVersionAndExpires(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	cache := NewProxyLatencyCache(client)
	ctx := context.Background()
	require.NoError(t, cache.SetProxyLatency(ctx, 7, &service.ProxyLatencyInfo{ConfigVersion: 2, Success: true, UpdatedAt: time.Now()}))
	require.NoError(t, cache.SetProxyLatency(ctx, 7, &service.ProxyLatencyInfo{ConfigVersion: 1, Success: false, UpdatedAt: time.Now()}))
	got, err := cache.GetProxyLatencies(ctx, []int64{7})
	require.NoError(t, err)
	require.Equal(t, int64(2), got[7].ConfigVersion)
	require.True(t, got[7].Success)
	r.FastForward(24*time.Hour + time.Second)
	got, err = cache.GetProxyLatencies(ctx, []int64{7})
	require.NoError(t, err)
	require.Empty(t, got)
	// Historical native unversioned cache semantics remain unchanged.
	require.NoError(t, cache.SetProxyLatency(ctx, 8, &service.ProxyLatencyInfo{Success: true}))
	require.Equal(t, time.Duration(0), r.TTL(proxyLatencyKey(8)))
}
