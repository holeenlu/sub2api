package service

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// This extends the existing Ops settings document; it has no independent
// scheduler, quota ledger or background worker.
type SchedulingQuotaProtection struct {
	AnthropicTierThresholdsEnabled bool    `json:"anthropic_tier_thresholds_enabled"`
	AnthropicDefault5h             int     `json:"anthropic_default_5h"`
	AnthropicDefault7d             int     `json:"anthropic_default_7d"`
	AnthropicMax5h                 int     `json:"anthropic_max_5h"`
	AnthropicMax7d                 int     `json:"anthropic_max_7d"`
	CriticalBandPercent            float64 `json:"critical_band_percent"`
	LargeColdInputTokens           int     `json:"large_cold_input_tokens"`
	GuardLargeCold                 bool    `json:"guard_large_cold"`
	SampleMaxAgeSeconds            int     `json:"sample_max_age_seconds"`
}

//go:embed scheduling_quota_defaults.json
var schedulingQuotaDefaultsJSON []byte

type schedulingQuotaBounds struct{ Min, Max, Step float64 }

var schedulingQuotaCatalog = func() struct {
	Protection SchedulingQuotaProtection
	Limits     map[string]schedulingQuotaBounds
} {
	var catalog struct {
		Protection SchedulingQuotaProtection
		Limits     map[string]schedulingQuotaBounds
	}
	if err := json.Unmarshal(schedulingQuotaDefaultsJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog
}()

func defaultSchedulingQuotaProtection() SchedulingQuotaProtection {
	return schedulingQuotaCatalog.Protection
}
func effectiveSchedulingQuotaProtection(values ...SchedulingQuotaProtection) SchedulingQuotaProtection {
	if len(values) == 0 || values[0] == (SchedulingQuotaProtection{}) {
		return defaultSchedulingQuotaProtection()
	}
	return values[0]
}
func (p SchedulingQuotaProtection) validate() error {
	p = effectiveSchedulingQuotaProtection(p)
	for key, value := range map[string]float64{
		"anthropic_default_5h": float64(p.AnthropicDefault5h), "anthropic_default_7d": float64(p.AnthropicDefault7d),
		"anthropic_max_5h": float64(p.AnthropicMax5h), "anthropic_max_7d": float64(p.AnthropicMax7d),
		"critical_band_percent": p.CriticalBandPercent, "large_cold_input_tokens": float64(p.LargeColdInputTokens), "sample_max_age_seconds": float64(p.SampleMaxAgeSeconds),
	} {
		bounds := schedulingQuotaCatalog.Limits[key]
		if math.IsNaN(value) || math.IsInf(value, 0) || value < bounds.Min || value > bounds.Max {
			return fmt.Errorf("%s must be between %g and %g", key, bounds.Min, bounds.Max)
		}
	}

	return nil
}

const schedulingQuotaAnthropicTierKey = "scheduler_quota_anthropic_max"

func anthropicSchedulingMaxPlan(account *Account) bool {
	if account == nil {
		return false
	}
	for _, key := range []string{"subscription_type", "billing_type", "seat_tier"} {
		value := strings.ToLower(strings.TrimSpace(account.GetCredential(key)))
		if value == "max" || strings.HasPrefix(value, "max_") || strings.HasPrefix(value, "max-") {
			return true
		}
	}
	// Reduced scheduler metadata keeps only this non-secret derived plan bit.
	if account.GetCredential("access_token") != "" {
		return false
	}
	maxPlan, _ := account.Extra[schedulingQuotaAnthropicTierKey].(bool)
	return maxPlan
}
func anthropicSchedulingWindowThreshold(account *Account, thresholds map[string]int, window string, p SchedulingQuotaProtection) int {
	if override, ok := accountSchedulingThresholdOverride(account, accountSchedulingThresholdCredentialKey); ok {
		return override
	}
	global, _ := lookupAccountSchedulingThreshold(thresholds, PlatformAnthropic)
	if !p.AnthropicTierThresholdsEnabled {
		return global
	}
	limit := p.AnthropicDefault5h
	if window == "7d" {
		limit = p.AnthropicDefault7d
	}
	if anthropicSchedulingMaxPlan(account) {
		limit = p.AnthropicMax5h
		if window == "7d" {
			limit = p.AnthropicMax7d
		}
	}
	// Activating the tier profile may tighten, never loosen an explicit global stop.
	if global > 0 && global < 100 {
		limit = min(limit, global)
	}
	return limit
}
