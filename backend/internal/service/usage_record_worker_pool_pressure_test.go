package service

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUsageRecordSharedExecutionBudget(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{WorkerCount: 4, ExecutionLimit: 2, QueueSize: 32, OverflowPolicy: "sync"})
	defer pool.Stop()
	var current, peak atomic.Int32
	var wg sync.WaitGroup
	job := func(context.Context) {
		defer wg.Done()
		n := current.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		current.Add(-1)
	}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		if i%2 == 0 {
			go pool.Run(job)
		} else {
			pool.Submit(job)
		}
	}
	wg.Wait()
	if peak.Load() > 2 {
		t.Fatalf("shared budget exceeded: %d", peak.Load())
	}
}
func TestUsageRecordDBPressureSuppressesExpansion(t *testing.T) {
	pool := NewUsageRecordWorkerPoolWithOptions(UsageRecordWorkerPoolOptions{WorkerCount: 1, ExecutionLimit: 4, QueueSize: 4, AutoScaleEnabled: true, AutoScaleMinWorkers: 1, AutoScaleMaxWorkers: 4, AutoScaleUpPercent: 1, AutoScaleInterval: time.Hour, DatabaseStats: func() sql.DBStats { return sql.DBStats{MaxOpenConnections: 4, InUse: 4, WaitCount: 1} }})
	release := make(chan struct{})
	defer pool.Stop()
	defer close(release)
	pool.Submit(func(context.Context) { <-release })
	pool.Submit(func(context.Context) { <-release })
	pool.autoScaleTick()
	if pool.pool.MaxConcurrency() != 1 {
		t.Fatal("expanded while database saturated")
	}
}
