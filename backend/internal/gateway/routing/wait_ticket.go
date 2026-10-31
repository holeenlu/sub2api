package routing

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

var (
	ErrCapacityWaitFull    = errors.New("capacity wait queue is full")
	ErrCapacityWaitExpired = errors.New("capacity wait ticket expired or canceled")
)

// CapacityWaitStore is implemented by the existing Redis capacity authority.
// Admission checks the queue head and capacity in ONE atomic script. End removes
// only request.Token, and can roll back a possibly acquired slot on failed entry.
// A terminal token cannot re-enqueue during its original remaining lifetime.
type CapacityWaitStore interface {
	CapacityStore
	CapacityLeaseStore
	EnqueueCapacityWait(context.Context, CapacityRequest, int, time.Duration) error
	AcquireQueuedCapacity(context.Context, CapacityRequest) (bool, error)
	EndCapacityWait(context.Context, CapacityRequest, bool) error
}

type waitBudgetKey struct{}

// WaitBudget holds only the first capacity-wait deadline of one operation. It
// neither queues requests nor owns execution/retry policy. User/account queues
// are independent; resource reselection and subsequent waits share this bound.
type WaitBudget struct {
	mu       sync.Mutex
	deadline time.Time
}

// WithWaitBudget explicitly binds a caller-owned budget; nil creates a fresh one.
// Only a new logical operation should replace an existing budget.
func WithWaitBudget(ctx context.Context, budget *WaitBudget) context.Context {
	if budget == nil {
		budget = &WaitBudget{}
	}
	return context.WithValue(ctx, waitBudgetKey{}, budget)
}

func WaitBudgetFrom(ctx context.Context) *WaitBudget {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(waitBudgetKey{}).(*WaitBudget)
	return b
}

// Deadline starts once, and can only tighten against a later wait/request bound.
// time.Time retains Go's monotonic clock; Redis receives only remaining duration.
func (b *WaitBudget) Deadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deadline.IsZero() || deadline.Before(b.deadline) {
		b.deadline = deadline
	}
	return b.deadline
}

func (b *WaitBudget) expired() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.deadline.IsZero() && !time.Now().Before(b.deadline)
}

// WaitTicket is one resource's FIFO position, never an authorization or lease.
// The caller must Close it. Cancellation also closes it, and Close joins that
// cleanup. A successful Acquire transfers capacity to the ordinary lease owner.
type WaitTicket struct {
	store   CapacityWaitStore
	request CapacityRequest
	ctx     context.Context
	end     func()

	// Serializes bounded acquisition with cleanup so cancellation cannot delete
	// a slot after it has transferred to the existing lease owner. Network calls
	// inside this lock are bounded by the original wait context, never Background.
	mu          sync.Mutex
	closed      bool
	transferred bool
}

// EnqueueCapacityWait registers exactly once. ctx must already have the shared
// wait deadline. The same token is used for queue position and acquired capacity.
func EnqueueCapacityWait(ctx context.Context, store CapacityWaitStore, request CapacityRequest, maxWaiting int, onCleanupError func(error)) (*WaitTicket, error) {
	deadline, bounded := ctx.Deadline()
	if store == nil || request.ID <= 0 || request.Token == "" || !bounded || maxWaiting < 0 {
		return nil, ErrCapacityDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !time.Now().Before(deadline) {
		return nil, ErrCapacityWaitExpired
	}
	request.Wait = true
	ticket := &WaitTicket{store: store, request: request, ctx: ctx}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			ticket.mu.Lock()
			defer ticket.mu.Unlock()
			ticket.closed = true
			cleanup, cancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), 5*time.Second)
			defer cancel()
			if err := store.EndCapacityWait(cleanup, request, !ticket.transferred); err != nil && onCleanupError != nil {
				onCleanupError(err)
			}
		})
	}
	if err := store.EnqueueCapacityWait(ctx, request, maxWaiting, time.Until(deadline)); err != nil {
		if errors.Is(err, ErrCapacityWaitFull) || errors.Is(err, ErrCapacityWaitExpired) {
			return nil, err // definite rejection did not create a new position
		}
		// A failed response may follow a committed enqueue. Keep a bounded
		// terminal marker so a delayed retransmission cannot resurrect it.
		finish()
		return nil, err
	}
	stop := context.AfterFunc(ctx, finish)
	ticket.end = func() { stop(); finish() }
	if err := ctx.Err(); err != nil {
		ticket.Close()
		return nil, err
	}
	return ticket, nil
}

// Acquire rechecks the original wait lifetime and obtains a normal capacity
// receipt only if Redis still grants this token the head position. The caller
// refreshes actual eligibility/limit before calling, and revalidates after grant.
func (t *WaitTicket) Acquire(ctx context.Context, limit int, onReleaseError func(error)) (*AcquireResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.transferred {
		return nil, ErrCapacityWaitExpired
	}
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	deadline, _ := t.ctx.Deadline()
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	stop := context.AfterFunc(t.ctx, cancel)
	defer stop()
	request := t.request
	request.Limit = limit
	result, err := AcquireCapacity(callCtx, t.store, request, onReleaseError)
	if err != nil {
		return nil, err
	}
	if result.Acquired {
		if callCtx.Err() != nil {
			result.ReleaseFunc()
			return nil, context.Cause(callCtx)
		}
		t.transferred = true
	}
	return result, nil
}

func (t *WaitTicket) Close() {
	if t != nil {
		t.end()
	}
}
