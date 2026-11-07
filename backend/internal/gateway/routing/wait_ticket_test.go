//go:build unit

package routing

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitTicketBudgetSurvivesAccountReselectionAndTightens(t *testing.T) {
	ctx, _ := WithCapacityScope(t.Context())
	budget := WaitBudgetFrom(ctx)
	require.NotNil(t, budget)
	first := budget.Deadline(ctx, time.Minute)
	require.Equal(t, first, budget.Deadline(ctx, time.Hour))
	child, cancel := context.WithDeadline(ctx, first.Add(-time.Second))
	defer cancel()
	shorter := budget.Deadline(child, time.Hour)
	require.Equal(t, first.Add(-time.Second), shorter)
	require.Equal(t, shorter, budget.Deadline(ctx, time.Hour))
	// Expiration is shared by a later account's fast path, not restarted there.
	budget.Deadline(ctx, -time.Second)
	result, err := AcquireCapacity(ctx, nil, CapacityRequest{ID: 2, Token: "reselected", Limit: 1}, nil)
	require.ErrorIs(t, err, ErrCapacityWaitExpired)
	require.Nil(t, result)
	newTurn, _ := WithCapacityScope(ctx)
	require.NotSame(t, budget, WaitBudgetFrom(newTurn))
	require.False(t, WaitBudgetFrom(newTurn).expired())
}

func TestWaitTicketMissingAuthorityFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ticket, err := EnqueueCapacityWait(ctx, nil, CapacityRequest{ID: 1, Token: "token"}, 20, nil)
	require.ErrorIs(t, err, ErrCapacityDependencyUnavailable)
	require.Nil(t, ticket)
}
