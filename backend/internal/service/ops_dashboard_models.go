package service

import "time"

type OpsDashboardFilter struct {
	StartTime time.Time
	EndTime   time.Time

	Platform string
	GroupID  *int64

	// QueryMode controls whether dashboard queries should use raw logs or pre-aggregated tables.
	// Expected values: auto/raw/preagg (see OpsQueryMode).
	QueryMode OpsQueryMode
}

type OpsRateSummary struct {
	Current float64 `json:"current"`
	Peak    float64 `json:"peak"`
	Avg     float64 `json:"avg"`
}

type OpsPercentiles struct {
	P50 *int `json:"p50_ms"`
	P90 *int `json:"p90_ms"`
	P95 *int `json:"p95_ms"`
	P99 *int `json:"p99_ms"`
	Avg *int `json:"avg_ms"`
	Max *int `json:"max_ms"`
}

type OpsDashboardOverview struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Platform  string    `json:"platform"`
	GroupID   *int64    `json:"group_id"`

	// HealthScore is a backend-computed overall health score (0-100).
	// It is derived from the monitored metrics in this overview, plus best-effort system metrics/job heartbeats.
	HealthScore int `json:"health_score"`
	// HealthScoreBreakdown 解释 HealthScore 的各子分与失联/失败任务；空闲时为 nil。
	HealthScoreBreakdown *OpsHealthScoreBreakdown `json:"health_score_breakdown,omitempty"`

	// Latest system-level snapshot (window=1m, global).
	SystemMetrics *OpsSystemMetricsSnapshot `json:"system_metrics"`

	// Background jobs health (heartbeats).
	JobHeartbeats []*OpsJobHeartbeat `json:"job_heartbeats"`

	SuccessCount         int64 `json:"success_count"`
	ErrorCountTotal      int64 `json:"error_count_total"`
	BusinessLimitedCount int64 `json:"business_limited_count"`

	ErrorCountSLA     int64 `json:"error_count_sla"`
	RequestCountTotal int64 `json:"request_count_total"`
	RequestCountSLA   int64 `json:"request_count_sla"`

	TokenConsumed int64 `json:"token_consumed"`

	SLA                          float64 `json:"sla"`
	ErrorRate                    float64 `json:"error_rate"`
	UpstreamErrorRate            float64 `json:"upstream_error_rate"`
	UpstreamErrorCountExcl429529 int64   `json:"upstream_error_count_excl_429_529"`
	Upstream429Count             int64   `json:"upstream_429_count"`
	Upstream529Count             int64   `json:"upstream_529_count"`

	QPS OpsRateSummary `json:"qps"`
	TPS OpsRateSummary `json:"tps"`

	Duration OpsPercentiles `json:"duration"`
	TTFT     OpsPercentiles `json:"ttft"`
}

// OpsHealthScoreBreakdown 是健康评分的明细：各子分均为 0-100。
// 总分 = Business×0.7 + Infra×0.3；Business = ErrorRate×0.5 + TTFT×0.5；
// Infra = Storage×0.4 + Compute×0.3 + Jobs×0.3。
type OpsHealthScoreBreakdown struct {
	Business  float64 `json:"business"`
	ErrorRate float64 `json:"error_rate"`
	TTFT      float64 `json:"ttft"`
	// TTFTFullScoreMs 是当前生效的 TTFT 满分点（= ttft_p99_ms_max 或默认值），零分点为其 3 倍。
	TTFTFullScoreMs float64 `json:"ttft_full_score_ms"`

	Infra   float64 `json:"infra"`
	Storage float64 `json:"storage"`
	Compute float64 `json:"compute"`
	Jobs    float64 `json:"jobs"`
	// FailedJobs 最近一次运行显式报错的任务；StaleJobs 超过自身周期阈值没有成功的任务。
	FailedJobs []string `json:"failed_jobs"`
	StaleJobs  []string `json:"stale_jobs"`
}

type OpsLatencyHistogramBucket struct {
	Range string `json:"range"`
	Count int64  `json:"count"`
}

// OpsLatencyHistogramResponse is a coarse latency distribution histogram (success requests only).
// It is used by the Ops dashboard to quickly identify tail latency regressions.
type OpsLatencyHistogramResponse struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Platform  string    `json:"platform"`
	GroupID   *int64    `json:"group_id"`

	TotalRequests int64                        `json:"total_requests"`
	Buckets       []*OpsLatencyHistogramBucket `json:"buckets"`
}
