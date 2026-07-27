package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateAnthropicOAuthCredentials(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		accountType string
		credentials map[string]any
		wantErr     bool
	}{
		{
			name:        "accepts bearer token for setup token",
			platform:    PlatformAnthropic,
			accountType: AccountTypeSetupToken,
			credentials: map[string]any{"access_token": "claude-code-token"},
		},
		{
			name:        "rejects missing bearer token",
			platform:    PlatformAnthropic,
			accountType: AccountTypeSetupToken,
			credentials: map[string]any{},
			wantErr:     true,
		},
		{
			name:        "rejects mixed api key",
			platform:    PlatformAnthropic,
			accountType: AccountTypeOAuth,
			credentials: map[string]any{"access_token": "token", "api_key": "key"},
			wantErr:     true,
		},
		{
			name:        "does not affect api key accounts",
			platform:    PlatformAnthropic,
			accountType: AccountTypeAPIKey,
			credentials: map[string]any{"api_key": "key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAnthropicOAuthCredentials(tt.platform, tt.accountType, tt.credentials)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
