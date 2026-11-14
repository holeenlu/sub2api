// Package reservation owns the lifetime of an already admitted inflight hold.
// Pricing, eligibility, cache admission and fail-open policy belong to callers.
package reservation

import (
	"context"
	"sync"
	"time"
)

// Preserve the existing independent cleanup and renewal budgets.
const (
	cleanupTimeout = 2 * time.Second
	renewTimeout   = 2 * time.Second
)

// Options describes an already admitted reservation. TTL and identifiers come
// from the admission owner; New does not register anything or choose defaults.
// Balance may be nil for scope-only holds. Optional error callbacks run outside
// the state mutex; they must return promptly and must not call lifecycle methods.
// OnReleaseError receives an empty scope for a balance release failure.
type Options struct {
	UserID         int64
	RequestID      string
	Amount         float64
	TTL            time.Duration
	Balance        BalanceReleaser
	OnReleaseError func(scope string, err error)
	OnRenewError   func(err error)
}

type scopeHold struct {
	cache ScopeReleaser
	scope string
}

// Snapshot contains only the identity needed by durable billing recovery to
// release the same holds. Scopes is detached from the handle's internal storage.
type Snapshot struct {
	UserID    int64
	RequestID string
	Balance   bool
	Scopes    []string
}

// Handle holds one handler reference and additional asynchronous billing
// references. HandlerDone stops and joins renewal, but leaves the reservation
// until all billing references return or the cache TTL expires. Release is an
// explicit immediate rollback, independent of references. All methods are nil
// safe; the zero value is inactive. A Handle must not be copied after use.
type Handle struct {
	options Options // immutable after construction

	// mu protects references, scopes and renewal start/stop state. Cache calls,
	// callbacks and waiting for renewal happen outside mu.
	mu             sync.Mutex
	refs           int64
	scopes         []scopeHold
	renewalStopped bool
	renewCancel    context.CancelFunc
	renewDone      chan struct{}
	handlerOnce    sync.Once
	releaseOnce    sync.Once
}

// New constructs an active handle with one handler reference. It performs no
// cache calls and starts no goroutines; the owner explicitly calls StartRenewal.
// Optional cache ports must be nil or usable implementations, never typed nils.
func New(options Options) *Handle {
	return &Handle{options: options, refs: 1}
}

// Amount returns the admitted amount, or zero for a nil handle.
func (r *Handle) Amount() float64 {
	if r == nil {
		return 0
	}
	return r.options.Amount
}

// RequestID returns the shared balance/scope reservation identity, or empty for nil.
func (r *Handle) RequestID() string {
	if r == nil {
		return ""
	}
	return r.options.RequestID
}

// TTL returns the admission owner's expiry duration, or zero for nil.
func (r *Handle) TTL() time.Duration {
	if r == nil {
		return 0
	}
	return r.options.TTL
}

// Snapshot returns the registered release identities, or an empty value for nil.
func (r *Handle) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := Snapshot{UserID: r.options.UserID, RequestID: r.options.RequestID, Balance: r.options.Balance != nil}
	for _, hold := range r.scopes {
		snapshot.Scopes = append(snapshot.Scopes, hold.scope)
	}
	return snapshot
}

// AddScope transfers cleanup ownership of an already registered scope to this
// handle. Call before publishing the handle; a concurrent release still cleans
// up the transferred scope. Nil handles or nil caches do nothing.
func (r *Handle) AddScope(cache ScopeReleaser, scope string) {
	if r == nil || cache == nil {
		return
	}
	r.mu.Lock()
	if r.refs > 0 {
		r.scopes = append(r.scopes, scopeHold{cache: cache, scope: scope})
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	r.reportRelease(scope, cache.ReleaseInflightScope(ctx, scope, r.options.RequestID))
}

// Acquire holds the reservation for a billing task. The returned completion is
// idempotent and must run after deduction or task abandonment. After release (or
// on nil/zero handles) it is a no-op; cancellation alone never returns a reference.
func (r *Handle) Acquire() func() {
	if r == nil {
		return func() {}
	}
	r.mu.Lock()
	if r.refs <= 0 {
		r.mu.Unlock()
		return func() {}
	}
	r.refs++
	r.mu.Unlock()
	var once sync.Once
	return func() { once.Do(r.dropReference) }
}

// HandlerDone idempotently stops and joins renewal, then returns the handler's
// reference. Pending billing keeps the existing cache TTL without renewal.
func (r *Handle) HandlerDone() {
	if r == nil {
		return
	}
	r.handlerOnce.Do(func() {
		r.stopRenewal()
		r.dropReference()
	})
}

func (r *Handle) dropReference() {
	r.mu.Lock()
	if r.refs <= 0 {
		r.mu.Unlock()
		return
	}
	r.refs--
	release := r.refs == 0
	r.mu.Unlock()
	if release {
		r.Release()
	}
}

// Release stops and joins renewal, then releases all registered holds once,
// regardless of references. Only use immediate release for an explicit rollback
// or completed billing, never to turn unresolved usage into a released hold.
// Cleanup shares one independent two-second budget; failures fall back to TTL.
func (r *Handle) Release() {
	if r == nil {
		return
	}
	r.stopRenewal()
	r.releaseOnce.Do(func() {
		r.mu.Lock()
		r.refs = 0
		scopes := r.scopes
		r.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if r.options.Balance != nil {
			r.reportRelease("", r.options.Balance.ReleaseInflightBalance(ctx, r.options.UserID, r.options.RequestID))
		}
		for _, hold := range scopes {
			r.reportRelease(hold.scope, hold.cache.ReleaseInflightScope(ctx, hold.scope, r.options.RequestID))
		}
	})
}

func (r *Handle) reportRelease(scope string, err error) {
	if err != nil && r.options.OnReleaseError != nil {
		r.options.OnReleaseError(scope, err)
	}
}

// StartRenewal explicitly starts at most one balance renewal loop. Calls after
// HandlerDone/Release or with a nil renewer do nothing. The handle owns the loop
// context; HandlerDone/Release cancel and join it, including in-flight cache I/O.
// Scope reservations are never renewed.
func (r *Handle) StartRenewal(renewer BalanceRenewer) {
	if r == nil || renewer == nil {
		return
	}
	r.mu.Lock()
	if r.refs <= 0 || r.renewalStopped || r.renewDone != nil {
		r.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.renewCancel, r.renewDone = cancel, done
	r.mu.Unlock()
	go r.renew(ctx, renewer, done)
}

func (r *Handle) stopRenewal() {
	r.mu.Lock()
	r.renewalStopped = true
	cancel, done := r.renewCancel, r.renewDone
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (r *Handle) renew(ctx context.Context, renewer BalanceRenewer, done chan struct{}) {
	defer close(done)
	interval := max(r.options.TTL/3, 10*time.Millisecond)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			renewCtx, cancel := context.WithTimeout(ctx, renewTimeout)
			alive, err := renewer.RenewInflightBalance(renewCtx, r.options.UserID, r.options.RequestID, r.options.TTL)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				if r.options.OnRenewError != nil {
					r.options.OnRenewError(err)
				}
				continue
			}
			if !alive {
				return
			}
		}
	}
}
