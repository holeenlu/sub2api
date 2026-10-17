package service

import (
	"context"
	"math"
	"sort"
	"time"
)

type schedulerHealthSample struct {
	errorRate, ttft float64
	hasTTFT         bool
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
	req.costBands = make(map[int64]int, len(accounts))
	req.capacities = make(map[int64]int, len(accounts))
	req.health = make(map[int64]schedulerHealthSample, len(accounts))
	layers := make(map[schedulerBusinessLayer][]*Account)
	for _, a := range accounts {
		if a == nil {
			continue
		}
		errorRate, ttft, hasTTFT := s.stats.snapshot(a.ID, canonicalOpenAIAccountSchedulingModel(a, req.RequestedModel, req.RequireCompact))
		req.health[a.ID] = schedulerHealthSample{errorRate, ttft, hasTTFT}
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
		maxFinite := make(map[int]int)
		for _, a := range layer {
			band := req.costBands[a.ID]
			if a.Concurrency > 0 {
				maxFinite[band] = max(maxFinite[band], a.EffectiveLoadFactor())
			}
		}
		for _, a := range layer {
			capacity := a.EffectiveLoadFactor()
			if a.Concurrency <= 0 && (a.LoadFactor == nil || *a.LoadFactor <= 0) {
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
func gatewayAccountLoadRequest(accounts []*Account, preferOAuth bool) []AccountWithConcurrency {
	type layerKey struct {
		priority int
		oauth    bool
	}
	maximum := make(map[layerKey]int)
	key := func(a *Account) layerKey { return layerKey{a.Priority, preferOAuth && a.IsOAuth()} }
	for _, a := range accounts {
		if a.Concurrency > 0 {
			maximum[key(a)] = max(maximum[key(a)], a.EffectiveLoadFactor())
		}
	}
	result := make([]AccountWithConcurrency, 0, len(accounts))
	for _, a := range accounts {
		capacity := a.EffectiveLoadFactor()
		if a.Concurrency <= 0 && (a.LoadFactor == nil || *a.LoadFactor <= 0) {
			capacity = max(1, maximum[key(a)])
		}
		result = append(result, AccountWithConcurrency{ID: a.ID, MaxConcurrency: capacity})
	}
	return result
}
