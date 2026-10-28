//go:build unit

package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type explicitRunner interface {
	Start(context.Context) error
	Drain(context.Context) error
	Stop(context.Context) error
}
type explicitRunnerSettings struct{ SettingRepository }

func (explicitRunnerSettings) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyExcelBPSEnabled {
		return "true", nil
	}
	return "{\"enabled\":true,\"interval_minutes\":30,\"debounce_minutes\":1}", nil
}

type explicitProbeGate struct {
	entered chan context.Context
	release chan struct{}
	calls   atomic.Int32
}

func (g *explicitProbeGate) wait(ctx context.Context) ([]Account, error) {
	g.calls.Add(1)
	g.entered <- ctx
	<-g.release
	return nil, ctx.Err()
}

type explicitUpstreamRepo struct {
	AccountRepository
	*explicitProbeGate
}

func (r *explicitUpstreamRepo) ListDueUpstreamBillingProbeAccounts(ctx context.Context, _ time.Time, _ int) ([]Account, error) {
	return r.wait(ctx)
}

type explicitOllamaRepo struct {
	AccountRepository
	ollamaCloudUsageRepository
	*explicitProbeGate
}

func (r *explicitOllamaRepo) ListDueOllamaCloudUsageAccounts(ctx context.Context, _ time.Time, _ time.Duration, _ time.Duration, _ int) ([]Account, error) {
	return r.wait(ctx)
}

type explicitOpenCodeRepo struct {
	AccountRepository
	openCodeGoUsageRepository
	*explicitProbeGate
}

func (r *explicitOpenCodeRepo) ListDueOpenCodeGoUsageAccounts(ctx context.Context, _ time.Time, _ time.Duration, _ time.Duration, _ int) ([]Account, error) {
	return r.wait(ctx)
}
func TestExplicitProbeProviderLifecycleOwnsAcceptedCycle(t *testing.T) {
	for _, name := range []string{"upstream", "ollama", "opencode"} {
		t.Run(name, func(t *testing.T) {
			g := &explicitProbeGate{entered: make(chan context.Context, 1), release: make(chan struct{})}
			settings := NewSettingService(explicitRunnerSettings{}, nil)
			var svc explicitRunner
			switch name {
			case "upstream":
				svc = ProvideUpstreamBillingProbeService(&explicitUpstreamRepo{explicitProbeGate: g}, nil, settings, nil, nil)
			case "ollama":
				svc = ProvideOllamaCloudUsageService(&explicitOllamaRepo{explicitProbeGate: g}, nil, settings, nil, &config.Config{}, nil, nil)
			case "opencode":
				svc = ProvideOpenCodeGoUsageService(&explicitOpenCodeRepo{explicitProbeGate: g}, nil, settings, nil, nil)
			}
			var releaseOnce sync.Once
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(g.release) })
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, svc.Stop(ctx))
			})
			require.Zero(t, g.calls.Load(), "provider must construct without starting")
			require.NoError(t, svc.Start(context.Background()))
			var work context.Context
			select {
			case work = <-g.entered:
			case <-time.After(time.Second):
				t.Fatal("explicit Start did not run the cycle")
			}
			drain, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			require.ErrorIs(t, svc.Drain(drain), context.DeadlineExceeded)
			require.NoError(t, work.Err(), "Drain must preserve the accepted cycle")
			force, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer stop()
			require.ErrorIs(t, svc.Stop(force), context.DeadlineExceeded)
			require.ErrorIs(t, work.Err(), context.Canceled)
			require.Error(t, svc.Start(context.Background()))
			releaseOnce.Do(func() { close(g.release) })
			join, done := context.WithTimeout(context.Background(), time.Second)
			defer done()
			require.NoError(t, svc.Stop(join))
			require.EqualValues(t, 1, g.calls.Load())
		})
	}
}
func TestUpstreamProbeLifecycleJoinsDeferredCadenceRelease(t *testing.T) {
	svc := NewUpstreamBillingProbeService(nil, nil, nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc.releaseCadenceLock(func() { calls.Add(1); close(entered); <-release }, time.Now().Add(time.Hour))
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Stop(short), context.DeadlineExceeded)
	select {
	case <-entered:
	default:
		t.Fatal("shutdown did not wake delayed lock release")
	}
	close(release)
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.Stop(join))
	require.EqualValues(t, 1, calls.Load())
}

type explicitBatchQueue struct {
	BatchImageQueue
	entered chan context.Context
	release chan struct{}
}

func (q *explicitBatchQueue) Reserve(ctx context.Context, _ time.Duration) (ReservedBatchImageJob, error) {
	q.entered <- ctx
	<-q.release
	return ReservedBatchImageJob{}, ctx.Err()
}
func (q *explicitBatchQueue) MoveDueDelayedToReady(context.Context, int) (int, error) { return 0, nil }
func (q *explicitBatchQueue) RecoverStaleActive(context.Context, time.Duration, int) (int, error) {
	return 0, nil
}

type explicitBatchProcessor struct{}

func (*explicitBatchProcessor) Process(ctx context.Context, _ string) (BatchImageProcessResult, error) {
	return BatchImageProcessResult{}, ctx.Err()
}

func TestBatchImageLifecycleRetainsJoinAfterDeadline(t *testing.T) {
	cfg := &config.Config{BatchImage: config.BatchImageConfig{QueueEnabled: true}}
	constructed := ProvideBatchImageWorkerRuntime(nil, nil, nil, nil, nil, nil, nil, cfg)
	require.False(t, constructed.Running(), "provider must not start the durable consumer")
	q := &explicitBatchQueue{entered: make(chan context.Context, 1), release: make(chan struct{})}
	svc := NewBatchImageWorkerRuntime(NewBatchImageWorker(q, &explicitBatchProcessor{}, BatchImageWorkerOptions{}), cfg)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(q.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Stop(ctx))
	})
	require.NoError(t, svc.Start(context.Background()))
	var work context.Context
	select {
	case work = <-q.entered:
	case <-time.After(time.Second):
		t.Fatal("reservation loop did not start")
	}
	firstDone := svc.done
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Drain(short), context.DeadlineExceeded)
	require.ErrorIs(t, work.Err(), context.Canceled)
	require.True(t, svc.Running())
	require.Equal(t, firstDone, svc.done, "deadline must retain the original join handle")
	releaseOnce.Do(func() { close(q.release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.Stop(join))
	require.False(t, svc.Running())
	require.Error(t, svc.Start(context.Background()))
}

type explicitBPSRepo struct {
	AccountRepository
	AccountExcelBPSRecoveryRepository
	entered chan context.Context
	release chan struct{}
}

func (r *explicitBPSRepo) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	r.entered <- ctx
	<-r.release
	return nil, ctx.Err()
}
func TestBPSRecoveryLifecycleConstructorDoesNotStartAndStopJoins(t *testing.T) {
	repo := &explicitBPSRepo{entered: make(chan context.Context, 1), release: make(chan struct{})}
	svc := NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, NewSettingService(explicitRunnerSettings{}, nil), nil)
	require.Nil(t, svc.excelBPSRecoveryCancel)
	require.Nil(t, svc.excelBPSRecoveryDone)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(repo.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.StopBPS403Recovery(ctx))
	})
	require.NoError(t, svc.StartBPS403Recovery(context.Background()))
	var work context.Context
	select {
	case work = <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("BPS recovery did not start")
	}
	firstDone := svc.excelBPSRecoveryDone
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.StopBPS403Recovery(short), context.DeadlineExceeded)
	require.ErrorIs(t, work.Err(), context.Canceled)
	require.Equal(t, firstDone, svc.excelBPSRecoveryDone)
	releaseOnce.Do(func() { close(repo.release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.StopBPS403Recovery(join))
	require.Error(t, svc.StartBPS403Recovery(context.Background()))
}
