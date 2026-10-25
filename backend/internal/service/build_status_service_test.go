//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildStatusServiceManualDeploymentStatus(t *testing.T) {
	for _, buildType := range []string{"source", "release", ""} {
		t.Run(buildType, func(t *testing.T) {
			svc := ProvideBuildStatusService(BuildInfo{
				Version:         "rebuild-dev",
				BuildType:       buildType,
				UpstreamVersion: " v0.2.17 ",
				BuildCommit:     " ABCDEF0123456789 ",
			})
			info := svc.Status()
			require.Equal(t, "manual", info.UpdateMethod)
			require.True(t, info.Disabled)
			require.False(t, info.HasUpdate)
			require.False(t, info.Cached)
			require.Equal(t, "rebuild-dev", info.CurrentVersion)
			require.Equal(t, info.CurrentVersion, info.LatestVersion)
			require.Equal(t, buildType, info.BuildType)
			require.Equal(t, "v0.2.17", info.UpstreamVersion)
			require.Equal(t, "abcdef0123456789", info.BuildCommit)
			require.Contains(t, info.Warning, "manually")
		})
	}
}

func TestBuildStatusServiceReportsRunningBuildCommit(t *testing.T) {
	const commit = "2776c84a5ebcc94d2fba1f551c200ab1d9ffe149"
	svc := NewBuildStatusService("rebuild-dev", "source").WithBuildCommit(commit)
	info := svc.Status()
	require.Equal(t, commit, info.BuildCommit)
	require.Empty(t, info.UpstreamVersion)

	for _, invalid := range []string{"", "unknown", "docker", "123xyz7", "abcdef", strings.Repeat("a", 41)} {
		svc.WithBuildCommit(commit).WithBuildCommit(invalid)
		info = svc.Status()
		require.Empty(t, info.BuildCommit)
	}
}
