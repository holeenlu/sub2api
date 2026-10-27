package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type stoppingOpsAggregationRepo struct {
	OpsRepository
	started    chan struct{}
	cancelled  chan struct{}
	release    chan struct{}
	heartbeats atomic.Int32
	blockStage string
}

func (r *stoppingOpsAggregationRepo) wait(ctx context.Context) error {
	r.started <- struct{}{}
	<-ctx.Done()
	r.cancelled <- struct{}{}
	<-r.release
	return ctx.Err()
}

func (r *stoppingOpsAggregationRepo) latest(ctx context.Context) (time.Time, bool, error) {
	if r.blockStage == "latest" {
		return time.Time{}, false, r.wait(ctx)
	}
	return time.Time{}, false, nil
}

func (r *stoppingOpsAggregationRepo) GetLatestHourlyBucketStart(ctx context.Context) (time.Time, bool, error) {
	return r.latest(ctx)
}

func (r *stoppingOpsAggregationRepo) GetLatestDailyBucketDate(ctx context.Context) (time.Time, bool, error) {
	return r.latest(ctx)
}

func (r *stoppingOpsAggregationRepo) UpsertHourlyMetrics(ctx context.Context, _, _ time.Time) error {
	return r.wait(ctx)
}

func (r *stoppingOpsAggregationRepo) UpsertDailyMetrics(ctx context.Context, _, _ time.Time) error {
	return r.wait(ctx)
}

func (r *stoppingOpsAggregationRepo) UpsertJobHeartbeat(context.Context, *OpsUpsertJobHeartbeatInput) error {
	r.heartbeats.Add(1)
	return nil
}

func TestOpsAggregationStopCancelsAndJoinsBothWorkers(t *testing.T) {
	for _, stage := range []string{"latest", "metrics"} {
		t.Run(stage, func(t *testing.T) { testOpsAggregationStopAtStage(t, stage) })
	}
}

func testOpsAggregationStopAtStage(t *testing.T, stage string) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	repo := &stoppingOpsAggregationRepo{
		started: make(chan struct{}, 2), cancelled: make(chan struct{}, 2), release: make(chan struct{}),
		blockStage: stage,
	}
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(repo.release) })
	svc := NewOpsAggregationService(repo, nil, nil, client, nil)
	svc.Start()
	await := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("aggregation worker did not reach the expected lifecycle boundary")
		}
	}
	await(repo.started)
	await(repo.started)
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	await(repo.cancelled)
	await(repo.cancelled)
	select {
	case <-stopped:
		t.Fatal("Stop returned before the cancelled queries and their cleanup finished")
	default:
	}
	releaseOnce.Do(func() { close(repo.release) })
	await(stopped)
	svc.Stop()
	if repo.heartbeats.Load() != 0 {
		t.Fatal("shutdown cancellation must not publish a failed or successful aggregation heartbeat")
	}
	if server.Exists(opsAggHourlyLeaderLockKey) || server.Exists(opsAggDailyLeaderLockKey) {
		t.Fatal("Stop returned before releasing the aggregation leader locks")
	}
}

func TestOpsAggregationStopBeforeStartDoesNotQuery(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer client.Close()
	repo := &stoppingOpsAggregationRepo{
		started: make(chan struct{}, 2), cancelled: make(chan struct{}, 2), release: make(chan struct{}), blockStage: "latest",
	}
	svc := NewOpsAggregationService(repo, nil, nil, client, nil)
	defer func() { close(repo.release); svc.Stop() }()
	svc.Stop()
	svc.Start()
	select {
	case <-repo.started:
		t.Fatal("an aggregation query started after the service had stopped")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestOpsAggregationStartStopConcurrent(t *testing.T) {
	for i := 0; i < 30; i++ {
		svc := NewOpsAggregationService(nil, nil, nil, nil, nil)
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); svc.Start() }()
		go func() { defer wg.Done(); svc.Stop() }()
		go func() { defer wg.Done(); svc.Stop() }()
		wg.Wait()
		svc.Start()
		svc.Stop()
	}
}
