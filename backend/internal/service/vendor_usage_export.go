package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// VendorUsageEntry is the sole source-data whitelist used for CSV and durable
// allocations. Never embed UsageLog: it includes customer identity and charges.
type VendorUsageEntry struct {
	// Omit absent historical snapshots to preserve frozen allocation hashes.
	// API consumers interpret an absent mode as unknown; never infer it anew.
	AuthorizationMode string `json:"authorization_mode,omitempty"`
	RootVendorID      *int64 `json:"root_vendor_id,omitempty"`
	ParentVendorID    *int64 `json:"parent_vendor_id,omitempty"`
	// Opaque native billing identity stays server-side; never part of supplier JSON.
	sourceFingerprint   *string
	ContractVersion     int64     `json:"contract_version,omitempty"`
	UsageLogID          int64     `json:"usage_log_id"`
	VendorID            int64     `json:"vendor_id"`
	AccountID           int64     `json:"account_id"`
	AccountName         string    `json:"account_name"`
	GroupID             *int64    `json:"group_id"`
	GroupName           string    `json:"group_name"`
	Platform            string    `json:"platform"`
	CreatedAt           time.Time `json:"created_at"`
	RequestedModel      string    `json:"requested_model"`
	UpstreamModel       string    `json:"upstream_model"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	CostUSD             string    `json:"cost_usd"`
}

// Retain the native request/API-key uniqueness after disposable usage rows are
// removed. Neither source identifier is exposed or retained in clear text.
const vendorUsageSourceFingerprintSQL = "CASE WHEN COALESCE(ul.request_id,'')<>'' AND ul.api_key_id IS NOT NULL THEN encode(sha256(convert_to(ul.request_id || ':' || ul.api_key_id::text,'UTF8')),'hex') END"

func vendorUsageAllocatedSQL(alias string, f VendorUsageFilter) string {
	// Reservations belong to the payer's book. Including the payee here would
	// let a retained native source be billed again after raw cleanup and rerouting.
	edge := alias + ".payer_vendor_id IS NULL"
	if f.payerVendorID != nil {
		edge = alias + ".payer_vendor_id=" + vendorInt(*f.payerVendorID)
	}
	return "EXISTS (SELECT 1 FROM vendor_settlement_allocations " + alias + " WHERE " + alias + ".active AND " + edge + " AND (" + alias + ".usage_log_id=ul.id OR " + alias + ".source_fingerprint=(" + vendorUsageSourceFingerprintSQL + ")))"
}

func vendorUsageEntries(ctx context.Context, q vendorSQL, f VendorUsageFilter, unallocated, lock bool) ([]VendorUsageEntry, error) {
	where, args := vendorUsageWhere(f)
	if unallocated {
		where += " AND NOT " + vendorUsageAllocatedSQL("va", f)
	}
	query := "SELECT " + vendorUsageSourceFingerprintSQL + ",ul.id,ul.vendor_id_snapshot,ul.vendor_root_id_snapshot,ul.vendor_parent_id_snapshot,ul.account_id,COALESCE(ul.account_name_snapshot,'unknown'),ul.group_id,COALESCE(ul.group_name_snapshot,'unknown'),COALESCE(ul.platform_snapshot,'unknown'),COALESCE(NULLIF(ul.authorization_mode,''),'unknown'),ul.created_at,COALESCE(NULLIF(ul.requested_model,''),ul.model),COALESCE(NULLIF(ul.upstream_model,''),'unknown'),ul.input_tokens,ul.output_tokens,ul.cache_creation_tokens,ul.cache_read_tokens,(" + AccountStatsCostSQL("ul") + ")::text FROM usage_logs ul WHERE " + where + " ORDER BY ul.id LIMIT " + strconv.Itoa(VendorUsageExportLimit+1)
	if lock {
		query += " FOR SHARE OF ul"
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VendorUsageEntry, 0)
	for rows.Next() {
		var r VendorUsageEntry
		if err = rows.Scan(&r.sourceFingerprint, &r.UsageLogID, &r.VendorID, &r.RootVendorID, &r.ParentVendorID, &r.AccountID, &r.AccountName, &r.GroupID, &r.GroupName, &r.Platform, &r.AuthorizationMode, &r.CreatedAt, &r.RequestedModel, &r.UpstreamModel, &r.InputTokens, &r.OutputTokens, &r.CacheCreationTokens, &r.CacheReadTokens, &r.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > VendorUsageExportLimit {
		return nil, ErrVendorUsageInput.WithMetadata(map[string]string{"limit": strconv.Itoa(VendorUsageExportLimit), "action": "narrow_date_range"})
	}
	return out, nil
}

// Excel interprets leading whitespace before formula markers. Prefix long digit
// identifiers too so receipt/account IDs retain precision and leading zeros.
func vendorCSVText(v string) string {
	trimmed := strings.TrimLeftFunc(v, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' })
	dangerous := len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0]))
	digits := len(v) > 0
	for _, r := range v {
		if r < '0' || r > '9' {
			digits = false
			break
		}
	}
	if dangerous || strings.ContainsAny(v, "\t\r\n") || (digits && (len(v) > 15 || (len(v) > 1 && v[0] == '0'))) {
		return "'" + v
	}
	return v
}
func vendorCSV(records [][]string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&b)
	for _, r := range records {
		if err := w.Write(r); err != nil {
			return nil, err
		}
		if b.Len() > 16<<20 {
			return nil, ErrVendorUsageInput
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	if b.Len() > 16<<20 {
		return nil, ErrVendorUsageInput
	}
	return b.Bytes(), nil
}
func vendorInt(v int64) string { return strconv.FormatInt(v, 10) }
func vendorOptionalID(v *int64) string {
	if v == nil {
		return ""
	}
	return vendorInt(*v)
}

func (s *VendorService) VendorUsageCSV(ctx context.Context, f VendorUsageFilter) ([]byte, error) {
	if err := s.vendorUsageScope(ctx, &f, true); err != nil {
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
	if err = vendorUsageWatermark(ctx, tx, &f); err != nil {
		return nil, err
	}
	entries, err := vendorUsageEntries(ctx, tx, f, false, false)
	if err != nil {
		return nil, err
	}
	totals, err := vendorUsageSummary(ctx, tx, f)
	if err != nil {
		return nil, err
	}
	rows := [][]string{{"row_type", "usage_log_id", "vendor_id", "account_id", "account_name", "group_id", "group_name", "platform", "request_time", "requested_model", "upstream_model", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "usage_cost_usd", "total_cost_usd", "requests", "timezone", "start_at", "end_at_exclusive", "snapshot_at", "watermark", "cost_basis", "authorization_mode"}}
	loc, _ := time.LoadLocation(f.Timezone)
	suffix := []string{f.Timezone, f.start.Format(time.RFC3339), f.end.Format(time.RFC3339), f.SnapshotAt.Format(time.RFC3339Nano), vendorInt(f.Watermark), "historical_account_stats_cost_x_account_rate_multiplier"}
	for _, e := range entries {
		r := []string{"usage", vendorInt(e.UsageLogID), vendorInt(e.VendorID), vendorInt(e.AccountID), e.AccountName, vendorOptionalID(e.GroupID), e.GroupName, e.Platform, e.CreatedAt.In(loc).Format(time.RFC3339), e.RequestedModel, e.UpstreamModel, vendorInt(e.InputTokens), vendorInt(e.OutputTokens), vendorInt(e.CacheCreationTokens), vendorInt(e.CacheReadTokens), e.CostUSD, "", ""}
		r = append(r, suffix...)
		r = append(r, DisplayUsageAuthorizationMode(e.AuthorizationMode))
		for i := range r {
			r[i] = vendorCSVText(r[i])
		}
		rows = append(rows, r)
	}
	total := make([]string, len(rows[0]))
	total[0] = "total"
	total[11] = vendorInt(totals.InputTokens)
	total[12] = vendorInt(totals.OutputTokens)
	total[13] = vendorInt(totals.CacheCreationTokens)
	total[14] = vendorInt(totals.CacheReadTokens)
	total[16] = totals.CostUSD
	total[17] = vendorInt(totals.Requests)
	copy(total[18:], suffix)
	total[len(total)-1] = f.AuthorizationMode
	rows = append(rows, total)
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	data, err := vendorCSV(rows)
	if err != nil {
		return nil, err
	}
	if _, err = s.vendorUsageAdmin(ctx, s.db, false); err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, f.VendorID, "usage.export", 0, fmt.Sprintf("rows=%d", len(entries)))
	return data, nil
}
