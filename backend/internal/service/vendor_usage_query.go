package service

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

// Reporting recognizes explicit model IDs, not pricing's fuzzy fallback or the
// scheduling Fable substring matcher. Reuse the native catalog and historical
// Opus IDs still supported by native pricing. Unknown aliases stay unclassified.
func vendorUsageFamilyModels(family string) []string {
	ids := append(claude.DefaultModelIDs(), "claude-opus-4", "claude-opus-4-1", "claude-3-opus")
	var out []string
	for _, id := range ids {
		id = claude.DenormalizeModelID(id)
		if strings.HasPrefix(id, "claude-"+family+"-") || (family == "opus" && id == "claude-3-opus") {
			out = append(out, id)
		}
	}
	return out
}

func vendorUsageFamilyPattern(family string) string {
	known := make(map[string]bool)
	for _, id := range vendorUsageFamilyModels(family) {
		known[regexp.QuoteMeta(id)] = true
		// Native OpenRouter normalization accepts dotted minor versions.
		prefix := "claude-" + family + "-"
		if strings.HasPrefix(id, prefix) {
			known[regexp.QuoteMeta(prefix+strings.ReplaceAll(strings.TrimPrefix(id, prefix), "-", "."))] = true
		}
	}
	patterns := make([]string, 0, len(known))
	for id := range known {
		patterns = append(patterns, id)
	}
	sort.Strings(patterns)
	// Same provider/date/Bedrock aliases as native Claude normalization, plus
	// the explicit [1m] request variant. Arbitrary prefixes/suffixes do not match.
	return "^([a-z0-9_-]+/)?(models/)?((us|eu|apac|jp|au|us-gov|global)[.])?(anthropic[.])?(" + strings.Join(patterns, "|") + ")([-@][0-9]{8})?(-v[0-9]+(:[0-9]+)?)?(-thinking)?(\\[1m\\])?$"
}

var vendorUsageFamilyPatterns = map[string]string{
	"opus": vendorUsageFamilyPattern("opus"), "fable": vendorUsageFamilyPattern("fable"),
}

// usage_logs does not persist the final pricing candidate. Classify by the
// actual sent model, using upstreamSentModel's upstream -> model precedence;
// neither requested_model nor today's prices can reconstruct historical billing.
func vendorUsageFamilySQL(family string) string {
	model := "LOWER(BTRIM(COALESCE(NULLIF(BTRIM(ul.upstream_model),''),ul.model,'')))"
	var exact []string
	for _, id := range vendorUsageFamilyModels(family) {
		exact = append(exact, "'"+strings.ReplaceAll(id, "'", "''")+"'")
	}
	// Most records use canonical IDs. Avoid running the provider-alias regex
	// for every token row (or for other model families) in large histories.
	return "CASE WHEN " + model + " IN (" + strings.Join(exact, ",") + ") THEN TRUE WHEN STRPOS(" + model + ",'" + family + "')=0 THEN FALSE ELSE " + model + " ~ '" + vendorUsageFamilyPatterns[family] + "' END"
}

// Supplier procurement uses the recorded model cost before commercial multipliers.
// Legacy rows without account pricing retain their recorded total_cost; never
// reprice historical usage from today's model catalog or customer actual_cost.
const vendorUsageCostBasis = "COALESCE(account_stats_cost,total_cost)"

func vendorUsageCostSQL(alias string) string {
	if alias != "" {
		alias += "."
	}
	return "COALESCE(" + alias + "account_stats_cost," + alias + "total_cost)"
}

func vendorUsageAggregateSQL() string { return vendorUsageWindowAggregateSQL("") }

func vendorUsageWindowAggregateSQL(predicate string) string {
	filter := ""
	if predicate != "" {
		filter = " FILTER (WHERE " + predicate + ")"
	}
	sum := func(expr string) string { return "COALESCE(SUM(" + expr + ")" + filter + ",0)" }
	cost := vendorUsageCostSQL("ul")
	return "COUNT(*)" + filter + " AS requests," + sum("ul.input_tokens") + " AS input_tokens," +
		sum("ul.output_tokens") + " AS output_tokens," + sum("ul.cache_creation_tokens") + " AS cache_creation_tokens," +
		sum("ul.cache_read_tokens") + " AS cache_read_tokens," + sum(cost) + " AS cost_usd," +
		sum("CASE WHEN "+vendorUsageFamilySQL("opus")+" THEN "+cost+" ELSE 0 END") + " AS opus_cost_usd," +
		sum("CASE WHEN "+vendorUsageFamilySQL("fable")+" THEN "+cost+" ELSE 0 END") + " AS fable_cost_usd," +
		"AVG(ul.duration_ms)" + filter + " AS avg_duration_ms,AVG(ul.first_token_ms)" + filter + " AS avg_first_token_ms"
}

func vendorUsageOrderSQL(f VendorUsageFilter, dimension string) (string, error) {
	by, direction := f.SortBy, f.Order
	if by == "" {
		by = "cost_usd"
		if dimension == "request" {
			by = "created_at"
		}
		if dimension == "date" || dimension == "month" || dimension == "year" {
			by = "date"
		}
	}
	if direction == "" {
		direction = "desc"
		if by == "date" {
			direction = "asc"
		}
	}
	if direction != "asc" && direction != "desc" {
		return "", ErrVendorUsageInput
	}
	switch by {
	case "cost_usd", "opus_cost_usd", "fable_cost_usd", "requests":
		by = "s." + by
	case "total_tokens":
		by = "(s.input_tokens+s.output_tokens)"
	case "created_at":
		if dimension != "request" {
			return "", ErrVendorUsageInput
		}
		by = "s.created_at"
	case "date":
		if dimension != "date" && dimension != "account_date" && dimension != "month" && dimension != "year" {
			return "", ErrVendorUsageInput
		}
		by = "s.date"
	default:
		return "", ErrVendorUsageInput
	}
	if dimension == "request" {
		return by + " " + direction + ",s.usage_log_id " + direction, nil
	}
	// Every group key is a deterministic tie breaker, including null groups.
	return by + " " + direction + ",s.vendor_id,s.account_id,s.group_id NULLS FIRST,s.date,s.requested_model,s.upstream_model", nil
}

// GROUPING SETS reads filtered logs once for exact totals, weighted averages and
// dimension rows. Materialize only the aggregates; count/page reuse those rows.
// A summary row is always returned, even for empty or out-of-range pages. Join
// live display metadata only after pagination, never once per raw usage record.
func vendorUsageGroupedQuery(f VendorUsageFilter, dimension string, limit, offset int) (string, []any, error) {
	order, err := vendorUsageOrderSQL(f, dimension)
	if err != nil {
		return "", nil, err
	}
	where, args := vendorUsageWhere(f)
	vid, aid, gid, date, requested, upstream := "0::bigint", "0::bigint", "NULL::bigint", "''::text", "''::text", "''::text"
	aname, gname, vendorName, status, deleted := "''::text", "''::text", "''::text", "''::text", "false"
	archived := "false"
	var keys []string
	if dimension == "vendor" || dimension == "account" || dimension == "group" || dimension == "account_date" {
		vid = "ul.vendor_id_snapshot"
		keys = append(keys, vid)
		vendorName = "CASE WHEN s.is_summary THEN '' ELSE COALESCE(v.name,'unknown') END"
	}
	switch dimension {
	case "vendor":
	case "account", "account_date":
		aid = "ul.account_id"
		keys = append(keys, aid)
		aname = "COALESCE(MAX(ul.account_name_snapshot),'unknown')"
		deleted = "NOT s.is_summary AND (a.id IS NULL OR a.deleted_at IS NOT NULL)"
		archived = "NOT s.is_summary AND COALESCE(a.extra->>'vendor_archived_at','')<>''"
		status = "CASE WHEN s.is_summary THEN '' ELSE COALESCE(a.status,'unknown') END"
	case "group":
		gid = "ul.group_id"
		keys = append(keys, gid)
		gname = "COALESCE(MAX(ul.group_name_snapshot),'unknown')"
	case "date", "month", "year":
	case "model":
		requested = "COALESCE(NULLIF(ul.requested_model,''),ul.model,'unknown')"
		upstream = "COALESCE(NULLIF(ul.upstream_model,''),'unknown')"
		keys = append(keys, requested, upstream)
	default:
		return "", nil, ErrVendorUsageInput
	}
	if dimension == "date" || dimension == "month" || dimension == "year" || dimension == "account_date" {
		format := "YYYY-MM-DD"
		if dimension == "month" {
			format = "YYYY-MM"
		}
		if dimension == "year" {
			format = "YYYY"
		}
		args = append(args, f.Timezone)
		date = fmt.Sprintf("TO_CHAR(ul.created_at AT TIME ZONE $%d,'%s')", len(args), format)
		keys = append(keys, date)
	}
	query := "WITH grouped AS MATERIALIZED (SELECT GROUPING(" + keys[0] + ")=1 AS is_summary," +
		vid + " AS vendor_id," + aid + " AS account_id," + aname + " AS account_name," + gid + " AS group_id," +
		gname + " AS group_name," + date + " AS date," + requested + " AS requested_model," + upstream + " AS upstream_model," +
		vendorUsageAggregateSQL() + ",CASE WHEN COUNT(DISTINCT CASE WHEN ul.platform_snapshot='openai' THEN COALESCE(NULLIF(ul.authorization_mode,''),'unknown') END) > 1 THEN 'mixed' ELSE COALESCE(MIN(CASE WHEN ul.platform_snapshot='openai' THEN COALESCE(NULLIF(ul.authorization_mode,''),'unknown') END),'') END AS authorization_mode,MIN(ul.created_at) AS earliest FROM usage_logs ul WHERE " + where +
		" GROUP BY GROUPING SETS ((" + strings.Join(keys, ",") + "),()))"
	args = append(args, limit, offset)
	query += fmt.Sprintf(",page AS (SELECT * FROM grouped s WHERE NOT is_summary ORDER BY %s LIMIT $%d OFFSET $%d)", order, len(args)-1, len(args))
	query += ",selected AS (SELECT * FROM grouped WHERE is_summary UNION ALL SELECT * FROM page) SELECT s.is_summary,(SELECT COUNT(*)-1 FROM grouped)," +
		"COALESCE(s.vendor_id,0),COALESCE(s.account_id,0),s.account_name," + deleted + "," + archived + "," + status + ",s.group_id,s.group_name,COALESCE(s.date,''),COALESCE(s.requested_model,''),COALESCE(s.upstream_model,'')," +
		"s.requests,s.input_tokens,s.output_tokens,s.cache_creation_tokens,s.cache_read_tokens,s.cost_usd::text,s.opus_cost_usd::text,s.fable_cost_usd::text,s.avg_duration_ms,s.avg_first_token_ms,s.earliest," + vendorName + ",s.authorization_mode" +
		" FROM selected s"
	if dimension == "account" || dimension == "account_date" {
		query += " LEFT JOIN accounts a ON a.id=s.account_id AND a.vendor_id=s.vendor_id"
	}
	if vendorName != "''::text" {
		query += " LEFT JOIN vendors v ON v.id=s.vendor_id"
	}
	return query + " ORDER BY s.is_summary DESC," + order, args, nil
}

func vendorUsageGrouped(ctx context.Context, q vendorSQL, f VendorUsageFilter, dimension string, limit, offset int) ([]VendorUsageRow, int64, VendorUsageMetrics, time.Time, error) {
	query, args, err := vendorUsageGroupedQuery(f, dimension, limit, offset)
	out := make([]VendorUsageRow, 0)
	var total int64
	var summary VendorUsageMetrics
	var earliest time.Time
	if err != nil {
		return out, total, summary, earliest, err
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return out, total, summary, earliest, err
	}
	defer rows.Close()
	for rows.Next() {
		var r VendorUsageRow
		var m vendorMetricsScan
		var isSummary bool
		var at sql.NullTime
		dest := []any{&isSummary, &total, &r.VendorID, &r.AccountID, &r.AccountName, &r.AccountDeleted, &r.AccountArchived, &r.AccountStatus, &r.GroupID, &r.GroupName, &r.Date, &r.RequestedModel, &r.UpstreamModel}
		dest = append(dest, m.dest()...)
		dest = append(dest, &at, &r.VendorName, &r.AuthorizationMode)
		if err := rows.Scan(dest...); err != nil {
			return out, total, summary, earliest, err
		}
		if isSummary {
			summary = m.finish()
			if at.Valid {
				earliest = at.Time
			}
		} else {
			if r.AuthorizationMode != "" && r.AuthorizationMode != "mixed" {
				r.AuthorizationMode = DisplayUsageAuthorizationMode(r.AuthorizationMode)
			}
			r.VendorUsageMetrics = m.finish()
			out = append(out, r)
		}
	}
	return out, total, summary, earliest, rows.Err()
}

// Request drilldown uses the existing VendorUsageEntry disclosure whitelist.
// Keep its frozen settlement serialization untouched: project explicitly into
// the report DTO, with current display metadata joined only for the bounded page.
// One aggregate provides both summary and total; a separate ordered page avoids
// materializing/grouping every historical request merely to return <=100 rows.
func vendorUsageRequests(ctx context.Context, q vendorSQL, f VendorUsageFilter) ([]VendorUsageRow, VendorUsageMetrics, error) {
	out := make([]VendorUsageRow, 0)
	order, err := vendorUsageOrderSQL(f, "request")
	if err != nil {
		return out, VendorUsageMetrics{}, err
	}
	summary, err := vendorUsageSummary(ctx, q, f)
	offset := (f.Page - 1) * f.PageSize
	if err != nil || int64(offset) >= summary.Requests {
		return out, summary, err
	}
	where, args := vendorUsageWhere(f)
	cost := vendorUsageCostSQL("ul")
	projection := "ul.id AS usage_log_id,ul.vendor_id_snapshot AS vendor_id,ul.account_id,COALESCE(ul.account_name_snapshot,'unknown') AS account_name," +
		"ul.group_id,COALESCE(ul.group_name_snapshot,'unknown') AS group_name,COALESCE(ul.platform_snapshot,'unknown') AS platform,COALESCE(NULLIF(ul.authorization_mode,''),'unknown') AS authorization_mode,ul.created_at," +
		"COALESCE(NULLIF(ul.requested_model,''),ul.model,'unknown') AS requested_model,COALESCE(NULLIF(ul.upstream_model,''),'unknown') AS upstream_model," +
		"1::bigint AS requests,ul.input_tokens,ul.output_tokens,ul.cache_creation_tokens,ul.cache_read_tokens," + cost + " AS cost_usd," +
		"CASE WHEN " + vendorUsageFamilySQL("opus") + " THEN " + cost + " ELSE 0 END AS opus_cost_usd," +
		"CASE WHEN " + vendorUsageFamilySQL("fable") + " THEN " + cost + " ELSE 0 END AS fable_cost_usd," +
		"ul.duration_ms AS avg_duration_ms,ul.first_token_ms AS avg_first_token_ms"
	args = append(args, f.PageSize, offset)
	query := "WITH page AS (SELECT * FROM (SELECT " + projection + " FROM usage_logs ul WHERE " + where + ") s ORDER BY " + order +
		fmt.Sprintf(" LIMIT $%d OFFSET $%d)", len(args)-1, len(args)) +
		" SELECT s.usage_log_id,s.vendor_id,s.account_id,s.account_name,s.group_id,s.group_name,s.platform,s.authorization_mode,s.created_at,s.requested_model,s.upstream_model," +
		"s.input_tokens,s.output_tokens,s.cache_creation_tokens,s.cache_read_tokens,s.cost_usd::text,s.opus_cost_usd::text,s.fable_cost_usd::text,s.avg_duration_ms,s.avg_first_token_ms," +
		"COALESCE(v.name,'unknown'),a.id IS NULL OR a.deleted_at IS NOT NULL,COALESCE(a.extra->>'vendor_archived_at','')<>'',COALESCE(a.status,'unknown')" +
		" FROM page s LEFT JOIN vendors v ON v.id=s.vendor_id LEFT JOIN accounts a ON a.id=s.account_id AND a.vendor_id=s.vendor_id ORDER BY " + order
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return out, summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var e VendorUsageEntry
		var r VendorUsageRow
		var m vendorMetricsScan
		err := rows.Scan(&e.UsageLogID, &e.VendorID, &e.AccountID, &e.AccountName, &e.GroupID, &e.GroupName, &e.Platform, &e.AuthorizationMode, &e.CreatedAt, &e.RequestedModel, &e.UpstreamModel,
			&e.InputTokens, &e.OutputTokens, &e.CacheCreationTokens, &e.CacheReadTokens, &e.CostUSD, &m.m.OpusCostUSD, &m.m.FableCostUSD, &m.duration, &m.first,
			&r.VendorName, &r.AccountDeleted, &r.AccountArchived, &r.AccountStatus)
		if err != nil {
			return out, summary, err
		}
		r.AuthorizationMode = DisplayUsageAuthorizationMode(e.AuthorizationMode)
		r.UsageLogID, r.CreatedAt, r.Platform, r.Result = e.UsageLogID, &e.CreatedAt, e.Platform, "unknown"
		r.VendorID, r.AccountID, r.AccountName = e.VendorID, e.AccountID, e.AccountName
		r.GroupID, r.GroupName, r.RequestedModel, r.UpstreamModel = e.GroupID, e.GroupName, e.RequestedModel, e.UpstreamModel
		m.m.Requests, m.m.InputTokens, m.m.OutputTokens = 1, e.InputTokens, e.OutputTokens
		m.m.CacheCreationTokens, m.m.CacheReadTokens, m.m.CostUSD = e.CacheCreationTokens, e.CacheReadTokens, e.CostUSD
		r.VendorUsageMetrics = m.finish()
		out = append(out, r)
	}
	return out, summary, rows.Err()
}

func vendorUsageFixedWindows(ctx context.Context, q vendorSQL, f VendorUsageFilter) (map[string]VendorUsageMetrics, error) {
	f.start, f.end = f.SnapshotAt.Add(-30*24*time.Hour), f.SnapshotAt
	f.GroupID, f.Platform = 0, ""
	where, args := vendorUsageWhere(f)
	keys := []string{"5h", "7d", "30d", "1m"}
	durations := []time.Duration{5 * time.Hour, 7 * 24 * time.Hour, 30 * 24 * time.Hour, time.Minute}
	metrics := make([]vendorMetricsScan, len(keys))
	var selects []string
	var dest []any
	for i, duration := range durations {
		args = append(args, f.SnapshotAt.Add(-duration))
		selects = append(selects, vendorUsageWindowAggregateSQL(fmt.Sprintf("ul.created_at >= $%d", len(args))))
		dest = append(dest, metrics[i].dest()...)
	}
	if err := q.QueryRowContext(ctx, "SELECT "+strings.Join(selects, ",")+" FROM usage_logs ul WHERE "+where, args...).Scan(dest...); err != nil {
		return nil, err
	}
	out := make(map[string]VendorUsageMetrics, len(keys))
	for i, key := range keys {
		out[key] = metrics[i].finish()
	}
	return out, nil
}
