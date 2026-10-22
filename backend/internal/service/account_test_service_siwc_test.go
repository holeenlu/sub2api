package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func siwcAccountTestFixture(t *testing.T, response *http.Response) (*AccountTestService, *httpUpstreamRecorder, *Account) {
	t.Helper()
	oauth, _, _, account := siwcFixture(t)
	authorizeSIWC(t, oauth, account)
	account.Status = StatusActive
	account.Schedulable = true
	account.Concurrency = 2
	upstream := &httpUpstreamRecorder{resp: response}
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 0
	gateway := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, openAITokenProvider: NewOpenAITokenProvider(nil, nil, oauth), openaiWSResolver: NewOpenAIWSProtocolResolver(cfg)}
	return &AccountTestService{openaiGatewayService: gateway}, upstream, account
}
func TestSIWCAccountTestUsesSamePublicGrantAndPreservesIncomingRequest(t *testing.T) {
	svc, upstream, account := siwcAccountTestFixture(t, bpsCompletionResponse(200, bpsCompletedStream("resp_siwc_probe", "OK")))
	account.Credentials["model_mapping"] = map[string]any{"visible-alias": "gpt-6-astra"}
	// A stale BPS selection cannot override the explicitly selected SIWC route.
	account.Extra["openai_excel_bps"] = true
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts/70/test", nil)
	c.Request.Header.Set("Cookie", "native-cookie")
	c.Request.Header.Set("Authorization", "Bearer inbound-secret")
	require.NoError(t, svc.testOpenAIAccountConnection(c, account, "visible-alias", "Reply OK", AccountTestModeDefault))
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, upstream.lastReq.Header.Get("Authorization"))
	require.Empty(t, upstream.lastReq.Header.Get("Cookie"))
	require.Empty(t, upstream.lastReq.Header.Get("ChatGPT-Account-Id"))
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.lastBody, "model").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.Equal(t, "Bearer inbound-secret", c.Request.Header.Get("Authorization"))
	require.Equal(t, "native-secret", account.GetCredential("access_token"))
}
func TestSIWCAccountTestNeverReportsFailedOrIncompleteResponseAsSuccess(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http forbidden", 403, `{"detail":"route not available"}`},
		{"failed stream", 200, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_fail\",\"status\":\"failed\",\"error\":{\"code\":\"subscription_sharing_usage_unavailable\",\"message\":\"not available\"}}}\n\n"},
		{"incomplete stream", 200, "data: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_short\",\"status\":\"incomplete\",\"output\":[]}}\n\n"},
		{"no terminal", 200, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"},
		{"empty completed", 200, bpsCompletedStream("resp_empty", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			svc, upstream, account := siwcAccountTestFixture(t, response)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts/70/test", nil)
			require.Error(t, svc.testOpenAIAccountConnection(c, account, "gpt-6-astra", "Reply OK", AccountTestModeDefault))
			require.NotContains(t, recorder.Body.String(), `"success":true`)
			require.Contains(t, recorder.Body.String(), `"type":"error"`)
			require.Len(t, upstream.requests, 1)
		})
	}
}
func TestSIWCAccountTestRejectsUnsupportedModesBeforeUpstream(t *testing.T) {
	for _, tc := range []struct{ mode, model string }{{AccountTestModeCompact, "gpt-6-astra"}, {AccountTestModeBPSTools, "gpt-6-astra"}, {AccountTestModeDefault, "gpt-image-2"}, {AccountTestModeDefault, "image-alias"}} {
		t.Run(tc.mode+tc.model, func(t *testing.T) {
			svc, upstream, a := siwcAccountTestFixture(t, nil)
			a.Credentials["model_mapping"] = map[string]any{"image-alias": "gpt-image-2"}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts/70/test", nil)
			require.Error(t, svc.testOpenAIAccountConnection(c, a, tc.model, "probe", tc.mode))
			require.Empty(t, upstream.requests)
			require.Error(t, svc.testOpenAIImageOAuth(c, t.Context(), a, "gpt-image-2", "draw"))
			require.Empty(t, upstream.requests)
		})
	}
}

func TestSIWCAccountTestPickerDoesNotSynthesizeCodexImageModels(t *testing.T) {
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{\"models\":[{\"slug\":\"gpt-6-astra\",\"display_name\":\"Granted model\",\"visibility\":\"list\"}]}"))}
	svc, upstream, account := siwcAccountTestFixture(t, response)
	models, err := svc.FetchOpenAIAccountModels(t.Context(), account)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "gpt-6-astra", models[0].ID)
	require.Equal(t, "https://api.openai.com/v1/models", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, upstream.lastReq.Header.Get("Authorization"))
}
