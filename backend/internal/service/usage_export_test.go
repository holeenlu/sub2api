package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type exportRepoStub struct {
	UsageLogRepository
	stream func(context.Context, usagestats.UsageLogFilters, int, func([]UsageLog) error) error
}

func (r exportRepoStub) StreamUsageLogs(ctx context.Context, f usagestats.UsageLogFilters, max int, emit func([]UsageLog) error) error {
	return r.stream(ctx, f, max, emit)
}

type exportLockStub struct {
	mu   sync.Mutex
	held bool
	ttl  time.Duration
}

func (l *exportLockStub) TryAcquireLeaderLock(_ context.Context, _, _ string, ttl time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		return false, nil
	}
	l.held = true
	l.ttl = ttl
	return true, nil
}
func (l *exportLockStub) ReleaseLeaderLock(context.Context, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.held = false
	return nil
}
func TestUsageExportBoundaries(t *testing.T) {
	s := &UsageService{}
	now := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	bounded, err := s.BoundUsageFilters(usagestats.UsageLogFilters{}, now)
	require.NoError(t, err)
	require.Equal(t, now.AddDate(0, 0, -90), *bounded.StartTime)
	old := now.AddDate(0, 0, -91)
	_, err = s.BoundUsageFilters(usagestats.UsageLogFilters{StartTime: &old, EndTime: &now}, now)
	require.Error(t, err)
	start := now.AddDate(-1, 0, 0)
	end := start.AddDate(0, 0, 7)
	_, err = s.BoundUsageFilters(usagestats.UsageLogFilters{StartTime: &start, EndTime: &end}, now)
	require.NoError(t, err, "bounded old history remains queryable")
}
func TestUsageExportLeaseCancellationAndFailure(t *testing.T) {
	lock := &exportLockStub{}
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	repo := exportRepoStub{stream: func(ctx context.Context, _ usagestats.UsageLogFilters, _ int, _ func([]UsageLog) error) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}}
	s := &UsageService{usageRepo: repo, exportLocks: lock, exportLimits: config.UsageExportConfig{TimeoutSeconds: 10}}
	result := make(chan error, 1)
	go func() {
		result <- s.ExportUsage(ctx, 7, usagestats.UsageLogFilters{UserID: 7}, func([]UsageLog) error { return nil })
	}()
	<-entered
	err := s.ExportUsage(context.Background(), 7, usagestats.UsageLogFilters{UserID: 7}, func([]UsageLog) error { return nil })
	require.ErrorContains(t, err, "already running")
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	lock.mu.Lock()
	require.False(t, lock.held)
	require.Greater(t, lock.ttl, s.UsageExportTimeout())
	lock.mu.Unlock()
	want := errors.New("client write failed")
	s.usageRepo = exportRepoStub{stream: func(_ context.Context, _ usagestats.UsageLogFilters, _ int, emit func([]UsageLog) error) error {
		return emit([]UsageLog{{ID: 1}})
	}}
	require.ErrorIs(t, s.ExportUsage(context.Background(), 7, usagestats.UsageLogFilters{UserID: 7}, func([]UsageLog) error { return want }), want)
	lock.mu.Lock()
	require.False(t, lock.held)
	lock.mu.Unlock()
}
