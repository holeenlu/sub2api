//go:build unit

package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 国产供应商原生 Anthropic 直通路径（api_protocol=anthropic）的 reasoning_effort 记录。
// 回归背景：该路径此前从不提取 Claude 协议的 output_config.effort，也不做
// thinking-enabled 兜底，导致 kimi/zhipu/deepseek 平台分组的 /v1/messages 请求
// usage_log.reasoning_effort 恒为 NULL。

func nativeAnthropicTestAccount() *Account {
	return &Account{
		ID:          702,
		Name:        "kimi-native",
		Platform:    PlatformKimi,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":      "sk-test",
			"api_protocol": APIProtocolAnthropic,
			"api_base_urls": map[string]any{
				APIProtocolAnthropic: "http://anthropic.example",
			},
		},
	}
}

func nativeAnthropicGLMTestAccount() *Account {
	account := nativeAnthropicTestAccount()
	account.Name = "zhipu-native"
	account.Platform = PlatformZhipu
	return account
}

func nativeAnthropicBufferedResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"msg_1","type":"message","role":"assistant","model":"k3",` +
				`"content":[{"type":"text","text":"pong"}],"stop_reason":"end_turn",` +
				`"usage":{"input_tokens":93,"output_tokens":16}}`,
		)),
	}
}

func nativeAnthropicStreamResponse() *http.Response {
	sse := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"k3","content":[],"stop_reason":null,"usage":{"input_tokens":93,"output_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":16}}

event: message_stop
data: {"type":"message_stop"}

`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}
}

func TestNativeAnthropicPassthroughRecordsOutputConfigEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"k3","max_tokens":32,"stream":false,` +
		`"output_config":{"effort":"low"},` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardAsAnthropic(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestAccount(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "low", *result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughThinkingEnabledFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 未显式传 effort，但 thinking 已启用：k3 属于 passback-required 白名单，应兜底记为 high。
	body := []byte(`{"model":"k3","max_tokens":32,"stream":false,` +
		`"thinking":{"type":"enabled","budget_tokens":1024},` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardAsAnthropic(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestAccount(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "high", *result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughStreamRecordsEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"k3","max_tokens":32,"stream":true,` +
		`"output_config":{"effort":"max"},` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardAsAnthropic(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestAccount(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ReasoningEffort)
	require.Equal(t, "max", *result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughNoEffortStaysNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 既无 output_config.effort 也未启用 thinking：保持 nil，不做语义注入。
	body := []byte(`{"model":"k3","max_tokens":32,"stream":false,` +
		`"messages":[{"role":"user","content":"hi"}]}`)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	result, err := svc.ForwardAsAnthropic(context.Background(),
		adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicTestAccount(), body, "", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.ReasoningEffort)
}

func TestNativeAnthropicPassthroughNormalizesGLM53Thinking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		stream     bool
		preference string
		wantEffort string
	}{
		{name: "disabled buffered", preference: `"thinking":{"type":"disabled"},`, wantEffort: "low"},
		{name: "off buffered", preference: `"thinking":{"type":"off"},`, wantEffort: "low"},
		{name: "none buffered", preference: `"thinking":{"type":"none"},`, wantEffort: "low"},
		{name: "enabled buffered", preference: `"thinking":{"type":"enabled"},`, wantEffort: "high"},
		{name: "enabled streaming", stream: true, preference: `"thinking":{"type":"enabled"},`, wantEffort: "high"},
		{name: "adaptive buffered", preference: `"thinking":{"type":"adaptive"},`, wantEffort: "high"},
		{name: "adaptive streaming", stream: true, preference: `"thinking":{"type":"adaptive"},`, wantEffort: "high"},
		{name: "minimal buffered", preference: `"output_config":{"effort":"minimal"},`, wantEffort: "low"},
		{name: "low buffered", preference: `"output_config":{"effort":"low"},`, wantEffort: "low"},
		{name: "medium streaming", stream: true, preference: `"output_config":{"effort":"medium"},`, wantEffort: "high"},
		{name: "high buffered", preference: `"output_config":{"effort":"high"},`, wantEffort: "high"},
		{name: "xhigh buffered", preference: `"output_config":{"effort":"xhigh"},`, wantEffort: "max"},
		{name: "max buffered", preference: `"output_config":{"effort":"max"},`, wantEffort: "max"},
		{name: "ultra buffered", preference: `"output_config":{"effort":"ultra"},`, wantEffort: "max"},
		{name: "output effort wins over thinking", preference: `"thinking":{"type":"adaptive"},"output_config":{"effort":"low"},`, wantEffort: "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"glm-5.3","max_tokens":32,"stream":%t,%s"messages":[{"role":"user","content":"hi"}]}`, tt.stream, tt.preference))
			response := nativeAnthropicBufferedResponse()
			if tt.stream {
				response = nativeAnthropicStreamResponse()
			}
			upstream := &httpUpstreamRecorder{resp: response}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

			_, err := svc.ForwardAsAnthropic(context.Background(),
				adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicGLMTestAccount(), body, "", "")
			require.NoError(t, err)
			require.Equal(t, "enabled", gjson.GetBytes(upstream.lastBody, "thinking.type").String())
			require.Equal(t, tt.wantEffort, gjson.GetBytes(upstream.lastBody, "output_config.effort").String())
		})
	}
}

func TestNativeAnthropicPassthroughLeavesOtherThinkingUntouched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name string
		body string
	}{
		{name: "glm 5.3 unspecified", body: `{"model":"glm-5.3","max_tokens":32,"stream":false,"messages":[]}`},
		{name: "glm 5.2 disabled", body: `{"model":"glm-5.2","max_tokens":32,"stream":false,"thinking":{"type":"disabled"},"messages":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			upstream := &httpUpstreamRecorder{resp: nativeAnthropicBufferedResponse()}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			_, err := svc.ForwardAsAnthropic(context.Background(),
				adaptiveProtocolTestContext("/v1/messages", body), nativeAnthropicGLMTestAccount(), body, "", "")
			require.NoError(t, err)
			require.JSONEq(t, tt.body, string(upstream.lastBody))
		})
	}
}

func TestNativeAnthropicMessagesCompleteFramesAndMetering(t *testing.T) {
	body := []byte(`{"model":"k3","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	start := ": upstream comment\r\nid: start-1\r\nretry: 3000\r\nevent: message_start\r\n" +
		`data: {"type":"message_start",` + "\r\n" +
		`data: "message":{"id":"msg_1","role":"assistant","model":"provider-observed","content":[],"usage":{"input_tokens":100,"prompt_tokens":100,"cache_read_input_tokens":30,"cache_creation_input_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":15}}}}` + "\r\n\r\n"
	tool := "id: tool-1\r\nevent: content_block_start\r\n" +
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","name":"cc_sess_list","id":"call_cc_sess_list","input":{"literal":"cc_sess_list","n":9007199254740993}}}` + "\r\n\r\n"
	delta := `data: {"type":"message_delta","usage":{"input_tokens":50,"prompt_tokens":100,"cache_read_input_tokens":30,"cache_creation_input_tokens":20,"output_tokens":7}}` + "\r\n\r\n"
	fixture := start + tool + delta + strings.ReplaceAll(anthropicCompatStop, "\n", "\r\n")
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK,
		Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(fixture))}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	c.Set(toolNameRewriteKey, buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`)))
	result, err := svc.ForwardAsAnthropic(t.Context(), c, nativeAnthropicTestAccount(), body, "", "")
	require.NoError(t, err)
	require.Equal(t, 100, result.Usage.InputTokens, "CN input total must include exclusive prompt/cache buckets exactly once")
	require.Equal(t, 30, result.Usage.CacheReadInputTokens)
	require.Equal(t, 20, result.Usage.CacheCreationInputTokens)
	require.Equal(t, 5, result.Usage.CacheCreation5mTokens)
	require.Equal(t, 15, result.Usage.CacheCreation1hTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, "k3", result.Model)
	require.Equal(t, "k3", result.BillingModel)
	require.Equal(t, "k3", result.UpstreamModel)
	require.Equal(t, "provider-observed", observedUpstreamResponseModel(c))
	require.True(t, IsResponseCommitted(c))
	wire := rec.Body.String()
	require.Contains(t, wire, strings.ReplaceAll(start, "\r\n", "\n"))
	require.Contains(t, wire, "id: tool-1\n")
	require.Contains(t, wire, `"name":"sessions_list"`)
	require.Contains(t, wire, `"id":"call_cc_sess_list"`)
	require.Contains(t, wire, `"literal":"cc_sess_list","n":9007199254740993`)
	require.Equal(t, 1, strings.Count(wire, `"type":"message_stop"`))
}

func TestNativeAnthropicMessagesStrictTerminalAndPartialUsage(t *testing.T) {
	prefix := `data: {"type":"message_start","message":{"id":"msg_1","model":"k3","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}` + "\n\n" +
		`data: {"type":"message_delta","usage":{"output_tokens":7}}` + "\n\n"
	errorFrame := ": error comment\nid: failed-1\nretry: 900\nevent: error\ndata: {\"type\":\"error\",\ndata: \"error\":{\"type\":\"overloaded_error\",\"message\":\"busy\"}}\n\n"
	for _, mode := range []string{"normal", "missing", "event_only", "empty_data", "truncated", "invalid_stop_data", "error", "read_error", "event_limit"} {
		t.Run(mode, func(t *testing.T) {
			fixture := prefix
			var readErr error
			switch mode {
			case "normal":
				fixture += anthropicCompatStop
				readErr = errors.New("must stop before a later read failure")
			case "event_only":
				fixture += "event: message_stop\n\n"
			case "empty_data":
				fixture += "event: message_stop\ndata:\n\n"
			case "truncated":
				fixture += strings.TrimSuffix(anthropicCompatStop, "\n")
			case "invalid_stop_data":
				fixture += "event: message_stop\ndata: 42\n\n"
			case "error":
				fixture += errorFrame + anthropicCompatStop
			case "read_error":
				readErr = errors.New("synthetic read failure")
			case "event_limit":
				fixture += strings.Repeat(": comment\n", 100) + "\n"
			}
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: &antigravityCompatErrorReader{data: []byte(fixture), err: readErr}}
			upstream := &httpUpstreamRecorder{resp: response}
			cfg := rawChatCompletionsTestConfig()
			cfg.Gateway.MaxLineSize = 512
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			body := []byte(`{"model":"k3","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
			result, err := svc.ForwardAsAnthropic(t.Context(), c, nativeAnthropicTestAccount(), body, "", "")
			require.NotNil(t, result)
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Equal(t, 7, result.Usage.OutputTokens)
			require.True(t, IsResponseCommitted(c))
			if mode == "normal" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "already delivered output/usage cannot be replayed")
			require.NotContains(t, rec.Body.String(), `"type":"message_stop"`)
			if mode == "read_error" {
				require.ErrorIs(t, err, readErr)
			}
			if mode == "event_limit" {
				require.ErrorIs(t, err, bufio.ErrTooLong)
			}
			if mode == "error" {
				var streamError *sseStreamErrorEventError
				require.ErrorAs(t, err, &streamError)
				require.Equal(t, 1, strings.Count(rec.Body.String(), errorFrame))
			}
		})
	}
}

func TestNativeAnthropicMessagesTerminalAndBoundedDrain(t *testing.T) {
	for _, mode := range []string{"terminal_without_eof", "canceled_terminal", "canceled_timeout", "idle_timeout"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			canceled := strings.HasPrefix(mode, "canceled_")
			if canceled {
				cancel()
			}
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			type outcome struct {
				result *OpenAIForwardResult
				err    error
			}
			finished := make(chan outcome, 1)
			svc := &OpenAIGatewayService{}
			if mode == "idle_timeout" {
				svc = newNativeAnthropicHangTestService(1)
			}
			go func() {
				result, err := svc.handleNativeAnthropicStreamingResponse(t.Context(), &http.Response{Body: reader}, c, nativeAnthropicTestAccount(), "k3", "k3", "k3", nil, time.Now())
				finished <- outcome{result, err}
			}()
			fixture := miniAnthropicSSEStream()
			if strings.HasSuffix(mode, "timeout") {
				fixture = strings.TrimSuffix(fixture, anthropicCompatStop)
			}
			_, err := io.WriteString(writer, fixture)
			require.NoError(t, err)
			select {
			case got := <-finished:
				if mode == "canceled_timeout" {
					require.ErrorIs(t, got.err, context.DeadlineExceeded)
				} else if mode == "idle_timeout" {
					require.ErrorIs(t, got.err, errAnthropicCompatStreamIdle)
				} else {
					require.NoError(t, got.err)
				}
				require.Equal(t, 5, got.result.Usage.OutputTokens)
				require.Equal(t, canceled, got.result.ClientDisconnect)
				if got.result.ClientDisconnect {
					require.Empty(t, rec.Body.String())
				}
			case <-time.After(4 * time.Second):
				t.Fatal("native Messages waited for EOF or failed to bound disconnect draining")
			}
		})
	}
}

func TestNativeAnthropicMessagesWriteFailureDrainsWithoutReplay(t *testing.T) {
	writer := &ccPingResponseWriter{ResponseRecorder: httptest.NewRecorder(), failPing: true}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	fixture := ": ping\n\n" + miniAnthropicSSEStream()
	result, err := (&OpenAIGatewayService{}).handleNativeAnthropicStreamingResponse(t.Context(), &http.Response{Body: io.NopCloser(strings.NewReader(fixture))}, c, nativeAnthropicTestAccount(), "k3", "k3", "k3", nil, time.Now())
	require.NoError(t, err)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 5, result.Usage.OutputTokens)
	require.Nil(t, result.FirstTokenMs)
	require.Zero(t, writer.writesAfterError)
	require.Zero(t, writer.flushesAfterError)
}

func TestNativeAnthropicMessagesKeepaliveWhileFrameBuffered(t *testing.T) {
	const partial = ": delayed frame\nid: delayed-start\nevent: message_start\ndata: {\"type\":\"message_start\",\n"
	const remaining = `data: "message":{"id":"msg_slow","model":"k3","content":[],"usage":{"input_tokens":10,"output_tokens":1}}}` + "\n\n"
	const ping = "event: ping\ndata: {\"type\":\"ping\"}\n\n"
	for _, mode := range []string{"connected", "canceled", "write_failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			writer := &ccPingResponseWriter{ResponseRecorder: httptest.NewRecorder(), flushed: make(chan string, 16), failPing: mode == "write_failure"}
			c, _ := gin.CreateTestContext(writer)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			if mode == "canceled" {
				cancel()
			}
			reader, upstream := io.Pipe()
			defer reader.Close()
			defer upstream.Close()
			svc := newNativeAnthropicHangTestService(5)
			svc.cfg.Gateway.StreamKeepaliveInterval = 1
			type outcome struct {
				result *OpenAIForwardResult
				err    error
			}
			finished := make(chan outcome, 1)
			go func() {
				result, err := svc.handleNativeAnthropicStreamingResponse(t.Context(), &http.Response{Body: reader}, c, nativeAnthropicTestAccount(), "k3", "k3", "k3", nil, time.Now())
				finished <- outcome{result, err}
			}()
			if mode == "write_failure" {
				// The existing writer fails this complete comment frame before
				// the heartbeat timer can fire; no later write/flush is permitted.
				_, err := io.WriteString(upstream, ": ping\n\n")
				require.NoError(t, err)
			}
			// Supply complete lines but no event delimiter. The reader must
			// buffer this partial JSON without making any of it visible.
			_, err := io.WriteString(upstream, partial)
			require.NoError(t, err)
			if mode == "connected" {
				select {
				case visible := <-writer.flushed:
					require.Equal(t, ping, visible, "heartbeat must be a complete independent frame with no buffered payload")
				case <-time.After(2500 * time.Millisecond):
					t.Fatal("buffered upstream half-frame suppressed downstream keepalive")
				}
				_, err := io.WriteString(upstream, remaining+anthropicCompatStop)
				require.NoError(t, err)
			}
			select {
			case got := <-finished:
				if mode == "connected" {
					require.NoError(t, got.err)
					require.False(t, got.result.ClientDisconnect)
					require.Equal(t, 10, got.result.Usage.InputTokens)
					require.Contains(t, writer.Body.String(), partial+remaining)
					require.Equal(t, 1, strings.Count(writer.Body.String(), "id: delayed-start"))
					require.Equal(t, 1, strings.Count(writer.Body.String(), `"type":"message_stop"`))
				} else {
					// The 1s heartbeat timer fires before the 1.2s drain deadline.
					require.ErrorIs(t, got.err, context.DeadlineExceeded)
					require.True(t, got.result.ClientDisconnect)
					require.Empty(t, writer.Body.String())
					require.Zero(t, writer.writesAfterError)
					require.Zero(t, writer.flushesAfterError)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("native consumer failed to finish after terminal or bounded drain")
			}
		})
	}
}

func TestNativeAnthropicChatPreservesStopSequences(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		body := []byte(fmt.Sprintf(`{"model":"k3","stream":%t,"messages":[{"role":"user","content":"hello"}],"stop":["END",3,null,"DONE"]}`, stream))
		upstream := &httpUpstreamRecorder{resp: nativeAnthropicStreamResponse()}
		svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
		_, err := svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), adaptiveProtocolTestContext("/v1/chat/completions", body), nativeAnthropicTestAccount(), body, "")
		require.NoError(t, err)
		require.Equal(t, "END", gjson.GetBytes(upstream.lastBody, "stop_sequences.0").String())
		require.Equal(t, "DONE", gjson.GetBytes(upstream.lastBody, "stop_sequences.1").String())
	}
}
