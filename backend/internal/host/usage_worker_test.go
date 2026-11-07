package host

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUsageWorker_SubmitEnqueued(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           1,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
	})
	startTestUsageWorker(t, pool)

	done := make(chan struct{})
	mode := pool.Submit(func(ctx context.Context) {
		close(done)
	})
	require.Equal(t, SubmitModeEnqueued, mode)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task not executed")
	}

	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.SubmittedTasks == 1 && stats.SuccessfulTasks == 1
	}, time.Second, 10*time.Millisecond)
}

func TestUsageWorker_OverflowDrop(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
	})
	startTestUsageWorker(t, pool)

	block := make(chan struct{})
	started := make(chan struct{})
	secondDone := make(chan struct{})

	require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started

	require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(secondDone)
	}))
	require.Equal(t, SubmitModeDropped, pool.Submit(func(ctx context.Context) {}))

	close(block)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("queued task not executed")
	}

	require.Eventually(t, func() bool {
		return pool.Stats().DroppedQueueFull >= 1
	}, time.Second, 10*time.Millisecond)
}

func TestUsageWorker_OverflowSync(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		ExecutionLimit:        2,
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowSync,
		OverflowSamplePercent: 0,
	})
	startTestUsageWorker(t, pool)

	block := make(chan struct{})
	started := make(chan struct{})
	secondDone := make(chan struct{})
	var syncExecuted atomic.Bool

	require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started

	require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(secondDone)
	}))

	mode := pool.Submit(func(ctx context.Context) {
		syncExecuted.Store(true)
	})
	require.Equal(t, SubmitModeSync, mode)
	require.True(t, syncExecuted.Load())

	close(block)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("queued task not executed")
	}

	require.Eventually(t, func() bool {
		return pool.Stats().SyncFallbackTasks >= 1
	}, time.Second, 10*time.Millisecond)
}

func TestUsageWorker_OverflowSample(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		ExecutionLimit:        2,
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowSample,
		OverflowSamplePercent: 1,
	})
	startTestUsageWorker(t, pool)

	block := make(chan struct{})
	started := make(chan struct{})
	secondDone := make(chan struct{})
	var syncExecuted atomic.Bool

	require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started

	require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(secondDone)
	}))

	firstOverflow := pool.Submit(func(ctx context.Context) {
		syncExecuted.Store(true)
	})
	require.Equal(t, SubmitModeSync, firstOverflow)
	require.True(t, syncExecuted.Load())

	secondOverflow := pool.Submit(func(ctx context.Context) {})
	require.Equal(t, SubmitModeDropped, secondOverflow)

	close(block)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("queued task not executed")
	}

	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.SyncFallbackTasks >= 1 && stats.DroppedQueueFull >= 1
	}, time.Second, 10*time.Millisecond)
}

func TestUsageWorker_SubmitAfterStop(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
	})

	pool.Stop(context.Background())
	mode := pool.Submit(func(ctx context.Context) {})
	require.Equal(t, SubmitModeDroppedStopped, mode)
	require.True(t, mode.Dropped())
	require.True(t, SubmitModeDropped.Dropped())
	require.False(t, SubmitModeEnqueued.Dropped())
	require.False(t, SubmitModeSync.Dropped())
	require.GreaterOrEqual(t, pool.Stats().DroppedPoolStopped, uint64(1))
}

func TestUsageWorker_AutoScaleUpAndDown(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           2,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   1,
		AutoScaleMaxWorkers:   4,
		AutoScaleUpPercent:    40,
		AutoScaleDownPercent:  10,
		AutoScaleUpStep:       1,
		AutoScaleDownStep:     1,
		AutoScaleInterval:     20 * time.Millisecond,
		AutoScaleCooldown:     20 * time.Millisecond,
	})
	startTestUsageWorker(t, pool)

	block := make(chan struct{})

	// 填满运行槽位 + 队列，触发扩容阈值。
	for i := 0; i < 8; i++ {
		require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
			<-block
		}))
	}

	require.Eventually(t, func() bool {
		return pool.Stats().MaxConcurrency >= 3
	}, 2*time.Second, 20*time.Millisecond)

	close(block)

	require.Eventually(t, func() bool {
		return pool.Stats().CompletedTasks >= 8
	}, 2*time.Second, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		return pool.Stats().MaxConcurrency == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestUsageWorker_AutoScaleDownRequiresLowRunningUtilization(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           2,
		QueueSize:             8,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   1,
		AutoScaleMaxWorkers:   2,
		AutoScaleUpPercent:    80,
		AutoScaleDownPercent:  50,
		AutoScaleUpStep:       1,
		AutoScaleDownStep:     1,
		AutoScaleInterval:     20 * time.Millisecond,
		AutoScaleCooldown:     20 * time.Millisecond,
	})
	startTestUsageWorker(t, pool)

	block := make(chan struct{})
	for i := 0; i < 2; i++ {
		require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
			<-block
		}))
	}

	// 虽然 waiting=0，但 running 利用率为 100%，不应缩容。
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 2, pool.Stats().MaxConcurrency)

	close(block)
	require.Eventually(t, func() bool {
		return pool.Stats().MaxConcurrency == 1
	}, 2*time.Second, 20*time.Millisecond)
}

func TestUsageWorker_SubmitNilReceiverAndNilTask(t *testing.T) {
	var nilPool *UsageWorker
	require.Equal(t, SubmitModeDropped, nilPool.Submit(func(ctx context.Context) {}))

	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           1,
		QueueSize:             1,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
	})
	startTestUsageWorker(t, pool)

	require.Equal(t, SubmitModeDropped, pool.Submit(nil))
}

func TestUsageWorker_AutoScaleDisabledKeepsFixedConcurrency(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:           2,
		QueueSize:             4,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowDrop,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      false,
		AutoScaleMinWorkers:   1,
		AutoScaleMaxWorkers:   4,
		AutoScaleUpPercent:    10,
		AutoScaleDownPercent:  1,
		AutoScaleUpStep:       2,
		AutoScaleDownStep:     2,
		AutoScaleInterval:     10 * time.Millisecond,
		AutoScaleCooldown:     10 * time.Millisecond,
	})
	startTestUsageWorker(t, pool)

	require.Equal(t, 2, pool.Stats().MaxConcurrency)

	block := make(chan struct{})
	for i := 0; i < 4; i++ {
		require.Equal(t, SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
			<-block
		}))
	}

	time.Sleep(120 * time.Millisecond)
	require.Equal(t, 2, pool.Stats().MaxConcurrency)
	close(block)
}

func TestUsageWorker_StringHelpers(t *testing.T) {
	require.Equal(t, "enqueued", SubmitModeEnqueued.String())
	stats := UsageWorkerStats{RunningWorkers: 2, WaitingTasks: 3, SubmittedTasks: 5, DroppedTasks: 1}
	require.Contains(t, stats.String(), "running=2")
	require.Contains(t, stats.String(), "waiting=3")
}

func TestUsageWorker_NormalizeOptions_BoundsAndDefaults(t *testing.T) {
	opts := NormalizeUsageWorkerOptions(UsageWorkerOptions{
		WorkerCount:           0,
		QueueSize:             0,
		TaskTimeout:           0,
		OverflowPolicy:        "invalid",
		OverflowSamplePercent: 300,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   0,
		AutoScaleMaxWorkers:   0,
		AutoScaleUpPercent:    0,
		AutoScaleDownPercent:  100,
		AutoScaleUpStep:       0,
		AutoScaleDownStep:     0,
		AutoScaleInterval:     0,
		AutoScaleCooldown:     -time.Second,
	})

	require.Equal(t, defaultUsageRecordWorkerCount, opts.WorkerCount)
	require.Equal(t, defaultUsageRecordQueueSize, opts.QueueSize)
	require.Equal(t, time.Duration(defaultUsageRecordTaskTimeoutSeconds)*time.Second, opts.TaskTimeout)
	require.Equal(t, defaultUsageRecordOverflowPolicy, opts.OverflowPolicy)
	require.Equal(t, 100, opts.OverflowSamplePercent)
	require.Equal(t, defaultUsageRecordAutoScaleMinWorkers, opts.AutoScaleMinWorkers)
	require.Equal(t, defaultUsageRecordAutoScaleMaxWorkers, opts.AutoScaleMaxWorkers)
	require.Equal(t, defaultUsageRecordAutoScaleUpPercent, opts.AutoScaleUpPercent)
	require.Equal(t, defaultUsageRecordAutoScaleDownPercent, opts.AutoScaleDownPercent)
	require.Equal(t, defaultUsageRecordAutoScaleUpStep, opts.AutoScaleUpStep)
	require.Equal(t, defaultUsageRecordAutoScaleDownStep, opts.AutoScaleDownStep)
	require.Equal(t, defaultUsageRecordAutoScaleInterval, opts.AutoScaleInterval)
	require.Equal(t, defaultUsageRecordAutoScaleCooldown, opts.AutoScaleCooldown)
}

func TestUsageWorker_NormalizeOptions_SampleAndAutoScaleDisabled(t *testing.T) {
	sampleOpts := NormalizeUsageWorkerOptions(UsageWorkerOptions{
		WorkerCount:           32,
		QueueSize:             128,
		TaskTimeout:           time.Second,
		OverflowPolicy:        OverflowSample,
		OverflowSamplePercent: 0,
		AutoScaleEnabled:      true,
		AutoScaleMinWorkers:   64,
		AutoScaleMaxWorkers:   48,
		AutoScaleUpPercent:    30,
		AutoScaleDownPercent:  40,
		AutoScaleUpStep:       1,
		AutoScaleDownStep:     1,
		AutoScaleInterval:     time.Second,
		AutoScaleCooldown:     time.Second,
	})
	require.Equal(t, defaultUsageRecordOverflowSampleRatio, sampleOpts.OverflowSamplePercent)
	require.Equal(t, 64, sampleOpts.AutoScaleMinWorkers)
	require.Equal(t, 64, sampleOpts.AutoScaleMaxWorkers)
	require.Equal(t, 64, sampleOpts.WorkerCount)
	require.Equal(t, 15, sampleOpts.AutoScaleDownPercent)

	fixedOpts := NormalizeUsageWorkerOptions(UsageWorkerOptions{
		WorkerCount:      20,
		AutoScaleEnabled: false,
	})
	require.Equal(t, 20, fixedOpts.AutoScaleMinWorkers)
	require.Equal(t, 20, fixedOpts.AutoScaleMaxWorkers)
}

func TestUsageWorker_ShouldSyncFallbackEdgeCases(t *testing.T) {
	pool := &UsageWorker{overflowSamplePercent: 0}
	require.False(t, pool.shouldSyncFallback())

	pool.overflowSamplePercent = 100
	require.True(t, pool.shouldSyncFallback())
	require.True(t, pool.shouldSyncFallback())
}

func TestUsageWorker_StatsAndStop_NilBranches(t *testing.T) {
	var nilPool *UsageWorker
	require.Equal(t, UsageWorkerStats{}, nilPool.Stats())
	require.NotPanics(t, func() { nilPool.Stop(context.Background()) })

	emptyPool := &UsageWorker{}
	require.Equal(t, UsageWorkerStats{}, emptyPool.Stats())
	require.NotPanics(t, func() { emptyPool.Stop(context.Background()) })
}

func TestUsageWorker_Execute_PanicAndTimeout(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{TaskTimeout: 30 * time.Millisecond})
	startTestUsageWorker(t, pool)

	require.NotPanics(t, func() {
		pool.execute(context.Background(), func(ctx context.Context) {
			panic("boom")
		})
	})

	done := make(chan struct{})
	pool.execute(context.Background(), func(ctx context.Context) {
		<-ctx.Done()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout context not cancelled")
	}
}

func TestUsageWorker_ResizeAndLogDropBranches(t *testing.T) {
	pool := NewUsageWorker(UsageWorkerOptions{
		WorkerCount:      1,
		QueueSize:        8,
		TaskTimeout:      time.Second,
		OverflowPolicy:   OverflowDrop,
		AutoScaleEnabled: false,
	})
	startTestUsageWorker(t, pool)

	// 目标值与当前值相同，应该直接返回。
	pool.resizePool(1, 1, 0, 0, 0, 8, "noop")
	require.Equal(t, 1, pool.Stats().MaxConcurrency)

	// 在限流窗口内应静默返回。
	pool.lastDropLogNanos.Store(time.Now().UnixNano())
	require.NotPanics(t, func() {
		pool.logDrop("full")
	})
}

func startTestUsageWorker(t *testing.T, pool *UsageWorker) {
	t.Helper()
	require.NoError(t, pool.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := pool.Drain(ctx); err != nil {
			pool.cancelWork()
			t.Error(err)
		}
	})
}
