//go:build unit

package service

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountRuntimeStatsIdleErrorsRecover(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	now := time.Now()
	ttft := 16000
	for range 3 {
		stats.reportAt(1, false, &ttft, now)
	}
	rate, _, _ := stats.snapshotAt(1, now)
	require.InDelta(t, 0.488, rate, 1e-10)
	half, latency, known := stats.snapshotAt(1, now.Add(2*time.Minute))
	require.InDelta(t, rate/2, half, 1e-10)
	require.True(t, known)
	require.Equal(t, float64(ttft), latency, "idle recovery cannot invent latency samples")
	recovered, _, _ := stats.snapshotAt(1, now.Add(6*time.Minute))
	require.InDelta(t, rate/8, recovered, 1e-10)
	stats.reportAt(1, false, nil, now.Add(6*time.Minute))
	after, _, _ := stats.snapshotAt(1, now.Add(6*time.Minute))
	require.InDelta(t, 0.2+0.8*recovered, after, 1e-10)
	stats.reportAt(1, true, nil, now.Add(5*time.Minute))
	afterOlder, _, _ := stats.snapshotAt(1, now.Add(6*time.Minute))
	require.InDelta(t, 0.8*after, afterOlder, 1e-10, "older completions cannot reverse time")
}

func TestOpenAIAccountRuntimeStatsConcurrentDecay(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	now := time.Now()
	stats.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 100 {
				stats.report(1, false, nil)
				stats.snapshot(1)
			}
		})
	}
	wg.Wait()
	rate, _, _ := stats.snapshot(1)
	require.InDelta(t, 1, rate, 1e-10)
	half, _, _ := stats.snapshotAt(1, now.Add(2*time.Minute))
	require.InDelta(t, rate/2, half, 1e-10)
	require.Equal(t, 1, stats.size())
}
