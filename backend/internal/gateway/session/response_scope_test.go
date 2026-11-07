package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResponseScopeInvalidationRetainsProducerAndOriginalExpiry(t *testing.T) {
	for _, scope := range []ResponseScope{ResponseScopeUnknown, ResponseScopePersisted, ResponseScopeConnectionLocal} {
		t.Run(map[ResponseScope]string{ResponseScopeUnknown: "unknown", ResponseScopePersisted: "persisted", ResponseScopeConnectionLocal: "local"}[scope], func(t *testing.T) {
			s := NewStateStore(nil)
			s.BindResponseConn("parent", "producer", time.Minute, scope)
			s.responseToConnMu.RLock()
			expiry := s.responseToConn["parent"].expiresAt
			s.responseToConnMu.RUnlock()
			s.DeleteResponseConn("parent", "different")
			conn, actualScope, found := s.GetResponseContinuation("parent")
			require.True(t, found)
			require.Equal(t, "producer", conn)
			require.Equal(t, scope, actualScope)
			s.DeleteResponseConn("parent", "producer")
			conn, actualScope, found = s.GetResponseContinuation("parent")
			if scope == ResponseScopeUnknown {
				require.False(t, found)
				return
			}
			require.True(t, found)
			require.Empty(t, conn)
			require.Equal(t, scope, actualScope)
			s.responseToConnMu.Lock()
			binding := s.responseToConn["parent"]
			require.Equal(t, expiry, binding.expiresAt, "socket invalidation cannot renew the producer record")
			binding.expiresAt = time.Now().Add(-time.Second)
			s.responseToConn["parent"] = binding
			s.responseToConnMu.Unlock()
			_, _, found = s.GetResponseContinuation("parent")
			require.False(t, found)
		})
	}
}

func TestResponseScopeCanRecordKnownLostSocketWithoutMakingItUnknown(t *testing.T) {
	s := NewStateStore(nil)
	s.BindResponseConn("local", "", time.Minute, ResponseScopeConnectionLocal)
	conn, scope, found := s.GetResponseContinuation("local")
	require.True(t, found)
	require.Empty(t, conn)
	require.Equal(t, ResponseScopeConnectionLocal, scope)
	_, hint := s.GetResponseConn("local")
	require.False(t, hint)
	s.BindResponseConn("unknown", "", time.Minute, ResponseScopeUnknown)
	_, _, found = s.GetResponseContinuation("unknown")
	require.False(t, found)
	s.BindResponseConn("local", "successor", time.Minute, ResponseScopePersisted)
	s.DeleteResponseConn("local", "old-socket")
	conn, scope, found = s.GetResponseContinuation("local")
	require.True(t, found)
	require.Equal(t, "successor", conn)
	require.Equal(t, ResponseScopePersisted, scope)
}
