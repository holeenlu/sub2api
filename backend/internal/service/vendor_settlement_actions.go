package service

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"
)

type VendorSettlementVersionInput struct {
	Version int64  `json:"version"`
	Reason  string `json:"reason,omitempty"`
}
type VendorSettlementPaymentInput struct {
	Version        int64     `json:"version"`
	Amount         string    `json:"amount"`
	PaidAt         time.Time `json:"paid_at"`
	Channel        string    `json:"channel"`
	Reference      string    `json:"reference"`
	IdempotencyKey string    `json:"idempotency_key"`
}
type VendorSettlementAdjustmentInput struct {
	Version        int64  `json:"version"`
	Amount         string `json:"amount"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *VendorService) finishVendorSettlement(ctx context.Context, tx *sql.Tx, st *VendorSettlement, action string) (*VendorSettlement, error) {
	updated, err := loadVendorSettlement(ctx, tx, st.ID, false)
	if err != nil {
		return nil, err
	}
	if err = vendorSettlementBalance(ctx, tx, updated); err != nil {
		return nil, err
	}
	updated.Payments, err = vendorSettlementPaymentRows(ctx, tx, st.ID)
	if err != nil {
		return nil, err
	}
	updated.AllocationTotal = updated.UsageCount
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendor_settlement_payments WHERE statement_id=$1", st.ID).Scan(&updated.PaymentTotal); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, vendorSettlementDBError(err)
	}
	s.VendorAudit(ctx, updated.VendorID, action, updated.ID, "success")
	return updated, nil
}
func (s *VendorService) ConfirmVendorSettlement(ctx context.Context, id int64, in VendorSettlementVersionInput) (*VendorSettlement, error) {
	if in.Version <= 0 {
		return nil, ErrVendorUsageInput
	}
	tx, st, actor, err := s.vendorSettlementMutation(ctx, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if vendorSettlementVisible(st.Status) && in.Version == 1 {
		return s.finishVendorSettlement(ctx, tx, st, "settlement.confirm.replay")
	}
	if st.Status != "draft" || st.Version != in.Version || st.EndAt.After(time.Now()) {
		return nil, ErrVendorSettlementConflict
	}
	entries, err := vendorSettlementAllocations(ctx, tx, id, VendorUsageExportLimit+1, 0)
	if err != nil {
		return nil, err
	}
	hash, err := vendorHash(entries)
	if err != nil {
		return nil, err
	}
	var active int64
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendor_settlement_allocations WHERE statement_id=$1 AND active AND vendor_id=$2 AND payer_vendor_id IS NOT DISTINCT FROM $3::bigint", id, st.VendorID, st.PayerVendorID).Scan(&active); err != nil {
		return nil, err
	}
	if int64(len(entries)) != st.UsageCount || active != st.UsageCount || hash != st.DataHash {
		return nil, ErrVendorSettlementConflict
	}
	for _, e := range entries {
		if !vendorSettlementEntryBelongs(e, st.VendorID, st.PayerVendorID) || e.CreatedAt.Before(st.StartAt) || !e.CreatedAt.Before(st.EndAt) {
			return nil, ErrVendorSettlementConflict
		}
	}
	state := "pending_payment"
	total, _ := vendorDecimal(st.Total, 2, false)
	if total.IsZero() {
		state = "paid"
	}
	if st.ParentStatementID != nil {
		parent, e := loadVendorSettlement(ctx, tx, *st.ParentStatementID, true)
		if e != nil {
			return nil, e
		}
		if parent.VendorID != st.VendorID || !vendorSettlementSamePayer(parent.PayerVendorID, st.PayerVendorID) || !vendorSettlementVisible(parent.Status) || parent.Kind == "credit" {
			return nil, ErrVendorSettlementConflict
		}
		if st.Kind == "credit" {
			credits, e := vendorSettlementCredit(ctx, tx, parent.ID, true)
			if e != nil {
				return nil, e
			}
			pt, _ := vendorDecimal(parent.Total, 2, false)
			pp, _ := vendorDecimal(parent.Paid, 2, false)
			if credits.GreaterThan(pt.Sub(pp)) {
				return nil, ErrVendorSettlementConflict
			}
			state = "paid"
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE vendor_settlements SET status=$1,version=version+1,confirmed_by=$2,confirmed_at=NOW(),updated_at=NOW() WHERE id=$3 AND version=$4", state, actor, id, in.Version)
	if err != nil {
		return nil, err
	}
	if st.Kind == "credit" {
		if err = refreshVendorParentState(ctx, tx, *st.ParentStatementID); err != nil {
			return nil, err
		}
	}
	return s.finishVendorSettlement(ctx, tx, st, "settlement.confirm")
}
func refreshVendorParentState(ctx context.Context, tx *sql.Tx, parentID int64) error {
	parent, err := loadVendorSettlement(ctx, tx, parentID, true)
	if err != nil {
		return err
	}
	credits, err := vendorSettlementCredit(ctx, tx, parentID, false)
	if err != nil {
		return err
	}
	total, _ := vendorDecimal(parent.Total, 2, false)
	paid, _ := vendorDecimal(parent.Paid, 2, false)
	if total.Sub(credits).LessThan(paid) {
		return ErrVendorSettlementConflict
	}
	_, err = tx.ExecContext(ctx, "UPDATE vendor_settlements SET status=$1,version=version+1,updated_at=NOW() WHERE id=$2", vendorSettlementState(total, paid, credits), parentID)
	return err
}
func (s *VendorService) VoidVendorSettlement(ctx context.Context, id int64, in VendorSettlementVersionInput) (*VendorSettlement, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Version <= 0 || in.Reason == "" || len(in.Reason) > 2000 {
		return nil, ErrVendorUsageInput
	}
	tx, st, _, err := s.vendorSettlementMutation(ctx, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if st.Status == "void" && st.VoidReason == in.Reason && st.Version == in.Version+1 {
		return s.finishVendorSettlement(ctx, tx, st, "settlement.void.replay")
	}
	paid, _ := vendorDecimal(st.Paid, 2, false)
	if st.Version != in.Version || st.Status == "void" || !paid.IsZero() {
		return nil, ErrVendorSettlementConflict
	}
	var children int64
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM vendor_settlements WHERE parent_statement_id=$1 AND status<>'void'", id).Scan(&children); err != nil {
		return nil, err
	}
	if children > 0 {
		return nil, ErrVendorSettlementConflict
	}
	if _, err = tx.ExecContext(ctx, "UPDATE vendor_settlements SET status='void',version=version+1,void_reason=$1,updated_at=NOW() WHERE id=$2 AND version=$3", in.Reason, id, in.Version); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE vendor_settlement_allocations SET active=FALSE WHERE statement_id=$1 AND active", id); err != nil {
		return nil, err
	}
	if st.Kind == "credit" && vendorSettlementVisible(st.Status) {
		if err = refreshVendorParentState(ctx, tx, *st.ParentStatementID); err != nil {
			return nil, err
		}
	}
	return s.finishVendorSettlement(ctx, tx, st, "settlement.void")
}
func normalizeVendorReceipt(v string, max int) (string, error) {
	v = strings.ToLower(strings.Join(strings.Fields(v), ""))
	if len(v) == 0 || len(v) > max {
		return "", ErrVendorUsageInput
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return "", ErrVendorUsageInput
		}
	}
	return v, nil
}
func vendorPaymentReplay(ctx context.Context, tx *sql.Tx, id int64, key, hash string) (bool, error) {
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT input_hash FROM vendor_settlement_payments WHERE statement_id=$1 AND idempotency_key=$2", id, key).Scan(&previous)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if previous != hash {
		return false, ErrVendorSettlementConflict
	}
	return true, nil
}
func (s *VendorService) RegisterVendorPayment(ctx context.Context, id int64, in VendorSettlementPaymentInput) (*VendorSettlement, error) {
	amount, err := vendorDecimal(in.Amount, 2, false)
	if err != nil || !amount.IsPositive() || in.Version <= 0 || !vendorKeyPattern.MatchString(in.IdempotencyKey) || in.PaidAt.IsZero() || in.PaidAt.After(time.Now().Add(time.Minute)) {
		return nil, ErrVendorUsageInput
	}
	in.Channel, err = normalizeVendorReceipt(in.Channel, 64)
	if err != nil {
		return nil, err
	}
	in.Reference, err = normalizeVendorReceipt(in.Reference, 128)
	if err != nil {
		return nil, err
	}
	in.Amount = amount.StringFixed(2)
	version := in.Version
	in.Version = 0
	in.PaidAt = in.PaidAt.UTC()
	hash, err := vendorHash(in)
	if err != nil {
		return nil, err
	}
	tx, st, actor, err := s.vendorSettlementMutation(ctx, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	replay, err := vendorPaymentReplay(ctx, tx, id, in.IdempotencyKey, hash)
	if err != nil {
		return nil, err
	}
	if replay {
		return s.finishVendorSettlement(ctx, tx, st, "settlement.payment.replay")
	}
	if !vendorSettlementVisible(st.Status) || st.Kind == "credit" || version != st.Version {
		return nil, ErrVendorSettlementConflict
	}
	if in.PaidAt.Before(st.StartAt) {
		return nil, ErrVendorUsageInput
	}
	// Draft credits reserve refundable capacity as well as confirmed credits.
	credit, err := vendorSettlementCredit(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	total, _ := vendorDecimal(st.Total, 2, false)
	paid, _ := vendorDecimal(st.Paid, 2, false)
	next := paid.Add(amount)
	if next.GreaterThan(total.Sub(credit)) {
		return nil, ErrVendorSettlementConflict
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO vendor_settlement_payments (statement_id,amount,currency,paid_at,channel,reference,actor_id,idempotency_key,input_hash,payer_vendor_id) VALUES ($1,$2,'USDT',$3,$4,$5,$6,$7,$8,$9)", id, in.Amount, in.PaidAt, in.Channel, in.Reference, actor, in.IdempotencyKey, hash, st.PayerVendorID)
	if err != nil {
		return nil, vendorSettlementDBError(err)
	}
	confirmed, err := vendorSettlementCredit(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE vendor_settlements SET paid=$1,status=$2,version=version+1,updated_at=NOW() WHERE id=$3 AND version=$4", next.StringFixed(2), vendorSettlementState(total, next, confirmed), id, version); err != nil {
		return nil, err
	}
	return s.finishVendorSettlement(ctx, tx, st, "settlement.payment")
}
func (s *VendorService) ReverseVendorPayment(ctx context.Context, id, paymentID int64, in VendorSettlementAdjustmentInput) (*VendorSettlement, error) {
	amount, err := vendorDecimal(in.Amount, 2, false)
	in.Reason = strings.TrimSpace(in.Reason)
	if err != nil || !amount.IsPositive() || in.Version <= 0 || paymentID <= 0 || in.Reason == "" || len(in.Reason) > 2000 || !vendorKeyPattern.MatchString(in.IdempotencyKey) {
		return nil, ErrVendorUsageInput
	}
	version := in.Version
	in.Version = 0
	in.Amount = amount.StringFixed(2)
	hash, err := vendorHash(struct {
		PaymentID int64
		Input     VendorSettlementAdjustmentInput
	}{paymentID, in})
	if err != nil {
		return nil, err
	}
	tx, st, actor, err := s.vendorSettlementMutation(ctx, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	replay, err := vendorPaymentReplay(ctx, tx, id, in.IdempotencyKey, hash)
	if err != nil {
		return nil, err
	}
	if replay {
		return s.finishVendorSettlement(ctx, tx, st, "settlement.reverse.replay")
	}
	if !vendorSettlementVisible(st.Status) || st.Kind == "credit" || version != st.Version {
		return nil, ErrVendorSettlementConflict
	}
	var original, channel, reference string
	err = tx.QueryRowContext(ctx, "SELECT amount::text,channel,reference FROM vendor_settlement_payments WHERE id=$1 AND statement_id=$2 AND reversal_of_id IS NULL FOR UPDATE", paymentID, id).Scan(&original, &channel, &reference)
	if err == sql.ErrNoRows {
		return nil, ErrVendorSettlementNotFound
	}
	if err != nil {
		return nil, err
	}
	var reversed string
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(amount),0)::text FROM vendor_settlement_payments WHERE reversal_of_id=$1", paymentID).Scan(&reversed); err != nil {
		return nil, err
	}
	orig, _ := decimal.NewFromString(original)
	rev, _ := decimal.NewFromString(reversed)
	paid, _ := vendorDecimal(st.Paid, 2, false)
	if amount.GreaterThan(orig.Add(rev)) || amount.GreaterThan(paid) {
		return nil, ErrVendorSettlementConflict
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO vendor_settlement_payments (statement_id,amount,currency,paid_at,channel,reference,actor_id,idempotency_key,input_hash,reversal_of_id,reason,payer_vendor_id) VALUES ($1,$2,'USDT',NOW(),$3,$4,$5,$6,$7,$8,$9,$10)", id, amount.Neg().StringFixed(2), channel, reference, actor, in.IdempotencyKey, hash, paymentID, in.Reason, st.PayerVendorID)
	if err != nil {
		return nil, vendorSettlementDBError(err)
	}
	next := paid.Sub(amount)
	total, _ := vendorDecimal(st.Total, 2, false)
	credits, err := vendorSettlementCredit(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE vendor_settlements SET paid=$1,status=$2,version=version+1,updated_at=NOW() WHERE id=$3 AND version=$4", next.StringFixed(2), vendorSettlementState(total, next, credits), id, version); err != nil {
		return nil, err
	}
	return s.finishVendorSettlement(ctx, tx, st, "settlement.reverse")
}
func (s *VendorService) AdjustVendorSettlement(ctx context.Context, id int64, in VendorSettlementAdjustmentInput) (*VendorSettlement, error) {
	amount, err := vendorDecimal(in.Amount, 2, true)
	in.Reason = strings.TrimSpace(in.Reason)
	if err != nil || amount.IsZero() || in.Version <= 0 || in.Reason == "" || len(in.Reason) > 2000 || !vendorKeyPattern.MatchString(in.IdempotencyKey) {
		return nil, ErrVendorUsageInput
	}
	version := in.Version
	in.Version = 0
	in.Amount = amount.StringFixed(2)
	hash, err := vendorHash(struct {
		ParentID int64
		Input    VendorSettlementAdjustmentInput
	}{id, in})
	if err != nil {
		return nil, err
	}
	tx, parent, actor, err := s.vendorSettlementMutation(ctx, id)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing int64
	var previous string
	err = tx.QueryRowContext(ctx, "SELECT id,input_hash FROM vendor_settlements WHERE vendor_id=$1 AND idempotency_key=$2 AND payer_vendor_id IS NOT DISTINCT FROM $3::bigint", parent.VendorID, in.IdempotencyKey, parent.PayerVendorID).Scan(&existing, &previous)
	if err == nil {
		if hash != previous {
			return nil, ErrVendorSettlementConflict
		}
		st, e := loadVendorSettlement(ctx, tx, existing, false)
		if e != nil {
			return nil, e
		}
		return s.finishVendorSettlement(ctx, tx, st, "settlement.adjustment.replay")
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if !vendorSettlementVisible(parent.Status) || parent.Kind == "credit" || version != parent.Version {
		return nil, ErrVendorSettlementConflict
	}
	if amount.IsNegative() {
		credits, e := vendorSettlementCredit(ctx, tx, id, true)
		if e != nil {
			return nil, e
		}
		total, _ := vendorDecimal(parent.Total, 2, false)
		paid, _ := vendorDecimal(parent.Paid, 2, false)
		if credits.Add(amount.Abs()).GreaterThan(total.Sub(paid)) {
			return nil, ErrVendorSettlementConflict
		}
	}
	snap := parent.VendorSettlementSnapshot
	snap.Kind = "adjustment"
	if amount.IsNegative() {
		snap.Kind = "credit"
	}
	snap.ParentStatementID = &id
	snap.SnapshotAt = time.Now().UTC()
	snap.CostUSD = "0"
	snap.Subtotal = "0.00"
	snap.Adjustment = amount.StringFixed(2)
	snap.AdjustmentReason = in.Reason
	snap.Total = amount.Abs().StringFixed(2)
	snap.UsageCount = 0
	entries := []VendorUsageEntry{}
	snap.DataHash, err = vendorHash(entries)
	if err != nil {
		return nil, err
	}
	snap.Lines = []VendorSettlementLine{{GroupName: "adjustment", ContractVersion: snap.ContractVersion, Coefficient: "0", CostUSD: "0", Amount: amount.StringFixed(2), Kind: snap.Kind, Reason: in.Reason}}
	st, err := insertVendorSettlement(ctx, tx, snap, entries, in.IdempotencyKey, hash, actor)
	if err != nil {
		return nil, vendorSettlementDBError(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE vendor_settlements SET version=version+1,updated_at=NOW() WHERE id=$1", id); err != nil {
		return nil, err
	}
	return s.finishVendorSettlement(ctx, tx, st, "settlement.adjustment")
}
