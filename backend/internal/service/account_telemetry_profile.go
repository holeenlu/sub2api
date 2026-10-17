package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Parse only server-generated init profiles. Retired sample/import formats are
// deliberately not accepted as an account installation source.
func parseClaudeTelemetryProfile(profile AccountTelemetryProfile) (*claudeTelemetryEvent, error) {
	var event claudeTelemetryEvent
	if profile.Source != "official-cc-init" || json.Unmarshal([]byte(profile.PayloadJSON), &event) != nil || event.Type != "ClaudeCodeInternalEvent" || event.Data == nil {
		return nil, telemetryForwardFailure("telemetry_profile_invalid")
	}
	var device, name string
	_ = json.Unmarshal(event.Data["device_id"], &device)
	_ = json.Unmarshal(event.Data["event_name"], &name)
	if officialTelemetryDevice(device) == "" || name != "tengu_init" || len(event.Data["process"]) > 0 {
		return nil, telemetryForwardFailure("telemetry_profile_invalid")
	}
	if env, err := telemetryObject(event.Data["env"]); err != nil || env == nil {
		return nil, telemetryForwardFailure("telemetry_profile_invalid")
	}
	return &event, nil
}

func telemetryObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, telemetryForwardFailure("telemetry_profile_invalid")
	}
	return out, nil
}

func accountTelemetryProfile(account *Account) (AccountTelemetryProfile, bool, error) {
	var profile AccountTelemetryProfile
	if account == nil {
		return profile, false, telemetryForwardFailure("telemetry_account_unavailable")
	}
	state := ReadAccountTelemetry(account)
	if state.Profile == nil || state.Owner != accountTelemetryOwner(account) {
		return profile, false, nil
	}
	profile = *state.Profile
	_, err := parseClaudeTelemetryProfile(profile)
	return profile, true, err
}

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

// Also used by prompt-template environment cleanup; retain the shared policy.
func cleanTelemetryEnvironment(data map[string]json.RawMessage) {
	for _, key := range []string{"additional_metadata", "betas", "model", "baseUrl", "base_url", "gateway", "apiBaseUrlHost", "hostname", "host_name", "authorization", "cookie", "x-api-key", "proxy-authorization", "machine_id", "machine-id", "kernel_release", "os_pretty", "os_id", "runtime_kind", "node_version", "is_running_with_bun"} {
		delete(data, key)
	}
}

// Account-generated official identity supplies inference and telemetry together.
// Native fallback fingerprints remain independent until initialization completes.
func (s *IdentityService) GetAccountFingerprint(ctx context.Context, account *Account, headers http.Header) (*Fingerprint, error) {
	if s == nil || s.cache == nil {
		return nil, fmt.Errorf("account identity cache unavailable")
	}
	fp, err := s.GetOrCreateFingerprint(ctx, account.ID, headers)
	if err != nil {
		return nil, err
	}
	copyFP := *fp
	profile, exists, err := accountTelemetryProfile(account)
	if err != nil || !exists {
		return &copyFP, nil
	}
	event, _ := parseClaudeTelemetryProfile(profile)
	_ = json.Unmarshal(event.Data["device_id"], &copyFP.ClientID)
	// The existing fingerprint admission/version rules remain authoritative.
	if isAcceptableFingerprintUserAgent(accountTelemetryInferenceHeaders(profile.Headers).Get("User-Agent")) && isClaudeCodeRuntime(profile.Headers.Get("X-Stainless-Lang"), profile.Headers.Get("X-Stainless-Runtime")) {
		mergeHeadersIntoFingerprint(&copyFP, accountTelemetryInferenceHeaders(profile.Headers))
		if floored, changed := floorClaudeCLIUserAgentVersion(copyFP.UserAgent); changed {
			copyFP.UserAgent = floored
			defaults := defaultFingerprint()
			copyFP.StainlessPackageVersion = defaults.StainlessPackageVersion
			copyFP.StainlessRuntimeVersion = defaults.StainlessRuntimeVersion
		}
	}
	return &copyFP, nil
}

// Build a complete identity from official installation facts. A stale dispatch
// must not replace the official device with a gateway-generated/random device.
func claudeTelemetryIdentityForActivity(profile AccountTelemetryProfile, account *Account, binding AccountTelemetryActivity) (claudeTelemetryIdentity, http.Header, error) {
	event, err := parseClaudeTelemetryProfile(profile)
	if err != nil {
		return claudeTelemetryIdentity{}, nil, err
	}
	var device string
	_ = json.Unmarshal(event.Data["device_id"], &device)
	device = officialTelemetryDevice(device)
	if binding.DeviceID != "" && officialTelemetryDevice(binding.DeviceID) != device {
		return claudeTelemetryIdentity{}, nil, telemetryForwardFailure("telemetry_identity_changed_waiting_dispatch")
	}
	headers := accountTelemetryInferenceHeaders(profile.Headers)
	headers.Del("Anthropic-Beta")
	version := ExtractCLIVersion(headers.Get("User-Agent"))
	if !isAcceptableFingerprintUserAgent(headers.Get("User-Agent")) {
		version = ExtractCLIVersion(defaultFingerprint().UserAgent)
	}
	factsRaw, err := telemetryObject(event.Data["env"])
	if err != nil {
		return claudeTelemetryIdentity{}, nil, err
	}
	facts := make(map[string]any, len(factsRaw))
	for name, raw := range factsRaw {
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		var value any
		if decoder.Decode(&value) == nil {
			facts[name] = value
		}
	}
	platform, _ := facts["platform"].(string)
	if value := headers.Get("X-Stainless-OS"); value != "" {
		platform = strings.ToLower(value)
	}
	if platform == "macos" {
		platform = "darwin"
	}
	if platform == "" {
		platform = "linux"
	}
	arch, _ := facts["arch"].(string)
	if value := headers.Get("X-Stainless-Arch"); value != "" {
		arch = value
	}
	if arch == "" {
		arch = "x64"
	}
	session := binding.SessionID
	if session == "" {
		_ = json.Unmarshal(event.Data["session_id"], &session)
		if session == "" {
			return claudeTelemetryIdentity{}, nil, telemetryForwardFailure("telemetry_session_missing")
		}
	}
	id := claudeTelemetryIdentity{
		DeviceID: device, SessionID: session, AccountUUID: telemetryCredential(account, "account_uuid"),
		OrgUUID: telemetryCredential(account, "org_uuid", "organization_uuid"), Email: telemetryCredential(account, "email"),
		SubscriptionType: telemetryCredential(account, "subscription_type", "billing_type", "seat_tier"),
		Version:          version, Platform: platform, Entrypoint: "cli",
	}
	auth, _ := telemetryObject(event.Data["auth"])
	if id.AccountUUID == "" {
		_ = json.Unmarshal(auth["account_uuid"], &id.AccountUUID)
	}
	if id.OrgUUID == "" {
		_ = json.Unmarshal(auth["organization_uuid"], &id.OrgUUID)
	}
	if id.Email == "" {
		_ = json.Unmarshal(event.Data["email"], &id.Email)
	}
	if id.SubscriptionType == "" {
		id.SubscriptionType = profile.SubscriptionType
	}
	// Current credentials overlay the official installation account facts.
	id.Env = fullClaudeTelemetryEnv(facts, version, platform, arch, id.AccountUUID != "")
	headers.Set("User-Agent", "claude-cli/"+version+" (external, cli)")
	headers.Set("X-Claude-Code-Session-Id", session)
	headers.Set("X-Service-Name", "claude-code")
	headers.Set("X-App", "cli")
	headers.Set("Anthropic-Version", "2023-06-01")
	return id, headers, nil
}

// Telemetry uses claude-code/version; inference retains its native CLI suffix.
func accountTelemetryInferenceHeaders(source http.Header) http.Header {
	headers := accountTelemetryHeaders(source)
	if ua := headers.Get("User-Agent"); strings.HasPrefix(ua, "claude-code/") {
		headers.Set("User-Agent", "claude-cli/"+strings.TrimPrefix(ua, "claude-code/")+" (external, cli)")
	}
	return headers
}
