package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type periodicCNBlockingAccounts struct {
	AccountRepository
	entered chan struct{}
	release <-chan struct{}
	calls   atomic.Int32
}

func (r *periodicCNBlockingAccounts) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	if r.calls.Add(1) == 1 {
		close(r.entered)
	}
	if r.release != nil {
		<-r.release // Simulate a dependency that ignores cancellation until released.
	} else {
		<-ctx.Done()
	}
	return nil, ctx.Err()
}

func waitPeriodicIO(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic owner never entered its actual IO dependency")
	}
}

func TestCNPeriodicStopCancelsRepositoryAndRetainsActualJoin(t *testing.T) {
	for _, ignoresCancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("non_cooperative_%t", ignoresCancel), func(t *testing.T) {
			repo := &periodicCNBlockingAccounts{entered: make(chan struct{})}
			var release chan struct{}
			if ignoresCancel {
				release = make(chan struct{})
				repo.release = release
			}
			cfg := &config.Config{}
			cfg.Gateway.CNProviders.BalanceCheckEnabled = true
			svc := NewCNProviderBalanceCheckService(repo, &CNProviderBalanceService{}, nil, cfg, time.Millisecond)
			var releaseOnce sync.Once
			t.Cleanup(func() {
				if release != nil {
					releaseOnce.Do(func() { close(release) })
				}
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, svc.Stop(ctx))
			})
			require.NoError(t, svc.Start(t.Context()))
			require.NoError(t, svc.Start(t.Context()))
			waitPeriodicIO(t, repo.entered)
			if ignoresCancel {
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				require.ErrorIs(t, svc.Stop(ctx), context.DeadlineExceeded)
				cancel()
				releaseOnce.Do(func() { close(release) })
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			require.NoError(t, svc.Drain(ctx))
			require.EqualValues(t, 1, repo.calls.Load(), "canceled polling must not query the next platform")
			require.ErrorIs(t, svc.Start(t.Context()), errMaintenanceStopped)
		})
	}
}

type periodicBlockingLeaderLock struct {
	entered chan struct{}
}

func (l *periodicBlockingLeaderLock) TryAcquireLeaderLock(ctx context.Context, _, _ string, _ time.Duration) (bool, error) {
	close(l.entered)
	<-ctx.Done()
	return false, ctx.Err()
}
func (*periodicBlockingLeaderLock) ReleaseLeaderLock(context.Context, string, string) error {
	return nil
}

func TestPaymentPeriodicStopCancelsLeaderAcquisition(t *testing.T) {
	lock := &periodicBlockingLeaderLock{entered: make(chan struct{})}
	svc := NewPaymentOrderExpiryService(&PaymentService{}, time.Hour)
	svc.SetLeaderLock(lock, nil)
	require.NoError(t, svc.Start(t.Context()))
	waitPeriodicIO(t, lock.entered)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
	require.ErrorIs(t, svc.Start(t.Context()), errMaintenanceStopped)
}

func TestPaymentPeriodicStopReachesEveryActualDatabaseStage(t *testing.T) {
	// With empty result sets, the production service performs deferred-payment,
	// ordinary pending-payment, refund, and expiry SELECTs in that order.
	for blockedStage, name := range []string{"deferred_payment", "pending_payment", "refund", "expiry"} {
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{})
			var once sync.Once
			matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				if !strings.Contains(actual, "payment_orders") {
					return errors.New("unexpected database dependency in payment cycle")
				}
				if expected == "blocked" {
					once.Do(func() { close(entered) })
				}
				return nil
			})
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
			require.NoError(t, err)
			for i := 0; i < blockedStage; i++ {
				mock.ExpectQuery("completed").WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			mock.ExpectQuery("blocked").WillDelayFor(time.Hour).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			mock.ExpectClose()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			svc := NewPaymentOrderExpiryService(&PaymentService{entClient: client}, time.Hour)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, svc.Stop(ctx))
				require.NoError(t, client.Close())
				require.NoError(t, mock.ExpectationsWereMet())
			})
			require.NoError(t, svc.Start(t.Context()))
			waitPeriodicIO(t, entered)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			require.NoError(t, svc.Drain(ctx))
		})
	}
}
