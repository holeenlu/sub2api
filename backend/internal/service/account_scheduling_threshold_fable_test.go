//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fableThresholdAccount 构造一个只有 Fable 专属窗口越线、通用 5h/7d 都很空闲的
// Anthropic 账号——这正是「Fable 贵、其他模型便宜」的典型现场。
func fableThresholdAccount(now time.Time, oiUtilization float64) *Account {
	return &Account{
		ID:          9001,
		Platform:    PlatformAnthropic,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			"passive_usage_7d_utilization":    0.10,
			"passive_usage_7d_reset":          float64(now.Add(3 * 24 * time.Hour).Unix()),
			"passive_usage_7d_oi_utilization": oiUtilization,
			"passive_usage_7d_oi_reset":       float64(now.Add(4 * 24 * time.Hour).Unix()),
		},
	}
}

func TestEvaluateAnthropicFableSchedulingThreshold_ScopeThresholdTightensAnthropic(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	account := fableThresholdAccount(now, 0.60)
	thresholds := map[string]int{
		PlatformAnthropic:                      90,
		SchedulingThresholdScopeAnthropicFable: 50,
	}

	// 通用阈值 90 拦不住 60%，Fable 专属阈值 50 能拦住。
	require.False(t, EvaluateAccountSchedulingThreshold(account, thresholds, now).ShouldPause)

	decision := evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, 50, decision.ThresholdPercent)
	require.Equal(t, "7d_oi", decision.Window)
	require.Equal(t, anthropicFableRateLimitKey, decision.Scope)
	require.Equal(t, 60.0, decision.UsedPercent)
}

func TestEvaluateAnthropicFableSchedulingThreshold_InheritsAnthropicWhenDisabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	// Fable 侧填 100（未启用）时沿用 Anthropic 通用阈值——本 scope 引入前就是这个
	// 行为，必须保持向后兼容。
	account := fableThresholdAccount(now, 0.95)
	decision := evaluateAnthropicFableSchedulingThreshold(account, map[string]int{
		PlatformAnthropic:                      90,
		SchedulingThresholdScopeAnthropicFable: 100,
	}, true, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, 90, decision.ThresholdPercent)
}

// Fable 专属窗口撞满只让上游对 Fable 返 429、不停账号，余量用不完就是浪费。运维要
// 表达「账号在 7d 70% 停，但 Fable 允许用满专属窗口的 95%」时，取更严者会把它悄悄
// 压成 70。
func TestEvaluateAnthropicFableSchedulingThreshold_MayExceedAnthropicThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	thresholds := map[string]int{
		PlatformAnthropic:                      70,
		SchedulingThresholdScopeAnthropicFable: 95,
	}

	// 7d_oi 80% 低于 Fable 的 95，不停 Fable。
	account := fableThresholdAccount(now, 0.80)
	decision := evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now)
	require.False(t, decision.ShouldPause)
	require.Equal(t, 95, decision.ThresholdPercent)

	// 到 96% 才停。
	account = fableThresholdAccount(now, 0.96)
	decision = evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, "7d_oi", decision.Window)
}

// 生产回归（2026-09-03）：共享 7d 已到 41%/72%，但 Fable 专属 7d_oi 缺失/仅 3%。
// anthropic_fable=30 只能看 7d_oi，不能让其他模型消耗的共享窗口触发 CFable5。
func TestEvaluateAnthropicFableSchedulingThreshold_SharedWindowNeverTriggersFableLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	thresholds := map[string]int{
		PlatformAnthropic:                      100,
		SchedulingThresholdScopeAnthropicFable: 30,
	}

	for _, tc := range []struct {
		name    string
		shared  float64
		fableOI *float64
	}{
		{name: "claude-cc-2 shape", shared: 0.41, fableOI: nil},
		{name: "claude-cc-5 shape", shared: 0.72, fableOI: floatPtr(0.03)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := fableThresholdAccount(now, 0)
			account.Extra["passive_usage_7d_utilization"] = tc.shared
			if tc.fableOI == nil {
				delete(account.Extra, "passive_usage_7d_oi_utilization")
				delete(account.Extra, "passive_usage_7d_oi_reset")
			} else {
				account.Extra["passive_usage_7d_oi_utilization"] = *tc.fableOI
			}

			decision := evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now)
			require.False(t, decision.ShouldPause)
		})
	}
}

// 共享 7d 只属于普通 Anthropic 阈值：它可以停掉整个账号，但不得额外产生 Fable
// 模型级限流。
func TestEvaluateAnthropicFableSchedulingThreshold_SharedWindowBelongsToGeneralThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	thresholds := map[string]int{
		PlatformAnthropic:                      50,
		SchedulingThresholdScopeAnthropicFable: 30,
	}

	account := fableThresholdAccount(now, 0.10)
	account.Extra["passive_usage_7d_utilization"] = 0.60

	// 通用规则停掉整个账号。
	require.True(t, EvaluateAccountSchedulingThreshold(account, thresholds, now).ShouldPause)

	// Fable 求值器只看 7d_oi，而 7d_oi 才 10%。
	decision := evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now)
	require.False(t, decision.ShouldPause)
	require.Len(t, anthropicFableThresholdCandidates(account), 1)
}

func TestAnthropicFableThresholdCandidates_FallsBackToSevenDayReset(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	wantUntil := now.Add(2 * 24 * time.Hour)

	// 上游没下发 7d_oi-reset 头：没有回退的话 until 为 nil，阈值就永远不触发。
	account := &Account{
		Platform: PlatformAnthropic,
		Extra: map[string]any{
			"passive_usage_7d_reset":          float64(wantUntil.Unix()),
			"passive_usage_7d_oi_utilization": 0.75,
		},
	}

	candidates := anthropicFableThresholdCandidates(account)
	require.Len(t, candidates, 1, "只有 7d_oi 一个窗口有用量采样")
	require.Equal(t, "7d_oi", candidates[0].window)
	require.Equal(t, "7d", candidates[0].untilSource)
	require.NotNil(t, candidates[0].until)
	require.True(t, wantUntil.Equal(*candidates[0].until))

	decision := evaluateAnthropicFableSchedulingThreshold(account, map[string]int{
		SchedulingThresholdScopeAnthropicFable: 50,
	}, true, now)
	require.True(t, decision.ShouldPause)
	require.True(t, wantUntil.Equal(*decision.Until))
	require.Equal(t, "7d", decision.UntilSource)

	// 借来的解除时刻必须在 reason 里留痕，排障时才分得清封到的是哪个窗口的重置。
	reason := BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{
		Platform: decision.Platform, Window: decision.Window, Scope: decision.Scope,
		UntilSource: decision.UntilSource, ThresholdPercent: decision.ThresholdPercent,
		UsedPercent: decision.UsedPercent, Until: *decision.Until, Now: now,
	})
	payload, ok := parseTempUnschedReasonPayload(reason)
	require.True(t, ok)
	require.Equal(t, "7d", payload.UntilSource)
}

// until 取自本窗口自己的采样时不标记来源。
func TestAnthropicFableThresholdCandidates_OwnResetLeavesUntilSourceEmpty(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	candidates := anthropicFableThresholdCandidates(fableThresholdAccount(now, 0.75))

	require.Len(t, candidates, 1)
	require.Equal(t, "7d_oi", candidates[0].window)
	require.Empty(t, candidates[0].untilSource)
}

func TestAnthropicFableThresholdCandidates_NoResetAtAllDoesNotPause(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformAnthropic,
		Extra: map[string]any{
			"passive_usage_7d_oi_utilization": 0.90,
		},
	}

	// 宁可不停调，也不用一个凭空捏造的解除时间把 Fable 封住。用量采样仍然构成候选，
	// 只是没有 until 打不了限流。
	candidates := anthropicFableThresholdCandidates(account)
	require.Len(t, candidates, 1)
	require.Nil(t, candidates[0].until)
	require.False(t, evaluateAnthropicFableSchedulingThreshold(account, map[string]int{
		SchedulingThresholdScopeAnthropicFable: 50,
	}, true, now).ShouldPause)
}

// 回退来的 until 仍然要落在未来：7d 窗口早已重置时不该把 Fable 封在过去的时刻上。
func TestAnthropicFableThresholdCandidates_ExpiredFallbackResetDoesNotPause(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	account := &Account{
		Platform: PlatformAnthropic,
		Extra: map[string]any{
			"passive_usage_7d_reset":          float64(now.Add(-time.Hour).Unix()),
			"passive_usage_7d_oi_utilization": 0.90,
		},
	}

	require.False(t, evaluateAnthropicFableSchedulingThreshold(account, map[string]int{
		SchedulingThresholdScopeAnthropicFable: 50,
	}, true, now).ShouldPause)
}

func TestEvaluateAnthropicFableSchedulingThreshold_BothUnconfiguredDoesNotPause(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	account := fableThresholdAccount(now, 0.99)

	require.False(t, evaluateAnthropicFableSchedulingThreshold(account, map[string]int{}, true, now).ShouldPause)
}

func TestEvaluateAnthropicFableSchedulingThreshold_AccountOverrideBeatsGlobalScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	thresholds := map[string]int{
		PlatformAnthropic:                      100,
		SchedulingThresholdScopeAnthropicFable: 80,
	}

	// 全局 80 拦不住 40%。
	account := fableThresholdAccount(now, 0.40)
	require.False(t, evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now).ShouldPause)

	// 账号单独收紧到 30 后必须被拦下。
	account = fableThresholdAccount(now, 0.40)
	account.Credentials = map[string]any{
		anthropicFableSchedulingThresholdCredentialKey: 30,
	}
	decision := evaluateAnthropicFableSchedulingThreshold(account, thresholds, true, now)
	require.True(t, decision.ShouldPause)
	require.Equal(t, 30, decision.ThresholdPercent)
}

func TestEvaluateAnthropicFableSchedulingThreshold_AccountOverrideIsIndependent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	account := fableThresholdAccount(now, 0.65)
	// 账号级把 Fable 放宽到 90、通用收到 60：两个旋钮各自独立，Fable 用 90。
	account.Credentials = map[string]any{
		accountSchedulingThresholdCredentialKey:        60,
		anthropicFableSchedulingThresholdCredentialKey: 90,
	}

	decision := evaluateAnthropicFableSchedulingThreshold(account, map[string]int{}, true, now)
	require.False(t, decision.ShouldPause, "7d_oi 65% 低于 Fable 自己的 90")
	require.Equal(t, 90, decision.ThresholdPercent)
}

func TestEvaluateAnthropicFableSchedulingThreshold_NonAnthropicAccountIgnored(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	account := fableThresholdAccount(now, 0.99)
	account.Platform = PlatformOpenAI

	require.False(t, evaluateAnthropicFableSchedulingThreshold(account, map[string]int{
		SchedulingThresholdScopeAnthropicFable: 50,
	}, true, now).ShouldPause)
}

// 窗口撞满时上游会下发略大于 1 的 utilization。按 1.0 作硬边界会把 1.02 读成
// 1.02%，恰好在越线那一刻把结论反过来。
func TestUtilizationAsPercent_TreatsOverflowFractionAsPercent(t *testing.T) {
	t.Parallel()

	require.InDelta(t, 102.0, utilizationAsPercent(1.02), 1e-9)
	require.InDelta(t, 102.0, utilizationAsPercent("1.02"), 1e-9)
	require.InDelta(t, 100.0, utilizationAsPercent(1.0), 1e-9)
	require.InDelta(t, 87.0, utilizationAsPercent(0.87), 1e-9)
	// 整数仍按百分比口径读，不受影响。
	require.InDelta(t, 87.0, utilizationAsPercent(87), 1e-9)
	require.InDelta(t, 87.0, utilizationAsPercent("87"), 1e-9)
}
