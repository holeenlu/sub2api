package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/stretchr/testify/require"
)

func backgroundTestContext(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

type backgroundTestBlock struct {
	entered chan context.Context
	release chan struct{}
	calls   atomic.Int32
	once    sync.Once
}

func newBackgroundTestBlock(t *testing.T) *backgroundTestBlock {
	b := &backgroundTestBlock{entered: make(chan context.Context, 4), release: make(chan struct{})}
	t.Cleanup(b.unblock)
	return b
}
func (b *backgroundTestBlock) unblock() { b.once.Do(func() { close(b.release) }) }
func (b *backgroundTestBlock) wait(ctx context.Context) error {
	b.calls.Add(1)
	b.entered <- ctx
	<-b.release
	return ctx.Err()
}

func TestBackgroundWorkRegistrationAndRetryableJoin(t *testing.T) {
	var owner backgroundWork
	require.NoError(t, owner.bind(t.Context(), nil))
	b := newBackgroundTestBlock(t)
	require.NoError(t, owner.launch(func(ctx context.Context) { _ = b.wait(ctx) }))
	callback := timingReceive(t, b.entered)
	owner.requestStop()
	require.Error(t, callback.Err())
	require.ErrorIs(t, owner.join(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.ErrorIs(t, owner.launch(func(context.Context) { t.Error("late task admitted") }), errBackgroundWorkStopped)
	b.unblock()
	require.NoError(t, owner.join(backgroundTestContext(t, time.Second)))
}

func TestBackgroundWorkTimerStartupRemainsOwnedUntilPublication(t *testing.T) {
	for _, kind := range []string{"usage", "deferred", "dashboard"} {
		t.Run(kind, func(t *testing.T) {
			wheel := timingTestService(t)
			var owner *backgroundWork
			var start, stop func(context.Context) error
			switch kind {
			case "usage":
				s := NewUsageCleanupService(&backgroundUsageRepo{}, wheel, nil, nil)
				owner, start, stop = &s.work, s.Start, s.Stop
			case "deferred":
				s := NewDeferredService(&backgroundLastUsedRepo{}, wheel, time.Hour)
				owner, start, stop = &s.work, s.Start, s.Stop
			case "dashboard":
				s := NewDashboardAggregationService(&backgroundDashboardRepo{}, wheel, nil)
				owner, start, stop = &s.work, s.Start, s.Stop
			}
			// Pause at the real registration mutex, after startup admission.
			wheel.mu.Lock()
			unlock := sync.OnceFunc(wheel.mu.Unlock)
			t.Cleanup(unlock)
			started := make(chan error, 1)
			go func() { started <- start(t.Context()) }()
			require.Eventually(t, func() bool {
				owner.mu.Lock()
				defer owner.mu.Unlock()
				return owner.active == 1
			}, time.Second, time.Millisecond)
			owner.requestStop()
			require.ErrorIs(t, owner.join(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
			unlock()
			require.ErrorIs(t, timingReceive(t, started), errBackgroundWorkStopped)
			require.NoError(t, stop(backgroundTestContext(t, time.Second)))
			require.Empty(t, wheel.scheduler.Entries(), "late startup must not publish a recurring callback after shutdown")
		})
	}
}

type backgroundIdempotencyRepo struct {
	IdempotencyRepository
	b *backgroundTestBlock
}

func (r *backgroundIdempotencyRepo) DeleteExpired(ctx context.Context, _ time.Time, _ int) (int64, error) {
	return 0, r.b.wait(ctx)
}

func TestIdempotencyCleanupLifecycleJoinsActualDelete(t *testing.T) {
	b := newBackgroundTestBlock(t)
	s := NewIdempotencyCleanupService(&backgroundIdempotencyRepo{b: b}, nil)
	s.interval = time.Millisecond
	require.NoError(t, s.Start(t.Context()))
	callback := timingReceive(t, b.entered)
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.Error(t, callback.Err())
	b.unblock()
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	require.EqualValues(t, 1, b.calls.Load())
	require.ErrorIs(t, s.Start(t.Context()), errBackgroundWorkStopped)
}

type backgroundUsageRepo struct {
	UsageCleanupRepository
	b *backgroundTestBlock
}

func (r *backgroundUsageRepo) ClaimNextPendingTask(ctx context.Context, _ int64) (*UsageCleanupTask, error) {
	return nil, r.b.wait(ctx)
}

func TestUsageCleanupLifecycleStopsClaimAndPreventsLateRun(t *testing.T) {
	wheel := timingTestService(t)
	b := newBackgroundTestBlock(t)
	s := NewUsageCleanupService(&backgroundUsageRepo{b: b}, wheel, nil, &config.Config{UsageCleanup: config.UsageCleanupConfig{Enabled: true, WorkerIntervalSeconds: 60}})
	require.NoError(t, s.Start(t.Context()))
	ran := make(chan struct{})
	go func() { s.runOnce(); close(ran) }()
	callback := timingReceive(t, b.entered)
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.Error(t, callback.Err())
	b.unblock()
	timingReceive(t, ran)
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	s.runOnce()
	require.EqualValues(t, 1, b.calls.Load())
}

type backgroundUsageCreateRepo struct {
	UsageCleanupRepository
	b      *backgroundTestBlock
	claims atomic.Int32
}

func (r *backgroundUsageCreateRepo) CreateTask(ctx context.Context, task *UsageCleanupTask) error {
	_ = r.b.wait(ctx) // Model a durable commit completing as shutdown cancels.
	task.ID = 41
	return nil
}
func (r *backgroundUsageCreateRepo) ClaimNextPendingTask(context.Context, int64) (*UsageCleanupTask, error) {
	r.claims.Add(1)
	return nil, nil
}

func TestUsageCleanupLifecycleOwnsPersistThenDispatchHandoff(t *testing.T) {
	wheel := timingTestService(t)
	b := newBackgroundTestBlock(t)
	repo := &backgroundUsageCreateRepo{b: b}
	s := NewUsageCleanupService(repo, wheel, nil, nil)
	require.NoError(t, s.Start(t.Context()))
	type created struct {
		task *UsageCleanupTask
		err  error
	}
	result := make(chan created, 1)
	go func() {
		task, err := s.CreateTask(t.Context(), UsageCleanupFilters{StartTime: time.Now().Add(-time.Hour), EndTime: time.Now()}, 1)
		result <- created{task, err}
	}()
	callback := timingReceive(t, b.entered)
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.Error(t, callback.Err())
	b.unblock()
	committed := timingReceive(t, result)
	require.NoError(t, committed.err, "a persisted pending task must not become a false create failure")
	require.Equal(t, UsageCleanupStatusPending, committed.task.Status)
	require.EqualValues(t, 41, committed.task.ID)
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	require.Zero(t, repo.claims.Load(), "shutdown must reject the asynchronous claim handoff")
}

type backgroundLastUsedRepo struct {
	AccountRepository
	apply func(context.Context, map[int64]time.Time) error
}

func (r *backgroundLastUsedRepo) BatchUpdateLastUsed(ctx context.Context, m map[int64]time.Time) error {
	return r.apply(ctx, m)
}

func TestDeferredLifecycleSerializesFlushAndRestoresNewest(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var calls, active, peak atomic.Int32
	failure := errors.New("fixture old batch failed")
	var sentMu sync.Mutex
	var sent []map[int64]time.Time
	repo := &backgroundLastUsedRepo{apply: func(ctx context.Context, m map[int64]time.Time) error {
		n := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		sentMu.Lock()
		sent = append(sent, m)
		sentMu.Unlock()
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			return failure
		}
		return nil
	}}
	s := NewDeferredService(repo, nil, time.Hour)
	older, newer := time.Unix(10, 0), time.Unix(20, 0)
	s.lastUsedUpdates.Store(int64(1), older)
	done := make(chan struct{})
	go func() { s.flushLastUsed(); close(done) }()
	timingReceive(t, entered)
	s.mu.Lock()
	s.lastUsedUpdates.Store(int64(1), newer)
	s.mu.Unlock()
	second := make(chan struct{})
	go func() { s.flushLastUsed(); close(second) }()
	require.Eventually(t, func() bool { s.work.mu.Lock(); defer s.work.mu.Unlock(); return s.work.active == 2 }, time.Second, time.Millisecond)
	require.EqualValues(t, 1, calls.Load(), "second flush must wait for the first DB write")
	unblock()
	timingReceive(t, done)
	timingReceive(t, second)
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	require.EqualValues(t, 1, peak.Load())
	require.EqualValues(t, 2, calls.Load())
	sentMu.Lock()
	require.Equal(t, newer, sent[1][1])
	sentMu.Unlock()
	require.ErrorIs(t, s.ScheduleLastUsedUpdate(1), errBackgroundWorkStopped)
}

func TestDeferredLifecycleFinalFlushRemainsOwnedAfterTimeout(t *testing.T) {
	b := newBackgroundTestBlock(t)
	repo := &backgroundLastUsedRepo{apply: func(ctx context.Context, _ map[int64]time.Time) error { _ = b.wait(ctx); return nil }}
	s := NewDeferredService(repo, nil, time.Hour)
	require.NoError(t, s.ScheduleLastUsedUpdate(1))
	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop(backgroundTestContext(t, 30*time.Millisecond)) }()
	callback := timingReceive(t, b.entered)
	require.ErrorIs(t, timingReceive(t, stopped), context.DeadlineExceeded)
	require.Error(t, callback.Err())
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.EqualValues(t, 1, b.calls.Load())
	b.unblock()
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	require.EqualValues(t, 1, b.calls.Load())
}

func TestDeferredLifecycleFailedFinalFlushKeepsPendingData(t *testing.T) {
	failure := errors.New("fixture storage rejected batch")
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	repo := &backgroundLastUsedRepo{apply: func(context.Context, map[int64]time.Time) error {
		calls.Add(1)
		if fail.Load() {
			return failure
		}
		return nil
	}}
	s := NewDeferredService(repo, nil, time.Hour)
	require.NoError(t, s.ScheduleLastUsedUpdate(8))
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, time.Second)), failure)
	_, pending := s.lastUsedUpdates.Load(int64(8))
	require.True(t, pending)
	fail.Store(false)
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	_, pending = s.lastUsedUpdates.Load(int64(8))
	require.False(t, pending)
	require.EqualValues(t, 2, calls.Load())
}

func TestDeferredLifecycleConcurrentStopsShareFinalFlush(t *testing.T) {
	b := newBackgroundTestBlock(t)
	repo := &backgroundLastUsedRepo{apply: func(ctx context.Context, _ map[int64]time.Time) error { return b.wait(ctx) }}
	s := NewDeferredService(repo, nil, time.Hour)
	require.NoError(t, s.ScheduleLastUsedUpdate(1))
	ctx := backgroundTestContext(t, time.Second)
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		go func() { results <- s.Stop(ctx) }()
	}
	timingReceive(t, b.entered)
	b.unblock()
	for i := 0; i < cap(results); i++ {
		require.NoError(t, timingReceive(t, results))
	}
	require.EqualValues(t, 1, b.calls.Load())
}

func TestDeferredLifecycleRootRetainsDependenciesUntilFinalFlush(t *testing.T) {
	life := host.NewLifetime()
	wheel, err := ProvideLifetimeTimingWheelService(life)
	require.NoError(t, err)
	b := newBackgroundTestBlock(t)
	var flushed, storageClosed atomic.Bool
	repo := &backgroundLastUsedRepo{apply: func(ctx context.Context, batch map[int64]time.Time) error {
		if _, ok := batch[41]; !ok {
			return errors.New("final usage update missing from flush")
		}
		_ = b.wait(ctx) // Commit can finish after a caller's shutdown deadline.
		flushed.Store(true)
		return nil
	}}
	s := ProvideLifetimeDeferredService(repo, wheel, life)
	life.Add(host.Component{Name: "fixture-final-usage", Phase: host.Usage, Constructed: true, Stop: func(context.Context) error {
		return s.ScheduleLastUsedUpdate(41)
	}})
	life.Add(host.Component{Name: "fixture-storage", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error {
		if !flushed.Load() {
			return errors.New("storage closed before deferred commit")
		}
		storageClosed.Store(true)
		return nil
	}})
	t.Cleanup(func() {
		b.unblock()
		require.NoError(t, life.Cleanup(backgroundTestContext(t, time.Second)))
	})
	require.NoError(t, life.Start(context.Background()))
	require.ErrorIs(t, life.Cleanup(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	timingReceive(t, b.entered)
	require.False(t, storageClosed.Load())
	require.Contains(t, life.Pending(), "DeferredService")
	require.Contains(t, life.Pending(), "TimingWheelService")
	require.NoError(t, wheel.Schedule("fixture-still-owned", time.Hour, func() { t.Error("pending timer must be canceled") }))
	b.unblock()
	require.NoError(t, life.Cleanup(backgroundTestContext(t, time.Second)))
	require.True(t, storageClosed.Load())
	require.EqualValues(t, 1, b.calls.Load())
	require.Empty(t, life.Pending())
}

type backgroundPlansRepo struct {
	ScheduledTestPlanRepository
	b *backgroundTestBlock
}

func (r *backgroundPlansRepo) ListDue(ctx context.Context, _ time.Time) ([]*ScheduledTestPlan, error) {
	return nil, r.b.wait(ctx)
}

func TestScheduledRunnerLifecycleOffsetIsCancelable(t *testing.T) {
	b := newBackgroundTestBlock(t)
	s := NewScheduledTestRunnerService(&backgroundPlansRepo{b: b}, nil, nil, nil, nil)
	require.NoError(t, s.Start(t.Context()))
	runDone := make(chan struct{})
	go func() { s.runScheduled(); close(runDone) }()
	require.Eventually(t, func() bool { s.work.mu.Lock(); defer s.work.mu.Unlock(); return s.work.active > 0 }, time.Second, time.Millisecond)
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	timingReceive(t, runDone)
	require.Zero(t, b.calls.Load(), "cancellation during :10 offset must not enter SQL")
}

func TestScheduledRunnerLifecycleFiveMinuteCycleIsOwned(t *testing.T) {
	b := newBackgroundTestBlock(t)
	s := NewScheduledTestRunnerService(&backgroundPlansRepo{b: b}, nil, nil, nil, nil)
	require.NoError(t, s.Start(t.Context()))
	require.NoError(t, s.work.launch(s.runDuePlans))
	callback := timingReceive(t, b.entered)
	deadline, ok := callback.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), 5*time.Minute)
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.Error(t, callback.Err())
	b.unblock()
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	require.ErrorIs(t, s.Start(t.Context()), errBackgroundWorkStopped)
}

type backgroundDashboardRepo struct {
	DashboardAggregationRepository
	b *backgroundTestBlock
}

func (r *backgroundDashboardRepo) EnsureUsageLogsPartitions(context.Context, time.Time) error {
	return nil
}
func (r *backgroundDashboardRepo) AggregateRange(ctx context.Context, _, _ time.Time) error {
	return r.b.wait(ctx)
}
func (r *backgroundDashboardRepo) RecomputeRange(ctx context.Context, _, _ time.Time) error {
	return r.b.wait(ctx)
}

func TestDashboardLifecycleOwnsStartupAndManualJobs(t *testing.T) {
	for _, kind := range []string{"startup", "backfill", "recompute"} {
		t.Run(kind, func(t *testing.T) {
			wheel := timingTestService(t)
			b := newBackgroundTestBlock(t)
			cfg := &config.Config{}
			cfg.DashboardAgg.Enabled = true
			cfg.DashboardAgg.BackfillEnabled = true
			cfg.DashboardAgg.IntervalSeconds = 3600
			if kind == "startup" {
				cfg.DashboardAgg.RecomputeDays = 1
			}
			s := NewDashboardAggregationService(&backgroundDashboardRepo{b: b}, wheel, cfg)
			require.NoError(t, s.Start(t.Context()))
			start, end := time.Now().Add(-time.Hour), time.Now()
			switch kind {
			case "backfill":
				require.NoError(t, s.TriggerBackfill(start, end))
			case "recompute":
				require.NoError(t, s.TriggerRecomputeRange(start, end))
			}
			callback := timingReceive(t, b.entered)
			require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
			require.Error(t, callback.Err())
			b.unblock()
			require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
			require.ErrorIs(t, s.TriggerBackfill(start, end), errBackgroundWorkStopped)
			require.ErrorIs(t, s.TriggerRecomputeRange(start, end), errBackgroundWorkStopped)
		})
	}
}

type backgroundMonitorRepo struct {
	ChannelMonitorV2Repository
	b *backgroundTestBlock
}

func (r *backgroundMonitorRepo) GetConfig(ctx context.Context) (*ChannelMonitorV2Config, error) {
	return nil, r.b.wait(ctx)
}

type backgroundMonitorSettings struct {
	mu       sync.Mutex
	listener func()
	unsub    atomic.Int32
}

func (s *backgroundMonitorSettings) GetChannelMonitorRuntime(context.Context) ChannelMonitorRuntime {
	return ChannelMonitorRuntime{Enabled: true, Mode: ChannelMonitorModeV2}
}
func (s *backgroundMonitorSettings) SubscribeChannelMonitorRuntime(fn func()) func() {
	s.mu.Lock()
	s.listener = fn
	s.mu.Unlock()
	return func() { s.unsub.Add(1) }
}

func TestChannelAggregatorLifecycleOwnsConfigLookupAndUnsubscribe(t *testing.T) {
	b := newBackgroundTestBlock(t)
	settings := &backgroundMonitorSettings{}
	s := NewChannelMonitorV2Aggregator(&backgroundMonitorRepo{b: b}, nil, settings)
	require.NoError(t, s.Start(t.Context()))
	callback := timingReceive(t, b.entered)
	settings.mu.Lock()
	late := settings.listener
	settings.mu.Unlock()
	require.ErrorIs(t, s.Stop(backgroundTestContext(t, 20*time.Millisecond)), context.DeadlineExceeded)
	require.Error(t, callback.Err())
	require.EqualValues(t, 1, settings.unsub.Load())
	late()
	require.Empty(t, s.kickCh)
	b.unblock()
	require.NoError(t, s.Stop(backgroundTestContext(t, time.Second)))
	require.EqualValues(t, 1, settings.unsub.Load())
	require.EqualValues(t, 1, b.calls.Load())
}
