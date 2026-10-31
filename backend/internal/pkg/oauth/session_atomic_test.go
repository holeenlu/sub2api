package oauth

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSharedSessionClaimsHaveExactlyOneWinnerAcrossClients(t *testing.T) {
	server := miniredis.RunT(t)
	ctx := context.Background()
	a := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer a.Close()
	b := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer b.Close()
	value := map[string]any{"state": "verified-state", "vendor": 1, "policy": 7, "status": "pending"}
	require.NoError(t, PutSharedSession(ctx, a, "test", value, time.Minute))
	_, raw, err := ReadSharedSession[map[string]any](ctx, b, "test")
	require.NoError(t, err)
	var won atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := a
			if i%2 == 1 {
				client = b
			}
			if _, err := CompareSwapSession(ctx, client, "test", raw, map[string]any{"status": "claimed", "claimant": i}); err == nil {
				won.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, int32(1), won.Load())
}
func TestSharedSessionRebindPreservesExpiryAndInvalidatesStaleClaim(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rdb.Close()
	ctx := context.Background()
	require.NoError(t, PutSharedSession(ctx, rdb, "test", map[string]string{"state": "same", "pkce": "same", "proxy": "1"}, time.Minute))
	_, old, err := ReadSharedSession[map[string]string](ctx, rdb, "test")
	require.NoError(t, err)
	server.FastForward(20 * time.Second)
	next, err := CompareSwapSession(ctx, rdb, "test", old, map[string]string{"state": "same", "pkce": "same", "proxy": "2"})
	require.NoError(t, err)
	_, err = CompareSwapSession(ctx, rdb, "test", old, nil)
	require.ErrorIs(t, err, ErrSessionConflict)
	value, _, err := ReadSharedSession[map[string]string](ctx, rdb, "test")
	require.NoError(t, err)
	require.Equal(t, "same", (*value)["pkce"])
	require.Equal(t, "2", (*value)["proxy"])
	require.LessOrEqual(t, server.TTL("test"), 40*time.Second)
	server.FastForward(41 * time.Second)
	_, err = CompareSwapSession(ctx, rdb, "test", next, nil)
	require.ErrorIs(t, err, ErrSessionConflict)
}
func TestSharedSessionOutageDoesNotFallback(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1, DialTimeout: 50 * time.Millisecond})
	defer rdb.Close()
	require.NoError(t, PutSharedSession(context.Background(), rdb, "test", 1, time.Minute))
	server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := ReadSharedSession[int](ctx, rdb, "test")
	require.Error(t, err)
	_, err = CompareSwapSession(ctx, rdb, "test", "1", nil)
	require.Error(t, err)
}
func TestSessionConsumeRejectsReplacementAndConcurrentReplay(t *testing.T) {
	s := NewSessionStore()
	defer s.Stop(context.Background())
	one := &OAuthSession{CreatedAt: time.Now()}
	s.Set("id", one)
	s.Set("id", &OAuthSession{CreatedAt: time.Now()})
	require.False(t, s.Consume("id", one))
	for i := 0; i < 10; i++ {
		id := fmt.Sprint(i)
		s.Set(id, one)
		require.True(t, s.Consume(id, one))
		require.False(t, s.Consume(id, one))
	}
}
