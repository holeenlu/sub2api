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
	"github.com/tidwall/gjson"
)

func TestResponseExecutionPlanMappedModelAndCloudPriority(t *testing.T) {
	for _, mode := range []session.ResponseMode{session.ResponseModeNative, session.ResponseModeBPS, session.ResponseModeCloud} {
		t.Run(string(rune('0'+mode)), func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			account := excelBPSDualGrantTestAccount(t, svc)
			account.Credentials["model_mapping"] = map[string]any{"alias": "selected", "selected": "must-not-map-twice"}
			account.Extra["openai_excel_bps_models"] = []string{"selected"}
			account.Extra["openai_excel_bps"] = mode != session.ResponseModeNative
			account.Extra[OpenAICloudEnabledKey] = mode == session.ResponseModeCloud
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			httpPlan, err := svc.planResponseExecution(ctx, account, "alias")
			require.NoError(t, err)
			wsPlan, err := svc.planResponseUpstreamExecution(ctx, account, "selected")
			require.NoError(t, err)
			require.Equal(t, httpPlan, wsPlan)
			require.Equal(t, mode, httpPlan.mode)
			require.Equal(t, "selected", httpPlan.upstreamModel)
			require.False(t, excelBPSExecutionPinned(ctx), "planning must not mutate the preceding WS turn")
			require.NoError(t, svc.admitResponseExecutionPlan(ctx, 0, "", httpPlan))
			require.Equal(t, mode == session.ResponseModeBPS, excelBPSExecutionPinned(ctx))
		})
	}
}

func TestResponseExecutionPlanRealEntrypointsDoNotRemapBPSAlias(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_plan", "ok"))}
			svc := excelBPSGatewayTestService(upstream)
			account := excelBPSDualGrantTestAccount(t, svc)
			account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-5.1", "gpt-5.1": "must-not-map-twice"}
			account.Extra["openai_excel_bps_models"] = []string{"gpt-5.1"}
			ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			body := []byte(`{"model":"alias","input":"hello","stream":false}`)
			if transport == "sse" {
				body = []byte(`{"model":"alias","input":"hello","stream":true}`)
			}
			var result *OpenAIForwardResult
			var err error
			if transport == "ws" {
				plan, planErr := svc.planResponseExecution(ctx, account, "alias")
				require.NoError(t, planErr)
				body = []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`)
				var terminal bool
				result, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "native", body, len(body), "alias", "", "", "", "", 1,
					func(event []byte) error {
						terminal = terminal || gjson.GetBytes(event, "type").String() == "response.completed"
						return nil
					},
					openAIWSHTTPBridgeOptions{plan: plan})
				require.True(t, terminal)
			} else {
				result, err = svc.Forward(ctx, c, account, body)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
			require.Equal(t, "gpt-5.1", gjson.GetBytes(upstream.lastBody, "model").String())
			require.True(t, excelBPSExecutionPinned(ctx))
		})
	}
}

func TestResponseExecutionPlanFailureAndNewTurnBoundaries(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	account := excelBPSDualGrantTestAccount(t, svc)
	ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	plan, err := svc.planResponseExecution(ctx, account, "gpt-5.1")
	require.NoError(t, err)
	require.NoError(t, svc.admitResponseExecutionPlan(ctx, 0, "", plan))
	require.NoError(t, svc.bindResponseMode(ctx, 0, "original-bps", UsageAuthorizationModeBPS, time.Hour))
	account.Extra["openai_excel_bps"] = false
	next, err := svc.planResponseExecution(ctx, account, "gpt-5.1")
	require.NoError(t, err)
	require.Equal(t, session.ResponseModeNative, next.mode)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, svc.admitResponseExecutionPlan(ctx, 0, "", next), &failure)
	require.Equal(t, GatewayFailureReason("basispoints_route_unavailable"), failure.Reason)
	fresh := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	require.NoError(t, svc.admitResponseExecutionPlan(fresh, 0, "", next))
	require.ErrorIs(t, svc.admitResponseExecutionPlan(fresh, 0, "original-bps", next), ErrOpenAIContinuationUnavailable)

	account.Extra["openai_excel_bps"] = true
	svc.settingService = &SettingService{settingRepo: &excelBPSImageSettingsRepo{err: errors.New("policy unavailable")}}
	_, err = svc.planResponseExecution(fresh, account, "gpt-5.1")
	require.ErrorAs(t, err, &failure)
	require.Equal(t, GatewayFailureReason("basispoints_policy_unavailable"), failure.Reason)
	// Cloud does not consume the suppressed BPS policy or require its storage.
	account.Extra[OpenAICloudEnabledKey] = true
	cloud, err := svc.planResponseExecution(fresh, account, "gpt-5.1")
	require.NoError(t, err)
	require.Equal(t, session.ResponseModeCloud, cloud.mode)
	require.ErrorAs(t, svc.admitResponseExecutionPlan(ctx, 0, "", cloud), &failure)
}
