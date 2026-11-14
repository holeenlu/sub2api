//go:build unit

package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/stretchr/testify/require"
)

func TestGatewayRuntimeLifecycleRootOwnsDebugWriterAndClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway.log")
	t.Setenv(debugGatewayBodyEnv, path)
	life := host.NewLifetime()
	svc := ProvideLifetimeGatewayService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, life)
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "construction must not open the configured output")
	var storageClosed atomic.Bool
	life.Add(host.Component{Name: "gateway-test-storage", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error { storageClosed.Store(true); return nil }})
	require.NoError(t, life.Start(context.Background()))
	f, finish := svc.acquireDebugGatewayBodyFile()
	require.NotNil(t, f)
	defer finish()
	t.Cleanup(func() {
		finish()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, life.Cleanup(ctx))
	})
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, life.Cleanup(short), context.DeadlineExceeded)
	firstDone := svc.runtime.debugCloseDone
	require.NotNil(t, firstDone)
	require.False(t, storageClosed.Load())
	require.Contains(t, life.Pending(), "GatewayService")
	_, err = f.WriteString("accepted writer completes before close\n")
	require.NoError(t, err)
	next, _ := svc.acquireDebugGatewayBodyFile()
	require.Nil(t, next, "shutdown must reject new file writers")
	require.ErrorIs(t, svc.Start(context.Background()), errGatewayRuntimeStopped)
	finish()
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, life.Cleanup(join))
	require.Equal(t, firstDone, svc.runtime.debugCloseDone)
	require.True(t, storageClosed.Load())
	_, err = f.Stat()
	require.ErrorIs(t, err, os.ErrClosed)
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "accepted writer completes before close\n", string(content))
}

func TestGatewayRuntimeLifecycleRetainsFileCloseError(t *testing.T) {
	svc := &GatewayService{debugGatewayBodyPath: filepath.Join(t.TempDir(), "gateway.log")}
	require.NoError(t, svc.Start(context.Background()))
	f := svc.debugGatewayBodyFile.Load()
	require.NoError(t, f.Close())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, svc.Stop(ctx), os.ErrClosed)
	done := svc.runtime.debugCloseDone
	require.ErrorIs(t, svc.Stop(ctx), os.ErrClosed)
	require.Equal(t, done, svc.runtime.debugCloseDone)
}

func TestGatewayRuntimeLifecycleReportsDebugOpenFailure(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "regular-file")
	require.NoError(t, os.WriteFile(parent, []byte("not a directory"), 0600))
	svc := &GatewayService{debugGatewayBodyPath: filepath.Join(parent, "gateway.log")}
	require.ErrorContains(t, svc.Start(context.Background()), "create gateway debug log directory")
	require.Nil(t, svc.accountTelemetryWorker)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
}

type telemetryLifecycleReleaseCache struct {
	*accountTelemetryCacheStub
	entered chan context.Context
	release chan struct{}
	err     error
	calls   atomic.Int32
}

func (c *telemetryLifecycleReleaseCache) ReleaseAccountTelemetryLeader(ctx context.Context, _ string) error {
	c.calls.Add(1)
	if c.entered != nil {
		c.entered <- ctx
	}
	if c.release != nil {
		<-c.release
	}
	return c.err
}

func TestAccountTelemetryLifecycleRetainsReleaseJoinAfterDeadline(t *testing.T) {
	svc, _, cache, _ := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	blocking := &telemetryLifecycleReleaseCache{accountTelemetryCacheStub: cache, entered: make(chan context.Context, 1), release: make(chan struct{})}
	svc.cache = blocking
	var release sync.Once
	t.Cleanup(func() {
		release.Do(func() { close(blocking.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Stop(ctx))
	})
	require.Nil(t, svc.accountTelemetryWorker)
	require.NoError(t, svc.StartAccountTelemetry(context.Background()))
	worker := svc.accountTelemetryWorker
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.StopAccountTelemetry(short), context.DeadlineExceeded)
	select {
	case <-blocking.entered:
	case <-time.After(time.Second):
		t.Fatal("owned leader release did not start")
	}
	require.Same(t, worker, svc.accountTelemetryWorker)
	require.ErrorIs(t, svc.StartAccountTelemetry(context.Background()), errGatewayRuntimeStopped)
	release.Do(func() { close(blocking.release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.StopAccountTelemetry(join))
	require.EqualValues(t, 1, blocking.calls.Load())
}

func TestAccountTelemetryLifecyclePreservesReleaseFailure(t *testing.T) {
	svc, _, cache, _ := newAccountTelemetryFixture(t)
	svc.telemetrySettings.storeAdvancedSettingsSnapshot(defaultOpsAdvancedSettings())
	failure := errors.New("leader release unavailable")
	broken := &telemetryLifecycleReleaseCache{accountTelemetryCacheStub: cache, err: failure}
	svc.cache = broken
	svc.debugGatewayBodyPath = filepath.Join(t.TempDir(), "gateway.log")
	require.NoError(t, svc.Start(context.Background()))
	f := svc.debugGatewayBodyFile.Load()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, svc.Stop(ctx), failure)
	require.ErrorIs(t, svc.Stop(ctx), failure)
	require.EqualValues(t, 1, broken.calls.Load())
	_, err := f.Stat()
	require.ErrorIs(t, err, os.ErrClosed, "joined telemetry release failure must not leak independent file")
}

func TestAccountTelemetryLifecycleParentCancellationJoinsHTTP(t *testing.T) {
	svc, _, _, _ := newAccountTelemetryFixture(t)
	upstream := &accountTelemetryBlockingHTTP{started: make(chan struct{}, 1), canceled: make(chan struct{}, 1)}
	upstream.block.Store(true)
	svc.httpUpstream = upstream
	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		ctx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		require.NoError(t, svc.Stop(ctx))
	})
	require.NoError(t, svc.StartAccountTelemetry(parent))
	select {
	case <-upstream.started:
	case <-time.After(3 * time.Second):
		t.Fatal("real telemetry HTTP call did not start")
	}
	cancel()
	select {
	case <-upstream.canceled:
	case <-time.After(time.Second):
		t.Fatal("application cancellation did not reach telemetry HTTP")
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.StopAccountTelemetry(ctx))
	select {
	case <-svc.accountTelemetryWorker.done:
	default:
		t.Fatal("Stop returned before the real telemetry owner completed")
	}
}
