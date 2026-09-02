//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergePreservingSensitiveCreds_PreservesSensitiveWhenIncomingMissing(t *testing.T) {
	existing := map[string]any{
		"refresh_token": "rt-old",
		"access_token":  "at-old",
		"api_key":       "sk-old",
		"base_url":      "https://old.example.com",
	}
	incoming := map[string]any{
		"base_url":      "https://new.example.com",
		"model_mapping": map[string]any{"foo": "bar"},
	}

	out := MergePreservingSensitiveCreds(existing, incoming)

	require.Equal(t, "rt-old", out["refresh_token"], "incoming 没传 refresh_token，应保留 existing")
	require.Equal(t, "at-old", out["access_token"])
	require.Equal(t, "sk-old", out["api_key"])
	require.Equal(t, "https://new.example.com", out["base_url"], "非敏感键由 incoming 决定")
	require.Equal(t, map[string]any{"foo": "bar"}, out["model_mapping"])
}

func TestMergePreservingSensitiveCreds_OverwritesWhenIncomingProvidesSensitive(t *testing.T) {
	existing := map[string]any{
		"refresh_token": "rt-old",
		"api_key":       "sk-old",
	}
	incoming := map[string]any{
		"refresh_token": "rt-new",
		// 显式没传 api_key —— 应保留
	}
	out := MergePreservingSensitiveCreds(existing, incoming)
	require.Equal(t, "rt-new", out["refresh_token"], "incoming 显式传入应覆盖")
	require.Equal(t, "sk-old", out["api_key"], "incoming 没传应保留")
}

func TestMergePreservingSensitiveCreds_DoesNotMutateInputs(t *testing.T) {
	existing := map[string]any{"refresh_token": "rt"}
	incoming := map[string]any{"base_url": "x"}

	_ = MergePreservingSensitiveCreds(existing, incoming)

	require.Equal(t, "rt", existing["refresh_token"])
	require.NotContains(t, existing, "base_url")
	require.Equal(t, "x", incoming["base_url"])
	require.NotContains(t, incoming, "refresh_token")
}

func TestMergePreservingSensitiveCreds_NilInputs(t *testing.T) {
	out := MergePreservingSensitiveCreds(nil, map[string]any{"base_url": "x"})
	require.Equal(t, "x", out["base_url"])
	require.NotContains(t, out, "refresh_token")

	out2 := MergePreservingSensitiveCreds(map[string]any{"refresh_token": "rt"}, nil)
	require.Equal(t, "rt", out2["refresh_token"])
}

func TestMergePreservingSensitiveCreds_NonSensitiveDeletionAllowed(t *testing.T) {
	existing := map[string]any{
		"refresh_token": "rt",
		"base_url":      "https://old",
		"project_id":    "p1",
	}
	incoming := map[string]any{
		"base_url": "https://new",
		// 不带 project_id —— 等同删除（非敏感键由 incoming 决定）
	}
	out := MergePreservingSensitiveCreds(existing, incoming)
	require.Equal(t, "rt", out["refresh_token"], "敏感键保留")
	require.Equal(t, "https://new", out["base_url"])
	require.NotContains(t, out, "project_id", "非敏感键 incoming 不传 = 删除")
}

func TestIsSensitiveCredentialKey(t *testing.T) {
	require.True(t, IsSensitiveCredentialKey("refresh_token"))
	require.True(t, IsSensitiveCredentialKey("api_key"))
	require.True(t, IsSensitiveCredentialKey("private_key"))
	require.False(t, IsSensitiveCredentialKey("base_url"))
	require.False(t, IsSensitiveCredentialKey(""))
	require.False(t, IsSensitiveCredentialKey("model_mapping"))
}

// 重新授权换发的是一整套 token。默认的"缺失即保留"语义会留下上一套的
// refresh_token（敏感键）而丢掉 expires_at（非敏感键），账号于是同时持有长期
// token 与一个还能用的 refresh_token——一次刷新就会用后者换回短期 token 覆盖前者。
func TestReplaceOAuthTokenCredentials_DropsPreviousTokenSet(t *testing.T) {
	existing := map[string]any{
		"access_token":  "old-access",
		"refresh_token": "old-refresh",
		"id_token":      "old-id",
		"expires_at":    float64(1700000000),
		"token_type":    "Bearer",
		"scope":         "user:profile user:inference",
		// 账号自身的配置不属于 token 集，必须留下。
		"model_mapping":    map[string]any{"claude-opus-5": "claude-opus-5"},
		"header_overrides": map[string]any{"x-foo": "bar"},
	}
	incoming := map[string]any{
		"access_token": "sk-ant-oat01-new",
		"token_type":   "Bearer",
		"scope":        "user:inference",
	}

	got := ReplaceOAuthTokenCredentials(existing, incoming)

	require.Equal(t, "sk-ant-oat01-new", got["access_token"])
	require.Equal(t, "user:inference", got["scope"])
	require.NotContains(t, got, "refresh_token")
	require.NotContains(t, got, "id_token")
	require.NotContains(t, got, "expires_at")
	require.Equal(t, map[string]any{"claude-opus-5": "claude-opus-5"}, got["model_mapping"])
	require.Equal(t, map[string]any{"x-foo": "bar"}, got["header_overrides"])

	// 对照：编辑账号的合并语义正是问题的来源，不能用于换发凭据。
	merged := MergePreservingSensitiveCreds(existing, incoming)
	require.Equal(t, "old-refresh", merged["refresh_token"])
	require.NotContains(t, merged, "expires_at")

	// 不修改入参。
	require.Equal(t, "old-refresh", existing["refresh_token"])
	require.Len(t, incoming, 3)
}

// 一次授权的产物不止 token：auth_mode / organization_id / chatgpt_account_id 等身份与
// 模式键同样只对这一次授权有效。留下它们，账号会带着上一次授权的身份与模式转发。
func TestReplaceOAuthTokenCredentials_DropsPreviousIdentityAndMode(t *testing.T) {
	existing := map[string]any{
		"access_token":               "pat-access",
		"expires_in":                 "28800",
		"auth_mode":                  OpenAIAuthModePersonalAccessToken,
		"openai_auth_mode":           "personal_access_token",
		"organization_id":            "org-old",
		"chatgpt_account_id":         "acct-old",
		"chatgpt_user_id":            "user-old",
		"chatgpt_account_is_fedramp": true,
		"plan_type":                  "enterprise",
		"subscription_expires_at":    "2026-01-01T00:00:00Z",
		"email":                      "old@example.com",
		"client_id":                  "client-old",
		// 与授权无关的账号配置必须留下。
		"model_mapping":    map[string]any{"gpt-5": "gpt-5"},
		"header_overrides": map[string]any{"x-foo": "bar"},
		"base_url":         "https://api.example.com",
	}
	incoming := map[string]any{
		"access_token":  "chatgpt-access",
		"refresh_token": "chatgpt-refresh",
		"expires_at":    "2026-09-02T00:00:00Z",
		"email":         "new@example.com",
	}

	got := ReplaceOAuthTokenCredentials(existing, incoming)

	// PAT 模式残留会让 IsOpenAIPersonalAccessToken 恒为 true，NeedsRefresh 恒 false，
	// 换来的 8h token 永不续期。
	require.NotContains(t, got, "auth_mode")
	require.NotContains(t, got, "openai_auth_mode")
	// 组织 / 账号身份残留会带着上一次授权的身份打上游，换来 401/403。
	require.NotContains(t, got, "organization_id")
	require.NotContains(t, got, "chatgpt_account_id")
	require.NotContains(t, got, "chatgpt_user_id")
	require.NotContains(t, got, "chatgpt_account_is_fedramp")
	require.NotContains(t, got, "plan_type")
	require.NotContains(t, got, "subscription_expires_at")
	require.NotContains(t, got, "client_id")
	require.NotContains(t, got, "expires_in")
	require.Equal(t, "new@example.com", got["email"])
	require.Equal(t, "chatgpt-access", got["access_token"])
	require.Equal(t, map[string]any{"gpt-5": "gpt-5"}, got["model_mapping"])
	require.Equal(t, map[string]any{"x-foo": "bar"}, got["header_overrides"])
	require.Equal(t, "https://api.example.com", got["base_url"])
}

// 完整换发（OAuth → OAuth）时 incoming 带全套字段，结果与直接覆盖一致。
func TestReplaceOAuthTokenCredentials_KeepsFullTokenSet(t *testing.T) {
	got := ReplaceOAuthTokenCredentials(
		map[string]any{"access_token": "old", "refresh_token": "old-refresh", "expires_at": float64(1)},
		map[string]any{"access_token": "new", "refresh_token": "new-refresh", "expires_at": float64(1800000000)},
	)

	require.Equal(t, map[string]any{
		"access_token":  "new",
		"refresh_token": "new-refresh",
		"expires_at":    float64(1800000000),
	}, got)
}

func TestReplaceOAuthTokenCredentials_NilInputs(t *testing.T) {
	require.Equal(t, map[string]any{"access_token": "new"}, ReplaceOAuthTokenCredentials(nil, map[string]any{"access_token": "new"}))
	require.Empty(t, ReplaceOAuthTokenCredentials(map[string]any{"refresh_token": "old"}, nil))
}
