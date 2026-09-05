//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 直接导入的 Anthropic setup-token 不参与 refresh_token 续期；只有旧版交换流程写入的
// 带 expires_at 的 8 小时令牌行才续期。
func TestAccountCanRefreshToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{
			name:    "nil account",
			account: nil,
			want:    false,
		},
		{
			name:    "api key account",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey},
			want:    false,
		},
		{
			name:    "oauth account",
			account: &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
			want:    true,
		},
		{
			name: "setup-token imported directly",
			account: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeSetupToken,
				Credentials: map[string]any{"access_token": "sk-ant-oat01-direct", "scope": "user:inference"},
			},
			want: false,
		},
		{
			name: "setup-token with a blank refresh token",
			account: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeSetupToken,
				Credentials: map[string]any{"access_token": "sk-ant-oat01-direct", "refresh_token": "   "},
			},
			want: false,
		},
		{
			// 旧版浏览器交换流程写入的行：8 小时令牌 + refresh_token + expires_at，
			// 只有续期才能活过到期时间。
			name: "anthropic setup-token from the legacy exchange flow",
			account: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeSetupToken,
				Credentials: map[string]any{
					"access_token":  "short-lived",
					"refresh_token": "legacy-refresh",
					"expires_at":    "1800000000",
				},
			},
			want: true,
		},
		{
			// 没有 expires_at 就是直接导入的长期凭据，残留 refresh_token 不能改变语义。
			name: "anthropic setup-token with a stray refresh token but no expiry",
			account: &Account{
				Platform:    PlatformAnthropic,
				Type:        AccountTypeSetupToken,
				Credentials: map[string]any{"access_token": "sk-ant-oat01-direct", "refresh_token": "legacy-refresh"},
			},
			want: false,
		},
		{
			name: "openai setup-token with a refresh token",
			account: &Account{
				Platform:    PlatformOpenAI,
				Type:        AccountTypeSetupToken,
				Credentials: map[string]any{"refresh_token": "rt"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.CanRefreshToken())
		})
	}
}
