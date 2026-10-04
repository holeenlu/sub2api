//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	latestErr      error
	release        *GitHubRelease
	recentReleases []*GitHubRelease
	recentErr      error
	latestCalls    int
	recentCalls    int
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	s.latestCalls++
	return s.release, s.latestErr
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	s.recentCalls++
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := newUpstreamUpdateTestService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132",
				Name:    "v0.1.132",
			},
		},
		"0.1.132",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newUpstreamUpdateTestService(cache UpdateCache, client GitHubReleaseClient, version, buildType string) *UpdateService {
	svc := NewUpdateService(cache, client, version, buildType)
	svc.releaseChannel = ""
	return svc
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return newUpstreamUpdateTestService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148", PublishedAt: "2026-07-09T00:00:00Z"},                       // newer than current: excluded
		{TagName: "v0.1.147", PublishedAt: "2026-07-08T00:00:00Z"},                       // current: excluded
		{TagName: "v0.1.146-rc1", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // prerelease: excluded
		{TagName: "v0.1.146", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.144", versions[1].Version)
	require.Equal(t, "0.1.143", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146", versions[0].Version)
	require.Equal(t, "0.1.145", versions[1].Version)
	require.Equal(t, "0.1.144", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.148"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := newUpstreamUpdateTestService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148"},
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
		{TagName: "v0.1.145"},
		{TagName: "v0.1.144"},
		{TagName: "v0.1.143"},
		{TagName: "v0.1.142"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	for _, target := range []string{
		"",         // empty
		"0.1.147",  // current version
		"v0.1.147", // current version with prefix
		"0.1.148",  // newer than current
		"0.1.142",  // older than the 3 most recent
		"9.9.9",    // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147"},
		{TagName: "v0.1.146"},
	}
	svc := newRollbackTestService("0.1.147", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

// update.check_enabled=false (wire.go feeds it through WithCheckEnabled) must
// short-circuit every network-backed operation without touching GitHub.
func TestUpdateServiceCheckDisabledSkipsGitHub(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		release:        &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
		recentReleases: []*GitHubRelease{{TagName: "v0.1.146"}},
	}
	svc := newUpstreamUpdateTestService(&updateServiceCacheStub{}, client, "0.1.147", "release").WithCheckEnabled(false)

	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, info.Disabled)
	require.False(t, info.HasUpdate)
	require.Equal(t, "0.1.147", info.CurrentVersion)
	require.Equal(t, "0.1.147", info.LatestVersion)
	require.Equal(t, "release", info.BuildType)

	require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrUpdateCheckDisabled)
	require.ErrorIs(t, svc.RollbackToVersion(context.Background(), "0.1.146"), ErrUpdateCheckDisabled)

	versions, err := svc.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Empty(t, versions)

	require.Zero(t, client.latestCalls+client.recentCalls, "no GitHub call may happen while the update check is disabled")
}

// The default keeps upstream behaviour: checks are enabled unless configured off.
func TestUpdateServiceCheckEnabledByDefault(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}
	svc := newUpstreamUpdateTestService(&updateServiceCacheStub{}, client, "0.1.147", "release")

	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.False(t, info.Disabled)
	require.True(t, info.HasUpdate)
	require.Equal(t, 1, client.latestCalls)
}

// The upstream baseline is build-time information and must be reported on every
// CheckUpdate path, including the disabled and remote-failure ones.
func TestUpdateServiceReportsUpstreamVersion(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		release: &GitHubRelease{TagName: "v9.9.9", Name: "v9.9.9"},
	}
	svc := newUpstreamUpdateTestService(&updateServiceCacheStub{}, client, "0.1.147", "release").
		WithUpstreamVersion(" v0.2.1 ")

	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "v0.2.1", info.UpstreamVersion)

	disabled, err := svc.WithCheckEnabled(false).CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.True(t, disabled.Disabled)
	require.Equal(t, "v0.2.1", disabled.UpstreamVersion)

	plain := newUpstreamUpdateTestService(&updateServiceCacheStub{}, client, "0.1.147", "release")
	info, err = plain.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Empty(t, info.UpstreamVersion, "upstream builds inject nothing and must omit the field")
}

func TestUpdateServiceReportsRunningBuildCommit(t *testing.T) {
	const commit = "2776c84a5ebcc94d2fba1f551c200ab1d9ffe149"
	cache := &updateServiceCacheStub{}
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v9.9.9"}}
	svc := newUpstreamUpdateTestService(cache, client, "0.2.1", "release").WithBuildCommit(commit)
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, commit, info.BuildCommit)
	// A new binary sharing an existing release cache must report its own commit.
	svc.WithBuildCommit("abcdef012")
	info, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.True(t, info.Cached)
	require.Equal(t, "abcdef012", info.BuildCommit)
	svc.WithCheckEnabled(false)
	info, err = svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "abcdef012", info.BuildCommit)
	svc.WithCheckEnabled(true)
	client.latestErr = errors.New("offline")
	cache.data = ""
	info, err = svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "abcdef012", info.BuildCommit)
	for _, invalid := range []string{"", "unknown", "docker", "123xyz7"} {
		svc.WithBuildCommit(invalid)
		info, err = svc.CheckUpdate(context.Background(), true)
		require.NoError(t, err)
		require.Empty(t, info.BuildCommit)
	}
}
