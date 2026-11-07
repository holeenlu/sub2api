//go:build unit

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeOAuthSessionAcrossIngress(t *testing.T) {
	resetGatewayForwardingSettingsCacheForTest(t)
	cache := &stubIdentityCache{}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	account.Extra = map[string]any{"account_uuid": "upstream-account", "session_id_masking_enabled": false}
	forward := func(route, system, question, metadata string, keyID int64) *ParsedUserID {
		t.Helper()
		request := map[string]any{"model": "claude-sonnet-4-6", "stream": false}
		switch route {
		case "responses":
			request["instructions"], request["input"] = system, question
		case "chat":
			request["messages"] = []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": question}}
		default:
			request["system"] = system
			request["messages"] = []map[string]string{{"role": "user", "content": question}}
			if metadata != "" {
				request["metadata"] = map[string]string{"user_id": metadata}
			}
		}
		body, err := json.Marshal(request)
		require.NoError(t, err)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+route, strings.NewReader(string(body)))
		c.Request.RemoteAddr = "192.0.2.1:1234"
		c.Request.Header.Set("User-Agent", "session-client/1.0")
		c.Set("api_key", &APIKey{ID: keyID})
		if metadata != "" {
			c.Request.Header.Set("X-Claude-Code-Session-Id", ParseMetadataUserID(metadata).SessionID)
		}
		// Match the authenticated handler projection on Messages/count_tokens.
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		require.NoError(t, err)
		parsed.SessionContext = &SessionContext{ClientIP: ip.GetClientIP(c), UserAgent: c.GetHeader("User-Agent"), APIKeyID: keyID}
		response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"type":"message","content":[],"usage":{"input_tokens":1,"output_tokens":1}}`))}
		if route == "count_tokens" {
			response.Body = io.NopCloser(strings.NewReader(`{"input_tokens":42}`))
		} else if route == "chat" || route == "responses" {
			response.Header.Set("Content-Type", "text/event-stream")
			response.Body = io.NopCloser(strings.NewReader(miniAnthropicSSEStream()))
		}
		upstream := &anthropicHTTPUpstreamRecorder{resp: response}
		svc := newForwardPartialUsageServiceForTest(upstream)
		svc.identityService = NewIdentityService(cache)
		svc.settingService = NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{}}, &config.Config{})
		switch route {
		case "messages":
			_, err = svc.Forward(t.Context(), c, account, parsed)
		case "count_tokens":
			err = svc.ForwardCountTokens(t.Context(), c, account, parsed)
		case "chat":
			_, err = svc.ForwardAsChatCompletions(t.Context(), c, account, body, parsed)
		case "responses":
			_, err = svc.ForwardAsResponses(t.Context(), c, account, body, parsed)
		}
		require.NoError(t, err)
		uid := ParseMetadataUserID(gjson.GetBytes(upstream.lastBody, "metadata.user_id").String())
		require.NotNil(t, uid)
		require.Equal(t, uid.SessionID, getHeaderRaw(upstream.lastReq.Header, "X-Claude-Code-Session-Id"))
		require.Contains(t, string(upstream.lastBody), "[System Instructions]", "exercise actual system migration")
		return uid
	}
	baseline := forward("messages", "system A", "original question", "", 41)
	for _, route := range []string{"messages", "count_tokens", "chat", "responses"} {
		t.Run(route, func(t *testing.T) {
			got := forward(route, "system A", "original question", "", 41)
			require.Equal(t, baseline.SessionID, got.SessionID, "same authenticated caller and logical input must agree across ingress")
			require.NotEqual(t, got.SessionID, forward(route, "system A", "original question", "", 42).SessionID, "actual API key IDs isolate callers on the same upstream account")
			require.NotEqual(t, got.SessionID, forward(route, "system A", "another question", "", 41).SessionID, "a common system must not collapse distinct original user text")
			require.Equal(t, got.SessionID, forward(route, "system B", "original question", "", 41).SessionID, "system migration cannot become the session anchor")
		})
	}
	// Anthropic's explicit metadata contract remains owned by identity mapping.
	// Chat/Responses don't expose this provider-specific metadata field.
	for _, route := range []string{"messages", "count_tokens"} {
		t.Run(route+"/explicit", func(t *testing.T) {
			parent := `{"device_id":"client-device","account_uuid":"client-account","session_id":"explicit-parent","tk":{"n":9007199254740993}}`
			child := `{"device_id":"client-device","account_uuid":"client-account","session_id":"explicit-child","parent_session_id":"explicit-parent","tk":{"n":9007199254740993}}`
			p := forward(route, "system A", "original question", parent, 41)
			first := forward(route, "system A", "original question", child, 41)
			other := forward(route, "system B", "different question", child, 41)
			require.Equal(t, first.SessionID, other.SessionID)
			require.NotEqual(t, baseline.SessionID, first.SessionID)
			require.Equal(t, p.SessionID, first.metadataUserIDParentSession())
			require.Equal(t, "9007199254740993", gjson.Get(string(first.ExtraFields["tk"]), "n").Raw)
		})
	}
}
