package service

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

const (
	opsMetricsCollectorJobName     = "ops_metrics_collector"
	opsMetricsCollectorMinInterval = 60 * time.Second
	opsMetricsCollectorMaxInterval = 1 * time.Hour

	opsMetricsCollectorTimeout = 10 * time.Second

	opsMetricsCollectorLeaderLockKey = "ops:metrics:collector:leader"
	opsMetricsCollectorLeaderLockTTL = 90 * time.Second

	opsMetricsCollectorHeartbeatTimeout = 2 * time.Second

	bytesPerMB = 1024 * 1024
)

var opsMetricsCollectorAdvisoryLockID = hashAdvisoryLockID(opsMetricsCollectorLeaderLockKey)

type opsSchedulableAccountLoadRepository interface {
	ListSchedulableAccountLoads(ctx context.Context) ([]AccountWithConcurrency, error)
}

type OpsMetricsCollector struct {
	opsRepo     OpsRepository
	settingRepo SettingRepository
	cfg         *config.Config

	accountRepo        AccountRepository
	concurrencyService *ConcurrencyService

	db          *sql.DB
	redisClient *redis.Client
	instanceID  string

	nodeStats               atomic.Pointer[OpsNodeMetricsSnapshot]
	nodesEnabled            atomic.Bool
	lastCgroupCPUUsageNanos uint64
	lastCgroupCPUSampleAt   time.Time

	stopCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once

	skipLogMu sync.Mutex
	skipLogAt time.Time
}

func NewOpsMetricsCollector(
	opsRepo OpsRepository,
	settingRepo SettingRepository,
	accountRepo AccountRepository,
	concurrencyService *ConcurrencyService,
	db *sql.DB,
	redisClient *redis.Client,
	cfg *config.Config,
) *OpsMetricsCollector {
	return &OpsMetricsCollector{
		opsRepo:            opsRepo,
		settingRepo:        settingRepo,
		cfg:                cfg,
		accountRepo:        accountRepo,
		concurrencyService: concurrencyService,
		db:                 db,
		redisClient:        redisClient,
		instanceID:         uuid.NewString(),
	}
}

func (c *OpsMetricsCollector) Start() {
	if c == nil {
		return
	}
	c.startOnce.Do(func() {
		if c.stopCh == nil {
			c.stopCh = make(chan struct{})
		}
		go c.sampleResources()
		go c.run()
	})
}

func (c *OpsMetricsCollector) Stop() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() {
		if c.stopCh != nil {
			close(c.stopCh)
		}
	})
}

func (c *OpsMetricsCollector) run() {
	// First run immediately so the dashboard has data soon after startup.
	c.collectOnce()

	for {
		interval := c.getInterval()
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
			c.collectOnce()
		case <-c.stopCh:
			timer.Stop()
			return
		}
	}
}

func (c *OpsMetricsCollector) getInterval() time.Duration {
	interval := opsMetricsCollectorMinInterval

	if c.settingRepo == nil {
		return interval
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	raw, err := c.settingRepo.GetValue(ctx, SettingKeyOpsMetricsIntervalSeconds)
	if err != nil {
		return interval
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return interval
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil {
		return interval
	}
	if seconds < int(opsMetricsCollectorMinInterval.Seconds()) {
		seconds = int(opsMetricsCollectorMinInterval.Seconds())
	}
	if seconds > int(opsMetricsCollectorMaxInterval.Seconds()) {
		seconds = int(opsMetricsCollectorMaxInterval.Seconds())
	}
	return time.Duration(seconds) * time.Second
}

func (c *OpsMetricsCollector) collectOnce() {
	slog.Info("gateway.scheduler_metrics", "local", SnapshotSchedulingRuntimeMetrics())
	if c == nil {
		return
	}
	if c.cfg != nil && !c.cfg.Ops.Enabled {
		return
	}
	if c.opsRepo == nil {
		return
	}
	if c.db == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), opsMetricsCollectorTimeout)
	defer cancel()

	enabled := c.isMonitoringEnabled(ctx)
	c.nodesEnabled.Store(enabled)
	if !enabled {
		return
	}

	release, ok := c.tryAcquireLeaderLock(ctx)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	startedAt := time.Now().UTC()
	// The lock serializes replicas; a completed bucket also skips repeated SQL aggregation.
	if latest, err := c.opsRepo.GetLatestSystemMetrics(ctx, 1); err == nil && latest != nil && latest.CreatedAt.Equal(startedAt.Truncate(time.Minute)) {
		return
	}
	err := c.collectAndPersist(ctx)
	finishedAt := time.Now().UTC()

	durationMs := finishedAt.Sub(startedAt).Milliseconds()
	dur := durationMs
	runAt := startedAt

	if err != nil {
		msg := truncateString(err.Error(), 2048)
		errAt := finishedAt
		hbCtx, hbCancel := context.WithTimeout(context.Background(), opsMetricsCollectorHeartbeatTimeout)
		defer hbCancel()
		_ = c.opsRepo.UpsertJobHeartbeat(hbCtx, &OpsUpsertJobHeartbeatInput{
			JobName:        opsMetricsCollectorJobName,
			LastRunAt:      &runAt,
			LastErrorAt:    &errAt,
			LastError:      &msg,
			LastDurationMs: &dur,
		})
		log.Printf("[OpsMetricsCollector] collect failed: %v", err)
		return
	}

	successAt := finishedAt
	hbCtx, hbCancel := context.WithTimeout(context.Background(), opsMetricsCollectorHeartbeatTimeout)
	defer hbCancel()
	_ = c.opsRepo.UpsertJobHeartbeat(hbCtx, &OpsUpsertJobHeartbeatInput{
		JobName:        opsMetricsCollectorJobName,
		LastRunAt:      &runAt,
		LastSuccessAt:  &successAt,
		LastDurationMs: &dur,
	})
}

func (c *OpsMetricsCollector) isMonitoringEnabled(ctx context.Context) bool {
	if c == nil {
		return false
	}
	if c.cfg != nil && !c.cfg.Ops.Enabled {
		return false
	}
	if c.settingRepo == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	value, err := c.settingRepo.GetValue(ctx, SettingKeyOpsMonitoringEnabled)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return true
		}
		// Fail-open: collector should not become a hard dependency.
		return true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "false", "0", "off", "disabled":
		return false
	default:
		return true
	}
}

func (c *OpsMetricsCollector) collectAndPersist(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Align to stable minute boundaries to avoid partial buckets and to maximize cache hits.
	now := time.Now().UTC()
	windowEnd := now.Truncate(time.Minute)
	windowStart := windowEnd.Add(-1 * time.Minute)

	nodes, nodesErr := c.ListNodeMetrics(ctx)
	sys := summarizeOpsNodes(nodes)
	if nodesErr != nil || sys == nil {
		sys = &OpsSystemMetricsSnapshot{}
	}

	input, err := c.opsRepo.GetSystemMetricsWindow(ctx, windowStart, windowEnd)
	if err != nil {
		return err
	}
	input.CPUUsagePercent = sys.CPUUsagePercent
	input.MemoryUsedMB = sys.MemoryUsedMB
	input.MemoryTotalMB = sys.MemoryTotalMB
	input.MemoryUsagePercent = sys.MemoryUsagePercent
	input.DBOK = sys.DBOK
	input.RedisOK = sys.RedisOK
	input.RedisConnTotal = sys.RedisConnTotal
	input.RedisConnIdle = sys.RedisConnIdle
	input.DBConnActive = sys.DBConnActive
	input.DBConnIdle = sys.DBConnIdle
	input.GoroutineCount = sys.GoroutineCount
	input.ConcurrencyQueueDepth = c.collectConcurrencyQueueDepth(ctx)

	return c.opsRepo.InsertSystemMetrics(ctx, input)
}

func (c *OpsMetricsCollector) collectConcurrencyQueueDepth(parentCtx context.Context) *int {
	if c == nil || c.accountRepo == nil || c.concurrencyService == nil {
		return nil
	}
	if parentCtx == nil {
		parentCtx = context.Background()
	}

	// Best-effort: never let concurrency sampling break the metrics collector.
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Second)
	defer cancel()

	accountLoads, err := c.listSchedulableAccountLoads(ctx)
	if err != nil {
		return nil
	}
	if len(accountLoads) == 0 {
		zero := 0
		return &zero
	}

	loadMap, err := c.concurrencyService.GetAccountsLoadBatch(ctx, accountLoads)
	if err != nil {
		return nil
	}

	var total int64
	for _, info := range loadMap {
		if info == nil || info.WaitingCount <= 0 {
			continue
		}
		total += int64(info.WaitingCount)
	}
	if total < 0 {
		total = 0
	}

	maxInt := int64(^uint(0) >> 1)
	if total > maxInt {
		total = maxInt
	}
	v := int(total)
	return &v
}

func (c *OpsMetricsCollector) listSchedulableAccountLoads(ctx context.Context) ([]AccountWithConcurrency, error) {
	if repo, ok := c.accountRepo.(opsSchedulableAccountLoadRepository); ok {
		return repo.ListSchedulableAccountLoads(ctx)
	}

	accounts, err := c.accountRepo.ListSchedulable(ctx)
	if err != nil {
		return nil, err
	}
	loads := make([]AccountWithConcurrency, 0, len(accounts))
	for _, account := range accounts {
		if account.ID <= 0 {
			continue
		}
		loads = append(loads, AccountWithConcurrency{
			ID:             account.ID,
			MaxConcurrency: account.EffectiveLoadFactor(),
		})
	}
	return loads, nil
}

type opsCollectedSystemStats struct {
	cpuScope           string
	memoryScope        string
	cpuCapacity        float64
	cpuUsagePercent    *float64
	memoryUsedMB       *int64
	memoryTotalMB      *int64
	memoryUsagePercent *float64
}

func (c *OpsMetricsCollector) collectSystemStats(ctx context.Context) (*opsCollectedSystemStats, error) {
	out := &opsCollectedSystemStats{cpuScope: "host", memoryScope: "host", cpuCapacity: float64(runtime.NumCPU())}
	if ctx == nil {
		ctx = context.Background()
	}

	sampleAt := time.Now().UTC()

	// CPU: prefer cgroup (container) metrics, falling back to host metrics when
	// cgroup CPU accounting is unavailable.
	if cpuPct := c.tryCgroupCPUPercent(sampleAt); cpuPct != nil {
		out.cpuUsagePercent = cpuPct
		out.cpuScope = "container"
		out.cpuCapacity = readCgroupCPULimitCores("/sys/fs/cgroup", runtime.NumCPU())
	}
	if out.cpuUsagePercent == nil {
		if cpuPercents, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(cpuPercents) > 0 {
			v := roundTo1DP(cpuPercents[0])
			out.cpuUsagePercent = &v
		}
	}

	// Memory: prefer cgroup (container) metrics, but only when the cgroup exposes
	// BOTH a current usage and a concrete limit (memory.max != "max"). When the
	// limit is missing or the cgroup data is otherwise incomplete, fall back
	// ENTIRELY to host metrics. Never mix a container "used" with a host "total":
	// doing so reports a misleadingly tiny percentage (e.g. 60MB / 23GB ~= 0.3%).
	cgroupUsed, cgroupTotal, cgroupOK := readCgroupMemoryBytes("/sys/fs/cgroup")
	var host *mem.VirtualMemoryStat
	if !cgroupOK || cgroupTotal == 0 {
		if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
			host = vm
		}
	}
	if cgroupOK && cgroupTotal > 0 {
		out.memoryScope = "container"
	}
	out.memoryUsedMB, out.memoryTotalMB, out.memoryUsagePercent = resolveMemoryStats(cgroupUsed, cgroupTotal, cgroupOK, host)

	return out, nil
}

// resolveMemoryStats picks a single, self-consistent (used, total, percent)
// memory trio from either cgroup (container) or host metrics — never a mix of
// the two.
//
// cgroup metrics are preferred, but only when the cgroup reports BOTH a current
// usage and a concrete limit (i.e. memory.max is a number, not "max", so
// cgroupTotal > 0). If the limit is absent or the cgroup data is otherwise
// incomplete, all three values fall back to the host reading. This avoids the
// classic pitfall of dividing a container "used" by a host "total", which
// wildly understates memory usage (e.g. 60MB / 23GB ~= 0.3%).
func resolveMemoryStats(cgroupUsed, cgroupTotal uint64, cgroupOK bool, host *mem.VirtualMemoryStat) (usedMB *int64, totalMB *int64, usagePercent *float64) {
	if cgroupOK && cgroupTotal > 0 {
		u := int64(cgroupUsed / bytesPerMB)
		t := int64(cgroupTotal / bytesPerMB)
		p := roundTo1DP(float64(cgroupUsed) / float64(cgroupTotal) * 100)
		return &u, &t, &p
	}

	if host == nil {
		return nil, nil, nil
	}

	// Available accounts for shared memory and kernel watermarks.
	used := host.Used
	if host.Total > 0 {
		if host.Available > host.Total {
			return nil, nil, nil
		}
		used = host.Total - host.Available
	}
	u := int64(used / bytesPerMB)
	usedMB = &u
	if host.Total > 0 {
		t := int64(host.Total / bytesPerMB)
		totalMB = &t
		p := roundTo1DP(float64(used) / float64(host.Total) * 100)
		usagePercent = &p
	} else {
		// Degenerate: host reported no total. Preserve gopsutil's own percentage.
		p := roundTo1DP(host.UsedPercent)
		usagePercent = &p
	}
	return usedMB, totalMB, usagePercent
}

func (c *OpsMetricsCollector) tryCgroupCPUPercent(now time.Time) *float64 {
	usageNanos, ok := readCgroupCPUUsageNanos()
	if !ok {
		return nil
	}

	// Initialize baseline sample.
	if c.lastCgroupCPUSampleAt.IsZero() {
		c.lastCgroupCPUUsageNanos = usageNanos
		c.lastCgroupCPUSampleAt = now
		return nil
	}

	elapsed := now.Sub(c.lastCgroupCPUSampleAt)
	if elapsed <= 0 {
		c.lastCgroupCPUUsageNanos = usageNanos
		c.lastCgroupCPUSampleAt = now
		return nil
	}

	prev := c.lastCgroupCPUUsageNanos
	c.lastCgroupCPUUsageNanos = usageNanos
	c.lastCgroupCPUSampleAt = now

	if usageNanos < prev {
		// Counter reset (container restarted).
		return nil
	}

	deltaUsageSec := float64(usageNanos-prev) / 1e9
	elapsedSec := elapsed.Seconds()
	if elapsedSec <= 0 {
		return nil
	}

	cores := readCgroupCPULimitCores("/sys/fs/cgroup", runtime.NumCPU())
	if cores <= 0 {
		// Can't reliably normalize; skip and fall back to gopsutil.
		return nil
	}

	pct := (deltaUsageSec / (elapsedSec * cores)) * 100
	if pct < 0 {
		pct = 0
	}
	// Clamp to avoid noise/jitter showing impossible values.
	if pct > 100 {
		pct = 100
	}
	v := roundTo1DP(pct)
	return &v
}

// Read the container working set, excluding reclaimable inactive file cache.
// Incomplete/unlimited readings fall back as a whole to host metrics.
func readCgroupMemoryBytes(root string) (usedBytes uint64, totalBytes uint64, ok bool) {
	usagePath, limitPath := filepath.Join(root, "memory.current"), filepath.Join(root, "memory.max")
	statPath, inactiveKey := filepath.Join(root, "memory.stat"), "inactive_file"
	if _, err := os.Stat(usagePath); err != nil {
		dir := filepath.Join(root, "memory")
		usagePath, limitPath = filepath.Join(dir, "memory.usage_in_bytes"), filepath.Join(dir, "memory.limit_in_bytes")
		statPath, inactiveKey = filepath.Join(dir, "memory.stat"), "total_inactive_file"
	}
	used, usageOK := readUintFile(usagePath)
	limit, limitOK := readUintFile(limitPath)
	inactive, statOK := readCgroupStatValue(statPath, inactiveKey)
	if !usageOK || !limitOK || !statOK || limit == 0 || limit >= 1<<60 {
		return 0, 0, false
	}
	return used - min(used, inactive), limit, true
}

func readCgroupCPUUsageNanos() (usageNanos uint64, ok bool) {
	if usec, ok := readCgroupStatValue("/sys/fs/cgroup/cpu.stat", "usage_usec"); ok {
		return usec * 1000, true
	}

	// cgroup v1: cpuacct.usage is in nanoseconds
	if v, ok := readUintFile("/sys/fs/cgroup/cpuacct/cpuacct.usage"); ok {
		return v, true
	}

	return 0, false
}

func readCgroupCPULimitCores(root string, schedulableCPUs int) float64 {
	if schedulableCPUs <= 0 {
		return 0
	}
	// cgroup v2: cpu.max => "<quota> <period>" or "max <period>"
	if raw, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 2 && fields[0] != "max" {
			quota, err1 := strconv.ParseFloat(fields[0], 64)
			period, err2 := strconv.ParseFloat(fields[1], 64)
			if err1 == nil && err2 == nil && quota > 0 && period > 0 {
				return min(quota/period, float64(schedulableCPUs))
			}
		}
	}

	// cgroup v1: cpu.cfs_quota_us / cpu.cfs_period_us
	quota, okQuota := readIntFile(filepath.Join(root, "cpu", "cpu.cfs_quota_us"))
	period, okPeriod := readIntFile(filepath.Join(root, "cpu", "cpu.cfs_period_us"))
	if okQuota && okPeriod && quota > 0 && period > 0 {
		return min(float64(quota)/float64(period), float64(schedulableCPUs))
	}

	return 0
}

func readCgroupStatValue(path, key string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return value, err == nil
		}
	}
	return 0, false
}

func readUintFile(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func readIntFile(path string) (int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func (c *OpsMetricsCollector) checkDB(ctx context.Context) bool {
	if c == nil || c.db == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var one int
	if err := c.db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return false
	}
	return one == 1
}

func (c *OpsMetricsCollector) checkRedis(ctx context.Context) bool {
	if c == nil || c.redisClient == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return c.redisClient.Ping(ctx).Err() == nil
}

func (c *OpsMetricsCollector) redisPoolStats() (total int, idle int, ok bool) {
	if c == nil || c.redisClient == nil {
		return 0, 0, false
	}
	stats := c.redisClient.PoolStats()
	if stats == nil {
		return 0, 0, false
	}
	return int(stats.TotalConns), int(stats.IdleConns), true
}

var opsMetricsCollectorReleaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

func (c *OpsMetricsCollector) tryAcquireLeaderLock(ctx context.Context) (func(), bool) {
	if c == nil || c.redisClient == nil {
		return nil, true
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ok, err := c.redisClient.SetNX(ctx, opsMetricsCollectorLeaderLockKey, c.instanceID, opsMetricsCollectorLeaderLockTTL).Result()
	if err != nil {
		// Prefer fail-closed to avoid stampeding the database when Redis is flaky.
		// Fallback to a DB advisory lock when Redis is present but unavailable.
		release, ok := tryAcquireDBAdvisoryLock(ctx, c.db, opsMetricsCollectorAdvisoryLockID)
		if !ok {
			c.maybeLogSkip()
			return nil, false
		}
		return release, true
	}
	if !ok {
		c.maybeLogSkip()
		return nil, false
	}

	release := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = opsMetricsCollectorReleaseScript.Run(ctx, c.redisClient, []string{opsMetricsCollectorLeaderLockKey}, c.instanceID).Result()
	}
	return release, true
}

func (c *OpsMetricsCollector) maybeLogSkip() {
	c.skipLogMu.Lock()
	defer c.skipLogMu.Unlock()

	now := time.Now()
	if !c.skipLogAt.IsZero() && now.Sub(c.skipLogAt) < time.Minute {
		return
	}
	c.skipLogAt = now
	log.Printf("[OpsMetricsCollector] leader lock held by another instance; skipping")
}

func roundTo1DP(v float64) float64 {
	return math.Round(v*10) / 10
}

func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func boolPtr(v bool) *bool {
	out := v
	return &out
}

func intPtr(v int) *int {
	out := v
	return &out
}

func float64Ptr(v float64) *float64 {
	out := v
	return &out
}
