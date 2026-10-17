package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestClaudeUsageResponse_FableWindowDecoding(t *testing.T) {
	t.Run("seven_day_overage_included", func(t *testing.T) {
		raw := `{
  "five_hour": {"utilization": 12.0, "resets_at": "2026-07-03T10:00:00Z"},
  "seven_day": {"utilization": 34.0, "resets_at": "2026-07-08T00:00:00Z"},
  "seven_day_overage_included": {"utilization": 56.0, "resets_at": "2026-07-08T03:00:00Z"}
}`
		var resp ClaudeUsageResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &resp))
		require.JSONEq(t, "{\"utilization\":56,\"resets_at\":\"2026-07-08T03:00:00Z\"}", string(resp.RawFields["seven_day_overage_included"]))
	})

	t.Run("absent", func(t *testing.T) {
		raw := `{"five_hour": {"utilization": 12.0, "resets_at": "2026-07-03T10:00:00Z"}}`
		var resp ClaudeUsageResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &resp))
		require.NotContains(t, resp.RawFields, "seven_day_overage_included")
	})
}

func TestBuildUsageInfo_SevenDayFable(t *testing.T) {
	svc := &AccountUsageService{}
	now := time.Now()

	resetAt := now.Add(72 * time.Hour).UTC().Truncate(time.Second)
	resp := claudeUsageFixture(t, map[string]any{
		"five_hour":                  map[string]any{"utilization": 10},
		"seven_day_overage_included": map[string]any{"utilization": 88, "resets_at": resetAt.Format(time.RFC3339)},
	})
	info := svc.buildUsageInfo(resp, &now)
	require.NotNil(t, info.SevenDayFable)
	require.Equal(t, 88.0, info.SevenDayFable.Utilization)
	require.NotNil(t, info.SevenDayFable.ResetsAt)
	require.True(t, info.SevenDayFable.ResetsAt.Equal(resetAt))
	require.Greater(t, info.SevenDayFable.RemainingSeconds, 0)

	// 无 Fable 数据时不应创建窗口
	empty := claudeUsageFixture(t, map[string]any{"five_hour": map[string]any{"utilization": 10}})
	info = svc.buildUsageInfo(empty, &now)
	require.Nil(t, info.SevenDayFable)
}

func TestBuildPassiveUsageWindow(t *testing.T) {
	future := time.Now().Add(48 * time.Hour).Unix()

	t.Run("utilization and reset", func(t *testing.T) {
		window := buildPassiveUsageWindow(map[string]any{
			"passive_usage_7d_oi_utilization": 0.87,
			"passive_usage_7d_oi_reset":       float64(future),
		}, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset")
		require.NotNil(t, window)
		require.InDelta(t, 87.0, window.Utilization, 1e-9)
		require.NotNil(t, window.ResetsAt)
		require.Equal(t, future, window.ResetsAt.Unix())
		require.Greater(t, window.RemainingSeconds, 0)
	})

	t.Run("no data returns nil", func(t *testing.T) {
		require.Nil(t, buildPassiveUsageWindow(nil, "u", "r"))
		require.Nil(t, buildPassiveUsageWindow(map[string]any{}, "u", "r"))
	})

	t.Run("expired reset clamps remaining to zero", func(t *testing.T) {
		past := time.Now().Add(-time.Hour).Unix()
		window := buildPassiveUsageWindow(map[string]any{
			"u": 0.5,
			"r": float64(past),
		}, "u", "r")
		require.NotNil(t, window)
		require.Equal(t, 0, window.RemainingSeconds)
		require.Zero(t, window.Utilization)
		require.Nil(t, window.ResetsAt)
	})

	t.Run("utilization only", func(t *testing.T) {
		window := buildPassiveUsageWindow(map[string]any{"u": 0.25}, "u", "r")
		require.NotNil(t, window)
		require.InDelta(t, 25.0, window.Utilization, 1e-9)
		require.Nil(t, window.ResetsAt)
	})
}

type claudeUsageSnapshotRepo struct {
	AccountRepository
	account Account
	writes  int
}

func (r *claudeUsageSnapshotRepo) GetByID(context.Context, int64) (*Account, error) {
	// Simulate a fresh DB load, including JSON numbers decoded as float64.
	raw, err := json.Marshal(r.account)
	if err != nil {
		return nil, err
	}
	var account Account
	err = json.Unmarshal(raw, &account)
	return &account, err
}

func (r *claudeUsageSnapshotRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	mergeAccountExtra(&r.account, updates)
	r.writes++
	return nil
}

func (r *claudeUsageSnapshotRepo) UpdateSessionWindowEnd(_ context.Context, _ int64, end time.Time) error {
	r.account.SessionWindowEnd = &end
	return nil
}

// Fixtures exercise the production JSON presence path, with no secondary parser.
func claudeUsageFixture(t *testing.T, values map[string]any) *ClaudeUsageResponse {
	t.Helper()
	raw, err := json.Marshal(values)
	require.NoError(t, err)
	var resp ClaudeUsageResponse
	require.NoError(t, json.Unmarshal(raw, &resp))
	return &resp
}

type claudeSnapshotFetcher struct {
	ClaudeUsageFetcher
	response *ClaudeUsageResponse
	calls    int
}

type claudeWindowStatsRepo struct {
	UsageLogRepository
	starts []time.Time
	query  func(time.Time) (*usagestats.AccountStats, error)
}

func (r *claudeWindowStatsRepo) GetAccountWindowStats(_ context.Context, _ int64, start time.Time) (*usagestats.AccountStats, error) {
	r.starts = append(r.starts, start)
	return r.query(start)
}

func (r *claudeWindowStatsRepo) GetAccountFableWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	return nil, errors.New("Fable statistics fixture not configured")
}

func (f *claudeSnapshotFetcher) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	f.calls++
	return f.response, nil
}

func TestClaudeUsageSnapshot_RefreshAndReload(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reset5h, reset7d := now.Add(time.Hour), now.Add(6*24*time.Hour)
	resp := claudeUsageFixture(t, map[string]any{
		"five_hour":                  map[string]any{"utilization": 65, "resets_at": reset5h.Format(time.RFC3339)},
		"seven_day":                  map[string]any{"utilization": 74, "resets_at": reset7d.Format(time.RFC3339)},
		"seven_day_overage_included": map[string]any{"utilization": 42, "resets_at": reset7d.Format(time.RFC3339)},
	})
	fetcher := &claudeSnapshotFetcher{response: resp}
	repo := &claudeUsageSnapshotRepo{account: Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-token", "scope": "user:profile user:inference"}}}
	fiveHourStats := &usagestats.AccountStats{Requests: 2, Tokens: 100, Cost: 3, StandardCost: 4, UserCost: 2}
	sevenDayStats := &usagestats.AccountStats{Requests: 10, Tokens: 500, Cost: 12, StandardCost: 18, UserCost: 6}
	logs := &claudeWindowStatsRepo{query: func(start time.Time) (*usagestats.AccountStats, error) {
		if start.Equal(reset5h.Add(-5 * time.Hour)) {
			return fiveHourStats, nil
		}
		require.True(t, start.Equal(reset7d.Add(-7*24*time.Hour)), "unexpected window start: %v", start)
		return sevenDayStats, nil
	}}
	svc := &AccountUsageService{accountRepo: repo, usageLogRepo: logs, cache: NewUsageCache(), usageFetcher: fetcher}

	active, err := svc.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, windowStatsFromAccountStats(fiveHourStats), active.FiveHour.WindowStats)
	require.Equal(t, windowStatsFromAccountStats(sevenDayStats), active.SevenDay.WindowStats)

	// A new service must recover window times and costs from persisted records.
	reloaded := &AccountUsageService{accountRepo: repo, usageLogRepo: logs, cache: NewUsageCache()}
	usage, err := reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.InDelta(t, 65, usage.FiveHour.Utilization, 1e-9)
	require.InDelta(t, 74, usage.SevenDay.Utilization, 1e-9)
	require.InDelta(t, 42, usage.SevenDayFable.Utilization, 1e-9)
	require.True(t, reset5h.Equal(*usage.FiveHour.ResetsAt))
	require.True(t, reset7d.Equal(*usage.SevenDay.ResetsAt))
	require.True(t, reset7d.Equal(*usage.SevenDayFable.ResetsAt))
	require.Equal(t, active.FiveHour.WindowStats, usage.FiveHour.WindowStats)
	require.Equal(t, active.SevenDay.WindowStats, usage.SevenDay.WindowStats)
	require.Len(t, logs.starts, 4, "empty cache must reload both windows from storage")

	// Setup Token manual queries use the same persisted windows without OAuth scope.
	repo.account.Type = AccountTypeSetupToken
	setupUsage, err := reloaded.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.Equal(t, usage.SevenDay, setupUsage.SevenDay)
	require.Equal(t, usage.FiveHour.WindowStats, setupUsage.FiveHour.WindowStats)
	require.Len(t, logs.starts, 4, "unchanged windows reuse their separate stats caches")
	repo.account.Type = AccountTypeOAuth

	// A newer header sample must not be overwritten by replaying the API cache.
	require.NoError(t, repo.UpdateExtra(t.Context(), 12, map[string]any{"passive_usage_7d_utilization": 0.75}))
	before := repo.writes
	_, err = svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, before, repo.writes, "cache hits must not rewrite persisted observations")
	require.Equal(t, 0.75, repo.account.Extra["passive_usage_7d_utilization"])

	// Explicit query bypasses a successful cache; missing Fable keeps its snapshot.
	fetcher.response = claudeUsageFixture(t, map[string]any{
		"five_hour": map[string]any{"utilization": 65, "resets_at": reset5h.Format(time.RFC3339)},
		"seven_day": map[string]any{"utilization": 76, "resets_at": reset7d.Format(time.RFC3339)},
	})
	usage, err = svc.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.Equal(t, 2, fetcher.calls)
	require.Equal(t, 76.0, usage.SevenDay.Utilization)
	require.InDelta(t, 42, usage.SevenDayFable.Utilization, 1e-9)
	usage, err = reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.InDelta(t, 76, usage.SevenDay.Utilization, 1e-9)
	require.InDelta(t, 42, usage.SevenDayFable.Utilization, 1e-9)

	// Forced refresh still respects the short error cache to avoid retry storms.
	upstreamErr := errors.New("upstream rate limited")
	svc.cache.apiCache.Store(int64(12), &apiUsageCache{identity: anthropicCredentialIdentity(&repo.account), err: upstreamErr, timestamp: time.Now()})
	fallback, err := svc.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.NotEmpty(t, fallback.Error)
	require.NotNil(t, fallback.SevenDay)
	require.Equal(t, 2, fetcher.calls)
	usage, err = reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.InDelta(t, 76, usage.SevenDay.Utilization, 1e-9)
}

func TestAddWindowStats_CacheTracksWindowBoundaries(t *testing.T) {
	reset5h := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	reset7d := reset5h.Add(4 * 24 * time.Hour)
	logs := &claudeWindowStatsRepo{}
	logs.query = func(time.Time) (*usagestats.AccountStats, error) {
		return &usagestats.AccountStats{Requests: int64(len(logs.starts))}, nil
	}
	svc := &AccountUsageService{usageLogRepo: logs, cache: NewUsageCache()}
	account := &Account{ID: 42}
	usage := &UsageInfo{FiveHour: &UsageProgress{ResetsAt: &reset5h}, SevenDay: &UsageProgress{ResetsAt: &reset7d}}
	svc.addWindowStats(t.Context(), account, usage)
	require.Equal(t, []time.Time{reset5h.Add(-5 * time.Hour), reset7d.Add(-7 * 24 * time.Hour)}, logs.starts)
	require.EqualValues(t, 1, usage.FiveHour.WindowStats.Requests)
	require.EqualValues(t, 2, usage.SevenDay.WindowStats.Requests)
	svc.addWindowStats(t.Context(), account, usage)
	require.Len(t, logs.starts, 2)

	// Either upstream reset changes before the TTL: invalidate only that window.
	reset5h = reset5h.Add(time.Hour)
	svc.addWindowStats(t.Context(), account, usage)
	require.Len(t, logs.starts, 3)
	require.Equal(t, reset5h.Add(-5*time.Hour), logs.starts[2])
	require.EqualValues(t, 3, usage.FiveHour.WindowStats.Requests)
	require.EqualValues(t, 2, usage.SevenDay.WindowStats.Requests)
	reset7d = reset7d.Add(24 * time.Hour)
	svc.addWindowStats(t.Context(), account, usage)
	require.Len(t, logs.starts, 4)
	require.Equal(t, reset7d.Add(-7*24*time.Hour), logs.starts[3])
	require.EqualValues(t, 3, usage.FiveHour.WindowStats.Requests)
	require.EqualValues(t, 4, usage.SevenDay.WindowStats.Requests)

	// Cache size stays bounded across resets; TTL expiration refreshes costs.
	entries := 0
	svc.cache.windowStatsCache.Range(func(_, value any) bool {
		entries++
		value.(*windowStatsCache).timestamp = time.Now().Add(-2 * windowStatsCacheTTL)
		return true
	})
	require.Equal(t, 2, entries)
	svc.addWindowStats(t.Context(), account, usage)
	require.Len(t, logs.starts, 6)
}

func TestAddWindowStats_IndependentFailuresAndRollingFallback(t *testing.T) {
	reset5h := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	failFiveHour := true
	logs := &claudeWindowStatsRepo{query: func(start time.Time) (*usagestats.AccountStats, error) {
		if failFiveHour && start.Equal(reset5h.Add(-5*time.Hour)) {
			return nil, errors.New("stats temporarily unavailable")
		}
		return &usagestats.AccountStats{Requests: 5, Cost: 12}, nil
	}}
	svc := &AccountUsageService{usageLogRepo: logs, cache: NewUsageCache()}
	account := &Account{ID: 42}
	usage := &UsageInfo{FiveHour: &UsageProgress{ResetsAt: &reset5h}, SevenDay: &UsageProgress{Utilization: 40}}
	svc.addWindowStats(t.Context(), account, usage)
	require.Nil(t, usage.FiveHour.WindowStats)
	require.Equal(t, 12.0, usage.SevenDay.WindowStats.Cost)
	require.WithinDuration(t, time.Now().Add(-7*24*time.Hour), logs.starts[1], time.Second)

	// A failed query is retried; a rolling window still uses the one-minute cache.
	failFiveHour = false
	svc.addWindowStats(t.Context(), account, usage)
	require.NotNil(t, usage.FiveHour.WindowStats)
	require.Len(t, logs.starts, 3)
	usage.FiveHour = nil
	svc.addWindowStats(t.Context(), account, usage)
	require.Len(t, logs.starts, 3)
	require.NotNil(t, usage.SevenDay.WindowStats)
}

func TestSyncActiveToPassive_WritesFableExtras(t *testing.T) {
	repo := &accountUsageCodexProbeRepo{updateExtraCh: make(chan map[string]any, 1)}
	svc := &AccountUsageService{accountRepo: repo}

	resetAt := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	usage := &UsageInfo{
		SevenDayFable: &UsageProgress{
			Utilization: 87,
			ResetsAt:    &resetAt,
		},
	}

	now := time.Now()
	usage.UpdatedAt = &now
	usage.Anthropic = &AnthropicUsageDetails{Observations: map[string]UsageObservation{}}
	putUsageObservation(usage.Anthropic.Observations, "seven_day_fable.utilization", 87.0, "upstream_api", &now)
	putUsageObservation(usage.Anthropic.Observations, "seven_day_fable.resets_at", resetAt.Format(time.RFC3339), "upstream_api", &now)
	svc.syncActiveToPassive(t.Context(), 1, usage)

	select {
	case updates := <-repo.updateExtraCh:
		require.InDelta(t, 0.87, updates["passive_usage_7d_oi_utilization"], 1e-9)
		require.Equal(t, resetAt.Unix(), updates["passive_usage_7d_oi_reset"])
		require.Contains(t, updates, "passive_usage_sampled_at")
	default:
		t.Fatal("expected UpdateExtra to be called with fable extras")
	}
}

type claudeFableWindowStatsRepo struct {
	*claudeWindowStatsRepo
	fableCalls int
	fableStart time.Time
}

func (r *claudeFableWindowStatsRepo) GetAccountFableWindowStats(_ context.Context, _ int64, start time.Time) (*usagestats.AccountStats, error) {
	r.fableCalls++
	r.fableStart = start
	return &usagestats.AccountStats{Requests: 3, Tokens: 90, Cost: 8, UserCost: 4}, nil
}

func TestAnthropicWindowStats_FableIsIndependent(t *testing.T) {
	reset := time.Now().Add(72 * time.Hour)
	logs := &claudeFableWindowStatsRepo{claudeWindowStatsRepo: &claudeWindowStatsRepo{query: func(time.Time) (*usagestats.AccountStats, error) {
		return &usagestats.AccountStats{Requests: 20, Tokens: 900, Cost: 40}, nil
	}}}
	svc := &AccountUsageService{usageLogRepo: logs, cache: NewUsageCache()}
	info := &UsageInfo{SevenDay: &UsageProgress{Utilization: 80, ResetsAt: &reset}, SevenDayFable: &UsageProgress{Utilization: 40, ResetsAt: &reset}, Anthropic: &AnthropicUsageDetails{Observations: map[string]UsageObservation{}}}
	svc.addWindowStats(t.Context(), &Account{ID: 12}, info)
	require.Equal(t, 40.0, info.SevenDay.WindowStats.Cost)
	require.Equal(t, 8.0, info.SevenDayFable.WindowStats.Cost)
	require.True(t, logs.fableStart.Equal(reset.Add(-7*24*time.Hour)))
	require.Equal(t, "local_statistics", info.Anthropic.Observations["seven_day_fable.window_stats"].Source)
	svc.addWindowStats(t.Context(), &Account{ID: 12}, info)
	require.Equal(t, 1, logs.fableCalls)
	require.Len(t, logs.starts, 1)
	moved := reset.Add(time.Hour)
	info.SevenDayFable.ResetsAt = &moved
	svc.addWindowStats(t.Context(), &Account{ID: 12}, info)
	require.Equal(t, 2, logs.fableCalls)
	require.Len(t, logs.starts, 1, "Fable rollover must not invalidate all-model cache")
}
