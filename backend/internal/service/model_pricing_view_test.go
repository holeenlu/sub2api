//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestModelPricingViewKeepsCatalogAndChannelAfterPublication(t *testing.T) {
	prices := newHotReloadPricingService(t, "", "")
	billing := NewBillingService(nil, prices)
	group := &Group{ID: 9, Platform: PlatformOpenAI, LongContextPricingEnabled: true}
	channel := newChannelServiceWithPricings(group.ID, []ChannelModelPricing{tokenPricingForModels([]string{"channel-model"}, 4)})
	resolver := NewModelPricingResolver(channel, billing)
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	old, err := resolver.Pin(t.Context(), at)
	require.NoError(t, err)
	other, err := resolver.Pin(t.Context(), at)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%p", old.resolver.pricingSnapshot.pricingData), fmt.Sprintf("%p", other.resolver.pricingSnapshot.pricingData))
	require.Same(t, old.resolver.channelSnapshot, other.resolver.channelSnapshot)
	// Mutation of a returned object may not alter either published view.
	catalogRead := prices.GetModelPricing("remote-model")
	catalogRead.InputCostPerToken = 90
	returnedChannel, err := channel.GetChannelForGroup(t.Context(), group.ID)
	require.NoError(t, err)
	*returnedChannel.ModelPricing[0].InputPrice = 90
	data, err := json.Marshal(map[string]any{"remote-model": map[string]any{"input_cost_per_token": 8e-6, "output_cost_per_token": 9e-6}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(prices.getPricingFilePath(), data, 0600))
	require.NoError(t, prices.loadPricingData(prices.getPricingFilePath()))
	channel.repo = &mockChannelRepository{
		listAllFn: func(context.Context) ([]Channel, error) {
			return []Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID}, ModelPricing: []ChannelModelPricing{tokenPricingForModels([]string{"channel-model"}, 20)}}}, nil
		},
		getGroupPlatformsFn: func(context.Context, []int64) (map[int64]string, error) {
			return map[int64]string{group.ID: PlatformOpenAI}, nil
		},
	}
	_, err = channel.buildCache(t.Context())
	require.NoError(t, err)
	current, err := resolver.Pin(t.Context(), at)
	require.NoError(t, err)
	for _, test := range []struct {
		view  *ModelPricingView
		model string
		want  float64
	}{
		{old, "remote-model", 1e-6}, {other, "remote-model", 1e-6}, {current, "remote-model", 8e-6},
		{old, "channel-model", 4e-6}, {current, "channel-model", 20e-6},
	} {
		card := test.view.Resolve(t.Context(), PricingInput{Model: test.model, GroupID: &group.ID, Group: group})
		require.InDelta(t, test.want, card.BasePricing.InputPricePerToken, 1e-12)
		cost, err := billing.CalculateCostUnified(CostInput{Ctx: t.Context(), Model: test.model, Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: 1, PricingAt: at, Resolved: card})
		require.NoError(t, err, "explicit Resolved must work without a second live resolver")
		require.InDelta(t, test.want*1_000_000, cost.TotalCost, 1e-9)
	}
	require.True(t, old.HasIdentifiedTokenPricing("remote-model"))
	require.False(t, old.HasIdentifiedTokenPricing("not-a-real-haiku-model"), "family fallback cannot authorize a returned-model switch")
}

func TestModelPricingViewDetachPreservesPrivateAndExplicitZero(t *testing.T) {
	zero, fast, limit := 0.0, 3.0, 2000
	card := ChannelModelPricing{Models: []string{"gpt-5.6-sol"}, Platform: PlatformOpenAI, BillingMode: BillingModeToken,
		InputPrice: float64Ptr(1e-6), OutputPrice: float64Ptr(2e-6), CacheWritePrice: &zero, FastMultiplier: &fast,
		ReasoningEffortMultipliers: map[string]float64{"high": 2},
		Intervals:                  []PricingInterval{{MinTokens: 0, MaxTokens: &limit, CacheWritePrice: &zero}},
		TimePricing:                &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "00:00", EndTime: "01:00", Multiplier: 2}}}}
	billing := NewBillingService(nil, nil)
	channel := newChannelServiceWithPricings(9, []ChannelModelPricing{card})
	view, err := NewModelPricingResolver(channel, billing).Pin(t.Context(), time.Now())
	require.NoError(t, err)
	group := &Group{ID: 9, Platform: PlatformOpenAI, LongContextPricingEnabled: true}
	read := func() *ResolvedPricing {
		return view.Resolve(t.Context(), PricingInput{Model: "gpt-5.6-sol", GroupID: &group.ID, Group: group})
	}
	resolved := read()
	require.True(t, resolved.BasePricing.cacheWriteFlatOverride)
	require.Zero(t, resolved.BasePricing.CacheCreationPricePerToken)
	*resolved.BasePricing.FastMultiplier = 70
	resolved.BasePricing.ReasoningEffortMultipliers["high"] = 70
	*resolved.Intervals[0].MaxTokens = 70
	*resolved.channelPricing.CacheWritePrice = 70
	resolved.channelPricing.TimePricing.Periods[0].Multiplier = 70
	fresh := read()
	require.Equal(t, fast, *fresh.BasePricing.FastMultiplier)
	require.Equal(t, 2.0, fresh.BasePricing.ReasoningEffortMultipliers["high"])
	require.Equal(t, limit, *fresh.Intervals[0].MaxTokens)
	require.Zero(t, *fresh.channelPricing.CacheWritePrice)
	require.Equal(t, 2.0, fresh.channelPricing.TimePricing.Periods[0].Multiplier)
	freeze, err := FreezeNativeUsagePricing(fresh, nil, false)
	require.NoError(t, err)
	restored, err := freeze.Resolved.Restore()
	require.NoError(t, err)
	require.True(t, restored.BasePricing.cacheWriteFlatOverride)
	require.True(t, restored.longContextPricingEnabled)
}

func TestModelPricingViewRejectsUnavailableOrCanceledCapture(t *testing.T) {
	dependency := errors.New("channel storage unavailable")
	channels := NewChannelService(&mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, dependency }}, nil, nil, nil, nil)
	resolver := NewModelPricingResolver(channels, NewBillingService(nil, nil))
	for range 2 {
		view, err := resolver.Pin(t.Context(), time.Now())
		require.Nil(t, view)
		require.ErrorIs(t, err, dependency, "cached failure is not an empty table")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := resolver.Pin(ctx, time.Now())
	require.ErrorIs(t, err, context.Canceled)
	_, err = NewModelPricingResolver(nil, NewBillingService(nil, nil)).Pin(t.Context(), time.Time{})
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestModelPricingViewConcurrentReloadKeepsPublishedReaders(t *testing.T) {
	prices := newHotReloadPricingService(t, "", "")
	resolver := NewModelPricingResolver(nil, NewBillingService(nil, prices))
	view, err := resolver.Pin(t.Context(), time.Now())
	require.NoError(t, err)
	var readers sync.WaitGroup
	for range 4 {
		readers.Go(func() {
			for range 100 {
				card := view.Resolve(t.Context(), PricingInput{Model: "remote-model"})
				if card.BasePricing.InputPricePerToken != 1e-6 {
					t.Errorf("pinned price changed: %g", card.BasePricing.InputPricePerToken)
				}
				card.BasePricing.InputPricePerToken = 77
			}
		})
	}
	for i := range 5 {
		data, err := json.Marshal(map[string]any{"remote-model": map[string]any{"input_cost_per_token": float64(i+2) * 1e-6, "output_cost_per_token": 9e-6}})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(prices.getPricingFilePath(), data, 0600))
		require.NoError(t, prices.loadPricingData(prices.getPricingFilePath()))
	}
	readers.Wait()
}
