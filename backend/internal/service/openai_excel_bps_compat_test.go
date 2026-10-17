package service

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSCompatibilityRoutingAndFailures(t *testing.T) {
	for _, anthropic := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, outcome := range []string{"complete", "eof", "disabled", "fast_denied"} {
				t.Run(fmt.Sprintf("anthropic=%t/stream=%t/%s", anthropic, stream, outcome), func(t *testing.T) {
					wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_compat\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
						"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_compat\",\"delta\":\"hello\"}\n\n" + bpsCompletedStream("resp_compat", "hello")
					if outcome == "eof" {
						wire = "data: {\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n"
					}
					upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, wire)}
					svc := excelBPSGatewayTestService(upstream)
					account := excelAccount()
					account.Credentials["model_mapping"] = map[string]any{"channel": "gpt-6-astra", "gpt-6-astra": "must-not-map-twice"}
					account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
					if outcome == "disabled" {
						require.NoError(t, svc.settingService.settingRepo.SetMultiple(t.Context(), map[string]string{SettingKeyExcelBPSEnabled: "false"}))
					}
					ctx := t.Context()
					if outcome == "fast_denied" {
						ctx = withOpenAIFastPolicyContext(ctx, &OpenAIFastPolicySettings{Rules: []OpenAIFastPolicyRule{{ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionBlock, Scope: BetaPolicyScopeAll, ErrorMessage: "fast blocked"}}})
					}
					body := []byte(fmt.Sprintf(`{"model":"public","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"stream":%t,"service_tier":"priority"}`, stream))
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
					if anthropic && outcome == "fast_denied" {
						c.Request.Header.Set("anthropic-beta", "fast-mode-2026-02-01")
					}
					var result *OpenAIForwardResult
					var err error
					if anthropic {
						result, err = svc.ForwardAsAnthropic(ctx, c, account, body, "cache-compat", "channel")
					} else {
						result, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "cache-compat", "channel")
					}
					if outcome == "disabled" || outcome == "fast_denied" {
						require.Error(t, err)
						require.Nil(t, upstream.lastReq, "Excel credentials must never fall through to native Codex")
						if outcome == "fast_denied" {
							require.Equal(t, 403, rec.Code)
						} else {
							require.Equal(t, 400, rec.Code)
						}
						return
					}
					require.NotNil(t, result)
					require.Equal(t, UsageAuthorizationModeBPS, result.AuthorizationMode)
					require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
					require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
					require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.lastBody, "model").String())
					require.Equal(t, "public", result.Model)
					require.Equal(t, stream, result.Stream)
					if outcome == "eof" {
						require.Error(t, err)
						require.Equal(t, 7, result.Usage.InputTokens)
						require.Equal(t, 3, result.Usage.OutputTokens)
						require.NotEmpty(t, result.RequestID)
						require.Contains(t, rec.Body.String(), "error")
						require.NotContains(t, rec.Body.String(), "message_stop")
						require.NotContains(t, rec.Body.String(), "[DONE]")
					} else {
						require.NoError(t, err)
						require.Equal(t, 200, rec.Code)
						require.Contains(t, rec.Body.String(), "hello")
						require.Contains(t, rec.Body.String(), "public")
						if stream && !anthropic {
							require.Contains(t, rec.Body.String(), "[DONE]")
						}
					}
				})
			}
		}
	}
}
