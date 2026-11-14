package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSExplicitSelectionMissingListUsesNativeOnlyWithNativeGrant(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, selection := range []string{"missing", "empty", "malformed", "blank"} {
			for _, nativeGrant := range []bool{false, true} {
				name := protocol + "/" + selection + "/excel-only"
				if nativeGrant {
					name = protocol + "/" + selection + "/dual-grant"
				}
				t.Run(name, func(t *testing.T) {
					if protocol == "ws" && nativeGrant {
						testExcelBPSUnselectedNativeWS(t, selection)
						return
					}
					completed, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_native_selection", "status": "completed", "model": "gpt-6-astra", "output": []any{}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1}}})
					require.NoError(t, err)
					upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(completed) + "\n\n"))}}
					svc := excelBPSGatewayTestService(upstream)
					account := excelAccount()
					if nativeGrant {
						account = excelBPSDualGrantTestAccount(t, svc)
					}
					switch selection {
					case "missing":
						delete(account.Extra, "openai_excel_bps_models")
					case "empty":
						account.Extra["openai_excel_bps_models"] = []string{}
					case "malformed":
						account.Extra["openai_excel_bps_models"] = "gpt-6-astra"
					case "blank":
						account.Extra["openai_excel_bps_models"] = []any{" ", 42, false}
					}
					ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
					body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "hello", "stream": protocol != "json"})
					require.NoError(t, err)
					if protocol == "ws" {
						ingressCtx, downstream, _, first := newBPSAdmissionDownstream(t)
						c.Request = c.Request.WithContext(ingressCtx)
						err = svc.ProxyResponsesWebSocketFromClient(ingressCtx, c, downstream, account, account.GetCredential("access_token"), first, nil)
						require.ErrorIs(t, err, ErrOpenAIWSModelSwitchRequiresReconnect, "reject the Excel-only route at ingress, not because a Native fixture is missing authority")
					} else {
						_, err = svc.Forward(ctx, c, account, body)
					}
					if !nativeGrant {
						require.Error(t, err)
						require.Empty(t, upstream.requests, "Excel-only must not acquire Native capability from an empty BPS selection")
						return
					}
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, "chatgpt.com", upstream.lastReq.URL.Host)
					require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
					require.False(t, excelBPSExecutionPinned(ctx), "no BPS execution was selected")
				})
			}
		}
	}
}

func TestExcelBPSExplicitSelectionCatalogDoesNotProbeUnselectedGrant(t *testing.T) {
	for _, manifest := range []bool{false, true} {
		for _, nativeGrant := range []bool{false, true} {
			name := "models/excel-only"
			if manifest {
				name = "manifest/excel-only"
			}
			if nativeGrant {
				name += "/native"
			}
			t.Run(name, func(t *testing.T) {
				source := map[string]any{"models": []any{map[string]string{"slug": "gpt-6-astra"}}}
				raw, err := json.Marshal(source)
				require.NoError(t, err)
				var nativeCalls atomic.Int32
				var nativeVersion atomic.Value
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					nativeCalls.Add(1)
					nativeVersion.Store(r.URL.Query().Get("client_version"))
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(raw)
				}))
				t.Cleanup(server.Close)
				originalURL := chatgptCodexModelsURL
				chatgptCodexModelsURL = server.URL + "/backend-api/codex/models"
				t.Cleanup(func() { chatgptCodexModelsURL = originalURL })
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(raw)))}}
				svc := excelBPSGatewayTestService(upstream)
				account := excelAccount()
				if nativeGrant {
					account = excelBPSDualGrantTestAccount(t, svc)
				}
				delete(account.Extra, "openai_excel_bps_models")
				var result *OpenAIModelsResponse
				if manifest {
					result, err = svc.FetchCodexModelsManifest(context.Background(), account, "9.9.9-explicit", "")
				} else {
					result, err = svc.fetchExcelBPSAccountModels(context.Background(), account)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Empty(t, upstream.requests, "no BPS catalog request is allowed without a selection")
				if nativeGrant {
					require.EqualValues(t, 1, nativeCalls.Load())
					if manifest {
						require.Equal(t, "9.9.9-explicit", nativeVersion.Load())
					}
				} else {
					require.Zero(t, nativeCalls.Load())
					path := "data.#"
					if manifest {
						path = "models.#"
					}
					require.EqualValues(t, 0, gjson.GetBytes(result.Body, path).Int())
					require.NotEmpty(t, result.ETag)
				}
			})
		}
	}
}

func TestExcelBPSExplicitSelectionEmptyCatalogPreservesCancellation(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	account := excelAccount()
	delete(account.Extra, "openai_excel_bps_models")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	list, err := svc.fetchExcelBPSAccountModels(ctx, account)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, list)
	manifest, err := svc.fetchExcelBPSCodexManifest(ctx, account, "", "")
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, manifest)
}

func testExcelBPSUnselectedNativeWS(t *testing.T, selection string) {
	t.Helper()
	f := newWSBridgeCredentialFixture(t, 1)
	f.service.settingService = excelBPSSettingsTestService()
	f.resource.Credentials["chatgpt_account_id"] = "test-account"
	useExcelBPSMixedGrant(t, f.service, f.resource)
	if f.resource.Extra == nil {
		f.resource.Extra = map[string]any{}
	}
	f.resource.Extra["openai_excel_bps"] = true
	switch selection {
	case "missing":
		delete(f.resource.Extra, "openai_excel_bps_models")
	case "empty":
		f.resource.Extra["openai_excel_bps_models"] = []string{}
	case "malformed":
		f.resource.Extra["openai_excel_bps_models"] = "mapped-provider-model"
	case "blank":
		f.resource.Extra["openai_excel_bps_models"] = []any{" ", 42, false}
	}
	f.transport.reply = func(req *http.Request, _ native.TransportOptions) (*http.Response, error) {
		require.Equal(t, "chatgpt.com", req.URL.Host)
		require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
		return bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_native_selection", "answer")), nil
	}
	ctx, c := f.turn(t, t.Context(), 1)
	result, events, err := f.forward(ctx, c, f.resource.GetCredential("access_token"), 1)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, events)
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
	require.False(t, excelBPSExecutionPinned(ctx))
}

func TestExcelBPSExplicitSelectionNativeManifestSurvivesBPSCatalogFailure(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		nativeGrant, unselectedNative, wantError bool
	}{
		{name: "independent native model", nativeGrant: true, unselectedNative: true},
		{name: "selected BPS model never becomes native", nativeGrant: true, wantError: true},
		{name: "Excel-only has no native source", unselectedNative: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models := []any{map[string]string{"slug": "gpt-6-astra"}}
			if tc.unselectedNative {
				models = append(models, map[string]string{"slug": "gpt-6-sol"})
			}
			raw, err := json.Marshal(map[string]any{"models": models})
			require.NoError(t, err)
			var nativeCalls atomic.Int32
			var nativeVersion atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nativeCalls.Add(1)
				nativeVersion.Store(r.URL.Query().Get("client_version"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(raw)
			}))
			t.Cleanup(server.Close)
			originalURL := chatgptCodexModelsURL
			chatgptCodexModelsURL = server.URL + "/backend-api/codex/models"
			t.Cleanup(func() { chatgptCodexModelsURL = originalURL })
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{}"))}}
			svc := excelBPSGatewayTestService(upstream)
			account := excelAccount()
			if tc.nativeGrant {
				account = excelBPSDualGrantTestAccount(t, svc)
			}
			account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
			result, err := svc.FetchCodexModelsManifest(t.Context(), account, "9.9.9-explicit", "")
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.EqualValues(t, 1, gjson.GetBytes(result.Body, "models.#").Int())
				require.Equal(t, "gpt-6-sol", gjson.GetBytes(result.Body, "models.0.slug").String())
				require.NotContains(t, string(result.Body), "gpt-6-astra", "selected BPS models cannot be supplied by Native after discovery fails")
			}
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
			require.Equal(t, "/basispoints/api/responses/access", upstream.lastReq.URL.Path)
			require.True(t, account.IsExcelBPSEnabled(), "catalog dependency failure is not an account revocation")
			if tc.nativeGrant {
				require.EqualValues(t, 1, nativeCalls.Load())
				require.Equal(t, "9.9.9-explicit", nativeVersion.Load())
			} else {
				require.Zero(t, nativeCalls.Load())
			}
		})
	}
}
