package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const excelBPSToolPolicyResponse = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_policy\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"

func TestExcelBPSClientToolChoiceUsesSameAccountNative(t *testing.T) {
	choices := []any{"required", map[string]any{"type": "function", "name": "inspect"}, map[string]any{"type": "custom", "name": "exec"}, map[string]any{"type": "allowed_tools", "mode": "required", "tools": []any{map[string]any{"type": "function", "name": "inspect"}}}, map[string]any{"type": "allowed_tools", "mode": "auto", "tools": []any{map[string]any{"type": "function", "name": "inspect"}}}}
	for _, protocol := range []string{"json", "sse", "ws", "chat", "chat-sse"} {
		for index, choice := range choices {
			if strings.HasPrefix(protocol, "chat") && index == 2 {
				continue
			}
			t.Run(fmt.Sprintf("%s/choice_%d", protocol, index), func(t *testing.T) {
				svc := excelBPSGatewayTestService(nil)
				account := excelBPSNativeFallbackTestAccount(t, svc)
				tool := map[string]any{"type": "function", "name": "inspect", "parameters": map[string]any{"type": "object"}}
				if index == 2 {
					tool = map[string]any{"type": "custom", "name": "exec"}
				}
				stream := protocol == "sse" || protocol == "ws" || protocol == "chat-sse"
				source := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": "inspect", "tools": []any{tool}, "tool_choice": choice}
				if strings.HasPrefix(protocol, "chat") {
					delete(source, "input")
					source["messages"] = []any{map[string]any{"role": "user", "content": "inspect"}}
					source["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "inspect", "parameters": map[string]any{"type": "object"}}}}
					if index == 1 {
						source["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": "inspect"}}
					}
				}
				body, err := json.Marshal(source)
				require.NoError(t, err)
				wantChoice, err := json.Marshal(choice)
				require.NoError(t, err)
				calls := 0
				svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, id int64, _ int) (*http.Response, error) {
					calls++
					require.Equal(t, account.ID, id)
					require.Equal(t, "/backend-api/codex/responses", req.URL.Path, "no BPS upload or generation precedes native dispatch")
					require.Equal(t, "Bearer native-codex-token", req.Header.Get("Authorization"))
					wire, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.JSONEq(t, string(wantChoice), gjson.GetBytes(wire, "tool_choice").Raw, "must not silently degrade to auto")
					return bpsCompletionResponse(200, bpsCompletedStream("resp_native_choice", "native answer")), nil
				}}
				ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
				ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget).bpsRecovery.managed = true
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
				var result *OpenAIForwardResult
				switch {
				case protocol == "ws":
					result, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "native-codex-token", body, len(body), "gpt-6-astra", "", "", "", "", 1, func(event []byte) error { _, e := rec.Write(append(event, byte(10))); return e })
				case strings.HasPrefix(protocol, "chat"):
					result, err = svc.forwardOpenAIGrantCompatibility(ctx, c, account, body, "", "gpt-6-astra", false)
				default:
					result, err = svc.Forward(ctx, c, account, body)
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.True(t, account.IsExcelBPSEnabled())
				require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
			})
		}
	}
}

func TestExcelBPSClientToolChoiceFallbackStaysLocalWhenUnsafe(t *testing.T) {
	for _, scenario := range []string{"excel_only", "no_native_grant", "probe", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(500, "{}")}
			svc := excelBPSGatewayTestService(upstream)
			account := excelBPSNativeFallbackTestAccount(t, svc)
			if scenario == "excel_only" {
				account = excelAccount()
			}
			if scenario == "no_native_grant" {
				delete(account.Credentials, "access_token")
				delete(account.Credentials, "refresh_token")
			}
			source := map[string]any{"model": "gpt-6-astra", "input": "inspect", "tools": []any{map[string]any{"type": "function", "name": "inspect", "parameters": map[string]any{"type": "object"}}}, "tool_choice": "required"}
			body, err := json.Marshal(source)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			if scenario == "probe" {
				c.Set(bpsAccountProbeRequiredContextKey, true)
			}
			_, err = svc.Forward(ctx, c, account, body)
			require.Error(t, err)
			require.Empty(t, upstream.requests)
			if scenario != "cancelled" {
				require.Equal(t, 400, rec.Code)
				require.Equal(t, "tool_choice", gjson.Get(rec.Body.String(), "error.param").String())
				require.False(t, svc.ReportOpenAIAccountScheduleFailure(t.Context(), account, "gpt-6-astra", err))
			}
			require.True(t, account.IsExcelBPSEnabled())
		})
	}
}

func TestExcelBPSToolFallbackPolicy(t *testing.T) {
	cases := []struct {
		name         string
		tool         map[string]any
		choice       any
		nativeReason string
	}{
		{"desktop search", map[string]any{"type": "web_search", "external_web_access": true}, "auto", "web_search"},
		{"implicit auto", map[string]any{"type": "web_search", "external_web_access": true}, nil, "web_search"},
		{"default search", map[string]any{"type": "web_search"}, "auto", ""},
		{"offline search", map[string]any{"type": "web_search", "external_web_access": false}, "auto", ""},
		{"high context", map[string]any{"type": "web_search", "external_web_access": false, "search_context_size": "high"}, "auto", "web_search"},
		{"medium context", map[string]any{"type": "web_search", "search_context_size": "medium"}, "auto", ""},
		{"low context", map[string]any{"type": "web_search", "search_context_size": "low"}, "auto", ""},
		{"preview", map[string]any{"type": "web_search_preview", "external_web_access": true}, "auto", "web_search"},
		{"dated preview", map[string]any{"type": "web_search_preview_2025_03_11", "external_web_access": true}, "auto", "web_search"},
		{"dated search", map[string]any{"type": "web_search_2025_08_26", "external_web_access": true}, "auto", "web_search"},
		{"image", map[string]any{"type": "image_generation"}, "auto", "image_generation"},
		{"disabled search", map[string]any{"type": "web_search", "external_web_access": true}, "none", ""},
		{"disabled image", map[string]any{"type": "image_generation"}, "none", ""},
		{"forced search", map[string]any{"type": "web_search"}, map[string]any{"type": "web_search"}, "tool_choice"},
		{"forced image", map[string]any{"type": "image_generation"}, map[string]any{"type": "image_generation"}, "tool_choice"},
		{"required search", map[string]any{"type": "web_search", "external_web_access": true}, "required", "web_search"},
	}
	for _, tc := range cases {
		for _, omit := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/omit=%t/stream=%t", tc.name, omit, stream), func(t *testing.T) {
					core, logs := observer.New(zap.InfoLevel)
					ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "req-policy")))
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(excelBPSToolPolicyResponse))}}
					svc := excelBPSGatewayTestService(upstream)
					account := excelAccount()
					if omit {
						account.Extra[ExcelBPSOmitUnsupportedToolsKey] = true
					}
					source := map[string]any{"model": "gpt-6-astra", "reasoning": map[string]any{"effort": "high"}, "input": "private-user-prompt", "stream": stream, "tools": []any{tc.tool, map[string]any{"type": "function", "name": "lookup_client", "parameters": map[string]any{"type": "object"}}}}
					if tc.choice != nil {
						source["tool_choice"] = tc.choice
					}
					body, err := json.Marshal(source)
					require.NoError(t, err)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
					c.Header("X-Codex2API-Basispoints-Bypass", "stale-attempt")
					c.Header("X-Codex2API-Upstream", "codex")
					_, err = svc.Forward(ctx, c, account, body)
					choice, _ := tc.choice.(string)
					forced := tc.choice != nil && choice != "auto" && choice != "none"
					if forced {
						require.Error(t, err)
						require.Equal(t, http.StatusBadRequest, rec.Code)
						require.Contains(t, rec.Body.String(), "basispoints supports tool_choice auto or none only")
						require.True(t, IsResponseCommitted(c))
						require.Empty(t, upstream.requests)
					} else if tc.nativeReason != "" && !omit {
						require.Error(t, err)
						require.Equal(t, http.StatusBadRequest, rec.Code)
						require.Contains(t, rec.Body.String(), "basispoints_unsupported_tools")
						require.Empty(t, upstream.requests)
					} else {
						require.NoError(t, err)
						require.Equal(t, http.StatusOK, rec.Code)
						require.Len(t, upstream.requests, 1)
						require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
						require.Equal(t, "/basispoints/api/responses", GetActualOpenAIUpstreamEndpoint(c))
						if choice != "none" {
							require.Contains(t, string(upstream.lastBody), "Hosted tools unavailable through Basispoints: "+fmt.Sprint(tc.tool["type"]))
							require.Contains(t, string(upstream.lastBody), "Do not claim to have used them")
							require.Contains(t, string(upstream.lastBody), "lookup_client")
						} else {
							require.NotContains(t, string(upstream.lastBody), "Hosted tools unavailable through Basispoints")
							require.NotContains(t, string(upstream.lastBody), "lookup_client")
						}
					}
					entries := logs.FilterMessage("excel_bps.native_fallback").All()
					require.Empty(t, entries)
					require.Empty(t, rec.Header().Get("X-Codex2API-Basispoints-Bypass"))
					require.Empty(t, rec.Header().Get("X-Codex2API-Upstream"))
				})
			}
		}
	}
}

func TestExcelBPSOmitUnsupportedToolsRequiresExplicitOptIn(t *testing.T) {
	var missing *Account
	require.False(t, missing.IsExcelBPSOmitUnsupportedToolsEnabled())
	for _, value := range []any{nil, false, "true", 1, true} {
		account := excelAccount()
		account.Extra[ExcelBPSOmitUnsupportedToolsKey] = value
		require.Equal(t, value == true, account.IsExcelBPSOmitUnsupportedToolsEnabled())
		account.Extra["openai_excel_bps"] = false
		require.False(t, account.IsExcelBPSOmitUnsupportedToolsEnabled())
		account.Extra["openai_excel_bps"] = true
		account.Type = AccountTypeAPIKey
		require.False(t, account.IsExcelBPSOmitUnsupportedToolsEnabled())
	}
}

func TestExcelBPSToolPolicyRespectsModelScope(t *testing.T) {
	for _, models := range [][]string{{}, {"gpt-5.6-sol"}} {
		t.Run(fmt.Sprint(models), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(excelBPSToolPolicyResponse))}}
			account := excelAccount()
			delete(account.Credentials, "client_id")
			account.Extra[ExcelBPSOmitUnsupportedToolsKey] = true
			account.Extra["openai_excel_bps_models"] = models
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			_, err := excelBPSGatewayTestService(upstream).Forward(context.Background(), c, account, []byte("{\"model\":\"gpt-6-astra\",\"input\":\"hello\",\"tools\":[{\"type\":\"web_search\",\"external_web_access\":true}]}"))
			require.NoError(t, err)
			require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
			require.Empty(t, rec.Header().Get("X-Codex2API-Basispoints-Bypass"))
			require.NotContains(t, string(upstream.lastBody), "Hosted tools unavailable through Basispoints")
		})
	}
}
