package routing

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

// CapacityStore retains the atomic Redis capacity admission and token release.
// Limited acquisition additionally requires CapacityLeaseStore; absence is a
// hard dependency error, never an unverified success or an unlimited slot.
type CapacityStore interface {
	AcquireAccountSlot(context.Context, int64, int, string) (bool, error)
	ReleaseAccountSlot(context.Context, int64, string) error
	AcquireUserSlot(context.Context, int64, int, string) (bool, error)
	ReleaseUserSlot(context.Context, int64, string) error
}

// CapacityLeaseStore checks or renews only an existing, unexpired token. The
// positive duration is derived from Redis TIME and the existing member score.
// Zero means absent/expired; errors mean the authority could not be consulted.
// Refresh must never insert/reacquire a missing member.
type CapacityLeaseStore interface {
	CheckCapacityLease(context.Context, CapacityRequest) (time.Duration, error)
	RefreshCapacityLease(context.Context, CapacityRequest) (time.Duration, error)
}

// CapacityRequest binds one unique token to one concrete account or user.
// User selects the user namespace; otherwise ID is an account. Limit<=0 is the
// existing explicit unlimited mode, not a fallback when storage is unavailable.
type CapacityRequest struct {
	ID    int64
	Token string
	Limit int
	User  bool
	Wait  bool // token must own the head of this resource's Redis waiting queue
}

// AcquireResult retains the ingress/scheduler's existing ownership contract.
// Call ReleaseFunc on every successful exit; it is safe to call concurrently.
type AcquireResult struct {
	Acquired    bool
	Unlimited   bool // explicit non-positive configured limit; Lease is nil only here
	Lease       *Lease
	ReleaseFunc func()
	policy      CapacityPolicy
	released    *atomic.Bool // shared by value copies; this receipt is not a second lease owner
}

// AcquireCapacity acquires once and transfers the token's sole release owner to
// the caller. It neither waits nor retries; admission policy stays with routing.
func AcquireCapacity(ctx context.Context, store CapacityStore, request CapacityRequest, onReleaseError func(error)) (*AcquireResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if WaitBudgetFrom(ctx).expired() {
		return nil, ErrCapacityWaitExpired
	}
	if request.ID <= 0 {
		return nil, ErrCapacityDependencyUnavailable
	}
	policy := CapacityPolicy{ResourceID: request.ID, User: request.User, Limit: request.Limit}
	if request.Limit <= 0 {
		return acquiredCapacityResult(ctx, policy, nil)
	}
	if store == nil || request.ID <= 0 || request.Token == "" {
		return nil, ErrCapacityDependencyUnavailable
	}
	authority, ok := store.(CapacityLeaseStore)
	if !ok {
		return nil, ErrCapacityDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	acquire, release := store.AcquireAccountSlot, store.ReleaseAccountSlot
	if request.User {
		acquire, release = store.AcquireUserSlot, store.ReleaseUserSlot
	}
	var acquired bool
	var err error
	if request.Wait {
		queued, ok := store.(CapacityWaitStore)
		if !ok {
			return nil, ErrCapacityDependencyUnavailable
		}
		acquired, err = queued.AcquireQueuedCapacity(ctx, request)
	} else {
		acquired, err = acquire(ctx, request.ID, request.Limit, request.Token)
	}
	if err != nil {
		return nil, err
	}
	if !acquired {
		return &AcquireResult{}, nil
	}
	cleanup := func(cleanupCtx context.Context) error {
		// The lease owns cancellation/join, while this acquisition owns the
		// selection trace. Cleanup keeps that trace and remains allowed after
		// cancellation or budget exhaustion, under the owner's bounded deadline.
		base := selectionbudget.CleanupContext(ctx)
		var releaseCtx context.Context
		var cancel context.CancelFunc
		if deadline, ok := cleanupCtx.Deadline(); ok {
			releaseCtx, cancel = context.WithDeadline(base, deadline)
		} else {
			releaseCtx, cancel = context.WithTimeout(base, 5*time.Second)
		}
		defer cancel()
		err := release(releaseCtx, request.ID, request.Token)
		if err != nil && onReleaseError != nil {
			onReleaseError(err)
		}
		return err
	}
	rollback := func() {
		cleanupCtx, cancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), 5*time.Second)
		defer cancel()
		_ = cleanup(cleanupCtx)
	}
	started := time.Now()
	proofCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	remaining, err := authority.CheckCapacityLease(proofCtx, request)
	cancel()
	if err != nil {
		rollback()
		return nil, errors.Join(ErrCapacityDependencyUnavailable, err)
	}
	identity := LeaseIdentity{Token: request.Token}
	if request.User {
		identity.UserID = request.ID
	} else {
		identity.AccountID = request.ID
	}
	lease, err := newOwnedLease(identity, started, remaining,
		func(ctx context.Context) (time.Duration, error) { return authority.CheckCapacityLease(ctx, request) },
		func(ctx context.Context) (time.Duration, error) { return authority.RefreshCapacityLease(ctx, request) }, cleanup, true)
	if err != nil {
		rollback()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = lease.Release()
		return nil, err
	}
	return acquiredCapacityResult(ctx, policy, lease)
}

// acquiredCapacityResult registers the exact successful receipt only when the
// operation root opted into a scope. Existing ReleaseFunc paths retain ownership.
func acquiredCapacityResult(ctx context.Context, policy CapacityPolicy, lease *Lease) (*AcquireResult, error) {
	released := &atomic.Bool{}
	result := &AcquireResult{Acquired: true, Unlimited: policy.Limit <= 0, Lease: lease, policy: policy, released: released}
	result.ReleaseFunc = func() {
		released.Store(true)
		if lease != nil {
			_ = lease.Release()
		} // all callers still join the original owner
	}
	if ctx != nil && ctx.Err() != nil {
		result.ReleaseFunc()
		return nil, context.Cause(ctx)
	}
	if scope := CapacityScopeFrom(ctx); scope != nil {
		scope.record(result)
	}
	return result, nil
}
