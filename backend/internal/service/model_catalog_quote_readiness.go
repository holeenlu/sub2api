package service

import "context"

func (s *ModelCatalogService) quoteStatus(ctx context.Context, model *GroupCatalogModel, group *Group) string {
	if s.pricingResolver == nil {
		return ""
	}
	if len(model.BillingModels) == 0 {
		return "unavailable"
	}
	prices := s.prices
	if pinned := RequestPricingFromContext(ctx); pinned != nil && pinned.prices != nil {
		prices = pinned.prices
	}
	for _, id := range model.BillingModels {
		ready := false
		inputKnown, outputKnown := false, false
		r := s.pricingResolver.Resolve(ctx, PricingInput{Model: id, Group: group, GroupID: &group.ID})
		// Image settlement has per-image fallback prices even when a new image
		// ID has no token entry in the remote reference catalog. Only an explicit
		// group/channel token override opts out of those defaults.
		explicitToken := r != nil && (r.Source == PricingSourceGroup || r.Source == PricingSourceChannel) && r.Mode == BillingModeToken
		if IsImageGenerationIntent("", id, nil) && !explicitToken && s.pricingResolver.billingService != nil {
			continue
		}
		if r != nil && r.channelPricing != nil {
			p := r.channelPricing
			if r.Mode == BillingModeImage || r.Mode == BillingModeVideo || r.Mode == BillingModePerRequest {
				ready = p.PerRequestPrice != nil || len(p.Intervals) > 0
			} else {
				inputKnown = p.InputPrice != nil
				outputKnown = p.OutputPrice != nil
				ready = inputKnown && outputKnown
			}
		}
		if !ready && prices != nil {
			if p := prices.GetExactModelPricing(id); p != nil {
				ready = p.ProvidedFields == nil || ((inputKnown || p.ProvidedFields["input_cost_per_token"]) && (outputKnown || p.ProvidedFields["output_cost_per_token"])) ||
					p.ProvidedFields["output_cost_per_image"] || p.ProvidedFields["output_cost_per_image_token"] ||
					(p.Mode == "embedding" && p.ProvidedFields["input_cost_per_token"])
			}
		}
		if prices != nil {
			if p := prices.GetExactModelPricing(id); p != nil && p.ProvidedFields != nil && p.SupportsPromptCaching && !p.ProvidedFields["cache_read_input_token_cost"] {
				ready = ready && r != nil && r.channelPricing != nil && r.channelPricing.CacheReadPrice != nil
			}
		}
		if !ready && s.pricingResolver.billingService != nil {
			ready = s.pricingResolver.billingService.fallbackPrices[id] != nil
		}
		if !ready {
			return "unavailable"
		}
	}
	if model.ResponseDependent || len(model.BillingModels) > 1 {
		return "conditional"
	}
	return "ready"
}

func (s *PricingService) GetExactModelPricing(model string) *LiteLLMModelPricing {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if value := s.pricingData[model]; value != nil {
		copy := *value
		return &copy
	}
	return nil
}

func (s *PricingService) CatalogManaged() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.catalogManaged
}
