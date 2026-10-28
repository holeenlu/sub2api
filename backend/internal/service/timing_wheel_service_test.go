package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func timingTestService(t *testing.T) *TimingWheelService {
	t.Helper()
	s, err := NewTimingWheelService()
	require.NoError(t, err)
	require.NoError(t, s.Start(t.Context()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, s.Stop(ctx))
	})
	return s
}
func timingReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("timing callback did not complete")
		var z T
		return z
	}
}
func TestTimingWheelConstructionStartsNothing(t *testing.T) {
	s, err := NewTimingWheelService()
	require.NoError(t, err)
	require.Nil(t, s.scheduler)
	require.ErrorIs(t, s.Schedule("before-start", time.Millisecond, func() { t.Error("constructor launched a callback") }), errTimingWheelNotStarted)
	require.NoError(t, s.Stop(t.Context()))
	require.ErrorIs(t, s.Start(t.Context()), errBackgroundWorkStopped)
}
func TestTimingWheelStartOwnsInitializationFailure(t *testing.T) {
	original := newTimingScheduler
	t.Cleanup(func() { newTimingScheduler = original })
	failure := errors.New("fixture scheduler construction failed")
	newTimingScheduler = func() (*cron.Cron, error) { return nil, failure }
	s, err := NewTimingWheelService()
	require.NoError(t, err)
	require.Nil(t, s.scheduler)
	require.ErrorIs(t, s.Start(t.Context()), failure)
	require.NoError(t, s.Stop(t.Context()))
}
func TestTimingWheelOnceCancelAndReplacement(t *testing.T) {
	s := timingTestService(t)
	calls := make(chan int, 2)
	require.NoError(t, s.Schedule("same", time.Hour, func() { calls <- 1 }))
	require.NoError(t, s.Schedule("same", 5*time.Millisecond, func() { calls <- 2 }))
	require.Equal(t, 2, timingReceive(t, calls))
	require.NoError(t, s.CancelAndWait(t.Context(), "same"))
	require.NoError(t, s.Schedule("cancel", time.Hour, func() { t.Error("canceled task ran") }))
	require.NoError(t, s.CancelAndWait(t.Context(), "cancel"))
	require.Eventually(t, func() bool { return len(s.scheduler.Entries()) == 0 }, time.Second, time.Millisecond)
	require.Empty(t, calls)
}
func TestTimingWheelRecurringRunsAfterPreviousCompletion(t *testing.T) {
	s := timingTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	var count, active, peak atomic.Int32
	second := make(chan struct{})
	require.NoError(t, s.ScheduleRecurring("recurring", 5*time.Millisecond, func() {
		n := active.Add(1)
		defer active.Add(-1)
		if n > peak.Load() {
			peak.Store(n)
		}
		switch count.Add(1) {
		case 1:
			close(entered)
			<-release
		case 2:
			close(second)
		}
	}))
	timingReceive(t, entered)
	require.EqualValues(t, 1, count.Load())
	releaseOnce()
	timingReceive(t, second)
	require.NoError(t, s.CancelAndWait(t.Context(), "recurring"))
	require.EqualValues(t, 1, peak.Load())
}
func TestTimingWheelCancelJoinsReplacedRunningOccurrence(t *testing.T) {
	s := timingTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	require.NoError(t, s.ScheduleRecurring("same", time.Millisecond, func() { close(entered); <-release }))
	timingReceive(t, entered)
	require.NoError(t, s.Schedule("same", time.Hour, func() { t.Error("replacement ran after cancel") }))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.CancelAndWait(ctx, "same"), context.DeadlineExceeded)
	releaseOnce()
	require.NoError(t, s.CancelAndWait(t.Context(), "same"))
}
func TestTimingWheelStopJoinsDispatchedBeforeCallbackEntry(t *testing.T) {
	original := newTimingScheduler
	t.Cleanup(func() { newTimingScheduler = original })
	dispatched, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	newTimingScheduler = func() (*cron.Cron, error) {
		return cron.New(cron.WithLogger(cron.DiscardLogger), cron.WithChain(func(j cron.Job) cron.Job { return cron.FuncJob(func() { close(dispatched); <-release; j.Run() }) })), nil
	}
	s := timingTestService(t)
	var ran atomic.Int32
	require.NoError(t, s.Schedule("dispatch", time.Millisecond, func() { ran.Add(1) }))
	timingReceive(t, dispatched)
	cancelCtx, cancelWait := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancelWait()
	require.ErrorIs(t, s.CancelAndWait(cancelCtx, "dispatch"), context.DeadlineExceeded, "a dispatched wrapper belongs to the named task before its body enters")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Stop(ctx), context.DeadlineExceeded)
	require.Zero(t, ran.Load())
	require.ErrorIs(t, s.Schedule("late", time.Millisecond, func() { ran.Add(1) }), errBackgroundWorkStopped)
	releaseOnce()
	require.NoError(t, s.Stop(t.Context()))
	require.Zero(t, ran.Load())
}
func TestTimingWheelStopJoinsCallbackAndPreventsRecurringRevival(t *testing.T) {
	s := timingTestService(t)
	entered, release := make(chan struct{}), make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
	var ran atomic.Int32
	require.NoError(t, s.ScheduleRecurring("recurring", time.Millisecond, func() { ran.Add(1); close(entered); <-release }))
	timingReceive(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Drain(ctx), context.DeadlineExceeded)
	releaseOnce()
	require.NoError(t, s.Stop(t.Context()))
	require.EqualValues(t, 1, ran.Load())
	require.Empty(t, s.scheduler.Entries())
}
func TestTimingWheelParentCancellationAndRepeatedStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	s, err := NewTimingWheelService()
	require.NoError(t, err)
	require.NoError(t, s.Start(ctx))
	cancel()
	err = s.Schedule("canceled-parent", time.Hour, func() { t.Error("canceled parent admitted a callback") })
	require.True(t, errors.Is(err, context.Canceled) || errors.Is(err, errBackgroundWorkStopped), "cancellation must close admission before the AfterFunc runs: %v", err)
	require.NoError(t, s.Stop(t.Context()))
	require.NoError(t, s.Stop(t.Context()))
	require.ErrorIs(t, s.ScheduleRecurring("late", time.Millisecond, func() { t.Error("late callback") }), errBackgroundWorkStopped)
	s.Cancel("missing")
	require.NoError(t, s.CancelAndWait(t.Context(), "missing"))
}
