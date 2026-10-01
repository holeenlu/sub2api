//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPlazaResponseBillingKeepsConfiguredAndOfficialPrices(t *testing.T) {
	const model = "gpt-6.1-sol"
	channel := plazaPricedChannel(1, "OpenAI channel", []int64{10}, PlatformOpenAI, model)
	channel.BillingModelSource = BillingModelSourceResponse
	channel.ModelPricing[0].InputPrice = testPtrFloat64(8e-6)
	channel.ModelPricing[0].OutputPrice = testPtrFloat64(32e-6)
	group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.5}
	svc := newPlazaServiceWithBilling([]Channel{channel}, []Group{group}, map[int64]string{10: PlatformOpenAI}, nil)
	svc.catalog = &GroupModelCatalogService{accounts: &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{model: model})}}}
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Models, 1)
	m := &groups[0].Models[0]
	require.NotNil(t, m.OfficialPricing)
	require.InDelta(t, 2e-6, *m.OfficialPricing.InputPrice, 1e-12)
	require.InDelta(t, 10e-6, *m.OfficialPricing.OutputPrice, 1e-12)
	require.InDelta(t, 0.1e-6, *m.OfficialPricing.CacheReadPrice, 1e-12)
	for _, personal := range []*float64{nil, testPtrFloat64(0.25), testPtrFloat64(0)} {
		q := QuotePlazaModel(m, &groups[0], personal, false)
		require.Equal(t, "conditional", q.Status)
		require.Equal(t, "response_model_pricing", q.Reason)
		require.Contains(t, q.Conditions, "response_model")
		require.NotNil(t, q.Pricing)
		require.InDelta(t, 8e-6*q.RateMultiplier, *q.Pricing.InputPrice, 1e-12)
		require.InDelta(t, 32e-6*q.RateMultiplier, *q.Pricing.OutputPrice, 1e-12)
		require.InDelta(t, 2e-6, *m.OfficialPricing.InputPrice, 1e-12, "never multiply official prices")
	}
}

func TestModelPlazaAmbiguousRoutesKeepOnlyPublicOfficialPrice(t *testing.T) {
	group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.5}
	svc := newPlazaServiceWithBilling(nil, []Group{group}, nil, nil)
	svc.catalog = &GroupModelCatalogService{accounts: &catalogAccountRepo{accounts: []Account{
		catalogAccount(1, PlatformOpenAI, map[string]any{"gpt-6.1-sol": "gpt-6-astra", "custom-alias": "gpt-6-astra"}),
		catalogAccount(2, PlatformOpenAI, map[string]any{"gpt-6.1-sol": "gpt-5.5", "custom-alias": "gpt-5.5"}),
	}}}
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	models := plazaModelsByName(groups[0].Models)
	sol := models["gpt-6.1-sol"]
	require.NotNil(t, sol.OfficialPricing)
	require.InDelta(t, 2e-6, *sol.OfficialPricing.InputPrice, 1e-12)
	require.Nil(t, QuotePlazaModel(&sol, &groups[0], nil, false).Pricing)
	require.Nil(t, models["custom-alias"].OfficialPricing, "do not label an upstream target price as a custom alias's official price")
}

func TestModelPlazaDefaultImageBillingMatchesSettlement(t *testing.T) {
	for _, model := range []string{"gpt-image-2.5-flare", "gpt-image-2.5-sunburst"} {
		t.Run(model, func(t *testing.T) {
			group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.5, AllowImageGeneration: true}
			svc := newPlazaServiceWithBilling(nil, []Group{group}, nil, nil)
			svc.catalog = &GroupModelCatalogService{accounts: &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{model: model})}}}
			groups, err := svc.ListGroups(context.Background())
			require.NoError(t, err)
			require.Len(t, groups[0].Models, 1)
			m := groups[0].Models[0]
			require.Equal(t, BillingModeImage, m.Pricing.BillingMode)
			require.Equal(t, "image", m.PriceUnit)
			require.Nil(t, m.ImageTokenPricing)
			require.Len(t, m.Pricing.Intervals, 3)
			gateway := &OpenAIGatewayService{billingService: svc.billingService, resolver: svc.resolver}
			for _, tier := range m.Pricing.Intervals {
				charged := gateway.calculateOpenAIImageCost(context.Background(), model, &APIKey{Group: &group, GroupID: &group.ID}, &OpenAIForwardResult{ImageCount: 1, ImageSize: tier.TierLabel}, 0.5)
				require.InDelta(t, charged.ActualCost, *tier.PerRequestPrice*0.5, 1e-12)
			}
		})
	}
}

func TestModelPlazaExplicitTokenImageBillingStaysToken(t *testing.T) {
	const model = "gpt-image-2.5-flare"
	channel := plazaPricedChannel(1, "images", []int64{10}, PlatformOpenAI, model)
	group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 1, AllowImageGeneration: true}
	svc := newPlazaServiceWithBilling([]Channel{channel}, []Group{group}, map[int64]string{10: PlatformOpenAI}, nil)
	svc.catalog = &GroupModelCatalogService{accounts: &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{model: model})}}}
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups[0].Models, 1)
	require.Equal(t, BillingModeToken, groups[0].Models[0].Pricing.BillingMode)
	require.NotEqual(t, "image", groups[0].Models[0].PriceUnit)
}

func TestModelCatalogDefaultImagePricingDoesNotRequireRemoteTokenPrice(t *testing.T) {
	svc := newPlazaServiceWithBilling(nil, nil, nil, nil)
	catalog := &ModelCatalogService{pricingResolver: svc.resolver}
	group := &Group{ID: 10, Platform: PlatformOpenAI, AllowImageGeneration: true}
	require.Equal(t, "ready", catalog.quoteStatus(context.Background(), &GroupCatalogModel{BillingModels: []string{"gpt-image-2.5-flare"}}, group))
}
