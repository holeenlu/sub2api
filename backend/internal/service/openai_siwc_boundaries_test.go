package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestSIWCUnsupportedEndpointsNeverAcquireOrSendCredentials(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, endpoint := range []string{"images", "embeddings", "alpha/search"} {
			t.Run(kind+"/"+endpoint, func(t *testing.T) {
				a := &Account{ID: 4, Platform: PlatformOpenAI, Type: kind, Extra: map[string]any{OpenAISIWCEnabledKey: true}, Credentials: map[string]any{"access_token": "native-secret", "api_key": "native-key"}}
				s := &OpenAIGatewayService{} // Any credential/network dependency would panic.
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, nil)
				var err error
				switch endpoint {
				case "images":
					_, err = s.ForwardImages(context.Background(), c, a, nil, nil, "")
				case "embeddings":
					_, err = s.ForwardEmbeddings(context.Background(), c, a, nil, "")
				case "alpha/search":
					_, err = s.ForwardAlphaSearch(context.Background(), c, a, nil)
				}
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Contains(t, rec.Body.String(), "OPENAI_SIWC_UNSUPPORTED_ENDPOINT")
				require.NotContains(t, rec.Body.String(), "native-secret")
				require.NotContains(t, rec.Body.String(), "native-key")
			})
		}
	}
}

func TestSIWCAccountSettingsRequireExplicitBooleanAndEligibleOwner(t *testing.T) {
	for _, raw := range []any{true, false, "true", float64(1), nil} {
		a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAISIWCEnabledKey: raw}}
		err := validateSIWCAccountSettings(a)
		_, boolean := raw.(bool)
		require.Equal(t, boolean, err == nil)
	}
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{OpenAISIWCEnabledKey: true}}
	require.True(t, a.IsSIWCEnabled(), "an invalidated selection must not silently fall back")
	require.Error(t, validateSIWCAccountSettings(a))
	a.Extra[OpenAISIWCEnabledKey] = false
	require.NoError(t, validateSIWCAccountSettings(a), "administrator can explicitly leave SIWC")
}

func TestSIWCResponsesPreservesWebSearchAndVisionOnHTTPAndClientWS(t *testing.T) {
	for _, ws := range []bool{false, true} {
		t.Run(fmt.Sprintf("client_ws=%t", ws), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: siwcWSResponse(`{"type":"response.completed","response":{"id":"resp_vision_search","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`)}
			svc, _, account := siwcWSFixture(t, upstream)
			payload := `{"model":"gpt-6.1-sol","input":[{"role":"user","content":[{"type":"input_text","text":"Explain this picture and search the web"},{"type":"input_image","image_url":"data:image/png;base64,cGljdHVyZQ==","detail":"high"}]}],"tools":[{"type":"web_search","search_context_size":"low"}],"stream":true}`
			if ws {
				request, err := sjson.Set(payload, "type", "response.create")
				require.NoError(t, err)
				turns := make(chan bpsWSTestTurn, 1)
				client, done := startOpenAIWSMemorySession(t, svc, account, nil, request, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
				readNativeWSTestTurn(t, client)
				require.NoError(t, (<-turns).err)
				requireNativeWSTestServerExit(t, client, done)
			} else {
				_, c := siwcHTTPTestContext(t, []byte(payload))
				_, err := svc.Forward(t.Context(), c, account, []byte(payload))
				require.NoError(t, err)
			}
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "web_search", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
			require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "tools.0.search_context_size").String())
			require.Equal(t, "data:image/png;base64,cGljdHVyZQ==", gjson.GetBytes(upstream.lastBody, "input.0.content.1.image_url").String())
			require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "input.0.content.1.detail").String())
			require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
		})
	}
}

func TestSIWCMixedPoolSkipsUnsupportedEndpointsBeforeSelection(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	native := Account{ID: 49201, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1}
	siwc := native
	siwc.ID = 49202
	siwc.Priority = -100 // Prefer SIWC if the capability filter is missing.
	siwc.Extra = map[string]any{OpenAISIWCEnabledKey: true}
	for _, endpoint := range []OpenAIEndpointCapability{"", OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityChatCompletions} {
		require.True(t, siwc.SupportsOpenAIEndpointCapability(endpoint))
	}
	for _, endpoint := range []OpenAIEndpointCapability{OpenAIEndpointCapabilityAlphaSearch, OpenAIEndpointCapabilityEmbeddings, OpenAIEndpointCapabilityLive, OpenAIEndpointCapabilitySeedance} {
		require.False(t, siwc.SupportsOpenAIEndpointCapability(endpoint))
	}
	for _, capability := range []OpenAIImagesCapability{OpenAIImagesCapabilityBasic, OpenAIImagesCapabilityNative, OpenAIImagesCapabilityAPIKey} {
		require.False(t, siwc.SupportsOpenAIImageCapability(capability))
	}
	require.Zero(t, openAICompactSupportTier(&siwc))
	groupID := int64(49200)
	for _, route := range []string{"alpha/search", "images", "live", "compact"} {
		t.Run(route, func(t *testing.T) {
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{siwc, native}},
				cache:       &schedulerTestGatewayCache{}, cfg: &config.Config{},
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}
			var selection *AccountSelectionResult
			var err error
			if route == "images" {
				selection, _, err = svc.SelectAccountWithSchedulerForImages(t.Context(), &groupID, "", "gpt-image-2", nil, OpenAIImagesCapabilityNative)
			} else {
				capability := OpenAIEndpointCapabilityAlphaSearch
				if route == "live" {
					capability = OpenAIEndpointCapabilityLive
				} else if route == "compact" {
					capability = OpenAIEndpointCapabilityResponses
				}
				selection, _, err = svc.SelectAccountWithSchedulerForCapability(t.Context(), &groupID, "", "", "", nil, OpenAIUpstreamTransportHTTPSSE, capability, route == "compact", false, false)
			}
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, native.ID, selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestSIWCRecoveryExclusionCannotBeOverriddenByNativeHint(t *testing.T) {
	ctx := EnsureOpenAITurnBudget(t.Context(), nil)
	account := &Account{ID: 49301, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{OpenAISIWCEnabledKey: true}}
	ExcludeTurnRecoveryAccount(ctx, account, &UpstreamFailoverError{StatusCode: http.StatusUnauthorized})
	for _, hint := range []string{"native", "bps", UsageAuthorizationModeSIWC} {
		require.False(t, turnAccountEligible(ctx, account, hint))
	}
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}}
	allowed, reason := scheduler.isAccountRequestCompatibleReason(ctx, account, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI})
	require.False(t, allowed)
	require.Equal(t, "turn_recovery_target", reason)
}
