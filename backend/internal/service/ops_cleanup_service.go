package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

const (
	opsCleanupJobName = "ops_cleanup"

	opsCleanupLeaderLockKeyDefault = "ops:cleanup:leader"
	opsCleanupLeaderLockTTLDefault = 30 * time.Minute
)

var opsCleanupCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

var opsCleanupReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// OpsCleanupService periodically deletes old ops data to prevent unbounded DB growth.
//
// - Scheduling: 5-field cron spec (minute hour dom month dow).
// - Multi-instance: best-effort Redis leader lock so only one node runs cleanup.
// - Safety: deletes in batches to avoid long transactions.
//
// 附带：在 runCleanupOnce 末尾调用 ChannelMonitorService.RunDailyMaintenance，
// 统一共享 cron schedule + leader lock + heartbeat，避免再引一套调度。
type OpsCleanupService struct {
	opsRepo           OpsRepository
	db                *sql.DB
	redisClient       *redis.Client
	cfg               *config.Config
	channelMonitorSvc *ChannelMonitorService
	settingRepo       SettingRepository

	// 生命周期切换单独串行化；运行任务不获取此锁，等待退出时不占用配置锁。
	scheduleMu sync.Mutex
	cron       *cron.Cron
	cronCancel context.CancelFunc
	started    bool
	stopped    bool

	mu        sync.Mutex
	effective opsCleanupEffectiveConfig

	warnNoRedisOnce sync.Once
}

type opsCleanupEffectiveConfig struct {
	config.OpsCleanupConfig
	SystemLogRetentionDays int
}

func NewOpsCleanupService(
	opsRepo OpsRepository,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
	channelMonitorSvc *ChannelMonitorService,
	settingRepo SettingRepository,
) *OpsCleanupService {
	return &OpsCleanupService{
		opsRepo:           opsRepo,
		db:                db,
		redisClient:       redisClient,
		cfg:               cfg,
		channelMonitorSvc: channelMonitorSvc,
		settingRepo:       settingRepo,
	}
}

// Start 首次启动 cron 调度。Enabled / Schedule 由 effective 配置决定（settings 优先 cfg）。
// 重复调用幂等。
func (s *OpsCleanupService) Start() {
	if s == nil {
		return
	}
	if s.cfg != nil && !s.cfg.Ops.Enabled {
		return
	}
	if s.opsRepo == nil || s.db == nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started (missing deps)")
		return
	}

	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true
	if err := s.applyScheduleLocked(context.Background()); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] not started: %v", err)
	}
}

// Stop 关闭 cron。幂等。
func (s *OpsCleanupService) Stop() {
	if s == nil {
		return
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	s.stopped = true
	if err := s.stopCronLocked(); err != nil {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] %v", err)
	}
}

// stopCronLocked 取消当前代次后等待任务退出；只持调度切换锁，不持配置锁。
// 超时保留句柄供下次关闭重试，禁止启动与旧任务重叠的新代次。
func (s *OpsCleanupService) stopCronLocked() error {
	if s.cron == nil {
		return nil
	}
	if s.cronCancel != nil {
		s.cronCancel()
	}
	ctx := s.cron.Stop()
	timer := time.NewTimer(opsCleanupCronStopTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.cron = nil
		s.cronCancel = nil
		return nil
	case <-timer.C:
		return fmt.Errorf("cron stop timed out; cleanup tasks are still exiting")
	}
}

// applyScheduleLocked 重新计算 effective 配置并按其 schedule 重建 cron。调用方持调度切换锁。
// 若 effective.Enabled=false（用户在 UI 关闭清理），停旧 cron 后直接返回，不创建新 cron。
func (s *OpsCleanupService) applyScheduleLocked(ctx context.Context) error {
	if err := s.stopCronLocked(); err != nil {
		return err
	}
	s.mu.Lock()
	s.computeEffectiveLocked(ctx)
	effective := s.effective
	s.mu.Unlock()

	if !effective.Enabled {
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cron disabled by settings")
		return nil
	}

	schedule := strings.TrimSpace(effective.Schedule)
	if schedule == "" {
		schedule = opsCleanupDefaultSchedule
	}

	loc := time.Local
	if s.cfg != nil && strings.TrimSpace(s.cfg.Timezone) != "" {
		if parsed, err := time.LoadLocation(strings.TrimSpace(s.cfg.Timezone)); err == nil && parsed != nil {
			loc = parsed
		}
	}

	c := cron.New(cron.WithParser(opsCleanupCronParser), cron.WithLocation(loc))
	runCtx, cancel := context.WithCancel(context.Background())
	if _, err := c.AddFunc(schedule, func() { s.runScheduled(runCtx) }); err != nil {
		cancel()
		return fmt.Errorf("invalid schedule %q: %w", schedule, err)
	}
	c.Start()
	s.cron = c
	s.cronCancel = cancel
	logger.LegacyPrintf("service.ops_cleanup",
		"[OpsCleanup] scheduled (schedule=%q tz=%s retention_days=err:%d/system:%d/min:%d/hour:%d)",
		schedule, loc.String(),
		effective.ErrorLogRetentionDays,
		effective.SystemLogRetentionDays,
		effective.MinuteMetricsRetentionDays,
		effective.HourlyMetricsRetentionDays,
	)
	return nil
}

// Reload 重新读取 ops_advanced_settings.data_retention 并按新配置重建 cron。
// 适用于 admin 在 UI 修改清理设置后立即生效（schedule / enabled 改动需要 Reload；
// retention 改动 runScheduled 顶部也会刷新，下一次触发即生效）。
// 若 service 还未 Start 或已 Stop，Reload 不做任何事。
func (s *OpsCleanupService) Reload(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.scheduleMu.Lock()
	defer s.scheduleMu.Unlock()
	if !s.started || s.stopped {
		return nil
	}
	return s.applyScheduleLocked(ctx)
}

// computeEffectiveLocked 计算"生效配置"并写入 s.effective。调用方持锁。
//
// 优先级：UI 写入的 settings.ops_advanced_settings.data_retention（权威）覆盖 cfg.Ops.Cleanup 的副本。
//   - Enabled：settings 直接覆盖
//   - Schedule：settings 非空时覆盖，否则保留 cfg
//   - *RetentionDays：settings >=0 时覆盖（包括 0=TRUNCATE），<0 沿用 cfg
//
// System log retention has a separate owner: settings.ops_runtime_log_config.retention_days.
// Missing or invalid settings fall back independently instead of disabling all cleanup overlays.
func (s *OpsCleanupService) computeEffectiveLocked(ctx context.Context) {
	base := opsCleanupEffectiveConfig{
		SystemLogRetentionDays: defaultOpsRuntimeLogConfig(s.cfg).RetentionDays,
	}
	if s.cfg != nil {
		base.OpsCleanupConfig = s.cfg.Ops.Cleanup
	}
	defer func() { s.effective = base }()

	if s.settingRepo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyOpsAdvancedSettings,
		SettingKeyOpsRuntimeLogConfig,
	})
	if err != nil {
		logger.LegacyPrintf("service.ops_cleanup",
			"[OpsCleanup] read cleanup settings failed, using defaults: %v", err)
		return
	}
	if raw, ok := values[SettingKeyOpsAdvancedSettings]; ok {
		adv := defaultOpsAdvancedSettingsForConfig(s.cfg)
		if err := json.Unmarshal([]byte(raw), adv); err != nil {
			logger.LegacyPrintf("service.ops_cleanup",
				"[OpsCleanup] parse advanced settings failed, using cfg: %v", err)
		} else {
			dr := adv.DataRetention
			base.Enabled = dr.CleanupEnabled
			if sched := strings.TrimSpace(dr.CleanupSchedule); sched != "" {
				base.Schedule = sched
			}
			if dr.ErrorLogRetentionDays >= 0 {
				base.ErrorLogRetentionDays = dr.ErrorLogRetentionDays
			}
			if dr.MinuteMetricsRetentionDays >= 0 {
				base.MinuteMetricsRetentionDays = dr.MinuteMetricsRetentionDays
			}
			if dr.HourlyMetricsRetentionDays >= 0 {
				base.HourlyMetricsRetentionDays = dr.HourlyMetricsRetentionDays
			}
		}
	}
	if raw, ok := values[SettingKeyOpsRuntimeLogConfig]; ok {
		defaults := defaultOpsRuntimeLogConfig(s.cfg)
		runtimeCfg := *defaults
		if err := json.Unmarshal([]byte(raw), &runtimeCfg); err != nil {
			logger.LegacyPrintf("service.ops_cleanup",
				"[OpsCleanup] parse runtime log config failed, using default system-log retention: %v", err)
		} else {
			normalizeOpsRuntimeLogConfig(&runtimeCfg, defaults)
			if runtimeCfg.RetentionDays >= 1 && runtimeCfg.RetentionDays <= 3650 {
				base.SystemLogRetentionDays = runtimeCfg.RetentionDays
			}
		}
	}
}

// snapshotEffective 取一份 effective 副本（runCleanupOnce 等读路径使用）。
func (s *OpsCleanupService) snapshotEffective() opsCleanupEffectiveConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effective
}

// refreshEffectiveBeforeRun 在 cron 触发时刷新 effective，让 retention 改动当次即生效。
// schedule 改动不影响当次（cron 调度由库管理，需要 Reload 才换 schedule）。
func (s *OpsCleanupService) refreshEffectiveBeforeRun(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.computeEffectiveLocked(ctx)
}

func (s *OpsCleanupService) runScheduled(parent context.Context) {
	if s == nil || s.db == nil || s.opsRepo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(parent, opsCleanupRunTimeout)
	defer cancel()

	// 让 retention 改动当次生效（schedule/enabled 改动需要 Reload）。
	s.refreshEffectiveBeforeRun(ctx)
	if ctx.Err() != nil || !s.snapshotEffective().Enabled {
		return
	}

	release, ok := s.tryAcquireLeaderLock(ctx)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	startedAt := time.Now().UTC()
	runAt := startedAt

	counts, err := s.runCleanupOnce(ctx)
	// 管理停用和进程关闭不是清理故障，不再用独立心跳写入延长关闭时间。
	if parent.Err() != nil {
		return
	}
	if err != nil {
		s.recordHeartbeatError(runAt, time.Since(startedAt), err)
		logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] cleanup failed: %v", err)
		return
	}
	s.recordHeartbeatSuccess(runAt, time.Since(startedAt), counts)
	logger.L().Info("[OpsCleanup] cleanup complete",
		zap.String("component", "service.ops_cleanup"),
		zap.String("deleted_counts", counts.String()),
	)
}

func (s *OpsCleanupService) runCleanupOnce(ctx context.Context) (opsCleanupDeletedCounts, error) {
	out := opsCleanupDeletedCounts{}
	if s == nil || s.opsRepo == nil || s.cfg == nil {
		return out, nil
	}

	effective := s.snapshotEffective()
	now := time.Now().UTC()

	targets := []opsCleanupTarget{
		{effective.ErrorLogRetentionDays, "ops_error_logs", &out.errorLogs},
		{effective.ErrorLogRetentionDays, "ops_ingress_reject_aggregates", &out.ingressRejects},
		{effective.ErrorLogRetentionDays, "ops_alert_events", &out.alertEvents},
		{effective.SystemLogRetentionDays, "ops_system_logs", &out.systemLogs},
		{effective.SystemLogRetentionDays, "ops_system_log_cleanup_audits", &out.logAudits},
		{effective.MinuteMetricsRetentionDays, "ops_system_metrics", &out.systemMetrics},
		{effective.HourlyMetricsRetentionDays, "ops_metrics_hourly", &out.hourlyPreagg},
		{effective.HourlyMetricsRetentionDays, "ops_metrics_daily", &out.dailyPreagg},
	}

	// 各表每轮只执行一批，避免首表积压耗尽整轮期限而饿死后续表。
	for len(targets) > 0 {
		pending := targets[:0]
		for _, t := range targets {
			if err := ctx.Err(); err != nil {
				return out, err
			}
			cutoff, truncate, ok := opsCleanupPlan(now, t.retentionDays)
			if !ok {
				continue
			}
			n, err := s.opsRepo.CleanupOpsTable(ctx, t.table, cutoff, truncate, opsCleanupBatchSize)
			*t.counter += n
			if err != nil {
				return out, err
			}
			if !truncate && n == opsCleanupBatchSize {
				pending = append(pending, t)
			}
		}
		targets = pending
	}

	// Channel monitor 每日维护（聚合昨日明细 + 软删过期明细/聚合）。
	// 失败只记日志，不影响 ops 清理的成功状态（与 ops 各步骤风格一致）；
	// 维护本身已经把每步错误打到 slog，heartbeat result 不再分项记录。
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if s.channelMonitorSvc != nil {
		if err := s.channelMonitorSvc.RunDailyMaintenance(ctx); err != nil {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] channel monitor maintenance failed: %v", err)
		}
	}

	return out, ctx.Err()
}

func (s *OpsCleanupService) tryAcquireLeaderLock(ctx context.Context) (func(), bool) {
	if s == nil {
		return nil, false
	}
	// In simple run mode, assume single instance.
	if s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
		return nil, true
	}

	key := opsCleanupLeaderLockKeyDefault
	ttl := opsCleanupLeaderLockTTLDefault

	// Prefer Redis leader lock when available, but avoid stampeding the DB when Redis is flaky by
	// falling back to a DB advisory lock.
	if s.redisClient != nil {
		// 绑定本次租约，旧轮退出不能释放 TTL 后同实例重新取得的锁。
		token := uuid.NewString()
		ok, err := s.redisClient.SetNX(ctx, key, token, ttl).Result()
		if err == nil {
			if !ok {
				return nil, false
			}
			return func() {
				unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_, _ = opsCleanupReleaseScript.Run(unlockCtx, s.redisClient, []string{key}, token).Result()
			}, true
		}
		// Redis error: fall back to DB advisory lock.
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] leader lock SetNX failed; falling back to DB advisory lock: %v", err)
		})
	} else {
		s.warnNoRedisOnce.Do(func() {
			logger.LegacyPrintf("service.ops_cleanup", "[OpsCleanup] redis not configured; using DB advisory lock")
		})
	}

	release, ok := tryAcquireDBAdvisoryLock(ctx, s.db, hashAdvisoryLockID(key))
	if !ok {
		return nil, false
	}
	return release, true
}

func (s *OpsCleanupService) recordHeartbeatSuccess(runAt time.Time, duration time.Duration, counts opsCleanupDeletedCounts) {
	if s == nil || s.opsRepo == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	result := truncateString(counts.String(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupHeartbeatTimeout)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastSuccessAt:  &now,
		LastDurationMs: &durMs,
		LastResult:     &result,
	})
}

func (s *OpsCleanupService) recordHeartbeatError(runAt time.Time, duration time.Duration, err error) {
	if s == nil || s.opsRepo == nil || err == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := truncateString(err.Error(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsCleanupHeartbeatTimeout)
	defer cancel()
	_ = s.opsRepo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsCleanupJobName,
		LastRunAt:      &runAt,
		LastErrorAt:    &now,
		LastError:      &msg,
		LastDurationMs: &durMs,
	})
}
