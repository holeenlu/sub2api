package service

import "context"

// Prices for dimensions that actually occurred must be known. An absent cache
// or image rate is not an explicit zero, even if base input/output are ready.
func catalogUsagePriced(ctx context.Context, resolver *ModelPricingResolver, billing *BillingService, group *Group, model string, tokens UsageTokens) bool {
	if !CatalogEnforced(group) || resolver == nil || billing == nil {
		return true
	}
	resolved := resolver.Resolve(ctx, PricingInput{Model: model, Group: group, GroupID: &group.ID})
	if resolved != nil && resolved.Mode != BillingModeToken {
		return true
	}
	if billing.fallbackPrices[model] != nil {
		return true
	}
	prices := billing.pinnedPriceService(ctx).pricingService
	var base *LiteLLMModelPricing
	if prices != nil {
		base = prices.GetExactModelPricing(model)
	}
	if base == nil {
		p := (*ChannelModelPricing)(nil)
		if resolved != nil {
			p = resolved.channelPricing
		}
		if p == nil || p.InputPrice == nil || p.OutputPrice == nil {
			return false
		}
	}
	known := map[string]bool{}
	if base != nil && base.ProvidedFields == nil {
		for _, field := range []string{"input_cost_per_token", "output_cost_per_token", "cache_read_input_token_cost", "cache_creation_input_token_cost", "cache_creation_input_token_cost_above_1hr", "input_cost_per_image_token", "output_cost_per_image_token", "cache_read_input_image_token_cost"} {
			known[field] = true
		}
	}
	if base != nil {
		for field, present := range base.ProvidedFields {
			known[field] = present
		}
	}
	if resolved != nil && resolved.channelPricing != nil {
		p := resolved.channelPricing
		known["input_cost_per_token"] = known["input_cost_per_token"] || p.InputPrice != nil
		known["output_cost_per_token"] = known["output_cost_per_token"] || p.OutputPrice != nil
		known["cache_read_input_token_cost"] = known["cache_read_input_token_cost"] || p.CacheReadPrice != nil
		known["cache_creation_input_token_cost"] = known["cache_creation_input_token_cost"] || p.CacheWritePrice != nil
		known["cache_creation_input_token_cost_above_1hr"] = known["cache_creation_input_token_cost_above_1hr"] || p.CacheWrite1hPrice != nil
		known["input_cost_per_image_token"] = known["input_cost_per_image_token"] || p.ImageInputPrice != nil
		known["output_cost_per_image_token"] = known["output_cost_per_image_token"] || p.ImageOutputPrice != nil
		total := tokens.InputTokens + tokens.CacheReadTokens + tokens.CacheCreationTokens
		for _, iv := range p.Intervals {
			if total >= iv.MinTokens && (iv.MaxTokens == nil || total < *iv.MaxTokens) {
				known["cache_read_input_token_cost"] = known["cache_read_input_token_cost"] || iv.CacheReadPrice != nil
				known["cache_creation_input_token_cost"] = known["cache_creation_input_token_cost"] || iv.CacheWritePrice != nil
				known["cache_creation_input_token_cost_above_1hr"] = known["cache_creation_input_token_cost_above_1hr"] || iv.CacheWrite1hPrice != nil
				break
			}
		}
	}
	for field, count := range map[string]int{"input_cost_per_token": tokens.InputTokens, "output_cost_per_token": tokens.OutputTokens, "cache_read_input_token_cost": tokens.CacheReadTokens, "cache_creation_input_token_cost": tokens.CacheCreationTokens + tokens.CacheCreation5mTokens, "cache_creation_input_token_cost_above_1hr": tokens.CacheCreation1hTokens, "input_cost_per_image_token": tokens.ImageInputTokens, "output_cost_per_image_token": tokens.ImageOutputTokens, "cache_read_input_image_token_cost": tokens.ImageCacheReadTokens} {
		if count > 0 && !known[field] {
			return false
		}
	}
	return true
}
