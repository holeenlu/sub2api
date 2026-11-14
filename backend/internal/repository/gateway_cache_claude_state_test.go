package repository

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestClaudeConversationCacheOwnershipExpiryAndBounds(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client)
	ctx := context.Background()
	key := strings.Repeat("a", 64)
	require.NoError(t, cache.StoreClaudeConversationState(ctx, service.ClaudeStateSignatures, map[string]string{key: "owner-a"}))
	require.NoError(t, cache.StoreClaudeConversationState(ctx, service.ClaudeStateThreads, map[string]string{key: "thread-a"}))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			require.NoError(t, cache.StoreClaudeConversationState(ctx, service.ClaudeStateSignatures, map[string]string{key: "owner-b"}))
		})
	}
	wg.Wait()
	got, err := cache.GetClaudeConversationState(ctx, service.ClaudeStateSignatures, []string{key})
	require.NoError(t, err)
	require.Equal(t, "owner-a", got[key], "another replica cannot take ownership")
	server.FastForward(25 * time.Hour)
	threads, err := cache.GetClaudeConversationState(ctx, service.ClaudeStateThreads, []string{key})
	require.NoError(t, err)
	require.Empty(t, threads)
	got, err = cache.GetClaudeConversationState(ctx, service.ClaudeStateSignatures, []string{key})
	require.NoError(t, err)
	require.Equal(t, "owner-a", got[key])
	require.Equal(t, 30*24*time.Hour, server.TTL("claude_state:{signatures}:"+key), "reading history refreshes ownership after a day")
	server.FastForward(30 * 24 * time.Hour)
	got, err = cache.GetClaudeConversationState(ctx, service.ClaudeStateSignatures, []string{key})
	require.NoError(t, err)
	require.Empty(t, got)
	require.Error(t, cache.StoreClaudeConversationState(ctx, service.ClaudeStateSignatures, map[string]string{"bad-key": "owner"}))
	require.Error(t, cache.StoreClaudeConversationState(ctx, service.ClaudeStateSignatures, map[string]string{key: strings.Repeat("x", 16385)}))
	// Exercise the same bounded eviction script at a small capacity.
	prefix, _, _, _ := claudeStateStorage(service.ClaudeStateThreads)
	for n, value := range []string{"b", "c", "d"} {
		require.NoError(t, storeClaudeStateScript.Run(ctx, client, []string{prefix + "index"}, time.Now().Unix()+int64(n), 86400, 2, prefix, "0", strings.Repeat(value, 64), value).Err())
	}
	require.Equal(t, int64(2), client.ZCard(ctx, prefix+"index").Val())
	require.False(t, server.Exists(prefix+strings.Repeat("b", 64)))
}
