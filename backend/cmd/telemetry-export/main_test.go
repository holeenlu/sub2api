package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOfficialIdentityReaderAndPrivateOutput(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, ".claude.json")
	doc := `{"userID":"` + strings.Repeat("b", 64) + `","machineID":"real-machine","oauthAccount":{"email":"SECRET"},"apiKey":"SECRET","firstStartTime":"2026-10-20T00:00:00Z"}`
	if err := os.WriteFile(name, []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	got := readInstall([]string{name})
	if got == nil || got.UserID != strings.Repeat("b", 64) || got.MachineID != "real-machine" {
		t.Fatal(got)
	}
	raw, _ := json.Marshal(got)
	if bytes.Contains(raw, []byte("SECRET")) {
		t.Fatal("account information escaped allowlist")
	}
	config := filepath.Join(dir, "separate")
	if !reflect.DeepEqual(configCandidates(dir, config), []string{filepath.Join(config, ".claude.json")}) {
		t.Fatal("explicit config fallback")
	}
	target := filepath.Join(dir, "bundle.json")
	if err := writeBundle(target, raw); err != nil {
		t.Fatal(err)
	}
	if err := writeBundle(target, []byte("overwritten")); err == nil {
		t.Fatal("overwrite accepted")
	}
	check, _ := os.ReadFile(target)
	if !bytes.Equal(raw, check) {
		t.Fatal("existing output modified")
	}
	info, _ := os.Stat(target)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "SECRET")
	t.Setenv("ANTHROPIC_API_KEY", "SECRET")
	if strings.Contains(strings.Join(isolatedEnvironment(dir, config), "|"), "SECRET") {
		t.Fatal("inherited credentials")
	}
	if err := os.WriteFile(name, []byte(`{"machineID":"real"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if readInstall([]string{name}) != nil {
		t.Fatal("missing official user ID accepted")
	}
}

func TestPlatformEnvironmentAndInstallerSelection(t *testing.T) {
	o := observer{cores: 8, mac: "aa:bb:cc:dd:ee:ff", env: func(k string) string { return map[string]string{"SHELL": "/bin/zsh", "SystemDrive": "C:"}[k] }, exists: func(k string) bool { return k == "git" },
		read: func(k string) string {
			return map[string]string{"/etc/os-release": "ID=ubuntu\nPRETTY_NAME=Ubuntu 24.04 LTS", "/proc/sys/kernel/osrelease": "6.8.0", "/proc/meminfo": "MemTotal: 8388608 kB", "/proc/cpuinfo": "model name: fixture CPU", "/proc/mounts": "/dev/vda1 / ext4 rw 0 0", "/sys/class/dmi/id/product_name": "fixture PC"}[k]
		},
		command: func(k string, args ...string) string {
			switch k {
			case "sysctl":
				if args[1] == "hw.memsize" {
					return "8589934592"
				}
				if args[1] == "machdep.cpu.brand_string" {
					return "fixture CPU"
				}
			case "ioreg":
				return `"model" = <"fixture Mac">`
			case "powershell.exe":
				return `{"memory_bytes":8589934592,"cpu_model":"fixture CPU","dmi_product":"fixture Windows","locale":"en-US","accessToken":"SECRET"}`
			}
			return ""
		}}
	for _, platform := range []string{"linux", "darwin", "windows"} {
		t.Run(platform, func(t *testing.T) {
			facts, _ := collectEnvironment(platform, o)
			if facts["memory_bytes"] != int64(8589934592) {
				t.Fatal(facts)
			}
			if facts["cpu_model"] != "fixture CPU" {
				t.Fatal(facts)
			}
			raw, _ := json.Marshal(facts)
			if bytes.Contains(raw, []byte("SECRET")) {
				t.Fatal("unlisted hardware fields")
			}
			if platform == "linux" && facts["linux_distro_version"] != "24.04" {
				t.Fatal(facts)
			}
			target, err := installPlatform(platform, "amd64", o)
			if err != nil {
				t.Fatal(err)
			}
			prefix := platform
			if platform == "windows" {
				prefix = "win32"
			}
			if target != prefix+"-x64" {
				t.Fatal(target)
			}
		})
	}
	o.exists = func(k string) bool { return k == "/lib/ld-musl-aarch64.so.1" }
	target, err := installPlatform("linux", "arm64", o)
	if err != nil || target != "linux-arm64-musl" {
		t.Fatal(target, err)
	}
	o.env = func(k string) string {
		if k == "PROCESSOR_ARCHITEW6432" {
			return "ARM64"
		}
		return ""
	}
	target, err = installPlatform("windows", "amd64", o)
	if err != nil || target != "win32-arm64" {
		t.Fatal(target, err)
	}
}

func TestOfficialDownloadIntegrityAndLatestResolution(t *testing.T) {
	payload := []byte("fixture official binary")
	digest := sha256.Sum256(payload)
	for _, kind := range []string{"valid", "bad-checksum", "truncated", "invalid-latest", "missing-platform", "http-error"} {
		t.Run(kind, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest":
					if kind == "invalid-latest" {
						io.WriteString(w, "<html>error</html>")
					} else {
						io.WriteString(w, "2.1.999")
					}
				case "/2.1.999/manifest.json":
					sum := hex.EncodeToString(digest[:])
					if kind == "bad-checksum" {
						sum = strings.Repeat("0", 64)
					}
					target := "linux-x64"
					if kind == "missing-platform" {
						target = "unknown"
					}
					json.NewEncoder(w).Encode(map[string]any{"platforms": map[string]any{target: map[string]any{"checksum": sum, "size": len(payload)}}})
				case "/2.1.999/linux-x64/claude":
					if kind == "http-error" {
						w.WriteHeader(500)
					} else if kind == "truncated" {
						w.Write(payload[:3])
					} else {
						w.Write(payload)
					}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			file, version, err := fetchOfficial(context.Background(), server.Client(), server.URL, "linux-x64", t.TempDir(), io.Discard)
			if kind != "valid" {
				if err == nil {
					t.Fatal("invalid download accepted")
				}
				return
			}
			if err != nil || version != "2.1.999" {
				t.Fatal(version, err)
			}
			raw, _ := os.ReadFile(file)
			if !bytes.Equal(raw, payload) {
				t.Fatal("wrong bytes")
			}
		})
	}
}

func TestExistingCLIAndExplicitPathNeverRequireInstall(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, ".local", "bin", executableName(runtime.GOOS))
	os.MkdirAll(filepath.Dir(file), 0700)
	magic := []byte{0x7f, 'E', 'L', 'F'}
	if runtime.GOOS == "windows" {
		magic = []byte{'M', 'Z', 0, 0}
	}
	if runtime.GOOS == "darwin" {
		magic = []byte{0xcf, 0xfa, 0xed, 0xfe}
	}
	os.WriteFile(file, magic, 0700)
	// An empty search path still finds the standard user installation.
	t.Setenv("PATH", "")
	got, err := findCLI("claude", home)
	if err != nil || got != file {
		t.Fatal(got, err)
	}
	if _, err := findCLI(filepath.Join(home, "missing"), home); err == nil {
		t.Fatal("explicit invalid path silently replaced")
	}
	os.WriteFile(file, []byte("#!/usr/bin/env node"), 0700)
	if nativeCLI(file) {
		t.Fatal("Node wrapper accepted as native CLI")
	}
	os.Remove(file)
	got, err = findCLI("claude", home)
	if err != nil || got != "" {
		t.Fatal(got, err)
	}
}

func TestRealOfficialCLINoLoginExport(t *testing.T) {
	cli := os.Getenv("SUB2API_TEST_CLAUDE_CLI")
	if cli == "" {
		t.Skip("set SUB2API_TEST_CLAUDE_CLI for real offline CLI")
	}
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "NOT_EXPORTED")
	t.Setenv("ANTHROPIC_API_KEY", "NOT_EXPORTED")
	var previous string
	for _, name := range []string{"relative path.json", filepath.Join(dir, "absolute path.json")} {
		args := []string{"--claude", cli, "--output", name}
		output := name
		if !filepath.IsAbs(output) {
			output = filepath.Join(cwd, output)
		}
		var message bytes.Buffer
		if err := run(args, &message); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(message.String(), "Saved "+output+" (") {
			t.Fatalf("missing absolute output path: %s", message.String())
		}
		raw, _ := os.ReadFile(output)
		if bytes.Contains(raw, []byte("NOT_EXPORTED")) {
			t.Fatal("secret in bundle")
		}
		var got bundle
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		actual := readInstall([]string{filepath.Join(config, "telemetry-export", ".claude", ".claude.json"), filepath.Join(config, "telemetry-export", ".claude.json")})
		if actual == nil || got.DeviceID != actual.UserID || got.MachineID != actual.MachineID {
			t.Fatal("identity did not match official file")
		}
		if previous != "" && previous != got.DeviceID {
			t.Fatal("installation rotated")
		}
		previous = got.DeviceID
		if !releaseVersion.MatchString(got.CLIVersion) || got.ExportedAt.Before(time.Now().Add(-time.Minute)) {
			t.Fatal(got.CLIVersion)
		}
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		for _, key := range []string{"account", "session_id", "access_token"} {
			if _, ok := fields[key]; ok {
				t.Fatal(key)
			}
		}
		if err := run(args, io.Discard); err == nil || !strings.Contains(err.Error(), output) {
			t.Fatalf("expected existing absolute output path, got %v", err)
		}
	}
}
