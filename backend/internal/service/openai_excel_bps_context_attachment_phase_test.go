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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClampExcelBPSCodexContext(t *testing.T) {
	for _, tc := range []struct {
		name, model, wantWindow, wantMax, wantAuto string
		changed                                    bool
	}{
		{name: "native astra window", model: `{"context_window":1050000,"max_context_window":1050000,"auto_compact_token_limit":null}`,
			wantWindow: "922000", wantMax: "922000", wantAuto: "830000", changed: true},
		{name: "missing threshold", model: `{"context_window":1050000}`, wantWindow: "922000", wantAuto: "830000", changed: true},
		{name: "higher threshold is lowered", model: `{"context_window":1050000,"auto_compact_token_limit":1000000}`,
			wantWindow: "922000", wantAuto: "830000", changed: true},
		{name: "lower explicit threshold kept", model: `{"context_window":1050000,"auto_compact_token_limit":500000}`,
			wantWindow: "922000", wantAuto: "500000", changed: true},
		{name: "only max window above limit", model: `{"context_window":272000,"max_context_window":1050000}`,
			wantWindow: "272000", wantMax: "922000", wantAuto: "244800", changed: true},
		{name: "small window unchanged", model: `{"context_window":272000,"max_context_window":272000,"auto_compact_token_limit":null}`,
			wantWindow: "272000", wantMax: "272000", wantAuto: "null"},
		{name: "missing window is not invented", model: `{"slug":"x"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var model map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(tc.model), &model))
			require.Equal(t, tc.changed, clampExcelBPSCodexContext(model))
			require.Equal(t, tc.wantWindow, string(model["context_window"]))
			require.Equal(t, tc.wantMax, string(model["max_context_window"]))
			require.Equal(t, tc.wantAuto, string(model["auto_compact_token_limit"]))
		})
	}
}

func TestExcelBPSConfiguredCatalogClampsContextWindowOnlyForBPSRoute(t *testing.T) {
	for _, bps := range []bool{true, false} {
		account := excelBPSManifestAccount()
		if !bps {
			account.Extra["openai_excel_bps"] = false
		}
		svc := &OpenAIGatewayService{
			accountRepo:    codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{88: {account}}},
			settingService: excelBPSSettingsTestService(),
		}
		manifest, configured, err := svc.BuildGroupConfiguredCodexModelsManifest(context.Background(), &Group{ID: 88, Platform: PlatformOpenAI}, "")
		require.NoError(t, err)
		require.True(t, configured)
		found := false
		for _, model := range decodeCodexManifestModels(t, manifest.Body) {
			if model["slug"] != "gpt-6-astra" {
				continue
			}
			found = true
			if bps {
				// BPS rejects input above ~922k; Codex must compact before that.
				require.EqualValues(t, excelBPSCodexContextWindow, model["context_window"])
				require.EqualValues(t, excelBPSCodexContextWindow, model["max_context_window"])
				require.EqualValues(t, excelBPSCodexAutoCompactTokenLimit, model["auto_compact_token_limit"])
			} else {
				require.EqualValues(t, configuredCodexGPT6AstraContext, model["context_window"])
				require.Nil(t, model["auto_compact_token_limit"])
			}
		}
		require.True(t, found)
	}
}

func TestExcelBPSRestrictManifestClampsFetchedCatalog(t *testing.T) {
	account := excelBPSManifestAccount()
	body := []byte(`{"models":[{"slug":"gpt-6-astra","context_window":1050000,"max_context_window":1050000,"auto_compact_token_limit":null,"multi_agent_version":null,"multi_agent_reasoning_effort":null},{"slug":"gpt-5.6-sol","context_window":1050000}]}`)
	restricted, changed, err := restrictExcelBPSCodexModelsManifest(body, []Account{account}, nil)
	require.NoError(t, err)
	require.True(t, changed, "a clamp alone must update the body and its ETag")
	models := decodeCodexManifestModels(t, restricted)
	require.EqualValues(t, excelBPSCodexContextWindow, models[0]["context_window"])
	require.EqualValues(t, excelBPSCodexAutoCompactTokenLimit, models[0]["auto_compact_token_limit"])
	require.EqualValues(t, 1_050_000, models[1]["context_window"], "a model not routed through BPS keeps its window")
}

func TestExcelBPSHTTP400ContextLengthKeepsCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, raw string }{
		{"code", `{"error":{"code":"context_length_exceeded","message":"Your input exceeds the context window of this model. Please adjust your input and try again.","param":"input","type":"invalid_request_error"}}`},
		{"context_too_large", `{"error":{"code":"context_too_large","message":"Request is too large for this model.","type":"invalid_request_error"}}`},
		{"message only", `{"error":{"message":"This model's maximum context length is 922000 tokens.","type":"invalid_request_error"}}`},
	} {
		for _, stream := range []bool{true, false} {
			t.Run(tc.name+"/"+map[bool]string{true: "stream", false: "json"}[stream], func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.raw))}}
				svc := excelBPSGatewayTestService(upstream)
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"input":"hello"}`, stream))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				account := excelAccount()
				_, err := svc.Forward(context.Background(), c, account, body)
				require.Error(t, err)
				if stream {
					// Codex only maps context_length_exceeded to its context-full state from
					// response.failed; an HTTP 400 would be shown as a plain error and the
					// next turn would never compact.
					require.Equal(t, http.StatusOK, rec.Code)
					require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
					out := rec.Body.String()
					require.True(t, strings.HasPrefix(out, "event: response.failed\ndata: "), out)
					payload := strings.TrimSuffix(strings.TrimPrefix(out, "event: response.failed\ndata: "), "\n\n")
					require.Equal(t, "context_length_exceeded", gjson.Get(payload, "response.error.code").String())
					require.Equal(t, "invalid_request_error", gjson.Get(payload, "response.error.type").String())
					require.Equal(t, "failed", gjson.Get(payload, "response.status").String())
					streamErr, recorded := GetOpsStreamError(c)
					require.True(t, recorded)
					require.Equal(t, http.StatusBadRequest, streamErr.IntendedStatus)
				} else {
					require.Equal(t, http.StatusBadRequest, rec.Code)
					require.Equal(t, "context_length_exceeded", gjson.Get(rec.Body.String(), "error.code").String())
					require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
				}
				require.Len(t, upstream.requests, 1, "a full context window is not replayed")
				var forwardErr *excelBPSForwardError
				require.ErrorAs(t, err, &forwardErr)
				require.True(t, forwardErr.RequestScoped(), "a full context window is not an account failure")
				require.Equal(t, StatusActive, account.Status)
				require.True(t, account.Schedulable)
				require.Nil(t, account.RateLimitResetAt)
				require.Nil(t, account.TempUnschedulableUntil)
			})
		}
	}
}

func TestOpenAIFirstOutputGuardPauseResume(t *testing.T) {
	t.Run("pause excludes the paused time", func(t *testing.T) {
		ctx, guard := newOpenAIFirstOutputHeaderGuard(context.Background(), func() {}, time.Now().Add(80*time.Millisecond))
		defer guard.close()
		require.True(t, guard.pause(time.Second))
		time.Sleep(150 * time.Millisecond)
		require.False(t, guard.expired(), "a paused guard must not fire")
		require.NoError(t, ctx.Err())
		guard.resume()
		time.Sleep(20 * time.Millisecond)
		require.False(t, guard.expired(), "the remaining budget is kept after resume")
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("resumed guard must still fire")
		}
		require.ErrorIs(t, context.Cause(ctx), ErrOpenAIWSFirstOutputBudgetExhausted)
	})
	t.Run("stop while paused does not block or time out", func(t *testing.T) {
		_, guard := newOpenAIFirstOutputHeaderGuard(context.Background(), func() {}, time.Now().Add(time.Hour))
		require.True(t, guard.pause(time.Second))
		done := make(chan bool, 1)
		go func() { done <- guard.stopHeaderWait() }()
		select {
		case timedOut := <-done:
			require.False(t, timedOut)
		case <-time.After(time.Second):
			t.Fatal("stopHeaderWait blocked on a paused guard")
		}
		guard.resume()
		require.False(t, guard.expired(), "a stopped guard stays stopped after resume")
		guard.close()
	})
	t.Run("expired guard cannot pause", func(t *testing.T) {
		_, guard := newOpenAIFirstOutputHeaderGuard(context.Background(), func() {}, time.Now().Add(time.Millisecond))
		defer guard.close()
		time.Sleep(20 * time.Millisecond)
		require.False(t, guard.pause(time.Second))
		var nilGuard *openAIFirstOutputHeaderGuard
		require.False(t, nilGuard.pause(time.Second))
		nilGuard.resume()
	})
}

func TestOpenAITurnFirstOutputPauseSurvivesAccountSwitch(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(context.Background(), nil, "low")
	budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	timeout := 300 * time.Millisecond
	start := budget.firstOutput.Snapshot().Started
	original := openAITurnFirstOutputDeadline(ctx, timeout, start)
	resume := pauseOpenAITurnFirstOutput(ctx, time.Second)
	budget.mu.Lock()
	require.True(t, budget.firstOutput.Snapshot().Deadline.After(original.Add(900*time.Millisecond)), "waits inside the phase see the extended deadline")
	budget.mu.Unlock()
	time.Sleep(120 * time.Millisecond)
	resume()
	resume() // idempotent
	budget.mu.Lock()
	paused := budget.firstOutput.Snapshot().Paused
	extended := budget.firstOutput.Snapshot().Deadline
	budget.mu.Unlock()
	require.GreaterOrEqual(t, paused, 120*time.Millisecond)
	require.Less(t, paused, time.Second)
	require.Equal(t, original.Add(paused), extended)
	// A later attempt (for example on another account) derives the same deadline.
	require.Equal(t, extended, openAITurnFirstOutputDeadline(ctx, timeout, start))
	require.NoError(t, TakeOpenAITurnAttempt(ctx))
}

func TestExcelBPSAttachmentPhaseDoesNotConsumeFirstOutputBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	var generations atomic.Int64
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.String() == basispoints.AttachmentsURL {
			// Slower than the whole first-output budget, within the phase limit.
			time.Sleep(1300 * time.Millisecond)
			return bpsCompletionResponse(200, `{"openai_file_id":"file-slow"}`), nil
		}
		generations.Add(1)
		return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_slow\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"), nil
	}}
	body, _ := nativeGatewayBody(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	_, err := svc.Forward(context.Background(), c, excelAccount(), body)
	require.NoError(t, err, rec.Body.String())
	require.Equal(t, int64(1), generations.Load())
	require.NotContains(t, rec.Body.String(), "first_output_timeout")
}

func TestExcelBPSAttachmentPhaseTimeoutReportsAttachmentTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := excelBPSAttachmentPhaseTimeout
	excelBPSAttachmentPhaseTimeout = 150 * time.Millisecond
	t.Cleanup(func() { excelBPSAttachmentPhaseTimeout = previous })
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	var generations atomic.Int64
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.String() != basispoints.AttachmentsURL {
			generations.Add(1)
			return nil, context.Canceled
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	body, _ := nativeGatewayBody(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	started := time.Now()
	_, err := svc.Forward(context.Background(), c, excelAccount(), body)
	require.Error(t, err)
	require.Less(t, time.Since(started), 5*time.Second)
	require.Equal(t, http.StatusGatewayTimeout, rec.Code)
	require.Equal(t, "attachment_timeout", gjson.Get(rec.Body.String(), "error.code").String())
	require.Zero(t, generations.Load(), "no generation starts after an attachment timeout")
	var forwardErr *excelBPSForwardError
	require.ErrorAs(t, err, &forwardErr)
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	require.NotEmpty(t, events.([]*OpsUpstreamErrorEvent))
}

func TestExcelBPSHTTP200ContextLengthFailureAcrossTransports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, kind := range []string{"error", "response.failed"} {
			for _, created := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/created=%t", protocol, kind, created), func(t *testing.T) {
					detail := map[string]any{"code": "context_length_exceeded", "message": "Your input exceeds the context window of this model.", "type": "invalid_request_error", "param": "input"}
					failure := map[string]any{"type": kind, "error": detail, "usage": map[string]any{"input_tokens": 7, "output_tokens": 0, "total_tokens": 7}}
					if kind == "response.failed" {
						failure = map[string]any{"type": kind, "response": map[string]any{"status": "failed", "output": []any{}, "error": detail, "usage": failure["usage"]}}
					}
					raw, e := json.Marshal(failure)
					require.NoError(t, e)
					wire := ""
					if created {
						head, _ := json.Marshal(map[string]any{"type": "response.created", "response": map[string]any{"id": "resp_audit", "created_at": 1792709674, "model": "gpt-6-astra", "status": "in_progress", "output": []any{}}})
						wire = "data: " + string(head) + "\n\n"
					}
					wire += "data: " + string(raw) + "\n\n"
					upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, wire)}
					svc := excelBPSGatewayTestService(upstream)
					body, _ := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": protocol != "json", "input": "hello"})
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
					var out strings.Builder
					var result *OpenAIForwardResult
					var err error
					if protocol == "ws" {
						SetOpenAIClientTransport(c, OpenAIClientTransportWS)
						result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(raw []byte) error { out.Write(raw); out.WriteByte(10); return nil })
					} else {
						result, err = svc.Forward(t.Context(), c, excelAccount(), body)
						out.WriteString(rec.Body.String())
					}
					require.Error(t, err)
					require.Len(t, upstream.requests, 1, "context rejection must not replay")
					require.NotNil(t, result)
					require.Equal(t, 7, result.Usage.InputTokens)
					if protocol == "json" {
						require.Equal(t, 400, rec.Code)
						require.Equal(t, "context_length_exceeded", gjson.Get(out.String(), "error.code").String())
						return
					}
					found := false
					for _, line := range strings.Split(out.String(), "\n") {
						line = strings.TrimPrefix(line, "data: ")
						if gjson.Get(line, "type").String() != "response.failed" {
							continue
						}
						found = true
						require.Equal(t, "context_length_exceeded", gjson.Get(line, "response.error.code").String())
						require.Equal(t, "failed", gjson.Get(line, "response.status").String())
						require.NotEmpty(t, gjson.Get(line, "response.id").String())
						require.Positive(t, gjson.Get(line, "response.created_at").Int())
						if created {
							require.Equal(t, "resp_audit", gjson.Get(line, "response.id").String())
							require.EqualValues(t, 1792709674, gjson.Get(line, "response.created_at").Int())
						}
					}
					require.True(t, found, out.String())
				})
			}
		}
	}
}
