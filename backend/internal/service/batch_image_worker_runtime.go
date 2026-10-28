package service

import (
	"context"
	"errors"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type BatchImageWorkerRuntime struct {
	worker          *BatchImageWorker
	billingRecovery *BatchImageBillingRecoveryService
	cfg             *config.Config

	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewBatchImageWorkerRuntime(worker *BatchImageWorker, cfg *config.Config) *BatchImageWorkerRuntime {
	return &BatchImageWorkerRuntime{worker: worker, cfg: cfg}
}

func ProvideBatchImageWorkerRuntime(
	repo BatchImageRepository,
	accountRepo AccountRepository,
	queue BatchImageQueue,
	billingRepo UsageBillingRepository,
	usageLogRepo UsageLogRepository,
	pricing *BatchImageModelPricingResolver,
	authCache APIKeyAuthCacheInvalidator,
	cfg *config.Config,
) *BatchImageWorkerRuntime {
	processor := &BatchImagePipelineProcessor{
		ProviderProcessor: &BatchImageProviderProcessor{
			Repo:             repo,
			ProviderRegistry: NewBatchImageProviderRegistryFromConfig(cfg),
			AccountResolver:  &BatchImageAccountRepositoryResolver{Repo: accountRepo},
			BillingRepo:      billingRepo,
			AuthCache:        authCache,
		},
		SettlementService: &BatchImageSettlementService{
			Repo:         repo,
			BillingRepo:  billingRepo,
			UsageLogRepo: usageLogRepo,
			Pricing:      pricing,
			AuthCache:    authCache,
			Config:       cfg,
		},
	}
	runtime := NewBatchImageWorkerRuntime(NewBatchImageWorker(queue, processor, NewBatchImageWorkerOptionsFromConfig(cfg)), cfg)
	runtime.billingRecovery = &BatchImageBillingRecoveryService{
		Repo:       repo,
		Billing:    billingRepo,
		AuthCache:  authCache,
		Queue:      queue,
		StaleAfter: NewBatchImageWorkerOptionsFromConfig(cfg).StaleActiveAfter,
		Limit:      NewBatchImageWorkerOptionsFromConfig(cfg).RecoverLimit,
	}
	return runtime
}

func (r *BatchImageWorkerRuntime) Start(parent context.Context) error {
	if r == nil {
		return nil
	}
	if err := parent.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return errors.New("batch image worker stopped")
	}
	if r.cancel != nil {
		return nil
	}
	if r.worker == nil || r.cfg == nil || !r.cfg.BatchImage.QueueEnabled {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		r.worker.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		r.worker.RunDelayedMover(ctx)
	}()
	go func() {
		defer wg.Done()
		r.worker.RunStaleActiveRecovery(ctx)
	}()
	go func() {
		defer wg.Done()
		r.runBillingRecovery(ctx)
	}()
	go func() {
		wg.Wait()
		close(done)
	}()
	return nil
}

func (r *BatchImageWorkerRuntime) runBillingRecovery(ctx context.Context) {
	if r == nil || r.worker == nil || r.billingRecovery == nil {
		return
	}
	interval := r.worker.opts.RecoveryInterval
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		_, _ = r.billingRecovery.ReleaseStaleUnsubmittedOnce(ctx)
		sleepOrDone(ctx, interval)
	}
}

// Drain cancels reservation loops and joins the existing durable queue worker.
// Its established cancellation/requeue/recovery path retains accepted jobs.
func (r *BatchImageWorkerRuntime) Drain(ctx context.Context) error { return r.Stop(ctx) }
func (r *BatchImageWorkerRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.stopped = true
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *BatchImageWorkerRuntime) Running() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel == nil {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}
