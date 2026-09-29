//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelQuotePersonalMediaAndZeroPrices(t *testing.T) {
	for _, tc := range []struct {
		mode     BillingMode
		expected float64
	}{{BillingModeToken, 0.25}, {BillingModeImage, 0.3}, {BillingModeVideo, 0.4}} {
		t.Run(string(tc.mode), func(t *testing.T) {
			p := &ChannelModelPricing{BillingMode: tc.mode, InputPrice: testPtrFloat64(0), OutputPrice: testPtrFloat64(2), PerRequestPrice: testPtrFloat64(3), Intervals: []PricingInterval{{PerRequestPrice: testPtrFloat64(4)}}}
			model := &PlazaModel{Pricing: p}
			group := &PlazaGroup{RateMultiplier: 2, ImageRateIndependent: true, ImageRateMultiplier: 0.3, VideoRateIndependent: true, VideoRateMultiplier: 0.4}
			q := QuotePlazaModel(model, group, testPtrFloat64(0.25), false)
			require.Equal(t, "personal", q.Scope)
			require.Equal(t, tc.expected, q.RateMultiplier)
			require.NotNil(t, q.Pricing.InputPrice)
			require.Zero(t, *q.Pricing.InputPrice)
			require.InDelta(t, 2*tc.expected, *q.Pricing.OutputPrice, 1e-12)
			require.InDelta(t, 4*tc.expected, *q.Pricing.Intervals[0].PerRequestPrice, 1e-12)
			require.Equal(t, 2.0, *p.OutputPrice)
			require.Equal(t, 4.0, *p.Intervals[0].PerRequestPrice, "no shared cache mutation")
		})
	}
	q := QuotePlazaModel(&PlazaModel{Pricing: &ChannelModelPricing{InputPrice: testPtrFloat64(0), OutputPrice: testPtrFloat64(0)}}, &PlazaGroup{RateMultiplier: 2}, testPtrFloat64(0), false)
	require.Equal(t, "resolved", q.Status)
	require.Zero(t, q.RateMultiplier)
}

func TestModelQuoteDistinguishesUnknownConditionalAndPersonalFailure(t *testing.T) {
	g := &PlazaGroup{RateMultiplier: 2, PeakRateEnabled: true}
	q := QuotePlazaModel(&PlazaModel{}, g, nil, false)
	require.Equal(t, "unavailable", q.Status)
	require.Nil(t, q.Pricing)
	q = QuotePlazaModel(&PlazaModel{QuoteReason: "request_dependent_pricing"}, g, nil, false)
	require.Equal(t, "conditional", q.Status)
	require.Nil(t, q.Pricing)
	q = QuotePlazaModel(&PlazaModel{Pricing: &ChannelModelPricing{InputPrice: testPtrFloat64(1), OutputPrice: testPtrFloat64(2)}}, g, nil, true)
	require.Equal(t, "group_fallback", q.Scope)
	require.Equal(t, "personal_rate_unavailable", q.Reason)
	require.Contains(t, q.Conditions, "peak_window")
}

func TestModelQuoteMatchesBillingForGroupOverrides(t *testing.T) {
	group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.37, LongContextPricingEnabled: true, ModelPricing: []ChannelModelPricing{{Models: []string{"gpt-5.4"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(7e-6), OutputPrice: testPtrFloat64(21e-6)}}}
	svc := newPlazaServiceWithBilling(nil, []Group{group}, nil, nil)
	svc.catalog = &GroupModelCatalogService{accounts: &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{"gpt-5.4": "gpt-5.4"})}}}
	groups, err := svc.ListGroups(context.Background())
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Models, 1)
	q := QuotePlazaModel(&groups[0].Models[0], &groups[0], nil, false)
	require.Equal(t, "group", q.Source)
	require.NotNil(t, q.Pricing)
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 400}
	actual, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: "gpt-5.4", Group: &group, RateMultiplier: group.RateMultiplier, Resolver: svc.resolver, Tokens: tokens})
	require.NoError(t, err)
	require.InDelta(t, actual.ActualCost, 1000*(*q.Pricing.InputPrice)+400*(*q.Pricing.OutputPrice), 1e-10)
	group.ModelPricing[0].FastMultiplier = testPtrFloat64(1.7)
	m := PlazaModel{Name: "gpt-5.4", Platform: PlatformOpenAI}
	svc.fillDisplayPricing(context.Background(), &m, &group)
	q = QuotePlazaModel(&m, &groups[0], nil, false)
	require.Contains(t, q.Conditions, "service_tier")
	actual, err = svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: m.Name, Group: &group, RateMultiplier: group.RateMultiplier, Resolver: svc.resolver, Tokens: tokens, ServiceTier: "fast"})
	require.NoError(t, err)
	require.InDelta(t, actual.ActualCost, (1000*(*q.Pricing.InputPrice)+400*(*q.Pricing.OutputPrice))*(*q.Pricing.FastMultiplier), 1e-10)
}

func TestModelQuoteImageTokenDimensionsMatchBilling(t *testing.T) {
	group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.5}
	catalog := newStubPricingServiceFromJSON(t, `{"gpt-image-1":{"input_cost_per_token":0.000005,"output_cost_per_token":0.00004,"input_cost_per_image_token":0.00001,"output_cost_per_image_token":0.00004,"cache_read_input_token_cost":0.00000125,"cache_read_input_image_token_cost":0.0000025}}`)
	svc := newPlazaServiceWithBilling(nil, []Group{group}, nil, catalog)
	m := PlazaModel{Name: "gpt-image-1", Platform: PlatformOpenAI}
	svc.fillDisplayPricing(context.Background(), &m, &group)
	require.NotNil(t, m.ImageTokenPricing)
	q := QuotePlazaModel(&m, &PlazaGroup{RateMultiplier: 0.5, ImageRateIndependent: true, ImageRateMultiplier: 10}, nil, false)
	require.NotNil(t, q.ImageTokenPricing)
	require.Equal(t, 0.5, q.RateMultiplier, "token images use token rates")
	actual, err := svc.billingService.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Model: m.Name, Group: &group, RateMultiplier: 0.5, Resolver: svc.resolver, Tokens: UsageTokens{InputTokens: 1000, ImageInputTokens: 1000, CacheReadTokens: 200, ImageCacheReadTokens: 200, OutputTokens: 400, ImageOutputTokens: 400}})
	require.NoError(t, err)
	require.InDelta(t, actual.ActualCost, 1000*(*q.ImageTokenPricing.Input)+200*(*q.ImageTokenPricing.CacheRead)+400*(*q.ImageTokenPricing.Output), 1e-10)
}

func TestModelQuoteMediaResolutionPrecedenceAndUnits(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeImage, BillingModeVideo} {
		t.Run(string(mode), func(t *testing.T) {
			group := Group{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 0.5, ImagePrice1K: testPtrFloat64(0.11), VideoPrice480P: testPtrFloat64(0.12)}
			model := "custom-image"
			if mode == BillingModeVideo {
				model = "grok-imagine-video"
			}
			ch := plazaPricedChannel(1, "ch", []int64{10}, PlatformOpenAI, model)
			ch.ModelPricing[0].BillingMode = mode
			ch.ModelPricing[0].PerRequestPrice = testPtrFloat64(0.9)
			svc := newPlazaServiceWithBilling([]Channel{ch}, []Group{group}, map[int64]string{10: PlatformOpenAI}, nil)
			m := PlazaModel{Name: model, Platform: PlatformOpenAI}
			svc.fillDisplayPricing(context.Background(), &m, &group)
			svc.fillMediaDisplayPricing(context.Background(), &m, &group)
			q := QuotePlazaModel(&m, &PlazaGroup{RateMultiplier: 0.5}, nil, false)
			require.Len(t, q.Pricing.Intervals, 3)
			expected := 0.11
			if mode == BillingModeVideo {
				expected = 0.12
				require.Equal(t, "second", q.Unit)
				require.Contains(t, q.Conditions, "duration")
			} else {
				require.Equal(t, "image", q.Unit)
			}
			require.InDelta(t, expected*0.5, *q.Pricing.Intervals[0].PerRequestPrice, 1e-12)
			require.InDelta(t, 0.9*0.5, *q.Pricing.Intervals[1].PerRequestPrice, 1e-12)
			// A model-specific group card wins over the group's generic resolution table.
			group.ModelPricing = []ChannelModelPricing{{Models: []string{model}, BillingMode: mode, PerRequestPrice: testPtrFloat64(0)}}
			svc.fillDisplayPricing(context.Background(), &m, &group)
			svc.fillMediaDisplayPricing(context.Background(), &m, &group)
			q = QuotePlazaModel(&m, &PlazaGroup{RateMultiplier: 0.5}, nil, false)
			for _, tier := range q.Pricing.Intervals {
				require.Zero(t, *tier.PerRequestPrice)
			}
		})
	}
}
