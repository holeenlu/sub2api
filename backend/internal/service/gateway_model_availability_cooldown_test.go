//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// cooldownAccount builds a persistently eligible OpenAI account whose model
// mapping accepts everything, optionally parked in an account-wide rate-limit
// cooldown.
func cooldownAccount(id int64, resetAt *time.Time) Account {
	return Account{
		ID:               id,
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		Status:           StatusActive,
		Schedulable:      true,
		RateLimitResetAt: resetAt,
	}
}

// modelCooldownAccount parks the account in a model-scoped cooldown while
// leaving the account-wide reset untouched.
func modelCooldownAccount(id int64, model string, resetAt time.Time) Account {
	acc := cooldownAccount(id, nil)
	acc.Extra = map[string]any{
		modelRateLimitsKey: map[string]any{
			model: map[string]any{
				"rate_limit_reset_at": resetAt.UTC().Format(time.RFC3339),
			},
		},
	}
	return acc
}

// otherModelAccount only admits someOtherModel, so it never counts as
// model-capable for the requested one.
func otherModelAccount(id int64, someOtherModel string, resetAt *time.Time) Account {
	acc := cooldownAccount(id, resetAt)
	acc.Credentials = map[string]any{
		"model_mapping": map[string]any{
			someOtherModel: someOtherModel,
		},
	}
	return acc
}

type poolCooldownCase struct {
	name             string
	accounts         []Account
	wantModelSupport bool
	wantAllCooling   bool
	// wantEarliest is nil when no counted account is cooling.
	wantEarliest *time.Time
}

func poolCooldownCases(model string) []poolCooldownCase {
	now := time.Now()
	in30s := now.Add(30 * time.Second)
	in2m := now.Add(2 * time.Minute)
	in5m := now.Add(5 * time.Minute)
	expired := now.Add(-time.Minute)
	past := now.Add(-time.Hour)

	return []poolCooldownCase{
		{
			name: "每个支持该模型的账号都在冷却中",
			accounts: []Account{
				cooldownAccount(1, &in2m),
				cooldownAccount(2, &in30s),
				cooldownAccount(3, &in5m),
			},
			wantModelSupport: true,
			wantAllCooling:   true,
			wantEarliest:     &in30s,
		},
		{
			// 有一个账号没冷却就不是全池限流，但最早恢复时刻仍作为提示保留。
			name: "只要有一个账号未冷却就不算全池限流",
			accounts: []Account{
				cooldownAccount(1, &in2m),
				cooldownAccount(2, nil),
			},
			wantModelSupport: true,
			wantAllCooling:   false,
			wantEarliest:     &in2m,
		},
		{
			name: "模型级冷却同样计入",
			accounts: []Account{
				modelCooldownAccount(1, model, in2m),
			},
			wantModelSupport: true,
			wantAllCooling:   true,
			wantEarliest:     &in2m,
		},
		{
			name: "账号级与模型级冷却取较晚者",
			accounts: []Account{
				func() Account {
					acc := modelCooldownAccount(1, model, in5m)
					acc.RateLimitResetAt = &in30s
					return acc
				}(),
			},
			wantModelSupport: true,
			wantAllCooling:   true,
			wantEarliest:     &in5m,
		},
		{
			// 冷却中的账号压根不支持该模型时，可用的那个账号没冷却 —— 不能报 429。
			name: "冷却的账号不支持该模型",
			accounts: []Account{
				otherModelAccount(1, "gpt-image", &in2m),
				otherModelAccount(2, "gpt-image", &in5m),
				cooldownAccount(3, nil),
			},
			wantModelSupport: true,
			wantAllCooling:   false,
		},
		{
			// 池子里没有账号支持该模型：这是 404 的地盘，绝不能变成 429。
			name: "没有账号支持该模型",
			accounts: []Account{
				otherModelAccount(1, "gpt-image", &in2m),
			},
			wantModelSupport: false,
			wantAllCooling:   false,
		},
		{
			name: "过期的冷却时间不算冷却",
			accounts: []Account{
				cooldownAccount(1, &expired),
			},
			wantModelSupport: true,
			wantAllCooling:   false,
		},
		{
			// 限流窗口更早结束的那个账号同时总额度超限：冷却结束它也回不来，
			// Retry-After 必须指向另一个账号。
			name: "额度超限的限流账号不参与最早恢复时刻",
			accounts: []Account{
				func() Account {
					acc := cooldownAccount(1, &in30s)
					acc.Type = AccountTypeAPIKey
					acc.Extra = map[string]any{"quota_limit": 10.0, "quota_used": 10.0}
					return acc
				}(),
				cooldownAccount(2, &in5m),
			},
			wantModelSupport: true,
			wantAllCooling:   true,
			wantEarliest:     &in5m,
		},
		{
			name: "过载的限流账号不参与判定",
			accounts: []Account{
				func() Account {
					acc := cooldownAccount(1, &in30s)
					acc.OverloadUntil = &in2m
					return acc
				}(),
			},
			wantModelSupport: true,
			wantAllCooling:   false,
		},
		{
			name: "临时停调的限流账号不参与判定",
			accounts: []Account{
				func() Account {
					acc := cooldownAccount(1, &in30s)
					acc.TempUnschedulableUntil = &in2m
					return acc
				}(),
			},
			wantModelSupport: true,
			wantAllCooling:   false,
		},
		{
			name: "到期自动暂停的限流账号不参与判定",
			accounts: []Account{
				func() Account {
					acc := cooldownAccount(1, &in30s)
					acc.AutoPauseOnExpired = true
					acc.ExpiresAt = &past
					return acc
				}(),
			},
			wantModelSupport: true,
			wantAllCooling:   false,
		},
		{
			// 过载账号被排除后，剩下的支持账号全部在冷却：仍是全池限流，
			// Retry-After 只看限流账号。
			name: "过载账号排除后其余账号全冷却",
			accounts: []Account{
				func() Account {
					acc := cooldownAccount(1, nil)
					acc.OverloadUntil = &in30s
					return acc
				}(),
				cooldownAccount(2, &in2m),
			},
			wantModelSupport: true,
			wantAllCooling:   true,
			wantEarliest:     &in2m,
		},
	}
}

func assertPoolCooldown(t *testing.T, tc poolCooldownCase, diag ModelAvailabilityDiagnosis) {
	t.Helper()
	require.True(t, diag.HasAccountsInPool)
	require.Equal(t, tc.wantModelSupport, diag.HasModelSupport)
	require.Equal(t, tc.wantAllCooling, diag.AllModelCapableRateLimited)
	if tc.wantEarliest == nil {
		require.Nil(t, diag.EarliestRateLimitResetAt)
		return
	}
	require.NotNil(t, diag.EarliestRateLimitResetAt)
	require.WithinDuration(t, *tc.wantEarliest, *diag.EarliestRateLimitResetAt, 2*time.Second)
}

func TestDiagnoseModelAvailability_PoolCooldown(t *testing.T) {
	const model = "gpt-5.4"
	for _, tc := range poolCooldownCases(model) {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockAccountRepoForPlatform{accounts: tc.accounts}
			svc := &GatewayService{accountRepo: repo, cfg: testConfig()}

			diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, model, PlatformOpenAI)

			assertPoolCooldown(t, tc, diag)
		})
	}
}

func TestOpenAIDiagnoseModelAvailability_PoolCooldown(t *testing.T) {
	const model = "gpt-5.4"
	for _, tc := range poolCooldownCases(model) {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockAccountRepoForPlatform{accounts: tc.accounts}
			svc := &OpenAIGatewayService{accountRepo: repo, cfg: testConfig()}

			diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, model, PlatformOpenAI)

			assertPoolCooldown(t, tc, diag)
		})
	}
}

// 空池仍然只是「没有账号」，不得被当成限流。
func TestDiagnoseModelAvailability_EmptyPoolIsNotCooldown(t *testing.T) {
	repo := &mockAccountRepoForPlatform{}

	for name, diag := range map[string]ModelAvailabilityDiagnosis{
		"generic": (&GatewayService{accountRepo: repo, cfg: testConfig()}).
			DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.4", PlatformOpenAI),
		"openai": (&OpenAIGatewayService{accountRepo: repo, cfg: testConfig()}).
			DiagnoseModelAvailabilityForPlatform(context.Background(), nil, "gpt-5.4", PlatformOpenAI),
	} {
		require.False(t, diag.HasAccountsInPool, name)
		require.False(t, diag.HasModelSupport, name)
		require.False(t, diag.AllModelCapableRateLimited, name)
		require.Nil(t, diag.EarliestRateLimitResetAt, name)
	}
}

// Antigravity 账号开了 overages 且积分未耗尽时，模型级限流不阻塞调度，
// 也就不能算作冷却；积分耗尽后才算。
func TestDiagnoseModelAvailability_AntigravityOveragesBypassModelCooldown(t *testing.T) {
	const model = "claude-sonnet-4-5"
	in2m := time.Now().Add(2 * time.Minute)
	build := func(creditsExhausted bool) Account {
		limits := map[string]any{
			model: map[string]any{"rate_limit_reset_at": in2m.UTC().Format(time.RFC3339)},
		}
		if creditsExhausted {
			limits[creditsExhaustedKey] = map[string]any{"rate_limit_reset_at": in2m.UTC().Format(time.RFC3339)}
		}
		return Account{
			ID:          1,
			Platform:    PlatformAntigravity,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{model: model}},
			Extra: map[string]any{
				"mixed_scheduling": true,
				"allow_overages":   true,
				modelRateLimitsKey: limits,
			},
		}
	}

	t.Run("有积分则不算冷却", func(t *testing.T) {
		svc := &GatewayService{accountRepo: &mockAccountRepoForPlatform{accounts: []Account{build(false)}}, cfg: testConfig()}
		diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, model, PlatformAnthropic)
		require.True(t, diag.HasModelSupport)
		require.False(t, diag.AllModelCapableRateLimited)
		require.Nil(t, diag.EarliestRateLimitResetAt)
	})
	t.Run("积分耗尽则算冷却", func(t *testing.T) {
		svc := &GatewayService{accountRepo: &mockAccountRepoForPlatform{accounts: []Account{build(true)}}, cfg: testConfig()}
		diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), nil, model, PlatformAnthropic)
		require.True(t, diag.HasModelSupport)
		require.True(t, diag.AllModelCapableRateLimited)
		require.NotNil(t, diag.EarliestRateLimitResetAt)
	})
}

func TestAccountRateLimitCooldownEnd(t *testing.T) {
	const model = "gpt-5.4"
	now := time.Now()
	accountReset := now.Add(time.Minute)
	modelReset := now.Add(4 * time.Minute)

	t.Run("取账号级与模型级较晚者", func(t *testing.T) {
		acc := modelCooldownAccount(1, model, modelReset)
		acc.RateLimitResetAt = &accountReset
		end := accountRateLimitCooldownEnd(context.Background(), &acc, model)
		require.NotNil(t, end)
		require.WithinDuration(t, modelReset, *end, 2*time.Second)
	})
	t.Run("未限流返回 nil", func(t *testing.T) {
		acc := cooldownAccount(1, nil)
		require.Nil(t, accountRateLimitCooldownEnd(context.Background(), &acc, model))
	})
	t.Run("nil 账号返回 nil", func(t *testing.T) {
		require.Nil(t, accountRateLimitCooldownEnd(context.Background(), nil, model))
	})
}

// IsSchedulable 的语义不能因为拆出 isSchedulableIgnoringRateLimit 而改变。
func TestIsSchedulableIgnoringRateLimit(t *testing.T) {
	in2m := time.Now().Add(2 * time.Minute)

	rateLimitedOnly := cooldownAccount(1, &in2m)
	require.False(t, rateLimitedOnly.IsSchedulable())
	require.True(t, rateLimitedOnly.isSchedulableIgnoringRateLimit())

	overloaded := cooldownAccount(2, &in2m)
	overloaded.OverloadUntil = &in2m
	require.False(t, overloaded.IsSchedulable())
	require.False(t, overloaded.isSchedulableIgnoringRateLimit())

	healthy := cooldownAccount(3, nil)
	require.True(t, healthy.IsSchedulable())
	require.True(t, healthy.isSchedulableIgnoringRateLimit())
}
