package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/stretchr/testify/require"
)

// This only proves handshake authentication, not endpoint eligibility, routing,
// principal/resource access or a SendPermit. Cloud is an execution choice, not
// a replacement for proof of the exact credential used on a native handshake.
func TestNativeWSHandshakeCloudConfigurationKeepsCredentialProof(t *testing.T) {
	now := time.Now()
	for _, cloud := range []bool{false, true} {
		name := "native_account"
		if cloud {
			name = "cloud_configured"
		}
		t.Run(name, func(t *testing.T) {
			owner := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Extra:       map[string]any{OpenAICloudEnabledKey: cloud},
				Credentials: map[string]any{"access_token": "native-unit-grant", "expires_at": now.Add(time.Hour).Format(time.RFC3339), "chatgpt_account_id": "native-unit-owner"}}
			headers := make(http.Header)
			headers.Set("Authorization", "Bearer native-unit-grant")
			setOpenAIChatGPTAccountHeaders(headers, owner)
			require.Equal(t, cloud, owner.IsOpenAICloudConfigured())
			require.NoError(t, validateNativeWSHandshakeAuthentication(owner, headers, now))
			wrongToken := headers.Clone()
			wrongToken.Set("Authorization", "Bearer other-unit-grant")
			require.ErrorIs(t, validateNativeWSHandshakeAuthentication(owner, wrongToken, now), ErrNativeDispatchGrantUnproven)
			wrongOwner := headers.Clone()
			wrongOwner.Set("chatgpt-account-id", "other-unit-owner")
			require.ErrorIs(t, validateNativeWSHandshakeAuthentication(owner, wrongOwner, now), ErrNativeDispatchGrantUnproven)
			require.ErrorIs(t, validateNativeWSHandshakeAuthentication(owner, headers, now.Add(2*time.Hour)), ErrNativeDispatchGrantUnproven)
		})
	}
}

func TestNativeWSActualModeCannotBeInferredFromClientTransport(t *testing.T) {
	f, handshake, identity := nativeWSHeldFixture(t)
	// Even a valid held native handshake and the same ws client transport may
	// not authorize a prepared attempt from the Cloud/BPS execution domain.
	for _, mode := range []string{UsageAuthorizationModeCloud, "bps"} {
		t.Run(mode, func(t *testing.T) {
			attempt := identity
			attempt.Mode = mode
			_, err := f.gate.revalidateWS(t.Context(), attempt, handshake, func() bool { return true }, nil)
			require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity)
		})
	}
}
