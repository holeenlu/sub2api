//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// opsHeartbeatRecorder 记录每次 UpsertJobHeartbeat 的输入，其余方法沿用 opsRepoMock。
type opsHeartbeatRecorder struct {
	opsRepoMock
	mu     sync.Mutex
	inputs []*OpsUpsertJobHeartbeatInput
}

func (r *opsHeartbeatRecorder) UpsertJobHeartbeat(ctx context.Context, input *OpsUpsertJobHeartbeatInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inputs = append(r.inputs, input)
	return nil
}

func (r *opsHeartbeatRecorder) last(t *testing.T) *OpsUpsertJobHeartbeatInput {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	require.NotEmpty(t, r.inputs, "expected a heartbeat to be written")
	return r.inputs[len(r.inputs)-1]
}

func (r *opsHeartbeatRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.inputs)
}

// resetSkippedHeartbeatThrottle 清掉 skipped 心跳的进程级节流状态，
// 让每个用例都从"还没写过"开始。
func resetSkippedHeartbeatThrottle(t *testing.T, jobNames ...string) {
	t.Helper()
	for _, name := range jobNames {
		opsSkippedHeartbeats.Delete(name)
	}
}

func requireIntervalSeconds(t *testing.T, input *OpsUpsertJobHeartbeatInput, want int64) {
	t.Helper()
	require.NotNil(t, input.ExpectedIntervalSeconds)
	require.Equal(t, want, *input.ExpectedIntervalSeconds)
}

func TestOpsCronInterval(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	parse := func(spec string) time.Duration {
		sched, err := opsCleanupCronParser.Parse(spec)
		require.NoError(t, err)
		return opsCronInterval(sched, now)
	}

	require.Equal(t, 24*time.Hour, parse("0 2 * * *"))
	require.Equal(t, 7*24*time.Hour, parse("0 2 * * 0"))
	require.Equal(t, time.Hour, parse("0 * * * *"))
	require.Equal(t, time.Duration(0), opsCronInterval(nil, now))
}

// 不等距的 cron 要取最大间隔，否则自报周期取决于服务启动时刻：
// '0 3 * * 1-5' 在周一至周四启动只看到 24h（阈值 72h），而周五→周一的真实间隔
// 恰好是 72h，周一那次执行稍慢就会被误判失联。
func TestOpsCronInterval_UnevenScheduleUsesLargestGap(t *testing.T) {
	t.Parallel()

	sched, err := opsCleanupCronParser.Parse("0 3 * * 1-5")
	require.NoError(t, err)

	// 2026-09-02 是周三，往后逐天覆盖一整周的启动时刻。
	for i := range 7 {
		start := time.Date(2026, 9, 2, 4, 0, 0, 0, time.UTC).AddDate(0, 0, i)
		require.Equalf(t, 72*time.Hour, opsCronInterval(sched, start),
			"启动于 %s 时自报周期不应随启动时刻漂移", start.Weekday())
	}
}

func TestOpsJobIntervalSeconds(t *testing.T) {
	t.Parallel()

	requireIntervalSeconds(t, &OpsUpsertJobHeartbeatInput{ExpectedIntervalSeconds: opsJobIntervalSeconds(10 * time.Minute)}, 600)
	requireIntervalSeconds(t, &OpsUpsertJobHeartbeatInput{ExpectedIntervalSeconds: opsJobIntervalSeconds(0)}, 0)
	requireIntervalSeconds(t, &OpsUpsertJobHeartbeatInput{ExpectedIntervalSeconds: opsJobIntervalSeconds(-time.Second)}, 0)
}

// 跳过的一轮只证明任务循环还活着，不能写成一次成功：UPSERT 会在 last_success_at
// 非空时清空 last_error/last_error_at，把上一次真实失败抹掉。
func TestRecordOpsJobSkipped_DoesNotFakeSuccess(t *testing.T) {
	t.Parallel()

	resetSkippedHeartbeatThrottle(t, "job-x")
	rec := &opsHeartbeatRecorder{}
	recordOpsJobSkipped(rec, "job-x", 10*time.Minute, "nothing to do")

	hb := rec.last(t)
	require.Equal(t, "job-x", hb.JobName)
	require.NotNil(t, hb.LastRunAt)
	require.Nil(t, hb.LastSuccessAt, "skipped 心跳不能伪装成一次成功")
	require.Nil(t, hb.LastErrorAt)
	require.NotNil(t, hb.LastResult)
	require.Equal(t, "skipped: nothing to do", *hb.LastResult)
	requireIntervalSeconds(t, hb, 600)

	// nil repo 不 panic
	recordOpsJobSkipped(nil, "job-x", time.Minute, "noop")
}

// 被设置关闭的清理任务写 skipped 心跳并声明"未调度"，而不是沉默。
// 不并行：与下面的用例共享 opsCleanupJobName 的 skipped 心跳节流状态。
func TestOpsCleanupService_DisabledWritesSkippedHeartbeat(t *testing.T) {
	resetSkippedHeartbeatThrottle(t, opsCleanupJobName)

	rec := &opsHeartbeatRecorder{}
	cfg := &config.Config{}
	cfg.Ops.Enabled = true
	cfg.Ops.Cleanup.Enabled = false
	svc := &OpsCleanupService{opsRepo: rec, cfg: cfg}

	svc.mu.Lock()
	require.NoError(t, svc.applyScheduleLocked(context.Background()))
	svc.mu.Unlock()

	hb := rec.last(t)
	require.Equal(t, opsCleanupJobName, hb.JobName)
	require.NotNil(t, hb.LastRunAt)
	require.Nil(t, hb.LastSuccessAt, "关闭清理不能把上一次失败抹掉")
	require.NotNil(t, hb.LastResult)
	require.Equal(t, "skipped: cleanup disabled by settings", *hb.LastResult)
	requireIntervalSeconds(t, hb, 0)
	require.Equal(t, time.Duration(0), svc.snapshotInterval())
}

// 启用时只声明周期，不伪造成功时间；周期来自 cron 表达式。
func TestOpsCleanupService_ScheduledDeclaresCronInterval(t *testing.T) {
	t.Parallel()

	rec := &opsHeartbeatRecorder{}
	cfg := &config.Config{Timezone: "UTC"}
	cfg.Ops.Enabled = true
	cfg.Ops.Cleanup.Enabled = true
	cfg.Ops.Cleanup.Schedule = "0 2 * * 0"
	svc := &OpsCleanupService{opsRepo: rec, cfg: cfg}

	svc.mu.Lock()
	require.NoError(t, svc.applyScheduleLocked(context.Background()))
	svc.mu.Unlock()
	defer svc.Stop()

	require.Equal(t, 7*24*time.Hour, svc.snapshotInterval())

	hb := rec.last(t)
	require.Equal(t, opsCleanupJobName, hb.JobName)
	require.Nil(t, hb.LastRunAt)
	require.Nil(t, hb.LastSuccessAt)
	require.Nil(t, hb.LastErrorAt)
	requireIntervalSeconds(t, hb, 7*24*3600)

	// 成功 / 失败心跳都带上同一个周期。
	svc.recordHeartbeatSuccess(time.Now(), time.Second, opsCleanupDeletedCounts{})
	requireIntervalSeconds(t, rec.last(t), 7*24*3600)
	svc.recordHeartbeatError(time.Now(), time.Second, context.DeadlineExceeded)
	requireIntervalSeconds(t, rec.last(t), 7*24*3600)
}

// cron 建不起来时清理实际停摆，必须写一条 error 心跳把它标成异常。
// 否则「先关闭（心跳周期 0）再用非法 cron 开启」会让 opsJobStaleThreshold 恒返回
// ok=false，清理停摆数月而仪表盘一直显示健康。
// 不并行：与上面的用例共享 opsCleanupJobName 的 skipped 心跳节流状态。
func TestOpsCleanupService_InvalidScheduleRecordsErrorHeartbeat(t *testing.T) {
	resetSkippedHeartbeatThrottle(t, opsCleanupJobName)

	rec := &opsHeartbeatRecorder{}
	cfg := &config.Config{Timezone: "UTC"}
	cfg.Ops.Enabled = true
	cfg.Ops.Cleanup.Enabled = false
	svc := &OpsCleanupService{opsRepo: rec, cfg: cfg}

	// 先关闭：心跳自报周期 0。
	svc.mu.Lock()
	require.NoError(t, svc.applyScheduleLocked(context.Background()))
	svc.mu.Unlock()
	requireIntervalSeconds(t, rec.last(t), 0)

	// 再用非法 cron 开启。
	cfg.Ops.Cleanup.Enabled = true
	cfg.Ops.Cleanup.Schedule = "0 99 * * *"
	svc.mu.Lock()
	err := svc.applyScheduleLocked(context.Background())
	svc.mu.Unlock()
	require.Error(t, err)

	hb := rec.last(t)
	require.Equal(t, opsCleanupJobName, hb.JobName)
	require.NotNil(t, hb.LastErrorAt, "调度没建起来必须写 error 心跳")
	require.NotNil(t, hb.LastError)
	require.Contains(t, *hb.LastError, "invalid schedule")
	require.Nil(t, hb.LastSuccessAt)
	require.Equal(t, opsJobFailed, classifyOpsJobHeartbeat(time.Now().UTC(), &OpsJobHeartbeat{
		JobName:                 opsCleanupJobName,
		LastRunAt:               hb.LastRunAt,
		LastErrorAt:             hb.LastErrorAt,
		ExpectedIntervalSeconds: hb.ExpectedIntervalSeconds,
	}))
}

// 预聚合被配置关闭时，每轮 tick 仍写 skipped 心跳并带上自身周期。
func TestOpsAggregationService_DisabledWritesSkippedHeartbeat(t *testing.T) {
	t.Parallel()

	resetSkippedHeartbeatThrottle(t, opsAggHourlyJobName, opsAggDailyJobName)
	rec := &opsHeartbeatRecorder{}
	cfg := &config.Config{}
	cfg.Ops.Enabled = true
	cfg.Ops.Aggregation.Enabled = false
	svc := &OpsAggregationService{opsRepo: rec, cfg: cfg}

	svc.aggregateHourly()
	hb := rec.last(t)
	require.Equal(t, opsAggHourlyJobName, hb.JobName)
	require.NotNil(t, hb.LastRunAt)
	require.Nil(t, hb.LastSuccessAt)
	requireIntervalSeconds(t, hb, int64(opsAggHourlyInterval/time.Second))

	svc.aggregateDaily()
	hb = rec.last(t)
	require.Equal(t, opsAggDailyJobName, hb.JobName)
	requireIntervalSeconds(t, hb, int64(opsAggDailyInterval/time.Second))
}

// 空转的 tick 不该按 tick 频率写库：定时报表每分钟一次，而判活阈值是 15 分钟。
func TestRecordOpsJobSkipped_ThrottlesIdleWrites(t *testing.T) {
	t.Parallel()

	const job = "job-throttle"
	resetSkippedHeartbeatThrottle(t, job)

	rec := &opsHeartbeatRecorder{}
	for range 5 {
		recordOpsJobSkipped(rec, job, opsScheduledReportTickInterval, "no scheduled reports enabled")
	}
	require.Equal(t, 1, rec.count(), "同一原因的连续空转只写一次")

	// 原因或自报周期变了要立刻落库，否则任务被关闭后周期会停在旧值上。
	recordOpsJobSkipped(rec, job, 0, "cleanup disabled by settings")
	require.Equal(t, 2, rec.count())
	requireIntervalSeconds(t, rec.last(t), 0)

	// 写入间隔取判活阈值的 1/3：1 分钟周期用下限 15 分钟，10 分钟周期用 30 分钟。
	require.Equal(t, 5*time.Minute, opsSkippedHeartbeatMinGap(time.Minute))
	require.Equal(t, 5*time.Minute, opsSkippedHeartbeatMinGap(0))
	require.Equal(t, 10*time.Minute, opsSkippedHeartbeatMinGap(10*time.Minute))
}

// 成功 / 失败心跳的公共写入：各服务只提供任务名、周期与结果。
func TestRecordOpsJobSuccessAndError(t *testing.T) {
	t.Parallel()

	runAt := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	rec := &opsHeartbeatRecorder{}

	recordOpsJobSuccess(rec, "job-hb", runAt, 1500*time.Millisecond, 10*time.Minute, " window=a..b ")
	hb := rec.last(t)
	require.Equal(t, "job-hb", hb.JobName)
	require.True(t, hb.LastRunAt.Equal(runAt))
	require.NotNil(t, hb.LastSuccessAt)
	require.Nil(t, hb.LastErrorAt)
	require.Equal(t, int64(1500), *hb.LastDurationMs)
	require.Equal(t, "window=a..b", *hb.LastResult)
	requireIntervalSeconds(t, hb, 600)

	// 结果为空时补 "ok"，仪表盘不至于空着。
	recordOpsJobSuccess(rec, "job-hb", runAt, 0, time.Minute, "")
	require.Equal(t, "ok", *rec.last(t).LastResult)

	recordOpsJobError(rec, "job-hb", runAt, 2*time.Second, time.Minute, context.DeadlineExceeded)
	hb = rec.last(t)
	require.NotNil(t, hb.LastErrorAt)
	require.Nil(t, hb.LastSuccessAt)
	require.Nil(t, hb.LastResult, "报错不覆盖上一次成功的产出")
	require.Equal(t, context.DeadlineExceeded.Error(), *hb.LastError)
	require.Equal(t, int64(2000), *hb.LastDurationMs)
	requireIntervalSeconds(t, hb, 60)

	// nil repo / nil error 不 panic，也不写入。
	before := rec.count()
	recordOpsJobSuccess(nil, "job-hb", runAt, 0, 0, "")
	recordOpsJobError(nil, "job-hb", runAt, 0, 0, context.DeadlineExceeded)
	recordOpsJobError(rec, "job-hb", runAt, 0, 0, nil)
	require.Equal(t, before, rec.count())
}
