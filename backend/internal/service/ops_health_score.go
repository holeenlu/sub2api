package service

import (
	"math"
	"time"
)

const (
	// dashboardTTFTDefaultFullScoreMs 是 ttft_p99_ms_max 未配置（nil 或 0）时 TTFT 评分的满分点。
	// 与 defaultOpsMetricThresholds 的默认值保持一致。
	dashboardTTFTDefaultFullScoreMs = 10_000
	// dashboardTTFTZeroScoreFactor：TTFT 达到满分点的这个倍数时评 0 分，中间线性插值。
	dashboardTTFTZeroScoreFactor = 3
)

// dashboardTTFTFullScoreMs 返回 TTFT 评分刻度的满分点（毫秒）。
//
// 刻度不单独设置，而是从已有的 ttft_p99_ms_max 派生：同一个阈值同时驱动卡片变红、诊断提示
// 与健康评分，避免三处尺度互相矛盾（例如诊断说偏高、评分却给满分）。
func dashboardTTFTFullScoreMs(thresholds *OpsMetricThresholds) float64 {
	if thresholds == nil || thresholds.TTFTp99MsMax == nil || *thresholds.TTFTp99MsMax <= 0 {
		return dashboardTTFTDefaultFullScoreMs
	}
	return *thresholds.TTFTp99MsMax
}

// computeDashboardHealth computes a 0-100 health score from the metrics returned by the dashboard overview,
// together with the sub-scores and unhealthy job names that explain it.
//
// Design goals:
// - Backend-owned scoring (UI only displays).
// - Layered scoring: Business Health (70%) + Infrastructure Health (30%)
// - Avoids double-counting (e.g., DB failure affects both infra and business metrics)
// - Conservative + stable: penalize clear degradations; avoid overreacting to missing/idle data.
// - Explainable: the breakdown comes from the same pass as the score, so the two cannot drift.
//
// 无流量的窗口不会被判为"差"——错误率与 TTFT 都缺样本，业务分自然是满分；但基础设施与
// 后台任务的判定与流量无关，必须照常计算并下发明细，否则夜间/低峰时段 collector 死掉或
// 任务报错时，只认 breakdown 的前端任务面板会一路全绿。
func computeDashboardHealth(now time.Time, overview *OpsDashboardOverview, thresholds *OpsMetricThresholds) (int, *OpsHealthScoreBreakdown) {
	if overview == nil {
		return 0, nil
	}

	business := computeBusinessHealth(overview, thresholds)
	infra := computeInfraHealth(now, overview)

	// Weighted combination: 70% business + 30% infrastructure
	score := business.total*0.7 + infra.total*0.3
	breakdown := &OpsHealthScoreBreakdown{
		Business:        roundTo1DP(business.total),
		ErrorRate:       roundTo1DP(business.errorRate),
		TTFT:            roundTo1DP(business.ttft),
		TTFTFullScoreMs: dashboardTTFTFullScoreMs(thresholds),
		Infra:           roundTo1DP(infra.total),
		Storage:         roundTo1DP(infra.storage),
		Compute:         roundTo1DP(infra.compute),
		Jobs:            roundTo1DP(infra.jobs),
		FailedJobs:      infra.failedJobs,
		StaleJobs:       infra.staleJobs,
	}
	return int(math.Round(clampFloat64(score, 0, 100))), breakdown
}

type opsBusinessHealthParts struct {
	total     float64
	errorRate float64
	ttft      float64
}

type opsInfraHealthParts struct {
	total      float64
	storage    float64
	compute    float64
	jobs       float64
	failedJobs []string
	staleJobs  []string
}

// computeBusinessHealth calculates business health score (0-100)
// Components: Error Rate (50%) + TTFT (50%)
func computeBusinessHealth(overview *OpsDashboardOverview, thresholds *OpsMetricThresholds) opsBusinessHealthParts {
	// Error rate score: 1% → 100, 10% → 0 (linear)
	// Combines request errors and upstream errors
	errorScore := 100.0
	errorPct := clampFloat64(overview.ErrorRate*100, 0, 100)
	upstreamPct := clampFloat64(overview.UpstreamErrorRate*100, 0, 100)
	combinedErrorPct := math.Max(errorPct, upstreamPct) // Use worst case
	if combinedErrorPct > 1.0 {
		if combinedErrorPct <= 10.0 {
			errorScore = (10.0 - combinedErrorPct) / 9.0 * 100
		} else {
			errorScore = 0
		}
	}

	// TTFT score: threshold → 100, 3×threshold → 0 (linear)
	// TTFT 主要由上游模型 prefill 决定（输入越长预处理越久），不是本服务能压缩的延迟，
	// 所以刻度跟着管理员配置的阈值走，而不是写死秒级常量。
	ttftScore := 100.0
	if overview.TTFT.P99 != nil {
		p99 := float64(*overview.TTFT.P99)
		fullScoreMs := dashboardTTFTFullScoreMs(thresholds)
		zeroScoreMs := fullScoreMs * dashboardTTFTZeroScoreFactor
		if p99 > fullScoreMs {
			if p99 <= zeroScoreMs {
				ttftScore = (zeroScoreMs - p99) / (zeroScoreMs - fullScoreMs) * 100
			} else {
				ttftScore = 0
			}
		}
	}

	// Weighted combination: 50% error rate + 50% TTFT
	return opsBusinessHealthParts{
		total:     errorScore*0.5 + ttftScore*0.5,
		errorRate: errorScore,
		ttft:      ttftScore,
	}
}

// computeInfraHealth calculates infrastructure health score (0-100)
// Components: Storage (40%) + Compute Resources (30%) + Background Jobs (30%)
func computeInfraHealth(now time.Time, overview *OpsDashboardOverview) opsInfraHealthParts {
	// Storage score: DB critical, Redis less critical
	storageScore := 100.0
	if overview.SystemMetrics != nil {
		if overview.SystemMetrics.DBOK != nil && !*overview.SystemMetrics.DBOK {
			storageScore = 0 // DB failure is critical
		} else if overview.SystemMetrics.RedisOK != nil && !*overview.SystemMetrics.RedisOK {
			storageScore = 50 // Redis failure is degraded but not critical
		}
	}

	// Compute resources score: CPU + Memory
	computeScore := 100.0
	if overview.SystemMetrics != nil {
		cpuScore := 100.0
		if overview.SystemMetrics.CPUUsagePercent != nil {
			cpuPct := clampFloat64(*overview.SystemMetrics.CPUUsagePercent, 0, 100)
			if cpuPct > 80 {
				if cpuPct <= 100 {
					cpuScore = (100 - cpuPct) / 20 * 100
				} else {
					cpuScore = 0
				}
			}
		}

		memScore := 100.0
		if overview.SystemMetrics.MemoryUsagePercent != nil {
			memPct := clampFloat64(*overview.SystemMetrics.MemoryUsagePercent, 0, 100)
			if memPct > 85 {
				if memPct <= 100 {
					memScore = (100 - memPct) / 15 * 100
				} else {
					memScore = 0
				}
			}
		}

		computeScore = (cpuScore + memScore) / 2
	}

	// Background jobs score
	jobScore := 100.0
	totalJobs := 0
	failedJobs := make([]string, 0)
	staleJobs := make([]string, 0)
	for _, hb := range overview.JobHeartbeats {
		if hb == nil {
			continue
		}
		totalJobs++
		switch classifyOpsJobHeartbeat(now, hb) {
		case opsJobFailed:
			failedJobs = append(failedJobs, hb.JobName)
		case opsJobStale:
			staleJobs = append(staleJobs, hb.JobName)
		case opsJobHealthy:
		}
	}
	if unhealthy := len(failedJobs) + len(staleJobs); totalJobs > 0 && unhealthy > 0 {
		jobScore = (1 - float64(unhealthy)/float64(totalJobs)) * 100
	}

	// Weighted combination
	return opsInfraHealthParts{
		total:      storageScore*0.4 + computeScore*0.3 + jobScore*0.3,
		storage:    storageScore,
		compute:    computeScore,
		jobs:       jobScore,
		failedJobs: failedJobs,
		staleJobs:  staleJobs,
	}
}

const (
	// opsJobStaleToleranceFactor 是「自报周期」到「判定失联」之间的容忍倍数：
	// 3 倍留给一次执行超时 + 一次重试的余量。
	opsJobStaleToleranceFactor = 3
	// opsJobStaleFloor 是判活阈值的下限：秒级任务不会因为周期短就被苛刻对待；
	// 尚未自报周期的旧心跳行也用它，与引入自报周期前的行为一致。
	opsJobStaleFloor = 15 * time.Minute
)

type opsJobHealth int

const (
	opsJobHealthy opsJobHealth = iota
	// opsJobFailed：最近一次运行显式报错（last_error_at 晚于 last_success_at）。
	opsJobFailed
	// opsJobStale：没有报错，但距上次成功已超过按自身周期算出的阈值。
	opsJobStale
)

// opsJobStaleThreshold 返回该任务「多久没成功才算失联」。
// ok=false 表示任务自报当前没有调度（周期 0），不参与失联判定。
func opsJobStaleThreshold(hb *OpsJobHeartbeat) (time.Duration, bool) {
	if hb == nil || hb.ExpectedIntervalSeconds == nil {
		return opsJobStaleFloor, true
	}
	if *hb.ExpectedIntervalSeconds <= 0 {
		return 0, false
	}
	threshold := time.Duration(*hb.ExpectedIntervalSeconds) * time.Second * opsJobStaleToleranceFactor
	if threshold < opsJobStaleFloor {
		threshold = opsJobStaleFloor
	}
	return threshold, true
}

// classifyOpsJobHeartbeat 判定单个后台任务的健康状态。
// 显式报错优先于周期判活：任务刚跑过但报了错，周期再宽也算失败。
func classifyOpsJobHeartbeat(now time.Time, hb *OpsJobHeartbeat) opsJobHealth {
	if hb == nil {
		return opsJobHealthy
	}
	if hb.LastErrorAt != nil && (hb.LastSuccessAt == nil || hb.LastErrorAt.After(*hb.LastSuccessAt)) {
		return opsJobFailed
	}
	// 判活看的是「任务循环还在转」，所以取成功与运行里更晚的那个：没有工作可做的一轮
	// 只写 last_run_at（见 recordOpsJobSkipped），它同样是活着的证据。
	alive := opsJobLastAliveAt(hb)
	if alive == nil {
		// A cron job declares its interval as soon as the schedule is installed,
		// before its first callback. Use the heartbeat row timestamp as the
		// provisional liveness point so a callback that never fires eventually
		// becomes stale instead of remaining healthy forever.
		if hb.ExpectedIntervalSeconds == nil || *hb.ExpectedIntervalSeconds <= 0 || hb.UpdatedAt.IsZero() {
			return opsJobHealthy
		}
		alive = &hb.UpdatedAt
	}
	threshold, ok := opsJobStaleThreshold(hb)
	if ok && now.Sub(*alive) > threshold {
		return opsJobStale
	}
	return opsJobHealthy
}

// opsJobLastAliveAt 返回最后一次「证明任务还活着」的时间：成功与运行取更晚者。
func opsJobLastAliveAt(hb *OpsJobHeartbeat) *time.Time {
	if hb == nil {
		return nil
	}
	alive := hb.LastSuccessAt
	if hb.LastRunAt != nil && (alive == nil || hb.LastRunAt.After(*alive)) {
		alive = hb.LastRunAt
	}
	return alive
}

func clampFloat64(v float64, min float64, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
