package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type VendorSettlementListFilter struct {
	Direction     string
	PayerVendorID *int64
	VendorID      int64
	Status        string
	StartDate     string
	EndDate       string
	Timezone      string
	Page          int
	PageSize      int
}
type VendorSettlementList struct {
	Items    []*VendorSettlement `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func (s *VendorService) vendorSettlementReadScope(ctx context.Context, admin bool) (int64, error) {
	if admin {
		_, err := s.vendorUsageAdmin(ctx, s.db, false)
		return 0, err
	}
	v, err := s.vendorUsageIdentity(ctx, s.db, false)
	if err != nil {
		return 0, err
	}
	return v.ID, nil
}
func (s *VendorService) ListVendorSettlements(ctx context.Context, in VendorSettlementListFilter, admin bool) (*VendorSettlementList, error) {
	vendorID, err := s.vendorSettlementReadScope(ctx, admin)
	if err != nil {
		return nil, err
	}
	if in.Direction != "" && in.Direction != "receivable" && in.Direction != "payable" {
		return nil, ErrVendorUsageInput
	}
	payable := admin || in.Direction == "payable"
	if !admin {
		v, err := s.vendorUsageIdentity(ctx, s.db, false)
		if err != nil {
			return nil, err
		}
		if payable {
			if v.Tier != 1 || (in.PayerVendorID != nil && *in.PayerVendorID != vendorID) {
				return nil, ErrVendorUsageForbidden
			}
			in.PayerVendorID = &vendorID
			if in.VendorID > 0 {
				if _, err := vendorSettlementPayee(ctx, s.db, in.VendorID, &vendorID); err != nil {
					return nil, err
				}
			}
		} else {
			if in.VendorID != 0 && in.VendorID != vendorID {
				return nil, ErrVendorUsageForbidden
			}
			if in.PayerVendorID != nil && !vendorSettlementSamePayer(in.PayerVendorID, v.ParentVendorID) {
				return nil, ErrVendorUsageForbidden
			}
			in.PayerVendorID = v.ParentVendorID
			in.VendorID = vendorID
		}
	}
	if in.PayerVendorID != nil && *in.PayerVendorID <= 0 {
		return nil, ErrVendorUsageInput
	}
	if in.Page == 0 {
		in.Page = 1
	}
	if in.PageSize == 0 {
		in.PageSize = 20
	}
	if in.Page < 1 || in.Page > vendorUsageMaxPage || in.PageSize < 1 || in.PageSize > 100 || in.VendorID < 0 {
		return nil, ErrVendorUsageInput
	}
	where := "TRUE"
	args := []any{}
	add := func(clause string, val any) {
		args = append(args, val)
		where += fmt.Sprintf(" AND "+clause, len(args))
	}
	if in.VendorID > 0 {
		add("vendor_id=$%d", in.VendorID)
	}
	if in.PayerVendorID == nil {
		where += " AND payer_vendor_id IS NULL"
	} else {
		add("payer_vendor_id=$%d", *in.PayerVendorID)
	}
	if !payable {
		where += " AND status IN ('pending_payment','partially_paid','paid')"
	}
	if in.Status != "" {
		switch in.Status {
		case "draft", "pending_payment", "partially_paid", "paid", "void":
		default:
			return nil, ErrVendorUsageInput
		}
		add("status=$%d", in.Status)
	}
	if in.StartDate != "" || in.EndDate != "" {
		f := VendorUsageFilter{StartDate: in.StartDate, EndDate: in.EndDate, Timezone: in.Timezone, AllHistory: true}
		if err = normalizeVendorUsageFilter(&f, time.Now()); err != nil {
			return nil, err
		}
		add("end_at>$%d", f.start)
		add("start_at<$%d", f.end)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &VendorSettlementList{Items: []*VendorSettlement{}, Page: in.Page, PageSize: in.PageSize}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendor_settlements WHERE "+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	args = append(args, in.PageSize, (in.Page-1)*in.PageSize)
	rows, err := tx.QueryContext(ctx, "SELECT "+vendorSettlementColumns+" FROM vendor_settlements WHERE "+where+fmt.Sprintf(" ORDER BY id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		st, e := scanVendorSettlement(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, st.ID)
		st.AllocationTotal = st.UsageCount
		out.Items = append(out.Items, st)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	credits := map[int64]decimal.Decimal{}
	if len(ids) > 0 {
		creditRows, e := tx.QueryContext(ctx, "SELECT parent_statement_id,SUM(total)::text FROM vendor_settlements WHERE parent_statement_id=ANY($1) AND kind='credit' AND status IN ('pending_payment','partially_paid','paid') GROUP BY parent_statement_id", pq.Array(ids))
		if e != nil {
			return nil, e
		}
		for creditRows.Next() {
			var id int64
			var val string
			if e = creditRows.Scan(&id, &val); e != nil {
				creditRows.Close()
				return nil, e
			}
			amount, e := decimal.NewFromString(val)
			if e != nil {
				creditRows.Close()
				return nil, e
			}
			credits[id] = amount
		}
		e = creditRows.Err()
		creditRows.Close()
		if e != nil {
			return nil, e
		}
	}
	for _, st := range out.Items {
		if e := applyVendorSettlementBalance(st, credits[st.ID]); e != nil {
			return nil, e
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if _, err = s.vendorSettlementReadScope(ctx, admin); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *VendorService) GetVendorSettlement(ctx context.Context, id int64, page, pageSize int, admin bool, direction ...string) (*VendorSettlement, error) {
	return s.vendorSettlementDetail(ctx, id, page, pageSize, admin, false, direction...)
}
func (s *VendorService) vendorSettlementDetail(ctx context.Context, id int64, page, pageSize int, admin, export bool, direction ...string) (*VendorSettlement, error) {
	vendorID, err := s.vendorSettlementReadScope(ctx, admin)
	if err != nil {
		return nil, err
	}
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	max := 100
	if export {
		max = VendorUsageExportLimit
		page = 1
		pageSize = max
	}
	if id <= 0 || page < 1 || page > vendorUsageMaxPage || pageSize < 1 || pageSize > max {
		return nil, ErrVendorUsageInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	st, err := loadVendorSettlement(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	payable := st.PayerVendorID != nil && *st.PayerVendorID == vendorID
	if len(direction) > 0 {
		switch direction[0] {
		case "payable":
			if admin || !payable {
				return nil, ErrVendorSettlementNotFound
			}
		case "receivable":
			if !admin && st.VendorID != vendorID {
				return nil, ErrVendorSettlementNotFound
			}
		default:
			return nil, ErrVendorUsageInput
		}
	}
	if !admin && !payable && (st.VendorID != vendorID || !vendorSettlementVisible(st.Status)) {
		return nil, ErrVendorSettlementNotFound
	}
	if err = vendorSettlementBalance(ctx, tx, st); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendor_settlement_allocations WHERE statement_id=$1", id).Scan(&st.AllocationTotal); err != nil {
		return nil, err
	}
	if export && st.AllocationTotal > int64(max) {
		return nil, ErrVendorUsageInput
	}
	st.Allocations, err = vendorSettlementAllocations(ctx, tx, id, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	st.Page = page
	st.PageSize = pageSize
	if export {
		hash, e := vendorHash(st.Allocations)
		if e != nil {
			return nil, e
		}
		if hash != st.DataHash || st.AllocationTotal != st.UsageCount {
			return nil, ErrVendorSettlementConflict
		}
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendor_settlement_payments WHERE statement_id=$1", id).Scan(&st.PaymentTotal); err != nil {
		return nil, err
	}
	if export && st.PaymentTotal+st.AllocationTotal+int64(len(st.Lines)) > VendorUsageExportLimit {
		return nil, ErrVendorUsageInput
	}
	st.Payments, err = vendorSettlementPaymentRows(ctx, tx, id, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, err
	}
	st.LateUsage = VendorSettlementLateUsage{CostUSD: "0", CollectedAt: time.Now().UTC()}
	if st.Kind == "usage" && st.Status != "void" {
		f := VendorUsageFilter{VendorID: st.VendorID, AccountID: st.AccountID, GroupID: st.GroupID, Platform: st.Platform, start: st.StartAt, end: st.EndAt, SnapshotAt: st.LateUsage.CollectedAt, Watermark: int64(^uint64(0) >> 1)}
		f.payerVendorID = st.PayerVendorID
		if st.PayerVendorID == nil {
			f.rootVendorID, f.VendorID = st.VendorID, 0
		} else {
			f.parentVendorID = st.PayerVendorID
		}
		where, args := vendorUsageWhere(f)
		query := "SELECT COUNT(*),COALESCE(SUM(" + vendorUsageCostSQL("ul") + "),0)::text FROM usage_logs ul WHERE " + where + " AND NOT " + vendorUsageAllocatedSQL("a", f)
		if err = tx.QueryRowContext(ctx, query, args...).Scan(&st.LateUsage.Requests, &st.LateUsage.CostUSD); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if _, err = s.vendorSettlementReadScope(ctx, admin); err != nil {
		return nil, err
	}
	return st, nil
}
func (s *VendorService) VendorSettlementCSV(ctx context.Context, id int64, admin bool, direction ...string) ([]byte, error) {
	st, err := s.vendorSettlementDetail(ctx, id, 1, VendorUsageExportLimit, admin, true, direction...)
	if err != nil {
		return nil, err
	}
	header := []string{"row_type", "statement_number", "vendor_id", "usage_log_id", "account_id", "account_name", "group_id", "group_name", "request_time", "requested_model", "upstream_model", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "usage_cost_usd", "group_cost_usd", "coefficient", "contract_version", "group_amount_usdt", "adjustment_usdt", "statement_cost_usd", "statement_total_usdt", "statement_paid_usdt", "statement_credited_usdt", "statement_unpaid_usdt", "payment_amount_usdt", "payment_reference", "payment_channel", "payment_date", "reversal_of_id", "reason", "exchange_rate", "exchange_rate_source", "coefficient_unit", "settlement_currency", "timezone", "start_at", "end_at_exclusive", "snapshot_at", "snapshot_hash", "contact_name", "settlement_email", "binance_id", "kind", "payer_vendor_id", "direct_vendor_id", "root_vendor_id", "parent_vendor_id", "cost_basis"}
	rows := [][]string{header}
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		return nil, err
	}
	base := func(kind string) []string {
		r := make([]string, len(header))
		r[0] = kind
		r[1] = st.Number
		r[2] = vendorInt(st.VendorID)
		r[18] = vendorInt(st.ContractVersion)
		r[32] = st.ExchangeRate
		r[33] = st.ExchangeRateSource
		r[34] = st.CoefficientUnit
		r[35] = st.SettlementCurrency
		r[36] = st.Timezone
		r[37] = st.StartAt.In(loc).Format(time.RFC3339)
		r[38] = st.EndAt.In(loc).Format(time.RFC3339)
		r[39] = st.SnapshotAt.Format(time.RFC3339Nano)
		r[40] = st.SnapshotHash
		r[44] = st.Kind
		r[45] = vendorOptionalID(st.PayerVendorID)
		r[49] = st.CostBasis
		return r
	}
	for _, e := range st.Allocations {
		r := base("usage")
		r[3] = vendorInt(e.UsageLogID)
		r[46], r[47], r[48] = vendorInt(e.VendorID), vendorOptionalID(e.RootVendorID), vendorOptionalID(e.ParentVendorID)
		r[4] = vendorInt(e.AccountID)
		r[5] = e.AccountName
		r[6] = vendorOptionalID(e.GroupID)
		r[7] = e.GroupName
		r[8] = e.CreatedAt.In(loc).Format(time.RFC3339)
		r[9] = e.RequestedModel
		r[10] = e.UpstreamModel
		r[11] = vendorInt(e.InputTokens)
		r[12] = vendorInt(e.OutputTokens)
		r[13] = vendorInt(e.CacheCreationTokens)
		r[14] = vendorInt(e.CacheReadTokens)
		r[15] = e.CostUSD
		r[18] = vendorInt(e.ContractVersion)
		rows = append(rows, r)
	}
	for _, l := range st.Lines {
		r := base("group")
		r[6] = vendorOptionalID(l.GroupID)
		r[7] = l.GroupName
		r[16] = l.CostUSD
		r[17] = l.Coefficient
		r[18] = vendorInt(l.ContractVersion)
		r[19] = l.Amount
		r[31] = l.Reason
		rows = append(rows, r)
	}
	for _, p := range st.Payments {
		r := base("payment")
		r[26] = p.Amount
		r[27] = p.Reference
		r[28] = p.Channel
		r[29] = p.PaidAt.In(loc).Format(time.RFC3339)
		r[30] = vendorOptionalID(p.ReversalOfID)
		r[31] = p.Reason
		rows = append(rows, r)
	}
	r := base("total")
	r[20] = st.Adjustment
	r[21] = st.CostUSD
	r[22] = st.Total
	r[23] = st.Paid
	r[24] = st.Credited
	r[25] = st.Unpaid
	r[31] = st.AdjustmentReason
	r[41] = st.Payee.ContactName
	r[42] = st.Payee.SettlementEmail
	r[43] = st.Payee.BinanceID
	rows = append(rows, r)
	// Numeric financial columns retain signed decimal semantics; text never executes.
	numeric := map[int]bool{11: true, 12: true, 13: true, 14: true, 15: true, 16: true, 17: true, 18: true, 19: true, 20: true, 21: true, 22: true, 23: true, 24: true, 25: true, 26: true, 32: true}
	for i := 1; i < len(rows); i++ {
		for j := range rows[i] {
			if !numeric[j] {
				rows[i][j] = vendorCSVText(rows[i][j])
			}
		}
	}
	data, err := vendorCSV(rows)
	if err != nil {
		return nil, err
	}
	if _, err = s.vendorSettlementReadScope(ctx, admin); err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, st.VendorID, "settlement.export", st.ID, "success")
	return data, nil
}
