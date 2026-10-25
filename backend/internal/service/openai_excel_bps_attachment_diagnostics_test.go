package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/tidwall/gjson"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSAttachmentFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, stage, kind string
		status, clientStatus    int
		clientCode              string
		transport               error
	}{
		{name: "HTTP rejection", status: 403, body: "PRIVATE_UPSTREAM_BODY", stage: "attachment_http", kind: "upstream_http"},
		{name: "rate limit", status: 429, body: "PRIVATE_UPSTREAM_BODY", stage: "attachment_http", kind: "upstream_http"},
		{name: "redirect", status: 307, body: "PRIVATE_UPSTREAM_BODY", stage: "attachment_http", kind: "upstream_http"},
		{name: "invalid JSON", status: 200, body: "PRIVATE_UPSTREAM_BODY", stage: "attachment_response", kind: "invalid_json"},
		{name: "invalid file ID", status: 200, body: "{\"openai_file_id\":\"PRIVATE_FILE_ID\"}", stage: "attachment_response", kind: "invalid_file_id"},
		{name: "large response", status: 200, body: strings.Repeat("x", (64<<10)+1), stage: "attachment_response", kind: "response_too_large"},
		{name: "DNS", transport: &net.DNSError{Err: "PRIVATE_DNS_ERROR", Name: "PRIVATE_HOST"}, stage: "attachment_transport", kind: "dns_error"},
		{name: "deadline", transport: fmt.Errorf("PRIVATE_URL: %w", context.DeadlineExceeded), stage: "attachment_transport", kind: "deadline_exceeded", clientStatus: http.StatusGatewayTimeout, clientCode: "attachment_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			calls := 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
				if tc.transport != nil {
					return nil, tc.transport
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}}
			body, _ := nativeGatewayBody(t)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			account := excelAccount()
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.Equal(t, 1, calls, "attachment failure must not submit generation")
			if tc.status == 429 {
				requireExcelBPSRateLimitFailover(t, err, c)
			} else {
				wantStatus := tc.status
				if wantStatus < 400 || wantStatus > 599 {
					wantStatus = http.StatusBadGateway
				}
				if tc.clientStatus != 0 {
					// An upload timeout is reported as its own phase, not as a
					// first-output timeout or a generic attachment failure.
					wantStatus = tc.clientStatus
				}
				require.Equal(t, wantStatus, rec.Code, "upstream diagnostics must not replace the client failure status")
				if tc.clientCode != "" {
					require.Equal(t, tc.clientCode, gjson.Get(rec.Body.String(), "error.code").String())
				}
			}
			events, exists := c.Get(OpsUpstreamErrorsKey)
			require.True(t, exists, "safe stage/reason diagnostic must be persisted")
			attempts, ok := events.([]*OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.Len(t, attempts, 1)
			attempt := attempts[0]
			require.Equal(t, tc.stage, attempt.Stage)
			require.Equal(t, tc.kind, attempt.Reason)
			require.Equal(t, tc.status, attempt.UpstreamStatusCode, "record real upstream HTTP status, not synthesized 502")
			require.Equal(t, basispoints.AttachmentsURL, attempt.UpstreamURL)
			var detail map[string]any
			require.NoError(t, json.Unmarshal([]byte(attempt.Detail), &detail))
			require.Equal(t, tc.kind, detail["error_kind"])
			require.Equal(t, false, detail["generation_started"])
			require.NotContains(t, fmt.Sprint(err, attempt, c.GetString(OpsUpstreamErrorDetailKey), rec.Body.String()), "PRIVATE_")
		})
	}
}

func TestExcelBPSAttachmentClientCancellationIsNotProviderFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := excelBPSGatewayTestService(nil)
	enableNativeAttachments(svc)
	calls := 0
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(*http.Request, string, int64, int) (*http.Response, error) {
		calls++
		cancel()
		return nil, fmt.Errorf("PRIVATE_PROXY: %w", context.Canceled)
	}}
	body, _ := nativeGatewayBody(t)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
	_, err := svc.Forward(ctx, c, excelAccount(), body)
	require.True(t, errors.Is(err, context.Canceled))
	require.Equal(t, 1, calls)
	require.Empty(t, c.GetString(OpsUpstreamErrorMessageKey))
	_, exists := c.Get(OpsUpstreamErrorsKey)
	require.False(t, exists)
}

func TestExcelBPSFileOwnershipDiagnosticsLoggingBoundary(t *testing.T) {
	for _, logging := range []bool{false, true} {
		t.Run(fmt.Sprint(logging), func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			svc.cfg.Gateway.LogUpstreamErrorBody = logging
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			resp := bpsCompletionResponse(500, "")
			defer resp.Body.Close()
			resp.Header.Set("X-Request-Id", "req_ownership")
			raw := []byte(`{"error":{"type":"server_error","message":"Unknown error while validating file ownership.","param":"file-private123"}}`)
			local := &excelBPSFileOwnershipContext{FileReferenceCounts: basispoints.FileReferenceCounts{Unmanaged: 1}, Stage: "generation", CompactionCompleted: true}
			svc.recordExcelBPSUpstreamFailure(c, excelAccount(), resp, raw, "test-token", "http_error", "safe ownership failure", local)
			value, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			events := value.([]*OpsUpstreamErrorEvent)
			require.Len(t, events, 1)
			event := events[0]
			require.Equal(t, 500, event.UpstreamStatusCode)
			require.Equal(t, "req_ownership", event.UpstreamRequestID)
			if logging {
				require.Contains(t, event.Detail, "file_ownership_context")
				var diagnostic struct {
					Context excelBPSFileOwnershipContext `json:"file_ownership_context"`
				}
				require.NoError(t, json.Unmarshal([]byte(event.Detail), &diagnostic))
				require.True(t, diagnostic.Context.CompactionCompleted)
				require.Contains(t, event.Detail, "file-[redacted]")
				require.NotContains(t, event.UpstreamResponseBody, "file_ownership_context")
				require.Equal(t, "Unknown error while validating file ownership.", event.Message)
			} else {
				require.Empty(t, event.Detail)
				require.Empty(t, event.UpstreamResponseBody)
			}
			require.NotContains(t, fmt.Sprint(event), "private123")
		})
	}
}

func TestExcelBPSAttachment429BoundContextRecovery(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			svc.cfg.Gateway.OpenAITurnMaxAttempts = 2
			account := excelAccount()
			body := excelBPSBoundAttachmentBody(t)
			var source map[string]any
			require.NoError(t, json.Unmarshal(body, &source))
			source["stream"] = transport != "json"
			body, err := json.Marshal(source)
			require.NoError(t, err)
			uploads, generations := 0, 0
			rejected := &nativeAttachmentBody{Reader: strings.NewReader("PRIVATE_UPSTREAM_BODY")}
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				require.Equal(t, account.ID, id, "opaque history must stay on its owner")
				if req.URL.String() == basispoints.AttachmentsURL {
					uploads++
					if uploads == 1 {
						return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"0"}}, Body: rejected}, nil
					}
					require.True(t, rejected.closed, "release the failed upload response before waiting/retry")
					return bpsCompletionResponse(200, "{\"openai_file_id\":\"file-recovered-upload\"}"), nil
				}
				generations++
				require.Equal(t, basispoints.ResponsesURL, req.URL.String())
				wire, readErr := io.ReadAll(req.Body)
				require.NoError(t, readErr)
				require.Contains(t, string(wire), "PRIVATE_BOUND_CONTEXT")
				require.Contains(t, string(wire), "file-recovered-upload")
				return bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_recovered_attachment\",\"status\":\"completed\",\"output\":[]}}\n\n"), nil
			}}
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg, "low")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
			var output strings.Builder
			if transport == "ws" {
				SetOpenAIClientTransport(c, OpenAIClientTransportWS)
				_, err = svc.proxyOpenAIWSExcelBPSTurn(ctx, c, account, body, "gpt-6-astra", func(event []byte) error { output.Write(event); return nil })
			} else {
				_, err = svc.Forward(ctx, c, account, body)
				output.WriteString(rec.Body.String())
			}
			require.NoError(t, err)
			require.Equal(t, 2, uploads, "retry only the rejected upload; duplicate image remains deduplicated")
			require.Equal(t, 1, generations, "generation was never replayed")
			budget, ok := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
			require.True(t, ok)
			require.Equal(t, 1, budget.attemptBudget.Snapshot().Attempts, "only generation consumes generation attempts")
			require.Equal(t, 1, budget.attemptBudget.Snapshot().PreparationRetries)
			require.Empty(t, c.GetString(OpsUpstreamErrorMessageKey))
			events, _ := c.Get(OpsUpstreamErrorsKey)
			require.Len(t, events.([]*OpsUpstreamErrorEvent), 1)
			require.Equal(t, "retry", events.([]*OpsUpstreamErrorEvent)[0].Kind)
			require.NotContains(t, fmt.Sprint(events), "PRIVATE_")
			require.Contains(t, output.String(), "resp_recovered_attachment")
			require.False(t, c.GetBool(OpenAIAccountReplayAllowedContextKey), "recovery must not make opaque context portable")
			require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"), "a successful bounded upload recovery must not leave its own cooldown")
		})
	}
}

func excelBPSBoundAttachmentBody(t *testing.T, bindFile ...bool) []byte {
	t.Helper()
	body, _ := nativeGatewayBody(t)
	var source map[string]any
	require.NoError(t, json.Unmarshal(body, &source))
	source["input"] = append([]any{map[string]any{"type": "compaction", "id": "cmp_old", "encrypted_content": "PRIVATE_BOUND_CONTEXT"}}, source["input"].([]any)...)
	if len(bindFile) == 0 || bindFile[0] {
		// A server-owned file is a real portability blocker; ciphertext alone
		// must no longer stand in for ownership in bounded upload-retry tests.
		input, ok := source["input"].([]any)
		require.True(t, ok)
		source["input"] = append(input, map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "file_id": "file-bound-owner"}}})
	}
	body, err := json.Marshal(source)
	require.NoError(t, err)
	return body
}

func TestExcelBPSAttachment429RecoveryBounds(t *testing.T) {
	for _, tc := range []struct {
		name, retryAfter, setting, reason, source string
		wantDelayMS                               int
		portable, shortDeadline, shortPhase       bool
		limit, status, wantCalls, wantAttempts    int
	}{
		{name: "portable still fails over", retryAfter: "0", portable: true, status: 429, limit: 2, wantCalls: 1, reason: "portable_context"},
		{name: "long Retry-After", retryAfter: "6", status: 429, limit: 2, wantCalls: 1, reason: "delay_exceeds_limit", source: "retry_after", wantDelayMS: 6000},
		{name: "independent preparation budget", retryAfter: "0", status: 429, limit: 1, wantCalls: 2, reason: "retry_exhausted", source: "retry_after", wantDelayMS: 1000},
		{name: "attachment phase deadline", retryAfter: "0", shortPhase: true, status: 429, limit: 2, wantCalls: 1, reason: "turn_deadline", source: "retry_after", wantDelayMS: 1000},
		// The first-output budget is paused while attachments upload, so a nearly
		// spent turn deadline no longer cuts the short same-account retry.
		{name: "turn deadline does not bound attachment phase", retryAfter: "0", shortDeadline: true, status: 429, limit: 2, wantCalls: 2, reason: "retry_exhausted", source: "retry_after", wantDelayMS: 1000},
		{name: "persistent 429", retryAfter: "0", status: 429, limit: 2, wantCalls: 2, wantAttempts: 0, reason: "retry_exhausted", source: "retry_after", wantDelayMS: 1000},
		{name: "long configured fallback", setting: `{"enabled":true,"cooldown_seconds":30}`, status: 429, limit: 2, wantCalls: 1, reason: "delay_exceeds_limit", source: "fallback_setting", wantDelayMS: 30000},
		{name: "disabled fallback", setting: `{"enabled":false,"cooldown_seconds":30}`, status: 429, limit: 2, wantCalls: 1, reason: "fallback_disabled", source: "fallback_setting"},
		{name: "500 is not a rejected upload retry", retryAfter: "0", status: 500, limit: 2, wantCalls: 1},
		{name: "403 is not retried", retryAfter: "0", status: 403, limit: 2, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			svc.cfg.Gateway.OpenAITurnMaxAttempts = tc.limit
			if tc.setting != "" {
				repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
				svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
				svc.rateLimitService.SetSettingService(NewSettingService(&excelBPSQuotaSettingsRepo{cooldown: tc.setting}, svc.cfg))
			}
			account := excelAccount()
			body := excelBPSBoundAttachmentBody(t)
			if tc.portable {
				body, _ = nativeGatewayBody(t)
			}
			calls := 0
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, account.ID, id)
				require.Equal(t, basispoints.AttachmentsURL, req.URL.String(), "never start generation after upload rejection")
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {tc.retryAfter}}, Body: io.NopCloser(strings.NewReader("PRIVATE_BODY"))}, nil
			}}
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg, "low")
			budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
			if tc.shortDeadline {
				budget.deadline = time.Now().Add(200 * time.Millisecond)
			}
			if tc.shortPhase {
				previous := excelBPSAttachmentPhaseTimeout
				excelBPSAttachmentPhaseTimeout = 200 * time.Millisecond
				t.Cleanup(func() { excelBPSAttachmentPhaseTimeout = previous })
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
			_, err := svc.Forward(ctx, c, account, body)
			require.Error(t, err)
			require.Equal(t, tc.wantCalls, calls)
			require.Equal(t, tc.wantAttempts, budget.attemptBudget.Snapshot().Attempts)
			require.Equal(t, tc.portable, c.GetBool(OpenAIAccountReplayAllowedContextKey))
			if tc.status == 429 {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.True(t, failover.RequestNotSent)
				require.Equal(t, tc.retryAfter, failover.ResponseHeaders.Get("Retry-After"))
				require.Equal(t, tc.reason != "fallback_disabled", svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
				require.False(t, IsResponseCommitted(c))
				value, _ := c.Get(OpsUpstreamErrorsKey)
				events := value.([]*OpsUpstreamErrorEvent)
				var detail map[string]any
				require.NoError(t, json.Unmarshal([]byte(events[len(events)-1].Detail), &detail))
				require.Equal(t, tc.reason, detail["attachment_retry_reason"])
				require.Equal(t, float64(tc.wantCalls-1), detail["attachment_retry_attempts"])
				if tc.source != "" {
					require.Equal(t, tc.source, detail["retry_delay_source"])
					require.Equal(t, float64(tc.wantDelayMS), detail["retry_delay_ms"])
				}
				require.Equal(t, tc.portable, detail["context_portable"])
				if !tc.portable {
					require.Equal(t, "encrypted_content", detail["context_blocker"])
				}
				require.NotContains(t, fmt.Sprint(events), "PRIVATE_")
			} else {
				require.Equal(t, tc.status, rec.Code)
			}
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			require.NotContains(t, rec.Body.String(), "PRIVATE_")
		})
	}
}

func TestExcelBPSAttachment429RecoveryCancellationAndCooldown(t *testing.T) {
	for _, mode := range []string{"missing header cancellation", "invalid header cancellation", "longer concurrent cooldown"} {
		t.Run(mode, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			enableNativeAttachments(svc)
			account := excelAccount()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			body := excelBPSBoundAttachmentBody(t)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
			calls := 0
			rejected := &nativeAttachmentBody{Reader: strings.NewReader("PRIVATE_BODY")}
			var timer *time.Timer
			defer func() {
				if timer != nil {
					timer.Stop()
				}
			}()
			concurrent := mode == "longer concurrent cooldown"
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				calls++
				require.Equal(t, basispoints.AttachmentsURL, req.URL.String())
				retryAfter := ""
				if concurrent {
					retryAfter = "0"
				} else if strings.HasPrefix(mode, "invalid") {
					retryAfter = "PRIVATE_INVALID_HEADER"
				}
				timer = time.AfterFunc(50*time.Millisecond, func() {
					if concurrent {
						svc.coolDownExcelBPS(ctx, account, "30")
					} else {
						cancel()
					}
				})
				return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {retryAfter}}, Body: rejected}, nil
			}}
			started := time.Now()
			_, err := svc.Forward(ctx, c, account, body)
			require.Error(t, err)
			require.Equal(t, 1, calls, "no upload retry or generation after cancellation/extended cooldown")
			require.True(t, rejected.closed)
			if concurrent {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				until, _ := svc.excelBPSCooldownUntil.Load(account.ID)
				require.Greater(t, time.Until(until.(time.Time)), 20*time.Second, "retain the concurrent longer cooldown")
			} else {
				require.ErrorIs(t, err, context.Canceled)
				require.Less(t, time.Since(started), time.Second)
				require.Empty(t, rec.Body.String())
				require.Empty(t, c.GetString(OpsUpstreamErrorMessageKey))
			}
			events, _ := c.Get(OpsUpstreamErrorsKey)
			retry := events.([]*OpsUpstreamErrorEvent)[0]
			require.Equal(t, "retry", retry.Kind)
			var diagnostic map[string]any
			require.NoError(t, json.Unmarshal([]byte(retry.Detail), &diagnostic))
			wantDelay := 5000.0
			if concurrent {
				wantDelay = 1000
			}
			require.Equal(t, wantDelay, diagnostic["retry_delay_ms"])
			require.Equal(t, true, diagnostic["retry_same_account"])
			require.Equal(t, false, diagnostic["generation_started"])
			require.Equal(t, "short_wait", diagnostic["attachment_retry_reason"])
			require.Equal(t, float64(0), diagnostic["attachment_retry_attempts"], "waiting does not mean the attachment was retried")
			if concurrent {
				events := events.([]*OpsUpstreamErrorEvent)
				require.NoError(t, json.Unmarshal([]byte(events[len(events)-1].Detail), &diagnostic))
				require.Equal(t, "cooldown_extended", diagnostic["attachment_retry_reason"])
			}
			require.NotContains(t, fmt.Sprint(events), "PRIVATE_")
		})
	}
}
