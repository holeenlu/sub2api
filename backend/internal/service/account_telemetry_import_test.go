//go:build unit

package service

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func telemetryDeviceFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/account_telemetry_device.json")
	require.NoError(t, err)
	return raw
}

func TestAccountTelemetryDeviceImportPreservesDeviceAndResetsSending(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	svc.activateAccountTelemetry(t.Context(), account, nil)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 3)
	old := *account
	old.Extra = maps.Clone(account.Extra)
	oldIdentity, oldHeaders, err := claudeTelemetryIdentityForActivity(&old, AccountTelemetryActivity{})
	require.NoError(t, err)
	state, err := svc.ImportAccountTelemetry(t.Context(), account, telemetryDeviceFixture(t))
	require.NoError(t, err)
	require.Equal(t, "ready", state.Status)
	require.NotEqual(t, ReadAccountTelemetry(&old).Revision, state.Revision)
	require.Equal(t, "win32", state.DeviceInstallation.Platform)
	require.Equal(t, "Windows", claudeTelemetryDeviceHeaders(state.DeviceInstallation, state.SessionID).Get("X-Stainless-OS"))
	require.Equal(t, strings.Repeat("b", 64), state.DeviceInstallation.DeviceID)
	require.Empty(t, cache.activity.DeviceID, "old request binding must be cleared")
	require.EqualError(t, svc.sendClaudeTelemetry(t.Context(), ClaudeTelemetryBatchPath, &claudeTelemetryDispatch{account: &old, headers: oldHeaders, identity: oldIdentity}, []byte(`{}`)), "telemetry_profile_changed")
	require.Len(t, upstream.requests, 3)
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 6, "a new imported identity gets init, event and eval immediately")
	require.Equal(t, "win32", gjson.GetBytes(upstream.bodies[3], "events.0.event_data.env.platform").String())
	require.Equal(t, strings.Repeat("b", 64), gjson.GetBytes(upstream.bodies[5], "attributes.deviceID").String())
	require.Equal(t, state.Revision, cache.state.ProfileRevision)
	// A concurrent import holding the previous revision cannot overwrite this one.
	saved, err := svc.accountRepo.(AccountTelemetryRepository).SaveAccountTelemetry(t.Context(), account.ID, "", ReadAccountTelemetry(&old).Revision, ReadAccountTelemetry(&old))
	require.NoError(t, err)
	require.False(t, saved)
}

func TestAccountTelemetryImportedDeviceRebindsWithSendingOff(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	state, err := svc.ImportAccountTelemetry(t.Context(), account, telemetryDeviceFixture(t))
	require.NoError(t, err)
	account.Credentials["account_uuid"] = "changed"
	bound, err := svc.bindAccountTelemetry(t.Context(), account)
	require.NoError(t, err)
	require.Equal(t, state.DeviceInstallation, bound.DeviceInstallation)
	require.NotEqual(t, state.SessionID, bound.SessionID)
	require.False(t, cache.active)
	require.Empty(t, upstream.requests)
	raw, err := json.Marshal(bound)
	require.NoError(t, err)
	for _, key := range []string{"profile", "payload_json", "headers", "retry_at", "error_code", "generated_at"} {
		require.False(t, gjson.GetBytes(raw, key).Exists())
	}
}

func TestAccountTelemetryReimportDiscardsPreviousRevisionActivity(t *testing.T) {
	svc, account, cache, upstream := newAccountTelemetryFixture(t)
	old := ReadAccountTelemetry(account)
	stale := AccountTelemetryActivity{Revision: old.Revision, DeviceID: old.DeviceInstallation.DeviceID, SessionID: "old-wire-session"}
	state, err := svc.ImportAccountTelemetry(t.Context(), account, mustMarshalTelemetryTest(t, old.DeviceInstallation))
	require.NoError(t, err)
	// Reproduce a tick that read the old activity before loading the new account.
	cache.activity = stale
	svc.runAccountTelemetryTick(t.Context(), account.ID)
	require.Len(t, upstream.requests, 3)
	for i, request := range upstream.requests {
		require.Equal(t, state.SessionID, request.Header.Get("X-Claude-Code-Session-Id"))
		require.NotContains(t, string(upstream.bodies[i]), stale.SessionID)
	}
	require.Equal(t, state.SessionID, gjson.GetBytes(upstream.bodies[0], "events.0.event_data.session_id").String())
	require.Equal(t, state.SessionID, gjson.GetBytes(upstream.bodies[2], "attributes.sessionId").String())
	stale.Revision = state.Revision
	id, _, err := claudeTelemetryIdentityForActivity(account, stale)
	require.NoError(t, err)
	require.Equal(t, stale.SessionID, id.SessionID, "matching current dispatch still supplies its actual session")
}

func mustMarshalTelemetryTest(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestAccountTelemetryImportRejectsAccountBearingOrInvalidBundles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"account identity", func(b map[string]any) { b["account"] = map[string]any{"account_uuid": "account-fixture"} }},
		{"missing CLI version", func(b map[string]any) { delete(b, "cli_version") }},
		{"invalid CLI version", func(b map[string]any) { b["cli_version"] = "2.1.296\r\nX-Test: invalid" }},
		{"missing machine", func(b map[string]any) { delete(b, "machine_id") }},
		{"credential field", func(b map[string]any) { b["access_token"] = "must-not-be-saved" }},
		{"credential environment", func(b map[string]any) { b["environment"].(map[string]any)["authorization"] = "must-not-be-saved" }},
		{"event upload", func(b map[string]any) { b["events"] = []any{map[string]any{"event_type": "ClaudeCodeInternalEvent"}} }},
		{"unsupported format", func(b map[string]any) { b["version"] = 1 }},
		{"invalid device", func(b map[string]any) { b["device_id"] = "random-device" }},
		{"invalid platform", func(b map[string]any) { b["platform"] = "other" }},
		{"invalid session", func(b map[string]any) { b["session_id"] = "invalid" }},
		{"empty session", func(b map[string]any) { b["session_id"] = "00000000-0000-0000-0000-000000000000" }},
		{"invalid env value", func(b map[string]any) { b["environment"].(map[string]any)["shell"] = map[string]any{"token": "secret"} }},
		{"fractional hardware", func(b map[string]any) { b["environment"].(map[string]any)["cpu_cores"] = 1.5 }},
		{"header injection", func(b map[string]any) { b["locale"] = "en-US\r\nAuthorization: secret" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, account, _, upstream := newAccountTelemetryFixture(t)
			before := ReadAccountTelemetry(account)
			var bundle map[string]any
			require.NoError(t, json.Unmarshal(telemetryDeviceFixture(t), &bundle))
			tc.change(bundle)
			raw, err := json.Marshal(bundle)
			require.NoError(t, err)
			_, err = svc.ImportAccountTelemetry(t.Context(), account, raw)
			require.Error(t, err)
			require.Equal(t, before, ReadAccountTelemetry(account))
			require.Empty(t, upstream.requests)
			require.Zero(t, svc.accountRepo.(*accountTelemetryRepoStub).saves)
		})
	}
	svc, account, _, _ := newAccountTelemetryFixture(t)
	_, err := svc.ImportAccountTelemetry(t.Context(), account, append(telemetryDeviceFixture(t), []byte(` {}`)...))
	require.Error(t, err)
	_, err = svc.ImportAccountTelemetry(t.Context(), account, []byte(strings.Repeat(" ", int(AccountTelemetryMaxBodyBytes)+1)))
	require.Error(t, err)
	account.ExpiresAt = new(time.Time)
	*account.ExpiresAt = time.Now().Add(-time.Minute)
	_, err = svc.ImportAccountTelemetry(context.Background(), account, telemetryDeviceFixture(t))
	require.EqualError(t, err, "telemetry_account_unavailable")
}

func TestAccountTelemetryImportsAllDevicePlatforms(t *testing.T) {
	oracle, err := os.ReadFile("testdata/account_telemetry_vm2api.json")
	require.NoError(t, err)
	for _, platform := range []string{"win32", "linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			svc, account, _, _ := newAccountTelemetryFixture(t)
			var b map[string]any
			require.NoError(t, json.Unmarshal(telemetryDeviceFixture(t), &b))
			b["platform"] = platform
			raw, err := json.Marshal(b)
			require.NoError(t, err)
			_, err = svc.ImportAccountTelemetry(t.Context(), account, raw)
			require.NoError(t, err)
			id, _, err := claudeTelemetryIdentityForActivity(account, AccountTelemetryActivity{})
			require.NoError(t, err)
			require.Equal(t, platform, id.Platform)
			require.Equal(t, platform, id.Env["platform"])
			require.Equal(t, account.GetCredential("account_uuid"), id.AccountUUID)
			require.JSONEq(t, gjson.GetBytes(oracle, "device_envs."+platform).Raw, string(mustMarshalTelemetryTest(t, id.Env)))
		})
	}
}

func TestAccountTelemetryNoLoginBundleBindsOnlyCurrentGatewayAccount(t *testing.T) {
	sessions := map[string]bool{}
	for _, owner := range []string{"one", "two"} {
		svc, account, cache, upstream := newAccountTelemetryFixture(t)
		svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
		account.Credentials["account_uuid"] = "account-" + owner
		account.Credentials["org_uuid"] = "org-" + owner
		account.Credentials["email"] = owner + "@example.test"
		account.Credentials["subscription_type"] = "max"
		PrepareAccountTelemetryImport(account)
		state, err := svc.ImportAccountTelemetry(t.Context(), account, telemetryDeviceFixture(t))
		require.NoError(t, err)
		require.NoError(t, ValidateAccountTelemetry(account.Extra))
		id, headers, err := claudeTelemetryIdentityForActivity(account, AccountTelemetryActivity{})
		require.NoError(t, err)
		require.Equal(t, "2.1.296", id.Version)
		require.Equal(t, strings.Repeat("b", 64), id.DeviceID)
		require.Equal(t, "account-"+owner, id.AccountUUID)
		require.Equal(t, "org-"+owner, id.OrgUUID)
		require.Equal(t, owner+"@example.test", id.Email)
		require.Equal(t, "max", id.SubscriptionType)
		require.Empty(t, headers.Get("Authorization"))
		require.NotContains(t, string(id.event(true, time.Now(), time.Now())), account.GetCredential("access_token"))
		require.False(t, sessions[state.SessionID])
		sessions[state.SessionID] = true
		account.Credentials["account_uuid"] = "replacement-" + owner
		delete(account.Credentials, "email")
		delete(account.Credentials, "org_uuid")
		updated, err := svc.bindAccountTelemetry(t.Context(), account)
		require.NoError(t, err)
		rebound, _, err := claudeTelemetryIdentityForActivity(account, AccountTelemetryActivity{})
		require.NoError(t, err)
		require.Equal(t, "replacement-"+owner, rebound.AccountUUID)
		require.Empty(t, rebound.Email)
		require.Empty(t, rebound.OrgUUID)
		require.NotEqual(t, state.SessionID, updated.SessionID)
		require.Equal(t, state.DeviceInstallation, updated.DeviceInstallation)
		require.False(t, cache.active)
		require.Empty(t, upstream.requests)
	}
}

func TestAccountTelemetryExistingV2ImportBindsWithoutLegacyProfile(t *testing.T) {
	svc, account, _, upstream := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	old := ReadAccountTelemetry(account)
	account.Extra[AccountTelemetryExtraKey] = map[string]any{"status": "ready", "revision": old.Revision, "owner": old.Owner, "device_installation": old.DeviceInstallation, "profile": map[string]any{"source": "device-import", "payload_json": "untrusted old event"}}
	svc.scanAccountTelemetry(t.Context(), true)
	state := ReadAccountTelemetry(account)
	require.Equal(t, old.DeviceInstallation, state.DeviceInstallation)
	require.NotEmpty(t, state.SessionID)
	require.NoError(t, ValidateAccountTelemetry(account.Extra))
	require.Empty(t, upstream.requests)
	raw := mustMarshalTelemetryTest(t, account.Extra[AccountTelemetryExtraKey])
	require.False(t, gjson.GetBytes(raw, "profile").Exists())
}

func TestAccountTelemetryStandaloneExporterBundle(t *testing.T) {
	path := os.Getenv("SUB2API_TEST_DEVICE_BUNDLE")
	if path == "" {
		t.Skip("set SUB2API_TEST_DEVICE_BUNDLE for actual standalone tool output")
	}
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var facts AccountTelemetryBundle
	require.NoError(t, json.Unmarshal(raw, &facts))
	svc, account, _, _ := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	state, err := svc.ImportAccountTelemetry(t.Context(), account, raw)
	require.NoError(t, err)
	require.Equal(t, "ready", state.Status)
	require.Equal(t, facts.DeviceID, state.DeviceInstallation.DeviceID)
	require.Equal(t, facts.CLIVersion, state.DeviceInstallation.CLIVersion)
	require.Equal(t, facts.Platform, state.DeviceInstallation.Platform)
	id, _, err := claudeTelemetryIdentityForActivity(account, AccountTelemetryActivity{})
	require.NoError(t, err)
	require.Equal(t, account.GetCredential("account_uuid"), id.AccountUUID)
}
