package service

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func ProvideGrokOAuthService(proxyRepo ProxyRepository, oauthClient GrokOAuthClient, cfg *config.Config, redisClient *redis.Client) *GrokOAuthService {
	svc := NewGrokOAuthService(proxyRepo, oauthClient, cfg)
	// wire.go is depguard-exempt for redis; construct the Redis session store here.
	if redisClient != nil {
		svc = svc.WithSessionStore(xai.NewRedisSessionStore(redisClient))
	}
	return svc
}

// BuildInfo contains build information
type BuildInfo struct {
	UpstreamVersion string
	BuildCommit     string
	Version         string
	BuildType       string
}

// ProvidePricingService creates and initializes PricingService
func ProvidePricingService(cfg *config.Config, remoteClient PricingRemoteClient) (*PricingService, error) {
	svc := NewPricingService(cfg, remoteClient)
	return svc, nil
}

// ProvideBuildStatusService creates BuildStatusService with BuildInfo
func ProvideBuildStatusService(buildInfo BuildInfo) *BuildStatusService {
	return NewBuildStatusService(buildInfo.Version).WithUpstreamVersion(buildInfo.UpstreamVersion).WithBuildCommit(buildInfo.BuildCommit)
}

// ProvideEmailQueueService creates EmailQueueService with default worker count
func ProvideEmailQueueService(emailService *EmailService) *EmailQueueService {
	return NewEmailQueueService(emailService, 3)
}

// ProvideAuthService wires the optional captcha providers into AuthService while
// keeping NewAuthService's public constructor compatible with existing tests.
func ProvideAuthService(
	entClient *dbent.Client,
	userRepo UserRepository,
	refreshTokenCache RefreshTokenCache,
	cfg *config.Config,
	settingService *SettingService,
	emailService *EmailService,
	turnstileService *TurnstileService,
	tencentCaptchaService *TencentCaptchaService,
	aliyunCaptchaService *AliyunCaptchaService,
	emailQueueService *EmailQueueService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	userPlatformQuotaRepo UserPlatformQuotaRepository) *AuthService {
	svc := NewAuthService(
		entClient,
		userRepo,
		refreshTokenCache,
		cfg,
		settingService,
		emailService,
		turnstileService,
		emailQueueService,
		defaultSubAssigner,
		userPlatformQuotaRepo)
	svc.SetTencentCaptchaService(tencentCaptchaService)
	svc.SetAliyunCaptchaService(aliyunCaptchaService)
	return svc
}

// ProvideOAuthRefreshAPI creates OAuthRefreshAPI with the default lock TTL.
func ProvideOAuthRefreshAPI(accountRepo AccountRepository, tokenCache GeminiTokenCache) *OAuthRefreshAPI {
	return NewOAuthRefreshAPI(accountRepo, tokenCache)
}

func ProvideBatchImageModelPricingResolver(resolver *ModelPricingResolver) *BatchImageModelPricingResolver {
	return &BatchImageModelPricingResolver{Resolver: resolver}
}

func ProvideBatchImageCleanupService(repo BatchImageRepository, accountRepo AccountRepository, cfg *config.Config) *BatchImageCleanupService {
	svc := NewBatchImageCleanupService(repo, accountRepo, cfg)
	return svc
}

// ProvideOpenAIOAuthService creates OpenAIOAuthService with privacy/account enrichment support.
func ProvideOpenAIOAuthService(
	accountRepo AccountRepository, encryptor SecretEncryptor, cfg *config.Config,
	proxyRepo ProxyRepository,
	oauthClient OpenAIOAuthClient,
	privacyClientFactory PrivacyClientFactory,
	redisClient *redis.Client,
) *OpenAIOAuthService {
	svc := NewOpenAIOAuthService(proxyRepo, oauthClient, redisClient)

	svc.SetPrivacyClientFactory(privacyClientFactory)
	svc.excelAccountRepo = accountRepo
	if cfg != nil && cfg.Totp.EncryptionKeyConfigured {
		svc.excelEncryptor = encryptor
	}
	return svc
}

// ProvideOpenAICloudService attaches Cloud to the existing gateway at startup.
func ProvideOpenAICloudService(accounts AccountRepository, tokens *OpenAITokenProvider, oauth *OpenAIOAuthService, gateway *OpenAIGatewayService, lifetime *host.Lifetime) *OpenAICloudService {
	svc := NewOpenAICloudService(accounts, tokens, oauth)
	// SetCloudService also starts durable billing recovery. Invoke it only
	// after cache/subscription readiness and normal Usage startup, before ingress.
	owner := host.OwnBlockingShutdown("CloudBilling", func() error { gateway.StopCloudBilling(); return nil }, true)
	lifetime.Add(host.Component{Name: "CloudBilling", Phase: host.Usage, ShutdownOrder: 20, StartOrder: 21,
		Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			gateway.SetCloudService(svc)
			return nil
		},
		RequestStop: owner.RequestStop, Stop: owner.Stop})
	return svc
}

// ProvideTokenRefreshService constructs TokenRefreshService
func ProvideTokenRefreshService(
	accountRepo AccountRepository,
	oauthService *OAuthService,
	openaiOAuthService *OpenAIOAuthService,
	geminiOAuthService *GeminiOAuthService,
	antigravityOAuthService *AntigravityOAuthService,
	grokOAuthService *GrokOAuthService,
	cacheInvalidator TokenCacheInvalidator,
	schedulerCache SchedulerCache,
	cfg *config.Config,
	tempUnschedCache TempUnschedCache,
	privacyClientFactory PrivacyClientFactory,
	proxyRepo ProxyRepository,
	refreshAPI *OAuthRefreshAPI,
	runtimeBlocker AccountRuntimeBlocker,
) *TokenRefreshService {
	svc := NewTokenRefreshService(accountRepo, oauthService, openaiOAuthService, geminiOAuthService, antigravityOAuthService, cacheInvalidator, schedulerCache, cfg, tempUnschedCache, grokOAuthService)
	// 注入 OpenAI privacy opt-out 依赖
	svc.SetPrivacyDeps(privacyClientFactory, proxyRepo)
	// 注入统一 OAuth 刷新 API（消除 TokenRefreshService 与 TokenProvider 之间的竞争条件）
	svc.SetRefreshAPI(refreshAPI)
	// 调用侧显式注入后台刷新策略，避免策略漂移
	svc.SetRefreshPolicy(DefaultBackgroundRefreshPolicy())
	svc.SetAccountRuntimeBlocker(runtimeBlocker)
	return svc
}

// ProvideClaudeTokenProvider creates ClaudeTokenProvider with OAuthRefreshAPI injection
func ProvideClaudeTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	oauthService *OAuthService,
	refreshAPI *OAuthRefreshAPI,
) *ClaudeTokenProvider {
	p := NewClaudeTokenProvider(accountRepo, tokenCache, oauthService)
	executor := NewClaudeTokenRefresher(oauthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(ClaudeProviderRefreshPolicy())
	return p
}

// ProvideOpenAITokenProvider creates OpenAITokenProvider with OAuthRefreshAPI injection
func ProvideOpenAITokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	openaiOAuthService *OpenAIOAuthService,
	refreshAPI *OAuthRefreshAPI,
) *OpenAITokenProvider {
	p := NewOpenAITokenProvider(accountRepo, tokenCache, openaiOAuthService)
	executor := NewOpenAITokenRefresher(openaiOAuthService, accountRepo)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(OpenAIProviderRefreshPolicy())
	return p
}

// ProvidePluginManager preserves account-directory wiring when regenerating Wire.
func ProvidePluginManager(repo PluginRepository, encryptor SecretEncryptor, cfg *config.Config, hostInfo PluginHostInfo, kvStore PluginKVStore, gateway *OpenAIGatewayService) *PluginManager {
	manager := NewPluginManager(repo, encryptor, cfg, hostInfo, kvStore)
	manager.SetAccountDirectory(gateway)
	return manager
}

// ProvideOpenAIQuotaService wires the OpenAI quota query/reset service.
// It depends on the OpenAI token provider for refreshed access tokens and the
// privacy client factory for the impersonated upstream HTTP client.
func ProvideOpenAIQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	tokenProvider *OpenAITokenProvider,
	privacyClientFactory PrivacyClientFactory,
	referralClient OpenAIReferralClient,
	openAIGatewayService *OpenAIGatewayService,
) *OpenAIQuotaService {
	service := NewOpenAIQuotaService(accountRepo, proxyRepo, tokenProvider, privacyClientFactory, referralClient)
	service.agentIdentityWS = openAIGatewayService
	return service
}

// ProvideOpenAIQuotaAutoResetService 启动账号级自动用卡队列与补偿扫描。
func ProvideOpenAIQuotaAutoResetService(
	accountRepo AccountRepository,
	quotaService *OpenAIQuotaService,
	rateLimitService *RateLimitService,
	idempotency *IdempotencyCoordinator,
	audit *AuditLogService,
	settingService *SettingService,
	leaderLock LeaderLockCache,
) *OpenAIQuotaAutoResetService {
	service := NewOpenAIQuotaAutoResetService(
		accountRepo,
		quotaService,
		rateLimitService,
		idempotency,
		audit,
		settingService,
		leaderLock,
	)
	return service
}

func ProvideAccountUsageService(
	accountRepo AccountRepository,
	usageLogRepo UsageLogRepository,
	usageFetcher ClaudeUsageFetcher,
	geminiQuotaService *GeminiQuotaService,
	antigravityQuotaFetcher *AntigravityQuotaFetcher,
	grokQuotaFetcher *GrokQuotaFetcher,
	grokQuotaService *GrokQuotaService,
	openAIQuotaService *OpenAIQuotaService,
	cache *UsageCache,
	identityCache IdentityCache,
	tlsFPProfileService *TLSFingerprintProfileService,
	openAIGatewayService *OpenAIGatewayService,
) *AccountUsageService {
	service := NewAccountUsageService(
		accountRepo,
		usageLogRepo,
		usageFetcher,
		geminiQuotaService,
		antigravityQuotaFetcher,
		grokQuotaFetcher,
		grokQuotaService,
		openAIQuotaService,
		cache,
		identityCache,
		tlsFPProfileService,
	)
	service.agentIdentityWS = openAIGatewayService
	return service
}

func ProvideAccountTestService(
	accountRepo AccountRepository,
	geminiTokenProvider *GeminiTokenProvider,
	claudeTokenProvider *ClaudeTokenProvider,
	grokTokenProvider *GrokTokenProvider,
	antigravityGatewayService *AntigravityGatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
	openAIGatewayService *OpenAIGatewayService,
	settingService *SettingService,
	pluginManager *PluginManager,
) *AccountTestService {
	service := NewAccountTestService(
		accountRepo,
		geminiTokenProvider,
		claudeTokenProvider,
		grokTokenProvider,
		antigravityGatewayService,
		httpUpstream,
		cfg,
		tlsFPProfileService,
	)
	service.agentIdentityWS = openAIGatewayService
	service.SetOpenAIGatewayService(openAIGatewayService)
	service.SetSettingService(settingService)
	service.SetPluginManager(pluginManager)
	return service
}

func ProvideGrokQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	tokenProvider *GrokTokenProvider,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	usageLogRepo UsageLogRepository,
	settingService *SettingService,
) *GrokQuotaService {
	service := NewGrokQuotaService(accountRepo, proxyRepo, tokenProvider, httpUpstream, cfg, usageLogRepo)
	service.SetSettingService(settingService)
	return service
}

// ProvideCNProviderQuotaService 构造国产供应商 Coding Plan 额度探测服务。
func ProvideCNProviderQuotaService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *CNProviderQuotaService {
	return NewCNProviderQuotaService(accountRepo, proxyRepo, httpUpstream, cfg)
}

// ProvideCNProviderBalanceService 构造国产供应商余额探测服务。
func ProvideCNProviderBalanceService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *CNProviderBalanceService {
	return NewCNProviderBalanceService(accountRepo, proxyRepo, httpUpstream, cfg)
}

// ProvideCNProviderBalanceCheckService 构造并启动周期余额/额度检测任务。
// payg 账号探余额（低余额停调）；coding plan 账号探 5h/weekly 滚动窗口
// （落 extra 快照供调度阈值评估自动停调）。
// 间隔取自 gateway.cn_providers.balance_check_interval_minutes；<=0 或关闭时不启动。
func ProvideCNProviderBalanceCheckService(
	accountRepo AccountRepository,
	balanceService *CNProviderBalanceService,
	quotaService *CNProviderQuotaService,
	cfg *config.Config,
) *CNProviderBalanceCheckService {
	minutes := 10
	if cfg != nil && cfg.Gateway.CNProviders.BalanceCheckIntervalMinutes > 0 {
		minutes = cfg.Gateway.CNProviders.BalanceCheckIntervalMinutes
	}
	svc := NewCNProviderBalanceCheckService(accountRepo, balanceService, quotaService, cfg, time.Duration(minutes)*time.Minute)
	return svc
}

// ProvideGeminiTokenProvider creates GeminiTokenProvider with OAuthRefreshAPI injection
func ProvideGeminiTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	geminiOAuthService *GeminiOAuthService,
	refreshAPI *OAuthRefreshAPI,
) *GeminiTokenProvider {
	p := NewGeminiTokenProvider(accountRepo, tokenCache, geminiOAuthService)
	executor := NewGeminiTokenRefresher(geminiOAuthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(GeminiProviderRefreshPolicy())
	return p
}

// ProvideAntigravityTokenProvider creates AntigravityTokenProvider with OAuthRefreshAPI injection
func ProvideAntigravityTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	antigravityOAuthService *AntigravityOAuthService,
	refreshAPI *OAuthRefreshAPI,
	tempUnschedCache TempUnschedCache,
) *AntigravityTokenProvider {
	p := NewAntigravityTokenProvider(accountRepo, tokenCache, antigravityOAuthService)
	executor := NewAntigravityTokenRefresher(antigravityOAuthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(AntigravityProviderRefreshPolicy())
	p.SetTempUnschedCache(tempUnschedCache)
	return p
}

// ProvideGrokTokenProvider creates GrokTokenProvider with OAuthRefreshAPI injection.
func ProvideGrokTokenProvider(
	accountRepo AccountRepository,
	tokenCache GeminiTokenCache,
	grokOAuthService *GrokOAuthService,
	refreshAPI *OAuthRefreshAPI,
	tempUnschedCache TempUnschedCache,
) *GrokTokenProvider {
	p := NewGrokTokenProvider(accountRepo, tokenCache)
	executor := NewGrokTokenRefresher(grokOAuthService)
	p.SetRefreshAPI(refreshAPI, executor)
	p.SetRefreshPolicy(GrokProviderRefreshPolicy())
	p.SetTempUnschedCache(tempUnschedCache)
	return p
}

// ProvideDashboardAggregationService 构造仪表盘聚合服务
func ProvideDashboardAggregationService(repo DashboardAggregationRepository, timingWheel *TimingWheelService, lockCache LeaderLockCache, db *sql.DB, cfg *config.Config, settingRepo SettingRepository) *DashboardAggregationService {
	svc := NewDashboardAggregationService(repo, timingWheel, cfg)
	svc.settingRepo = settingRepo
	svc.SetLeaderLock(lockCache, db)
	return svc
}

// ProvideUsageCleanupService 构造使用记录清理任务服务
func ProvideUsageCleanupService(repo UsageCleanupRepository, timingWheel *TimingWheelService, dashboardAgg *DashboardAggregationService, cfg *config.Config) *UsageCleanupService {
	svc := NewUsageCleanupService(repo, timingWheel, dashboardAgg, cfg)
	return svc
}

// ProvideAccountExpiryService constructs AccountExpiryService.
func ProvideAccountExpiryService(accountRepo AccountRepository) *AccountExpiryService {
	svc := NewAccountExpiryService(accountRepo, time.Minute)
	return svc
}

// ProvideOpenAICodexVersionSyncService constructs OpenAICodexVersionSyncService.
// 出站 Codex 身份的版本号靠它跟随官方发布，无需为了跟版本而发新版本；面板可关闭。
func ProvideOpenAICodexVersionSyncService(
	settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
) *OpenAICodexVersionSyncService {
	svc := NewOpenAICodexVersionSyncService(settingRepo, settingService, githubClient, openAICodexVersionSyncInterval)
	return svc
}

// ProvideClaudeCodeVersionSyncService constructs ClaudeCodeVersionSyncService.
// 出站 Claude Code 身份的版本号靠它跟随官方发布，无需为了跟版本而发新版本；面板可关闭。
func ProvideClaudeCodeVersionSyncService(
	settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
) *ClaudeCodeVersionSyncService {
	svc := NewClaudeCodeVersionSyncService(settingRepo, settingService, githubClient, claudeCodeVersionSyncInterval)
	return svc
}

// ProvideProxyExpiryService constructs ProxyExpiryService.
func ProvideProxyExpiryService(proxyRepo ProxyRepository) *ProxyExpiryService {
	svc := NewProxyExpiryService(proxyRepo, time.Minute)
	return svc
}

// ProvideSubscriptionExpiryService constructs SubscriptionExpiryService.
func ProvideSubscriptionExpiryService(userSubRepo UserSubscriptionRepository, settingRepo SettingRepository, notificationEmailService *NotificationEmailService, lockCache LeaderLockCache, db *sql.DB) *SubscriptionExpiryService {
	svc := NewSubscriptionExpiryService(userSubRepo, time.Minute)
	svc.SetSettingRepository(settingRepo)
	svc.SetNotificationEmailService(notificationEmailService)
	svc.SetLeaderLock(lockCache, db)
	return svc
}

// ProvideTimingWheelService constructs TimingWheelService
func ProvideTimingWheelService() (*TimingWheelService, error) {
	svc, err := NewTimingWheelService()
	if err != nil {
		return nil, err
	}
	return svc, nil
}

// ProvideDeferredService constructs DeferredService
func ProvideDeferredService(accountRepo AccountRepository, timingWheel *TimingWheelService) *DeferredService {
	svc := NewDeferredService(accountRepo, timingWheel, 10*time.Second)
	return svc
}

// ProvideConcurrencyService creates ConcurrencyService and starts slot cleanup worker.
func ProvideConcurrencyService(cache ConcurrencyCache, accountRepo AccountRepository, cfg *config.Config) *ConcurrencyService {
	svc := NewConcurrencyService(cache)
	if cfg != nil {
		svc.SetAccountLoadBatchCacheTTL(time.Duration(cfg.Gateway.Scheduling.LoadBatchCacheTTLMS) * time.Millisecond)
	}
	return svc
}

// ProvideUserMessageQueueService 创建用户消息串行队列服务并启动清理 worker
func ProvideUserMessageQueueService(cache UserMsgQueueCache, rpmCache RPMCache, cfg *config.Config) *UserMessageQueueService {
	svc := NewUserMessageQueueService(cache, rpmCache, &cfg.Gateway.UserMessageQueue)
	return svc
}

// ProvideSchedulerSnapshotService constructs SchedulerSnapshotService.
func ProvideSchedulerSnapshotService(
	cache SchedulerCache,
	outboxRepo SchedulerOutboxRepository,
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	cfg *config.Config,
) *SchedulerSnapshotService {
	svc := NewSchedulerSnapshotService(cache, outboxRepo, accountRepo, groupRepo, cfg)
	return svc
}

// ProvideRateLimitService creates RateLimitService with optional dependencies.
func ProvideRateLimitService(
	accountRepo AccountRepository,
	usageRepo UsageLogRepository,
	cfg *config.Config,
	geminiQuotaService *GeminiQuotaService,
	tempUnschedCache TempUnschedCache,
	timeoutCounterCache TimeoutCounterCache,
	openAI403CounterCache OpenAI403CounterCache,
	settingService *SettingService,
	tokenCacheInvalidator TokenCacheInvalidator,
	ollamaCloudUsage *OllamaCloudUsageService,
) *RateLimitService {
	svc := NewRateLimitService(accountRepo, usageRepo, cfg, geminiQuotaService, tempUnschedCache)
	if healthCache, ok := tempUnschedCache.(OpenAIAPIKeyHealthCache); ok {
		svc.SetOpenAIAPIKeyHealthCache(healthCache)
	}
	svc.SetTimeoutCounterCache(timeoutCounterCache)
	svc.SetOpenAI403CounterCache(openAI403CounterCache)
	svc.SetSettingService(settingService)
	svc.SetTokenCacheInvalidator(tokenCacheInvalidator)
	svc.SetOllamaCloudUsageProbeScheduler(ollamaCloudUsage)
	return svc
}

// ProvideOpsMetricsCollector constructs OpsMetricsCollector.
func ProvideOpsMetricsCollector(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	accountRepo AccountRepository,
	concurrencyService *ConcurrencyService,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
) *OpsMetricsCollector {
	collector := NewOpsMetricsCollector(opsRepo, settingRepo, accountRepo, concurrencyService, db, redisClient, cfg)
	return collector
}

// ProvideOpsAggregationService constructs OpsAggregationService (hourly/daily pre-aggregation).
func ProvideOpsAggregationService(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
) *OpsAggregationService {
	svc := NewOpsAggregationService(opsRepo, settingRepo, db, redisClient, cfg)
	return svc
}

// ProvideOpsAlertEvaluatorService constructs OpsAlertEvaluatorService.
func ProvideOpsAlertEvaluatorService(
	opsService *OpsService,
	opsRepo OpsRepository,
	emailService *EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
	proxyRepo ProxyRepository,
) *OpsAlertEvaluatorService {
	svc := NewOpsAlertEvaluatorService(opsService, opsRepo, emailService, redisClient, cfg, proxyRepo)
	return svc
}

// ProvideOpsCleanupService constructs OpsCleanupService (cron scheduled).
// channelMonitorSvc 让维护任务（聚合 + 历史/聚合软删）跟随 ops 清理 cron 一起跑，
// 共享 leader lock + heartbeat。
// settingRepo 让 cleanup service 自己读 ops_advanced_settings.data_retention 覆盖 cfg；
// opsService 用来反向注入 cleanup hook，以便 UI 改清理设置时能 Reload cron。
func ProvideOpsCleanupService(
	opsRepo OpsRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	settingRepo SettingRepository,
	opsService *OpsService,
) *OpsCleanupService {
	svc := NewOpsCleanupService(opsRepo, db, redisClient, cfg, channelMonitorSvc, settingRepo)
	if opsService != nil {
		opsService.SetCleanupReloader(svc)
	}
	return svc
}

func ProvideOpsSystemLogSink(opsRepo OpsRepository) *OpsSystemLogSink {
	sink := NewOpsSystemLogSink(opsRepo)
	logger.SetSink(sink)
	return sink
}

// ProvideAuditLogService 创建操作审计日志服务并启动异步写入与保留期清理协程。
// 启动和有序停止由根 Application 的 host.Lifetime 装配。
func ProvideAuditLogService(repo AuditLogRepository, settingService *SettingService) *AuditLogService {
	svc := NewAuditLogService(repo, settingService)
	return svc
}

func buildIdempotencyConfig(cfg *config.Config) IdempotencyConfig {
	idempotencyCfg := DefaultIdempotencyConfig()
	if cfg != nil {
		if cfg.Idempotency.DefaultTTLSeconds > 0 {
			idempotencyCfg.DefaultTTL = time.Duration(cfg.Idempotency.DefaultTTLSeconds) * time.Second
		}
		if cfg.Idempotency.SystemOperationTTLSeconds > 0 {
			idempotencyCfg.SystemOperationTTL = time.Duration(cfg.Idempotency.SystemOperationTTLSeconds) * time.Second
		}
		if cfg.Idempotency.ProcessingTimeoutSeconds > 0 {
			idempotencyCfg.ProcessingTimeout = time.Duration(cfg.Idempotency.ProcessingTimeoutSeconds) * time.Second
		}
		if cfg.Idempotency.FailedRetryBackoffSeconds > 0 {
			idempotencyCfg.FailedRetryBackoff = time.Duration(cfg.Idempotency.FailedRetryBackoffSeconds) * time.Second
		}
		if cfg.Idempotency.MaxStoredResponseLen > 0 {
			idempotencyCfg.MaxStoredResponseLen = cfg.Idempotency.MaxStoredResponseLen
		}
		idempotencyCfg.ObserveOnly = cfg.Idempotency.ObserveOnly
	}
	return idempotencyCfg
}

func ProvideIdempotencyCoordinator(repo IdempotencyRepository, cfg *config.Config) *IdempotencyCoordinator {
	coordinator := NewIdempotencyCoordinator(repo, buildIdempotencyConfig(cfg))
	SetDefaultIdempotencyCoordinator(coordinator)
	return coordinator
}

func ProvideSystemOperationLockService(repo IdempotencyRepository, cfg *config.Config) *SystemOperationLockService {
	return NewSystemOperationLockService(repo, buildIdempotencyConfig(cfg))
}

func ProvideIdempotencyCleanupService(repo IdempotencyRepository, cfg *config.Config) *IdempotencyCleanupService {
	svc := NewIdempotencyCleanupService(repo, cfg)
	return svc
}

// ProvideGeminiCachedContentService creates GeminiCachedContentService and starts its mutation recovery loop.
func ProvideGeminiCachedContentService(repo GeminiCachedContentRepository, compat *GeminiMessagesCompatService, cfg *config.Config) *GeminiCachedContentService {
	svc := NewGeminiCachedContentService(repo, compat, cfg)
	return svc
}

// ProvideScheduledTestService creates ScheduledTestService.
func ProvideScheduledTestService(
	planRepo ScheduledTestPlanRepository, resultRepo ScheduledTestResultRepository,
	accounts AccountRepository, keys *APIKeyService, users UserRepository,
	gateway *OpenAIGatewayService,
) *ScheduledTestService {
	svc := NewScheduledTestService(planRepo, resultRepo)
	svc.accounts, svc.keys, svc.users, svc.gateway = accounts, keys, users, gateway
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if gateway != nil && gateway.settingService != nil {
		_ = gateway.settingService.LoadModelTraceBank(ctx)
	}
	return svc
}

// ProvideScheduledTestRunnerService constructs ScheduledTestRunnerService.
func ProvideScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	svc := NewScheduledTestRunnerService(planRepo, scheduledSvc, accountTestSvc, rateLimitSvc, cfg)
	return svc
}

// ProvideOpsScheduledReportService constructs OpsScheduledReportService.
func ProvideOpsScheduledReportService(
	opsService *OpsService,
	userService *UserService,
	emailService *EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
) *OpsScheduledReportService {
	svc := NewOpsScheduledReportService(opsService, userService, emailService, redisClient, cfg)
	return svc
}

// ProvideAPIKeyAuthCacheInvalidator 提供 API Key 认证缓存失效能力
func ProvideAPIKeyAuthCacheInvalidator(apiKeyService *APIKeyService) APIKeyAuthCacheInvalidator {
	// Start Pub/Sub subscriber for L1 cache invalidation across instances
	return apiKeyService
}

// ProvideImageStorageSettingService 构造异步生图对象存储的后台设置服务。
//
// config.yaml 里的 image_storage 作为回落：后台从未保存过设置时沿用它，
// 使升级前已通过配置文件开启该功能的部署不被打断。
func ProvideImageStorageSettingService(
	settingRepo SettingRepository,
	encryptor SecretEncryptor,
	backup *BackupService,
	factory ImageStorageFactory,
	cfg *config.Config,
) *ImageStorageSettingService {
	if cfg.ImageStorage.Enabled && !cfg.ImageStorage.Active() {
		// 列出具体缺失的键。若这些键其实已在环境变量里设过，说明它们没被读进来，
		// 请确认 setDefaults 中已为其注册默认值（见 config.setEnvReachableDefaults）。
		logger.L().Warn("image_storage.enabled is true in config but object storage is not fully configured; configure it in the admin UI or complete the config file",
			zap.Strings("missing_keys", cfg.ImageStorage.MissingCredentialKeys()))
	}
	return NewImageStorageSettingService(settingRepo, encryptor, backup, factory, cfg.ImageStorage)
}

// ProvideImageTaskService 构造异步图片任务服务。
//
// 对象存储是异步图片任务的启用前提：仅当开关打开且凭证齐全时功能才可用，否则整体禁用
// （handler 返回 404，不创建任务、不写 Redis），从而避免大 base64 结果撑爆 Redis。
// 启用状态由 settings 服务在运行时解析，因此后台改开关后无需重启即可生效。
func ProvideImageTaskService(store ImageTaskStore, settings *ImageStorageSettingService) *ImageTaskService {
	return NewImageTaskServiceWithResolver(store, settings.Resolver(), defaultImageTaskTTL, defaultImageTaskExecutionTimeout)
}

// ProvideBackupService constructs BackupService
func ProvideBackupService(
	settingRepo SettingRepository,
	cfg *config.Config,
	encryptor SecretEncryptor,
	storeFactory BackupObjectStoreFactory,
	dumper DBDumper,
	lockCache LeaderLockCache,
	db *sql.DB,
) *BackupService {
	svc := NewBackupService(settingRepo, cfg, encryptor, storeFactory, dumper)
	svc.SetLeaderLock(lockCache, db)
	return svc
}

// ProvideOpsService constructs OpsService and wires the SettingService-backed quota
// auto-pause cache sink. Mirrors the SetCleanupReloader pattern: OpsService doesn't
// hold a *SettingService reference, but wire injects a tiny callback so writes to
// ops_advanced_settings immediately propagate into the scheduler hot-path cache.
func ProvideOpsService(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	cfg *config.Config,
	accountRepo AccountRepository,
	userRepo UserRepository,
	concurrencyService *ConcurrencyService,
	gatewayService *GatewayService,
	openAIGatewayService *OpenAIGatewayService,
	geminiCompatService *GeminiMessagesCompatService,
	antigravityGatewayService *AntigravityGatewayService,
	systemLogSink *OpsSystemLogSink,
	settingService *SettingService,
	authCacheInvalidationWorker *AuthCacheInvalidationWorker,
	apiKeyService *APIKeyService,
) *OpsService {
	svc := NewOpsService(
		opsRepo,
		settingRepo,
		cfg,
		accountRepo,
		userRepo,
		concurrencyService,
		gatewayService,
		openAIGatewayService,
		geminiCompatService,
		antigravityGatewayService,
		systemLogSink,
	)
	if settingService != nil {
		svc.SetQuotaSchedulingSettingsSink(settingService.SetQuotaSchedulingSettings)
		// Optional warm-up so the first scheduled request after process start observes
		// a populated cache rather than zero defaults. Best-effort, sync-bounded.
		settingService.WarmOpenAIQuotaAutoPauseSettings(context.Background())
	}
	svc.authCacheInvalidationWorker = authCacheInvalidationWorker
	svc.apiKeyService = apiKeyService
	return svc
}

// ProvideOpsIngressRejectAggregator starts the bounded security aggregation
// runtime and attaches it to OpsService, which is the middleware recorder.
func ProvideOpsIngressRejectAggregator(opsRepo OpsRepository, opsService *OpsService) *OpsIngressRejectAggregator {
	repo, ok := opsRepo.(OpsIngressRejectRepository)
	if !ok {
		return nil
	}
	aggregator := NewOpsIngressRejectAggregator(repo)
	opsService.SetIngressRejectAggregator(aggregator)
	return aggregator
}

// ProvideSettingService wires SettingService with group reader and proxy repo.
func ProvideSettingService(scopedKeys ScopedAdminKeyRepository, settingRepo SettingRepository, groupRepo GroupRepository, proxyRepo ProxyRepository, cfg *config.Config, prices *PricingService) *SettingService {
	svc := NewSettingService(settingRepo, cfg)
	svc.scopedAdminKeyRepo = scopedKeys
	svc.SetDefaultSubscriptionGroupReader(groupRepo)
	svc.SetProxyRepository(proxyRepo)
	prices.SetModelSettings(svc)
	if err := svc.LoadForwardedClientIPSettings(context.Background()); err != nil {
		logger.LegacyPrintf("service.setting", "Warning: load forwarded client IP settings failed: %v", err)
	}
	if err := svc.MigrateOpenAIAllowClaudeCodeCodexPluginSetting(context.Background()); err != nil {
		logger.LegacyPrintf("service.setting", "Warning: migrate openai allow Claude Code Codex plugin setting failed: %v", err)
	}
	if err := svc.MigrateCodexBodyFingerprintToSignals(context.Background()); err != nil {
		logger.LegacyPrintf("service.setting", "Warning: migrate codex body fingerprint to signals failed: %v", err)
	}
	if err := svc.MigrateGrokDefaultTextModel(context.Background()); err != nil {
		logger.LegacyPrintf("service.setting", "Warning: migrate Grok default text model failed: %v", err)
	}
	antigravity.SetUserAgentVersionResolver(svc.GetAntigravityUserAgentVersion)
	// enforceCodexIdentityHeaders 是所有 Codex 出站路径共用的纯函数收口点，拿不到 ctx，
	// 故注入无参解析器；解析器内部自带 60s TTL 缓存，热路径不触库。
	SetCodexCanonicalUserAgentResolver(func() string {
		return svc.GetOpenAICodexCanonicalUserAgent(context.Background())
	})
	// Claude CLI 伪装版本号同理：运行期解析（面板手动值 → 后台同步值 → 内置基线），
	// 解析器内部自带 60s TTL 缓存，热路径不触库。
	claude.SetCLIVersionResolver(func() string {
		return svc.GetClaudeCodeClientVersion(context.Background())
	})
	return svc
}

// ProvideBillingCacheService wires BillingCacheService with its RPM dependencies.
func ProvideManagedChannelService(repo ChannelRepository, groupRepo GroupRepository, authCacheInvalidator APIKeyAuthCacheInvalidator, prices *PricingService, cachePubSub ChannelCachePubSub, settings *SettingService) *ChannelService {
	svc := NewChannelService(repo, groupRepo, authCacheInvalidator, prices, cachePubSub)
	svc.SetModelSettings(settings)
	return svc
}

func ProvideBillingCacheService(
	cache BillingCache,
	userRepo UserRepository,
	subRepo UserSubscriptionRepository,
	apiKeyRepo APIKeyRepository,
	rpmCache UserRPMCache,
	rateRepo UserGroupRateRepository,
	cfg *config.Config,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
) *BillingCacheService {
	return NewBillingCacheService(cache, userRepo, subRepo, apiKeyRepo, rpmCache, rateRepo, cfg, userPlatformQuotaRepo)
}

// ProvideAPIKeyService wires APIKeyService and connects rate-limit cache invalidation.
func ProvideAPIKeyService(
	apiKeyRepo APIKeyRepository,
	userRepo UserRepository,
	groupRepo GroupRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache APIKeyCache,
	cfg *config.Config,
	billingCacheService *BillingCacheService,
	concurrencyService *ConcurrencyService,
) *APIKeyService {
	svc := NewAPIKeyService(apiKeyRepo, userRepo, groupRepo, userSubRepo, userGroupRateRepo, cache, cfg)
	svc.SetRateLimitCacheInvalidator(billingCacheService)
	svc.SetConcurrencyService(concurrencyService)
	return svc
}

// ProviderSet is the Wire provider set for all services
var ProviderSet = wire.NewSet(
	ProvideVendorService,
	// Core services
	ProvideAuthService,
	NewPasskeyService,
	NewUserService,
	ProvideLifetimeAPIKeyService,
	ProvideAPIKeyAuthCacheInvalidator,
	ProvideLifetimeAuthCacheInvalidationWorker,
	NewGroupService,
	NewCompositeRouteResolver,
	NewAccountService,
	NewProxyService,
	NewRedeemService,
	ProvideUsageService,
	NewDashboardService,
	ProvideLifetimePricingService,
	NewBillingService,
	ProvideLifetimeBillingCacheService,
	NewAnnouncementService,
	NewAdminService,
	ProvideLifetimeGatewayService,
	ProvideLifetimeOpenAIGatewayService,
	ProvideImageStorageSettingService,
	ProvideImageTaskService,
	ProvideBatchImageModelPricingResolver,
	NewBatchImagePublicService,
	NewBatchImageDownloadService,
	ProvideLifetimeBatchImageCleanupService,
	ProvideLifetimeBatchImageWorkerRuntime,
	wire.Bind(new(AccountRuntimeBlocker), new(*OpenAIGatewayService)),
	ProvideLifetimeOAuthService,
	ProvideClaudeResetCreditService,
	ProvideLifetimeOpenAIOAuthService,
	ProvideOpenAICloudService,
	ProvideLifetimeGrokOAuthService,
	wire.Bind(new(GrokOAuthTokenService), new(*GrokOAuthService)),
	ProvideLifetimeGeminiOAuthService,
	ProvideLifetimeGeminiCachedContentService,
	NewGeminiQuotaService,
	NewCompositeTokenCacheInvalidator,
	wire.Bind(new(TokenCacheInvalidator), new(*CompositeTokenCacheInvalidator)),
	ProvideLifetimeAntigravityOAuthService,
	ProvideOAuthRefreshAPI,
	ProvideGeminiTokenProvider,
	NewGeminiMessagesCompatService,
	ProvideAntigravityTokenProvider,
	ProvideGrokTokenProvider,
	ProvideOpenAITokenProvider,
	ProvideOpenAIQuotaService,
	ProvideLifetimeOpenAIQuotaAutoResetService,
	ProvideGrokQuotaService,
	ProvideCNProviderQuotaService,
	ProvideCNProviderBalanceService,
	ProvideLifetimeCNProviderBalanceCheckService,
	ProvideClaudeTokenProvider,
	NewAntigravityGatewayService,
	ProvideRateLimitService,
	ProvideAccountUsageService,
	ProvideAccountTestService,
	ProvideLifetimeUpstreamBillingProbeService,
	ProvideLifetimeOllamaCloudUsageService,
	ProvideLifetimeOpenCodeGoUsageService,
	ProvideSettingService,
	ProvideLifetimeBackupService,
	ProvideLifetimeOpsSystemLogSink,
	ProvideLifetimeOpsService,
	ProvideLifetimeOpsIngressRejectAggregator,
	ProvideLifetimeAuditLogService,
	ProvideLifetimeOpsMetricsCollector,
	ProvideLifetimeOpsAggregationService,
	ProvideLifetimeOpsAlertEvaluatorService,
	ProvideLifetimeOpsCleanupService,
	ProvideLifetimeOpsScheduledReportService,
	NewEmailService,
	NewNotificationEmailService,
	ProvideLifetimeEmailQueueService,
	NewTurnstileService,
	NewTencentCaptchaService,
	NewAliyunCaptchaService,
	ProvideLifetimeSubscriptionService,
	wire.Bind(new(DefaultSubscriptionAssigner), new(*SubscriptionService)),
	ProvideLifetimeConcurrencyService,
	ProvideLifetimeUserMessageQueueService,
	ProvideLifetimeUsageRecordWorkerPool,
	ProvideLifetimeSchedulerSnapshotService,
	NewIdentityService,
	NewCRSSyncService,
	ProvideBuildStatusService,
	ProvideLifetimeTokenRefreshService,
	wire.Bind(new(GrokOAuthReconciler), new(*TokenRefreshService)),
	ProvideLifetimeAccountExpiryService,
	ProvideLifetimeOpenAICodexVersionSyncService,
	ProvideLifetimeClaudeCodeVersionSyncService,
	ProvideLifetimeProxyExpiryService,
	ProvideLifetimeSubscriptionExpiryService,
	ProvideLifetimeTimingWheelService,
	ProvideLifetimeDashboardAggregationService,
	ProvideLifetimeUsageCleanupService,
	ProvideLifetimeDeferredService,
	NewAntigravityQuotaFetcher,
	NewGrokQuotaFetcher,
	NewUserAttributeService,
	NewUsageCache,
	NewTotpService,
	NewErrorPassthroughService,
	NewTLSFingerprintProfileService,
	ProvideLifetimePluginManager,
	NewDigestSessionStore,
	ProvideIdempotencyCoordinator,
	ProvideSystemOperationLockService,
	ProvideLifetimeIdempotencyCleanupService,
	ProvideScheduledTestService,
	ProvideLifetimeScheduledTestRunnerService,
	NewGroupCapacityService,
	ProvideManagedChannelService,
	wire.Bind(new(ChannelCacheInvalidator), new(*ChannelService)),
	NewModelPricingResolver,
	NewModelPlazaService,
	ProvideLifetimeContentModerationService,
	ProvidePaymentConfigService,
	ProvidePaymentService,
	ProvideLifetimePaymentOrderExpiryService,
	ProvideBalanceNotifyService,
	ProvideChannelMonitorService,
	ProvideLifetimeChannelMonitorRunner,
	NewChannelMonitorQuotaFetcher,
	ProvideChannelMonitorV2Service,
	ProvideLifetimeChannelMonitorV2Aggregator,
	NewChannelMonitorRequestTemplateService,
)

// ProvidePaymentConfigService wraps NewPaymentConfigService to accept the named
// payment.EncryptionKey type instead of raw []byte, avoiding Wire ambiguity.
func ProvidePaymentConfigService(entClient *dbent.Client, settingRepo SettingRepository, key payment.EncryptionKey) *PaymentConfigService {
	return NewPaymentConfigService(entClient, settingRepo, []byte(key))
}

// ProvideBalanceNotifyService creates BalanceNotifyService
func ProvideBalanceNotifyService(emailService *EmailService, settingRepo SettingRepository, accountRepo AccountRepository, notificationEmailService *NotificationEmailService, lifetime *host.Lifetime) *BalanceNotifyService {
	svc := NewBalanceNotifyService(emailService, settingRepo, accountRepo)
	svc.SetNotificationEmailService(notificationEmailService)
	// Final Usage/Cloud settlements may enqueue alerts; keep this owner alive
	// until they finish, and join SMTP before caches, Redis and PG.
	lifetime.Add(host.Component{Name: "BalanceNotifyService", Phase: host.Usage, ShutdownOrder: 40, StartOrder: 15, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	return svc
}

// ProvidePaymentService creates PaymentService and attaches notification email delivery.
func ProvidePaymentService(entClient *dbent.Client, registry *payment.Registry, loadBalancer payment.LoadBalancer, redeemService *RedeemService, subscriptionSvc *SubscriptionService, configService *PaymentConfigService, userRepo UserRepository, groupRepo GroupRepository, notificationEmailService *NotificationEmailService, lifetime *host.Lifetime) *PaymentService {
	svc := NewPaymentService(entClient, registry, loadBalancer, redeemService, subscriptionSvc, configService, userRepo, groupRepo)
	svc.SetNotificationEmailService(notificationEmailService)
	lifetime.Add(host.Component{
		Name: "PaymentNotifications", Phase: host.Producers, StartOrder: 40, ShutdownOrder: 35,
		Start: svc.StartNotifications, Drain: svc.DrainNotifications, Stop: svc.StopNotifications,
	})
	return svc
}

// ProvidePaymentOrderExpiryService constructs PaymentOrderExpiryService.
func ProvidePaymentOrderExpiryService(paymentSvc *PaymentService, lockCache LeaderLockCache, db *sql.DB) *PaymentOrderExpiryService {
	svc := NewPaymentOrderExpiryService(paymentSvc, 60*time.Second)
	svc.SetLeaderLock(lockCache, db)
	return svc
}

// ProvideChannelMonitorService 创建渠道监控服务（CRUD + RunCheck + 用户视图聚合）。
// 加密器复用 wire 中已注入的 SecretEncryptor（AES-256-GCM）。
// settingService gates RunCheck via channel_monitor_enabled + channel_monitor_mode.
func ProvideChannelMonitorService(
	repo ChannelMonitorRepository,
	encryptor SecretEncryptor,
	settingService *SettingService,
) *ChannelMonitorService {
	svc := NewChannelMonitorService(repo, encryptor)
	svc.SetRuntimeReader(settingService)
	return svc
}

// ProvideChannelMonitorRunner 构造渠道监控调度器。
// 通过 SetScheduler 注入回 service 后再 Start，确保启动时加载所有 enabled monitor，
// 后续 CRUD 也能即时同步任务表。Runner.Stop 由 cleanup function 调用。
// settingService 用于 runner 每次 fire 读取功能开关。
// quotaFetcher（账号侧用量聚合）也在此注入：accountUsage/CN 服务在 wire 图中
// 晚于 channelMonitorService 构造，走 setter 注入避免调整既有构造顺序。
func ProvideChannelMonitorRunner(
	svc *ChannelMonitorService,
	settingService *SettingService,
	quotaFetcher *ChannelMonitorQuotaFetcher,
) *ChannelMonitorRunner {
	r := NewChannelMonitorRunner(svc, settingService)
	if svc != nil {
		// Ensure runtime reader is set even if ProvideChannelMonitorService
		// was constructed without settings (tests / alternate providers).
		svc.SetRuntimeReader(settingService)
		svc.SetScheduler(r)
		svc.SetQuotaFetcher(quotaFetcher)
	}
	return r
}

// ProvideChannelMonitorV2Service wires settings for user-facing privacy flags
// (e.g. hide RPM/TPM throughput).
func ProvideChannelMonitorV2Service(repo ChannelMonitorV2Repository, settingService *SettingService) *ChannelMonitorV2Service {
	svc := NewChannelMonitorV2Service(repo)
	svc.SetRuntimeReader(settingService)
	return svc
}

// ProvideChannelMonitorV2Aggregator starts the passive minute-rollup worker.
// Aggregation only runs when channel_monitor_enabled=true and mode=v2 (and V2 config enabled).
// Set CHANNEL_MONITOR_V2_DISABLE_AGGREGATOR=1 to skip Start (local demo with seeded facts).
func ProvideChannelMonitorV2Aggregator(repo ChannelMonitorV2Repository, db *sql.DB, settingService *SettingService) *ChannelMonitorV2Aggregator {
	aggregator := NewChannelMonitorV2Aggregator(repo, db, settingService)
	return aggregator
}

// ProvideClaudeResetCreditService wires the Claude reset query and, with the
// idempotency store and Redis leases, manual redemption.
func ProvideClaudeResetCreditService(accounts AccountRepository, tokens *ClaudeTokenProvider, proxies ProxyRepository, settings *SettingService, idem *IdempotencyCoordinator, locks LeaderLockCache) *ClaudeResetCreditService {
	s := NewClaudeResetCreditService(accounts, tokens, proxies, settings)
	s.ConfigureRedemption(idem, locks)
	return s
}

func ProvideVendorCompatibleOAuthService(repo ProxyRepository, client ClaudeOAuthClient, rdb *redis.Client) *OAuthService {
	return NewOAuthService(repo, client, rdb)
}

func ProvideVendorService(db *sql.DB, client *dbent.Client, accounts AccountRepository, adminAccounts AdminAccountRepository, proxies ProxyRepository, groups GroupRepository, users UserRepository, admin AdminService, settings *SettingService, oauth *OAuthService, openaiOAuth *OpenAIOAuthService, rdb *redis.Client, audit *AuditLogService, accountUsage *AccountUsageService, cfg *config.Config) *VendorService {
	s := NewVendorService(db, client, accounts, proxies, groups, users, admin, settings, oauth, openaiOAuth, rdb, audit, accountUsage, cfg)
	guard := s.NativeSupplyIdentityGuard()
	if repo, ok := accounts.(interface {
		SetSupplyIdentityGuard(NativeSupplyIdentityGuard)
	}); ok {
		repo.SetSupplyIdentityGuard(guard)
	}
	if repo, ok := adminAccounts.(interface {
		SetSupplyIdentityGuard(NativeSupplyIdentityGuard)
	}); ok {
		repo.SetSupplyIdentityGuard(guard)
	}
	return s
}

// Lifetime providers register ownership immediately after construction.
func ProvideLifetimeAPIKeyService(apiKeyRepo APIKeyRepository,
	userRepo UserRepository,
	groupRepo GroupRepository,
	userSubRepo UserSubscriptionRepository,
	userGroupRateRepo UserGroupRateRepository,
	cache APIKeyCache,
	cfg *config.Config,
	billingCacheService *BillingCacheService,
	concurrencyService *ConcurrencyService,
	lifetime *host.Lifetime) *APIKeyService {
	svc := ProvideAPIKeyService(apiKeyRepo, userRepo, groupRepo, userSubRepo, userGroupRateRepo, cache, cfg, billingCacheService, concurrencyService)
	if svc != nil {
		owner := host.OwnBlockingShutdown("APIKeyService", func() error {
			svc.StopAuthCacheInvalidationSubscriber()
			if svc.authCacheL1 != nil {
				svc.authCacheL1.Close()
			}
			if svc.authNegativeCacheL1 != nil {
				svc.authNegativeCacheL1.Close()
			}
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "APIKeyService", ShutdownOrder: 40, Phase: host.Caches, StartOrder: 50, Constructed: true, Start: func(ctx context.Context) error { svc.StartAuthCacheInvalidationSubscriber(ctx); return nil }, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeAuthCacheInvalidationWorker(repo AuthCacheInvalidationOutboxRepository, cache APIKeyCache, apiKeyService *APIKeyService,
	lifetime *host.Lifetime) *AuthCacheInvalidationWorker {
	svc := NewAuthCacheInvalidationWorker(repo, cache, apiKeyService)
	if svc != nil {
		owner := host.OwnBlockingShutdown("AuthCacheInvalidationWorker", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "AuthCacheInvalidationWorker", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimePricingService(cfg *config.Config, remoteClient PricingRemoteClient,
	lifetime *host.Lifetime) (*PricingService, error) {
	svc, err := ProvidePricingService(cfg, remoteClient)
	if err != nil {
		return nil, err
	}
	if svc != nil {
		lifetime.Add(host.Component{Name: "PricingService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 15, Constructed: false, Start: func(ctx context.Context) error {
			if err := svc.Initialize(); err != nil {
				logger.L().Warn("pricing.initialize_degraded", zap.Error(err))
			}
			return svc.Start(ctx)
		}, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc, nil
}
func ProvideLifetimeBillingCacheService(cache BillingCache,
	userRepo UserRepository,
	subRepo UserSubscriptionRepository,
	apiKeyRepo APIKeyRepository,
	rpmCache UserRPMCache,
	rateRepo UserGroupRateRepository,
	cfg *config.Config,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	lifetime *host.Lifetime) *BillingCacheService {
	svc := ProvideBillingCacheService(cache, userRepo, subRepo, apiKeyRepo, rpmCache, rateRepo, cfg, userPlatformQuotaRepo)
	if svc != nil {
		lifetime.Add(host.Component{Name: "BillingCacheService", ShutdownOrder: 30, Phase: host.Caches, StartOrder: 5, DrainBeforeUsage: true, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeGatewayService(accountRepo AccountRepository,
	groupRepo GroupRepository,
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
	identityService *IdentityService,
	httpUpstream HTTPUpstream,
	deferredService *DeferredService,
	claudeTokenProvider *ClaudeTokenProvider,
	sessionLimitCache SessionLimitCache,
	rpmCache RPMCache,
	digestStore *DigestSessionStore,
	settingService *SettingService,
	tlsFPProfileService *TLSFingerprintProfileService,
	channelService *ChannelService,
	resolver *ModelPricingResolver,
	compositeResolver *CompositeRouteResolver,
	balanceNotifyService *BalanceNotifyService,
	userPlatformQuotaRepo UserPlatformQuotaRepository,
	lifetime *host.Lifetime) *GatewayService {
	svc := NewGatewayService(accountRepo, groupRepo, usageLogRepo, usageBillingRepo, userRepo, userSubRepo, userGroupRateRepo, cache, cfg, schedulerSnapshot, concurrencyService, billingService, rateLimitService, billingCacheService, identityService, httpUpstream, deferredService, claudeTokenProvider, sessionLimitCache, rpmCache, digestStore, settingService, tlsFPProfileService, channelService, resolver, compositeResolver, balanceNotifyService, userPlatformQuotaRepo)
	if svc != nil {
		lifetime.Add(host.Component{Name: "GatewayService", ShutdownOrder: 10, Phase: host.Caches, StartOrder: 60, Constructed: true, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpenAIGatewayService(accountRepo AccountRepository,
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
	lifetime *host.Lifetime) *OpenAIGatewayService {
	svc := NewOpenAIGatewayService(accountRepo, usageLogRepo, usageBillingRepo, userRepo, userSubRepo, userGroupRateRepo, cache, cfg, schedulerSnapshot, concurrencyService, billingService, rateLimitService, billingCacheService, httpUpstream, deferredService, openAITokenProvider, grokTokenProvider, resolver, channelService, balanceNotifyService, settingService, userPlatformQuotaRepo)
	if svc != nil {
		lifetime.Add(host.Component{Name: "BPSRecovery", ShutdownOrder: 10, Phase: host.Producers, StartOrder: 40,
			Start: func(ctx context.Context) error { svc.logOpenAIWSModeBootstrap(); return svc.StartBPS403Recovery(ctx) },
			Drain: svc.StopBPS403Recovery, Stop: svc.StopBPS403Recovery})
		images := host.OwnBlockingShutdown("BPSImages", svc.CloseExcelBPSImages, true)
		lifetime.Add(host.Component{Name: "BPSImages", ShutdownOrder: 12, Phase: host.Producers, Constructed: true, RequestStop: images.RequestStop, Stop: images.Stop})
		// These owners are installed later during graph construction or lazily
		// during admitted requests. Do not create a new pool while shutting down.
		lifetime.Add(host.Component{Name: "OpenAICloudSessions", ShutdownOrder: 10, Phase: host.Producers, Constructed: true,
			Drain: func(ctx context.Context) error { return svc.cloudSessions.Drain(ctx) },
			Stop:  func(ctx context.Context) error { return svc.cloudSessions.Stop(ctx) }})
		lifetime.Add(host.Component{Name: "OpenAIWSPool", ShutdownOrder: 15, Phase: host.Producers, Constructed: true,
			Drain: func(ctx context.Context) error { return svc.openaiWSPool.Drain(ctx) },
			Stop:  func(ctx context.Context) error { return svc.openaiWSPool.Stop(ctx) }})
	}
	return svc
}
func ProvideLifetimeBatchImageCleanupService(repo BatchImageRepository, accountRepo AccountRepository, cfg *config.Config,
	lifetime *host.Lifetime) *BatchImageCleanupService {
	svc := ProvideBatchImageCleanupService(repo, accountRepo, cfg)
	if svc != nil {
		owner := host.OwnBlockingShutdown("BatchImageCleanupService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "BatchImageCleanupService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeBatchImageWorkerRuntime(repo BatchImageRepository,
	accountRepo AccountRepository,
	queue BatchImageQueue,
	billingRepo UsageBillingRepository,
	usageLogRepo UsageLogRepository,
	pricing *BatchImageModelPricingResolver,
	authCache APIKeyAuthCacheInvalidator,
	cfg *config.Config,
	lifetime *host.Lifetime) *BatchImageWorkerRuntime {
	svc := ProvideBatchImageWorkerRuntime(repo, accountRepo, queue, billingRepo, usageLogRepo, pricing, authCache, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "BatchImageWorkerRuntime", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOAuthService(repo ProxyRepository, client ClaudeOAuthClient, rdb *redis.Client,
	lifetime *host.Lifetime) *OAuthService {
	svc := ProvideVendorCompatibleOAuthService(repo, client, rdb)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OAuthService", ShutdownOrder: 50, Phase: host.Caches, StartOrder: 12, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpenAIOAuthService(accountRepo AccountRepository, encryptor SecretEncryptor, cfg *config.Config,
	proxyRepo ProxyRepository,
	oauthClient OpenAIOAuthClient,
	privacyClientFactory PrivacyClientFactory,
	redisClient *redis.Client,
	lifetime *host.Lifetime) *OpenAIOAuthService {
	svc := ProvideOpenAIOAuthService(accountRepo, encryptor, cfg, proxyRepo, oauthClient, privacyClientFactory, redisClient)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OpenAIOAuthService", ShutdownOrder: 50, Phase: host.Caches, StartOrder: 12, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeGrokOAuthService(proxyRepo ProxyRepository, oauthClient GrokOAuthClient, cfg *config.Config, redisClient *redis.Client,
	lifetime *host.Lifetime) *GrokOAuthService {
	svc := ProvideGrokOAuthService(proxyRepo, oauthClient, cfg, redisClient)
	if svc != nil {
		lifetime.Add(host.Component{Name: "GrokOAuthService", ShutdownOrder: 50, Phase: host.Caches, StartOrder: 12, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeGeminiOAuthService(proxyRepo ProxyRepository,
	oauthClient GeminiOAuthClient,
	codeAssist GeminiCliCodeAssistClient,
	driveClient geminicli.DriveClient,
	cfg *config.Config,
	lifetime *host.Lifetime) *GeminiOAuthService {
	svc := NewGeminiOAuthService(proxyRepo, oauthClient, codeAssist, driveClient, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "GeminiOAuthService", ShutdownOrder: 50, Phase: host.Caches, StartOrder: 12, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeGeminiCachedContentService(repo GeminiCachedContentRepository, compat *GeminiMessagesCompatService, cfg *config.Config,
	lifetime *host.Lifetime) *GeminiCachedContentService {
	svc := ProvideGeminiCachedContentService(repo, compat, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "GeminiCachedContentService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeAntigravityOAuthService(proxyRepo ProxyRepository,
	lifetime *host.Lifetime) *AntigravityOAuthService {
	svc := NewAntigravityOAuthService(proxyRepo)
	if svc != nil {
		lifetime.Add(host.Component{Name: "AntigravityOAuthService", ShutdownOrder: 50, Phase: host.Caches, StartOrder: 12, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpenAIQuotaAutoResetService(accountRepo AccountRepository,
	quotaService *OpenAIQuotaService,
	rateLimitService *RateLimitService,
	idempotency *IdempotencyCoordinator,
	audit *AuditLogService,
	settingService *SettingService,
	leaderLock LeaderLockCache,
	lifetime *host.Lifetime) *OpenAIQuotaAutoResetService {
	svc := ProvideOpenAIQuotaAutoResetService(accountRepo, quotaService, rateLimitService, idempotency, audit, settingService, leaderLock)
	if svc != nil {
		owner := host.OwnBlockingShutdown("OpenAIQuotaAutoResetService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpenAIQuotaAutoResetService", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeCNProviderBalanceCheckService(accountRepo AccountRepository,
	balanceService *CNProviderBalanceService,
	quotaService *CNProviderQuotaService,
	cfg *config.Config,
	lifetime *host.Lifetime) *CNProviderBalanceCheckService {
	svc := ProvideCNProviderBalanceCheckService(accountRepo, balanceService, quotaService, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "CNProviderBalanceCheckService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeUpstreamBillingProbeService(accountRepo AccountRepository,
	accountTestService *AccountTestService,
	settingService *SettingService,
	lockCache LeaderLockCache,
	db *sql.DB,
	lifetime *host.Lifetime) *UpstreamBillingProbeService {
	svc := ProvideUpstreamBillingProbeService(accountRepo, accountTestService, settingService, lockCache, db)
	if svc != nil {
		lifetime.Add(host.Component{Name: "UpstreamBillingProbeService", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOllamaCloudUsageService(accountRepo AccountRepository,
	httpUpstream HTTPUpstream,
	settingService *SettingService,
	encryptor SecretEncryptor,
	cfg *config.Config,
	lockCache LeaderLockCache,
	db *sql.DB,
	lifetime *host.Lifetime) *OllamaCloudUsageService {
	svc := ProvideOllamaCloudUsageService(accountRepo, httpUpstream, settingService, encryptor, cfg, lockCache, db)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OllamaCloudUsageService", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpenCodeGoUsageService(accountRepo AccountRepository,
	httpUpstream HTTPUpstream,
	settingService *SettingService,
	lockCache LeaderLockCache,
	db *sql.DB,
	lifetime *host.Lifetime) *OpenCodeGoUsageService {
	svc := ProvideOpenCodeGoUsageService(accountRepo, httpUpstream, settingService, lockCache, db)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OpenCodeGoUsageService", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeBackupService(settingRepo SettingRepository,
	cfg *config.Config,
	encryptor SecretEncryptor,
	storeFactory BackupObjectStoreFactory,
	dumper DBDumper,
	lockCache LeaderLockCache,
	db *sql.DB,
	lifetime *host.Lifetime) *BackupService {
	svc := ProvideBackupService(settingRepo, cfg, encryptor, storeFactory, dumper, lockCache, db)
	if svc != nil {
		lifetime.Add(host.Component{Name: "BackupService", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpsSystemLogSink(opsRepo OpsRepository,
	lifetime *host.Lifetime) *OpsSystemLogSink {
	svc := ProvideOpsSystemLogSink(opsRepo)
	if svc != nil {
		owner := host.OwnBlockingShutdown("OpsSystemLogSink", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpsSystemLogSink", ShutdownOrder: 30, Phase: host.Observations, StartOrder: 0, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeOpsService(opsRepo OpsRepository,
	settingRepo SettingRepository,
	cfg *config.Config,
	accountRepo AccountRepository,
	userRepo UserRepository,
	concurrencyService *ConcurrencyService,
	gatewayService *GatewayService,
	openAIGatewayService *OpenAIGatewayService,
	geminiCompatService *GeminiMessagesCompatService,
	antigravityGatewayService *AntigravityGatewayService,
	systemLogSink *OpsSystemLogSink,
	settingService *SettingService,
	authCacheInvalidationWorker *AuthCacheInvalidationWorker,
	apiKeyService *APIKeyService,
	opsMetricsCollector *OpsMetricsCollector,
	lifetime *host.Lifetime) *OpsService {
	svc := ProvideOpsService(opsRepo, settingRepo, cfg, accountRepo, userRepo, concurrencyService, gatewayService, openAIGatewayService, geminiCompatService, antigravityGatewayService, systemLogSink, settingService, authCacheInvalidationWorker, apiKeyService)
	if gatewayService != nil {
		// Bind settings during construction; the host starts telemetry after Ops.
		gatewayService.telemetrySettings = svc
	}
	if svc != nil {
		svc.nodeMetrics = opsMetricsCollector
		owner := host.OwnBlockingShutdown("OpsService", func() error {
			svc.StopRuntimeSettingsRefresh()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpsService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error { svc.StartRuntimeSettingsRefresh(ctx); return nil }, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeOpsIngressRejectAggregator(opsRepo OpsRepository, opsService *OpsService,
	lifetime *host.Lifetime) *OpsIngressRejectAggregator {
	svc := ProvideOpsIngressRejectAggregator(opsRepo, opsService)
	if svc != nil {
		owner := host.OwnBlockingShutdown("OpsIngressRejectAggregator", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpsIngressRejectAggregator", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeAuditLogService(repo AuditLogRepository, settingService *SettingService,
	lifetime *host.Lifetime) *AuditLogService {
	svc := ProvideAuditLogService(repo, settingService)
	if svc != nil {
		owner := host.OwnBlockingShutdown("AuditLogService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "AuditLogService", ShutdownOrder: 30, Phase: host.Observations, StartOrder: 0, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeOpsMetricsCollector(opsRepo OpsRepository,
	settingRepo SettingRepository,
	accountRepo AccountRepository,
	concurrencyService *ConcurrencyService,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	lifetime *host.Lifetime) *OpsMetricsCollector {
	svc := ProvideOpsMetricsCollector(opsRepo, settingRepo, accountRepo, concurrencyService, db, redisClient, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OpsMetricsCollector", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpsAggregationService(opsRepo OpsRepository,
	settingRepo SettingRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	lifetime *host.Lifetime) *OpsAggregationService {
	svc := ProvideOpsAggregationService(opsRepo, settingRepo, db, redisClient, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OpsAggregationService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpsAlertEvaluatorService(opsService *OpsService,
	opsRepo OpsRepository,
	emailService *EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
	proxyRepo ProxyRepository,
	lifetime *host.Lifetime) *OpsAlertEvaluatorService {
	svc := ProvideOpsAlertEvaluatorService(opsService, opsRepo, emailService, redisClient, cfg, proxyRepo)
	if svc != nil {
		owner := host.OwnBlockingShutdown("OpsAlertEvaluatorService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpsAlertEvaluatorService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeOpsCleanupService(opsRepo OpsRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	settingRepo SettingRepository,
	opsService *OpsService,
	lifetime *host.Lifetime) *OpsCleanupService {
	svc := ProvideOpsCleanupService(opsRepo, db, redisClient, cfg, channelMonitorSvc, settingRepo, opsService)
	if svc != nil {
		lifetime.Add(host.Component{Name: "OpsCleanupService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeOpsScheduledReportService(opsService *OpsService,
	userService *UserService,
	emailService *EmailService,
	redisClient *redis.Client,
	cfg *config.Config,
	lifetime *host.Lifetime) *OpsScheduledReportService {
	svc := ProvideOpsScheduledReportService(opsService, userService, emailService, redisClient, cfg)
	if svc != nil {
		owner := host.OwnBlockingShutdown("OpsScheduledReportService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpsScheduledReportService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeEmailQueueService(emailService *EmailService,
	lifetime *host.Lifetime) *EmailQueueService {
	svc := ProvideEmailQueueService(emailService)
	if svc != nil {
		lifetime.Add(host.Component{Name: "EmailQueueService", ShutdownOrder: 60, Phase: host.Producers, StartOrder: 25, Constructed: false, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeSubscriptionService(groupRepo GroupRepository, userSubRepo UserSubscriptionRepository, billingCacheService *BillingCacheService, entClient *dbent.Client, cfg *config.Config,
	lifetime *host.Lifetime) *SubscriptionService {
	svc := NewSubscriptionService(groupRepo, userSubRepo, billingCacheService, entClient, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "SubscriptionService", ShutdownOrder: 50, Phase: host.Producers, StartOrder: 10, Constructed: false, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeConcurrencyService(cache ConcurrencyCache, accountRepo AccountRepository, cfg *config.Config,
	lifetime *host.Lifetime) *ConcurrencyService {
	svc := ProvideConcurrencyService(cache, accountRepo, cfg)
	if svc != nil {
		owner := host.OwnBlockingShutdown("ConcurrencyService", func() error {
			svc.StopSlotCleanupWorker()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "ConcurrencyService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := svc.CleanupStaleProcessSlots(ctx); err != nil {
				logger.L().Warn("concurrency.startup_cleanup", zap.Error(err))
			}
			if cfg != nil {
				svc.StartSlotCleanupWorker(accountRepo, cfg.Gateway.Scheduling.SlotCleanupInterval)
			}
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeUserMessageQueueService(cache UserMsgQueueCache, rpmCache RPMCache, cfg *config.Config,
	lifetime *host.Lifetime) *UserMessageQueueService {
	svc := ProvideUserMessageQueueService(cache, rpmCache, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "UserMessageQueueService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeUsageRecordWorkerPool(cfg *config.Config, db *sql.DB, repo UsageBillingRepository, billing *BillingService, resolver *ModelPricingResolver, groups GroupRepository, cache BillingCache, keys *APIKeyService,
	lifetime *host.Lifetime) *host.UsageWorker {
	svc := ProvideUsageRecordWorkerPool(cfg, db, repo, billing, resolver, groups, cache, keys)
	if svc != nil {
		lifetime.Add(host.Component{Name: "UsageRecordWorkerPool", ShutdownOrder: 30, Phase: host.Usage, StartOrder: 20, Constructed: false, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeSchedulerSnapshotService(cache SchedulerCache,
	outboxRepo SchedulerOutboxRepository,
	accountRepo AccountRepository,
	groupRepo GroupRepository,
	cfg *config.Config,
	lifetime *host.Lifetime) *SchedulerSnapshotService {
	svc := ProvideSchedulerSnapshotService(cache, outboxRepo, accountRepo, groupRepo, cfg)
	if svc != nil {
		owner := host.OwnBlockingShutdown("SchedulerSnapshotService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "SchedulerSnapshotService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeTokenRefreshService(accountRepo AccountRepository,
	oauthService *OAuthService,
	openaiOAuthService *OpenAIOAuthService,
	geminiOAuthService *GeminiOAuthService,
	antigravityOAuthService *AntigravityOAuthService,
	grokOAuthService *GrokOAuthService,
	cacheInvalidator TokenCacheInvalidator,
	schedulerCache SchedulerCache,
	cfg *config.Config,
	tempUnschedCache TempUnschedCache,
	privacyClientFactory PrivacyClientFactory,
	proxyRepo ProxyRepository,
	refreshAPI *OAuthRefreshAPI,
	runtimeBlocker AccountRuntimeBlocker,
	lifetime *host.Lifetime) *TokenRefreshService {
	svc := ProvideTokenRefreshService(accountRepo, oauthService, openaiOAuthService, geminiOAuthService, antigravityOAuthService, grokOAuthService, cacheInvalidator, schedulerCache, cfg, tempUnschedCache, privacyClientFactory, proxyRepo, refreshAPI, runtimeBlocker)
	if svc != nil {
		owner := host.OwnBlockingShutdown("TokenRefreshService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "TokenRefreshService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeAccountExpiryService(accountRepo AccountRepository,
	lifetime *host.Lifetime) *AccountExpiryService {
	svc := ProvideAccountExpiryService(accountRepo)
	if svc != nil {
		owner := host.OwnBlockingShutdown("AccountExpiryService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "AccountExpiryService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeOpenAICodexVersionSyncService(settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
	lifetime *host.Lifetime) *OpenAICodexVersionSyncService {
	svc := ProvideOpenAICodexVersionSyncService(settingRepo, settingService, githubClient)
	if svc != nil {
		owner := host.OwnBlockingShutdown("OpenAICodexVersionSyncService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "OpenAICodexVersionSyncService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeClaudeCodeVersionSyncService(settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
	lifetime *host.Lifetime) *ClaudeCodeVersionSyncService {
	svc := ProvideClaudeCodeVersionSyncService(settingRepo, settingService, githubClient)
	if svc != nil {
		owner := host.OwnBlockingShutdown("ClaudeCodeVersionSyncService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "ClaudeCodeVersionSyncService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeProxyExpiryService(proxyRepo ProxyRepository,
	lifetime *host.Lifetime) *ProxyExpiryService {
	svc := ProvideProxyExpiryService(proxyRepo)
	if svc != nil {
		owner := host.OwnBlockingShutdown("ProxyExpiryService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "ProxyExpiryService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeSubscriptionExpiryService(userSubRepo UserSubscriptionRepository, settingRepo SettingRepository, notificationEmailService *NotificationEmailService, lockCache LeaderLockCache, db *sql.DB,
	lifetime *host.Lifetime) *SubscriptionExpiryService {
	svc := ProvideSubscriptionExpiryService(userSubRepo, settingRepo, notificationEmailService, lockCache, db)
	if svc != nil {
		owner := host.OwnBlockingShutdown("SubscriptionExpiryService", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "SubscriptionExpiryService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeTimingWheelService(lifetime *host.Lifetime) (*TimingWheelService, error) {
	svc, err := ProvideTimingWheelService()
	if err != nil {
		return nil, err
	}
	if svc != nil {
		lifetime.Add(host.Component{Name: "TimingWheelService", Phase: host.Caches, ShutdownOrder: 90, StartOrder: 10, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc, nil
}
func ProvideLifetimeDashboardAggregationService(repo DashboardAggregationRepository, timingWheel *TimingWheelService, lockCache LeaderLockCache, db *sql.DB, cfg *config.Config, settingRepo SettingRepository,
	lifetime *host.Lifetime) *DashboardAggregationService {
	svc := ProvideDashboardAggregationService(repo, timingWheel, lockCache, db, cfg, settingRepo)
	if svc != nil {
		lifetime.Add(host.Component{Name: "DashboardAggregationService", ShutdownOrder: 35, Phase: host.Producers, StartOrder: 50, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeUsageCleanupService(repo UsageCleanupRepository, timingWheel *TimingWheelService, dashboardAgg *DashboardAggregationService, cfg *config.Config,
	lifetime *host.Lifetime) *UsageCleanupService {
	svc := ProvideUsageCleanupService(repo, timingWheel, dashboardAgg, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "UsageCleanupService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeDeferredService(accountRepo AccountRepository, timingWheel *TimingWheelService,
	lifetime *host.Lifetime) *DeferredService {
	svc := ProvideDeferredService(accountRepo, timingWheel)
	if svc != nil {
		lifetime.Add(host.Component{Name: "DeferredService", ShutdownOrder: 20, Phase: host.Caches, StartOrder: 50, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimePluginManager(repo PluginRepository, encryptor SecretEncryptor, cfg *config.Config, hostInfo PluginHostInfo, kvStore PluginKVStore, gateway *OpenAIGatewayService,
	lifetime *host.Lifetime) *PluginManager {
	svc := ProvidePluginManager(repo, encryptor, cfg, hostInfo, kvStore, gateway)
	if svc != nil {
		lifetime.Add(host.Component{Name: "PluginManager", ShutdownOrder: 20, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeIdempotencyCleanupService(repo IdempotencyRepository, cfg *config.Config,
	lifetime *host.Lifetime) *IdempotencyCleanupService {
	svc := ProvideIdempotencyCleanupService(repo, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "IdempotencyCleanupService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeScheduledTestRunnerService(planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
	lifetime *host.Lifetime) *ScheduledTestRunnerService {
	svc := ProvideScheduledTestRunnerService(planRepo, scheduledSvc, accountTestSvc, rateLimitSvc, cfg)
	if svc != nil {
		lifetime.Add(host.Component{Name: "ScheduledTestRunnerService", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeContentModerationService(settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	proxyRepo ProxyRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
	lifetime *host.Lifetime) *ContentModerationService {
	svc := NewContentModerationService(settingRepo, repo, hashCache, groupRepo, userRepo, proxyRepo, authCacheInvalidator, emailService)
	if svc != nil {
		lifetime.Add(host.Component{Name: "ContentModerationService", ShutdownOrder: 40, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: svc.Start, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimePaymentOrderExpiryService(paymentSvc *PaymentService, lockCache LeaderLockCache, db *sql.DB,
	lifetime *host.Lifetime) *PaymentOrderExpiryService {
	svc := ProvidePaymentOrderExpiryService(paymentSvc, lockCache, db)
	if svc != nil {
		lifetime.Add(host.Component{Name: "PaymentOrderExpiryService", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
func ProvideLifetimeChannelMonitorRunner(monitor *ChannelMonitorService,
	settingService *SettingService,
	quotaFetcher *ChannelMonitorQuotaFetcher,
	lifetime *host.Lifetime) *ChannelMonitorRunner {
	svc := ProvideChannelMonitorRunner(monitor, settingService, quotaFetcher)
	if svc != nil {
		owner := host.OwnBlockingShutdown("ChannelMonitorRunner", func() error {
			svc.Stop()
			return nil
		}, true)
		lifetime.Add(host.Component{Name: "ChannelMonitorRunner", ShutdownOrder: 0, Phase: host.Producers, StartOrder: 50, Constructed: false, Start: func(ctx context.Context) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			svc.Start()
			return nil
		}, RequestStop: owner.RequestStop, Stop: owner.Stop})
	}
	return svc
}
func ProvideLifetimeChannelMonitorV2Aggregator(repo ChannelMonitorV2Repository, db *sql.DB, settingService *SettingService,
	lifetime *host.Lifetime) *ChannelMonitorV2Aggregator {
	svc := ProvideChannelMonitorV2Aggregator(repo, db, settingService)
	if svc != nil {
		lifetime.Add(host.Component{Name: "ChannelMonitorV2Aggregator", ShutdownOrder: 30, Phase: host.Producers, StartOrder: 50, Start: svc.Start, RequestStop: svc.RequestStop, Drain: svc.Drain, Stop: svc.Stop})
	}
	return svc
}
