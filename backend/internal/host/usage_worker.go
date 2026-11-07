package host

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/alitto/pond/v2"
	"go.uber.org/zap"
)

var ErrUsageWorkerStopped = errors.New("usage worker is draining or stopped")

// UsageWorker owns the recovery queue, execution limit, periodic producers and
// their join handles. Tasks must honor context cancellation. A shutdown timeout
// means ownership is retained until they return, not that Go can kill the task.
// Run is mandatory finalization of an already admitted request, never admission
// of a new generation. Stop ingress (including upgraded connections) before Drain.
type UsageWorker struct {
	options               UsageWorkerOptions
	pool                  pond.Pool
	executionSlots        chan struct{}
	taskTimeout           time.Duration
	overflowPolicy        string
	overflowSamplePercent int
	overflowCounter       atomic.Uint64
	inlineTasks           atomic.Uint64
	inlineCompleted       atomic.Uint64
	lateInlineTasks       atomic.Uint64
	executionWaitNanos    atomic.Uint64
	droppedQueueFull      atomic.Uint64
	droppedPoolStopped    atomic.Uint64
	syncFallback          atomic.Uint64
	lastDropLogNanos      atomic.Int64
	autoScaleMinWorkers   int
	autoScaleMaxWorkers   int
	autoScaleUpPercent    int
	autoScaleDownPercent  int
	autoScaleUpStep       int
	autoScaleDownStep     int
	autoScaleCooldown     time.Duration
	lastScaleNanos        atomic.Int64
	previousDBWaitCount   int64 // owned by the single periodic loop
	recoveryActive        atomic.Bool

	// mu linearizes acceptance, accepted task counts and drain. No callbacks run
	// under it. Pond TrySubmit is nonblocking; it is inside the admission boundary.
	mu           sync.Mutex
	accepting    bool
	started      bool
	pending      int
	idle         chan struct{}
	producerDone chan struct{}
	halt         chan struct{}
	poolDone     <-chan struct{}
	workCtx      context.Context
	cancelWork   context.CancelFunc
}

// NewUsageWorker constructs resources without starting any goroutine. Explicit
// Submit/Run can execute work; Start enables periodic recovery and autoscaling.
func NewUsageWorker(opts UsageWorkerOptions) *UsageWorker {
	opts = NormalizeUsageWorkerOptions(opts)
	limit := opts.ExecutionLimit
	if limit <= 0 {
		limit = opts.WorkerCount
		if opts.AutoScaleEnabled {
			limit = opts.AutoScaleMaxWorkers
		}
	}
	limit = max(1, limit)
	opts.WorkerCount = min(opts.WorkerCount, limit)
	opts.AutoScaleMinWorkers = min(opts.AutoScaleMinWorkers, limit)
	opts.AutoScaleMaxWorkers = min(opts.AutoScaleMaxWorkers, limit)
	ctx, cancel := context.WithCancel(context.Background())
	idle, done := make(chan struct{}), make(chan struct{})
	close(idle)
	close(done)
	return &UsageWorker{
		options: opts, pool: pond.NewPool(opts.WorkerCount, pond.WithQueueSize(opts.QueueSize)),
		executionSlots: make(chan struct{}, limit), taskTimeout: opts.TaskTimeout,
		overflowPolicy: opts.OverflowPolicy, overflowSamplePercent: opts.OverflowSamplePercent,
		autoScaleMinWorkers: opts.AutoScaleMinWorkers, autoScaleMaxWorkers: opts.AutoScaleMaxWorkers,
		autoScaleUpPercent: opts.AutoScaleUpPercent, autoScaleDownPercent: opts.AutoScaleDownPercent,
		autoScaleUpStep: opts.AutoScaleUpStep, autoScaleDownStep: opts.AutoScaleDownStep,
		autoScaleCooldown: opts.AutoScaleCooldown,
		accepting:         true, idle: idle, producerDone: done, halt: make(chan struct{}), workCtx: ctx, cancelWork: cancel,
	}
}

// Start attaches periodic work to the process lifetime, never a request context.
// Repeated starts are idempotent. A drained worker cannot be restarted.
func (p *UsageWorker) Start(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return ErrUsageWorkerStopped
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.accepting {
		return ErrUsageWorkerStopped
	}
	if p.started {
		return nil
	}
	if p.pending != 0 {
		return errors.New("start usage worker before accepting work")
	}
	p.cancelWork()
	p.workCtx, p.cancelWork = context.WithCancel(ctx)
	p.started = true
	p.producerDone = make(chan struct{})
	go p.produce(ctx, p.producerDone)
	return nil
}

func (p *UsageWorker) produce(ctx context.Context, done chan struct{}) {
	defer close(done)
	var scale, recoverTick <-chan time.Time
	if p.options.AutoScaleEnabled {
		ticker := time.NewTicker(p.options.AutoScaleInterval)
		defer ticker.Stop()
		scale = ticker.C
	}
	if p.options.Recover != nil {
		ticker := time.NewTicker(p.options.RecoveryInterval)
		defer ticker.Stop()
		recoverTick = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			p.beginDrain()
			p.cancelWork()
			return
		case <-p.halt:
			return
		case <-scale:
			p.autoScaleTick()
		case <-recoverTick:
			if !p.recoveryActive.CompareAndSwap(false, true) {
				continue
			}
			mode := p.Submit(func(taskCtx context.Context) {
				defer p.recoveryActive.Store(false)
				if err := p.options.Recover(taskCtx); err != nil {
					logger.L().Warn("billing.recovery_failed", zap.Error(err))
				}
			})
			if mode.Dropped() {
				p.recoveryActive.Store(false)
			}
		}
	}
}

// Submit owns a task only if it returns Enqueued or Sync. Explicit drop/sample
// policies apply to this recovery/observation queue, never to first settlement.
func (p *UsageWorker) Submit(task UsageTask) SubmitMode {
	if p == nil || p.pool == nil || task == nil {
		return SubmitModeDropped
	}
	p.mu.Lock()
	if !p.accepting {
		p.mu.Unlock()
		p.droppedPoolStopped.Add(1)
		p.logDrop("stopped")
		return SubmitModeDroppedStopped
	}
	p.addPendingLocked()
	_, ok := p.pool.TrySubmitErr(func() error {
		defer p.complete()
		return p.execute(p.workCtx, task)
	})
	if ok {
		p.mu.Unlock()
		return SubmitModeEnqueued
	}
	fallback := p.overflowPolicy == OverflowSync || (p.overflowPolicy == OverflowSample && p.shouldSyncFallback())
	if !fallback {
		p.completeLocked()
	}
	p.mu.Unlock()
	if fallback {
		defer p.complete()
		p.syncFallback.Add(1)
		_ = p.execute(p.workCtx, task) // execute logs at the worker boundary
		return SubmitModeSync
	}
	p.droppedQueueFull.Add(1)
	p.logDrop("full")
	return SubmitModeDropped
}

func (p *UsageWorker) addPendingLocked() {
	if p.pending == 0 {
		p.idle = make(chan struct{})
	}
	p.pending++
}
func (p *UsageWorker) completeLocked() {
	p.pending--
	if p.pending == 0 {
		close(p.idle)
	}
}
func (p *UsageWorker) complete() { p.mu.Lock(); defer p.mu.Unlock(); p.completeLocked() }

// Run synchronously completes the first monetary attempt before returning. Even
// during shutdown an admitted request must not lose its final usage: late calls
// run on the caller's stack with a fresh bounded context and are counted apart.
// Their caller owns the join; final assembly must join ingress before Drain.
func (p *UsageWorker) Run(task UsageTask) {
	if p == nil || p.pool == nil || task == nil {
		return
	}
	p.inlineTasks.Add(1)
	defer p.inlineCompleted.Add(1)
	p.mu.Lock()
	owned := p.accepting
	if owned {
		p.addPendingLocked()
	}
	p.mu.Unlock()
	if owned {
		defer p.complete()
	} else {
		p.lateInlineTasks.Add(1)
	}
	p.settleSynchronously(task)
}

// settleSynchronously deliberately does not use the recovery cancellation
// context. Before this first call there may be no durable journal record. Spend
// the full existing task timeout AFTER gaining capacity, as before migration.
// Its caller remains joined to this work; bounded Stop reports unfinished inline
// work rather than cancelling away the only copy of its billing evidence.
func (p *UsageWorker) settleSynchronously(task UsageTask) {
	start := time.Now()
	p.executionSlots <- struct{}{}
	p.executionWaitNanos.Add(uint64(time.Since(start)))
	defer func() { <-p.executionSlots }()
	ctx, cancel := context.WithTimeout(context.Background(), p.taskTimeout)
	defer cancel()
	_ = invokeUsageTask(ctx, task)
}

func (p *UsageWorker) execute(parent context.Context, task UsageTask) error {
	ctx, cancel := context.WithTimeout(parent, p.taskTimeout)
	defer cancel()
	start := time.Now()
	select {
	case p.executionSlots <- struct{}{}:
		defer func() { <-p.executionSlots }()
	case <-ctx.Done():
		// Recovery already has a durable owner. Invoke its cancellation path so
		// callbacks release their claim/in-flight bookkeeping; it must honor ctx.
	}
	p.executionWaitNanos.Add(uint64(time.Since(start)))
	return invokeUsageTask(ctx, task)
}

func invokeUsageTask(ctx context.Context, task UsageTask) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("usage task panic: %v", recovered)
			logger.L().Error("usage_record.task_panic", zap.Error(err))
		}
	}()
	task(ctx)
	return ctx.Err()
}

func (p *UsageWorker) beginDrain() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.accepting {
		return
	}
	p.accepting = false
	close(p.halt)
	// Pond owns this join future. The host never starts a detached Wait goroutine.
	p.poolDone = p.pool.Stop().Done()
}

// Drain synchronously closes queue admission, stops producers and waits for all
// accepted queued AND inline work. A deadline leaves those tasks owned here;
// Stop cancels them and a subsequent call can finish joining them.
func (p *UsageWorker) Drain(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return nil
	}
	p.beginDrain()
	return p.join(ctx)
}

// Stop cancels recovery work and joins all owned work within the supplied bound.
// First settlements retain their bounded independent context until journaled; a
// timeout reports incomplete ownership instead of discarding monetary evidence.
func (p *UsageWorker) Stop(ctx context.Context) error {
	if p == nil || p.pool == nil {
		return nil
	}
	p.beginDrain()
	p.cancelWork()
	return p.join(ctx)
}
func (p *UsageWorker) join(ctx context.Context) error {
	p.mu.Lock()
	waits := []<-chan struct{}{p.producerDone, p.poolDone, p.idle}
	p.mu.Unlock()
	for _, done := range waits {
		select {
		case <-done:
			continue
		default:
		}
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	p.cancelWork()
	return nil
}

// Stats includes caller-owned late finalization explicitly, not as queue intake.
func (p *UsageWorker) Stats() UsageWorkerStats {
	if p == nil || p.pool == nil {
		return UsageWorkerStats{}
	}
	return UsageWorkerStats{
		ExecutionLimit: cap(p.executionSlots), ExecutionInFlight: len(p.executionSlots),
		ExecutionWaitMilliseconds: p.executionWaitNanos.Load() / uint64(time.Millisecond),
		InlineTasks:               p.inlineTasks.Load(), InlineCompleted: p.inlineCompleted.Load(), LateInlineTasks: p.lateInlineTasks.Load(),
		MaxConcurrency: p.pool.MaxConcurrency(), RunningWorkers: p.pool.RunningWorkers(), WaitingTasks: p.pool.WaitingTasks(),
		SubmittedTasks: p.pool.SubmittedTasks(), CompletedTasks: p.pool.CompletedTasks(), SuccessfulTasks: p.pool.SuccessfulTasks(),
		FailedTasks: p.pool.FailedTasks(), DroppedTasks: p.pool.DroppedTasks(), DroppedQueueFull: p.droppedQueueFull.Load(),
		DroppedPoolStopped: p.droppedPoolStopped.Load(), SyncFallbackTasks: p.syncFallback.Load(),
	}
}
