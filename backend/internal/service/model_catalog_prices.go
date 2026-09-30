package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type CatalogReferencePrices struct {
	Supplemental map[string]json.RawMessage `json:"supplemental,omitempty"`
	Revision     string                     `json:"revision"`
	Source       string                     `json:"source"`
	UpdatedAt    time.Time                  `json:"updated_at"`
	Models       map[string]json.RawMessage `json:"models"`
}

// Must be called under the price publication lock. No administrator overrides
// are applied to this distinct reference layer. Raw fields preserve explicit 0.
func (s *PricingService) setCatalogReferenceLocked(body []byte) {
	var models map[string]json.RawMessage
	if json.Unmarshal(body, &models) != nil {
		return
	}
	if models == nil {
		models = make(map[string]json.RawMessage)
	}
	s.referenceBase = append(json.RawMessage(nil), body...)
	for id, value := range s.referenceSupplement {
		if _, exists := models[id]; !exists {
			models[id] = value
		}
	}
	s.referencePrices = &CatalogReferencePrices{Revision: modelCatalogHash(models), Source: "pricing_catalog", UpdatedAt: time.Now().UTC(), Models: models}
}

func (s *PricingService) ReferencePrices() *CatalogReferencePrices {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.referencePrices == nil {
		return nil
	}
	copy := *s.referencePrices
	copy.Supplemental = make(map[string]json.RawMessage, len(s.referenceSupplement))
	for id, raw := range s.referenceSupplement {
		copy.Supplemental[id] = append(json.RawMessage(nil), raw...)
	}
	copy.Models = make(map[string]json.RawMessage, len(s.referencePrices.Models))
	for id, entry := range s.referencePrices.Models {
		copy.Models[id] = append(json.RawMessage(nil), entry...)
	}
	return &copy
}

func (s *PricingService) ReferencePrice(model string) (json.RawMessage, string, string) {
	if s == nil {
		return nil, "", ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.referencePrices == nil {
		return nil, "", ""
	}
	return append(json.RawMessage(nil), s.referencePrices.Models[model]...), s.referencePrices.Revision, s.referencePrices.Source
}

func (s *PricingService) PriceRevision() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return modelCatalogHash([]string{s.localHash, s.customFilesHash, s.referenceSupplementHash})
}

// Import validated data without rebuilding the server. The administrator's
// configured fallback/override files are still applied only to the sales layer.
func (s *ModelCatalogService) ImportPrices(ctx context.Context, body json.RawMessage) error {
	if s.prices == nil {
		return ErrModelCatalogUnavailable
	}
	var entries map[string]json.RawMessage
	if json.Unmarshal(body, &entries) != nil || len(entries) > 20000 {
		return fmt.Errorf("invalid reference price object")
	}
	for id, raw := range entries {
		if strings.TrimSpace(id) == "" || len(id) > 256 {
			return fmt.Errorf("invalid pricing model ID")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return fmt.Errorf("invalid price record")
		}
		for key, value := range fields {
			if !strings.Contains(key, "cost_per_") && !strings.Contains(key, "multiplier") {
				continue
			}
			var n float64
			if string(value) == "null" {
				continue
			}
			if json.Unmarshal(value, &n) != nil || n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
				return fmt.Errorf("invalid nonnegative price field %s", key)
			}
		}
	}
	// Validate the same parser used by billing before persisting the supplement.
	if _, _, err := s.prices.buildPricingData(body); err != nil {
		return err
	}
	if s.settings == nil || s.settings.settingRepo == nil {
		return ErrModelCatalogUnavailable
	}
	if err := s.repo.SavePrices(ctx, modelCatalogHash(entries), body); err != nil {
		return err
	}
	if err := s.settings.settingRepo.Set(ctx, "model_catalog_reference_prices", string(body)); err != nil {
		return err
	}
	return s.prices.applyReferenceSupplement(body)
}

func (s *PricingService) applyReferenceSupplement(body []byte) error {
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	var supplement map[string]json.RawMessage
	if err := json.Unmarshal(body, &supplement); err != nil {
		return err
	}
	hash := modelCatalogHash(supplement)
	s.mu.RLock()
	oldHash := s.referenceSupplementHash
	base := append(json.RawMessage(nil), s.referenceBase...)
	s.mu.RUnlock()
	if oldHash == hash {
		return nil
	}
	if len(base) == 0 {
		base = []byte("{}")
	}
	builder := &PricingService{cfg: s.cfg, referenceSupplement: supplement}
	data, fingerprint, err := builder.buildPricingData(base)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Do not overwrite a newer remote publication that raced this import.
	if string(s.referenceBase) != string(base) && len(s.referenceBase) > 0 {
		return fmt.Errorf("price source changed; retry supplemental import")
	}
	s.referenceSupplement = supplement
	s.referenceSupplementHash = hash
	s.pricingData = data
	s.customFilesHash = fingerprint
	s.setCatalogReferenceLocked(base)
	return nil
}

func (s *ModelCatalogService) ReferencePrices() *CatalogReferencePrices {
	return s.prices.ReferencePrices()
}

func (s *ModelCatalogService) restoreReferencePrices(ctx context.Context) {
	if s.settings == nil || s.settings.settingRepo == nil || s.prices == nil {
		return
	}
	raw, err := s.settings.settingRepo.GetValue(ctx, "model_catalog_reference_prices")
	if err != nil || raw == "" {
		return
	}
	if err := s.prices.applyReferenceSupplement([]byte(raw)); err != nil {
		return
	}
}

func catalogPricePointer(fields map[string]json.RawMessage, key string) *float64 {
	raw, ok := fields[key]
	if !ok || string(raw) == "null" {
		return nil
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil || n < 0 {
		return nil
	}
	return &n
}

func catalogPlazaReference(raw json.RawMessage, revision, source string) *PlazaOfficialPricing {
	if len(raw) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	out := &PlazaOfficialPricing{
		Source: source, Revision: revision,
		InputPrice: catalogPricePointer(fields, "input_cost_per_token"), OutputPrice: catalogPricePointer(fields, "output_cost_per_token"),
		CacheWritePrice: catalogPricePointer(fields, "cache_creation_input_token_cost"), CacheWrite1hPrice: catalogPricePointer(fields, "cache_creation_input_token_cost_above_1hr"),
		CacheReadPrice: catalogPricePointer(fields, "cache_read_input_token_cost"), ImageInputPrice: catalogPricePointer(fields, "input_cost_per_image_token"),
		ImageOutputPrice: catalogPricePointer(fields, "output_cost_per_image_token"), ImageCacheReadPrice: catalogPricePointer(fields, "cache_read_input_image_token_cost"),
	}
	var pricing LiteLLMModelPricing
	_ = json.Unmarshal(raw, &pricing)
	deriveLongContextFromAboveTierFields(raw, &pricing)
	if pricing.LongContextInputTokenThreshold > 0 && (pricing.LongContextInputCostMultiplier > 0 || pricing.LongContextOutputCostMultiplier > 0) {
		n := pricing.LongContextInputTokenThreshold
		base := PricingInterval{MinTokens: 0, MaxTokens: &n, InputPrice: out.InputPrice, OutputPrice: out.OutputPrice, CacheWritePrice: out.CacheWritePrice, CacheWrite1hPrice: out.CacheWrite1hPrice, CacheReadPrice: out.CacheReadPrice}
		high := base
		high.MinTokens = n
		high.MaxTokens = nil
		high.SortOrder = 1
		scale := func(p *float64, m float64) *float64 {
			if p == nil {
				return nil
			}
			if m <= 0 {
				m = 1
			}
			v := *p * m
			return &v
		}
		high.InputPrice = scale(base.InputPrice, pricing.LongContextInputCostMultiplier)
		high.OutputPrice = scale(base.OutputPrice, pricing.LongContextOutputCostMultiplier)
		high.CacheWritePrice = scale(base.CacheWritePrice, pricing.LongContextInputCostMultiplier)
		high.CacheWrite1hPrice = scale(base.CacheWrite1hPrice, pricing.LongContextInputCostMultiplier)
		high.CacheReadPrice = scale(base.CacheReadPrice, pricing.LongContextInputCostMultiplier)
		out.Intervals = []PricingInterval{base, high}
	}
	return out
}

// Called by the bounded background loop. New IDs bypass the ordinary interval,
// but refresh attempts are still coalesced to at most once per minute.
func (s *ModelCatalogService) refreshPricesIfDue(cfg ModelCatalogSettings) {
	if s.prices != nil {
		s.prices.catalogRefreshManaged.Store(cfg.Enabled)
	}
	if !cfg.Enabled || s.prices == nil || s.prices.cfg == nil || s.prices.remoteClient == nil || s.prices.cfg.Pricing.RemoteURL == "" {
		return
	}
	due := time.Since(s.lastPriceCheck)
	if due < time.Minute || (!s.priceRefreshRequested.Load() && due < time.Duration(cfg.PriceIntervalSeconds)*time.Second) {
		return
	}
	s.lastPriceCheck = time.Now()
	s.priceRefreshRequested.Store(false)
	_ = s.prices.ForceUpdate() // Last successful prices remain active on error.
}
