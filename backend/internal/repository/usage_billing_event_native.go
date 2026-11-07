package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *usageBillingRepository) RecordNativeUsageEvidence(ctx context.Context, input *service.NativeUsageEvidenceInput) (service.NativeUsageRecordResult, error) {
	if input == nil {
		return service.NativeUsageRecordResult{}, billing.ErrNativeUsageEvidence
	}
	if input.Intent != nil || input.Evidence.Version == 2 {
		return service.NativeUsageRecordResult{}, billing.ErrNativeIntentInvalid
	}
	prepared, err := service.PrepareNativeUsageEvidence(*input)
	if err != nil {
		return service.NativeUsageRecordResult{}, err
	}
	a := &usageBillingLedger{repo: r}
	l := billing.Ledger{Journal: a, Transaction: a}
	if r != nil && r.invalidate != nil {
		l.Invalidator = a
	}
	receipt, err := l.RecordNativeUsage(ctx, prepared)
	return service.NativeUsageRecordResult{Receipt: receipt, ActualApplyResult: a.result}, err
}

func (a *usageBillingLedger) PreserveNativeUsage(ctx context.Context, input billing.NativeEvidenceCommand) (_ billing.NativeUsageReceipt, _ billing.Command, err error) {
	var empty billing.NativeUsageReceipt
	concrete, ok := input.(*service.NativeUsageEvidenceInput)
	if !ok || concrete == nil || a.repo == nil || a.repo.db == nil {
		return empty, nil, billing.ErrNativeUsageEvidence
	}
	if concrete.Intent != nil || concrete.Evidence.Version == 2 {
		return empty, nil, billing.ErrNativeIntentInvalid
	}
	prepared, err := service.PrepareNativeUsageEvidence(*concrete)
	if err != nil {
		return empty, nil, err
	}
	tx, err := a.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	receipt, settlement, err := a.preserveNativeUsageTx(ctx, tx, prepared, nil)
	if err != nil {
		return empty, nil, err
	}
	if err = tx.Commit(); err != nil {
		return empty, nil, err
	}
	return receipt, settlement, nil
}

// preserveNativeUsageTx is the single canonical evidence/V1 persistence body.
// Native technical binding supplies its already-open transaction; money is
// still committed only by the original ledger transaction after this commit.
func (a *usageBillingLedger) preserveNativeUsageTx(ctx context.Context, tx *sql.Tx, prepared *service.NativeUsageEvidenceInput, binding *nativeCanonicalBinding) (billing.NativeUsageReceipt, billing.Command, error) {
	var empty billing.NativeUsageReceipt
	if prepared == nil || prepared.Evidence.Identity.RequestID == "" ||
		(prepared.Intent == nil) != (binding == nil) ||
		(prepared.Intent != nil && (prepared.Evidence.Version != 2 || binding.key != prepared.Intent.Key)) ||
		(prepared.Intent == nil && prepared.Evidence.Version > 1) {
		return empty, nil, billing.ErrNativeIntentInvalid
	}
	evidence := prepared.Evidence
	id := evidence.Identity
	initial, err := service.MergeNativeUsageRecord(nil, prepared)
	if err != nil {
		return empty, nil, err
	}
	raw, err := json.Marshal(initial)
	if err != nil {
		return empty, nil, err
	}
	status := "pending_usage"
	if evidence.Confidence == billing.NativeUsageFinal {
		status = "usage_recorded"
	}
	// First reserve only evidence. Binding a V1 settlement fingerprint happens
	// under this same row lock, before any unique ledger transaction can run.
	_, err = tx.ExecContext(ctx, `INSERT INTO usage_billing_events(request_id,api_key_id,request_fingerprint,payload,status,native_evidence,native_evidence_fingerprint)
 VALUES($1,$2,'',NULL,$3,$4,$5) ON CONFLICT(request_id,api_key_id) DO NOTHING`, id.RequestID, id.APIKeyID, status, string(raw), evidence.Fingerprint())
	if err != nil {
		return empty, nil, err
	}
	var existingFingerprint string
	var payload, saved []byte
	var evidenceFingerprint sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT request_fingerprint,payload,status,native_evidence,native_evidence_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2 FOR UPDATE`, id.RequestID, id.APIKeyID).Scan(&existingFingerprint, &payload, &status, &saved, &evidenceFingerprint)
	if err != nil {
		return empty, nil, err
	}
	if binding != nil {
		var other bool
		err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM usage_billing_events WHERE event_type='native_intent' AND canonical_request_id=$1 AND api_key_id=$2 AND operation_id<>$3)", id.RequestID, id.APIKeyID, binding.key.OperationID).Scan(&other)
		if err != nil {
			return empty, nil, err
		}
		if other || (len(saved) == 0 && existingFingerprint != "" && !binding.allowLegacy) {
			return empty, nil, billing.ErrNativeIntentConflict
		}
	}
	var record *service.NativeUsageEvidenceRecord
	if len(saved) == 0 {
		// A V1 event already won. Unknown evidence cannot overwrite or claim
		// that charge; only the exact existing final fingerprint may attach it.
		if prepared.Settlement == nil || prepared.Settlement.RequestFingerprint != existingFingerprint {
			return empty, nil, service.ErrUsageBillingRequestConflict
		}
		record = initial
	} else {
		record, err = decodeNativeUsageRecord(saved, id, evidenceFingerprint.String)
		if err != nil {
			return empty, nil, err
		}
		record, err = service.MergeNativeUsageRecord(record, prepared)
		if err != nil {
			return empty, nil, err
		}
	}
	if prepared.Settlement != nil {
		mergedInput := *prepared
		mergedInput.Evidence = record.Evidence
		mergedInput.Observations = nil
		if _, err = service.PrepareNativeUsageEvidence(mergedInput); err != nil {
			return empty, nil, err
		}
	}
	if command := prepared.Settlement; command != nil {
		if existingFingerprint != "" && existingFingerprint != command.RequestFingerprint {
			return empty, nil, service.ErrUsageBillingRequestConflict
		}
		if existingFingerprint == "" {
			if status != "pending_usage" && status != "usage_recorded" {
				return empty, nil, billing.ErrNativeUsageConflict
			}
			payload, err = usageBillingEventPayload(command)
			if err != nil {
				return empty, nil, err
			}
			existingFingerprint = command.RequestFingerprint
			status = "pending"
			if command.NeedsPricing() {
				status = "pending_pricing"
			}
		}
	} else if status == "pending_usage" && record.Evidence.Confidence == billing.NativeUsageFinal {
		status = "usage_recorded"
	}
	raw, err = json.Marshal(record)
	if err != nil {
		return empty, nil, err
	}
	if len(raw) > 1<<20 {
		return empty, nil, errors.New("native usage evidence exceeds size limit")
	}
	var payloadArg any
	if len(payload) > 0 {
		payloadArg = string(payload)
	}
	_, err = tx.ExecContext(ctx, `UPDATE usage_billing_events SET
 updated_at=CASE WHEN native_evidence IS DISTINCT FROM $3::jsonb OR status<>$5 THEN NOW() ELSE updated_at END,
 native_evidence=$3,native_evidence_fingerprint=$4,status=$5,request_fingerprint=$6,payload=$7
 WHERE request_id=$1 AND api_key_id=$2`, id.RequestID, id.APIKeyID, string(raw), record.Evidence.Fingerprint(), status, existingFingerprint, payloadArg)
	if err != nil {
		return empty, nil, err
	}
	receipt, err := nativeUsageReceipt(record, status, len(payload) > 0, existingFingerprint)
	if err != nil {
		return empty, nil, err
	}
	var settlement billing.Command
	if len(payload) > 0 {
		settlement, err = a.Decode(billing.Event{Identity: id, Payload: payload})
		if err != nil {
			return empty, nil, err
		}
	}
	return receipt, settlement, nil
}

func decodeNativeUsageRecord(raw []byte, id billing.Identity, fingerprint string) (*service.NativeUsageEvidenceRecord, error) {
	var record service.NativeUsageEvidenceRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, billing.ErrNativeUsageEvidence
	}
	if (record.Version != 1 && record.Version != 2) || (record.Version == 1 && record.Intent != nil) || (record.Version == 2 && record.Intent == nil) ||
		record.Evidence.Identity != id || fingerprint == "" || record.Evidence.Fingerprint() != fingerprint || len(record.Observations) == 0 || len(record.Observations) > 64 {
		return nil, billing.ErrNativeUsageConflict
	}
	validated, err := service.PrepareNativeUsageEvidence(service.NativeUsageEvidenceInput{Protocol: record.Protocol, Intent: record.Intent, Evidence: record.Evidence, Observations: record.Observations, FrozenPricing: record.FrozenPricing})
	if err != nil {
		return nil, err
	}
	record.Evidence = validated.Evidence
	for _, e := range record.Observations {
		if e.Validate() != nil || e.Fingerprint() != fingerprint {
			return nil, billing.ErrNativeUsageConflict
		}
	}
	return &record, nil
}

func nativeUsageReceipt(record *service.NativeUsageEvidenceRecord, status string, hasPayload bool, settlementFingerprint string) (billing.NativeUsageReceipt, error) {
	r := billing.NativeUsageReceipt{Identity: record.Evidence.Identity, EvidenceFingerprint: record.Evidence.Fingerprint(), RecoveryPending: status == "pending" || status == "pending_pricing" || status == "failed" || status == "settled"}
	switch status {
	case "pending_usage":
		if record.Evidence.Confidence == billing.NativeUsageFinal || hasPayload || settlementFingerprint != "" {
			return billing.NativeUsageReceipt{}, billing.ErrNativeUsageConflict
		}
		r.Disposition = billing.DurablePendingUsage
	case "usage_recorded":
		if record.Evidence.Confidence != billing.NativeUsageFinal || hasPayload || settlementFingerprint != "" {
			return billing.NativeUsageReceipt{}, billing.ErrNativeUsageConflict
		}
		r.Disposition = billing.UsageRecorded
	case "pending", "failed", "pending_pricing":
		if !hasPayload || settlementFingerprint == "" || record.Evidence.Confidence != billing.NativeUsageFinal {
			return billing.NativeUsageReceipt{}, billing.ErrNativeUsageConflict
		}
		r.Disposition = billing.SettlementPending
		if status == "pending_pricing" {
			r.Disposition = billing.NativePendingPricing
		}
	case "settled", "applied":
		if settlementFingerprint == "" || record.Evidence.Confidence != billing.NativeUsageFinal {
			return billing.NativeUsageReceipt{}, billing.ErrNativeUsageConflict
		}
		r.Disposition = billing.NativeSettled
	default:
		return billing.NativeUsageReceipt{}, billing.ErrNativeUsageEvidence
	}
	return r, nil
}

func (r *usageBillingRepository) LoadNativeUsageEvidence(ctx context.Context, id billing.Identity) (*service.NativeUsageEvidenceRecord, error) {
	if r == nil || r.db == nil || id.RequestID == "" || id.APIKeyID <= 0 {
		return nil, billing.ErrNativeUsageEvidence
	}
	var raw, payload []byte
	var status, fingerprint string
	var evidenceFingerprint sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT native_evidence,native_evidence_fingerprint,status,payload,request_fingerprint FROM usage_billing_events WHERE request_id=$1 AND api_key_id=$2`, id.RequestID, id.APIKeyID).Scan(&raw, &evidenceFingerprint, &status, &payload, &fingerprint)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, sql.ErrNoRows
	}
	record, err := decodeNativeUsageRecord(raw, id, evidenceFingerprint.String)
	if err != nil {
		return nil, err
	}
	record.Receipt, err = nativeUsageReceipt(record, status, len(payload) > 0, fingerprint)
	return record, err
}

func (a *usageBillingLedger) ReadNativeReceipt(ctx context.Context, id billing.Identity) (billing.NativeUsageReceipt, error) {
	record, err := a.repo.LoadNativeUsageEvidence(ctx, id)
	if err != nil {
		return billing.NativeUsageReceipt{}, err
	}
	return record.Receipt, nil
}

var _ service.NativeUsageEvidenceRepository = (*usageBillingRepository)(nil)
var _ billing.NativeEvidenceJournal = (*usageBillingLedger)(nil)
