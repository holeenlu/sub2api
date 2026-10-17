package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
		require.Equal(t, 56.0, resp.SevenDayOverageIncluded.Utilization)
		require.Equal(t, "2026-07-08T03:00:00Z", resp.SevenDayOverageIncluded.ResetsAt)
	})

	t.Run("absent", func(t *testing.T) {
		raw := `{"five_hour": {"utilization": 12.0, "resets_at": "2026-07-03T10:00:00Z"}}`
		var resp ClaudeUsageResponse
		require.NoError(t, json.Unmarshal([]byte(raw), &resp))
		require.Zero(t, resp.SevenDayOverageIncluded.Utilization)
		require.Empty(t, resp.SevenDayOverageIncluded.ResetsAt)
	})
}

func TestBuildUsageInfo_SevenDayFable(t *testing.T) {
	svc := &AccountUsageService{}
	now := time.Now()

	resetAt := now.Add(72 * time.Hour).UTC().Truncate(time.Second)
	var resp ClaudeUsageResponse
	resp.FiveHour.Utilization = 10
	resp.SevenDayOverageIncluded = ClaudeUsageWindow{
		Utilization: 88,
		ResetsAt:    resetAt.Format(time.RFC3339),
	}

	info := svc.buildUsageInfo(&resp, &now)
	require.NotNil(t, info.SevenDayFable)
	require.Equal(t, 88.0, info.SevenDayFable.Utilization)
	require.NotNil(t, info.SevenDayFable.ResetsAt)
	require.True(t, info.SevenDayFable.ResetsAt.Equal(resetAt))
	require.Greater(t, info.SevenDayFable.RemainingSeconds, 0)

	// 无 Fable 数据时不应创建窗口
	var empty ClaudeUsageResponse
	empty.FiveHour.Utilization = 10
	info = svc.buildUsageInfo(&empty, &now)
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

type claudeSnapshotFetcher struct {
	ClaudeUsageFetcher
	response *ClaudeUsageResponse
	calls    int
}

func (f *claudeSnapshotFetcher) FetchUsageWithOptions(context.Context, *ClaudeUsageFetchOptions) (*ClaudeUsageResponse, error) {
	f.calls++
	return f.response, nil
}

func TestClaudeUsageSnapshot_RefreshAndReload(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	reset5h, reset7d := now.Add(time.Hour), now.Add(6*24*time.Hour)
	resp := &ClaudeUsageResponse{}
	resp.FiveHour.Utilization, resp.FiveHour.ResetsAt = 65, reset5h.Format(time.RFC3339)
	resp.SevenDay.Utilization, resp.SevenDay.ResetsAt = 74, reset7d.Format(time.RFC3339)
	resp.SevenDayOverageIncluded = ClaudeUsageWindow{Utilization: 42, ResetsAt: reset7d.Format(time.RFC3339)}
	fetcher := &claudeSnapshotFetcher{response: resp}
	repo := &claudeUsageSnapshotRepo{account: Account{ID: 12, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-token"}}}
	svc := &AccountUsageService{accountRepo: repo, usageLogRepo: &usageBatchLogRepoStub{}, cache: NewUsageCache(), usageFetcher: fetcher}

	_, err := svc.GetUsage(t.Context(), 12, true)
	require.NoError(t, err)
	require.Equal(t, 1, repo.writes)

	// A new service with an empty memory cache must recover all three windows.
	reloaded := &AccountUsageService{accountRepo: repo, usageLogRepo: &usageBatchLogRepoStub{}, cache: NewUsageCache()}
	usage, err := reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.InDelta(t, 65, usage.FiveHour.Utilization, 1e-9)
	require.InDelta(t, 74, usage.SevenDay.Utilization, 1e-9)
	require.InDelta(t, 42, usage.SevenDayFable.Utilization, 1e-9)
	require.True(t, reset5h.Equal(*usage.FiveHour.ResetsAt))
	require.True(t, reset7d.Equal(*usage.SevenDay.ResetsAt))
	require.True(t, reset7d.Equal(*usage.SevenDayFable.ResetsAt))

	// A newer header sample must not be overwritten by replaying the API cache.
	require.NoError(t, repo.UpdateExtra(t.Context(), 12, map[string]any{"passive_usage_7d_utilization": 0.75}))
	before := repo.writes
	_, err = svc.GetUsage(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, before, repo.writes, "cache hits must not rewrite persisted observations")
	require.Equal(t, 0.75, repo.account.Extra["passive_usage_7d_utilization"])

	// Explicit query bypasses a successful cache; missing Fable keeps its snapshot.
	updated := *resp
	updated.SevenDay.Utilization = 76
	updated.SevenDayOverageIncluded = ClaudeUsageWindow{}
	fetcher.response = &updated
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
	svc.cache.apiCache.Store(int64(12), &apiUsageCache{err: upstreamErr, timestamp: time.Now()})
	_, err = svc.GetUsage(t.Context(), 12, true)
	require.ErrorIs(t, err, upstreamErr)
	require.Equal(t, 2, fetcher.calls)
	usage, err = reloaded.GetPassiveUsage(t.Context(), 12)
	require.NoError(t, err)
	require.InDelta(t, 76, usage.SevenDay.Utilization, 1e-9)
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
