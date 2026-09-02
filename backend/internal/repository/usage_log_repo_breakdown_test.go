//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResolveEndpointColumn(t *testing.T) {
	tests := []struct {
		endpointType string
		want         string
	}{
		{"inbound", "ul.inbound_endpoint"},
		{"upstream", "ul.upstream_endpoint"},
		{"path", "ul.inbound_endpoint || ' -> ' || ul.upstream_endpoint"},
		{"", "ul.inbound_endpoint"},        // default
		{"unknown", "ul.inbound_endpoint"}, // fallback
	}

	for _, tc := range tests {
		t.Run(tc.endpointType, func(t *testing.T) {
			got := resolveEndpointColumn(tc.endpointType)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveModelDimensionExpression(t *testing.T) {
	tests := []struct {
		modelType string
		want      string
	}{
		{usagestats.ModelSourceRequested, "COALESCE(NULLIF(TRIM(requested_model), ''), model)"},
		{usagestats.ModelSourceUpstream, "COALESCE(NULLIF(TRIM(upstream_model), ''), model)"},
		{usagestats.ModelSourceMapping, "(COALESCE(NULLIF(TRIM(requested_model), ''), model) || ' -> ' || COALESCE(NULLIF(TRIM(upstream_model), ''), model))"},
		{"", "COALESCE(NULLIF(TRIM(requested_model), ''), model)"},
		{"invalid", "COALESCE(NULLIF(TRIM(requested_model), ''), model)"},
	}

	for _, tc := range tests {
		t.Run(tc.modelType, func(t *testing.T) {
			got := resolveModelDimensionExpression(tc.modelType)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestResolveBreakdownOrderBy(t *testing.T) {
	tests := []struct {
		sortBy string
		want   string
	}{
		{"total_tokens", "total_tokens"},
		{"input_tokens", "input_tokens"},
		{"output_tokens", "output_tokens"},
		{"cache_tokens", "cache_tokens"},
		{"requests", "requests"},
		{"cost", "cost"},
		{"actual_cost", "actual_cost"},
		{"", "actual_cost"},                   // default
		{"account_cost", "actual_cost"},       // not exposed for sorting
		{"user_id", "actual_cost"},            // identity column, not a metric
		{"total_tokens; DROP", "actual_cost"}, // never echoed back into SQL
	}

	for _, tc := range tests {
		t.Run(tc.sortBy, func(t *testing.T) {
			require.Equal(t, tc.want, resolveBreakdownOrderBy(tc.sortBy))
		})
	}
}

// 用户排行的 SQL 文本是后续按其他维度聚合的排行的基准，这里按字面断言全文，
// 防止抽公共 helper 时悄悄改动占位符编号或子句顺序。
func TestGetUserBreakdownStatsExactQueryText(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	stream := true
	billingType := int8(1)

	want := `
		SELECT
			COALESCE(ul.user_id, 0) as user_id,
			COALESCE(u.email, '') as email,
			COUNT(*) as requests,
			COALESCE(SUM(ul.input_tokens), 0) as input_tokens,
			COALESCE(SUM(ul.output_tokens), 0) as output_tokens,
			COALESCE(SUM(ul.cache_creation_tokens + ul.cache_read_tokens), 0) as cache_tokens,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(ul.total_cost), 0) as cost,
			COALESCE(SUM(ul.actual_cost), 0) as actual_cost,
			COALESCE(SUM(COALESCE(ul.account_stats_cost, ul.total_cost) * COALESCE(ul.account_rate_multiplier, 1)), 0) as account_cost
		FROM usage_logs ul
		LEFT JOIN users u ON u.id = ul.user_id
		WHERE ul.created_at >= $1 AND ul.created_at < $2
	` +
		" AND ul.group_id = $3" +
		" AND COALESCE(NULLIF(TRIM(upstream_model), ''), model) = $4" +
		" AND ul.upstream_endpoint = $5" +
		" AND ul.user_id = $6" +
		" AND ul.api_key_id = $7" +
		" AND ul.account_id = $8" +
		" AND ul.stream = $9" +
		" AND ul.billing_type = $10" +
		" GROUP BY ul.user_id, u.email ORDER BY total_tokens DESC LIMIT 20"

	mock.ExpectQuery(want).
		WithArgs(start, end, int64(3), "claude-fable-5", "/v1/messages", int64(7), int64(9), int64(11), true, billingType).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "email", "requests", "input_tokens", "output_tokens",
			"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
		}))

	rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		GroupID:      3,
		Model:        "claude-fable-5",
		ModelType:    usagestats.ModelSourceUpstream,
		Endpoint:     "/v1/messages",
		EndpointType: "upstream",
		UserID:       7,
		APIKeyID:     9,
		AccountID:    11,
		Stream:       &stream,
		BillingType:  &billingType,
		SortBy:       "total_tokens",
	}, 20)

	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

var apiKeyBreakdownColumns = []string{
	"api_key_id", "key_name", "key_deleted", "user_id", "email", "requests",
	"input_tokens", "output_tokens", "cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
}

func TestGetAPIKeyBreakdownStatsKeepsDeletedKeysAndAggregatesByKey(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("AND ul.group_id = $3")+".*"+regexp.QuoteMeta("ORDER BY requests DESC LIMIT 50")).
		WithArgs(start, end, int64(3)).
		WillReturnRows(sqlmock.NewRows(apiKeyBreakdownColumns).
			AddRow(int64(5), "prod", false, int64(2), "a@test.com", int64(3), int64(10), int64(20), int64(30), int64(60), 1.5, 1.2, 1.0).
			AddRow(int64(6), "gone", true, int64(2), "a@test.com", int64(1), int64(1), int64(1), int64(0), int64(2), 0.1, 0.1, 0.1).
			AddRow(int64(7), "", true, int64(9), "", int64(1), int64(0), int64(0), int64(0), int64(0), 0.0, 0.0, 0.0))

	rows, err := repo.GetAPIKeyBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		GroupID: 3,
		SortBy:  "requests",
	}, 50)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())

	require.Len(t, rows, 3)
	require.Equal(t, int64(5), rows[0].APIKeyID)
	require.Equal(t, "prod", rows[0].KeyName)
	require.False(t, rows[0].KeyDeleted)
	require.Equal(t, int64(60), rows[0].TotalTokens)
	require.True(t, rows[1].KeyDeleted, "软删除的 Key 必须带标记返回")
	require.True(t, rows[2].KeyDeleted, "Key 行已不存在时同样视为已删除")
	require.Equal(t, int64(9), rows[2].UserID, "Key 行不存在时归属用户回退到日志里的 user_id")
}

// 断言 SQL 形态：LEFT JOIN api_keys 且不过滤 deleted_at、key_deleted 同时覆盖软删与物理删、
// 只按 Key 聚合（ul.user_id 不进 GROUP BY）、共用筛选与排序 allowlist。
func TestGetAPIKeyBreakdownStatsQueryShape(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expectedSQL, actualSQL string) error {
		require.Contains(t, actualSQL, "LEFT JOIN api_keys k ON k.id = ul.api_key_id")
		require.Contains(t, actualSQL, "LEFT JOIN users u ON u.id = k.user_id")
		require.NotContains(t, actualSQL, "deleted_at IS NULL")
		require.Contains(t, actualSQL, "(k.id IS NULL OR k.deleted_at IS NOT NULL) as key_deleted")
		require.Contains(t, actualSQL, "COALESCE(k.user_id, MAX(ul.user_id), 0) as user_id")
		require.Contains(t, actualSQL, " AND ul.group_id = $3 AND ul.api_key_id = $4")
		require.Contains(t, actualSQL, " GROUP BY ul.api_key_id, k.id, k.name, k.deleted_at, k.user_id, u.email ORDER BY total_tokens DESC LIMIT 20")
		require.NotContains(t, actualSQL, "ul.user_id, u.email")
		return nil
	})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery("").
		WithArgs(start, end, int64(3), int64(9)).
		WillReturnRows(sqlmock.NewRows(apiKeyBreakdownColumns))

	rows, err := repo.GetAPIKeyBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		GroupID:  3,
		APIKeyID: 9,
		SortBy:   "total_tokens",
	}, 20)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 非法 sort_by 回退 actual_cost，limit<=0 不加 LIMIT——与用户排行一致。
func TestGetAPIKeyBreakdownStatsSortFallbackAndNoLimit(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY actual_cost DESC")+"$").
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows(apiKeyBreakdownColumns))

	rows, err := repo.GetAPIKeyBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		SortBy: "drop_table",
	}, 0)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserBreakdownStatsRequestTypeIncludesLegacyFallback(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	requestType := int16(service.RequestTypeStream)

	legacyFilter := `(ul.request_type = $3 OR (ul.request_type = 0 AND ul.stream = TRUE AND ul.openai_ws_mode = FALSE))`
	mock.ExpectQuery(regexp.QuoteMeta(legacyFilter)).
		WithArgs(start, end, requestType).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "email", "requests", "input_tokens", "output_tokens",
			"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
		}))

	rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		RequestType: &requestType,
	}, 0)

	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetUserBreakdownStatsFiltersNativeCompactionV2(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	nativeCompactionV2 := true

	mock.ExpectQuery(regexp.QuoteMeta("AND ul.native_compaction_v2 = $3")).
		WithArgs(start, end, true).
		WillReturnRows(sqlmock.NewRows([]string{
			"user_id", "email", "requests", "input_tokens", "output_tokens",
			"cache_tokens", "total_tokens", "cost", "actual_cost", "account_cost",
		}))

	rows, err := repo.GetUserBreakdownStats(context.Background(), start, end, usagestats.UserBreakdownDimension{
		NativeCompactionV2: &nativeCompactionV2,
	}, 0)

	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, mock.ExpectationsWereMet())
}
