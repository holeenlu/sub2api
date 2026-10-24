//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestComputeDashboardHealthScore_IdleReturns100(t *testing.T) {
	t.Parallel()

	score := computeDashboardHealthScore(time.Now().UTC(), &OpsDashboardOverview{}, nil)
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

	score := computeDashboardHealthScore(time.Now().UTC(), ov, nil)
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
			wantMin: 89,
			wantMax: 89,
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
			wantMin: 49,
			wantMax: 49,
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
			wantMin: 71,
			wantMax: 73,
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
			wantMin: 49,
			wantMax: 49,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeDashboardHealthScore(time.Now().UTC(), tt.overview, nil)
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
			wantMin: 92,
			wantMax: 92,
		},
		{
			name: "error rate 5%",
			overview: &OpsDashboardOverview{
				SLA:               0.95,
				ErrorRate:         0.05,
				UpstreamErrorRate: 0,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 60,
			wantMax: 60,
		},
		{
			name: "TTFT boundary 2s",
			overview: &OpsDashboardOverview{
				SLA:               0.99,
				ErrorRate:         0,
				UpstreamErrorRate: 0,
				TTFT:              OpsPercentiles{P99: intPtr(2000)},
			},
			wantMin: 100,
			wantMax: 100,
		},
		{
			name: "upstream error dominates",
			overview: &OpsDashboardOverview{
				SLA:               0.995,
				ErrorRate:         0.001,
				UpstreamErrorRate: 0.03,
				Duration:          OpsPercentiles{P99: intPtr(500)},
			},
			wantMin: 76,
			wantMax: 76,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeBusinessHealth(tt.overview, nil)
			require.GreaterOrEqual(t, score, tt.wantMin, "score should be >= %.1f", tt.wantMin)
			require.LessOrEqual(t, score, tt.wantMax, "score should be <= %.1f", tt.wantMax)
			require.GreaterOrEqual(t, score, 0.0, "score must be >= 0")
			require.LessOrEqual(t, score, 100.0, "score must be <= 100")
		})
	}
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
			score := computeInfraHealth(now, tt.overview)
			require.GreaterOrEqual(t, score, tt.wantMin, "score should be >= %.1f", tt.wantMin)
			require.LessOrEqual(t, score, tt.wantMax, "score should be <= %.1f", tt.wantMax)
			require.GreaterOrEqual(t, score, 0.0, "score must be >= 0")
			require.LessOrEqual(t, score, 100.0, "score must be <= 100")
		})
	}
}

func timePtr(v time.Time) *time.Time { return &v }

func stringPtr(v string) *string { return &v }

func TestOpsHealthCalibrationAndFailureBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	thresholds := &OpsMetricThresholds{TTFTp99MsMax: float64Ptr(60_000), RequestErrorRatePercentMax: float64Ptr(5), UpstreamErrorRatePercentMax: float64Ptr(5)}
	screenshot := OpsDashboardOverview{RequestCountTotal: 353, RequestCountSLA: 353, SuccessCount: 350, ErrorCountTotal: 3, ErrorCountSLA: 3, SLA: 350.0 / 353, ErrorRate: 3.0 / 353, UpstreamErrorRate: 2.0 / 353, TTFT: OpsPercentiles{P99: intPtr(21827)}, Duration: OpsPercentiles{P99: intPtr(111528)},
		SystemMetrics: &OpsSystemMetricsSnapshot{DBOK: boolPtr(true), RedisOK: boolPtr(true), CPUUsagePercent: float64Ptr(2.4), MemoryUsagePercent: float64Ptr(9.6)},
		JobHeartbeats: []*OpsJobHeartbeat{
			{JobName: "ops_cleanup", ExpectedIntervalSeconds: 86400, LastSuccessAt: timePtr(now.Add(-6 * time.Hour))},
			{JobName: "ops_preaggregation_daily", ExpectedIntervalSeconds: 3600, LastSuccessAt: timePtr(now.Add(-35 * time.Minute))},
		}}
	require.Equal(t, 95, computeDashboardHealthScore(now, &screenshot, thresholds))
	// Existing configured limits are honored, and total generation duration is not a penalty.
	strict := *thresholds
	strict.TTFTp99MsMax = float64Ptr(10000)
	require.Less(t, computeDashboardHealthScore(now, &screenshot, &strict), 90)
	screenshot.Duration.P99 = intPtr(500000)
	require.Equal(t, 95, computeDashboardHealthScore(now, &screenshot, thresholds))
	previous := 101
	for _, rate := range []float64{0, 0.0085, 0.02, 0.05, 0.1, 1} {
		sample := screenshot
		sample.ErrorRate = rate
		sample.UpstreamErrorRate = 0
		score := computeDashboardHealthScore(now, &sample, thresholds)
		require.LessOrEqual(t, score, previous)
		previous = score
		if rate >= 0.1 {
			require.Less(t, score, 60)
		}
	}
	for _, traffic := range []int64{0, 353} {
		sample := screenshot
		sample.RequestCountTotal = traffic
		sample.RequestCountSLA = traffic
		sample.ErrorCountTotal = 0
		sample.SystemMetrics = &OpsSystemMetricsSnapshot{DBOK: boolPtr(false)}
		require.Less(t, computeDashboardHealthScore(now, &sample, thresholds), 60, "DB failure cannot be hidden by idle or successful requests")
	}
	require.Equal(t, 10000.0, *defaultOpsMetricThresholds().TTFTp99MsMax)
}

func TestOpsJobHealthFollowsExpectedSchedule(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name string
		hb   *OpsJobHeartbeat
		want string
	}{
		{"daily job still on schedule", &OpsJobHeartbeat{ExpectedIntervalSeconds: 86400, LastSuccessAt: timePtr(now.Add(-6 * time.Hour))}, "healthy"},
		{"hourly job still on schedule", &OpsJobHeartbeat{ExpectedIntervalSeconds: 3600, LastSuccessAt: timePtr(now.Add(-35 * time.Minute))}, "healthy"},
		{"three missed collector cycles", &OpsJobHeartbeat{ExpectedIntervalSeconds: 60, LastSuccessAt: timePtr(now.Add(-4 * time.Minute))}, "stale"},
		{"newer failure", &OpsJobHeartbeat{LastSuccessAt: timePtr(now.Add(-time.Minute)), LastErrorAt: &now}, "failed"},
		{"recovered failure", &OpsJobHeartbeat{LastSuccessAt: &now, LastErrorAt: timePtr(now.Add(-time.Minute))}, "healthy"},
		{"disabled reports", &OpsJobHeartbeat{JobName: "ops_scheduled_reports", LastResult: stringPtr("skipped: no scheduled reports enabled"), LastRunAt: timePtr(now.Add(-10 * 24 * time.Hour))}, "unknown"},
		{"never run", &OpsJobHeartbeat{}, "unknown"},
		{"legacy interval fallback", &OpsJobHeartbeat{LastSuccessAt: timePtr(now.Add(-16 * time.Minute))}, "stale"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, opsJobHealthStatus(now, tc.hb)) })
	}
}

// Exercise the overview path, not only the scoring function: configured limits
// and persisted job schedules must reach the API response.
type opsHealthOverviewRepo struct {
	opsRepoMock
	overview *OpsDashboardOverview
	jobs     []*OpsJobHeartbeat
}

func (r *opsHealthOverviewRepo) GetDashboardOverview(context.Context, *OpsDashboardFilter) (*OpsDashboardOverview, error) {
	return r.overview, nil
}
func (r *opsHealthOverviewRepo) ListJobHeartbeats(context.Context) ([]*OpsJobHeartbeat, error) {
	return r.jobs, nil
}
func TestOpsDashboardUsesConfiguredHealthLimitsAndJobStatus(t *testing.T) {
	now := time.Now().UTC()
	settings := newStubSettingRepo()
	settings.values[SettingKeyOpsMetricThresholds] = `{"ttft_p99_ms_max":60000}`
	repo := &opsHealthOverviewRepo{overview: &OpsDashboardOverview{RequestCountTotal: 353, RequestCountSLA: 353, ErrorRate: 3.0 / 353, UpstreamErrorRate: 2.0 / 353, TTFT: OpsPercentiles{P99: intPtr(21827)}}, jobs: []*OpsJobHeartbeat{{JobName: "ops_cleanup", ExpectedIntervalSeconds: 86400, LastSuccessAt: timePtr(now.Add(-6 * time.Hour))}}}
	svc := &OpsService{opsRepo: repo, settingRepo: settings}
	got, err := svc.GetDashboardOverview(context.Background(), &OpsDashboardFilter{StartTime: now.Add(-time.Hour), EndTime: now})
	require.NoError(t, err)
	require.Equal(t, 95, got.HealthScore)
	require.Equal(t, "healthy", got.JobHeartbeats[0].HealthStatus)
	resolved, err := svc.GetMetricThresholds(context.Background())
	require.NoError(t, err)
	require.Equal(t, 60000.0, *resolved.TTFTp99MsMax)
	require.Equal(t, 5.0, *resolved.RequestErrorRatePercentMax)
	require.Equal(t, 99.5, *resolved.SLAPercentMin)
}
