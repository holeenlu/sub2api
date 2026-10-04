package service

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ReleaseChannel may be overridden by a release build. Plain SDK builds retain
// the brand's channel rather than querying the shared repository Latest tag.
var ReleaseChannel = DefaultReleaseChannel

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
	// ErrUpdateCheckDisabled is returned by every network-backed update
	// operation while update.check_enabled is false.
	ErrUpdateCheckDisabled = infraerrors.Forbidden("UPDATE_CHECK_DISABLED", "online update check is disabled by configuration")
)

const (
	updateCacheKey = "update_check_cache"
	updateCacheTTL = 1200 // 20 minutes
	// githubRepo is the release repository consulted by the update check; brand
	// builds override DefaultReleaseRepo in brand.go.
	githubRepo = DefaultReleaseRepo

	// updateDisabledWarning is surfaced to the admin UI in place of a version
	// comparison when the online update check is turned off.
	updateDisabledWarning = "online update check is disabled by configuration"

	// Security: allowed download domains for updates
	allowedDownloadHost = "github.com"
	allowedAssetHost    = "objects.githubusercontent.com"

	// Security: max download size (500MB)
	maxDownloadSize = 500 * 1024 * 1024

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 100
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// UpdateService handles software updates
type UpdateService struct {
	releaseChannel string
	composeSocket  string
	container      bool

	cache          UpdateCache
	githubClient   GitHubReleaseClient
	currentVersion string
	buildType      string // "source" for manual builds, "release" for CI builds
	checkEnabled   bool   // update.check_enabled; false disables all remote lookups
	// upstreamVersion is the upstream Sub2API version this build is based on
	// (e.g. "v0.2.1"); empty for upstream builds that inject nothing.
	upstreamVersion string
	buildCommit     string
}

// NewUpdateService creates a new UpdateService. The online update check is on
// by default (upstream behaviour); wire.go applies update.check_enabled.
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return &UpdateService{
		releaseChannel: ReleaseChannel,
		composeSocket:  os.Getenv("UPDATE_COMPOSE_SOCKET"),
		container:      inContainer(),
		cache:          cache,
		githubClient:   githubClient,
		currentVersion: version,
		buildType:      buildType,
		checkEnabled:   true,
	}
}

// WithUpstreamVersion records the upstream version this build is based on.
func (s *UpdateService) WithUpstreamVersion(version string) *UpdateService {
	if s == nil {
		return nil
	}
	s.upstreamVersion = strings.TrimSpace(version)
	return s
}

// WithBuildCommit records the identity of the running binary, not a cached release.
func (s *UpdateService) WithBuildCommit(commit string) *UpdateService {
	if s == nil {
		return nil
	}
	s.buildCommit = ""
	commit = strings.TrimSpace(commit)
	if len(commit) < 7 || len(commit) > 40 {
		return s
	}
	for _, c := range commit {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return s
		}
	}
	s.buildCommit = strings.ToLower(commit)
	return s
}

// WithCheckEnabled toggles the online update check (update.check_enabled).
func (s *UpdateService) WithCheckEnabled(enabled bool) *UpdateService {
	if s == nil {
		return nil
	}
	s.checkEnabled = enabled
	return s
}

// disabledInfo describes the "no remote lookup performed" state.
func (s *UpdateService) disabledInfo() *UpdateInfo {
	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  s.currentVersion,
		HasUpdate:      false,
		Warning:        updateDisabledWarning,
		BuildType:      s.buildType,
		// Build-time information is independent of the remote lookup.
		UpstreamVersion: s.upstreamVersion,
		BuildCommit:     s.buildCommit,
		Disabled:        true,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	UpdateMethod    string                 `json:"update_method"`
	ReleaseChannel  string                 `json:"release_channel,omitempty"`
	ContainerUpdate *ContainerUpdateStatus `json:"container_update,omitempty"`
	BuildCommit     string                 `json:"build_commit,omitempty"`
	CurrentVersion  string                 `json:"current_version"`
	LatestVersion   string                 `json:"latest_version"`
	HasUpdate       bool                   `json:"has_update"`
	ReleaseInfo     *ReleaseInfo           `json:"release_info,omitempty"`
	Cached          bool                   `json:"cached"`
	Warning         string                 `json:"warning,omitempty"`
	BuildType       string                 `json:"build_type"` // "source" or "release"
	// UpstreamVersion is the upstream Sub2API version this build is based on,
	// e.g. "v0.2.1". Omitted when the build injects nothing.
	UpstreamVersion string `json:"upstream_version,omitempty"`
	// Disabled reports that update.check_enabled is false, so LatestVersion is
	// simply the current version and no remote lookup was performed.
	Disabled bool `json:"disabled,omitempty"`
}

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.146"
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	APIURL             string `json:"url"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (info *UpdateInfo, err error) {
	defer func() { s.decorateUpdateMethod(ctx, info) }()
	if !s.checkEnabled {
		return s.disabledInfo(), nil
	}

	// Try cache first
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	// Fetch from GitHub
	info, err = s.fetchLatestRelease(ctx)
	if err != nil {
		// Return cached on error
		if cached, cacheErr := s.getFromCache(ctx); cacheErr == nil && cached != nil {
			cached.Warning = "Using cached data: " + err.Error()
			return cached, nil
		}
		return &UpdateInfo{
			CurrentVersion:  s.currentVersion,
			LatestVersion:   s.currentVersion,
			HasUpdate:       false,
			Warning:         err.Error(),
			BuildType:       s.buildType,
			UpstreamVersion: s.upstreamVersion,
			BuildCommit:     s.buildCommit,
		}, nil
	}

	// Cache result
	s.saveToCache(ctx, info)
	return info, nil
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) error {
	if !s.checkEnabled {
		return ErrUpdateCheckDisabled
	}

	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return err
	}

	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}

	if s.container || s.composeSocket != "" {
		return s.queueContainerUpdate(ctx, info.LatestVersion)
	}
	return s.applyReleaseAssets(ctx, info.ReleaseInfo.Assets)
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, releaseAssets []Asset) error {
	// Find matching archive and checksum for current platform
	archiveName := s.getArchiveName()
	var downloadURL string
	var downloadName string
	var checksumURL string

	for _, asset := range releaseAssets {
		if strings.Contains(asset.Name, archiveName) && !strings.HasSuffix(asset.Name, ".txt") {
			downloadURL = asset.DownloadURL
			downloadName = filepath.Base(asset.Name)
		}
		if asset.Name == "checksums.txt" {
			checksumURL = asset.DownloadURL
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	if checksumURL == "" {
		return fmt.Errorf("release has no checksums.txt; refusing unverified update")
	}

	// SECURITY: Validate download URL is from trusted domain
	if err := validateDownloadURL(downloadURL); err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if checksumURL != "" {
		if err := validateDownloadURL(checksumURL); err != nil {
			return fmt.Errorf("invalid checksum URL: %w", err)
		}
	}

	// Get current executable path
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)

	// Create temp directory in the SAME directory as executable
	// This ensures os.Rename is atomic (same filesystem)
	tempDir, err := os.MkdirTemp(exeDir, ".kdan-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Download archive
	archivePath := filepath.Join(tempDir, downloadName)
	if err := s.downloadFile(ctx, downloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Verify checksum if available
	if checksumURL != "" {
		if err := s.verifyChecksum(ctx, archivePath, checksumURL); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Extract binary from archive
	newBinaryPath := filepath.Join(tempDir, "kdan")
	if err := s.extractBinary(archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}

	// Set executable permission before replacement
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}

	// Atomic replacement using rename pattern:
	// 1. Rename current -> backup (atomic on Unix)
	// 2. Rename new -> current (atomic on Unix, same filesystem)
	// If step 2 fails, restore backup
	backupPath := exePath + ".backup"

	// Remove old backup if exists
	_ = os.Remove(backupPath)

	// Step 1: Move current binary to backup
	if err := os.Rename(exePath, backupPath); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	// Step 2: Move new binary to target location (atomic, same filesystem)
	if err := os.Rename(newBinaryPath, exePath); err != nil {
		// Restore backup on failure
		if restoreErr := os.Rename(backupPath, exePath); restoreErr != nil {
			return fmt.Errorf("replace failed and restore failed: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace failed (restored backup): %w", err)
	}

	// Success - backup file is kept for rollback capability
	// It will be cleaned up on next successful update
	return nil
}

// Rollback restores the previous version
func (s *UpdateService) Rollback() error {
	if s.container || s.composeSocket != "" {
		return fmt.Errorf("select a published version for Compose rollback")
	}
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	backupFile := exePath + ".backup"
	if _, err := os.Stat(backupFile); os.IsNotExist(err) {
		return fmt.Errorf("no backup found")
	}

	// Replace current with backup
	if err := os.Rename(backupFile, exePath); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	if !s.checkEnabled {
		return []RollbackVersion{}, nil
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     s.releaseVersion(r.TagName),
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	if !s.checkEnabled {
		return ErrUpdateCheckDisabled
	}

	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if s.releaseVersion(r.TagName) == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	if s.container || s.composeSocket != "" {
		return s.queueContainerUpdate(ctx, target)
	}
	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.downloadURL(),
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	releases, err := s.recentChannelReleases(ctx, rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v := s.releaseVersion(r.TagName)
		if v == "" || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			s.releaseVersion(candidates[i].TagName),
			s.releaseVersion(candidates[j].TagName),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) fetchLatestRelease(ctx context.Context) (*UpdateInfo, error) {
	release, err := s.latestChannelRelease(ctx)
	if err != nil {
		return nil, err
	}

	latestVersion := s.releaseVersion(release.TagName)

	assets := make([]Asset, len(release.Assets))
	for i, a := range release.Assets {
		assets[i] = Asset{
			Name:        a.Name,
			DownloadURL: a.downloadURL(),
			Size:        a.Size,
		}
	}

	return &UpdateInfo{
		CurrentVersion: s.currentVersion,
		LatestVersion:  latestVersion,
		HasUpdate:      compareVersions(s.currentVersion, latestVersion) < 0,
		ReleaseInfo: &ReleaseInfo{
			Name:        release.Name,
			Body:        release.Body,
			PublishedAt: release.PublishedAt,
			HTMLURL:     release.HTMLURL,
			Assets:      assets,
		},
		Cached:          false,
		BuildType:       s.buildType,
		UpstreamVersion: s.upstreamVersion,
		BuildCommit:     s.buildCommit,
	}, nil
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func (s *UpdateService) getArchiveName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s_%s", osName, arch)
}

// validateDownloadURL checks if the URL is from an allowed domain
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}

	// Check against allowed hosts
	host := parsedURL.Host
	// GitHub release URLs can be from github.com or objects.githubusercontent.com
	if host != allowedDownloadHost &&
		!strings.HasSuffix(host, "."+allowedDownloadHost) &&
		host != allowedAssetHost &&
		!strings.HasSuffix(host, "."+allowedAssetHost) {
		return fmt.Errorf("download from untrusted host: %s", host)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find expected hash in checksums file
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			if parts[0] == actualHash {
				return nil
			}
			return fmt.Errorf("checksum mismatch: expected %s, got %s", parts[0], actualHash)
		}
	}

	return fmt.Errorf("checksum not found for %s", fileName)
}

func (s *UpdateService) extractBinary(archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = f

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				continue // Skip directories and special files
			}

			// Only extract the specific binary we need
			// Accept both the KDAN binary name and the upstream sub2api
			// name so archives produced before the rename still update cleanly.
			if baseName == "kdan" || baseName == "kdan.exe" ||
				baseName == "sub2api" || baseName == "sub2api.exe" {
				// Additional security: limit file size (max 500MB)
				const maxBinarySize = 500 * 1024 * 1024
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				// Use LimitReader to prevent decompression bombs
				limited := io.LimitReader(tr, maxBinarySize)
				if _, err := io.Copy(out, limited); err != nil {
					_ = out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}
				return nil
			}
		}
		return fmt.Errorf("binary not found in archive")
	}

	// Direct copy for non-tar files (with size limit)
	const maxBinarySize = 500 * 1024 * 1024
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize)
	if _, err := io.Copy(out, limited); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Channel     string       `json:"channel"`
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}

	if cached.Channel != s.releaseChannel || time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	return &UpdateInfo{
		CurrentVersion:  s.currentVersion,
		LatestVersion:   cached.Latest,
		HasUpdate:       compareVersions(s.currentVersion, cached.Latest) < 0,
		ReleaseInfo:     cached.ReleaseInfo,
		Cached:          true,
		BuildType:       s.buildType,
		UpstreamVersion: s.upstreamVersion,
		BuildCommit:     s.buildCommit,
	}, nil
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Channel     string       `json:"channel"`
		Latest      string       `json:"latest"`
		ReleaseInfo *ReleaseInfo `json:"release_info"`
		Timestamp   int64        `json:"timestamp"`
	}{
		Channel:     s.releaseChannel,
		Latest:      info.LatestVersion,
		ReleaseInfo: info.ReleaseInfo,
		Timestamp:   time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	currentParts := parseVersion(current)
	latestParts := parseVersion(latest)

	for i := 0; i < 4; i++ {
		if currentParts[i] < latestParts[i] {
			return -1
		}
		if currentParts[i] > latestParts[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) [4]int {
	v = strings.TrimPrefix(v, "v")
	v = strings.SplitN(v, "+", 2)[0]
	if idx := strings.IndexByte(v, '-'); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := [4]int{0, 0, 0, 0}
	for i := 0; i < len(parts) && i < 4; i++ {
		if parsed, err := strconv.Atoi(parts[i]); err == nil {
			result[i] = parsed
		}
	}
	return result
}

var stableReleaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(\.(0|[1-9][0-9]*))?$`)

func (s *UpdateService) releaseVersion(tag string) string {
	if s.releaseChannel != "" {
		prefix := s.releaseChannel + "/v"
		if !strings.HasPrefix(tag, prefix) {
			return ""
		}
		tag = strings.TrimPrefix(tag, prefix)
	} else {
		tag = strings.TrimPrefix(tag, "v")
	}
	if !stableReleaseVersion.MatchString(tag) {
		return ""
	}
	return tag
}

func (s *UpdateService) latestChannelRelease(ctx context.Context) (*GitHubRelease, error) {
	if s.releaseChannel == "" {
		return s.githubClient.FetchLatestRelease(ctx, githubRepo)
	}
	releases, err := s.recentChannelReleases(ctx, 100)
	if err != nil {
		return nil, err
	}
	var latest *GitHubRelease
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease || s.releaseVersion(r.TagName) == "" {
			continue
		}
		if latest == nil || compareVersions(s.releaseVersion(r.TagName), s.releaseVersion(latest.TagName)) > 0 {
			latest = r
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no published release for channel %s", s.releaseChannel)
	}
	return latest, nil
}

func (a GitHubAsset) downloadURL() string {
	if a.APIURL != "" {
		return a.APIURL
	}
	return a.BrowserDownloadURL
}

func (s *UpdateService) recentChannelReleases(ctx context.Context, limit int) ([]*GitHubRelease, error) {
	if s.releaseChannel != "" {
		if client, ok := s.githubClient.(interface {
			FetchChannelReleases(context.Context, string, string, int) ([]*GitHubRelease, error)
		}); ok {
			return client.FetchChannelReleases(ctx, githubRepo, s.releaseChannel, limit)
		}
	}
	return s.githubClient.FetchRecentReleases(ctx, githubRepo, limit)
}
