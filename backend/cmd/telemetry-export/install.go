package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Same distribution used by Anthropic's install.sh / install.ps1. Never pin a
// stale latest version; resolve it once per new installation and verify bytes.
const officialDistribution = "https://downloads.claude.ai/claude-code-releases"

var releaseVersion = regexp.MustCompile(`^[0-9]+[.][0-9]+[.][0-9]+$`)
var checksumPattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func executableName(platform string) string {
	if platform == "windows" {
		return "claude.exe"
	}
	return "claude"
}
func nativeCLI(file string) bool {
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	f, err := os.Open(file)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [4]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil {
		return false
	}
	switch runtime.GOOS {
	case "windows":
		return strings.EqualFold(filepath.Ext(file), ".exe") && magic[0] == 'M' && magic[1] == 'Z'
	case "linux":
		return info.Mode()&0111 != 0 && magic == [4]byte{0x7f, 'E', 'L', 'F'}
	case "darwin":
		for _, supported := range [][4]byte{{0xcf, 0xfa, 0xed, 0xfe}, {0xfe, 0xed, 0xfa, 0xcf}, {0xca, 0xfe, 0xba, 0xbe}, {0xbe, 0xba, 0xfe, 0xca}, {0xca, 0xfe, 0xba, 0xbf}, {0xbf, 0xba, 0xfe, 0xca}} {
			if magic == supported {
				return info.Mode()&0111 != 0
			}
		}
	}
	return false
}
func findCLI(requested, home string) (string, error) {
	if file, err := exec.LookPath(requested); err == nil && nativeCLI(file) {
		return filepath.Abs(file)
	}
	if requested != "claude" {
		return "", errors.New("specified --claude executable unavailable; correct the path or omit it to auto-install")
	}
	file := filepath.Join(home, ".local", "bin", executableName(runtime.GOOS))
	if nativeCLI(file) {
		return file, nil
	}
	return "", nil
}
func installPlatform(platform, arch string, o observer) (string, error) {
	if arch == "amd64" {
		arch = "x64"
	}
	if platform == "darwin" && o.command("sysctl", "-n", "sysctl.proc_translated") == "1" {
		arch = "arm64"
	}
	if platform == "windows" {
		platform = "win32"
		if strings.EqualFold(first(o.env("PROCESSOR_ARCHITEW6432"), o.env("PROCESSOR_ARCHITECTURE")), "ARM64") {
			arch = "arm64"
		}
	}
	if (platform != "linux" && platform != "darwin" && platform != "win32") || (arch != "x64" && arch != "arm64") {
		return "", errors.New("unsupported installation platform")
	}
	suffix := ""
	if platform == "linux" {
		if o.exists("/lib/ld-musl-x86_64.so.1") || o.exists("/lib/ld-musl-aarch64.so.1") || o.exists("/lib/libc.musl-x86_64.so.1") || o.exists("/lib/libc.musl-aarch64.so.1") || strings.Contains(o.command("ldd", "--version"), "musl") {
			suffix = "-musl"
		}
	}
	return platform + "-" + arch + suffix, nil
}
func officialClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || req.URL.Hostname() != "downloads.claude.ai" {
			return errors.New("unexpected download redirect")
		}
		return nil
	}}
}
func download(ctx context.Context, client *http.Client, url string, dst io.Writer, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return errors.New("invalid official download URL")
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("official download unavailable; check network or HTTPS proxy")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength > limit {
		return errors.New("official download returned invalid status or size")
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return errors.New("official download interrupted")
	}
	if n > limit {
		return errors.New("official download exceeds size limit")
	}
	return nil
}
func fetchOfficial(ctx context.Context, client *http.Client, base, platform, dir string, progress io.Writer) (string, string, error) {
	var versionBytes, manifestBytes strings.Builder
	if err := download(ctx, client, base+"/latest", &versionBytes, 128); err != nil {
		return "", "", err
	}
	version := strings.TrimSpace(versionBytes.String())
	if !releaseVersion.MatchString(version) {
		return "", "", errors.New("official latest version is invalid")
	}
	if err := download(ctx, client, base+"/"+version+"/manifest.json", &manifestBytes, 2<<20); err != nil {
		return "", "", err
	}
	var manifest struct {
		Platforms map[string]struct {
			Checksum string `json:"checksum"`
			Size     int64  `json:"size"`
		} `json:"platforms"`
	}
	if json.Unmarshal([]byte(manifestBytes.String()), &manifest) != nil {
		return "", "", errors.New("invalid official release manifest")
	}
	entry, ok := manifest.Platforms[platform]
	if !ok || !checksumPattern.MatchString(entry.Checksum) || entry.Size < 0 || entry.Size > 1<<30 {
		return "", "", errors.New("official manifest has no valid binary for this platform")
	}
	name := "claude"
	if strings.HasPrefix(platform, "win32-") {
		name += ".exe"
	}
	file, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", errors.New("cannot stage official installer")
	}
	hash := sha256.New()
	limit := int64(1 << 30)
	if entry.Size > 0 {
		limit = entry.Size
	}
	fmt.Fprintf(progress, "Downloading official Claude Code %s (%s)...\n", version, platform)
	err = download(ctx, client, base+"/"+version+"/"+platform+"/"+name, io.MultiWriter(file, hash), limit)
	info, statErr := file.Stat()
	closeErr := file.Close()
	if err != nil {
		return "", "", err
	}
	if statErr != nil || closeErr != nil || (entry.Size > 0 && info.Size() != entry.Size) || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), entry.Checksum) {
		os.Remove(file.Name())
		return "", "", errors.New("official binary integrity check failed")
	}
	if err := os.Chmod(file.Name(), 0700); err != nil {
		return "", "", errors.New("cannot make official installer executable")
	}
	return file.Name(), version, nil
}
func installLatest(home string, progress io.Writer) (string, error) {
	platform, err := installPlatform(runtime.GOOS, runtime.GOARCH, realObserver())
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "claude-install-")
	if err != nil {
		return "", errors.New("temporary directory unavailable")
	}
	defer os.RemoveAll(scratch)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	binary, version, err := fetchOfficial(ctx, officialClient(), officialDistribution, platform, scratch, progress)
	if err != nil {
		return "", err
	}
	// Standard user-home install through the verified official binary. Unlike
	// telemetry initialization, installation needs access to the official CDN.
	env := []string{}
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "ComSpec", "PATHEXT", "TMP", "TEMP", "TMPDIR", "LANG", "LC_ALL", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = append(env, "HOME="+home, "USERPROFILE="+home, "DISABLE_TELEMETRY=1", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1")
	fmt.Fprintln(progress, "Installing verified official CLI into your user home...")
	if _, err := execute(binary, []string{"install", version}, scratch, env, 5*time.Minute, 2<<20); err != nil {
		return "", errors.New("official installation failed; check system dependencies, home permissions and network")
	}
	file := filepath.Join(home, ".local", "bin", executableName(runtime.GOOS))
	if !nativeCLI(file) {
		return "", errors.New("official installation did not produce the expected executable")
	}
	verify, err := execute(file, []string{"--version"}, scratch, isolatedEnvironment(scratch, filepath.Join(scratch, ".claude")), 15*time.Second, 4096)
	match := versionPattern.FindSubmatch(verify)
	if err != nil || len(match) != 2 || string(match[1]) != version {
		return "", errors.New("installed official CLI version could not be verified")
	}
	fmt.Fprintln(progress, "Installed official Claude Code", version)
	return file, nil
}
