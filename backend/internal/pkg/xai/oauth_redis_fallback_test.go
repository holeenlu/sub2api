//go:build unit

package xai

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSessionStoreRedisFallbackIsLimitedToFailedWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	store := NewRedisSessionStore(client)
	if err := store.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer store.Stop(context.Background())
	session := func(state string) *OAuthSession { return &OAuthSession{State: state, CreatedAt: time.Now()} }

	store.Set(context.Background(), "remote", session("remote"))
	require.NoError(t, store.remote.Delete(context.Background(), "remote"))
	_, ok := store.Get(context.Background(), "remote")
	require.False(t, ok, "a remote miss must not revive the stale local copy")

	mr.Close()
	store.Set(context.Background(), "local-only", session("local"))
	got, ok := store.Get(context.Background(), "local-only")
	require.True(t, ok)
	require.Equal(t, "local", got.State)
	require.True(t, store.TryConsumeSession(context.Background(), "local-only"))
	require.False(t, store.TryConsumeSession(context.Background(), "local-only"))
}
