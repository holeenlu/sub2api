package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUnifiedSchedulerLargePoolCoverageAndCapacity(t *testing.T) {
	candidates := make([]openAIAccountCandidateScore, 1000)
	for i := range candidates {
		candidates[i] = openAIAccountCandidateScore{account: &Account{ID: int64(i + 1), Concurrency: 1}, capacity: 1, loadInfo: &AccountLoadInfo{}}
	}
	scheduler := &defaultOpenAIAccountScheduler{}
	plan := openAIAccountLoadPlan{candidates: candidates, topK: 64}
	seen := map[int64]bool{}
	for i := 0; i < 1000; i++ {
		order := scheduler.buildOpenAISelectionOrder(OpenAIAccountScheduleRequest{SessionHash: fmt.Sprint(i)}, plan)
		require.Len(t, order, len(candidates), "the planner retains lower layers; external work has a separate bound")
		seen[order[0].account.ID] = true
	}
	require.Greater(t, len(seen), 500, "eligible tail accounts must not be starved by ID-based Top-K")
	require.True(t, seen[1000] || seen[999] || seen[998])
	// Capacity affects relative share within the same priority, not authorization.
	plan.candidates = candidates[:2]
	plan.candidates[1].account.Concurrency = 9
	plan.candidates[1].capacity = 9
	counts := map[int64]int{}
	for i := 0; i < 2000; i++ {
		order := scheduler.buildOpenAISelectionOrder(OpenAIAccountScheduleRequest{SessionHash: fmt.Sprint(i)}, plan)
		counts[order[0].account.ID]++
	}
	require.Greater(t, counts[2], 1600)
	require.Less(t, counts[2], 1950)
}

func TestUnifiedSchedulerFullHeadCannotHideAvailableTail(t *testing.T) {
	candidates := make([]openAIAccountCandidateScore, 1000)
	for i := range candidates {
		candidates[i] = openAIAccountCandidateScore{account: &Account{ID: int64(i + 1), Concurrency: 1}, capacity: 1, loadInfo: &AccountLoadInfo{CurrentConcurrency: 1}, loadKnown: true}
	}
	candidates[999].loadInfo.CurrentConcurrency = 0
	scheduler := &defaultOpenAIAccountScheduler{}
	order := scheduler.buildOpenAISelectionOrder(OpenAIAccountScheduleRequest{}, openAIAccountLoadPlan{candidates: candidates, topK: 64})
	require.Len(t, order, len(candidates))
	require.Equal(t, int64(1000), order[0].account.ID)
}

func TestUnifiedSchedulerPriorityAndSubscriptionBoundaries(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{}
	regular := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Priority: 0}
	subscription := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "plus"}, Concurrency: 1, Priority: 10}
	require.True(t, subscription.IsOpenAIChatGPTSubscription())
	plan := openAIAccountLoadPlan{topK: 64, candidates: []openAIAccountCandidateScore{{account: regular, loadInfo: &AccountLoadInfo{}}, {account: subscription, loadInfo: &AccountLoadInfo{}}}}
	req := OpenAIAccountScheduleRequest{SubscriptionPriority: true}
	require.Equal(t, int64(1), scheduler.buildOpenAISelectionOrder(req, plan)[0].account.ID)
	subscription.Priority = 0
	require.Equal(t, int64(2), scheduler.buildOpenAISelectionOrder(req, plan)[0].account.ID)
	plan.candidates[1].errorRate = 0.9
	require.Equal(t, int64(1), scheduler.buildOpenAISelectionOrder(req, plan)[0].account.ID)
}

func TestUnifiedSchedulerHealthIsolationExpiryAndBound(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	for i := 0; i < openAIHealthMinSamples-1; i++ {
		stats.report(1, false, nil, "model-a")
	}
	errorRate, _, _ := stats.snapshot(1, "model-a")
	require.Zero(t, errorRate, "low sample history must be neutral")
	stats.report(1, false, nil, "model-a")
	errorRate, _, _ = stats.snapshot(1, "model-a")
	require.Greater(t, errorRate, 0.5)
	errorRate, _, _ = stats.snapshot(1, "model-b")
	require.Zero(t, errorRate)
	stat := stats.loadOrCreate(1, "model-a")
	stat.mu.Lock()
	stat.lastSample = time.Now().Add(-2 * openAIHealthTTL)
	stat.mu.Unlock()
	errorRate, _, _ = stats.snapshot(1, "model-a")
	require.Zero(t, errorRate)
	stats.report(1, true, nil, "model-a")
	errorRate, _, _ = stats.snapshot(1, "model-a")
	require.Zero(t, errorRate)
	bounded := newOpenAIAccountRuntimeStats()
	for i := int64(1); i <= openAIHealthMaxEntries+20; i++ {
		bounded.loadOrCreate(i, "a")
	}
	require.Equal(t, openAIHealthMaxEntries, bounded.size())
	require.Nil(t, bounded.loadOrCreate(openAIHealthMaxEntries+99, "a"))
}

func TestUnifiedSchedulerProbeBudgetAndCancellation(t *testing.T) {
	accounts := make([]Account, 1000)
	reject := map[int64]bool{}
	for i := range accounts {
		accounts[i] = Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}
		reject[int64(i+1)] = false
	}
	acquired := []int64{}
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: reject, acquiredIDs: &acquired})}, stats: newOpenAIAccountRuntimeStats()}
	_, _, _, _, err := scheduler.selectByLoadBalance(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI})
	require.NoError(t, err, "a bounded wait plan remains possible")
	require.LessOrEqual(t, len(acquired), openAIAccountSelectionProbeLimit)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = scheduler.tryAcquireOpenAISelectionOrder(ctx, OpenAIAccountScheduleRequest{}, []openAIAccountCandidateScore{{account: &accounts[0], loadInfo: &AccountLoadInfo{}}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestUnifiedSchedulerUsesRemainingCapacity(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{}
	plan := openAIAccountLoadPlan{candidates: []openAIAccountCandidateScore{
		{account: &Account{ID: 1, Concurrency: 100}, capacity: 100, loadKnown: true, loadInfo: &AccountLoadInfo{CurrentConcurrency: 99}, score: 3},
		{account: &Account{ID: 2, Concurrency: 10}, capacity: 10, loadKnown: true, loadInfo: &AccountLoadInfo{}, score: 3},
	}}
	counts := map[int64]int{}
	for i := 0; i < 1000; i++ {
		order := scheduler.buildOpenAISelectionOrder(OpenAIAccountScheduleRequest{SessionHash: fmt.Sprint(i)}, plan)
		counts[order[0].account.ID]++
	}
	require.Greater(t, counts[2], 800, "remaining capacity must dominate nominal capacity")
	require.Greater(t, counts[1], 20, "nearly full accounts with a free slot still participate")
}

func TestUnifiedSchedulerLatencyExpiresEvenWithNewErrorSamples(t *testing.T) {
	stats := newOpenAIAccountRuntimeStats()
	ttft := 100
	for i := 0; i < openAIHealthMinSamples; i++ {
		stats.report(1, true, &ttft, "a")
	}
	stat := stats.loadOrCreate(1, "a")
	stat.mu.Lock()
	stat.lastTTFTSample = time.Now().Add(-2 * openAIHealthTTL)
	stat.mu.Unlock()
	stats.report(1, false, nil, "a")
	rate, _, hasTTFT := stats.snapshot(1, "a")
	require.Greater(t, rate, 0.0)
	require.False(t, hasTTFT)
}

func TestUnifiedSchedulerBorrowBindingSurvivesTerminalAdmission(t *testing.T) {
	cache := newStickyTTLCacheStub()
	busy := map[int64]bool{1: false, 2: true}
	accounts := []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}, {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}}
	svc := &OpenAIGatewayService{cache: cache, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: busy})}
	ctx := context.Background()
	require.NoError(t, svc.BindStickySession(ctx, nil, "busy", 1))
	selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "busy", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.True(t, selected.Acquired)
	require.Equal(t, int64(2), selected.Account.ID)
	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ContextWithSelectionProfitGate(ctx, selected), nil, "busy", 2))
	require.Equal(t, int64(1), cache.bindings[cache.key(0, "openai:busy")])
	selected.ReleaseFunc()
	busy[1] = true
	next, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", "busy", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), next.Account.ID)
	next.ReleaseFunc()
}

func TestUnifiedSchedulerBusyHomeDoesNotBorrowDegradedPeer(t *testing.T) {
	accounts := []Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}, {ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1}}
	cache := newStickyTTLCacheStub()
	svc := &OpenAIGatewayService{cache: cache, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: false, 2: true}}), openaiAccountStats: newOpenAIAccountRuntimeStats()}
	for range openAIHealthMinSamples {
		svc.openaiAccountStats.report(2, false, nil, "gpt-5.1")
	}
	require.NoError(t, svc.BindStickySession(t.Context(), nil, "busy", 1))
	selected, _, err := svc.SelectAccountWithScheduler(t.Context(), nil, "", "busy", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), selected.Account.ID)
	require.False(t, selected.Acquired)
	require.NotNil(t, selected.WaitPlan)
}
