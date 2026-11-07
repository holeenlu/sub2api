package routing

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSamplingUnitPreservesNativeZeroAndRange(t *testing.T) {
	require.Zero(t, SamplingUnit(0))
	for _, seed := range []uint64{1, 77, 0x9e3779b97f4a7c15} {
		require.GreaterOrEqual(t, SamplingUnit(seed), 0.0)
		require.Less(t, SamplingUnit(seed), 1.0)
	}
}

func TestOrderCandidatesPreservesBusinessLayersAndCapacity(t *testing.T) {
	pool := []Candidate{
		{ID: 1, Priority: 0, ErrorRate: 0.8, Capacity: 1},
		{ID: 2, Priority: 2, Capacity: 1},
		{ID: 3, Priority: 1, Capacity: 1},
		{ID: 4, Priority: 0, LoadKnown: true, Limit: 1, CurrentConcurrency: 1, Capacity: 1},
	}
	ordered := OrderCandidates(pool, OrderOptions{Seed: 7, DegradedErrorRate: 0.5})
	ids := make([]int64, 0, len(ordered))
	for _, candidate := range ordered {
		ids = append(ids, candidate.ID)
	}
	require.Equal(t, []int64{3, 2, 1, 4}, ids)
	require.False(t, pool[0].Degraded, "normal ordering must not mutate caller observations")
}

func TestOrderCandidatesIdentityWeightsSurviveFiltering(t *testing.T) {
	pool := []Candidate{{ID: 71, Capacity: 3, Score: 0.5}, {ID: 72, Capacity: 1, Score: 0.2}, {ID: 73, Capacity: 2, Score: 0.8}}
	options := OrderOptions{Seed: 77, DegradedErrorRate: 0.5}
	full := OrderCandidates(pool, options)
	filtered := OrderCandidates([]Candidate{pool[0], pool[2]}, options)
	keys := map[int64]float64{}
	for _, value := range full {
		keys[value.ID] = value.OrderKey
	}
	for _, value := range filtered {
		require.Equal(t, keys[value.ID], value.OrderKey)
	}
}
