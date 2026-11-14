package service

import (
	"context"
	"math"
	"sort"
	"time"
)

const schedulerLoadBatchSize = 32

type schedulerHealthSample struct {
	errorRate, ttft float64
	hasTTFT         bool
	latencyFactor   float64
	cohort          string
}

type schedulerBusinessLayer struct {
	degraded               bool
	priority, subscription int
}

func sameOpenAIBusinessLayer(a, b openAIAccountCandidateScore) bool {
	return a.degraded == b.degraded && a.account.Priority == b.account.Priority && a.preference == b.preference && a.costBand == b.costBand
}

// Snapshot cost bands and estimated capacity once per selection. Retries reuse
// these maps even when the cheapest candidate disappears or load is refreshed.
func (s *defaultOpenAIAccountScheduler) prepareOpenAIBusinessLayers(ctx context.Context, req OpenAIAccountScheduleRequest, accounts []*Account) OpenAIAccountScheduleRequest {
	if req.capacities != nil {
		return req
	}
	if req.UseUpstreamTokenCost {
		req.costBands = make(map[int64]int, len(accounts))
	}
	req.capacities = make(map[int64]int, len(accounts))
	req.limits = make(map[int64]int)
	req.health = make(map[int64]schedulerHealthSample)
	layers := make(map[schedulerBusinessLayer][]*Account)
	for _, a := range accounts {
		if a == nil {
			continue
		}
		limit := nativeSchedulingConcurrency(ctx, a, s.service.settingService)
		if limit != a.Concurrency {
			req.limits[a.ID] = limit
		}
		errorRate, ttft, hasTTFT := s.stats.snapshot(a.ID, canonicalOpenAIAccountSchedulingModel(a, req.RequestedModel, req.RequireCompact), s.schedulingHealthCohort(ctx, a, req))
		if errorRate > 0 || hasTTFT {
			req.health[a.ID] = schedulerHealthSample{errorRate: errorRate, ttft: ttft, hasTTFT: hasTTFT, latencyFactor: 0.5, cohort: s.schedulingHealthCohort(ctx, a, req)}
		}
		key := schedulerBusinessLayer{degraded: errorRate > openAIHealthDegradedErrorRate, priority: a.Priority}
		if req.SubscriptionPriority && !a.IsOpenAIChatGPTSubscription() {
			key.subscription = 1
		}
		layers[key] = append(layers[key], a)
	}
	now := time.Now()
	var oauthRate *float64
	if req.UseUpstreamTokenCost {
		oauthRate = s.service.openAIOAuthSchedulingRateMultiplier(ctx)
	}
	for _, layer := range layers {
		if req.UseUpstreamTokenCost {
			type rateSample struct {
				account *Account
				rate    float64
				known   bool
			}
			samples := make([]rateSample, 0, len(layer))
			for _, a := range layer {
				rate, known := openAISchedulingRate(a, now, oauthRate)
				known = known && rate >= 0 && !math.IsInf(rate, 0) && !math.IsNaN(rate)
				samples = append(samples, rateSample{a, rate, known})
			}
			sort.Slice(samples, func(i, j int) bool {
				a, b := samples[i], samples[j]
				if a.known != b.known {
					return a.known
				}
				if a.known && a.rate != b.rate {
					return a.rate < b.rate
				}
				return a.account.ID < b.account.ID
			})
			band, anchor := -1, -1.0
			wasKnown := true
			for _, sample := range samples {
				if band < 0 || sample.known != wasKnown || (sample.known && (sample.rate > anchor*1.10 || anchor == 0 && sample.rate > 0)) {
					band++
					anchor = sample.rate
				}
				req.costBands[sample.account.ID] = band
				wasKnown = sample.known
			}
		}
		// Compare only the same request cohort and business layer. Median ratios
		// are robust to a single outlier and remain neutral without evidence.
		type latencyCohort struct {
			mode string
			cost int
		}
		cohorts := make(map[latencyCohort][]float64)
		for _, a := range layer {
			h := req.health[a.ID]
			if h.hasTTFT && h.ttft > 0 {
				key := latencyCohort{h.cohort, req.costBands[a.ID]}
				cohorts[key] = append(cohorts[key], h.ttft)
			}
		}
		medians := make(map[latencyCohort]float64, len(cohorts))
		for cohort, latencies := range cohorts {
			sort.Float64s(latencies)
			median := latencies[len(latencies)/2]
			if len(latencies)%2 == 0 {
				median = (median + latencies[len(latencies)/2-1]) / 2
			}
			medians[cohort] = median
		}
		for _, a := range layer {
			h := req.health[a.ID]
			if median := medians[latencyCohort{h.cohort, req.costBands[a.ID]}]; h.hasTTFT && median > 0 {
				h.latencyFactor = math.Max(0.1, math.Min(0.9, median/(median+h.ttft)))
				req.health[a.ID] = h
			}
		}

		maxFinite := make(map[int]int)
		for _, a := range layer {
			band := req.costBands[a.ID]
			if req.concurrencyLimit(a) > 0 {
				maxFinite[band] = max(maxFinite[band], schedulingLoadFactor(a, req.concurrencyLimit(a)))
			}
		}
		for _, a := range layer {
			capacity := schedulingLoadFactor(a, req.concurrencyLimit(a))
			if req.concurrencyLimit(a) <= 0 && (a.LoadFactor == nil || *a.LoadFactor <= 0) {
				capacity = max(1, maxFinite[req.costBands[a.ID]])
			}
			req.capacities[a.ID] = capacity
		}
	}
	return req
}

func buildOpenAIAccountLoadRequestWithCapacity(accounts []*Account, capacities map[int64]int) []AccountWithConcurrency {
	result := buildOpenAIAccountLoadRequest(accounts)
	for i := range result {
		if capacity := capacities[result[i].ID]; capacity > 0 {
			result[i].MaxConcurrency = capacity
		}
	}
	return result
}

// Native ordering remains priority/reset/load/LRU; only its capacity denominator
// follows the same within-layer estimate for unlimited accounts.
func gatewayAccountLoadRequest(ctx context.Context, accounts []*Account, preferOAuth bool, settings *SettingService) []AccountWithConcurrency {
	type layerKey struct {
		priority int
		oauth    bool
	}
	maximum := make(map[layerKey]int)
	limits := make(map[int64]int)
	limitFor := func(a *Account) int {
		if value, ok := limits[a.ID]; ok {
			return value
		}
		return a.Concurrency
	}
	key := func(a *Account) layerKey { return layerKey{a.Priority, preferOAuth && a.IsOAuth()} }
	for _, a := range accounts {
		limit := nativeSchedulingConcurrency(ctx, a, settings)
		if limit != a.Concurrency {
			limits[a.ID] = limit
		}
		if limit > 0 {
			maximum[key(a)] = max(maximum[key(a)], schedulingLoadFactor(a, limitFor(a)))
		}
	}
	result := make([]AccountWithConcurrency, 0, len(accounts))
	for _, a := range accounts {
		capacity := schedulingLoadFactor(a, limitFor(a))
		if limitFor(a) <= 0 && (a.LoadFactor == nil || *a.LoadFactor <= 0) {
			capacity = max(1, maximum[key(a)])
		}
		result = append(result, AccountWithConcurrency{ID: a.ID, MaxConcurrency: capacity})
	}
	return result
}

// Preserve the operator's relative routing preference while reducing its
// capacity estimate with an active quota/model ceiling. Unlimited accounts only
// borrow their layer's finite estimate when no current protection applies.
func schedulingLoadFactor(account *Account, limit int) int {
	if limit <= 0 {
		return account.EffectiveLoadFactor()
	}
	if account.LoadFactor == nil || *account.LoadFactor <= 0 {
		return limit
	}
	if account.Concurrency > limit {
		return max(1, int(math.Ceil(float64(*account.LoadFactor)*float64(limit)/float64(account.Concurrency))))
	}
	return account.EffectiveLoadFactor()
}

func (req OpenAIAccountScheduleRequest) concurrencyLimit(account *Account) int {
	if limit, ok := req.limits[account.ID]; ok {
		return limit
	}
	return account.Concurrency
}
