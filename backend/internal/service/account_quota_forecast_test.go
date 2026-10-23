package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaForecastRepoStub struct {
	UsageLogRepository
	sample  QuotaForecastSample
	calls   int
	scope   string
	through time.Time
}

func (r *quotaForecastRepoStub) GetAccountQuotaForecastSample(_ context.Context, _ int64, _, through time.Time, scope string) (*QuotaForecastSample, error) {
	r.calls++
	r.scope = scope
	r.through = through
	return &r.sample, nil
}

func TestQuotaForecastRequiresMatchingWindowEvidence(t *testing.T) {
	now := time.Now()
	observed := now.Add(-time.Minute)
	reset := now.Add(24 * time.Hour)
	for _, tc := range []struct {
		name             string
		percent          float64
		joined, sampleAt time.Time
		missingCost      int64
		want             string
	}{
		{"ready", 25, now.Add(-8 * 24 * time.Hour), observed, 0, "ready"},
		{"low_sample", 6, now.Add(-8 * 24 * time.Hour), observed, 0, "ready"},
		{"insufficient", 4, now.Add(-8 * 24 * time.Hour), observed, 0, "insufficient_sample"},
		{"partial_window", 25, now, observed, 0, "partial_window"},
		{"stale", 25, now.Add(-8 * 24 * time.Hour), now.Add(-time.Hour), 0, "stale_observation"},
		{"missing_price", 25, now.Add(-8 * 24 * time.Hour), observed, 1, "ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &quotaForecastRepoStub{sample: QuotaForecastSample{Requests: 8, Tokens: 1000, Cost: 2, MissingCost: tc.missingCost}}
			svc := &AccountUsageService{usageLogRepo: repo}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, CreatedAt: tc.joined, Extra: map[string]any{"codex_usage_updated_at": tc.sampleAt.Format(time.RFC3339Nano)}}
			progress := &UsageProgress{Utilization: tc.percent, ResetsAt: &reset}
			svc.addQuotaCapacityForecast(context.Background(), account, &UsageInfo{}, "seven_day", progress)
			forecast := progress.CapacityForecast
			require.Equal(t, tc.want, forecast.Status)
			if tc.want != "ready" {
				require.Zero(t, repo.calls)
				require.Nil(t, forecast.EstimatedCost)
				return
			}
			require.Equal(t, PlatformOpenAI, repo.scope)
			require.True(t, tc.sampleAt.Equal(repo.through))
			require.Equal(t, tc.percent < 10, forecast.LowSample)
			require.NotNil(t, forecast.EstimatedTokens)
			if tc.missingCost > 0 {
				require.Nil(t, forecast.EstimatedCost)
			} else {
				require.InDelta(t, 200/tc.percent, *forecast.EstimatedCost, 0.000001)
			}
			require.Greater(t, *forecast.MonthlyEquivalentTokens, *forecast.EstimatedTokens)
		})
	}
}

func TestQuotaForecastFableUsesOwnObservation(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	repo := &quotaForecastRepoStub{sample: QuotaForecastSample{Requests: 10, Tokens: 900, Cost: 3}}
	svc := &AccountUsageService{usageLogRepo: repo}
	account := &Account{ID: 1, Platform: PlatformAnthropic, CreatedAt: now.Add(-8 * 24 * time.Hour)}
	usage := &UsageInfo{Anthropic: &AnthropicUsageDetails{Observations: map[string]UsageObservation{"seven_day_fable.utilization": {Value: 20.0, SampledAt: &now}}}}
	progress := &UsageProgress{Utilization: 20, ResetsAt: &reset}
	svc.addQuotaCapacityForecast(context.Background(), account, usage, "seven_day_fable", progress)
	require.Equal(t, "anthropic_fable", repo.scope)
	require.Equal(t, "ready", progress.CapacityForecast.Status)
	svc.addQuotaCapacityForecast(context.Background(), account, usage, "seven_day", progress)
	require.Equal(t, "stale_observation", progress.CapacityForecast.Status, "不得用 Fable 观测代替共享周窗口")
}

func TestQuotaForecastRejectsKnownResetInsideWindow(t *testing.T) {
	now := time.Now()
	reset := now.Add(time.Hour)
	repo := &quotaForecastRepoStub{sample: QuotaForecastSample{Requests: 10, Tokens: 900, Cost: 3}}
	svc := &AccountUsageService{usageLogRepo: repo}
	account := &Account{ID: 1, Platform: PlatformOpenAI, CreatedAt: now.Add(-8 * 24 * time.Hour), Extra: map[string]any{"codex_usage_updated_at": now.Format(time.RFC3339Nano), "codex_quota_reset_at": now.Add(-time.Minute).Format(time.RFC3339Nano)}}
	progress := &UsageProgress{Utilization: 20, ResetsAt: &reset}
	svc.addQuotaCapacityForecast(context.Background(), account, &UsageInfo{}, "seven_day", progress)
	require.Equal(t, "partial_window", progress.CapacityForecast.Status)
	require.Zero(t, repo.calls)
}
