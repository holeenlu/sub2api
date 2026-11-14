//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type schedulingTraceTestSnapshot struct {
	Summary           schedulingSummary    `json:"summary"`
	Dropped           int                  `json:"dropped_events"`
	DecisionsComplete *bool                `json:"decisions_complete"`
	Events            []SchedulingDecision `json:"events"`
}

func readSchedulingTraceSnapshot(t *testing.T, ctx context.Context) schedulingTraceTestSnapshot {
	t.Helper()
	events := WithOpsSchedulingTrace(ctx, nil)
	require.Len(t, events, 1)
	require.LessOrEqual(t, len(events[0].Detail), maxSchedulingTraceBytes)
	var snapshot schedulingTraceTestSnapshot
	require.NoError(t, json.Unmarshal([]byte(events[0].Detail), &snapshot))
	require.NotNil(t, snapshot.DecisionsComplete)
	return snapshot
}

func TestSchedulingTraceCompletenessRetainsKnownSummaryCounts(t *testing.T) {
	ctx := WithSchedulingTrace(t.Context(), nil)
	reasons := make(map[string]int)
	for i := range 32 {
		reasons[fmt.Sprintf("reason_%02d", i)] = 1
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "filtered", Excluded: reasons})
	first := readSchedulingTraceSnapshot(t, ctx)
	require.True(t, *first.DecisionsComplete)
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "filtered", Excluded: map[string]int{"reason_00": 7, "overflow": 3}})
	last := readSchedulingTraceSnapshot(t, ctx)
	require.Equal(t, 8, last.Summary.Excluded["reason_00"])
	require.Len(t, last.Summary.Excluded, 32)
	require.NotContains(t, last.Summary.Excluded, "overflow")
	require.False(t, *last.DecisionsComplete, "lost summary facts must be explicit even with both decisions retained")
	require.Zero(t, last.Dropped)
	require.Equal(t, 1, first.Summary.Excluded["reason_00"], "published snapshots remain immutable")
}

func TestSchedulingTraceCompletenessReportsPayloadClipping(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event SchedulingDecision
	}{
		{name: "text", event: SchedulingDecision{Stage: "selection", Action: "stopped", Reason: strings.Repeat("x", 129)}},
		{name: "reason key", event: SchedulingDecision{Stage: "filter", Action: "filtered", Excluded: map[string]int{strings.Repeat("k", 33): 2}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithSchedulingTrace(t.Context(), nil)
			RecordSchedulingDecision(ctx, tc.event)
			snapshot := readSchedulingTraceSnapshot(t, ctx)
			require.False(t, *snapshot.DecisionsComplete)
			require.Zero(t, snapshot.Dropped)
			entry := &OpsInsertErrorLogInput{UpstreamErrors: WithOpsSchedulingTrace(ctx, nil)}
			require.NoError(t, sanitizeOpsUpstreamErrors(entry))
			out := BuildOpsRequestDiagnostics(&OpsErrorLogDetail{UpstreamErrors: *entry.UpstreamErrorsJSON})
			require.Contains(t, out.Gaps, "incomplete_scheduling")
			require.Zero(t, out.DroppedDecisions, "a clipped field is not a dropped decision")
			require.Zero(t, out.DroppedAttempts, "local decisions are not upstream attempts")
			require.Equal(t, "recorded_events_only", out.Coverage)
			resetSchedulingTrace(ctx)
			require.Empty(t, WithOpsSchedulingTrace(ctx, nil))
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "slot", Action: "acquired"})
			require.True(t, *readSchedulingTraceSnapshot(t, ctx).DecisionsComplete)
		})
	}
}

func TestSchedulingTraceCompletenessCapsReasonsDeterministically(t *testing.T) {
	counts := make(map[string]int)
	for i := range 40 {
		counts[fmt.Sprintf("reason_%02d", i)] = i + 1
	}
	for range 8 {
		ctx := WithSchedulingTrace(t.Context(), nil)
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "filtered", Excluded: counts})
		snapshot := readSchedulingTraceSnapshot(t, ctx)
		require.False(t, *snapshot.DecisionsComplete)
		require.Len(t, snapshot.Events[0].Excluded, 32)
		for i := range 32 {
			require.Equal(t, i+1, snapshot.Events[0].Excluded[fmt.Sprintf("reason_%02d", i)])
		}
	}
}

func TestSchedulingTraceCompletenessCapsSummaryDeterministically(t *testing.T) {
	for range 8 {
		ctx := WithSchedulingTrace(t.Context(), nil)
		counts := make(map[string]int)
		for i := range 31 {
			counts[fmt.Sprintf("reason_%02d", i)] = 1
		}
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "filtered", Excluded: counts})
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Action: "filtered", Excluded: map[string]int{"z_new": 5, "a_new": 3, "reason_00": 2}})
		snapshot := readSchedulingTraceSnapshot(t, ctx)
		require.Equal(t, 3, snapshot.Summary.Excluded["a_new"])
		require.Equal(t, 3, snapshot.Summary.Excluded["reason_00"])
		require.NotContains(t, snapshot.Summary.Excluded, "z_new")
		require.False(t, *snapshot.DecisionsComplete)
	}
}

func TestSchedulingTraceCompletenessAccountsForEveryEvictedDecision(t *testing.T) {
	ctx := WithSchedulingTrace(t.Context(), nil)
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "retry", Action: "stopped", Reason: "keep_failure"})
	for range 100 {
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "slot", Action: "acquired"})
	}
	snapshot := readSchedulingTraceSnapshot(t, ctx)
	require.Equal(t, 101, len(snapshot.Events)+snapshot.Dropped)
	require.Equal(t, 100, snapshot.Summary.Attempts)
	require.False(t, *snapshot.DecisionsComplete)
	require.Equal(t, "keep_failure", snapshot.Events[0].Reason)
}

func TestSchedulingTraceCompletenessSurvivesAllEventEviction(t *testing.T) {
	var publish func()
	ctx := WithSchedulingTrace(t.Context(), func(job func()) bool { publish = job; return true })
	counts := make(map[string]int)
	for i := range 32 {
		counts[fmt.Sprintf("%02d%s", i, strings.Repeat("<", 30))] = 1
	}
	for range 4 {
		RecordSchedulingDecision(ctx, SchedulingDecision{
			Stage: "filter", Policy: strings.Repeat("<", 64), Layer: strings.Repeat("<", 64),
			Action: strings.Repeat("<", 32), Reason: strings.Repeat("<", 128), Excluded: counts,
		})
	}
	first := readSchedulingTraceSnapshot(t, ctx)
	require.Empty(t, first.Events, "escaped JSON forces every event out of the final byte budget")
	require.Equal(t, 4, first.Dropped)
	require.False(t, *first.DecisionsComplete)
	require.Equal(t, first, readSchedulingTraceSnapshot(t, ctx), "second export must retain the loss-only summary")
	LogSchedulingTrace(ctx, true)
	require.NotNil(t, publish, "loss-only snapshots must still be publishable")
	resetSchedulingTrace(ctx)
	publish()
	require.Empty(t, WithOpsSchedulingTrace(ctx, nil))
}

func TestSchedulingTraceDiagnosticsLegacyCompletenessIsUnknown(t *testing.T) {
	for _, detail := range []string{
		`{"events":[{"stage":"selection"}]}`,
		`{"decisions_complete":true,"events":[{"stage":"selection"}]}`,
	} {
		raw, err := json.Marshal([]*OpsUpstreamErrorEvent{{Kind: opsSchedulingTrace, Detail: detail}})
		require.NoError(t, err)
		out := BuildOpsRequestDiagnostics(&OpsErrorLogDetail{UpstreamErrors: string(raw)})
		require.NotContains(t, out.Gaps, "incomplete_scheduling")
		require.Contains(t, out.Gaps, "best_effort_capture")
		require.Equal(t, "recorded_events_only", out.Coverage)
	}
}
