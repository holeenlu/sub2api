//go:build unit

package admin

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// stubClaudeOAuthClient 只实现刷新一条路径，其余方法不会被 refreshSingleAccount 触达。
type stubClaudeOAuthClient struct {
	refreshCalls int
}

func (c *stubClaudeOAuthClient) GetOrganizationUUID(context.Context, string, string) (string, error) {
	return "", nil
}

func (c *stubClaudeOAuthClient) GetAuthorizationCode(context.Context, string, string, string, string, string, string) (string, error) {
	return "", nil
}

func (c *stubClaudeOAuthClient) ExchangeCodeForToken(context.Context, string, string, string, string, bool) (*oauth.TokenResponse, error) {
	return nil, nil
}

func (c *stubClaudeOAuthClient) RefreshToken(context.Context, string, string) (*oauth.TokenResponse, error) {
	c.refreshCalls++
	return &oauth.TokenResponse{
		AccessToken:  "refreshed-access-token",
		RefreshToken: "rotated-refresh-token",
		TokenType:    "Bearer",
		ExpiresIn:    28800,
		Scope:        "user:inference",
	}, nil
}

// 直接粘贴导入的 `claude setup-token` 是长期凭据，没有 refresh_token，续期无从谈起：
// 手动刷新入口必须在打上游之前就拒绝。
func TestRefreshSingleAccountRejectsImportedSetupToken(t *testing.T) {
	t.Parallel()

	client := &stubClaudeOAuthClient{}
	handler := NewAccountHandler(newStubAdminService(), service.NewOAuthService(nil, client), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	_, _, err := handler.refreshSingleAccount(context.Background(), &service.Account{
		ID:          1,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Status:      service.StatusActive,
		Credentials: map[string]any{"access_token": "sk-ant-oat01-direct", "token_type": "Bearer", "scope": "user:inference"},
	})

	require.Error(t, err)
	require.Equal(t, "SETUP_TOKEN_NO_REFRESH", infraerrors.Reason(err))
	require.Zero(t, client.refreshCalls)
}

// 历史记录即使残留 refresh_token，也不能把 Anthropic setup-token 误当成可刷新 OAuth。
func TestRefreshSingleAccountRejectsLegacySetupTokenRefreshToken(t *testing.T) {
	t.Parallel()

	client := &stubClaudeOAuthClient{}
	handler := NewAccountHandler(newStubAdminService(), service.NewOAuthService(nil, client), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	_, _, err := handler.refreshSingleAccount(context.Background(), &service.Account{
		ID:          2,
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeSetupToken,
		Status:      service.StatusActive,
		Credentials: map[string]any{"access_token": "legacy", "refresh_token": "legacy-refresh", "expires_at": float64(1_800_000_000)},
	})

	require.Error(t, err)
	require.Equal(t, "SETUP_TOKEN_NO_REFRESH", infraerrors.Reason(err))
	require.Zero(t, client.refreshCalls)
}
