//go:build unit

package service

import (
	"bufio"
	"context"
	"errors"
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

func newSSEStreamingGatewayService() *GatewayService {
	return &GatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				MaxLineSize: defaultMaxLineSize,
			},
		},
		rateLimitService: &RateLimitService{},
	}
}

func TestClaudeStreamingCancellationDrainsUsageButNotEndlessHeartbeat(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, terminal := range []bool{false, true} {
			name := "native"
			if passthrough {
				name = "apikey"
			}
			if terminal {
				name += "_usage"
			} else {
				name += "_heartbeat"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader, writer := io.Pipe()
				producerDone := make(chan struct{})
				go func() {
					defer close(producerDone)
					defer func() { _ = writer.Close() }()
					_, _ = io.WriteString(writer, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n")
					cancel()
					if terminal {
						_, _ = io.WriteString(writer, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
						return
					}
					for {
						if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
				resp := &http.Response{Body: reader, Header: http.Header{}}
				svc := newSSEStreamingGatewayService()
				start := time.Now()
				var result *streamingResult
				var err error
				if passthrough {
					result, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(ctx, resp, c, &Account{ID: 1}, start, "model")
				} else {
					result, err = svc.handleStreamingResponse(ctx, resp, c, &Account{ID: 1}, start, "model", "model", false)
				}
				<-producerDone
				require.NotNil(t, result)
				require.True(t, result.clientDisconnect)
				require.Equal(t, 7, result.usage.InputTokens)
				if terminal {
					require.NoError(t, err)
					require.Equal(t, 3, result.usage.OutputTokens)
				} else {
					require.Error(t, err)
				}
				require.Less(t, time.Since(start), 2*time.Second)
			})
		}
	}
}

func runMimicStream(t *testing.T, raw string) (*httptest.ResponseRecorder, *streamingResult, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("{}"))

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(raw)),
	}
	svc := newSSEStreamingGatewayService()
	result, err := svc.handleStreamingResponse(
		context.Background(), resp, c, &Account{ID: 1}, time.Now(), "claude-opus-5", "claude-opus-5", true,
	)
	return rec, result, err
}

// 缺最后空行的尾事件不是一个完整 SSE 事件：不得派发给客户端，也不得据此
// 置位 sawTerminalEvent 把截断当成正常结束。
func TestHandleStreamingResponse_UnterminatedFinalEventIsNotDispatched(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":11}}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
	}, "\n") + "\n" // 注意：没有收尾空行

	rec, result, err := runMimicStream(t, raw)
	require.Error(t, err, "截断的流必须报错，不能伪装成正常结束")
	require.Contains(t, err.Error(), "missing terminal event")
	require.NotContains(t, rec.Body.String(), `"type":"message_stop"`, "残片不得发给客户端")
	// 已观测到的 usage 仍要保留，供上层按部分用量入账。
	require.NotNil(t, result)
	require.Equal(t, 11, result.usage.InputTokens)
}

// 半截 JSON 同样不得派发，更不得被当成完成。
func TestHandleStreamingResponse_TruncatedFinalEventIsNotDispatched(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":11}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_d`,
	}, "\n")

	rec, _, err := runMimicStream(t, raw)
	require.Error(t, err)
	require.NotContains(t, rec.Body.String(), "text_d")
}

// 以空行正常收尾的流照常处理，且末事件只派发一次。
func TestHandleStreamingResponse_TrailingBlankLineUnchanged(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":7}}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
		``,
	}, "\n")

	rec, result, err := runMimicStream(t, raw)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, strings.Count(rec.Body.String(), `"type":"message_stop"`),
		"合成边界不应导致末事件被派发两次")
	require.Equal(t, 7, result.usage.InputTokens)
}

// 多条 data 行按 SSE 规范拼成一个值；旧实现只取第一条，JSON 会被截断成
// 非法片段，于是整条事件退化成原样透传且 usage 丢失。
func TestHandleStreamingResponse_MultiLineDataIsJoined(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start",`,
		`data: "message":{"usage":{"input_tokens":21}}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
		``,
	}, "\n")

	_, result, err := runMimicStream(t, raw)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 21, result.usage.InputTokens, "多条 data 行必须先拼接再解析")
}

// 改写 usage 时只替换 data 字段，id / retry / 注释等其它 SSE 字段必须原位保留。
func TestHandleStreamingResponse_PreservesNonDataSSEFields(t *testing.T) {
	raw := strings.Join([]string{
		`: upstream comment`,
		`id: evt-1`,
		`retry: 3000`,
		`event: message_start`,
		`data: {"type":"message_start","message":{"extension":{"big":9007199254740993123,"decimal":1.234567890123456789},"usage":{"input_tokens":3,"cached_tokens":9}}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
		``,
	}, "\n")

	rec, result, err := runMimicStream(t, raw)
	require.NoError(t, err)
	require.NotNil(t, result)

	firstBlock, _, found := strings.Cut(rec.Body.String(), "\n\n")
	require.True(t, found)
	require.Contains(t, firstBlock, ": upstream comment")
	require.Contains(t, firstBlock, "id: evt-1")
	require.Contains(t, firstBlock, "retry: 3000")
	// cached_tokens 归一化会改写 data，确认改写确实发生且只动了 data。
	require.Equal(t, 9, result.usage.CacheReadInputTokens)
	require.Equal(t, 1, strings.Count(firstBlock, "data: "), "改写后只应保留一条 data 行")
	require.Contains(t, firstBlock, "cache_read_input_tokens")
	require.Contains(t, firstBlock, "9007199254740993123")
	require.Contains(t, firstBlock, "1.234567890123456789")
}

// 上游省略 event 字段时仍按 data.type 补齐事件名（旧行为，客户端可能依赖）。
func TestHandleStreamingResponse_SynthesizesMissingEventName(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"type":"message_start","message":{"usage":{"input_tokens":2}}}`,
		``,
		`data: {"type":"message_stop"}`,
		``,
		``,
	}, "\n")

	rec, _, err := runMimicStream(t, raw)
	require.NoError(t, err)
	out := rec.Body.String()
	require.Contains(t, out, "event: message_start\n")
	require.Contains(t, out, "event: message_stop\n")
}

func TestParseAnthropicSSELine(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		field string
		value string
		ok    bool
	}{
		{"标准 data", "data: {\"a\":1}", "data", `{"a":1}`, true},
		{"无空格 data", "data:{\"a\":1}", "data", `{"a":1}`, true},
		{"只剥离一个空格", "data:  x", "data", " x", true},
		{"event", "event: message_stop", "event", "message_stop", true},
		{"注释行", ": keep-alive", "", "keep-alive", true},
		{"无冒号整行作字段名", "ping", "ping", "", true},
		{"空行", "", "", "", false},
		{"缩进仍然宽容匹配", "  data: x", "data", "x", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field, value, ok := parseSSELine(tc.line)
			require.Equal(t, tc.ok, ok)
			require.Equal(t, tc.field, field)
			require.Equal(t, tc.value, value)
		})
	}
}

func TestAnthropicSSEEventBlock(t *testing.T) {
	t.Run("原样回放保留所有行", func(t *testing.T) {
		lines := []string{": c", "id: 1", "event: x", "data: a", "data: b"}
		event := parseSSEEvent(lines)
		require.True(t, event.hasData)
		require.Equal(t, "a\nb", event.data)
		require.Equal(t, strings.Join(lines, "\n")+"\n\n", event.block("x", nil))
	})

	t.Run("改写 data 合并多行并保留其它字段", func(t *testing.T) {
		event := parseSSEEvent([]string{"id: 1", "event: x", "data: a", "data: b"})
		next := "merged"
		require.Equal(t, "id: 1\nevent: x\ndata: merged\n\n", event.block("x", &next))
	})

	t.Run("缺少 event 字段时补齐", func(t *testing.T) {
		event := parseSSEEvent([]string{"data: a"})
		require.False(t, event.hasEvent)
		require.Equal(t, "event: message_stop\ndata: a\n\n", event.block("message_stop", nil))
	})

	t.Run("无 data 字段时追加改写值", func(t *testing.T) {
		event := parseSSEEvent([]string{"event: x"})
		next := "added"
		require.Equal(t, "event: x\ndata: added\n\n", event.block("x", &next))
	})
}

func TestHandleStreamingResponse_InBandErrorPreservesPartialUsage(t *testing.T) {
	for _, header := range []string{"event: error\n", ""} {
		t.Run(header, func(t *testing.T) {
			const errorData = `{"type":"error",` + "\n" + `"error":{"type":"overloaded_error","message":"Overloaded"}}`
			raw := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
				"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
				header + "data: " + strings.ReplaceAll(errorData, "\n", "\ndata: ") + "\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n"
			rec, result, err := runMimicStream(t, raw)
			var streamErr *sseStreamErrorEventError
			require.ErrorAs(t, err, &streamErr)
			require.Equal(t, errorData, streamErr.RawData)
			require.NotNil(t, result, "stream errors must retain already observed usage")
			require.Equal(t, 11, result.usage.InputTokens)
			require.Equal(t, 7, result.usage.OutputTokens, "usage is cumulative, not additive")
			require.NotContains(t, rec.Body.String(), "message_stop")
		})
	}
}

func TestHandleStreamingResponse_StopsAtTerminalBoundary(t *testing.T) {
	for _, terminal := range []string{`{"type":"message_stop"}`, "[DONE]"} {
		t.Run(terminal, func(t *testing.T) {
			raw := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n" +
				"data: " + terminal + "\n\n" +
				"data: {\"type\":\"message_start\",\"message\":{\"id\":\"second_message\",\"usage\":{\"input_tokens\":99}}}\n\n" +
				"event: error\ndata: {\"type\":\"error\"}\n\n"
			rec, result, err := runMimicStream(t, raw)
			require.NoError(t, err)
			require.Equal(t, 11, result.usage.InputTokens)
			require.Equal(t, 1, strings.Count(rec.Body.String(), terminal))
			require.NotContains(t, rec.Body.String(), "second_message")
			require.NotContains(t, rec.Body.String(), "event: error")
		})
	}
}

func TestHandleStreamingResponse_TerminalDoesNotWaitForEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for mode, passthrough := range map[string]bool{"ordinary": false, "passthrough": true} {
		t.Run(mode, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			reader, writer := io.Pipe()
			defer reader.Close()
			defer func() { _ = writer.Close() }()
			done := make(chan error, 1)
			go func() {
				resp := &http.Response{Header: make(http.Header), Body: reader}
				svc := newSSEStreamingGatewayService()
				var err error
				if passthrough {
					_, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
				} else {
					_, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", true)
				}
				done <- err
			}()
			_, err := io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{}}\n\n"+"data: {\"type\":\"message_stop\"}\n\n")
			require.NoError(t, err)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("a complete terminal event must not wait for connection close")
			}
		})
	}
}

func TestHandleStreamingResponse_WhitespaceDoesNotTerminateEvent(t *testing.T) {
	raw := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n \t\n"
	rec, result, err := runMimicStream(t, raw)
	require.ErrorContains(t, err, "missing terminal event")
	require.Equal(t, 11, result.usage.InputTokens)
	require.NotContains(t, rec.Body.String(), "message_stop")
}

// Simulate staged output and a failed downstream write without a network server.
type sseUncommittedWriter struct {
	gin.ResponseWriter
	writeErr error
}

func (w *sseUncommittedWriter) Write(p []byte) (int, error) {
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return len(p), nil
}
func (w *sseUncommittedWriter) Flush() {}

func TestHandleStreamingResponse_PartialUsageSurvivesUncommittedWriter(t *testing.T) {
	for _, disconnected := range []bool{false, true} {
		t.Run(map[bool]string{false: "staged_read_error", true: "disconnected_stream_error"}[disconnected], func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			w := &sseUncommittedWriter{ResponseWriter: c.Writer}
			c.Writer = w
			const start = "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"output_tokens\":1}}}\n\n"
			var body io.ReadCloser = &streamReadCloser{payload: []byte(start), err: io.ErrUnexpectedEOF}
			if disconnected {
				w.writeErr = errors.New("client closed")
				body = io.NopCloser(strings.NewReader(start + "event: error\ndata: {\"type\":\"error\"}\n\n"))
			}
			result, err := newSSEStreamingGatewayService().handleStreamingResponse(context.Background(),
				&http.Response{Header: make(http.Header), Body: body}, c, &Account{ID: 1}, time.Now(), "model", "model", true)
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "observed usage must not be discarded for retry")
			require.NotNil(t, result)
			require.Equal(t, 1, result.usage.OutputTokens)
			require.Equal(t, disconnected, result.clientDisconnect)
		})
	}
}

func TestHandleStreamingResponse_EventSizeLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const limit = 256
	const start = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":1}}}\n\n"
	const stop = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for mode, passthrough := range map[string]bool{"ordinary": false, "passthrough": true} {
		for name, line := range map[string]string{"data": "data: {}\n", "comments": ": heartbeat\n", "unknown_fields": "extension: value\n", "disconnected": "data: {}\n"} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				if name == "disconnected" {
					c.Writer = &failWriteResponseWriter{ResponseWriter: c.Writer}
				}
				svc := newSSEStreamingGatewayService()
				svc.cfg.Gateway.MaxLineSize = limit
				reader, writer := io.Pipe()
				defer reader.Close()
				defer func() { _ = writer.Close() }()
				type outcome struct {
					result *streamingResult
					err    error
				}
				done := make(chan outcome, 1)
				go func() {
					resp := &http.Response{Header: make(http.Header), Body: reader}
					var out outcome
					if passthrough {
						out.result, out.err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
					} else {
						out.result, out.err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
					}
					done <- out
				}()
				_, err := io.WriteString(writer, start+strings.Repeat(line, 64))
				require.NoError(t, err)
				// Every line fits; no blank delimiter or EOF is ever sent.
				select {
				case out := <-done:
					require.ErrorIs(t, out.err, bufio.ErrTooLong)
					require.ErrorContains(t, out.err, "SSE event exceeded")
					var failover *UpstreamFailoverError
					require.False(t, errors.As(out.err, &failover))
					require.NotNil(t, out.result)
					require.Equal(t, 11, out.result.usage.InputTokens)
					require.Equal(t, 1, out.result.usage.OutputTokens)
					require.Equal(t, name == "disconnected", out.result.clientDisconnect)
					require.NotContains(t, rec.Body.String(), line, "never forward an oversized fragment")
				case <-time.After(time.Second):
					t.Fatal("an oversized event must stop without waiting for delimiter or EOF")
				}
			})
		}
		t.Run(mode+"/exact_limit_resets_at_boundary", func(t *testing.T) {
			const ping = "event: ping\ndata: {\"type\":\"ping\"}\n\n"
			frame := ":" + strings.Repeat("x", limit-len(ping)-2) + "\n" + ping
			require.Len(t, frame, limit)
			raw := start + frame + frame + stop
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			svc := newSSEStreamingGatewayService()
			svc.cfg.Gateway.MaxLineSize = limit
			resp := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model")
			} else {
				_, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "model", "model", false)
			}
			require.NoError(t, err)
			require.Equal(t, raw, rec.Body.String())
		})
	}
}

func TestHandleStreamingResponse_ReadErrorMarksCommittedResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	resp := &http.Response{Header: make(http.Header), Body: &streamReadCloser{
		payload: []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\n"),
		err:     io.ErrUnexpectedEOF,
	}}
	result, err := newSSEStreamingGatewayService().handleStreamingResponse(context.Background(),
		resp, c, &Account{ID: 1}, time.Now(), "model", "model", true)
	require.Error(t, err)
	require.Equal(t, 11, result.usage.InputTokens)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: error\n"))
	require.True(t, IsResponseCommitted(c), "the handler must not append a second fallback error")
}
