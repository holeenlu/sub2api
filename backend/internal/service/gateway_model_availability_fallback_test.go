//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func modelAvailabilityFallbackChain(t *testing.T, origin int64, groups map[int64]*Group) *noAccountFallbackChain {
	t.Helper()
	return newNoAccountFallbackChain(&origin, noAccountFallbackTestLoader(groups, nil), nil)
}

// 起点分组不支持该模型、兜底分组支持但全池冷却：只诊断起点会答 404
// model_not_found，客户端据此不再重试，而冷却结束后请求本可成功。诊断必须走
// 与选号相同的兜底链。
func TestDiagnoseModelAvailability_FallbackGroupSupportsModel(t *testing.T) {
	origin, fallback := int64(1), int64(2)
	reset := time.Now().Add(90 * time.Second)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:            10,
				Platform:      PlatformAnthropic,
				Status:        StatusActive,
				Schedulable:   true,
				AccountGroups: []AccountGroup{{GroupID: origin}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"},
				},
			},
			{
				ID:               20,
				Platform:         PlatformAnthropic,
				Status:           StatusActive,
				Schedulable:      true,
				RateLimitResetAt: &reset,
				AccountGroups:    []AccountGroup{{GroupID: fallback}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	svc := &GatewayService{
		accountRepo: repo,
		cfg:         testConfig(),
		groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{
			origin:   noAccountFallbackGroup(origin, PlatformAnthropic, &fallback),
			fallback: noAccountFallbackGroup(fallback, PlatformAnthropic, nil),
		}},
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &origin, "claude-opus-4-8", PlatformAnthropic)

	require.True(t, diag.HasAccountsInPool)
	require.True(t, diag.HasModelSupport, "the fallback group serves this model, so it is not a model_not_found")
	require.True(t, diag.AllModelCapableRateLimited, "the only account that can serve the model is cooling down")
	require.NotNil(t, diag.EarliestRateLimitResetAt)
}

// 反面：整条链上都没有账号支持该模型时，404 仍然成立。
func TestDiagnoseModelAvailability_FallbackChainAlsoLacksModel(t *testing.T) {
	origin, fallback := int64(1), int64(2)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:            10,
				Platform:      PlatformAnthropic,
				Status:        StatusActive,
				Schedulable:   true,
				AccountGroups: []AccountGroup{{GroupID: origin}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"},
				},
			},
			{
				ID:            20,
				Platform:      PlatformAnthropic,
				Status:        StatusActive,
				Schedulable:   true,
				AccountGroups: []AccountGroup{{GroupID: fallback}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-haiku-4-5": "claude-haiku-4-5"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	svc := &GatewayService{
		accountRepo: repo,
		cfg:         testConfig(),
		groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{
			origin:   noAccountFallbackGroup(origin, PlatformAnthropic, &fallback),
			fallback: noAccountFallbackGroup(fallback, PlatformAnthropic, nil),
		}},
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &origin, "claude-opus-4-8", PlatformAnthropic)

	require.True(t, diag.HasAccountsInPool)
	require.False(t, diag.HasModelSupport, "no group on the chain serves the model — 404 stays correct")
}

// 起点已经能服务该模型且不是全池冷却时不再走链：答案不会改变，而每一跳都是
// 一次分组读取加一次候选查询，跑在错误路径上不该白花。
func TestDiagnoseModelAvailability_SkipsChainWhenOriginAlreadyAnswers(t *testing.T) {
	origin, fallback := int64(1), int64(2)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:            10,
				Platform:      PlatformAnthropic,
				Status:        StatusActive,
				Schedulable:   true,
				AccountGroups: []AccountGroup{{GroupID: origin}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	groupRepo := &mockGroupRepoForGateway{groups: map[int64]*Group{
		origin:   noAccountFallbackGroup(origin, PlatformAnthropic, &fallback),
		fallback: noAccountFallbackGroup(fallback, PlatformAnthropic, nil),
	}}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig(), groupRepo: groupRepo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &origin, "claude-opus-4-8", PlatformAnthropic)

	require.True(t, diag.HasModelSupport)
	require.False(t, diag.AllModelCapableRateLimited)
	require.Zero(t, groupRepo.getByIDLiteCalls, "no chain walk when the origin diagnosis is already final")
}

func TestDiagnoseAcrossNoAccountFallback_Merge(t *testing.T) {
	early := time.Now().Add(30 * time.Second)
	late := time.Now().Add(120 * time.Second)
	groups := map[int64]*Group{
		1: noAccountFallbackGroup(1, PlatformAnthropic, int64Ptr(2)),
		2: noAccountFallbackGroup(2, PlatformAnthropic, nil),
	}

	t.Run("cooling hop supplies the earliest reset", func(t *testing.T) {
		origin := ModelAvailabilityDiagnosis{
			HasAccountsInPool:          true,
			HasModelSupport:            true,
			AllModelCapableRateLimited: true,
			EarliestRateLimitResetAt:   &late,
		}
		got := diagnoseAcrossNoAccountFallback(context.Background(),
			modelAvailabilityFallbackChain(t, 1, groups), origin,
			func(context.Context, *int64) ModelAvailabilityDiagnosis {
				return ModelAvailabilityDiagnosis{
					HasAccountsInPool:          true,
					HasModelSupport:            true,
					AllModelCapableRateLimited: true,
					EarliestRateLimitResetAt:   &early,
				}
			})

		require.True(t, got.AllModelCapableRateLimited)
		require.Equal(t, &early, got.EarliestRateLimitResetAt, "Retry-After must point at the first account to come back anywhere on the chain")
	})

	t.Run("a hop with a live capable account vetoes the pool-wide cooldown", func(t *testing.T) {
		origin := ModelAvailabilityDiagnosis{
			HasAccountsInPool:          true,
			HasModelSupport:            true,
			AllModelCapableRateLimited: true,
			EarliestRateLimitResetAt:   &late,
		}
		got := diagnoseAcrossNoAccountFallback(context.Background(),
			modelAvailabilityFallbackChain(t, 1, groups), origin,
			func(context.Context, *int64) ModelAvailabilityDiagnosis {
				return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
			})

		require.True(t, got.HasModelSupport)
		require.False(t, got.AllModelCapableRateLimited, "not every model-capable account is rate-limited any more")
	})

	t.Run("a hop without model support does not veto", func(t *testing.T) {
		origin := ModelAvailabilityDiagnosis{
			HasAccountsInPool:          true,
			HasModelSupport:            true,
			AllModelCapableRateLimited: true,
			EarliestRateLimitResetAt:   &late,
		}
		got := diagnoseAcrossNoAccountFallback(context.Background(),
			modelAvailabilityFallbackChain(t, 1, groups), origin,
			func(context.Context, *int64) ModelAvailabilityDiagnosis {
				return ModelAvailabilityDiagnosis{HasAccountsInPool: true}
			})

		require.True(t, got.AllModelCapableRateLimited)
	})

	t.Run("nil chain returns the origin verbatim", func(t *testing.T) {
		origin := ModelAvailabilityDiagnosis{HasAccountsInPool: true}
		require.Equal(t, origin, diagnoseAcrossNoAccountFallback(context.Background(), nil, origin,
			func(context.Context, *int64) ModelAvailabilityDiagnosis {
				t.Fatal("hop must not run without a chain")
				return ModelAvailabilityDiagnosis{}
			}))
	})
}

// OpenAI 族走同一条链。
func TestOpenAIDiagnoseModelAvailability_FallbackGroupSupportsModel(t *testing.T) {
	origin, fallback := int64(1), int64(2)
	reset := time.Now().Add(60 * time.Second)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:            10,
				Platform:      PlatformOpenAI,
				Status:        StatusActive,
				Schedulable:   true,
				AccountGroups: []AccountGroup{{GroupID: origin}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-5-mini": "gpt-5-mini"},
				},
			},
			{
				ID:               20,
				Platform:         PlatformOpenAI,
				Status:           StatusActive,
				Schedulable:      true,
				RateLimitResetAt: &reset,
				AccountGroups:    []AccountGroup{{GroupID: fallback}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"gpt-5.3-codex": "gpt-5.3-codex"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	svc := &OpenAIGatewayService{
		accountRepo: repo,
		cfg:         testConfig(),
		schedulerSnapshot: &SchedulerSnapshotService{
			groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{
				origin:   noAccountFallbackGroup(origin, PlatformOpenAI, &fallback),
				fallback: noAccountFallbackGroup(fallback, PlatformOpenAI, nil),
			}},
		},
	}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &origin, "gpt-5.3-codex", PlatformOpenAI)

	require.True(t, diag.HasModelSupport, "the fallback group serves this model")
	require.True(t, diag.AllModelCapableRateLimited)
}

// 起点全池冷却时同样不走链：结论已经是 429，选号刚刚沿链失败过，兜底分组也没有
// 可用账号；沿链只会用兜底分组的冷却时间修正 Retry-After，却要在客户端按
// Retry-After 节奏反复重试的这条响应上每跳多付一次分组读取加一次候选查询。
func TestDiagnoseModelAvailability_SkipsChainWhenOriginFullyCooling(t *testing.T) {
	origin, fallback := int64(1), int64(2)
	resetAt := time.Now().Add(2 * time.Minute)
	repo := &mockAccountRepoForPlatform{
		accounts: []Account{
			{
				ID:               10,
				Platform:         PlatformAnthropic,
				Status:           StatusActive,
				Schedulable:      true,
				RateLimitResetAt: &resetAt,
				AccountGroups:    []AccountGroup{{GroupID: origin}},
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8"},
				},
			},
		},
		accountsByID: map[int64]*Account{},
	}
	groupRepo := &mockGroupRepoForGateway{groups: map[int64]*Group{
		origin:   noAccountFallbackGroup(origin, PlatformAnthropic, &fallback),
		fallback: noAccountFallbackGroup(fallback, PlatformAnthropic, nil),
	}}
	svc := &GatewayService{accountRepo: repo, cfg: testConfig(), groupRepo: groupRepo}

	diag := svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &origin, "claude-opus-4-8", PlatformAnthropic)

	require.True(t, diag.HasModelSupport)
	require.True(t, diag.AllModelCapableRateLimited, "the origin alone already yields the 429 verdict")
	require.NotNil(t, diag.EarliestRateLimitResetAt)
	require.Zero(t, groupRepo.getByIDLiteCalls, "a fully cooling origin must not walk the fallback chain")
}
