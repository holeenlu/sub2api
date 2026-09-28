package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// Construct the harvester only after history and proxy settings are available.
func ProvideOpenAIGatewayService(
	accountRepo AccountRepository,
	usageLogRepo UsageLogRepository,
	usageBillingRepo UsageBillingRepository,
	userRepo UserRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache GatewayCache,
	cfg *config.Config,
	schedulerSnapshot *SchedulerSnapshotService,
	concurrencyService *ConcurrencyService,
	billingService *BillingService,
	rateLimitService *RateLimitService,
	billingCacheService *BillingCacheService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	openAITokenProvider *OpenAITokenProvider,
	grokTokenProvider *GrokTokenProvider,
	resolver *ModelPricingResolver,
	channelService *ChannelService,
	balanceNotifyService *BalanceNotifyService,
	settingService *SettingService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	history CodexTicketAttemptRepository,
	rpmCache RPMCache,
) *OpenAIGatewayService {
	s := NewOpenAIGatewayService(accountRepo, usageLogRepo, usageBillingRepo, userRepo,
		userSubRepo, userGroupRateRepo, cache, cfg, schedulerSnapshot, concurrencyService,
		billingService, rateLimitService, billingCacheService, httpUpstream, deferredService,
		openAITokenProvider, grokTokenProvider, resolver, channelService, balanceNotifyService,
		settingService, userPlatformQuotaRepo, WithOpenAIRPMCache(rpmCache))
	s.SetCodexTicketHistory(history)
	if settingService != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = settingService.LoadModelTraceBank(ctx)
		cancel()
	}
	s.StartOpenAICodexTicketHarvester()
	return s
}

func (s *OpenAIGatewayService) SetCodexTicketHistory(history CodexTicketAttemptRepository) {
	s.openaiCodexTicketHistory = history
	s.openaiCodexTicketLifecycle, _ = history.(CodexTicketLifecycleRepository)
}
