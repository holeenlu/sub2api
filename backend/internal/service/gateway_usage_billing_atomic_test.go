package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestApplyUsageBillingRequiresAtomicRepository(t *testing.T) {
	// The adapter no longer holds any direct money mutation ports.
	deps := &billingDeps{}
	for _, pending := range []bool{false, true} {
		p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 1, TotalCost: 1}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2, Quota: 10}, Account: &Account{ID: 3}, Platform: "openai", APIKeyService: &apiKeyQuotaUpdaterStub{}}
		if pending {
			p.Cost.PricingError = "missing"
		}
		applied, err := applyUsageBilling(context.Background(), "missing-repository", &UsageLog{}, p, deps, nil)
		require.False(t, applied)
		require.ErrorIs(t, err, ErrUsageBillingUnavailable)
	}
}

func TestApplyUsageBillingRejectsMissingUsageFacts(t *testing.T) {
	repo := &simpleModeUsageBillingRepoStub{}
	p := &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 1}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3}}
	applied, err := applyUsageBilling(context.Background(), "missing-evidence", nil, p, &billingDeps{}, repo)
	require.False(t, applied)
	require.ErrorIs(t, err, ErrUsageBillingRecordRequired)
	require.Empty(t, repo.cmds)
}

func TestApplyUsageBillingRejectsInvalidIntent(t *testing.T) {
	repo := &simpleModeUsageBillingRepoStub{}
	p := &postUsageBillingParams{Cost: &CostBreakdown{}, User: &User{ID: 1}, APIKey: &APIKey{ID: 2}, Account: &Account{ID: 3}}
	_, err := applyUsageBilling(context.Background(), "", &UsageLog{}, p, &billingDeps{}, repo)
	require.ErrorIs(t, err, ErrUsageBillingRequestIDRequired)
	require.Empty(t, repo.cmds)
	_, err = applyUsageBilling(context.Background(), "valid", &UsageLog{}, nil, &billingDeps{}, repo)
	require.ErrorIs(t, err, ErrUsageBillingUnavailable)
}
