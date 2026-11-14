//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Fault injection covers transport cancellation and an ambiguous Start reply;
// repository tests separately exercise the actual Lua with independent clients.
type waitObservationCacheForTest struct {
	stubConcurrencyCacheForTest
	start    func(context.Context, string, int64, string, time.Duration) error
	end      func(context.Context, string, int64, string) error
	admit    func(context.Context, string, int64, string) (int64, int, error)
	releases atomic.Int32
}

func (c *waitObservationCacheForTest) StartWaitObservation(ctx context.Context, scope string, id int64, token string, timeout time.Duration) error {
	if c.start != nil {
		return c.start(ctx, scope, id, token, timeout)
	}
	return nil
}

func (c *waitObservationCacheForTest) EndWaitObservation(ctx context.Context, scope string, id int64, token string) error {
	if c.end != nil {
		return c.end(ctx, scope, id, token)
	}
	return nil
}

func (c *waitObservationCacheForTest) ObserveWaitAdmission(ctx context.Context, scope string, id int64, token string) (int64, int, error) {
	if c.admit != nil {
		return c.admit(ctx, scope, id, token)
	}
	return 0, 0, nil
}

func (c *waitObservationCacheForTest) ReleaseAccountSlot(context.Context, int64, string) error {
	c.releases.Add(1)
	return nil
}

func (c *waitObservationCacheForTest) ReleaseUserSlot(context.Context, int64, string) error {
	c.releases.Add(1)
	return nil
}

func TestConcurrencyWaitAdmissionCancellationReleasesGrantedCapacity(t *testing.T) {
	for _, scope := range []string{"account", "user"} {
		t.Run(scope, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			cause := errors.New("request canceled during wait observation")
			cache := &waitObservationCacheForTest{stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
			cache.admit = func(context.Context, string, int64, string) (int64, int, error) {
				cancel(cause)
				return 0, 0, context.Canceled
			}
			svc := NewConcurrencyService(cache)
			acquire := svc.AcquireAccountSlot
			if scope == "user" {
				acquire = svc.AcquireUserSlot
			}
			result, err := acquire(ctx, 17, 1)
			require.ErrorIs(t, err, cause)
			require.Nil(t, result)
			require.Equal(t, int32(1), cache.releases.Load())
		})
	}
}

func TestConcurrencyWaitObservationFailureDoesNotRejectCapacity(t *testing.T) {
	cache := &waitObservationCacheForTest{stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	cache.admit = func(context.Context, string, int64, string) (int64, int, error) {
		return 0, 0, errors.New("diagnostic Redis call failed")
	}
	svc := NewConcurrencyService(cache)
	for _, acquire := range []func(context.Context, int64, int) (*AcquireResult, error){svc.AcquireAccountSlot, svc.AcquireUserSlot} {
		result, err := acquire(t.Context(), 17, 1)
		require.NoError(t, err)
		require.True(t, result.Acquired)
		result.ReleaseFunc()
	}
	require.Equal(t, int32(2), cache.releases.Load())
}

func TestConcurrencyWaitUnconfirmedStartCleansUpWithoutFabricatingBypass(t *testing.T) {
	var admissions, cleanups atomic.Int32
	var startedToken string
	cache := &waitObservationCacheForTest{stubConcurrencyCacheForTest: stubConcurrencyCacheForTest{acquireResult: true}}
	cache.start = func(_ context.Context, _ string, _ int64, token string, _ time.Duration) error {
		startedToken = token
		return errors.New("start reply lost after write")
	}
	cache.admit = func(context.Context, string, int64, string) (int64, int, error) {
		admissions.Add(1)
		return 100, 9, nil
	}
	cache.end = func(ctx context.Context, scope string, id int64, token string) error {
		cleanups.Add(1)
		require.NoError(t, ctx.Err())
		require.Equal(t, "account", scope)
		require.Equal(t, int64(17), id)
		require.Equal(t, startedToken, token)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), time.Second)
		return nil
	}
	svc := NewConcurrencyService(cache)
	ctx := WithSchedulingTrace(t.Context(), nil)
	ctx, end := svc.BeginWaitObservation(ctx, "account", 17, time.Minute)
	defer end()
	result, err := svc.AcquireAccountSlot(ctx, 17, 1)
	require.NoError(t, err)
	require.True(t, result.Acquired)
	result.ReleaseFunc()
	end()
	end()
	require.Zero(t, admissions.Load())
	require.Equal(t, int32(1), cleanups.Load())
	trace := schedulingTraceFromContext(ctx)
	trace.mu.Lock()
	defer trace.mu.Unlock()
	require.Len(t, trace.events, 1)
	require.Equal(t, "wait_finished", trace.events[0].Action)
}

func TestConcurrencyWaitCancellationCleanupIsJoinedAndIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cleanupStarted := make(chan struct{})
	allowCleanup := make(chan struct{})
	var allowOnce sync.Once
	allow := func() { allowOnce.Do(func() { close(allowCleanup) }) }
	defer allow()
	var cleanups atomic.Int32
	var cleanupErr error
	cache := &waitObservationCacheForTest{}
	cache.end = func(ctx context.Context, _ string, _ int64, _ string) error {
		cleanups.Add(1)
		cleanupErr = ctx.Err()
		close(cleanupStarted)
		select {
		case <-allowCleanup:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	svc := NewConcurrencyService(cache)
	_, end := svc.BeginWaitObservation(ctx, "user", 17, time.Minute)
	cancel()
	select {
	case <-cleanupStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not start cleanup")
	}
	const callers = 16
	done := make(chan struct{}, callers)
	for range callers {
		go func() { end(); done <- struct{}{} }()
	}
	select {
	case <-done:
		t.Fatal("end returned before cancellation cleanup finished")
	default:
	}
	allow()
	for range callers {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("end did not join cleanup")
		}
	}
	require.NoError(t, cleanupErr)
	require.Equal(t, int32(1), cleanups.Load())
}

func TestConcurrencyWaitCanceledStartStillRetiresItsExactToken(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var startedToken, endedToken string
	var cleanups atomic.Int32
	cache := &waitObservationCacheForTest{}
	cache.start = func(_ context.Context, _ string, _ int64, token string, _ time.Duration) error {
		startedToken = token
		cancel() // the Redis write may already have happened
		return context.Canceled
	}
	cache.end = func(_ context.Context, _ string, _ int64, token string) error {
		endedToken = token
		cleanups.Add(1)
		return nil
	}
	svc := NewConcurrencyService(cache)
	_, end := svc.BeginWaitObservation(ctx, "account", 17, time.Minute)
	end()
	require.NotEmpty(t, startedToken)
	require.Equal(t, startedToken, endedToken)
	require.Equal(t, int32(1), cleanups.Load())
}

func TestConcurrencyWaitObservationDoesNotResetParentDeadline(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	var registeredTimeout time.Duration
	cache := &waitObservationCacheForTest{}
	cache.start = func(_ context.Context, _ string, _ int64, _ string, timeout time.Duration) error {
		registeredTimeout = timeout
		return nil
	}
	svc := NewConcurrencyService(cache)
	waitCtx, end := svc.BeginWaitObservation(ctx, "account", 17, time.Hour)
	defer end()
	got, ok := waitCtx.Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, got)
	require.Positive(t, registeredTimeout)
	require.LessOrEqual(t, registeredTimeout, time.Minute)
}
