package host

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	defaultUsageRecordWorkerCount        = 128
	defaultUsageRecordQueueSize          = 16384
	defaultUsageRecordTaskTimeoutSeconds = 5
	// 默认 sync：溢出时提交方内联执行，保证计费任务不被静默丢弃（issue #3656）。
	defaultUsageRecordOverflowPolicy       = OverflowSync
	defaultUsageRecordOverflowSampleRatio  = 10
	defaultUsageRecordAutoScaleEnabled     = true
	defaultUsageRecordAutoScaleMinWorkers  = 128
	defaultUsageRecordAutoScaleMaxWorkers  = 512
	defaultUsageRecordAutoScaleUpPercent   = 70
	defaultUsageRecordAutoScaleDownPercent = 15
	defaultUsageRecordAutoScaleUpStep      = 32
	defaultUsageRecordAutoScaleDownStep    = 16
	defaultUsageRecordAutoScaleInterval    = 3 * time.Second
	defaultUsageRecordAutoScaleCooldown    = 10 * time.Second
	usageRecordDropLogInterval             = 5 * time.Second
)

// UsageTask 是提交到使用量记录池的任务。
// 任务实现应自行处理业务错误日志；池本身只负责调度与超时控制。
type UsageTask func(ctx context.Context)

// SubmitMode 表示任务提交结果。
type SubmitMode string

const (
	SubmitModeEnqueued SubmitMode = "enqueued"
	SubmitModeDropped  SubmitMode = "dropped"
	// SubmitModeDroppedStopped 表示任务因池已停止（进程关停窗口）被丢弃。
	// 与显式 drop/sample 溢出策略的丢弃区分开：溢出丢弃是运维显式配置的取舍，
	// 而关停窗口丢弃不是，计费关键任务应在调用侧降级为同步执行兜底。
	SubmitModeDroppedStopped SubmitMode = "dropped_stopped"
	SubmitModeSync           SubmitMode = "sync_fallback"
)

// Dropped 报告任务是否未被执行（入队失败且未同步执行）。
func (m SubmitMode) Dropped() bool {
	return m == SubmitModeDropped || m == SubmitModeDroppedStopped
}

// UsageWorkerOptions 使用量记录池配置。
type UsageWorkerOptions struct {
	// Recover retries already durable billing facts; it never receives an in-memory monetary closure.
	Recover               func(context.Context) error
	RecoveryInterval      time.Duration
	ExecutionLimit        int // Shared by queued recovery work and synchronous first attempts.
	DatabaseStats         func() sql.DBStats
	WorkerCount           int
	QueueSize             int
	TaskTimeout           time.Duration
	OverflowPolicy        string
	OverflowSamplePercent int
	AutoScaleEnabled      bool
	AutoScaleMinWorkers   int
	AutoScaleMaxWorkers   int
	AutoScaleUpPercent    int
	AutoScaleDownPercent  int
	AutoScaleUpStep       int
	AutoScaleDownStep     int
	AutoScaleInterval     time.Duration
	AutoScaleCooldown     time.Duration
}

// UsageWorkerStats 使用量记录池运行时统计。
type UsageWorkerStats struct {
	ExecutionLimit            int
	ExecutionInFlight         int
	ExecutionWaitMilliseconds uint64
	InlineTasks               uint64
	InlineCompleted           uint64
	LateInlineTasks           uint64
	MaxConcurrency            int
	RunningWorkers            int64
	WaitingTasks              uint64
	SubmittedTasks            uint64
	CompletedTasks            uint64
	SuccessfulTasks           uint64
	FailedTasks               uint64
	DroppedTasks              uint64
	DroppedQueueFull          uint64
	DroppedPoolStopped        uint64
	SyncFallbackTasks         uint64
}

const (
	OverflowSync   = "sync"
	OverflowDrop   = "drop"
	OverflowSample = "sample"
)

// NormalizeUsageWorkerOptions is the single owner of worker sizing and overflow defaults.
func NormalizeUsageWorkerOptions(opts UsageWorkerOptions) UsageWorkerOptions {
	if opts.RecoveryInterval <= 0 {
		opts.RecoveryInterval = 5 * time.Second
	}
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = defaultUsageRecordWorkerCount
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultUsageRecordQueueSize
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = time.Duration(defaultUsageRecordTaskTimeoutSeconds) * time.Second
	}
	switch strings.ToLower(strings.TrimSpace(opts.OverflowPolicy)) {
	case OverflowDrop,
		OverflowSample,
		OverflowSync:
		opts.OverflowPolicy = strings.ToLower(strings.TrimSpace(opts.OverflowPolicy))
	default:
		opts.OverflowPolicy = defaultUsageRecordOverflowPolicy
	}
	if opts.OverflowSamplePercent < 0 {
		opts.OverflowSamplePercent = 0
	}
	if opts.OverflowSamplePercent > 100 {
		opts.OverflowSamplePercent = 100
	}
	if opts.OverflowPolicy == OverflowSample && opts.OverflowSamplePercent == 0 {
		opts.OverflowSamplePercent = defaultUsageRecordOverflowSampleRatio
	}
	if opts.AutoScaleEnabled {
		if opts.AutoScaleMinWorkers <= 0 {
			opts.AutoScaleMinWorkers = defaultUsageRecordAutoScaleMinWorkers
		}
		if opts.AutoScaleMaxWorkers <= 0 {
			opts.AutoScaleMaxWorkers = defaultUsageRecordAutoScaleMaxWorkers
		}
		if opts.AutoScaleMaxWorkers < opts.AutoScaleMinWorkers {
			opts.AutoScaleMaxWorkers = opts.AutoScaleMinWorkers
		}
		if opts.WorkerCount < opts.AutoScaleMinWorkers {
			opts.WorkerCount = opts.AutoScaleMinWorkers
		}
		if opts.WorkerCount > opts.AutoScaleMaxWorkers {
			opts.WorkerCount = opts.AutoScaleMaxWorkers
		}
		if opts.AutoScaleUpPercent <= 0 || opts.AutoScaleUpPercent > 100 {
			opts.AutoScaleUpPercent = defaultUsageRecordAutoScaleUpPercent
		}
		if opts.AutoScaleDownPercent < 0 || opts.AutoScaleDownPercent >= 100 {
			opts.AutoScaleDownPercent = defaultUsageRecordAutoScaleDownPercent
		}
		if opts.AutoScaleDownPercent >= opts.AutoScaleUpPercent {
			opts.AutoScaleDownPercent = max(0, opts.AutoScaleUpPercent/2)
		}
		if opts.AutoScaleUpStep <= 0 {
			opts.AutoScaleUpStep = defaultUsageRecordAutoScaleUpStep
		}
		if opts.AutoScaleDownStep <= 0 {
			opts.AutoScaleDownStep = defaultUsageRecordAutoScaleDownStep
		}
		if opts.AutoScaleInterval <= 0 {
			opts.AutoScaleInterval = defaultUsageRecordAutoScaleInterval
		}
		if opts.AutoScaleCooldown < 0 {
			opts.AutoScaleCooldown = defaultUsageRecordAutoScaleCooldown
		}
	} else {
		opts.AutoScaleMinWorkers = opts.WorkerCount
		opts.AutoScaleMaxWorkers = opts.WorkerCount
	}
	return opts
}

func (m SubmitMode) String() string {
	return string(m)
}

func (s UsageWorkerStats) String() string {
	return fmt.Sprintf("running=%d waiting=%d submitted=%d dropped=%d", s.RunningWorkers, s.WaitingTasks, s.SubmittedTasks, s.DroppedTasks)
}

// DefaultUsageWorkerOptions returns the existing process sizing and policy defaults.
func DefaultUsageWorkerOptions() UsageWorkerOptions {
	return NormalizeUsageWorkerOptions(UsageWorkerOptions{
		WorkerCount: defaultUsageRecordWorkerCount, QueueSize: defaultUsageRecordQueueSize,
		TaskTimeout:    time.Duration(defaultUsageRecordTaskTimeoutSeconds) * time.Second,
		OverflowPolicy: defaultUsageRecordOverflowPolicy, OverflowSamplePercent: defaultUsageRecordOverflowSampleRatio,
		AutoScaleEnabled: defaultUsageRecordAutoScaleEnabled, AutoScaleMinWorkers: defaultUsageRecordAutoScaleMinWorkers,
		AutoScaleMaxWorkers: defaultUsageRecordAutoScaleMaxWorkers, AutoScaleUpPercent: defaultUsageRecordAutoScaleUpPercent,
		AutoScaleDownPercent: defaultUsageRecordAutoScaleDownPercent, AutoScaleUpStep: defaultUsageRecordAutoScaleUpStep,
		AutoScaleDownStep: defaultUsageRecordAutoScaleDownStep, AutoScaleInterval: defaultUsageRecordAutoScaleInterval,
		AutoScaleCooldown: defaultUsageRecordAutoScaleCooldown,
	})
}
