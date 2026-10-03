package service

// Adapted from ranxi2001/sub2api PR #242, commit
// 8cd0c6c99277e87cdd65b1df68f1263125646e65 (psyche314).
// Retains this project's explicit text-only fallback when image support is off.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise both HTTP boundaries with the real Forward/protocol/SSE pipeline:
// default/native/relay modes keep every image, and the account-level
// text-only fallback (see TestExcelBPSIgnoreImagesHTTPFlow) stays inert while
// image support is on.
func TestExcelBPSImagesHTTPFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DATA_DIR", t.TempDir())
	body, pixels := nativeGatewayBody(t)
	image := gjson.GetBytes(body, "input.0.content.0").Raw
	const tools = `[{"type":"function","name":"inspect","parameters":{"type":"object"}},{"type":"custom","name":"capture"}]`
	history := fmt.Sprintf(`[
		{"role":"user","content":[{"type":"input_text","text":"before screenshot"},%s,{"type":"input_text","text":"after screenshot"}]},
		{"type":"agent_message","author":"/root/worker","recipient":"/root","content":[%s]},
		{"type":"function_call","call_id":"call_function","name":"inspect","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_function","output":[{"type":"input_text","text":"function result"},%s]},
		{"type":"custom_tool_call","call_id":"call_custom","name":"capture","input":"capture text"},
		{"type":"custom_tool_call_output","call_id":"call_custom","output":[%s]},
		{"role":"user","content":[%s,{"type":"input_text","text":"continue"}]}
	]`, image, image, image, image, image)

	for _, mode := range []string{"", ExcelBPSImageModeNative, ExcelBPSImageModeRelay} {
		for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("mode=%s/%s/stream=%t", mode, path, stream), func(t *testing.T) {
					forwarded := make(chan []byte, 8)
					uploads := make(chan []byte, 8)
					attachmentURL, err := url.Parse(basispoints.AttachmentsURL)
					require.NoError(t, err)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == attachmentURL.Path {
							file, _, err := r.FormFile("file")
							if err != nil {
								http.Error(w, err.Error(), http.StatusBadRequest)
								return
							}
							defer func() { assert.NoError(t, r.MultipartForm.RemoveAll()) }()
							defer func() { assert.NoError(t, file.Close()) }()
							data, err := io.ReadAll(file)
							if err != nil {
								http.Error(w, err.Error(), http.StatusBadRequest)
								return
							}
							uploads <- data
							w.Header().Set("Content-Type", "application/json")
							_, _ = io.WriteString(w, `{"openai_file_id":"file-native-http"}`)
							return
						}
						data, err := io.ReadAll(r.Body)
						if err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						forwarded <- data
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_images\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[]}}\n\n")
					}))
					defer upstream.Close()
					target, err := url.Parse(upstream.URL)
					require.NoError(t, err)
					repo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSImageRelayEnabled: "false"}}
					if mode != "" {
						repo.values[SettingKeyExcelBPSImageMode] = mode
					}
					if mode == ExcelBPSImageModeRelay {
						repo.values[SettingKeyExcelBPSImageBaseURL] = "https://images.example"
					}
					svc := openAIClientToolsTestService(nil)
					svc.httpUpstream = &excelBPSIgnoreImagesHTTPUpstream{target: target, client: upstream.Client()}
					svc.settingService = NewSettingService(repo, svc.cfg)
					defer func() { require.NoError(t, svc.CloseExcelBPSImages()) }()
					router := gin.New()
					router.POST(path, func(c *gin.Context) {
						account := excelAccount()
						// The text-only fallback only applies while image support is off.
						account.Extra[ExcelBPSIgnoreImagesKey] = c.GetHeader("X-Test-Ignore-Images") == "true"
						body, err := c.GetRawData()
						if err != nil {
							c.AbortWithStatus(http.StatusBadRequest)
							return
						}
						_, _ = svc.Forward(c.Request.Context(), c, account, body)
					})
					gateway := httptest.NewServer(router)
					defer gateway.Close()
					send := func(input string, ignore bool, wantStatus int) string {
						t.Helper()
						body := fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"tools":%s,"input":%s}`, stream, tools, input)
						req, err := http.NewRequest(http.MethodPost, gateway.URL+path, bytes.NewBufferString(body))
						require.NoError(t, err)
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("session_id", t.Name())
						req.Header.Set("X-Test-Ignore-Images", fmt.Sprint(ignore))
						resp, err := gateway.Client().Do(req)
						require.NoError(t, err)
						defer func() { assert.NoError(t, resp.Body.Close()) }()
						result, err := io.ReadAll(resp.Body)
						require.NoError(t, err)
						require.Equal(t, wantStatus, resp.StatusCode, string(result))
						return string(result)
					}

					rejected := send(history, false, http.StatusBadRequest)
					require.Contains(t, rejected, "enable BPS image support")
					require.Empty(t, forwarded)
					require.Empty(t, uploads)
					https := `[{"role":"user","content":[{"type":"input_image","image_url":"https://images.example/photo.png"}]}]`
					send(https, false, http.StatusOK)
					require.Len(t, forwarded, 1)
					require.Contains(t, string(<-forwarded), "https://images.example/photo.png")

					// Unset configuration enables image support by default.
					repo.mu.Lock()
					delete(repo.values, SettingKeyExcelBPSImageRelayEnabled)
					repo.mu.Unlock()
					send(history, true, http.StatusOK)
					require.Len(t, forwarded, 1)
					wire := <-forwarded
					require.NotContains(t, string(wire), "Image input is unavailable")
					for _, text := range []string{"before screenshot", "after screenshot", "function result", "continue", "/root/worker"} {
						require.Contains(t, string(wire), text)
					}
					images := 0
					calls, results := map[string]bool{}, map[string]bool{}
					for _, item := range gjson.GetBytes(wire, "input").Array() {
						switch item.Get("type").String() {
						case "function_call":
							calls[item.Get("call_id").String()] = true
						case "function_call_output":
							results[item.Get("call_id").String()] = true
						}
						for _, field := range []string{"content", "output"} {
							for _, part := range item.Get(field).Array() {
								if part.Get("type").String() != "input_image" {
									continue
								}
								images++
								if mode == ExcelBPSImageModeRelay {
									require.Contains(t, part.Get("image_url").String(), "https://images.example/")
								} else if field == "output" {
									// BPS accepts validated tool screenshots inline; only
									// message images use native attachment uploads.
									require.JSONEq(t, image, part.Raw)
								} else {
									require.Equal(t, "file-native-http", part.Get("file_id").String())
									require.False(t, part.Get("image_url").Exists())
								}
							}
						}
					}
					require.Equal(t, 5, images)
					require.Equal(t, map[string]bool{"call_function": true, "call_custom": true}, calls)
					require.Equal(t, calls, results)
					if mode == ExcelBPSImageModeRelay {
						require.Empty(t, uploads)
					} else {
						require.Len(t, uploads, 1, "identical images share a native attachment")
						require.Equal(t, pixels, <-uploads)
					}
					require.NoError(t, repo.SetMultiple(context.Background(), map[string]string{SettingKeyExcelBPSImageRelayEnabled: "false"}))
					send(history, false, http.StatusBadRequest)
					require.Empty(t, forwarded)
					require.Empty(t, uploads)
				})
			}
		}
	}
}
