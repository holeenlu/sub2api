package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *usageBillingRepository) StoreNativeUsageIntent(ctx context.Context, input *service.NativeUsageIntent) (billing.NativeIntentReceipt, error) {
	if input == nil {
		return billing.NativeIntentReceipt{}, billing.ErrNativeIntentInvalid
	}
	prepared, err := service.PrepareNativeUsageIntent(*input)
	if err != nil {
		return billing.NativeIntentReceipt{}, err
	}
	adapter := &usageBillingLedger{repo: r}
	ledger := billing.Ledger{Journal: adapter}
	return ledger.PreserveNativeIntent(ctx, prepared)
}

func (a *usageBillingLedger) PreserveNativeIntent(ctx context.Context, command billing.NativeIntentCommand) (billing.NativeIntentReceipt, error) {
	var empty billing.NativeIntentReceipt
	input, ok := command.(*service.NativeUsageIntent)
	if !ok || input == nil || a.repo == nil || a.repo.db == nil {
		return empty, billing.ErrNativeIntentUnavailable
	}
	prepared, err := service.PrepareNativeUsageIntent(*input)
	if err != nil {
		return empty, err
	}
	reference, err := prepared.NativeIntentReference()
	if err != nil {
		return empty, err
	}
	initial, err := service.MergeNativeUsageIntent(nil, *prepared)
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		return empty, err
	}
	tx, err := a.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, "INSERT INTO usage_billing_events(event_type,operation_id,api_key_id,request_id,request_fingerprint,payload,status,native_intent,native_intent_fingerprint) VALUES('native_intent',$1,$2,NULL,'',NULL,'intent_preserved',$3,$4) ON CONFLICT(operation_id,api_key_id) DO NOTHING", reference.Key.OperationID, reference.Key.APIKeyID, string(raw), reference.IntentFingerprint)
	if err != nil {
		return empty, err
	}
	var financialFingerprint, status string
	var canonicalMoneyID sql.NullString
	var payload, saved []byte
	var fingerprint sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT request_id,request_fingerprint,payload,status,native_intent,native_intent_fingerprint FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2 FOR UPDATE", reference.Key.OperationID, reference.Key.APIKeyID).Scan(&canonicalMoneyID, &financialFingerprint, &payload, &status, &saved, &fingerprint)
	if err != nil {
		return empty, err
	}
	if canonicalMoneyID.Valid || financialFingerprint != "" || len(payload) != 0 || len(saved) == 0 || !fingerprint.Valid {
		return empty, billing.ErrNativeIntentConflict
	}
	if status == "intent_not_sent" {
		return empty, billing.ErrNativeIntentClosed
	}
	previous, err := service.DecodeNativeUsageIntentRecord(saved, reference.Key, fingerprint.String)
	if err != nil {
		return empty, err
	}
	merged, err := service.MergeNativeUsageIntent(previous, *prepared)
	if err != nil {
		return empty, err
	}
	// A finalized technical event may confirm the same scope again, but cannot
	// acquire another attempt. No canonical money field is written here.
	if (status != "intent_preserved" && status!="pending_usage") && len(merged.Attempts) != len(previous.Attempts) {
		return empty, billing.ErrNativeIntentConflict
	}
	if status=="pending_usage" && len(merged.Attempts)!=len(previous.Attempts) {
		row,err:=loadNativeIntentRow(ctx,tx,reference.Key,false)
		if err!=nil { return empty,err }
		if row.finalization!=nil && row.finalization.Evidence.Evidence.Confidence==billing.NativeUsageFinal &&
			row.finalization.Evidence.Evidence.TerminalConfirmed { return empty,billing.ErrNativeIntentConflict }
	}
	raw, err = json.Marshal(merged)
	if err != nil {
		return empty, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE usage_billing_events SET updated_at=CASE WHEN native_intent IS DISTINCT FROM $3::jsonb THEN NOW() ELSE updated_at END,native_intent=$3,native_intent_fingerprint=$4 WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", reference.Key.OperationID, reference.Key.APIKeyID, string(raw), reference.IntentFingerprint)
	if err != nil {
		return empty, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return empty, err
	}
	if affected != 1 {
		return empty, billing.ErrNativeIntentConflict
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return billing.NativeIntentReceipt{NativeIntentReference: reference, Disposition: billing.NativeIntentPreserved}, nil
}

func (r *usageBillingRepository) LoadNativeUsageIntent(ctx context.Context, id billing.NativeIntentKey) (*service.NativeUsageIntentRecord, error) {
	if ctx == nil || r == nil || r.db == nil || id.OperationID == "" || id.APIKeyID <= 0 {
		return nil, billing.ErrNativeIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var raw []byte
	var fingerprint sql.NullString
	err := r.db.QueryRowContext(ctx, "SELECT native_intent,native_intent_fingerprint FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", id.OperationID, id.APIKeyID).Scan(&raw, &fingerprint)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || !fingerprint.Valid {
		return nil, sql.ErrNoRows
	}
	return service.DecodeNativeUsageIntentRecord(raw, id, fingerprint.String)
}

var _ service.NativeUsageIntentRepository = (*usageBillingRepository)(nil)
var _ billing.NativeIntentJournal = (*usageBillingLedger)(nil)
