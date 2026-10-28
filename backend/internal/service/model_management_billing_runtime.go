package service

import (
	"encoding/json"
	"strings"
	"time"
)

// getModelPricingFrom runs the same base resolver against the request's pinned
// publication. Managed tiers/modes survive until the authoritative calculator
// selects the actual context and service tier from observed usage.
func (s *BillingService) getModelPricingFrom(name string, at time.Time, prices *PricingService) (*ModelPricing, error) {
	base, baseErr := s.baseModelPricingFrom(name, at, prices)
	if prices == nil || prices.managedSnapshot == nil {
		return base, baseErr
	}
	p := managedPriceFromSnapshot(prices.managedSnapshot, name)
	if p == nil {
		return base, baseErr
	}
	return applyManagedPriceCard(base, p), nil
}

func managedPriceFromSnapshot(prices *ManagedPrices, name string) *ManagedPrice {
	if prices == nil {
		return nil
	}
	for _, candidate := range []string{name, strings.ToLower(name)} {
		if p, ok := prices.Overrides[candidate]; ok {
			return cloneManagedPrice(&p)
		}
		if p, ok := prices.External[candidate]; ok {
			return cloneManagedPrice(&p)
		}
	}
	return nil
}
func applyManagedPriceCard(base *ModelPricing, p *ManagedPrice) *ModelPricing {
	next := applyManagedTokenPrice(base, p.ManagedTokenPrice)
	next.managedPrice = cloneManagedPrice(p)
	if p.Image != nil {
		next.ImageInputPricePerToken = *p.Image.Input / 1e6
		next.ImageOutputPricePerToken = *p.Image.Output / 1e6
		next.ImageCacheReadPricePerToken = *p.Image.CacheRead / 1e6
		next.ImageOutputPriceExplicit = true
	}
	return next
}
func cloneManagedPrice(p *ManagedPrice) *ManagedPrice {
	if p == nil {
		return nil
	}
	// Validated saved cards contain finite scalar JSON values only.
	raw, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var copy ManagedPrice
	if json.Unmarshal(raw, &copy) != nil {
		return nil
	}
	return &copy
}
