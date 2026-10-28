//go:build wireinject
// +build wireinject

package main

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"log"
	"net/http"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

func initializeApplication(buildInfo handler.BuildInfo, lifetime *host.Lifetime) (*Application, error) {
	wire.Build(
		// Infrastructure layer ProviderSets
		config.ProviderSet,

		// Business layer ProviderSets
		repository.ProviderSet,
		service.ProviderSet,
		securityaudit.ProviderSet,
		payment.ProviderSet,
		middleware.ProviderSet,
		handler.ProviderSet,

		// Server layer ProviderSet
		server.ProviderSet,

		// Privacy client factory for OpenAI training opt-out
		providePrivacyClientFactory,

		// BuildInfo provider
		provideServiceBuildInfo,
		providePluginHostInfo,

		// Cleanup function provider
		provideBackgroundRoots,
		provideEntBinding,
		provideInfrastructure,
		provideApplication,
	)
	return nil, nil
}

func providePrivacyClientFactory() service.PrivacyClientFactory {
	return repository.CreatePrivacyReqClient
}

func provideServiceBuildInfo(buildInfo handler.BuildInfo) service.BuildInfo {
	return service.BuildInfo{
		UpstreamVersion: buildInfo.UpstreamVersion,
		BuildCommit:     buildInfo.BuildCommit,
		Version:         buildInfo.Version,
		BuildType:       buildInfo.BuildType,
	}
}

func providePluginHostInfo(buildInfo handler.BuildInfo) service.PluginHostInfo {
	return service.PluginHostInfo{
		Version:   buildInfo.Version,
		BuildType: buildInfo.BuildType,
	}
}

func provideBackgroundRoots(
	entClient *ent.Client,
	rdb *redis.Client,
	opsMetricsCollector *service.OpsMetricsCollector,
	opsAggregation *service.OpsAggregationService,
	opsAlertEvaluator *service.OpsAlertEvaluatorService,
	opsCleanup *service.OpsCleanupService,
	opsScheduledReport *service.OpsScheduledReportService,
	opsSystemLogSink *service.OpsSystemLogSink,
	opsService *service.OpsService,
	opsIngressReject *service.OpsIngressRejectAggregator,
	apiKeyService *service.APIKeyService,
	authCacheInvalidationWorker *service.AuthCacheInvalidationWorker,
	schedulerSnapshot *service.SchedulerSnapshotService,
	tokenRefresh *service.TokenRefreshService,
	accountExpiry *service.AccountExpiryService,
	cnProviderBalanceCheck *service.CNProviderBalanceCheckService,
	codexVersionSync *service.OpenAICodexVersionSyncService,
	claudeCodeVersionSync *service.ClaudeCodeVersionSyncService,
	proxyExpiry *service.ProxyExpiryService,
	subscriptionExpiry *service.SubscriptionExpiryService,
	usageCleanup *service.UsageCleanupService,
	idempotencyCleanup *service.IdempotencyCleanupService,
	geminiCachedContent *service.GeminiCachedContentService,
	batchImageCleanup *service.BatchImageCleanupService,
	batchImageWorker *service.BatchImageWorkerRuntime,
	pricing *service.PricingService,
	emailQueue *service.EmailQueueService,
	billingCache *service.BillingCacheService,
	usageRecordWorkerPool *host.UsageWorker,
	subscriptionService *service.SubscriptionService,
	oauth *service.OAuthService,
	openaiOAuth *service.OpenAIOAuthService,
	geminiOAuth *service.GeminiOAuthService,
	antigravityOAuth *service.AntigravityOAuthService,
	grokOAuth *service.GrokOAuthService,
	openAIGateway *service.OpenAIGatewayService,
	accountTelemetry *service.GatewayService,
	scheduledTestRunner *service.ScheduledTestRunnerService,
	backupSvc *service.BackupService,
	paymentOrderExpiry *service.PaymentOrderExpiryService,
	channelMonitorRunner *service.ChannelMonitorRunner,
	channelMonitorV2Aggregator *service.ChannelMonitorV2Aggregator,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	ollamaCloudUsage *service.OllamaCloudUsageService,
	opencodeGoUsage *service.OpenCodeGoUsageService,
	auditLog *service.AuditLogService,
	openAIAutoReset *service.OpenAIQuotaAutoResetService,
	promptAudit *securityaudit.PromptService,
	pluginManager *service.PluginManager,

	contentModeration *service.ContentModerationService,
	concurrency *service.ConcurrencyService,
	userQueue *service.UserMessageQueueService,
	timingWheel *service.TimingWheelService,
	deferred *service.DeferredService,
	dashboard *service.DashboardAggregationService,
	lifetime *host.Lifetime,
) *backgroundRoots {
	return &backgroundRoots{lifetime: lifetime}
}

type backgroundRoots struct{ lifetime *host.Lifetime }
type entBinding struct{}
type infrastructure struct{}

func provideEntBinding(client *ent.Client, lifetime *host.Lifetime) *entBinding {
	owner := host.OwnBlockingShutdown("PostgreSQL", client.Close, true)
	lifetime.Add(host.Component{Name: "PostgreSQL", ShutdownOrder: 10, Phase: host.Infrastructure, Constructed: true, RequestStop: owner.RequestStop, Stop: owner.Stop})
	return &entBinding{}
}
func provideInfrastructure(_ *entBinding, rdb *redis.Client, lifetime *host.Lifetime) *infrastructure {
	owner := host.OwnBlockingShutdown("Redis", rdb.Close, true)
	lifetime.Add(host.Component{Name: "Redis", Phase: host.Infrastructure, Constructed: true, RequestStop: owner.RequestStop, Stop: owner.Stop})
	return &infrastructure{}
}
func provideApplication(_ *infrastructure, roots *backgroundRoots, server *http.Server, prompt *securityaudit.PromptService, cloud *service.OpenAICloudService) *Application {
	roots.lifetime.Add(host.Component{Name: "PromptAudit", ShutdownOrder: 40, Phase: host.Producers, StartOrder: 80, Start: func(ctx context.Context) error {
		if err := prompt.Start(ctx); err != nil {
			log.Printf("Prompt Audit started in degraded state: %v", err)
		}
		return nil
	}, Stop: prompt.Shutdown})
	ingress := newRequestOwners()
	server.Handler = ingress.handler(server.Handler)
	return &Application{Server: server, Cloud: cloud, lifetime: roots.lifetime, ingress: ingress}
}
