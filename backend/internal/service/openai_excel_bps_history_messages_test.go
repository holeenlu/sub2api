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

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSHistoryMessageAttributionForward(t *testing.T) {
	for _, kind := range []string{"message", "agent_message", "function_call_output"} {
		for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", kind, path, stream), func(t *testing.T) {
					wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_attribution\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}}
					svc := excelBPSGatewayTestService(upstream)
					source := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": []any{map[string]any{"type": kind, "role": "user", "author": "/root/worker", "recipient": "/root", "content": []any{map[string]any{"type": "input_text", "text": "task result\nnext line"}}}}}
					if kind == "function_call_output" {
						item := source["input"].([]any)[0].(map[string]any)
						item["name"], item["namespace"], item["output"] = "send_message_to_thread", "codex_app", item["content"]
						delete(item, "content")
					}
					body, err := json.Marshal(source)
					require.NoError(t, err)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
					result, err := svc.Forward(context.Background(), c, excelAccount(), body)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, http.StatusOK, rec.Code)
					items := gjson.GetBytes(upstream.lastBody, "input").Array()
					require.NotEmpty(t, items)
					for _, item := range items {
						require.False(t, item.Get("author").Exists())
						require.False(t, item.Get("recipient").Exists())
						require.NotEqual(t, "agent_message", item.Get("type").String())
					}
					messageIndex := len(items) - 1
					if path == "/v1/responses/compact" {
						require.Equal(t, "compaction_trigger", items[messageIndex].Get("type").String())
						messageIndex--
					}
					require.GreaterOrEqual(t, messageIndex, 0)
					last := items[messageIndex]
					require.Equal(t, "message", last.Get("type").String())
					require.Equal(t, "user", last.Get("role").String())
					require.Contains(t, last.Get("content.0.text").String(), "/root/worker")
					require.Equal(t, "task result\nnext line", last.Get("content.1.text").String())
				})
			}
		}
	}
}

func TestExcelBPSNamedStandaloneInputAcrossTransports(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		for _, variant := range []string{"text", "image", "orphan", "encrypted"} {
			t.Run(transport+"/"+variant, func(t *testing.T) {
				item := map[string]any{"type": "function_call_output", "namespace": "codex_app", "name": "send_message_to_thread", "output": "exact external task\nsecond line"}
				switch variant {
				case "image":
					fixture, _ := nativeGatewayBody(t)
					var source map[string]any
					require.NoError(t, json.Unmarshal(fixture, &source))
					image := source["input"].([]any)[0].(map[string]any)["content"].([]any)[0]
					item["output"] = []any{map[string]any{"type": "input_text", "text": "inspect this external task image"}, image}
				case "orphan":
					item["call_id"] = "call_without_original"
				case "encrypted":
					item["output"] = []any{map[string]any{"type": "input_text", "text": "header"}, map[string]any{"type": "encrypted_content", "data": "PRIVATE_CIPHERTEXT"}}
				}
				input := []any{}
				for index := 0; index < 5; index++ {
					input = append(input, map[string]any{"type": "message", "role": "user", "content": fmt.Sprintf("context %d", index)})
				}
				body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": transport != "json", "input": append(input, item)})
				require.NoError(t, err)
				svc := excelBPSGatewayTestService(nil)
				enableNativeAttachments(svc)
				account := excelAccount()
				account.Extra[ExcelBPSIgnoreEncryptedContentKey] = true
				uploads, generations := 0, 0
				svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
					require.Equal(t, account.ID, accountID)
					require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
					if req.URL.String() == basispoints.AttachmentsURL {
						uploads++
						require.Equal(t, "image", variant)
						return bpsCompletionResponse(200, "{\"openai_file_id\":\"file-external-input\"}"), nil
					}
					generations++
					require.Equal(t, basispoints.ResponsesURL, req.URL.String())
					raw, readErr := io.ReadAll(req.Body)
					require.NoError(t, readErr)
					require.NotContains(t, string(raw), "PRIVATE_CIPHERTEXT")
					items := gjson.GetBytes(raw, "input").Array()
					for _, value := range items {
						require.NotEqual(t, "function_call", value.Get("type").String())
						require.NotEqual(t, "function_call_output", value.Get("type").String())
					}
					last := items[len(items)-1]
					require.Equal(t, "message", last.Get("type").String())
					require.Equal(t, "user", last.Get("role").String())
					require.Contains(t, last.Get("content.0.text").String(), "codex_app")
					require.Contains(t, last.Get("content.0.text").String(), "no prior tool call is asserted")
					if variant == "image" {
						require.Equal(t, "inspect this external task image", last.Get("content.1.text").String())
						require.Equal(t, "file-external-input", last.Get("content.2.file_id").String())
						require.NotContains(t, string(raw), "data:image")
					} else {
						require.Equal(t, item["output"], last.Get("content.1.text").String())
					}
					require.Equal(t, "1", gjson.GetBytes(raw, "metadata.agent_iteration").String())
					return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_external\",\"status\":\"completed\",\"output\":[]}}\n\n"), nil
				}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				var output strings.Builder
				if transport == "ws" {
					c.Request.Method = http.MethodGet
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					_, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func(event []byte) error { output.Write(event); return nil })
				} else {
					_, err = svc.Forward(t.Context(), c, account, body)
					output.WriteString(rec.Body.String())
				}
				if variant == "orphan" || variant == "encrypted" {
					require.Error(t, err)
					require.Zero(t, uploads)
					require.Zero(t, generations)
					require.Contains(t, output.String(), "basispoints_request_invalid")
					require.Contains(t, output.String(), "input[5]")
					require.NotContains(t, output.String(), "PRIVATE_CIPHERTEXT")
					if transport != "ws" {
						require.Equal(t, 400, rec.Code)
					}
				} else {
					require.NoError(t, err)
					require.Equal(t, 1, generations)
					expectedUploads := 0
					if variant == "image" {
						expectedUploads = 1
					}
					require.Equal(t, expectedUploads, uploads)
					require.Contains(t, output.String(), "resp_external")
				}
			})
		}
	}
}
