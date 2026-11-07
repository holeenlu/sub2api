package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSModelSelection(t *testing.T) {
	a := excelAccount()
	a.Credentials["client_id"] = openai.ExcelClientID
	delete(a.Extra, "openai_excel_bps_models")
	require.False(t, a.IsExcelBPSEnabledForModel("gpt-6-sol"), "a missing selection never enables every model")
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	require.True(t, a.IsExcelBPSEnabledForModel("gpt-6-astra"))
	require.False(t, a.IsExcelBPSEnabledForModel("gpt-6-sol"))
	require.False(t, a.IsExcelBPSEnabledForModel("gpt-6-astra-other"))
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "gpt-6-astra": "gpt-6-sol"}
	require.True(t, a.IsExcelBPSEnabledForModel("alias"))
	for _, compact := range []bool{false, true} {
		require.True(t, supportsOpenAIRequestModel(a, "alias", compact))
		require.False(t, supportsOpenAIRequestModel(a, "gpt-6-astra", compact))
	}
	require.False(t, a.IsExcelBPSEnabledForModel("gpt-6-astra"), "selection matches mapped upstream")
	require.True(t, a.isExcelBPSUpstreamModelEnabled("gpt-6-astra"), "already mapped names must not map again")
	for _, models := range []any{[]any{}, []string{}, nil, "gpt-6-astra", []any{42, false}} {
		a.Extra["openai_excel_bps_models"] = models
		require.False(t, a.IsExcelBPSEnabledForModel("alias"))
		require.False(t, a.hasExcelBPSSelectedModels())
	}
	a.Extra["openai_excel_bps_models"] = []string{" gpt-6-astra "}
	require.True(t, a.IsExcelBPSEnabledForModel("alias"))
	a.Extra["openai_excel_bps"] = false
	require.False(t, a.IsExcelBPSEnabledForModel("alias"))
	require.False(t, a.IsModelSupported("alias"))
	var absent *Account
	require.False(t, absent.IsExcelBPSEnabledForModel("gpt-6-astra"))
}

func TestExcelBPSSelectedModelForwarding(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6-sol"} {
		t.Run(model, func(t *testing.T) {
			wire := fmt.Sprintf("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_test\",\"status\":\"completed\",\"model\":%q,\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", model)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}}
			svc := excelBPSGatewayTestService(upstream)
			a := excelAccount()
			useExcelBPSMixedGrant(t, svc, a)
			a.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			result, err := svc.Forward(context.Background(), c, a, []byte(fmt.Sprintf(`{"model":%q,"stream":true,"input":"test"}`, model)))
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			if model == "gpt-6-astra" {
				require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
				require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
			} else {
				require.Equal(t, "chatgpt.com", upstream.lastReq.URL.Host)
				require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
			}
		})
	}
}

func TestExcelBPSSelectedModelsPreserveCodexTransport(t *testing.T) {
	a := excelAccount()
	a.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	a.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	a.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	svc := &OpenAIGatewayService{cfg: cfg, settingService: excelBPSSettingsTestService()}
	require.False(t, a.IsOpenAIWSForceHTTPEnabled())
	require.True(t, a.IsOpenAIResponsesWebSocketV2Enabled())
	require.Equal(t, OpenAIWSIngressModeCtxPool, a.ResolveOpenAIResponsesWebSocketV2Mode("off"))
	for _, transport := range []OpenAIUpstreamTransport{OpenAIUpstreamTransportResponsesWebsocketV2, OpenAIUpstreamTransportResponsesWebsocketV2Ingress} {
		require.Equal(t, transport == OpenAIUpstreamTransportResponsesWebsocketV2Ingress, svc.isOpenAIAccountTransportCompatible(context.Background(), a, transport, "gpt-6-astra"))
		require.True(t, svc.isOpenAIAccountTransportCompatible(context.Background(), a, transport, "gpt-6-sol"))
	}
	require.True(t, svc.isOpenAIAccountTransportCompatible(context.Background(), a, OpenAIUpstreamTransportHTTPSSE, "gpt-6-astra"))

	// Account toggles do not override the global gate, or turn native models
	// into HTTP bridges when the account's native WS mode is off.
	a.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	require.True(t, svc.isOpenAIAccountTransportCompatible(context.Background(), a, OpenAIUpstreamTransportResponsesWebsocketV2Ingress, "gpt-6-astra"))
	require.False(t, svc.isOpenAIAccountTransportCompatible(context.Background(), a, OpenAIUpstreamTransportResponsesWebsocketV2Ingress, "gpt-6-sol"))
	svc.settingService = nil
	require.False(t, svc.isOpenAIAccountTransportCompatible(context.Background(), a, OpenAIUpstreamTransportResponsesWebsocketV2Ingress, "gpt-6-astra"))
}

func TestExcelBPSSelectedCompactKeepsExcelModel(t *testing.T) {
	a := excelAccount()
	a.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra"}
	a.Credentials["compact_model_mapping"] = map[string]any{"alias": "gpt-6-sol"}
	require.Equal(t, "gpt-6-astra", resolveOpenAIAccountUpstreamModelForRequest(a, "alias", true))
	require.Equal(t, "gpt-6-sol", resolveOpenAIAccountUpstreamModelForRequest(a, "alias", true, false))
	_, nativeModel := resolveOpenAIForwardMappedModels(a, "alias", true)
	require.Equal(t, "gpt-6-sol", nativeModel, "native forwarding must retain compact mapping after BPS is disabled")
}
