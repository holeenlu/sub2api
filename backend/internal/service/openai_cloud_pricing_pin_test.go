package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type cloudPricingPublicationContext struct {
	context.Context
	once    sync.Once
	publish func()
}

func TestCloudBillingCaptureUsesOriginalPricingTime(t *testing.T) {
	// Exercise the shared calculator's existing date-dependent card through
	// capture; this does not claim the Cloud protocol admits a DeepSeek model.
	svc, input, _ := cloudBillingFixture(t)
	input.APIKey.Group.ModelPricing = nil
	input.Result.Model, input.Result.UpstreamModel = "deepseek-v4-pro", "deepseek-v4-pro"
	input.OriginalModel, input.ChannelMappedModel = "deepseek-v4-pro", "deepseek-v4-pro"
	var frozen []float64
	for _, at := range []time.Time{deepseekProRoutesToFlashAt.Add(-time.Second), deepseekProRoutesToFlashAt} {
		input.PricingAt = at
		ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
		require.NoError(t, err)
		expected, err := svc.billingService.getModelPricingAt("deepseek-v4-pro", at)
		require.NoError(t, err)
		card := cloudBillingContext(ctx).Prices["deepseek-v4-pro"].Resolved.BasePricing
		require.Equal(t, expected.InputPricePerToken, card.InputPricePerToken)
		frozen = append(frozen, card.InputPricePerToken)
	}
	require.NotEqual(t, frozen[0], frozen[1], "request pricingAt must reach the actual resolver, not only snapshot metadata")
}

func (c *cloudPricingPublicationContext) Value(key any) any {
	// The original channel resolver reads this key after Pin. Publish a new
	// catalog/channel generation exactly as the first candidate is resolved.
	if key == ctxkey.ForcePlatform {
		c.once.Do(c.publish)
	}
	return c.Context.Value(key)
}

func TestCloudBillingCapturePinsCandidatesAndStatsToOnePublication(t *testing.T) {
	svc, input, _ := cloudBillingFixture(t)
	input.APIKey.Group.ModelPricing = nil
	input.Result.UpstreamModel = "gpt-4.1"
	oldInput, secondInput, zero := 2e-6, 4e-6, 0.0
	oldChannel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{input.APIKey.Group.ID}, ModelPricing: []ChannelModelPricing{
		{Platform: PlatformOpenAI, Models: []string{"gpt-5.1"}, BillingMode: BillingModeToken, InputPrice: &oldInput, CacheWritePrice: &zero},
		{Platform: PlatformOpenAI, Models: []string{"gpt-4.1"}, BillingMode: BillingModeToken, InputPrice: &secondInput, CacheWritePrice: &zero},
	}}
	channels := NewChannelService(nil, nil, nil, nil, nil)
	channels.cache.Store(populateChannelCache([]Channel{oldChannel}, map[int64]string{input.APIKey.Group.ID: PlatformOpenAI}))
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-5.1": {InputCostPerToken: 7e-6, OutputCostPerToken: 11e-6},
		"gpt-4.1": {InputCostPerToken: 8e-6, OutputCostPerToken: 12e-6},
	}}
	svc.billingService = NewBillingService(svc.cfg, prices)
	svc.channelService = channels
	svc.resolver = NewModelPricingResolver(channels, svc.billingService)
	published := false
	ctx := &cloudPricingPublicationContext{Context: t.Context(), publish: func() {
		newPrice := 99.0
		newChannel := oldChannel
		newChannel.ModelPricing = []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"gpt-*"}, BillingMode: BillingModeToken, InputPrice: &newPrice}}
		pricingViewPublicationMu.Lock()
		prices.mu.Lock()
		prices.pricingData = map[string]*LiteLLMModelPricing{"gpt-5.1": {InputCostPerToken: 99, OutputCostPerToken: 99}, "gpt-4.1": {InputCostPerToken: 99, OutputCostPerToken: 99}}
		prices.mu.Unlock()
		channels.cache.Store(populateChannelCache([]Channel{newChannel}, map[int64]string{input.APIKey.Group.ID: PlatformOpenAI}))
		pricingViewPublicationMu.Unlock()
		published = true
	}}
	captured, err := svc.CaptureCloudBillingSnapshot(ctx, input)
	require.NoError(t, err)
	require.True(t, published)
	snapshot := cloudBillingContext(captured)
	require.Equal(t, 3, snapshot.Version, "sampling-aware writer keeps the pinned prices and legacy frozen policy")
	require.Empty(t, snapshot.BillingMode)
	require.Equal(t, oldInput, snapshot.Prices["gpt-5.1"].Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, secondInput, snapshot.Prices["gpt-4.1"].Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, 12e-6, snapshot.Prices["gpt-4.1"].Resolved.BasePricing.OutputPricePerToken)
	require.True(t, snapshot.Prices["gpt-4.1"].resolved().BasePricing.cacheWriteFlatOverride)
	require.NotNil(t, snapshot.Stats)
	require.Equal(t, 8e-6, snapshot.Stats.DefaultPrice.Resolved.BasePricing.InputPricePerToken)
	live := svc.resolver.Resolve(t.Context(), PricingInput{Model: "gpt-4.1", GroupID: input.APIKey.GroupID, Group: input.APIKey.Group})
	require.Equal(t, 99.0, live.BasePricing.InputPricePerToken, "new requests see the actual new publication")
}

func TestCloudBillingCaptureDoesNotTreatPinFailureAsEmptyPricing(t *testing.T) {
	svc, input, _ := cloudBillingFixture(t)
	channels := NewChannelService(nil, nil, nil, nil, nil)
	cause := errors.New("channel publication unavailable")
	channels.storeErrorCache(cause)
	svc.channelService = channels
	svc.resolver = NewModelPricingResolver(channels, svc.billingService)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.ErrorIs(t, err, cause)
	require.Nil(t, cloudBillingContext(ctx))
}
