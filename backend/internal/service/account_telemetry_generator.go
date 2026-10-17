package service

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Wire contract verified against vm2api 819ee2a7 worker/internal/telemetry
// and src/lib/identity/telemetry-env.mjs. Client uploads are triggers, not the
// source of generated event facts. Installation facts come from the account.
const claudeTelemetryEvalPath = "/api/eval/sdk-zAZezfDKGoZuXXKe"

type claudeTelemetryIdentity struct {
	DeviceID, SessionID, AccountUUID, OrgUUID, Email, SubscriptionType string
	Version, Platform, Entrypoint                                      string
	Env                                                                map[string]any
}

func officialTelemetryDevice(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func telemetryVersionBase(version string) string {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 4)
	if len(parts) == 4 {
		return strings.Join(parts[:3], ".")
	}
	return strings.TrimSpace(version)
}

func telemetryShell(shell string) string {
	shell = strings.TrimSpace(shell)
	if shell == "" {
		return "bash"
	}
	parts := strings.Split(strings.ReplaceAll(shell, "\\", "/"), "/")
	shell = strings.TrimSuffix(strings.ToLower(parts[len(parts)-1]), ".exe")
	switch shell {
	case "zsh", "bash", "fish", "sh", "dash", "ash", "ksh", "tcsh", "csh", "nu", "nushell", "pwsh", "powershell", "cmd", "elvish", "xonsh", "ion":
		return shell
	}
	return "other"
}

// Complete environment, including empty protocol fields. Optional installation
// and hardware facts stay absent when unknown; never invent observed hardware.
func fullClaudeTelemetryEnv(facts map[string]any, version, platform, arch string, authenticated bool) map[string]any {
	env := map[string]any{
		"platform": platform, "platform_raw": platform, "arch": arch,
		"terminal": "unknown", "shell": "bash", "runtimes": "node", "vcs": "git",
		"version": version, "version_base": telemetryVersionBase(version),
		"deployment_environment": "unknown-" + platform, "is_claude_ai_auth": authenticated, "tags": []any{},
	}
	for _, key := range []string{"is_ci", "is_claubbit", "is_claude_code_remote", "is_local_agent_mode", "is_conductor", "is_github_action", "is_claude_code_action"} {
		env[key] = false
	}
	for _, key := range []string{"package_managers", "github_event_name", "github_actions_runner_environment", "github_actions_runner_os", "github_action_ref", "wsl_version", "remote_environment_type", "claude_code_container_id", "claude_code_remote_session_id", "coworker_type", "linux_distro_id", "linux_distro_version", "linux_kernel"} {
		env[key] = ""
	}
	for _, key := range []string{"terminal", "shell", "package_managers", "runtimes", "vcs", "linux_distro_id", "linux_distro_version", "linux_kernel", "build_time", "cpu_model", "root_disk", "mac", "dmi_manufacturer", "dmi_product", "dmi_version", "dmi_family", "dmi_serial", "dmi_uuid"} {
		if value, ok := facts[key].(string); ok && strings.TrimSpace(value) != "" {
			env[key] = strings.TrimSpace(value)
		}
	}
	for _, key := range []string{"cpu_cores", "memory_bytes"} {
		if value, ok := facts[key]; ok {
			var number float64
			raw, err := json.Marshal(value)
			if err == nil && json.Unmarshal(raw, &number) == nil && number >= 0 {
				env[key] = value
			}
		}
	}
	env["shell"] = telemetryShell(env["shell"].(string))
	return env
}

func (id claudeTelemetryIdentity) event(init bool, now time.Time, started time.Time) json.RawMessage {
	name := "tengu_api_success"
	if init {
		name = "tengu_init"
	}
	data := map[string]any{
		"event_name": name, "event_id": uuid.NewString(),
		"client_timestamp": now.UTC().Format("2006-01-02T15:04:05.000Z"),
		"entrypoint":       id.Entrypoint, "is_interactive": !init, "client_type": "cli", "user_type": "external",
		"device_id": id.DeviceID, "env": id.Env,
	}
	putTelemetryString(data, "session_id", id.SessionID)
	putTelemetryString(data, "email", id.Email)
	auth := map[string]any{}
	putTelemetryString(auth, "account_uuid", id.AccountUUID)
	putTelemetryString(auth, "organization_uuid", id.OrgUUID)
	if len(auth) > 0 {
		data["auth"] = auth
	}
	if !init {
		// vm2api generates synthetic process data, with exclusive upper bounds.
		process, _ := json.Marshal(map[string]any{
			"uptime": math.Max(0, now.Sub(started).Seconds()),
			"rss":    300_000_000 + rand.Int64N(200_000_000), "heapTotal": 40_000_000 + rand.Int64N(40_000_000),
			"heapUsed": 100_000_000 + rand.Int64N(100_000_000), "external": 1_000_000 + rand.Int64N(2_000_000),
			"arrayBuffers": 10_000 + rand.Int64N(40_000), "constrainedMemory": 0,
			"cpuUsage":   map[string]int64{"user": 50_000 + rand.Int64N(450_000), "system": 15_000 + rand.Int64N(135_000)},
			"cpuPercent": 0.5 + rand.Float64()*4.5,
		})
		data["process"] = base64.StdEncoding.EncodeToString(process)
	}
	raw, _ := json.Marshal(map[string]any{"event_type": "ClaudeCodeInternalEvent", "event_data": data})
	return raw
}

func putTelemetryString(dst map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		dst[key] = value
	}
}

func (id claudeTelemetryIdentity) growthbookEval() []byte {
	attributes := map[string]any{
		"id": id.DeviceID, "deviceID": id.DeviceID, "sessionId": id.SessionID,
		"platform": id.Platform, "appVersion": id.Version, "userType": "external", "entrypoint": id.Entrypoint,
	}
	putTelemetryString(attributes, "email", id.Email)
	putTelemetryString(attributes, "accountUUID", id.AccountUUID)
	putTelemetryString(attributes, "organizationUUID", id.OrgUUID)
	putTelemetryString(attributes, "subscriptionType", id.SubscriptionType)
	raw, _ := json.Marshal(map[string]any{"attributes": attributes, "forcedVariations": map[string]any{}, "forcedFeatures": []any{}, "url": ""})
	return raw
}
