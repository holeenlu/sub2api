package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const (
	defaultDashboardAggregationTimeout         = 2 * time.Minute
	defaultDashboardAggregationBackfillTimeout = 30 * time.Minute
	dashboardAggregationRetentionInterval      = 6 * time.Hour

	// dashboardAggregationLeaderLockKey 保证多副本部署中每个周期只有一个实例执行聚合。
	dashboardAggregationLeaderLockKey = "dashboard:aggregation:leader"
	// TTL 必须覆盖 dashboard 聚合与分组日汇总两个有界阶段，避免任务中途失锁。
	dashboardAggregationLeaderLockTTL = 5 * time.Minute

	// 启动回填耗时可能远长于周期聚合，因此使用独立锁并让 TTL 严格大于回填超时。
	dashboardAggregationGroupUsageBackfillLeaderLockKey = "dashboard:aggregation:group-usage-backfill:leader"
	dashboardAggregationGroupUsageBackfillLeaderLockTTL = defaultDashboardAggregationBackfillTimeout + time.Minute
)

var (
	// ErrDashboardBackfillDisabled 当配置禁用回填时返回。
	ErrDashboardBackfillDisabled = errors.New("仪表盘聚合回填已禁用")
	// ErrDashboardBackfillTooLarge 当回填跨度超过限制时返回。
	ErrDashboardBackfillTooLarge   = errors.New("回填时间跨度过大")
	errDashboardAggregationRunning = errors.New("聚合作业正在运行")
)

// DashboardAggregationRepository 定义仪表盘预聚合仓储接口。
type DashboardAggregationRepository interface {
	AggregateRange(ctx context.Context, start, end time.Time) error
	// RecomputeRange 重新计算指定时间范围内的聚合数据（包含活跃用户等派生表）。
	// 设计目的：当 usage_logs 被批量删除/回滚后，确保聚合表可恢复一致性。
	RecomputeRange(ctx context.Context, start, end time.Time) error
	GetAggregationWatermark(ctx context.Context) (time.Time, error)
	UpdateAggregationWatermark(ctx context.Context, aggregatedAt time.Time) error
	CleanupAggregates(ctx context.Context, hourlyCutoff, dailyCutoff time.Time) error
	CleanupUsageLogs(ctx context.Context, cutoff time.Time) error
	CleanupUsageBillingDedup(ctx context.Context, cutoff time.Time) error
	EnsureUsageLogsPartitions(ctx context.Context, now time.Time) error
}

// DashboardAggregationService 负责定时聚合与回填。
type DashboardAggregationService struct {
	repo                 DashboardAggregationRepository
	timingWheel          *TimingWheelService
	cfg                  config.DashboardAggregationConfig
	settingRepo          SettingRepository
	running              int32
	work                 backgroundWork
	startOnce            sync.Once
	startErr             error
	lastRetentionCleanup atomic.Value // time.Time

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

// NewDashboardAggregationService 创建聚合服务。
func NewDashboardAggregationService(repo DashboardAggregationRepository, timingWheel *TimingWheelService, cfg *config.Config) *DashboardAggregationService {
	var aggCfg config.DashboardAggregationConfig
	if cfg != nil {
		aggCfg = cfg.DashboardAgg
	}
	return &DashboardAggregationService{
		repo:        repo,
		timingWheel: timingWheel,
		cfg:         aggCfg,
		instanceID:  uuid.NewString(),
	}
}

// SetLeaderLock injects the leader-lock cache and DB used to elect a single
// instance for the periodic scheduled aggregation. When both are nil the job runs
// ungated (single-instance / test behavior).
func (s *DashboardAggregationService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

// Start 启动定时聚合作业（重启生效配置）。
func (s *DashboardAggregationService) Start(ctx context.Context) error {
	if s == nil || s.repo == nil || s.timingWheel == nil {
		return nil
	}
	if err := s.work.bind(ctx, s.RequestStop); err != nil {
		return err
	}
	owner, done, err := s.work.begin()
	if err != nil {
		return err
	}
	defer done() // Own scheduling and startup handoff, not just the children.
	s.startOnce.Do(func() {
		if !s.cfg.Enabled {
			s.startErr = s.timingWheel.ScheduleRecurring("dashboard:retention", time.Minute, s.runScheduledRetention)
			return
		}
		interval := time.Duration(s.cfg.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = time.Minute
		}
		if err := s.timingWheel.ScheduleRecurring("dashboard:aggregation", interval, s.runScheduledAggregation); err != nil {
			s.startErr = err
			return
		}
		if err := s.work.launch(s.runStartupGroupUsageSyncOwned); err != nil {
			s.startErr = err
			return
		}
		if s.cfg.RecomputeDays > 0 {
			if err := s.work.launch(s.recomputeRecentDaysOwned); err != nil {
				s.startErr = err
				return
			}
		}
	})
	if owner.Err() != nil {
		s.timingWheel.Cancel("dashboard:aggregation")
		s.timingWheel.Cancel("dashboard:retention")
		return context.Cause(owner)
	}
	return s.startErr
}
func (s *DashboardAggregationService) RequestStop() {
	if s == nil {
		return
	}
	s.work.requestStop()
	if s.timingWheel != nil {
		s.timingWheel.Cancel("dashboard:aggregation")
		s.timingWheel.Cancel("dashboard:retention")
	}
}
func (s *DashboardAggregationService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.RequestStop()
	workErr := s.work.join(ctx)
	var err error
	if s.timingWheel != nil {
		err = errors.Join(s.timingWheel.CancelAndWait(ctx, "dashboard:aggregation"), s.timingWheel.CancelAndWait(ctx, "dashboard:retention"))
	}
	return errors.Join(workErr, err)
}
func (s *DashboardAggregationService) Drain(ctx context.Context) error { return s.Stop(ctx) }

// TriggerBackfill 触发回填（异步）。
func (s *DashboardAggregationService) TriggerBackfill(start, end time.Time) error {
	if s == nil || s.repo == nil {
		return errors.New("聚合服务未初始化")
	}
	if !s.cfg.BackfillEnabled {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填被拒绝: backfill_enabled=false")
		return ErrDashboardBackfillDisabled
	}
	if !end.After(start) {
		return errors.New("回填时间范围无效")
	}
	if s.cfg.BackfillMaxDays > 0 {
		maxRange := time.Duration(s.cfg.BackfillMaxDays) * 24 * time.Hour
		if end.Sub(start) > maxRange {
			return ErrDashboardBackfillTooLarge
		}
	}

	return s.work.launch(func(parent context.Context) {
		ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationBackfillTimeout)
		defer cancel()
		if err := s.backfillRange(ctx, start, end); err != nil {
			logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填失败: %v", err)
		}
	})
}

// TriggerRecomputeRange 触发指定范围的重新计算（异步）。
// 与 TriggerBackfill 不同：
// - 不依赖 backfill_enabled（这是内部一致性修复）
// - 不更新 watermark（避免影响正常增量聚合游标）
func (s *DashboardAggregationService) TriggerRecomputeRange(start, end time.Time) error {
	if s == nil || s.repo == nil {
		return errors.New("聚合服务未初始化")
	}
	if !s.cfg.Enabled {
		return errors.New("聚合服务已禁用")
	}
	if !end.After(start) {
		return errors.New("重新计算时间范围无效")
	}

	return s.work.launch(func(parent context.Context) {
		const maxRetries = 3
		for i := 0; i < maxRetries; i++ {
			ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationBackfillTimeout)
			err := s.recomputeRange(ctx, start, end)
			cancel()
			if err == nil {
				return
			}
			if !errors.Is(err, errDashboardAggregationRunning) {
				logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 重新计算失败: %v", err)
				return
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case <-timer.C:
			case <-parent.Done():
				timer.Stop()
				return
			}
		}
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 重新计算放弃: 聚合作业持续占用")
	})
}

func (s *DashboardAggregationService) recomputeRecentDays() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	s.recomputeRecentDaysOwned(parent)
}
func (s *DashboardAggregationService) recomputeRecentDaysOwned(parent context.Context) {
	days := s.cfg.RecomputeDays
	if days <= 0 {
		return
	}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -days)

	ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationBackfillTimeout)
	defer cancel()
	if err := s.backfillRange(ctx, start, now); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 启动重算失败: %v", err)
		return
	}
}

func (s *DashboardAggregationService) recomputeRange(ctx context.Context, start, end time.Time) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return errDashboardAggregationRunning
	}
	defer atomic.StoreInt32(&s.running, 0)

	jobStart := time.Now().UTC()
	if err := s.repo.RecomputeRange(ctx, start, end); err != nil {
		return err
	}
	logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 重新计算完成 (start=%s end=%s duration=%s)",
		start.UTC().Format(time.RFC3339),
		end.UTC().Format(time.RFC3339),
		time.Since(jobStart).String(),
	)
	return nil
}

func (s *DashboardAggregationService) runScheduledAggregation() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	s.runScheduledAggregationOwned(parent)
}
func (s *DashboardAggregationService) runScheduledAggregationOwned(parent context.Context) {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)

	jobStart := time.Now().UTC()
	ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationTimeout)
	defer cancel()

	// Multi-instance guard: only the leader runs the periodic aggregation; peers
	// skip this cycle to avoid N× redundant GROUP BY queries and watermark races.
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, dashboardAggregationLeaderLockKey, s.instanceID, dashboardAggregationLeaderLockTTL)
	if !ok {
		return
	}
	defer release()
	defer s.runScheduledGroupUsageSyncOwned(parent)

	now := time.Now().UTC()
	last, err := s.repo.GetAggregationWatermark(ctx)
	if err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 读取水位失败: %v", err)
		last = time.Unix(0, 0).UTC()
	}

	lookback := time.Duration(s.cfg.LookbackSeconds) * time.Second
	epoch := time.Unix(0, 0).UTC()
	start := last.Add(-lookback)
	if !last.After(epoch) {
		retentionDays := s.cfg.Retention.UsageLogsDays
		if retentionDays <= 0 {
			retentionDays = 1
		}
		start = truncateToDayUTC(now.AddDate(0, 0, -retentionDays))
	} else if start.After(now) {
		start = now.Add(-lookback)
	}

	if err := s.aggregateRange(ctx, start, now); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 聚合失败: %v", err)
		return
	}

	updateErr := s.repo.UpdateAggregationWatermark(ctx, now)
	if updateErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 更新水位失败: %v", updateErr)
	}
	slog.Debug("[DashboardAggregation] 聚合完成",
		"start", start.Format(time.RFC3339),
		"end", now.Format(time.RFC3339),
		"duration", time.Since(jobStart).String(),
		"watermark_updated", updateErr == nil,
	)

	s.maybeCleanupRetention(ctx, now)
}

func (s *DashboardAggregationService) runScheduledGroupUsageSync() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	s.runScheduledGroupUsageSyncOwned(parent)
}
func (s *DashboardAggregationService) runScheduledGroupUsageSyncOwned(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationTimeout)
	defer cancel()
	if err := s.syncGroupUsageRollups(ctx, time.Now().UTC()); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 分组用量日汇总失败: %v", err)
	}
}

func (s *DashboardAggregationService) runStartupGroupUsageSync() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	s.runStartupGroupUsageSyncOwned(parent)
}
func (s *DashboardAggregationService) runStartupGroupUsageSyncOwned(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationBackfillTimeout)
	defer cancel()
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, dashboardAggregationGroupUsageBackfillLeaderLockKey, s.instanceID, dashboardAggregationGroupUsageBackfillLeaderLockTTL)
	if !ok {
		return
	}
	defer release()
	if err := s.syncGroupUsageRollups(ctx, time.Now().UTC()); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 启动分组用量回填失败: %v", err)
	}
}

func (s *DashboardAggregationService) syncGroupUsageRollups(ctx context.Context, now time.Time) error {
	repo, ok := s.repo.(GroupUsageRollupRepository)
	if !ok {
		return nil
	}
	return repo.SyncGroupUsageRollups(ctx, GroupUsageTodayStart(now))
}

func (s *DashboardAggregationService) backfillRange(ctx context.Context, start, end time.Time) error {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return errDashboardAggregationRunning
	}
	defer atomic.StoreInt32(&s.running, 0)

	jobStart := time.Now().UTC()
	startUTC := start.UTC()
	endUTC := end.UTC()
	if !endUTC.After(startUTC) {
		return errors.New("回填时间范围无效")
	}

	cursor := truncateToDayUTC(startUTC)
	for cursor.Before(endUTC) {
		windowEnd := cursor.Add(24 * time.Hour)
		if windowEnd.After(endUTC) {
			windowEnd = endUTC
		}
		if err := s.aggregateRange(ctx, cursor, windowEnd); err != nil {
			return err
		}
		cursor = windowEnd
	}

	updateErr := s.repo.UpdateAggregationWatermark(ctx, endUTC)
	if updateErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 更新水位失败: %v", updateErr)
	}
	logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 回填聚合完成 (start=%s end=%s duration=%s watermark_updated=%t)",
		startUTC.Format(time.RFC3339),
		endUTC.Format(time.RFC3339),
		time.Since(jobStart).String(),
		updateErr == nil,
	)

	s.maybeCleanupRetention(ctx, endUTC)
	return nil
}

func (s *DashboardAggregationService) aggregateRange(ctx context.Context, start, end time.Time) error {
	if !end.After(start) {
		return nil
	}
	if err := s.repo.EnsureUsageLogsPartitions(ctx, end); err != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 分区检查失败: %v", err)
	}
	return s.repo.AggregateRange(ctx, start, end)
}

func (s *DashboardAggregationService) maybeCleanupRetention(ctx context.Context, now time.Time) {
	lastAny := s.lastRetentionCleanup.Load()
	if lastAny != nil {
		if last, ok := lastAny.(time.Time); ok && now.Sub(last) < dashboardAggregationRetentionInterval {
			return
		}
	}

	usageDays, err := s.requestRetentionDays(ctx)
	if err != nil {
		// Never fall back to a shorter window after a settings read failure.
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 读取日志保留设置失败，跳过清理: %v", err)
		return
	}
	hourlyCutoff := now.AddDate(0, 0, -s.cfg.Retention.HourlyDays)
	dailyCutoff := now.AddDate(0, 0, -s.cfg.Retention.DailyDays)
	usageCutoff := now.AddDate(0, 0, -usageDays)
	dedupDays := s.cfg.Retention.UsageBillingDedupDays
	if dedupDays <= 0 {
		dedupDays = 365
	}
	dedupDays = max(dedupDays, usageDays)
	dedupCutoff := now.AddDate(0, 0, -dedupDays)

	var aggErr, usageErr, dedupErr error
	if s.cfg.Enabled {
		aggErr = s.repo.CleanupAggregates(ctx, hourlyCutoff, dailyCutoff)
	}
	if aggErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] 聚合保留清理失败: %v", aggErr)
	}
	if usageDays > 0 {
		usageErr = s.repo.CleanupUsageLogs(ctx, usageCutoff)
	}
	if usageErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] usage_logs 保留清理失败: %v", usageErr)
	}
	if usageDays > 0 {
		dedupErr = s.repo.CleanupUsageBillingDedup(ctx, dedupCutoff)
	}
	if dedupErr != nil {
		logger.LegacyPrintf("service.dashboard_aggregation", "[DashboardAggregation] usage_billing_dedup 保留清理失败: %v", dedupErr)
	}
	if aggErr == nil && usageErr == nil && dedupErr == nil {
		s.lastRetentionCleanup.Store(now)
	}
}

func (s *DashboardAggregationService) requestRetentionDays(ctx context.Context) (int, error) {
	days := s.cfg.Retention.UsageLogsDays
	if !s.cfg.Enabled {
		days = 0
	}
	if s.settingRepo == nil {
		return days, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOpsRuntimeLogConfig)
	if errors.Is(err, ErrSettingNotFound) {
		return days, nil
	}
	if err != nil {
		return 0, err
	}
	var cfg struct {
		RequestRetentionDays *int `json:"request_retention_days"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return 0, err
	}
	if cfg.RequestRetentionDays != nil {
		days = *cfg.RequestRetentionDays
		if days < 0 || days > 3650 {
			return 0, fmt.Errorf("invalid request_retention_days: %d", days)
		}
	}
	return days, nil
}

func (s *DashboardAggregationService) runScheduledRetention() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	s.runScheduledRetentionOwned(parent)
}
func (s *DashboardAggregationService) runScheduledRetentionOwned(parent context.Context) {
	if !atomic.CompareAndSwapInt32(&s.running, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&s.running, 0)
	ctx, cancel := context.WithTimeout(parent, defaultDashboardAggregationTimeout)
	defer cancel()
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, dashboardAggregationLeaderLockKey, s.instanceID, dashboardAggregationLeaderLockTTL)
	if !ok {
		return
	}
	defer release()
	s.maybeCleanupRetention(ctx, time.Now().UTC())
}

func truncateToDayUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
