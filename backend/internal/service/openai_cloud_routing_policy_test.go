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

func TestCloudWSRefreshRejectsProtocolChangesAndPreservesInactiveBPSSettings(t *testing.T) {
	selected := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "native-token"},
		Extra:       map[string]any{OpenAICloudEnabledKey: true, "openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-5.1"}},
	}
	latest := *selected
	latest.Extra = maps.Clone(selected.Extra)
	svc := &OpenAIGatewayService{accountRepo: &codexAccountIdentityRepoStub{account: &latest}, settingService: excelBPSSettingsTestService()}
	latest.Extra["openai_excel_bps_models"] = []string{"gpt-6-luna"}
	got, err := svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-5.1")
	require.NoError(t, err)
	require.Same(t, &latest, got)
	latest.Extra[OpenAICloudEnabledKey] = false
	_, err = svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-5.1")
	require.ErrorIs(t, err, ErrOpenAIWSLocalClientPolicy, "Cloud sessions cannot become BPS/native sessions")
	selected.Extra[OpenAICloudEnabledKey] = false
	latest.Extra[OpenAICloudEnabledKey] = true
	_, err = svc.RefreshOpenAIWSAccount(t.Context(), nil, selected, nil, "gpt-5.1")
	require.ErrorIs(t, err, ErrOpenAIWSLocalClientPolicy, "a stale BPS selection cannot bypass a newly enabled Cloud route")
}
