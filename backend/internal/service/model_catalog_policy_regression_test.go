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
	require.True(t, a.IsModelSupported("gpt-image-novel"), "inventory retirement is not an account permission")
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

func TestAdminAccountEditsCannotOverwriteCatalogPolicy(t *testing.T) {
	policy := map[string]any{"mode": "fixed", "models": []any{}, "excluded": []any{}}
	account := catalogAccount(310, PlatformOpenAI, nil)
	account.Extra = map[string]any{"model_catalog_policy": policy}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: &account}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Extra: map[string]any{
		"model_catalog_policy":     map[string]any{"mode": "legacy"},
		"model_catalog_visibility": map[string]any{"models": map[string]any{"forged": true}},
	}})
	require.NoError(t, err)
	require.NotContains(t, updated.Extra, "model_catalog_policy")
	require.NotContains(t, updated.Extra, "model_catalog_visibility")

	require.NoError(t, svc.UpdateAccountExtra(context.Background(), account.ID, map[string]any{"model_catalog_policy": map[string]any{"mode": "legacy"}, "custom": "value"}))
	persisted := repo.updates[account.ID][len(repo.updates[account.ID])-1]
	require.NotContains(t, persisted, "model_catalog_policy")
}
