//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloudLiveTerminalOwnerResets403OnceWithoutSettlement(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	counter := &openAI403CounterResetStub{}
	svc.rateLimitService = NewRateLimitService(nil, nil, nil, nil, nil)
	svc.rateLimitService.SetOpenAI403CounterCache(counter)
	svc.cloudSessions.terminalPolicy = svc.applyCloudOwnerTerminalPolicy
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, schedulingCohortKey{}, true)
	before := SnapshotSchedulingRuntimeMetrics()
	body, err := json.Marshal(map[string]any{"model": "client-model", "input": "go", "stream": true})
	require.NoError(t, err)
	ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
	done := make(chan struct{})
	go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
	result, err := svc.forwardOpenAICloud(ctx, c, in.Account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	<-done
	require.Equal(t, []int64{in.Account.ID}, counter.resetCalls)
	record := store.snapshot()
	require.False(t, record.Result.AccountPolicyApplied, "success reset occurs only after the fenced terminal save")
	require.NotNil(t, record.SchedulingEscaped)
	require.True(t, *record.SchedulingEscaped)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens[0], after.InputTokens[0])
	require.Equal(t, before.InputTokens[1]+uint64(record.Result.Usage.InputTokens), after.InputTokens[1])
	require.Equal(t, before.CacheReadTokens[1]+uint64(record.Result.Usage.CachedInputTokens), after.CacheReadTokens[1])
	// Repeated live terminal processing after persistence/publish trouble must
	// not erase failures accumulated after this turn's first observation.
	actor := &cloudSessionActor{service: svc.cloudSessions, record: record, ctx: ctx,
		liveUsageObserved: true, liveUsageAttempt: [3]string{record.RootRequestID, record.ThreadID, record.TurnID},
		terminalPolicyApplied: true, terminalPolicyAttempt: [3]string{record.RootRequestID, record.ThreadID, record.TurnID}}
	copy := *record.Result
	copy.AccountPolicyApplied = false
	actor.applyTerminalAccountPolicy(&copy)
	actor.observeLiveTerminalUsage(&copy)
	require.True(t, copy.AccountPolicyApplied)
	require.Len(t, counter.resetCalls, 1)
	require.Equal(t, after.InputTokens, SnapshotSchedulingRuntimeMetrics().InputTokens)
	require.Equal(t, after.CacheReadTokens, SnapshotSchedulingRuntimeMetrics().CacheReadTokens)
	require.Nil(t, svc.cloudBillingWorker.store, "no settlement consumer ran")
}

type cloudLiveObservationFailedSave struct{ OpenAICloudSessionStore }

func (*cloudLiveObservationFailedSave) SaveCloudSession(context.Context, *OpenAICloudSessionRecord, []string, *OpenAICloudSettlement) error {
	return errors.New("isolated owner-token save refused")
}

func TestCloudLiveTerminalRejectedSaveDoesNotObserve(t *testing.T) {
	resets := 0
	cohort := true
	sessions := &OpenAICloudSessions{store: &cloudLiveObservationFailedSave{}, terminalPolicy: func(context.Context, OpenAICloudOwner, *OpenAICloudResult) bool { resets++; return true }}
	actor := &cloudSessionActor{service: sessions, ctx: t.Context(), requestID: "actual-request", responseID: "actual-response",
		record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "actual-root", ThreadID: "actual-thread", TurnID: "actual-turn", Status: "completed"}, SchedulingEscaped: &cohort}}
	before := SnapshotSchedulingRuntimeMetrics()
	err := actor.finish("completed", false, true, &OpenAICloudUsage{InputTokens: 10, CachedInputTokens: 2, TotalTokens: 10}, nil)
	require.ErrorContains(t, err, "save refused")
	require.Zero(t, resets, "an unfenced terminal must not clear account failures")
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens, after.InputTokens)
	require.Equal(t, before.CacheReadTokens, after.CacheReadTokens)
}

func TestCloudLiveTerminal403ObservationHonorsEvidenceAndOwner(t *testing.T) {
	for _, sample := range []string{"valid", "replayed", "awaiting_tools", "lower_bound", "unknown_usage", "invalid_usage", "failed", "replacement", "lookup_failed", "probe"} {
		t.Run(sample, func(t *testing.T) {
			svc, in, _, _ := cloudForwardFixture(t, nil)
			counter := &openAI403CounterResetStub{}
			svc.rateLimitService = NewRateLimitService(nil, nil, nil, nil, nil)
			svc.rateLimitService.SetOpenAI403CounterCache(counter)
			grants := &cloudTerminalOwnerRepo{cloudAuthorizationRepoFixture: svc.cloudService.repo.(*cloudAuthorizationRepoFixture)}
			svc.cloudService.repo = grants
			owner := OpenAICloudOwner{AccountID: in.Account.ID, GrantGeneration: "grant"}
			result := &OpenAICloudResult{Terminal: true, UsageCredible: true, Usage: &OpenAICloudUsage{InputTokens: 1, TotalTokens: 1}, Response: []byte("{\"status\":\"completed\"}")}
			switch sample {
			case "replayed":
				result.Replayed = true
			case "awaiting_tools":
				result.AwaitingTools = true
			case "lower_bound":
				result.UsageCredible = false
			case "unknown_usage":
				result.Usage = nil
			case "invalid_usage":
				result.Usage.CachedInputTokens = 2
			case "failed":
				result.Response = []byte("{\"status\":\"failed\"}")
			case "replacement":
				grants.outcome = "replacement"
				grants.terminal.Store(true)
			case "lookup_failed":
				grants.outcome = "unavailable"
				grants.terminal.Store(true)
			case "probe":
				owner.Probe = true
			}
			svc.applyCloudOwnerTerminalPolicy(t.Context(), owner, result)
			if sample == "valid" {
				require.Equal(t, []int64{in.Account.ID}, counter.resetCalls)
			} else {
				require.Empty(t, counter.resetCalls)
			}
		})
	}
}
