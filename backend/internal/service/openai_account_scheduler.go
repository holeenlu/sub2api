package service

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
	"golang.org/x/sync/singleflight"
)

const (
	openAIAccountScheduleLayerPreviousResponse = "previous_response_id"
	openAIAccountScheduleLayerGuardianParent   = "guardian_parent"
	openAIAccountScheduleLayerSessionSticky    = "session_hash"
	openAIAccountScheduleLayerLoadBalance      = "load_balance"
	openAIAdvancedSchedulerSettingKey          = "openai_scheduler_business_preferences"
)

const (
	openAIAdvancedSchedulerSettingCacheTTL  = 5 * time.Second
	openAIAdvancedSchedulerSettingDBTimeout = 2 * time.Second
	// Bound serial slot probes and fresh-account rechecks independently of pool size.
	openAIAccountSelectionProbeLimit = 64
)

const (
	openAIQuotaHeadroomNeutralFactor           = 0.5
	openAIQuotaHeadroomSecondaryLowRemain      = 0.10
	openAIQuotaHeadroomSnapshotStaleAfter      = 8 * time.Hour
	defaultOpenAIOAuthSchedulingRateMultiplier = 1.0
)

type cachedOpenAIAdvancedSchedulerSetting struct {
	lowUpstreamRatePriorityEnabled bool
	oauthSchedulingRateMultiplier  *float64
	subscriptionPriorityEnabled    bool
	expiresAt                      int64
}

type openAIAdvancedSchedulerRuntimeSettings struct {
	lowUpstreamRatePriorityEnabled bool
	oauthSchedulingRateMultiplier  *float64
	subscriptionPriorityEnabled    bool
}

var openAIAdvancedSchedulerSettingCache atomic.Value // *cachedOpenAIAdvancedSchedulerSetting
var openAIAdvancedSchedulerSettingSF singleflight.Group

type OpenAIAccountScheduleRequest struct {
	samplingOnly bool
	costBands    map[int64]int
	capacities   map[int64]int
	limits       map[int64]int
	health       map[int64]schedulerHealthSample

	GroupID                 *int64
	Platform                string
	SessionHash             string
	StickyAccountID         int64
	GuardianParentAccountID int64
	SubscriptionPriority    bool
	PreserveStickyBinding   bool
	// DisableStickyEscape keeps task-owner lookups on their account even when
	// generic sticky health or concurrency heuristics would prefer another.
	DisableStickyEscape     bool
	RequirePrivacySet       bool
	PreviousResponseID      string
	UseUpstreamTokenCost    bool
	RequestedModel          string
	RequiredTransport       OpenAIUpstreamTransport
	RequiredCapability      OpenAIEndpointCapability
	RequiredImageCapability OpenAIImagesCapability
	// RequireCompact is only for legacy /responses/compact capability filtering
	// and compact_model_mapping; native remote compaction v2 leaves it false.
	RequireCompact bool
	ExcludedIDs    map[int64]struct{}
}

type OpenAIAccountScheduleDecision struct {
	Layer               string
	StickyPreviousHit   bool
	StickySessionHit    bool
	CandidateCount      int
	TopK                int
	LatencyMs           int64
	LoadSkew            float64
	SelectedAccountID   int64
	SelectedAccountType string
}

type OpenAIAccountSchedulerMetricsSnapshot struct {
	SelectTotal             int64
	StickyPreviousHitTotal  int64
	StickySessionHitTotal   int64
	LoadBalanceSelectTotal  int64
	AccountSwitchTotal      int64
	SchedulerLatencyMsTotal int64
	SchedulerLatencyMsAvg   float64
	StickyHitRatio          float64
	AccountSwitchRate       float64
	LoadSkewAvg             float64
	RuntimeStatsEntryCount  int
}

type OpenAIAccountScheduler interface {
	Select(ctx context.Context, req OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error)
	ReportResult(accountID int64, success bool, firstTokenMs *int, models ...string)
	ReportSwitch()
	SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot
}

type openAIAccountSchedulerMetrics struct {
	selectTotal            atomic.Int64
	stickyPreviousHitTotal atomic.Int64
	stickySessionHitTotal  atomic.Int64
	stickyHitTotal         atomic.Int64
	loadBalanceSelectTotal atomic.Int64
	accountSwitchTotal     atomic.Int64
	latencyMsTotal         atomic.Int64
	loadSkewMilliTotal     atomic.Int64
}

type openAIAccountLoadPlan struct {
	allCandidates             []openAIAccountCandidateScore
	candidates                []openAIAccountCandidateScore
	staleSnapshotCompactRetry []openAIAccountCandidateScore
	selectionOrder            []openAIAccountCandidateScore
	candidateCount            int
	topK                      int
	loadSkew                  float64
}

type openAIAccountLoadSelectionAttempt struct {
	result              *AccountSelectionResult
	selectionOrder      []openAIAccountCandidateScore
	candidateCount      int
	topK                int
	loadSkew            float64
	compactBlocked      bool
	noCompactCandidates bool
	err                 error
}

func (m *openAIAccountSchedulerMetrics) recordSelect(decision OpenAIAccountScheduleDecision) {
	if m == nil {
		return
	}
	m.selectTotal.Add(1)
	m.latencyMsTotal.Add(decision.LatencyMs)
	m.loadSkewMilliTotal.Add(int64(math.Round(decision.LoadSkew * 1000)))
	if decision.StickyPreviousHit {
		m.stickyPreviousHitTotal.Add(1)
	}
	if decision.StickySessionHit {
		m.stickySessionHitTotal.Add(1)
	}
	if decision.StickyPreviousHit || decision.StickySessionHit {
		m.stickyHitTotal.Add(1)
	}
	if decision.Layer == openAIAccountScheduleLayerLoadBalance {
		m.loadBalanceSelectTotal.Add(1)
	}
}

func (m *openAIAccountSchedulerMetrics) recordSwitch() {
	if m == nil {
		return
	}
	m.accountSwitchTotal.Add(1)
}

type openAIAccountRuntimeStats struct {
	accounts     sync.Map
	accountCount atomic.Int64
	nextSweep    atomic.Int64
}

const openAIHealthTTL = 10 * time.Minute
const openAIHealthMinSamples = 8
const openAIHealthDegradedErrorRate = 0.5
const openAIHealthMaxEntries = 32768

type openAIHealthKey struct {
	accountID int64
	model     string
	cohort    string
}

func openAIStatsKey(accountID int64, models []string) openAIHealthKey {
	model := ""
	if len(models) > 0 {
		model = normalizeOpenAIAccountModelTransientModel(models[0])
	}
	cohort := ""
	if len(models) > 1 {
		cohort = models[1]
	}
	return openAIHealthKey{accountID: accountID, model: model, cohort: cohort}
}

type openAIAccountRuntimeStat struct {
	mu                sync.Mutex
	lastSample        time.Time
	lastTTFTSample    time.Time
	samples           int
	ttftSamples       int
	errorRateEWMABits atomic.Uint64
	ttftEWMABits      atomic.Uint64
}

func newOpenAIAccountRuntimeStats() *openAIAccountRuntimeStats {
	return &openAIAccountRuntimeStats{}
}

func (s *openAIAccountRuntimeStats) loadOrCreate(accountID int64, models ...string) *openAIAccountRuntimeStat {
	key := openAIStatsKey(accountID, models)
	if v, ok := s.accounts.Load(key); ok {
		stat, _ := v.(*openAIAccountRuntimeStat)
		return stat
	}
	for {
		count := s.accountCount.Load()
		if count >= openAIHealthMaxEntries {
			return nil
		}
		if s.accountCount.CompareAndSwap(count, count+1) {
			break
		}
	}
	stat := &openAIAccountRuntimeStat{}
	stat.ttftEWMABits.Store(math.Float64bits(math.NaN()))
	value, loaded := s.accounts.LoadOrStore(key, stat)
	if loaded {
		s.accountCount.Add(-1)
	}
	result, _ := value.(*openAIAccountRuntimeStat)
	return result
}

func updateEWMAAtomic(target *atomic.Uint64, sample float64, alpha float64) {
	for {
		oldBits := target.Load()
		oldValue := math.Float64frombits(oldBits)
		newValue := alpha*sample + (1-alpha)*oldValue
		if target.CompareAndSwap(oldBits, math.Float64bits(newValue)) {
			return
		}
	}
}

func (s *openAIAccountRuntimeStats) report(accountID int64, success bool, firstTokenMs *int, models ...string) {
	if s == nil || accountID <= 0 {
		return
	}
	now := time.Now()
	sweep := s.nextSweep.Load()
	if now.UnixNano() > sweep && s.nextSweep.CompareAndSwap(sweep, now.Add(time.Minute).UnixNano()) {
		s.accounts.Range(func(key, value any) bool {
			stat, ok := value.(*openAIAccountRuntimeStat)
			if !ok || stat == nil {
				return true
			}
			stat.mu.Lock()
			if !stat.lastSample.IsZero() && now.Sub(stat.lastSample) > openAIHealthTTL && s.accounts.CompareAndDelete(key, stat) {
				s.accountCount.Add(-1)
			}
			stat.mu.Unlock()
			return true
		})
	}
	var stat *openAIAccountRuntimeStat
	for {
		stat = s.loadOrCreate(accountID, models...)
		if stat == nil {
			return
		}
		stat.mu.Lock()
		current, exists := s.accounts.Load(openAIStatsKey(accountID, models))
		if exists && current == stat {
			break
		}
		stat.mu.Unlock() // An expiry sweep removed the stale record before this lock.
	}
	defer stat.mu.Unlock()
	now = time.Now()
	if now.Sub(stat.lastSample) > openAIHealthTTL {
		stat.samples = 0
		stat.ttftSamples = 0
		stat.errorRateEWMABits.Store(0)
		stat.ttftEWMABits.Store(math.Float64bits(math.NaN()))
	}
	sample := 0.0
	if !success {
		sample = 1
	}
	stat.errorRateEWMABits.Store(math.Float64bits(decayedSchedulingError(math.Float64frombits(stat.errorRateEWMABits.Load()), now.Sub(stat.lastSample))))
	updateEWMAAtomic(&stat.errorRateEWMABits, sample, 0.2)
	if firstTokenMs != nil && *firstTokenMs > 0 && success {
		if now.Sub(stat.lastTTFTSample) > openAIHealthTTL {
			stat.ttftSamples = 0
			stat.ttftEWMABits.Store(math.Float64bits(math.NaN()))
		}
		stat.lastTTFTSample = now
		previous := math.Float64frombits(stat.ttftEWMABits.Load())
		if math.IsNaN(previous) {
			previous = float64(*firstTokenMs)
		} else {
			previous = 0.2*float64(*firstTokenMs) + 0.8*previous
		}
		stat.ttftEWMABits.Store(math.Float64bits(previous))
		stat.ttftSamples++
	}
	stat.lastSample = now
	stat.samples++
}

func (s *openAIAccountRuntimeStats) snapshot(accountID int64, models ...string) (errorRate float64, ttft float64, hasTTFT bool) {
	if s == nil || accountID <= 0 {
		return 0, 0, false
	}
	value, ok := s.accounts.Load(openAIStatsKey(accountID, models))
	if !ok {
		return 0, 0, false
	}
	stat, ok := value.(*openAIAccountRuntimeStat)
	if !ok || stat == nil {
		return 0, 0, false
	}
	stat.mu.Lock()
	defer stat.mu.Unlock()
	if stat.samples < openAIHealthMinSamples || time.Since(stat.lastSample) > openAIHealthTTL {
		return 0, 0, false
	}
	errorRate = decayedSchedulingError(math.Float64frombits(stat.errorRateEWMABits.Load()), time.Since(stat.lastSample))
	latency := math.Float64frombits(stat.ttftEWMABits.Load())
	if stat.ttftSamples < openAIHealthMinSamples || time.Since(stat.lastTTFTSample) > openAIHealthTTL || math.IsNaN(latency) {
		return errorRate, 0, false
	}
	return errorRate, latency, true
}

func (s *openAIAccountRuntimeStats) size() int {
	if s == nil {
		return 0
	}
	return int(s.accountCount.Load())
}

type defaultOpenAIAccountScheduler struct {
	service                *OpenAIGatewayService
	metrics                openAIAccountSchedulerMetrics
	stats                  *openAIAccountRuntimeStats
	grokFreeQuotaGateCache sync.Map // key: int64(accountID), value: grokFreeQuotaGateCacheEntry
}

type openAISelectionProbeBudget struct {
	acquires  int
	rechecks  int
	attempted map[int64]struct{}
}

func newOpenAISelectionProbeBudget() *openAISelectionProbeBudget {
	return &openAISelectionProbeBudget{attempted: make(map[int64]struct{})}
}

func (b *openAISelectionProbeBudget) recordAcquire(accountID int64) bool {
	if b == nil {
		return false
	}
	if b.acquires >= openAIAccountSelectionProbeLimit {
		return false
	}
	if b.attempted == nil {
		b.attempted = make(map[int64]struct{})
	}
	b.acquires++
	b.attempted[accountID] = struct{}{}
	return true
}

func (b *openAISelectionProbeBudget) recordRecheck() bool {
	if b == nil {
		return false
	}
	if b.rechecks >= openAIAccountSelectionProbeLimit {
		return false
	}
	b.rechecks++
	return true
}

func (b *openAISelectionProbeBudget) acquireExhausted() bool {
	return b != nil && b.acquires >= openAIAccountSelectionProbeLimit
}

func (b *openAISelectionProbeBudget) wasAttempted(accountID int64) bool {
	if b == nil {
		return false
	}
	_, ok := b.attempted[accountID]
	return ok
}

func newDefaultOpenAIAccountScheduler(service *OpenAIGatewayService, stats *openAIAccountRuntimeStats) OpenAIAccountScheduler {
	if stats == nil {
		stats = newOpenAIAccountRuntimeStats()
	}
	return &defaultOpenAIAccountScheduler{
		service: service,
		stats:   stats,
	}
}

func (s *defaultOpenAIAccountScheduler) Select(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision, err error) {
	ctx = withSchedulingAffinity(ctx)
	standalone := selectionbudget.From(ctx) == nil
	ctx, externalBudget, cancelSelection := selectionbudget.Start(ctx)
	defer cancelSelection()
	defer func() {
		if ctx.Err() != nil {
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			selection = nil
			err = ctx.Err()
		}
		if standalone {
			finishSchedulingBudget(ctx, externalBudget, &err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, decision, err
	}
	if s != nil && s.service != nil && s.service.openAIGroupRequiresPrivacySet(ctx, req.GroupID) {
		req.RequirePrivacySet = true
	}
	start := time.Now()
	// 命名返回值保证 defer 写入的耗时同时返回给调用方。
	defer func() {
		if selection != nil && selection.Account != nil {
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "capacity", Action: "selected", AccountID: selection.Account.ID, ConfiguredCapacity: selection.Account.Concurrency, EffectiveCapacity: nativeSchedulingConcurrency(ctx, selection.Account, s.service.settingService)})
		}
		if selection != nil && selection.WaitPlan != nil && selection.Account != nil {
			account := selection.Account
			selection.WaitPlan.RefreshLimit = func(waitCtx context.Context) (int, error) {
				fresh := s.service.recheckSelectedOpenAIAccountFromDB(selectionbudget.PreparationContext(waitCtx), account, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
				if fresh == nil || !s.isAccountRequestCompatible(waitCtx, fresh, req) || !s.isAccountTransportCompatible(waitCtx, fresh, req.RequiredTransport, req.RequestedModel) {
					return 0, ErrNoAvailableAccounts
				}
				return nativeSchedulingConcurrency(waitCtx, fresh, s.service.settingService), nil
			}
		}
		decision.LatencyMs = time.Since(start).Milliseconds()
		s.metrics.recordSelect(decision)
	}()

	previousResponseID := strings.TrimSpace(req.PreviousResponseID)
	if previousResponseID != "" && NormalizeOpenAICompatiblePlatform(req.Platform) == PlatformOpenAI {
		selection, err := s.service.selectAccountByPreviousResponseIDForCapability(
			ctx,
			req.GroupID,
			previousResponseID,
			req.RequestedModel,
			req.ExcludedIDs,
			req.RequiredCapability,
			req.RequireCompact,
		)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			compatible, reason := s.isAccountRequestCompatibleReason(ctx, selection.Account, req)
			hasGroupMetadata := len(selection.Account.GroupIDs) > 0 || len(selection.Account.AccountGroups) > 0
			groupCompatible := !hasGroupMetadata || openAIStickyAccountMatchesGroup(selection.Account, req.GroupID)
			if hasGroupMetadata && s.service != nil {
				groupCompatible = s.service.openAIAccountMatchesSchedulingGroup(selection.Account, req.GroupID)
			}
			if !groupCompatible ||
				!compatible || !s.isAccountTransportCompatible(ctx, selection.Account, req.RequiredTransport, req.RequestedModel) {
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				selection = nil
				// Request-size protection cannot move a server-side response owner.
				// Let the protocol layer decide whether complete history permits a
				// new request, rather than silently selecting a different account.
				if reason == SchedulingQuotaRequestTooLarge {
					return nil, decision, fmt.Errorf("%w: %s", ErrNoAvailableAccounts, reason)
				}
			}
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerPreviousResponse
			decision.StickyPreviousHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			if req.SessionHash != "" {
				_ = s.service.bindOpenAIStickySessionDuringSelection(ctx, req.GroupID, req.SessionHash, selection.Account.ID)
			}
			return selection, decision, nil
		}
	}

	if req.GuardianParentAccountID > 0 {
		parentReq := req
		parentReq.StickyAccountID = req.GuardianParentAccountID
		parentReq.PreserveStickyBinding = true
		selection, _, err := s.selectBySessionHash(ctx, parentReq)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerGuardianParent
			decision.StickySessionHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			return selection, decision, nil
		}
	}

	{
		selection, escapedSticky, err := s.selectBySessionHash(ctx, req)
		if err != nil {
			return nil, decision, err
		}
		if selection != nil && selection.Account != nil {
			decision.Layer = openAIAccountScheduleLayerSessionSticky
			decision.StickySessionHit = true
			decision.SelectedAccountID = selection.Account.ID
			decision.SelectedAccountType = selection.Account.Type
			return selection, decision, nil
		}
		if escapedSticky {
			if home := schedulingAffinityFrom(ctx); home != nil && home.home != nil {
				req.PreserveStickyBinding = true
			}
		}
	}

	selection, candidateCount, topK, loadSkew, err := s.selectByLoadBalance(ctx, req)
	decision.Layer = openAIAccountScheduleLayerLoadBalance
	decision.CandidateCount = candidateCount
	decision.TopK = topK
	decision.LoadSkew = loadSkew
	if home := schedulingAffinityFrom(ctx); home != nil && home.home != nil && (selection == nil || !selection.Acquired) && (err == nil || errors.Is(err, ErrNoAvailableAccounts)) {
		fresh := s.service.recheckSelectedOpenAIAccountFromDB(ctx, home.home, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
		if fresh != nil && s.isAccountRequestCompatible(ctx, fresh, req) && s.isAccountTransportCompatible(ctx, fresh, req.RequiredTransport, req.RequestedModel) {
			wait := *home.wait
			wait.MaxConcurrency = nativeSchedulingConcurrency(ctx, fresh, s.service.settingService)
			selection = attachSelectionProfitGate(ctx, &AccountSelectionResult{Account: fresh, WaitPlan: &wait})
			err = nil
			decision.Layer = openAIAccountScheduleLayerSessionSticky
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "affinity", Action: "wait_owner", AccountID: fresh.ID})
		}
	}
	if err != nil {
		return nil, decision, err
	}
	if selection != nil && selection.Account != nil {
		decision.SelectedAccountID = selection.Account.ID
		decision.SelectedAccountType = selection.Account.Type

	}
	return selection, decision, nil
}

func (s *defaultOpenAIAccountScheduler) selectBySessionHash(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, bool, error) {
	sessionHash := strings.TrimSpace(req.SessionHash)
	if s == nil || s.service == nil || (TurnRetryAccount(ctx) == nil && (sessionHash == "" || s.service.cache == nil)) {
		return nil, false, nil
	}

	accountID := req.StickyAccountID
	clearBinding := func() {
		if !req.PreserveStickyBinding {
			_ = s.service.deleteStickySessionAccountID(ctx, req.GroupID, sessionHash)
		}
	}
	if accountID <= 0 {
		var err error
		accountID, err = s.service.getStickySessionAccountID(ctx, req.GroupID, sessionHash)
		if err != nil || accountID <= 0 {
			return nil, false, nil
		}
	}
	if accountID <= 0 {
		return nil, false, nil
	}
	if req.ExcludedIDs != nil {
		if _, excluded := req.ExcludedIDs[accountID]; excluded {
			return nil, false, nil
		}
	}

	account, err := s.service.getSchedulableAccount(ctx, accountID)
	if err != nil {
		// A failed lookup does not prove that the bound account disappeared.
		// Keep affinity across transient repository/snapshot failures.
		if errors.Is(err, ErrAccountNotFound) {
			clearBinding()
		}
		return nil, false, nil
	}
	if account == nil {
		clearBinding()
		return nil, false, nil
	}
	if shouldClearStickySession(account, req.RequestedModel) || account.Platform != NormalizeOpenAICompatiblePlatform(req.Platform) || !account.IsOpenAICompatible() || !account.IsSchedulable() {
		clearBinding()
		return nil, false, nil
	}
	if compatible, reason := s.isAccountRequestCompatibleReason(ctx, account, req); !compatible {
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "affinity", Layer: openAIAccountScheduleLayerSessionSticky, Action: "rejected", Reason: reason, AccountID: accountID})
		return nil, false, nil
	}
	if !s.isAccountTransportCompatible(ctx, account, req.RequiredTransport, req.RequestedModel) {
		// Transport requirements belong to this request; an HTTP-only account
		// remains a valid home if a WS attempt cannot find a replacement.
		return nil, false, nil
	}
	account, err = s.service.recheckSelectedOpenAIAccountFromDBBeforeProfit(ctx, account, req.GroupID, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
	if err != nil {
		if errors.Is(err, ErrAccountNotFound) {
			clearBinding()
		}
		return nil, false, nil
	}
	if account == nil || !s.service.openAIAccountMatchesSchedulingGroup(account, req.GroupID) {
		clearBinding()
		return nil, false, nil
	}
	// Fresh request-only vetoes (including profit and transport) must retain
	// the same affinity semantics as the snapshot checks above.
	if !s.isAccountRequestCompatible(ctx, account, req) || !s.isAccountTransportCompatible(ctx, account, req.RequiredTransport, req.RequestedModel) {
		return nil, false, nil
	}
	// Free-tier soft gate: sticky session must not pin an over-quota free OAuth account.
	// Admin QueryQuota / import probes do not use this path.
	if account != nil && len(s.filterGrokFreeQuotaAccounts(ctx, []Account{*account})) == 0 {
		clearBinding()
		return nil, false, nil
	}
	// Team+model cool: sticky must not pin a sibling under the same team 429 window.
	now := time.Now()
	upstreamModel := canonicalOpenAIAccountSchedulingModel(account, req.RequestedModel, req.RequireCompact)
	if account != nil && isGrokTeamModelRateLimited(account, upstreamModel, now) {
		clearBinding()
		return nil, false, nil
	}
	if account != nil && isGrokModelQuotaBlocked(account.ID, upstreamModel, now) {
		clearBinding()
		return nil, false, nil
	}
	if reason, errorRate, ttft, shouldEscape := s.shouldEscapeStickyAccount(accountID, upstreamModel, s.schedulingHealthCohort(ctx, account, req)); shouldEscape && !req.DisableStickyEscape {
		recordSchedulingEscape(ctx, reason)
		slog.Info("sticky_escape_triggered",
			"account_id", accountID,
			"reason", reason,
			"error_rate", errorRate,
			"ttft", ttft,
		)
		return nil, true, nil
	}
	result, acquireErr := s.service.tryAcquireAccountSlot(ctx, accountID, nativeSchedulingConcurrency(ctx, account, s.service.settingService))
	if acquireErr != nil || (result != nil && !result.Acquired) {
		reason := "concurrency_full"
		if acquireErr != nil {
			reason = "acquire_error"
		}
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "affinity", Layer: openAIAccountScheduleLayerSessionSticky, Action: "unavailable", Reason: reason, AccountID: accountID})
	}
	if acquireErr != nil {
		return nil, false, acquireErr
	}
	if acquireErr == nil && result != nil && result.Acquired {
		if !req.PreserveStickyBinding {
			_ = s.service.refreshStickySessionTTL(ctx, req.GroupID, sessionHash, account.ID, s.service.openAIWSSessionStickyTTL())
		}
		return attachSelectionProfitGate(ctx, &AccountSelectionResult{
			Account:     account,
			Acquired:    true,
			ReleaseFunc: result.ReleaseFunc,
		}), false, nil
	}

	cfg := s.service.schedulingConfig()
	// WaitPlan.MaxConcurrency 使用 Concurrency（非 EffectiveLoadFactor），因为 WaitPlan 控制的是 Redis 实际并发槽位等待。
	if s.service.concurrencyService != nil {
		if !req.DisableStickyEscape && acquireErr == nil && result != nil && !result.Acquired {
			recordSchedulingEscape(ctx, "concurrency_full")
			if !req.PreserveStickyBinding {
				rememberBusyStickyHome(ctx, sessionHash, account, &AccountWaitPlan{AccountID: account.ID, MaxConcurrency: nativeSchedulingConcurrency(ctx, account, s.service.settingService), Timeout: cfg.StickySessionWaitTimeout, MaxWaiting: cfg.StickySessionMaxWaiting})
			}
			errorRate, ttft, _ := s.stats.snapshot(accountID, canonicalOpenAIAccountSchedulingModel(account, req.RequestedModel, req.RequireCompact), s.schedulingHealthCohort(ctx, account, req))
			slog.Info("sticky_escape_triggered",
				"account_id", accountID,
				"reason", "concurrency_full",
				"error_rate", errorRate,
				"ttft", ttft,
			)
			return nil, true, nil
		}
		return attachSelectionProfitGate(ctx, &AccountSelectionResult{
			Account: account,
			WaitPlan: &AccountWaitPlan{
				AccountID:      accountID,
				MaxConcurrency: nativeSchedulingConcurrency(ctx, account, s.service.settingService),
				Timeout:        cfg.StickySessionWaitTimeout,
				MaxWaiting:     cfg.StickySessionMaxWaiting,
			},
		}), false, nil
	}
	return nil, false, nil
}

func openAIStickyAccountMatchesGroup(account *Account, groupID *int64) bool {
	if account == nil {
		return false
	}
	if groupID == nil {
		return len(account.AccountGroups) == 0 && len(account.GroupIDs) == 0
	}
	for _, accountGroupID := range account.GroupIDs {
		if accountGroupID == *groupID {
			return true
		}
	}
	for _, accountGroup := range account.AccountGroups {
		if accountGroup.GroupID == *groupID {
			return true
		}
	}
	return false
}

func (s *defaultOpenAIAccountScheduler) shouldEscapeStickyAccount(accountID int64, models ...string) (reason string, errorRate float64, ttft float64, shouldEscape bool) {
	if s == nil || s.stats == nil || accountID <= 0 {
		return "", 0, 0, false
	}
	errorRate, ttft, _ = s.stats.snapshot(accountID, models...)
	if errorRate > openAIHealthDegradedErrorRate {
		return "error_rate", errorRate, ttft, true
	}
	return "", errorRate, ttft, false
}

type openAIAccountCandidateScore struct {
	account       *Account
	loadInfo      *AccountLoadInfo
	loadKnown     bool
	score         float64
	orderKey      float64
	preference    int
	costBand      int
	domainPenalty int
	capacity      int
	degraded      bool
	errorRate     float64
	ttft          float64
	hasTTFT       bool
}

func isOpenAIAccountCandidateBetter(left openAIAccountCandidateScore, right openAIAccountCandidateScore) bool {
	// Confirmed model-scoped degradation yields to healthy capacity, even in a
	// lower business tier. Otherwise a health escape would select its owner again.
	if left.degraded != right.degraded {
		return !left.degraded
	}
	if left.account.Priority != right.account.Priority {
		return left.account.Priority < right.account.Priority
	}
	if left.preference != right.preference {
		return left.preference < right.preference
	}
	if left.costBand != right.costBand {
		return left.costBand < right.costBand
	}
	if left.domainPenalty != right.domainPenalty {
		return left.domainPenalty < right.domainPenalty
	}
	if left.orderKey != right.orderKey {
		return left.orderKey < right.orderKey
	}
	if left.score != right.score {
		return left.score > right.score
	}
	if left.loadInfo.LoadRate != right.loadInfo.LoadRate {
		return left.loadInfo.LoadRate < right.loadInfo.LoadRate
	}
	if left.loadInfo.WaitingCount != right.loadInfo.WaitingCount {
		return left.loadInfo.WaitingCount < right.loadInfo.WaitingCount
	}
	return left.account.ID < right.account.ID
}

type openAISelectionRNG struct {
	state uint64
}

func newOpenAISelectionRNG(seed uint64) openAISelectionRNG {
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	return openAISelectionRNG{state: seed}
}

func (r *openAISelectionRNG) nextUint64() uint64 {
	// xorshift64*
	x := r.state
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.state = x
	return x * 2685821657736338717
}

func (r *openAISelectionRNG) nextFloat64() float64 {
	// [0,1)
	return float64(r.nextUint64()>>11) / (1 << 53)
}

func deriveOpenAISelectionSeed(req OpenAIAccountScheduleRequest) uint64 {
	hasher := fnv.New64a()
	writeValue := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		_, _ = hasher.Write([]byte(trimmed))
		_, _ = hasher.Write([]byte{0})
	}

	writeValue(req.SessionHash)
	writeValue(req.PreviousResponseID)
	writeValue(req.RequestedModel)
	if req.GroupID != nil {
		_, _ = hasher.Write([]byte(strconv.FormatInt(*req.GroupID, 10)))
	}

	seed := hasher.Sum64()
	// 对“无会话锚点”的纯负载均衡请求引入时间熵，避免固定命中同一账号。
	if strings.TrimSpace(req.SessionHash) == "" && strings.TrimSpace(req.PreviousResponseID) == "" {
		seed ^= uint64(time.Now().UnixNano())
	}
	if seed == 0 {
		seed = uint64(time.Now().UnixNano()) ^ 0x9e3779b97f4a7c15
	}
	return seed
}

func (s *defaultOpenAIAccountScheduler) buildOpenAIAccountLoadPlan(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	filtered []*Account,
	loadMap map[int64]*AccountLoadInfo,
) openAIAccountLoadPlan {
	req = s.prepareOpenAIBusinessLayers(ctx, req, filtered)
	allCandidates := make([]openAIAccountCandidateScore, 0, len(filtered))
	for _, account := range filtered {
		loadInfo, loadKnown := loadMap[account.ID]
		if !loadKnown || loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
			loadKnown = false
		}
		health := req.health[account.ID]
		allCandidates = append(allCandidates, openAIAccountCandidateScore{
			costBand:      req.costBands[account.ID],
			domainPenalty: turnRecoveryDomainPenalty(ctx, account),
			capacity:      req.capacities[account.ID],
			account:       account,
			loadInfo:      loadInfo,
			loadKnown:     loadKnown,
			errorRate:     health.errorRate,
			ttft:          health.ttft,
			hasTTFT:       health.hasTTFT,
		})
	}

	candidates := allCandidates
	staleSnapshotCompactRetry := make([]openAIAccountCandidateScore, 0, len(allCandidates))
	if req.RequireCompact {
		candidates = make([]openAIAccountCandidateScore, 0, len(allCandidates))
		for _, candidate := range allCandidates {
			if openAICompactSupportTier(candidate.account) == 0 {
				staleSnapshotCompactRetry = append(staleSnapshotCompactRetry, candidate)
				continue
			}
			candidates = append(candidates, candidate)
		}
	}

	plan := openAIAccountLoadPlan{
		allCandidates:             allCandidates,
		candidates:                candidates,
		staleSnapshotCompactRetry: staleSnapshotCompactRetry,
		candidateCount:            len(candidates),
		topK:                      min(openAIAccountSelectionProbeLimit, len(candidates)),
	}
	// The sampling stage needs capacity and business layers; dynamic soft
	// scores are computed only for the observed batch.
	if len(candidates) == 0 || req.samplingOnly {
		plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
		return plan
	}

	maxWaiting := 1
	loadRateSum := 0.0
	loadRateSumSquares := 0.0
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.loadInfo.WaitingCount > maxWaiting {
			maxWaiting = candidate.loadInfo.WaitingCount
		}

		loadRate := float64(candidate.loadInfo.LoadRate)
		loadRateSum += loadRate
		loadRateSumSquares += loadRate * loadRate
	}
	plan.loadSkew = calcLoadSkewByMoments(loadRateSum, loadRateSumSquares, len(candidates))

	weights := s.service.openAIWSSchedulerWeightsForRequest(ctx)
	if !req.SubscriptionPriority {
		weights.Reset = 0
	}
	now := time.Now()
	// Reset 因子（use-it-or-lose-it）：优先读取 Codex 5h 重置时间，回退到会话窗口。
	// 剩余时间越短 → 因子越接近 1（越早重置越优先用尽）。无活跃窗口的账号因子为 0。
	// 仅在 weights.Reset > 0 时计算，默认关闭不影响原有行为。
	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	if weights.Reset > 0 {
		for _, candidate := range candidates {
			end, ok := openAISchedulingResetWindowEnd(candidate.account, now)
			if !ok {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining = remaining, remaining
				hasResetSample = true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	for i := range candidates {
		item := &candidates[i]
		loadFactor := 1 - clamp01(float64(item.loadInfo.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(item.loadInfo.WaitingCount)/float64(maxWaiting))
		errorFactor := 1 - clamp01(item.errorRate)
		ttftFactor := req.health[item.account.ID].latencyFactor
		if ttftFactor == 0 {
			ttftFactor = 0.5
		}
		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end, ok := openAISchedulingResetWindowEnd(item.account, now); ok {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01((end.Sub(now).Seconds()-minResetRemaining)/(maxResetRemaining-minResetRemaining))
				} else {
					// 所有有窗口的账号剩余时间相同：一律给满分，让其优于无窗口账号。
					resetFactor = 1
				}
			}
		}
		quotaHeadroomFactor := 0.0
		if weights.QuotaHeadroom > 0 {
			quotaHeadroomFactor = openAIQuotaHeadroomFactor(item.account, now)
		}

		item.score = weights.Load*loadFactor +
			weights.Queue*queueFactor +
			weights.ErrorRate*errorFactor +
			weights.TTFT*ttftFactor +
			weights.Reset*resetFactor +
			weights.QuotaHeadroom*quotaHeadroomFactor

	}
	plan.candidates = candidates

	plan.selectionOrder = s.buildOpenAISelectionOrder(req, plan)
	return plan
}

func (s *defaultOpenAIAccountScheduler) buildOpenAISelectionOrder(
	req OpenAIAccountScheduleRequest,
	plan openAIAccountLoadPlan,
) []openAIAccountCandidateScore {
	// Keep complete business layers, including lower layers that a global top-K
	// would discard. External work, rather than candidate count, is bounded.
	buildSelectionOrder := func(pool []openAIAccountCandidateScore) []openAIAccountCandidateScore {
		var ready []openAIAccountCandidateScore
		if req.samplingOnly {
			ready = pool[:0]
		} else {
			ready = make([]openAIAccountCandidateScore, 0, len(pool))
		}
		waiting := make([]openAIAccountCandidateScore, 0)
		seed := deriveOpenAISelectionSeed(req)
		for _, candidate := range pool {
			if req.SubscriptionPriority && !candidate.account.IsOpenAIChatGPTSubscription() {
				candidate.preference = 1
			}
			candidate.degraded = candidate.errorRate > openAIHealthDegradedErrorRate
			capacity := float64(max(1, candidate.capacity))
			limit := req.concurrencyLimit(candidate.account)
			if candidate.loadKnown && limit > 0 {
				capacity *= math.Max(0.01, 1-clamp01(float64(candidate.loadInfo.CurrentConcurrency)/float64(limit)))
			}
			if math.IsNaN(candidate.score) || math.IsInf(candidate.score, 0) {
				candidate.score = 0
			}
			weight := capacity * math.Max(0.1, 1+candidate.score)
			// Bind the sample to identity, not the filtered slice position. Removing
			// a failed account must not move its winning weight to its neighbour.
			rng := newOpenAISelectionRNG(seed ^ (uint64(candidate.account.ID) * 0x9e3779b97f4a7c15))
			candidate.orderKey = -math.Log(math.Max(rng.nextFloat64(), math.SmallestNonzeroFloat64)) / weight
			if candidate.loadKnown && limit > 0 && candidate.loadInfo.CurrentConcurrency >= limit {
				waiting = append(waiting, candidate)
			} else {
				ready = append(ready, candidate)
			}
		}
		if req.samplingOnly {
			return append(ready, waiting...)
		}
		for _, bucket := range [][]openAIAccountCandidateScore{ready, waiting} {
			sort.Slice(bucket, func(i, j int) bool { return isOpenAIAccountCandidateBetter(bucket[i], bucket[j]) })
		}
		return append(ready, waiting...)
	}

	if req.RequireCompact {
		supported := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		unknown := make([]openAIAccountCandidateScore, 0, len(plan.candidates))
		for _, candidate := range plan.candidates {
			switch openAICompactSupportTier(candidate.account) {
			case 2:
				supported = append(supported, candidate)
			case 1:
				unknown = append(unknown, candidate)
			}
		}
		selectionOrder := make([]openAIAccountCandidateScore, 0, len(plan.allCandidates))
		selectionOrder = append(selectionOrder, buildSelectionOrder(supported)...)
		selectionOrder = append(selectionOrder, buildSelectionOrder(unknown)...)
		if len(plan.staleSnapshotCompactRetry) > 0 && s.service.schedulerSnapshot != nil {
			selectionOrder = append(selectionOrder, buildSelectionOrder(plan.staleSnapshotCompactRetry)...)
		}
		return selectionOrder
	}

	return buildSelectionOrder(plan.candidates)
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAISelectionOrder(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	selectionOrder []openAIAccountCandidateScore,
) (*AccountSelectionResult, bool, error) {
	budget := newOpenAISelectionProbeBudget()
	return s.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, selectionOrder, budget)
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAISelectionOrderWithBudget(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	selectionOrder []openAIAccountCandidateScore,
	budget *openAISelectionProbeBudget,
) (*AccountSelectionResult, bool, error) {
	compactBlocked := false
	release := func(result *AcquireResult) {
		if result != nil && result.ReleaseFunc != nil {
			result.ReleaseFunc()
		}
	}
	selectionCtx := ctx
	layerEnd, probeCeiling := 0, openAIAccountSelectionProbeLimit
	for i := 0; i < len(selectionOrder); i++ {
		if i >= layerEnd {
			layerEnd = i + 1
			for layerEnd < len(selectionOrder) && sameOpenAIBusinessLayer(selectionOrder[i], selectionOrder[layerEnd]) {
				layerEnd++
			}
			ctx = selectionCtx
			probeCeiling = openAIAccountSelectionProbeLimit
			if layerEnd < len(selectionOrder) {
				ctx = selectionbudget.Reserve(ctx, min(selectionbudget.Remaining(ctx), max(12, (selectionbudget.Remaining(ctx)+3)/4)))
				probeCeiling -= (openAIAccountSelectionProbeLimit - budget.acquires + 3) / 4
			}
			if i > 0 {
				RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "selection", Action: "next_layer"})
			}
		}
		if selectionbudget.Remaining(ctx) < 12 || budget.acquires >= probeCeiling {
			i = layerEnd - 1
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, compactBlocked, err
		}
		candidate := selectionOrder[i]
		if candidate.account == nil {
			continue
		}
		if candidate.loadKnown && nativeSchedulingConcurrency(ctx, candidate.account, s.service.settingService) > 0 &&
			candidate.loadInfo.CurrentConcurrency >= nativeSchedulingConcurrency(ctx, candidate.account, s.service.settingService) {
			continue
		}

		result, attempted, acquireErr := s.tryAcquireOpenAIAccountSlot(ctx, candidate.account.ID, nativeSchedulingConcurrency(ctx, candidate.account, s.service.settingService), budget)
		if !attempted {
			break
		}
		if acquireErr != nil {
			return nil, compactBlocked, acquireErr
		}
		if result == nil || !result.Acquired {
			continue
		}

		fresh := s.service.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(ctx, fresh, req.RequiredTransport, req.RequestedModel) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			release(result)
			continue
		}
		if !s.consumeOpenAISelectionDBRecheck(budget) {
			release(result)
			break
		}
		fresh = s.service.recheckSelectedOpenAIAccountFromDB(ctx, fresh, req.GroupID, req.Platform, req.RequestedModel, false, req.RequiredCapability)
		if fresh == nil || !s.isAccountTransportCompatible(ctx, fresh, req.RequiredTransport, req.RequestedModel) || !s.isAccountRequestCompatible(ctx, fresh, req) {
			release(result)
			continue
		}
		if req.RequireCompact && openAICompactSupportTier(fresh) == 0 {
			compactBlocked = true
			release(result)
			continue
		}

		if nativeSchedulingConcurrency(ctx, fresh, s.service.settingService) != nativeSchedulingConcurrency(ctx, candidate.account, s.service.settingService) {
			release(result)
			result, attempted, acquireErr = s.tryAcquireOpenAIAccountSlot(ctx, fresh.ID, nativeSchedulingConcurrency(ctx, fresh, s.service.settingService), budget)
			if !attempted {
				continue
			}
			if acquireErr != nil {
				return nil, compactBlocked, acquireErr
			}
			if result == nil || !result.Acquired {
				continue
			}
		}
		if req.SessionHash != "" && !req.PreserveStickyBinding {
			_ = s.service.bindOpenAIStickySessionDuringSelection(ctx, req.GroupID, req.SessionHash, fresh.ID)
		}
		return attachSelectionProfitGate(ctx, &AccountSelectionResult{
			Account:     fresh,
			Acquired:    true,
			ReleaseFunc: result.ReleaseFunc,
		}), compactBlocked, nil
	}
	return nil, compactBlocked, nil
}

func (s *defaultOpenAIAccountScheduler) tryAcquireOpenAIAccountSlot(
	ctx context.Context,
	accountID int64,
	maxConcurrency int,
	budget *openAISelectionProbeBudget,
) (*AcquireResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if !budget.recordAcquire(accountID) {
		return nil, false, nil
	}
	result, err := s.service.tryAcquireAccountSlot(ctx, accountID, maxConcurrency)
	return result, true, err
}

func (s *defaultOpenAIAccountScheduler) consumeOpenAISelectionDBRecheck(budget *openAISelectionProbeBudget) bool {
	if s.service.schedulerSnapshot == nil || s.service.accountRepo == nil {
		return true
	}
	return budget.recordRecheck()
}

// openAISelectionFilterStats counts why candidates were dropped by the
// selectByLoadBalance initial filter. Historically these exclusions were
// silent (debug logs at best), so a "no available accounts" failure with
// excluded_account_count=0 was undiagnosable from the error alone (#4599).
// The reasons map is lazily allocated: on the happy path (nothing filtered
// out, or an account is eventually selected) no extra allocation happens.
type openAISelectionFilterStats struct {
	pool    int
	reasons map[string]int
}

func (s *openAISelectionFilterStats) exclude(reason string) {
	if s.reasons == nil {
		s.reasons = make(map[string]int, 4)
	}
	s.reasons[reason]++
}

// summary renders deterministic exclusion statistics for scheduling error
// messages, e.g. "pool=3, filtered: model_not_supported=2 quota_auto_pause_7d=1".
// Reasons are sorted lexicographically so the output is stable for tests and
// log aggregation. extra, when non-empty, is appended as a trailing marker.
func (s openAISelectionFilterStats) summary(extra string) string {
	var b strings.Builder
	_, _ = b.WriteString("pool=")
	_, _ = b.WriteString(strconv.Itoa(s.pool))
	if len(s.reasons) > 0 {
		reasons := make([]string, 0, len(s.reasons))
		for reason := range s.reasons {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		_, _ = b.WriteString(", filtered:")
		for _, reason := range reasons {
			_, _ = b.WriteString(" ")
			_, _ = b.WriteString(reason)
			_, _ = b.WriteString("=")
			_, _ = b.WriteString(strconv.Itoa(s.reasons[reason]))
		}
	}
	if extra != "" {
		_, _ = b.WriteString(", ")
		_, _ = b.WriteString(extra)
	}
	return b.String()
}

func (s *defaultOpenAIAccountScheduler) selectByLoadBalance(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
) (*AccountSelectionResult, int, int, float64, error) {
	budget := newOpenAISelectionProbeBudget()
	accounts, err := s.service.listSchedulableAccounts(ctx, req.GroupID, req.Platform)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	if len(accounts) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false, openAISelectionFilterStats{}.summary(""))
	}
	// Local free-tier soft gate on the Grok scheduling path only (not admin probe).
	accounts = s.filterGrokFreeQuotaAccounts(ctx, accounts)
	if len(accounts) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false, openAISelectionFilterStats{}.summary("grok_free_quota_soft_gate"))
	}
	// Team+model rate-limit cool: siblings of a 429'd team skip the hot model.
	if req.Platform == PlatformGrok {
		now := time.Now()
		filtered := filterGrokTeamModelRateLimitedAccounts(accounts, req.RequestedModel, now)
		if len(filtered) == 0 && len(accounts) > 0 {
			return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false, openAISelectionFilterStats{}.summary("grok_team_model_rate_limit"))
		}
		if filtered != nil {
			accounts = filtered
		}
		// Per-account model free-usage soft-block (other models stay eligible).
		modelFiltered := filterGrokModelQuotaBlockedAccounts(accounts, req.RequestedModel, now)
		if len(modelFiltered) == 0 && len(accounts) > 0 {
			return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false, openAISelectionFilterStats{}.summary("grok_model_quota_block"))
		}
		accounts = modelFiltered
	}

	// require_privacy_set: 获取分组配置。GetByID 会聚合账号计数，选号不能走它。
	var schedGroup *Group
	if req.GroupID != nil && s.service.schedulerSnapshot != nil {
		schedGroup, _ = s.service.schedulerSnapshot.GetGroupByIDLite(ctx, *req.GroupID)
	}

	if schedGroup != nil && schedGroup.RequirePrivacySet {
		req.RequirePrivacySet = true
	}
	// Probe native capability in the same immutable candidate view. No slot/DB
	// scan is added; actual eligibility is checked again when that phase starts.
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		probeCtx := context.WithValue(ctx, excelBPSNativeProbeKey{}, true)
		for i := range accounts {
			if !excelBPSHasNativeFallback(&accounts[i]) {
				continue
			}
			if s.isAccountRequestCompatible(probeCtx, &accounts[i], req) && s.isAccountTransportCompatible(probeCtx, excelBPSNativeAccount(&accounts[i]), req.RequiredTransport, req.RequestedModel) {
				b.mu.Lock()
				b.bpsRecovery.nativeAvailable = true
				b.mu.Unlock()
				break
			}
		}
	}
	filterStats := openAISelectionFilterStats{pool: len(accounts)}
	filtered := make([]*Account, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if compatible, reason := s.isAccountRequestCompatibleReason(ctx, account, req); !compatible {
			filterStats.exclude(reason)
			continue
		}
		if !s.isAccountTransportCompatible(ctx, account, req.RequiredTransport, req.RequestedModel) {
			filterStats.exclude("transport_incompatible")
			continue
		}
		filtered = append(filtered, account)
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "filter", Layer: openAIAccountScheduleLayerLoadBalance, Action: "filtered", Pool: filterStats.pool, Candidates: len(filtered), Excluded: filterStats.reasons})
	if len(filtered) == 0 {
		return nil, 0, 0, 0, noAvailableOpenAISelectionError(req.RequestedModel, false, filterStats.summary(""))
	}

	req = s.prepareOpenAIBusinessLayers(ctx, req, filtered)
	if home := schedulingAffinityFrom(ctx); home != nil && home.home != nil {
		peers := make([]*Account, 0, len(filtered))
		for _, account := range filtered {
			if account.ID != home.home.ID && account.Priority == home.home.Priority && (req.health[account.ID].errorRate > openAIHealthDegradedErrorRate) == (req.health[home.home.ID].errorRate > openAIHealthDegradedErrorRate) && req.costBands[account.ID] == req.costBands[home.home.ID] && (!req.SubscriptionPriority || account.IsOpenAIChatGPTSubscription() == home.home.IsOpenAIChatGPTSubscription()) {
				peers = append(peers, account)
			}
		}
		filtered = peers
		if len(filtered) == 0 {
			return nil, 0, 0, 0, ErrNoAvailableAccounts
		}
	}
	if len(filtered) <= schedulerLoadBatchSize {
		loads := map[int64]*AccountLoadInfo{}
		if s.service.concurrencyService != nil {
			loads, _ = s.service.concurrencyService.GetAccountsLoadBatch(ctx, buildOpenAIAccountLoadRequestWithCapacity(filtered, req.capacities))
		}
		attempt := s.trySelectByLoadBalancePool(ctx, req, filtered, loads, budget)
		if attempt.err != nil || attempt.result != nil {
			return attempt.result, attempt.candidateCount, attempt.topK, attempt.loadSkew, attempt.err
		}
		return s.finishLoadBalanceSelectionFallback(ctx, req, attempt, budget, filterStats)
	}
	// The static view covers the whole pool; Redis reads follow stable weighted
	// batches. Failed/full batches expand within the same selection budget.
	samplingReq := req
	samplingReq.samplingOnly = true
	initial := s.buildOpenAIAccountLoadPlan(ctx, samplingReq, filtered, nil)
	queue := newSchedulerCandidateHeap(initial.selectionOrder, req.RequireCompact)
	total := queue.Len()
	if req.RequireCompact && total == 0 {
		return nil, len(filtered), initial.topK, 0, ErrNoAvailableCompactAccounts
	}
	combined := openAIAccountLoadSelectionAttempt{candidateCount: len(filtered), topK: initial.topK}
	batches, scanned, layerIndex := 0, 0, 0
	for queue.Len() > 0 {
		if layerIndex > 0 {
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "selection", Action: "next_layer"})
		}
		layerIndex++
		layer := queue.layer(queue.items[0])
		layerCtx := ctx
		if len(queue.layers) > 1 {
			layerCtx = selectionbudget.Reserve(ctx, min(selectionbudget.Remaining(ctx), max(12, (selectionbudget.Remaining(ctx)+3)/4)))
		}
		for queue.Len() > 0 && queue.layer(queue.items[0]) == layer {
			if selectionbudget.Remaining(layerCtx) < 12 || budget.acquireExhausted() || batches >= openAIAccountSelectionProbeLimit {
				queue.dropLayer(layer)
				break
			}
			batch := make([]*Account, 0, schedulerLoadBatchSize)
			for len(batch) < schedulerLoadBatchSize && queue.Len() > 0 && queue.layer(queue.items[0]) == layer {
				candidate, _ := heap.Pop(queue).(openAIAccountCandidateScore)
				batch = append(batch, candidate.account)
			}
			loads := map[int64]*AccountLoadInfo{}
			if s.service.concurrencyService != nil {
				var loadErr error
				loads, loadErr = s.service.concurrencyService.GetAccountsLoadBatch(layerCtx, buildOpenAIAccountLoadRequestWithCapacity(batch, req.capacities))
				if loadErr != nil && ctx.Err() != nil {
					return nil, len(filtered), initial.topK, 0, ctx.Err()
				}
			}
			batches++
			scanned += len(batch)
			attempt := s.trySelectByLoadBalancePool(layerCtx, req, batch, loads, budget)
			combined.selectionOrder = append(combined.selectionOrder, attempt.selectionOrder...)
			combined.compactBlocked = combined.compactBlocked || attempt.compactBlocked
			combined.loadSkew = attempt.loadSkew
			if attempt.err != nil {
				return nil, len(filtered), initial.topK, attempt.loadSkew, attempt.err
			}
			if attempt.result != nil {
				return attempt.result, len(filtered), initial.topK, attempt.loadSkew, nil
			}
		}
	}
	if len(combined.selectionOrder) == 0 && total > 0 {
		return nil, len(filtered), initial.topK, 0, selectionbudget.ErrExhausted
	}
	result, count, topK, skew, err := s.finishLoadBalanceSelectionFallback(ctx, req, combined, budget, filterStats)
	if result == nil && scanned < total && (errors.Is(err, ErrNoAvailableAccounts) || errors.Is(err, ErrNoAvailableCompactAccounts)) {
		err = selectionbudget.ErrExhausted
	}
	return result, count, topK, skew, err
}

func (s *defaultOpenAIAccountScheduler) trySelectByLoadBalancePool(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	filtered []*Account,
	loadMap map[int64]*AccountLoadInfo,
	budget *openAISelectionProbeBudget,
) openAIAccountLoadSelectionAttempt {
	plan := s.buildOpenAIAccountLoadPlan(ctx, req, filtered, loadMap)
	attempt := openAIAccountLoadSelectionAttempt{
		selectionOrder: plan.selectionOrder,
		candidateCount: plan.candidateCount,
		topK:           plan.topK,
		loadSkew:       plan.loadSkew,
	}
	if req.RequireCompact && len(plan.candidates) == 0 && len(plan.staleSnapshotCompactRetry) == 0 {
		attempt.noCompactCandidates = true
		attempt.err = ErrNoAvailableCompactAccounts
		return attempt
	}
	if req.RequireCompact && len(attempt.selectionOrder) == 0 && s.service.schedulerSnapshot == nil {
		attempt.noCompactCandidates = true
		attempt.err = ErrNoAvailableCompactAccounts
		return attempt
	}
	if len(attempt.selectionOrder) == 0 {
		attempt.compactBlocked = req.RequireCompact && len(plan.allCandidates) > 0
		return attempt
	}

	result, compactBlocked, acquireErr := s.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, attempt.selectionOrder, budget)
	attempt.compactBlocked = compactBlocked
	if acquireErr != nil {
		attempt.err = acquireErr
		return attempt
	}
	if result != nil {
		attempt.result = result
		return attempt
	}

	if s.service.concurrencyService != nil && !budget.acquireExhausted() {
		loadReq := buildOpenAIAccountLoadRequestWithCapacity(filtered, req.capacities)
		if freshLoadMap, loadErr := s.service.concurrencyService.GetAccountsLoadBatchFresh(ctx, loadReq); loadErr == nil {
			freshPlan := s.buildOpenAIAccountLoadPlan(ctx, req, filtered, freshLoadMap)

			if len(freshPlan.selectionOrder) > 0 {
				freshResult, freshCompactBlocked, freshAcquireErr := s.tryAcquireOpenAISelectionOrderWithBudget(ctx, req, freshPlan.selectionOrder, budget)
				if freshAcquireErr != nil {
					attempt.err = freshAcquireErr
					return attempt
				}
				if freshResult != nil {
					attempt.result = freshResult
					attempt.selectionOrder = freshPlan.selectionOrder
					attempt.candidateCount = freshPlan.candidateCount
					attempt.topK = freshPlan.topK
					attempt.loadSkew = freshPlan.loadSkew
					return attempt
				}
				attempt.compactBlocked = attempt.compactBlocked || freshCompactBlocked
				attempt.selectionOrder = freshPlan.selectionOrder
				attempt.candidateCount = freshPlan.candidateCount
				attempt.topK = freshPlan.topK
				attempt.loadSkew = freshPlan.loadSkew
			}
		}
	}

	return attempt
}

func buildOpenAIAccountLoadRequest(accounts []*Account) []AccountWithConcurrency {
	loadReq := make([]AccountWithConcurrency, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadReq = append(loadReq, AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: account.EffectiveLoadFactor(),
		})
	}
	return loadReq
}

func (s *defaultOpenAIAccountScheduler) finishLoadBalanceSelectionFallback(
	ctx context.Context,
	req OpenAIAccountScheduleRequest,
	attempt openAIAccountLoadSelectionAttempt,
	budget *openAISelectionProbeBudget,
	filterStats openAISelectionFilterStats,
) (*AccountSelectionResult, int, int, float64, error) {
	candidateCount := attempt.candidateCount
	topK := attempt.topK
	loadSkew := attempt.loadSkew

	if len(attempt.selectionOrder) == 0 {
		return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, attempt.compactBlocked, filterStats.summary("selection_order_empty"))
	}

	cfg := s.service.schedulingConfig()
	compactBlocked := attempt.compactBlocked
	// WaitPlan.MaxConcurrency 使用 Concurrency（非 EffectiveLoadFactor），因为 WaitPlan 控制的是 Redis 实际并发槽位等待。
	for pass := 0; pass < 4; pass++ {
		wantAttempted := pass == 1 || pass == 3
		wantKnownFull := pass >= 2
		for _, candidate := range attempt.selectionOrder {
			if selectionbudget.Remaining(ctx) < 4 {
				return nil, candidateCount, topK, loadSkew, selectionbudget.ErrExhausted
			}
			if err := ctx.Err(); err != nil {
				return nil, candidateCount, topK, loadSkew, err
			}
			if candidate.account == nil {
				continue
			}
			if budget != nil {
				knownFull := candidate.loadKnown && nativeSchedulingConcurrency(ctx, candidate.account, s.service.settingService) > 0 &&
					candidate.loadInfo.CurrentConcurrency >= nativeSchedulingConcurrency(ctx, candidate.account, s.service.settingService)
				if budget.wasAttempted(candidate.account.ID) != wantAttempted || knownFull != wantKnownFull {
					continue
				}
			}
			fresh := s.service.resolveFreshSchedulableOpenAIAccount(ctx, candidate.account, req.Platform, req.RequestedModel, false, req.RequiredCapability)
			if fresh == nil || !s.isAccountTransportCompatible(ctx, fresh, req.RequiredTransport, req.RequestedModel) || !s.isAccountRequestCompatible(ctx, fresh, req) {
				continue
			}
			if !s.consumeOpenAISelectionDBRecheck(budget) {
				return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, compactBlocked, filterStats.summary("selection_order_exhausted"))
			}
			fresh = s.service.recheckSelectedOpenAIAccountFromDB(ctx, fresh, req.GroupID, req.Platform, req.RequestedModel, false, req.RequiredCapability)
			if fresh == nil || !s.isAccountTransportCompatible(ctx, fresh, req.RequiredTransport, req.RequestedModel) || !s.isAccountRequestCompatible(ctx, fresh, req) {
				continue
			}
			if req.RequireCompact && openAICompactSupportTier(fresh) == 0 {
				compactBlocked = true
				continue
			}
			return attachSelectionProfitGate(ctx, &AccountSelectionResult{
				Account: fresh,
				WaitPlan: &AccountWaitPlan{
					AccountID:      fresh.ID,
					MaxConcurrency: nativeSchedulingConcurrency(ctx, fresh, s.service.settingService),
					Timeout:        cfg.FallbackWaitTimeout,
					MaxWaiting:     cfg.FallbackMaxWaiting,
				},
			}), candidateCount, topK, loadSkew, nil
		}
	}

	return nil, candidateCount, topK, loadSkew, noAvailableOpenAISelectionError(req.RequestedModel, compactBlocked, filterStats.summary("selection_order_exhausted"))
}

func (s *defaultOpenAIAccountScheduler) isAccountTransportCompatible(ctx context.Context, account *Account, requiredTransport OpenAIUpstreamTransport, models ...string) bool {
	if account != nil && account.IsOpenAICloudConfigured() {
		return requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE
	}
	if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	if s == nil || s.service == nil {
		return false
	}
	return s.service.isOpenAIAccountTransportCompatible(ctx, account, requiredTransport, models...)
}

func (s *defaultOpenAIAccountScheduler) lookupShadowParentAccount(ctx context.Context, id int64) *Account {
	if s == nil || s.service == nil {
		return nil
	}
	if s.service.schedulerSnapshot != nil {
		if account, err := s.service.schedulerSnapshot.GetAccount(ctx, id); err == nil && account != nil {
			return account
		}
	}
	if s.service.accountRepo == nil {
		return nil
	}
	account, _ := s.service.accountRepo.GetByID(ctx, id)
	return account
}

func (s *defaultOpenAIAccountScheduler) isAccountRequestCompatible(ctx context.Context, account *Account, req OpenAIAccountScheduleRequest) bool {
	compatible, _ := s.isAccountRequestCompatibleReason(ctx, account, req)
	return compatible
}

// isAccountRequestCompatibleReason reports whether the account can serve the
// request, and when it cannot, names the veto point. The reason feeds
// openAISelectionFilterStats so that "no available accounts" errors state why
// each candidate was dropped instead of failing silently (#4599).
func (s *defaultOpenAIAccountScheduler) isAccountRequestCompatibleReason(ctx context.Context, account *Account, req OpenAIAccountScheduleRequest) (bool, string) {
	if account == nil {
		return false, "account_nil"
	}
	if binding := cloudContinuationFromContext(ctx); binding != nil {
		if account.ID != binding.AccountID || !account.IsOpenAICloudConfigured() {
			return false, "cloud_continuation_account"
		}
	}
	if account.IsOpenAICloudConfigured() {
		if req.RequiredCapability != OpenAIEndpointCapabilityResponses || req.RequireCompact {
			return false, "cloud_endpoint_unsupported"
		}
		if s == nil || s.service == nil || s.service.cloudSessions == nil {
			return false, "cloud_gateway_unavailable"
		}
	}
	_, excluded := req.ExcludedIDs[account.ID]
	if s != nil && s.service != nil {
		var eligible bool
		account, eligible = s.service.excelBPSPoolCandidate(ctx, account, req.RequestedModel, excluded)
		if !eligible {
			return false, "excluded"
		}
	} else if excluded {
		return false, "excluded"
	}
	if s != nil && s.service != nil {
		if s.service.isExcelBPSModelCoolingDown(account, req.RequestedModel) {
			return false, excelBPSModelPermissionFilterReason
		}
		if s.service.isExcelBPSRateLimitBlocking(account, req.RequestedModel) {
			return false, excelBPSRateLimitedFilterReason
		}
	}
	mode := "native"

	if account.IsOpenAICloudConfigured() {
		mode = UsageAuthorizationModeCloud
	} else if account.IsExcelBPSEnabledForModel(req.RequestedModel) {
		mode = "bps"
	}
	if !turnAccountEligible(ctx, account, mode) {
		return false, "turn_recovery_target"
	}
	if reason := openAICompatibleAccountBaseEligibilityFailureReason(ctx, account, req.Platform, req.RequestedModel); reason != "" {
		return false, reason
	}

	if req.RequirePrivacySet && !account.IsPrivacySet() {
		return false, "privacy_not_set"
	}
	if s != nil && s.service != nil && s.service.isOpenAIAccountRequestRuntimeBlocked(account, req.RequestedModel) {
		return false, "runtime_blocked"
	}
	if s != nil && s.service != nil && s.service.isOpenAIProxyStreamQuarantined(ctx, account) {
		return false, "proxy_stream_quarantined"
	}
	// Quota auto-pause must be evaluated during the initial filter too. Without it the
	// TopK candidate pool can be filled with paused accounts and the later fresh/DB
	// rechecks won't reach healthy accounts that fell outside TopK — manifesting as
	// "no available accounts" even though healthy ones exist.
	if paused, decision := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
		if decision.reason != "" {
			return false, decision.reason
		}
		reason := "quota_auto_pause"
		if decision.window != "" {
			reason += "_" + decision.window
		}
		return false, reason
	}
	// 母账号健康联动：影子账号的凭据来自母账号，母账号不可调度时影子也不应被选中。
	// Parent-health gate: shadow borrows the parent's credentials; an unschedulable
	// parent must block the shadow across all scheduler paths.
	if !parentHealthyForShadow(account, func(id int64) *Account {
		return s.lookupShadowParentAccount(ctx, id)
	}) {
		return false, "shadow_parent_unhealthy"
	}
	if req.RequestedModel != "" && !supportsOpenAIRequestModel(account, req.RequestedModel, req.RequireCompact) {
		return false, "model_not_supported"
	}
	if req.GroupID != nil && s != nil && s.service != nil &&
		s.service.needsUpstreamChannelRestrictionCheck(ctx, req.GroupID) &&
		s.service.isUpstreamModelRestrictedByChannel(ctx, *req.GroupID, account, req.RequestedModel, req.RequireCompact) {
		return false, "channel_upstream_restricted"
	}
	if !accountSupportsOpenAICapabilities(account, req.RequiredCapability, req.RequiredImageCapability) {
		return false, "capability_mismatch"
	}
	// 分组利润控制：不合格账号在候选过滤与抢槽后终检阶段即被排除，
	// 排序/评分/粘性/熔断只在合格账号之间工作；named reason 进入 filter stats。
	if vetoed, reason := openAIProfitControlVetoReason(ctx, account); vetoed {
		return false, reason
	}
	var quotaSettings *SettingService
	if s != nil && s.service != nil {
		quotaSettings = s.service.settingService
	}
	if reason := schedulingQuotaRequestBlockReason(ctx, account, quotaSettings); reason != "" {
		return false, reason
	}
	return true, ""
}

func (s *defaultOpenAIAccountScheduler) ReportResult(accountID int64, success bool, firstTokenMs *int, models ...string) {
	if s == nil || s.stats == nil {
		return
	}
	s.stats.report(accountID, success, firstTokenMs, models...)
}

func (s *defaultOpenAIAccountScheduler) ReportSwitch() {
	if s == nil {
		return
	}
	s.metrics.recordSwitch()
}

func (s *defaultOpenAIAccountScheduler) SnapshotMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	if s == nil {
		return OpenAIAccountSchedulerMetricsSnapshot{}
	}

	selectTotal := s.metrics.selectTotal.Load()
	prevHit := s.metrics.stickyPreviousHitTotal.Load()
	sessionHit := s.metrics.stickySessionHitTotal.Load()
	stickyHit := s.metrics.stickyHitTotal.Load()
	switchTotal := s.metrics.accountSwitchTotal.Load()
	latencyTotal := s.metrics.latencyMsTotal.Load()
	loadSkewTotal := s.metrics.loadSkewMilliTotal.Load()

	snapshot := OpenAIAccountSchedulerMetricsSnapshot{
		SelectTotal:             selectTotal,
		StickyPreviousHitTotal:  prevHit,
		StickySessionHitTotal:   sessionHit,
		LoadBalanceSelectTotal:  s.metrics.loadBalanceSelectTotal.Load(),
		AccountSwitchTotal:      switchTotal,
		SchedulerLatencyMsTotal: latencyTotal,
		RuntimeStatsEntryCount:  s.stats.size(),
	}
	if selectTotal > 0 {
		snapshot.SchedulerLatencyMsAvg = float64(latencyTotal) / float64(selectTotal)
		snapshot.StickyHitRatio = float64(stickyHit) / float64(selectTotal)
		snapshot.AccountSwitchRate = float64(switchTotal) / float64(selectTotal)
		snapshot.LoadSkewAvg = float64(loadSkewTotal) / 1000 / float64(selectTotal)
	}
	return snapshot
}

func (s *OpenAIGatewayService) openAIAdvancedSchedulerSettingRepo() SettingRepository {
	if s == nil || s.rateLimitService == nil || s.rateLimitService.settingService == nil {
		return nil
	}
	return s.rateLimitService.settingService.settingRepo
}

func (s *OpenAIGatewayService) openAIAdvancedSchedulerRuntimeSettings(ctx context.Context) openAIAdvancedSchedulerRuntimeSettings {
	if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return openAIAdvancedSchedulerRuntimeSettings{
				lowUpstreamRatePriorityEnabled: cached.lowUpstreamRatePriorityEnabled,
				oauthSchedulingRateMultiplier:  cached.oauthSchedulingRateMultiplier,
				subscriptionPriorityEnabled:    cached.subscriptionPriorityEnabled,
			}
		}
	}

	result, _, _ := openAIAdvancedSchedulerSettingSF.Do(openAIAdvancedSchedulerSettingKey, func() (any, error) {
		if cached, ok := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return openAIAdvancedSchedulerRuntimeSettings{
					lowUpstreamRatePriorityEnabled: cached.lowUpstreamRatePriorityEnabled,
					oauthSchedulingRateMultiplier:  cached.oauthSchedulingRateMultiplier,
					subscriptionPriorityEnabled:    cached.subscriptionPriorityEnabled,
				}, nil
			}
		}

		lowUpstreamRatePriorityEnabled := false
		oauthSchedulingRateMultiplier := parseOpenAIOAuthSchedulingRateMultiplier(nil)
		subscriptionPriorityEnabled := false
		if repo := s.openAIAdvancedSchedulerSettingRepo(); repo != nil {
			dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIAdvancedSchedulerSettingDBTimeout)
			defer cancel()

			if values, err := repo.GetMultiple(dbCtx, openAIAdvancedSchedulerRuntimeSettingKeys()); err == nil {
				lowUpstreamRatePriorityEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAILowUpstreamRatePriorityEnabled]), "true")
				oauthSchedulingRateMultiplier = parseOpenAIOAuthSchedulingRateMultiplier(values)
				subscriptionPriorityEnabled = strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled]), "true")
			} else {
				// Read the three business preferences individually on a batch read
				// failure; keep the existing short TTL and diagnostic.
				slog.Warn("openai_advanced_scheduler_settings_batch_load_failed", "error", err)
				fallbackValues := make(map[string]string)
				for _, key := range openAIAdvancedSchedulerRuntimeSettingKeys() {
					if value, valueErr := repo.GetValue(dbCtx, key); valueErr == nil {
						fallbackValues[key] = value
					}
				}
				lowUpstreamRatePriorityEnabled = strings.EqualFold(strings.TrimSpace(fallbackValues[SettingKeyOpenAILowUpstreamRatePriorityEnabled]), "true")
				oauthSchedulingRateMultiplier = parseOpenAIOAuthSchedulingRateMultiplier(fallbackValues)
				subscriptionPriorityEnabled = strings.EqualFold(strings.TrimSpace(fallbackValues[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled]), "true")
			}
		}

		openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
			lowUpstreamRatePriorityEnabled: lowUpstreamRatePriorityEnabled,
			oauthSchedulingRateMultiplier:  oauthSchedulingRateMultiplier,
			subscriptionPriorityEnabled:    subscriptionPriorityEnabled,
			expiresAt:                      time.Now().Add(openAIAdvancedSchedulerSettingCacheTTL).UnixNano(),
		})
		return openAIAdvancedSchedulerRuntimeSettings{
			lowUpstreamRatePriorityEnabled: lowUpstreamRatePriorityEnabled,
			oauthSchedulingRateMultiplier:  oauthSchedulingRateMultiplier,
			subscriptionPriorityEnabled:    subscriptionPriorityEnabled,
		}, nil
	})

	settings, _ := result.(openAIAdvancedSchedulerRuntimeSettings)
	return settings
}

func (s *OpenAIGatewayService) openAIOAuthSchedulingRateMultiplier(ctx context.Context) *float64 {
	return s.openAIAdvancedSchedulerRuntimeSettings(ctx).oauthSchedulingRateMultiplier
}

func (s *OpenAIGatewayService) isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(ctx context.Context) bool {
	settings := s.openAIAdvancedSchedulerRuntimeSettings(ctx)
	return settings.subscriptionPriorityEnabled
}

func openAIAdvancedSchedulerRuntimeSettingKeys() []string {
	return []string{SettingKeyOpenAILowUpstreamRatePriorityEnabled, SettingKeyOpenAIOAuthSchedulingRateMultiplier, SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled}
}

func (s *OpenAIGatewayService) getOpenAIAccountScheduler(ctx context.Context) OpenAIAccountScheduler {
	if s == nil {
		return nil
	}
	s.openaiSchedulerOnce.Do(func() {
		if s.openaiAccountStats == nil {
			s.openaiAccountStats = newOpenAIAccountRuntimeStats()
		}
		if s.openaiScheduler == nil {
			s.openaiScheduler = newDefaultOpenAIAccountScheduler(s, s.openaiAccountStats)
		}
	})
	return s.openaiScheduler
}

func (s *OpenAIGatewayService) SelectAccountWithScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requireCompact bool,
) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	return s.selectAccountWithScheduler(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, "", "", requireCompact, PlatformOpenAI, false, true)
}

// SelectAccountWithSchedulerForCapability 按能力要求调度账号。
// previousResponseCanMove 表示首包 input 可自行重建工具续链，previous_response_id 允许跨账号迁移
// 可用原账号仍优先复用；是否可恢复换号由调用方的 Responses 契约决定。
func (s *OpenAIGatewayService) SelectAccountWithSchedulerForCapability(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requireCompact bool,
	previousResponseCanMove bool,
	useUpstreamTokenCost bool,
	platformOverride ...string,
) (selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision, err error) {
	for {
		selection, decision, err = s.selectAccountWithSchedulerForCapabilityAttempt(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, requireCompact, previousResponseCanMove, useUpstreamTokenCost, platformOverride...)
		retry, finalErr := retrySelectionDependency(ctx, err)
		if !retry {
			err = finalErr
			return
		}
	}
}

func (s *OpenAIGatewayService) selectAccountWithSchedulerForCapabilityAttempt(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requireCompact bool,
	previousResponseCanMove bool,
	useUpstreamTokenCost bool,
	platformOverride ...string,
) (selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision, err error) {
	platform := PlatformOpenAI
	if len(platformOverride) > 0 {
		platform = platformOverride[0]
	}
	waitCtx, cancel := OpenAITurnWaitContext(ctx)
	defer cancel()
	if waitCtx.Err() != nil {
		return nil, OpenAIAccountScheduleDecision{}, context.Cause(waitCtx)
	}
	// BPS exhaustion and native recovery are phases of one selection. Keep its
	// storage allowance and absolute deadline across both, as for image fallback.
	standalone := selectionbudget.From(waitCtx) == nil
	waitCtx, externalBudget, cancelSelection := selectionbudget.Start(waitCtx)
	defer cancelSelection()
	defer func() {
		if standalone {
			finishSchedulingBudget(waitCtx, externalBudget, &err)
		}
		if errors.Is(context.Cause(waitCtx), ErrOpenAIWSFirstOutputBudgetExhausted) {
			if selection != nil && selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			selection, err = nil, ErrOpenAIWSFirstOutputBudgetExhausted
		}
	}()
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget != nil {
		budget.mu.Lock()
		budget.bpsRecovery.managed = true
		if !budget.bpsRecovery.native && budget.bpsRecovery.nativeAvailable && len(budget.bpsRecovery.rejected) > 0 && budget.limit > 2 && budget.attempts >= budget.limit-2 {
			budget.bpsRecovery.native = true
			budget.pinnedAccount = nil
			for id := range budget.bpsRecovery.rejected {
				delete(excludedIDs, id)
			}
		}
		budget.mu.Unlock()
	}
	selection, decision, err = s.selectAccountWithScheduler(waitCtx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, "", requireCompact, platform, previousResponseCanMove, useUpstreamTokenCost)
	if budget != nil && errors.Is(err, ErrNoAvailableAccounts) && waitCtx.Err() == nil &&
		externalBudget.DependencyError() == nil && !externalBudget.Snapshot().Exhausted && OpenAITurnRetryAllowed(ctx, 0, nil) {
		budget.mu.Lock()
		startNative := len(budget.bpsRecovery.rejected) > 0 && !budget.bpsRecovery.native
		if startNative {
			budget.bpsRecovery.native = true
			// Reset only the BPS-route rejections, once, in the caller's turn
			// exclusions. Later profit/admission vetoes and native failures must
			// remain excluded instead of being resurrected on every selection.
			for id := range budget.bpsRecovery.rejected {
				delete(excludedIDs, id)
			}
		}
		budget.mu.Unlock()
		if startNative {
			selection, decision, err = s.selectAccountWithScheduler(waitCtx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, "", requireCompact, platform, previousResponseCanMove, useUpstreamTokenCost)
		}
	}
	return selection, decision, err
}

func (s *OpenAIGatewayService) SelectAccountWithSchedulerForImages(
	ctx context.Context,
	groupID *int64,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredCapability OpenAIImagesCapability,
) (selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision, err error) {
	ctx = WithOpenAIImagesEndpoint(WithOpenAIImageGenerationIntent(ctx))
	ctx, budget, cancel := selectionbudget.Start(ctx)
	defer cancel()
	defer func() { finishSchedulingBudget(ctx, budget, &err) }()
	selection, decision, err = s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", requiredCapability, false, PlatformOpenAI, false, false)
	if err == nil && selection != nil && selection.Account != nil {
		return selection, decision, nil
	}
	// 如果要求 native 能力（如指定了模型）但没有可用的 APIKey 账号，回退到 basic（OAuth 账号）
	if requiredCapability == OpenAIImagesCapabilityNative && ctx.Err() == nil && !errors.Is(err, selectionbudget.ErrExhausted) {
		return s.selectAccountWithScheduler(ctx, groupID, "", sessionHash, requestedModel, excludedIDs, OpenAIUpstreamTransportHTTPSSE, "", OpenAIImagesCapabilityBasic, false, PlatformOpenAI, false, false)
	}
	return selection, decision, err
}

// selectAccountWithScheduler wraps selectAccountWithSchedulerOnce with a
// fail-open second pass for the proxy stream circuit (#5056): when the only
// reason no account is available is that every candidate sits behind a
// quarantined proxy, the quarantine must degrade to a preference instead of
// zeroing out capacity. The retry re-runs the exact same selection with the
// quarantine checks bypassed, so healthy proxies always win the first pass
// and quarantined ones only serve when nothing else can.
func (s *OpenAIGatewayService) selectAccountWithScheduler(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requiredImageCapability OpenAIImagesCapability,
	requireCompact bool,
	platform string,
	previousResponseCanMove bool,
	useUpstreamTokenCost bool,
) (selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision, err error) {
	standalone := selectionbudget.From(ctx) == nil
	ctx, externalBudget, cancel := selectionbudget.Start(ctx)
	defer cancel()
	defer func() {
		if standalone {
			finishSchedulingBudget(ctx, externalBudget, &err)
		}
	}()
	s.installSchedulingRecovery(ctx, requestedModel)
	return s.selectAccountWithSchedulerOnce(ctx, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, requiredImageCapability, requireCompact, platform, previousResponseCanMove, useUpstreamTokenCost)
}

type openAIGroupPrivacyRequirementContextKey struct{}

type openAIGroupPrivacyRequirement struct {
	groupID  int64
	required bool
}

func (s *OpenAIGatewayService) withOpenAIGroupPrivacyRequirement(ctx context.Context, groupID *int64) context.Context {
	return context.WithValue(ctx, openAIGroupPrivacyRequirementContextKey{}, openAIGroupPrivacyRequirement{
		groupID:  derefGroupID(groupID),
		required: s.loadOpenAIGroupRequiresPrivacySet(ctx, groupID),
	})
}

func (s *OpenAIGatewayService) openAIGroupRequiresPrivacySet(ctx context.Context, groupID *int64) bool {
	if cached, ok := ctx.Value(openAIGroupPrivacyRequirementContextKey{}).(openAIGroupPrivacyRequirement); ok && cached.groupID == derefGroupID(groupID) {
		return cached.required
	}
	return s.loadOpenAIGroupRequiresPrivacySet(ctx, groupID)
}

func (s *OpenAIGatewayService) loadOpenAIGroupRequiresPrivacySet(ctx context.Context, groupID *int64) bool {
	if s == nil || groupID == nil || s.schedulerSnapshot == nil {
		return false
	}
	// GetByID 会聚合账号计数；隐私开关只在分组行上。
	group, err := s.schedulerSnapshot.GetGroupByIDLite(ctx, *groupID)
	if err != nil {
		return true
	}
	return group != nil && group.RequirePrivacySet
}

func (s *OpenAIGatewayService) selectAccountWithSchedulerOnce(
	ctx context.Context,
	groupID *int64,
	previousResponseID string,
	sessionHash string,
	requestedModel string,
	excludedIDs map[int64]struct{},
	requiredTransport OpenAIUpstreamTransport,
	requiredCapability OpenAIEndpointCapability,
	requiredImageCapability OpenAIImagesCapability,
	requireCompact bool,
	platform string,
	previousResponseCanMove bool,
	useUpstreamTokenCost bool,
) (selection *AccountSelectionResult, decision OpenAIAccountScheduleDecision, selectionErr error) {
	started := time.Now()
	policy := "openai_unified_v2"
	defer func() {
		recordSchedulingSelection(ctx, SchedulingDecision{Policy: policy, Layer: decision.Layer, Candidates: decision.CandidateCount, TopK: decision.TopK}, started, selection, selectionErr)
	}()
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	ctx = s.withOpenAIGroupPrivacyRequirement(ctx, groupID)
	// 分组利润控制：唯一文本调度入口的防御性装门。handler 文本
	// 入口已在请求开始经 WithOpenAIRequestPricingContext 装门并固定 pricingAt，
	// 此处对同分组门直接复用（failover 重入阈值稳定），仅为不经 handler 装配的
	// 内部调用兜底。图片/视频调度不在利润门范围：requiredImageCapability 非空的
	// Images 调度不装门；其他使用 Responses 能力的文本请求（包括原生远程压缩）
	// 仍须装门。其余媒体路径通过 WithOpenAIProfitControlSuppressed 显式跳过。
	if requiredImageCapability == "" {
		ctx = s.withOpenAIProfitControlGate(ctx, groupID)
	}
	platform = NormalizeOpenAICompatiblePlatform(platform)
	if targetAccountID, ok := ctx.Value(codexDiagnosticTargetKey{}).(int64); ok && targetAccountID > 0 {
		accounts, err := s.listSchedulableAccounts(ctx, groupID, platform)
		if err != nil {
			return nil, OpenAIAccountScheduleDecision{}, err
		}
		found := false
		for _, account := range accounts {
			if account.ID == targetAccountID {
				found = true
				break
			}
		}
		if !found {
			return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
		}
		excludedIDs = cloneExcludedAccountIDs(excludedIDs)
		if excludedIDs == nil {
			excludedIDs = make(map[int64]struct{})
		}
		for _, account := range accounts {
			if account.ID != targetAccountID {
				excludedIDs[account.ID] = struct{}{}
			}
		}
		previousResponseID = ""
		sessionHash = ""
	}
	decision = OpenAIAccountScheduleDecision{}
	preserveGuardianParentBinding := preserveOpenAIGuardianParentBinding(ctx, sessionHash)
	guardianParentAccountID := int64(0)
	if strings.TrimSpace(previousResponseID) == "" {
		guardianParentAccountID = s.resolveOpenAIGuardianParentAccountID(ctx, groupID)
	}
	scheduler := s.getOpenAIAccountScheduler(ctx)
	policy = "openai_unified_v2"

	if s.checkChannelPricingRestriction(ctx, groupID, requestedModel) {
		slog.Warn("channel pricing restriction blocked request",
			"group_id", derefGroupID(groupID),
			"model", requestedModel)
		return nil, decision, fmt.Errorf("%w supporting model: %s (channel pricing restriction)", ErrNoAvailableAccounts, requestedModel)
	}

	var stickyAccountID int64
	if sessionHash != "" && s.cache != nil {
		if accountID, err := s.getStickySessionAccountID(ctx, groupID, sessionHash); err == nil && accountID > 0 {
			stickyAccountID = accountID
		}
	}
	if pinned := TurnRetryAccount(ctx); pinned != nil {
		stickyAccountID = pinned.ID
	}
	subscriptionPriority := s.isOpenAIAdvancedSchedulerSubscriptionPriorityEnabled(ctx)

	return scheduler.Select(ctx, OpenAIAccountScheduleRequest{
		GroupID:                 groupID,
		Platform:                platform,
		SessionHash:             sessionHash,
		StickyAccountID:         stickyAccountID,
		DisableStickyEscape:     TurnRetryAccount(ctx) != nil,
		GuardianParentAccountID: guardianParentAccountID,
		SubscriptionPriority:    subscriptionPriority,
		PreserveStickyBinding:   preserveGuardianParentBinding,
		RequirePrivacySet:       s.openAIGroupRequiresPrivacySet(ctx, groupID),
		PreviousResponseID:      previousResponseID,
		UseUpstreamTokenCost:    useUpstreamTokenCost,
		RequestedModel:          requestedModel,
		RequiredTransport:       requiredTransport,
		RequiredCapability:      requiredCapability,
		RequiredImageCapability: requiredImageCapability,
		RequireCompact:          requireCompact,
		ExcludedIDs:             excludedIDs,
	})
}

func accountSupportsOpenAICapabilities(account *Account, requiredCapability OpenAIEndpointCapability, requiredImageCapability OpenAIImagesCapability) bool {
	if account == nil {
		return false
	}
	return account.SupportsOpenAIEndpointCapability(requiredCapability) &&
		account.SupportsOpenAIImageCapability(requiredImageCapability)
}

func cloneExcludedAccountIDs(excludedIDs map[int64]struct{}) map[int64]struct{} {
	if len(excludedIDs) == 0 {
		return nil
	}
	cloned := make(map[int64]struct{}, len(excludedIDs))
	for id := range excludedIDs {
		cloned[id] = struct{}{}
	}
	return cloned
}

func (s *OpenAIGatewayService) isOpenAIAccountTransportCompatible(ctx context.Context, account *Account, requiredTransport OpenAIUpstreamTransport, models ...string) bool {
	if account != nil && account.IsOpenAICloudConfigured() {
		return requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE
	}
	if requiredTransport == OpenAIUpstreamTransportAny || requiredTransport == OpenAIUpstreamTransportHTTPSSE {
		return true
	}
	if s == nil || account == nil {
		return false
	}

	if len(models) > 0 && account.IsExcelBPSEnabledForModel(models[0]) && s.excelBPSGloballyEnabled(ctx) {
		return requiredTransport == OpenAIUpstreamTransportResponsesWebsocketV2Ingress
	}
	if requiredTransport == OpenAIUpstreamTransportResponsesWebsocketV2Ingress {
		if s.cfg == nil || !s.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled {
			return s.getOpenAIWSProtocolResolver().Resolve(account).Transport == OpenAIUpstreamTransportResponsesWebsocketV2
		}
		mode := account.ResolveOpenAIResponsesWebSocketV2Mode(s.cfg.Gateway.OpenAIWS.IngressModeDefault)
		switch mode {
		case OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge, OpenAIWSIngressModeShared, OpenAIWSIngressModeDedicated:
			return true
		default:
			return false
		}
	}
	return s.getOpenAIWSProtocolResolver().Resolve(account).Transport == requiredTransport
}

// ReportOpenAIAccountScheduleSuccess consumes an already resolved outbound model.
func (s *OpenAIGatewayService) ReportOpenAIAccountScheduleSuccess(ctx context.Context, account *Account, canonicalModel string, firstTokenMs *int) bool {
	return s.reportOpenAIAccountScheduleResult(ctx, account, canonicalModel, true, firstTokenMs, nil)
}

// A failure always carries a cause. A missing cause is a diagnostic defect, not
// evidence of an unhealthy account. An upstream deadline with a live client is
// distinct from expiration of the client's request context.
func (s *OpenAIGatewayService) ReportOpenAIAccountScheduleFailure(ctx context.Context, account *Account, canonicalModel string, cause error) bool {
	finishSchedulingRecovery(ctx, account, false, cause)
	reason := ""
	var inputErr *openAIResponsesInputError
	var failoverErr *UpstreamFailoverError
	switch {
	case cause == nil:
		reason = "missing_cause"
	case ctx.Err() != nil || errors.Is(cause, context.Canceled):
		reason = "client_canceled"
	case errors.Is(cause, ErrGatewayInstanceOverloaded):
		reason = "instance_overload"
	case IsTurnBudgetError(cause):
		reason = "turn_budget"
	case errors.Is(cause, selectionbudget.ErrExhausted):
		reason = "selection_budget"
	case errors.Is(cause, ErrOpenAIWSReplayUnavailable) || errors.As(cause, &inputErr):
		reason = "request"
	case errors.As(cause, &failoverErr):
		if failoverErr.Reason == "recovery_target_exhausted" || failoverErr.Stage == "selection" || strings.HasPrefix(string(failoverErr.Reason), "basispoints_credentials_") {
			reason = "recovery_policy"
		}
		if failoverErr.Scope == GatewayFailureScopeRequest {
			reason = "request"
		}
		if failoverErr.IsCredentialFailure() && failoverErr.Scope == GatewayFailureScopeProvider {
			reason = "provider_credentials"
		}
		if failoverErr.StatusCode == 429 {
			reason = "rate_limit"
		}
	}
	if reason != "" {
		recordSchedulingHealthRejection(reason)
		return false
	}
	return s.reportOpenAIAccountScheduleResult(ctx, account, canonicalModel, false, nil, cause)
}

func (s *OpenAIGatewayService) reportOpenAIAccountScheduleResult(ctx context.Context, account *Account, canonicalModel string, success bool, firstTokenMs *int, cause error) bool {
	finishSchedulingRecovery(ctx, account, success, cause)
	if s == nil || account == nil {
		return false
	}
	healthTripped := false
	if s.rateLimitService != nil {
		if success {
			s.rateLimitService.ObserveOpenAIAPIKeyHealthSuccess(ctx, account)
		} else {
			healthTripped = s.rateLimitService.ObserveOpenAIAPIKeyHealthFailure(ctx, account, cause)
		}
	}
	if success {
		s.openaiOAuth429RetryStartedAt.Delete(account.ID)
		s.clearOpenAIAccountModelTransientState(account.ID, normalizeOpenAIAccountModelTransientModel(canonicalModel))
	}
	if scheduler := s.getOpenAIAccountScheduler(ctx); scheduler != nil {
		scheduler.ReportResult(account.ID, success, firstTokenMs, strings.TrimSpace(canonicalModel), reportedSchedulingHealthCohort(ctx, account))
	}
	return healthTripped
}

// ObserveOpenAIAccountHealthFailure records failures that cannot reach the
// scheduler-result path, for example after semantic response bytes were sent.
func (s *OpenAIGatewayService) ObserveOpenAIAccountHealthFailure(ctx context.Context, account *Account, observedErr error) bool {
	finishSchedulingRecovery(ctx, account, false, observedErr)
	if s == nil || s.rateLimitService == nil || account == nil || observedErr == nil {
		return false
	}
	return s.rateLimitService.ObserveOpenAIAPIKeyHealthFailure(ctx, account, observedErr)
}

func (s *OpenAIGatewayService) RecordOpenAIAccountSwitch() {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return
	}
	scheduler.ReportSwitch()
}

func (s *OpenAIGatewayService) SnapshotOpenAIAccountSchedulerMetrics() OpenAIAccountSchedulerMetricsSnapshot {
	scheduler := s.getOpenAIAccountScheduler(context.Background())
	if scheduler == nil {
		return OpenAIAccountSchedulerMetricsSnapshot{}
	}
	return scheduler.SnapshotMetrics()
}

func (s *OpenAIGatewayService) openAIWSSessionStickyTTL() time.Duration {
	if s != nil && s.cfg != nil && s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds > 0 {
		return time.Duration(s.cfg.Gateway.OpenAIWS.StickySessionTTLSeconds) * time.Second
	}
	return openaiStickySessionTTL
}

func (s *OpenAIGatewayService) openAIWSSchedulerWeights() GatewayOpenAIWSSchedulerScoreWeightsView {
	return GatewayOpenAIWSSchedulerScoreWeightsView{Load: 1, Queue: 0.7, ErrorRate: 0.8, TTFT: 0.5, Reset: 0.2, QuotaHeadroom: 0.4}
}
func (s *OpenAIGatewayService) openAIWSSchedulerWeightsForRequest(ctx context.Context) GatewayOpenAIWSSchedulerScoreWeightsView {
	return s.openAIWSSchedulerWeights()
}

type GatewayOpenAIWSSchedulerScoreWeightsView struct {
	Load      float64
	Queue     float64
	ErrorRate float64
	TTFT      float64
	// Reset 仅在订阅优先偏好启用时参与层内评分。
	Reset         float64
	QuotaHeadroom float64
}

type OpenAIAccountSchedulerScoreSnapshot struct {
	BaseScore float64
}

func (s *RateLimitService) BuildOpenAIAccountSchedulerScoreSnapshot(
	ctx context.Context,
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	gateway := &OpenAIGatewayService{cfg: nil, rateLimitService: s}
	if s != nil {
		gateway.cfg = s.cfg
	}
	weights := gateway.openAIWSSchedulerWeightsForRequest(ctx)
	if !gateway.openAIAdvancedSchedulerRuntimeSettings(ctx).subscriptionPriorityEnabled {
		weights.Reset = 0
	}
	return buildOpenAIAccountSchedulerScoreSnapshot(
		accounts,
		loadMap,
		weights)
}

func BuildOpenAIAccountSchedulerScoreSnapshot(
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	gateway := &OpenAIGatewayService{}
	weights := gateway.openAIWSSchedulerWeights()
	weights.Reset = 0
	return buildOpenAIAccountSchedulerScoreSnapshot(accounts, loadMap, weights)
}

func buildOpenAIAccountSchedulerScoreSnapshot(
	accounts []*Account,
	loadMap map[int64]*AccountLoadInfo,
	weights GatewayOpenAIWSSchedulerScoreWeightsView,
) map[int64]OpenAIAccountSchedulerScoreSnapshot {
	if len(accounts) == 0 {
		return nil
	}
	candidates := make([]openAIAccountCandidateScore, 0, len(accounts))
	for _, account := range accounts {
		if account == nil {
			continue
		}
		loadInfo := loadMap[account.ID]
		if loadInfo == nil {
			loadInfo = &AccountLoadInfo{AccountID: account.ID}
		}
		candidates = append(candidates, openAIAccountCandidateScore{
			account:   account,
			loadInfo:  loadInfo,
			errorRate: 0,
			ttft:      0,
			hasTTFT:   false,
		})
	}
	if len(candidates) == 0 {
		return nil
	}

	maxWaiting := 1
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.loadInfo.WaitingCount > maxWaiting {
			maxWaiting = candidate.loadInfo.WaitingCount
		}
	}

	minResetRemaining, maxResetRemaining := 0.0, 0.0
	hasResetSample := false
	now := time.Now()
	if weights.Reset > 0 {
		for _, candidate := range candidates {
			end, ok := openAISchedulingResetWindowEnd(candidate.account, now)
			if !ok {
				continue
			}
			remaining := end.Sub(now).Seconds()
			if !hasResetSample {
				minResetRemaining, maxResetRemaining = remaining, remaining
				hasResetSample = true
				continue
			}
			if remaining < minResetRemaining {
				minResetRemaining = remaining
			}
			if remaining > maxResetRemaining {
				maxResetRemaining = remaining
			}
		}
	}

	result := make(map[int64]OpenAIAccountSchedulerScoreSnapshot, len(candidates))
	for _, candidate := range candidates {
		loadFactor := 1 - clamp01(float64(candidate.loadInfo.LoadRate)/100.0)
		queueFactor := 1 - clamp01(float64(candidate.loadInfo.WaitingCount)/float64(maxWaiting))
		errorFactor := 1.0
		ttftFactor := 0.5
		resetFactor := 0.0
		if weights.Reset > 0 && hasResetSample {
			if end, ok := openAISchedulingResetWindowEnd(candidate.account, now); ok {
				if maxResetRemaining > minResetRemaining {
					resetFactor = 1 - clamp01((end.Sub(now).Seconds()-minResetRemaining)/(maxResetRemaining-minResetRemaining))
				} else {
					resetFactor = 1
				}
			}
		}
		quotaHeadroomFactor := 0.0
		if weights.QuotaHeadroom > 0 {
			quotaHeadroomFactor = openAIQuotaHeadroomFactor(candidate.account, now)
		}
		baseScore := weights.Load*loadFactor +
			weights.Queue*queueFactor +
			weights.ErrorRate*errorFactor +
			weights.TTFT*ttftFactor +
			weights.Reset*resetFactor +
			weights.QuotaHeadroom*quotaHeadroomFactor
		score := OpenAIAccountSchedulerScoreSnapshot{BaseScore: baseScore}
		result[candidate.account.ID] = score
	}
	return result
}

func openAISchedulingRate(account *Account, now time.Time, oauthSchedulingRateMultiplier *float64) (float64, bool) {
	if account == nil || (!account.IsOpenAIApiKey() && !account.IsOpenAIOAuthLike()) {
		return 0, false
	}
	if account.IsOpenAIOAuthLike() {
		if rate := oauthSchedulingRateMultiplier; rate != nil && *rate >= 0 && !math.IsNaN(*rate) && !math.IsInf(*rate, 0) {
			return *rate, true
		}
	} else if rate, ok := openAIFreshUpstreamBillingRate(account, now); ok {
		return rate, true
	}
	if account.RateMultiplier != nil && *account.RateMultiplier < 0 {
		return 0, false
	}
	rate := account.BillingRateMultiplier()
	return rate, !math.IsNaN(rate) && !math.IsInf(rate, 0)
}

func openAIFreshUpstreamBillingRate(account *Account, now time.Time) (float64, bool) {
	if !isUpstreamBillingProbeAccount(account) {
		return 0, false
	}
	snapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	if snapshot == nil || (snapshot.Status != UpstreamBillingProbeStatusOK && snapshot.Status != UpstreamBillingProbeStatusFailed) ||
		snapshot.ReceivedAt == nil || snapshot.ReceivedAt.IsZero() {
		return 0, false
	}
	receivedAt := *snapshot.ReceivedAt
	freshUntil := snapshot.FreshUntil
	if freshUntil == nil && snapshot.Status == UpstreamBillingProbeStatusOK {
		interval := snapshot.NextProbeAt.Sub(receivedAt)
		if interval > 0 {
			freshUntil = probeTimePtr(receivedAt.Add(2 * interval))
		}
	}
	if freshUntil == nil || !freshUntil.After(receivedAt) || now.Before(receivedAt) || now.After(*freshUntil) {
		return 0, false
	}
	return upstreamBillingRateAt(snapshot.Data, now)
}

func openAIQuotaHeadroomFactor(account *Account, now time.Time) float64 {
	if account == nil || len(account.Extra) == 0 || openAIQuotaHeadroomSnapshotStale(account.Extra, now) {
		return openAIQuotaHeadroomNeutralFactor
	}
	window5h, window7d := openAICanonicalQuotaWindows(account.Extra, now)
	if !window7d.hasUsed || window7d.reset {
		return openAIQuotaHeadroomNeutralFactor
	}

	factor := 1 - clamp01(window7d.usedPercent/100)
	if window5h.hasUsed && !window5h.reset {
		remaining := 1 - clamp01(window5h.usedPercent/100)
		if remaining < openAIQuotaHeadroomSecondaryLowRemain {
			factor *= openAIQuotaHeadroomNeutralFactor
		}
	}
	return factor
}

type openAICanonicalQuotaWindow struct {
	usedPercent float64
	hasUsed     bool
	reset       bool
}

// 规范字段优先；历史原始字段复用写入端 Normalize 的窗口分类，不能固定把 primary 当作 7d。
func openAICanonicalQuotaWindows(extra map[string]any, now time.Time) (window5h, window7d openAICanonicalQuotaWindow) {
	if used, ok := resolveAccountExtraNumber(extra, "codex_5h_used_percent"); ok {
		window5h = openAICanonicalQuotaWindow{usedPercent: used, hasUsed: true, reset: openAIQuotaWindowReset(extra, "5h", now)}
	}
	if used, ok := resolveAccountExtraNumber(extra, "codex_7d_used_percent"); ok {
		window7d = openAICanonicalQuotaWindow{usedPercent: used, hasUsed: true, reset: openAIQuotaWindowReset(extra, "7d", now)}
	}
	if window5h.hasUsed && window7d.hasUsed {
		return window5h, window7d
	}

	snapshot := &OpenAICodexUsageSnapshot{}
	if used, ok := resolveAccountExtraNumber(extra, "codex_primary_used_percent"); ok {
		snapshot.PrimaryUsedPercent = &used
	}
	if used, ok := resolveAccountExtraNumber(extra, "codex_secondary_used_percent"); ok {
		snapshot.SecondaryUsedPercent = &used
	}
	if minutes := parseExtraInt(extra["codex_primary_window_minutes"]); minutes > 0 {
		snapshot.PrimaryWindowMinutes = &minutes
	}
	if minutes := parseExtraInt(extra["codex_secondary_window_minutes"]); minutes > 0 {
		snapshot.SecondaryWindowMinutes = &minutes
	}
	normalized := snapshot.Normalize()
	if normalized == nil {
		return window5h, window7d
	}
	fromRaw := func(used *float64) openAICanonicalQuotaWindow {
		if used == nil {
			return openAICanonicalQuotaWindow{}
		}
		// Normalize 保留原始字段指针，据此配对同一个窗口的用量和重置时间。
		window := "secondary"
		if used == snapshot.PrimaryUsedPercent {
			window = "primary"
		}
		return openAICanonicalQuotaWindow{usedPercent: *used, hasUsed: true, reset: openAIQuotaWindowReset(extra, window, now)}
	}
	if !window5h.hasUsed {
		window5h = fromRaw(normalized.Used5hPercent)
	}
	if !window7d.hasUsed {
		window7d = fromRaw(normalized.Used7dPercent)
	}
	return window5h, window7d
}

func openAISchedulingResetWindowEnd(account *Account, now time.Time) (time.Time, bool) {
	if account == nil {
		return time.Time{}, false
	}
	if end, ok := openAICodexWindowResetAt(account.Extra, "5h"); ok && now.Before(end) {
		return end, true
	}
	if end := account.SessionWindowEnd; end != nil && now.Before(*end) {
		return *end, true
	}
	return time.Time{}, false
}

func openAIQuotaHeadroomSnapshotStale(extra map[string]any, now time.Time) bool {
	updatedRaw, ok := extra["codex_usage_updated_at"]
	if !ok {
		return true
	}
	updatedAt, err := parseTime(fmt.Sprint(updatedRaw))
	if err != nil {
		return true
	}
	return now.Sub(updatedAt) >= openAIQuotaHeadroomSnapshotStaleAfter
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func calcLoadSkewByMoments(sum float64, sumSquares float64, count int) float64 {
	if count <= 1 {
		return 0
	}
	mean := sum / float64(count)
	variance := sumSquares/float64(count) - mean*mean
	if variance < 0 {
		variance = 0
	}
	return math.Sqrt(variance)
}
