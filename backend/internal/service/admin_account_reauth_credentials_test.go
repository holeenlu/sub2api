//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// reAuthAccountRepo 补上换 type 时会走到的 spark 影子查询，其余沿用既有桩。
type reAuthAccountRepo struct {
	*upstreamBillingProbeAccountRepo
}

func (r *reAuthAccountRepo) ListShadowsByParent(_ context.Context, _ int64) ([]*Account, error) {
	return nil, nil
}

func newReAuthAccountRepo(account *Account) *reAuthAccountRepo {
	return &reAuthAccountRepo{&upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}}
}

// 把一个 OAuth 账号重新授权成直接导入的 setup-token 时，上一套凭据不得有任何残留：
// 默认的合并语义会保留 refresh_token 并丢掉 expires_at，账号于是"有长期 token 也有
// 可用的 refresh_token"，一次刷新就会把长期 token 覆盖成 8h 短期 token。
func TestApplyOAuthCredentialsReplacesTokenSet(t *testing.T) {
	repo := newReAuthAccountRepo(&Account{
		ID:       3101,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
			"id_token":      "old-id",
			"expires_at":    float64(1700000000),
			"model_mapping": map[string]any{"claude-opus-5": "claude-opus-5"},
		},
	})

	updated, err := (&adminServiceImpl{accountRepo: repo}).ApplyOAuthCredentials(context.Background(), 3101, &ApplyOAuthCredentialsInput{
		Type: AccountTypeSetupToken,
		Credentials: map[string]any{
			"access_token": "sk-ant-oat01-new",
			"token_type":   "Bearer",
			"scope":        "user:inference",
		},
	})

	require.NoError(t, err)
	require.Equal(t, AccountTypeSetupToken, updated.Type)
	require.Equal(t, "sk-ant-oat01-new", updated.Credentials["access_token"])
	require.NotContains(t, updated.Credentials, "refresh_token")
	require.NotContains(t, updated.Credentials, "id_token")
	require.NotContains(t, updated.Credentials, "expires_at")
	// 账号自身的配置不是凭据的一部分，重新授权不该顺手清掉。
	require.Equal(t, map[string]any{"claude-opus-5": "claude-opus-5"}, updated.Credentials["model_mapping"])

	// 落库的也是替换后的结果，而不只是返回值。
	stored, err := repo.GetByID(context.Background(), 3101)
	require.NoError(t, err)
	require.NotContains(t, stored.Credentials, "refresh_token")
	require.Equal(t, AccountTypeSetupToken, stored.Type)
}

// OAuth → OAuth 完整换发时 incoming 自带全套字段，结果与覆盖一致，非 token 键保留。
func TestApplyOAuthCredentialsKeepsNonTokenKeysOnFullReAuthorization(t *testing.T) {
	repo := newReAuthAccountRepo(&Account{
		ID:       3102,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":     "old-access",
			"refresh_token":    "old-refresh",
			"expires_at":       float64(1700000000),
			"header_overrides": map[string]any{"x-foo": "bar"},
		},
	})

	updated, err := (&adminServiceImpl{accountRepo: repo}).ApplyOAuthCredentials(context.Background(), 3102, &ApplyOAuthCredentialsInput{
		Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_at":    float64(1800000000),
		},
	})

	require.NoError(t, err)
	require.Equal(t, "new-access", updated.Credentials["access_token"])
	require.Equal(t, "new-refresh", updated.Credentials["refresh_token"])
	require.Equal(t, float64(1800000000), updated.Credentials["expires_at"])
	require.Equal(t, map[string]any{"x-foo": "bar"}, updated.Credentials["header_overrides"])
}

// PAT 账号改用普通 ChatGPT OAuth 重新授权：auth_mode 残留会让
// IsOpenAIPersonalAccessToken 恒为 true、NeedsRefresh 恒 false，换来的 8h token
// 永不续期；organization_id / chatgpt_account_id 残留会带着上一次授权的身份打上游。
func TestApplyOAuthCredentialsDropsPersonalAccessTokenMode(t *testing.T) {
	repo := newReAuthAccountRepo(&Account{
		ID:       3105,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":       "pat-access",
			"auth_mode":          OpenAIAuthModePersonalAccessToken,
			"openai_auth_mode":   "personal_access_token",
			"organization_id":    "org-old",
			"chatgpt_account_id": "acct-old",
			"plan_type":          "enterprise",
			"model_mapping":      map[string]any{"gpt-5": "gpt-5"},
		},
	})

	updated, err := (&adminServiceImpl{accountRepo: repo}).ApplyOAuthCredentials(context.Background(), 3105, &ApplyOAuthCredentialsInput{
		Type: AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":  "chatgpt-access",
			"refresh_token": "chatgpt-refresh",
			"expires_at":    "2026-09-02T00:00:00Z",
		},
	})

	require.NoError(t, err)
	require.False(t, updated.IsOpenAIPersonalAccessToken())
	require.NotContains(t, updated.Credentials, "auth_mode")
	require.NotContains(t, updated.Credentials, "openai_auth_mode")
	require.NotContains(t, updated.Credentials, "organization_id")
	require.NotContains(t, updated.Credentials, "chatgpt_account_id")
	require.NotContains(t, updated.Credentials, "plan_type")
	require.Equal(t, map[string]any{"gpt-5": "gpt-5"}, updated.Credentials["model_mapping"])

	stored, err := repo.GetByID(context.Background(), 3105)
	require.NoError(t, err)
	require.NotContains(t, stored.Credentials, "auth_mode")
}

func TestApplyOAuthCredentialsRejectsNonOAuthAccountAndEmptyInput(t *testing.T) {
	repo := newReAuthAccountRepo(&Account{
		ID:          3103,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Credentials: map[string]any{"api_key": "sk-old"},
	})
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.ApplyOAuthCredentials(context.Background(), 3103, &ApplyOAuthCredentialsInput{
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "new"},
	})
	require.ErrorContains(t, err, "NOT_OAUTH")

	_, err = svc.ApplyOAuthCredentials(context.Background(), 3103, &ApplyOAuthCredentialsInput{Type: AccountTypeOAuth})
	require.ErrorContains(t, err, "INVALID_OAUTH_CREDENTIALS")

	stored, getErr := repo.GetByID(context.Background(), 3103)
	require.NoError(t, getErr)
	require.Equal(t, "sk-old", stored.Credentials["api_key"])
}

// 普通编辑保持原语义：前端提交的是脱敏后的全对象，缺失的敏感键必须保留，
// 否则一次改名就会清空账号凭据。
func TestUpdateAccountKeepsMergeSemanticsForOrdinaryEdit(t *testing.T) {
	repo := newReAuthAccountRepo(&Account{
		ID:       3104,
		Platform: PlatformAnthropic,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":  "old-access",
			"refresh_token": "old-refresh",
		},
	})

	updated, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), 3104, &UpdateAccountInput{
		Credentials: map[string]any{"model_mapping": map[string]any{"a": "b"}},
	})

	require.NoError(t, err)
	require.Equal(t, "old-access", updated.Credentials["access_token"])
	require.Equal(t, "old-refresh", updated.Credentials["refresh_token"])
	require.Equal(t, map[string]any{"a": "b"}, updated.Credentials["model_mapping"])
}
