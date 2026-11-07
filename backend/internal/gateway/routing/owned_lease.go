package routing

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrCapacityLeaseLost             = errors.New("capacity lease ownership lost")
	ErrCapacityDependencyUnavailable = errors.New("capacity lease authority unavailable")
)

// LeaseIdentity names existing Redis members, not a fabricated epoch. Token is
// the globally unique acquiring request/Live-call token and is never reusable.
type LeaseIdentity struct {
	AccountID, UserID, APIKeyID int64
	Token                       string
}

// LeaseSnapshot is diagnostic only. A snapshot cannot substitute for Validate.
type LeaseSnapshot struct {
	Identity  LeaseIdentity
	ExpiresAt time.Time
}

// Lease owns one verified token's renewal and cancellation. Redis confirms the
// exact member and its remaining lifetime. No epoch exists for these ZSET slots.
// Release stops and joins renewal/validation before releasing that token once.
type Lease struct {
	identity     LeaseIdentity
	ctx          context.Context
	cancel       context.CancelCauseFunc
	done         chan struct{}
	changed      chan struct{}
	mu           sync.Mutex // deadline only; no I/O under this lock
	expiresAt    time.Time
	interval     time.Duration
	opMu         sync.Mutex // serializes bounded checks/renewals and final release
	check, renew func(context.Context) (time.Duration, error)
	release      func(context.Context) error
	releaseOnce  sync.Once
	releaseErr   error
	autoRelease  bool
}

// newOwnedLease starts only after a positive proof from the real storage port.
// The caller owns Release. Acquisition contexts can be short selection phases;
// Bind explicitly attaches the successful lease to its operation owner.
func newOwnedLease(identity LeaseIdentity, started time.Time, validFor time.Duration, check, renew func(context.Context) (time.Duration, error), release func(context.Context) error, autoRelease bool) (*Lease, error) {
	if validFor <= 0 || !time.Now().Before(started.Add(validFor)) {
		return nil, ErrCapacityLeaseLost
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	lease := &Lease{identity: identity, ctx: ctx, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{}, 1), expiresAt: started.Add(validFor), interval: validFor / 3, check: check, renew: renew, release: release, autoRelease: autoRelease}
	go lease.run()
	return lease, nil
}

func (l *Lease) Context() context.Context {
	if l == nil {
		return nil
	}
	return l.ctx
}
func (l *Lease) Snapshot() LeaseSnapshot {
	if l == nil {
		return LeaseSnapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return LeaseSnapshot{Identity: l.identity, ExpiresAt: l.expiresAt}
}

// Bind makes cancellation flow both ways without changing the acquiring token.
// Call the returned cancel to detach the binding and still defer Release. This
// explicit handoff avoids accidentally canceling a lease when selection ends.
func (l *Lease) Bind(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(parent)
	if l == nil {
		cancel(ErrCapacityDependencyUnavailable)
		return ctx, func() {}
	}
	stopLease := context.AfterFunc(l.ctx, func() { cancel(context.Cause(l.ctx)) })
	stopParent := context.AfterFunc(parent, func() { l.cancel(context.Cause(parent)) })
	if l.ctx.Err() != nil {
		cancel(context.Cause(l.ctx))
	}
	return ctx, func() { stopLease(); stopParent(); cancel(nil) }
}

// Validate confirms current ownership for a new send. Dependency errors deny
// this send; they never turn a limited slot into unlimited capacity. Missing or
// expired members also cancel the in-flight owner's context immediately.
func (l *Lease) Validate(ctx context.Context) error {
	if l == nil {
		return ErrCapacityDependencyUnavailable
	}
	return l.confirm(ctx, false)
}

func (l *Lease) confirm(ctx context.Context, renew bool) error {
	l.opMu.Lock()
	defer l.opMu.Unlock()
	if err := context.Cause(l.ctx); err != nil {
		return err
	}
	snapshot := l.Snapshot()
	if !time.Now().Before(snapshot.ExpiresAt) {
		l.cancel(ErrCapacityLeaseLost)
		return ErrCapacityLeaseLost
	}
	started := time.Now()
	deadline := minTime(snapshot.ExpiresAt, started.Add(3*time.Second))
	opCtx, cancel := context.WithDeadline(ctx, deadline)
	stop := context.AfterFunc(l.ctx, cancel)
	defer func() { stop(); cancel() }()
	operation := l.check
	if renew {
		operation = l.renew
	}
	remaining, err := operation(opCtx)
	if l.ctx.Err() != nil {
		return context.Cause(l.ctx)
	}
	if !time.Now().Before(snapshot.ExpiresAt) {
		l.cancel(ErrCapacityLeaseLost)
		return ErrCapacityLeaseLost
	}
	if err != nil {
		return errors.Join(ErrCapacityDependencyUnavailable, err)
	}
	if remaining <= 0 {
		l.cancel(ErrCapacityLeaseLost)
		return ErrCapacityLeaseLost
	}
	deadline = started.Add(remaining)
	if !time.Now().Before(deadline) {
		l.cancel(ErrCapacityLeaseLost)
		return ErrCapacityLeaseLost
	}
	l.mu.Lock()
	if renew || deadline.Before(l.expiresAt) {
		l.expiresAt = deadline
	}
	l.mu.Unlock()
	select {
	case l.changed <- struct{}{}:
	default:
	}
	return nil
}

func (l *Lease) run() {
	defer close(l.done)
	if l.autoRelease {
		defer l.releaseToken()
	}
	renew := time.NewTimer(max(time.Millisecond, l.interval))
	defer renew.Stop()
	expiry := time.NewTimer(time.Until(l.Snapshot().ExpiresAt))
	defer expiry.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.changed:
			expiry.Reset(max(0, time.Until(l.Snapshot().ExpiresAt)))
		case <-expiry.C:
			if time.Now().Before(l.Snapshot().ExpiresAt) {
				expiry.Reset(time.Until(l.Snapshot().ExpiresAt))
				continue
			}
			l.cancel(ErrCapacityLeaseLost)
			return
		case <-renew.C:
			_ = l.confirm(context.Background(), true)
			if l.ctx.Err() != nil {
				return
			}
			remaining := time.Until(l.Snapshot().ExpiresAt)
			renew.Reset(max(time.Millisecond, min(l.interval, remaining/2)))
		}
	}
}

func (l *Lease) releaseToken() {
	l.releaseOnce.Do(func() {
		l.opMu.Lock()
		defer l.opMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.releaseErr = l.release(ctx)
	})
}

// Release is idempotent and joins even if the request or storage has failed.
func (l *Lease) Release() error {
	if l == nil {
		return nil
	}
	l.cancel(context.Canceled)
	l.Join()
	l.releaseToken()
	return l.releaseErr
}

// Handoff stops this local Live controller without freeing its call token. A
// successor must freshly validate the token; this object can never send again.
// Ordinary slots auto-release and must not use Handoff as a transfer mechanism.
func (l *Lease) Handoff() {
	if l == nil {
		return
	}
	l.cancel(context.Canceled)
	l.Join()
}

// Join waits for cancellation/expiry and every in-flight authority operation.
// Release and Handoff already call it; it never cancels a still-active owner.
func (l *Lease) Join() {
	if l == nil {
		return
	}
	<-l.done
	l.opMu.Lock()
	l.opMu.Unlock()
}
