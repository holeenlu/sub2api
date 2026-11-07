package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCacheContract_AnthropicAffinityAcrossTenTurns(t *testing.T) {
	svc := &GatewayService{}
	var messages []any
	var firstHash string
	for turn := 0; turn < 10; turn++ {
		// The cache marker moves to each new user message; it is not identity.
		for _, m := range messages {
			delete(m.(map[string]any)["content"].([]any)[0].(map[string]any), "cache_control")
		}
		messages = append(messages, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": fmt.Sprintf("turn %d", turn), "cache_control": map[string]any{"type": "ephemeral"}}}})
		body, err := json.Marshal(map[string]any{"model": "claude-sonnet-4", "system": "stable", "messages": messages})
		require.NoError(t, err)
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		require.NoError(t, err)
		parsed.SessionContext = &SessionContext{APIKeyID: 7, ClientIP: "127.0.0.1", UserAgent: "test/1"}
		hash := svc.GenerateSessionHash(parsed)
		if turn == 0 {
			firstHash = hash
		}
		require.Equal(t, firstHash, hash)
		parsed.SessionContext.APIKeyID = 8
		require.NotEqual(t, firstHash, svc.GenerateSessionHash(parsed))
	}
}

func TestCacheContract_ExplicitSessionSurvivesCompaction(t *testing.T) {
	svc := &GatewayService{}
	var first string
	for _, text := range []string{"original task", "compacted context"} {
		body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": text}}})
		parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
		require.NoError(t, err)
		parsed.SessionContext = &SessionContext{APIKeyID: 7, ExplicitSessionID: "conversation-a"}
		hash := svc.GenerateSessionHash(parsed)
		if first == "" {
			first = hash
		}
		require.Equal(t, first, hash)
		parsed.SessionContext.ExplicitSessionID = "conversation-b"
		require.NotEqual(t, first, svc.GenerateSessionHash(parsed))
	}
}

func TestCacheContract_ToolAliasesAreValidAndReversible(t *testing.T) {
	for _, names := range [][]string{{"a中文工具"}, {"🔍search", "中文工具", "mcp__中文工具", "read", "write", "test"}} {
		tools := make([]any, 0, len(names))
		for _, name := range names {
			tools = append(tools, map[string]any{"name": name, "input_schema": map[string]any{"type": "object"}})
		}
		body, _ := json.Marshal(map[string]any{"tools": tools})
		rw := buildToolNameRewriteFromBody(body)
		require.NotNil(t, rw)
		out := applyToolNameRewriteToBody(body, rw)
		for i, name := range names {
			fake := gjson.GetBytes(out, fmt.Sprintf("tools.%d.name", i)).String()
			require.True(t, utf8.ValidString(fake))
			require.True(t, validAnthropicToolName(fake))
			payload, _ := json.Marshal(map[string]any{"type": "tool_use", "name": fake, "input": map[string]any{"path": fake + ".txt"}})
			restored := restoreToolNamesInBytes(payload, rw)
			require.Equal(t, name, gjson.GetBytes(restored, "name").String())
			require.Equal(t, fake+".txt", gjson.GetBytes(restored, "input.path").String())
		}
	}
}

func TestCacheContract_SSEOnlyRestoresToolNameFields(t *testing.T) {
	rw := &ToolNameRewrite{Reverse: map[string]string{"fake": "real"}}
	frame := "id: fake\r\nevent: content_block_start\r\ndata: {\"type\":\"content_block_start\",\r\ndata: \"content_block\":{\"type\":\"tool_use\",\"name\":\"fake\",\"input\":{\"file\":\"fake.txt\"}}}\r\n\r\n"
	out := string(restoreToolNamesInBytes([]byte(frame), rw))
	require.Contains(t, out, "id: fake")
	require.Contains(t, out, "\"name\":\"real\"")
	require.Contains(t, out, "\"file\":\"fake.txt\"")
	text := []byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"fake\"}}\n\n")
	require.Equal(t, text, restoreToolNamesInBytes(text, rw))
	partial := []byte("data: {\"type\":\"tool_use\",\"name\":\"fake\"}")
	require.Equal(t, partial, restoreToolNamesInBytes(partial, rw))
}

func TestCacheContract_ResponsesRetainsClientCachePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ua := range []string{"custom-sdk/1", "codex_cli_rs/0.159.3"} {
		for _, tc := range []struct {
			model, field string
			value        any
		}{{"gpt-5.4", "prompt_cache_retention", "24h"}, {"gpt-5.6", "prompt_cache_options", map[string]any{"mode": "explicit", "ttl": "30m"}}} {
			t.Run(ua+"/"+tc.field, func(t *testing.T) {
				body, _ := json.Marshal(map[string]any{"model": tc.model, "instructions": "unchanged", "input": []any{map[string]any{"role": "user", "content": "hello"}}, tc.field: tc.value, "safety_identifier": "user-hash"})
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("User-Agent", ua)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_policy","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test", "base_url": "https://api.openai.com"}}
				_, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.Equal(t, gjson.GetBytes(body, tc.field).Raw, gjson.GetBytes(upstream.lastBody, tc.field).Raw)
				require.Equal(t, "user-hash", gjson.GetBytes(upstream.lastBody, "safety_identifier").String())
			})
		}
	}
}

func TestCacheContract_DiagnosticIdentitySurvivesWorkerAndScopesTenant(t *testing.T) {
	ctx := WithGatewayCacheDiagnostics(context.Background(), 7, "anthropic", "session", "route-1", "claude-cli/1")
	worker := WithSchedulingMetricsContext(context.Background(), ctx)
	d := worker.Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic)
	require.NotEmpty(t, d.Session)
	require.NotEqual(t, "session", d.Session)
	require.NotEqual(t, d.Session, gatewayCacheIdentity(8, "session"))
	other := WithGatewayCacheDiagnostics(context.Background(), 7, "anthropic", "session", "route-2", "claude-cli/1").Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic)
	require.Equal(t, d.Session, other.Session)
	require.NotEqual(t, d.Routing, other.Routing)
	require.Empty(t, gatewayCacheIdentity(7, ""))
}

func TestCacheContract_RawTTLSurvivesResponsePaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const rawUsage = `{"input_tokens":3,"output_tokens":2,"cache_creation_input_tokens":20,"cache_read_input_tokens":11,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":20}}`
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			svc := &GatewayService{cfg: &config.Config{}, rateLimitService: &RateLimitService{}}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Extra: map[string]any{"cache_ttl_override_enabled": true, "cache_ttl_override_target": "5m"}}
			raw := `{"type":"message","model":"claude-sonnet-4","content":[],"usage":` + rawUsage + "}"
			if stream {
				raw = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + raw + "}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
			}
			resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
			var usage *ClaudeUsage
			if stream {
				result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "claude-sonnet-4", "claude-sonnet-4", false)
				require.NoError(t, err)
				usage = result.usage
			} else {
				var err error
				usage, err = svc.handleNonStreamingResponse(context.Background(), resp, c, account, "claude-sonnet-4", "claude-sonnet-4")
				require.NoError(t, err)
			}
			require.Equal(t, 20, usage.CacheCreation1hTokens)
			require.Zero(t, usage.CacheCreation5mTokens)
			require.Contains(t, rec.Body.String(), "\"ephemeral_1h_input_tokens\":20")
		})
	}
}

func TestCacheContract_PrefixFingerprintDeterministic(t *testing.T) {
	a := []byte(`{"tools":[{"name":"read","input_schema":{"type":"object","properties":{}}}],"system":"fixed","model":"claude-sonnet-4"}`)
	b := []byte(`{"model":"claude-sonnet-4","system":"fixed","tools":[{"input_schema":{"properties":{},"type":"object"},"name":"read"}],"messages":[{"role":"user","content":"new turn"}]}`)
	require.Equal(t, gatewayCachePrefixFingerprint(a), gatewayCachePrefixFingerprint(b))
	require.NotEqual(t, gatewayCachePrefixFingerprint(a), gatewayCachePrefixFingerprint([]byte(strings.Replace(string(b), "fixed", "changed", 1))))
}
