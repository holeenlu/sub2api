package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"github.com/stretchr/testify/require"
)

func TestSchedulerLargePoolReadsBoundedBatchesWithoutIDHeadBias(t *testing.T) {
	for _, size := range []int{100, 1000, 5000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			accounts := make([]Account, size)
			for i := range accounts {
				accounts[i] = Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10}
			}
			cache := &replayConcurrencyCache{}
			svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(cache)}
			svc.concurrencyService.SetAccountLoadBatchCacheTTL(0)
			seen := map[int64]bool{}
			for i := 0; i < 80; i++ {
				ctx, budget, cancel := selectionbudget.Start(context.Background())
				selected, _, err := svc.SelectAccountWithScheduler(ctx, nil, "", fmt.Sprint(i), "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.True(t, selected.Acquired)
				seen[selected.Account.ID] = true
				selected.ReleaseFunc()
				cancel()
				require.LessOrEqual(t, budget.Snapshot().Operations, 64)
			}
			require.LessOrEqual(t, cache.maxLoadBatch, schedulerLoadBatchSize)
			require.Equal(t, 80*schedulerLoadBatchSize, cache.loadAccounts)
			require.Greater(t, len(seen), 35)
			tail := false
			for id := range seen {
				tail = tail || id > int64(size*3/4)
			}
			require.True(t, tail, "the static sample must include the tail of the pool")
			for i := range accounts {
				accounts[i].Extra = map[string]any{"openai_compact_supported": false}
			}
			compact, _, err := svc.SelectAccountWithScheduler(t.Context(), nil, "", "compact", "gpt-5.1", nil, OpenAIUpstreamTransportAny, true)
			require.ErrorIs(t, err, ErrNoAvailableCompactAccounts)
			require.Nil(t, compact)

		})
	}
}

func TestSchedulerHealthCohortIsolationAndEvidenceDecay(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}, stats: newOpenAIAccountRuntimeStats()}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1}
	small := WithSchedulingQuotaRequest(context.Background(), []byte(`{"input":"hi","reasoning":{"effort":"low"}}`), ContentModerationProtocolOpenAIResponses)
	large := WithSchedulingQuotaRequest(context.Background(), []byte(`{"input":"`+strings.Repeat("x", 410000)+`","reasoning":{"effort":"high"}}`), ContentModerationProtocolOpenAIResponses)
	req := OpenAIAccountScheduleRequest{RequestedModel: "gpt-5.1"}
	shortKey := scheduler.schedulingHealthCohort(small, account, req)
	longKey := scheduler.schedulingHealthCohort(large, account, req)
	require.NotEqual(t, shortKey, longKey)
	latency := 20000
	for range openAIHealthMinSamples {
		scheduler.stats.report(1, true, &latency, "gpt-5.1", longKey)
	}
	_, _, known := scheduler.stats.snapshot(1, "gpt-5.1", shortKey)
	require.False(t, known)
	longPlan := scheduler.buildOpenAIAccountLoadPlan(large, req, []*Account{account}, nil)
	require.True(t, longPlan.candidates[0].hasTTFT)
	require.False(t, longPlan.candidates[0].degraded, "long context is not an absolute latency failure")
	wsReq := req
	wsReq.RequiredTransport = OpenAIUpstreamTransportResponsesWebsocketV2
	_, _, known = scheduler.stats.snapshot(1, "gpt-5.1", scheduler.schedulingHealthCohort(large, account, wsReq))
	require.False(t, known)
	for range openAIHealthMinSamples {
		scheduler.stats.report(2, false, nil, "gpt-5.1", shortKey)
	}
	stat := scheduler.stats.loadOrCreate(2, "gpt-5.1", shortKey)
	stat.mu.Lock()
	stat.lastSample = time.Now().Add(-4 * time.Minute)
	before := stat.lastSample
	stat.mu.Unlock()
	rate, _, _ := scheduler.stats.snapshot(2, "gpt-5.1", shortKey)
	require.Less(t, rate, 0.26)
	stat.mu.Lock()
	require.Equal(t, before, stat.lastSample, "reads do not extend evidence lifetime")
	stat.mu.Unlock()
}

func TestSchedulerBusinessLayersCostCapacityAndFrozenBands(t *testing.T) {
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}, stats: newOpenAIAccountRuntimeStats()}
	account := func(id int64, rate float64, capacity, priority int) *Account {
		return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, RateMultiplier: &rate, Concurrency: capacity, Priority: priority}
	}
	a, b, other := account(1, 1, 0, 0), account(2, 1.1, 10, 0), account(3, 1, 10000, 1)
	near, free, invalid := account(4, 1.105, 20, 0), account(5, 0, 30, 0), account(6, math.NaN(), 40, 0)
	req := scheduler.prepareOpenAIBusinessLayers(context.Background(), OpenAIAccountScheduleRequest{UseUpstreamTokenCost: true}, []*Account{a, b, other, near, free, invalid})
	require.Equal(t, req.costBands[a.ID], req.costBands[b.ID])
	require.Less(t, req.costBands[a.ID], req.costBands[near.ID])
	require.Less(t, req.costBands[free.ID], req.costBands[a.ID])
	require.Greater(t, req.costBands[invalid.ID], req.costBands[near.ID])
	require.Equal(t, 10, req.capacities[a.ID], "other layers cannot inflate unlimited capacity")
	for range openAIHealthMinSamples {
		scheduler.stats.report(a.ID, false, nil, "")
	}
	frozenHealth := scheduler.buildOpenAIAccountLoadPlan(context.Background(), req, []*Account{a, b}, nil)
	require.Zero(t, frozenHealth.candidates[0].errorRate, "capacity and ranking share one health snapshot")
	scheduler.stats = newOpenAIAccountRuntimeStats()
	frozen := scheduler.prepareOpenAIBusinessLayers(context.Background(), req, []*Account{b, near})
	require.NotEqual(t, frozen.costBands[b.ID], frozen.costBands[near.ID], "retry must not redraw bands after anchor exits")
	next := scheduler.prepareOpenAIBusinessLayers(context.Background(), OpenAIAccountScheduleRequest{UseUpstreamTokenCost: true}, []*Account{b, near})
	require.Equal(t, next.costBands[b.ID], next.costBands[near.ID], "next request snapshots its own anchor")
	// A new request with costs in the same band apportions equal capacity, including
	// an unlimited account. A twofold cost gap enforces business preference.
	for _, rate := range []float64{1.05, 2} {
		b.RateMultiplier = &rate
		b.Concurrency = 10
		wins := 0
		for i := 0; i < 2000; i++ {
			plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{UseUpstreamTokenCost: true, SessionHash: fmt.Sprint(i)}, []*Account{a, b}, nil)
			if plan.selectionOrder[0].account.ID == a.ID {
				wins++
			}
		}
		if rate == 2 {
			require.Equal(t, 2000, wins)
		} else {
			require.InDelta(t, 1000, wins, 110)
		}
	}
	b.Priority = -1
	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{UseUpstreamTokenCost: true}, []*Account{a, b}, nil)
	require.Equal(t, b.ID, plan.selectionOrder[0].account.ID, "cost does not override priority")
}

func TestSchedulingDependencyStopReasonAndCleanupAfterCancellation(t *testing.T) {
	ctx, budget, cancel := selectionbudget.Start(WithSchedulingTrace(context.Background(), nil))
	defer cancel()
	cause := errors.New("database unavailable")
	selectionbudget.RecordError(ctx, cause)
	err := ErrNoAvailableAccounts
	finishSchedulingBudget(ctx, budget, &err)
	require.ErrorIs(t, err, cause)
	require.Equal(t, "dependency_error", schedulingTraceFromContext(ctx).summary.StopReason)
	// Storage failure followed by a successful fallback is not a failed selection.
	err = nil
	finishSchedulingBudget(ctx, budget, &err)
	require.NoError(t, err)

	ctx = WithGatewaySessionTracking(ctx)
	cache := &finalBudgetConcurrency{}
	slots := NewConcurrencyService(cache)
	slot, err := slots.AcquireAccountSlot(ctx, 42, 10)
	require.NoError(t, err)
	require.True(t, slot.Acquired)
	for selectionbudget.Remaining(ctx) > 0 {
		require.NoError(t, selectionbudget.Take(ctx, "sql", 0))
	}
	account := newSessionLimitTestAccount()
	sessions := newSessionLimitReleaseCacheStub()
	rememberSessionRegistration(ctx, account.ID, "created-here", true)
	cancel()
	svc := &GatewayService{sessionLimitCache: sessions}
	selected, err := svc.newSelectionResult(ctx, account, true, slot.ReleaseFunc, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, selected)
	require.Equal(t, 1, cache.released)
	require.Equal(t, []string{"created-here"}, sessions.unregistered[account.ID])
	require.Equal(t, 64, budget.Snapshot().Operations)
	require.Positive(t, budget.Snapshot().Cleanup)
}

type finalBudgetConcurrency struct {
	ConcurrencyCache
	acceptAfter        int64
	acquired, released int
}

func (c *finalBudgetConcurrency) AcquireAccountSlot(ctx context.Context, id int64, _ int, _ string) (bool, error) {
	if err := selectionbudget.Take(ctx, "redis", 1); err != nil {
		return false, err
	}
	c.acquired++
	return id > c.acceptAfter, nil
}

func (*finalBudgetConcurrency) ConcurrencySlotTTL() time.Duration { return 0 }
func (c *finalBudgetConcurrency) ReleaseAccountSlot(ctx context.Context, _ int64, _ string) error {
	c.released++
	return selectionbudget.Take(ctx, "redis", 1)
}
func (c *finalBudgetConcurrency) GetAccountsLoadBatch(ctx context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	if err := selectionbudget.Take(ctx, "redis", 1); err != nil {
		return nil, err
	}
	out := make(map[int64]*AccountLoadInfo, len(accounts))
	for start := 0; start < len(accounts); start += 256 {
		if err := selectionbudget.Take(ctx, "redis", 5*min(256, len(accounts)-start)); err != nil {
			return nil, err
		}
	}
	for _, a := range accounts {
		out[a.ID] = &AccountLoadInfo{AccountID: a.ID}
	}
	return out, nil
}

type finalBudgetAccountRepo struct {
	schedulerTestOpenAIAccountRepo
	rejectBelow int64
}

func (r finalBudgetAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	// Model multiple actual SQL executions inside one repository operation.
	for range 3 {
		if err := selectionbudget.Take(ctx, "sql", 0); err != nil {
			return nil, err
		}
	}
	a, err := r.schedulerTestOpenAIAccountRepo.GetByID(ctx, id)
	if err == nil && id <= r.rejectBelow {
		copy := *a
		copy.Schedulable = false
		return &copy, nil
	}
	return a, err
}
func TestSchedulerLowerLayersSurviveStaleLargeHead(t *testing.T) {
	for _, mode := range []string{"priority_slot", "cost_slot", "subscription_slot", "priority_recheck"} {
		t.Run(mode, func(t *testing.T) {
			accounts := make([]Account, 570)
			snapshot := &openAISnapshotCacheStub{accountsByID: make(map[int64]*Account)}
			for i := range accounts {
				rate := 1.0
				priority := 0
				kind := AccountTypeAPIKey
				credentials := map[string]any(nil)
				if i >= 70 {
					priority = 1
					rate = 2
				}
				if mode == "cost_slot" {
					priority = 0
				}
				if mode == "subscription_slot" {
					priority = 0
					if i < 70 {
						kind = AccountTypeOAuth
						credentials = map[string]any{"plan_type": "team"}
					}
				}
				accounts[i] = Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: kind, Status: StatusActive, Schedulable: true, Concurrency: 10, Priority: priority, RateMultiplier: &rate, Credentials: credentials}
				snapshot.accountsByID[int64(i+1)] = &accounts[i]
				snapshot.snapshotAccounts = append(snapshot.snapshotAccounts, &accounts[i])
			}
			cache := &finalBudgetConcurrency{acceptAfter: 70}
			repo := finalBudgetAccountRepo{schedulerTestOpenAIAccountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}}
			svc := &OpenAIGatewayService{accountRepo: repo, concurrencyService: NewConcurrencyService(cache)}
			if mode == "priority_recheck" {
				cache.acceptAfter = 0
				repo.rejectBelow = 70
				svc.accountRepo = repo
				svc.schedulerSnapshot = &SchedulerSnapshotService{cache: snapshot}
			}
			scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
			ctx, b, cancel := selectionbudget.Start(WithSchedulingTrace(context.Background(), nil))
			defer cancel()
			selected, _, _, _, err := scheduler.selectByLoadBalance(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, UseUpstreamTokenCost: mode == "cost_slot", SubscriptionPriority: mode == "subscription_slot"})
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Greater(t, selected.Account.ID, int64(70))
			require.True(t, selected.Acquired)
			require.LessOrEqual(t, b.Snapshot().Operations, 64)
			selected.ReleaseFunc()
			if mode == "priority_recheck" {
				require.Equal(t, cache.acquired, cache.released)
			}
		})
	}
}

func TestHealthUsesActualModelOnceAndSeparatesClientDeadline(t *testing.T) {
	account := &Account{ID: 99, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "actual", "actual": "wrong"}}}
	svc := &OpenAIGatewayService{}
	for range openAIHealthMinSamples {
		svc.ReportOpenAIAccountScheduleFailure(context.Background(), account, "actual", errors.New("upstream reset"))
	}
	scheduler, ok := svc.getOpenAIAccountScheduler(context.Background()).(*defaultOpenAIAccountScheduler)
	require.True(t, ok)
	actual, _, _ := scheduler.stats.snapshot(account.ID, "actual")
	wrong, _, _ := scheduler.stats.snapshot(account.ID, "wrong")
	require.Greater(t, actual, 0.5)
	require.Zero(t, wrong)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	other := &Account{ID: 100}
	svc.ReportOpenAIAccountScheduleFailure(canceled, other, "m", context.DeadlineExceeded)
	svc.ReportOpenAIAccountScheduleFailure(context.Background(), other, "m", nil)
	svc.ReportOpenAIAccountScheduleFailure(context.Background(), other, "m", ErrGatewayInstanceOverloaded)
	require.Equal(t, 1, scheduler.stats.size())
}

func TestInstancePressureHysteresisStalenessAndMissingSamples(t *testing.T) {
	pressure := &instancePressure{}
	now := time.Now()
	low, high, edge := 20.0, 91.0, 87.0
	for i := 0; i < 2; i++ {
		pressure.observe(now.Add(time.Duration(i)*5*time.Second), &high, &low)
		require.False(t, pressure.rejecting(now.Add(time.Duration(i)*5*time.Second)))
	}
	pressure.observe(now.Add(10*time.Second), &high, &low)
	require.True(t, pressure.rejecting(now.Add(10*time.Second)))
	pressure.observe(now.Add(15*time.Second), &edge, &low)
	require.True(t, pressure.rejecting(now.Add(15*time.Second)))
	pressure.observe(now.Add(20*time.Second), &low, &low)
	require.True(t, pressure.rejecting(now.Add(20*time.Second)))
	pressure.observe(now.Add(25*time.Second), &low, &low)
	require.False(t, pressure.rejecting(now.Add(25*time.Second)))
	for i := 6; i < 9; i++ {
		pressure.observe(now.Add(time.Duration(i)*5*time.Second), &high, &low)
	}
	require.True(t, pressure.rejecting(now.Add(40*time.Second)))
	require.False(t, pressure.rejecting(now.Add(71*time.Second)))
	pressure.observe(now.Add(75*time.Second), nil, &high)
	require.False(t, pressure.rejecting(now.Add(75*time.Second)))
}

func TestSchedulingCacheCohortSurvivesNextTurn(t *testing.T) {
	ctx := WithSchedulingTrace(context.Background(), nil)
	recordSchedulingEscape(ctx, "concurrency_full")
	frozen := FreezeSchedulingMetricsContext(ctx)
	resetSchedulingTrace(ctx)
	worker := WithSchedulingMetricsContext(context.Background(), frozen)
	before := SnapshotSchedulingRuntimeMetrics()
	RecordSchedulingCacheTokens(worker, 100, 70)
	after := SnapshotSchedulingRuntimeMetrics()
	require.Equal(t, before.InputTokens[1]+100, after.InputTokens[1])
	require.Equal(t, before.CacheReadTokens[1]+70, after.CacheReadTokens[1])
	require.Equal(t, before.InputTokens[0], after.InputTokens[0])
}

func TestSeedanceFailureScopeAndNoBlindRetries(t *testing.T) {
	for _, test := range []struct {
		name                string
		endpoint            GrokMediaEndpoint
		status              int
		code                string
		request, credential bool
	}{
		{"parameter", SeedanceEndpointCreate, 400, "InvalidParameter", true, false},
		{"policy", SeedanceEndpointCreate, 403, "ContentPolicyViolation", true, false},
		{"credentials", SeedanceEndpointCreate, 401, "InvalidApiKey", false, true},
		{"task missing", SeedanceEndpointStatus, 404, "NotFound", true, false},
		{"endpoint missing", SeedanceEndpointCreate, 404, "NotFound", false, false},
		{"server", SeedanceEndpointCreate, 500, "InternalError", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := seedanceUpstreamFailure(test.endpoint, test.status, []byte(fmt.Sprintf(`{"error":{"code":%q}}`, test.code)), nil)
			require.Equal(t, test.request, err.Scope == GatewayFailureScopeRequest)
			require.Equal(t, test.credential, err.IsCredentialFailure())
			require.False(t, err.ShouldRetryNextAccount())
		})
	}
}
