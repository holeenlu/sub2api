package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *GatewayService) initializeClaudeTelemetryProfile(ctx context.Context, account *Account) (AccountTelemetryProfile, error) {
	empty := AccountTelemetryProfile{}
	if s.cfg == nil || !filepath.IsAbs(s.cfg.Gateway.AccountTelemetryCLIPath) || !filepath.IsAbs(s.cfg.Gateway.AccountTelemetryHomeDir) {
		return empty, telemetryForwardFailure("telemetry_official_cli_not_configured")
	}
	if err := validateVendorNativeDispatch(ctx, s.accountRepo, account, nil); err != nil {
		return empty, telemetryForwardFailure("telemetry_account_not_authorized")
	}
	// A different imported account gets a different installation directory.
	owner := accountTelemetryOwner(account)[:16]
	home := filepath.Join(s.cfg.Gateway.AccountTelemetryHomeDir, fmt.Sprintf("%d-%s", account.ID, owner))
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0700); err != nil {
		return empty, telemetryForwardFailure("telemetry_home_unavailable")
	}
	if err := os.Chmod(home, 0700); err != nil {
		return empty, telemetryForwardFailure("telemetry_home_unavailable")
	}
	if profile, err := readOfficialTelemetryProfile(home, account); err == nil {
		return profile, nil
	}
	token, kind, err := s.GetAccessToken(ctx, account)
	if err != nil || kind != "oauth" || token == "" {
		return empty, telemetryForwardFailure("telemetry_credentials_unavailable")
	}
	// Only this account's access token is passed. No inherited auth, project,
	// OTLP, proxy or hook settings; no refresh token is handed to the CLI.
	env := []string{"HOME=" + home, "CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"),
		"PATH=/usr/local/bin:/usr/bin:/bin", "CLAUDE_CODE_OAUTH_TOKEN=" + token, "ANTHROPIC_AUTH_TOKEN=" + token,
		"DISABLE_TELEMETRY=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1",
		"ENABLE_CLAUDEAI_MCP_SERVERS=false", "SHELL=/bin/sh"}
	locale := telemetryCredential(account, "locale")
	if locale == "" {
		locale = "en_US.UTF-8"
	}
	env = append(env, "LANG="+locale, "LC_ALL="+locale)
	if timezone := telemetryCredential(account, "timezone"); timezone != "" {
		env = append(env, "TZ="+timezone)
	}
	relayURL, closeRelay, err := s.startClaudeTelemetryBootstrapRelay(ctx, account, token)
	if err != nil {
		return empty, err
	}
	defer closeRelay()
	env = append(env, "ANTHROPIC_BASE_URL="+relayURL)
	cmd := exec.CommandContext(ctx, s.cfg.Gateway.AccountTelemetryCLIPath, "--safe-mode", "--print", "hello", "--output-format", "json", "--max-turns", "1", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence")
	output := &telemetryOutputBuffer{limit: 1 << 20}
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr, cmd.WaitDelay = home, env, output, io.Discard, time.Second
	if err := cmd.Run(); err != nil {
		return empty, telemetryForwardFailure("telemetry_official_cli_init_failed")
	}
	var result struct {
		Type    string `json:"type"`
		IsError bool   `json:"is_error"`
	}
	if output.exceeded || json.Unmarshal(output.Bytes(), &result) != nil || result.Type != "result" || result.IsError {
		return empty, telemetryForwardFailure("telemetry_official_cli_init_failed")
	}
	versionCmd := exec.CommandContext(ctx, s.cfg.Gateway.AccountTelemetryCLIPath, "--version")
	versionCmd.Dir, versionCmd.Env, versionCmd.WaitDelay = home, env, time.Second
	versionOutput := &telemetryOutputBuffer{limit: 4096}
	versionCmd.Stdout = versionOutput
	err = versionCmd.Run()
	version := strings.Fields(versionOutput.String())
	if err != nil || versionOutput.exceeded || len(version) == 0 || !isAcceptableFingerprintUserAgent("claude-cli/"+version[0]+" (external, cli)") {
		return empty, telemetryForwardFailure("telemetry_official_cli_version_invalid")
	}
	facts := observedTelemetryEnvironment()
	facts["version"] = version[0]
	facts["session_id"] = uuid.NewString()
	facts["build_time"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	raw, _ := json.Marshal(facts)
	// Only observation data is written here, never a fabricated userID.
	if err := os.WriteFile(filepath.Join(home, "telemetry-environment.json"), raw, 0600); err != nil {
		return empty, telemetryForwardFailure("telemetry_home_unavailable")
	}
	return readOfficialTelemetryProfile(home, account)
}

func readTelemetryFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, AccountTelemetryMaxBodyBytes+1))
	if err != nil || int64(len(raw)) > AccountTelemetryMaxBodyBytes {
		return nil, fmt.Errorf("invalid telemetry file")
	}
	return raw, nil
}

func readOfficialTelemetryProfile(home string, account *Account) (AccountTelemetryProfile, error) {
	empty := AccountTelemetryProfile{}
	var install struct {
		UserID       string         `json:"userID"`
		MachineID    string         `json:"machineID"`
		OAuthAccount map[string]any `json:"oauthAccount"`
	}
	for _, path := range []string{filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", ".claude.json")} {
		raw, err := readTelemetryFile(path)
		if err == nil && json.Unmarshal(raw, &install) == nil && install.MachineID != "" && officialTelemetryDevice(install.UserID) != "" {
			break
		}
		install.UserID = ""
	}
	if officialTelemetryDevice(install.UserID) == "" {
		return empty, telemetryForwardFailure("waiting_official_identity")
	}
	raw, err := readTelemetryFile(filepath.Join(home, "telemetry-environment.json"))
	var facts map[string]any
	if err != nil || json.Unmarshal(raw, &facts) != nil {
		return empty, telemetryForwardFailure("telemetry_environment_unavailable")
	}
	version, _ := facts["version"].(string)
	if !isAcceptableFingerprintUserAgent("claude-cli/" + version + " (external, cli)") {
		return empty, telemetryForwardFailure("telemetry_official_cli_version_invalid")
	}
	platform, _ := facts["platform"].(string)
	arch, _ := facts["arch"].(string)
	if platform == "" || arch == "" {
		return empty, telemetryForwardFailure("telemetry_environment_unavailable")
	}
	session, _ := facts["session_id"].(string)
	if _, err := uuid.Parse(session); err != nil {
		return empty, telemetryForwardFailure("telemetry_environment_unavailable")
	}
	official := func(names ...string) string {
		for _, name := range names {
			if v, ok := install.OAuthAccount[name].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	accountID, orgID, email := telemetryCredential(account, "account_uuid"), telemetryCredential(account, "org_uuid", "organization_uuid"), telemetryCredential(account, "email")
	if v := official("accountUuid", "uuid"); v != "" {
		if accountID != "" && accountID != v {
			return empty, telemetryForwardFailure("telemetry_official_account_mismatch")
		}
		accountID = v
	}
	if orgID == "" {
		orgID = official("organizationUuid", "orgUuid")
	}
	if email == "" {
		email = official("emailAddress", "email")
	}
	id := claudeTelemetryIdentity{DeviceID: officialTelemetryDevice(install.UserID), SessionID: session, AccountUUID: accountID, OrgUUID: orgID, Email: email, Version: version, Platform: platform, Entrypoint: "cli"}
	id.Env = fullClaudeTelemetryEnv(facts, version, platform, arch, id.AccountUUID != "")
	headers := make(http.Header)
	fp := defaultFingerprint()
	fp.applyHeaders(&http.Request{Header: headers})
	headers.Set("User-Agent", "claude-code/"+version)
	headers.Set("X-Stainless-OS", map[string]string{"linux": "Linux", "darwin": "MacOS", "windows": "Windows"}[platform])
	headers.Set("X-Stainless-Arch", arch)
	headers.Set("X-App", "cli")
	headers.Set("X-Service-Name", "claude-code")
	headers.Set("Anthropic-Version", "2023-06-01")
	locale := telemetryCredential(account, "locale")
	if locale == "" {
		locale = "en-US"
	}
	headers.Set("Accept-Language", strings.ReplaceAll(strings.Split(locale, ".")[0], "_", "-"))
	subscription := official("billingType", "subscriptionType", "seatTier")
	return AccountTelemetryProfile{Source: "official-cc-init", SubscriptionType: subscription, PayloadJSON: string(id.event(true, time.Now(), time.Now())), Headers: headers}, nil
}

// Observe the environment in which the official CLI ran. Optional unavailable
// hardware remains omitted, exactly as for an incomplete vm2api slot snapshot.
func observedTelemetryEnvironment() map[string]any {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	facts := map[string]any{"platform": runtime.GOOS, "arch": arch, "shell": "sh", "runtimes": "node", "vcs": "git", "cpu_cores": runtime.NumCPU()}
	read := func(path string) string { raw, _ := os.ReadFile(path); return strings.TrimSpace(string(raw)) }
	if runtime.GOOS != "linux" {
		return facts
	}
	facts["linux_kernel"] = read("/proc/sys/kernel/osrelease")
	for _, line := range strings.Split(read("/etc/os-release"), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		value := strings.Trim(parts[1], `"`)
		if parts[0] == "ID" {
			facts["linux_distro_id"] = value
		}
		if parts[0] == "PRETTY_NAME" {
			facts["linux_distro_version"] = regexp.MustCompile(`\d+(?:\.\d+){0,2}`).FindString(value)
		}
	}
	for _, line := range strings.Split(read("/proc/cpuinfo"), "\n") {
		if parts := strings.SplitN(line, ":", 2); len(parts) == 2 && (strings.TrimSpace(parts[0]) == "model name" || strings.TrimSpace(parts[0]) == "Hardware") {
			facts["cpu_model"] = strings.TrimSpace(parts[1])
			break
		}
	}
	for _, line := range strings.Split(read("/proc/meminfo"), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "MemTotal:" {
			if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				facts["memory_bytes"] = n * 1024
			}
		}
	}
	for _, line := range strings.Split(read("/proc/mounts"), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "/" {
			disk := fields[0]
			for _, marker := range []string{"docker", "containerd", "podman"} {
				if strings.Contains(disk, marker) {
					disk = "overlay"
					break
				}
			}
			facts["root_disk"] = disk
			break
		}
	}
	interfaces, _ := filepath.Glob("/sys/class/net/*")
	for _, iface := range interfaces {
		if filepath.Base(iface) == "lo" {
			continue
		}
		if mac := read(filepath.Join(iface, "address")); mac != "" && mac != "00:00:00:00:00:00" {
			facts["mac"] = mac
			break
		}
	}
	packages := []string{}
	for _, command := range [][2]string{{"apt-get", "apt"}, {"dnf", "dnf"}, {"yum", "yum"}, {"pacman", "pacman"}, {"zypper", "zypper"}, {"apk", "apk"}, {"nix-env", "nix"}, {"brew", "brew"}} {
		if _, err := exec.LookPath(command[0]); err == nil {
			packages = append(packages, command[1])
		}
	}
	facts["package_managers"] = strings.Join(packages, ",")
	runtimes := []string{}
	seen := map[string]bool{}
	for _, command := range [][2]string{{"node", "node"}, {"bun", "bun"}, {"python3", "python"}, {"python", "python"}, {"go", "go"}, {"rustc", "rust"}, {"java", "java"}, {"ruby", "ruby"}, {"php", "php"}, {"deno", "deno"}, {"perl", "perl"}} {
		if _, err := exec.LookPath(command[0]); err == nil && !seen[command[1]] {
			runtimes = append(runtimes, command[1])
			seen[command[1]] = true
		}
	}
	if len(runtimes) > 0 {
		facts["runtimes"] = strings.Join(runtimes, ",")
	}

	for key, file := range map[string]string{"dmi_manufacturer": "sys_vendor", "dmi_product": "product_name", "dmi_version": "product_version", "dmi_family": "product_family", "dmi_serial": "product_serial", "dmi_uuid": "product_uuid"} {
		if value := read(filepath.Join("/sys/class/dmi/id", file)); value != "" {
			facts[key] = value
		}
	}
	return facts
}

type telemetryOutputBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *telemetryOutputBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.Len()
	if n > remaining {
		b.exceeded = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
