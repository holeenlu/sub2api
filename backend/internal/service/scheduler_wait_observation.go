package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

// Diagnostics only: admission continues to use the existing slot semaphore.
// Observation failure must never reject a request or imply FIFO admission.
type ConcurrencyWaitObserver interface {
	// Start preserves a live token's first arrival and may only shorten its
	// deadline. An error means diagnostics are unavailable, not queue rejection.
	StartWaitObservation(context.Context, string, int64, string, time.Duration) error
	// End removes only this token and is idempotent, including after admission.
	EndWaitObservation(context.Context, string, int64, string) error
	// Observe measures older live registrations and atomically retires token.
	// An empty token observes an immediate admission without removing a waiter.
	ObserveWaitAdmission(context.Context, string, int64, string) (int64, int, error)
}
type schedulingWaitKey struct{}
type schedulingWait struct {
	scope, token string
	id           int64
	registered   bool
}

// BeginWaitObservation attaches diagnostics to one existing capacity-wait
// episode. It neither admits a waiter nor creates/resets a waiting budget. The
// caller must invoke end, which joins cancellation cleanup and is idempotent.
// Cancellation also retires the registration if the polling loop is slow to exit.
func (s *ConcurrencyService) BeginWaitObservation(ctx context.Context, scope string, id int64, timeout time.Duration) (context.Context, func()) {
	if s == nil || ctx.Err() != nil {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	wait := schedulingWait{scope: scope, id: id, token: generateRequestID()}
	cache, ok := s.cache.(ConcurrencyWaitObserver)
	started := time.Now()
	if ok {
		wait.registered = cache.StartWaitObservation(ctx, scope, id, wait.token, timeout) == nil
	}
	ctx = context.WithValue(ctx, schedulingWaitKey{}, wait)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "queue", Action: "wait_finished", Layer: scope, AccountID: id, DurationMs: time.Since(started).Milliseconds()})
			if ok {
				// A failed start response may still have written to Redis. Retire
				// that exact identity even when registration was unconfirmed.
				cleanup, cancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), time.Second)
				defer cancel()
				_ = cache.EndWaitObservation(cleanup, scope, id, wait.token)
			}
		})
	}
	stop := context.AfterFunc(ctx, finish)
	return ctx, func() {
		stop()
		finish() // sync.Once also joins an already-running cancellation callback.
	}
}

func (s *ConcurrencyService) observeWaitAdmission(ctx context.Context, scope string, id int64) {
	cache, ok := s.cache.(ConcurrencyWaitObserver)
	if !ok {
		return
	}
	token := ""
	if wait, ok := ctx.Value(schedulingWaitKey{}).(schedulingWait); ok && wait.scope == scope && wait.id == id {
		if !wait.registered {
			return // Missing evidence must not be reported as a known bypass.
		}
		token = wait.token
	}
	age, older, err := cache.ObserveWaitAdmission(ctx, scope, id, token)
	if err != nil {
		return
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "queue", Action: "slot_admitted", Layer: scope, AccountID: id, OldestWaitMs: age, BypassedWaiters: older})
}
