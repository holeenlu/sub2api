package handler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖：worker 池已停止（进程关停窗口）时，计费任务不得静默丢失，
// 必须降级为内联同步执行；资金事件即使配置 drop/sample 也不得丢失。

func newStoppedUsageRecordPoolForTest(t *testing.T) *host.UsageWorker {
	pool := host.NewUsageWorker(host.UsageWorkerOptions{
		WorkerCount:    1,
		QueueSize:      1,
		TaskTimeout:    time.Second,
		OverflowPolicy: config.UsageRecordOverflowPolicySync,
	})
	startHandlerUsageWorker(t, pool)
	stopHandlerUsageWorker(t, pool)
	return pool
}

func TestGatewayHandlerSubmitUsageRecordTask_StoppedPoolFallsBackToSync(t *testing.T) {
	h := &GatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest(t)}

	executed := false
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		executed = true
	})
	require.True(t, executed, "池已停止时计费任务必须内联同步执行")
}

func TestOpenAIGatewayHandlerSubmitUsageRecordTask_StoppedPoolFallsBackToSync(t *testing.T) {
	h := &OpenAIGatewayHandler{usageRecordWorkerPool: newStoppedUsageRecordPoolForTest(t)}

	executed := false
	h.submitUsageRecordTask(context.Background(), func(ctx context.Context) {
		executed = true
	})
	require.True(t, executed, "池已停止时计费任务必须内联同步执行")
}

func TestGatewayHandlerSubmitUsageRecordTask_DropPolicyOverflowWaitsAndBills(t *testing.T) {
	pool := host.NewUsageWorker(host.UsageWorkerOptions{
		WorkerCount:    1,
		QueueSize:      1,
		TaskTimeout:    time.Minute,
		OverflowPolicy: config.UsageRecordOverflowPolicyDrop,
	})
	startHandlerUsageWorker(t, pool)
	h := &GatewayHandler{usageRecordWorkerPool: pool}

	started := make(chan struct{})
	block := make(chan struct{})

	// 占满 worker 槽位后再填满队列，保证第三个任务触发溢出。
	require.Equal(t, host.SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		close(started)
		<-block
	}))
	<-started
	require.Equal(t, host.SubmitModeEnqueued, pool.Submit(func(ctx context.Context) {
		<-block
	}))

	var executed atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.submitUsageRecordTask(context.Background(), func(context.Context) { executed.Store(true) })
	}()
	select {
	case <-done:
		t.Fatal("billing bypassed shared execution limit")
	case <-time.After(20 * time.Millisecond):
	}
	close(block)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("billing did not resume")
	}
	require.True(t, executed.Load())
}
