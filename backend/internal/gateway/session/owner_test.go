package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type ownerCacheStub struct {
	mu           sync.Mutex
	values       map[string]string
	claimErr     error
	renewStarted chan struct{}
}

func (c *ownerCacheStub) ClaimOpenAIResponsesSessionWindow(_ context.Context, group int64, hash string, token []byte, _ time.Duration) ([]byte, error) {
	if c.claimErr != nil {
		return nil, c.claimErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = make(map[string]string)
	}
	key := fmt.Sprintf("%d:%s", group, hash)
	previous := c.values[key]
	c.values[key] = string(token)
	return []byte(previous), nil
}
func (c *ownerCacheStub) CompareAndRefreshOpenAIResponsesSessionWindow(ctx context.Context, group int64, hash string, token []byte, _ time.Duration) (bool, error) {
	if c.renewStarted != nil {
		close(c.renewStarted)
		<-ctx.Done()
		return false, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[fmt.Sprintf("%d:%s", group, hash)] == string(token), nil
}
func (c *ownerCacheStub) CompareAndDeleteOpenAIResponsesSessionWindow(_ context.Context, group int64, hash string, token []byte) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := fmt.Sprintf("%d:%s", group, hash)
	if c.values[key] != string(token) {
		return false, nil
	}
	delete(c.values, key)
	return true, nil
}

func TestOwnerScopeRejectsUnboundIdentity(t *testing.T) {
	for _, scope := range []OwnerScope{{GroupID: 1, APIKeyID: 2, SessionHash: " "}, {}, {GroupID: 1, APIKeyID: 2}, {GroupID: 1, SessionHash: "s"}, {APIKeyID: 2, SessionHash: "s"}, {GroupID: -1, APIKeyID: 2, SessionHash: "s"}} {
		var registry OwnerRegistry
		owner, replaced := registry.Acquire(context.Background(), scope, "token", nil, nil, nil)
		require.Nil(t, owner)
		require.False(t, replaced)
		require.Empty(t, registry.active)
	}
	var registry OwnerRegistry
	owner, _ := registry.Acquire(nil, OwnerScope{GroupID: 1, APIKeyID: 2, SessionHash: " s "}, "t", nil, nil, nil)
	require.Equal(t, "s", owner.scope.SessionHash)
	owner.Close()
}

func TestOwnerEpochScopesAndStaleCleanup(t *testing.T) {
	var registry OwnerRegistry
	scope := OwnerScope{GroupID: 7, APIKeyID: 11, SessionHash: "sess"}
	first, replaced := registry.Acquire(context.Background(), scope, "a", nil, nil, nil)
	require.False(t, replaced)
	t.Cleanup(first.Close)
	for _, other := range []OwnerScope{{7, 12, "sess"}, {8, 11, "sess"}, {7, 11, "other-stream"}} {
		owner, replaced := registry.Acquire(context.Background(), other, "x", nil, nil, nil)
		require.False(t, replaced)
		t.Cleanup(owner.Close)
		t.Cleanup(func() { require.False(t, owner.Preempted()) })
	}
	second, replaced := registry.Acquire(context.Background(), scope, "b", nil, nil, nil)
	require.True(t, replaced)
	t.Cleanup(second.Close)
	require.Greater(t, second.Epoch(), first.Epoch())
	require.True(t, first.Preempted())
	require.ErrorIs(t, context.Cause(first.Context()), ErrPreempted)
	require.False(t, first.Current())
	require.True(t, second.Current())
	first.Close()
	first.Close()
	require.True(t, second.Current(), "a stale close cannot remove the successor epoch")
}

func TestOwnerRemoteClaimAndStaleReleasePreserveWireProtocol(t *testing.T) {
	cache := &ownerCacheStub{}
	var left, right OwnerRegistry
	scope := OwnerScope{GroupID: 7, APIKeyID: 11, SessionHash: "sess"}
	first, _ := left.Acquire(context.Background(), scope, "owner-a", cache, nil, nil)
	second, replaced := right.Acquire(context.Background(), scope, "owner-b", cache, nil, nil)
	require.True(t, replaced)
	t.Cleanup(second.Close)
	first.Close()
	cache.mu.Lock()
	current := cache.values["7:wspreempt:11:sess"]
	cache.mu.Unlock()
	require.Equal(t, "owner-b", current)
	second.Close()
	require.Empty(t, cache.values)
}

func TestOwnerLostRemoteTokenCancelsWithoutNewClaim(t *testing.T) {
	cache := &ownerCacheStub{}
	var registry OwnerRegistry
	scope := OwnerScope{GroupID: 7, APIKeyID: 11, SessionHash: "s"}
	owner, _ := registry.Acquire(context.Background(), scope, "a", cache, nil, nil)
	t.Cleanup(owner.Close)
	cache.mu.Lock()
	cache.values["7:wspreempt:11:s"] = "b"
	cache.mu.Unlock()
	select {
	case <-owner.Context().Done():
	case <-time.After(OwnerWatchInterval + time.Second):
		t.Fatal("lost owner was not canceled")
	}
	require.ErrorIs(t, context.Cause(owner.Context()), ErrPreempted)
	owner.Close()
	require.Equal(t, "b", cache.values["7:wspreempt:11:s"])
}

func TestOwnerNotificationCanceledAndJoined(t *testing.T) {
	var registry OwnerRegistry
	started, finished := make(chan struct{}), make(chan struct{})
	var causeAtNotify error
	first, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, "a", nil, nil, func(ctx context.Context) {
		causeAtNotify = ctx.Err()
		close(started)
		<-ctx.Done()
		close(finished)
	})
	t.Cleanup(first.Close)
	second, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, "b", nil, nil, nil)
	t.Cleanup(second.Close)
	require.True(t, first.Preempted(), "visible before notification/cancellation")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("notification not started")
	}
	require.NoError(t, causeAtNotify)
	start := time.Now()
	first.Close()
	require.Less(t, time.Since(start), time.Second)
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before its notification joined")
	}
	require.ErrorIs(t, context.Cause(first.Context()), ErrPreempted)
}

func TestOwnerCleanupInterruptsAndJoinsPendingRenewal(t *testing.T) {
	cache := &ownerCacheStub{renewStarted: make(chan struct{})}
	var registry OwnerRegistry
	owner, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, "a", cache, nil, nil)
	t.Cleanup(owner.Close)
	select {
	case <-cache.renewStarted:
	case <-time.After(OwnerWatchInterval + time.Second):
		t.Fatal("renewal not started")
	}
	start := time.Now()
	owner.Close()
	require.Less(t, time.Since(start), time.Second, "cleanup must cancel Redis I/O rather than wait for the independent 3s timeout")
	require.Empty(t, cache.values)
	require.Empty(t, registry.active)
}

func TestOwnerGraceCancelsBeforeSlowBoundedNotificationJoins(t *testing.T) {
	var registry OwnerRegistry
	started, release := make(chan struct{}), make(chan struct{})
	first, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, "a", nil, nil, func(context.Context) {
		close(started)
		<-release // stand-in for the transport's independent bounded close
	})
	t.Cleanup(first.Close)
	t.Cleanup(func() { close(release) })
	second, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, "b", nil, nil, nil)
	t.Cleanup(second.Close)
	<-started
	select {
	case <-first.Context().Done():
		require.ErrorIs(t, context.Cause(first.Context()), ErrPreempted)
	case <-time.After(OwnerCloseGrace + time.Second):
		t.Fatal("execution cancellation waited for the close handshake")
	}
	select {
	case <-first.done:
		t.Fatal("notification worker was abandoned instead of joined")
	default:
	}
}

func TestOwnerLocalFallbackDoesNotClaimSharedAuthority(t *testing.T) {
	cache := &ownerCacheStub{claimErr: errors.New("unavailable")}
	var registry OwnerRegistry
	owner, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, "a", cache, nil, nil)
	t.Cleanup(owner.Close)
	require.False(t, owner.remote)
	require.True(t, owner.Current())
	require.Empty(t, cache.values)
}

func TestCanceledOwnerAcquisitionCannotPreemptCurrent(t *testing.T) {
	cache := &ownerCacheStub{}
	var registry OwnerRegistry
	scope := OwnerScope{7, 11, "s"}
	current, _ := registry.Acquire(context.Background(), scope, "current", cache, nil, nil)
	t.Cleanup(current.Close)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	owner, replaced := registry.Acquire(ctx, scope, "canceled", cache, nil, nil)
	require.Nil(t, owner)
	require.False(t, replaced)
	require.True(t, current.Current())
	cache.mu.Lock()
	require.Equal(t, "current", cache.values["7:wspreempt:11:s"])
	cache.mu.Unlock()
}

func TestOwnerFencesPublicationAndLateInvalidation(t *testing.T) {
	var registry OwnerRegistry
	store := NewStateStore(nil)
	scope := OwnerScope{7, 11, "s"}
	var invalidations atomic.Int64
	invalidate := func() { invalidations.Add(1); store.DeleteSessionConn(7, "s"); store.DeleteSessionTurnState(7, "s") }
	first, _ := registry.Acquire(context.Background(), scope, "a", nil, invalidate, nil)
	t.Cleanup(first.Close)
	require.True(t, first.Publish(func() { store.BindSessionConn(7, "s", "old", time.Minute) }))
	second, _ := registry.Acquire(context.Background(), scope, "b", nil, invalidate, nil)
	t.Cleanup(second.Close)
	require.True(t, second.Publish(func() { store.BindSessionConn(7, "s", "new", time.Minute) }))
	first.preempt()
	first.Close()
	require.False(t, first.Publish(func() { store.BindSessionConn(7, "s", "stale", time.Minute) }))
	conn, found := store.GetSessionConn(7, "s")
	require.True(t, found)
	require.Equal(t, "new", conn)
	require.EqualValues(t, 1, invalidations.Load(), "old callback must not invalidate after replacement")
	second.preempt()
	require.EqualValues(t, 2, invalidations.Load())
	_, found = store.GetSessionConn(7, "s")
	require.False(t, found)
}

func TestOwnerConcurrentReplacementAndClose(t *testing.T) {
	var registry OwnerRegistry
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Go(func() {
			owner, _ := registry.Acquire(context.Background(), OwnerScope{7, 11, "s"}, fmt.Sprint(i), nil, nil, nil)
			owner.Publish(func() {})
			owner.Close()
		})
	}
	workers.Wait()
	require.Empty(t, registry.active)
}
