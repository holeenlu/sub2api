package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestForwardOpenAIWSV2_HTTPPartialFailurePreservesUsageAndDoesNotReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const metered = `{"type":"response.in_progress","response":{"id":"resp_partial","model":"gpt-5.5","usage":{"input_tokens":11,"output_tokens":2,"input_tokens_details":{"cached_tokens":3}}}}`
	const delta = `{"type":"response.output_text.delta","response_id":"resp_partial","delta":"partial reply"}`
	for _, stream := range []bool{false, true} {
		for _, tc := range []struct {
			name                            string
			events                          []string
			metered, disconnect, hostedTool bool
		}{
			{name: "nonterminal_usage_then_read_error", events: []string{metered}, metered: true},
			{name: "usage_then_malformed_json", events: []string{metered, `{`}, metered: true},
			{name: "usage_then_error_event", events: []string{metered, `{"type":"error","error":{"type":"server_error","code":"server_error","message":"upstream failed"}}`}, metered: true},
			{name: "usage_then_short_server_error", events: []string{metered, `{"type":"error","error":{"type":"server_err","code":"server_err","message":"upstream failed"}}`}, metered: true},
			{name: "output_without_usage", events: []string{delta}},
			{name: "tool_output_without_usage", events: []string{`{"type":"response.output_item.done","item":{"type":"function_call","id":"call_1","name":"write_file","arguments":"{}"}}`}},
			{name: "hosted_tool_uncertain_execution", hostedTool: true},
			{name: "usage_after_downstream_disconnect", events: []string{delta, metered}, metered: true, disconnect: true},
		} {
			if tc.disconnect && !stream {
				continue
			}
			t.Run(fmt.Sprintf("stream_%t/%s", stream, tc.name), func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				if tc.disconnect {
					c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
				}
				cfg := newOpenAIWSV2TestConfig()
				cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
				capture := &openAIWSCaptureConn{}
				for _, event := range tc.events {
					capture.events = append(capture.events, []byte(event))
				}
				dialer := &openAIWSCaptureDialer{conn: capture}
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(dialer)
				t.Cleanup(func() { pool.Close() })
				upstream := &httpUpstreamSequenceRecorder{}
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), openaiWSPool: pool, toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 5886, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1,
					Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "owner"},
					Extra:       map[string]any{"responses_websockets_v2_enabled": true}}
				budget := NewOpenAITurnBudget(cfg)
				ctx := WithOpenAITurnBudget(context.Background(), budget)
				tools := ""
				if tc.hostedTool {
					tools = `,"tools":[{"type":"mcp","server_label":"write_service","server_url":"https://tools.example.test"}]`
				}
				result, err := svc.Forward(ctx, c, account, []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":"hello"%s}`, stream, tools)))
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "observed execution or hosted tools cannot be replayed on another account")
				require.Equal(t, 1, dialer.DialCount())
				capture.mu.Lock()
				writes := len(capture.writes)
				capture.mu.Unlock()
				require.Equal(t, 1, writes, "one actual response.create send")
				require.Zero(t, upstream.callCount, "no HTTP replay")
				budget.mu.Lock()
				attempts := budget.attempts
				budget.mu.Unlock()
				require.Equal(t, 1, attempts, "preserve actual-send budget accounting")
				if tc.metered {
					require.NotNil(t, result, "Forward must retain the inner partial result")
					require.Equal(t, "resp_partial", result.RequestID)
					require.Equal(t, 11, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.Equal(t, 3, result.Usage.CacheReadInputTokens)
					require.Equal(t, tc.disconnect, result.ClientDisconnect)
					require.Equal(t, "gpt-5.5", result.BillingModel)
					require.True(t, result.OpenAIWSMode)
				} else {
					require.Nil(t, result, "do not invent usage for output-only or uncertain attempts")
				}
			})
		}
	}
}

func TestForwardOpenAIWSV2_ExplicitHTTPIngressOAuthRouting(t *testing.T) {
	for _, tc := range []struct {
		name, path                             string
		forceHTTP, passthrough, stream, wantWS bool
	}{
		{name: "oauth_stream", path: "/v1/responses", stream: true, wantWS: true},
		{name: "oauth_json", path: "/v1/responses", wantWS: true},
		{name: "force_http", path: "/v1/responses", forceHTTP: true},
		{name: "compact_http", path: "/v1/responses/compact"},
		{name: "passthrough_http", path: "/v1/responses", passthrough: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			cfg := newOpenAIWSV2TestConfig()
			cfg.Gateway.OpenAIWS.ForceHTTP = tc.forceHTTP
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			SetOpenAIHTTPResponseOwner(c, 88, 77)
			c.Set("api_key", &APIKey{ID: 77, UserID: 88})
			capture := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_http_ws","status":"completed","output":[],"usage":{"input_tokens":4,"output_tokens":2}}}`)}}
			pool := newOpenAIWSConnPool(cfg)
			dialer := &openAIWSCaptureDialer{conn: capture}
			pool.setClientDialerForTest(dialer)
			t.Cleanup(pool.Close)
			upstream := &httpUpstreamSequenceRecorder{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_http","output":[],"usage":{"input_tokens":4,"output_tokens":2}}`))}}}
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), openaiWSPool: pool, toolCorrector: NewCodexToolCorrector()}
			account := &Account{ID: 5885, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"}, Extra: map[string]any{"responses_websockets_v2_enabled": true, "openai_passthrough": tc.passthrough}}
			body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":[{"role":"user","content":"hello"}]}`, tc.stream))
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.wantWS, result.OpenAIWSMode)
			if tc.wantWS {
				require.Equal(t, 0, upstream.callCount)
				require.Equal(t, "response.create", capture.lastWrite["type"])
				require.Equal(t, 4, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				owned, ownerErr := svc.ValidateOpenAIHTTPResponseOwner(context.Background(), 0, result.ResponseID, 88, 77)
				require.NoError(t, ownerErr)
				require.True(t, owned, "HTTP continuation must recognize the WS-produced response")
				owned, ownerErr = svc.ValidateOpenAIHTTPResponseOwner(context.Background(), 0, result.ResponseID, 99, 78)
				require.NoError(t, ownerErr)
				require.False(t, owned, "another tenant must not continue this response")
				capture.mu.Lock()
				capture.events = append(capture.events, []byte(`{"type":"response.completed","response":{"id":"resp_http_ws_next","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":2}}}`))
				capture.mu.Unlock()
				next, _ := gin.CreateTestContext(httptest.NewRecorder())
				next.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				next.Set("api_key", &APIKey{ID: 77, UserID: 88})
				SetOpenAIClientTransport(next, OpenAIClientTransportHTTP)
				SetOpenAIHTTPResponseOwner(next, 88, 77)
				nextResult, nextErr := svc.Forward(context.Background(), next, account, []byte(`{"model":"gpt-5.5","stream":false,"previous_response_id":"resp_http_ws","input":[{"role":"user","content":"next turn"}]}`))
				require.NoError(t, nextErr)
				require.NotNil(t, nextResult)
				require.Equal(t, "resp_http_ws_next", nextResult.ResponseID)
				require.Equal(t, 1, dialer.DialCount(), "headerless HTTP continuation must keep its connection-local state")
				require.Equal(t, "resp_http_ws", capture.lastWrite["previous_response_id"])
			} else {
				require.Equal(t, 1, upstream.callCount)
			}
		})
	}
}

// HTTP POST /v1/responses -> forwardOpenAIWSV2 keeps the canonical outbound
// tier separate from response.completed.service_tier for usage-time billing.
func TestForwardOpenAIWSV2_KeepsOutboundAndObservedServiceTiersSeparate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name        string
		requestTier string
		stream      bool
	}{
		{name: "priority_nonstream", requestTier: "priority", stream: false},
		{name: "fast_stream", requestTier: "fast", stream: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("User-Agent", "unit-test-agent/1.0")

			cfg := &config.Config{}
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
			cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

			captureConn := &openAIWSCaptureConn{
				events: [][]byte{
					[]byte(`{"type":"response.completed","response":{"id":"resp_tier_v2","model":"gpt-5.5","status":"completed","service_tier":"default","usage":{"input_tokens":1,"output_tokens":1}}}`),
				},
			}
			captureDialer := &openAIWSCaptureDialer{conn: captureConn}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(captureDialer)

			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}
			account := &Account{
				ID:          5882,
				Name:        "openai-ws-v2-tier",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true},
			}

			body := []byte(fmt.Sprintf(
				`{"model":"gpt-5.5","stream":%t,"service_tier":%q,"input":[{"type":"input_text","text":"hi"}]}`,
				tc.stream, tc.requestTier,
			))
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, UsageAuthorizationModeNative, result.AuthorizationMode)
			require.True(t, result.OpenAIWSMode, "must take HTTP POST → forwardOpenAIWSV2, not HTTP fallback")
			require.Equal(t, tc.stream, result.Stream)
			require.Equal(t, "resp_tier_v2", result.RequestID)
			require.NotNil(t, result.ServiceTier)
			require.Equal(t, "priority", *result.ServiceTier)
			require.Equal(t, "default", result.UpstreamResponseServiceTier)
			require.Equal(t, "priority", captureConn.lastWrite["service_tier"],
				"outbound WS payload still carries the requested Fast tier")
		})
	}
}

func TestForwardOpenAIWSV2_MarksCyberPolicyForFailureEventShapes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		upstreamEvent []byte
		wantError     bool
		wantInput     int
		wantOutput    int
	}{
		{
			name:          "error_before_return",
			upstreamEvent: []byte(`{"type":"error","error":{"type":"rate_limit_error","code":"cyber_policy","message":"rate limit exceeded by cyber policy"},"usage":{"input_tokens":5,"output_tokens":1}}`),
			wantError:     true,
			wantInput:     5,
			wantOutput:    1,
		},
		{
			name:          "response_failed_terminal",
			upstreamEvent: []byte(`{"type":"response.failed","response":{"id":"resp_cyber","status":"failed","error":{"code":"cyber_policy","message":"blocked by cyber policy"},"usage":{"input_tokens":9,"output_tokens":2}}}`),
			wantInput:     9,
			wantOutput:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)

			cfg := newOpenAIWSV2TestConfig()
			cfg.Security.URLAllowlist.Enabled = false
			cfg.Security.URLAllowlist.AllowInsecureHTTP = true
			cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			captureConn := &openAIWSCaptureConn{events: [][]byte{append([]byte(nil), tt.upstreamEvent...)}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: captureConn})
			svc := &OpenAIGatewayService{
				cfg:              cfg,
				httpUpstream:     &httpUpstreamRecorder{},
				cache:            &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
				toolCorrector:    NewCodexToolCorrector(),
				openaiWSPool:     pool,
			}
			account := &Account{
				ID: 5883, Name: "openai-ws-v2-cyber", Platform: PlatformOpenAI,
				Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{"responses_websockets_v2_enabled": true},
			}

			result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`))
			if tt.wantError {
				require.Error(t, err)
				require.NotNil(t, result, "a metered error must retain partial usage")
				require.Equal(t, tt.wantInput, result.Usage.InputTokens)
				require.Equal(t, tt.wantOutput, result.Usage.OutputTokens)
				var failoverErr *UpstreamFailoverError
				require.False(t, errors.As(err, &failoverErr))
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, UsageAuthorizationModeNative, result.AuthorizationMode)
			}

			mark := GetOpsCyberPolicy(c)
			require.NotNil(t, mark)
			require.Equal(t, "cyber_policy", mark.Code)
			require.Equal(t, tt.wantInput, mark.UpstreamInTok)
			require.Equal(t, tt.wantOutput, mark.UpstreamOutTok)
		})
	}
}
