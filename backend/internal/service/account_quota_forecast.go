package service

import (
	"context"
	"fmt"
	"math"
	"time"
)

// QuotaCapacityForecast 仅展示已留存网关记录对应的窗口容量，不参与调度或结算。
type QuotaCapacityForecast struct {
	Status                  string     `json:"status"`
	ObservedAt              *time.Time `json:"observed_at,omitempty"`
	SamplePercent           float64    `json:"sample_percent"`
	LowSample               bool       `json:"low_sample"`
	EstimatedTokens         *int64     `json:"estimated_tokens,omitempty"`
	EstimatedCost           *float64   `json:"estimated_cost,omitempty"`
	MonthlyEquivalentTokens *int64     `json:"monthly_equivalent_tokens,omitempty"`
	MonthlyEquivalentCost   *float64   `json:"monthly_equivalent_cost,omitempty"`
}

type QuotaForecastSample struct {
	Requests, Tokens, MissingTokens, MissingCost int64
	Cost                                         float64
}

type quotaForecastReader interface {
	GetAccountQuotaForecastSample(context.Context, int64, time.Time, time.Time, string) (*QuotaForecastSample, error)
}

func (s *AccountUsageService) addQuotaCapacityForecast(ctx context.Context, account *Account, usage *UsageInfo, window string, progress *UsageProgress) {
	if progress == nil || account == nil {
		return
	}
	forecast := &QuotaCapacityForecast{Status: "insufficient_sample"}
	progress.CapacityForecast = forecast
	now := time.Now()
	percent := progress.Utilization
	if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 5 || percent > 100 {
		return
	}
	forecast.SamplePercent = percent
	if progress.ResetsAt == nil || !now.Before(*progress.ResetsAt) || progress.ResetsAt.Sub(now) > 7*24*time.Hour {
		forecast.Status = "invalid_window"
		return
	}
	start := progress.ResetsAt.Add(-7 * 24 * time.Hour)
	if account.CreatedAt.IsZero() || account.CreatedAt.After(start) {
		forecast.Status = "partial_window"
		return
	}
	var observed *time.Time
	scope := account.Platform
	if account.Platform == PlatformOpenAI {
		if resetAt, err := parseTime(fmt.Sprint(account.Extra["codex_quota_reset_at"])); err == nil && resetAt.After(start) {
			forecast.Status = "partial_window"
			return
		}
		if !openAICodexSnapshotIdentityTrusted(account) {
			forecast.Status = "unknown_observation"
			return
		}
		if stamp, err := parseTime(fmt.Sprint(account.Extra["codex_usage_updated_at"])); err == nil {
			observed = &stamp
		}
		// 当前授权必须仍是原生额度；历史 BPS/SIWC 在查询中按用量模式隔离。
		if account.IsSIWCEnabled() {
			forecast.Status = "unsupported_scope"
			return
		}
	} else if account.Platform == PlatformAnthropic && usage.Anthropic != nil {
		if anthropicWindowClearedAt(usage.Anthropic.Observations, window).After(start) {
			forecast.Status = "partial_window"
			return
		}
		observation := usage.Anthropic.Observations[window+".utilization"]
		if observation.Value != nil {
			observed = observation.SampledAt
		}
		if window == "seven_day_fable" {
			scope = "anthropic_fable"
		}
	} else {
		forecast.Status = "unknown_observation"
		return
	}
	forecast.ObservedAt = observed
	if observed == nil || observed.Before(start) || observed.After(now) || now.Sub(*observed) > 10*time.Minute {
		forecast.Status = "stale_observation"
		return
	}
	reader, ok := s.usageLogRepo.(quotaForecastReader)
	if !ok {
		forecast.Status = "missing_usage"
		return
	}
	sample, err := reader.GetAccountQuotaForecastSample(ctx, account.ID, start, *observed, scope)
	if err != nil || sample == nil || sample.Requests == 0 {
		forecast.Status = "missing_usage"
		return
	}
	forecast.LowSample = percent < 10 || sample.Requests < 5
	forecast.Status = "ready"
	if sample.MissingTokens == 0 && sample.Tokens > 0 {
		forecast.EstimatedTokens = quotaForecastTokens(float64(sample.Tokens) * 100 / percent)
		forecast.MonthlyEquivalentTokens = quotaForecastTokens(float64(sample.Tokens) * 100 / percent * 30 / 7)
	}
	if sample.MissingCost == 0 && sample.Cost >= 0 {
		forecast.EstimatedCost = quotaForecastCost(sample.Cost * 100 / percent)
		forecast.MonthlyEquivalentCost = quotaForecastCost(sample.Cost * 100 / percent * 30 / 7)
	}
	if forecast.EstimatedTokens == nil && forecast.EstimatedCost == nil {
		forecast.Status = "missing_usage"
	}
}

func quotaForecastTokens(value float64) *int64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value >= float64(math.MaxInt64) {
		return nil
	}
	tokens := int64(math.Round(value))
	return &tokens
}
func quotaForecastCost(value float64) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return nil
	}
	return &value
}
