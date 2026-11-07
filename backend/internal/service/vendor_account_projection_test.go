package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVendorAccountProjectionPublicMetadata(t *testing.T) {
	a := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusDisabled,
		Credentials: map[string]any{
			"access_token": "secret-access", "refresh_token": "secret-refresh",
			"auth_mode": "personal_access_token", "plan_type": "pro",
			"subscription_expires_at": "2026-11-26T00:00:00Z",
		},
		Extra: map[string]any{
			"privacy_mode": "training_set_failed", "openai_compact_mode": "force_off",
			"openai_compact_supported": false, "openai_compact_checked_at": "2026-10-26T00:00:00Z",
			"private_token": "secret-private", "openai_cloud": true, "openai_excel_bps": true,
			ExcelBPSFailureKey: map[string]any{"policy": map[string]any{"groups": "secret-recovery-groups"}, "grant_fingerprint": "secret-grant-fingerprint"},
		},
	}
	d := vendorAccountDTO(a, nil)
	require.Equal(t, "personal_access_token", d.AuthMode)
	require.Equal(t, "pro", d.PlanType)
	require.Equal(t, "2026-11-26T00:00:00Z", d.SubscriptionExpiresAt)
	require.Equal(t, map[string]any{
		"model_rate_limits": map[string]any{}, "allow_overages": false,
		"privacy_mode": "training_set_failed", "openai_compact_mode": "force_off",
		"openai_compact_supported": false, "openai_compact_checked_at": "2026-10-26T00:00:00Z",
	}, d.Extra)
	require.False(t, d.Schedulable)
	require.Contains(t, d.Blockers, "platform_disabled")
	require.Empty(t, d.AuthorizationModes, "the existing authorization owner must populate modes, not display flags")

	raw, err := json.Marshal(d)
	require.NoError(t, err)
	for _, secret := range []string{"secret-access", "secret-refresh", "secret-private", "secret-recovery-groups", "secret-grant-fingerprint", "credentials", "openai_cloud", "openai_excel_bps"} {
		require.NotContains(t, string(raw), secret)
	}
	require.Contains(t, string(raw), `"openai_compact_supported":false`, "an explicit unsupported result must not be lost")

	d.Extra["privacy_mode"] = "training_off"
	require.Equal(t, "training_set_failed", a.Extra["privacy_mode"], "the response must not alias the account's Extra map")
}

func TestVendorAccountProjectionOmitsInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "absent"},
		{name: "empty and null", extra: map[string]any{
			"privacy_mode": "", "openai_compact_mode": "", "openai_compact_checked_at": "", "openai_compact_supported": nil,
		}},
		{name: "wrong types", extra: map[string]any{
			"privacy_mode":        map[string]any{"token": "malformed-secret"},
			"openai_compact_mode": []string{"malformed-secret"}, "openai_compact_checked_at": 123,
			"openai_compact_supported": "false",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: tc.extra}
			d := vendorAccountDTO(a, nil)
			require.Equal(t, map[string]any{"model_rate_limits": map[string]any{}, "allow_overages": false}, d.Extra)
			require.False(t, d.Schedulable)
			require.Contains(t, d.Blockers, "authorization_required")
			raw, err := json.Marshal(d)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "auth_mode")
			require.NotContains(t, string(raw), "malformed-secret")
		})
	}
}

func TestVendorAccountProjectionDisplayFlagsDoNotAuthorize(t *testing.T) {
	a := &Account{
		Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"auth_mode": "personal_access_token"},
		Extra: map[string]any{
			"privacy_mode": "training_off", "openai_compact_mode": "force_on", "openai_compact_supported": true,
			"openai_cloud": true, "openai_excel_bps": true,
		},
	}
	d := vendorAccountDTO(a, nil)
	require.Equal(t, true, d.Extra["openai_compact_supported"])
	require.False(t, d.Schedulable)
	require.False(t, d.CanRefresh)
	require.Contains(t, d.Blockers, "authorization_required")
	require.Empty(t, d.AuthorizationModes)
	require.Empty(t, d.Authorizations)
}
