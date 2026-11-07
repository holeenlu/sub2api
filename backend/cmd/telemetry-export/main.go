// telemetry-export creates a gateway-compatible device bundle without Node.js.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const maxBundle = 1 << 20

var devicePattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
var versionPattern = regexp.MustCompile(`^([0-9]+[.][0-9]+[.][0-9]+)(?:[[:space:]]|$)`)

type installation struct {
	UserID     string `json:"userID"`
	MachineID  string `json:"machineID"`
	FirstStart string `json:"firstStartTime"`
}
type bundle struct {
	Format      string         `json:"format"`
	Version     int            `json:"version"`
	ExportedAt  time.Time      `json:"exported_at"`
	DeviceID    string         `json:"device_id"`
	MachineID   string         `json:"machine_id"`
	CLIVersion  string         `json:"cli_version"`
	Platform    string         `json:"platform"`
	Arch        string         `json:"arch"`
	Locale      string         `json:"locale"`
	Environment map[string]any `json:"environment"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("telemetry-export", flag.ContinueOnError)
	flags.SetOutput(output)
	destination := flags.String("output", "", "JSON output file (default: account-telemetry.json beside this executable); explicit relative paths use the current working directory; will not overwrite")
	executable := flags.String("claude", "claude", "native official Claude Code executable")
	shell := flags.String("shell", "", "shell used on this device, e.g. powershell")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments; use --help")
	}
	if *destination == "" {
		toolPath, err := os.Executable()
		if err != nil {
			return errors.New("cannot locate this executable; specify an absolute --output path")
		}
		*destination = filepath.Join(filepath.Dir(toolPath), "account-telemetry.json")
	}
	outputPath, err := filepath.Abs(*destination)
	if err != nil {
		return errors.New("cannot resolve output path from the current working directory")
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return fmt.Errorf("output already exists: %s; choose another --output file", outputPath)
	} else if !os.IsNotExist(err) {
		return errors.New("cannot inspect output path")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return errors.New("home directory unavailable")
	}
	cli, err := findCLI(*executable, home)
	if err != nil {
		return err
	}
	if cli == "" {
		fmt.Fprintln(output, "Official Claude Code not found; installing the latest native version.")
		cli, err = installLatest(home, output)
		if err != nil {
			return err
		}
	}
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	candidates := configCandidates(home, config)
	if config == "" {
		config = filepath.Join(home, ".claude")
	}
	config, err = filepath.Abs(config)
	if err != nil {
		return errors.New("invalid configuration directory")
	}
	local := filepath.Join(config, "telemetry-export")
	scratch, err := os.MkdirTemp("", "telemetry-export-")
	if err != nil {
		return errors.New("temporary directory unavailable")
	}
	defer os.RemoveAll(scratch)
	rawVersion, err := execute(cli, []string{"--version"}, scratch, isolatedEnvironment(scratch, filepath.Join(scratch, ".claude")), 15*time.Second, 4096)
	match := versionPattern.FindSubmatch(bytes.TrimSpace(rawVersion))
	if err != nil || len(match) != 2 {
		return errors.New("cannot read a supported official CLI version")
	}
	version := string(match[1])
	localFiles := []string{filepath.Join(local, ".claude", ".claude.json"), filepath.Join(local, ".claude.json")}
	install := readInstall(append(candidates, localFiles...))
	if install == nil {
		if err := os.MkdirAll(filepath.Join(local, ".claude"), 0700); err != nil {
			return errors.New("cannot create local installation directory")
		}
		if err := os.Chmod(local, 0700); err != nil {
			return errors.New("cannot protect local installation directory")
		}
		fmt.Fprintln(output, "Initializing official local installation without login:", local)
		args := []string{"--safe-mode", "--print", "hello", "--output-format", "json", "--max-turns", "1", "--tools", "", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`, "--no-session-persistence"}
		_, err := execute(cli, args, scratch, isolatedEnvironment(local, filepath.Join(local, ".claude")), 20*time.Second, maxBundle)
		var exit *exec.ExitError
		// Unauthenticated startup can persist installation IDs before exiting 1.
		if err != nil && !errors.As(err, &exit) {
			return errors.New("official local initialization failed or timed out")
		}
		install = readInstall(localFiles)
	}
	if install == nil {
		return errors.New("official installation identity unavailable; no random replacement generated")
	}
	facts, locale := collectEnvironment(runtime.GOOS, realObserver())
	if *shell != "" {
		facts["shell"] = *shell
	}
	if started, err := time.Parse(time.RFC3339Nano, install.FirstStart); err == nil {
		facts["build_time"] = started.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	if (platform != "win32" && platform != "linux" && platform != "darwin") || (arch != "x64" && arch != "arm64") {
		return errors.New("supported targets: Windows/Linux/macOS, x64/arm64")
	}
	value := bundle{Format: "sub2api-account-telemetry", Version: 2, ExportedAt: time.Now().UTC(), DeviceID: strings.ToLower(install.UserID), MachineID: install.MachineID, CLIVersion: version, Platform: platform, Arch: arch, Locale: locale, Environment: facts}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil || len(raw)+1 > maxBundle {
		return errors.New("invalid or oversized bundle")
	}
	if err := writeBundle(outputPath, append(raw, '\n')); err != nil {
		return fmt.Errorf("%s: %w", outputPath, err)
	}
	fmt.Fprintln(output, "Saved", outputPath, "(real device installation and CLI version; no account, session or credentials).")
	return nil
}

func configCandidates(home, config string) []string {
	if config != "" {
		return []string{filepath.Join(config, ".claude.json")}
	}
	return []string{filepath.Join(home, ".claude.json"), filepath.Join(home, ".claude", ".claude.json")}
}

func readInstall(files []string) *installation {
	for _, name := range files {
		file, err := os.Open(name)
		if err != nil {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
		file.Close()
		if err != nil || len(raw) > 32<<20 {
			continue
		}
		var value installation
		if json.Unmarshal(raw, &value) == nil && devicePattern.MatchString(value.UserID) && strings.TrimSpace(value.MachineID) != "" {
			value.MachineID = strings.TrimSpace(value.MachineID)
			return &value
		}
	}
	return nil
}

func isolatedEnvironment(home, config string) []string {
	env := []string{}
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "ComSpec", "PATHEXT", "TMP", "TEMP", "TMPDIR", "LANG", "LC_ALL"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	return append(env, "HOME="+home, "USERPROFILE="+home, "CLAUDE_CONFIG_DIR="+config, "DISABLE_TELEMETRY=1", "CLAUDE_CODE_ENABLE_TELEMETRY=0", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "ENABLE_CLAUDEAI_MCP_SERVERS=false", "ANTHROPIC_BASE_URL=http://127.0.0.1:9", "HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "ALL_PROXY=http://127.0.0.1:9", "NO_PROXY=")
}

type limitedOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.exceeded = true
		return 0, errors.New("output limit")
	}
	return b.Buffer.Write(p)
}
func execute(file string, args []string, cwd string, env []string, timeout time.Duration, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Dir, cmd.Env = cwd, env
	out := &limitedOutput{limit: limit}
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = out, io.Discard, time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if out.exceeded {
		return nil, errors.New("output limit")
	}
	return out.Bytes(), err
}

func writeBundle(name string, raw []byte) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("cannot create output; existing files are never overwritten")
	}
	_, err = file.Write(raw)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		os.Remove(name)
		return errors.New("cannot write complete output")
	}
	return nil
}
