//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newSettingServiceForPlatformThresholdTest(seed map[string]string) *SettingService {
	accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
	accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})
	repo := newMockSettingRepo()
	for k, v := range seed {
		repo.data[k] = v
	}
	return NewSettingService(repo, &config.Config{})
}

func TestPlatformSchedulingThresholds_RoundTrip_DefaultsAndStoredValues(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	got := svc.parseSettings(map[string]string{})
	require.Equal(t, map[string]int{
		PlatformOpenAI:                         100,
		PlatformAnthropic:                      100,
		PlatformGrok:                           100,
		SchedulingThresholdScopeAnthropicFable: 100,
	}, got.AccountSchedulingThresholds)

	got = svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":91,"grok":77,"gemini":85,"kiro":99}`,
	})
	require.Equal(t, 91, got.AccountSchedulingThresholds[PlatformOpenAI])
	require.Equal(t, 100, got.AccountSchedulingThresholds[PlatformAnthropic])
	require.Equal(t, 77, got.AccountSchedulingThresholds[PlatformGrok])
	require.NotContains(t, got.AccountSchedulingThresholds, PlatformGemini)
	require.NotContains(t, got.AccountSchedulingThresholds, "kiro")
}

func TestBuildSystemSettingsUpdates_PersistsAccountSchedulingThresholds(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{
		AccountSchedulingThresholds: map[string]int{
			PlatformOpenAI:    91,
			PlatformAnthropic: 88,
			PlatformGrok:      77,
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"openai":91,"anthropic":88,"grok":77,"anthropic_fable":100}`, updates[SettingKeyAccountSchedulingThresholds])
}

func TestValidateAndNormalizeAccountSchedulingThresholds_FillsMissingPlatforms(t *testing.T) {
	normalized, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{
		PlatformOpenAI: 91,
	})
	require.NoError(t, err)
	require.Equal(t, 91, normalized[PlatformOpenAI])
	require.Equal(t, 100, normalized[PlatformAnthropic])
	require.Equal(t, 100, normalized[PlatformGrok])
	require.NotContains(t, normalized, PlatformGemini)
	require.NotContains(t, normalized, "kiro")
	require.NotContains(t, normalized, PlatformAntigravity)
}

func TestValidateAndNormalizeAccountSchedulingThresholds_RejectsUnsupportedPlatforms(t *testing.T) {
	_, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{
		PlatformGemini: 85,
	})
	require.Error(t, err)
}

func TestUpdateSettings_StoresAccountSchedulingThresholds(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AccountSchedulingThresholds: map[string]int{
			PlatformOpenAI:    92,
			PlatformAnthropic: 89,
			PlatformGrok:      76,
		},
	})
	require.NoError(t, err)

	got := svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: svc.settingRepo.(*mockSettingRepo).data[SettingKeyAccountSchedulingThresholds],
	})
	require.Equal(t, 92, got.AccountSchedulingThresholds[PlatformOpenAI])
	require.Equal(t, 89, got.AccountSchedulingThresholds[PlatformAnthropic])
	require.Equal(t, 76, got.AccountSchedulingThresholds[PlatformGrok])
	require.NotContains(t, got.AccountSchedulingThresholds, "kiro")
}

func TestGetAccountSchedulingThresholds_ReadsStoredValue(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":93,"grok":88,"kiro":87}`,
	})

	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())

	require.True(t, resolved)
	require.Equal(t, 93, got[PlatformOpenAI])
	require.Equal(t, 100, got[PlatformAnthropic])
	require.Equal(t, 88, got[PlatformGrok])
	require.NotContains(t, got, "kiro")
}

func TestGetAccountSchedulingThresholds_MissingSettingUsesDefaultsAndNormalCacheTTL(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)
	repo := svc.settingRepo.(*mockSettingRepo)
	repo.getValueErr = ErrSettingNotFound

	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, defaultAccountSchedulingThresholds(), got)
	require.True(t, resolved, "「这一项没配过」是配置本身给出的答案，不是读取失败")
	require.Equal(t, 1, repo.getValueCalls)

	repo.data[SettingKeyAccountSchedulingThresholds] = `{"openai":91}`
	got, resolved = svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, 100, got[PlatformOpenAI], "missing-setting defaults should remain cached for the normal TTL")
	require.True(t, resolved)
	require.Equal(t, 1, repo.getValueCalls)

	cached, ok := accountSchedulingThresholdsCache.Load().(*cachedAccountSchedulingThresholds)
	require.True(t, ok)
	require.Greater(t, cached.expiresAt, time.Now().Add(accountSchedulingThresholdsCacheTTL-time.Second).UnixNano())
}

// 读取失败（DB 抖动、5s 超时）返回的兜底默认与「运维把阈值全关了」取值完全一样，
// 必须靠 resolved 才能分开：解除路径把后者当判据，前者不能。
func TestGetAccountSchedulingThresholds_ReadFailureIsNotResolved(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)
	repo := svc.settingRepo.(*mockSettingRepo)
	repo.getValueErr = errors.New("settings backend unavailable")

	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, defaultAccountSchedulingThresholds(), got)
	require.False(t, resolved)

	cached, ok := accountSchedulingThresholdsCache.Load().(*cachedAccountSchedulingThresholds)
	require.True(t, ok)
	require.False(t, cached.resolved, "ErrorTTL 期内的缓存命中同样不可信")
	require.LessOrEqual(t, cached.expiresAt, time.Now().Add(accountSchedulingThresholdsErrorTTL).UnixNano())

	// 缓存命中路径也要把 resolved 带出来。
	_, resolved = svc.GetAccountSchedulingThresholds(context.Background())
	require.False(t, resolved)
	require.Equal(t, 1, repo.getValueCalls)
}

// 存量值损坏时同样读不出运维意图，不能当成「阈值已关闭」。
func TestGetAccountSchedulingThresholds_ParseFailureIsNotResolved(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":`,
	})

	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, defaultAccountSchedulingThresholds(), got)
	require.False(t, resolved)
}

func TestUpdateSettings_OmittedAccountSchedulingThresholdsDoesNotCacheDefaults(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":85,"grok":88,"kiro":87}`,
	})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		FrontendURL: "https://example.test",
	})
	require.NoError(t, err)

	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())
	require.True(t, resolved)
	require.Equal(t, 85, got[PlatformOpenAI])
	require.Equal(t, 88, got[PlatformGrok])
	require.NotContains(t, got, "kiro")
}

func TestAccountSchedulingThresholds_InvalidStoredValueUsesSameDefaultsInSettingsAndCache(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":0,"grok":88,"kiro":87}`,
	})

	settings := svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":0,"grok":88,"kiro":87}`,
	})
	cached, resolved := svc.GetAccountSchedulingThresholds(context.Background())

	require.True(t, resolved, "值越界会被规范化成 100，但配置确实读到了")
	require.Equal(t, settings.AccountSchedulingThresholds, cached)
	require.Equal(t, 100, cached[PlatformOpenAI])
	require.Equal(t, 88, cached[PlatformGrok])
	require.NotContains(t, cached, "kiro")
}

func TestGetAccountSchedulingThresholds_NilRepoReturnsDefaults(t *testing.T) {
	svc := &SettingService{}
	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())
	require.False(t, resolved, "没有 settingRepo 就读不到配置，兜底默认不能当成运维关掉了阈值")
	require.Equal(t, map[string]int{
		PlatformOpenAI:                         100,
		PlatformAnthropic:                      100,
		PlatformGrok:                           100,
		SchedulingThresholdScopeAnthropicFable: 100,
	}, got)
}

// anthropic_fable 是非平台 scope，与平台 key 共用同一张 map，必须能存能读能校验。
func TestAccountSchedulingThresholds_AcceptsAnthropicFableScope(t *testing.T) {
	normalized, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{
		PlatformAnthropic:                      90,
		SchedulingThresholdScopeAnthropicFable: 50,
	})
	require.NoError(t, err)
	require.Equal(t, 90, normalized[PlatformAnthropic])
	require.Equal(t, 50, normalized[SchedulingThresholdScopeAnthropicFable])

	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"anthropic":90,"anthropic_fable":50,"kiro":87}`,
	})
	got, resolved := svc.GetAccountSchedulingThresholds(context.Background())
	require.True(t, resolved)
	require.Equal(t, 90, got[PlatformAnthropic])
	require.Equal(t, 50, got[SchedulingThresholdScopeAnthropicFable])
	require.NotContains(t, got, "kiro")
}

// 越界值仍回落到 100（不启用），与平台 key 行为一致。
func TestAccountSchedulingThresholds_AnthropicFableScopeOutOfRangeFallsBack(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)
	got := svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"anthropic_fable":0}`,
	})
	require.Equal(t, 100, got.AccountSchedulingThresholds[SchedulingThresholdScopeAnthropicFable])

	_, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{
		SchedulingThresholdScopeAnthropicFable: 0,
	})
	require.Error(t, err)

	_, err = validateAndNormalizeAccountSchedulingThresholds(map[string]int{
		SchedulingThresholdScopeAnthropicFable: 101,
	})
	require.Error(t, err)
}
