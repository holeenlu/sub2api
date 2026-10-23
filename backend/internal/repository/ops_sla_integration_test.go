//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsSLAHistoryMatchesWritesAndAggregates(t *testing.T) {
	ctx := context.Background()
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	start := time.Date(2096, 3, 9, 4, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	const model = "ops-sla-439-regression"
	const payload = "Request payload is too large"
	type expected struct {
		limited            bool
		owner, phase, kind string
	}
	wants := map[string]expected{}
	for _, tc := range []struct {
		id, owner, phase, kind, message  string
		status, upstream                 int
		oldLimited, wantLimited, payload bool
	}{
		{"payload", "provider", "upstream", "invalid_request_error", payload, 400, 400, false, true, true},
		{"wrapped", "provider", "upstream", "api_error", payload, 502, 413, false, true, true},
		{"in-band", "provider", "upstream", "api_error", payload, 400, 200, false, true, true},
		{"pool503", "platform", "routing", "api_error", "No available accounts", 503, 0, true, false, false},
		{"pool429", "platform", "routing", "rate_limit_error", "All available accounts are rate-limited", 429, 0, true, false, false},
		{"pool-stale-evidence", "platform", "routing", "api_error", payload, 503, 413, true, false, false},
		{"user429", "client", "request", "rate_limit_error", "concurrency limit exceeded", 429, 0, true, true, false},
		{"model", "platform", "routing", "model_not_found", "Model not supported by any configured account", 404, 0, true, true, false},
		{"account-auth", "provider", "account_auth", "api_error", payload, 503, 400, false, false, false},
		{"provider503", "provider", "upstream", "api_error", payload, 503, 503, false, false, false},
		{"invalid", "provider", "upstream", "invalid_request_error", "Encrypted content could not be verified", 400, 400, false, false, false},
		{"recovered", "provider", "upstream", "api_error", payload, 200, 413, false, false, false},
	} {
		source := "gateway"
		if tc.owner == "provider" {
			source = "upstream_http"
		}
		entry := &service.OpsInsertErrorLogInput{RequestID: tc.id, Platform: "openai", Model: model,
			ErrorOwner: tc.owner, ErrorPhase: tc.phase, ErrorType: tc.kind, ErrorSource: source,
			StatusCode: tc.status, ErrorMessage: "original client message", UpstreamErrorMessage: &tc.message,
			ErrorBody: "original diagnostic body", Severity: "P2", IsBusinessLimited: tc.oldLimited, CreatedAt: start.Add(time.Minute)}
		if tc.upstream > 0 {
			entry.UpstreamStatusCode = &tc.upstream
		}
		id, err := repo.InsertErrorLog(ctx, entry) // bypass new-log normalization: legacy storage
		require.NoError(t, err)
		want := expected{tc.wantLimited, tc.owner, tc.phase, tc.kind}
		if tc.payload {
			want.owner, want.phase, want.kind = "client", "request", "context_limit"
		}
		wants[tc.id] = want
		detail, err := repo.GetErrorLogByID(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, expected{detail.IsBusinessLimited, detail.Owner, detail.Phase, detail.Type}, tc.id)
		require.Equal(t, entry.UpstreamStatusCode, detail.UpstreamStatusCode, tc.id)
		require.Equal(t, tc.message, detail.UpstreamErrorMessage, tc.id)
		require.Equal(t, source, detail.Source, tc.id)
		require.Equal(t, entry.ErrorBody, detail.ErrorBody, tc.id)

		// The same evidence through the real service write path must persist the
		// classification used by SQL, not merely display a corrected value.
		fresh := *entry
		fresh.RequestID, fresh.Model, fresh.CreatedAt = "new-"+tc.id, model+"-new", start.Add(48*time.Hour)
		require.NoError(t, svc.RecordError(ctx, &fresh))
		var got expected
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT is_business_limited, error_owner, error_phase, error_type FROM ops_error_logs WHERE request_id=$1 AND model=$2", fresh.RequestID, fresh.Model).Scan(&got.limited, &got.owner, &got.phase, &got.kind))
		require.Equal(t, want, got, tc.id)
	}
	snapshot := func() string {
		var raw string
		require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT jsonb_agg(to_jsonb(e) ORDER BY id)::text FROM ops_error_logs e WHERE model=$1", model).Scan(&raw))
		return raw
	}
	rawBefore := snapshot()
	const wantTotal, wantExcluded, wantSLA = 11, 5, 6
	filter := &service.OpsDashboardFilter{StartTime: start, EndTime: end, Platform: "openai"}
	total, excluded, sla, _, _, _, err := repo.queryErrorCounts(ctx, filter, start, end)
	require.NoError(t, err)
	require.EqualValues(t, wantTotal, total)
	require.EqualValues(t, wantExcluded, excluded)
	require.EqualValues(t, wantSLA, sla)
	trend, err := repo.GetErrorTrend(ctx, filter, 60)
	require.NoError(t, err)
	var trendTotal, trendExcluded, trendSLA int64
	for _, p := range trend.Points {
		trendTotal += p.ErrorCountTotal
		trendExcluded += p.BusinessLimitedCount
		trendSLA += p.ErrorCountSLA
	}
	require.Equal(t, []int64{total, excluded, sla}, []int64{trendTotal, trendExcluded, trendSLA})
	distribution, err := repo.GetErrorDistribution(ctx, filter)
	require.NoError(t, err)
	var distributionExcluded, distributionSLA int64
	for _, item := range distribution.Items {
		distributionExcluded += item.BusinessLimited
		distributionSLA += item.SLA
	}
	require.Equal(t, []int64{total, excluded, sla}, []int64{distribution.Total, distributionExcluded, distributionSLA})
	for _, view := range []string{"errors", "excluded", "all"} {
		result, err := repo.ListErrorLogs(ctx, &service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Model: model, View: view, PageSize: 100})
		require.NoError(t, err)
		wantCount := map[string]int{"errors": wantSLA, "excluded": wantExcluded, "all": wantTotal}[view]
		require.Equal(t, wantCount, result.Total, view)
		for _, item := range result.Errors {
			want, ok := wants[item.RequestID]
			require.True(t, ok)
			require.Equal(t, []string{want.owner, want.phase, want.kind}, []string{item.Owner, item.Phase, item.Type})
			if view != "all" {
				require.Equal(t, view == "excluded", want.limited)
			}
		}
	}
	// Responsibility filters must agree with the displayed historical values.
	result, err := repo.ListErrorLogs(ctx, &service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Model: model, View: "excluded", Owner: "client", Phase: "request", ErrorTypesAny: []string{"context_limit"}})
	require.NoError(t, err)
	require.Equal(t, 3, result.Total)
	for _, includeRecovered := range []bool{false, true} {
		result, err := repo.ListErrorLogs(ctx, &service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Model: model, View: "all", Phase: "upstream", RequestID: "recovered", IncludeRecoveredUpstream: includeRecovered})
		require.NoError(t, err)
		if includeRecovered {
			require.Equal(t, 1, result.Total)
		} else {
			require.Zero(t, result.Total)
		}
	}
	require.NoError(t, repo.UpsertHourlyMetrics(ctx, start, end))
	var aggTotal, aggExcluded, aggSLA int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT error_count_total, business_limited_count, error_count_sla FROM ops_metrics_hourly WHERE bucket_start=$1 AND platform IS NULL AND group_id IS NULL", start).Scan(&aggTotal, &aggExcluded, &aggSLA))
	require.Equal(t, []int64{total, excluded, sla}, []int64{aggTotal, aggExcluded, aggSLA})
	day := start.Truncate(24 * time.Hour)
	require.NoError(t, repo.UpsertDailyMetrics(ctx, day, day.Add(24*time.Hour)))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT error_count_total, business_limited_count, error_count_sla FROM ops_metrics_daily WHERE bucket_date=$1 AND platform IS NULL AND group_id IS NULL", day).Scan(&aggTotal, &aggExcluded, &aggSLA))
	require.Equal(t, []int64{total, excluded, sla}, []int64{aggTotal, aggExcluded, aggSLA})
	// Channel monitoring retains actual error totals. Canonical context-limit
	// metadata is recognized without treating excluded failures as successes.
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, channelMonitorV2ErrorAggregationSQL, start.Add(48*time.Hour), end.Add(48*time.Hour))
	require.NoError(t, err)
	var channelErrors, contextLimits int64
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT sum(error_requests) FROM channel_monitor_v2_metrics_1m WHERE model=$1", model+"-new").Scan(&channelErrors))
	require.EqualValues(t, wantTotal, channelErrors)
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT sum(error_requests) FROM channel_monitor_v2_error_metrics_1m WHERE model=$1 AND error_category='context_limit'", model+"-new").Scan(&contextLimits))
	require.EqualValues(t, 3, contextLimits)
	require.NoError(t, tx.Rollback())
	require.Equal(t, rawBefore, snapshot(), "history queries and recomputation must not rewrite raw evidence")
}
