package execution

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDispatcherPreparationPurposeRequiresRootApproval(t *testing.T) {
	d, identity, target := dispatchFixture(t, context.Background(), nil)
	identity.Transport = "ws"
	identity.Purpose = model.DispatchInternalPreparation
	_, err := d.Admit(t.Context(), identity, target)
	require.ErrorIs(t, err, ErrInvalidPreparedIdentity)
	require.Zero(t, d.budget.Snapshot().Attempts)
	require.Zero(t, d.budget.Snapshot().PreparationRetries)
}
func TestDispatcherPreparationSharesExistingBudgetAndRemainsOneShot(t *testing.T) {
	approved := model.DispatchInternalPreparation
	authority := dispatchAuthorityFunc(func(context.Context, model.PreparedIdentity) (DispatchValidation, error) {
		return DispatchValidation{Purpose: approved, Authorization: [32]byte{1}, Leases: [32]byte{2}}, nil
	})
	d, identity, target := dispatchFixture(t, t.Context(), authority)
	identity.Transport = "ws"
	identity.Purpose = model.DispatchInternalPreparation
	for range 2 {
		permit, err := d.Admit(t.Context(), identity, target)
		require.NoError(t, err)
		require.NoError(t, permit.Consume(t.Context(), identity))
		copied := *permit
		require.ErrorIs(t, copied.Consume(t.Context(), identity), ErrSendPermitConsumed)
	}
	_, err := d.Admit(t.Context(), identity, target)
	require.ErrorIs(t, err, ErrPreparationBudgetExhausted)
	require.Zero(t, d.budget.Snapshot().Attempts)
	require.Equal(t, 2, d.budget.Snapshot().PreparationRetries)
	identity.Purpose = model.DispatchGeneration
	approved = model.DispatchGeneration
	permit, err := d.Admit(t.Context(), identity, target)
	require.NoError(t, err)
	require.NoError(t, permit.Consume(t.Context(), identity))
	require.Equal(t, 1, d.budget.Snapshot().Attempts)
	approved = model.DispatchInternalPreparation
	require.ErrorIs(t, permit.RevalidateConsumed(t.Context(), identity), ErrDispatchChanged)
}
