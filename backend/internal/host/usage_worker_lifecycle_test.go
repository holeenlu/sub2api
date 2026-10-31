package host

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageWorkerConstructDoesNotStartRecovery(t *testing.T) {
	var calls atomic.Int32
	p := NewUsageWorker(UsageWorkerOptions{Recover: func(context.Context) error { calls.Add(1); return nil }, RecoveryInterval: time.Millisecond})
	require.False(t, p.started)
	require.Zero(t, calls.Load())
	require.NoError(t, p.Start(context.Background()))
	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool { return calls.Load() > 0 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Drain(ctx))
	count := calls.Load()
	require.ErrorIs(t, p.Start(context.Background()), ErrUsageWorkerStopped)
	require.Equal(t, count, calls.Load())
}

func TestUsageWorkerDrainOwnsInlineAndQueuedWork(t *testing.T) {
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 1, ExecutionLimit: 2, QueueSize: 1, TaskTimeout: time.Second})
	startTestUsageWorker(t, p)
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var calls atomic.Int32
	job := func(context.Context) { started <- struct{}{}; <-release; calls.Add(1) }
	inlineDone := make(chan struct{})
	go func() { p.Run(job); close(inlineDone) }()
	require.Equal(t, SubmitModeEnqueued, p.Submit(job))
	<-started
	<-started
	require.Equal(t, SubmitModeEnqueued, p.Submit(func(context.Context) { calls.Add(1) }))
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Drain(expired), context.Canceled)
	require.Equal(t, SubmitModeDroppedStopped, p.Submit(func(context.Context) { t.Error("accepted after drain") }))
	close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Drain(ctx))
	<-inlineDone
	require.EqualValues(t, 3, calls.Load())
	require.Zero(t, p.Stats().ExecutionInFlight)
	require.Equal(t, p.Stats().InlineTasks, p.Stats().InlineCompleted)
}

func TestUsageWorkerStopCancelsAndJoinsEveryAcceptedClosure(t *testing.T) {
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 1, QueueSize: 2, TaskTimeout: time.Minute})
	require.NoError(t, p.Start(context.Background()))
	started := make(chan struct{})
	var canceled atomic.Int32
	require.Equal(t, SubmitModeEnqueued, p.Submit(func(ctx context.Context) { close(started); <-ctx.Done(); canceled.Add(1) }))
	<-started
	require.Equal(t, SubmitModeEnqueued, p.Submit(func(ctx context.Context) { require.ErrorIs(t, ctx.Err(), context.Canceled); canceled.Add(1) }))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Stop(ctx))
	require.EqualValues(t, 2, canceled.Load())
	require.Zero(t, p.Stats().ExecutionInFlight)
	require.NoError(t, p.Stop(ctx))
}

func TestUsageWorkerTimeoutRetainsJoinForNonCooperativeTask(t *testing.T) {
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 1})
	release := make(chan struct{})
	started := make(chan struct{})
	p.Submit(func(context.Context) { close(started); <-release })
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Stop(ctx), context.Canceled)
	require.EqualValues(t, 1, p.Stats().RunningWorkers)
	close(release)
	bounded, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Stop(bounded))
	require.Zero(t, p.Stats().RunningWorkers)
}

func TestUsageWorkerParentCancelStopsRecoveryAndAdmission(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 1, TaskTimeout: time.Minute, RecoveryInterval: time.Millisecond, Recover: func(ctx context.Context) error { close(started); <-ctx.Done(); close(finished); return ctx.Err() }})
	require.NoError(t, p.Start(parent))
	<-started
	cancel()
	<-finished
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, p.Stop(ctx))
	require.False(t, p.recoveryActive.Load())
	require.Equal(t, SubmitModeDroppedStopped, p.Submit(func(context.Context) {}))
}

func TestUsageWorkerFirstSettlementKeepsBudgetThroughWaitAndStop(t *testing.T) {
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 1, ExecutionLimit: 1, TaskTimeout: 20 * time.Millisecond})
	release, started, firstDone, secondDone := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { p.Run(func(context.Context) { close(started); <-release }); close(firstDone) }()
	<-started
	go func() {
		p.Run(func(ctx context.Context) {
			require.NoError(t, ctx.Err(), "first settlement must retain a live context for its journal")
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			require.Greater(t, time.Until(deadline), time.Duration(0))
		})
		close(secondDone)
	}()
	require.Eventually(t, func() bool { return p.Stats().InlineTasks == 2 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, p.Stop(ctx), context.Canceled)
	// Stop must neither drop the queued first attempt nor bypass the shared limit.
	select {
	case <-secondDone:
		t.Fatal("first settlement bypassed occupied capacity")
	default:
	}
	close(release)
	<-firstDone
	<-secondDone
	bounded, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Stop(bounded))
	require.EqualValues(t, 2, p.Stats().InlineCompleted)
}

func TestUsageWorkerLateSettlementRemainsSynchronous(t *testing.T) {
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 1})
	require.NoError(t, p.Stop(context.Background()))
	called := false
	p.Run(func(ctx context.Context) {
		require.NoError(t, ctx.Err())
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		called = true
	})
	require.True(t, called)
	require.EqualValues(t, 1, p.Stats().LateInlineTasks)
	require.Equal(t, p.Stats().InlineTasks, p.Stats().InlineCompleted)
}

func TestUsageWorkerConcurrentSubmitStopOwnsAcceptedWork(t *testing.T) {
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 4, ExecutionLimit: 4, QueueSize: 16, OverflowPolicy: OverflowSync})
	var accepted, executed atomic.Int32
	var senders sync.WaitGroup
	for i := 0; i < 40; i++ {
		senders.Add(1)
		go func() {
			defer senders.Done()
			if !p.Submit(func(context.Context) { executed.Add(1) }).Dropped() {
				accepted.Add(1)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Stop(ctx))
	senders.Wait()
	require.Equal(t, accepted.Load(), executed.Load())
}

func TestUsageWorkerRecoverySerializesAndContinuesAfterFailure(t *testing.T) {
	var calls, current, peak atomic.Int32
	p := NewUsageWorker(UsageWorkerOptions{WorkerCount: 4, RecoveryInterval: time.Millisecond, Recover: func(ctx context.Context) error {
		calls.Add(1)
		n := current.Add(1)
		if n > peak.Load() {
			peak.Store(n)
		}
		defer current.Add(-1)
		select {
		case <-ctx.Done():
		case <-time.After(4 * time.Millisecond):
		}
		return errors.New("retryable durable recovery")
	}})
	require.NoError(t, p.Start(context.Background()))
	require.Eventually(t, func() bool { return calls.Load() >= 3 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, p.Drain(ctx))
	require.EqualValues(t, 1, peak.Load())
}
