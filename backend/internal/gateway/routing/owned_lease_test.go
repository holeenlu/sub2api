package routing

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOwnedLeaseMissingProofCancelsAndCannotDispatch(t *testing.T) {
	var release atomic.Int32
	l, err := newOwnedLease(LeaseIdentity{AccountID: 7, Token: "old"}, time.Now(), time.Second, func(context.Context) (time.Duration, error) { return 0, nil }, func(context.Context) (time.Duration, error) { return time.Second, nil }, func(context.Context) error { release.Add(1); return nil }, true)
	require.NoError(t, err)
	require.ErrorIs(t, l.Validate(t.Context()), ErrCapacityLeaseLost)
	require.ErrorIs(t, context.Cause(l.Context()), ErrCapacityLeaseLost)
	require.NoError(t, l.Release())
	require.NoError(t, l.Release())
	require.EqualValues(t, 1, release.Load())
}

func TestOwnedLeaseDependencyErrorIsNotUnrestrictedOrRenewalProof(t *testing.T) {
	l, err := newOwnedLease(LeaseIdentity{AccountID: 7, Token: "owned"}, time.Now(), 50*time.Millisecond, func(context.Context) (time.Duration, error) { return 0, errors.New("redis down") }, func(context.Context) (time.Duration, error) { return 0, errors.New("redis down") }, func(context.Context) error { return nil }, true)
	require.NoError(t, err)
	defer l.Release()
	require.ErrorIs(t, l.Validate(t.Context()), ErrCapacityDependencyUnavailable)
	select {
	case <-l.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("unconfirmed lease remained valid beyond expiry")
	}
	require.ErrorIs(t, context.Cause(l.Context()), ErrCapacityLeaseLost)
}

func TestOwnedLeaseRenewalKeepsLongOperationAndBindingCancels(t *testing.T) {
	var renewals, releases atomic.Int32
	confirm := func(context.Context) (time.Duration, error) { renewals.Add(1); return 90 * time.Millisecond, nil }
	l, err := newOwnedLease(LeaseIdentity{UserID: 8, Token: "owner"}, time.Now(), 90*time.Millisecond, confirm, confirm, func(context.Context) error { releases.Add(1); return nil }, true)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	bound, detach := l.Bind(ctx)
	defer detach()
	require.Eventually(t, func() bool { return renewals.Load() >= 4 }, time.Second, 5*time.Millisecond)
	require.NoError(t, l.Validate(t.Context()))
	require.NoError(t, bound.Err())
	cancel()
	require.NoError(t, l.Release())
	require.ErrorIs(t, bound.Err(), context.Canceled)
	require.EqualValues(t, 1, releases.Load())
}

func TestOwnedLeaseReleaseJoinsInFlightValidation(t *testing.T) {
	entered, exit := make(chan struct{}), make(chan struct{})
	var releases atomic.Int32
	l, err := newOwnedLease(LeaseIdentity{AccountID: 7, Token: "owner"}, time.Now(), time.Minute, func(ctx context.Context) (time.Duration, error) {
		close(entered)
		<-ctx.Done()
		<-exit
		return 0, ctx.Err()
	}, func(context.Context) (time.Duration, error) { return time.Minute, nil }, func(context.Context) error { releases.Add(1); return nil }, true)
	require.NoError(t, err)
	checked := make(chan error, 1)
	go func() { checked <- l.Validate(t.Context()) }()
	<-entered
	joined := make(chan struct{})
	go func() { _ = l.Release(); close(joined) }()
	<-l.Context().Done()
	select {
	case <-joined:
		t.Fatal("release did not join validation")
	default:
	}
	require.Zero(t, releases.Load())
	close(exit)
	require.Error(t, <-checked)
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("release did not complete")
	}
	require.EqualValues(t, 1, releases.Load())
}

func TestOwnedLiveHandoffStopsOldLocalOwnerAndRetainsToken(t *testing.T) {
	var releases atomic.Int32
	proof := func(context.Context) (time.Duration, error) { return time.Minute, nil }
	l, err := newOwnedLease(LeaseIdentity{AccountID: 7, UserID: 8, APIKeyID: 9, Token: "call"}, time.Now(), time.Minute, proof, proof, func(context.Context) error { releases.Add(1); return nil }, false)
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(l.Handoff)
	}
	wg.Wait()
	require.ErrorIs(t, l.Validate(t.Context()), context.Canceled)
	require.Zero(t, releases.Load())
	require.NoError(t, l.Release())
	require.EqualValues(t, 1, releases.Load())
}

type unsupportedCapacityStore struct{ CapacityStore }

func TestCapacityUnlimitedIsDifferentFromMissingAuthority(t *testing.T) {
	unlimited, err := AcquireCapacity(t.Context(), nil, CapacityRequest{ID: 7, Limit: 0}, nil)
	require.NoError(t, err)
	require.True(t, unlimited.Unlimited)
	require.Nil(t, unlimited.Lease)
	limited, err := AcquireCapacity(t.Context(), unsupportedCapacityStore{}, CapacityRequest{ID: 7, Limit: 1, Token: "token"}, nil)
	require.Nil(t, limited)
	require.ErrorIs(t, err, ErrCapacityDependencyUnavailable)
}
