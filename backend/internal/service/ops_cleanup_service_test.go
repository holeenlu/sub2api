package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
)

func TestOpsCleanupPlan(t *testing.T) {
	now := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		days         int
		wantOK       bool
		wantTruncate bool
		wantCutoff   time.Time
	}{
		{name: "negative skips", days: -1, wantOK: false},
		{name: "zero truncates", days: 0, wantOK: true, wantTruncate: true},
		{name: "positive yields past cutoff", days: 7, wantOK: true, wantCutoff: now.AddDate(0, 0, -7)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cutoff, truncate, ok := opsCleanupPlan(now, tc.days)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if truncate != tc.wantTruncate {
				t.Fatalf("truncate = %v, want %v", truncate, tc.wantTruncate)
			}
			if !tc.wantTruncate && !cutoff.Equal(tc.wantCutoff) {
				t.Fatalf("cutoff = %v, want %v", cutoff, tc.wantCutoff)
			}
		})
	}
}

func newOpsCleanupTestService(t *testing.T) (*OpsCleanupService, *opsRepoMock) {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Ops.Enabled = true
	cfg.Ops.Cleanup = config.OpsCleanupConfig{Enabled: true, ErrorLogRetentionDays: 7, MinuteMetricsRetentionDays: 7, HourlyMetricsRetentionDays: 7}
	repo := &opsRepoMock{}
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := NewOpsCleanupService(repo, db, nil, cfg, nil, nil)
	svc.computeEffectiveLocked(context.Background())
	return svc, repo
}

func TestOpsCleanupRotatesBatchesAndRetainsCounts(t *testing.T) {
	svc, repo := newOpsCleanupTestService(t)
	batches := []struct {
		table string
		rows  int64
	}{
		{"ops_error_logs", opsCleanupBatchSize}, {"ops_ingress_reject_aggregates", 0}, {"ops_alert_events", 3},
		{"ops_system_logs", opsCleanupBatchSize}, {"ops_system_log_cleanup_audits", 0}, {"ops_system_metrics", 4},
		{"ops_metrics_hourly", 0}, {"ops_metrics_daily", 5}, {"ops_error_logs", 19}, {"ops_system_logs", 7},
	}
	called := 0
	repo.CleanupOpsTableFn = func(_ context.Context, table string, cutoff time.Time, truncate bool, size int) (int64, error) {
		if called >= len(batches) || table != batches[called].table || truncate || size != opsCleanupBatchSize || !cutoff.Before(time.Now()) {
			t.Fatalf("unexpected cleanup call %d: table=%s truncate=%t size=%d cutoff=%s", called, table, truncate, size, cutoff)
		}
		rows := batches[called].rows
		called++
		return rows, nil
	}
	got, err := svc.runCleanupOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := opsCleanupDeletedCounts{errorLogs: opsCleanupBatchSize + 19, alertEvents: 3, systemLogs: opsCleanupBatchSize + 7, systemMetrics: 4, dailyPreagg: 5}
	if got != want || called != len(batches) {
		t.Fatalf("counts=%+v calls=%d", got, called)
	}
}

func TestOpsCleanupTruncatesEachZeroRetentionTableOnce(t *testing.T) {
	svc, repo := newOpsCleanupTestService(t)
	svc.effective.ErrorLogRetentionDays = 0
	svc.effective.MinuteMetricsRetentionDays = -1
	svc.effective.HourlyMetricsRetentionDays = -1
	tables := []string{"ops_error_logs", "ops_ingress_reject_aggregates", "ops_alert_events", "ops_system_logs", "ops_system_log_cleanup_audits"}
	called := 0
	repo.CleanupOpsTableFn = func(_ context.Context, table string, cutoff time.Time, truncate bool, size int) (int64, error) {
		if called >= len(tables) || table != tables[called] || truncate != (called < 3) || size != opsCleanupBatchSize {
			t.Fatalf("unexpected cleanup call %d: %s truncate=%t", called, table, truncate)
		}
		called++
		if truncate {
			if !cutoff.IsZero() {
				t.Fatal("truncate must not use a cutoff")
			}
			return opsCleanupBatchSize, nil
		}
		return 0, nil
	}
	got, err := svc.runCleanupOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if called != len(tables) || got.errorLogs != opsCleanupBatchSize || got.ingressRejects != opsCleanupBatchSize || got.alertEvents != opsCleanupBatchSize {
		t.Fatalf("counts=%+v calls=%d", got, called)
	}
}

func TestOpsCleanupStopsAfterBatchFailure(t *testing.T) {
	svc, repo := newOpsCleanupTestService(t)
	failure := errors.New("database unavailable")
	called := 0
	repo.CleanupOpsTableFn = func(_ context.Context, table string, _ time.Time, _ bool, _ int) (int64, error) {
		called++
		if called == 1 && table == "ops_error_logs" {
			return opsCleanupBatchSize, nil
		}
		if called != 2 || table != "ops_ingress_reject_aggregates" {
			t.Fatalf("unexpected cleanup %d: %s", called, table)
		}
		return 0, failure
	}
	got, err := svc.runCleanupOnce(context.Background())
	if !errors.Is(err, failure) || got.errorLogs != opsCleanupBatchSize || called != 2 {
		t.Fatalf("counts=%+v calls=%d err=%v", got, called, err)
	}
}

func TestOpsCleanupCanceledCycleDoesNotDelete(t *testing.T) {
	svc, repo := newOpsCleanupTestService(t)
	repo.CleanupOpsTableFn = func(context.Context, string, time.Time, bool, int) (int64, error) {
		t.Fatal("canceled cleanup reached repository")
		return 0, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.runCleanupOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

// 一次触发后不再调度，避免生命周期测试依赖分钟边界或生成额外清理任务。
type opsCleanupOnceSchedule struct{ at time.Time }

func (s opsCleanupOnceSchedule) Next(after time.Time) time.Time {
	if after.Before(s.at) {
		return s.at
	}
	return time.Time{}
}

func TestOpsCleanupLifecycleCancelsAndJoinsRunningCleanup(t *testing.T) {
	for _, action := range []string{"stop", "disable"} {
		t.Run(action, func(t *testing.T) {
			svc, repo := newOpsCleanupTestService(t)
			entered := make(chan struct{})
			repo.CleanupOpsTableFn = func(ctx context.Context, _ string, _ time.Time, _ bool, _ int) (int64, error) {
				close(entered)
				<-ctx.Done()
				return 0, ctx.Err()
			}
			runCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan struct{})
			c := cron.New()
			c.Schedule(opsCleanupOnceSchedule{at: time.Now().Add(10 * time.Millisecond)}, cron.FuncJob(func() {
				defer close(finished)
				svc.runScheduled(runCtx)
			}))
			svc.started, svc.cron, svc.cronCancel = true, c, cancel
			c.Start()
			t.Cleanup(svc.Stop)
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("scheduled cleanup did not reach repository")
			}
			if action == "stop" {
				svc.Stop()
			} else {
				svc.cfg.Ops.Cleanup.Enabled = false
				if err := svc.Reload(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-finished:
			default:
				t.Fatal("lifecycle operation returned before canceled cleanup exited")
			}
			if svc.cron != nil || svc.cronCancel != nil || runCtx.Err() == nil {
				t.Fatal("cleanup generation was not canceled and released")
			}
		})
	}
}

func TestOpsCleanupStopDoesNotHoldEffectiveLockWhileJoining(t *testing.T) {
	svc := &OpsCleanupService{started: true}
	runCtx, cancel := context.WithCancel(context.Background())
	entered, finished := make(chan struct{}), make(chan struct{})
	c := cron.New()
	c.Schedule(opsCleanupOnceSchedule{at: time.Now().Add(10 * time.Millisecond)}, cron.FuncJob(func() {
		close(entered)
		<-runCtx.Done()
		_ = svc.snapshotEffective()
		close(finished)
	}))
	svc.cron, svc.cronCancel = c, cancel
	c.Start()
	t.Cleanup(svc.Stop)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup job did not start")
	}
	var stops sync.WaitGroup
	for range 2 {
		stops.Add(1)
		go func() {
			defer stops.Done()
			svc.Stop()
		}()
	}
	stops.Wait()
	select {
	case <-finished:
	default:
		t.Fatal("Stop held the effective-settings lock while waiting for the job")
	}
}

func TestOpsCleanupReloadTimeoutDoesNotStartOverlappingGeneration(t *testing.T) {
	svc, _ := newOpsCleanupTestService(t)
	runCtx, cancel := context.WithCancel(context.Background())
	entered, unblock := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(unblock) }) }
	c := cron.New()
	c.Schedule(opsCleanupOnceSchedule{at: time.Now().Add(10 * time.Millisecond)}, cron.FuncJob(func() {
		close(entered)
		<-unblock
	}))
	svc.started, svc.cron, svc.cronCancel = true, c, cancel
	c.Start()
	t.Cleanup(func() { release(); svc.Stop() })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("cleanup job did not start")
	}
	if err := svc.Reload(context.Background()); err == nil {
		t.Fatal("reload must report a job that did not finish within the shutdown budget")
	}
	if svc.cron != c || runCtx.Err() == nil {
		t.Fatal("reload lost the exiting generation or started a replacement")
	}
	release()
	svc.Stop()
	if svc.cron != nil || svc.cronCancel != nil {
		t.Fatal("completed generation was not released on the next close")
	}
}

func TestOpsCleanupUnlockAfterCancellationKeepsOwnership(t *testing.T) {
	for _, replaced := range []bool{false, true} {
		t.Run(fmt.Sprint(replaced), func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			svc := &OpsCleanupService{redisClient: client}
			ctx, cancel := context.WithCancel(context.Background())
			release, acquired := svc.tryAcquireLeaderLock(ctx)
			if !acquired {
				t.Fatal("could not acquire cleanup lease")
			}
			if replaced {
				if err := server.Set(opsCleanupLeaderLockKeyDefault, "new-owner"); err != nil {
					t.Fatal(err)
				}
			}
			cancel()
			release()
			if replaced {
				if owner, err := server.Get(opsCleanupLeaderLockKeyDefault); err != nil || owner != "new-owner" {
					t.Fatalf("another owner lease was changed: owner=%q err=%v", owner, err)
				}
			} else if server.Exists(opsCleanupLeaderLockKeyDefault) {
				t.Fatal("canceled cleanup retained its own lease")
			}
		})
	}
}

func TestOpsCleanupExpiredLeaseCannotReleaseNextCycle(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	svc := &OpsCleanupService{redisClient: client}
	oldCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldRelease, acquired := svc.tryAcquireLeaderLock(oldCtx)
	if !acquired {
		t.Fatal("could not acquire first cleanup lease")
	}
	server.FastForward(opsCleanupLeaderLockTTLDefault)
	newRelease, acquired := svc.tryAcquireLeaderLock(context.Background())
	if !acquired {
		t.Fatal("could not acquire cleanup lease after expiry")
	}
	newOwner, err := server.Get(opsCleanupLeaderLockKeyDefault)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	oldRelease()
	if owner, err := server.Get(opsCleanupLeaderLockKeyDefault); err != nil || owner != newOwner {
		t.Fatalf("expired lease released the next cycle: owner=%q err=%v", owner, err)
	}
	newRelease()
	if server.Exists(opsCleanupLeaderLockKeyDefault) {
		t.Fatal("current cycle could not release its own lease")
	}
}
