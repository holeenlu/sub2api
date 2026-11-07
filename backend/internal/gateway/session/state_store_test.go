package session

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOpenAIWSStateStore_MaybeCleanupRemovesExpiredIncrementally(t *testing.T) {
	store := NewStateStore(nil)

	expiredAt := time.Now().Add(-time.Minute)
	total := 2048
	store.responseToConnMu.Lock()
	for i := 0; i < total; i++ {
		store.responseToConn[fmt.Sprintf("resp_%d", i)] = openAIWSConnBinding{
			connID:    "conn_incremental",
			expiresAt: expiredAt,
		}
	}
	store.responseToConnMu.Unlock()

	store.lastCleanupUnixNano.Store(time.Now().Add(-2 * openAIWSStateStoreCleanupInterval).UnixNano())
	store.maybeCleanup()

	store.responseToConnMu.RLock()
	remainingAfterFirst := len(store.responseToConn)
	store.responseToConnMu.RUnlock()
	require.Less(t, remainingAfterFirst, total, "单轮 cleanup 应至少有进展")
	require.Greater(t, remainingAfterFirst, 0, "增量清理不要求单轮清空全部键")

	for i := 0; i < 8; i++ {
		store.lastCleanupUnixNano.Store(time.Now().Add(-2 * openAIWSStateStoreCleanupInterval).UnixNano())
		store.maybeCleanup()
	}

	store.responseToConnMu.RLock()
	remaining := len(store.responseToConn)
	store.responseToConnMu.RUnlock()
	require.Zero(t, remaining, "多轮 cleanup 后应逐步清空全部过期键")
}

func TestEnsureBindingCapacity_EvictsOneWhenMapIsFull(t *testing.T) {
	bindings := map[string]int{
		"a": 1,
		"b": 2,
	}

	ensureBindingCapacity(bindings, "c", 2)
	bindings["c"] = 3

	require.Len(t, bindings, 2)
	require.Equal(t, 3, bindings["c"])
}

func TestEnsureBindingCapacity_DoesNotEvictWhenUpdatingExistingKey(t *testing.T) {
	bindings := map[string]int{
		"a": 1,
		"b": 2,
	}

	ensureBindingCapacity(bindings, "a", 2)
	bindings["a"] = 9

	require.Len(t, bindings, 2)
	require.Equal(t, 9, bindings["a"])
}

func TestWithOpenAIWSStateStoreRedisTimeout_WithParentContext(t *testing.T) {
	ctx, cancel := withCacheTimeout(context.Background())
	defer cancel()
	require.NotNil(t, ctx)
	_, ok := ctx.Deadline()
	require.True(t, ok, "应附加短超时")
}

func TestBindingCleanupExpiresAtExactDeadline(t *testing.T) {
	now := time.Now()
	accounts := map[string]openAIWSAccountBinding{"expired": {accountID: 1, expiresAt: now}, "future": {accountID: 2, expiresAt: now.Add(time.Second)}}
	owners := map[string]openAIHTTPResponseOwnerBinding{"expired": {userID: 1, apiKeyID: 2, expiresAt: now}}
	connections := map[string]openAIWSConnBinding{"expired": {connID: "c", expiresAt: now}}
	turns := map[string]openAIWSTurnStateBinding{"expired": {turnState: "s", expiresAt: now}}
	sessions := map[string]openAIWSSessionConnBinding{"expired": {connID: "c", expiresAt: now}}
	cleanupExpiredAccountBindings(accounts, now, 10)
	cleanupExpiredHTTPResponseOwnerBindings(owners, now, 10)
	cleanupExpiredConnBindings(connections, now, 10)
	cleanupExpiredTurnStateBindings(turns, now, 10)
	cleanupExpiredSessionConnBindings(sessions, now, 10)
	require.Len(t, accounts, 1)
	require.Contains(t, accounts, "future")
	require.Empty(t, owners)
	require.Empty(t, connections)
	require.Empty(t, turns)
	require.Empty(t, sessions)
}
