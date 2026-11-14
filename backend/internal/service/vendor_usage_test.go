package service

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

func TestVendorUsageImmutableSelectedSnapshot(t *testing.T) {
	id := int64(31)
	a := &Account{ID: 11, VendorID: &id, Name: "before", Platform: "openai"}
	g := &Group{ID: 2, Name: "served"}
	u := &UsageLog{}
	u.CaptureVendorSnapshot(a, g)
	id = 99
	a.Name = "after"
	g.Name = "changed"
	require.Equal(t, int64(31), *u.VendorIDSnapshot)
	require.Equal(t, "before", *u.AccountNameSnapshot)
	require.Equal(t, "served", *u.GroupNameSnapshot)
	legacy := &UsageLog{}
	legacy.CaptureVendorSnapshot(&Account{ID: 11}, nil)
	require.Nil(t, legacy.VendorIDSnapshot)
}
func TestVendorUsageDateBoundsAndSnapshot(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	f := VendorUsageFilter{Timezone: "America/New_York", StartDate: "2026-03-08", EndDate: "2026-03-08"}
	require.NoError(t, normalizeVendorUsageFilter(&f, now))
	require.Equal(t, 23*time.Hour, f.end.Sub(f.start))
	for _, f := range []VendorUsageFilter{{StartDate: "2026-02-30", EndDate: "2026-03-01"}, {StartDate: "2026-01-01", EndDate: "2026-02-01"}, {Timezone: "not/a/zone"}, {Dimension: "account; DROP TABLE usage_logs"}, {PageSize: 101}, {VendorID: -1}, {SnapshotAt: now.Add(time.Hour)}} {
		require.ErrorIs(t, normalizeVendorUsageFilter(&f, now), ErrVendorUsageInput)
	}
	all := VendorUsageFilter{AllHistory: true}
	require.NoError(t, normalizeVendorUsageFilter(&all, now))
	require.Equal(t, time.Unix(0, 0).UTC(), all.start)
}
func TestVendorUsageScopeSQLAndUnknownMetrics(t *testing.T) {
	f := VendorUsageFilter{VendorID: 4, AccountID: 7, GroupID: 3, Platform: "openai", Watermark: 12}
	where, args := vendorUsageWhere(f)
	require.Contains(t, where, "ul.vendor_id_snapshot=$5")
	require.NotContains(t, where, "a.vendor_id")
	require.NotContains(t, where, "actual_cost")
	require.Len(t, args, 8)
	m := vendorMetricsScan{}
	m.m.InputTokens = 100
	m.m.OutputTokens = 40
	m.m.CacheReadTokens = 200
	m.m.CacheCreationTokens = 20
	m.m.CostUSD = "0.0100000000"
	got := m.finish()
	require.Equal(t, int64(140), got.TotalTokens)
	require.Equal(t, int64(220), got.CacheTokens)
	require.InDelta(t, 0.625, *got.CacheHitRate, 1e-10)
	require.Nil(t, got.SuccessRate)
	require.Nil(t, got.SuccessfulRequests)
	require.Nil(t, got.FailedRequests)
	require.Nil(t, got.AvgDurationMS)
}

func TestVendorUsageAuthorizationDisplayPreservesCosts(t *testing.T) {
	for _, tc := range []struct{ raw, grouped, request string }{
		{"native", "native", "native"}, {"bps", "bps", "bps"},
		{"unsupported", "unknown", "unknown"}, {"legacy", "unknown", "unknown"},
		{"mixed", "mixed", "unknown"}, {"", "", "unknown"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			f := VendorUsageFilter{Page: 1, PageSize: 10}
			mock.ExpectQuery("WITH grouped").WillReturnRows(sqlmock.NewRows(strings.Fields(
				"is_summary total vendor_id account_id account_name deleted archived status group_id group_name date requested upstream requests input output cache_creation cache_read cost opus_cost fable_cost duration first earliest vendor_name mode")).AddRow(
				false, 1, 1, 11, "account", false, false, "active", nil, "", "", "gpt", "gpt",
				1, 10, 5, 0, 0, "12.34", "0", "0", nil, nil, nil, "vendor", tc.raw))
			rows, total, _, _, err := vendorUsageGrouped(t.Context(), db, f, "account", 10, 0)
			require.NoError(t, err)
			require.Equal(t, int64(1), total)
			require.Len(t, rows, 1)
			require.Equal(t, tc.grouped, rows[0].AuthorizationMode)
			require.Equal(t, "12.34", rows[0].CostUSD)
			require.Equal(t, int64(15), rows[0].TotalTokens)

			mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows(strings.Fields(
				"requests input output cache_creation cache_read cost opus_cost fable_cost duration first")).AddRow(1, 10, 5, 0, 0, "12.34", "0", "0", nil, nil))
			mock.ExpectQuery("WITH page").WillReturnRows(sqlmock.NewRows(strings.Fields(
				"id vendor_id account_id account_name group_id group_name platform mode created requested upstream input output cache_creation cache_read cost opus_cost fable_cost duration first vendor_name deleted archived status")).AddRow(
				1, 1, 11, "account", nil, "", "openai", tc.raw, time.Now(), "gpt", "gpt",
				10, 5, 0, 0, "12.34", "0", "0", nil, nil, "vendor", false, false, "active"))
			rows, summary, err := vendorUsageRequests(t.Context(), db, f)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, tc.request, rows[0].AuthorizationMode)
			require.Equal(t, "12.34", rows[0].CostUSD)
			require.Equal(t, "12.34", summary.CostUSD)
			require.Equal(t, int64(15), rows[0].TotalTokens)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVendorUsageCSVPrivacyAndFormulaSafety(t *testing.T) {
	for _, in := range []string{"=SUM(A1:A2)", "  +evil", "\t@evil", "\ufeff-evil", "0000123", "123456789012345678", "normal\n=evil"} {
		require.True(t, strings.HasPrefix(vendorCSVText(in), "'"), in)
	}
	rows := [][]string{{"text", "money"}, {vendorCSVText("=secret,\"quote\""), "1.20"}}
	b, e := vendorCSV(rows)
	require.NoError(t, e)
	require.True(t, strings.HasPrefix(string(b), "\xef\xbb\xbf"))
	parsed, e := csv.NewReader(strings.NewReader(string(b[3:]))).ReadAll()
	require.NoError(t, e)
	require.Equal(t, rows, parsed)
	data, e := json.Marshal(VendorUsageEntry{})
	require.NoError(t, e)
	for _, secret := range []string{"api_key", "user_id", "request_id", "ip_address", "actual_cost", "credentials", "session_id"} {
		require.NotContains(t, string(data), secret)
	}
}
func TestVendorUsageLiveAdminDemotionAndRevokedSession(t *testing.T) {
	for _, role := range []string{"admin", "super_admin"} {
		t.Run(role, func(t *testing.T) {
			db, mock, e := sqlmock.New()
			require.NoError(t, e)
			defer db.Close()
			s := &VendorService{db: db}
			ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 1, Role: authz.SuperAdmin, SessionGeneration: 4})
			mock.ExpectQuery("SELECT role, status, session_generation FROM users").WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"role", "status", "session_generation"}).AddRow(role, "active", int64(5)))
			_, e = s.vendorUsageAdmin(ctx, db, false)
			require.ErrorIs(t, e, ErrVendorUsageForbidden)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVendorUsagePostgresModelFamiliesAndPrecedence(t *testing.T) {
	s, ctx, _ := vendorSettlementPostgres(t)
	cases := []struct{ model, upstream, family string }{
		{"claude-fable-5", "claude-opus-4-6", "opus"},
		{"claude-opus-4-6", "claude-fable-5-1", "fable"},
		{"claude-opus-4-6", "custom-alias", ""},
		{"claude-opus-4-6", "gpt-5", ""},
		{"claude-fable-5", "  ", "fable"},
		{"", " Claude-Fable-5[1m] ", "fable"},
		{"", "anthropic/claude-opus-4.6", "opus"},
		{"", "us.anthropic.claude-opus-4-5-20251101-v1:0", "opus"},
		{"", "models/claude-opus-4-6-thinking", "opus"},
		{"", "claude-3-opus@20240229", "opus"},
		{"", "not-fable", ""}, {"", "fable", ""}, {"", "opus", ""},
		{"", "claude-fable-999", ""}, {"", "claude-opus-4-6-custom", ""},
		{"", "fake-claude-opus-4-6", ""}, {"", "", ""},
	}
	for _, id := range claude.DefaultModelIDs() {
		family := ""
		if strings.HasPrefix(id, "claude-opus-") {
			family = "opus"
		}
		if strings.HasPrefix(id, "claude-fable-") {
			family = "fable"
		}
		cases = append(cases, struct{ model, upstream, family string }{id, "", family})
	}
	for _, tc := range cases {
		var opus, fable bool
		err := s.db.QueryRowContext(ctx, "SELECT "+vendorUsageFamilySQL("opus")+","+vendorUsageFamilySQL("fable")+" FROM (SELECT $1::text AS model,$2::text AS upstream_model) ul", tc.model, tc.upstream).Scan(&opus, &fable)
		require.NoError(t, err)
		require.Equal(t, tc.family == "opus", opus, "%+v sent=%s", tc, upstreamSentModel(tc.model, tc.upstream))
		require.Equal(t, tc.family == "fable", fable, "%+v", tc)
	}
}

func TestVendorUsagePostgresRankingSnapshotAndEmptyPages(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	_, err := s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,account_name_snapshot,group_id,created_at,requested_model,model,upstream_model,input_tokens,output_tokens,account_stats_cost,total_cost,actual_cost,account_rate_multiplier,duration_ms,first_token_ms) VALUES" +
		"(1,11,'A',1,'2026-10-01T12:00Z','claude-fable-5','claude-fable-5','claude-opus-4-6',100,50,6,999,77777,2,100,NULL)," +
		"(1,11,'A',NULL,'2026-10-02T12:00Z','alias','claude-fable-5',NULL,200,100,NULL,4,77777,NULL,300,60)," +
		"(1,12,'deleted A',1,'2026-10-01T12:00Z','claude-opus-4-6','claude-opus-4-6','unknown',1000,500,NULL,10,77777,NULL,NULL,20)," +
		"(1,13,'missing A',1,'2026-10-01T12:00Z','claude-opus-4-6','claude-opus-4-6','gpt-5',10,5,NULL,9.0000000001,77777,NULL,800,NULL)," +
		"(2,21,'foreign',1,'2026-10-01T12:00Z','claude-opus-4-6','claude-opus-4-6',NULL,10000,5000,NULL,999,77777,NULL,1,1)," +
		"(NULL,11,'legacy',1,'2026-10-01T12:00Z','claude-fable-5','claude-fable-5',NULL,10000,5000,NULL,8888,77777,NULL,1,1)")
	require.NoError(t, err)
	f := VendorUsageFilter{StartDate: "2026-10-01", EndDate: "2026-10-02", Timezone: "UTC", PageSize: 1}
	first, err := s.VendorUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, int64(3), first.Total)
	require.Equal(t, int64(4), first.Summary.Requests)
	require.Equal(t, "29.0000000001", first.Summary.CostUSD)
	require.Equal(t, "6", first.Summary.OpusCostUSD)
	require.Equal(t, "4", first.Summary.FableCostUSD)
	require.InDelta(t, 400, *first.Summary.AvgDurationMS, 1e-12, "summary averages weight raw non-null samples")
	require.InDelta(t, 40, *first.Summary.AvgFirstTokenMS, 1e-12)
	require.Equal(t, int64(11), first.Items[0].AccountID)
	require.Equal(t, int64(450), first.Items[0].TotalTokens)
	f.SnapshotAt, f.Watermark = first.SnapshotAt, first.Watermark
	// Delayed writes and a changed current owner must not rewrite the old view.
	_, err = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES(1,13,'2026-10-01T12:00Z','claude-opus-4-6',1000); UPDATE accounts SET vendor_id=2 WHERE id=11")
	require.NoError(t, err)
	for page, id := range []int64{11, 12, 13} {
		f.Page = page + 1
		r, err := s.VendorUsage(supplier, f, false)
		require.NoError(t, err)
		require.Equal(t, first.Summary, r.Summary)
		require.Len(t, r.Items, 1)
		require.Equal(t, id, r.Items[0].AccountID)
		require.True(t, r.Items[0].AccountDeleted)
		require.Equal(t, "A", r.Items[0].VendorName)
	}
	f.Page = 4
	last, err := s.VendorUsage(supplier, f, false)
	require.NoError(t, err)
	require.Empty(t, last.Items)
	require.Equal(t, int64(3), last.Total)
	require.Equal(t, first.Summary, last.Summary)
	f.Page = 1
	for _, tc := range []struct {
		by, direction string
		id            int64
	}{
		{"cost_usd", "asc", 13}, {"cost_usd", "desc", 11}, {"requests", "desc", 11},
		{"total_tokens", "desc", 12}, {"opus_cost_usd", "desc", 11}, {"fable_cost_usd", "desc", 11},
	} {
		f.SortBy, f.Order = tc.by, tc.direction
		r, err := s.VendorUsage(supplier, f, false)
		require.NoError(t, err)
		require.Equal(t, tc.id, r.Items[0].AccountID, "%+v", tc)
	}
	f.SortBy, f.Order = "", ""
	for _, dimension := range []string{"vendor", "account", "date", "group", "account_date"} {
		f.Dimension, f.PageSize = dimension, 100
		r, err := s.VendorUsage(supplier, f, false)
		require.NoError(t, err)
		require.Equal(t, first.Summary, r.Summary, dimension)
		if dimension == "group" {
			require.Equal(t, int64(2), r.Total, "NULL group must not become the summary row")
		}
	}
	f.Dimension, f.SortBy, f.Order = "date", "date", "desc"
	days, err := s.VendorUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, "2026-10-02", days.Items[0].Date)
	f.AccountID = 11
	detail, err := s.VendorAccountUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, "2026-10-02", detail.Items[0].Date)
	require.Equal(t, "2026-10-01", detail.Trends[0].Date, "table ordering must not reverse chart chronology")
	require.Equal(t, int64(2), detail.ModelTotal)
	require.Equal(t, "6", detail.Models[0].OpusCostUSD, "model table still ranks native numeric account cost")
	f.Page, f.PageSize = 2, 1
	nextDay, err := s.VendorAccountUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, detail.Models, nextDay.Models, "daily pagination must not change the model distribution")
	f.AccountID, f.SortBy, f.Order = 0, "", ""
	f.VendorID = 2
	_, err = s.VendorUsage(supplier, f, false)
	require.ErrorIs(t, err, ErrVendorUsageForbidden)
	f.VendorID = 1
	f.Watermark, f.WatermarkSet = 0, true
	empty, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	require.Zero(t, empty.Total)
	require.Empty(t, empty.Items)
	require.Equal(t, "0", empty.Summary.CostUSD)
	require.Equal(t, "0", empty.Summary.OpusCostUSD)
	require.Equal(t, "0", empty.Summary.FableCostUSD)
	require.Nil(t, empty.Summary.AvgDurationMS)
	for _, bad := range []VendorUsageFilter{{SortBy: "cost_usd;DROP TABLE usage_logs"}, {Order: "DESC NULLS FIRST"}, {Dimension: "account", SortBy: "date"}} {
		require.ErrorIs(t, normalizeVendorUsageFilter(&bad, time.Now()), ErrVendorUsageInput)
	}
}

type vendorUsageCountingSQL struct {
	vendorSQL
	queries   []string
	arguments [][]any
}

func (q *vendorUsageCountingSQL) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	q.queries = append(q.queries, query)
	q.arguments = append(q.arguments, args)
	return q.vendorSQL.QueryContext(ctx, query, args...)
}
func (q *vendorUsageCountingSQL) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	q.queries = append(q.queries, query)
	q.arguments = append(q.arguments, args)
	return q.vendorSQL.QueryRowContext(ctx, query, args...)
}

func TestVendorUsagePostgresFixedWindowBoundaries(t *testing.T) {
	s, ctx, _ := vendorSettlementPostgres(t)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for i, d := range []time.Duration{0, time.Second, time.Minute, time.Minute + time.Second, 5 * time.Hour, 5*time.Hour + time.Second, 7 * 24 * time.Hour, 7*24*time.Hour + time.Second, 30 * 24 * time.Hour, 30*24*time.Hour + time.Second} {
		_, err := s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,group_id,platform_snapshot,created_at,model,total_cost,duration_ms) VALUES(1,12,2,'anthropic',$1,'claude-opus-4-6',$2,$3)", now.Add(-d), fmt.Sprintf("%d", i+1), i*100)
		require.NoError(t, err)
	}
	f := VendorUsageFilter{VendorID: 1, AccountID: 12, GroupID: 1, Platform: "openai", SnapshotAt: now, Watermark: 10}
	// Same-account windows intentionally ignore the selected date/group/platform,
	// while retaining vendor, selected-account snapshot and ID watermark bounds.
	q := &vendorUsageCountingSQL{vendorSQL: s.db}
	windows, err := vendorUsageFixedWindows(ctx, q, f)
	require.NoError(t, err)
	require.Len(t, q.queries, 1)
	for _, tc := range []struct {
		key   string
		count int64
		cost  string
	}{{"1m", 2, "5"}, {"5h", 4, "14"}, {"7d", 6, "27"}, {"30d", 8, "44"}} {
		m := windows[tc.key]
		require.Equal(t, tc.count, m.Requests, tc.key)
		require.Equal(t, tc.cost, m.CostUSD, tc.key)
		require.Equal(t, m.CostUSD, m.OpusCostUSD)
		require.Equal(t, "0", m.FableCostUSD)
	}
	f.Watermark = 2
	windows, err = vendorUsageFixedWindows(ctx, s.db, f)
	require.NoError(t, err)
	require.Equal(t, int64(1), windows["30d"].Requests)
	f.VendorID = 2
	windows, err = vendorUsageFixedWindows(ctx, s.db, f)
	require.NoError(t, err)
	require.Zero(t, windows["30d"].Requests)
}

func TestVendorUsagePostgresIndependentModelPagination(t *testing.T) {
	s, _, supplier := vendorSettlementPostgres(t)
	_, err := s.db.Exec(`INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost)
SELECT 1,12,'2026-10-01T12:00Z'::timestamptz + (n%2)*INTERVAL '1 day','model-'||n::text,n FROM generate_series(1,101) n`)
	require.NoError(t, err)
	f := VendorUsageFilter{AccountID: 12, PageSize: 1}
	first, err := s.VendorAccountUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, int64(101), first.ModelTotal)
	require.Len(t, first.Models, 100)
	require.Equal(t, 1, first.ModelPage)
	require.Equal(t, 100, first.ModelPageSize)
	f.SnapshotAt, f.Watermark, f.ModelPage = first.SnapshotAt, first.Watermark, 2
	last, err := s.VendorAccountUsage(supplier, f, false)
	require.NoError(t, err)
	require.Len(t, last.Models, 1)
	require.Equal(t, "model-1", last.Models[0].RequestedModel)
	require.Equal(t, first.Items, last.Items, "model paging keeps the daily page")
	require.Equal(t, first.Summary, last.Summary)
	f.Page = 2
	nextDay, err := s.VendorAccountUsage(supplier, f, false)
	require.NoError(t, err)
	require.Equal(t, last.Models, nextDay.Models)
	require.NotEqual(t, first.Items[0].Date, nextDay.Items[0].Date)
	for _, page := range []int{-1, vendorUsageMaxPage + 1} {
		f.ModelPage = page
		_, err = s.VendorAccountUsage(supplier, f, false)
		require.ErrorIs(t, err, ErrVendorUsageInput)
	}
}

func TestVendorUsagePostgresQueryPlanAndRoundTrips(t *testing.T) {
	s, ctx, _ := vendorSettlementPostgres(t)
	_, err := s.db.Exec("INSERT INTO accounts(id,vendor_id,name,status) SELECT n,1,'account-'||n,'active' FROM generate_series(100,299) n;" +
		"INSERT INTO usage_logs(vendor_id_snapshot,account_id,account_name_snapshot,created_at,model,upstream_model,total_cost,input_tokens,output_tokens,duration_ms) " +
		"SELECT CASE WHEN n%5=0 THEN 2 ELSE 1 END,CASE WHEN n%5=0 THEN 21 ELSE 100+n%200 END,'snapshot-'||n,'2026-10-01T12:00Z'," +
		"'alias',CASE WHEN n%2=0 THEN 'claude-opus-4-6' ELSE 'claude-fable-5' END,(n%101)::numeric/10000,n%1000,n%500,n%3000 FROM generate_series(1,50000) n;" +
		"ANALYZE usage_logs; ANALYZE accounts; ANALYZE vendors")
	require.NoError(t, err)
	f := VendorUsageFilter{VendorID: 1, StartDate: "2026-10-01", EndDate: "2026-10-01", Timezone: "UTC", PageSize: 20, Watermark: 50000}
	require.NoError(t, normalizeVendorUsageFilter(&f, time.Now()))
	q := &vendorUsageCountingSQL{vendorSQL: s.db}
	report, err := vendorUsageReport(ctx, q, f)
	require.NoError(t, err)
	require.Len(t, q.queries, 1, "summary, group count and page use one round trip")
	require.Equal(t, int64(40000), report.Summary.Requests)
	require.Equal(t, int64(160), report.Total)
	// Compare the previous three-query execution shape with identical metrics.
	// This is an isolated fixture measurement, never a production latency target.
	where, args := vendorUsageWhere(f)
	from := " FROM usage_logs ul LEFT JOIN accounts a ON a.id=ul.account_id AND a.vendor_id=ul.vendor_id_snapshot LEFT JOIN vendors v ON v.id=ul.vendor_id_snapshot WHERE " + where + " GROUP BY ul.vendor_id_snapshot,ul.account_id"
	baseline := []string{
		"SELECT " + vendorUsageAggregateSQL() + " FROM usage_logs ul WHERE " + where,
		"SELECT COUNT(*) FROM (SELECT 1" + from + ") counts",
		"SELECT ul.vendor_id_snapshot,ul.account_id,MAX(ul.account_name_snapshot),BOOL_OR(a.id IS NULL OR a.deleted_at IS NOT NULL),MAX(a.status),MAX(v.name)," + vendorUsageAggregateSQL() + from + " ORDER BY SUM(" + vendorUsageCostSQL("ul") + ") DESC,1,2 LIMIT 20",
	}
	var baselineScans int
	var baselineRows float64
	for i, query := range append(baseline, q.queries[0]) {
		params := args
		label := fmt.Sprintf("baseline_%d", i+1)
		if i == len(baseline) {
			params = q.arguments[0]
			label = "optimized"
		}
		var raw []byte
		require.NoError(t, s.db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, params...).Scan(&raw))
		var plan []map[string]any
		require.NoError(t, json.Unmarshal(raw, &plan))
		scans, scannedRows := 0, float64(0)
		var visit func(map[string]any)
		visit = func(node map[string]any) {
			if node["Relation Name"] == "usage_logs" && node["Node Type"] != "Bitmap Index Scan" {
				scans++
				scannedRows += node["Actual Rows"].(float64) * node["Actual Loops"].(float64)
			}
			if children, ok := node["Plans"].([]any); ok {
				for _, child := range children {
					visit(child.(map[string]any))
				}
			}
		}
		visit(plan[0]["Plan"].(map[string]any))
		t.Logf("EXPLAIN %s usage_scans=%d matched_rows=%.0f execution_ms=%v root_shared_hits=%v", label, scans, scannedRows, plan[0]["Execution Time"], plan[0]["Plan"].(map[string]any)["Shared Hit Blocks"])
		t.Logf("EXPLAIN_JSON %s %s", label, raw)
		if i < len(baseline) {
			baselineScans += scans
			baselineRows += scannedRows
		} else {
			require.Equal(t, 1, scans)
			require.Equal(t, float64(40000), scannedRows)
			require.Equal(t, 3, baselineScans)
			require.Equal(t, float64(120000), baselineRows)
		}
	}
	t.Log("report_data_queries baseline=3 optimized=1; fixed_window_data_queries baseline=4 optimized=1; fixture_rows=50000 scoped_rows=40000 groups=160")
}

func TestVendorUsagePostgresRequestDrilldown(t *testing.T) {
	s, admin, supplier := vendorSettlementPostgres(t)
	vendorSettlementSeedUsage(t, s)
	_, err := s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,account_name_snapshot,group_id,group_name_snapshot,platform_snapshot,created_at,model,upstream_model,input_tokens,output_tokens,cache_read_tokens,account_stats_cost,total_cost,actual_cost,account_rate_multiplier,duration_ms,first_token_ms,request_id,api_key_id) " +
		"VALUES(1,11,'snapshot A',1,'snapshot group','openai','2026-10-02T12:00Z','client-alias','claude-opus-4-6',40,20,10,2.5,999,77777,2,123,45,'SECRET_REQUEST_ID',123456789)")
	require.NoError(t, err)
	f := VendorUsageFilter{VendorID: 1, Dimension: "request", AllHistory: true, PageSize: 1, Timezone: "UTC"}
	first, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	require.Equal(t, int64(3), first.Total)
	require.Equal(t, "9032.9", first.Summary.CostUSD)
	require.Equal(t, "2.5", first.Summary.OpusCostUSD)
	require.Len(t, first.Items, 1)
	row := first.Items[0]
	require.Equal(t, int64(5), row.UsageLogID, "equal timestamps break ties by ID descending")
	require.Equal(t, "2026-10-02T12:00:00Z", row.CreatedAt.Format(time.RFC3339))
	require.Equal(t, "openai", row.Platform)
	require.Equal(t, "client-alias", row.RequestedModel)
	require.Equal(t, "claude-opus-4-6", row.UpstreamModel)
	require.Equal(t, "snapshot A", row.AccountName)
	require.Equal(t, "snapshot group", row.GroupName)
	require.Equal(t, "A", row.VendorName)
	require.Equal(t, "2.5", row.CostUSD)
	require.Equal(t, int64(1), row.Requests)
	require.Equal(t, int64(60), row.TotalTokens)
	require.Equal(t, int64(10), row.CacheTokens)
	require.Equal(t, "unknown", row.Result)
	require.Equal(t, float64(123), *row.AvgDurationMS)
	require.Equal(t, float64(45), *row.AvgFirstTokenMS)
	require.Nil(t, row.SuccessRate)
	require.Nil(t, row.SuccessfulRequests)
	require.Nil(t, row.FailedRequests)
	body, err := json.Marshal(first)
	require.NoError(t, err)
	for _, private := range []string{"user_id", "api_key", "ip_address", "content", "request_id", "credentials", "actual_cost", "SECRET_REQUEST_ID", "123456789", "77777"} {
		require.NotContains(t, string(body), private)
	}
	_, err = s.VendorUsage(supplier, f, false)
	require.ErrorIs(t, err, ErrVendorUsageInput, "supplier report service cannot open the admin-only dimension")
	f.SnapshotAt, f.Watermark = first.SnapshotAt, first.Watermark
	_, err = s.db.Exec("INSERT INTO usage_logs(vendor_id_snapshot,account_id,created_at,model,total_cost) VALUES(1,11,'2026-10-02T12:00Z','late',1000)")
	require.NoError(t, err)
	for page, id := range []int64{5, 2, 1} {
		f.Page = page + 1
		r, err := s.VendorUsage(admin, f, true)
		require.NoError(t, err)
		require.Equal(t, first.Summary, r.Summary)
		require.Equal(t, id, r.Items[0].UsageLogID)
	}
	f.Page = 4
	empty, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	require.Empty(t, empty.Items)
	require.Equal(t, first.Total, empty.Total)
	require.Equal(t, first.Summary, empty.Summary)
	f.Page = 1
	for _, tc := range []struct {
		by, order string
		id        int64
	}{{"created_at", "asc", 1}, {"cost_usd", "desc", 1}, {"opus_cost_usd", "desc", 5}, {"fable_cost_usd", "desc", 5}, {"total_tokens", "desc", 2}, {"requests", "desc", 5}} {
		f.SortBy, f.Order = tc.by, tc.order
		r, err := s.VendorUsage(admin, f, true)
		require.NoError(t, err)
		require.Equal(t, tc.id, r.Items[0].UsageLogID, "%+v", tc)
	}
	f.SortBy, f.Order = "", ""
	f.AccountID, f.GroupID, f.Platform = 12, 2, "openai"
	deleted, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted.Total)
	require.True(t, deleted.Items[0].AccountDeleted)
	require.Nil(t, deleted.Items[0].AvgDurationMS, "missing durations remain unknown")
	f.GroupID = 1
	scopedEmpty, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	require.Zero(t, scopedEmpty.Total)
	f.AccountID, f.GroupID, f.Platform = 0, 0, ""
	require.NoError(t, normalizeVendorUsageFilter(&f, time.Now()))
	q := &vendorUsageCountingSQL{vendorSQL: s.db}
	_, err = vendorUsageReport(admin, q, f)
	require.NoError(t, err)
	require.Len(t, q.queries, 2, "summary includes the count; there is no separate count query")
	f.Watermark, f.WatermarkSet = 0, true
	zero, err := s.VendorUsage(admin, f, true)
	require.NoError(t, err)
	require.Empty(t, zero.Items)
	require.Zero(t, zero.Total)
}
