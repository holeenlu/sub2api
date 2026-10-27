package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// ConcurrencySlotLeaseCache is implemented by expiring slot stores. Renewal
// only confirms an existing member; it must never recreate lost capacity.
type ConcurrencySlotLeaseCache interface {
	RefreshAccountSlot(context.Context, int64, string) (bool, error)
	RefreshUserSlot(context.Context, int64, string) (bool, error)
	RefreshAPIKeySlot(context.Context, int64, string) (bool, error)
}

var (
	ErrConcurrencySlotLeaseLost    = errors.New("concurrency slot lease lost")
	ErrConcurrencySlotLeaseExpired = errors.New("concurrency slot lease renewal expired")
	ErrConcurrencySlotLifecycle    = errors.New("concurrency slot request lifecycle is unavailable")
)

type concurrencySlotLifecycleKey struct{}

// The owner belongs to the gateway request, not a temporary selection/wait
// context. Its separate failure signal also reaches detached usage drains.
type concurrencySlotLifecycle struct {
	failure       context.Context
	fail          context.CancelCauseFunc
	cancelRequest context.CancelCauseFunc
	mu            sync.Mutex
	closed        bool
	leases        map[*concurrencySlotLease]struct{}
}

// WithConcurrencySlotLifecycle binds slot failures to the real request before
// handlers derive selection, wait or detached upstream contexts. Finish joins
// and releases any remaining leases, including exceptional handler exits.
func WithConcurrencySlotLifecycle(ctx context.Context) (context.Context, func()) {
	request, cancelRequest := context.WithCancelCause(ctx)
	failure, fail := context.WithCancelCause(context.Background())
	owner := &concurrencySlotLifecycle{
		failure: failure, fail: fail, cancelRequest: cancelRequest,
		leases: make(map[*concurrencySlotLease]struct{}),
	}
	return context.WithValue(request, concurrencySlotLifecycleKey{}, owner), sync.OnceFunc(func() {
		owner.mu.Lock()
		owner.closed = true
		leases := make([]*concurrencySlotLease, 0, len(owner.leases))
		for lease := range owner.leases {
			leases = append(leases, lease)
		}
		owner.mu.Unlock()
		for _, lease := range leases {
			lease.release()
		}
		cancelRequest(nil)
		fail(nil)
	})
}

func concurrencySlotOwner(ctx context.Context) *concurrencySlotLifecycle {
	if ctx == nil {
		return nil
	}
	owner, _ := ctx.Value(concurrencySlotLifecycleKey{}).(*concurrencySlotLifecycle)
	return owner
}

func (o *concurrencySlotLifecycle) lost(err error) {
	o.fail(err)
	o.cancelRequest(err)
}

// Retain lease cancellation when a transport deliberately detaches client
// cancellation to finish usage collection. Billing persistence stays detached.
func withConcurrencySlotCancellation(ctx context.Context) (context.Context, context.CancelFunc) {
	owner := concurrencySlotOwner(ctx)
	if owner == nil {
		return ctx, func() {}
	}
	bound, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(owner.failure, func() { cancel(context.Cause(owner.failure)) })
	if err := context.Cause(owner.failure); err != nil {
		cancel(err)
	}
	return bound, func() { stop(); cancel(nil) }
}

type concurrencySlotLease struct {
	owner       *concurrencySlotLifecycle
	stop        context.CancelFunc
	done        chan struct{}
	once        sync.Once
	releaseSlot func(context.Context) error
	refresh     func(context.Context) (bool, error)
	ttl         time.Duration
	started     time.Time
	scope       string
	id          int64
}

func maintainConcurrencySlot(owner *concurrencySlotLifecycle, ttl time.Duration, started time.Time, scope string, id int64, refresh func(context.Context) (bool, error), release func(context.Context) error) func() {
	ctx, stop := context.WithCancel(context.Background())
	lease := &concurrencySlotLease{
		owner: owner, stop: stop, done: make(chan struct{}),
		releaseSlot: release, refresh: refresh, ttl: ttl, started: started, scope: scope, id: id,
	}
	go lease.run(ctx)
	if owner != nil {
		owner.mu.Lock()
		closed := owner.closed
		if !closed {
			owner.leases[lease] = struct{}{}
		}
		owner.mu.Unlock()
		if closed {
			lease.release()
		}
	}
	return lease.release
}

func (l *concurrencySlotLease) run(ctx context.Context) {
	defer close(l.done)
	// Redis slot scores use whole seconds. Confirm from operation start and
	// leave one second of margin so local time never outlives Redis ownership.
	margin := min(time.Second, l.ttl/10)
	confirmedUntil := l.started.Add(l.ttl - margin)
	delay := l.ttl / 3
	for {
		remaining := time.Until(confirmedUntil)
		if remaining <= 0 {
			l.lost(ErrConcurrencySlotLeaseExpired)
			return
		}
		timer := time.NewTimer(min(delay, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return
		}
		if !time.Now().Before(confirmedUntil) {
			l.lost(ErrConcurrencySlotLeaseExpired)
			return
		}
		started := time.Now()
		operationDeadline := started.Add(2 * time.Second)
		if confirmedUntil.Before(operationDeadline) {
			operationDeadline = confirmedUntil
		}
		operation, cancel := context.WithDeadline(ctx, operationDeadline)
		// Some Redis transports finish their socket timeout after ctx expires.
		// Cancel the real request at the last confirmed lease deadline even if
		// that I/O has not returned; release still joins it before deleting slots.
		expired := make(chan struct{})
		watchdog := time.AfterFunc(time.Until(confirmedUntil), func() {
			if ctx.Err() == nil {
				l.lost(ErrConcurrencySlotLeaseExpired)
			}
			close(expired)
		})
		owned, err := l.refresh(operation)
		cancel()
		if !watchdog.Stop() {
			<-expired
			return
		}
		if ctx.Err() != nil {
			return
		}
		if !time.Now().Before(confirmedUntil) {
			l.lost(ErrConcurrencySlotLeaseExpired)
			return
		}
		if err == nil && !owned {
			l.lost(ErrConcurrencySlotLeaseLost)
			return
		}
		if err == nil {
			confirmedUntil = started.Add(l.ttl - margin)
			delay = l.ttl / 3
			continue
		}
		logger.L().Warn("concurrency_slot_renewal_failed", zap.String("scope", l.scope), zap.Int64("id", l.id), zap.Error(err))
		delay = min(time.Second, l.ttl/10)
	}
}

func (l *concurrencySlotLease) lost(err error) {
	if l.owner != nil && l.scope != "api_key" {
		l.owner.lost(err)
	}
	logger.L().Warn("concurrency_slot_lease_lost", zap.String("scope", l.scope), zap.Int64("id", l.id), zap.Error(err))
}

func (l *concurrencySlotLease) release() {
	l.once.Do(func() {
		l.stop()
		<-l.done
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := l.releaseSlot(ctx); err != nil {
			logger.L().Warn("concurrency_slot_release_failed", zap.String("scope", l.scope), zap.Int64("id", l.id), zap.Error(err))
		}
		if l.owner != nil {
			l.owner.mu.Lock()
			delete(l.owner.leases, l)
			l.owner.mu.Unlock()
		}
	})
}
