package routing

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestUnlimitedEvidenceRequiresRealAcquisitionAndCurrentPolicy(t *testing.T) {
	result, err := AcquireCapacity(t.Context(), nil, CapacityRequest{ID: 7, Limit: 0, User: true}, nil)
	require.NoError(t, err)
	proof, err := result.Verify(t.Context(), CapacityPolicy{ResourceID: 7, Limit: 0, User: true})
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, proof.Fingerprint())
	require.True(t, proof.ValidUntil().IsZero())
	_, err = result.Verify(t.Context(), CapacityPolicy{ResourceID: 7, Limit: 1, User: true})
	require.ErrorIs(t, err, ErrCapacityPolicyChanged)
	unverified := &AcquireResult{Acquired: true, Unlimited: true}
	_, err = unverified.Verify(t.Context(), CapacityPolicy{ResourceID: 7, Limit: 0, User: true})
	require.ErrorIs(t, err, ErrCapacityDependencyUnavailable)
	_, err = AcquireCapacity(t.Context(), nil, CapacityRequest{Limit: 0}, nil)
	require.ErrorIs(t, err, ErrCapacityDependencyUnavailable)
}

func TestCapacityEvidenceFingerprintUsesStableValidatedTokenAndPolicy(t *testing.T) {
	store := &capacityStore{acquire: func(context.Context, int64, int, string) (bool, error) { return true, nil }, release: func(context.Context, int64, string) error { return nil }}
	result, err := AcquireCapacity(t.Context(), store, CapacityRequest{ID: 7, Limit: 1, Token: "real-owned-token"}, nil)
	require.NoError(t, err)
	defer result.ReleaseFunc()
	first, err := result.Verify(t.Context(), result.Policy())
	require.NoError(t, err)
	require.True(t, first.ValidUntil().After(time.Now()))
	require.NoError(t, result.Lease.confirm(t.Context(), true))
	renewed, err := result.Verify(t.Context(), result.Policy())
	require.NoError(t, err)
	require.Equal(t, first.Fingerprint(), renewed.Fingerprint())
	require.False(t, renewed.ValidUntil().Before(first.ValidUntil()))
	result.ReleaseFunc()
	_, err = result.Verify(t.Context(), result.Policy())
	require.Error(t, err)
}
