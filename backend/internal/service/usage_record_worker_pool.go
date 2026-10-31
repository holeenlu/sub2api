package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
)

func usageRecordPoolOptionsFromConfig(cfg *config.Config) host.UsageWorkerOptions {
	opts := host.DefaultUsageWorkerOptions()
	if cfg == nil {
		return opts
	}
	if cfg.Gateway.UsageRecord.WorkerCount > 0 {
		opts.WorkerCount = cfg.Gateway.UsageRecord.WorkerCount
	}
	if cfg.Gateway.UsageRecord.QueueSize > 0 {
		opts.QueueSize = cfg.Gateway.UsageRecord.QueueSize
	}
	if cfg.Gateway.UsageRecord.TaskTimeoutSeconds > 0 {
		opts.TaskTimeout = time.Duration(cfg.Gateway.UsageRecord.TaskTimeoutSeconds) * time.Second
	}
	if policy := strings.TrimSpace(strings.ToLower(cfg.Gateway.UsageRecord.OverflowPolicy)); policy != "" {
		opts.OverflowPolicy = policy
	}
	if cfg.Gateway.UsageRecord.OverflowSamplePercent >= 0 {
		opts.OverflowSamplePercent = cfg.Gateway.UsageRecord.OverflowSamplePercent
	}
	opts.AutoScaleEnabled = cfg.Gateway.UsageRecord.AutoScaleEnabled
	if cfg.Gateway.UsageRecord.AutoScaleMinWorkers > 0 {
		opts.AutoScaleMinWorkers = cfg.Gateway.UsageRecord.AutoScaleMinWorkers
	}
	if cfg.Gateway.UsageRecord.AutoScaleMaxWorkers > 0 {
		opts.AutoScaleMaxWorkers = cfg.Gateway.UsageRecord.AutoScaleMaxWorkers
	}
	if cfg.Gateway.UsageRecord.AutoScaleUpQueuePercent > 0 {
		opts.AutoScaleUpPercent = cfg.Gateway.UsageRecord.AutoScaleUpQueuePercent
	}
	if cfg.Gateway.UsageRecord.AutoScaleDownQueuePercent >= 0 {
		opts.AutoScaleDownPercent = cfg.Gateway.UsageRecord.AutoScaleDownQueuePercent
	}
	if cfg.Gateway.UsageRecord.AutoScaleUpStep > 0 {
		opts.AutoScaleUpStep = cfg.Gateway.UsageRecord.AutoScaleUpStep
	}
	if cfg.Gateway.UsageRecord.AutoScaleDownStep > 0 {
		opts.AutoScaleDownStep = cfg.Gateway.UsageRecord.AutoScaleDownStep
	}
	if cfg.Gateway.UsageRecord.AutoScaleCheckIntervalSeconds > 0 {
		opts.AutoScaleInterval = time.Duration(cfg.Gateway.UsageRecord.AutoScaleCheckIntervalSeconds) * time.Second
	}
	if cfg.Gateway.UsageRecord.AutoScaleCooldownSeconds >= 0 {
		opts.AutoScaleCooldown = time.Duration(cfg.Gateway.UsageRecord.AutoScaleCooldownSeconds) * time.Second
	}
	return host.NormalizeUsageWorkerOptions(opts)
}

// ProvideUsageRecordWorkerPool is the current production assembly boundary.
// Recovery receives only a bounded batch callback; host never interprets money
// or stores unjournaled billing commands. Application startup and ordered
// drain/stop consume the host worker directly; this provider starts no workers.
func ProvideUsageRecordWorkerPool(cfg *config.Config, db *sql.DB, repo UsageBillingRepository, billing *BillingService, resolver *ModelPricingResolver, groups GroupRepository, cache BillingCache, keys *APIKeyService) *host.UsageWorker {
	opts := usageRecordPoolOptionsFromConfig(cfg)
	if cfg != nil && cfg.Database.MaxOpenConns > 0 {
		opts.ExecutionLimit = max(1, cfg.Database.MaxOpenConns/2)
	}
	if db != nil {
		opts.DatabaseStats = db.Stats
	}
	// Configure the existing journal's Native recovery calculator before the
	// host starts this worker. This facade has only the original calculator;
	// frozen preparation cannot fetch live accounts, policy or prices.
	if nativeRepo, ok := repo.(NativeIntentFinalizationRepository); ok {
		_, nativeErr := NewNativeIntentFinalizationService(nativeRepo, &OpenAIGatewayService{billingService: billing})
		if nativeErr != nil {
			// A malformed recovery dependency remains visible on the original
			// worker; it must never become a second best-effort queue.
			opts.Recover = func(context.Context) error { return nativeErr }
			return host.NewUsageWorker(opts)
		}
		if systemOneRepo, ok := repo.(interface {
			ConfigureSystemOneIntentPreparation(func(context.Context, *SystemOneUsagePreparation) (*UsageBillingCommand, *UsagePricingRequest, error))
		}); ok {
			systemOne := &GatewayService{billingService: billing}
			systemOneRepo.ConfigureSystemOneIntentPreparation(func(ctx context.Context, input *SystemOneUsagePreparation) (*UsageBillingCommand, *UsagePricingRequest, error) {
				return systemOne.PrepareSystemOneUsage(ctx, *input)
			})
		}
	}
	if recovery, ok := repo.(UsageBillingRecoveryRepository); ok {
		recovery.ConfigureUsageBillingRecovery(usageBillingRepricer(billing, resolver, groups), usageBillingCacheInvalidator(cache, keys))
		opts.Recover = func(ctx context.Context) error {
			_, err := recovery.RecoverPendingUsageBilling(ctx, 16, nil)
			return err
		}
	}
	pool := host.NewUsageWorker(opts)
	return pool
}
