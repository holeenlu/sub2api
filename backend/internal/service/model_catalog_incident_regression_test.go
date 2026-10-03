//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestModelCatalogCannotVetoNativeScheduler(t *testing.T) {
	ctx := context.Background()
	catalog, repo, account, _ := newCatalogTestService()
	setTestModelWhitelist(account, []string{"selected-model"})
	gateway := &OpenAIGatewayService{accountRepo: catalog.accounts, modelCatalog: catalog}
	for _, state := range []string{"unavailable", "stale", "authentication_unavailable", "retired"} {
		t.Run(state, func(t *testing.T) {
			repo.snapshots = map[string]*ModelCatalogSnapshot{modelCatalogSourceKey(account.ID): {
				AccountID: account.ID, Platform: account.Platform, Status: state, LastError: state,
				CheckedAt: time.Now().Add(-8 * 24 * time.Hour), ScopeRevision: modelCatalogScope(account, account),
				Models: []ModelCatalogEntry{{ID: "selected-model", Lifecycle: "retired"}},
			}}
			require.NotNil(t, gateway.resolveFreshSchedulableOpenAIAccountBeforeProfit(ctx, account, PlatformOpenAI, "selected-model", false, ""))
			require.NotNil(t, gateway.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, account, nil, PlatformOpenAI, "selected-model", false, ""))
			require.Nil(t, gateway.resolveFreshSchedulableOpenAIAccountBeforeProfit(ctx, account, PlatformOpenAI, "blocked-model", false, ""))
		})
	}
	account.Status = StatusDisabled
	require.Nil(t, gateway.resolveFreshSchedulableOpenAIAccountBeforeProfit(ctx, account, PlatformOpenAI, "selected-model", false, ""))
}

func TestModelCatalogReadinessUsesRequestPriceGeneration(t *testing.T) {
	id := "incident-price-generation"
	priced := map[string]*LiteLLMModelPricing{id: {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, ProvidedFields: map[string]bool{"input_cost_per_token": true, "output_cost_per_token": true}}}
	for _, initiallyPriced := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-price-cannot-admit-old-request", true: "price-removal-cannot-reject-old-request"}[initiallyPriced], func(t *testing.T) {
			prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
			if initiallyPriced {
				prices.pricingData = priced
			}
			billing := NewBillingService(&config.Config{}, prices)
			group := &Group{ID: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{id}}}
			gateway := &GatewayService{billingService: billing}
			ctx := gateway.PinRequestPricing(context.Background(), &APIKey{GroupID: &group.ID, Group: group})
			prices.pricingData = priced
			if initiallyPriced {
				prices.pricingData = map[string]*LiteLLMModelPricing{}
			}
			catalog := &ModelCatalogService{prices: prices, pricingResolver: NewModelPricingResolver(nil, billing)}
			expected := "unavailable"
			if initiallyPriced {
				expected = "ready"
			}
			require.Equal(t, expected, catalog.quoteStatus(ctx, &GroupCatalogModel{BillingModels: []string{id}}, group))
		})
	}
}
