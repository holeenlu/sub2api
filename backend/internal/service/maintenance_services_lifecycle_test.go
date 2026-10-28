//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func waitBackupTasks(t *testing.T, svc *BackupService) {
	t.Helper()
	require.Eventually(t, func() bool {
		svc.tasks.mu.Lock()
		defer svc.tasks.mu.Unlock()
		return svc.tasks.pending == 0
	}, 3*time.Second, time.Millisecond)
}

type localBalanceNotificationSender struct{}

func (localBalanceNotificationSender) SendEmail(context.Context, string, string, string) error {
	return nil
}

type cancelableBackupDumper struct {
	entered chan struct{}
	release <-chan struct{}
}

func (d *cancelableBackupDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	close(d.entered)
	if d.release != nil {
		<-d.release
	} else {
		<-ctx.Done()
	}
	return nil, ctx.Err()
}
func (d *cancelableBackupDumper) Restore(ctx context.Context, _ io.Reader) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestBackupLifecycleStopCancelsDumpAndPersistsFailure(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	dumper := &cancelableBackupDumper{entered: make(chan struct{})}
	svc := newTestBackupService(t, repo, dumper, newMockObjectStore())
	record, err := svc.StartBackup(t.Context(), "manual", 14)
	require.NoError(t, err)
	select {
	case <-dumper.entered:
	case <-time.After(time.Second):
		t.Fatal("dump not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
	saved, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", saved.Status)
	require.Contains(t, saved.ErrorMsg, "context canceled")
	_, err = svc.StartBackup(context.Background(), "manual", 14)
	require.Error(t, err)
}

func TestBackupLifecycleStopTimeoutRetainsDumpOwner(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	release := make(chan struct{})
	var once sync.Once
	dumper := &cancelableBackupDumper{entered: make(chan struct{}), release: release}
	svc := newTestBackupService(t, repo, dumper, newMockObjectStore())
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	_, err := svc.StartBackup(t.Context(), "manual", 14)
	require.NoError(t, err)
	select {
	case <-dumper.entered:
	case <-time.After(time.Second):
		t.Fatal("dump not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Stop(ctx), context.DeadlineExceeded)
	select {
	case <-svc.tasks.done:
		t.Fatal("backup falsely joined")
	default:
	}
	once.Do(func() { close(release) })
	stopMaintenanceForTest(t, svc)
}

type lifecycleNotificationSender struct{ entered, canceled chan struct{} }

func (s *lifecycleNotificationSender) SendEmail(ctx context.Context, _, _, _ string) error {
	close(s.entered)
	<-ctx.Done()
	close(s.canceled)
	return ctx.Err()
}

func TestBalanceNotifyLifecycleOwnsRequestTriggeredMail(t *testing.T) {
	repo := newMockSettingRepo()
	require.NoError(t, repo.SetMultiple(t.Context(), map[string]string{SettingKeyBalanceLowNotifyEnabled: "true", SettingKeyBalanceLowNotifyThreshold: "10"}))
	sender := &lifecycleNotificationSender{entered: make(chan struct{}), canceled: make(chan struct{})}
	svc := NewBalanceNotifyService(nil, repo, nil)
	svc.emailService = sender
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() { stopMaintenanceForTest(t, svc) })
	user := &User{ID: 1, BalanceNotifyEnabled: true, BalanceNotifyExtraEmails: []NotifyEmailEntry{{Email: "local@example.invalid", Verified: true}}}
	svc.CheckBalanceAfterDeduction(t.Context(), user, 11, 2)
	select {
	case <-sender.entered:
	case <-time.After(time.Second):
		t.Fatal("local sender not called")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Drain(ctx), context.DeadlineExceeded)
	svc.CheckBalanceAfterDeduction(t.Context(), user, 11, 2) // fenced; a duplicate would panic closing entered
	stopMaintenanceForTest(t, svc)
	select {
	case <-sender.canceled:
	default:
		t.Fatal("Stop returned before transport exited")
	}
}

type lifecycleQueueCache struct {
	cleanupWorkerUserMsgQueueCache
	entered chan struct{}
}

func (c *lifecycleQueueCache) ReconcileExpiredLockCandidates(ctx context.Context, _ int) (int, error) {
	close(c.entered)
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestUserMessageQueueLifecycleStopJoinsCleanupIO(t *testing.T) {
	cache := &lifecycleQueueCache{entered: make(chan struct{})}
	svc := NewUserMessageQueueService(cache, nil, &config.UserMessageQueueConfig{CleanupIntervalSeconds: 1})
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() { stopMaintenanceForTest(t, svc) })
	select {
	case <-cache.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup not called")
	}
	stopMaintenanceForTest(t, svc)
	require.ErrorIs(t, svc.Start(t.Context()), errMaintenanceStopped)
}

type lifecycleOpsRepository struct {
	OpsRepository
	hourly, daily chan struct{}
}

func (r *lifecycleOpsRepository) GetLatestHourlyBucketStart(ctx context.Context) (time.Time, bool, error) {
	close(r.hourly)
	<-ctx.Done()
	return time.Time{}, false, ctx.Err()
}
func (r *lifecycleOpsRepository) GetLatestDailyBucketDate(ctx context.Context) (time.Time, bool, error) {
	close(r.daily)
	<-ctx.Done()
	return time.Time{}, false, ctx.Err()
}
func (*lifecycleOpsRepository) UpsertHourlyMetrics(ctx context.Context, _, _ time.Time) error {
	return ctx.Err()
}
func (*lifecycleOpsRepository) UpsertDailyMetrics(ctx context.Context, _, _ time.Time) error {
	return ctx.Err()
}
func (*lifecycleOpsRepository) UpsertJobHeartbeat(ctx context.Context, _ *OpsUpsertJobHeartbeatInput) error {
	return ctx.Err()
}

func TestOpsAggregationLifecycleJoinsBothDatabaseLoops(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	repo := &lifecycleOpsRepository{hourly: make(chan struct{}), daily: make(chan struct{})}
	svc := NewOpsAggregationService(repo, nil, nil, rdb, nil)
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() { stopMaintenanceForTest(t, svc) })
	for _, entered := range []<-chan struct{}{repo.hourly, repo.daily} {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("aggregation loop not started")
		}
	}
	stopMaintenanceForTest(t, svc)
	select {
	case <-svc.tasks.done:
	default:
		t.Fatal("aggregation not joined")
	}
}

type lifecycleMetricsSettings struct {
	SettingRepository
	entered chan struct{}
	once    sync.Once
}

func (r *lifecycleMetricsSettings) GetValue(ctx context.Context, _ string) (string, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	return "", ctx.Err()
}

func TestOpsMetricsLifecycleCancelsCollectionAndJoinsSampler(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
	settings := &lifecycleMetricsSettings{entered: make(chan struct{})}
	cfg := &config.Config{}
	cfg.Ops.Enabled = true
	svc := NewOpsMetricsCollector(&lifecycleOpsRepository{}, settings, nil, nil, db, nil, cfg)
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() { stopMaintenanceForTest(t, svc) })
	select {
	case <-settings.entered:
	case <-time.After(time.Second):
		t.Fatal("collection settings IO not started")
	}
	stopMaintenanceForTest(t, svc)
	select {
	case <-svc.tasks.done:
	default:
		t.Fatal("resource sampler not joined")
	}
}

func TestBackupLifecycleConstructionAndExplicitSchedulerStart(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewBackupService(repo, &config.Config{}, &plainEncryptor{}, nil, &mockDumper{})
	require.Nil(t, svc.cronSched)
	require.Nil(t, svc.tasks.done)
	require.NoError(t, svc.Start(t.Context()))
	t.Cleanup(func() { stopMaintenanceForTest(t, svc) })
	cron := svc.cronSched
	require.NotNil(t, cron)
	require.NoError(t, svc.Start(t.Context()))
	require.Same(t, cron, svc.cronSched)
	stopMaintenanceForTest(t, svc)
	require.NotNil(t, svc.cronStopped)
	select {
	case <-svc.cronStopped.Done():
	default:
		t.Fatal("cron not joined")
	}
}

type lifecycleCloseErrorReader struct {
	io.Reader
	err error
}

func (r lifecycleCloseErrorReader) Close() error { return r.err }

type lifecycleCloseErrorDumper struct{ err error }

func (d lifecycleCloseErrorDumper) Dump(context.Context) (io.ReadCloser, error) {
	return lifecycleCloseErrorReader{Reader: strings.NewReader("local fake dump"), err: d.err}, nil
}
func (lifecycleCloseErrorDumper) Restore(context.Context, io.Reader) error { return nil }

func TestBackupLifecycleReportsReaderCloseFailureAfterJoin(t *testing.T) {
	repo := newMockSettingRepo()
	seedS3Config(t, repo)
	want := errors.New("local dump close failed")
	store := newMockObjectStore()
	svc := NewBackupService(repo, &config.Config{}, &plainEncryptor{}, func(context.Context, *BackupS3Config) (BackupObjectStore, error) { return store, nil }, lifecycleCloseErrorDumper{err: want})
	require.NoError(t, svc.tasks.start(t.Context(), nil))
	record, err := svc.CreateBackup(t.Context(), "manual", 14)
	require.ErrorIs(t, err, want)
	require.Equal(t, "failed", record.Status)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.ErrorIs(t, svc.Stop(ctx), want)
	select {
	case <-svc.tasks.done:
	default:
		t.Fatal("close error lost completion state")
	}
}

type lifecycleUsageBillingRepository struct {
	UsageBillingRepository
	applied chan struct{}
}

func (r lifecycleUsageBillingRepository) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	close(r.applied)
	return &UsageBillingApplyResult{Applied: true}, nil
}

func TestBalanceNotifyLifecycleAdmissionPrecedesUsageReturn(t *testing.T) {
	settings := newMockSettingRepo()
	require.NoError(t, settings.SetMultiple(t.Context(), map[string]string{SettingKeyBalanceLowNotifyEnabled: "true", SettingKeyBalanceLowNotifyThreshold: "10"}))
	notifier := NewBalanceNotifyService(nil, settings, nil)
	notifier.emailService = localBalanceNotificationSender{}
	require.NoError(t, notifier.Start(t.Context()))
	t.Cleanup(func() { stopMaintenanceForTest(t, notifier) })
	notifier.tasks.mu.Lock()
	var once sync.Once
	unlock := func() { once.Do(notifier.tasks.mu.Unlock) }
	defer unlock()
	repo := lifecycleUsageBillingRepository{applied: make(chan struct{})}
	returned := make(chan error, 1)
	go func() {
		_, err := applyUsageBilling(context.Background(), "owned-notification", &UsageLog{}, &postUsageBillingParams{
			Cost: &CostBreakdown{ActualCost: 2}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3},
			User: &User{ID: 1, Balance: 11, BalanceNotifyEnabled: true, BalanceNotifyExtraEmails: []NotifyEmailEntry{{Email: "local@example.invalid", Verified: true}}},
		}, &billingDeps{balanceNotifyService: notifier}, repo)
		returned <- err
	}()
	select {
	case <-repo.applied:
	case <-time.After(time.Second):
		t.Fatal("billing adapter not reached")
	}
	select {
	case <-returned:
		t.Fatal("Usage returned before handing the alert to its owner")
	case <-time.After(20 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-returned:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Usage handoff did not complete")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, notifier.Drain(ctx))
}
