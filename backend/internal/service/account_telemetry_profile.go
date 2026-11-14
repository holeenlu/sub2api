package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

func telemetryCredential(account *Account, names ...string) string {
	for _, name := range names {
		if v := account.GetCredential(name); v != "" {
			return v
		}
		if v := account.GetExtraString(name); v != "" {
			return v
		}
	}
	return ""
}

// Shared with prompt-template environment cleanup.
func cleanTelemetryEnvironment(data map[string]json.RawMessage) {
	for _, key := range []string{"additional_metadata", "betas", "model", "baseUrl", "base_url", "gateway", "apiBaseUrlHost", "hostname", "host_name", "authorization", "cookie", "x-api-key", "proxy-authorization", "machine_id", "machine-id", "kernel_release", "os_pretty", "os_id", "runtime_kind", "node_version", "is_running_with_bun"} {
		delete(data, key)
	}
}

// Inference can use account-free device facts immediately, even while an
// account identity change is being rebound to a new gateway-owned session.
func (s *IdentityService) GetAccountFingerprint(ctx context.Context, account *Account, headers http.Header) (*Fingerprint, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("account identity cache unavailable")
	}
	fp, err := s.GetOrCreateFingerprint(ctx, account.ID, headers)
	if err != nil {
		return nil, err
	}
	copyFP := *fp
	device := ReadAccountTelemetry(account).DeviceInstallation
	if device == nil {
		return &copyFP, nil
	}
	copyFP.ClientID = officialTelemetryDevice(device.DeviceID)
	importedHeaders := accountTelemetryInferenceHeaders(claudeTelemetryDeviceHeaders(device, ""))
	if isAcceptableFingerprintUserAgent(importedHeaders.Get("User-Agent")) && isClaudeCodeRuntime(importedHeaders.Get("X-Stainless-Lang"), importedHeaders.Get("X-Stainless-Runtime")) {
		mergeHeadersIntoFingerprint(&copyFP, importedHeaders)
		if floored, changed := floorClaudeCLIUserAgentVersion(copyFP.UserAgent); changed {
			copyFP.UserAgent = floored
			defaults := defaultFingerprint()
			copyFP.StainlessPackageVersion, copyFP.StainlessRuntimeVersion = defaults.StainlessPackageVersion, defaults.StainlessRuntimeVersion
		}
	}
	return &copyFP, nil
}

func claudeTelemetryIdentityForActivity(account *Account, binding AccountTelemetryActivity) (claudeTelemetryIdentity, http.Header, error) {
	state := ReadAccountTelemetry(account)
	device := state.DeviceInstallation
	if device == nil {
		return claudeTelemetryIdentity{}, nil, telemetryForwardFailure("telemetry_import_required")
	}
	if _, err := uuid.Parse(state.SessionID); err != nil || state.SessionID == uuid.Nil.String() || state.Owner != accountTelemetryOwner(account) {
		return claudeTelemetryIdentity{}, nil, telemetryForwardFailure("telemetry_account_changed")
	}
	deviceID := officialTelemetryDevice(device.DeviceID)
	// A tick can observe activity before a concurrent import/account rebind.
	// Device IDs can remain identical across revisions; the session cannot.
	if binding.Revision != state.Revision {
		binding = AccountTelemetryActivity{}
	}
	if binding.DeviceID != "" && officialTelemetryDevice(binding.DeviceID) != deviceID {
		return claudeTelemetryIdentity{}, nil, telemetryForwardFailure("telemetry_identity_changed_waiting_dispatch")
	}
	session := state.SessionID
	if binding.SessionID != "" && binding.DeviceID == deviceID {
		session = binding.SessionID
	}
	id := claudeTelemetryIdentity{
		DeviceID: deviceID, SessionID: session, AccountUUID: telemetryCredential(account, "account_uuid"),
		OrgUUID: telemetryCredential(account, "org_uuid", "organization_uuid"), Email: telemetryCredential(account, "email"),
		SubscriptionType: telemetryCredential(account, "subscription_type", "billing_type", "seat_tier"),
		Version:          device.CLIVersion, Platform: device.Platform, Entrypoint: "cli",
	}
	id.Env = fullClaudeTelemetryEnv(device.Environment, id.Version, id.Platform, device.Arch, id.AccountUUID != "")
	return id, claudeTelemetryDeviceHeaders(device, session), nil
}

func claudeTelemetryDeviceHeaders(device *AccountTelemetryBundle, session string) http.Header {
	headers := make(http.Header)
	fingerprint := defaultFingerprint()
	fingerprint.applyHeaders(&http.Request{Header: headers})
	headers.Set("User-Agent", "claude-code/"+device.CLIVersion)
	headers.Set("X-Stainless-OS", map[string]string{"linux": "Linux", "darwin": "MacOS", "win32": "Windows"}[device.Platform])
	headers.Set("X-Stainless-Arch", device.Arch)
	headers.Set("X-App", "cli")
	headers.Set("X-Service-Name", "claude-code")
	headers.Set("Anthropic-Version", "2023-06-01")
	if session != "" {
		headers.Set("X-Claude-Code-Session-Id", session)
	}
	locale := device.Locale
	if locale == "" {
		locale = "en-US"
	}
	headers.Set("Accept-Language", strings.ReplaceAll(strings.Split(locale, ".")[0], "_", "-"))
	return accountTelemetryHeaders(headers)
}

func accountTelemetryInferenceHeaders(source http.Header) http.Header {
	headers := accountTelemetryHeaders(source)
	if ua := headers.Get("User-Agent"); strings.HasPrefix(ua, "claude-code/") {
		headers.Set("User-Agent", "claude-cli/"+strings.TrimPrefix(ua, "claude-code/")+" (external, cli)")
	}
	return headers
}
