package dto

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 「能否刷新」的规则只有后端一份，通过 can_refresh_token 下发给前端。
func TestAccountFromServiceShallow_CanRefreshToken(t *testing.T) {
	tests := []struct {
		name    string
		account *service.Account
		want    bool
	}{
		{
			name:    "oauth account",
			account: &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth},
			want:    true,
		},
		{
			name: "setup-token imported directly",
			account: &service.Account{
				Platform:    service.PlatformAnthropic,
				Type:        service.AccountTypeSetupToken,
				Credentials: map[string]any{"access_token": "sk-ant-oat01-direct"},
			},
			want: false,
		},
		{
			name: "anthropic setup-token with legacy refresh fields",
			account: &service.Account{
				Platform:    service.PlatformAnthropic,
				Type:        service.AccountTypeSetupToken,
				Credentials: map[string]any{"access_token": "short", "refresh_token": "legacy-refresh"},
			},
			want: false,
		},
		{
			name:    "api key account",
			account: &service.Account{Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey},
			want:    false,
		},
		{
			// 影子账号不持凭据，刷新对其无效。
			name: "spark shadow",
			account: &service.Account{
				Platform:        service.PlatformOpenAI,
				Type:            service.AccountTypeOAuth,
				ParentAccountID: func() *int64 { id := int64(7); return &id }(),
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AccountFromServiceShallow(tt.account)
			require.NotNil(t, got)
			require.Equal(t, tt.want, got.CanRefreshToken)
		})
	}
}
