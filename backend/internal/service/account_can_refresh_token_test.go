//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Anthropic setup-token 永远不参与 refresh_token 续期；历史残留字段不能改变凭据语义。
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
			name: "anthropic setup-token with legacy refresh fields",
			account: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeSetupToken,
				Credentials: map[string]any{
					"access_token":  "short-lived",
					"refresh_token": "legacy-refresh",
					"expires_at":    "1800000000",
				},
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
