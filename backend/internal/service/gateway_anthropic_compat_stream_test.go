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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

var anthropicCompatRoutes = []string{"chat_json", "chat_sse", "responses_json", "responses_sse"}

// Both gateway families must share the same return-path contracts.
var anthropicCompatReturnRoutes = append(append([]string(nil), anthropicCompatRoutes...),
	"native_chat_json", "native_chat_sse", "native_responses_json", "native_responses_sse")

func anthropicCompatFixture() string {
	return "data: " + strings.Join([]string{
		`{"type":"message_start","message":{"id":"msg_compat","role":"assistant","content":[],"usage":{"input_tokens":10,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"cc_sess_list"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_cc_sess_list","name":"cc_sess_list","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"literal\":\"cc_sess_list\",\"number\":9007199254740993}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
	}, "\n\ndata: ") + "\n\n"
}

const anthropicCompatStop = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func runAnthropicCompatRoute(svc *GatewayService, route string, c *gin.Context, resp *http.Response) (*ForwardResult, error) {
	if strings.HasPrefix(route, "native_") {
		native := &OpenAIGatewayService{cfg: svc.cfg, responseHeaderFilter: svc.responseHeaderFilter}
		var result *OpenAIForwardResult
		var err error
		switch strings.TrimPrefix(route, "native_") {
		case "chat_json":
			result, err = native.handleCCBufferedFromNativeAnthropic(resp, c, "client-model", "billing-model", "claude-sonnet-4-6", nil, time.Now())
		case "chat_sse":
			result, err = native.handleCCStreamingFromNativeAnthropic(resp, c, "client-model", "billing-model", "claude-sonnet-4-6", nil, time.Now())
		case "responses_json":
			result, err = native.handleResponsesBufferedFromNativeAnthropic(resp, c, "client-model", "billing-model", "claude-sonnet-4-6", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		default:
			result, err = native.handleResponsesStreamingFromNativeAnthropic(resp, c, "client-model", "billing-model", "claude-sonnet-4-6", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		}
		if result == nil {
			return nil, err
		}
		return &ForwardResult{Model: result.Model, UpstreamModel: result.UpstreamModel, ClientDisconnect: result.ClientDisconnect, FirstTokenMs: result.FirstTokenMs,
			Usage: ClaudeUsage{InputTokens: result.Usage.InputTokens - result.Usage.CacheReadInputTokens - result.Usage.CacheCreationInputTokens,
				OutputTokens: result.Usage.OutputTokens, CacheReadInputTokens: result.Usage.CacheReadInputTokens,
				CacheCreationInputTokens: result.Usage.CacheCreationInputTokens, CacheCreation5mTokens: result.Usage.CacheCreation5mTokens, CacheCreation1hTokens: result.Usage.CacheCreation1hTokens}}, err
	}

	switch route {
	case "chat_json":
		return svc.handleCCBufferedFromAnthropic(resp, c, "client-model", "claude-sonnet-4-6", nil, time.Now())
	case "chat_sse":
		return svc.handleCCStreamingFromAnthropic(resp, c, "client-model", "claude-sonnet-4-6", nil, time.Now(), true)
	case "responses_json":
		return svc.handleResponsesBufferedStreamingResponse(resp, c, "client-model", "claude-sonnet-4-6", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
	default:
		return svc.handleResponsesStreamingResponse(resp, c, "client-model", "claude-sonnet-4-6", nil, time.Now(), apicompat.ResponsesClientToolMapping{}, true)
	}
}

func anthropicCompatPayloads(body, route string) []gjson.Result {
	if strings.HasSuffix(route, "json") {
		return []gjson.Result{gjson.Parse(body)}
	}
	var payloads []gjson.Result
	for _, line := range strings.Split(body, "\n") {
		if data, ok := extractOpenAISSEDataLine(line); ok && gjson.Valid(data) {
			payloads = append(payloads, gjson.Parse(data))
		}
	}
	return payloads
}

// Promotes the prior overlay's four return-path assertions, including the
// untouched argument, ID and text contract and full SSE field framing.
func TestAnthropicCompatToolNamesAndEventFraming(t *testing.T) {
	for _, route := range anthropicCompatReturnRoutes {
		for _, mapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mapped=%t", route, mapped), func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				if mapped {
					c.Set(toolNameRewriteKey, buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`)))
				}
				// Split every JSON event over data lines, with comments/id between
				// fields, optional event fields, compact colons and CRLF endings.
				fixture := anthropicCompatFixture() + anthropicCompatStop
				var framed strings.Builder
				for i, frame := range strings.Split(strings.TrimSuffix(fixture, "\n\n"), "\n\n") {
					payload := frame[strings.Index(frame, "data: ")+6:]
					parts := strings.SplitN(payload, ",", 2)
					fmt.Fprintf(&framed, ": comment\r\nid: %d\r\ndata:%s", i, parts[0])
					if len(parts) > 1 {
						fmt.Fprintf(&framed, ",\r\n: between data\r\ndata: %s", parts[1])
					}
					if i%2 == 0 {
						fmt.Fprintf(&framed, "\r\nevent:%s", gjson.Get(payload, "type").String())
					}
					framed.WriteString("\r\n\r\n")
				}
				result, err := runAnthropicCompatRoute(&GatewayService{}, route, c, &http.Response{Body: io.NopCloser(strings.NewReader(framed.String()))})
				require.NoError(t, err)
				require.Equal(t, ClaudeUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 3, CacheCreationInputTokens: 2}, result.Usage)
				want := "cc_sess_list"
				if mapped {
					want = "sessions_list"
				}
				names, args, ids, texts := []string{}, "", []string{}, ""
				for _, p := range anthropicCompatPayloads(rec.Body.String(), route) {
					for _, choice := range p.Get("choices").Array() {
						for _, field := range []string{"message", "delta"} {
							texts += choice.Get(field + ".content").String()
							for _, call := range choice.Get(field + ".tool_calls").Array() {
								if name := call.Get("function.name").String(); name != "" {
									names = append(names, name)
								}
								if id := call.Get("id").String(); id != "" {
									ids = append(ids, id)
								}
								args += call.Get("function.arguments").String()
							}
						}
					}
					for _, path := range []string{"output", "response.output"} {
						for _, item := range p.Get(path).Array() {
							if item.Get("type").String() == "function_call" {
								names = append(names, item.Get("name").String())
								ids = append(ids, item.Get("call_id").String())
								args = item.Get("arguments").String()
							}
							for _, part := range item.Get("content").Array() {
								texts += part.Get("text").String()
							}
						}
					}
					if p.Get("item.type").String() == "function_call" {
						names = append(names, p.Get("item.name").String())
					}
				}
				require.NotEmpty(t, names)
				for _, name := range names {
					require.Equal(t, want, name)
				}
				require.NotEmpty(t, ids)
				for _, id := range ids {
					require.Equal(t, "call_cc_sess_list", id)
				}
				require.JSONEq(t, `{"literal":"cc_sess_list","number":9007199254740993}`, args)
				require.Equal(t, "9007199254740993", gjson.Get(args, "number").Raw)
				require.Contains(t, texts, "cc_sess_list")
				require.NotContains(t, texts, "sessions_list")
			})
		}
	}
}

func TestAnthropicCompatStrictTerminalAndPartialUsage(t *testing.T) {
	for _, route := range anthropicCompatReturnRoutes {
		for _, kind := range []string{"eof", "message_start_only", "partial_output", "truncated_stop", "read_error", "stream_error", "line_limit", "event_limit", "normal"} {
			t.Run(route+"/"+kind, func(t *testing.T) {
				fixture := anthropicCompatFixture()
				var readErr error
				wantOutput := 5
				svc := &GatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: 1024}}}
				switch kind {
				case "normal":
					fixture += anthropicCompatStop
					readErr = errors.New("read failure after terminal must not override success")
				case "message_start_only", "partial_output":
					frames := strings.Split(fixture, "\n\n")
					count := 1
					if kind == "partial_output" {
						count = 3
					}
					fixture = strings.Join(frames[:count], "\n\n") + "\n\n"
					wantOutput = 0
				case "truncated_stop":
					fixture += strings.TrimSuffix(anthropicCompatStop, "\n")
				case "read_error":
					readErr = errors.New("synthetic upstream read failure")
				case "stream_error":
					fixture += "event: error\ndata: " + `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"},"usage":{"output_tokens":7}}` + "\n\n" + anthropicCompatStop
					wantOutput = 7
				case "line_limit":
					fixture += ":" + strings.Repeat("x", 1024) + "\n\n"
				case "event_limit":
					fixture += strings.Repeat(": comment\n", 120) + "\n"
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				result, err := runAnthropicCompatRoute(svc, route, c, &http.Response{Body: &antigravityCompatErrorReader{data: []byte(fixture), err: readErr}})
				require.NotNil(t, result)
				require.Equal(t, ClaudeUsage{InputTokens: 10, OutputTokens: wantOutput, CacheReadInputTokens: 3, CacheCreationInputTokens: 2}, result.Usage)
				if kind == "normal" {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "observed output/usage must not be replayed")
				if kind == "read_error" {
					require.ErrorIs(t, err, readErr)
				}
				if strings.HasSuffix(kind, "limit") {
					require.ErrorIs(t, err, bufio.ErrTooLong)
				}
				if kind == "stream_error" {
					var upstreamErr *sseStreamErrorEventError
					require.ErrorAs(t, err, &upstreamErr)
				}
				require.NotContains(t, rec.Body.String(), "response.completed")
				require.NotContains(t, rec.Body.String(), "[DONE]")
				if strings.HasSuffix(route, "json") {
					require.Equal(t, http.StatusBadGateway, rec.Code)
				}
				if strings.HasSuffix(route, "responses_sse") {
					payloads := anthropicCompatPayloads(rec.Body.String(), route)
					failed := payloads[len(payloads)-1]
					require.Equal(t, "response.failed", failed.Get("type").String())
					require.Equal(t, "failed", failed.Get("response.status").String())
					require.Equal(t, int64(wantOutput), failed.Get("response.usage.output_tokens").Int())
				}
			})
		}
	}
}

func TestAnthropicCompatEarlyErrorDefersOnlyForFailoverOwner(t *testing.T) {
	for _, route := range []string{"chat_sse", "responses_sse", "native_chat_sse", "native_responses_sse"} {
		for _, mode := range []string{"early", "heartbeat", "usage", "output", "canceled"} {
			t.Run(route+"/"+mode, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
				if mode == "canceled" {
					cancel()
				}
				if mode == "heartbeat" {
					stop := StartSSEKeepalive(c, time.Hour)
					value, _ := c.Get(openAICompactSSEKeepaliveKey)
					require.True(t, value.(*openAICompactSSEKeepalive).beat())
					stop()
				}
				usage := ""
				if mode == "usage" {
					usage = `,"usage":{"input_tokens":12}`
				}
				wire := `event: error` + "\ndata: " + `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}` + usage + "}\n\n"
				if mode == "output" {
					wire = "data: " + `{"type":"message_start","message":{"id":"msg_empty_usage","role":"assistant","content":[]}}` + "\n\n" + wire
				}
				before := rec.Body.String()
				result, err := runAnthropicCompatRoute(&GatewayService{}, route, c, &http.Response{Body: io.NopCloser(strings.NewReader(wire))})
				var upstreamErr *sseStreamErrorEventError
				require.ErrorAs(t, err, &upstreamErr)
				if !strings.HasPrefix(route, "native_") && (mode == "early" || mode == "heartbeat") {
					require.Nil(t, result)
					require.Equal(t, before, rec.Body.String(), "caller decides failover before a failure frame is committed")
				} else {
					require.NotNil(t, result)
					if mode == "canceled" {
						require.True(t, result.ClientDisconnect)
						require.Equal(t, before, rec.Body.String())
					} else {
						require.Contains(t, rec.Body.String(), "overloaded")
						if mode == "usage" {
							require.Equal(t, 12, result.Usage.InputTokens)
						}
					}
				}
			})
		}
	}
}

func TestAnthropicCompatTerminalBeforeEOFAndCancellationDrain(t *testing.T) {
	for _, route := range anthropicCompatReturnRoutes {
		for _, mode := range []string{"terminal", "canceled_terminal", "canceled_timeout", "canceled_ping_timeout"} {
			t.Run(route+"/"+mode, func(t *testing.T) {
				t.Parallel()
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx)
				if mode != "terminal" {
					cancel()
				}
				reader, writer := io.Pipe()
				defer reader.Close()
				defer writer.Close()
				type outcome struct {
					result *ForwardResult
					err    error
				}
				finished := make(chan outcome, 1)
				go func() {
					result, err := runAnthropicCompatRoute(&GatewayService{}, route, c, &http.Response{Body: reader})
					finished <- outcome{result, err}
				}()
				fixture := anthropicCompatFixture()
				if !strings.HasSuffix(mode, "timeout") {
					fixture += anthropicCompatStop
				}
				_, err := io.WriteString(writer, fixture)
				require.NoError(t, err)
				if mode == "canceled_ping_timeout" {
					go func() {
						ticker := time.NewTicker(5 * time.Millisecond)
						defer ticker.Stop()
						for range ticker.C {
							if _, err := io.WriteString(writer, "event: ping\ndata: {\"type\":\"ping\"}\n\n"); err != nil {
								return
							}
						}
					}()
				}
				select {
				case got := <-finished:
					if strings.HasSuffix(mode, "timeout") {
						require.ErrorIs(t, got.err, context.DeadlineExceeded)
					} else {
						require.NoError(t, got.err)
					}
					require.Equal(t, 5, got.result.Usage.OutputTokens)
					require.Equal(t, mode != "terminal", got.result.ClientDisconnect)
					if mode != "terminal" {
						require.Empty(t, rec.Body.String())
					}
				case <-time.After(4 * time.Second):
					t.Fatal("stream waited for EOF or exceeded bounded drain")
				}
			})
		}
	}
}

func TestAnthropicCompatForwardFeedbackAndAttemptMapping(t *testing.T) {
	for _, route := range anthropicCompatRoutes {
		for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("%s/status=%d", route, status), func(t *testing.T) {
				cache := &identityCacheStub{}
				upstream := &queuedHTTPUpstreamStub{}
				feedback := http.Header{"Set-Cookie": {"do-not-cache"}, "Authorization": {"do-not-cache"}}
				for _, name := range claudeCodeControlHeaderNames {
					feedback.Set(name, "observed-"+name)
				}
				responseBody := anthropicCompatFixture() + anthropicCompatStop
				if status != http.StatusOK {
					responseBody = `{"error":{"type":"invalid_request_error","message":"synthetic rejection"}}`
				}
				upstream.responses = []*http.Response{
					{StatusCode: status, Header: feedback, Body: io.NopCloser(strings.NewReader(responseBody))},
					{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(anthropicCompatFixture() + anthropicCompatStop))},
				}
				upstream.onCall = func(req *http.Request, stub *queuedHTTPUpstreamStub) {
					if stub.callCount == 2 {
						for _, name := range claudeCodeControlHeaderNames {
							require.Equal(t, "observed-"+name, getHeaderRaw(req.Header, name))
						}
					}
				}
				svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, identityService: NewIdentityService(cache)}
				account := newAnthropicOAuthAccountForPartialUsageTest()
				body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"sessions_list","parameters":{"type":"object"}}}]`
				if strings.HasPrefix(route, "responses") {
					body = `{"model":"claude-sonnet-4-6","input":"hello","tools":[{"type":"function","name":"sessions_list","parameters":{"type":"object"}}]`
				}
				body += fmt.Sprintf(",\"stream\":%t}", strings.HasSuffix(route, "sse"))
				for attempt := 0; attempt < 2; attempt++ {
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
					if attempt == 0 {
						c.Request.Header.Set("anthropic-usage-limit", "caller-only")
					}
					var result *ForwardResult
					var err error
					if strings.HasPrefix(route, "chat") {
						result, err = svc.ForwardAsChatCompletions(t.Context(), c, account, []byte(body), nil)
					} else {
						result, err = svc.ForwardAsResponses(t.Context(), c, account, []byte(body), nil)
					}
					if attempt == 0 && status != http.StatusOK {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
						require.NotNil(t, result)
						require.Contains(t, rec.Body.String(), `"name":"sessions_list"`)
					}
					require.Equal(t, "cc_sess_list", gjson.GetBytes(upstream.requestBodies[attempt], "tools.0.name").String())
					cached, err := cache.GetClaudeCodeHeaders(t.Context(), account.ID)
					require.NoError(t, err)
					require.Equal(t, filterClaudeCodeControlHeaders(feedback), cached)
					other, err := cache.GetClaudeCodeHeaders(t.Context(), account.ID+1)
					require.NoError(t, err)
					require.Empty(t, other)
				}
			})
		}
	}
}

func TestAnthropicCompatWriteFailureDrainsWithoutMoreWrites(t *testing.T) {
	for _, route := range []string{"chat_sse", "responses_sse", "native_chat_sse", "native_responses_sse"} {
		t.Run(route, func(t *testing.T) {
			writer := &ccPingResponseWriter{ResponseRecorder: httptest.NewRecorder(), failPing: true}
			c, _ := gin.CreateTestContext(writer)
			fixture := "event: ping\ndata: {\"type\":\"ping\"}\n\n" + anthropicCompatFixture() + anthropicCompatStop
			result, err := runAnthropicCompatRoute(&GatewayService{}, route, c, &http.Response{Body: io.NopCloser(strings.NewReader(fixture))})
			require.NoError(t, err)
			require.True(t, result.ClientDisconnect)
			require.Equal(t, 5, result.Usage.OutputTokens)
			require.Zero(t, writer.writesAfterError)
			require.Zero(t, writer.flushesAfterError)
			require.Nil(t, result.FirstTokenMs)
		})
	}
}

func TestAnthropicCompatForwardFeedbackOnTransportError(t *testing.T) {
	for _, route := range []string{"chat", "responses"} {
		t.Run(route, func(t *testing.T) {
			cache := &identityCacheStub{}
			upstream := &queuedHTTPUpstreamStub{
				responses: []*http.Response{{StatusCode: http.StatusBadGateway, Header: http.Header{"X-Claude-Code-Compaction": {"observed"}}, Body: io.NopCloser(strings.NewReader(""))}},
				errors:    []error{errors.New("synthetic transport failure with response")},
			}
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream, identityService: NewIdentityService(cache)}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			account := newAnthropicOAuthAccountForPartialUsageTest()
			var err error
			if route == "chat" {
				_, err = svc.ForwardAsChatCompletions(t.Context(), c, account, []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]}`), nil)
			} else {
				_, err = svc.ForwardAsResponses(t.Context(), c, account, []byte(`{"model":"claude-sonnet-4-6","input":"hello"}`), nil)
			}
			require.Error(t, err)
			feedback, err := cache.GetClaudeCodeHeaders(t.Context(), account.ID)
			require.NoError(t, err)
			require.Equal(t, http.Header{"x-claude-code-compaction": {"observed"}}, feedback)
		})
	}
}

func TestAnthropicCompatMappingBelongsToCurrentAttempt(t *testing.T) {
	for _, route := range anthropicCompatRoutes {
		t.Run(route, func(t *testing.T) {
			upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(anthropicCompatFixture() + anthropicCompatStop))}}
			svc := newForwardPartialUsageServiceForTest(upstream)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
			c.Set(toolNameRewriteKey, buildToolNameRewriteFromBody([]byte(`{"tools":[{"name":"sessions_list"}]}`)))
			body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hello"}]`
			if strings.HasPrefix(route, "responses") {
				body = `{"model":"claude-sonnet-4-6","input":"hello"`
			}
			body += fmt.Sprintf(",\"stream\":%t}", strings.HasSuffix(route, "sse"))
			var err error
			if strings.HasPrefix(route, "chat") {
				_, err = svc.ForwardAsChatCompletions(t.Context(), c, newAnthropicAPIKeyAccountForTest(), []byte(body), nil)
			} else {
				_, err = svc.ForwardAsResponses(t.Context(), c, newAnthropicAPIKeyAccountForTest(), []byte(body), nil)
			}
			require.NoError(t, err)
			require.Nil(t, toolNameRewriteFromContext(c))
			require.Contains(t, rec.Body.String(), `"name":"cc_sess_list"`)
			require.NotContains(t, rec.Body.String(), `"name":"sessions_list"`)
		})
	}
}

func TestAnthropicCompatSafetyRefusalDoesNotDeferOrEmitQueuedOutput(t *testing.T) {
	for _, route := range []string{"chat_sse", "responses_sse"} {
		for _, code := range []string{"content_policy_violation", "content_policy_error", "safety_error", "cyber_policy"} {
			t.Run(route+"/"+code, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				observed := 0
				req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Request = req.WithContext(WithUpstreamRefusalObserver(req.Context(), func() { observed++ }))
				stream := fmt.Sprintf("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":%q}}\n\n", code) + anthropicCompatFixture() + anthropicCompatStop
				_, err := runAnthropicCompatRoute(&GatewayService{}, route, c, &http.Response{Body: io.NopCloser(strings.NewReader(stream))})
				require.Error(t, err)
				require.NotEmpty(t, rec.Body.String(), "refusal is committed, not deferred for failover")
				require.NotContains(t, rec.Body.String(), "cc_sess_list")
				require.Equal(t, 1, observed)
			})
		}
	}
}
