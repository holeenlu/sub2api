package service

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Values in the management contract are USD per million tokens; conversion to
// billing's per-token units occurs exactly once, at the resolution boundary.
type ManagedTokenPrice struct {
	Input     *float64 `json:"input"`
	Output    *float64 `json:"output"`
	CacheRead *float64 `json:"cache_read"`
	Cache5m   *float64 `json:"cache_5m,omitempty"`
	Cache1h   *float64 `json:"cache_1h,omitempty"`
}
type ManagedPriceTier struct {
	ManagedTokenPrice
	Above    int  `json:"above"`
	Unpriced bool `json:"unpriced,omitempty"`
}
type ManagedPrice struct {
	ManagedTokenPrice
	Tiers []ManagedPriceTier            `json:"tiers,omitempty"`
	Modes map[string]*ManagedTokenPrice `json:"modes,omitempty"`
	Image *ManagedTokenPrice            `json:"image,omitempty"`
}
type ManagedPrices struct {
	Version   int64                   `json:"version"`
	UpdatedAt string                  `json:"updated_at,omitempty"`
	Overrides map[string]ManagedPrice `json:"overrides"`
	External  map[string]ManagedPrice `json:"external"`
	Source    string                  `json:"source,omitempty"`
	SyncedAt  string                  `json:"synced_at,omitempty"`
}

// BaseModelPricingForManagement excludes the managed overlay. The HTTP
// projection applies its one captured ManagedPrices snapshot to these defaults.
func (s *BillingService) BaseModelPricingForManagement(model string) (*ModelPricing, error) {
	return s.baseModelPricingAt(model, time.Now())
}

func (s *SettingService) GetManagedPrices(ctx context.Context, fresh bool) (*ManagedPrices, error) {
	raw, err := s.readModelSetting(ctx, "pricing", fresh)
	if err != nil {
		return nil, err
	}
	p := &ManagedPrices{Overrides: map[string]ManagedPrice{}, External: map[string]ManagedPrice{}}
	if raw != "" && (json.Unmarshal([]byte(raw), p) != nil || p.Version < 0 || p.Overrides == nil) {
		return nil, ErrModelPolicyUnavailable
	}
	return p, nil
}
func validateManagedTokenPrice(p *ManagedTokenPrice, platform string) error {
	if p == nil || p.Input == nil || p.Output == nil || p.CacheRead == nil {
		return invalidModelPolicy("Input, output and cache read prices are required")
	}
	for _, v := range []*float64{p.Input, p.Output, p.CacheRead, p.Cache5m, p.Cache1h} {
		if v != nil && (math.IsInf(*v, 0) || math.IsNaN(*v) || *v < 0 || *v > 1e9) {
			return invalidModelPolicy("Prices must be finite nonnegative values")
		}
	}
	if p.Cache5m == nil {
		v := *p.Input
		if platform == PlatformAnthropic {
			v *= 1.25
		}
		p.Cache5m = &v
	}
	if p.Cache1h == nil {
		v := *p.Input
		if platform == PlatformAnthropic {
			v *= 2
		}
		p.Cache1h = &v
	}
	return nil
}
func validateManagedPrice(p *ManagedPrice, platform string) error {
	if err := validateManagedTokenPrice(&p.ManagedTokenPrice, platform); err != nil {
		return err
	}
	if len(p.Tiers) > 32 {
		return invalidModelPolicy("Too many context tiers")
	}
	sort.Slice(p.Tiers, func(i, j int) bool { return p.Tiers[i].Above < p.Tiers[j].Above })
	last := 0
	for i := range p.Tiers {
		tier := &p.Tiers[i]
		if tier.Above <= last {
			return invalidModelPolicy("Context thresholds must be positive and unique")
		}
		last = tier.Above
		if !tier.Unpriced {
			if err := validateManagedTokenPrice(&tier.ManagedTokenPrice, platform); err != nil {
				return err
			}
		}
	}
	for mode, price := range p.Modes {
		if mode != "fast" && mode != "flex" && mode != "priority" {
			return invalidModelPolicy("Unknown price mode")
		}
		if price != nil {
			if err := validateManagedTokenPrice(price, platform); err != nil {
				return err
			}
		}
	}
	if p.Image != nil {
		if err := validateManagedTokenPrice(p.Image, platform); err != nil {
			return err
		}
	}
	return nil
}
func (s *SettingService) SaveManagedPrice(ctx context.Context, platform, id string, version int64, price *ManagedPrice) (*ManagedPrices, error) {
	policy, err := s.GetModelPolicy(ctx, platform, true)
	if err != nil {
		return nil, err
	}
	if _, ok := policy.Models[id]; !ok {
		return nil, invalidModelPolicy("Unknown model")
	}
	p, err := s.GetManagedPrices(ctx, true)
	if err != nil {
		return nil, err
	}
	if p.Version != version {
		return nil, ErrModelPolicyConflict
	}
	if price == nil {
		delete(p.Overrides, id)
	} else {
		if err = validateManagedPrice(price, platform); err != nil {
			return nil, err
		}
		p.Overrides[id] = *price
	}
	p.Version++
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err = s.saveModelSetting(ctx, "pricing", version, p); err != nil {
		return nil, err
	}
	return p, nil
}
func (s *SettingService) managedPrice(ctx context.Context, id string) (*ManagedPrice, error) {
	p, err := s.GetManagedPrices(ctx, false)
	if err != nil {
		return nil, err
	}
	for _, candidate := range []string{id, strings.ToLower(id)} {
		if row, ok := p.Overrides[candidate]; ok {
			return &row, nil
		}
		if row, ok := p.External[candidate]; ok {
			return &row, nil
		}
	}
	return nil, nil
}
func applyManagedTokenPrice(base *ModelPricing, p ManagedTokenPrice) *ModelPricing {
	next := ModelPricing{}
	if base != nil {
		next = *base
	}
	next.InputPricePerToken = *p.Input / 1e6
	next.OutputPricePerToken = *p.Output / 1e6
	next.CacheReadPricePerToken = *p.CacheRead / 1e6
	if p.Cache5m != nil {
		next.CacheCreationPricePerToken = *p.Cache5m / 1e6
		next.CacheCreation5mPrice = *p.Cache5m / 1e6
		next.CacheCreationPriceExplicit = true
	}
	if p.Cache1h != nil {
		next.CacheCreation1hPrice = *p.Cache1h / 1e6
		next.SupportsCacheBreakdown = true
	}
	return &next
}
func (s *BillingService) getModelPricingAt(model string, at time.Time) (*ModelPricing, error) {
	base, baseErr := s.baseModelPricingAt(model, at)
	if s.pricingService == nil || s.pricingService.modelSettings == nil {
		return base, baseErr
	}
	// Legacy public quote callers have no request context. Bound this read at
	// that boundary; execution and settlement consume ModelPricingView.Pin(ctx)
	// and the durable card rather than resolving the current price here.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	p, err := s.pricingService.modelSettings.managedPrice(ctx, model)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return base, baseErr
	}
	base = applyManagedTokenPrice(base, p.ManagedTokenPrice)
	base.managedPrice = p
	if p.Image != nil {
		base.ImageInputPricePerToken = *p.Image.Input / 1e6
		base.ImageOutputPricePerToken = *p.Image.Output / 1e6
		base.ImageCacheReadPricePerToken = *p.Image.CacheRead / 1e6
		base.ImageOutputPriceExplicit = true
	}
	return base, nil
}
func prepareManagedPricing(base *ModelPricing, tokens UsageTokens, tier string, applyContext bool) (*ModelPricing, string, error) {
	if base == nil || base.managedPrice == nil {
		return base, tier, nil
	}
	p := base.managedPrice
	next := *base
	if len(p.Tiers) > 0 && applyContext {
		contextTokens := tokens.InputTokens + tokens.CacheReadTokens + tokens.CacheCreationTokens
		var selected *ManagedPriceTier
		for i := range p.Tiers {
			if contextTokens > p.Tiers[i].Above {
				selected = &p.Tiers[i]
			}
		}
		if selected != nil {
			if selected.Unpriced {
				return nil, tier, ErrModelPricingUnavailable
			}
			next = *applyManagedTokenPrice(&next, selected.ManagedTokenPrice)
		}
		next.LongContextInputThreshold = 0
	}
	if price, exists := p.Modes[normalizeBillingServiceTier(tier)]; exists {
		if price == nil {
			return nil, tier, ErrModelPricingUnavailable
		}
		next = *applyManagedTokenPrice(&next, *price)
		// Explicit mode prices already contain the premium/discount, including zero.
		tier = ""
	}
	return &next, tier, nil
}

// SyncManagedPrices reuses the existing bounded models.dev collector. It saves
// external facts separately, so synchronization never overwrites an operator price.
func (s *AccountTestService) SyncManagedPrices(ctx context.Context, settings *SettingService, platform string, expected int64, account *Account) (*ManagedPrices, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	policy, err := settings.GetModelPolicy(ctx, platform, true)
	if err != nil {
		return nil, err
	}
	p, err := settings.GetManagedPrices(ctx, true)
	if err != nil {
		return nil, err
	}
	if p.Version != expected {
		return nil, ErrModelPolicyConflict
	}
	if err := authz.Revalidate(ctx); err != nil {
		return nil, infraerrors.Forbidden("ADMIN_PERMISSION_DENIED", "Model price synchronization authorization expired")
	}
	if s == nil || s.httpUpstream == nil {
		return nil, ErrModelPolicyUnavailable
	}
	// The registry is public. Never turn a pricing permission into an account
	// probe or select a supplier's proxy, credentials or TLS identity.
	registry, err := s.fetchModelsDevRegistry(ctx, &Account{})
	if err != nil {
		return nil, infraerrors.New(502, "MODEL_PRICE_SOURCE_FAILED", "Failed to read the public model price registry").WithCause(err)
	}
	provider, ok := registry[platform]
	if !ok {
		return nil, invalidModelPolicy("Provider is absent from the price registry")
	}
	if p.External == nil {
		p.External = map[string]ManagedPrice{}
	}
	count := 0
	for id := range policy.Models {
		row, exists := provider.Models[id]
		if !exists || row.Cost.Input == nil || row.Cost.Output == nil || row.Cost.CacheRead == nil {
			continue
		}
		price := ManagedPrice{ManagedTokenPrice: ManagedTokenPrice{Input: row.Cost.Input, Output: row.Cost.Output, CacheRead: row.Cost.CacheRead, Cache5m: row.Cost.CacheWrite}}
		if validateManagedPrice(&price, platform) != nil {
			continue
		}
		p.External[id] = price
		count++
	}
	if count == 0 {
		return nil, invalidModelPolicy("Registry returned no complete model prices")
	}
	p.Version++
	p.Source = "models.dev"
	p.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	p.UpdatedAt = p.SyncedAt
	if err := authz.Revalidate(ctx); err != nil {
		return nil, infraerrors.Forbidden("ADMIN_PERMISSION_DENIED", "Model price synchronization authorization expired")
	}
	if err = settings.saveModelSetting(ctx, "pricing", expected, p); err != nil {
		return nil, err
	}
	return p, nil
}
