package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

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
