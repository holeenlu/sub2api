package routing

import (
	"math"
	"sort"
)

// Candidate contains only scheduling observations. The service adapter retains
// resource entities; Index relates this value back to the caller's candidate.
// No candidate is itself a send permission or a capacity lease.
type Candidate struct {
	Index                                                       int
	ID                                                          int64
	Priority, Preference, CostBand, DomainPenalty               int
	Capacity, Limit, CurrentConcurrency, LoadRate, WaitingCount int
	LoadKnown, Subscription, Degraded                           bool
	ErrorRate, Score, OrderKey                                  float64
}

// OrderOptions projects existing request preferences, not a new scoring policy.
type OrderOptions struct {
	Seed                 uint64
	SubscriptionPriority bool
	SamplingOnly         bool
	DegradedErrorRate    float64
}

// OrderCandidates retains all business layers, ready before waiting, using the
// existing identity-seeded weighted sampling and tie breaks. It mutates pool in
// sampling-only mode exactly as the former caller-owned scratch implementation.
func OrderCandidates(pool []Candidate, options OrderOptions) []Candidate {
	var ready []Candidate
	if options.SamplingOnly {
		ready = pool[:0]
	} else {
		ready = make([]Candidate, 0, len(pool))
	}
	waiting := make([]Candidate, 0)
	for _, candidate := range pool {
		if options.SubscriptionPriority && !candidate.Subscription {
			candidate.Preference = 1
		}
		candidate.Degraded = candidate.ErrorRate > options.DegradedErrorRate
		capacity := float64(max(1, candidate.Capacity))
		if candidate.LoadKnown && candidate.Limit > 0 {
			headroom := clamp01(float64(candidate.CurrentConcurrency) / float64(candidate.Limit))
			capacity *= math.Max(0.01, 1-headroom)
		}
		if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			candidate.Score = 0
		}
		weight := capacity * math.Max(0.1, 1+candidate.Score)
		seed := options.Seed ^ (uint64(candidate.ID) * 0x9e3779b97f4a7c15)
		if seed == 0 {
			seed = 0x9e3779b97f4a7c15
		}
		candidate.OrderKey = -math.Log(math.Max(SamplingUnit(seed), math.SmallestNonzeroFloat64)) / weight
		if candidate.LoadKnown && candidate.Limit > 0 && candidate.CurrentConcurrency >= candidate.Limit {
			waiting = append(waiting, candidate)
		} else {
			ready = append(ready, candidate)
		}
	}
	if !options.SamplingOnly {
		for _, bucket := range [][]Candidate{ready, waiting} {
			sort.Slice(bucket, func(i, j int) bool { return Precedes(bucket[i], bucket[j]) })
		}
	}
	return append(ready, waiting...)
}

// Precedes compares already-scored candidates, including candidates in the
// large-pool heap. It performs no new sampling or policy interpretation.
func Precedes(left Candidate, right Candidate) bool {
	// Confirmed model-scoped degradation yields to healthy capacity, even in a
	// lower business tier. Otherwise a health escape would select its owner again.
	if left.Degraded != right.Degraded {
		return !left.Degraded
	}
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	if left.Preference != right.Preference {
		return left.Preference < right.Preference
	}
	if left.CostBand != right.CostBand {
		return left.CostBand < right.CostBand
	}
	if left.DomainPenalty != right.DomainPenalty {
		return left.DomainPenalty < right.DomainPenalty
	}
	if left.OrderKey != right.OrderKey {
		return left.OrderKey < right.OrderKey
	}
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	if left.LoadRate != right.LoadRate {
		return left.LoadRate < right.LoadRate
	}
	if left.WaitingCount != right.WaitingCount {
		return left.WaitingCount < right.WaitingCount
	}
	return left.ID < right.ID
}

// SamplingUnit is the existing xorshift64* one-sample function. Native load
// batches intentionally retain a zero seed's zero result; OpenAI's caller
// normalizes its seed before invoking it. The returned value is in [0,1).
func SamplingUnit(state uint64) float64 {
	state ^= state >> 12
	state ^= state << 25
	state ^= state >> 27
	return float64((state*2685821657736338717)>>11) / (1 << 53)
}
