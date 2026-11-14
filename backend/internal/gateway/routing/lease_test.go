package routing

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLeaseMissingOwnerCancelsAndJoins(t *testing.T) {
	lost := errors.New("lease lost")
	var releases, aborts atomic.Int32
	l := &leaseLifetime{ttl: time.Second, interval: time.Millisecond, timeout: time.Second, confirmedUntil: time.Now().Add(time.Second), lost: lost,
		renew:   func(context.Context) (bool, error) { return false, nil },
		cleanup: func(ctx context.Context) error { require.NoError(t, ctx.Err()); releases.Add(1); return nil },
		onLost:  func(error) { aborts.Add(1) },
	}
	l.start(t.Context())
	select {
	case <-l.done:
	case <-time.After(time.Second):
		t.Fatal("lost lease did not stop")
	}
	require.ErrorIs(t, context.Cause(l.ctx), lost)
	require.NoError(t, l.release())
	require.NoError(t, l.release())
	require.EqualValues(t, 1, releases.Load())
	require.EqualValues(t, 1, aborts.Load())
}

func TestLeaseDependencyFailureHasFiniteGrace(t *testing.T) {
	lost := errors.New("lease lost")
	var attempts atomic.Int32
	l := &leaseLifetime{ttl: 40 * time.Millisecond, interval: 5 * time.Millisecond, timeout: time.Second, confirmedUntil: time.Now().Add(40 * time.Millisecond), lost: lost,
		renew: func(context.Context) (bool, error) { attempts.Add(1); return false, errors.New("redis unavailable") }, cleanup: func(context.Context) error { return nil },
	}
	l.start(t.Context())
	select {
	case <-l.done:
	case <-time.After(time.Second):
		t.Fatal("dependency failure extended grace indefinitely")
	}
	require.Greater(t, attempts.Load(), int32(0))
	require.ErrorIs(t, context.Cause(l.ctx), lost)
}

func TestLeaseReleaseCancelsAndJoinsRefreshBeforeCleanup(t *testing.T) {
	entered, allow := make(chan struct{}), make(chan struct{})
	var releaseCalls atomic.Int32
	l := &leaseLifetime{ttl: time.Second, interval: time.Millisecond, timeout: time.Second, confirmedUntil: time.Now().Add(time.Second), lost: errors.New("lost"),
		renew: func(ctx context.Context) (bool, error) {
			close(entered)
			<-ctx.Done()
			<-allow
			return false, ctx.Err()
		},
		cleanup: func(ctx context.Context) error { require.NoError(t, ctx.Err()); releaseCalls.Add(1); return nil },
	}
	l.start(t.Context())
	<-entered
	done := make(chan struct{})
	go func() { _ = l.release(); close(done) }()
	<-l.ctx.Done()
	select {
	case <-done:
		t.Fatal("release returned with refresh still active")
	default:
	}
	require.Zero(t, releaseCalls.Load())
	close(allow)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("release did not join")
	}
	require.EqualValues(t, 1, releaseCalls.Load())
}

func TestLeaseLateConfirmationCannotRestoreExpiredPermission(t *testing.T) {
	lost := errors.New("lease lost")
	l := &leaseLifetime{ttl: time.Second, interval: time.Millisecond, timeout: time.Second, confirmedUntil: time.Now().Add(25 * time.Millisecond), lost: lost,
		renew: func(ctx context.Context) (bool, error) { <-ctx.Done(); return true, nil }, cleanup: func(context.Context) error { return nil },
	}
	l.start(t.Context())
	select {
	case <-l.done:
	case <-time.After(time.Second):
		t.Fatal("late confirmation revived lease")
	}
	require.ErrorIs(t, context.Cause(l.ctx), lost)
}

func TestLeaseConfirmationExtendsFromCallStart(t *testing.T) {
	var attempts atomic.Int32
	confirmed := make(chan struct{})
	l := &leaseLifetime{ttl: 100 * time.Millisecond, interval: 10 * time.Millisecond, timeout: time.Second, confirmedUntil: time.Now().Add(20 * time.Millisecond), lost: errors.New("lost"),
		renew: func(context.Context) (bool, error) {
			if attempts.Add(1) == 3 {
				close(confirmed)
			}
			return true, nil
		}, cleanup: func(context.Context) error { return nil },
	}
	l.start(t.Context())
	defer l.release()
	select {
	case <-confirmed:
	case <-time.After(time.Second):
		t.Fatal("successful renewal did not keep lease alive")
	}
	require.NoError(t, l.ctx.Err())
}

func TestLeaseSuccessfulRetryAtFailureThresholdStillOwnsValidLease(t *testing.T) {
	var attempts atomic.Int32
	confirmed := make(chan struct{})
	l := &leaseLifetime{ttl: time.Second, interval: 10 * time.Millisecond, timeout: time.Second, failureGrace: 100 * time.Millisecond, confirmedUntil: time.Now().Add(time.Second), lost: errors.New("lost"),
		renew: func(context.Context) (bool, error) {
			if attempts.Add(1) == 1 {
				return false, errors.New("temporary redis error")
			}
			if attempts.Load() == 2 {
				time.Sleep(120 * time.Millisecond)
				close(confirmed)
			}
			return true, nil
		}, cleanup: func(context.Context) error { return nil },
	}
	l.start(t.Context())
	defer l.release()
	select {
	case <-confirmed:
	case <-time.After(time.Second):
		t.Fatal("valid lease rejected at failure threshold")
	}
	require.NoError(t, l.ctx.Err())
}

type ingressStore struct {
	acquire  func(context.Context) (bool, error)
	releases atomic.Int32
}

func (s *ingressStore) AcquireOpenAIWSIngressLease(ctx context.Context, _ int64, _ int, _ string) (bool, error) {
	return s.acquire(ctx)
}
func (s *ingressStore) RefreshOpenAIWSIngressLease(context.Context, int64, string) (bool, error) {
	return true, nil
}
func (s *ingressStore) ReleaseOpenAIWSIngressLease(ctx context.Context, _ int64, _ string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.releases.Add(1)
	return nil
}

func TestIngressLeaseCancellationDuringAcquireReclaimsToken(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &ingressStore{acquire: func(context.Context) (bool, error) { cancel(); return true, nil }}
	lease, acquired, err := AcquireIngressLease(ctx, store, 7, 1, "owner-token", nil)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, acquired)
	require.Nil(t, lease)
	require.EqualValues(t, 1, store.releases.Load())
}

func TestIngressLeaseParentCancellationReclaimsAndReleaseJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &ingressStore{acquire: func(context.Context) (bool, error) { return true, nil }}
	lease, acquired, err := AcquireIngressLease(ctx, store, 7, 1, "owner-token", nil)
	require.NoError(t, err)
	require.True(t, acquired)
	cancel()
	lease.Release()
	lease.Release()
	require.ErrorIs(t, lease.Context().Err(), context.Canceled)
	require.EqualValues(t, 1, store.releases.Load())
}
