package service

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"
)

// Publication alone holds the write lock, never database/file/network loading.
// Pin captures references that coexisted locally; this is not a database-wide
// transaction or a revision store for cross-process historical price lookup.
var pricingViewPublicationMu sync.RWMutex

// ModelPricingView retains the in-process catalog and channel publications for
// one live request. Its maps are shared, immutable and never copied per Pin.
// Returned cards are detached; callers may modify them without changing a view.
// Group pricing remains an explicit input: the request owner freezes only
// Group.ModelPricing and LongContextPricingEnabled, not an entire auth object.
type ModelPricingView struct {
	resolver ModelPricingResolver
}

// Pin loads channels before capturing both publications. A load error, including
// a cached load error, is not an empty price table. Nil resolver/billing fails.
// The caller owns request lifetime and persists only its finally selected card.
func (r *ModelPricingResolver) Pin(ctx context.Context, pricingAt time.Time) (*ModelPricingView, error) {
	if ctx == nil || r == nil || r.billingService == nil || pricingAt.IsZero() {
		return nil, fmt.Errorf("pin pricing: %w", ErrModelPricingUnavailable)
	}
	var managed *ManagedPrices
	if live := r.billingService.pricingService; live != nil && live.modelSettings != nil {
		var err error
		managed, err = live.modelSettings.GetManagedPrices(ctx, false)
		if err != nil {
			return nil, err
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.channelService != nil {
			if _, err := r.channelService.loadCache(ctx); err != nil {
				return nil, fmt.Errorf("pin channel pricing: %w", err)
			}
		}
		pricingViewPublicationMu.RLock()
		var channels *channelCache
		if r.channelService != nil {
			channels, _ = r.channelService.cache.Load().(*channelCache)
			if channels == nil || time.Since(channels.loadedAt) >= channelCacheTTL {
				pricingViewPublicationMu.RUnlock()
				continue // invalidated between load and reference capture
			}
			if channels.loadErr != nil {
				pricingViewPublicationMu.RUnlock()
				return nil, fmt.Errorf("pin channel pricing: %w", channels.loadErr)
			}
		}
		var catalog *PricingService
		if live := r.billingService.pricingService; live != nil {
			live.mu.RLock()
			// This private read-only facade runs the original matching methods.
			// It shares the immutable map, not the live service's mutex/lifecycle.
			catalog = &PricingService{pricingData: live.pricingData, managedSnapshot: managed}
			live.mu.RUnlock()
		}
		view := &ModelPricingView{resolver: ModelPricingResolver{
			channelService: r.channelService, billingService: r.billingService,
			channelSnapshot: channels, pricingSnapshot: catalog, pricingAt: pricingAt,
		}}
		pricingViewPublicationMu.RUnlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return view, nil
	}
}

// Resolve uses the original resolver, including literal/normalized matching,
// group precedence, intervals and private flags. Nil view has no usable price.
func (v *ModelPricingView) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	if v == nil {
		return nil
	}
	return detachResolvedPricing(v.resolver.Resolve(ctx, input))
}

// HasIdentifiedTokenPricing preserves the existing returned-model admission
// rule against this view. Family fallback is not proof of model identification.
func (v *ModelPricingView) HasIdentifiedTokenPricing(model string) bool {
	if v == nil {
		return false
	}
	if prices := v.resolver.pricingSnapshot; prices != nil && managedPriceFromSnapshot(prices.managedSnapshot, model) != nil {
		return true
	}
	return v.resolver.billingService.hasIdentifiedTokenPricing(model, v.resolver.pricingSnapshot)
}

func detachModelPricing(pricing *ModelPricing) *ModelPricing {
	if pricing == nil {
		return nil
	}
	copy := *pricing // preserve unexported pricing policy flags too
	copy.managedPrice = cloneManagedPrice(pricing.managedPrice)
	if pricing.FastMultiplier != nil {
		value := *pricing.FastMultiplier
		copy.FastMultiplier = &value
	}
	if pricing.FlexMultiplier != nil {
		value := *pricing.FlexMultiplier
		copy.FlexMultiplier = &value
	}
	copy.ReasoningEffortMultipliers = maps.Clone(pricing.ReasoningEffortMultipliers)
	return &copy
}

func detachResolvedPricing(pricing *ResolvedPricing) *ResolvedPricing {
	if pricing == nil {
		return nil
	}
	copy := *pricing
	copy.BasePricing = detachModelPricing(pricing.BasePricing)
	// Preserve private flags; JSON round trips lose explicit-zero policy.
	intervals := detachChannelModelPricing(ChannelModelPricing{Intervals: pricing.Intervals})
	tiers := detachChannelModelPricing(ChannelModelPricing{Intervals: pricing.RequestTiers})
	copy.Intervals, copy.RequestTiers = intervals.Intervals, tiers.Intervals
	if pricing.channelPricing != nil {
		card := detachChannelModelPricing(*pricing.channelPricing)
		copy.channelPricing = &card
	}
	return &copy
}

func detachLiteLLMPricing(pricing *LiteLLMModelPricing) *LiteLLMModelPricing {
	if pricing == nil {
		return nil
	}
	copy := *pricing // scalar fields, including json:"-" presence flags
	return &copy
}

func copyPricingValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// Clone already owns slices/maps/schedules; detach its documented shared scalar
// pointers too before publishing or returning a price outside the cache owner.
func detachChannelModelPricing(pricing ChannelModelPricing) ChannelModelPricing {
	copy := pricing.Clone()
	copy.InputPrice = copyPricingValue(pricing.InputPrice)
	copy.OutputPrice = copyPricingValue(pricing.OutputPrice)
	copy.CacheWritePrice = copyPricingValue(pricing.CacheWritePrice)
	copy.CacheWrite1hPrice = copyPricingValue(pricing.CacheWrite1hPrice)
	copy.CacheReadPrice = copyPricingValue(pricing.CacheReadPrice)
	copy.FastMultiplier = copyPricingValue(pricing.FastMultiplier)
	copy.FlexMultiplier = copyPricingValue(pricing.FlexMultiplier)
	copy.ImageInputPrice = copyPricingValue(pricing.ImageInputPrice)
	copy.ImageOutputPrice = copyPricingValue(pricing.ImageOutputPrice)
	copy.PerRequestPrice = copyPricingValue(pricing.PerRequestPrice)
	for i := range copy.Intervals {
		iv := &copy.Intervals[i]
		iv.MaxTokens = copyPricingValue(iv.MaxTokens)
		iv.InputPrice = copyPricingValue(iv.InputPrice)
		iv.OutputPrice = copyPricingValue(iv.OutputPrice)
		iv.CacheWritePrice = copyPricingValue(iv.CacheWritePrice)
		iv.CacheWrite1hPrice = copyPricingValue(iv.CacheWrite1hPrice)
		iv.CacheReadPrice = copyPricingValue(iv.CacheReadPrice)
		iv.InputMultiplier = copyPricingValue(iv.InputMultiplier)
		iv.OutputMultiplier = copyPricingValue(iv.OutputMultiplier)
		iv.CacheWriteMultiplier = copyPricingValue(iv.CacheWriteMultiplier)
		iv.CacheReadMultiplier = copyPricingValue(iv.CacheReadMultiplier)
		iv.PerRequestPrice = copyPricingValue(iv.PerRequestPrice)
	}
	return copy
}

func detachPricingChannel(channel *Channel) *Channel {
	copy := channel.Clone()
	if copy == nil {
		return nil
	}
	for i := range copy.ModelPricing {
		copy.ModelPricing[i] = detachChannelModelPricing(copy.ModelPricing[i])
	}
	for i := range copy.AccountStatsPricingRules {
		for j := range copy.AccountStatsPricingRules[i].Pricing {
			copy.AccountStatsPricingRules[i].Pricing[j] = detachChannelModelPricing(copy.AccountStatsPricingRules[i].Pricing[j])
		}
	}
	return copy
}
