package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nativeAttachmentUpstream struct {
	HTTPUpstream
	do func(*http.Request, string, int64, int) (*http.Response, error)
}

func (u *nativeAttachmentUpstream) Do(r *http.Request, p string, id int64, n int) (*http.Response, error) {
	return u.do(r, p, id, n)
}

type nativeAttachmentBody struct {
	io.Reader
	closed bool
}

func (b *nativeAttachmentBody) Close() error { b.closed = true; return nil }

func nativeGatewayBody(t *testing.T) ([]byte, []byte) {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 3))))
	part := map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes()), "detail": "original"}
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{map[string]any{"role": "user", "content": []any{part, part}}}})
	require.NoError(t, err)
	return body, data.Bytes()
}
func enableNativeAttachments(s *OpenAIGatewayService) {
	s.settingService = NewSettingService(&excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: "true", SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMode: "native"}}, s.cfg)
}

func TestExcelBPSNativeAttachmentForward(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", path, stream), func(t *testing.T) {
				body, pixels := nativeGatewayBody(t)
				body = bytes.Replace(body, []byte("{\"input\""), []byte(fmt.Sprintf("{\"stream\":%t,\"input\"", stream)), 1)
				uploaded := &nativeAttachmentBody{Reader: strings.NewReader("{\"openai_file_id\":\"file-native123\"}")}
				calls := 0
				svc := excelBPSGatewayTestService(nil)
				enableNativeAttachments(svc)
				account := excelAccount()
				account.Concurrency = 1
				account.Proxy = &Proxy{Protocol: "http", Host: "proxy.example", Port: 8080}
				svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
					calls++
					require.Equal(t, account.ID, id)
					require.Equal(t, 1, concurrency)
					require.Equal(t, account.Proxy.URL(), proxy)
					require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
					require.Equal(t, "test-account", req.Header.Get("Chatgpt-Account-Id"))
					require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
					require.Empty(t, req.Header.Get("Cookie"))
					require.Empty(t, req.Header.Get("x-codex-turn-state"))
					if calls == 1 {
						require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
						require.Equal(t, "identity", req.Header.Get("Accept-Encoding"))
						deadline, ok := req.Context().Deadline()
						require.True(t, ok)
						require.LessOrEqual(t, time.Until(deadline), 60*time.Second)
						raw, err := io.ReadAll(req.Body)
						require.NoError(t, err)
						require.Equal(t, int64(len(raw)), req.ContentLength)
						req.Body = io.NopCloser(bytes.NewReader(raw))
						mr, err := req.MultipartReader()
						require.NoError(t, err)
						part, err := mr.NextPart()
						require.NoError(t, err)
						require.Equal(t, "file", part.FormName())
						data, err := io.ReadAll(part)
						require.NoError(t, err)
						require.Equal(t, pixels, data)
						_, err = mr.NextPart()
						require.ErrorIs(t, err, io.EOF)
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: uploaded}, nil
					}
					require.True(t, uploaded.closed, "upload slot must be released before Responses at concurrency=1")
					require.Equal(t, basispoints.ResponsesURL, req.URL.String())
					raw, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.NotContains(t, string(raw), "data:image")
					found := 0
					for _, item := range gjson.GetBytes(raw, "input").Array() {
						for _, part := range item.Get("content").Array() {
							if part.Get("type").String() == "input_image" {
								found++
								require.Equal(t, "file-native123", part.Get("file_id").String())
								require.Len(t, part.Map(), 2, "native file references only accept type and file_id")
								require.False(t, part.Get("image_url").Exists())
							}
						}
					}
					require.Equal(t, 2, found)
					wire := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}, nil
				}}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				c.Request.Header.Set("Cookie", "private-cookie")
				c.Request.Header.Set("x-codex-turn-state", "private-state")
				result, err := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 2, calls)
			})
		}
	}
}

func TestExcelBPSNativeUploadFailureStopsWithoutQuotaWrite(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		body      string
		transport bool
		want      int
	}{
		{"429", 429, "PRIVATE_BODY", false, 429}, {"403", 403, "PRIVATE_BODY", false, 403}, {"500", 500, "PRIVATE_BODY", false, 500},
		{"redirect", 307, "PRIVATE_BODY", false, 502}, {"wrong ID", 200, "{\"openai_file_id\":\"PRIVATE_BODY\"}", false, 502},
		{"invalid JSON", 200, "PRIVATE_BODY", false, 502}, {"oversized JSON", 200, strings.Repeat("X", (64<<10)+1), false, 502},
		{"transport", 0, "", true, 502},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
			svc.accountRepo = repo
			svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
			svc.rateLimitService.SetAccountRuntimeBlocker(svc)
			calls := 0
			closed := &nativeAttachmentBody{Reader: strings.NewReader(tc.body)}
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
				if tc.transport {
					return nil, errors.New("PRIVATE_TOKEN")
				}
				return &http.Response{StatusCode: tc.status, Header: excelBPSQuotaHeaders("100", "100"), Body: closed}, nil
			}}
			body, _ := nativeGatewayBody(t)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			account := excelAccount()
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			if tc.status == http.StatusTooManyRequests {
				// Nothing was generated, so another account may take the request.
				requireExcelBPSRateLimitFailover(t, err, c)
			} else {
				require.Equal(t, tc.want, rec.Code)
				require.True(t, IsResponseCommitted(c))
			}
			require.Equal(t, tc.status == http.StatusTooManyRequests, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
			require.Equal(t, 1, calls)
			require.NotContains(t, rec.Body.String(), "PRIVATE")
			require.NotContains(t, err.Error(), "PRIVATE")
			requireNoExcelBPSQuotaWrite(t, repo)
			require.True(t, account.IsSchedulable())
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			if !tc.transport {
				require.True(t, closed.closed)
			}
		})
	}
}

func TestExcelBPSNativePreflightRejectsBeforeNetwork(t *testing.T) {
	body, _ := nativeGatewayBody(t)
	for name, change := range map[string]func(map[string]any){
		"invalid second image": func(v map[string]any) {
			parts := nativeGatewayParts(t, v)
			part, ok := parts[1].(map[string]any)
			require.True(t, ok)
			part["image_url"] = "data:image/png;base64,PRIVATE"
		},
		"invalid tool choice": func(v map[string]any) { v["tool_choice"] = "required" },
		"previous response":   func(v map[string]any) { v["previous_response_id"] = "resp_unsupported" },
		"ambiguous reference": func(v map[string]any) {
			parts := nativeGatewayParts(t, v)
			part, ok := parts[0].(map[string]any)
			require.True(t, ok)
			part["file_id"] = "file-mixed"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var source map[string]any
			require.NoError(t, json.Unmarshal(body, &source))
			change(source)
			raw, err := json.Marshal(source)
			require.NoError(t, err)
			upstream := &httpUpstreamRecorder{}
			svc := excelBPSGatewayTestService(upstream)
			enableNativeAttachments(svc)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
			_, err = svc.Forward(context.Background(), c, excelAccount(), raw)
			require.Error(t, err)
			require.Equal(t, 400, rec.Code)
			require.Empty(t, upstream.requests)
			require.NotContains(t, rec.Body.String(), "PRIVATE")
		})
	}
}

func TestExcelBPSNativeAttachmentIDRedaction(t *testing.T) {
	raw := "{\"error\":{\"message\":\"cannot read file-private123 from this account\"}}"
	out := excelBPSSanitizeErrorBody(raw, "test-token", excelAccount())
	require.NotContains(t, out, "private123")
	require.Contains(t, out, "file-[redacted]")
}

func TestExcelBPSFileOwnershipFailure(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, rejection := range []string{"http", "stream"} {
			for _, source := range []string{"inline", "repeated inline", "client file", "plaintext", "opaque history"} {
				t.Run(protocol+"/"+rejection+"/"+source, func(t *testing.T) {
					body, _ := nativeGatewayBody(t)
					var request map[string]any
					require.NoError(t, json.Unmarshal(body, &request))
					request["stream"] = protocol != "json"
					if source == "client file" {
						for _, value := range nativeGatewayParts(t, request) {
							part := value.(map[string]any)
							delete(part, "image_url")
							part["file_id"] = "file-clientPrivate"
						}
					} else if source == "plaintext" {
						request["input"] = "continue"
					} else if source == "opaque history" {
						request["input"] = append([]any{
							map[string]any{"type": "compaction", "encrypted_content": "PRIVATE_COMPACTION_STATE"},
							map[string]any{"type": "reasoning", "encrypted_content": "PRIVATE_REASONING_STATE"},
						}, request["input"].([]any)...)
					}
					body, err := json.Marshal(request)
					require.NoError(t, err)
					svc := excelBPSGatewayTestService(nil)
					enableNativeAttachments(svc)
					svc.cfg.Gateway.LogUpstreamErrorBody = true
					account := excelAccount()
					account.Proxy = &Proxy{Protocol: "http", Host: "ownership-proxy.example", Port: 8080}
					uploads, responses := 0, 0
					rejectionTurn := 1
					if source == "repeated inline" {
						rejectionTurn = 2 // follow a separate successful request with the same images
					}
					var forwardedIDs []string
					svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
						require.Equal(t, account.ID, id)
						require.Equal(t, account.Proxy.URL(), proxy)
						require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
						require.Equal(t, "test-account", req.Header.Get("Chatgpt-Account-Id"))
						require.Equal(t, "test-account", req.Header.Get("X-Openai-Account-Id"))
						if req.URL.String() == basispoints.AttachmentsURL {
							uploads++
							return bpsCompletionResponse(200, fmt.Sprintf(`{"openai_file_id":"file-upload%d"}`, uploads)), nil
						}
						responses++
						raw, readErr := io.ReadAll(req.Body)
						require.NoError(t, readErr)
						for _, item := range gjson.GetBytes(raw, "input").Array() {
							for _, part := range item.Get("content").Array() {
								if id := part.Get("file_id").String(); id != "" {
									forwardedIDs = append(forwardedIDs, id)
								}
							}
						}
						if responses == rejectionTurn {
							failure := `{"error":{"message":"Unknown error while validating file ownership.","type":"server_error"}}`
							if rejection == "http" {
								return bpsCompletionResponse(500, failure), nil
							}
							return bpsCompletionResponse(200, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_ownership\",\"status\":\"failed\",\"output\":[],\"usage\":{\"input_tokens\":7},"+failure[1:]+"}\n\n"), nil
						}
						return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_success\",\"status\":\"completed\",\"output\":[]}}\n\n"), nil
					}}
					var firstOutput string
					for turn := 0; turn < rejectionTurn+2; turn++ {
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
						c.Request.Header.Set("session_id", t.Name())
						var result *OpenAIForwardResult
						var events strings.Builder
						if protocol == "ws" {
							SetOpenAIClientTransport(c, OpenAIClientTransportWS)
							result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func(event []byte) error {
								events.Write(event)
								return nil
							})
						} else {
							result, err = svc.Forward(t.Context(), c, account, body)
						}
						staleCache := source == "repeated inline"
						if turn+1 == rejectionTurn {
							require.Error(t, err)
							var failover *UpstreamFailoverError
							if staleCache {
								// The rejected request reused a cached ID: invalidate it and
								// ask for one same-account retry with a fresh upload.
								require.ErrorAs(t, err, &failover)
								require.True(t, failover.RetryableOnSameAccount)
								require.EqualValues(t, "basispoints_attachment_cache_stale", failover.Reason)
							} else {
								require.NotErrorAs(t, err, &failover, "ownership rejection must not replay or switch accounts")
							}
							firstOutput = rec.Body.String() + events.String()
							value, ok := c.Get(OpsUpstreamErrorsKey)
							require.True(t, ok)
							attempts := value.([]*OpsUpstreamErrorEvent)
							require.Len(t, attempts, 1)
							diagnostic := gjson.Get(attempts[0].Detail, "file_ownership_context")
							require.Equal(t, "generation", diagnostic.Get("stage").String())
							want := basispoints.FileReferenceCounts{}
							removed := int64(0)
							switch source {
							case "inline":
								want.FreshUploadFiles = 1
								want.FreshUpload = 2
								removed = 1
							case "repeated inline":
								want.ReusedUpload = 2
								removed = 1
							case "client file":
								want.Unmanaged = 2
							case "opaque history":
								want.FreshUpload, want.FreshUploadFiles = 2, 1
								want.OpaqueCompactionItems, want.EncryptedReasoningItems = 1, 1
								removed = 1
							}
							var counts basispoints.FileReferenceCounts
							require.NoError(t, json.Unmarshal([]byte(diagnostic.Raw), &counts))
							require.Equal(t, want, counts)
							require.Equal(t, removed, diagnostic.Get("cache_entries_invalidated").Int())
							require.NotContains(t, attempts[0].UpstreamResponseBody, "file_ownership_context", "local context must not impersonate the upstream response")
							require.NotContains(t, firstOutput, "file_ownership_context")
							require.NotContains(t, attempts[0].Detail, "file-clientPrivate")
							require.NotContains(t, attempts[0].Detail, "file-upload")
							require.NotContains(t, attempts[0].Detail, "PRIVATE_")
							require.False(t, diagnostic.Get("compaction_completed").Bool(), "client history is distinct from compaction performed during this request")
							if rejection == "stream" {
								require.NotNil(t, result)
								require.Equal(t, 7, result.Usage.InputTokens)
								if staleCache {
									require.Empty(t, result.ResponseID, "an undelivered response ID is not reported before the retry")
								} else {
									require.Equal(t, "resp_ownership", result.ResponseID)
								}
							}
						} else {
							require.NoError(t, err)
						}
						if strings.HasSuffix(source, "inline") || source == "opaque history" {
							// Uploads happen on the first request and right after the
							// rejection invalidated the IDs; other turns reuse the cache.
							wantUploads := 1
							if turn >= rejectionTurn {
								wantUploads = 2
							}
							require.Equal(t, wantUploads, uploads, "accepted generation is never replayed; cached IDs are reused until rejected")
						} else {
							require.Zero(t, uploads)
						}
						require.Equal(t, turn+1, responses, "never regenerate within an accepted request")
						require.True(t, account.IsSchedulable())
						require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
					}
					if source == "repeated inline" {
						require.NotContains(t, firstOutput, "basispoints_file_ownership_error", "nothing is delivered before the retry")
					} else {
						require.Contains(t, firstOutput, "basispoints_file_ownership_error")
						require.Contains(t, firstOutput, "original image")
					}
					require.NotContains(t, firstOutput, "file-clientPrivate")
					if strings.HasSuffix(source, "inline") || source == "opaque history" {
						wantIDs := make([]string, 0, 2*(rejectionTurn+2))
						for turn := 0; turn < rejectionTurn+2; turn++ {
							id := "file-upload1"
							if turn >= rejectionTurn {
								id = "file-upload2"
							}
							wantIDs = append(wantIDs, id, id)
						}
						require.Equal(t, wantIDs, forwardedIDs)
					}
				})
			}
		}
	}
}

func nativeGatewayParts(t *testing.T, v map[string]any) []any {
	t.Helper()
	input, ok := v["input"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, input)
	message, ok := input[0].(map[string]any)
	require.True(t, ok)
	parts, ok := message["content"].([]any)
	require.True(t, ok)
	return parts
}

func TestExcelBPSAttachmentUsesResolvedSessionProxy(t *testing.T) {
	body, _ := nativeGatewayBody(t)
	images, err := basispoints.PrepareNativeImages(body)
	require.NoError(t, err)
	svc := excelBPSGatewayTestService(nil)
	account := excelAccount()
	account.Proxy = &Proxy{Protocol: "http", Host: "account-proxy.example", Port: 8080}
	const selected = "http://127.0.0.1:19007"
	calls := 0
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
		calls++
		require.Equal(t, selected, proxy)
		require.Equal(t, account.ID, id)
		require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-selected-proxy\"}"))}, nil
	}}
	_, err = images.Upload(context.Background(), new(basispoints.AttachmentUploader), func(ctx context.Context, img basispoints.InlineAttachment) (string, error) {
		return svc.uploadExcelBPSAttachment(ctx, account, "test-token", "test-account", selected, img)
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestExcelBPSNativeToolScreenshotsStayInline(t *testing.T) {
	imageBody, _ := nativeGatewayBody(t)
	dataURL := gjson.GetBytes(imageBody, "input.0.content.0.image_url").String()
	for _, kind := range []string{"function_call_output", "custom_tool_call_output"} {
		for _, mixed := range []bool{false, true} {
			for _, detail := range []string{"", "null", "low", "high", "original"} {
				t.Run(fmt.Sprintf("%s/mixed=%t/detail=%s", kind, mixed, detail), func(t *testing.T) {
					part := map[string]any{"type": "input_image", "image_url": dataURL}
					wantDetail := detail
					if detail == "null" {
						part["detail"] = nil
					} else if detail != "" {
						part["detail"] = detail
					}
					if detail == "" || detail == "null" {
						wantDetail = "auto"
					}
					input := []any{}
					if mixed {
						input = append(input, map[string]any{"role": "user", "content": []any{part}})
					}
					input = append(input, map[string]any{"type": "function_call", "name": "view_image", "call_id": "call_image", "arguments": "{}"}, map[string]any{"type": kind, "call_id": "call_image", "output": []any{part, map[string]any{"type": "input_text", "text": "screenshot context"}}})
					body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": input})
					require.NoError(t, err)
					svc := excelBPSGatewayTestService(nil)
					enableNativeAttachments(svc)
					uploads, responses := 0, 0
					svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
						if req.URL.String() == basispoints.AttachmentsURL {
							uploads++
							require.True(t, mixed, "tool-only requests must not upload")
							return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-user-image"}`))}, nil
						}
						responses++
						raw, err := io.ReadAll(req.Body)
						require.NoError(t, err)
						found := false
						for _, item := range gjson.GetBytes(raw, "input").Array() {
							if item.Get("type").String() == "function_call_output" {
								found = true
								require.Equal(t, dataURL, item.Get("output.0.image_url").String())
								require.False(t, item.Get("output.0.file_id").Exists())
								require.Equal(t, wantDetail, item.Get("output.0.detail").String())
								require.Equal(t, "screenshot context", item.Get("output.1.text").String())
							}
						}
						require.True(t, found)
						if mixed {
							require.Contains(t, string(raw), `"file_id":"file-user-image"`)
						}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_screenshot\",\"status\":\"completed\",\"output\":[]}}\n\n"))}, nil
					}}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					_, err = svc.Forward(context.Background(), c, excelAccount(), body)
					require.NoError(t, err)
					require.Equal(t, 1, responses)
					if mixed {
						require.Equal(t, 1, uploads)
					} else {
						require.Zero(t, uploads)
					}
				})
			}
		}
	}
}

func TestExcelBPSAttachmentsReusedAcrossTurns(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws", "chat-stream"} {
		t.Run(protocol, func(t *testing.T) {
			var parts []any
			for i := 0; i < 6; i++ {
				var img bytes.Buffer
				require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, i+2, 3))))
				url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())
				if protocol == "chat-stream" {
					parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
				} else {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
				}
			}
			// A duplicate inside the request still shares its fresh upload.
			parts = append(parts, parts[0])
			source := map[string]any{"model": "gpt-6-astra", "stream": protocol != "json", "input": []any{map[string]any{"role": "user", "content": parts}}}
			path := "/v1/responses"
			if protocol == "chat-stream" {
				path = "/v1/chat/completions"
				source["messages"] = source["input"]
				delete(source, "input")
			}
			body, err := json.Marshal(source)
			require.NoError(t, err)
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			var uploadCount atomic.Int64
			generations := 0
			var firstTurnIDs map[string]bool
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				if req.URL.String() == basispoints.AttachmentsURL {
					// Distinct images of one request upload in parallel.
					n := uploadCount.Add(1)
					return bpsCompletionResponse(200, fmt.Sprintf("{\"openai_file_id\":\"file-fresh%d\"}", n)), nil
				}
				generations++
				raw, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				current := map[string]bool{}
				for _, item := range gjson.GetBytes(raw, "input").Array() {
					for _, part := range item.Get("content").Array() {
						if id := part.Get("file_id").String(); id != "" {
							current[id] = true
						}
					}
				}
				require.Len(t, current, 6)
				if firstTurnIDs == nil {
					firstTurnIDs = current
				}
				// Later turns of the same conversation reuse the cached IDs.
				require.Equal(t, firstTurnIDs, current)
				return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fresh\",\"status\":\"completed\",\"output\":[]}}\n\n"), nil
			}}
			for turn := 0; turn < 3; turn++ {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				c.Request.Header.Set("session_id", t.Name())
				account := excelAccount()
				switch protocol {
				case "ws":
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					_, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func([]byte) error { return nil })
				case "chat-stream":
					_, err = svc.forwardOpenAIGrantCompatibility(t.Context(), c, account, body, "", "gpt-6-astra", false)
				default:
					_, err = svc.Forward(t.Context(), c, account, body)
				}
				require.NoError(t, err)
				require.Equal(t, int64(6), uploadCount.Load(), "history images upload once per conversation scope")
				require.Equal(t, turn+1, generations)
			}
		})
	}
}

func TestExcelBPSAttachment429RetainsSuccessfulImagesAndSharesRetry(t *testing.T) {
	body := excelBPSBoundAttachmentBody(t)
	var source map[string]any
	require.NoError(t, json.Unmarshal(body, &source))
	var parts []any
	var images [][]byte
	for i := 0; i < 3; i++ {
		var img bytes.Buffer
		require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, i+2, 3))))
		images = append(images, img.Bytes())
		parts = append(parts, map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())})
	}
	// A duplicate of the first image shares its upload.
	parts = append(parts[:2], parts[0], parts[2])
	source["input"].([]any)[1].(map[string]any)["content"] = parts
	body, err := json.Marshal(source)
	require.NoError(t, err)
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	var mu sync.Mutex
	perImage := map[int]int{}
	total := 0
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, basispoints.AttachmentsURL, req.URL.String(), "generation cannot start while any upload is rejected")
		reader, err := req.MultipartReader()
		require.NoError(t, err)
		part, err := reader.NextPart()
		require.NoError(t, err)
		raw, err := io.ReadAll(part)
		require.NoError(t, err)
		index := -1
		for i, img := range images {
			if bytes.Equal(img, raw) {
				index = i
			}
		}
		require.NotEqual(t, -1, index, "upload must send an original image")
		mu.Lock()
		perImage[index]++
		total++
		n := perImage[index]
		mu.Unlock()
		// The first image succeeds; the second succeeds only on its retry; the
		// third is always rejected.
		if index == 0 || (index == 1 && n > 1) {
			return bpsCompletionResponse(200, fmt.Sprintf("{\"openai_file_id\":\"file-upload-%d-%d\"}", index, n)), nil
		}
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"0"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	_, err = svc.Forward(t.Context(), c, excelAccount(), body)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, perImage[0], "a completed or duplicate image is never uploaded again")
	require.Len(t, perImage, 3, "every distinct image is attempted")
	require.LessOrEqual(t, total, 4, "one retry is shared by all attachments of the request")
	require.LessOrEqual(t, perImage[1]+perImage[2], 3)
}
