package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSUpstreamAuthClientStatus(t *testing.T) {
	for upstream, want := range map[int]int{
		http.StatusUnauthorized:    http.StatusServiceUnavailable,
		http.StatusForbidden:       http.StatusServiceUnavailable,
		http.StatusBadRequest:      http.StatusBadRequest,
		http.StatusNotFound:        http.StatusNotFound,
		http.StatusTooManyRequests: http.StatusTooManyRequests,
		http.StatusBadGateway:      http.StatusBadGateway,
	} {
		require.Equal(t, want, excelBPSUpstreamAuthClientStatus(upstream), "upstream %d", upstream)
	}
}

// A BPS HTTP 403 leaves recovery to the account loop; exhaustion is still
// a service 503, never a rejection of the client's own authorization.
// Clients must not see HTTP 401/403: Codex reads a gateway 401 as its own
// login expiring and a 403 as a banned API key.
func TestExcelBPSForbiddenIsNotForwardedAsClientAuthError(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader(`{"error":{"code":"permission_denied","message":"PRIVATE_UPSTREAM"}}`))}}
			svc := excelBPSGatewayTestService(upstream)
			account := excelAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := svc.Forward(context.Background(), c, account, []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"test","stream":%v}`, stream)))
			requireExcelBPSAuthFailover(t, err, c, http.StatusForbidden)
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
			require.Len(t, upstream.requests, 1, "do not replay the rejected request")
		})
	}
}

// Client-side rejections answered by BPS with a 4xx describe the request, not
// the account: they must not move the account's scheduling error rate. Upstream
// authorization recovery stays scoped to BPS; terminal account failures still do.
func TestExcelBPSClientRejectionsDoNotPenalizeScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, body, input string
		status            int
		refusal           bool
		wantRate          float64
	}{
		{name: "upstream 400", status: http.StatusBadRequest, body: `{"error":{"message":"bad input"}}`, input: `"test"`},
		{name: "upstream 404", status: http.StatusNotFound, body: `{"error":{"message":"missing"}}`, input: `"test"`},
		{name: "local validation", status: http.StatusOK, input: `[{"type":"function_call_output","call_id":"missing","output":"x"}]`},
		{name: "unsupported hosted tool with forced choice", status: http.StatusOK, input: `"test"`},
		{name: "upstream 403 preserves native health", status: http.StatusForbidden, body: `{"error":{"code":"permission_denied"}}`, input: `"test"`},
		{name: "explicit request refusal", status: http.StatusForbidden, body: `{"error":{"code":"content_policy_violation"}}`, input: `"test"`, refusal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}}
			svc := excelBPSGatewayTestService(upstream)
			svc.openaiAccountStats = newOpenAIAccountRuntimeStats()
			svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
			account := excelAccount()
			body := `{"model":"gpt-6-astra","input":` + tc.input + `}`
			if tc.name == "unsupported hosted tool with forced choice" {
				body = `{"model":"gpt-6-astra","input":"test","tools":[{"type":"web_search"}],"tool_choice":{"type":"web_search"}}`
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(body)))
			_, err := svc.Forward(context.Background(), c, account, []byte(body))
			if tc.status == 403 && !tc.refusal {
				requireExcelBPSAuthFailover(t, err, c, tc.status)
			} else {
				var forwardErr *excelBPSForwardError
				require.ErrorAs(t, err, &forwardErr, "forward error: %v", err)
				require.Equal(t, rec.Code, forwardErr.status)
				require.Equal(t, tc.wantRate == 0, forwardErr.RequestScoped())
			}
			for i := 0; i < openAIHealthMinSamples; i++ {
				svc.openaiAccountStats.report(account.ID, true, nil, "gpt-6-astra")
			}
			svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, "gpt-6-astra", fmt.Errorf("forward: %w", err))
			rate, _, _ := svc.openaiAccountStats.snapshot(account.ID, "gpt-6-astra")
			require.InDelta(t, tc.wantRate, rate, 0.001)
			if tc.refusal {
				require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			} else if tc.wantRate == 0 {
				require.Less(t, rec.Code, 500)
			} else {
				require.GreaterOrEqual(t, rec.Code, 500)
			}
		})
	}
}

func TestReportOpenAIAccountScheduleFailureClassifiesBPSClientErrors(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 301, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for range openAIHealthMinSamples {
		require.False(t, svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, "gpt-6-astra", fmt.Errorf("forward: %w", &excelBPSForwardError{code: "basispoints_request_invalid", status: http.StatusBadRequest})))
		require.False(t, svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, "gpt-6-astra", &openAIWSTurnRejectedError{cause: &excelBPSForwardError{code: "basispoints_unsupported_tools", status: http.StatusBadRequest}}))
	}
	scheduler := svc.getOpenAIAccountScheduler(context.Background()).(*defaultOpenAIAccountScheduler)
	rate, _, _ := scheduler.stats.snapshot(account.ID, "gpt-6-astra")
	require.Zero(t, rate, "client 4xx answered by BPS must not be sampled as account failures")
	for range openAIHealthMinSamples {
		svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, "gpt-6-astra", &excelBPSForwardError{code: "basispoints_upstream_error", status: http.StatusServiceUnavailable})
	}
	rate, _, _ = scheduler.stats.snapshot(account.ID, "gpt-6-astra")
	require.Greater(t, rate, 0.5, "terminal upstream failures still penalize the account")
	var sentinel *excelBPSForwardError
	require.False(t, errors.As(errors.New("other"), &sentinel))
}

func requireExcelBPSAuthFailover(t *testing.T, err error, c *gin.Context, status int) {
	t.Helper()
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, status, failure.StatusCode)
	require.Equal(t, http.StatusServiceUnavailable, failure.ClientStatusCode)
	require.True(t, failure.ShouldRetryNextAccount())
	require.True(t, failure.RequestNotSent || failure.RejectedBeforeGeneration)
	require.False(t, c.Writer.Written(), "defer terminal output until bounded account recovery exhausts")
	require.False(t, IsResponseCommitted(c))
}
