//go:build unit

package service

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateFourPartVersions(t *testing.T) {
	for _, pair := range [][2]string{{"0.2.8", "0.2.8.1"}, {"0.2.8.9", "0.2.8.10"}, {"0.2.8.99", "0.2.9"}} {
		require.Equal(t, -1, compareVersions(pair[0], pair[1]))
		require.Equal(t, 1, compareVersions(pair[1], pair[0]))
	}
	require.Equal(t, 0, compareVersions("v0.2.8", "0.2.8.0"))
}

func TestSourceBuildUsesBrandUpdateChannel(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "foreign/v99.0.0"}, recentReleases: []*GitHubRelease{
		{TagName: "foreign/v99.0.0"}, {TagName: DefaultReleaseChannel + "/v0.2.13.6"},
	}}
	svc := NewUpdateService(&updateServiceCacheStub{}, client, "0.2.13.5", "source")
	require.NotEmpty(t, DefaultReleaseChannel)
	require.Equal(t, DefaultReleaseChannel, svc.releaseChannel)
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "0.2.13.6", info.LatestVersion)
	require.Zero(t, client.latestCalls)
	require.Equal(t, 1, client.recentCalls)
}

func TestUpdateReleaseChannelIsolation(t *testing.T) {
	svc := newRollbackTestService("0.2.8.1", []*GitHubRelease{
		{TagName: "v1.1.4"}, {TagName: "sub2api/v0.3.0"},
		{TagName: "kdan/v0.2.8.2", Draft: true}, {TagName: "kdan/v0.2.8.4", Prerelease: true},
		{TagName: "kdan/v0.2.8.10"}, {TagName: "kdan/v0.2.8.9"}, {TagName: "kdan/v0.2.8"},
	})
	svc.releaseChannel = "kdan"
	info, err := svc.CheckUpdate(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "0.2.8.10", info.LatestVersion)
	require.True(t, info.HasUpdate)
	versions, err := svc.ListRollbackVersions(context.Background())
	require.NoError(t, err)
	require.Len(t, versions, 1)
	require.Equal(t, "0.2.8", versions[0].Version)
	// A cached public release must not leak into a branded instance.
	svc.releaseChannel = "sub2api"
	info, err = svc.CheckUpdate(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "0.3.0", info.LatestVersion)
}

func TestUpdateContainerQueueAndMissingAgent(t *testing.T) {
	svc := newRollbackTestService("0.2.8", nil)
	svc.container = true
	require.ErrorContains(t, svc.queueContainerUpdate(context.Background(), "0.2.8.1"), "UPDATE_COMPOSE_SOCKET")
	// Unix sockets have a small path limit on macOS.
	directory, err := os.MkdirTemp("/tmp", "updater-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	socket := filepath.Join(directory, "s")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/update", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"status":"running","version":"0.2.8.1"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	svc.composeSocket = socket
	require.NoError(t, svc.queueContainerUpdate(context.Background(), "0.2.8.1"))
	require.False(t, svc.RequiresRestart())
}
