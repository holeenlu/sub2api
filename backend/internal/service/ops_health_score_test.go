//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeDashboardHealthScore_IdleReturns100(t *testing.T) {
	t.Parallel()

	score := mustHealthScore(computeDashboardHealth(time.Now().UTC(), &OpsDashboardOverview{}, nil))
	require.Equal(t, 100, score)
}

func TestComputeDashboardHealthScore_DegradesOnBadSignals(t *testing.T) {
	t.Parallel()

	ov := &OpsDashboardOverview{
		RequestCountTotal: 100,
		RequestCountSLA:   100,
		SuccessCount:      90,
		ErrorCountTotal:   10,
		ErrorCountSLA:     10,

		SLA:               0.90,
		ErrorRate:         0.10,
		UpstreamErrorRate: 0.08,

		Duration: OpsPercentiles{P99: intPtr(20_000)},
		TTFT:     OpsPercentiles{P99: intPtr(2_000)},

		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:                  boolPtr(false),
			RedisOK:               boolPtr(false),
			CPUUsagePercent:       float64Ptr(98.0),
			MemoryUsagePercent:    float64Ptr(97.0),
			DBConnWaiting:         intPtr(3),
			ConcurrencyQueueDepth: intPtr(10),
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{
				JobName:     "job-a",
				LastErrorAt: timePtr(time.Now().UTC().Add(-1 * time.Minute)),
				LastError:   stringPtr("boom"),
			},
		},
	}

	score := mustHealthScore(computeDashboardHealth(time.Now().UTC(), ov, nil))
	require.Less(t, score, 80)
	require.GreaterOrEqual(t, score, 0)
}

func TestComputeDashboardHealthScore_Comprehensive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		overview *OpsDashboardOverview
		wantMin  int
		wantMax  int
	}{
		{
			name:     "nil overview returns 0",
			overview: nil,
			wantMin:  0,
			wantMax:  0,
		},
		{
			name: "perfect health",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               1.0,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
				TTFT:              OpsPercentiles{P99: intPtr(100)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "good health - SLA 99.8%",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.998,
				ErrorRate:         0.003,
				UpstreamErrorRate: 0.001,
				Duration:          OpsPercentiles{P99: intPtr(800)},
				TTFT:              OpsPercentiles{P99: intPtr(200)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(50),
					MemoryUsagePercent: float64Ptr(60),
				},
			},
			wantMin: 95,
			wantMax: 100,
		},
		{
			name: "medium health - SLA 96%",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.96,
				ErrorRate:         0.02,
				UpstreamErrorRate: 0.01,
				Duration:          OpsPercentiles{P99: intPtr(3000)},
				TTFT:              OpsPercentiles{P99: intPtr(600)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(70),
					MemoryUsagePercent: float64Ptr(75),
				},
			},
			wantMin: 96,
			wantMax: 97,
		},
		{
			name: "DB failure",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.995,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(false),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 70,
			wantMax: 90,
		},
		{
			name: "Redis failure",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.995,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(false),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 85,
			wantMax: 95,
		},
		{
			name: "high CPU usage",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.995,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(95),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 85,
			wantMax: 100,
		},
		{
			name: "combined failures - business degraded + infra healthy",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.90,
				ErrorRate:         0.05,
				UpstreamErrorRate: 0.02,
				Duration:          OpsPercentiles{P99: intPtr(10000)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(20),
					MemoryUsagePercent: float64Ptr(30),
				},
			},
			wantMin: 84,
			wantMax: 85,
		},
		{
			name: "combined failures - business healthy + infra degraded",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				RequestCountSLA:   1000,
				SLA:               0.998,
				ErrorRate:         0.001,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(600)},
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(false),
					RedisOK:            boolPtr(false),
					CPUUsagePercent:    float64Ptr(95),
					MemoryUsagePercent: float64Ptr(95),
				},
			},
			wantMin: 70,
			wantMax: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := mustHealthScore(computeDashboardHealth(time.Now().UTC(), tt.overview, nil))
			require.GreaterOrEqual(t, score, tt.wantMin, "score should be >= %d", tt.wantMin)
			require.LessOrEqual(t, score, tt.wantMax, "score should be <= %d", tt.wantMax)
			require.GreaterOrEqual(t, score, 0, "score must be >= 0")
			require.LessOrEqual(t, score, 100, "score must be <= 100")
		})
	}
}

func TestComputeBusinessHealth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		overview *OpsDashboardOverview
		wantMin  float64
		wantMax  float64
	}{
		{
			name: "perfect metrics",
			overview: &OpsDashboardOverview{
				SLA:               1.0,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "SLA boundary 99.5%",
			overview: &OpsDashboardOverview{
				SLA:               0.995,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "SLA boundary 95%",
			overview: &OpsDashboardOverview{
				SLA:               0.95,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "error rate boundary 1%",
			overview: &OpsDashboardOverview{
				SLA:               0.99,
				ErrorRate:         0.01,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "error rate 5%",
			overview: &OpsDashboardOverview{
				SLA:               0.95,
				ErrorRate:         0.05,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 77,
			wantMax: 78,
		},
		{
			name: "TTFT full-score boundary 10s",
			overview: &OpsDashboardOverview{
				SLA:               0.99,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				TTFT:              OpsPercentiles{P99: intPtr(10_000)},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "TTFT midpoint 20s",
			overview: &OpsDashboardOverview{
				SLA:               0.99,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				TTFT:              OpsPercentiles{P99: intPtr(20_000)},
			},
			wantMin: 75,
			wantMax: 75,
		},
		{
			name: "TTFT zero-score boundary 30s",
			overview: &OpsDashboardOverview{
				SLA:               0.99,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				TTFT:              OpsPercentiles{P99: intPtr(30_000)},
			},
			wantMin: 50,
			wantMax: 50,
		},
		{
			name: "TTFT above zero-score boundary stays clamped",
			overview: &OpsDashboardOverview{
				SLA:               0.99,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				TTFT:              OpsPercentiles{P99: intPtr(45_000)},
			},
			wantMin: 50,
			wantMax: 50,
		},
		{
			name: "upstream error dominates",
			overview: &OpsDashboardOverview{
				SLA:               0.995,
				ErrorRate:         0.001,
				UpstreamErrorRate: 0.03,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 88,
			wantMax: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeBusinessHealth(tt.overview, nil).total
			require.GreaterOrEqual(t, score, tt.wantMin, "score should be >= %.1f", tt.wantMin)
			require.LessOrEqual(t, score, tt.wantMax, "score should be <= %.1f", tt.wantMax)
			require.GreaterOrEqual(t, score, 0.0, "score must be >= 0")
			require.LessOrEqual(t, score, 100.0, "score must be <= 100")
		})
	}
}

// TTFT 评分刻度跟着 ttft_p99_ms_max 走：满分点 = 阈值，零分点 = 3 倍阈值；未配置时用默认 10s。
func TestComputeBusinessHealth_TTFTScaleFollowsConfiguredThreshold(t *testing.T) {
	t.Parallel()

	withTTFT := func(ms int) *OpsDashboardOverview {
		return &OpsDashboardOverview{SLA: 0.99, TTFT: OpsPercentiles{P99: intPtr(ms)}}
	}
	thresholdMs := func(v float64) *OpsMetricThresholds {
		return &OpsMetricThresholds{TTFTp99MsMax: &v}
	}

	// 阈值 5000：5s 满分、10s 中点、15s 零分
	require.Equal(t, 100.0, businessTotal(withTTFT(5_000), thresholdMs(5000)))
	require.Equal(t, 75.0, businessTotal(withTTFT(10_000), thresholdMs(5000)))
	require.Equal(t, 50.0, businessTotal(withTTFT(15_000), thresholdMs(5000)))

	// 阈值缺失或为 0 视作未配置，回退默认刻度（10s 满分）
	require.Equal(t, 100.0, businessTotal(withTTFT(10_000), nil))
	require.Equal(t, 100.0, businessTotal(withTTFT(10_000), &OpsMetricThresholds{}))
	require.Equal(t, 100.0, businessTotal(withTTFT(10_000), thresholdMs(0)))
	require.Equal(t, 75.0, businessTotal(withTTFT(20_000), thresholdMs(0)))
}

func TestDefaultOpsMetricThresholds_TTFTMatchesScoreScale(t *testing.T) {
	t.Parallel()

	defaults := defaultOpsMetricThresholds()
	require.NotNil(t, defaults.TTFTp99MsMax)
	require.Equal(t, float64(dashboardTTFTDefaultFullScoreMs), *defaults.TTFTp99MsMax)
	require.Equal(t, float64(dashboardTTFTDefaultFullScoreMs), dashboardTTFTFullScoreMs(defaults))
}

func TestComputeInfraHealth(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()

	tests := []struct {
		name     string
		overview *OpsDashboardOverview
		wantMin  float64
		wantMax  float64
	}{
		{
			name: "all infrastructure healthy",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "DB down",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(false),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 50,
			wantMax: 70,
		},
		{
			name: "Redis down",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(false),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 80,
			wantMax: 95,
		},
		{
			name: "CPU at 90%",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(90),
					MemoryUsagePercent: float64Ptr(40),
				},
			},
			wantMin: 85,
			wantMax: 95,
		},
		{
			name: "failed background job",
			overview: &OpsDashboardOverview{
				RequestCountTotal: 1000,
				SystemMetrics: &OpsSystemMetricsSnapshot{
					DBOK:               boolPtr(true),
					RedisOK:            boolPtr(true),
					CPUUsagePercent:    float64Ptr(30),
					MemoryUsagePercent: float64Ptr(40),
				},
				JobHeartbeats: []*OpsJobHeartbeat{
					{
						JobName:     "test-job",
						LastErrorAt: &now,
					},
				},
			},
			wantMin: 70,
			wantMax: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeInfraHealth(now, tt.overview).total
			require.GreaterOrEqual(t, score, tt.wantMin, "score should be >= %.1f", tt.wantMin)
			require.LessOrEqual(t, score, tt.wantMax, "score should be <= %.1f", tt.wantMax)
			require.GreaterOrEqual(t, score, 0.0, "score must be >= 0")
			require.LessOrEqual(t, score, 100.0, "score must be <= 100")
		})
	}
}

func mustHealthScore(score int, _ *OpsHealthScoreBreakdown) int { return score }

func businessTotal(overview *OpsDashboardOverview, thresholds *OpsMetricThresholds) float64 {
	return computeBusinessHealth(overview, thresholds).total
}

func timePtr(v time.Time) *time.Time { return &v }

func stringPtr(v string) *string { return &v }

// ── 后台任务判活：按心跳自报周期，而不是一刀切 15 分钟 ──────────────────────

func TestOpsJobStaleThreshold(t *testing.T) {
	t.Parallel()

	withInterval := func(seconds int64) *OpsJobHeartbeat {
		return &OpsJobHeartbeat{JobName: "j", ExpectedIntervalSeconds: int64Ptr(seconds)}
	}

	// 低频任务按 3 倍周期放宽
	threshold, ok := opsJobStaleThreshold(withInterval(24 * 3600))
	require.True(t, ok)
	require.Equal(t, 72*time.Hour, threshold)
	threshold, ok = opsJobStaleThreshold(withInterval(7 * 24 * 3600))
	require.True(t, ok)
	require.Equal(t, 21*24*time.Hour, threshold)
	threshold, ok = opsJobStaleThreshold(withInterval(600))
	require.True(t, ok)
	require.Equal(t, 30*time.Minute, threshold)

	// 高频任务不因周期短而被苛刻对待：60s × 3 < 15min，取下限
	threshold, ok = opsJobStaleThreshold(withInterval(60))
	require.True(t, ok)
	require.Equal(t, opsJobStaleFloor, threshold)

	// 旧行（未自报周期）沿用 15 分钟
	threshold, ok = opsJobStaleThreshold(&OpsJobHeartbeat{JobName: "legacy"})
	require.True(t, ok)
	require.Equal(t, opsJobStaleFloor, threshold)

	// 周期 0 = 当前没有调度，不判失联
	_, ok = opsJobStaleThreshold(withInterval(0))
	require.False(t, ok)
}

func TestClassifyOpsJobHeartbeat(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { return timePtr(now.Add(-d)) }

	// collector 60s 周期停更 16 分钟 → 失联（阈值下限 15 分钟）
	require.Equal(t, opsJobStale, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsMetricsCollectorJobName, LastSuccessAt: ago(16 * time.Minute), ExpectedIntervalSeconds: int64Ptr(60),
	}))
	// 每日 cleanup 5 小时前跑过 → 健康；断 73 小时 → 失联
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsCleanupJobName, LastSuccessAt: ago(5 * time.Hour), ExpectedIntervalSeconds: int64Ptr(86400),
	}))
	require.Equal(t, opsJobStale, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsCleanupJobName, LastSuccessAt: ago(73 * time.Hour), ExpectedIntervalSeconds: int64Ptr(86400),
	}))
	// 每周 cron：20 天没跑仍健康，22 天判失联
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsCleanupJobName, LastSuccessAt: ago(20 * 24 * time.Hour), ExpectedIntervalSeconds: int64Ptr(7 * 86400),
	}))
	require.Equal(t, opsJobStale, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsCleanupJobName, LastSuccessAt: ago(22 * 24 * time.Hour), ExpectedIntervalSeconds: int64Ptr(7 * 86400),
	}))
	// 旧行无周期 → 15 分钟
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{JobName: "legacy", LastSuccessAt: ago(14 * time.Minute)}))
	require.Equal(t, opsJobStale, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{JobName: "legacy", LastSuccessAt: ago(16 * time.Minute)}))
	// 被关闭的任务（周期 0）：多久没跑都不算失联
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsCleanupJobName, LastSuccessAt: ago(30 * 24 * time.Hour), ExpectedIntervalSeconds: int64Ptr(0),
	}))
	// 显式报错优先：刚跑过但报了错，周期再宽也算失败；关闭状态下的旧报错同样保留
	require.Equal(t, opsJobFailed, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: opsCleanupJobName, LastSuccessAt: ago(time.Second), LastErrorAt: &now, ExpectedIntervalSeconds: int64Ptr(86400),
	}))
	require.Equal(t, opsJobFailed, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName: "j", LastErrorAt: ago(time.Minute), ExpectedIntervalSeconds: int64Ptr(0),
	}))
	// 只声明了周期、还没跑过 → 健康
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{JobName: "j", ExpectedIntervalSeconds: int64Ptr(86400)}))
}

// 「本轮跳过」的心跳只写 last_run_at：判活要认它，否则没工作可做的任务会被判失联；
// 但它不是一次成功，上一次真实失败必须继续可见。
func TestClassifyOpsJobHeartbeat_SkippedTickCountsAsLiveness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { return timePtr(now.Add(-d)) }

	// 上次真正成功在 2 小时前，但每轮 tick 都写了 skipped 心跳 → 不算失联
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName:                 opsScheduledReportJobName,
		LastRunAt:               ago(time.Minute),
		LastSuccessAt:           ago(2 * time.Hour),
		ExpectedIntervalSeconds: int64Ptr(60),
	}))
	// 连 skipped 心跳都停了 → 失联
	require.Equal(t, opsJobStale, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName:                 opsScheduledReportJobName,
		LastRunAt:               ago(2 * time.Hour),
		LastSuccessAt:           ago(2 * time.Hour),
		ExpectedIntervalSeconds: int64Ptr(60),
	}))
	// skipped 心跳不掩盖上一次失败：last_error_at 仍晚于 last_success_at
	require.Equal(t, opsJobFailed, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName:                 opsCleanupJobName,
		LastRunAt:               ago(time.Minute),
		LastSuccessAt:           ago(26 * time.Hour),
		LastErrorAt:             ago(2 * time.Hour),
		ExpectedIntervalSeconds: int64Ptr(0),
	}))
}

func TestClassifyOpsJobHeartbeat_DeclaredButNeverRunEventuallyStales(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	interval := int64(60)
	recentlyDeclared := now.Add(-14 * time.Minute)
	staleDeclaration := now.Add(-16 * time.Minute)

	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName:                 "declared-recently",
		ExpectedIntervalSeconds: &interval,
		UpdatedAt:               recentlyDeclared,
	}))
	require.Equal(t, opsJobStale, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName:                 "declared-never-ran",
		ExpectedIntervalSeconds: &interval,
		UpdatedAt:               staleDeclaration,
	}))

	disabled := int64(0)
	require.Equal(t, opsJobHealthy, classifyOpsJobHeartbeat(now, &OpsJobHeartbeat{
		JobName:                 "disabled",
		ExpectedIntervalSeconds: &disabled,
		UpdatedAt:               now.Add(-24 * time.Hour),
	}))
}

// 线上现场回归（2026-09-02）：请求错误率 0.54%、TTFT p99 19.7s、5 个后台任务心跳，
// 综合健康评分应当是 83。改动前这一现场只得 61 分——TTFT 被秒级刻度判 0 分，
// ops_cleanup（每日）与 ops_preaggregation_daily（每小时）被一刀切 15 分钟阈值误判失败。
//
// 83 = 业务 75.75 × 0.7 + 基础 100 × 0.3：
//
//	错误率 0.54% ≤ 1% → 100
//	TTFT 19.7s 在 10s~30s 之间 → (30000-19700)/20000 × 100 = 51.5
//	5 个任务都按自身周期判活 → jobScore 100，基础健康分满分
//
// 剩下的扣分全部来自 TTFT，那是上游 prefill 的真实延迟。
func TestComputeDashboardHealth_ProductionIncident20260902(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { return timePtr(now.Add(-d)) }

	overview := &OpsDashboardOverview{
		RequestCountTotal: 20_000,
		RequestCountSLA:   20_000,
		SLA:               0.9946,
		ErrorRate:         0.0054,
		UpstreamErrorRate: 0.0031,
		TTFT:              OpsPercentiles{P99: intPtr(19_700)},
		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:               boolPtr(true),
			RedisOK:            boolPtr(true),
			CPUUsagePercent:    float64Ptr(2),
			MemoryUsagePercent: float64Ptr(18),
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: opsAlertEvaluatorJobName, LastSuccessAt: ago(18 * time.Second), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: opsCleanupJobName, LastSuccessAt: ago(5*time.Hour + 22*time.Minute), ExpectedIntervalSeconds: int64Ptr(86400)},
			{JobName: opsMetricsCollectorJobName, LastSuccessAt: ago(27 * time.Second), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: opsAggDailyJobName, LastSuccessAt: ago(22*time.Minute + 30*time.Second), ExpectedIntervalSeconds: int64Ptr(3600)},
			{JobName: opsAggHourlyJobName, LastSuccessAt: ago(2*time.Minute + 31*time.Second), ExpectedIntervalSeconds: int64Ptr(600)},
		},
	}

	score, breakdown := computeDashboardHealth(now, overview, defaultOpsMetricThresholds())
	require.Equal(t, 83, score)

	require.NotNil(t, breakdown)
	require.Equal(t, 100.0, breakdown.ErrorRate)
	require.Equal(t, 51.5, breakdown.TTFT)
	require.Equal(t, 75.8, breakdown.Business)
	require.Equal(t, 100.0, breakdown.Infra)
	require.Equal(t, 100.0, breakdown.Jobs)
	require.Empty(t, breakdown.FailedJobs)
	require.Empty(t, breakdown.StaleJobs, "5 个任务都没失联，扣分只应来自 TTFT")
}

// 零流量窗口仍要产出基础设施明细：夜间/低峰时段 collector 死掉或任务报错时，
// 前端的任务面板只认 breakdown 里的 failed_jobs/stale_jobs，明细缺席就等于全绿。
func TestComputeDashboardHealth_IdleWindowStillReportsInfra(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 3, 30, 0, 0, time.UTC)
	overview := &OpsDashboardOverview{
		// 窗口内一个请求都没有：SLA / 总数 / 错误数全为 0。
		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:               boolPtr(true),
			RedisOK:            boolPtr(true),
			CPUUsagePercent:    float64Ptr(3),
			MemoryUsagePercent: float64Ptr(20),
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: opsMetricsCollectorJobName, LastSuccessAt: timePtr(now.Add(-16 * time.Minute)), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: opsAlertEvaluatorJobName, LastSuccessAt: timePtr(now.Add(-time.Minute)), LastErrorAt: timePtr(now.Add(-30 * time.Second)), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: opsAggHourlyJobName, LastSuccessAt: timePtr(now.Add(-time.Minute)), ExpectedIntervalSeconds: int64Ptr(600)},
			{JobName: opsAggDailyJobName, LastSuccessAt: timePtr(now.Add(-time.Minute)), ExpectedIntervalSeconds: int64Ptr(3600)},
		},
	}

	score, breakdown := computeDashboardHealth(now, overview, nil)
	require.NotNil(t, breakdown, "零流量也必须下发明细，否则前端任务面板恒为绿")
	require.Equal(t, []string{opsAlertEvaluatorJobName}, breakdown.FailedJobs)
	require.Equal(t, []string{opsMetricsCollectorJobName}, breakdown.StaleJobs)

	// 无流量按满分处理业务分；基础分只被任务子分拉低：
	// jobs = (1 - 2/4)×100 = 50 → infra = 100×0.4 + 100×0.3 + 50×0.3 = 85
	require.Equal(t, 100.0, breakdown.Business)
	require.Equal(t, 50.0, breakdown.Jobs)
	require.Equal(t, 85.0, breakdown.Infra)
	// 总分 = 100×0.7 + 85×0.3 = 95.5 → 96
	require.Equal(t, 96, score)
}

// 评分明细：子分、失联/失败任务名与生效的 TTFT 满分点一起下发。
func TestComputeDashboardHealth_Breakdown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	score, breakdown := computeDashboardHealth(now, &OpsDashboardOverview{}, nil)
	require.Equal(t, 100, score)
	require.NotNil(t, breakdown, "空闲窗口也要有明细")
	require.Equal(t, 100.0, breakdown.Infra)

	_, breakdown = computeDashboardHealth(now, nil, nil)
	require.Nil(t, breakdown)

	ttftMax := 5000.0
	overview := &OpsDashboardOverview{
		RequestCountTotal: 1000,
		RequestCountSLA:   1000,
		SLA:               0.99,
		ErrorRate:         0.055, // 5.5% → errorScore 50
		UpstreamErrorRate: 0.01,
		TTFT:              OpsPercentiles{P99: intPtr(10_000)}, // 阈值 5s 下的中点 → 50
		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:               boolPtr(true),
			RedisOK:            boolPtr(false), // storage 50
			CPUUsagePercent:    float64Ptr(90), // cpu 50
			MemoryUsagePercent: float64Ptr(40), // mem 100 → compute 75
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: "healthy", LastSuccessAt: &now, ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: "stale", LastSuccessAt: timePtr(now.Add(-time.Hour)), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: "failed", LastSuccessAt: timePtr(now.Add(-time.Minute)), LastErrorAt: &now},
			{JobName: "disabled", LastSuccessAt: timePtr(now.Add(-48 * time.Hour)), ExpectedIntervalSeconds: int64Ptr(0)},
		},
	}

	score, breakdown = computeDashboardHealth(now, overview, &OpsMetricThresholds{TTFTp99MsMax: &ttftMax})
	require.NotNil(t, breakdown)

	require.Equal(t, 50.0, breakdown.ErrorRate)
	require.Equal(t, 50.0, breakdown.TTFT)
	require.Equal(t, 50.0, breakdown.Business)
	require.Equal(t, 5000.0, breakdown.TTFTFullScoreMs)

	require.Equal(t, 50.0, breakdown.Storage)
	require.Equal(t, 75.0, breakdown.Compute)
	require.Equal(t, 50.0, breakdown.Jobs, "4 个任务里 1 个失联 1 个失败")
	require.Equal(t, []string{"failed"}, breakdown.FailedJobs)
	require.Equal(t, []string{"stale"}, breakdown.StaleJobs)
	// infra = 50×0.4 + 75×0.3 + 50×0.3 = 57.5
	require.Equal(t, 57.5, breakdown.Infra)

	// 总分 = 50×0.7 + 57.5×0.3 = 52.25 → 52
	require.Equal(t, 52, score)

	// 没有任务时列表为空数组而非 nil，前端不必判空
	_, breakdown = computeDashboardHealth(now, &OpsDashboardOverview{RequestCountTotal: 1}, nil)
	require.NotNil(t, breakdown)
	require.Empty(t, breakdown.FailedJobs)
	require.NotNil(t, breakdown.FailedJobs)
	require.NotNil(t, breakdown.StaleJobs)
	require.Equal(t, float64(dashboardTTFTDefaultFullScoreMs), breakdown.TTFTFullScoreMs)
}

// 复现线上现场：两个低频任务刚跑完不久、从未出错，不该被算作失败。
func TestComputeInfraHealth_LowCadenceJobsAreNotStale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	healthyAt := func(d time.Duration) *time.Time { return timePtr(now.Add(-d)) }

	overview := &OpsDashboardOverview{
		RequestCountTotal: 1000,
		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:               boolPtr(true),
			RedisOK:            boolPtr(true),
			CPUUsagePercent:    float64Ptr(2),
			MemoryUsagePercent: float64Ptr(18),
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: opsAlertEvaluatorJobName, LastSuccessAt: healthyAt(18 * time.Second), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: opsCleanupJobName, LastSuccessAt: healthyAt(5*time.Hour + 22*time.Minute), ExpectedIntervalSeconds: int64Ptr(86400)},
			{JobName: opsMetricsCollectorJobName, LastSuccessAt: healthyAt(27 * time.Second), ExpectedIntervalSeconds: int64Ptr(60)},
			{JobName: opsAggDailyJobName, LastSuccessAt: healthyAt(22*time.Minute + 30*time.Second), ExpectedIntervalSeconds: int64Ptr(3600)},
			{JobName: opsAggHourlyJobName, LastSuccessAt: healthyAt(2*time.Minute + 31*time.Second), ExpectedIntervalSeconds: int64Ptr(600)},
		},
	}

	require.Equal(t, 100.0, computeInfraHealth(now, overview).total, "低频任务按自身周期判活后，基础健康分应当满分")
}

// 真正失联的低频任务仍要被抓出来：超过 3 倍周期就算失败。
func TestComputeInfraHealth_LowCadenceJobStillDetectedWhenTrulyStale(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 2, 7, 21, 0, 0, time.UTC)
	overview := &OpsDashboardOverview{
		RequestCountTotal: 1000,
		SystemMetrics: &OpsSystemMetricsSnapshot{
			DBOK:               boolPtr(true),
			RedisOK:            boolPtr(true),
			CPUUsagePercent:    float64Ptr(2),
			MemoryUsagePercent: float64Ptr(18),
		},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: opsCleanupJobName, LastSuccessAt: timePtr(now.Add(-73 * time.Hour)), ExpectedIntervalSeconds: int64Ptr(86400)},
			{JobName: opsAggHourlyJobName, LastSuccessAt: &now, ExpectedIntervalSeconds: int64Ptr(600)},
		},
	}

	// 2 个任务失败 1 个 → jobScore 50 → 40 + 30 + 15 = 85
	require.Equal(t, 85.0, computeInfraHealth(now, overview).total)
}
