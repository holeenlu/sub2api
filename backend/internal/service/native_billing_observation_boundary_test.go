package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNativeBillingPreparationDoesNotReplayLiveObservations(t *testing.T) {
	gateway, _, _, input := nativeApplicationFixture(t)
	counter := &openAI403CounterResetStub{}
	gateway.rateLimitService = NewRateLimitService(nil, nil, nil, nil, nil)
	gateway.rateLimitService.SetOpenAI403CounterCache(counter)
	input.UsageInput.Account.Platform = PlatformOpenAI
	ctx := WithSchedulingTrace(t.Context(), nil)
	before := SnapshotSchedulingRuntimeMetrics()
	for range 2 {
		_, err := gateway.prepareOpenAIUsage(ctx, input.UsageInput)
		require.NoError(t, err)
		_, err = gateway.RecordNativeOperationUsage(ctx, input)
		require.NoError(t, err)
	}
	after := SnapshotSchedulingRuntimeMetrics()
	require.Empty(t, counter.resetCalls, "repreparing a bill must not erase current account failures")
	require.Equal(t, before.InputTokens, after.InputTokens)
	require.Equal(t, before.CacheReadTokens, after.CacheReadTokens)
}

func TestNativeLiveFinalizationObservesOnceBeforeJournalRetry(t *testing.T) {
	wire := "{\"id\":\"resp-live\",\"object\":\"response\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1000,\"output_tokens\":12,\"input_tokens_details\":{\"cached_tokens\":100}}}"
	f, _, c := nativeHTTPObservedFixture(t, wire, "application/json")
	f.ctx = WithSchedulingTrace(f.ctx, nil)
	f.service.billingService = NewBillingService(f.service.cfg, nil)
	f.service.usageLogRepo = &openAIRecordUsageLogRepoStub{inserted: true}
	journal := &nativeApplicationJournal{t: t, fail: errors.New("journal unavailable")}
	f.service.usageBillingRepo = journal
	bindNativeRuntimeFixture(t, f)
	counter := &openAI403CounterResetStub{}
	f.service.rateLimitService = NewRateLimitService(nil, nil, nil, nil, nil)
	f.service.rateLimitService.SetOpenAI403CounterCache(counter)
	before := SnapshotSchedulingRuntimeMetrics()
	err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
		response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
		require.NoError(t, err)
		_, err = io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		input := &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource,
			Result: &OpenAIForwardResult{RequestID: "request-real", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 12, CacheReadInputTokens: 100}}}
		for range 2 {
			handled, finalErr := f.service.FinalizeNativeHTTPUsage(f.ctx, input)
			require.True(t, handled)
			require.Error(t, finalErr)
		}
		return NativeOperationDone()
	})
	require.Error(t, err)
	require.Equal(t, 1, journal.records)
	require.Equal(t, []int64{f.resource.ID}, counter.resetCalls)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens[0]+1000, after.InputTokens[0])
	require.Equal(t, before.CacheReadTokens[0]+100, after.CacheReadTokens[0])
}

func TestCloudSettlementWorkerDoesNotReplayLiveObservations(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(t.Context(), input)
	require.NoError(t, err)
	counter := &openAI403CounterResetStub{}
	svc.rateLimitService = NewRateLimitService(nil, nil, nil, nil, nil)
	svc.rateLimitService.SetOpenAI403CounterCache(counter)
	settlement := &OpenAICloudSettlement{
		OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "observation-retry"},
		BillingSnapshot:    CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, ObservedAt: input.PricingAt.Add(time.Minute),
	}
	store := &cloudBillingStoreStub{pending: []*OpenAICloudSettlement{settlement}}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	ctx = WithSchedulingTrace(context.Background(), nil)
	before := SnapshotSchedulingRuntimeMetrics()
	repo.err = errors.New("ledger unavailable")
	worker.consume(ctx)
	require.Equal(t, 1, repo.calls)
	repo.err = nil
	worker.consume(ctx)
	require.Equal(t, 2, repo.calls)
	require.Empty(t, counter.resetCalls)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens, after.InputTokens)
	require.Equal(t, before.CacheReadTokens, after.CacheReadTokens)
}
