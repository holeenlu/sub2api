package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *usageBillingRepository) CloseNativeUsageIntent(ctx context.Context, input *service.NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error) {
	return (billing.Ledger{Journal: &usageBillingLedger{repo: r}}).CloseNativeIntent(ctx, input)
}

func (a *usageBillingLedger) CloseNativeIntent(ctx context.Context, command billing.NativeIntentClosureCommand) (billing.NativeIntentClosureReceipt, error) {
	var empty billing.NativeIntentClosureReceipt
	input, ok := command.(*service.NativeIntentNotSentInput)
	if !ok || input == nil || a.repo == nil || a.repo.db == nil {
		return empty, billing.ErrNativeIntentUnavailable
	}
	ref, err := input.NativeIntentClosureReference()
	if err != nil {
		return empty, err
	}
	tx, err := a.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback() }()
	var raw, evidence, priorClosure []byte
	var fingerprint, status string
	var canonical, priorFingerprint sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT native_intent,native_intent_fingerprint,status,native_evidence,canonical_request_id,native_intent_closure,native_intent_closure_fingerprint FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2 FOR UPDATE", ref.Key.OperationID, ref.Key.APIKeyID).Scan(&raw, &fingerprint, &status, &evidence, &canonical, &priorClosure, &priorFingerprint)
	if err != nil {
		return empty, err
	}
	if (status != "intent_preserved" && status != "intent_not_sent") || len(evidence) != 0 || canonical.Valid || fingerprint != ref.IntentFingerprint {
		return empty, billing.ErrNativeIntentConflict
	}
	stored, err := service.DecodeNativeUsageIntentRecord(raw, ref.Key, fingerprint)
	if err != nil {
		return empty, err
	}
	closure, err := service.ValidateNativeIntentNotSent(stored, *input)
	if err != nil {
		return empty, err
	}
	if status == "intent_not_sent" && (!priorFingerprint.Valid || priorFingerprint.String != ref.ClosureFingerprint || len(priorClosure) == 0) {
		return empty, billing.ErrNativeIntentConflict
	}
	raw, err = json.Marshal(closure)
	if err != nil {
		return empty, err
	}
	// A replay does not renew retention. The row lock excludes a concurrent
	// scope append, canonical binding or evidence promotion before closure.
	result, err := tx.ExecContext(ctx, "UPDATE usage_billing_events SET status='intent_not_sent',native_intent_closure=$3,native_intent_closure_fingerprint=$4,last_error=NULL,updated_at=CASE WHEN status='intent_not_sent' THEN updated_at ELSE NOW() END WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", ref.Key.OperationID, ref.Key.APIKeyID, string(raw), ref.ClosureFingerprint)
	if err != nil {
		return empty, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return empty, err
	}
	if count != 1 {
		return empty, billing.ErrNativeIntentConflict
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return billing.NativeIntentClosureReceipt{NativeIntentClosureReference: ref, ClosedNotSent: true}, nil
}

var _ billing.NativeIntentClosureJournal = (*usageBillingLedger)(nil)
