package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

func isVendorConflict(err error) bool {
	if errors.Is(err, ErrVendorSettlementConflict) {
		return true
	}
	var state interface{ SQLState() string }
	return errors.As(err, &state) && (state.SQLState() == "23505" || state.SQLState() == "40001" || state.SQLState() == "40P01")
}
func vendorSettlementDBError(err error) error {
	if isVendorConflict(err) {
		return ErrVendorSettlementConflict
	}
	return err
}

const vendorSettlementColumns = "id,number,status,version,snapshot,snapshot_hash,total::text,paid::text,input_hash,created_by,confirmed_by,created_at,confirmed_at,COALESCE(void_reason,''),vendor_id,payer_vendor_id"

func loadVendorSettlement(ctx context.Context, q vendorSQL, id int64, lock bool) (*VendorSettlement, error) {
	query := "SELECT " + vendorSettlementColumns + " FROM vendor_settlements WHERE id=$1"
	if lock {
		query += " FOR UPDATE"
	}
	return scanVendorSettlement(q.QueryRowContext(ctx, query, id))
}
func scanVendorSettlement(row interface{ Scan(...any) error }) (*VendorSettlement, error) {
	var out VendorSettlement
	var raw []byte
	var total string
	var vendorID int64
	var payerID *int64
	err := row.Scan(&out.ID, &out.Number, &out.Status, &out.Version, &raw, &out.SnapshotHash, &total, &out.Paid, &out.inputHash, &out.CreatedBy, &out.ConfirmedBy, &out.CreatedAt, &out.ConfirmedAt, &out.VoidReason, &vendorID, &payerID)
	if err == sql.ErrNoRows {
		return nil, ErrVendorSettlementNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.VendorSettlementSnapshot); err != nil {
		return nil, err
	}
	hash, err := vendorHash(out.VendorSettlementSnapshot)
	if err != nil {
		return nil, err
	}
	d, e := decimal.NewFromString(total)
	sd, se := decimal.NewFromString(out.Total)
	if out.VendorID != vendorID || !vendorSettlementSamePayer(out.PayerVendorID, payerID) || hash != out.SnapshotHash || e != nil || se != nil || !d.Equal(sd) {
		return nil, ErrVendorSettlementConflict
	}
	out.Payments = []VendorSettlementPayment{}
	out.Allocations = []VendorUsageEntry{}
	return &out, nil
}
func vendorSettlementCredit(ctx context.Context, q vendorSQL, id int64, includeDraft bool) (decimal.Decimal, error) {
	states := "('pending_payment','partially_paid','paid')"
	if includeDraft {
		states = "('draft','pending_payment','partially_paid','paid')"
	}
	var val string
	err := q.QueryRowContext(ctx, "SELECT COALESCE(SUM(total),0)::text FROM vendor_settlements WHERE parent_statement_id=$1 AND kind='credit' AND status IN "+states, id).Scan(&val)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(val)
}
func vendorSettlementBalance(ctx context.Context, q vendorSQL, st *VendorSettlement) error {
	credit, err := vendorSettlementCredit(ctx, q, st.ID, false)
	if err != nil {
		return err
	}
	return applyVendorSettlementBalance(st, credit)
}
func applyVendorSettlementBalance(st *VendorSettlement, credit decimal.Decimal) error {
	total, e := vendorDecimal(st.Total, 2, false)
	if e != nil {
		return e
	}
	paid, e := vendorDecimal(st.Paid, 2, false)
	if e != nil {
		return e
	}
	outstanding := total.Sub(paid).Sub(credit)
	if st.Kind == "credit" {
		outstanding = decimal.Zero
	}
	if outstanding.IsNegative() {
		return ErrVendorSettlementConflict
	}
	st.Credited = credit.StringFixed(2)
	st.Unpaid = outstanding.StringFixed(2)
	return nil
}
func vendorSettlementState(total, paid, credit decimal.Decimal) string {
	if total.Sub(credit).Equal(paid) {
		return "paid"
	}
	if paid.IsPositive() {
		return "partially_paid"
	}
	return "pending_payment"
}

func (s *VendorService) CreateVendorSettlements(ctx context.Context, drafts []VendorSettlementDraftInput) ([]VendorSettlementDraftResult, error) {
	if _, _, err := s.vendorSettlementPayer(ctx, s.db, false); err != nil {
		return nil, err
	}
	if len(drafts) == 0 || len(drafts) > 50 {
		return nil, ErrVendorUsageInput
	}
	out := make([]VendorSettlementDraftResult, 0, len(drafts))
	for _, in := range drafts {
		p, err := s.vendorPreviewParse(in.PreviewToken)
		if err != nil {
			out = append(out, VendorSettlementDraftResult{Error: vendorSettlementPublicError(err)})
			continue
		}
		st, err := s.createVendorSettlement(ctx, in, p)
		if err != nil {
			out = append(out, VendorSettlementDraftResult{VendorID: p.VendorID, Error: vendorSettlementPublicError(err)})
			continue
		}
		out = append(out, VendorSettlementDraftResult{VendorID: p.VendorID, Statement: st})
	}
	return out, nil
}
func (s *VendorService) createVendorSettlement(ctx context.Context, in VendorSettlementDraftInput, p vendorSettlementToken) (*VendorSettlement, error) {
	if !vendorKeyPattern.MatchString(in.IdempotencyKey) {
		return nil, ErrVendorUsageInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = vendorSettlementLockEdge(ctx, tx, p.VendorID, p.Input.PayerVendorID); err != nil {
		return nil, vendorSettlementDBError(err)
	}
	actor, payer, err := s.vendorSettlementPayer(ctx, tx, true)
	if err != nil {
		return nil, err
	}
	if !vendorSettlementSamePayer(payer, p.Input.PayerVendorID) {
		return nil, ErrVendorUsageForbidden
	}
	v, err := vendorSettlementPayee(ctx, tx, p.VendorID, payer)
	if err != nil {
		return nil, err
	}
	if actor != p.ActorID {
		return nil, ErrVendorUsageForbidden
	}
	hash, err := vendorHash(p)
	if err != nil {
		return nil, err
	}
	var existing int64
	var previousHash string
	err = tx.QueryRowContext(ctx, "SELECT id,input_hash FROM vendor_settlements WHERE vendor_id=$1 AND idempotency_key=$2 AND payer_vendor_id IS NOT DISTINCT FROM $3::bigint", p.VendorID, in.IdempotencyKey, payer).Scan(&existing, &previousHash)
	if err == nil {
		if hash != previousHash {
			return nil, ErrVendorSettlementConflict
		}
		st, e := loadVendorSettlement(ctx, tx, existing, false)
		if e != nil {
			return nil, e
		}
		if e = vendorSettlementBalance(ctx, tx, st); e != nil {
			return nil, e
		}
		return st, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if time.Now().After(p.ExpiresAt) {
		return nil, ErrVendorSettlementConflict
	}
	if err = normalizeVendorSettlementInput(&p.Input, v); err != nil {
		return nil, err
	}
	f, err := vendorSettlementFilter(p.Input, p.VendorID, time.Now())
	if err != nil {
		return nil, err
	}
	f.SnapshotAt = p.SnapshotAt
	f.Watermark = p.Watermark
	entries, err := vendorUsageEntries(ctx, tx, f, true, true)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, ErrVendorSettlementConflict
	}
	snap, err := buildVendorSettlement(v, p.Input, f, entries)
	if err != nil {
		return nil, err
	}
	snapHash, err := vendorHash(snap)
	if err != nil {
		return nil, err
	}
	if snapHash != p.SnapshotHash || v.PolicyVersion != snap.PolicyVersion {
		return nil, ErrVendorSettlementConflict
	}
	if snap.ParentStatementID != nil {
		parent, e := loadVendorSettlement(ctx, tx, *snap.ParentStatementID, true)
		if e != nil {
			return nil, e
		}
		if parent.VendorID != snap.VendorID || !vendorSettlementSamePayer(parent.PayerVendorID, snap.PayerVendorID) || !vendorSettlementVisible(parent.Status) || parent.Kind == "credit" {
			return nil, ErrVendorSettlementConflict
		}
	}
	st, err := insertVendorSettlement(ctx, tx, snap, entries, in.IdempotencyKey, hash, actor)
	if err != nil {
		return nil, vendorSettlementDBError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, vendorSettlementDBError(err)
	}
	s.VendorAudit(ctx, st.VendorID, "settlement.create", st.ID, "success")
	return st, nil
}
func insertVendorSettlement(ctx context.Context, tx *sql.Tx, snap VendorSettlementSnapshot, entries []VendorUsageEntry, key, inputHash string, actor int64) (*VendorSettlement, error) {
	raw, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}
	hash, err := vendorHash(snap)
	if err != nil {
		return nil, err
	}
	// Sequence-generated statement numbers are global and cannot collide under concurrency.
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT nextval(pg_get_serial_sequence('vendor_settlements','id'))").Scan(&id); err != nil {
		return nil, err
	}
	number := fmt.Sprintf("VS-%s-%d", time.Now().UTC().Format("20060102"), id)
	var created time.Time
	err = tx.QueryRowContext(ctx, "INSERT INTO vendor_settlements (id,number,vendor_id,kind,parent_statement_id,start_at,end_at,timezone,snapshot_at,watermark,contract_version,policy_version,snapshot,snapshot_hash,total,idempotency_key,input_hash,created_by,payer_vendor_id) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING created_at", id, number, snap.VendorID, snap.Kind, snap.ParentStatementID, snap.StartAt, snap.EndAt, snap.Timezone, snap.SnapshotAt, snap.Watermark, snap.ContractVersion, snap.PolicyVersion, string(raw), hash, snap.Total, key, inputHash, actor, snap.PayerVendorID).Scan(&created)
	if err != nil {
		return nil, err
	}
	lineIDs := map[string]int64{}
	for _, l := range snap.Lines {
		var lid int64
		err = tx.QueryRowContext(ctx, "INSERT INTO vendor_settlement_lines (statement_id,group_id,group_name,contract_version,coefficient,cost_usd,amount,usage_count,kind,reason) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id", id, l.GroupID, l.GroupName, l.ContractVersion, l.Coefficient, l.CostUSD, l.Amount, l.UsageCount, l.Kind, l.Reason).Scan(&lid)
		if err != nil {
			return nil, err
		}
		lineIDs[vendorSettlementLineKey(l.GroupID, l.ContractVersion)] = lid
	}
	// Batches keep a 10k-row draft bounded in query count and memory.
	for offset := 0; offset < len(entries); offset += 200 {
		end := offset + 200
		if end > len(entries) {
			end = len(entries)
		}
		values := ""
		args := make([]any, 0, (end-offset)*7)
		for _, entry := range entries[offset:end] {
			lid, ok := lineIDs[vendorSettlementLineKey(entry.GroupID, entry.ContractVersion)]
			if !ok {
				return nil, ErrVendorSettlementConflict
			}
			b, e := json.Marshal(entry)
			if e != nil {
				return nil, e
			}
			n := len(args)
			if values != "" {
				values += ","
			}
			values += fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d)", n+1, n+2, n+3, n+4, n+5, n+6, n+7)
			args = append(args, id, lid, entry.UsageLogID, snap.VendorID, string(b), entry.sourceFingerprint, snap.PayerVendorID)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO vendor_settlement_allocations (statement_id,line_id,usage_log_id,vendor_id,snapshot,source_fingerprint,payer_vendor_id) VALUES "+values, args...); err != nil {
			return nil, err
		}
	}
	return &VendorSettlement{VendorSettlementSnapshot: snap, ID: id, Number: number, Status: "draft", Version: 1, Paid: "0.00", Credited: "0.00", Unpaid: snap.Total, SnapshotHash: hash, CreatedAt: created, CreatedBy: actor, Payments: []VendorSettlementPayment{}, Allocations: []VendorUsageEntry{}, AllocationTotal: int64(len(entries))}, nil
}

// Mutations authorize first, then lock vendor -> actor -> parent -> statement.
// The vendor lock serializes overlapping periods, credit documents and payments.
func (s *VendorService) vendorSettlementMutation(ctx context.Context, id int64) (*sql.Tx, *VendorSettlement, int64, error) {
	_, payer, err := s.vendorSettlementPayer(ctx, s.db, false)
	if err != nil {
		return nil, nil, 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	fail := func(e error) (*sql.Tx, *VendorSettlement, int64, error) { tx.Rollback(); return nil, nil, 0, e }
	var vendorID int64
	var parentID, storedPayer *int64
	if err = tx.QueryRowContext(ctx, "SELECT vendor_id,parent_statement_id,payer_vendor_id FROM vendor_settlements WHERE id=$1", id).Scan(&vendorID, &parentID, &storedPayer); err != nil {
		if err == sql.ErrNoRows {
			return fail(ErrVendorSettlementNotFound)
		}
		return fail(err)
	}
	if !vendorSettlementSamePayer(payer, storedPayer) {
		return fail(ErrVendorSettlementNotFound)
	}
	if err = vendorSettlementLockEdge(ctx, tx, vendorID, payer); err != nil {
		return fail(vendorSettlementDBError(err))
	}
	actor, livePayer, err := s.vendorSettlementPayer(ctx, tx, true)
	if err != nil {
		return fail(err)
	}
	if !vendorSettlementSamePayer(livePayer, payer) {
		return fail(ErrVendorUsageForbidden)
	}
	if _, err = vendorSettlementPayee(ctx, tx, vendorID, payer); err != nil {
		return fail(err)
	}
	if parentID != nil {
		if _, err = loadVendorSettlement(ctx, tx, *parentID, true); err != nil {
			return fail(err)
		}
	}
	st, err := loadVendorSettlement(ctx, tx, id, true)
	if err != nil {
		return fail(err)
	}
	return tx, st, actor, nil
}
func vendorSettlementVisible(status string) bool {
	return status == "pending_payment" || status == "partially_paid" || status == "paid"
}
func vendorSettlementAllocations(ctx context.Context, q vendorSQL, id int64, limit, offset int) ([]VendorUsageEntry, error) {
	rows, err := q.QueryContext(ctx, "SELECT snapshot FROM vendor_settlement_allocations WHERE statement_id=$1 ORDER BY usage_log_id LIMIT $2 OFFSET $3", id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VendorUsageEntry, 0)
	for rows.Next() {
		var raw []byte
		var e VendorUsageEntry
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func vendorSettlementPaymentRows(ctx context.Context, q vendorSQL, id int64, paging ...int) ([]VendorSettlementPayment, error) {
	limit, offset := 100, 0
	if len(paging) == 2 {
		limit, offset = paging[0], paging[1]
	}
	rows, err := q.QueryContext(ctx, "SELECT id,amount::text,currency,paid_at,channel,reference,reversal_of_id,reason,created_at FROM vendor_settlement_payments WHERE statement_id=$1 ORDER BY id LIMIT $2 OFFSET $3", id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []VendorSettlementPayment{}
	for rows.Next() {
		var p VendorSettlementPayment
		if err = rows.Scan(&p.ID, &p.Amount, &p.Currency, &p.PaidAt, &p.Channel, &p.Reference, &p.ReversalOfID, &p.Reason, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
