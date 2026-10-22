//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type imagePolicyMemoryCache struct {
	GatewayCache
	mu       sync.Mutex
	progress map[string]string
	warning  map[string]bool
	err      error
}

func newImagePolicyMemoryCache() *imagePolicyMemoryCache {
	return &imagePolicyMemoryCache{progress: map[string]string{}, warning: map[string]bool{}}
}
func (c *imagePolicyMemoryCache) LoadBPSImageProgress(_ context.Context, k string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progress[k], c.err
}
func (c *imagePolicyMemoryCache) SaveBPSImageProgress(_ context.Context, k, old, next string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.progress[k] != old {
		return errors.New("image history changed concurrently")
	}
	c.progress[k] = next
	return c.err
}
func (c *imagePolicyMemoryCache) ClaimBPSImageWarning(_ context.Context, k string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	first := !c.warning[k]
	c.warning[k] = true
	return first, c.err
}
func (c *imagePolicyMemoryCache) ResetBPSImageWarning(_ context.Context, k string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.warning, k)
	return c.err
}
func imagePolicyRequest(t *testing.T, h, k int) []byte {
	t.Helper()
	_, pixels := nativeGatewayBody(t)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pixels)
	msg := func(n int) map[string]any {
		parts := []any{}
		for i := 0; i < n; i++ {
			parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
		}
		return map[string]any{"role": "user", "content": parts}
	}
	b, e := json.Marshal(map[string]any{"model": "test-model", "input": []any{msg(h), map[string]any{"role": "assistant", "content": "seen"}, msg(k)}})
	require.NoError(t, e)
	return b
}
func imagePolicyContext(path string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
	return c
}
func imagePolicySettings(policy string) ExcelBPSImageRelaySettings {
	return ExcelBPSImageRelaySettings{Enabled: true, Policy: policy, WarningRemaining: 8, CompactReserve: 3, Limits: basispoints.DefaultImageRelayLimits()}
}
func TestExcelBPSImagePolicyWarningBoundaries(t *testing.T) {
	ctx := context.Background()
	cache := newImagePolicyMemoryCache()
	s := &OpenAIGatewayService{cache: cache}
	settings := imagePolicySettings("warn")
	for _, n := range []int{11, 12, 17, 18, 20, 21} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			scope := fmt.Sprint(n)
			_, e := s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses"), imagePolicyRequest(t, 0, n), settings, scope, true)
			if n < 12 {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
				if n == 12 {
					require.Contains(t, e.Error(), "5 张")
					require.NotContains(t, e.Error(), "预留")
				}
			}
			_, e = s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses"), imagePolicyRequest(t, 0, n), settings, scope, true)
			if n <= 17 {
				require.NoError(t, e)
			} else {
				require.Error(t, e)
			}
			compact, e := s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses/compact"), imagePolicyRequest(t, 0, n), settings, scope, true)
			require.NoError(t, e)
			require.Equal(t, 20, compact.maxImages)
			compact.finish(ctx, true)
			if n == 12 {
				_, e = s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses"), imagePolicyRequest(t, 0, n), settings, scope, true)
				require.Error(t, e)
			}
		})
	}
}
func TestExcelBPSImagePolicyAutomaticPartitions(t *testing.T) {
	s := &OpenAIGatewayService{cache: newImagePolicyMemoryCache()}
	settings := imagePolicySettings("auto_compact")
	for _, tt := range []struct {
		h, k, split int
		code        string
	}{{19, 1, 0, ""}, {19, 2, 2, ""}, {0, 21, 0, "basispoints_image_batch_too_large"}, {21, 1, 0, "basispoints_image_history_limit"}} {
		t.Run(fmt.Sprintf("%d/%d", tt.h, tt.k), func(t *testing.T) {
			p, e := s.prepareExcelImagePolicy(context.Background(), imagePolicyContext("/v1/responses"), imagePolicyRequest(t, tt.h, tt.k), settings, "scope", true)
			if tt.code != "" {
				var pe *excelImagePolicyError
				require.ErrorAs(t, e, &pe)
				require.Equal(t, tt.code, pe.Code)
			} else {
				require.NoError(t, e)
				require.Equal(t, tt.split, p.split)
			}
		})
	}
}
func TestExcelBPSImagePolicyStateFailureAndIsolation(t *testing.T) {
	cache := newImagePolicyMemoryCache()
	s := &OpenAIGatewayService{cache: cache}
	settings := imagePolicySettings("warn")
	ctx := context.Background()
	body := imagePolicyRequest(t, 0, 12)
	for _, scope := range []string{"account1/key1/thread1", "account1/key2/thread1", "account2/key1/thread1", "account1/key1/thread2"} {
		_, e := s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses"), body, settings, scope, true)
		require.Error(t, e)
	}
	cache.err = errors.New("offline")
	_, e := s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses"), body, settings, "scope", true)
	var pe *excelImagePolicyError
	require.ErrorAs(t, e, &pe)
	require.Equal(t, 503, pe.Status)
	settings.Policy = "off"
	_, e = s.prepareExcelImagePolicy(ctx, imagePolicyContext("/v1/responses"), body, settings, "", false)
	require.NoError(t, e)
}
func TestExcelBPSImagePolicySettings(t *testing.T) {
	// GetAllSettings publishes process-wide Grok defaults; restore them so this
	// settings test cannot change unrelated account-routing tests.
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })
	ctx := context.Background()
	repo := &excelBPSImageSettingsRepo{}
	svc := NewSettingService(repo, &config.Config{})
	saved, e := svc.GetAllSettings(ctx)
	require.NoError(t, e)
	require.Equal(t, DefaultExcelBPSImageLimitPolicy, saved.ExcelBPSImageLimitPolicy)
	require.Equal(t, 8, saved.ExcelBPSImageWarningRemaining)
	saved.ExcelBPSImageLimitPolicy = "warn"
	require.NoError(t, svc.UpdateSettings(ctx, saved))
	runtime, e := svc.GetExcelBPSImageRelaySettings(ctx)
	require.NoError(t, e)
	require.Equal(t, "warn", runtime.Policy)
	saved.ExcelBPSImageCompactReserve = 8
	require.Error(t, svc.UpdateSettings(ctx, saved))
	require.Equal(t, "3", repo.values[SettingKeyExcelBPSImageCompactReserve])
	saved.ExcelBPSImageCompactReserve = 3
	saved.ExcelBPSImageLimitPolicy = "bad"
	require.Error(t, svc.UpdateSettings(ctx, saved))
}

func TestExcelBPSImagePolicyForwardCompactAndContinue(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	for _, mode := range []string{"native", "relay"} {
		for _, stream := range []bool{false, true} {
			for _, failure := range []string{"", "compact_reject", "compact_failed", "compact_eof", "generation_reject", "generation_budget", "invalid_compaction", "ownership_http", "ownership_stream", "ownership_event_header", "cyber_http", "cyber_stream"} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", mode, stream, failure), func(t *testing.T) {
					svc := excelBPSGatewayTestService(nil)
					t.Cleanup(func() { require.NoError(t, svc.CloseExcelBPSImages()) })
					svc.cache = newImagePolicyMemoryCache()
					svc.cfg.Gateway.LogUpstreamErrorBody = true
					if failure == "generation_budget" {
						svc.cfg.Gateway.OpenAITurnMaxAttempts = 1
					}
					repo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: "true", SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMode: mode, SettingKeyExcelBPSImageBaseURL: "https://images.example", SettingKeyExcelBPSImageLimitPolicy: "auto_compact"}}
					svc.settingService = NewSettingService(repo, svc.cfg)
					raw := imagePolicyRequest(t, 19, 2)
					raw, e := sjson.SetBytes(raw, "stream", stream)
					require.NoError(t, e)
					raw, e = sjson.SetBytes(raw, "model", "gpt-6-astra")
					require.NoError(t, e)
					raw, e = sjson.SetBytes(raw, "tools", []any{map[string]any{"type": "function", "name": "inspect_image", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}})
					require.NoError(t, e)
					var phase, uploads int
					var compactBody *nativeAttachmentBody
					svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
						b, e := io.ReadAll(req.Body)
						require.NoError(t, e)
						if req.URL.String() == basispoints.AttachmentsURL {
							uploads++
							return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-policy\"}"))}, nil
						}
						phase++
						response := map[string]any{"id": "resp-policy", "status": "completed", "model": "gpt-6-astra", "output": []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "continued"}}}}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
						if phase == 1 {
							require.Contains(t, string(b), "compaction_trigger")
							require.Contains(t, gjson.GetBytes(b, "input.0.content.0.text").String(), "Do not call Excel, Office, workbook or connector tools.")
							count := 0
							for _, v := range gjson.GetBytes(b, "input").Array() {
								for _, part := range v.Get("content").Array() {
									if part.Get("type").String() == "input_image" {
										count++
									}
								}
							}
							require.Equal(t, 19, count, "new images must not enter compaction")
							if failure == "compact_reject" {
								return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
							}
							if failure == "ownership_http" {
								return bpsCompletionResponse(500, `{"error":{"type":"server_error","message":"Unknown error while validating file ownership."}}`), nil
							}
							if failure == "cyber_http" {
								return bpsCompletionResponse(403, "{\"error\":{\"code\":\"cyber_policy\",\"message\":\"policy rejected test-token\"}}"), nil
							}
							response["output"] = []any{map[string]any{"type": "compaction", "id": "cmp-policy", "encrypted_content": "opaque-policy-state"}}
							if failure == "invalid_compaction" {
								response["output"] = []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "not a compact"}}}}
							}
							response["usage"] = map[string]any{"input_tokens": 7, "output_tokens": 3, "total_tokens": 10}
						} else {
							require.True(t, compactBody.closed, "release upstream body before continuing")
							require.NotContains(t, string(b), "compaction_trigger")
							require.Contains(t, string(b), "opaque-policy-state")
							count := 0
							for _, v := range gjson.GetBytes(b, "input").Array() {
								for _, part := range v.Get("content").Array() {
									if part.Get("type").String() == "input_image" {
										count++
									}
								}
							}
							require.Equal(t, 2, count, "preserve all new images")
							if failure == "generation_reject" {
								return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
							}
						}
						kind := "response.completed"
						if phase == 1 && (failure == "compact_failed" || failure == "ownership_stream" || failure == "ownership_event_header" || failure == "cyber_stream") {
							kind = "response.failed"
							response["status"] = "failed"
							if failure == "cyber_stream" {
								response["error"] = map[string]any{"code": "cyber_policy", "message": "policy rejected test-token"}
							}
							if strings.HasPrefix(failure, "ownership_") {
								response["error"] = map[string]any{"type": "server_error", "message": "Unknown error while validating file ownership."}
							}
						}
						event, _ := json.Marshal(map[string]any{"type": kind, "response": response})
						wire := "data: " + string(event) + "\n\n"
						if phase == 1 && failure == "ownership_event_header" {
							payload, marshalErr := json.Marshal(map[string]any{"response": response})
							require.NoError(t, marshalErr)
							wire = "event: response.failed\ndata: " + string(payload) + "\n\n"
						}
						if phase == 1 && failure == "compact_eof" {
							wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-policy\"}}\n\n"
						}
						body := &nativeAttachmentBody{Reader: strings.NewReader(wire)}
						if phase == 1 {
							compactBody = body
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
					}}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(raw))
					c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
					c.Request.Header.Set("session_id", "policy-session")
					result, err := svc.forwardExcelBPS(context.Background(), c, excelAccount(), raw, time.Now())
					if failure != "" {
						require.Error(t, err)
						require.NotContains(t, rec.Body.String(), "continued")
						if strings.HasPrefix(failure, "cyber_") {
							require.Equal(t, 1, phase, "cyber rejected compaction must not start generation")
							mark := GetOpsCyberPolicy(c)
							require.NotNil(t, mark)
							require.NotContains(t, mark.Body, "test-token")
							require.Contains(t, rec.Body.String(), "cyber_policy")
							if failure == "cyber_stream" {
								require.Equal(t, 200, mark.UpstreamStatus)
								require.Equal(t, 7, mark.UpstreamInTok)
								require.Equal(t, 3, mark.UpstreamOutTok)
							} else {
								require.Equal(t, 403, mark.UpstreamStatus)
							}
						}
						if failure == "generation_budget" {
							require.Equal(t, 1, phase)
							require.ErrorIs(t, err, ErrOpenAITurnAttemptBudgetExhausted)
							require.NotEmpty(t, result.RequestID)
						}
						if failure == "generation_budget" || failure == "generation_reject" || failure == "compact_failed" || failure == "invalid_compaction" || failure == "ownership_stream" || failure == "ownership_event_header" {
							require.NotNil(t, result)
							require.Equal(t, 7, result.Usage.InputTokens)
							require.Equal(t, 3, result.Usage.OutputTokens)
						}
						if strings.HasPrefix(failure, "ownership_") {
							require.Equal(t, 1, phase, "failed compaction must not start generation")
							require.Contains(t, rec.Body.String(), "basispoints_file_ownership_error")
							value, ok := c.Get(OpsUpstreamErrorsKey)
							require.True(t, ok)
							attempts := value.([]*OpsUpstreamErrorEvent)
							require.Len(t, attempts, 1)
							diagnostic := gjson.Get(attempts[0].Detail, "file_ownership_context")
							require.Equal(t, "image_compaction", diagnostic.Get("stage").String())
							want := basispoints.FileReferenceCounts{}
							if mode == "native" {
								want.FreshUpload = 19
								want.FreshUploadFiles = 1
							} else {
								want.Remote = 19
							}
							var counts basispoints.FileReferenceCounts
							require.NoError(t, json.Unmarshal([]byte(diagnostic.Raw), &counts))
							require.Equal(t, want, counts, "diagnose only the compaction request, not all 21 original images")
							var failover *UpstreamFailoverError
							require.NotErrorAs(t, err, &failover)
							if mode == "native" {
								// A subsequent explicit request can upload fresh attachments.
								phase, failure = 0, ""
								next := imagePolicyContext("/v1/responses")
								next.Request.Header.Set("session_id", "policy-session")
								_, err = svc.forwardExcelBPS(t.Context(), next, excelAccount(), raw, time.Now())
								require.NoError(t, err)
								require.Equal(t, 2, uploads)
							}
						}
						return
					}
					require.NoError(t, err)
					require.Equal(t, 2, phase)
					require.Equal(t, 17, result.Usage.InputTokens)
					require.Equal(t, 5, result.Usage.OutputTokens)
					require.Contains(t, rec.Body.String(), "continued")
					require.Contains(t, rec.Body.String(), "opaque-policy-state")
					// Echo actual downstream output with the entire stale client history,
					// then verify a second turn reaches BPS without another compaction.
					var output []any
					if stream {
						for _, line := range strings.Split(rec.Body.String(), "\n") {
							if strings.HasPrefix(line, "data: ") {
								event := []byte(strings.TrimPrefix(line, "data: "))
								if gjson.GetBytes(event, "type").String() == "response.completed" {
									require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(event, "response.output").Raw), &output))
								}
							}
						}
					} else {
						require.NoError(t, json.Unmarshal([]byte(gjson.Get(rec.Body.String(), "output").Raw), &output))
					}
					require.NotEmpty(t, output)
					var original map[string]any
					require.NoError(t, json.Unmarshal(raw, &original))
					originalInput, ok := original["input"].([]any)
					require.True(t, ok)
					input := append(originalInput, output...)
					input = append(input, map[string]any{"role": "user", "content": "Continue using the two new images."})
					original["input"] = input
					nextRaw, e := json.Marshal(original)
					require.NoError(t, e)
					nextRec := httptest.NewRecorder()
					nextCtx, _ := gin.CreateTestContext(nextRec)
					nextCtx.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(nextRaw))
					nextCtx.Request.Header.Set("User-Agent", "codex_app/1.0")
					nextCtx.Request.Header.Set("session_id", "policy-session")
					nextResult, e := svc.forwardExcelBPS(context.Background(), nextCtx, excelAccount(), nextRaw, time.Now())
					require.NoError(t, e)
					require.Equal(t, 3, phase)
					require.Equal(t, 10, nextResult.Usage.InputTokens)
					require.NotContains(t, nextRec.Body.String(), "opaque-policy-state")

				})
			}
		}
	}

}

func TestExcelBPSImagePolicyCodexClients(t *testing.T) {
	for _, ua := range []string{"codex_cli_rs/1.0", "codex_app/1.0", "codex_chatgpt_desktop/1.0", "Codex Desktop/1.0"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Request.Header.Set("User-Agent", ua)
		require.True(t, isSupportedImagePolicyCodex(c), ua)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "other-client/1.0")
	require.False(t, isSupportedImagePolicyCodex(c))
}

func TestExcelBPSImagePolicyCompactSurvivesStateFailure(t *testing.T) {
	cache := newImagePolicyMemoryCache()
	cache.err = errors.New("offline")
	svc := &OpenAIGatewayService{cache: cache}
	settings := imagePolicySettings("warn")
	body := imagePolicyRequest(t, 0, 20)
	p, e := svc.prepareExcelImagePolicy(context.Background(), imagePolicyContext("/v1/responses/compact"), body, settings, "scope", true)
	require.NoError(t, e)
	require.True(t, p.compact)
	require.Equal(t, 20, p.maxImages)
	body, e = sjson.SetBytes(body, "input.-1", map[string]any{"type": "compaction_trigger"})
	require.NoError(t, e)
	p, e = svc.prepareExcelImagePolicy(context.Background(), imagePolicyContext("/v1/responses"), body, settings, "scope", true)
	require.NoError(t, e)
	require.True(t, p.compact)
}

func (c *imagePolicyMemoryCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func TestExcelBPSImagePolicyGatewayReconcile(t *testing.T) {
	svc := &OpenAIGatewayService{cache: newImagePolicyMemoryCache()}
	settings := imagePolicySettings("auto_compact")
	firstBody := imagePolicyRequest(t, 19, 2)
	state, e := svc.prepareExcelImagePolicy(context.Background(), imagePolicyContext("/v1/responses"), firstBody, settings, "reconcile", true)
	require.NoError(t, e)
	window := []any{map[string]any{"type": "compaction", "encrypted_content": "opaque-state"}}
	require.NoError(t, state.checkpoint(context.Background(), window))
	state.finish(context.Background(), true)
	var body map[string]any
	require.NoError(t, json.Unmarshal(firstBody, &body))
	items, ok := body["input"].([]any)
	require.True(t, ok)
	body["input"] = append(items, window[0], map[string]any{"role": "assistant", "content": "continued"}, map[string]any{"role": "user", "content": "continue"})
	raw, e := json.Marshal(body)
	require.NoError(t, e)
	next, e := svc.prepareExcelImagePolicy(context.Background(), imagePolicyContext("/v1/responses"), raw, settings, "reconcile", true)
	require.NoError(t, e)
	require.NotEmpty(t, next.reconciledBody)
	require.Equal(t, 2, next.history.Count)
	require.Equal(t, 0, next.split)
	require.Equal(t, items[2], next.history.Input[1])
	// A different thread has no authenticated checkpoint and cannot remove history.
	other, e := svc.prepareExcelImagePolicy(context.Background(), imagePolicyContext("/v1/responses"), raw, settings, "other-thread", true)
	require.Error(t, e)
	require.Nil(t, other)
}

func TestExcelBPS429NativeFallbackRestoresOriginalImages(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	for _, stage := range []string{"attachment", "compact_http", "compact_stream", "generation_after_compact", "attachment_after_compact"} {
		t.Run(stage, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			t.Cleanup(func() { require.NoError(t, svc.CloseExcelBPSImages()) })
			svc.cache = newImagePolicyMemoryCache()
			svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{values: map[string]string{
				SettingKeyExcelBPSEnabled: "true", SettingKeyExcelBPSImageRelayEnabled: "true",
				SettingKeyExcelBPSImageMode: "native", SettingKeyExcelBPSImageLimitPolicy: "auto_compact",
			}}, svc.cfg)
			account := excelBPSNativeFallbackTestAccount(t, svc)
			raw, _ := nativeGatewayBody(t)
			if stage != "attachment" {
				raw = imagePolicyRequest(t, 19, 2)
			}
			if stage == "attachment_after_compact" {
				_, pixels := nativeGatewayBody(t)
				// A distinct new image must be uploaded after compact, not deduplicated.
				url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(append(pixels, 1))
				raw, _ = sjson.SetBytes(raw, "input.2.content.0.image_url", url)
			}
			raw, err := sjson.SetBytes(raw, "model", "gpt-6-astra")
			require.NoError(t, err)
			uploads, generations, nativeCalls := 0, 0, 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
				b, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, account.ID, id)
				if req.URL.Host == "chatgpt.com" {
					nativeCalls++
					require.Equal(t, "Bearer native-codex-token", req.Header.Get("Authorization"))
					require.Contains(t, string(b), "data:image/png;base64,")
					require.NotContains(t, string(b), "file-fallback-image")
					require.NotContains(t, string(b), "bps-compact-state")
					reply := bpsCompletionResponse(200, bpsCompletedStream("resp_native_images", "images accepted"))
					reply.Header.Set("Content-Type", "text/event-stream")
					return reply, nil
				}
				if req.URL.String() == basispoints.AttachmentsURL {
					uploads++
					if stage == "attachment" || (stage == "attachment_after_compact" && uploads == 2) {
						return bpsCompletionResponse(429, `{}`), nil
					}
					return bpsCompletionResponse(200, `{"openai_file_id":"file-fallback-image"}`), nil
				}
				generations++
				if stage == "compact_http" || generations == 2 {
					return bpsCompletionResponse(429, `{}`), nil
				}
				if stage == "compact_stream" {
					return bpsCompletionResponse(200, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_compact_fail\",\"status\":\"failed\",\"error\":{\"code\":\"rate_limit_exceeded\",\"type\":\"rate_limit_error\"},\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"), nil
				}
				return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_compact\",\"status\":\"completed\",\"output\":[{\"type\":\"compaction\",\"id\":\"cmp_original\",\"encrypted_content\":\"bps-compact-state\"}],\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"), nil
			}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(raw))
			c.Request.Header.Set("session_id", "native-fallback-images")
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
			result, err := svc.Forward(t.Context(), c, account, raw)
			require.NoError(t, err)
			wantUploads := 1
			if stage == "attachment_after_compact" {
				wantUploads = 2
			}
			require.Equal(t, wantUploads, uploads)
			require.Equal(t, 1, nativeCalls)
			require.Contains(t, rec.Body.String(), "images accepted")
			wantInput, wantOutput := 10, 2
			if stage == "compact_stream" || stage == "generation_after_compact" || stage == "attachment_after_compact" {
				wantInput, wantOutput = 17, 5
			}
			require.Equal(t, wantInput, result.Usage.InputTokens)
			require.Equal(t, wantOutput, result.Usage.OutputTokens)
			require.True(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
		})
	}
}

// Use the same validated PNG header as the native attachment byte-limit tests.
func imagePolicyByteRequest(t *testing.T, historyMiB, newMiB int) []byte {
	t.Helper()
	_, pngBytes := nativeGatewayBody(t)
	item := func(sizeMiB int, marker byte) map[string]any {
		size := sizeMiB << 20
		if size == 0 {
			size = len(pngBytes)
		}
		pixels := make([]byte, size)
		copy(pixels, pngBytes)
		if size > len(pngBytes) {
			pixels[len(pixels)-1] = marker
		}
		return map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pixels)}}}
	}
	raw, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{item(historyMiB, 1), map[string]any{"role": "assistant", "content": "history seen"}, item(newMiB, 2)}})
	require.NoError(t, err)
	return raw
}

func TestExcelBPSImagePolicyByteBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, policy   string
		oldMiB, newMiB int
		trusted        bool
		wantSplit      int
		wantCode       string
	}{
		{"exact boundary", "auto_compact", 16, 16, true, 0, ""},
		{"bytes before count", "auto_compact", 17, 17, true, 2, ""},
		{"warning", "warn", 17, 17, true, 0, "basispoints_image_bytes_limit_reached"},
		{"untrusted history", "auto_compact", 17, 17, false, 0, "basispoints_image_policy_state_unavailable"},
		{"disabled policy", "off", 17, 17, true, 0, "basispoints_image_auto_compact_disabled"},
		{"disabled without identity", "off", 17, 17, false, 0, "basispoints_image_auto_compact_disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cache: newImagePolicyMemoryCache()}
			settings := imagePolicySettings(tc.policy)
			settings.Mode = ExcelBPSImageModeNative
			state, err := svc.prepareExcelImagePolicy(t.Context(), imagePolicyContext("/v1/responses"), imagePolicyByteRequest(t, tc.oldMiB, tc.newMiB), settings, "byte-policy", tc.trusted)
			if tc.wantCode != "" {
				var pe *excelImagePolicyError
				require.ErrorAs(t, err, &pe)
				require.Equal(t, tc.wantCode, pe.Code)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantSplit, state.split)
		})
	}
	t.Run("each partition remains bounded", func(t *testing.T) {
		for _, newBatch := range []bool{false, true} {
			raw := imagePolicyByteRequest(t, 17, 17)
			var source map[string]any
			require.NoError(t, json.Unmarshal(raw, &source))
			input := source["input"].([]any)
			index, code := 0, "basispoints_image_history_limit"
			if newBatch {
				index, code = 2, "basispoints_image_batch_too_large"
			}
			entry := input[index].(map[string]any)
			parts := entry["content"].([]any)
			entry["content"] = append(parts, parts[0])
			raw, err := json.Marshal(source)
			require.NoError(t, err)
			svc := &OpenAIGatewayService{cache: newImagePolicyMemoryCache()}
			_, err = svc.prepareExcelImagePolicy(t.Context(), imagePolicyContext("/v1/responses"), raw, imagePolicySettings("auto_compact"), "bounded", true)
			var pe *excelImagePolicyError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, code, pe.Code)
		}
	})
	t.Run("manual compact remains bounded without session state", func(t *testing.T) {
		body := imagePolicyByteRequest(t, 17, 17)
		for _, policy := range []string{"off", "warn", "auto_compact"} {
			svc := &OpenAIGatewayService{}
			_, err := svc.prepareExcelImagePolicy(t.Context(), imagePolicyContext("/v1/responses/compact"), body, imagePolicySettings(policy), "", false)
			var pe *excelImagePolicyError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, "basispoints_image_compaction_too_large", pe.Code)
			require.EqualValues(t, 34<<20, pe.diagnostics["decoded_image_bytes"])
		}
	})
}

// Exercise the disabled-policy error through the real forwarders without Redis
// or trusted identity. It must never send an attachment or billable compact.
func TestExcelBPSImageBytePolicyDisabledDiagnostics(t *testing.T) {
	raw := imagePolicyByteRequest(t, 17, 17)
	for _, transport := range []string{"sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{values: map[string]string{
				SettingKeyExcelBPSEnabled: "true", SettingKeyExcelBPSImageRelayEnabled: "true",
				SettingKeyExcelBPSImageLimitPolicy: "off",
			}}, svc.cfg)
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				t.Fatal("disabled policy must not upload or compact")
				return nil, nil
			}}
			body, err := sjson.SetBytes(raw, "stream", true)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
			MarkExcelBPSImageAdmission(c)
			var frames [][]byte
			if transport == "ws" {
				_, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(frame []byte) error { frames = append(frames, append([]byte(nil), frame...)); return nil })
			} else {
				_, err = svc.forwardExcelBPS(t.Context(), c, excelAccount(), body, time.Now())
			}
			require.Error(t, err)
			response := rec.Body.String() + string(bytes.Join(frames, nil)) + err.Error()
			require.Contains(t, response, "basispoints_image_auto_compact_disabled")
			events, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			event := events.([]*OpsUpstreamErrorEvent)[0]
			require.Equal(t, "image_preflight", event.Stage)
			require.Zero(t, event.UpstreamStatusCode, "local denial is not an upstream response")
			require.Empty(t, event.UpstreamURL)
			require.Equal(t, "off", gjson.Get(event.Detail, "image_policy").String())
			require.EqualValues(t, 34<<20, gjson.Get(event.Detail, "decoded_image_bytes").Int())
			require.EqualValues(t, 32<<20, gjson.Get(event.Detail, "native_request_limit_bytes").Int())
			require.False(t, gjson.Get(event.Detail, "compaction_attempted").Bool())
			require.False(t, gjson.Get(event.Detail, "generation_started").Bool())
			require.NotContains(t, event.Detail, "base64")
			require.Contains(t, event.Message, "excel_bps_image_limit_policy=off")
			require.Contains(t, event.Message, "auto_compact")
		})
	}
}

func TestExcelBPSImageByteCompactionFlow(t *testing.T) {
	for _, tc := range []struct{ transport, failure string }{
		{"json", ""}, {"sse", ""}, {"ws", ""}, {"ws_tool", ""}, {"ws_bound", ""},
		{"json", "compact_rejected"}, {"json", "tail_rate_limited"}, {"ws", "tail_rate_limited"}, {"json", "retained_images"}, {"json", "invalid_tail"},
	} {
		t.Run(tc.transport+"/"+tc.failure, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			svc.cache = newImagePolicyMemoryCache()
			svc.cfg.Gateway.LogUpstreamErrorBody = true
			svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{values: map[string]string{
				SettingKeyExcelBPSEnabled: "true", SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMode: "native",
			}}, svc.cfg)
			raw := imagePolicyByteRequest(t, 17, 17)
			if tc.failure == "retained_images" {
				// Keep the returned SSE event below its independent 16 MiB line limit.
				raw = imagePolicyByteRequest(t, 11, 11)
			}
			var original map[string]any
			require.NoError(t, json.Unmarshal(raw, &original))
			original["stream"] = tc.transport != "json"
			if tc.transport == "ws_bound" {
				original["input"] = append([]any{map[string]any{"type": "compaction", "id": "cmp-bound", "encrypted_content": "owner-bound-state"}}, original["input"].([]any)...)
				svc.cfg.Gateway.OpenAITurnMaxAttempts = 3 // compact, one rejected-upload retry, generation
			}
			if tc.transport == "ws_tool" {
				input := original["input"].([]any)
				tail := input[2].(map[string]any)["content"]
				original["input"] = append(input[:2],
					map[string]any{"type": "function_call", "call_id": "call-byte", "name": "inspect_image", "arguments": "{}"},
					map[string]any{"type": "function_call_output", "call_id": "call-byte", "output": tail})
				original["tools"] = []any{map[string]any{"type": "function", "name": "inspect_image", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}
			}
			if tc.failure == "retained_images" {
				tail := original["input"].([]any)[2].(map[string]any)
				parts := tail["content"].([]any)
				tail["content"] = append(parts, parts[0])
			}
			if tc.failure == "invalid_tail" {
				tail := original["input"].([]any)[2].(map[string]any)
				tail["content"] = []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64,bm90LWEtcG5n"}}
				// Force a count-triggered split while the malformed tail remains below the byte limit.
				svc.settingService.settingRepo.(*excelBPSImageSettingsRepo).values[SettingKeyExcelBPSImageMaxImages] = "1"
			}
			raw, err := json.Marshal(original)
			require.NoError(t, err)
			uploads, compactions, generations := 0, 0, 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				require.NoError(t, req.Context().Err())
				if req.URL.String() == basispoints.AttachmentsURL {
					_, readErr := io.Copy(io.Discard, req.Body)
					require.NoError(t, readErr)
					uploads++
					if uploads == 1 {
						require.Zero(t, compactions)
					}
					if uploads == 2 {
						require.Equal(t, 1, compactions, "new images upload only after successful history compaction")
						require.Zero(t, generations)
						if tc.transport == "ws_bound" {
							reply := bpsCompletionResponse(429, "{}")
							reply.Header.Set("Retry-After", "1")
							return reply, nil
						}
						if tc.failure == "tail_rate_limited" {
							return bpsCompletionResponse(429, "{}"), nil
						}
					}
					return bpsCompletionResponse(200, fmt.Sprintf("{\"openai_file_id\":\"file-byte-%d\"}", uploads)), nil
				}
				b, readErr := io.ReadAll(req.Body)
				require.NoError(t, readErr)
				require.NotContains(t, string(b), "file-preflight")
				if strings.Contains(string(b), "compaction_trigger") {
					compactions++
					require.Equal(t, 1, uploads, "the tail must not be uploaded before compaction")
					require.NotContains(t, string(b), "file-byte-2")
					if tc.failure == "compact_rejected" {
						return bpsCompletionResponse(400, "{}"), nil
					}
					window := []any{map[string]any{"type": "compaction", "id": "cmp-byte", "encrypted_content": "opaque-byte-state"}}
					if tc.failure == "retained_images" {
						retained := original["input"].([]any)[0].(map[string]any)
						retained["type"] = "message"
						window = append(window, retained)
					}
					event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp-compact-byte", "status": "completed", "output": window, "usage": map[string]any{"input_tokens": 7, "output_tokens": 3}}})
					return bpsCompletionResponse(200, "data: "+string(event)+"\n\n"), nil
				}
				generations++
				require.Equal(t, 1, compactions)
				require.Contains(t, string(b), "opaque-byte-state")
				require.NotContains(t, string(b), "file-byte-1")
				if tc.transport == "ws_tool" {
					require.Contains(t, string(b), "data:image/png;base64,")
					require.Contains(t, string(b), "call-byte")
				}
				return bpsCompletionResponse(200, bpsCompletedStream("resp-byte", "continued")), nil
			}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg, "low")
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw)).WithContext(ctx)
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
			c.Request.Header.Set("session_id", "byte-compaction-session")
			MarkExcelBPSImageAdmission(c)
			c.Set(OpenAIAccountReplayAllowedContextKey, OpenAIResponsesAccountReplayAllowed(raw))
			var result *OpenAIForwardResult
			var frames [][]byte
			if strings.HasPrefix(tc.transport, "ws") {
				result, err = svc.proxyOpenAIWSExcelBPSTurn(ctx, c, excelAccount(), raw, "gpt-6-astra", func(frame []byte) error { frames = append(frames, append([]byte(nil), frame...)); return nil })
			} else {
				result, err = svc.forwardExcelBPS(ctx, c, excelAccount(), raw, time.Now())
			}
			if tc.failure != "" {
				require.Error(t, err)
				var failover *UpstreamFailoverError
				if tc.failure == "tail_rate_limited" {
					require.ErrorAs(t, err, &failover)
					require.False(t, failover.RequestNotSent, "compact has already been submitted")
				} else {
					require.NotErrorAs(t, err, &failover)
				}
				require.Zero(t, generations)
				if tc.failure == "invalid_tail" {
					require.Zero(t, uploads)
					require.Zero(t, compactions)
					return
				}
				require.Equal(t, 1, compactions)
				if tc.failure == "compact_rejected" {
					require.Equal(t, 1, uploads)
					return
				}
				require.NotNil(t, result)
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 3, result.Usage.OutputTokens)
				if tc.failure == "retained_images" {
					require.Equal(t, 1, uploads)
					require.Contains(t, rec.Body.String(), "basispoints_image_compaction_insufficient")
					return
				}
				require.Equal(t, 2, uploads, "one rejected tail upload, without replay")
				events, _ := c.Get(OpsUpstreamErrorsKey)
				diagnostic := events.([]*OpsUpstreamErrorEvent)
				require.True(t, gjson.Get(diagnostic[len(diagnostic)-1].Detail, "compaction_completed").Bool())
				require.Equal(t, "portable_context", gjson.Get(diagnostic[len(diagnostic)-1].Detail, "attachment_retry_reason").String())
				return
			}
			require.NoError(t, err)
			wantUploads := 2
			if tc.transport == "ws_tool" {
				wantUploads = 1 // Tool screenshots remain inline and retain their complete call/result pair.
			}
			if tc.transport == "ws_bound" {
				wantUploads = 3 // Retry the rejected new image only; history upload and compact run once.
				budget, ok := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
				require.True(t, ok)
				require.Equal(t, 2, budget.attempts, "compact and generation each consume one attempt")
				require.Equal(t, 1, budget.preparationRetries)
				require.False(t, c.GetBool(OpenAIAccountReplayAllowedContextKey))
			}
			require.Equal(t, wantUploads, uploads)
			require.Equal(t, 1, generations)
			require.Equal(t, 17, result.Usage.InputTokens)
			require.Equal(t, 5, result.Usage.OutputTokens)
			if strings.HasPrefix(tc.transport, "ws") {
				require.True(t, result.OpenAIWSMode)
				require.Contains(t, string(bytes.Join(frames, nil)), "opaque-byte-state")
				for _, frame := range frames {
					require.True(t, json.Valid(frame))
				}
			}
			if tc.transport == "json" {
				var output []any
				require.NoError(t, json.Unmarshal([]byte(gjson.Get(rec.Body.String(), "output").Raw), &output))
				original["input"] = append(append(original["input"].([]any), output...), map[string]any{"role": "user", "content": "continue"})
				nextRaw, e := json.Marshal(original)
				require.NoError(t, e)
				next := imagePolicyContext("/v1/responses")
				next.Request.Header.Set("session_id", "byte-compaction-session")
				MarkExcelBPSImageAdmission(next)
				_, e = svc.forwardExcelBPS(t.Context(), next, excelAccount(), nextRaw, time.Now())
				require.NoError(t, e, "authenticated old history over 32 MiB must reconcile before active-window validation")
				require.Equal(t, 1, compactions)
				require.Equal(t, 2, generations)
				require.Equal(t, 3, uploads, "each client request still owns fresh uploads")
			}
		})
	}
}
