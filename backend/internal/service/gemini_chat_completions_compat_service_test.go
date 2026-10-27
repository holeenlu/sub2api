package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiResponseToChatCompletionsPreservesInlineData(t *testing.T) {
	tests := []struct {
		name  string
		parts []any
		want  string
	}{
		{
			name: "image only",
			parts: []any{
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "aW1hZ2U="}},
			},
			want: "![image](data:image/png;base64,aW1hZ2U=)",
		},
		{
			name: "text and image",
			parts: []any{
				map[string]any{"text": "rendered image:\n"},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/webp", "data": "d2VicA=="}},
			},
			want: "rendered image:\n![image](data:image/webp;base64,d2VicA==)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			geminiResp := map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"parts": tt.parts},
					"finishReason": "STOP",
				}},
			}
			rawData, err := json.Marshal(geminiResp)
			require.NoError(t, err)

			got, _, err := geminiResponseToChatCompletions(geminiResp, "gemini-test", rawData, nil)
			require.NoError(t, err)
			require.Len(t, got.Choices, 1)

			var content string
			require.NoError(t, json.Unmarshal(got.Choices[0].Message.Content, &content))
			require.Equal(t, tt.want, content)
			require.Equal(t, "stop", got.Choices[0].FinishReason)
		})
	}
}

func TestGeminiResponseToChatCompletionsOmitsInvalidInlineData(t *testing.T) {
	tests := []struct {
		name       string
		inlineData map[string]any
	}{
		{
			name:       "unsupported MIME type",
			inlineData: map[string]any{"mimeType": "image/svg+xml", "data": "PHN2Zz48L3N2Zz4="},
		},
		{
			name:       "malformed MIME type",
			inlineData: map[string]any{"mimeType": "image/png; charset=utf-8", "data": "aW1hZ2U="},
		},
		{
			name:       "malformed base64",
			inlineData: map[string]any{"mimeType": "image/png", "data": "not-valid-base64!!!"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			geminiResp := map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"parts": []any{map[string]any{"text": "before"}, map[string]any{"inlineData": tt.inlineData}, map[string]any{"text": "after"}}},
					"finishReason": "STOP",
				}},
			}
			rawData, err := json.Marshal(geminiResp)
			require.NoError(t, err)

			got, _, err := geminiResponseToChatCompletions(geminiResp, "gemini-test", rawData, nil)
			require.NoError(t, err)

			var content string
			require.NoError(t, json.Unmarshal(got.Choices[0].Message.Content, &content))
			require.Equal(t, "beforeafter", content)
		})
	}
}

func TestConvertGeminiToClaudeMessageOmitsInlineDataForAnthropicMessages(t *testing.T) {
	geminiResp := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{
				map[string]any{"text": "before"},
				map[string]any{"inlineData": map[string]any{"mimeType": "image/png", "data": "aW1hZ2U="}},
				map[string]any{"functionCall": map[string]any{"name": "get_weather", "args": map[string]any{"city": "Paris"}}},
				map[string]any{"text": "after"},
			}},
			"finishReason": "STOP",
		}},
	}
	rawData, err := json.Marshal(geminiResp)
	require.NoError(t, err)

	withInlineData, _ := convertGeminiToClaudeMessage(geminiResp, "gemini-test", rawData, true)
	contentWithInlineData, ok := withInlineData["content"].([]any)
	require.True(t, ok)
	require.Len(t, contentWithInlineData, 4)
	require.Equal(t, map[string]any{"type": "text", "text": "before"}, contentWithInlineData[0])
	require.Equal(t, map[string]any{"type": "text", "text": "![image](data:image/png;base64,aW1hZ2U=)"}, contentWithInlineData[1])
	toolUse, ok := contentWithInlineData[2].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tool_use", toolUse["type"])
	require.Equal(t, "get_weather", toolUse["name"])
	require.Equal(t, map[string]any{"type": "text", "text": "after"}, contentWithInlineData[3])

	withoutInlineData, _ := convertGeminiToClaudeMessage(geminiResp, "gemini-test", rawData, false)
	contentWithoutInlineData, ok := withoutInlineData["content"].([]any)
	require.True(t, ok)
	require.Len(t, contentWithoutInlineData, 3)
	require.Equal(t, map[string]any{"type": "text", "text": "before"}, contentWithoutInlineData[0])
	toolUseWithoutInlineData, ok := contentWithoutInlineData[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tool_use", toolUseWithoutInlineData["type"])
	require.Equal(t, "get_weather", toolUseWithoutInlineData["name"])
	require.Equal(t, map[string]any{"type": "text", "text": "after"}, contentWithoutInlineData[2])
}

func TestGeminiResponseToChatCompletionsRetainsTextAndToolBehavior(t *testing.T) {
	geminiResp := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{
				map[string]any{"text": "checking"},
				map[string]any{"functionCall": map[string]any{
					"name": "get_weather",
					"args": map[string]any{"city": "Paris"},
				}},
			}},
			"finishReason": "STOP",
		}},
	}
	rawData, err := json.Marshal(geminiResp)
	require.NoError(t, err)

	got, _, err := geminiResponseToChatCompletions(geminiResp, "gemini-test", rawData, nil)
	require.NoError(t, err)
	require.Len(t, got.Choices, 1)

	choice := got.Choices[0]
	var content string
	require.NoError(t, json.Unmarshal(choice.Message.Content, &content))
	require.Equal(t, "checking", content)
	require.Equal(t, "tool_calls", choice.FinishReason)
	require.Len(t, choice.Message.ToolCalls, 1)
	require.Equal(t, "get_weather", choice.Message.ToolCalls[0].Function.Name)
	require.JSONEq(t, `{"city":"Paris"}`, choice.Message.ToolCalls[0].Function.Arguments)
}

func TestGeminiStreamsRequireProtocolTerminal(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic", "gemini", "aggregate"} {
		for _, tc := range []struct {
			name, tail string
			ok         bool
		}{
			{"truncated", "", false},
			{"unterminated_usage", `data: {"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3}}`, false},
			{"truncated_usage", `data: {"usageMetadata":{"promptTokenCount":99,"candidatesTokenCount":`, false},
			{"unspecified_finish", "data: {\"candidates\":[{\"finishReason\":\"FINISH_REASON_UNSPECIFIED\"}]}\n\n", false},
			{"unterminated_terminal", `data: {"candidates":[{"finishReason":"STOP"}]}`, true}, {"done_is_not_terminal", "data: [DONE]\n\n", false},
			{"malformed", "data: {broken\n\n", false}, {"explicit_error", "data: {\"error\":{\"code\":500}}\n\n", false},
			{"stop", "data: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n", true},
			{"max_tokens", "data: {\"candidates\":[{\"finishReason\":\"MAX_TOKENS\"}]}\n\n", true},
			{"blocked", "data: {\"promptFeedback\":{\"blockReason\":\"SAFETY\"}}\n\n", true},
			{"error_before_terminal", "data: {\"error\":{\"code\":500}}\n\ndata: {\"candidates\":[{\"finishReason\":\"STOP\"}]}\n\n", false},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				input := `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3}}` + "\n\n" + tc.tail
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(input))}
				svc := &GeminiMessagesCompatService{}
				var usage *ClaudeUsage
				var err error
				switch protocol {
				case "openai":
					var result *geminiStreamResult
					result, err = svc.handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-test", false, true)
					require.NotNil(t, result)
					usage = result.usage
				case "anthropic":
					var result *geminiStreamResult
					result, err = svc.handleStreamingResponse(c, resp, time.Now(), "gemini-test")
					require.NotNil(t, result)
					usage = result.usage
				case "gemini":
					var result *geminiNativeStreamResult
					result, err = svc.handleNativeStreamingResponse(c, resp, time.Now(), false, &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey}, "")
					require.NotNil(t, result)
					usage = result.usage
				default:
					_, usage, err = collectGeminiSSE(resp.Body, false)
				}
				if tc.ok {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
					if protocol != "gemini" {
						require.NotContains(t, rec.Body.String(), "data: [DONE]\n\n")
					}
					require.NotContains(t, rec.Body.String(), `"type":"message_stop"`)
				}
				require.NotNil(t, usage)
				require.Equal(t, 7, usage.InputTokens)
				require.Equal(t, 3, usage.OutputTokens)
			})
		}
	}
}
func TestGeminiTerminalReaderCompletedThenCanceled(t *testing.T) {
	terminal := `data: {"candidates":[{"finishReason":"STOP"}]}` + "\n\n"
	for _, complete := range []bool{false, true} {
		prefix := ""
		if complete {
			prefix = terminal
		}
		r := newGeminiTerminalReader(io.MultiReader(strings.NewReader(prefix), geminiEndingReader{err: context.Canceled}), 0)
		_, err := io.ReadAll(r)
		if complete {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, context.Canceled)
		}
	}
}

type geminiEndingReader struct {
	err         error
	beforeError func()
}

func (r geminiEndingReader) Read([]byte) (int, error) {
	if r.beforeError != nil {
		r.beforeError()
	}
	return 0, r.err
}

type geminiFailedClientWriter struct {
	gin.ResponseWriter
	failOn string
}

func (w geminiFailedClientWriter) Write(p []byte) (int, error) {
	if w.failOn == "" || strings.Contains(string(p), w.failOn) {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseWriter.Write(p)
}
func (w geminiFailedClientWriter) WriteString(p string) (int, error) { return w.Write([]byte(p)) }

func TestGeminiChatCanceledWriteIsNotSuccessfulGeneration(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Writer = geminiFailedClientWriter{ResponseWriter: c.Writer}
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`data: {"candidates":[{"finishReason":"STOP"}]}` + "\n\n"))}
	result, err := (&GeminiMessagesCompatService{}).handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "gemini-test", false, false)
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, result)
	require.True(t, result.clientDisconnect)
	outcome, ok := GetOpsStreamError(c)
	require.True(t, ok)
	require.Equal(t, OpsClientCanceledCode, outcome.Code)
}

func TestGeminiMessagesThinkingWriteFailureKeepsUsage(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Writer = geminiFailedClientWriter{ResponseWriter: c.Writer, failOn: "thinking_delta"}
	wire := `data: {"candidates":[{"content":{"parts":[{"text":"thinking","thought":true}]}}],"usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":3}}` + "\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(wire))}
	result, err := (&GeminiMessagesCompatService{}).handleStreamingResponse(c, resp, time.Now(), "gemini-test")
	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, result)
	require.True(t, result.clientDisconnect)
	require.Equal(t, 7, result.usage.InputTokens)
	require.Equal(t, 3, result.usage.OutputTokens)
	outcome, ok := GetOpsStreamError(c)
	require.True(t, ok)
	require.Equal(t, OpsClientCanceledCode, outcome.Code)
}

func TestGeminiUsageObservationDistinguishesReportedZeroFromUnknown(t *testing.T) {
	for _, reported := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		payload := `data: {"candidates":[{"content":{"parts":[{"text":"partial"}]}}]}`
		if reported {
			payload = `data: {"usageMetadata":{"promptTokenCount":0,"candidatesTokenCount":0}}`
		}
		_, err := io.ReadAll(newGeminiTerminalReader(strings.NewReader(payload+"\n\n"), 0, c))
		require.Error(t, err)
		require.Equal(t, reported, geminiUsageWasReported(c))
	}
}

// Test-only adapters and reference algorithms; production uses the current entrypoints.
func collectGeminiSSE(body io.Reader, isOAuth bool) (map[string]any, *ClaudeUsage, error) {
	collected, usage, _, err := collectGeminiSSEObserved(body, isOAuth, nil)
	return collected, usage, err
}

func TestGeminiChatImageConfigSurvivesConversion(t *testing.T) {
	for _, tc := range []struct{ name, fields, want string }{
		{"omitted", ``, `{}`},
		{"top level", `,"imageConfig":{"imageSize":"4K","aspectRatio":"16:9"}`, `{"imageSize":"4K","aspectRatio":"16:9"}`},
		{"extra body overrides", `,"imageConfig":{"imageSize":"1K"},"extra_body":{"google":{"image_config":{"image_size":"4K","aspect_ratio":"16:9"}}}`, `{"imageSize":"4K","aspectRatio":"16:9"}`},
		{"empty extra keeps top level", `,"imageConfig":{"imageSize":"4K"},"extra_body":{"google":{"image_config":{}}}`, `{"imageSize":"4K"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"gemini-image","messages":[{"role":"user","content":"draw"}]` + tc.fields + `}`)
			gemini := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw"}]}],"generationConfig":{"temperature":0.7}}`)
			result, err := applyGeminiChatImageConfig(body, gemini)
			require.NoError(t, err)
			var out struct {
				Generation map[string]json.RawMessage `json:"generationConfig"`
			}
			require.NoError(t, json.Unmarshal(result, &out))
			require.JSONEq(t, `0.7`, string(out.Generation["temperature"]))
			if tc.want == `{}` {
				require.NotContains(t, out.Generation, "imageConfig")
			} else {
				require.JSONEq(t, tc.want, string(out.Generation["imageConfig"]))
			}
			require.Equal(t, `{"contents":[{"role":"user","parts":[{"text":"draw"}]}],"generationConfig":{"temperature":0.7}}`, string(gemini))
		})
	}
	_, err := applyGeminiChatImageConfig([]byte(`{"imageConfig":"invalid"}`), []byte(`{}`))
	require.Error(t, err)
}
