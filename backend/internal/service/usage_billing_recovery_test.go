package service

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestUsagePricingRecoveryPreservesPolicyAndSplitConservation(t *testing.T) {
	ctx := context.Background()
	billing := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 300000, OutputTokens: 700, CacheReadTokens: 1000}
	gate := false
	req := &UsagePricingRequest{OpenAI: &OpenAIForwardResult{Model: "gpt-5.1"}, Models: []string{"gpt-5.1"}, Tokens: tokens, Multiplier: 1.7, BaseMultiplier: 1.7, ServiceTier: "priority", LongContextGate: &gate, FreeFast: true, PricingAt: time.Now()}
	aggregate := &UsageLog{InputTokens: tokens.InputTokens, OutputTokens: tokens.OutputTokens, CacheReadTokens: tokens.CacheReadTokens}
	cmd := &UsageBillingCommand{UsageLog: aggregate, UsageLogs: []*UsageLog{{InputTokens: 100000}, {InputTokens: 200000, OutputTokens: 700, CacheReadTokens: 1000}}, Pricing: &UsageBillingPricingSnapshot{Pending: true, Request: req, Balance: true, KeyQuota: true, KeyRateLimit: true}, RequestID: "repair", APIKeyID: 1}
	cmd.Normalize()
	fingerprint := cmd.RequestFingerprint
	require.NoError(t, usageBillingRepricer(billing, nil, nil)(ctx, cmd))
	require.False(t, cmd.Pricing.Pending)
	expected, err := (&OpenAIGatewayService{billingService: billing}).calculateOpenAIRecordUsageCost(ctx, req.OpenAI, &APIKey{}, req.Models, 1.7, 0, 0, 1.7, tokens, "", &gate, false, req.PricingAt)
	require.NoError(t, err)
	require.Equal(t, QuantizeUsageBillingAmount(expected.ActualCost), cmd.BalanceCost)
	require.Equal(t, cmd.BalanceCost, cmd.APIKeyQuotaCost)
	require.Equal(t, cmd.BalanceCost, aggregate.ActualCost)
	require.Equal(t, fingerprint, cmd.RequestFingerprint)
	require.False(t, aggregate.LongContextBillingApplied)
	require.InDelta(t, aggregate.ActualCost, cmd.UsageLogs[0].ActualCost+cmd.UsageLogs[1].ActualCost, 1e-10)
	require.InDelta(t, aggregate.TotalCost, cmd.UsageLogs[0].TotalCost+cmd.UsageLogs[1].TotalCost, 1e-10)
}

func TestUsagePricingPreflightDistinguishesUnknownFromExplicitFree(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	deps := inflightEstimateDeps{billing: billing, resolver: NewModelPricingResolver(nil, billing)}
	zero := 0.0
	ctx := context.Background()
	key := &APIKey{User: &User{ID: 1}, Group: &Group{ID: 1, ModelPricing: []ChannelModelPricing{{Models: []string{"custom-free"}, BillingMode: BillingModeToken, InputPrice: &zero, OutputPrice: &zero}}}}
	require.ErrorIs(t, deps.validateUsagePricing(ctx, key, InflightEstimateRequest{Model: "unpriced-model-quality-check"}), ErrModelPricingUnavailable)
	require.NoError(t, deps.validateUsagePricing(ctx, key, InflightEstimateRequest{Model: "custom-free"}))
}

func TestUsagePricingRecoveryRetainsVertexAccountQuota(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	request := &UsagePricingRequest{
		Gateway: &ForwardResult{Model: "claude-sonnet-4-5", Usage: ClaudeUsage{InputTokens: 1000}},
		Models:  []string{"claude-sonnet-4-5"}, Multiplier: 1, PricingAt: time.Now(),
	}
	cmd := buildUsageBillingCommand("vertex-pending", &UsageLog{Model: request.Models[0], InputTokens: 1000}, &postUsageBillingParams{
		Cost:   &CostBreakdown{PricingError: "temporarily unavailable", PricingRequest: request},
		APIKey: &APIKey{ID: 1}, User: &User{ID: 2},
		Account:               &Account{ID: 3, Type: AccountTypeServiceAccount, Extra: map[string]any{"quota_daily_limit": 10.0}},
		AccountRateMultiplier: 1.5,
	})
	require.True(t, cmd.Pricing.AccountQuota)
	require.NoError(t, usageBillingRepricer(billing, nil, nil)(context.Background(), cmd))
	require.False(t, cmd.Pricing.Pending)
	require.Positive(t, cmd.AccountQuotaCost)
	require.Equal(t, QuantizeUsageBillingAmount(cmd.UsageLog.TotalCost*1.5), cmd.AccountQuotaCost)
}

type billingRecoveryHookStub struct {
	UsageBillingRepository
	reprice    func(context.Context, *UsageBillingCommand) error
	invalidate func(context.Context, *UsageBillingCommand) error
}

func (s *billingRecoveryHookStub) ConfigureUsageBillingRecovery(r func(context.Context, *UsageBillingCommand) error, i func(context.Context, *UsageBillingCommand) error) {
	s.reprice, s.invalidate = r, i
}
func (s *billingRecoveryHookStub) RecoverPendingUsageBilling(context.Context, int, func(context.Context, *UsageBillingCommand) error) (int, error) {
	return 0, nil
}
func TestUsageBillingRecoveryFactoryInstallsHooksBeforeStarting(t *testing.T) {
	stub := &billingRecoveryHookStub{}
	pool := ProvideUsageRecordWorkerPool(&config.Config{}, nil, stub, nil, nil, nil, nil, nil)
	defer pool.Stop()
	require.NotNil(t, stub.reprice)
	require.NotNil(t, stub.invalidate)
	require.ErrorIs(t, stub.reprice(context.Background(), &UsageBillingCommand{Pricing: &UsageBillingPricingSnapshot{Pending: true}}), ErrUsagePricingPending)
	require.Error(t, stub.invalidate(context.Background(), &UsageBillingCommand{}), "unavailable invalidation must remain retryable")
}

type recoveryPricingGroups struct {
	GroupRepository
	group *Group
}

func (s *recoveryPricingGroups) GetByIDLite(context.Context, int64) (*Group, error) {
	return s.group, nil
}
func TestUsagePricingRecoveryRequiresExplicitCNPrice(t *testing.T) {
	ctx := context.Background()
	id := int64(1)
	billing := NewBillingService(&config.Config{}, nil)
	groups := &recoveryPricingGroups{group: &Group{ID: id}}
	reprice := usageBillingRepricer(billing, NewModelPricingResolver(nil, billing), groups)
	cmd := &UsageBillingCommand{UsageLog: &UsageLog{}, Pricing: &UsageBillingPricingSnapshot{Pending: true, Balance: true, Request: &UsagePricingRequest{AccountPlatform: PlatformKimi, GroupID: &id, OpenAI: &OpenAIForwardResult{Model: "claude-sonnet-4-5"}, Models: []string{"claude-sonnet-4-5"}, Tokens: UsageTokens{InputTokens: 1000}, Multiplier: 1}}}
	require.Error(t, reprice(ctx, cmd))
	require.True(t, cmd.Pricing.Pending)
	price := 0.1
	groups.group.ModelPricing = []ChannelModelPricing{{Models: []string{"claude-sonnet-4-5"}, InputPrice: &price, OutputPrice: &price, BillingMode: BillingModeToken}}
	require.NoError(t, reprice(ctx, cmd))
	require.False(t, cmd.Pricing.Pending)
	require.Positive(t, cmd.BalanceCost)
}

func TestSuperAdminBillingKeepsQuotaWhenBalanceIsExempt(t *testing.T) {
	for _, role := range []string{RoleSuperAdmin, RoleAdmin, RoleUser} {
		t.Run(role, func(t *testing.T) {
			p := &postUsageBillingParams{User: &User{ID: 1, Role: role}, APIKey: &APIKey{ID: 2, Quota: 10, RateLimit1d: 10}, APIKeyService: &apiKeyQuotaUpdaterStub{}, Account: &Account{ID: 3}, Platform: "anthropic", Cost: &CostBreakdown{ActualCost: 2, TotalCost: 2}}
			cmd := buildUsageBillingCommand("role-quota", &UsageLog{ActualCost: 2}, p)
			require.Equal(t, role == RoleSuperAdmin, cmd.BalanceExempt)
			if role == RoleSuperAdmin {
				require.Zero(t, cmd.BalanceCost)
			} else {
				require.Equal(t, 2.0, cmd.BalanceCost)
			}
			require.Equal(t, 2.0, cmd.APIKeyQuotaCost)
			require.Equal(t, 2.0, cmd.APIKeyRateLimitCost)
			require.Equal(t, 2.0, cmd.UserPlatformQuotaCost())
		})
	}
}

func TestSuperAdminPricingRecoveryCannotRestoreBalanceDebit(t *testing.T) {
	request := &UsagePricingRequest{Gateway: &ForwardResult{Model: "claude-sonnet-4-5", Usage: ClaudeUsage{InputTokens: 1000}}, Models: []string{"claude-sonnet-4-5"}, Multiplier: 1, PricingAt: time.Now()}
	cmd := buildUsageBillingCommand("root-pending", &UsageLog{Model: request.Models[0], InputTokens: 1000}, &postUsageBillingParams{
		User: &User{ID: 1, Role: RoleSuperAdmin}, APIKey: &APIKey{ID: 2, Quota: 10, RateLimit1d: 10}, APIKeyService: &apiKeyQuotaUpdaterStub{}, Account: &Account{ID: 3}, Platform: "anthropic", Cost: &CostBreakdown{PricingError: "missing", PricingRequest: request},
	})
	require.False(t, cmd.Pricing.Balance)
	require.True(t, cmd.Pricing.KeyQuota)
	data, err := json.Marshal(cmd)
	require.NoError(t, err)
	var recovered UsageBillingCommand
	require.NoError(t, json.Unmarshal(data, &recovered))
	require.NoError(t, usageBillingRepricer(NewBillingService(&config.Config{}, nil), nil, nil)(context.Background(), &recovered))
	require.Zero(t, recovered.BalanceCost)
	require.Positive(t, recovered.APIKeyQuotaCost)
	require.Equal(t, recovered.APIKeyQuotaCost, recovered.APIKeyRateLimitCost)
	require.Equal(t, recovered.APIKeyQuotaCost, recovered.UserPlatformQuotaCost())
	require.Equal(t, cmd.RequestFingerprint, recovered.RequestFingerprint)
}
