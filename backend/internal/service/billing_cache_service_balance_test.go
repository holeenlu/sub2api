//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type balanceEligibilityCacheStub struct {
	billingCacheWorkerStub

	balance                  float64
	cacheMissAfterInvalidate bool
	invalidated              atomic.Bool
	invalidateCalls          atomic.Int64
}

func (s *balanceEligibilityCacheStub) GetUserBalance(context.Context, int64) (float64, error) {
	if s.cacheMissAfterInvalidate && s.invalidated.Load() {
		return 0, errors.New("cache miss")
	}
	return s.balance, nil
}

func (s *balanceEligibilityCacheStub) InvalidateUserBalance(context.Context, int64) error {
	s.invalidateCalls.Add(1)
	s.invalidated.Store(true)
	return nil
}

func TestCheckBillingEligibility_RejectsBalanceBelowMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.005}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	startBillingCacheForTest(t, svc)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestCheckBillingEligibility_AllowsBalanceAtMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.01}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	startBillingCacheForTest(t, svc)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.NoError(t, err)
}

func TestUsageBillingInvalidation_InvalidatesExhaustedBalance(t *testing.T) {
	cache := &balanceEligibilityCacheStub{
		balance:                  0.50,
		cacheMissAfterInvalidate: true,
	}
	userRepo := &balanceLoadUserRepoStub{balance: -0.25}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, cfg, nil)
	startBillingCacheForTest(t, svc)

	require.NoError(t, usageBillingCacheInvalidator(cache, nil)(context.Background(), &UsageBillingCommand{UserID: 1, BalanceCost: 0.75}))

	require.Equal(t, int64(1), cache.invalidateCalls.Load())

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, int64(1), userRepo.calls.Load())
}

func TestUsageBillingInvalidation_InvalidatesWhenBalanceFallsBelowReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.50}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	startBillingCacheForTest(t, svc)

	require.NoError(t, usageBillingCacheInvalidator(cache, nil)(context.Background(), &UsageBillingCommand{UserID: 1, BalanceCost: 0.495}))

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
}

func TestUsageBillingInvalidation_EvictsEvenWhenBalanceStillEligible(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 1}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	startBillingCacheForTest(t, svc)

	require.NoError(t, usageBillingCacheInvalidator(cache, nil)(context.Background(), &UsageBillingCommand{UserID: 1, BalanceCost: 0.25}))
	require.Equal(t, int64(1), cache.invalidateCalls.Load())
}

func TestSuperAdminBillingEligibilityStillEnforcesKeyWindows(t *testing.T) {
	cache := &fixedChargeCacheStub{balance: 0, rate: &APIKeyRateLimitCacheData{Usage1d: 5, Window1d: time.Now().Unix()}}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	startBillingCacheForTest(t, svc)
	root := &User{ID: 1, Role: RoleSuperAdmin}
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), root, &APIKey{ID: 2, RateLimit1d: 10}, nil, nil, ""))
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), root, &APIKey{ID: 2, RateLimit1d: 5}, nil, nil, ""), ErrAPIKeyRateLimit1dExceeded)
}
