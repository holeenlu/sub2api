package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBPSContinuationCannotMigrateAfterSwitchChange(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		t.Run(protocol, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			svc := excelBPSGatewayTestService(upstream)
			account := excelBPSDualGrantTestAccount(t, svc)
			require.NoError(t, svc.bindResponseMode(t.Context(), 0, "resp_bps_original", UsageAuthorizationModeBPS, time.Hour))
			account.Extra["openai_excel_bps"] = false
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			body := []byte(`{"model":"gpt-5.1","previous_response_id":"resp_bps_original","input":[{"type":"function_call_output","call_id":"original","output":"done"}]}`)
			if protocol == "sse" {
				body = append(body[:len(body)-1], []byte(`,"stream":true}`)...)
			}
			var err error
			if protocol == "ws" {
				_, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "native", body, len(body), "gpt-5.1", "", "", "", "", 1, func([]byte) error { return nil })
			} else {
				_, err = svc.Forward(ctx, c, account, body)
			}
			require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.False(t, failure.ShouldRetryNextAccount())
			require.Empty(t, upstream.requests)
		})
	}
}

func TestResponseContinuationModePreservesProducerAndIsolation(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	require.NoError(t, svc.bindResponseMode(ctx, 17, "opaque-id", UsageAuthorizationModeBPS, time.Hour))
	require.NoError(t, svc.checkResponseContinuationMode(ctx, 17, "opaque-id", session.ResponseModeBPS))
	require.True(t, excelBPSExecutionPinned(ctx))
	for _, mode := range []session.ResponseMode{session.ResponseModeNative, session.ResponseModeCloud} {
		require.ErrorIs(t, svc.checkResponseContinuationMode(ctx, 17, "opaque-id", mode), ErrOpenAIContinuationUnavailable)
	}
	fresh := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	require.NoError(t, svc.checkResponseContinuationMode(fresh, 18, "opaque-id", session.ResponseModeNative))
	require.False(t, excelBPSExecutionPinned(fresh), "mode metadata is group scoped and cannot grant ownership")
	require.NoError(t, svc.checkResponseContinuationMode(fresh, 17, "", session.ResponseModeNative))
	require.False(t, errors.Is(svc.checkResponseContinuationMode(fresh, 17, "unknown", session.ResponseModeNative), ErrOpenAIContinuationUnavailable))
}
