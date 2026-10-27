package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudHTTPUsesOrdinaryAccountModelWhitelist(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "wildcard alias"}[allow], func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.Account.Extra["openai_excel_bps"] = true
			svc.settingService = excelBPSSettingsTestService()
			in.Account.Credentials["model_mapping"] = map[string]any{"client-*": "gpt-5.1"}
			// A leftover native passthrough toggle must not bypass Cloud's account policy.
			in.Account.Extra["openai_passthrough"] = true
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			model := "gpt-5.1"
			if allow {
				model = "client-model"
				done := make(chan struct{})
				defer func() { cancel(); <-done }()
				go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			body, _ := json.Marshal(map[string]any{"model": model, "input": "hello"})
			result, err := svc.Forward(ctx, c, in.Account, body)
			if allow {
				require.NoError(t, err)
				require.Equal(t, "gpt-5.1", result.UpstreamModel)
				require.Equal(t, "client-model", gjson.Get(recorder.Body.String(), "model").String())
			} else {
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Equal(t, "model_not_allowed", gjson.Get(recorder.Body.String(), "error.code").String())
				require.Nil(t, store.snapshot())
				require.Empty(t, wire.writes)
			}
		})
	}
}

func TestCloudWSChecksChannelModelBeforeAccountMapping(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied", true: "channel and account aliases"}[allow], func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.Account.Extra["openai_excel_bps"] = true
			svc.settingService = excelBPSSettingsTestService()
			in.Account.Credentials["model_mapping"] = map[string]any{"account-model": "gpt-5.1"}
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			if allow {
				done := make(chan struct{})
				defer func() { cancel(); <-done }()
				go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
			}
			ended := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					ended <- err
					return
				}
				defer conn.CloseNow()
				_, first, err := conn.Read(ctx)
				if err != nil {
					ended <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r
				c.Set("api_key", in.APIKey)
				hooks := &OpenAIWSIngressHooks{
					MapRequestModel: func(_ int, original string) (string, error) {
						require.Equal(t, "public-model", original)
						if allow {
							return "account-model", nil
						}
						return "denied-model", nil
					},
					PrepareCloudTurn: func(c context.Context, _ int, _ []byte, _ string) (context.Context, error) {
						return svc.CaptureCloudBillingSnapshot(c, in)
					},
				}
				ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"public-model","input":"hello"}`)))
			if allow {
				for {
					_, raw, err := conn.Read(ctx)
					require.NoError(t, err)
					require.NotEqual(t, "error", gjson.GetBytes(raw, "type").String(), string(raw))
					if gjson.GetBytes(raw, "type").String() == "response.completed" {
						require.Equal(t, "public-model", gjson.GetBytes(raw, "response.model").String())
						break
					}
				}
				require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
			}
			select {
			case err := <-ended:
				if allow {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, ErrOpenAIWSLocalClientPolicy)
					require.Nil(t, store.snapshot())
					require.Empty(t, wire.writes)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestCloudWSRejectsAccountMappingChangedAfterNormalization(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	selected := *in.Account
	selected.Credentials = map[string]any{}
	for key, value := range in.Account.Credentials {
		selected.Credentials[key] = value
	}
	in.Account.Credentials["model_mapping"] = map[string]any{"new-model": "gpt-5.1"}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	c.Set("api_key", in.APIKey)
	_, err := svc.proxyOpenAIWSCloudTurn(t.Context(), c, &selected, []byte(`{"model":"gpt-5.1","input":"hello"}`), "client-model", func([]byte) error { return nil })
	require.Error(t, err)
	require.Nil(t, store.snapshot())
	require.Empty(t, wire.writes)
}

func TestCloudModelsUseActualDiscoveryAndOrdinaryGroupProjection(t *testing.T) {
	svc, in, _, _ := cloudForwardFixture(t, nil)
	account := in.Account
	account.Status, account.Schedulable = StatusActive, true
	account.Credentials["model_mapping"] = map[string]any{"allowed-alias": "gpt-5.1", "not-in-group": "gpt-5.1", "other-*": "gpt-6-luna"}
	account.Extra["openai_passthrough"] = true
	calls := 0
	svc.cloudService.SetModelDiscovery(func(context.Context, *OpenAICloudAccess) ([]string, error) {
		calls++
		return []string{"gpt-5.1", "gpt-6-luna", "not-mapped"}, nil
	})
	group := &Group{ID: 22, Platform: PlatformOpenAI, CodexModelsManifestConfig: GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{account.ID}}, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"allowed-alias", "other-small"}}}
	svc.accountRepo = splitCodexModelsAccountRepo{all: map[int64][]Account{22: {*account}}}
	response, _, err := svc.FetchPinnedOpenAIModelsList(t.Context(), group, 1, "")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body, &result))
	require.Len(t, result.Data, 2)
	require.Equal(t, "allowed-alias", result.Data[0].ID)
	require.Equal(t, "other-small", result.Data[1].ID)
	second, err := svc.FetchOpenAIModelsList(t.Context(), account)
	require.NoError(t, err)
	require.NotEmpty(t, second.Body)
	require.Equal(t, 1, calls, "Cloud discovery uses the shared model catalog cache")
	// A different proxy is an independent upstream context and must not reuse
	// the earlier catalog or conceal its own discovery failure.
	account.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 7890}
	unavailable := errors.New("catalog unavailable")
	svc.cloudService.SetModelDiscovery(func(context.Context, *OpenAICloudAccess) ([]string, error) { return nil, unavailable })
	_, err = svc.FetchOpenAIModelsList(t.Context(), account)
	require.ErrorIs(t, err, unavailable, "catalog failures cannot fall back to historical test model names")
}

func TestCloudSelectionPreservesAndRestoresBPSModelRouting(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "native-token", "model_mapping": map[string]any{"bps-alias": "gpt-5.1", "native-alias": "gpt-6-luna"}},
		Extra:       map[string]any{OpenAICloudEnabledKey: true, "openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-5.1"}},
	}
	require.True(t, account.IsOpenAICloudEnabled())
	require.False(t, account.IsExcelBPSEnabledForModel("bps-alias"))
	require.False(t, excelBPSHasNativeFallback(account))
	savedDigest := cloudSettingsDigest(account)
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-luna"}
	require.Equal(t, savedDigest, cloudSettingsDigest(account), "editing inactive BPS settings cannot change Cloud session ownership")
	account.Extra["openai_excel_bps_models"] = []string{"gpt-5.1"}
	account.Extra[OpenAICloudEnabledKey] = false
	require.True(t, account.Extra["openai_excel_bps"].(bool), "disabling Cloud preserves the saved BPS switch")
	require.True(t, account.IsExcelBPSEnabledForModel("bps-alias"), "BPS still matches the account-mapped upstream model")
	require.False(t, account.IsExcelBPSEnabledForModel("native-alias"), "unselected models retain the native route")
	require.True(t, excelBPSHasNativeFallback(account))
}

func TestCloudSelectionCannotEnterBPSRecovery(t *testing.T) {
	svc := &OpenAIGatewayService{settingService: excelBPSSettingsTestService()}
	account := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "native-token"},
		Extra:       map[string]any{OpenAICloudEnabledKey: true, "openai_excel_bps": true},
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	for _, nativePhase := range []bool{false, true} {
		budget := NewOpenAITurnBudget(nil)
		budget.bpsRecovery = excelBPSPoolRecovery{managed: true, native: nativePhase, rejected: map[int64]bool{7: true}}
		ctx := WithOpenAITurnBudget(t.Context(), budget)
		got, eligible := svc.excelBPSPoolCandidate(ctx, account, "gpt-5.1", false)
		require.False(t, eligible, "BPS recovery must not borrow a Cloud-selected account")
		require.Same(t, account, got)
		require.Same(t, account, excelBPSPoolForwardAccount(ctx, c, account))
	}
	require.Nil(t, excelBPSNativeFallbackAccount(t.Context(), c, account, []byte(`{"model":"gpt-5.1","input":"hello"}`), newExcelBPSRateLimitedFailoverError("30")))
	require.Equal(t, true, account.Extra["openai_excel_bps"])
	account.Status, account.Schedulable = StatusActive, true
	account.Extra["openai_excel_bps"] = false
	account.Extra["openai_excel_bps_auto_disable_on_403"] = true
	account.Extra[ExcelBPSAutoRecoverOn403Key] = true
	account.Extra[ExcelBPS403DisabledAtKey] = time.Now().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	require.False(t, account.ExcelBPS403RecoveryDue(time.Now()), "automatic BPS recovery stays suspended while Cloud is selected")
	account.Extra[OpenAICloudEnabledKey] = false
	require.True(t, account.ExcelBPS403RecoveryDue(time.Now()), "disabling Cloud restores the existing BPS recovery policy")
}

func TestCloudWSRefreshRebuildsHistoryAcrossProtocolChanges(t *testing.T) {
	for _, direction := range [][2]string{{"native", "cloud"}, {"bps", "cloud"}, {"cloud", "native"}, {"cloud", "bps"}} {
		t.Run(direction[0]+"_to_"+direction[1], func(t *testing.T) {
			selected := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"access_token": "native-token", "chatgpt_account_id": "workspace", "chatgpt_user_id": "user"},
				Extra:       map[string]any{OpenAICloudEnabledKey: direction[0] == "cloud", "openai_excel_bps": direction[0] == "bps", "openai_excel_bps_models": []string{"gpt-5.1"}}}
			latest := *selected
			latest.Extra = maps.Clone(selected.Extra)
			latest.Extra[OpenAICloudEnabledKey] = direction[1] == "cloud"
			latest.Extra["openai_excel_bps"] = direction[1] == "bps"
			svc := &OpenAIGatewayService{accountRepo: &codexAccountIdentityRepoStub{account: &latest}, settingService: excelBPSSettingsTestService()}
			got, err := svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-5.1")
			var changed *OpenAIWSAccountChangedError
			require.ErrorAs(t, err, &changed, "a configured protocol change must reuse native account refresh")
			require.Same(t, &latest, got)
			require.True(t, changed.PreserveContext, "same OAuth identity can carry its original history")
			payload, err := json.Marshal(map[string]any{"type": "response.create", "model": "gpt-5.1", "store": true, "previous_response_id": "resp_previous_protocol", "input": []any{cloudUserMessage("next")}})
			require.NoError(t, err)
			original, err := json.Marshal([]any{cloudUserMessage("old"), map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "original-opaque-history"}, cloudUserMessage("next")})
			require.NoError(t, err)
			var history []json.RawMessage
			require.NoError(t, json.Unmarshal(original, &history))
			retry := svc.openAIWSAccountRefreshRetry(selected, changed, payload, history, true, "gpt-5.1")
			body, ok := OpenAIWSCurrentTurnRetryPayload(retry)
			require.True(t, ok, "%v", retry)
			require.False(t, gjson.GetBytes(body, "previous_response_id").Exists(), "a foreign protocol response ID is never portable")
			require.JSONEq(t, string(original), gjson.GetBytes(body, "input").Raw)
			incomplete := svc.openAIWSAccountRefreshRetry(selected, changed, payload, nil, false, "gpt-5.1")
			require.ErrorIs(t, incomplete, ErrOpenAIWSReplayUnavailable, "store=true does not make an unexpanded foreign previous portable")
		})
	}
}

func TestCloudWSRefreshPreservesInactiveBPSSettings(t *testing.T) {
	selected := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "native-token"},
		Extra:       map[string]any{OpenAICloudEnabledKey: true, "openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-5.1"}}}
	latest := *selected
	latest.Extra = maps.Clone(selected.Extra)
	latest.Extra["openai_excel_bps_models"] = []string{"gpt-6-luna"}
	svc := &OpenAIGatewayService{accountRepo: &codexAccountIdentityRepoStub{account: &latest}, settingService: excelBPSSettingsTestService()}
	got, err := svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-5.1")
	require.NoError(t, err)
	require.Same(t, &latest, got)
}

// Cloud admission runs again for later frames. A local preparation failure must
// release the per-turn drain context and must not count against upstream health.
func TestCloudWSFollowupPreparationFailureIsLocalAndClosesDrain(t *testing.T) {
	for _, name := range []string{"missing owner", "unknown continuation", "missing billing hook", "billing failure"} {
		t.Run(name, func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ended := make(chan error, 1)
			prepared := make(chan context.Context, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					ended <- err
					return
				}
				defer conn.CloseNow()
				_, first, err := conn.Read(ctx)
				if err != nil {
					ended <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r
				if name != "missing owner" {
					c.Set("api_key", in.APIKey)
				}
				hooks := &OpenAIWSIngressHooks{}
				if name == "billing failure" {
					hooks.PrepareCloudTurn = func(turnCtx context.Context, turn int, _ []byte, _ string) (context.Context, error) {
						prepared <- turnCtx
						return turnCtx, errors.New("local pricing unavailable")
					}
				}
				ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"client-model","generate":false,"input":"warmup"}`)))
			for {
				_, raw, err := conn.Read(ctx)
				require.NoError(t, err)
				if gjson.GetBytes(raw, "type").String() == "response.completed" {
					break
				}
			}
			body := map[string]any{"type": "response.create", "model": "client-model", "input": "next"}
			if name == "unknown continuation" {
				// Avoid the generic unknown previous-response guard: Cloud call
				// references are resolved by the Cloud preparation path itself.
				body["input"] = []any{map[string]any{"type": "function_call_output", "call_id": "call_cloud_expired", "output": "tool result"}}
			}
			follow, err := json.Marshal(body)
			require.NoError(t, err)
			require.NoError(t, conn.Write(ctx, coderws.MessageText, follow))
			if name == "unknown continuation" {
				// A stale tool reply is a per-turn public error, like a native
				// previous_response_not_found; the connection stays usable.
				_, raw, err := conn.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, "error", gjson.GetBytes(raw, "type").String(), string(raw))
				require.Equal(t, "cloud_tool_call_expired", gjson.GetBytes(raw, "error.code").String(), string(raw))
				require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
				select {
				case err := <-ended:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			} else {
				select {
				case err := <-ended:
					require.ErrorIs(t, err, ErrOpenAIWSLocalClientPolicy)
					var closeErr *OpenAIWSClientCloseError
					require.ErrorAs(t, err, &closeErr)
					require.Equal(t, coderws.StatusInternalError, closeErr.StatusCode())
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if name == "billing failure" {
				turnCtx := <-prepared
				require.ErrorIs(t, turnCtx.Err(), context.Canceled)
			}
			require.Nil(t, store.snapshot())
			require.Empty(t, wire.writes)
		})
	}
}

func TestCloudWSBridgePreservesBudgetAndRejectionCauses(t *testing.T) {
	for _, name := range []string{"attempt budget", "first output deadline", "client cancellation", "settings rejection"} {
		t.Run(name, func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			budget := NewOpenAITurnBudget(svc.cfg)
			want := ErrOpenAITurnAttemptBudgetExhausted
			switch name {
			case "attempt budget":
				budget.attempts = budget.limit
			case "first output deadline":
				budget.deadline = time.Now().Add(-time.Second)
				want = ErrOpenAIWSFirstOutputBudgetExhausted
			case "client cancellation":
				want = context.Canceled
			case "settings rejection":
				in.Account.Extra[OpenAICloudEnabledKey] = false
				want = ErrOpenAICloudSettings
			}
			ctx, cancel := context.WithCancel(WithOpenAITurnBudget(t.Context(), budget))
			defer cancel()
			if name == "client cancellation" {
				cancel()
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			var events [][]byte
			result, err := svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`), "client-model", func(raw []byte) error { events = append(events, append([]byte(nil), raw...)); return nil })
			require.ErrorIs(t, err, want)
			require.Nil(t, result)
			if name == "settings rejection" {
				var rejected *openAIWSTurnRejectedError
				require.ErrorAs(t, err, &rejected)
				require.Len(t, events, 1)
				require.Equal(t, "cloud_settings_invalid", gjson.GetBytes(events[0], "error.code").String())
			} else {
				require.Empty(t, events, "the common WS lifecycle owns budget/cancellation handling")
			}
			require.Nil(t, store.snapshot())
			require.Empty(t, wire.writes)
		})
	}
}

func TestCloudWSProtocolMigrationPreservesPendingOwner(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	_, history, responseID := h.establish()
	historyJSON, _ := json.Marshal(append(history, cloudUserMessage("next")))
	var fullInput []json.RawMessage
	require.NoError(t, json.Unmarshal(historyJSON, &fullInput))
	request, _ := json.Marshal(map[string]any{"model": "client-model", "previous_response_id": responseID, "input": "next"})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Set("api_key", h.in.APIKey)
	sub := h.store.all()[0]
	for _, status := range []string{"completed", "awaiting_tools", "running", "closed"} {
		t.Run(status, func(t *testing.T) {
			sub.mu.Lock()
			sub.record.Status = status
			sub.mu.Unlock()
			err := h.svc.validateCloudWSProtocolMigration(h.ctx, c, request, fullInput, true)
			if status == "awaiting_tools" || status == "running" {
				require.ErrorIs(t, err, errCloudAuthorizationChanged)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, h.store.settlementCount(), "a migration check has no billing side effects")
		})
	}
	require.ErrorIs(t, h.svc.validateCloudWSProtocolMigration(h.ctx, c, request, fullInput, false), errCloudPreviousResponseNotFound)
	// The current delta has no call reference: it is only in verified shadow history.
	stale, _ := json.Marshal(map[string]any{"model": "client-model", "previous_response_id": "resp_cloud_missing", "input": "next"})
	for _, status := range []string{"running", "awaiting_tools"} {
		sub.mu.Lock()
		sub.record.Status = status
		sub.mu.Unlock()
		require.ErrorIs(t, h.svc.validateCloudWSProtocolMigration(h.ctx, c, stale, fullInput, true), errCloudAuthorizationChanged, "an expired previous does not authorize migration of another active RPC")
	}

}

func TestCloudWSProtocolMigrationRejectsUnansweredLiveCall(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	ready := make(chan *cloudRPCFakeConn, 1)
	go func() {
		wire := h.nextWire()
		if wire == nil {
			return
		}
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		cloudTestRead(wire, map[string]any{"id": "live-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "live-upstream", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}})
		ready <- wire
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "input": "first user", "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	wire := <-ready
	call := response.Get("output.#(type==\"function_call\")")
	require.NotEmpty(t, call.Get("call_id").String())
	require.Equal(t, "awaiting_tools", h.store.all()[0].snapshot().Status)
	history := []any{cloudUserMessage("first user"), call.Value(), cloudUserMessage("switch protocol")}
	raw, err := json.Marshal(map[string]any{"model": "client-model", "input": history})
	require.NoError(t, err)
	encoded, _ := json.Marshal(history)
	var full []json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &full))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Set("api_key", h.in.APIKey)
	require.ErrorIs(t, h.svc.validateCloudWSProtocolMigration(h.ctx, c, raw, full, true), errCloudAuthorizationChanged)
	require.Empty(t, wire.writes, "migration must not reply, interrupt, or replay the pending tool")
	require.Equal(t, 0, h.store.settlementCount())
	require.Equal(t, "awaiting_tools", h.store.all()[0].snapshot().Status)
}

// A stale scheduler snapshot must not reacquire credentials for a disabled
// account, nor emit a response before the ordinary recovery loop can switch.
func TestCloudIncidentDisabledAccountUsesNativeFailover(t *testing.T) {
	for _, state := range []string{"inactive", "unschedulable", "expired"} {
		for _, transport := range []string{"json", "sse", "ws"} {
			t.Run(state+"/"+transport, func(t *testing.T) {
				svc, in, store, wire := cloudForwardFixture(t, nil)
				cached := *in.Account
				cached.Status = StatusActive
				cached.Schedulable = true
				in.Account.Status = StatusActive
				in.Account.Schedulable = true
				switch state {
				case "inactive":
					in.Account.Status = "inactive"
				case "unschedulable":
					in.Account.Schedulable = false
				case "expired":
					at := time.Now().Add(-time.Minute)
					in.Account.AutoPauseOnExpired = true
					in.Account.ExpiresAt = &at
				}
				svc.cloudService.tokens = nil // A disabled account must fail before token access.
				body, _ := json.Marshal(map[string]any{"model": "client-model", "input": "hello", "stream": transport != "json"})
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				var result *OpenAIForwardResult
				var err error
				if transport == "ws" {
					result, err = svc.proxyOpenAIWSCloudTurn(t.Context(), c, &cached, body, "client-model", func([]byte) error { t.Error("credential failure was emitted before scheduler recovery"); return nil })
				} else {
					result, err = svc.forwardOpenAICloud(t.Context(), c, &cached, body)
				}
				require.Nil(t, result)
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.True(t, failure.RequestNotSent)
				require.False(t, failure.RetryableOnSameAccount)
				require.True(t, failure.ShouldRetryNextAccount())
				events := c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
				require.NotEmpty(t, events)
				require.Equal(t, "account_auth", events[0].Stage)
				require.Equal(t, cached.ID, events[0].AccountID)
				require.NotEmpty(t, events[0].Reason)
				require.Contains(t, events[0].Detail, "cloud_authorization_failed")

				require.Equal(t, GatewayFailureStageAccountAuth, failure.Stage)
				require.False(t, c.Writer.Written())
				require.Empty(t, wire.writes)
				require.Nil(t, store.snapshot())
				_, blocked := svc.openaiAccountRuntimeBlockUntil.Load(cached.ID)
				require.True(t, blocked)
			})
		}
	}
}
