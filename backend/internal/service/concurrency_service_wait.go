package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// BeginCapacityWait uses the same Redis authority as ordinary slot acquisition.
// ctx carries the operation's already-established first wait deadline. A missing
// ticket port is a dependency error, never a fallback to non-FIFO polling.
func (s *ConcurrencyService) BeginCapacityWait(ctx context.Context, id int64, user bool, maxWaiting int) (*routing.WaitTicket, error) {
	if s == nil {
		return nil, routing.ErrCapacityDependencyUnavailable
	}
	store, ok := s.cache.(routing.CapacityWaitStore)
	if !ok {
		return nil, routing.ErrCapacityDependencyUnavailable
	}
	request := routing.CapacityRequest{ID: id, Token: generateRequestID(), User: user, Wait: true}
	return routing.EnqueueCapacityWait(ctx, store, request, maxWaiting, func(err error) {
		logger.LegacyPrintf("service.concurrency", "Warning: failed to close capacity wait for %d (user=%t): %v", id, user, err)
	})
}

// AcquireCapacityWait returns the ordinary lease/receipt; wait tickets do not
// replace post-acquisition eligibility, budget or SendPermit revalidation.
func (s *ConcurrencyService) AcquireCapacityWait(ctx context.Context, ticket *routing.WaitTicket, id int64, user bool, limit int) (*AcquireResult, error) {
	if ticket == nil {
		return nil, routing.ErrCapacityDependencyUnavailable
	}
	result, err := ticket.Acquire(ctx, limit, func(err error) {
		logger.LegacyPrintf("service.concurrency", "Warning: failed to release queued capacity for %d (user=%t): %v", id, user, err)
	})
	if err != nil || !result.Acquired {
		return result, err
	}
	scope := "account"
	if user {
		scope = "user"
	}
	s.observeWaitAdmission(ctx, scope, id)
	if ctx.Err() != nil {
		result.ReleaseFunc()
		return nil, context.Cause(ctx)
	}
	if user {
		openAIRequestTimingFromContext(ctx).mark("user_slot", time.Now())
	}
	return result, nil
}
