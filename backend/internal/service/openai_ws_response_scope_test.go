package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSParentContextFollowsProducerScope(t *testing.T) {
	for _, tc := range []struct {
		name, accountType, conn string
		scope                   session.ResponseScope
		bind, required, denied  bool
	}{
		{"unknown_apikey", AccountTypeAPIKey, "", session.ResponseScopeUnknown, false, false, false},
		{"unknown_oauth", AccountTypeOAuth, "", session.ResponseScopeUnknown, false, false, true},
		{"persisted_apikey", AccountTypeAPIKey, "producer", session.ResponseScopePersisted, true, false, false},
		{"persisted_socket_lost", AccountTypeAPIKey, "", session.ResponseScopePersisted, true, false, false},
		{"local_apikey", AccountTypeAPIKey, "producer", session.ResponseScopeConnectionLocal, true, true, false},
		{"local_oauth", AccountTypeOAuth, "producer", session.ResponseScopeConnectionLocal, true, true, false},
		{"local_socket_lost", AccountTypeAPIKey, "", session.ResponseScopeConnectionLocal, true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &OpenAIGatewayService{}
			if tc.bind {
				s.getOpenAIWSStateStore().BindResponseConn("parent", tc.conn, time.Minute, tc.scope)
			}
			account := &Account{Platform: PlatformOpenAI, Type: tc.accountType}
			conn, required, err := s.resolveOpenAIWSParentContext(account, "parent")
			if tc.denied {
				require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.conn, conn)
			if tc.required {
				require.Equal(t, "parent", required)
			} else {
				require.Empty(t, required)
			}
		})
	}
}
