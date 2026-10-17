package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type gatewayForwardErrorPolicyRepoStub struct {
	AccountRepository
	tempCalls           int
	overloadCalls       int
	modelRateLimitCalls []gatewayForwardModelRateLimitCall
}

type gatewayForwardModelRateLimitCall struct {
	accountID int64
	scope     string
}

func (r *gatewayForwardErrorPolicyRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.tempCalls++
	return nil
}

func (r *gatewayForwardErrorPolicyRepoStub) SetModelRateLimit(_ context.Context, id int64, scope string, _ time.Time, _ ...string) error {
	r.modelRateLimitCalls = append(r.modelRateLimitCalls, gatewayForwardModelRateLimitCall{
		accountID: id,
		scope:     scope,
	})
	return nil
}

func (r *gatewayForwardErrorPolicyRepoStub) SetOverloaded(context.Context, int64, time.Time) error {
	r.overloadCalls++
	return nil
}

// 本文件覆盖 issue #5148：流式转发中途出错（缺失 terminal 事件、读错误等）时，
// 已观测到的上游 usage 不得随错误一起被丢弃，Forward 必须把部分结果与错误一同
// 返回，供 handler 照常提交 usage 记录。

func newForwardPartialUsageServiceForTest(upstream *anthropicHTTPUpstreamRecorder) *GatewayService {
	cfg := &config.Config{
		Gateway: config.GatewayConfig{
			MaxLineSize: defaultMaxLineSize,
		},
	}
	return &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     &RateLimitService{},
		deferredService:      &DeferredService{},
	}
}

func newAnthropicOAuthAccountForPartialUsageTest() *Account {
	return &Account{
		ID:          501,
		Name:        "anthropic-oauth-partial-usage",
		Platform:    PlatformAnthropic,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "oauth-token",
		},
		Status:      StatusActive,
		Schedulable: true,
	}
}

func TestForwardExplicitPolicyRefusalNeverRetriesOrPenalizesAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, status := range []int{400, 403, 503} {
			t.Run(fmt.Sprintf("%s/%d", accountType, status), func(t *testing.T) {
				resetGatewayForwardingSettingsCacheForTest(t)
				upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
					StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","code":"content_policy_violation","message":"refused"}}`)),
				}}}
				cfg := &config.Config{Gateway: config.GatewayConfig{FailoverOn400: true}}
				svc := &GatewayService{cfg: cfg, httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
				account := newAnthropicOAuthAccountForPartialUsageTest()
				if accountType == AccountTypeAPIKey {
					account = newAnthropicAPIKeyAccountForTest()
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				hits := 0
				ctx := WithUpstreamRefusalObserver(t.Context(), func() { hits++ })
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
				parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"messages":[{"role":"user","content":"test"}]}`)), PlatformAnthropic)
				require.NoError(t, err)
				_, err = svc.Forward(ctx, c, account, parsed)
				require.Error(t, err)
				var retry *UpstreamFailoverError
				require.False(t, errors.As(err, &retry))
				require.Equal(t, 1, upstream.callCount)
				require.Equal(t, 1, hits)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Contains(t, rec.Body.String(), "content_policy_violation")
			})
		}
	}
}

func TestGatewayService_Forward_ClaudeFeedbackAcrossErrorsAndRetries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		errors     []string
		finalError bool
	}{
		{name: "signature_and_tool_retries", errors: []string{"invalid thinking signature", "invalid tool_use signature"}},
		{name: "budget_retry", errors: []string{"thinking.budget_tokens must be greater than or equal to 1024"}},
		{name: "ordinary_error", errors: []string{"invalid request"}, finalError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetGatewayForwardingSettingsCacheForTest(t)
			cache := &identityCacheStub{}
			upstream := &queuedHTTPUpstreamStub{}
			for _, message := range tc.errors {
				errorBody, err := json.Marshal(map[string]any{"error": map[string]string{"type": "invalid_request_error", "message": message}})
				require.NoError(t, err)
				upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusBadRequest,
					Header: http.Header{"X-Claude-Code-Compaction": {message}, "Set-Cookie": {"must-not-cache"}},
					Body:   io.NopCloser(strings.NewReader(string(errorBody)))})
			}
			if !tc.finalError {
				upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"application/json"}, "X-Claude-Code-Compaction": {""}},
					Body:   io.NopCloser(strings.NewReader(`{"type":"message","content":[{"type":"text","text":"answer"}],"usage":{"input_tokens":1,"output_tokens":1}}`))})
			}
			upstream.onCall = func(req *http.Request, stub *queuedHTTPUpstreamStub) {
				want := ""
				if stub.callCount > 1 {
					want = tc.errors[stub.callCount-2]
				}
				require.Equal(t, want, getHeaderRaw(req.Header, "x-claude-code-compaction"), "each attempt observes the preceding real response")
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header.Set("Anthropic-Usage-Limit", "caller-only")
			cfg := &config.Config{}
			settings, err := json.Marshal(DefaultRectifierSettings())
			require.NoError(t, err)
			svc := &GatewayService{cfg: cfg, httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{},
				identityService: NewIdentityService(cache),
				settingService:  NewSettingService(&gatewayTTLSettingRepo{data: map[string]string{SettingKeyRectifierSettings: string(settings)}}, cfg)}
			account := newAnthropicOAuthAccountForPartialUsageTest()
			parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4-6","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1024},"system":"original instructions","messages":[{"role":"user","content":"original user 中🙂"},{"role":"assistant","content":[{"type":"thinking","thinking":"x","signature":"bad"},{"type":"tool_use","id":"t1","name":"lookup","input":{}}]}]}`)), PlatformAnthropic)
			require.NoError(t, err)
			_, err = svc.Forward(t.Context(), c, account, parsed)
			if tc.finalError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, len(upstream.responses), upstream.callCount)
			captured, exists := c.Get(claudeBillingSourceTextKey)
			require.True(t, exists)
			require.Equal(t, "original user 中🙂", captured, "capture before system text is migrated into the user message")
			feedback, err := cache.GetClaudeCodeHeaders(t.Context(), account.ID)
			require.NoError(t, err)
			wantFeedback := ""
			if tc.finalError {
				wantFeedback = tc.errors[len(tc.errors)-1]
			}
			require.Equal(t, http.Header{"x-claude-code-compaction": {wantFeedback}}, feedback, "errors update feedback; real empty values clear it; caller/cookie headers never enter the cache")
		})
	}
}

func TestGatewayService_Forward_AnthropicSSEEventBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const start = "event: message_start\nid: cc_ses_start\nretry: 250\n: usage\ndata: {\"type\":\"message_start\",\ndata: \"message\":{\"model\":\"claude-sonnet-4-5\",\"usage\":{\"input_tokens\":11,\"output_tokens\":1,\"cache_read_input_tokens\":3}}}\n\n"
	const delta = "event: message_delta\ndata: {\"type\":\"message_delta\",\ndata: \"usage\":{\"output_tokens\":7}}\n\n"
	const terminal = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	const errorData = `{"type":"error",` + "\n" + `"error":{"type":"overloaded_error","message":"cc_ses_error"}}`
	errorFrame := ": diagnostic\nid: cc_ses_error\nretry: 100\nevent: error\ndata: " + strings.ReplaceAll(errorData, "\n", "\ndata: ") + "\n\n"
	for _, passthrough := range []bool{false, true} {
		mode := "ordinary"
		if passthrough {
			mode = "passthrough"
		}
		for _, tc := range []struct {
			name, tail, emitted                              string
			wantError, streamError, disconnect, withoutDelta bool
		}{
			{name: "terminal_stops_trailing_events", tail: terminal + delta + errorFrame, emitted: terminal},
			{name: "done", tail: "data: [DONE]\n\n", emitted: "data: [DONE]\n\n"},
			{name: "unterminated_stop", tail: strings.TrimSuffix(terminal, "\n"), wantError: true},
			{name: "unterminated_done", tail: "data: [DONE]", wantError: true},
			{name: "header_only_stop", tail: "event: message_stop\n\n", emitted: "event: message_stop\n\n", wantError: true},
			{name: "truncated_json", tail: "event: message_stop\ndata: {\"type\":\n\n", emitted: "event: message_stop\ndata: {\"type\":\n\n", wantError: true},
			{name: "error_frame", tail: errorFrame + terminal, emitted: errorFrame, wantError: true, streamError: true},
			{name: "data_only_error", tail: strings.ReplaceAll(errorFrame, "event: error\n", "") + terminal, emitted: strings.ReplaceAll(errorFrame, "event: error\n", ""), wantError: true, streamError: true},
			{name: "error_after_disconnect", tail: errorFrame + terminal, wantError: true, streamError: true, disconnect: true},
			{name: "start_output_usage", tail: errorFrame + terminal, emitted: errorFrame, wantError: true, streamError: true, withoutDelta: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				if tc.disconnect {
					c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
				}
				parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hello"}]}`)), PlatformAnthropic)
				require.NoError(t, err)
				prefix := start + delta + delta
				outputTokens := 7
				if tc.withoutDelta {
					prefix = start
					outputTokens = 1
				}
				upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body:   io.NopCloser(strings.NewReader(prefix + tc.tail)),
				}}
				account := newAnthropicOAuthAccountForPartialUsageTest()
				if passthrough {
					account = newAnthropicAPIKeyAccountForTest()
				}
				result, err := newForwardPartialUsageServiceForTest(upstream).Forward(context.Background(), c, account, parsed)
				if tc.wantError {
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover), "observed usage forbids regeneration")
				} else {
					require.NoError(t, err)
				}
				if tc.streamError {
					var streamErr *sseStreamErrorEventError
					require.ErrorAs(t, err, &streamErr)
					require.Equal(t, errorData, streamErr.RawData)
				}
				require.NotNil(t, result)
				require.Equal(t, 11, result.Usage.InputTokens)
				require.Equal(t, outputTokens, result.Usage.OutputTokens, "cumulative updates are not additive")
				require.Equal(t, 3, result.Usage.CacheReadInputTokens)
				require.Equal(t, tc.disconnect, result.ClientDisconnect)
				if tc.wantError && !tc.disconnect {
					require.Equal(t, tc.streamError, IsResponseCommitted(c), "only a forwarded error frame suppresses the handler's terminal-error fallback")
				}
				require.Equal(t, "claude-sonnet-4-5", result.UpstreamResponseModel)
				if tc.disconnect {
					require.Empty(t, rec.Body.String())
				} else {
					require.Equal(t, prefix+tc.emitted, rec.Body.String())
				}
			})
		}
	}
}

func TestGatewayService_Forward_StreamMissingTerminalPreservesPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	// newapi 等聚合上游的典型失败形态：message_start/message_delta 携带 usage，
	// 但流在 message_stop 前直接结束。
	upstreamSSE := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-3-5-sonnet-latest","content":[],"usage":{"input_tokens":11,"cache_read_input_tokens":7}}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":null},"usage":{"output_tokens":5}}`,
		"",
		"",
	}, "\n")
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"rid-partial"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := newForwardPartialUsageServiceForTest(upstream)
	account := newAnthropicOAuthAccountForPartialUsageTest()

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing terminal event")
	require.NotNil(t, result, "流中断但已观测到 usage 时必须返回部分结果用于计费")
	require.True(t, result.Stream)
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.CacheReadInputTokens)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Equal(t, "rid-partial", result.RequestID)
	require.NotNil(t, result.FirstTokenMs)
}

func TestGatewayService_Forward_StreamReadErrorAfterOutputPreservesPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	// message_start 已写出（含 usage），随后上游连接异常中断。
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &streamReadCloser{
			payload: []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":9,\"cache_creation_input_tokens\":4}}}\n\n"),
			err:     io.ErrUnexpectedEOF,
		},
	}}
	svc := newForwardPartialUsageServiceForTest(upstream)
	account := newAnthropicOAuthAccountForPartialUsageTest()

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "stream read error")
	require.NotNil(t, result, "已写出内容后的读错误必须保留部分 usage")
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.CacheCreationInputTokens)
}

func TestGatewayService_Forward_StreamErrorWithoutUsageReturnsNilResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	// 只有 ping、没有任何 usage 的流中断：不应产生零 usage 的幽灵账单记录。
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: ping\ndata: {\"type\": \"ping\"}\n\n")),
	}}
	svc := newForwardPartialUsageServiceForTest(upstream)
	account := newAnthropicOAuthAccountForPartialUsageTest()

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing terminal event")
	require.Nil(t, result, "无已观测 usage 时不应返回部分结果")
}

func TestGatewayService_Forward_FailoverErrorKeepsNilResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	// 未向客户端写出任何字节前的读错误会包成 UpstreamFailoverError 走换号重试。
	// 该路径必须保持 result=nil：failover 成功后按成功请求计费，双份结果会重复计费。
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: &streamReadCloser{
			err: errors.New("connection reset by peer"),
		},
	}}
	svc := newForwardPartialUsageServiceForTest(upstream)
	account := newAnthropicOAuthAccountForPartialUsageTest()

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr))
	require.Nil(t, result, "failover 错误必须保持 result=nil，防止重试成功后双重计费")
}

func TestGatewayService_Forward_PreOutputSSEOverloadedErrorUsesSemantic529(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
	require.NoError(t, err)

	const errorJSON = `{"type":"error","error":{"details":null,"type":"overloaded_error","message":"Overloaded"},"request_id":"req_01"}`
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("event: error\ndata: " + errorJSON + "\n\n")),
	}}
	repo := &gatewayForwardErrorPolicyRepoStub{}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	svc := &GatewayService{
		cfg:                  cfg,
		responseHeaderFilter: compileResponseHeaderFilter(cfg),
		httpUpstream:         upstream,
		rateLimitService:     NewRateLimitService(repo, nil, cfg, nil, nil),
		deferredService:      &DeferredService{},
	}
	account := newAnthropicOAuthAccountForPartialUsageTest()
	account.Credentials["temp_unschedulable_enabled"] = true
	account.Credentials["temp_unschedulable_rules"] = []any{map[string]any{
		"error_code":       float64(529),
		"keywords":         []any{"Overloaded"},
		"duration_minutes": float64(10),
	}}

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Nil(t, result)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, 529, failoverErr.StatusCode)
	require.JSONEq(t, errorJSON, string(failoverErr.ResponseBody))
	require.Equal(t, 1, repo.overloadCalls, "synthetic 529 must apply global overload cooldown")
	require.Empty(t, repo.modelRateLimitCalls, "global 529 cooldown must take precedence over custom model rules")
	require.Empty(t, rec.Body.String(), "pre-output overload must remain eligible for account failover")
}

func TestGatewayService_Forward_PostOutputSSEOverloadedErrorUsesSemantic529(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for name, prefix := range map[string]string{
		"message_start":      "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n",
		"ping":               "event: ping\ndata: {\"type\":\"ping\"}\n\n",
		"text_without_usage": "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial reply\"}}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

			body := []byte(`{"model":"claude-3-5-sonnet-latest","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
			require.NoError(t, err)

			const errorJSON = `{"type":"error",` + "\n" + `"error":{"type":"overloaded_error","message":"Overloaded"},"request_id":"req_error"}`
			errorFrame := ": upstream comment\nid: cc_ses_error\nretry: 250\nevent: ping\nevent: error\ndata: " + strings.ReplaceAll(errorJSON, "\n", "\n: between data\ndata: ") + "\n\n"
			fixture := prefix + errorFrame + "data: {\"type\":\"message_stop\"}\n\n"
			upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(fixture)),
			}}
			repo := &gatewayForwardErrorPolicyRepoStub{}
			cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
			svc := &GatewayService{
				cfg:                  cfg,
				responseHeaderFilter: compileResponseHeaderFilter(cfg),
				httpUpstream:         upstream,
				rateLimitService:     NewRateLimitService(repo, nil, cfg, nil, nil),
				deferredService:      &DeferredService{},
			}

			result, err := svc.Forward(context.Background(), c, newAnthropicOAuthAccountForPartialUsageTest(), parsed)
			require.Error(t, err)
			if name == "message_start" {
				require.NotNil(t, result, "paid partial streams must reach handler billing")
				require.Equal(t, 1, result.Usage.InputTokens)
			} else {
				require.Nil(t, result, "no usage must not create a zero-token bill")
			}

			var failoverErr *UpstreamFailoverError
			require.False(t, errors.As(err, &failoverErr), "committed streams cannot be regenerated")
			var streamErr *sseStreamErrorEventError
			require.ErrorAs(t, err, &streamErr)
			require.Equal(t, errorJSON, streamErr.RawData)
			require.Equal(t, prefix+errorFrame, rec.Body.String(), "preserve the original terminal error exactly once")
			require.True(t, IsResponseCommitted(c))
			require.Zero(t, repo.tempCalls)
			require.Zero(t, repo.overloadCalls, "classification must not change post-output cooldown policy")
			require.Contains(t, rec.Body.String(), prefix)
			require.Greater(t, c.Writer.Size(), 0, "post-output errors must still trigger the no-replay guard")
			events, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			upstreamErrors := events.([]*OpsUpstreamErrorEvent)
			require.Len(t, upstreamErrors, 1)
			require.Equal(t, 529, upstreamErrors[0].UpstreamStatusCode)
			require.Equal(t, "stream_error", upstreamErrors[0].Kind)
			require.Equal(t, "Overloaded", upstreamErrors[0].Message)
		})
	}
}

func TestGatewayService_Forward_SSEErrorAfterClientDisconnectPreservesUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
	parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(
		`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hello"}]}`)), PlatformAnthropic)
	require.NoError(t, err)
	const errorData = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":1}}}\n\n" +
			"event: error\ndata: " + errorData + "\n\n")),
	}}
	result, err := newForwardPartialUsageServiceForTest(upstream).Forward(context.Background(), c, newAnthropicOAuthAccountForPartialUsageTest(), parsed)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	var streamErr *sseStreamErrorEventError
	require.ErrorAs(t, err, &streamErr)
	require.Equal(t, errorData, streamErr.RawData)
	require.NotNil(t, result)
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 1, result.Usage.OutputTokens)
	require.True(t, result.ClientDisconnect)
	require.False(t, c.Writer.Written(), "usage alone must forbid failover even without a committed write")
	require.Empty(t, rec.Body.String())
}

func TestGatewayService_AnthropicAPIKeyPassthrough_ForwardStreamMissingTerminalPreservesPartialUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	body := []byte(`{"model":"claude-3-7-sonnet-20250219","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	parsed := &ParsedRequest{
		Body:   NewRequestBodyRef(body),
		Model:  "claude-3-7-sonnet-20250219",
		Stream: true,
	}

	upstreamSSE := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":2}}}`,
		"",
		`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
		"",
		"",
	}, "\n")
	upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"rid-pass-partial"},
		},
		Body: io.NopCloser(strings.NewReader(upstreamSSE)),
	}}
	svc := newForwardPartialUsageServiceForTest(upstream)
	account := newAnthropicAPIKeyAccountForTest()

	result, err := svc.Forward(context.Background(), c, account, parsed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing terminal event")
	require.NotNil(t, result, "透传流中断但已观测到 usage 时必须返回部分结果用于计费")
	require.True(t, result.Stream)
	require.Equal(t, 9, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.CacheReadInputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)
	require.Equal(t, "claude-3-7-sonnet-20250219", result.Model)
}
