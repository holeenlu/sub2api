package service

import (
	"context"
	"errors"
	"time"
)

// Contains metering and pricing policy only: never prompts, headers, users or credentials.
// Multipliers, tier and long-context policy are frozen; missing price entries may be repaired.
type UsagePricingRequest struct {
	GroupLongContextEnabled                                      bool
	AccountPlatform                                              string
	Gateway                                                      *ForwardResult
	OpenAI                                                       *OpenAIForwardResult
	Models                                                       []string
	Tokens                                                       UsageTokens
	GroupID                                                      *int64
	Multiplier, ImageMultiplier, VideoMultiplier, BaseMultiplier float64
	ServiceTier                                                  string
	LongContextGate                                              *bool
	ModelLongContextExempt, FreeFast                             bool
	PricingAt                                                    time.Time
}

func gatewayPricingRequest(result *ForwardResult, key *APIKey, model string, rate, imageRate float64, at time.Time) *UsagePricingRequest {
	return &UsagePricingRequest{Gateway: &ForwardResult{Model: result.Model, Usage: result.Usage,
		ImageCount: result.ImageCount, ImageSize: result.ImageSize, SearchCount: result.SearchCount,
		AudioUsage: result.AudioUsage, CacheStorageTokenHours: result.CacheStorageTokenHours,
		ReasoningEffort: result.ReasoningEffort, ServiceTier: result.ServiceTier},
		Models: []string{model}, GroupID: key.GroupID, Multiplier: rate, ImageMultiplier: imageRate, PricingAt: at}
}

func openAIPricingRequest(result *OpenAIForwardResult, key *APIKey, models []string, tokens UsageTokens, rate, imageRate, videoRate, baseRate float64, tier string, gate *bool, exempt, freeFast bool, at time.Time) *UsagePricingRequest {
	return &UsagePricingRequest{OpenAI: &OpenAIForwardResult{Model: result.Model, Usage: result.Usage,
		ImageCount: result.ImageCount, ImageSize: result.ImageSize, VideoCount: result.VideoCount,
		VideoResolution: result.VideoResolution, VideoDurationSeconds: result.VideoDurationSeconds,
		SearchCount: result.SearchCount, WebSearchCalls: result.WebSearchCalls, AudioUsage: result.AudioUsage,
		ReasoningEffort: result.ReasoningEffort}, Models: append([]string(nil), models...), Tokens: tokens,
		GroupID: key.GroupID, Multiplier: rate, ImageMultiplier: imageRate, VideoMultiplier: videoRate,
		BaseMultiplier: baseRate, ServiceTier: tier, LongContextGate: gate, ModelLongContextExempt: exempt,
		FreeFast: freeFast, PricingAt: at}
}

func usageBillingRepricer(billing *BillingService, resolver *ModelPricingResolver, groups GroupRepository) func(context.Context, *UsageBillingCommand) error {
	return func(ctx context.Context, cmd *UsageBillingCommand) error {
		if cmd.Pricing == nil || !cmd.Pricing.Pending {
			return nil
		}
		p := cmd.Pricing.Request
		if p == nil || cmd.UsageLog == nil {
			return ErrUsagePricingPending
		}
		key := &APIKey{ID: cmd.APIKeyID, GroupID: p.GroupID}
		if p.GroupID != nil {
			group, err := groups.GetByIDLite(ctx, *p.GroupID)
			if err != nil {
				return err
			}
			if group != nil {
				clone := *group
				clone.LongContextPricingEnabled = p.GroupLongContextEnabled
				group = &clone
			}
			key.Group = group
		}
		var cost *CostBreakdown
		if p.OpenAI != nil {
			svc := &OpenAIGatewayService{billingService: billing, resolver: resolver}
			models := svc.filterCNProviderBillingModelCandidates(ctx, &Account{Platform: p.AccountPlatform}, key, p.Models)
			var err error
			cost, err = svc.calculateOpenAIRecordUsageCost(ctx, p.OpenAI, key, models, p.Multiplier, p.ImageMultiplier, p.VideoMultiplier, p.BaseMultiplier, p.Tokens, p.ServiceTier, p.LongContextGate, p.ModelLongContextExempt, p.PricingAt)
			if err != nil {
				return err
			}
			if p.FreeFast {
				standard, err := svc.calculateOpenAIRecordUsageCost(ctx, p.OpenAI, key, models, p.Multiplier, p.ImageMultiplier, p.VideoMultiplier, p.BaseMultiplier, p.Tokens, "", p.LongContextGate, p.ModelLongContextExempt, p.PricingAt)
				if err != nil {
					return err
				}
				cost.ActualCost = standard.ActualCost
			}
		} else if p.Gateway != nil && len(p.Models) > 0 {
			svc := &GatewayService{billingService: billing, resolver: resolver}
			cost = svc.calculateRecordUsageCost(ctx, p.Gateway, key, p.Models[0], p.Multiplier, p.ImageMultiplier, p.PricingAt)
		}
		if cost == nil || cost.PricingError != "" {
			return ErrUsagePricingPending
		}
		log := cmd.UsageLog
		log.InputCost, log.OutputCost = cost.InputCost, cost.OutputCost
		log.CacheCreationCost, log.CacheReadCost = cost.CacheCreationCost, cost.CacheReadCost
		log.ImageInputCost, log.ImageOutputCost = cost.ImageInputCost, cost.ImageOutputCost
		log.TotalCost, log.ActualCost = cost.TotalCost, cost.ActualCost
		log.LongContextBillingApplied = cost.LongContextBillingApplied
		log.BillingMode = optionalTrimmedStringPtr(cost.BillingMode)
		if len(cmd.UsageLogs) > 0 {
			allocateAuthorizationUsageRows(log, cmd.UsageLogs)
			for _, row := range cmd.UsageLogs {
				row.LongContextBillingApplied = log.LongContextBillingApplied
				row.BillingMode = log.BillingMode
			}
		}
		policy := cmd.Pricing
		if policy.Balance {
			cmd.BalanceCost = cost.ActualCost
		}
		if policy.Subscription {
			cmd.SubscriptionCost = cost.ActualCost
		}
		if policy.KeyQuota {
			cmd.APIKeyQuotaCost = cost.ActualCost
		}
		if policy.KeyRateLimit {
			cmd.APIKeyRateLimitCost = cost.ActualCost
		}
		if policy.AccountQuota {
			cmd.AccountQuotaCost = cost.TotalCost * policy.AccountRateMultiplier
		}
		policy.Pending = false
		cmd.Normalize()
		return nil
	}
}

func usageBillingCacheInvalidator(cache BillingCache, keys *APIKeyService) func(context.Context, *UsageBillingCommand) error {
	return func(ctx context.Context, cmd *UsageBillingCommand) error {
		if cache == nil {
			return errors.New("billing cache unavailable")
		}
		var errs []error
		if cmd.BalanceCost > 0 {
			errs = append(errs, cache.InvalidateUserBalance(ctx, cmd.UserID))
		}
		if cmd.SubscriptionCost > 0 && cmd.UsageLog != nil && cmd.UsageLog.GroupID != nil {
			errs = append(errs, cache.InvalidateSubscriptionCache(ctx, cmd.UserID, *cmd.UsageLog.GroupID))
		}
		if cmd.APIKeyRateLimitCost > 0 {
			errs = append(errs, cache.InvalidateAPIKeyRateLimit(ctx, cmd.APIKeyID))
		}
		if cmd.Platform != "" && cmd.BalanceCost > 0 {
			errs = append(errs, cache.DeleteUserPlatformQuotaCache(ctx, cmd.UserID, cmd.Platform))
		}
		if cmd.APIKeyQuotaCost > 0 && keys != nil {
			key, err := keys.apiKeyRepo.GetByID(ctx, cmd.APIKeyID)
			if err != nil && !errors.Is(err, ErrAPIKeyNotFound) {
				errs = append(errs, err)
			}
			if key != nil {
				cacheKey := keys.authCacheKey(key.Key)
				if keys.authCacheL1 != nil {
					keys.authCacheL1.Del(cacheKey)
				}
				if keys.cache != nil {
					errs = append(errs, keys.cache.DeleteAuthCache(ctx, cacheKey), keys.cache.PublishAuthCacheInvalidation(ctx, cacheKey))
				}
			}
		}
		return errors.Join(errs...)
	}
}
