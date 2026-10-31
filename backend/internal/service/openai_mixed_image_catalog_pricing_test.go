//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Synthetic catalog and repository fixtures exercise the public usage writer
// and recovery calculator; they do not represent an upstream or PG transaction.
func TestOpenAIMixedImageCatalogRecordUsage(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		group, countBilling, resolver bool
	}{
		{name: "group_count_off", group: true, resolver: true},
		{name: "group_count_on", group: true, countBilling: true, resolver: true},
		{name: "group_without_resolver", group: true, countBilling: true},
		{name: "ungrouped", resolver: true},
		{name: "ungrouped_without_resolver"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			svc := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			svc.cfg.Default.RateMultiplier = 2
			const requested = "mixed-catalog-model"
			svc.billingService = NewBillingService(svc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				requested: {InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6},
			}})
			if tc.resolver {
				svc.resolver = NewModelPricingResolver(nil, svc.billingService)
			}
			key := &APIKey{ID: 2, UserID: 3}
			if tc.group {
				key.GroupID = i64p(9)
				key.Group = &Group{ID: 9, Platform: PlatformOpenAI, Hydrated: true, RateMultiplier: 2,
					ImageCountBilling: tc.countBilling, ImageRateIndependent: true, ImageRateMultiplier: 0.3, ImagePrice2K: f64p(0.7)}
			}
			result := &OpenAIForwardResult{RequestID: "mixed-catalog", Model: requested, BillingModel: "different-image-producer",
				ImageCount: 1, ImageSize: ImageBillingSize2K, ServiceTier: stringPtr("priority"),
				Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 200, CacheReadInputTokens: 100, CacheCreationInputTokens: 60}}
			require.NoError(t, svc.RecordUsage(t.Context(), &OpenAIRecordUsageInput{
				Result: result, APIKey: key, User: &User{ID: 3}, Account: &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
				ChannelUsageFields: ChannelUsageFields{OriginalModel: requested, BillingModelSource: BillingModelSourceRequested},
			}))
			log := logs.lastLog
			require.NotNil(t, log)
			require.NotNil(t, log.BillingMode)
			require.Equal(t, string(BillingModeToken), *log.BillingMode)
			require.Equal(t, 1, log.ImageCount)
			require.Equal(t, 840, log.InputTokens)
			require.Equal(t, 200, log.OutputTokens)
			want := (840*2e-6 + 200*4e-6) * 2 // Priority, then the user/group rate.
			require.InDelta(t, want, log.TotalCost, 1e-12)
			require.InDelta(t, want*2, log.ActualCost, 1e-12)
			require.Equal(t, 2.0, log.RateMultiplier)
			cmd := recordUsageBillingCommand(t, svc.usageBillingRepo)
			require.InDelta(t, want*2, cmd.BalanceCost, 1e-12)
			require.False(t, result.ImageEndpoint)
			require.Equal(t, requested, log.Model)
		})
	}
}

func TestOpenAIMixedImageCatalogRecovery(t *testing.T) {
	const name = "mixed-recovery-catalog-model"
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billing := NewBillingService(nil, prices)
	svc := &OpenAIGatewayService{billingService: billing}
	result := &OpenAIForwardResult{Model: name, ImageCount: 1, ImageSize: ImageBillingSize2K}
	tokens := UsageTokens{InputTokens: 840, OutputTokens: 200}
	_, err := svc.calculateOpenAIRecordUsageCost(t.Context(), result, &APIKey{}, []string{name}, 2, 0.3, 1, 2, tokens, "priority", nil, false, time.Time{})
	require.ErrorIs(t, err, ErrModelPricingUnavailable, "missing mixed token prices must not fall back to per-image charges")
	request := openAIPricingRequest(result, &APIKey{}, []string{name}, tokens, 2, 0.3, 1, 2, "priority", nil, false, false, time.Time{})
	cmd := &UsageBillingCommand{RequestID: "mixed-recovery", APIKeyID: 2, UsageLog: &UsageLog{ImageCount: 1},
		Pricing: &UsageBillingPricingSnapshot{Pending: true, Request: request, Balance: true, KeyQuota: true}}
	cmd.Normalize()
	fingerprint := cmd.RequestFingerprint
	reprice := usageBillingRepricer(billing, nil, nil)
	require.ErrorIs(t, reprice(t.Context(), cmd), ErrModelPricingUnavailable)
	require.True(t, cmd.Pricing.Pending)
	require.Zero(t, cmd.BalanceCost)
	prices.pricingData[name] = &LiteLLMModelPricing{InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6}
	require.NoError(t, reprice(t.Context(), cmd))
	require.False(t, cmd.Pricing.Pending)
	require.Equal(t, fingerprint, cmd.RequestFingerprint)
	require.NotNil(t, cmd.UsageLog.BillingMode)
	require.Equal(t, string(BillingModeToken), *cmd.UsageLog.BillingMode)
	require.Equal(t, 1, cmd.UsageLog.ImageCount)
	require.InDelta(t, (840*2e-6+200*4e-6)*2, cmd.UsageLog.TotalCost, 1e-12)
	require.InDelta(t, (840*2e-6+200*4e-6)*2*2, cmd.BalanceCost, 1e-12)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyQuotaCost)
}

func TestOpenAIDedicatedImageCatalogPricingPreserved(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		countBilling, tokenOverride, imageOnly bool
	}{
		{name: "catalog_count_off"},
		{name: "catalog_count_on", countBilling: true},
		{name: "token_override_count_off", tokenOverride: true},
		{name: "token_override_count_on", tokenOverride: true, countBilling: true},
		{name: "image_only_catalog", imageOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const name = "dedicated-catalog-model"
			card := &LiteLLMModelPricing{InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6}
			if tc.imageOnly {
				card = &LiteLLMModelPricing{OutputCostPerImage: 0.25, TokenPricingAbsent: true}
			}
			billing := NewBillingService(nil, &PricingService{pricingData: map[string]*LiteLLMModelPricing{name: card}})
			svc := &OpenAIGatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
			key := &APIKey{GroupID: i64p(9), Group: &Group{ID: 9, Hydrated: true, ImageCountBilling: tc.countBilling, ImagePrice2K: f64p(0.7)}}
			if tc.tokenOverride {
				key.Group.ModelPricing = []ChannelModelPricing{{Models: []string{name}, BillingMode: BillingModeToken, InputPrice: f64p(2e-6), OutputPrice: f64p(4e-6)}}
			}
			cost, err := svc.calculateOpenAIRecordUsageCost(t.Context(), &OpenAIForwardResult{
				Model: name, ImageEndpoint: true, ImageCount: 2, ImageSize: ImageBillingSize2K,
			}, key, []string{name}, 2, 0.3, 1, 2, UsageTokens{InputTokens: 840, OutputTokens: 200}, "", nil, false, time.Time{})
			require.NoError(t, err)
			if tc.tokenOverride && !tc.countBilling {
				require.Equal(t, string(BillingModeToken), cost.BillingMode)
				require.InDelta(t, 840*2e-6+200*4e-6, cost.TotalCost, 1e-12)
				require.InDelta(t, cost.TotalCost*2, cost.ActualCost, 1e-12)
			} else {
				require.Equal(t, string(BillingModeImage), cost.BillingMode)
				require.InDelta(t, 2*0.7, cost.TotalCost, 1e-12)
				require.InDelta(t, 2*0.7*0.3, cost.ActualCost, 1e-12)
			}
		})
	}
}
