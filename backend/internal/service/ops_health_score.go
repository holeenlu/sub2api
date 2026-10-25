package service

import (
	"math"
	"time"
)

// computeDashboardHealthScore computes a 0-100 health score from the metrics returned by the dashboard overview.
//
// Design goals:
// - Backend-owned scoring (UI only displays).
// - Layered scoring: Request health (70%) + Infrastructure Health (30%)
// - Avoids double-counting (e.g., DB failure affects both infra and business metrics)
// - Conservative + stable: penalize clear degradations; avoid overreacting to missing/idle data.
func computeDashboardHealthScore(now time.Time, overview *OpsDashboardOverview, thresholds *OpsMetricThresholds) int {
	if overview == nil {
		return 0
	}

	// No traffic does not imply that storage or background jobs are healthy.
	businessHealth := computeBusinessHealth(overview, thresholds)
	infraHealth := computeInfraHealth(now, overview)

	// Weighted combination: 70% business + 30% infrastructure
	score := businessHealth*0.7 + infraHealth*0.3
	// Storage outages must never appear healthy just because recent traffic succeeded.
	if sm := overview.SystemMetrics; sm != nil {
		if sm.DBOK != nil && !*sm.DBOK {
			score = math.Min(score, 49)
		} else if sm.RedisOK != nil && !*sm.RedisOK {
			score = math.Min(score, 89)
		}
		if (sm.CPUUsagePercent != nil && *sm.CPUUsagePercent > 90) ||
			(sm.MemoryUsagePercent != nil && *sm.MemoryUsagePercent > 90) {
			score = math.Min(score, 89)
		}
	}
	for _, hb := range overview.JobHeartbeats {
		if status := opsJobHealthStatus(now, hb); status == "failed" || status == "stale" {
			score = math.Min(score, 89)
		}
	}
	return int(math.Round(clampFloat64(score, 0, 100)))
}

// computeBusinessHealth gives reliability 80% of request health and TTFT 20%.
// Whole-response duration is not scored: generation length and reasoning effort
// legitimately vary. Request/upstream failures overlap, so use the worse normalized
// rate rather than adding them or counting the complementary SLA again.
func computeBusinessHealth(overview *OpsDashboardOverview, thresholds *OpsMetricThresholds) float64 {
	defaults := defaultOpsMetricThresholds()
	if thresholds == nil {
		thresholds = defaults
	}
	requestLimit := opsHealthThreshold(thresholds.RequestErrorRatePercentMax, *defaults.RequestErrorRatePercentMax)
	upstreamLimit := opsHealthThreshold(thresholds.UpstreamErrorRatePercentMax, *defaults.UpstreamErrorRatePercentMax)
	errorPressure := math.Max(
		clampFloat64(overview.ErrorRate*100, 0, 100)/requestLimit,
		clampFloat64(overview.UpstreamErrorRate*100, 0, 100)/upstreamLimit,
	)
	// At the configured failure-rate limit reliability is 50, at twice it 0.
	errorScore := clampFloat64(100-50*errorPressure, 0, 100)

	ttftScore := 100.0
	if overview.TTFT.P99 != nil {
		limit := opsHealthThreshold(thresholds.TTFTp99MsMax, *defaults.TTFTp99MsMax)
		pressure := float64(*overview.TTFT.P99) / limit
		// Match the metric warning boundary (80% of its limit), with a gradual
		// penalty up to twice the limit, instead of wiping out half the score at 3s.
		ttftScore = clampFloat64(100-(pressure-0.8)/1.2*100, 0, 100)
	}
	return errorScore*0.8 + ttftScore*0.2
}

func opsHealthThreshold(value *float64, fallback float64) float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return fallback
	}
	// Zero means no tolerance, not disabled. Avoid division by zero while still
	// penalizing any nonzero error/latency, consistent with the metric indicator.
	return math.Max(*value, 0.000001)
}

// opsJobHealthStatus is shared by scoring and the dashboard job diagnostics.
func opsJobHealthStatus(now time.Time, hb *OpsJobHeartbeat) string {
	if hb == nil {
		return "unknown"
	}
	if hb.LastErrorAt != nil && (hb.LastSuccessAt == nil || hb.LastErrorAt.After(*hb.LastSuccessAt)) {
		return "failed"
	}
	if hb.JobName == "ops_scheduled_reports" && hb.LastResult != nil && *hb.LastResult == "skipped: no scheduled reports enabled" {
		return "unknown"
	}
	lastRun := hb.LastSuccessAt
	if hb.LastRunAt != nil && (lastRun == nil || hb.LastRunAt.After(*lastRun)) {
		lastRun = hb.LastRunAt
	}
	if lastRun == nil {
		return "unknown"
	}
	grace := 15 * time.Minute // Legacy/missing interval: keep the previous bound.
	if hb.ExpectedIntervalSeconds > 0 {
		// Three missed cycles, with at least two minutes for scheduler jitter.
		seconds := math.Max(120, float64(hb.ExpectedIntervalSeconds)*3)
		if now.Sub(*lastRun).Seconds() > seconds {
			return "stale"
		}
	} else if now.Sub(*lastRun) > grace {
		return "stale"
	}
	return "healthy"
}

// computeInfraHealth calculates infrastructure health score (0-100)
// Components: Storage (40%) + Compute Resources (30%) + Background Jobs (30%)
func computeInfraHealth(now time.Time, overview *OpsDashboardOverview) float64 {
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
	failedJobs := 0
	totalJobs := 0
	for _, hb := range overview.JobHeartbeats {
		if hb == nil {
			continue
		}
		status := opsJobHealthStatus(now, hb)
		if status == "unknown" {
			continue
		}
		totalJobs++
		if status == "failed" || status == "stale" {
			failedJobs++
		}
	}
	if totalJobs > 0 && failedJobs > 0 {
		jobScore = (1 - float64(failedJobs)/float64(totalJobs)) * 100
	}

	// Weighted combination
	return storageScore*0.4 + computeScore*0.3 + jobScore*0.3
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
