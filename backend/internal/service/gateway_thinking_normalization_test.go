//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestClaudeThinkingNativeWireAndAttemptIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			original := []byte(fmt.Sprintf(`{"model":"public-alias","max_tokens":8192,"stream":%t,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":4096,"display":"omitted"},"output_config":{"effort":"xhigh","format":{"type":"text"}},"future":{"keep":true}}`, stream))
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(original), PlatformAnthropic)
			require.NoError(t, err)
			for _, tc := range []struct{ model, mode, effort string }{
				{"claude-opus-4-5", "enabled", "high"},
				{"claude-opus-4-6", "enabled", "max"},
				{"claude-fable-5-1", "adaptive", "xhigh"},
				{"claude-sonnet-5-5", "adaptive", "xhigh"},
			} {
				t.Run(fmt.Sprintf("%s/pass=%t/stream=%t", tc.model, passthrough, stream), func(t *testing.T) {
					attempt, err := parsed.CloneForBody(original)
					require.NoError(t, err)
					responseBody := `{"id":"msg_test","type":"message","role":"assistant","model":"test","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`
					contentType := "application/json"
					if stream {
						responseBody = namespaceToolAnthropicStream() + "\n"
						contentType = "text/event-stream"
					}
					upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(responseBody))}}}
					svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}}
					account := newAnthropicAPIKeyAccountForTest()
					account.Extra["anthropic_passthrough"] = passthrough
					account.Credentials["model_mapping"] = map[string]any{"public-alias": tc.model}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
					result, err := svc.Forward(context.Background(), c, account, attempt)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Len(t, upstream.requestBodies, 1)
					wire := upstream.requestBodies[0]
					require.Equal(t, tc.model, gjson.GetBytes(wire, "model").String())
					require.Equal(t, tc.mode, gjson.GetBytes(wire, "thinking.type").String())
					require.Equal(t, tc.effort, gjson.GetBytes(wire, "output_config.effort").String())
					require.Equal(t, tc.effort, attempt.OutputEffort)
					for _, path := range []string{"future", "output_config.format", "thinking.display", "max_tokens"} {
						require.Equal(t, gjson.GetBytes(original, path).Raw, gjson.GetBytes(wire, path).Raw, path)
					}
					require.Equal(t, original, parsed.Body.Bytes(), "a different account starts from original intent")
				})
			}
		}
	}
}

func TestClaudeThinkingLocalRejectionAndCeiling(t *testing.T) {
	for _, tc := range []struct {
		model, thinking, effort, ceiling, overLimit, want, history string
		status                                                     int
	}{
		{"claude-fable-5-1", "disabled", "", "", "", "", "", 400},
		{"claude-sonnet-5-5", "disabled", "", "", "", "", "high", 200},
		{"claude-opus-5", "disabled", "high", "", "", "high", "low", 200},
		{"claude-haiku-5-5", "disabled", "high", "", "", "", "low", 400},
		{"claude-opus-4-6", "adaptive", "xhigh", "xhigh", "downgrade", "high", "", 200},
		{"claude-opus-4-6", "adaptive", "xhigh", "xhigh", "deny", "", "", 403},
		{"claude-fable-5-1", "adaptive", "medium", "high", "downgrade", "medium", "", 200},
	} {
		t.Run(tc.model+tc.thinking+tc.ceiling+tc.overLimit, func(t *testing.T) {
			outputConfig, historyConfig := "", ""
			if tc.effort != "" {
				outputConfig = fmt.Sprintf(`,"output_config":{"effort":%q}`, tc.effort)
			}
			if tc.history != "" {
				historyConfig = fmt.Sprintf(`,"output_config":{"effort":%q}`, tc.history)
			}
			body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":8192,"thinking":{"type":%q}%s,"messages":[{"role":"user","content":"hi"%s}]}`, tc.model, tc.thinking, outputConfig, historyConfig))
			ctx := WithOpenAIReasoningEffortPolicy(context.Background(), tc.ceiling, []ReasoningEffortMapping{{From: "medium", To: "max"}}, tc.overLimit)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			if tc.history != "" {
				c.Request.Header.Set("anthropic-beta", claude.BetaMidConversationOutputConfig)
			}
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg_test","type":"message","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))}}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}}
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
			require.NoError(t, err)
			_, err = svc.Forward(ctx, c, newAnthropicAPIKeyAccountForTest(), parsed)
			require.Equal(t, tc.status, rec.Code)
			if tc.status != 200 {
				require.Error(t, err)
				require.Empty(t, upstream.requestBodies)
				return
			}
			require.NoError(t, err)
			require.Len(t, upstream.requestBodies, 1)
			require.Equal(t, tc.want, gjson.GetBytes(upstream.requestBodies[0], "output_config.effort").String(), "policy mappings must not run twice")
			require.Equal(t, tc.want != "", gjson.GetBytes(upstream.requestBodies[0], "output_config.effort").Exists())
			require.Equal(t, gjson.GetBytes(body, "messages").Raw, gjson.GetBytes(upstream.requestBodies[0], "messages").Raw)
		})
	}
}

func TestClaudeBedrockFinalWireEffort(t *testing.T) {
	for _, tc := range []struct{ model, want string }{
		{"claude-opus-4-5", "high"}, {"claude-opus-4-6", "max"},
		{"claude-fable-5-1", "xhigh"}, {"claude-sonnet-4-5", ""},
	} {
		t.Run(tc.model, func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg_bedrock","type":"message","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`))}}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Credentials: map[string]any{"auth_mode": "apikey", "api_key": "synthetic-key", "aws_region": "us-east-1", "model_mapping": map[string]any{"alias": "us.anthropic." + tc.model + "-v1"}}}
			body := []byte(`{"model":"alias","max_tokens":8192,"thinking":{"type":"enabled","budget_tokens":2048},"output_config":{"effort":"xhigh"},"messages":[{"role":"user","content":"hi"}]}`)
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			result, err := svc.Forward(context.Background(), c, account, parsed)
			require.NoError(t, err)
			require.Len(t, upstream.requestBodies, 1)
			wire := upstream.requestBodies[0]
			require.Equal(t, tc.want, gjson.GetBytes(wire, "output_config.effort").String())
			require.Equal(t, tc.want, parsed.OutputEffort)
			require.Equal(t, NormalizeClaudeOutputEffort(tc.want), result.ReasoningEffort)
			require.False(t, gjson.GetBytes(wire, "model").Exists())
			if tc.model == "claude-opus-4-5" {
				require.Contains(t, string(wire), "effort-2025-11-24")
			}
			if tc.model == "claude-fable-5-1" {
				require.Equal(t, "adaptive", gjson.GetBytes(wire, "thinking.type").String())
			}
		})
	}
}

func TestFableDefaultThinkingKeepsOtherValidSignatures(t *testing.T) {
	body := []byte(`{"model":"alias","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"secret","signature":"valid-signature"},{"type":"thinking","thinking":"invalid"},{"type":"text","text":"ok"}]}]}`)
	result := FilterThinkingBlocks(body, "claude-fable-5-1")
	require.Contains(t, string(result), "valid-signature")
	require.NotContains(t, string(result), "invalid")
}

func TestClaudeThinkingOAuthAndCountTokensWire(t *testing.T) {
	for _, count := range []bool{false, true} {
		t.Run(fmt.Sprintf("count=%t", count), func(t *testing.T) {
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","content":[{"type":"text","text":"ok"}],"input_tokens":3,"usage":{"input_tokens":3,"output_tokens":1}}`))}}}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}, tlsFPProfileService: &TLSFingerprintProfileService{}}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "synthetic-token"}}
			parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-fable-5-1","max_tokens":8192,"messages":[{"role":"user","content":"hi"}],"thinking":{"type":"enabled","budget_tokens":4096}}`)), PlatformAnthropic)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
			if count {
				err = svc.ForwardCountTokens(context.Background(), c, account, parsed)
			} else {
				_, err = svc.Forward(context.Background(), c, account, parsed)
			}
			require.NoError(t, err)
			require.Len(t, upstream.requestBodies, 1)
			wire := upstream.requestBodies[0]
			require.Equal(t, "adaptive", gjson.GetBytes(wire, "thinking.type").String())
			require.Equal(t, "medium", gjson.GetBytes(wire, "output_config.effort").String())
			require.False(t, gjson.GetBytes(wire, "thinking.budget_tokens").Exists())
		})
	}
}

func TestClaudeThinkingDiagnosticsAreBoundedAndDoNotIncludeContent(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core))
	body := []byte(`{"model":"claude-fable-5-1","thinking":{"type":"enabled","budget_tokens":4096},"messages":[{"content":"private-conversation","signature":"private-signature"}],"metadata":{"key":"private-key"},"tools":[{"name":"private-tool","input":"private-argument"}]}`)
	ctx = WithAppliedReasoningEffortPolicy(ctx, "high", "downgrade", body)
	result, err := normalizeClaudeThinkingForUpstream(ctx, body, "claude-fable-5-1")
	require.NoError(t, err)
	_, err = normalizeClaudeThinkingForUpstream(ctx, result, "claude-fable-5-1")
	require.NoError(t, err)
	require.Equal(t, 1, logs.Len(), "idempotent final checks must not duplicate diagnostics")
	fields := logs.All()[0].ContextMap()
	require.Equal(t, "medium", fields["forwarded_effort"])
	require.Equal(t, true, fields["budget_to_effort_approximation"])
	encoded, err := json.Marshal(fields)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-")
	require.Less(t, len(encoded), 1024)
}

func TestClaudeThinkingCeilingDistinguishesIntentFromBridgeDefaults(t *testing.T) {
	for _, tc := range []struct{ name, input, wire, want string }{
		{"omitted", `{"model":"claude-sonnet-5-5"}`, `{"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`, "high"},
		{"explicit", `{"reasoning":{"effort":"high"}}`, `{"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`, "low"},
		{"budget", `{"thinking":{"type":"enabled","budget_tokens":4096}}`, `{"thinking":{"type":"enabled","budget_tokens":4096}}`, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithAppliedReasoningEffortPolicy(context.Background(), "low", "downgrade", []byte(tc.input))
			got, err := normalizeClaudeThinkingForUpstream(ctx, []byte(tc.wire), "claude-sonnet-5-5")
			require.NoError(t, err)
			require.Equal(t, tc.want, gjson.GetBytes(got, "output_config.effort").String())
		})
	}
}
