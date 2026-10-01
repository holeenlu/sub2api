//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelCatalogUnrestrictedSupplyPreservesAliases(t *testing.T) {
	s, _, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-new-a"},{"id":"gpt-image-new-b"}]}`))
	a.Credentials["model_mapping"] = map[string]any{"public-image": "gpt-image-new-a"}
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{}}
	_, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	ids, _, ok := s.ModelCatalogSnapshot(context.Background(), a)
	require.True(t, ok)
	require.ElementsMatch(t, []string{"gpt-image-new-a", "gpt-image-new-b", "public-image"}, ids)
	require.True(t, s.accountRouteAllowed(context.Background(), a, "public-image"))
	a.Credentials["api_key"] = "changed-credential"
	require.True(t, s.accountRouteAllowed(context.Background(), a, "public-image"), "declared supply is independent of snapshot freshness")
}

func TestModelCatalogOpenAIMultiplierUsesAdmittedRevision(t *testing.T) {
	group := &Group{ID: 11, RateMultiplier: 0.37}
	key := &APIKey{UserID: 9, GroupID: &group.ID, Group: group}
	gateway := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	ctx := gateway.PinRequestPricing(context.Background(), key)
	openai := &OpenAIGatewayService{}
	require.Equal(t, 0.37, openai.ResolveUserGroupRateMultiplier(ctx, 9, 11, 3))
	require.Equal(t, 3.0, openai.ResolveUserGroupRateMultiplier(ctx, 10, 11, 3), "another user's rate cannot reuse this snapshot")
}

func TestModelCatalogSupplementDoesNotCrossProviderNamespace(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://provider.example/v1"}}
	e := ModelCatalogEntry{Platform: PlatformOpenAI, UpstreamNamespace: "https://another.example/v1"}
	require.False(t, catalogRegistryApplies(e, account, account))
	e.UpstreamNamespace = "https://provider.example/v1"
	require.True(t, catalogRegistryApplies(e, account, account))
	e.SourceAccountID = 2
	require.False(t, catalogRegistryApplies(e, account, account))
}

type catalogSettingsMemory struct {
	SettingRepository
	values map[string]string
}

func (r *catalogSettingsMemory) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}
func (r *catalogSettingsMemory) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func TestModelCatalogTrustedRetirementAppliesWithoutRestart(t *testing.T) {
	s, _, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-novel"}]}`))
	_, err := s.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	s.settings = &SettingService{settingRepo: &catalogSettingsMemory{values: map[string]string{}}}
	require.NoError(t, setCatalogRegistryFixture(context.Background(), s, []ModelCatalogEntry{{ID: "gpt-image-novel", Platform: PlatformOpenAI, Kind: "image", Lifecycle: "retired", UpstreamNamespace: "https://upstream.example/v1"}}))
	snapshot, err := s.Account(context.Background(), a)
	require.NoError(t, err)
	require.Equal(t, "retired", snapshot.Models[0].Lifecycle)
	ids, _, ok := s.ModelCatalogSnapshot(context.Background(), a)
	require.True(t, ok)
	require.Empty(t, ids)
}

func TestModelCatalogMissingCachePriceIsNotExplicitZero(t *testing.T) {
	id := "price-presence-example"
	price := &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, ProvidedFields: map[string]bool{"input_cost_per_token": true, "output_cost_per_token": true}}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{id: price}}
	billing := NewBillingService(&config.Config{}, prices)
	resolver := NewModelPricingResolver(nil, billing)
	group := &Group{ID: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true}}
	require.False(t, catalogUsagePriced(context.Background(), resolver, billing, group, id, UsageTokens{CacheReadTokens: 100}))
	price.ProvidedFields["cache_read_input_token_cost"] = true
	require.True(t, catalogUsagePriced(context.Background(), resolver, billing, group, id, UsageTokens{CacheReadTokens: 100}), "explicit zero remains a real price")
	require.False(t, catalogUsagePriced(context.Background(), resolver, billing, group, "unknown-response-id", UsageTokens{InputTokens: 1, OutputTokens: 1}))
}

func TestModelCatalogCacheOverrideDoesNotInventOutputPricing(t *testing.T) {
	id := "partial-price-example"
	zero := 0.0
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{id: {InputCostPerToken: 1e-6, SupportsPromptCaching: true, ProvidedFields: map[string]bool{"input_cost_per_token": true}}}}
	billing := NewBillingService(&config.Config{}, prices)
	resolver := NewModelPricingResolver(nil, billing)
	registry := &ModelCatalogService{prices: prices, pricingResolver: resolver}
	group := &Group{ID: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true}, ModelPricing: []ChannelModelPricing{{Models: []string{id}, CacheReadPrice: &zero}}}
	require.Equal(t, "unavailable", registry.quoteStatus(context.Background(), &GroupCatalogModel{BillingModels: []string{id}}, group))
}

func TestModelCatalogOAuthRotationPreservesPrincipalScope(t *testing.T) {
	token := func(subject, scope string, expiry int) string {
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"iss":"https://auth.openai.com","sub":%q,"scope":%q,"exp":%d,"iat":%d}`, subject, scope, expiry, expiry-10))) + ".signature"
	}
	a := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": token("principal-a", "models", 100), "refresh_token": "old", "expires_at": "old"}}
	first := modelCatalogScope(a, a)
	a.Credentials["access_token"] = token("principal-a", "models", 200)
	a.Credentials["refresh_token"] = "rotated"
	a.Credentials["expires_at"] = "new"
	require.Equal(t, first, modelCatalogScope(a, a))
	a.Credentials["access_token"] = token("principal-a", "limited", 200)
	require.NotEqual(t, first, modelCatalogScope(a, a))
	a.Credentials["access_token"] = token("principal-b", "models", 200)
	require.NotEqual(t, first, modelCatalogScope(a, a))
}

func TestModelCatalogFixedPolicyDoesNotUnionStaleWhitelist(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"stale": "stale", "alias": "allowed"})
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"allowed"}}}
	require.True(t, a.IsModelSupported("allowed"))
	require.True(t, a.IsModelSupported("alias"))
	require.False(t, a.IsModelSupported("stale"), "stale credentials whitelist must not expand the fixed policy")
	a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{}
	for _, platform := range []string{PlatformOpenAI, PlatformGrok, PlatformAntigravity} {
		a.Platform = platform
		require.True(t, a.IsModelSupported("alias"), "empty account selection accepts all models")
		for model := range a.GetModelMapping() {
			require.True(t, a.IsModelSupported(model))
		}
	}
}

func TestAdminAccountPolicyValidatedWithCredentials(t *testing.T) {
	account := catalogAccount(311, PlatformOpenAI, map[string]any{"stale": "stale"})
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: &account}}
	svc := &adminServiceImpl{accountRepo: repo}
	policy := ModelCatalogPolicy{Models: []string{"new-model"}}
	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{
		Credentials: map[string]any{"model_mapping": map[string]any{"alias": "new-model"}}, ModelCatalogPolicy: &policy,
	})
	require.NoError(t, err)
	require.True(t, updated.IsModelSupported("alias"))
	require.False(t, updated.IsModelSupported("stale"))
	require.ElementsMatch(t, []string{"new-model"}, configuredUpstreamModelsForCapabilitySync(updated))
	invalid := ModelCatalogPolicy{Models: []string{"bad\nmodel"}}
	_, err = svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{ModelCatalogPolicy: &invalid})
	require.ErrorContains(t, err, "invalid model pattern")
	require.Equal(t, policy.Models, accountModelCatalogPolicy(updated).Models)
}

func TestAdminAccountEditsCannotOverwriteCatalogPolicy(t *testing.T) {
	policy := map[string]any{"mode": "fixed", "models": []any{}, "excluded": []any{}}
	account := catalogAccount(310, PlatformOpenAI, nil)
	account.Extra = map[string]any{ModelCatalogPolicyExtraKey: policy}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: &account}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Extra: map[string]any{
		ModelCatalogPolicyExtraKey: map[string]any{"mode": "legacy"},
		"model_catalog_visibility": map[string]any{"models": map[string]any{"forged": true}},
	}})
	require.NoError(t, err)
	require.Equal(t, policy, updated.Extra[ModelCatalogPolicyExtraKey])
	require.NotContains(t, updated.Extra, "model_catalog_visibility")

	require.NoError(t, svc.UpdateAccountExtra(context.Background(), account.ID, map[string]any{ModelCatalogPolicyExtraKey: map[string]any{"mode": "legacy"}, "custom": "value"}))
	persisted := repo.updates[account.ID][len(repo.updates[account.ID])-1]
	require.NotContains(t, persisted, ModelCatalogPolicyExtraKey)
}
