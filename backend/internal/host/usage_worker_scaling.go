package host

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
	"time"
)

func (p *UsageWorker) autoScaleTick() {
	if p == nil || p.pool == nil || p.pool.Stopped() {
		return
	}
	queueSize := p.pool.QueueSize()
	if queueSize <= 0 {
		return
	}
	current := p.pool.MaxConcurrency()
	waiting := int(p.pool.WaitingTasks())
	running := int(p.pool.RunningWorkers())
	if current <= 0 || waiting < 0 {
		return
	}
	queuePercent := waiting * 100 / queueSize
	runningPercent := 0
	if current > 0 {
		runningPercent = running * 100 / current
	}

	now := time.Now()
	lastScaleNanos := p.lastScaleNanos.Load()
	if lastScaleNanos > 0 && now.Sub(time.Unix(0, lastScaleNanos)) < p.autoScaleCooldown {
		return
	}

	// 扩容优先：队列占用率超过阈值时，按步长提升并发上限。
	dbSaturated := false
	if p.options.DatabaseStats != nil {
		stats := p.options.DatabaseStats()
		dbSaturated = stats.WaitCount > p.previousDBWaitCount || (stats.MaxOpenConnections > 0 && stats.InUse >= stats.MaxOpenConnections)
		p.previousDBWaitCount = stats.WaitCount
	}
	if !dbSaturated && queuePercent >= p.autoScaleUpPercent && current < p.autoScaleMaxWorkers {
		target := current + p.autoScaleUpStep
		if target > p.autoScaleMaxWorkers {
			target = p.autoScaleMaxWorkers
		}
		p.resizePool(current, target, queuePercent, waiting, runningPercent, queueSize, "scale_up")
		return
	}

	// 缩容：仅在队列为空且运行利用率低时收缩，避免高负载下“无排队误缩容”导致震荡。
	if queuePercent <= p.autoScaleDownPercent && waiting == 0 &&
		runningPercent <= p.autoScaleDownPercent &&
		current > p.autoScaleMinWorkers {
		target := current - p.autoScaleDownStep
		if target < p.autoScaleMinWorkers {
			target = p.autoScaleMinWorkers
		}
		p.resizePool(current, target, queuePercent, waiting, runningPercent, queueSize, "scale_down")
	}
}

func (p *UsageWorker) resizePool(current, target, queuePercent, waiting, runningPercent, queueSize int, action string) {
	if target == current {
		return
	}
	p.pool.Resize(target)
	p.lastScaleNanos.Store(time.Now().UnixNano())

	logger.L().With(
		zap.String("component", "host.usage_worker"),
		zap.String("action", action),
		zap.Int("from_workers", current),
		zap.Int("to_workers", target),
		zap.Int("queue_percent", queuePercent),
		zap.Int("waiting_tasks", waiting),
		zap.Int("running_percent", runningPercent),
		zap.Int("queue_size", queueSize),
	).Info("usage_record.auto_scale")
}

func (p *UsageWorker) shouldSyncFallback() bool {
	if p.overflowSamplePercent <= 0 {
		return false
	}
	n := p.overflowCounter.Add(1)
	return int((n-1)%100) < p.overflowSamplePercent
}

func (p *UsageWorker) logDrop(reason string) {
	now := time.Now().UnixNano()
	last := p.lastDropLogNanos.Load()
	if now-last < int64(usageRecordDropLogInterval) {
		return
	}
	if !p.lastDropLogNanos.CompareAndSwap(last, now) {
		return
	}

	stats := p.Stats()
	logger.L().With(
		zap.String("component", "host.usage_worker"),
		zap.String("reason", reason),
		zap.String("overflow_policy", p.overflowPolicy),
		zap.Int64("running_workers", stats.RunningWorkers),
		zap.Uint64("waiting_tasks", stats.WaitingTasks),
		zap.Uint64("dropped_queue_full", stats.DroppedQueueFull),
		zap.Uint64("dropped_pool_stopped", stats.DroppedPoolStopped),
		zap.Uint64("sync_fallback_tasks", stats.SyncFallbackTasks),
	).Warn("usage_record.task_dropped")
}
