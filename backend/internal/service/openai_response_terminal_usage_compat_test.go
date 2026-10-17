//go:build unit

package service

import (
	"bytes"
	"context"
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

func TestEffectiveOpenAISSEEventTypePrefersPayload(t *testing.T) {
	t.Parallel()

	require.Equal(t, "response.failed", effectiveOpenAISSEEventType([]byte(`{"type":"response.failed"}`), "error"))
	require.Equal(t, "error", effectiveOpenAISSEEventType([]byte(`{"error":{"message":"failed"}}`), " error "))
	require.JSONEq(t, `{"type":"error","error":{"message":"failed"}}`, openAICompatPayloadWithEventType(`{"type":"","error":{"message":"failed"}}`, "error"))
}

func TestExtractOpenAISSETerminalEventUsesEventField(t *testing.T) {
	t.Parallel()

	body := "event: error\n" +
		"data: {\"error\":{\"message\":\"provider failed\"}}\n\n" +
		"data: [DONE]\n\n"
	eventType, payload, ok := extractOpenAISSETerminalEvent(body)
	require.True(t, ok)
	require.Equal(t, "error", eventType)
	require.Equal(t, "provider failed", extractOpenAISSEErrorMessage(payload))
}

func TestExtractOpenAISSETerminalEventUsesFinalAuthoritativeTerminal(t *testing.T) {
	t.Parallel()

	body := "data: {\"type\":\"error\",\"error\":{\"message\":\"recovering\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\"}}\n\n"
	eventType, payload, ok := extractOpenAISSETerminalEvent(body)
	require.True(t, ok)
	require.Equal(t, "response.completed", eventType)
	require.Equal(t, "resp_1", gjson.GetBytes(payload, "response.id").String())
}

func TestParseSSEUsageEffectiveTerminalRules(t *testing.T) {
	t.Parallel()

	svc := &OpenAIGatewayService{}
	usage := &OpenAIUsage{}
	svc.parseSSEUsageBytesWithType([]byte(`{"usage":{"input_tokens":17,"output_tokens":5,"input_tokens_details":{"cached_tokens":3}}}`), "response.in_progress", usage)
	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"id":"resp_1"}}`), "response.completed", usage)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 5, CacheReadInputTokens: 3}, *usage)

	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"usage":{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":0}}}}`), "response.completed", usage)
	require.Equal(t, OpenAIUsage{InputTokens: 17, OutputTokens: 5, CacheReadInputTokens: 3}, *usage)

	svc.parseSSEUsageBytesWithType([]byte(`{"response":{"usage":{"input_tokens":2,"output_tokens":0,"input_tokens_details":{"cached_tokens":0}}}}`), "response.completed", usage)
	require.Equal(t, OpenAIUsage{InputTokens: 2}, *usage)
}

func BenchmarkParseSSEUsageNoUsageDelta(b *testing.B) {
	svc := &OpenAIGatewayService{}
	usage := &OpenAIUsage{}
	payload := []byte(`{"type":"response.output_text.delta","delta":"hello"}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.parseSSEUsageBytesWithType(payload, "response.output_text.delta", usage)
	}
}

func TestOpenAICompatTerminalResponseSynthesizesBareError(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"type":"error","code":"upstream_error","error":{"message":"provider failed"}}`)
	event := &apicompat.ResponsesStreamEvent{Type: "error", Code: "upstream_error"}
	response := openAICompatTerminalResponse(event, payload)
	require.NotNil(t, response)
	require.Equal(t, "failed", response.Status)
	require.Equal(t, "upstream_error", response.Error.Code)
	require.Equal(t, "provider failed", response.Error.Message)
}

func TestForEachOpenAISSEFrameDataTypeOverridesEventField(t *testing.T) {
	t.Parallel()

	var types []string
	forEachOpenAISSEFrame(strings.Join([]string{
		"event: response.in_progress",
		`data: {"type":"response.completed","response":{"id":"resp_1"}}`,
		"",
	}, "\n"), func(eventType string, _ []byte) {
		types = append(types, eventType)
	})
	require.Equal(t, []string{"response.completed"}, types)
}

func TestSampleOpenAIMissingUsageLogRateLimitsAndCounts(t *testing.T) {
	var sampler openAIMissingUsageLogSampler
	now := time.Unix(100, 0)
	logNow, total, suppressed := sampler.sample(now)
	require.True(t, logNow)
	require.Equal(t, uint64(1), total)
	require.Zero(t, suppressed)

	logNow, total, _ = sampler.sample(now.Add(time.Second))
	require.False(t, logNow)
	require.Equal(t, uint64(2), total)

	logNow, total, suppressed = sampler.sample(now.Add(openAIMissingUsageLogInterval))
	require.True(t, logNow)
	require.Equal(t, uint64(3), total)
	require.Equal(t, uint64(1), suppressed)
}

func TestOpenAIHTTPForwardRetainsMeteredFailure(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, terminal := range []string{"missing", "failed", "compact_failed"} {
				t.Run(fmt.Sprintf("passthrough=%t/stream=%t/terminal=%s", passthrough, stream, terminal), func(t *testing.T) {
					payload := []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":"hello"}`, stream))
					if terminal == "compact_failed" {
						payload = []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":[{"type":"message","role":"user","content":"hello"},{"type":"compaction_trigger"}]}`, stream))
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
					SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
					if terminal == "compact_failed" {
						MarkOpenAINativeCompactionV2(c)
					}
					wire := "data: " + `{"type":"response.in_progress","response":{"id":"resp_metered","usage":{"input_tokens":11,"output_tokens":2,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":4}}}}` + "\n\n"
					if stream && terminal != "compact_failed" {
						wire += "data: " + `{"type":"response.output_text.delta","response_id":"resp_metered","delta":"partial"}` + "\n\n"
					}
					if terminal != "missing" {
						wire += "data: " + `{"type":"response.failed","response":{"id":"resp_metered","status":"failed","error":{"type":"invalid_request_error","code":"content_policy_violation","message":"blocked"},"usage":{"input_tokens":11,"output_tokens":2,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":4}}}}` + "\n\n"
					}
					if terminal == "compact_failed" {
						wire = strings.ReplaceAll(wire, "content_policy_violation", "context_length_exceeded")
					}
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req_metered"}}, Body: io.NopCloser(strings.NewReader(wire))}}
					svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAICompactModel: "gpt-5.4"}}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
					account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-token"}, Extra: map[string]any{"openai_passthrough": passthrough}}
					result, err := svc.Forward(context.Background(), c, account, payload)
					require.Error(t, err, "missing or failed terminal cannot become a successful JSON response")
					require.NotNil(t, result, "observed usage must reach the handler even when forwarding fails")
					require.Equal(t, OpenAIUsage{InputTokens: 11, OutputTokens: 2, CacheReadInputTokens: 3, CacheCreationInputTokens: 4}, result.Usage)
					require.Equal(t, "req_metered", result.RequestID)
					require.Len(t, upstream.requests, 1)
				})
			}
		}
	}
}

func TestOpenAIHTTPDisconnectDrainIsBounded(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/stream=%t", passthrough, stream), func(t *testing.T) {
				reqCtx, cancel := context.WithCancel(context.Background())
				defer cancel()
				reader, writer := io.Pipe()
				defer reader.Close()
				defer writer.Close()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(reqCtx)
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":"hi"}`, stream))
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req_drain"}}, Body: reader}}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"openai_passthrough": passthrough}}
				type outcome struct {
					result *OpenAIForwardResult
					err    error
				}
				done := make(chan outcome, 1)
				go func() { result, err := svc.Forward(reqCtx, c, account, body); done <- outcome{result, err} }()
				_, err := io.WriteString(writer, "data: "+`{"type":"response.in_progress","response":{"id":"resp_drain","usage":{"input_tokens":11,"output_tokens":2,"input_tokens_details":{"cached_tokens":3}}}}`+"\n\n")
				require.NoError(t, err)
				cancel()
				pingDone := make(chan struct{})
				go func() {
					defer close(pingDone)
					for {
						if _, err := io.WriteString(writer, ": ping\n\n"); err != nil {
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()
				var got outcome
				select {
				case got = <-done:
				case <-time.After(2 * time.Second):
					writer.Close()
					<-done
					<-pingDone
					t.Fatal("continuous upstream traffic extended the disconnected usage drain")
				}
				writer.Close()
				<-pingDone
				require.Error(t, got.err)
				require.NotNil(t, got.result)
				require.True(t, got.result.ClientDisconnect)
				require.Equal(t, 11, got.result.Usage.InputTokens)
				require.Equal(t, 3, got.result.Usage.CacheReadInputTokens)
			})
		}
	}
}

func TestOpenAIHTTPBufferedTerminalReturnsBeforeEOF(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct {
			name, event, status string
			header, multiline   bool
		}{
			{"completed", "response.completed", "completed", true, false},
			{"headerless", "response.completed", "completed", false, false},
			{"incomplete_multiline", "response.incomplete", "incomplete", false, true},
			{"cancelled", "response.cancelled", "cancelled", true, false},
		} {
			t.Run(fmt.Sprintf("passthrough=%t/%s", passthrough, tc.name), func(t *testing.T) {
				reader, writer := io.Pipe()
				defer reader.Close()
				defer writer.Close()
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: reader}}
				if !tc.header {
					upstream.resp.Header = http.Header{}
				}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream, toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{"openai_passthrough": passthrough}}
				done := make(chan error, 1)
				go func() {
					_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"input":"hi"}`))
					done <- err
				}()
				payload := fmt.Sprintf(`{"type":%q,"response":{"id":"resp_done","status":%q,"output":[],"usage":{"input_tokens":11,"output_tokens":2}}}`, tc.event, tc.status)
				if tc.multiline {
					payload = strings.Replace(payload, ",", ",\ndata: ", 1)
				}
				_, err := io.WriteString(writer, "data: "+payload+"\n\n")
				require.NoError(t, err)
				select {
				case err := <-done:
					require.NoError(t, err)
					require.Equal(t, tc.status, gjson.Get(recorder.Body.String(), "status").String())
					require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")
				case <-time.After(500 * time.Millisecond):
					writer.Close()
					<-done
					t.Fatal("a complete response waited for the upstream connection to close")
				}
			})
		}
	}
}

func TestOpenAICompatBufferedFailureRetainsUsage(t *testing.T) {
	for _, messages := range []bool{false, true} {
		for _, terminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("messages=%t/terminal=%t", messages, terminal), func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				wire := "data: " + `{"type":"response.in_progress","response":{"id":"resp_partial","usage":{"input_tokens":11,"output_tokens":2,"input_tokens_details":{"cached_tokens":3,"cache_write_tokens":4}}}}` + "\n\n"
				if terminal {
					wire += "data: " + `{"type":"response.failed","response":{"id":"resp_partial","status":"failed","error":{"code":"rate_limit_exceeded","message":"rate limit"}}}` + "\n\n"
				}
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"req_partial"}}, Body: io.NopCloser(strings.NewReader(wire))}
				svc := &OpenAIGatewayService{cfg: &config.Config{}}
				account := &Account{ID: 45, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				var result *OpenAIForwardResult
				var err error
				if messages {
					result, err = svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "gpt-5.5", "gpt-5.5", "gpt-5.5", time.Now())
				} else {
					result, err = svc.handleChatBufferedStreamingResponse(resp, c, account, "gpt-5.5", "gpt-5.5", "gpt-5.5", time.Now())
				}
				require.Error(t, err)
				require.NotNil(t, result)
				require.Equal(t, OpenAIUsage{InputTokens: 11, OutputTokens: 2, CacheReadInputTokens: 3, CacheCreationInputTokens: 4}, result.Usage)
			})
		}
	}
}

func TestOpenAIHTTPCanceledHeaderWaitIsBounded(t *testing.T) {
	clientCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(clientCtx)
	upstream := &blockingOpenAIResponseHeaderUpstream{canceled: make(chan struct{})}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
	req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/responses", nil).WithContext(context.WithoutCancel(clientCtx))
	time.AfterFunc(10*time.Millisecond, cancel)
	resp, err := svc.doOpenAIResponsesUpstream(c, req, "", &Account{ID: 46, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, true)
	require.Nil(t, resp)
	require.ErrorIs(t, err, context.Canceled)
	select {
	case <-upstream.canceled:
	default:
		t.Fatal("upstream request outlived the disconnected drain window")
	}
}
