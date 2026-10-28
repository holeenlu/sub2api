//go:build unit

package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type lifecyclePluginRepo struct {
	PluginRepository
	started chan struct{}
}

func (r *lifecyclePluginRepo) List(ctx context.Context) ([]*PluginInstallation, error) {
	close(r.started)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestPluginLifecycleStopCancelsReconcileAndJoinsStartup(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.DataDir = t.TempDir()
	repo := &lifecyclePluginRepo{started: make(chan struct{})}
	m := NewPluginManager(repo, nil, cfg, PluginHostInfo{}, nil)
	started := make(chan error, 1)
	go func() { started <- m.Start(context.Background()) }()
	<-repo.started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, m.Stop(ctx))
	require.NoError(t, <-started)
	require.Error(t, m.Start(context.Background()))
	require.NoError(t, m.Stop(ctx))
}
func TestPluginLifecycleDrainFencesAndJoinsRuntimeLeases(t *testing.T) {
	rt := &pluginRuntime{done: make(chan struct{})}
	require.True(t, rt.beginRequest())
	m := &PluginManager{runtimes: map[int64]*pluginRuntime{1: rt}}
	m.route.Store(&pluginRoute{pluginID: 1, runtime: rt, rolloutPercent: 100})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, m.Drain(ctx), context.DeadlineExceeded)
	require.False(t, rt.beginRequest())
	require.NotEmpty(t, m.route.Load().unavailable)
	rt.finishRequest()
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, m.Stop(bounded))
	require.Empty(t, m.runtimes)
}
func TestPluginLifecycleOperationWaitAndNonCooperativeJoinAreBounded(t *testing.T) {
	m := &PluginManager{}
	_, release, err := m.beginOperation(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, _, err = m.beginOperation(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, m.Stop(ctx), context.DeadlineExceeded)
	release()
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, m.Stop(bounded))
}
func TestPluginLifecycleAcceptedPublishCannotReopenDrainingRoute(t *testing.T) {
	m := &PluginManager{runtimes: make(map[int64]*pluginRuntime)}
	_, release, err := m.beginOperation(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, m.Drain(ctx), context.Canceled)
	rt := &pluginRuntime{done: make(chan struct{})}
	m.mu.Lock()
	m.publishRuntimeLocked(&PluginInstallation{ID: 1}, rt)
	m.mu.Unlock()
	release()
	require.NotEmpty(t, m.route.Load().unavailable)
	require.False(t, rt.beginRequest())
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, m.Stop(bounded))
}
