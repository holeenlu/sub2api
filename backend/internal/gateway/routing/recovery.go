package routing

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

// RecoveryStore preserves the existing per-scope Redis epoch/owner contract.
// Epoch stays authoritative in Redis; callers never invent a local epoch.
type RecoveryStore interface {
	BlockRecoveryScope(context.Context, string, time.Duration) error
	AcquireRecoveryProbe(context.Context, string, int64, string) (bool, bool, error)
	RefreshRecoveryProbe(context.Context, string, string) (bool, error)
	FinishRecoveryProbe(context.Context, string, string, time.Duration, bool) error
}

// ErrRecoveryProbeUnavailable denies a generation send, not a new retry lane.
var ErrRecoveryProbeUnavailable = errors.New("recovery probe unavailable")

// RecoveryScope is an already-qualified model/proxy/account health scope.
// ObservedUntil is the existing breaker deadline in Unix milliseconds, not a
// new epoch. Empty scopes are invalid and never mean global permission.
type RecoveryScope struct {
	Key             string
	ObservedUntil   int64
	FailureCooldown time.Duration
}

// Recovery owns all half-open probes for one operation. Construction starts no
// worker. Acquire starts one joined renewal loop only when Redis grants a probe.
// onLost cancels the operation/transport; observe is a non-blocking diagnostic.
type Recovery struct {
	store   RecoveryStore
	root    context.Context
	onLost  func(error)
	observe func(int64, string)
	mu      sync.Mutex // only map membership; never held across storage or join
	active  map[int64]*recoveryClaim
}

type recoveryClaim struct {
	ready    chan struct{}
	lifetime *leaseLifetime // published by closing ready
	mu       sync.Mutex
	finished bool
	success  bool
	cooldown time.Duration
}

// NewRecovery binds one operation to the existing storage and cancellation owner.
// The root must outlive individual transport attempts and be canceled at exit.
func NewRecovery(root context.Context, store RecoveryStore, onLost func(error), observe func(int64, string)) *Recovery {
	return &Recovery{root: root, store: store, onLost: onLost, observe: observe, active: make(map[int64]*recoveryClaim)}
}

// Acquire grants at most one active probe set per resource. A healthy resource
// does not require recovery storage; known failures and partial probe sets do.
// All acquired scopes roll back on rejection. Token must be globally unique.
func (r *Recovery) Acquire(ctx context.Context, resourceID int64, token string, scopes []RecoveryScope, knownBlocked bool) error {
	if ctx == nil || r == nil || r.root == nil || resourceID <= 0 || token == "" || len(scopes) == 0 {
		return ErrRecoveryProbeUnavailable
	}
	for _, scope := range scopes {
		if scope.Key == "" {
			return ErrRecoveryProbeUnavailable
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.root.Err(); err != nil {
		return err
	}
	if r.store == nil {
		if knownBlocked {
			return ErrRecoveryProbeUnavailable
		}
		return nil
	}
	claim := &recoveryClaim{ready: make(chan struct{})}
	r.mu.Lock()
	if r.active[resourceID] != nil {
		r.mu.Unlock()
		return ErrRecoveryProbeUnavailable
	}
	r.active[resourceID] = claim
	r.mu.Unlock()
	started := time.Now()
	var held []RecoveryScope
	startedWorker := false
	defer func() {
		if !startedWorker {
			cleanup, cancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), 2*time.Second)
			defer cancel()
			for _, scope := range held {
				r.finishScope(cleanup, scope, token, 0, false)
			}
			r.remove(resourceID, claim)
		}
		close(claim.ready)
	}()
	for _, scope := range scopes {
		allowed, probe, err := r.store.AcquireRecoveryProbe(ctx, scope.Key, scope.ObservedUntil, token)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, selectionbudget.ErrExhausted) {
				return err
			}
			if knownBlocked || len(held) > 0 {
				return errors.Join(ErrRecoveryProbeUnavailable, err)
			}
			return nil
		}
		if !allowed {
			return ErrRecoveryProbeUnavailable
		}
		if probe {
			held = append(held, scope)
		}
	}
	if len(held) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.root.Err(); err != nil {
		return err
	}
	// Redis grants 60 seconds. Preserve the 20-second refresh interval and
	// 40-second failure grace: a successful second refresh can still confirm
	// ownership before expiry. A failure threshold is not the lease deadline.
	claim.lifetime = &leaseLifetime{
		ttl: 60 * time.Second, interval: 20 * time.Second, timeout: 2 * time.Second, failureGrace: 40 * time.Second,
		confirmedUntil: started.Add(60 * time.Second), lost: ErrRecoveryProbeUnavailable,
		onLost:  r.onLost,
		observe: func(error, bool) { r.record(resourceID, "recovery_lease_unconfirmed") },
		renew: func(ctx context.Context) (bool, error) {
			var failures error
			for _, scope := range held {
				owned, err := r.store.RefreshRecoveryProbe(ctx, scope.Key, token)
				if err == nil && !owned {
					return false, nil
				}
				failures = errors.Join(failures, err)
			}
			return failures == nil, failures
		},
		cleanup: func(ctx context.Context) error {
			claim.mu.Lock()
			claim.finished = true
			success, cooldown := claim.success, claim.cooldown
			claim.mu.Unlock()
			if errors.Is(context.Cause(claim.lifetime.ctx), ErrRecoveryProbeUnavailable) {
				success, cooldown = false, 0
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), 2*time.Second)
			defer cleanupCancel()
			for _, scope := range held {
				delay := cooldown
				if delay > 0 && scope.FailureCooldown > 0 {
					delay = scope.FailureCooldown
				}
				r.finishScope(cleanupCtx, scope, token, delay, success)
			}
			r.remove(resourceID, claim)
			return nil
		},
	}
	claim.lifetime.start(r.root)
	startedWorker = true
	r.record(resourceID, "probe_acquired")
	return nil
}

// Finish joins in-flight acquisition/renewal before returning. Failure cooldown
// has already been classified by the execution boundary; cancellation is zero.
// Repeated/concurrent calls cannot release another token or finish twice.
func (r *Recovery) Finish(resourceID int64, success bool, cooldown time.Duration) {
	if r == nil {
		return
	}
	r.mu.Lock()
	claim := r.active[resourceID]
	r.mu.Unlock()
	if claim == nil {
		return
	}
	<-claim.ready
	claim.mu.Lock()
	if !claim.finished {
		claim.finished = true
		claim.success = success
		claim.cooldown = cooldown
	}
	claim.mu.Unlock()
	_ = claim.lifetime.release()
}

func (r *Recovery) remove(id int64, claim *recoveryClaim) {
	r.mu.Lock()
	if r.active[id] == claim {
		delete(r.active, id)
	}
	r.mu.Unlock()
}

func (r *Recovery) finishScope(ctx context.Context, scope RecoveryScope, token string, cooldown time.Duration, success bool) {
	if err := r.store.FinishRecoveryProbe(ctx, scope.Key, token, cooldown, success); err != nil {
		r.record(0, "recovery_release_unconfirmed")
	}
}

func (r *Recovery) record(resourceID int64, event string) {
	if r.observe != nil {
		r.observe(resourceID, event)
	}
}
