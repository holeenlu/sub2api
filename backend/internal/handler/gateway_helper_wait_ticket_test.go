//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Existing polling fixtures model the port explicitly. Actual FIFO/atomicity is
// covered with the real production Lua below and in the repository Redis suite.
func (s *helperConcurrencyCacheStub) CheckCapacityLease(ctx context.Context, _ routing.CapacityRequest) (time.Duration, error) {
	return time.Minute, ctx.Err()
}
func (s *helperConcurrencyCacheStub) RefreshCapacityLease(ctx context.Context, r routing.CapacityRequest) (time.Duration, error) {
	return s.CheckCapacityLease(ctx, r)
}
func (s *helperConcurrencyCacheStub) EnqueueCapacityWait(ctx context.Context, _ routing.CapacityRequest, _ int, _ time.Duration) error {
	return ctx.Err()
}
func (s *helperConcurrencyCacheStub) AcquireQueuedCapacity(ctx context.Context, r routing.CapacityRequest) (bool, error) {
	if r.User {
		return s.AcquireUserSlot(ctx, r.ID, r.Limit, r.Token)
	}
	return s.AcquireAccountSlot(ctx, r.ID, r.Limit, r.Token)
}
func (s *helperConcurrencyCacheStub) EndCapacityWait(context.Context, routing.CapacityRequest, bool) error {
	return nil
}
func (s *concurrencyCacheMock) CheckCapacityLease(ctx context.Context, _ routing.CapacityRequest) (time.Duration, error) {
	return time.Minute, ctx.Err()
}
func (s *concurrencyCacheMock) RefreshCapacityLease(ctx context.Context, r routing.CapacityRequest) (time.Duration, error) {
	return s.CheckCapacityLease(ctx, r)
}
func (s *concurrencyCacheMock) EnqueueCapacityWait(ctx context.Context, _ routing.CapacityRequest, _ int, _ time.Duration) error {
	return ctx.Err()
}
func (s *concurrencyCacheMock) AcquireQueuedCapacity(ctx context.Context, r routing.CapacityRequest) (bool, error) {
	if r.User {
		return s.AcquireUserSlot(ctx, r.ID, r.Limit, r.Token)
	}
	return s.AcquireAccountSlot(ctx, r.ID, r.Limit, r.Token)
}
func (s *concurrencyCacheMock) EndCapacityWait(context.Context, routing.CapacityRequest, bool) error {
	return nil
}

type ticketHandlerCache struct {
	service.ConcurrencyCache
	service.APIKeyConcurrencyCache
	waits    routing.CapacityWaitStore
	enqueued chan routing.CapacityRequest
}

func (s *ticketHandlerCache) CheckCapacityLease(ctx context.Context, r routing.CapacityRequest) (time.Duration, error) {
	return s.waits.CheckCapacityLease(ctx, r)
}
func (s *ticketHandlerCache) RefreshCapacityLease(ctx context.Context, r routing.CapacityRequest) (time.Duration, error) {
	return s.waits.RefreshCapacityLease(ctx, r)
}
func (s *ticketHandlerCache) EnqueueCapacityWait(ctx context.Context, r routing.CapacityRequest, maxWaiting int, remaining time.Duration) error {
	err := s.waits.EnqueueCapacityWait(ctx, r, maxWaiting, remaining)
	if err == nil {
		s.enqueued <- r
	}
	return err
}
func (s *ticketHandlerCache) AcquireQueuedCapacity(ctx context.Context, r routing.CapacityRequest) (bool, error) {
	return s.waits.AcquireQueuedCapacity(ctx, r)
}
func (s *ticketHandlerCache) EndCapacityWait(ctx context.Context, r routing.CapacityRequest, rollback bool) error {
	return s.waits.EndCapacityWait(ctx, r, rollback)
}

func newTicketHandlerCache(t *testing.T, address string) *ticketHandlerCache {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	cache := repository.NewConcurrencyCache(client, 15, 900)
	return &ticketHandlerCache{ConcurrencyCache: cache, APIKeyConcurrencyCache: cache.(service.APIKeyConcurrencyCache), waits: cache.(routing.CapacityWaitStore), enqueued: make(chan routing.CapacityRequest, 8)}
}

func awaitTicketEnrollment(t *testing.T, c *ticketHandlerCache) routing.CapacityRequest {
	t.Helper()
	select {
	case request := <-c.enqueued:
		return request
	case <-time.After(3 * time.Second):
		t.Fatal("ticket was not registered")
		return routing.CapacityRequest{}
	}
}

func TestGatewayWaitTicketFIFOAcrossClientsAndFastPath(t *testing.T) {
	server := miniredis.RunT(t)
	firstCache, secondCache := newTicketHandlerCache(t, server.Addr()), newTicketHandlerCache(t, server.Addr())
	firstService, secondService := service.NewConcurrencyService(firstCache), service.NewConcurrencyService(secondCache)
	first := NewConcurrencyHelper(firstService, SSEPingFormatNone, time.Second)
	second := NewConcurrencyHelper(secondService, SSEPingFormatNone, time.Second)
	holder, err := firstService.AcquireAccountSlot(t.Context(), 71, 1)
	require.NoError(t, err)
	require.True(t, holder.Acquired)
	defer holder.ReleaseFunc()
	type outcome struct {
		release func()
		err     error
	}
	run := func(helper *ConcurrencyHelper) <-chan outcome {
		result := make(chan outcome, 1)
		c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
		c.Request = c.Request.WithContext(t.Context())
		go func() {
			release, err := helper.AcquireAccountSlotWithWaitTimeout(c, 71, 1, 3*time.Second, false, nil)
			result <- outcome{release, err}
		}()
		return result
	}
	firstResult := run(first)
	awaitTicketEnrollment(t, firstCache)
	secondResult := run(second)
	awaitTicketEnrollment(t, secondCache)
	holder.ReleaseFunc()
	fast, err := firstService.AcquireAccountSlot(t.Context(), 71, 1)
	require.NoError(t, err)
	require.False(t, fast.Acquired)
	var one outcome
	select {
	case one = <-firstResult:
	case <-time.After(4 * time.Second):
		t.Fatal("head did not acquire")
	}
	require.NoError(t, one.err)
	require.NotNil(t, one.release)
	defer one.release()
	select {
	case two := <-secondResult:
		if two.release != nil {
			two.release()
		}
		t.Fatal("tail passed the head's capacity")
	default:
	}
	one.release()
	select {
	case two := <-secondResult:
		require.NoError(t, two.err)
		require.NotNil(t, two.release)
		two.release()
	case <-time.After(4 * time.Second):
		t.Fatal("tail did not acquire after release")
	}
	count, err := firstCache.GetAccountConcurrency(t.Context(), 71)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestGatewayWaitTicketAccountReselectionDoesNotResetDeadline(t *testing.T) {
	server := miniredis.RunT(t)
	cache := newTicketHandlerCache(t, server.Addr())
	svc := service.NewConcurrencyService(cache)
	helper := NewConcurrencyHelper(svc, SSEPingFormatNone, time.Second)
	holder, err := svc.AcquireAccountSlot(t.Context(), 71, 1)
	require.NoError(t, err)
	require.True(t, holder.Acquired)
	defer holder.ReleaseFunc()
	c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
	c.Request = c.Request.WithContext(t.Context())
	release, err := helper.AcquireAccountSlotWithWaitTimeout(c, 71, 1, 10*time.Millisecond, false, nil)
	require.Nil(t, release)
	var timeout *ConcurrencyError
	require.ErrorAs(t, err, &timeout)
	require.True(t, timeout.IsTimeout)
	budget := routing.WaitBudgetFrom(c.Request.Context())
	release, err = helper.AcquireAccountSlotWithWaitTimeout(c, 72, 1, time.Hour, false, nil)
	require.Nil(t, release)
	require.ErrorAs(t, err, &timeout)
	require.Same(t, budget, routing.WaitBudgetFrom(c.Request.Context()))
	count, err := cache.GetAccountConcurrency(t.Context(), 72)
	require.NoError(t, err)
	require.Zero(t, count, "even a free reselected account cannot reset the first deadline")
}

func TestGatewayWaitTicketQueueFullIsLocalAndBounded(t *testing.T) {
	for _, user := range []bool{false, true} {
		t.Run(fmt.Sprintf("user=%t", user), func(t *testing.T) {
			server := miniredis.RunT(t)
			cache := newTicketHandlerCache(t, server.Addr())
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second)
			limit := service.CalculateMaxWait(1)
			if user {
				limit--
			}
			for i := range limit {
				r := routing.CapacityRequest{ID: 71, Token: fmt.Sprintf("existing-%d", i), User: user, Wait: true}
				require.NoError(t, cache.waits.EnqueueCapacityWait(t.Context(), r, limit, time.Minute))
			}
			c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
			c.Request = c.Request.WithContext(t.Context())
			var release func()
			var err error
			if user {
				release, err = helper.AcquireUserSlotWithWait(c, 71, 1, false, nil)
			} else {
				release, err = helper.AcquireAccountSlotWithWaitTimeout(c, 71, 1, time.Second, false, nil)
			}
			require.Nil(t, release)
			var full *WaitQueueFullError
			require.ErrorAs(t, err, &full)
			status, _, code, _ := concurrencyErrorResponse(err, "account")
			require.Equal(t, http.StatusTooManyRequests, status)
			require.Equal(t, gatewayQueueFullCode, code)
		})
	}
}
