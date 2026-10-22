package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const VendorUsageExportLimit = 10000
const vendorUsageMaxPage = 100000
const vendorUsageModelPageSize = 100

var (
	ErrVendorUsageInput         = infraerrors.New(422, "VENDOR_USAGE_INPUT", "Invalid usage or settlement input")
	ErrVendorUsageForbidden     = infraerrors.Forbidden("VENDOR_USAGE_FORBIDDEN", "Supplier scope or active super administrator required")
	ErrVendorSettlementConflict = infraerrors.Conflict("VENDOR_SETTLEMENT_CONFLICT", "Settlement changed, overlaps reserved usage, or request was already used")
	ErrVendorSettlementNotFound = infraerrors.NotFound("VENDOR_SETTLEMENT_NOT_FOUND", "Settlement not found")
)

type VendorUsageFilter struct {
	AuthorizationMode string `json:"authorization_mode,omitempty"`
	PayerVendorID     *int64 `json:"-"`
	rootVendorID      int64
	parentVendorID    *int64
	payerVendorID     *int64
	VendorID          int64     `json:"vendor_id"`
	AccountID         int64     `json:"account_id"`
	GroupID           int64     `json:"group_id"`
	Platform          string    `json:"platform"`
	StartDate         string    `json:"start_date"`
	EndDate           string    `json:"end_date"`
	Timezone          string    `json:"timezone"`
	Dimension         string    `json:"dimension"`
	SortBy            string    `json:"sort_by,omitempty"`
	Order             string    `json:"order,omitempty"`
	Page              int       `json:"page"`
	PageSize          int       `json:"page_size"`
	ModelPage         int       `json:"model_page,omitempty"`
	SnapshotAt        time.Time `json:"snapshot_at"`
	Watermark         int64     `json:"watermark"`
	AllHistory        bool      `json:"-"`
	WatermarkSet      bool      `json:"-"`
	start             time.Time
	end               time.Time
}
type VendorUsageMetrics struct {
	Requests            int64    `json:"requests"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheCreationTokens int64    `json:"cache_creation_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	TotalTokens         int64    `json:"total_tokens"`
	CacheTokens         int64    `json:"cache_tokens"`
	CostUSD             string   `json:"cost_usd"`
	OpusCostUSD         string   `json:"opus_cost_usd"`
	FableCostUSD        string   `json:"fable_cost_usd"`
	CacheHitRate        *float64 `json:"cache_hit_rate"`
	AvgDurationMS       *float64 `json:"avg_duration_ms"`
	AvgFirstTokenMS     *float64 `json:"avg_first_token_ms"`
	SuccessfulRequests  *int64   `json:"successful_requests"`
	FailedRequests      *int64   `json:"failed_requests"`
	SuccessRate         *float64 `json:"success_rate"`
}
type VendorUsageRow struct {
	// A grouped row may contain multiple modes; request rows are native/bps/unknown.
	AuthorizationMode string     `json:"authorization_mode"`
	UsageLogID        int64      `json:"usage_log_id,omitempty"`
	CreatedAt         *time.Time `json:"created_at,omitempty"`
	Platform          string     `json:"platform,omitempty"`
	Result            string     `json:"result,omitempty"`
	VendorID          int64      `json:"vendor_id,omitempty"`
	VendorName        string     `json:"vendor_name,omitempty"`
	AccountID         int64      `json:"account_id,omitempty"`
	AccountName       string     `json:"account_name,omitempty"`
	AccountDeleted    bool       `json:"account_deleted"`
	AccountArchived   bool       `json:"account_archived"`
	AccountStatus     string     `json:"account_status,omitempty"`
	GroupID           *int64     `json:"group_id,omitempty"`
	GroupName         string     `json:"group_name,omitempty"`
	Date              string     `json:"date,omitempty"`
	RequestedModel    string     `json:"requested_model,omitempty"`
	UpstreamModel     string     `json:"upstream_model,omitempty"`
	VendorUsageMetrics
}
type VendorUsageReport struct {
	Summary       VendorUsageMetrics `json:"summary"`
	Items         []VendorUsageRow   `json:"items"`
	Total         int64              `json:"total"`
	Page          int                `json:"page"`
	PageSize      int                `json:"page_size"`
	Dimension     string             `json:"dimension"`
	StartAt       time.Time          `json:"start_at"`
	EndAt         time.Time          `json:"end_at"`
	Timezone      string             `json:"timezone"`
	SnapshotAt    time.Time          `json:"snapshot_at"`
	Watermark     int64              `json:"watermark"`
	Source        string             `json:"source"`
	UnknownFields []string           `json:"unknown_fields"`
	earliest      time.Time
}
type VendorAccountUsage struct {
	*VendorUsageReport
	AccountArchived  bool                          `json:"account_archived"`
	Trends           []VendorUsageRow              `json:"trends"`
	TrendGranularity string                        `json:"trend_granularity"`
	Models           []VendorUsageRow              `json:"models"`
	ModelTotal       int64                         `json:"model_total"`
	ModelPage        int                           `json:"model_page"`
	ModelPageSize    int                           `json:"model_page_size"`
	Results          []VendorUsageResult           `json:"results"`
	FixedWindows     map[string]VendorUsageMetrics `json:"fixed_windows"`
	RecentMinute     VendorUsageMetrics            `json:"recent_minute"`
}
type VendorUsageResult struct {
	Result   string `json:"result"`
	Requests int64  `json:"requests"`
}
type vendorSQL interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Mutations hold the actor row lock through commit, serializing role demotion.
func (s *VendorService) vendorUsageAdmin(ctx context.Context, q vendorSQL, lock bool) (int64, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.UserID <= 0 || actor.Role != authz.SuperAdmin {
		return 0, ErrVendorUsageForbidden
	}
	if err := authz.Revalidate(ctx); err != nil {
		return 0, err
	}
	query := "SELECT role, status, session_generation FROM users WHERE id=$1 AND deleted_at IS NULL"
	if lock {
		query += " FOR SHARE"
	}
	var role, status string
	var generation int64
	if err := q.QueryRowContext(ctx, query, actor.UserID).Scan(&role, &status, &generation); err != nil {
		if err == sql.ErrNoRows {
			return 0, ErrVendorUsageForbidden
		}
		return 0, err
	}
	if role != authz.SuperAdmin || status != "active" || generation != actor.SessionGeneration {
		return 0, ErrVendorUsageForbidden
	}
	return actor.UserID, nil
}
func (s *VendorService) vendorUsageScope(ctx context.Context, f *VendorUsageFilter, admin bool) error {
	if admin {
		_, err := s.vendorUsageAdmin(ctx, s.db, false)
		return err
	}
	// Native admin account dispatch binds a verified direct-owner scope even
	// though it intentionally uses the scoped (non-aggregate) report handler.
	if actor, ok := authz.FromContext(ctx); ok && actor.Role == authz.SuperAdmin {
		scope, ok := authz.VendorScopeFromContext(ctx)
		if !ok || !scope.Admin || scope.ActorUserID != actor.UserID || scope.VendorID <= 0 || (f.VendorID != 0 && f.VendorID != scope.VendorID) {
			return ErrVendorUsageForbidden
		}
		if _, err := s.vendorUsageAdmin(ctx, s.db, false); err != nil {
			return err
		}
		f.VendorID = scope.VendorID
		return nil
	}
	v, err := s.vendorUsageIdentity(ctx, s.db, false)
	if err != nil {
		return err
	}
	f.rootVendorID, f.parentVendorID = 0, nil
	if v.Tier == 1 {
		// A root sees its own and direct children's historical supply, from snapshots.
		f.rootVendorID = v.ID
		if f.VendorID == v.ID {
			f.VendorID = 0
		}
		if f.VendorID != 0 {
			var child bool
			if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM vendors WHERE id=$1 AND tier=2 AND parent_vendor_id=$2)", f.VendorID, v.ID).Scan(&child); err != nil {
				return err
			}
			if !child {
				return ErrVendorUsageForbidden
			}
		}
	} else {
		if f.VendorID != 0 && f.VendorID != v.ID {
			return ErrVendorUsageForbidden
		}
		f.VendorID = v.ID
	}

	return nil
}
func normalizeVendorUsageFilter(f *VendorUsageFilter, now time.Time) error {
	if f.VendorID < 0 || f.AccountID < 0 || f.GroupID < 0 || f.Watermark < 0 {
		return ErrVendorUsageInput
	}
	if f.Platform != "" && f.Platform != "anthropic" && f.Platform != "openai" {
		return ErrVendorUsageInput
	}
	if f.AuthorizationMode != "" && UsageAuthorizationModePtr(f.AuthorizationMode) == nil {
		return ErrVendorUsageInput
	}
	if f.Timezone == "" {
		f.Timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(f.Timezone)
	if err != nil {
		return ErrVendorUsageInput
	}
	if f.SnapshotAt.IsZero() {
		f.SnapshotAt = now.UTC()
	}
	if f.SnapshotAt.After(now.Add(time.Second)) {
		return ErrVendorUsageInput
	}
	today := time.Date(now.In(loc).Year(), now.In(loc).Month(), now.In(loc).Day(), 0, 0, 0, 0, loc)
	if f.AllHistory && f.StartDate == "" && f.EndDate == "" {
		f.start = time.Unix(0, 0).UTC()
		f.end = today.AddDate(0, 0, 1)
	} else {
		if f.StartDate == "" && f.EndDate == "" {
			f.StartDate = today.AddDate(0, 0, -6).Format(time.DateOnly)
			f.EndDate = today.Format(time.DateOnly)
		}
		start, e1 := time.ParseInLocation(time.DateOnly, f.StartDate, loc)
		end, e2 := time.ParseInLocation(time.DateOnly, f.EndDate, loc)
		if e1 != nil || e2 != nil || end.Before(start) || (!f.AllHistory && end.After(start.AddDate(0, 0, 30))) {
			return ErrVendorUsageInput
		}
		f.start = start
		f.end = end.AddDate(0, 0, 1)
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	if f.ModelPage == 0 {
		f.ModelPage = 1
	}
	if f.ModelPage < 1 || f.ModelPage > vendorUsageMaxPage {
		return ErrVendorUsageInput
	}
	if f.Page < 1 || f.Page > vendorUsageMaxPage || f.PageSize < 1 || f.PageSize > 100 {
		return ErrVendorUsageInput
	}
	if f.Dimension == "" {
		f.Dimension = "account"
	}
	switch f.Dimension {
	case "vendor", "account", "date", "group", "account_date", "request":
	default:
		return ErrVendorUsageInput
	}
	if _, err := vendorUsageOrderSQL(*f, f.Dimension); err != nil {
		return err
	}
	return nil
}

// Snapshot attribution alone grants historical scope, never the current owner.
func vendorUsageWhere(f VendorUsageFilter) (string, []any) {
	args := []any{f.start, f.end, f.SnapshotAt, f.Watermark}
	where := "ul.vendor_id_snapshot IS NOT NULL AND ul.created_at >= $1 AND ul.created_at < $2 AND ul.created_at <= $3 AND ul.id <= $4"
	add := func(clause string, v any) { args = append(args, v); where += fmt.Sprintf(" AND "+clause, len(args)) }
	if f.VendorID > 0 {
		add("ul.vendor_id_snapshot=$%d", f.VendorID)
	}
	if f.rootVendorID > 0 {
		add("ul.vendor_root_id_snapshot=$%d", f.rootVendorID)
	}
	if f.parentVendorID != nil {
		add("ul.vendor_parent_id_snapshot=$%d", *f.parentVendorID)
	}
	if f.AccountID > 0 {
		add("ul.account_id=$%d", f.AccountID)
	}
	if f.GroupID > 0 {
		add("ul.group_id=$%d", f.GroupID)
	}
	if f.Platform != "" {
		add("ul.platform_snapshot=$%d", f.Platform)
	}
	if f.AuthorizationMode != "" {
		add("ul.authorization_mode=$%d", f.AuthorizationMode)
	}
	return where, args
}

type vendorMetricsScan struct {
	m               VendorUsageMetrics
	duration, first sql.NullFloat64
}

func (m *vendorMetricsScan) dest() []any {
	return []any{&m.m.Requests, &m.m.InputTokens, &m.m.OutputTokens, &m.m.CacheCreationTokens, &m.m.CacheReadTokens, &m.m.CostUSD, &m.m.OpusCostUSD, &m.m.FableCostUSD, &m.duration, &m.first}
}
func (m *vendorMetricsScan) finish() VendorUsageMetrics {
	m.m.TotalTokens = m.m.InputTokens + m.m.OutputTokens
	m.m.CacheTokens = m.m.CacheCreationTokens + m.m.CacheReadTokens
	if m.duration.Valid {
		v := m.duration.Float64
		m.m.AvgDurationMS = &v
	}
	if m.first.Valid {
		v := m.first.Float64
		m.m.AvgFirstTokenMS = &v
	}
	denom := float64(m.m.InputTokens) + float64(m.m.CacheTokens)
	if denom > 0 {
		v := float64(m.m.CacheReadTokens) / denom
		m.m.CacheHitRate = &v
	}
	for _, cost := range []*string{&m.m.CostUSD, &m.m.OpusCostUSD, &m.m.FableCostUSD} {
		if d, e := decimal.NewFromString(*cost); e == nil {
			*cost = d.String()
		}
	}
	return m.m
}
func vendorUsageSummary(ctx context.Context, q vendorSQL, f VendorUsageFilter) (VendorUsageMetrics, error) {
	where, args := vendorUsageWhere(f)
	var m vendorMetricsScan
	err := q.QueryRowContext(ctx, "SELECT "+vendorUsageAggregateSQL()+" FROM usage_logs ul WHERE "+where, args...).Scan(m.dest()...)
	return m.finish(), err
}
func vendorUsageWatermark(ctx context.Context, q vendorSQL, f *VendorUsageFilter) error {
	if f.WatermarkSet || f.Watermark > 0 {
		return nil
	}
	watermarkFilter := *f
	watermarkFilter.start, watermarkFilter.end = time.Unix(0, 0), f.SnapshotAt.Add(time.Nanosecond)
	watermarkFilter.Watermark = int64(^uint64(0) >> 1)
	where, args := vendorUsageWhere(watermarkFilter)
	query := "SELECT COALESCE(MAX(ul.id),0) FROM usage_logs ul WHERE " + where

	return q.QueryRowContext(ctx, query, args...).Scan(&f.Watermark)
}
func (s *VendorService) VendorUsage(ctx context.Context, f VendorUsageFilter, admin bool) (*VendorUsageReport, error) {
	if err := s.vendorUsageScope(ctx, &f, admin); err != nil {
		return nil, err
	}
	if f.Dimension == "request" && !admin {
		return nil, ErrVendorUsageInput
	}
	if err := normalizeVendorUsageFilter(&f, time.Now()); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = vendorUsageWatermark(ctx, tx, &f); err != nil {
		return nil, err
	}
	out, err := vendorUsageReport(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if err = s.vendorUsageScope(ctx, &f, admin); err != nil {
		return nil, err
	}
	return out, nil
}
func vendorUsageReport(ctx context.Context, q vendorSQL, f VendorUsageFilter) (*VendorUsageReport, error) {
	var items []VendorUsageRow
	var total int64
	var summary VendorUsageMetrics
	var earliest time.Time
	var err error
	if f.Dimension == "request" {
		items, summary, err = vendorUsageRequests(ctx, q, f)
		total = summary.Requests
	} else {
		items, total, summary, earliest, err = vendorUsageGrouped(ctx, q, f, f.Dimension, f.PageSize, (f.Page-1)*f.PageSize)
	}
	if err != nil {
		return nil, err
	}
	return &VendorUsageReport{Summary: summary, Items: items, Total: total, Page: f.Page, PageSize: f.PageSize, Dimension: f.Dimension, StartAt: f.start, EndAt: f.end, Timezone: f.Timezone, SnapshotAt: f.SnapshotAt, Watermark: f.Watermark, Source: "usage_logs.account_stats_cost", UnknownFields: []string{"successful_requests", "failed_requests", "success_rate", "upstream_quota"}, earliest: earliest}, nil
}
func (s *VendorService) VendorAccountUsage(ctx context.Context, f VendorUsageFilter, admin bool) (*VendorAccountUsage, error) {
	if f.AccountID <= 0 {
		return nil, ErrVendorUsageInput
	}
	f.AllHistory = true
	f.Dimension = "date"
	if err := s.vendorUsageScope(ctx, &f, admin); err != nil {
		return nil, err
	}
	if err := normalizeVendorUsageFilter(&f, time.Now()); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var owned, archived bool
	err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts a JOIN vendors v ON v.id=a.vendor_id WHERE a.id=$1 AND (($2>0 AND a.vendor_id=$2) OR ($2=0 AND (v.id=$3 OR v.parent_vendor_id=$3)))) OR EXISTS(SELECT 1 FROM usage_logs WHERE account_id=$1 AND ($2=0 OR vendor_id_snapshot=$2) AND ($3=0 OR vendor_root_id_snapshot=$3)),EXISTS(SELECT 1 FROM accounts WHERE id=$1 AND COALESCE(extra->>'vendor_archived_at','')<>'')", f.AccountID, f.VendorID, f.rootVendorID).Scan(&owned, &archived)
	if err != nil {
		return nil, err
	}
	if !owned {
		return nil, infraerrors.NotFound("VENDOR_ACCOUNT_NOT_FOUND", "Account not found")
	}
	if err = vendorUsageWatermark(ctx, tx, &f); err != nil {
		return nil, err
	}
	report, err := vendorUsageReport(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	granularity := "date"
	if !report.earliest.IsZero() {
		span := f.end.Sub(report.earliest)
		if span > 180*24*time.Hour {
			granularity = "month"
		}
		if span > 15*365*24*time.Hour {
			granularity = "year"
		}
	}
	// Trend chronology and model ranking are independent of the requested table sort.
	detailFilter := f
	detailFilter.SortBy, detailFilter.Order = "", ""
	trends, _, _, _, err := vendorUsageGrouped(ctx, tx, detailFilter, granularity, 200, 0)
	if err != nil {
		return nil, err
	}
	models, modelTotal, _, _, err := vendorUsageGrouped(ctx, tx, detailFilter, "model", vendorUsageModelPageSize, (f.ModelPage-1)*vendorUsageModelPageSize)
	if err != nil {
		return nil, err
	}
	fixed, err := vendorUsageFixedWindows(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	recent := fixed["1m"]
	delete(fixed, "1m")
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if err = s.vendorUsageScope(ctx, &f, admin); err != nil {
		return nil, err
	}
	return &VendorAccountUsage{VendorUsageReport: report, AccountArchived: archived, Trends: trends, TrendGranularity: granularity, Models: models, ModelTotal: modelTotal, ModelPage: f.ModelPage, ModelPageSize: vendorUsageModelPageSize, Results: []VendorUsageResult{{Result: "unknown", Requests: report.Summary.Requests}}, FixedWindows: fixed, RecentMinute: recent}, nil
}
