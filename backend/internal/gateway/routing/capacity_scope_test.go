package routing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var _ interface {
	Lookup(bool, int64) *AcquireResult
} = (*CapacityScope)(nil)

func scopeCapacityStore() *capacityStore {
	return &capacityStore{acquire: func(context.Context, int64, int, string) (bool, error) { return true, nil }, release: func(context.Context, int64, string) error { return nil }}
}

func TestCapacityScopeRequiresExplicitOperationInitialization(t *testing.T) {
	parent := t.Context()
	result, err := AcquireCapacity(parent, scopeCapacityStore(), CapacityRequest{ID: 7, Limit: 1, Token: "before-scope"}, nil)
	require.NoError(t, err)
	defer result.ReleaseFunc()
	require.Nil(t, CapacityScopeFrom(parent))
	require.Nil(t, CapacityScopeFrom(nil))
	ctx, scope := WithCapacityScope(parent)
	require.Same(t, scope, CapacityScopeFrom(ctx))
	require.Nil(t, scope.Lookup(false, 7))
	nilCtx, nilScope := WithCapacityScope(nil)
	require.Nil(t, nilCtx)
	require.Nil(t, nilScope)
	var missing *CapacityScope
	require.Nil(t, missing.Lookup(true, 7))
}

func TestCapacityScopeIndexesOriginalUserAndAccountReceiptsSeparately(t *testing.T) {
	ctx, scope := WithCapacityScope(t.Context())
	store := scopeCapacityStore()
	u, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, User: true, Limit: 1, Token: "user"}, nil)
	require.NoError(t, err)
	defer u.ReleaseFunc()
	a, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, Limit: 1, Token: "account"}, nil)
	require.NoError(t, err)
	defer a.ReleaseFunc()
	require.Same(t, u, scope.Lookup(true, 7))
	require.Same(t, a, scope.Lookup(false, 7))
	require.Nil(t, scope.Lookup(false, 8))
	require.Nil(t, scope.Lookup(true, 0))
	_, newOperation := WithCapacityScope(ctx)
	require.Nil(t, newOperation.Lookup(true, 7))
	require.Nil(t, newOperation.Lookup(false, 7))
	require.Same(t, a, scope.Lookup(false, 7))
}

func TestCapacityScopeWaitContextDoesNotOwnLeaseLifetime(t *testing.T) {
	operation, scope := WithCapacityScope(t.Context())
	wait, cancelWait := context.WithCancel(operation)
	result, err := AcquireCapacity(wait, scopeCapacityStore(), CapacityRequest{ID: 7, Limit: 1, Token: "owned"}, nil)
	require.NoError(t, err)
	defer result.ReleaseFunc()
	cancelWait()
	require.NoError(t, operation.Err())
	require.Same(t, result, scope.Lookup(false, 7))
	require.NoError(t, result.Lease.Validate(operation))
}

func TestCapacityScopeOnlySelectedBindCancelsSendingContext(t *testing.T) {
	operation, scope := WithCapacityScope(t.Context())
	store := scopeCapacityStore()
	candidate, err := AcquireCapacity(operation, store, CapacityRequest{ID: 7, Limit: 1, Token: "candidate"}, nil)
	require.NoError(t, err)
	defer candidate.ReleaseFunc()
	selected, err := AcquireCapacity(operation, store, CapacityRequest{ID: 8, Limit: 1, Token: "selected"}, nil)
	require.NoError(t, err)
	defer selected.ReleaseFunc()
	sending, detach := selected.Lease.Bind(operation)
	defer detach()
	candidate.Lease.cancel(ErrCapacityLeaseLost)
	candidate.ReleaseFunc()
	require.NoError(t, operation.Err())
	require.NoError(t, sending.Err())
	require.Same(t, selected, scope.Lookup(false, 8))
	selected.Lease.cancel(ErrCapacityLeaseLost)
	select {
	case <-sending.Done():
	case <-time.After(time.Second):
		t.Fatal("selected lease loss did not cancel sending")
	}
	require.ErrorIs(t, context.Cause(sending), ErrCapacityLeaseLost)
}

func TestCapacityScopeOldReleaseCannotEraseReplacement(t *testing.T) {
	ctx, scope := WithCapacityScope(t.Context())
	store := scopeCapacityStore()
	old, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, Limit: 2, Token: "old"}, nil)
	require.NoError(t, err)
	fresh, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, Limit: 2, Token: "fresh"}, nil)
	require.NoError(t, err)
	defer fresh.ReleaseFunc()
	old.ReleaseFunc()
	require.Same(t, fresh, scope.Lookup(false, 7))
	_, err = fresh.Verify(ctx, CapacityPolicy{ResourceID: 7, Limit: 2})
	require.NoError(t, err)
	_, err = old.Verify(ctx, CapacityPolicy{ResourceID: 7, Limit: 2})
	require.Error(t, err)
	require.NoError(t, ctx.Err())
}

func TestCapacityScopeReleasedUnlimitedReceiptAndCopiesCannotVerify(t *testing.T) {
	ctx, scope := WithCapacityScope(t.Context())
	result, err := AcquireCapacity(ctx, nil, CapacityRequest{ID: 7, User: true, Limit: 0}, nil)
	require.NoError(t, err)
	policy := CapacityPolicy{ResourceID: 7, User: true, Limit: 0}
	proof, err := result.Verify(ctx, policy)
	require.NoError(t, err)
	require.True(t, proof.ValidUntil().IsZero())
	copy := *result
	result.ReleaseFunc()
	require.Same(t, result, scope.Lookup(true, 7))
	_, err = scope.Lookup(true, 7).Verify(ctx, policy)
	require.Error(t, err)
	_, err = copy.Verify(ctx, policy)
	require.Error(t, err)
	require.NoError(t, ctx.Err())
}

func TestCapacityScopeFailureDoesNotRecordOrReplaceSuccess(t *testing.T) {
	ctx, scope := WithCapacityScope(t.Context())
	store := scopeCapacityStore()
	result, err := AcquireCapacity(ctx, store, CapacityRequest{ID: 7, Limit: 1, Token: "success"}, nil)
	require.NoError(t, err)
	defer result.ReleaseFunc()
	rejected := scopeCapacityStore()
	rejected.acquire = func(context.Context, int64, int, string) (bool, error) { return false, nil }
	failed, err := AcquireCapacity(ctx, rejected, CapacityRequest{ID: 7, Limit: 1, Token: "rejected"}, nil)
	require.NoError(t, err)
	require.False(t, failed.Acquired)
	require.Same(t, result, scope.Lookup(false, 7))
	failed, err = AcquireCapacity(ctx, unsupportedCapacityStore{}, CapacityRequest{ID: 8, Limit: 1, Token: "unknown"}, nil)
	require.ErrorIs(t, err, ErrCapacityDependencyUnavailable)
	require.Nil(t, failed)
	require.Nil(t, scope.Lookup(false, 8))
}

func TestCapacityScopeConcurrentRegistrationAndLookup(t *testing.T) {
	ctx, scope := WithCapacityScope(t.Context())
	store := scopeCapacityStore()
	var wg sync.WaitGroup
	for i := int64(1); i <= 16; i++ {
		wg.Go(func() {
			result, err := AcquireCapacity(ctx, store, CapacityRequest{ID: i, User: i%2 == 0, Limit: 1, Token: "test"}, nil)
			if err != nil {
				t.Error(err)
				return
			}
			if scope.Lookup(i%2 == 0, i) != result {
				t.Error("acquired receipt missing")
			}
		})
	}
	wg.Wait()
	for i := int64(1); i <= 16; i++ {
		result := scope.Lookup(i%2 == 0, i)
		require.NotNil(t, result)
		result.ReleaseFunc()
	}
}
