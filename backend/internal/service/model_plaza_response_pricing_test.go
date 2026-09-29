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
