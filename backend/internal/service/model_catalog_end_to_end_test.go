//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func (r *catalogAccountRepository) ListByGroup(context.Context, int64) ([]Account, error) {
	return []Account{*r.a}, nil
}

func TestModelCatalogNovelModelDiscoveryPublicationForwardAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// The ID is created at runtime, and cannot be in any compiled model table.
	id := fmt.Sprintf("Opaque-Vendor-%d", time.Now().UnixNano())
	testCatalogModelDiscoveryForwardAndSettlement(t, id)
}

func TestHiddenModelDiscoveryForwardAndSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, id := range []string{"codex-auto-review", "gpt-reserve"} {
		t.Run(id, func(t *testing.T) { testCatalogModelDiscoveryForwardAndSettlement(t, id) })
	}
}

func testCatalogModelDiscoveryForwardAndSettlement(t *testing.T, id string) {
	discovery := fmt.Sprintf(`{"data":[{"id":%q,"model_kind":"chat","reasoning":true,"supported_reasoning_levels":["medium","high"],"default_reasoning_level":"high","input_modalities":["text","image"],"context_window":222222,"endpoints":["responses"]}]}`, id)
	registry, _, a, _ := newCatalogTestService(catalogResponse(200, discovery))
	a.Schedulable = true
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{}}
	_, err := registry.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{id: {InputCostPerToken: 2e-6, OutputCostPerToken: 8e-6, Mode: "chat", ProvidedFields: map[string]bool{"input_cost_per_token": true, "output_cost_per_token": true}}}, localHash: "price-v1"}
	billing := NewBillingService(&config.Config{}, prices)
	resolver := NewModelPricingResolver(nil, billing)
	registry.prices = prices
	registry.pricingResolver = resolver
	channelRepo := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := &GroupModelCatalogService{accounts: registry.accounts, channels: channelRepo, snapshots: registry, registry: registry}
	registry.groupCatalog = catalog
	group := &Group{ID: 101, Platform: PlatformOpenAI, RateMultiplier: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{id}}}
	view, err := catalog.Resolve(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{id}, view.ModelIDs())
	key := &APIKey{ID: 9, UserID: 4, GroupID: &group.ID, Group: group}
	profile, err := registry.SetupProfile(context.Background(), key)
	require.NoError(t, err)
	if isBackgroundCodexModel(id) {
		require.Empty(t, profile.Model)
		require.Empty(t, profile.ReviewModel)
	} else {
		require.Equal(t, id, profile.Model)
		require.Equal(t, "high", profile.ReasoningEffort)
		require.Equal(t, int64(222222), profile.ContextWindow)
	}
	manifest, err := registry.CodexManifest(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, string(manifest), "222222")
	if isBackgroundCodexModel(id) {
		require.Equal(t, "hide", gjson.GetBytes(manifest, "models.0.visibility").String())
	}
	require.NotContains(t, string(manifest), "You are GPT-5")
	gateway := &GatewayService{billingService: billing}
	pinned := gateway.PinRequestPricing(context.Background(), key)
	admitted, err := catalog.Admit(pinned, group, []string{id})
	require.NoError(t, err)
	require.True(t, CatalogAccountAllowed(admitted, a))
	require.False(t, CatalogAccountAllowed(admitted, &Account{ID: 99}))
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			response := fmt.Sprintf(`{"id":"resp_test","object":"response","model":%q,"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}],"status":"completed"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`, id)
			contentType := "application/json"
			if stream {
				response = "data: {\"type\":\"response.completed\",\"response\":" + response + "}\n\n"
				contentType = "text/event-stream"
			}
			recorder := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(response))}}
			forwarder := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: recorder}
			raw := []byte(fmt.Sprintf(`{"model":%q,"reasoning":{"effort":"high"},"stream":%v,"input":[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.com/test.png"}]}]}`, id, stream))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(raw)).WithContext(admitted)
			c.Request.Header.Set("Content-Type", "application/json")
			result, err := forwarder.Forward(admitted, c, a, raw)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, id, gjson.GetBytes(recorder.lastBody, "model").String())
			require.Equal(t, "high", gjson.GetBytes(recorder.lastBody, "reasoning.effort").String())
			require.Contains(t, string(recorder.lastBody), "input_image")
			require.Equal(t, 5, result.Usage.InputTokens)
			cost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: admitted, Model: id, Group: group, RateMultiplier: 1, Resolver: resolver, Tokens: UsageTokens{InputTokens: 5, OutputTokens: 2}})
			require.NoError(t, err)
			require.InDelta(t, 26e-6, cost.ActualCost, 1e-12)
		})
	}
	// A published price update changes new requests, never an in-flight price generation.
	prices.mu.Lock()
	prices.pricingData = map[string]*LiteLLMModelPricing{id: {InputCostPerToken: 4e-6, OutputCostPerToken: 16e-6}}
	prices.localHash = "price-v2"
	prices.mu.Unlock()
	oldCost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: pinned, Model: id, Group: group, RateMultiplier: 1, Resolver: resolver, Tokens: UsageTokens{InputTokens: 5, OutputTokens: 2}})
	require.NoError(t, err)
	require.InDelta(t, 26e-6, oldCost.ActualCost, 1e-12)
	newCtx := gateway.PinRequestPricing(context.Background(), key)
	newCost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: newCtx, Model: id, Group: group, RateMultiplier: 1, Resolver: resolver, Tokens: UsageTokens{InputTokens: 5, OutputTokens: 2}})
	require.NoError(t, err)
	require.InDelta(t, 52e-6, newCost.ActualCost, 1e-12)
	require.NotEqual(t, RequestPricingFromContext(pinned).Revision, RequestPricingFromContext(newCtx).Revision)
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"another"}}
	restricted, err := catalog.Resolve(context.Background(), group)
	require.NoError(t, err)
	require.Empty(t, restricted.ModelIDs())
}

func TestModelCatalogReferencePricesPreserveZeroAndSeparateSales(t *testing.T) {
	raw := json.RawMessage(`{"input_cost_per_token":0,"output_cost_per_token":0.00001,"cache_creation_input_token_cost":0,"long_context_input_token_threshold":12345,"long_context_input_cost_multiplier":2,"long_context_output_cost_multiplier":1.5}`)
	p := catalogPlazaReference(raw, "v1", "pricing_catalog")
	require.NotNil(t, p.InputPrice)
	require.Zero(t, *p.InputPrice)
	require.NotNil(t, p.CacheWritePrice)
	require.Zero(t, *p.CacheWritePrice)
	require.Len(t, p.Intervals, 2)
	require.InDelta(t, 0.000015, *p.Intervals[1].OutputPrice, 1e-12)
	require.Nil(t, catalogPlazaReference(nil, "v1", "pricing_catalog"))
}
