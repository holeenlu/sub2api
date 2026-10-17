package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// A saved copy supplies account environment, never evidence that a new request
// succeeded. Both the editor and sender use this parser.
func parseClaudeTelemetryProfile(profile accountTelemetryCopy) (*claudeTelemetryEvent, error) {
	var event claudeTelemetryEvent
	if profile.Client != "claude" || json.Unmarshal([]byte(profile.PayloadJSON), &event) != nil ||
		(event.Type != "ClaudeCodeInternalEvent" && event.Type != "GrowthbookExperimentEvent") || event.Data == nil {
		return nil, telemetryForwardFailure("telemetry_account_sample_invalid")
	}
	var device string
	if json.Unmarshal(event.Data["device_id"], &device) != nil || strings.TrimSpace(device) == "" || len(device) > 512 {
		return nil, telemetryForwardFailure("telemetry_account_sample_invalid")
	}
	for _, name := range []string{"auth", "env", "user_attributes"} {
		if _, err := telemetryObject(event.Data[name]); err != nil {
			return nil, err
		}
	}
	if raw := event.Data["process"]; len(raw) > 0 {
		var encoded string
		if json.Unmarshal(raw, &encoded) == nil {
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				decoded = []byte(encoded)
			}
			if _, err = telemetryObject(decoded); err != nil {
				return nil, err
			}
		} else if _, err := telemetryObject(raw); err != nil {
			return nil, err
		}
	}
	return &event, nil
}

func telemetryObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		raw = []byte(encoded)
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, telemetryForwardFailure("telemetry_account_sample_invalid")
	}
	return out, nil
}

func accountTelemetryProfile(account *Account) (accountTelemetryCopy, bool, error) {
	var profile accountTelemetryCopy
	if account == nil {
		return profile, false, telemetryForwardFailure("telemetry_account_unavailable")
	}
	raw, exists := account.Extra[AccountClientTelemetryExtraKey]
	if !exists || raw == nil {
		return profile, false, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(encoded, &profile) != nil {
		return profile, true, telemetryForwardFailure("telemetry_account_sample_invalid")
	}
	_, err = parseClaudeTelemetryProfile(profile)
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

// Account copies also supply inference identity so the two paths cannot use
// independent devices. Do not persist a selected copy into the generated cache:
// removing the copy must restore the account's original generated fingerprint.
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
	if isAcceptableFingerprintUserAgent(profile.Headers.Get("User-Agent")) && isClaudeCodeRuntime(profile.Headers.Get("X-Stainless-Lang"), profile.Headers.Get("X-Stainless-Runtime")) {
		mergeHeadersIntoFingerprint(&copyFP, profile.Headers)
		if floored, changed := floorClaudeCLIUserAgentVersion(copyFP.UserAgent); changed {
			copyFP.UserAgent = floored
			defaults := defaultFingerprint()
			copyFP.StainlessPackageVersion = defaults.StainlessPackageVersion
			copyFP.StainlessRuntimeVersion = defaults.StainlessRuntimeVersion
		}
	}
	return &copyFP, nil
}

var clientTelemetryStartedAt = time.Now()

// This is an initialized account CLI profile, not a claim that a real VM or
// official CLI bootstrap ran. Reuse the native persistent fingerprint and OAuth
// identity. Generate process-shaped measurements only when a real upload arrives.
func (s *GatewayService) initializeClaudeTelemetryProfile(ctx context.Context, account *Account) (accountTelemetryCopy, error) {
	fp, err := s.identityService.GetAccountFingerprint(ctx, account, nil)
	if err != nil {
		return accountTelemetryCopy{}, telemetryForwardFailure("telemetry_identity_unavailable")
	}
	headers := make(http.Header)
	req := &http.Request{Header: headers}
	fp.applyHeaders(req)
	headers.Set("X-App", "cli")
	headers.Set("X-Service-Name", "claude-code")
	headers.Set("Anthropic-Version", "2023-06-01")
	locale := telemetryCredential(account, "locale")
	if locale == "" {
		locale = "en-US"
	}
	headers.Set("Accept-Language", strings.ReplaceAll(locale, "_", "-"))
	timezone := telemetryCredential(account, "timezone")
	if timezone == "" {
		timezone = "UTC"
	}
	platform := strings.ToLower(fp.StainlessOS)
	if platform == "macos" {
		platform = "darwin"
	}
	version := ExtractCLIVersion(fp.UserAgent)
	env := map[string]any{"platform": platform, "platform_raw": platform, "arch": fp.StainlessArch,
		"version": version, "version_base": strings.Split(version, "-")[0], "terminal": "unknown", "shell": "bash",
		"runtimes": fp.StainlessRuntime, "vcs": "git", "package_managers": "", "deployment_environment": "unknown-" + platform,
		"locale": locale, "timezone": timezone, "tags": []string{}, "is_claude_ai_auth": telemetryCredential(account, "account_uuid") != ""}
	for _, name := range []string{"is_ci", "is_claubbit", "is_claude_code_remote", "is_local_agent_mode", "is_conductor", "is_github_action", "is_claude_code_action"} {
		env[name] = false
	}
	for _, name := range []string{"github_event_name", "github_actions_runner_environment", "github_actions_runner_os", "github_action_ref", "wsl_version", "remote_environment_type", "claude_code_container_id", "claude_code_remote_session_id", "coworker_type", "linux_distro_id", "linux_distro_version", "linux_kernel"} {
		env[name] = ""
	}
	process := map[string]any{
		"uptime": time.Since(clientTelemetryStartedAt).Seconds(),
		"rss":    300_000_000 + rand.Int64N(200_000_001), "heapTotal": 120_000_000 + rand.Int64N(80_000_001),
		"heapUsed": 40_000_000 + rand.Int64N(80_000_001), "external": 1_000_000 + rand.Int64N(2_000_001),
		"constrainedMemory": int64(0), "cpuPercent": 0.5 + rand.Float64()*4.5, "arrayBuffers": 10_000 + rand.Int64N(40_001), "cpuUsage": map[string]int64{"user": 50_000 + rand.Int64N(450_001), "system": 15_000 + rand.Int64N(135_001)},
	}
	encodedProcess, _ := json.Marshal(process)
	data := map[string]any{"device_id": fp.ClientID, "env": env, "process": base64.StdEncoding.EncodeToString(encodedProcess), "auth": map[string]any{}}
	payload, _ := json.Marshal(map[string]any{"event_type": "ClaudeCodeInternalEvent", "event_data": data})
	return accountTelemetryCopy{Client: "claude", PayloadJSON: string(payload), Headers: headers}, nil
}

// Only known environment/identity extension keys are removed. Event facts and
// unrelated extension numbers retain their original JSON precision.
func cleanTelemetryEnvironment(data map[string]json.RawMessage) {
	for _, key := range []string{"additional_metadata", "betas", "model", "baseUrl", "base_url", "gateway", "apiBaseUrlHost", "hostname", "host_name", "authorization", "cookie", "x-api-key", "proxy-authorization", "machine_id", "machine-id", "kernel_release", "os_pretty", "os_id", "runtime_kind", "node_version", "is_running_with_bun"} {
		delete(data, key)
	}
}
