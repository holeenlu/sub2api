package service

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func cloudStageBillingFixture(t *testing.T) (*openAICloudBillingWorker, *OpenAICloudSettlement, *openAIRecordUsageBillingRepoStub) {
	t.Helper()
	svc, input, ledger := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	ctx, err = svc.FinalizeCloudBillingSnapshot(ctx, "gpt-5.1", "", "")
	require.NoError(t, err)
	raw := CloudBillingSnapshotFromContext(ctx)
	first := OpenAICloudUsage{InputTokens: 100, CachedInputTokens: 80, OutputTokens: 5, TotalTokens: 105}
	now := time.Now()
	root := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "stage-root", SessionID: "session", ThreadID: "thread", TurnID: "turn"}, BillingSnapshot: raw, Status: "awaiting_tools", Usage: &first, SamplingUsage: []OpenAICloudUsage{first}, ObservedAt: now}
	root.BillingStages = []OpenAICloudBillingStage{{ResponseID: "resp_cloud_first", BillingSnapshot: raw, SampleFrom: 0, SampleTo: 1, Usage: &first, StartedAt: now.Add(-time.Second), ClosedAt: now, Status: "completed"}}
	return &openAICloudBillingWorker{service: svc}, root, ledger
}

func TestCloudStageBillingChargesBeforeTurnCompletesAndNeverAggregate(t *testing.T) {
	w, root, ledger := cloudStageBillingFixture(t)
	require.NoError(t, w.settle(t.Context(), root))
	require.Equal(t, 1, ledger.calls)
	require.Equal(t, "cloud:stage-root/resp_cloud_first", ledger.lastCmd.RequestID)
	firstCost := ledger.lastCmd.UsageLog.TotalCost
	first := *root.Usage
	second := OpenAICloudUsage{InputTokens: 120, CachedInputTokens: 100, OutputTokens: 7, TotalTokens: 127}
	root.SamplingUsage = append(root.SamplingUsage, second)
	root.Usage = cloudStageUsageSum(&first, &second)
	root.Status = "completed"
	root.BillingStages = append(root.BillingStages, OpenAICloudBillingStage{ResponseID: "resp_cloud_final", BillingSnapshot: root.BillingSnapshot, SampleFrom: 1, SampleTo: 2, BaseUsage: &first, Usage: &second, StartedAt: root.ObservedAt, ClosedAt: root.ObservedAt.Add(time.Second), Status: "completed"})
	require.NoError(t, w.settle(t.Context(), root))
	require.Equal(t, 3, ledger.calls, "the prior phase may be resubmitted with the same ledger identity; no aggregate bill")
	require.Equal(t, "cloud:stage-root/resp_cloud_final", ledger.lastCmd.RequestID)
	require.Equal(t, 20, ledger.lastCmd.UsageLog.InputTokens)
	require.Equal(t, 100, ledger.lastCmd.UsageLog.CacheReadTokens)
	require.Equal(t, "cloud_response", ledger.lastCmd.UsageLog.SamplingDetails.Scope)
	var snapshot OpenAICloudBillingSnapshot
	require.NoError(t, json.Unmarshal(root.BillingSnapshot, &snapshot))
	cost, err := w.service.cloudSamplingCost(CostInput{Ctx: context.Background(), Model: "gpt-5.1", RequestCount: 1, RateMultiplier: snapshot.TokenMultiplier, Resolver: NewModelPricingResolver(nil, w.service.billingService), Resolved: snapshot.Prices["gpt-5.1"].resolved()}, root.SamplingUsage)
	require.NoError(t, err)
	require.InDelta(t, cost.TotalCost, firstCost+ledger.lastCmd.UsageLog.TotalCost, 1e-10)
}

func TestCloudStageBillingRejectsOverlapBeforeAnyDebit(t *testing.T) {
	w, root, ledger := cloudStageBillingFixture(t)
	duplicate := root.BillingStages[0]
	duplicate.ResponseID = "resp_cloud_other"
	root.BillingStages = append(root.BillingStages, duplicate)
	require.Error(t, w.settle(t.Context(), root))
	require.Zero(t, ledger.calls)
}

func TestCloudStageBillingRetainsKnownStagesWhenFinalUsageUnknown(t *testing.T) {
	w, root, ledger := cloudStageBillingFixture(t)
	root.Unknown = true
	root.Status = "unknown"
	require.ErrorIs(t, w.settle(t.Context(), root), cloudPermanentSettlementError("unknown_usage"))
	require.Equal(t, 1, ledger.calls)
	require.Equal(t, "cloud:stage-root/resp_cloud_first", ledger.lastCmd.RequestID)
}

func TestCloudStageBillingNeverPricesFinalUnassignedUsageAgain(t *testing.T) {
	w, root, ledger := cloudStageBillingFixture(t)
	root.Status = "completed"
	root.Usage = &OpenAICloudUsage{InputTokens: 200, CachedInputTokens: 80, OutputTokens: 5, TotalTokens: 205}
	root.SamplingUsage = append(root.SamplingUsage, OpenAICloudUsage{InputTokens: 100, TotalTokens: 100})
	require.ErrorIs(t, w.settle(t.Context(), root), cloudPermanentSettlementError("invalid_sampling_usage"))
	require.Equal(t, 1, ledger.calls)
}

func TestCloudStageUsageAfterToolReplyUsesNextConfirmedResponse(t *testing.T) {
	w, root, ledger := cloudStageBillingFixture(t)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-5.1","input":"hi"}`))
	require.NoError(t, err)
	a := &cloudSessionActor{ctx: t.Context(), record: &OpenAICloudSessionRecord{OpenAICloudBinding: root.OpenAICloudBinding, BillingSnapshot: root.BillingSnapshot}, responseID: "resp_cloud_unmetered"}
	cmd := OpenAICloudCommand{Request: OpenAICloudRequest{BillingSnapshot: root.BillingSnapshot}}
	require.NoError(t, a.beginBillingStage(cmd, p))
	require.NoError(t, a.closeBillingStage("completed"))
	require.Empty(t, a.record.BillingStages, "the first tool result has no reported usage yet")
	a.responseID = "resp_cloud_confirmed"
	require.NoError(t, a.beginBillingStage(cmd, p))
	a.record.Usage = root.Usage
	a.record.SamplingUsage = root.SamplingUsage
	require.NoError(t, a.closeBillingStage("completed"))
	require.Len(t, a.record.BillingStages, 1)
	a.record.Status = "awaiting_tools"
	require.NoError(t, w.settle(t.Context(), a.settlement("awaiting_tools", false)))
	require.Equal(t, "cloud:stage-root/resp_cloud_confirmed", ledger.lastCmd.RequestID)
	require.Equal(t, 20, ledger.lastCmd.UsageLog.InputTokens)
}

func TestCloudStageUnknownCurrentPhaseCannotBecomeConfirmedCharge(t *testing.T) {
	_, root, _ := cloudStageBillingFixture(t)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-5.1","input":"hi"}`))
	require.NoError(t, err)
	a := &cloudSessionActor{ctx: t.Context(), record: &OpenAICloudSessionRecord{OpenAICloudBinding: root.OpenAICloudBinding, BillingSnapshot: root.BillingSnapshot}, responseID: "resp_cloud_lost"}
	require.NoError(t, a.beginBillingStage(OpenAICloudCommand{Request: OpenAICloudRequest{BillingSnapshot: root.BillingSnapshot}}, p))
	a.record.Usage = root.Usage
	a.record.SamplingUsage = root.SamplingUsage
	a.uncertain = true
	require.NoError(t, a.closeBillingStage("failed"))
	require.Empty(t, a.record.BillingStages)
	require.Nil(t, a.record.OpenBillingStage.Usage)
}

// Closed stages never change when a later owner publishes the next snapshot.
func CloudBillingStagesExtend(old, next []OpenAICloudBillingStage) bool {
	if len(next) < len(old) {
		return false
	}
	for i := range old {
		a, _ := json.Marshal(old[i])
		b, _ := json.Marshal(next[i])
		if !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}
