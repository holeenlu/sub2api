package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type requestPricingContextKey struct{}
type RequestPricingSnapshot struct {
	Revision  string
	StartedAt time.Time
	prices    *PricingService
	channels  *channelCache
	group     *Group
	userID    int64
	rate      float64
	rateReady bool
	resolved  sync.Map
}

func RequestPricingFromContext(ctx context.Context) *RequestPricingSnapshot {
	if ctx == nil {
		return nil
	}
	value, _ := ctx.Value(requestPricingContextKey{}).(*RequestPricingSnapshot)
	return value
}
func CopyRequestPricingContext(parent, base context.Context) context.Context {
	if value := RequestPricingFromContext(parent); value != nil {
		return context.WithValue(base, requestPricingContextKey{}, value)
	}
	return base
}

// Price maps/cache generations are replaced, never edited after publication.
// Retaining their generation is O(1); only the selected rules are journaled.
func (s *GatewayService) PinRequestPricing(ctx context.Context, key *APIKey) context.Context {
	if s == nil || s.billingService == nil || RequestPricingFromContext(ctx) != nil {
		return ctx
	}
	p := &RequestPricingSnapshot{StartedAt: time.Now().UTC()}
	if prices := s.billingService.pricingService; prices != nil {
		prices.mu.RLock()
		p.prices = &PricingService{cfg: prices.cfg, catalogManaged: key != nil && CatalogEnforced(key.Group), pricingData: prices.pricingData, localHash: prices.localHash, customFilesHash: prices.customFilesHash, referencePrices: prices.referencePrices, referenceSupplementHash: prices.referenceSupplementHash}
		prices.mu.RUnlock()
		p.Revision = p.prices.PriceRevision()
	}
	if s.channelService != nil {
		p.channels, _ = s.channelService.loadCache(ctx)
	}
	if key != nil && key.Group != nil {
		g := *key.Group
		g.ModelPricing = make([]ChannelModelPricing, len(key.Group.ModelPricing))
		for i := range g.ModelPricing {
			g.ModelPricing[i] = key.Group.ModelPricing[i].Clone()
		}
		p.group = &g
		p.userID = key.UserID
		p.rate = s.getUserGroupRateMultiplier(ctx, key.UserID, g.ID, g.RateMultiplier)
		p.rateReady = true
	}
	if p.channels != nil {
		p.Revision = modelCatalogHash([]string{p.Revision, p.channels.loadedAt.String()})
	}
	if p.group != nil {
		p.Revision = modelCatalogHash(struct {
			Base  string
			Group int64
			Cards []ChannelModelPricing
			Rate  float64
		}{p.Revision, p.group.ID, p.group.ModelPricing, p.rate})
	}
	return context.WithValue(ctx, requestPricingContextKey{}, p)
}

func (s *BillingService) pinnedPriceService(ctx context.Context) *BillingService {
	if p := RequestPricingFromContext(ctx); p != nil && p.prices != nil && s.pricingService != p.prices {
		return &BillingService{cfg: s.cfg, pricingService: p.prices, fallbackPrices: s.fallbackPrices}
	}
	return s
}

func (r *ModelPricingResolver) resolvePinned(ctx context.Context, input PricingInput) *ResolvedPricing {
	p := RequestPricingFromContext(ctx)
	if p != nil && p.group != nil && input.GroupID != nil && *input.GroupID == p.group.ID {
		input.Group = p.group
	}
	copy := *r
	if r.billingService != nil {
		copy.billingService = r.billingService.pinnedPriceService(ctx)
	}
	result := copy.resolveCurrent(ctx, input)
	if p != nil && result != nil {
		result.PriceRevision = p.Revision
		p.resolved.Store(input.Model, result)
	}
	return result
}

type UsagePricingAuditRepository interface {
	SaveUsagePricingAudit(context.Context, int64, string, string, json.RawMessage) error
}

func journalUsagePricing(ctx context.Context, repo UsageLogRepository, usage *UsageLog) error {
	p := RequestPricingFromContext(ctx)
	if p == nil || usage == nil || usage.RequestID == "" {
		return nil
	}
	audit, ok := repo.(UsagePricingAuditRepository)
	if !ok {
		return nil
	}
	rules := map[string]any{}
	p.resolved.Range(func(key, value any) bool {
		v := value.(*ResolvedPricing)
		rules[key.(string)] = map[string]any{"source": v.Source, "mode": v.Mode, "base": v.BasePricing, "intervals": v.Intervals, "request_tiers": v.RequestTiers, "channel_pricing": v.channelPricing}
		return true
	})
	body, err := json.Marshal(map[string]any{"started_at": p.StartedAt, "model": usage.Model, "rate_multiplier": usage.RateMultiplier, "actual_cost": usage.ActualCost, "rules": rules})
	if err != nil {
		return err
	}
	return audit.SaveUsagePricingAudit(ctx, usage.APIKeyID, usage.RequestID, p.Revision, body)
}

func servicePricingContext(parent, base context.Context) context.Context {
	return CopyRequestPricingContext(parent, base)
}

func (s *OpenAIGatewayService) PrepareCatalogTurn(ctx context.Context, key *APIKey, models []string) (context.Context, error) {
	ctx = context.WithValue(ctx, requestPricingContextKey{}, (*RequestPricingSnapshot)(nil))
	gateway := &GatewayService{billingService: s.billingService, channelService: s.channelService, userGroupRateResolver: s.userGroupRateResolver}
	ctx = gateway.PinRequestPricing(ctx, key)
	if key != nil && s.groupModelCatalog != nil {
		return s.groupModelCatalog.Admit(ctx, key.Group, models)
	}
	return ctx, nil
}

// Unknown response-model prices are queued for operator settlement. The request
// already ran: do not replay generation or report an implicit free charge.
func pendingCatalogPricing(ctx context.Context, repo UsageLogRepository, keyID int64, requestID, model string, usage any) error {
	revision := ""
	if p := RequestPricingFromContext(ctx); p != nil {
		revision = p.Revision
	}
	raw, err := json.Marshal(map[string]any{"status": "pending", "reason": "pricing_unavailable", "model": model, "usage": usage})
	if err != nil {
		return err
	}
	audit, ok := repo.(UsagePricingAuditRepository)
	if !ok {
		return fmt.Errorf("pricing unavailable for %s; audit repository unavailable", model)
	}
	if err := audit.SaveUsagePricingAudit(ctx, keyID, requestID, revision, raw); err != nil {
		return err
	}
	return fmt.Errorf("pricing pending settlement for request %s", requestID)
}

func (s *PricingService) catalogPricingGeneration() *PricingService {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &PricingService{cfg: s.cfg, catalogManaged: true, pricingData: s.pricingData, localHash: s.localHash, customFilesHash: s.customFilesHash, referencePrices: s.referencePrices, referenceSupplementHash: s.referenceSupplementHash}
}
