package service

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildUsageBillingCommandSimpleModeOnlyChargesAPIKeyWindows(t *testing.T) {
	apiKey := &APIKey{ID: 13, Quota: 100, RateLimit5h: 10}
	user := &User{ID: 7}
	account := &Account{ID: 9, Type: AccountTypeAPIKey}
	p := &postUsageBillingParams{
		Cost:                       &CostBreakdown{ActualCost: 3.25, TotalCost: 2.5},
		User:                       user,
		APIKey:                     apiKey,
		Account:                    account,
		APIKeyService:              &apiKeyQuotaUpdaterStub{},
		SimpleModeKeyRateLimitOnly: true,
	}

	cmd := buildUsageBillingCommand("simple-req", nil, p)
	require.NotNil(t, cmd)
	require.Zero(t, cmd.BalanceCost)
	require.Zero(t, cmd.SubscriptionCost)
	require.Zero(t, cmd.APIKeyQuotaCost)
	require.Zero(t, cmd.AccountQuotaCost)
	require.Equal(t, 3.25, cmd.APIKeyRateLimitCost)
}

type apiKeyQuotaUpdaterStub struct{}

func (apiKeyQuotaUpdaterStub) UpdateQuotaUsed(context.Context, int64, float64) error { return nil }
func (apiKeyQuotaUpdaterStub) UpdateRateLimitUsage(context.Context, int64, float64) error {
	return nil
}

type simpleModeUsageBillingRepoStub struct {
	UsageBillingRepository
	seen map[string]struct{}
	cmds []*UsageBillingCommand
}

func (s *simpleModeUsageBillingRepoStub) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	s.cmds = append(s.cmds, cmd)
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	key := cmd.RequestID + ":" + strconv.FormatInt(cmd.APIKeyID, 10)
	if _, ok := s.seen[key]; ok {
		return &UsageBillingApplyResult{Applied: false}, nil
	}
	s.seen[key] = struct{}{}
	return &UsageBillingApplyResult{Applied: true}, nil
}

func TestApplyUsageBillingSimpleModeDeduplicatesWithoutBalanceEffects(t *testing.T) {
	const quote = 0.000078125
	repo := &simpleModeUsageBillingRepoStub{}
	p := &postUsageBillingParams{
		Cost:                       &CostBreakdown{ActualCost: quote, TotalCost: quote},
		User:                       &User{ID: 7, Balance: 0},
		APIKey:                     &APIKey{ID: 13, Quota: 100, RateLimit5h: 10},
		Account:                    &Account{ID: 9, Type: AccountTypeAPIKey},
		APIKeyService:              &apiKeyQuotaUpdaterStub{},
		SimpleModeKeyRateLimitOnly: true,
	}

	first, err := applyUsageBilling(context.Background(), "simple-req", &UsageLog{RequestID: "simple-req"}, p, &billingDeps{deferredService: &DeferredService{}}, repo)
	require.NoError(t, err)
	require.True(t, first)
	second, err := applyUsageBilling(context.Background(), "simple-req", &UsageLog{RequestID: "simple-req"}, p, &billingDeps{deferredService: &DeferredService{}}, repo)
	require.NoError(t, err)
	require.False(t, second)
	require.Len(t, repo.cmds, 2)
	require.Equal(t, quote, p.Cost.ActualCost, "retry must retain the original quote")
	require.Equal(t, repo.cmds[0].RequestFingerprint, repo.cmds[1].RequestFingerprint)
	for _, cmd := range repo.cmds {
		require.Zero(t, cmd.BalanceCost)
		require.Zero(t, cmd.SubscriptionCost)
		require.Zero(t, cmd.APIKeyQuotaCost)
		require.Zero(t, cmd.AccountQuotaCost)
		require.Equal(t, QuantizeUsageBillingAmount(quote), cmd.APIKeyRateLimitCost)
	}
}

func TestApplyUsageBillingSimpleModeRequiresAtomicDependencies(t *testing.T) {
	for _, missing := range []string{"repository", "request_id", "api_key"} {
		t.Run(missing, func(t *testing.T) {
			p := &postUsageBillingParams{
				Cost: &CostBreakdown{ActualCost: 1}, User: &User{ID: 7},
				APIKey: &APIKey{ID: 13, RateLimit5h: 10}, Account: &Account{ID: 9},
				SimpleModeKeyRateLimitOnly: true,
			}
			var repo UsageBillingRepository = &simpleModeUsageBillingRepoStub{}
			requestID := "simple-req"
			switch missing {
			case "repository":
				repo = nil
			case "request_id":
				requestID = ""
			case "api_key":
				p.APIKey = nil
			}
			_, err := applyUsageBilling(context.Background(), requestID, nil, p, &billingDeps{deferredService: &DeferredService{}}, repo)
			require.ErrorIs(t, err, ErrSimpleModeKeyRateLimitBillingUnavailable)
		})
	}
}

// A failed cache eviction must not fall through into standard-mode charges.
type simpleModeInvalidationCache struct {
	BillingCache
	invalidated []int64
}

func (c *simpleModeInvalidationCache) InvalidateAPIKeyRateLimit(_ context.Context, keyID int64) error {
	c.invalidated = append(c.invalidated, keyID)
	return errors.New("redis unavailable")
}
func TestSimpleModeBillingInvalidationOnlyEvictsKeyWindows(t *testing.T) {
	cache := &simpleModeInvalidationCache{}
	cmd := buildUsageBillingCommand("simple-invalidation", &UsageLog{}, &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 3}, User: &User{ID: 7}, APIKey: &APIKey{ID: 13, RateLimit5h: 10},
		Account: &Account{ID: 9}, SimpleModeKeyRateLimitOnly: true,
	})
	err := usageBillingCacheInvalidator(cache, nil)(context.Background(), cmd)
	require.ErrorContains(t, err, "redis unavailable")
	require.Equal(t, []int64{13}, cache.invalidated)
}
