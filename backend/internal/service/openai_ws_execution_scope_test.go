package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newOpenAIWSExecutionScopeTestContext(headers map[string]string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	for k, v := range headers {
		c.Request.Header.Set(k, v)
	}
	return c
}

// Pure precedence/lane cases live in gateway/session. These cases exercise the
// real Gin extraction, shared explicit-session resolver and existing hash.
func TestResolveOpenAIWSExecutionScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name       string
		headers    map[string]string
		body       string
		wantSeed   string
		wantThread string
	}{
		{
			name:       "thread header",
			headers:    map[string]string{openAIWSThreadIDHeader: " thread-a ", "session-id": "root-session"},
			wantSeed:   "openai_ws_exec:11|thread=thread-a",
			wantThread: "thread-a",
		},
		{
			name:       "metadata header and background lane",
			headers:    map[string]string{openAIWSTurnMetadataHeader: `{"thread_id":"thread-a","request_kind":"memory"}`},
			wantSeed:   "openai_ws_exec:11|thread=thread-a|kind=memory",
			wantThread: "thread-a",
		},
		{
			name:       "window and guardian headers",
			headers:    map[string]string{openAIWSWindowIDHeader: "thread-a:0", openAISubagentHeader: "Guardian"},
			wantSeed:   "openai_ws_exec:11|thread=thread-a|subagent=guardian",
			wantThread: "thread-a",
		},
		{
			name:     "session header precedes explicit cache key",
			headers:  map[string]string{"session-id": " root-session ", "session_id": "other"},
			body:     `{"prompt_cache_key":"cache-1"}`,
			wantSeed: "openai_ws_exec:11|session=root-session",
		},
		{
			name:     "existing session alias",
			headers:  map[string]string{"X-Session-Affinity": "opencode-session"},
			wantSeed: "openai_ws_exec:11|session=opencode-session",
		},
		{name: "explicit prompt cache key", body: `{"prompt_cache_key":" cache-1 ","input":"hi"}`, wantSeed: "openai_ws_exec:11|session=cache-1"},
		{name: "WS envelope explicit cache key", body: `{"type":"response.create","response":{"prompt_cache_key":"cache-ws"}}`, wantSeed: "openai_ws_exec:11|session=cache-ws"},
		{name: "HTTP response field is not an envelope", body: `{"response":{"prompt_cache_key":"nested"}}`},
		{name: "request content cannot create scope", body: `{"model":"gpt-5.1","input":"hi"}`},
		{name: "untrusted Grok identity cannot create scope", headers: map[string]string{"x-grok-conv-id": "other"}, body: `{"previous_response_id":"resp_other"}`},
		{name: "wire stream cannot create scope", body: `{"stream_id":"forged","connection_id":"forged","client_metadata":{"stream_id":"forged"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newOpenAIWSExecutionScopeTestContext(tc.headers)
			wire := []byte(tc.body)
			scope, thread := resolveOpenAIWSExecutionScope(c, wire, 11)
			want, _ := deriveOpenAISessionHashes(tc.wantSeed)
			require.Equal(t, want, scope, "must keep the existing key/hash format")
			require.Equal(t, tc.wantThread, thread)
			require.Equal(t, tc.body, string(wire))
		})
	}
}

func TestResolveOpenAIWSExecutionScopeContextBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		identity any
		wantSeed string
		wantLane string
	}{
		{name: "named stream", identity: openAIWSStreamIdentity{connection: "conn", stream: "lane"}, wantSeed: "openai_ws_exec:11|connection=conn|connection=conn|stream=lane|kind=memory", wantLane: "connection=conn|stream=lane|kind=memory"},
		{name: "default stream", identity: openAIWSStreamIdentity{connection: "conn"}, wantLane: "kind=memory"},
		{name: "wrong context value type", identity: "connection=forged|stream=forged", wantLane: "kind=memory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newOpenAIWSExecutionScopeTestContext(nil)
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), openAIWSStreamContextKey{}, tc.identity))
			body := []byte(`{"stream_id":"forged","client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`)
			require.Equal(t, tc.wantLane, resolveOpenAIWSExecutionLane(c, body))
			scope, thread := resolveOpenAIWSExecutionScope(c, body, 11)
			want, _ := deriveOpenAISessionHashes(tc.wantSeed)
			require.Equal(t, want, scope)
			require.Empty(t, thread)
		})
	}
}

func TestResolveOpenAIWSExecutionScopeWithoutRequest(t *testing.T) {
	// The real shared helper rejects nil context before inspecting the body.
	// Keep this distinct from a real request with no session headers.
	for _, body := range []string{
		`{"prompt_cache_key":"cache-1"}`,
		`{"type":"response.create","response":{"prompt_cache_key":"cache-ws"}}`,
	} {
		require.Empty(t, explicitOpenAIRequestSessionID(nil, []byte(body)))
		scope, thread := resolveOpenAIWSExecutionScope(nil, []byte(body), 11)
		require.Empty(t, scope)
		require.Empty(t, thread)
	}
	// Thread identity precedes the helper, whose header access needs Request
	// when the Gin context is non-nil. Do not eagerly invoke it for this path.
	for _, c := range []*gin.Context{nil, {}} {
		scope, thread := resolveOpenAIWSExecutionScope(c, []byte(`{"prompt_cache_key":"cache-1","client_metadata":{"thread_id":"t"}}`), 11)
		want, _ := deriveOpenAISessionHashes("openai_ws_exec:11|thread=t")
		require.Equal(t, want, scope)
		require.Equal(t, "t", thread)
	}
}
