//go:build unit

package service

import (
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
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func conversionValidationContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

func conversionValidationResponse(wire string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"validation-request"}}, Body: io.NopCloser(strings.NewReader(wire))}
}

// Usage is deliberately reported after the bad arguments, so a converter that
// returns early or confuses the error with client cancellation loses it.
func conversionValidationWire(mode string) string {
	if mode == "snapshot" {
		return "data: " + `{"type":"response.completed","response":{"id":"r","status":"completed","output":[{"type":"function_call","id":"f","call_id":"c","name":"inspect","arguments":"[]"}],"usage":{"input_tokens":19,"output_tokens":11,"input_tokens_details":{"cached_tokens":5}}}}` + "\n\n"
	}
	wire := "data: " + `{"type":"response.created","response":{"id":"r","status":"in_progress"}}` + "\n\n" +
		"data: " + `{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"f","call_id":"c","name":"inspect","arguments":""}}` + "\n\n" +
		"data: " + `{"type":"response.function_call_arguments.delta","output_index":0,"delta":"{\"record\":3"}` + "\n\n"
	if mode == "conflict" {
		wire += "data: " + `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"record\":42}"}` + "\n\n"
	}
	return wire + "data: " + `{"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":19,"output_tokens":11,"input_tokens_details":{"cached_tokens":5}}}}` + "\n\n"
}

func requireConversionFailure(t *testing.T, c *gin.Context, rec *httptest.ResponseRecorder, err error) {
	t.Helper()
	require.Error(t, err)
	var replay *UpstreamFailoverError
	require.False(t, errors.As(err, &replay), "a committed protocol failure must not replay")
	require.True(t, IsResponseCommitted(c))
	require.Contains(t, rec.Body.String(), `"error"`)
	require.NotContains(t, rec.Body.String(), "message_stop")
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
	require.NotContains(t, rec.Body.String(), `"finish_reason":"tool_calls"`)
}

func TestCompatConversionValidationPreservesUsageWithoutReplay(t *testing.T) {
	for _, anthropic := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"snapshot", "conflict", "truncated"} {
				t.Run(fmt.Sprintf("messages=%t/stream=%t/%s", anthropic, stream, mode), func(t *testing.T) {
					c, rec := conversionValidationContext()
					svc := &OpenAIGatewayService{cfg: &config.Config{}}
					resp := conversionValidationResponse(conversionValidationWire(mode))
					account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
					var result *OpenAIForwardResult
					var err error
					if anthropic && stream {
						result, err = svc.handleAnthropicStreamingResponse(resp, c, account, "example", "example", "example", time.Now())
					} else if anthropic {
						result, err = svc.handleAnthropicBufferedStreamingResponse(resp, c, account, "example", "example", "example", time.Now())
					} else if stream {
						result, err = svc.handleChatStreamingResponse(resp, c, account, "example", "example", "example", time.Now(), 0)
					} else {
						result, err = svc.handleChatBufferedStreamingResponse(resp, c, account, "example", "example", "example", time.Now())
					}
					requireConversionFailure(t, c, rec, err)
					require.NotNil(t, result)
					require.Equal(t, 19, result.Usage.InputTokens)
					require.Equal(t, 11, result.Usage.OutputTokens)
					require.Equal(t, 5, result.Usage.CacheReadInputTokens)
					require.False(t, result.ClientDisconnect)
					if !stream || mode == "snapshot" {
						require.Equal(t, http.StatusBadGateway, rec.Code)
					}
				})
			}
		}
	}
}

func TestNativeResponsesConversionValidationPreservesUsage(t *testing.T) {
	for _, mode := range []string{"snapshot", "conflict", "truncated"} {
		for _, path := range []string{"stream", "buffered", "passthrough"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				c, rec := conversionValidationContext()
				wire := conversionValidationWire(mode)
				resp := conversionValidationResponse(wire)
				svc := &OpenAIGatewayService{cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
				var usage *OpenAIUsage
				var err error
				switch path {
				case "stream":
					var result *openaiStreamingResult
					result, err = svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "example", "example")
					require.NotNil(t, result)
					usage = result.usage
				case "buffered":
					var result *openaiNonStreamingResult
					result, err = svc.handleSSEToJSON(resp, c, account, []byte(wire), "example", "example")
					require.NotNil(t, result)
					usage = result.usage
				case "passthrough":
					var result *openaiNonStreamingResultPassthrough
					result, err = svc.handlePassthroughSSEToJSON(resp, c, account, []byte(wire), "example", "example")
					require.NotNil(t, result)
					usage = result.usage
				}
				require.Error(t, err)
				var replay *UpstreamFailoverError
				require.False(t, errors.As(err, &replay))
				require.Equal(t, 19, usage.InputTokens)
				require.Equal(t, 11, usage.OutputTokens)
				require.Contains(t, rec.Body.String(), `"error"`)
				require.NotContains(t, rec.Body.String(), `"type":"response.completed"`)
				if path != "stream" || mode == "snapshot" {
					require.Equal(t, 502, rec.Code)
				}
			})
		}
	}
}

func TestRawChatMessagesValidationPreservesUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			c, rec := conversionValidationContext()
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			resp := conversionValidationResponse("data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"inspect","arguments":"{\"record\":"}}]}}]}` + "\n\n" + "data: " + `{"choices":[],"usage":{"prompt_tokens":19,"completion_tokens":11}}` + "\n\ndata: [DONE]\n\n")
			var result *OpenAIForwardResult
			var err error
			if stream {
				result, err = svc.streamChatCompletionsAsAnthropic(c, resp, "example", "example", "example", nil, nil, time.Now())
			} else {
				resp.Header.Set("Content-Type", "application/json")
				resp.Body = io.NopCloser(strings.NewReader(`{"choices":[{"message":{"tool_calls":[{"id":"c","function":{"name":"inspect","arguments":"[]"}}]}}],"usage":{"prompt_tokens":19,"completion_tokens":11}}`))
				result, err = svc.bufferChatCompletionsAsAnthropic(c, resp, "example", "example", "example", nil, nil, time.Now())
			}
			requireConversionFailure(t, c, rec, err)
			require.NotNil(t, result)
			require.Equal(t, 19, result.Usage.InputTokens)
			require.Equal(t, 11, result.Usage.OutputTokens)
		})
	}
	t.Run("buffered Responses", func(t *testing.T) {
		c, rec := conversionValidationContext()
		resp := conversionValidationResponse(`{"choices":[{"message":{"tool_calls":[{"id":"c","function":{"name":"inspect","arguments":"[]"}}]}}],"usage":{"prompt_tokens":19,"completion_tokens":11}}`)
		resp.Header.Set("Content-Type", "application/json")
		result, err := (&OpenAIGatewayService{cfg: &config.Config{}}).bufferChatCompletionsAsResponses(c, resp, "example", nil, nil, false, nil, "example", "example", nil, nil, time.Now())
		requireConversionFailure(t, c, rec, err)
		require.NotNil(t, result)
		require.Equal(t, 19, result.Usage.InputTokens)
		require.Equal(t, 11, result.Usage.OutputTokens)
	})
}

func TestAnthropicChatValidationDrainsTerminalUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			c, rec := conversionValidationContext()
			wire := ""
			for _, event := range []map[string]any{
				{"type": "message_start", "message": map[string]any{"id": "m", "usage": map[string]int{"input_tokens": 19}}},
				{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "c", "name": "inspect", "input": map[string]any{}}},
				{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": `{"record":`}},
				{"type": "content_block_stop", "index": 0},
				{"type": "message_delta", "delta": map[string]string{"stop_reason": "tool_use"}, "usage": map[string]int{"output_tokens": 11}},
				{"type": "message_stop"},
			} {
				raw, _ := json.Marshal(event)
				wire += "event: " + event["type"].(string) + "\ndata: " + string(raw) + "\n\n"
			}
			var result *ForwardResult
			var err error
			if stream {
				result, err = (&GatewayService{}).handleCCStreamingFromAnthropic(conversionValidationResponse(wire), c, "example", "example", nil, time.Now(), true)
			} else {
				result, err = (&GatewayService{}).handleCCBufferedFromAnthropic(conversionValidationResponse(wire), c, "example", "example", nil, time.Now())
			}
			requireConversionFailure(t, c, rec, err)
			require.NotNil(t, result)
			require.Equal(t, 19, result.Usage.InputTokens)
			require.Equal(t, 11, result.Usage.OutputTokens)
			if !stream {
				c, rec := conversionValidationContext()
				result, err := (&GatewayService{}).handleResponsesBufferedStreamingResponse(conversionValidationResponse(wire), c, "example", "example", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				requireConversionFailure(t, c, rec, err)
				require.NotNil(t, result)
				require.Equal(t, 19, result.Usage.InputTokens)
				require.Equal(t, 11, result.Usage.OutputTokens)
			}
		})
	}
}

func TestBPSCompatibilityConversionValidationPreservesUsage(t *testing.T) {
	for _, anthropic := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("messages=%t/stream=%t", anthropic, stream), func(t *testing.T) {
				c, rec := conversionValidationContext()
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, conversionValidationWire("snapshot"))}
				svc := excelBPSGatewayTestService(upstream)
				account := excelAccount()
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","max_tokens":100,"messages":[{"role":"user","content":"inspect"}],"stream":%t}`, stream))
				result, err := svc.forwardOpenAIGrantCompatibility(c.Request.Context(), c, account, body, "", "gpt-6-astra", anthropic)
				requireConversionFailure(t, c, rec, err)
				require.NotNil(t, result)
				require.Equal(t, UsageAuthorizationModeBPS, result.AuthorizationMode)
				require.Equal(t, 19, result.Usage.InputTokens)
				require.Equal(t, 11, result.Usage.OutputTokens)
				require.False(t, result.ClientDisconnect)
				if !stream {
					require.Equal(t, 502, rec.Code)
				}
			})
		}
	}
}

func TestGeminiChatConversionValidationPreservesUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			c, rec := conversionValidationContext()
			body := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"inspect","args":[1]}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":19,"candidatesTokenCount":11}}`
			svc := &GeminiMessagesCompatService{}
			if stream {
				result, err := svc.handleChatCompletionsStreamingResponseFromGemini(c, conversionValidationResponse("data: "+body+"\n\n"), time.Now(), "gemini-test", false, false)
				requireConversionFailure(t, c, rec, err)
				require.NotNil(t, result)
				require.Equal(t, 19, result.usage.InputTokens)
				require.Equal(t, 11, result.usage.OutputTokens)
				require.False(t, result.clientDisconnect)
			} else {
				resp := conversionValidationResponse(body)
				resp.Header.Set("Content-Type", "application/json")
				usage, err := svc.handleChatCompletionsNonStreamingResponseFromGemini(c, resp, "gemini-test", false)
				require.Error(t, err)
				require.NotNil(t, usage)
				require.Equal(t, 19, usage.InputTokens)
				require.Equal(t, 11, usage.OutputTokens)
				require.Equal(t, 502, rec.Code)
			}
		})
	}
}

func TestAntigravityChatConversionValidationPreservesUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			c, rec := conversionValidationContext()
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, nil)
			body := `data: {"response":{"responseId":"r","candidates":[{"content":{"parts":[{"functionCall":{"id":"c","name":"inspect","args":[1]}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":19,"candidatesTokenCount":11}}}` + "\n\n"
			var result *antigravityStreamResult
			var err error
			if stream {
				result, err = svc.handleChatCompletionsStreamingFromAntigravity(c, conversionValidationResponse(body), time.Now(), "gemini-test", false)
			} else {
				result, err = svc.handleChatCompletionsNonStreamingFromAntigravity(c, conversionValidationResponse(body), time.Now(), "gemini-test")
			}
			requireConversionFailure(t, c, rec, err)
			require.NotNil(t, result)
			require.Equal(t, 19, result.usage.InputTokens)
			require.Equal(t, 11, result.usage.OutputTokens)
			require.False(t, result.clientDisconnect)
		})
	}
}
