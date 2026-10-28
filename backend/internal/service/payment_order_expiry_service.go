package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

const expiryCheckTimeout = 30 * time.Second

const (
	// paymentOrderExpiryLeaderLockKey gates the periodic reconcile + expiry sweep so
	// that only one instance issues the upstream payment-provider calls per cycle.
	paymentOrderExpiryLeaderLockKey = "payment:order:expiry:leader"
	// paymentOrderExpiryLeaderLockTTL must exceed the combined reconcile + expiry
	// timeouts (3 * expiryCheckTimeout) so the lock never expires mid-run.
	paymentOrderExpiryLeaderLockTTL = 3 * time.Minute
)

// PaymentOrderExpiryService periodically expires timed-out payment orders.
type PaymentOrderExpiryService struct {
	paymentSvc *PaymentService
	interval   time.Duration
	tasks      maintenanceTaskOwner

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

func NewPaymentOrderExpiryService(paymentSvc *PaymentService, interval time.Duration) *PaymentOrderExpiryService {
	return &PaymentOrderExpiryService{
		paymentSvc: paymentSvc,
		interval:   interval,
		instanceID: uuid.NewString(),
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the periodic reconcile/expiry sweep. When both are nil the job
// runs ungated (single-instance / test behavior).
func (s *PaymentOrderExpiryService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *PaymentOrderExpiryService) Start(ctx context.Context) error {
	if s == nil || s.paymentSvc == nil || s.interval <= 0 {
		return nil
	}
	return s.tasks.start(ctx, func(context.Context) error { return s.tasks.launch(s.loop) })
}

func (s *PaymentOrderExpiryService) loop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runOnce(ctx)
		}
	}
}

func (s *PaymentOrderExpiryService) RequestStop() {
	if s != nil {
		s.tasks.seal(true)
	}
}

// Reconciliation remains durable/idempotent in PaymentService. The periodic
// owner cancels in-flight IO and retains its real join across deadline retries.
func (s *PaymentOrderExpiryService) Drain(ctx context.Context) error {
	if s == nil {
		return nil
	}
	return s.tasks.stop(ctx)
}

func (s *PaymentOrderExpiryService) Stop(ctx context.Context) error {
	return s.Drain(ctx)
}

func (s *PaymentOrderExpiryService) runOnce(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	// Multi-instance guard: only the leader reconciles/expires orders per cycle,
	// avoiding N× upstream payment-provider API calls and update races.
	lockCtx, lockCancel := context.WithTimeout(ctx, 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, paymentOrderExpiryLeaderLockKey, s.instanceID, paymentOrderExpiryLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()
	if ctx.Err() != nil {
		return
	}

	reconcileCtx, cancel := context.WithTimeout(ctx, expiryCheckTimeout)
	recovered, err := s.paymentSvc.ReconcilePendingPaymentOrders(reconcileCtx)
	cancel()
	if err != nil {
		slog.Warn("[PaymentOrderExpiry] failed to reconcile pending payment orders", "error", err)
	} else if recovered > 0 {
		slog.Info("[PaymentOrderExpiry] reconciled paid orders", "count", recovered)
	}

	if ctx.Err() != nil {
		return
	}
	refundCtx, refundCancel := context.WithTimeout(ctx, expiryCheckTimeout)
	if err := s.paymentSvc.ReconcilePendingRefunds(refundCtx); err != nil {
		slog.Warn("refund reconciliation failed", "error", err)
	}
	refundCancel()

	if ctx.Err() != nil {
		return
	}
	expireCtx, cancel := context.WithTimeout(ctx, expiryCheckTimeout)
	defer cancel()
	expired, err := s.paymentSvc.ExpireTimedOutOrders(expireCtx)
	if err != nil {
		slog.Error("[PaymentOrderExpiry] failed to expire orders", "error", err)
		return
	}
	if expired > 0 {
		slog.Info("[PaymentOrderExpiry] expired timed-out orders", "count", expired)
	}
}
