//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type fableThresholdReleaseRepoStub struct {
	rateLimitAccountRepoStub
	setCalls        int
	lastSetScope    string
	lastSetReason   string
	clearCalls      int
	lastClearScope  string
	lastClearReason string
	lastClearedAcct int64
	clearNoMatch    bool
}

func (r *fableThresholdReleaseRepoStub) SetModelRateLimit(_ context.Context, _ int64, scope string, _ time.Time, reason ...string) error {
	r.setCalls++
	r.lastSetScope = scope
	if len(reason) > 0 {
		r.lastSetReason = reason[0]
	}
	return nil
}

func (r *fableThresholdReleaseRepoStub) ClearModelRateLimit(_ context.Context, id int64, scope string, expectedReason string) (bool, error) {
	r.clearCalls++
	r.lastClearScope = scope
	r.lastClearReason = expectedReason
	r.lastClearedAcct = id
	return !r.clearNoMatch, nil
}

func newFableThresholdService(t *testing.T, thresholdsJSON string) (*RateLimitService, *fableThresholdReleaseRepoStub) {
	t.Helper()
	accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
	accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})

	settingsRepo := newMockSettingRepo()
	settingsRepo.data[SettingKeyAccountSchedulingThresholds] = thresholdsJSON

	accountRepo := &fableThresholdReleaseRepoStub{}
	rl := NewRateLimitService(accountRepo, nil, &config.Config{}, nil, nil)
	rl.SetSettingService(NewSettingService(settingsRepo, &config.Config{}))
	return rl, accountRepo
}

func newFableThresholdAccount(oiUtilization float64) *Account {
	now := time.Now().UTC()
	return &Account{
		ID:          2001,
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

func TestRateLimitService_FableScopeThreshold_KeepsSnapshotWhenCASMisses(t *testing.T) {
	rl, repo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":70}`)
	account := newFableThresholdAccount(0.60)
	markFableLimitFromThreshold(account, 60)
	reason := account.modelRateLimitReason(anthropicFableRateLimitKey)
	// The database now holds an upstream 429 reason; the stale snapshot cannot clear it.
	repo.clearNoMatch = true

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, repo.clearCalls)
	require.Equal(t, reason, account.modelRateLimitReason(anthropicFableRateLimitKey))
	require.False(t, account.IsSchedulableForModel("claude-fable-5"))
}

// markFableLimitFromThreshold 给账号打上一条「由阈值打出」的 Fable 限流。
func markFableLimitFromThreshold(account *Account, usedPercent float64) {
	now := time.Now().UTC()
	until := now.Add(2 * time.Hour)
	setAccountModelRateLimitSnapshot(account, anthropicFableRateLimitKey, until,
		BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{
			Platform: PlatformAnthropic, Window: "7d_oi", Scope: anthropicFableRateLimitKey,
			ThresholdPercent: 50, UsedPercent: usedPercent, Until: until, Now: now,
		}), now)
}

// 全局 Anthropic 阈值保持 100（账号整体不停调），只靠 Fable 阈值的 50% 拦 Fable。
func TestRateLimitService_FableScopeThreshold_LimitsOnlyFableModels(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)
	account := newFableThresholdAccount(0.60)

	blocked := rl.ApplyAccountSchedulingThreshold(context.Background(), account)

	require.False(t, blocked, "Fable 越线不得停掉整个账号")
	require.Zero(t, accountRepo.tempCalls)
	require.Equal(t, 1, accountRepo.setCalls)
	require.Equal(t, anthropicFableRateLimitKey, accountRepo.lastSetScope)
	require.True(t, IsAccountSchedulingThresholdReason(accountRepo.lastSetReason))

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(accountRepo.lastSetReason), &payload))
	require.Equal(t, float64(50), payload["threshold_percent"])
	require.Equal(t, float64(60), payload["used_percent"])
	require.Contains(t, payload["error_message"], "7d_oi/claude-fable-5")

	require.False(t, account.IsSchedulableForModel("claude-fable-5"))
	require.False(t, account.IsSchedulableForModel("claude-fable-5[1m]"))
	require.True(t, account.IsSchedulableForModel("claude-opus-5"))
	require.True(t, account.IsSchedulableForModel("claude-sonnet-4-6"))
}

func TestRateLimitService_FableScopeThreshold_BelowThresholdDoesNotLimit(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)
	account := newFableThresholdAccount(0.30)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Zero(t, accountRepo.setCalls)
	require.Zero(t, accountRepo.clearCalls)
	require.True(t, account.IsSchedulableForModel("claude-fable-5"))
}

func TestRateLimitService_FableScopeThreshold_IgnoresSharedSevenDayUsage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shared  float64
		fableOI *float64
	}{
		{name: "claude-cc-2 shape", shared: 0.41, fableOI: nil},
		{name: "claude-cc-5 shape", shared: 0.72, fableOI: floatPtr(0.03)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":30}`)
			account := newFableThresholdAccount(0)
			account.Extra["passive_usage_7d_utilization"] = tc.shared
			if tc.fableOI == nil {
				delete(account.Extra, "passive_usage_7d_oi_utilization")
				delete(account.Extra, "passive_usage_7d_oi_reset")
			} else {
				account.Extra["passive_usage_7d_oi_utilization"] = *tc.fableOI
			}

			require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
			require.Zero(t, accountRepo.setCalls)
			require.True(t, account.IsSchedulableForModel("claude-fable-5"))
		})
	}
}

func TestRateLimitService_FableScopeThreshold_ClearsLegacySharedWindowLimitWithoutOISample(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":30}`)
	account := newFableThresholdAccount(0)
	delete(account.Extra, "passive_usage_7d_oi_utilization")
	delete(account.Extra, "passive_usage_7d_oi_reset")
	account.Extra["passive_usage_7d_utilization"] = 0.72

	now := time.Now().UTC()
	until := now.Add(48 * time.Hour)
	setAccountModelRateLimitSnapshot(account, anthropicFableRateLimitKey, until,
		BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{
			Platform: PlatformAnthropic, Window: "7d", Scope: anthropicFableRateLimitKey,
			ThresholdPercent: 30, UsedPercent: 72, Until: until, Now: now,
		}), now)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls)
	require.True(t, account.IsSchedulableForModel("claude-fable-5"))
}

// Fable 侧填 100 时沿用 Anthropic 通用阈值（本 scope 引入前的行为）。
func TestRateLimitService_FableScopeThreshold_FallsBackToAnthropicThreshold(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":50,"anthropic_fable":100}`)
	account := newFableThresholdAccount(0.60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account),
		"5h/7d 都很空闲，账号整体不该被停")
	require.Equal(t, 1, accountRepo.setCalls)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(accountRepo.lastSetReason), &payload))
	require.Equal(t, float64(50), payload["threshold_percent"])
}

// 阈值是给运维反复调的旋钮：调宽之后必须立刻解除，而不是等窗口重置。
func TestRateLimitService_FableScopeThreshold_ReleasedWhenThresholdRaised(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)
	account := newFableThresholdAccount(0.60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.setCalls)
	require.False(t, account.IsSchedulableForModel("claude-fable-5"))

	// 运维把阈值放宽到 70。
	rl2, _ := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":70}`)
	rl2.accountRepo = accountRepo

	require.False(t, rl2.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls)
	require.Equal(t, anthropicFableRateLimitKey, accountRepo.lastClearScope)
	require.Equal(t, account.ID, accountRepo.lastClearedAcct)
	require.True(t, account.IsSchedulableForModel("claude-fable-5"), "内存快照也要同步解除")
}

// 阈值被彻底关闭（回到 100）本身就是判据，应当解除。
func TestRateLimitService_FableScopeThreshold_ReleasesWhenThresholdDisabled(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":100}`)

	account := newFableThresholdAccount(0.90)
	markFableLimitFromThreshold(account, 90)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls)
}

// 阈值读取失败时 GetAccountSchedulingThresholds 回落到全 100 的兜底默认值，并按
// ErrorTTL 缓存 5s。把兜底默认当成「运维关掉了阈值」，配置系统抖一下就等于对全池
// 执行一次解除：7d_oi 已接近撞满的账号重新接 Fable 请求、撞上游 429，TTL 过后又被
// 重新打上限流。
func TestRateLimitService_FableScopeThreshold_KeepsLimitWhenThresholdReadFails(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)
	settingsRepo := rl.settingService.settingRepo.(*mockSettingRepo)
	settingsRepo.getValueErr = errors.New("settings backend unavailable")

	account := newFableThresholdAccount(0.90)
	markFableLimitFromThreshold(account, 90)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Zero(t, accountRepo.clearCalls, "读取失败不构成「运维关掉阈值」的判据")
	require.False(t, account.IsSchedulableForModel("claude-fable-5"))
}

// 有采样且确认回落到阈值以下，才解除。
func TestRateLimitService_FableScopeThreshold_ReleasesWhenUsageDropsBelow(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)

	account := newFableThresholdAccount(0.20)
	markFableLimitFromThreshold(account, 60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls)
	require.Equal(t, anthropicFableRateLimitKey, accountRepo.lastClearScope)
	require.True(t, account.IsSchedulableForModel("claude-fable-5"))
}

// 解除必须带上「我看到的那条 reason」：这份账号副本是选号时刻从 Redis 拷来的，判断
// 与真正的 UPDATE 之间上游 429 完全可能把同一个 scope 改写成窗口耗尽限流。谓词交给
// DB，并发的清除请求才有串行化的地方。
func TestRateLimitService_FableScopeThreshold_ReleasePassesObservedReasonAsGuard(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)

	account := newFableThresholdAccount(0.20)
	markFableLimitFromThreshold(account, 60)
	observed := account.modelRateLimitReason(anthropicFableRateLimitKey)
	require.NotEmpty(t, observed)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls)
	require.Equal(t, observed, accountRepo.lastClearReason)
}

// 上游 429 打的窗口耗尽限流不是阈值来源，绝不能被阈值评估顺手清掉——清了就会继续往
// 这个账号送 Fable 请求，撞回上游 429。
func TestRateLimitService_FableScopeThreshold_DoesNotReleaseUpstream429Limit(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":70}`)

	// 用量远低于阈值 → 阈值评估判定「不该停调」，走解除分支。
	account := newFableThresholdAccount(0.10)
	setAccountModelRateLimitSnapshot(
		account,
		anthropicFableRateLimitKey,
		time.Now().UTC().Add(4*24*time.Hour),
		anthropicFableWindowReason,
		time.Now().UTC(),
	)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Zero(t, accountRepo.clearCalls, "429 窗口耗尽限流必须原样保留")
	require.False(t, account.IsSchedulableForModel("claude-fable-5"))
}

// ── 判据缺失时不得解除限流 ────────────────────────────────────────────────

// 用量采样缺失（投影被裁、账号刚建、5h 窗口滚动清空）时，评估必须回答「不知道」而不是
// 「没越线」，否则会把刚打上的限流清掉。
func TestRateLimitService_FableScopeThreshold_KeepsLimitWhenSampleMissing(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)

	account := newFableThresholdAccount(0.60)
	delete(account.Extra, "passive_usage_7d_oi_utilization")
	markFableLimitFromThreshold(account, 60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Zero(t, accountRepo.clearCalls, "没有采样就没有判据，不得解除")
	require.False(t, account.IsSchedulableForModel("claude-fable-5"))
}

// reset 采样只决定「封到什么时候」，不决定「现在越没越线」：用量采样自己就是判据。
// 反过来，用量仍在阈值之上时缺 reset 也不能解除。
func TestRateLimitService_FableScopeThreshold_ResetSampleOnlyGatesPausing(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)

	account := newFableThresholdAccount(0.20)
	delete(account.Extra, "passive_usage_7d_oi_reset")
	delete(account.Extra, "passive_usage_7d_reset")
	markFableLimitFromThreshold(account, 60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls, "20% 低于阈值 50，缺 reset 不影响这个结论")

	rl2, accountRepo2 := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)
	overLimit := newFableThresholdAccount(0.80)
	delete(overLimit.Extra, "passive_usage_7d_oi_reset")
	delete(overLimit.Extra, "passive_usage_7d_reset")
	markFableLimitFromThreshold(overLimit, 80)

	require.False(t, rl2.ApplyAccountSchedulingThreshold(context.Background(), overLimit))
	require.Zero(t, accountRepo2.setCalls, "没有 until 就打不了限流")
	require.Zero(t, accountRepo2.clearCalls, "但仍然越线，绝不能解除")
}

// 从没跑过 Fable 的账号，上游给的 7d_oi 用量就是 0——那是「确实没用掉」的有效判据，
// 不是「没有判据」。按候选条数算判据会把这类账号永远卡住：运维调宽阈值后解除不了，
// 只能等最长七天的窗口自然过期。
func TestRateLimitService_FableScopeThreshold_ReleasesWhenFableWindowUnused(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":70}`)

	account := newFableThresholdAccount(0)
	account.Extra["passive_usage_7d_utilization"] = 0.60
	markFableLimitFromThreshold(account, 60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.clearCalls, "7d_oi 报 0 同样构成判据")
	require.True(t, account.IsSchedulableForModel("claude-fable-5"))
}

// 同一账号连续评估两次必须收敛：第一次打限流，第二次既不重复打也不解除。
// 这正是投影裁掉账号级覆盖时出现的 set/clear 乒乓的直接回归用例。
func TestRateLimitService_FableScopeThreshold_IsIdempotentAcrossEvaluations(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":50}`)
	account := newFableThresholdAccount(0.60)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.setCalls)

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.setCalls, "已生效的限流不该重复写")
	require.Zero(t, accountRepo.clearCalls, "同一份判据不该反过来解除")
}

// 账号级覆盖必须能独立生效：全局 100（不启用）+ 账号覆盖 50 时照样拦 Fable。
// 覆盖键此前不在调度元数据投影里，列表路径读不到它，才有了乒乓。
func TestRateLimitService_FableScopeThreshold_HonoursAccountLevelOverride(t *testing.T) {
	rl, accountRepo := newFableThresholdService(t, `{"anthropic":100,"anthropic_fable":100}`)

	account := newFableThresholdAccount(0.60)
	account.Credentials = map[string]any{anthropicFableSchedulingThresholdCredentialKey: float64(50)}

	require.False(t, rl.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.Equal(t, 1, accountRepo.setCalls)
	require.Equal(t, anthropicFableRateLimitKey, accountRepo.lastSetScope)
	require.Zero(t, accountRepo.clearCalls)
}
