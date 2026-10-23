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

func TestHTTPToWSHealthFeedbackMatchesCandidateTransport(t *testing.T) {
	cfg := newSchedulerTestOpenAIWSV2Config()
	svc := &OpenAIGatewayService{cfg: cfg}
	account := &Account{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 10,
		Extra: map[string]any{"openai_oauth_responses_websockets_v2_enabled": true}}
	body := []byte(`{"model":"gpt-5.1","input":"hi"}`)
	canonicalModel := canonicalOpenAIAccountSchedulingModel(account, "gpt-5.1")
	for range openAIHealthMinSamples {
		ctx := WithSchedulingQuotaRequest(t.Context(), body, ContentModerationProtocolOpenAIResponses)
		budget := NewOpenAITurnBudget(cfg)
		ctx = WithOpenAITurnBudget(ctx, budget)
		require.NoError(t, TakeOpenAITurnAttempt(withTurnRecoveryTarget(ctx, account, "websocket", "/v1/responses", "", "token", body)))
		require.Equal(t, "native_ws:small:medium", reportedSchedulingHealthCohort(ctx, account))
		svc.ReportOpenAIAccountScheduleFailure(ctx, account, canonicalModel, errors.New("upstream connection reset"))
		budget.controlCancel(context.Canceled)
	}
	scheduler := svc.getOpenAIAccountScheduler(t.Context()).(*defaultOpenAIAccountScheduler)
	ctx := WithSchedulingQuotaRequest(t.Context(), body, ContentModerationProtocolOpenAIResponses)
	req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.1", RequiredTransport: OpenAIUpstreamTransportAny}
	require.Equal(t, "native_ws:small:medium", scheduler.schedulingHealthCohort(ctx, account, req))
	actual, _, _ := scheduler.stats.snapshot(account.ID, canonicalModel, "native_ws:small:medium")
	require.Greater(t, actual, openAIHealthDegradedErrorRate)
	plan := scheduler.buildOpenAIAccountLoadPlan(ctx, req, []*Account{account}, nil)
	require.Len(t, plan.candidates, 1)
	require.Greater(t, plan.candidates[0].errorRate, openAIHealthDegradedErrorRate)
	for _, transport := range []OpenAIUpstreamTransport{OpenAIUpstreamTransportAny, OpenAIUpstreamTransportHTTPSSE} {
		req.RequiredTransport, req.RequireCompact = transport, true
		require.Equal(t, "native_http:small:medium", scheduler.schedulingHealthCohort(ctx, account, req))
	}
	req.RequiredTransport, req.RequireCompact = OpenAIUpstreamTransportAny, false
	account.Type = AccountTypeAPIKey
	require.Equal(t, "native_http:small:medium", scheduler.schedulingHealthCohort(ctx, account, req))
}

func TestExcelBPSUpstreamFailuresStillPenalizeScheduling(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"error":{"type":"api_error","message":"test upstream failure"}}`)),
			}}
			svc := excelBPSGatewayTestService(upstream)
			svc.openaiAccountStats = newOpenAIAccountRuntimeStats()
			svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
			account := excelAccount()
			body := []byte(`{"model":"gpt-6-astra","input":"test"}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.NotNil(t, upstream.lastReq)
			for i := 0; i < openAIHealthMinSamples; i++ {
				svc.openaiAccountStats.report(account.ID, true, nil, "gpt-6-astra")
			}
			svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, "gpt-6-astra", err)
			rate, _, _ := svc.openaiAccountStats.snapshot(account.ID, "gpt-6-astra")
			require.InDelta(t, 0.2, rate, 0.001, "actual upstream errors must still affect account health")
		})
	}
}
