package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/selectionbudget"
)

// Diagnostics only: admission continues to use the existing slot semaphore.
// Observation failure must never reject a request or imply FIFO admission.
type ConcurrencyWaitObserver interface {
	StartWaitObservation(context.Context, string, int64, string, time.Duration) error
	EndWaitObservation(context.Context, string, int64, string) error
	ObserveWaitAdmission(context.Context, string, int64, string) (int64, int, error)
}
type schedulingWaitKey struct{}
type schedulingWait struct {
	scope, token string
	id           int64
}

func (s *ConcurrencyService) BeginWaitObservation(ctx context.Context, scope string, id int64, timeout time.Duration) (context.Context, func()) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < timeout {
		timeout = time.Until(deadline)
	}
	wait := schedulingWait{scope: scope, id: id, token: generateRequestID()}
	ctx = context.WithValue(ctx, schedulingWaitKey{}, wait)
	cache, ok := s.cache.(ConcurrencyWaitObserver)
	if ok {
		_ = cache.StartWaitObservation(ctx, scope, id, wait.token, timeout)
	}
	started := time.Now()
	return ctx, func() {
		RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "queue", Action: "wait_finished", Layer: scope, AccountID: id, DurationMs: time.Since(started).Milliseconds()})
		if ok {
			cleanup, cancel := context.WithTimeout(selectionbudget.CleanupContext(ctx), time.Second)
			defer cancel()
			_ = cache.EndWaitObservation(cleanup, scope, id, wait.token)
		}
	}
}

func (s *ConcurrencyService) observeWaitAdmission(ctx context.Context, scope string, id int64) {
	cache, ok := s.cache.(ConcurrencyWaitObserver)
	if !ok {
		return
	}
	token := ""
	if wait, ok := ctx.Value(schedulingWaitKey{}).(schedulingWait); ok && wait.scope == scope && wait.id == id {
		token = wait.token
	}
	age, older, err := cache.ObserveWaitAdmission(ctx, scope, id, token)
	if err != nil {
		return
	}
	RecordSchedulingDecision(ctx, SchedulingDecision{Stage: "queue", Action: "slot_admitted", Layer: scope, AccountID: id, OldestWaitMs: age, BypassedWaiters: older})
}
