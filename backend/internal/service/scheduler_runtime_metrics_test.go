//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulingRuntimeMetricsClippingDoesNotBecomeAffinityEscape(t *testing.T) {
	ctx := WithSchedulingTrace(t.Context(), nil)
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "filtered", Reason: strings.Repeat("x", 129)})
	require.False(t, *readSchedulingTraceSnapshot(t, ctx).DecisionsComplete)
	before := SnapshotSchedulingRuntimeMetrics()
	RecordSchedulingCacheTokens(ctx, 5, 3)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens[0]+5, after.InputTokens[0])
	require.Equal(t, before.CacheReadTokens[0]+3, after.CacheReadTokens[0])
	require.Equal(t, before.InputTokens[1], after.InputTokens[1])
	require.Equal(t, before.CacheReadTokens[1], after.CacheReadTokens[1])
}

func TestSchedulingRuntimeMetricsAffinityCohortSurvivesFreezeAndTurnReset(t *testing.T) {
	ctx := WithSchedulingTrace(t.Context(), nil)
	recordSchedulingEscape(ctx, "concurrency_full")
	require.True(t, *readSchedulingTraceSnapshot(t, ctx).DecisionsComplete, "affinity escape alone is not trace truncation")
	frozen := FreezeSchedulingMetricsContext(ctx)
	// Evict the affinity event and then start a fresh WS turn before usage work runs.
	for range 100 {
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "slot", Action: "acquired"})
	}
	require.False(t, *readSchedulingTraceSnapshot(t, ctx).DecisionsComplete)
	resetSchedulingTrace(ctx)
	worker := WithSchedulingMetricsContext(context.Background(), frozen)
	before := SnapshotSchedulingRuntimeMetrics()
	RecordSchedulingCacheTokens(worker, 11, 7)
	RecordSchedulingCacheTokens(ctx, 2, 1)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens[1]+11, after.InputTokens[1])
	require.Equal(t, before.CacheReadTokens[1]+7, after.CacheReadTokens[1])
	require.Equal(t, before.InputTokens[0]+2, after.InputTokens[0])
	require.Equal(t, before.CacheReadTokens[0]+1, after.CacheReadTokens[0])
}

func TestSchedulingRuntimeMetricsOrdinaryFrozenCohortIgnoresLaterEscape(t *testing.T) {
	ctx := WithSchedulingTrace(t.Context(), nil)
	frozen := FreezeSchedulingMetricsContext(ctx)
	recordSchedulingEscape(ctx, "error_rate")
	worker := WithSchedulingMetricsContext(context.Background(), frozen)
	before := SnapshotSchedulingRuntimeMetrics()
	RecordSchedulingCacheTokens(worker, 13, 9)
	RecordSchedulingCacheTokens(ctx, 5, 4)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens[0]+13, after.InputTokens[0])
	require.Equal(t, before.CacheReadTokens[0]+9, after.CacheReadTokens[0])
	require.Equal(t, before.InputTokens[1]+5, after.InputTokens[1])
	require.Equal(t, before.CacheReadTokens[1]+4, after.CacheReadTokens[1])
}
