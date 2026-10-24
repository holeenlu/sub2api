//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A turn that the upstream confirms ended early (interrupted or failed) has
// reported the usage of every completed sampling step. Like the native gateway
// bills a response.incomplete/response.failed usage block, that measured usage
// is billed; the discarded in-flight step is not estimated. Only a turn whose
// end was never confirmed stays unknown.
func TestCloudConfirmedEarlyEndSettlesMeasuredUsageAsLowerBound(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		turn       map[string]any
		withUsage  bool
		status     string
		lowerBound bool
	}{
		{name: "interrupted with usage", turn: map[string]any{"id": "turn", "status": "interrupted"}, withUsage: true, status: "interrupted", lowerBound: true},
		{name: "failed with usage", turn: map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"message": "upstream gave up"}}, withUsage: true, status: "failed", lowerBound: true},
		{name: "interrupted without usage", turn: map[string]any{"id": "turn", "status": "interrupted"}, status: "interrupted"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a, store, _ := cloudHostedActorForTest(t)
			if !scenario.withUsage {
				a.record.Usage = nil
			}
			measured := a.record.Usage
			require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": scenario.turn}, ""))
			require.Equal(t, scenario.status, a.record.Status)
			require.Len(t, store.settlements, 1)
			settlement := store.settlements[0]
			require.Equal(t, scenario.status, settlement.Status)
			require.Equal(t, scenario.lowerBound, settlement.LowerBound)
			require.Equal(t, !scenario.lowerBound, settlement.Unknown, "confirmed early end with measured usage is billable; without usage it stays unknown")
			require.Equal(t, measured, settlement.Usage, "the measured usage is preserved as reported")
			require.NotNil(t, store.record.Result)
			require.False(t, store.record.Result.UsageCredible, "a lower bound is not a completed turn's final usage for health, probes or replay")
			raw, err := json.Marshal(settlement)
			require.NoError(t, err)
			require.Equal(t, scenario.lowerBound, gjson.GetBytes(raw, "lower_bound").Bool(), "the marker must survive the Redis JSON round trip")
		})
	}
}

// Losing the connection or routing never confirms how the upstream turn ended,
// so its watermark remains unknown and unbilled even when usage was measured.
func TestCloudUnconfirmedEndKeepsMeasuredUsageUnknown(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.rpc = nil // no interrupt can be sent, exactly like a lost socket
	a.stop("Cloud connection or routing was lost; execution will not be replayed")
	require.Equal(t, "unknown", a.record.Status)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].Unknown)
	require.False(t, store.settlements[0].LowerBound)
	require.Equal(t, 12, store.settlements[0].Usage.TotalTokens, "the last watermark is retained as a fact without being billed")
}

func TestCloudBillingWorkerSettlesLowerBoundAndMarksUsageLog(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	store := &cloudBillingStoreStub{}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	owner := OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}
	usage := &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
	roundTrip := func(settlement *OpenAICloudSettlement) *OpenAICloudSettlement {
		raw, err := json.Marshal(settlement)
		require.NoError(t, err)
		var persisted OpenAICloudSettlement
		require.NoError(t, json.Unmarshal(raw, &persisted))
		return &persisted
	}
	interrupted := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "interrupted-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "interrupted", Usage: usage, LowerBound: true, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{interrupted}
	worker.consume(context.Background())
	require.Equal(t, 1, repo.calls, "a confirmed interrupt bills its measured usage")
	require.Equal(t, 1, store.acks)
	require.Zero(t, store.retained)
	require.NotNil(t, repo.lastCmd.UsageLog)
	require.True(t, repo.lastCmd.UsageLog.UsageLowerBound)
	require.Equal(t, "lower_bound", SettledUsageBillingStatus(repo.lastCmd.UsageLog), "the usage row is marked as a lower bound, not an ordinary settlement")
	require.Equal(t, 10, repo.lastCmd.UsageLog.InputTokens)
	require.Equal(t, 2, repo.lastCmd.UsageLog.OutputTokens)
	require.InDelta(t, 0.000056, repo.lastCmd.BalanceCost, 1e-12, "the lower bound is priced exactly like confirmed consumption")

	completed := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "completed-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: usage, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{completed}
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls)
	require.False(t, repo.lastCmd.UsageLog.UsageLowerBound)
	require.Equal(t, "settled", SettledUsageBillingStatus(repo.lastCmd.UsageLog), "completed turns keep the ordinary settlement status")

	contradictory := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "contradictory-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: usage, LowerBound: true, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{contradictory}
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls, "a completed turn cannot also be a lower bound; the fact is archived instead of billed")
	require.Equal(t, 1, store.retained)
	require.Equal(t, "invalid_settlement", svc.usageBillingRepo.(*cloudBillingArchiveRepoStub).reasons["contradictory-root"])

	unknown := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "unknown-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "unknown", Usage: usage, Unknown: true, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{unknown}
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls, "an unconfirmed end is still never billed")
	require.Equal(t, "unknown_usage", svc.usageBillingRepo.(*cloudBillingArchiveRepoStub).reasons["unknown-root"])
}

func TestSettledUsageBillingStatusMarksLowerBound(t *testing.T) {
	require.Equal(t, "settled", SettledUsageBillingStatus(&UsageLog{ActualCost: 0.01}))
	require.Equal(t, "free", SettledUsageBillingStatus(&UsageLog{}))
	require.Equal(t, "lower_bound", SettledUsageBillingStatus(&UsageLog{ActualCost: 0.01, UsageLowerBound: true}))
	require.Equal(t, "lower_bound", SettledUsageBillingStatus(&UsageLog{UsageLowerBound: true}), "a free lower bound still tells the reader the usage is partial")
	require.Equal(t, "lower_bound", UsageBillingStatus(&UsageLog{BillingStatus: "lower_bound"}))
}

func (s *cloudLiveRoutingStore) settlementFor(root string) *OpenAICloudSettlement {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, settle := range s.settlements {
		if settle.RootRequestID == root {
			return settle
		}
	}
	return nil
}

// The client leaves mid-turn; the gateway interrupts the upstream and the
// upstream confirms the interruption after reporting the completed sampling
// steps. That confirmed usage is settled once, as a lower bound.
func TestCloudClientCancellationBillsConfirmedInterruptAsLowerBound(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go func() {
		cloudWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "answer", "type": "agentMessage"}}})
		cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "answer", "delta": "partial"}})
		for {
			select {
			case raw := <-wire.writes:
				var msg cloudRPCMessage
				if json.Unmarshal(raw, &msg) != nil || msg.Method != "turn/interrupt" {
					continue
				}
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12}}}})
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	requestCtx, err = svc.FinalizeCloudBillingSnapshot(requestCtx, "gpt-5.1", "", "")
	require.NoError(t, err)
	clientCtx, disconnect := context.WithCancel(requestCtx)
	defer disconnect()
	owner := OpenAICloudOwner{UserID: in.User.ID, APIKeyID: in.APIKey.ID, GroupID: *in.APIKey.GroupID, AccountID: in.Account.ID, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: cloudSettingsDigest(in.Account)}
	_, err = svc.cloudSessions.Run(clientCtx, OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-5.1","input":"hi"}`), RootRequestID: "cloud_root_lower_bound", BillingSnapshot: CloudBillingSnapshotFromContext(requestCtx)}, func(event json.RawMessage) error {
		if gjson.GetBytes(event, "type").String() == "response.output_text.delta" {
			disconnect()
		}
		return nil
	})
	require.Error(t, err)
	require.Eventually(t, func() bool { return store.settlementFor("cloud_root_lower_bound") != nil }, 5*time.Second, 10*time.Millisecond)
	settlement := store.settlementFor("cloud_root_lower_bound")
	require.Equal(t, "interrupted", settlement.Status)
	require.False(t, settlement.Unknown)
	require.True(t, settlement.LowerBound)
	require.Equal(t, &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}, settlement.Usage)
	require.Equal(t, 1, store.settlementCount(), "one confirmed interrupt settles exactly once")
}
