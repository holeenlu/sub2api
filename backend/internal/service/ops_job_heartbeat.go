package service

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

const opsJobHeartbeatTimeout = 2 * time.Second

// opsJobIntervalSeconds 把任务周期换算成心跳里自报的秒数；非正数按 0（未调度）处理。
func opsJobIntervalSeconds(d time.Duration) *int64 {
	v := int64(d / time.Second)
	if v < 0 {
		v = 0
	}
	return &v
}

// opsCronProbeCount 是估算周期时向前探的触发次数。10 次足以让「工作日」「每月某日」
// 这类不等距表达式走完一个完整循环，从而看到真正的最大间隔。
const opsCronProbeCount = 10

// opsCronInterval 估算 cron 表达式的触发周期，用作任务自报周期。
//
// 取未来若干次触发间隔的最大值，而不是紧邻的那一段：'0 3 * * 1-5' 的相邻间隔在
// 周一至周四是 24h、周五是 72h，只看一段的话自报周期会随服务启动时刻漂移，判活阈值
// （3 倍周期）可能比真实的最大间隔还短，正常运行的任务会被误判失联。
func opsCronInterval(sched cron.Schedule, now time.Time) time.Duration {
	if sched == nil {
		return 0
	}
	prev := sched.Next(now)
	if prev.IsZero() {
		return 0
	}
	var longest time.Duration
	for range opsCronProbeCount {
		next := sched.Next(prev)
		if next.IsZero() || !next.After(prev) {
			break
		}
		if gap := next.Sub(prev); gap > longest {
			longest = gap
		}
		prev = next
	}
	return longest
}

// recordOpsJobSuccess 记一条成功心跳。result 为空时记 "ok"，让仪表盘不至于空着。
func recordOpsJobSuccess(repo OpsRepository, jobName string, runAt time.Time, duration time.Duration, interval time.Duration, result string) {
	if repo == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := strings.TrimSpace(result)
	if msg == "" {
		msg = "ok"
	}
	msg = truncateString(msg, 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsJobHeartbeatTimeout)
	defer cancel()
	_ = repo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:                 jobName,
		LastRunAt:               &runAt,
		LastSuccessAt:           &now,
		LastDurationMs:          &durMs,
		LastResult:              &msg,
		ExpectedIntervalSeconds: opsJobIntervalSeconds(interval),
	})
}

// recordOpsJobError 记一条失败心跳。不写 last_result：上一次成功的产出留着，
// 报错内容进 last_error。
func recordOpsJobError(repo OpsRepository, jobName string, runAt time.Time, duration time.Duration, interval time.Duration, err error) {
	if repo == nil || err == nil {
		return
	}
	now := time.Now().UTC()
	durMs := duration.Milliseconds()
	msg := truncateString(err.Error(), 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsJobHeartbeatTimeout)
	defer cancel()
	_ = repo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:                 jobName,
		LastRunAt:               &runAt,
		LastErrorAt:             &now,
		LastError:               &msg,
		LastDurationMs:          &durMs,
		ExpectedIntervalSeconds: opsJobIntervalSeconds(interval),
	})
}

// recordOpsJobSkipped 记一条「本轮跳过」的心跳：任务循环活着、只是没有工作可做
// （被设置关闭、没有配置报表等），把原因写进 last_result，而不是沉默——沉默会在
// 阈值到期后被当成失联。
//
// 只写 last_run_at，不写 last_success_at：UPSERT 在 last_success_at 非空时会清空
// last_error/last_error_at，把上一次真实失败抹掉；判活那边认 last_run_at，所以
// 跳过的一轮照样证明任务还活着。
//
// 空转的 tick 比判活需要的频率高得多（定时报表每分钟一次、判活阈值 15 分钟），
// 所以按 opsSkippedHeartbeatMinGap 节流，只有跳过的原因或自报周期变了才立刻落库。
func recordOpsJobSkipped(repo OpsRepository, jobName string, interval time.Duration, reason string) {
	if repo == nil {
		return
	}
	now := time.Now().UTC()
	if !claimOpsSkippedHeartbeat(jobName, interval, reason, now) {
		return
	}
	result := truncateString("skipped: "+reason, 2048)
	ctx, cancel := context.WithTimeout(context.Background(), opsJobHeartbeatTimeout)
	defer cancel()
	_ = repo.UpsertJobHeartbeat(ctx, &OpsUpsertJobHeartbeatInput{
		JobName:                 jobName,
		LastRunAt:               &now,
		LastResult:              &result,
		ExpectedIntervalSeconds: opsJobIntervalSeconds(interval),
	})
}

// opsSkippedHeartbeatState 是某个任务上一次写 skipped 心跳时的状态。
type opsSkippedHeartbeatState struct {
	at       time.Time
	interval time.Duration
	reason   string
}

// opsSkippedHeartbeats: jobName -> opsSkippedHeartbeatState。
var opsSkippedHeartbeats sync.Map

// opsSkippedHeartbeatMinGap 返回两次 skipped 心跳之间的最小间隔：判活阈值的 1/3。
// 阈值本身已经是周期的 3 倍，按 1/3 写入仍留有三次的余量。
func opsSkippedHeartbeatMinGap(interval time.Duration) time.Duration {
	threshold := interval * opsJobStaleToleranceFactor
	if threshold < opsJobStaleFloor {
		threshold = opsJobStaleFloor
	}
	return threshold / opsJobStaleToleranceFactor
}

// claimOpsSkippedHeartbeat 判断这一轮的 skipped 心跳该不该写。
// 周期或跳过原因变了（例如任务刚被关闭）一律立刻写，否则自报周期会停在旧值上。
func claimOpsSkippedHeartbeat(jobName string, interval time.Duration, reason string, now time.Time) bool {
	next := opsSkippedHeartbeatState{at: now, interval: interval, reason: reason}
	if prev, ok := opsSkippedHeartbeats.Load(jobName); ok {
		if last, ok := prev.(opsSkippedHeartbeatState); ok &&
			last.interval == interval && last.reason == reason &&
			now.Sub(last.at) < opsSkippedHeartbeatMinGap(interval) {
			return false
		}
	}
	opsSkippedHeartbeats.Store(jobName, next)
	return true
}
