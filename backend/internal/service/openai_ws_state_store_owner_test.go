//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSLocalBindingsFenceLatePublishAndSameValueCleanup(t *testing.T) {
	var registry session.OwnerRegistry
	scope := session.OwnerScope{GroupID: 7, APIKeyID: 11, SessionHash: "s"}
	first, _ := registry.Acquire(t.Context(), scope, "first", nil, nil, nil)
	t.Cleanup(first.Close)
	oldCtx := context.WithValue(first.Context(), openAIWSSessionPreemptContextKey{}, first)
	store := NewOpenAIWSStateStore(nil)
	bind := func() {
		store.BindResponseConn("r", "same-connection", time.Minute, openAIWSResponseScopeForStore(true))
		store.BindSessionConn(7, "s", "same-connection", time.Minute)
		store.BindSessionTurnState(7, "s", "same-state", time.Minute)
	}
	require.True(t, publishOpenAIWSLocalBindings(oldCtx, bind))
	next, _ := registry.Acquire(t.Context(), scope, "next", nil, nil, nil)
	t.Cleanup(next.Close)
	newCtx := context.WithValue(next.Context(), openAIWSSessionPreemptContextKey{}, next)
	require.True(t, publishOpenAIWSLocalBindings(newCtx, bind))
	require.False(t, publishOpenAIWSLocalBindings(oldCtx, func() {
		store.BindSessionConn(7, "s", "stale", time.Minute)
	}))
	require.False(t, publishOpenAIWSLocalBindings(oldCtx, func() {
		store.DeleteResponseConn("r", "same-connection")
		store.DeleteSessionConn(7, "s", "same-connection")
		store.DeleteSessionTurnState(7, "s", "same-state")
	}))
	conn, ok := store.GetResponseConn("r")
	require.True(t, ok)
	require.Equal(t, "same-connection", conn)
	conn, ok = store.GetSessionConn(7, "s")
	require.True(t, ok)
	require.Equal(t, "same-connection", conn)
	state, ok := store.GetSessionTurnState(7, "s")
	require.True(t, ok)
	require.Equal(t, "same-state", state)
	next.Close()
	require.False(t, publishOpenAIWSLocalBindings(newCtx, bind))
}

func TestOpenAIWSLocalBindingsRetainUnarmedMode(t *testing.T) {
	store := NewOpenAIWSStateStore(nil)
	require.True(t, publishOpenAIWSLocalBindings(context.Background(), func() {
		store.BindSessionConn(0, "http-one-shot", "connection", time.Minute)
	}))
	conn, ok := store.GetSessionConn(0, "http-one-shot")
	require.True(t, ok)
	require.Equal(t, "connection", conn)
}
