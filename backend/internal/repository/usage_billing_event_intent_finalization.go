package repository

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// RecordSystemOneUsage uses the existing native intent and money ledger. The
// System One path must not grow a second settlement store or apply money from
// a caller supplied scope that was not preserved before send.
func (r *usageBillingRepository) RecordSystemOneUsage(ctx context.Context, input *service.SystemOneUsagePreparation) (service.SystemOneBillingReceipt, error) {
	var empty service.SystemOneBillingReceipt
	if ctx == nil || input == nil || r == nil || r.db == nil {
		return empty, billing.ErrNativeIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := service.ValidateSystemOneUsagePreparation(*input); err != nil {
		return empty, err
	}
	stored, err := r.LoadNativeUsageIntent(ctx, input.Intent.Key)
	if err != nil {
		return empty, err
	}
	if stored == nil || stored.Key != input.Intent.Key || stored.OriginalWireDigest != input.Scope.OriginalWireDigest {
		return empty, billing.ErrNativeIntentConflict
	}
	scopeDigests := make(map[uint64][32]byte, len(stored.Attempts))
	for _, attempt := range stored.Attempts {
		ref, refErr := (&service.NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: attempt}).NativeIntentReference()
		if refErr != nil {
			return empty, billing.ErrNativeIntentConflict
		}
		digest, decodeErr := hex.DecodeString(ref.ScopeDigest)
		if decodeErr != nil || len(digest) != 32 {
			return empty, billing.ErrNativeIntentConflict
		}
		var scopeDigest [32]byte
		copy(scopeDigest[:], digest)
		scopeDigests[attempt.Sequence] = scopeDigest
	}
	if _, ok := scopeDigests[input.Intent.AttemptSequence]; !ok {
		return empty, billing.ErrNativeIntentConflict
	}
	root := billing.NativeUsageEvidence{Version: 2, Identity: input.Identity, OperationID: input.Operation.OperationID, UserID: input.Operation.UserID, GroupID: input.Operation.GroupID, GroupBound: input.Operation.GroupBound, OriginalWireDigest: input.Operation.OriginalWireDigest}
	observations := make([]billing.NativeUsageEvidence, 0, len(input.Operation.Attempts))
	var selected *billing.NativeUsageEvidence
	for _, attempt := range input.Operation.Attempts {
		observed, evidenceErr := service.NativeUsageEvidenceFromAttempt(root, attempt)
		if evidenceErr != nil {
			return empty, evidenceErr
		}
		if digest, ok := scopeDigests[attempt.Sequence]; ok {
			observed.AttemptScopeDigest = digest
		} else if attempt.Send != model.SendNotSent || attempt.Usage != nil || attempt.RequestID != "" || attempt.ResponseID != "" || attempt.Terminal != model.TerminalUnknown {
			return empty, billing.ErrNativeIntentConflict
		}
		observations = append(observations, observed)
		if attempt.Sequence == input.Intent.AttemptSequence {
			copy := observed
			selected = &copy
		}
	}
	if selected == nil || len(observations) == 0 {
		return empty, billing.ErrNativeIntentConflict
	}
	if input.Settlement != nil {
		if input.Settlement.RequestID != input.Identity.RequestID || input.Settlement.APIKeyID != input.Identity.APIKeyID || input.Settlement.RequestID == "" {
			return empty, billing.ErrNativeIntentConflict
		}
	}
	preparedEvidence, err := service.PrepareNativeUsageEvidence(service.NativeUsageEvidenceInput{
		Protocol: service.NativeIntentProtocolSystemOne,
		Intent:   &input.Intent, Evidence: *selected, Observations: observations,
		FrozenPricing: input.Scope.Attempt.FrozenPricing, SettlementUsage: input.SettlementUsage, Settlement: input.Settlement,
	})
	if err != nil {
		return empty, err
	}
	final := &service.NativeIntentFinalizationInput{Intent: input.Intent, Evidence: *preparedEvidence, SystemOne: input, Reason: billing.NativeBuilderUnavailable}
	if input.Settlement != nil {
		final.Reason = billing.NativePreparationReady
	}
	result, err := r.FinalizeNativeUsageIntent(ctx, final)
	if !result.Intent.Durable() {
		return empty, err
	}
	receipt := service.SystemOneBillingReceipt{Intent: input.Intent, Billing: model.BillingReceipt{
		Disposition: model.BillingPendingUsage, Durable: true, EvidenceID: result.Intent.FactsFingerprint}}
	if result.Intent.Disposition == billing.NativeIntentPendingPreparation {
		receipt.Billing.Disposition = model.BillingRecoveryPending
	}
	if result.Canonical != nil {
		receipt.ActualApplyResult = result.Canonical.ActualApplyResult
		canonical := result.Canonical.Receipt
		if !canonical.Durable() || canonical.Identity != input.Identity {
			return empty, errors.Join(err, billing.ErrNativeIntentConflict)
		}
		receipt.Billing.EvidenceID = canonical.EvidenceFingerprint
		switch canonical.Disposition {
		case billing.NativeSettled:
			receipt.Billing.Disposition = model.BillingSettled
		case billing.NativePendingPricing:
			receipt.Billing.Disposition = model.BillingPendingPricing
		case billing.UsageRecorded:
			receipt.Billing.Disposition = model.BillingUsageRecorded
		case billing.SettlementPending:
			receipt.Billing.Disposition = model.BillingRecoveryPending
		}
	}
	return receipt, err
}

type nativeCanonicalBinding struct {
	key         billing.NativeIntentKey
	allowLegacy bool
}
type nativeIntentPreparationHook struct {
	prepare func(context.Context, *service.NativeIntentRecoveryInput) (*service.NativeIntentFinalizationInput, error)
}
type systemOnePreparationHook struct {
	prepare func(context.Context, *service.SystemOneUsagePreparation) (*service.UsageBillingCommand, *service.UsagePricingRequest, error)
}
type nativeIntentLockedRow struct {
	intent              *service.NativeUsageIntentRecord
	finalization        *service.NativeIntentFinalizationRecord
	status              string
	proposed, canonical sql.NullString
}

func loadNativeIntentRow(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key billing.NativeIntentKey, lock bool) (*nativeIntentLockedRow, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var raw, facts []byte
	var fingerprint string
	var factsFingerprint sql.NullString
	row := &nativeIntentLockedRow{}
	err := q.QueryRowContext(ctx, "SELECT native_intent,native_intent_fingerprint,status,proposed_request_id,canonical_request_id,native_evidence,native_evidence_fingerprint FROM usage_billing_events WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2"+suffix, key.OperationID, key.APIKeyID).Scan(&raw, &fingerprint, &row.status, &row.proposed, &row.canonical, &facts, &factsFingerprint)
	if err != nil {
		return nil, err
	}
	row.intent, err = service.DecodeNativeUsageIntentRecord(raw, key, fingerprint)
	if err != nil {
		return nil, err
	}
	if len(facts) > 0 {
		if !factsFingerprint.Valid {
			return nil, billing.ErrNativeIntentConflict
		}
		row.finalization, err = service.DecodeNativeIntentFinalization(facts, key, factsFingerprint.String)
		if err != nil {
			return nil, err
		}
	}
	return row, nil
}

func (r *usageBillingRepository) FinalizeNativeUsageIntent(ctx context.Context, input *service.NativeIntentFinalizationInput) (service.NativeIntentFinalizationResult, error) {
	var result service.NativeIntentFinalizationResult
	if ctx == nil || r == nil || r.db == nil || input == nil {
		return result, billing.ErrNativeIntentUnavailable
	}
	prepared, err := service.PrepareNativeIntentFinalization(*input)
	if err != nil {
		return result, err
	}
	a := &usageBillingLedger{repo: r}
	ledger := billing.Ledger{Journal: a, Transaction: a}
	if r.invalidate != nil {
		ledger.Invalidator = a
	}
	applied, err := ledger.FinalizeNativeIntent(ctx, prepared)
	result.Intent = applied.Intent
	if applied.Canonical != nil || a.result != nil {
		result.Canonical = &service.NativeUsageRecordResult{ActualApplyResult: a.result}
		if applied.Canonical != nil {
			result.Canonical.Receipt = *applied.Canonical
		}
	}
	return result, err
}

func (a *usageBillingLedger) PreserveNativeFinalization(ctx context.Context, command billing.NativeIntentFinalizationCommand) (billing.NativeIntentFinalizationReceipt, error) {
	var empty billing.NativeIntentFinalizationReceipt
	input, ok := command.(*service.NativeIntentFinalizationInput)
	if !ok || a.repo == nil || a.repo.db == nil {
		return empty, billing.ErrNativeIntentUnavailable
	}
	prepared, err := service.PrepareNativeIntentFinalization(*input)
	if err != nil {
		return empty, err
	}
	tx, err := a.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := loadNativeIntentRow(ctx, tx, prepared.Intent.Key, true)
	if err != nil {
		return empty, err
	}
	if row.status == "intent_not_sent" {
		return empty, billing.ErrNativeIntentClosed
	}
	if row.status == "intent_conflict" {
		return empty, billing.ErrNativeIntentConflict
	}
	factsOnly := *prepared
	factsOnly.Evidence = prepared.Evidence
	factsOnly.Evidence.Settlement = nil
	if err = service.ValidateNativeIntentFinalization(row.intent, &factsOnly); err != nil {
		return empty, err
	}
	canonical := prepared.Evidence.Evidence.Identity.RequestID
	if row.proposed.Valid && canonical != "" && row.proposed.String != canonical {
		return empty, billing.ErrNativeIntentConflict
	}
	record, err := service.MergeNativeIntentFinalization(row.finalization, prepared)
	if err != nil {
		return empty, err
	}
	proposal, err := service.NativeIntentCanonicalProposal(record)
	if err != nil {
		return empty, err
	}
	canonical = proposal.RequestID
	if row.proposed.Valid {
		if canonical != "" && row.proposed.String != canonical {
			return empty, billing.ErrNativeIntentConflict
		}
		canonical = row.proposed.String
	}
	status := "pending_usage"
	disposition := billing.NativeIntentPendingUsage
	if record.Evidence.Evidence.Confidence == billing.NativeUsageFinal && record.Evidence.Evidence.TerminalConfirmed && canonical != "" {
		status, disposition = "pending_preparation", billing.NativeIntentPendingPreparation
		for _, scope := range row.intent.Attempts {
			if scope.Sequence != prepared.Intent.AttemptSequence {
				continue
			}
			if missing := service.NativeIntentMissingPreparationPolicy(row.intent, scope, record.Preparation); missing != billing.NativePreparationReady {
				record.Reason = missing
			}
		}
		if prepared.Evidence.Settlement != nil && prepared.Evidence.Settlement.NeedsPricing() {
			record.Reason = billing.NativeOriginalPolicyUnavailable
		}
	}
	if canonical == "" {
		record.Reason = billing.NativeCanonicalIdentityUnavailable
	}
	if row.status == "intent_bound" {
		if !row.canonical.Valid || row.canonical.String != canonical {
			return empty, billing.ErrNativeIntentConflict
		}
		status, disposition = "intent_bound", billing.NativeIntentBound
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return empty, err
	}
	fingerprint, err := record.Fingerprint()
	if err != nil {
		return empty, err
	}
	var proposalArg any
	if canonical != "" {
		proposalArg = canonical
	}
	result, err := tx.ExecContext(ctx, "UPDATE usage_billing_events SET native_evidence=$3,native_evidence_fingerprint=$4,proposed_request_id=$5,status=$6,last_error=CASE WHEN native_evidence IS DISTINCT FROM $3::jsonb THEN NULL ELSE last_error END,available_at=CASE WHEN native_evidence IS DISTINCT FROM $3::jsonb THEN NOW() ELSE available_at END,updated_at=CASE WHEN native_evidence IS DISTINCT FROM $3::jsonb OR status<>$6 THEN NOW() ELSE updated_at END WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2", prepared.Intent.Key.OperationID, prepared.Intent.Key.APIKeyID, string(raw), fingerprint, proposalArg, status)
	if err != nil {
		return empty, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return empty, err
		}
		return empty, billing.ErrNativeIntentConflict
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return billing.NativeIntentFinalizationReceipt{NativeIntentReference: prepared.Intent, FactsFingerprint: fingerprint, Disposition: disposition}, nil
}

func (a *usageBillingLedger) BindNativeFinalization(ctx context.Context, command billing.NativeIntentFinalizationCommand) (billing.NativeIntentFinalizationReceipt, billing.NativeUsageReceipt, billing.Command, error) {
	var empty billing.NativeIntentFinalizationReceipt
	var noCanonical billing.NativeUsageReceipt
	input, ok := command.(*service.NativeIntentFinalizationInput)
	if !ok || a.repo == nil || a.repo.db == nil {
		return empty, noCanonical, nil, billing.ErrNativeIntentUnavailable
	}
	prepared, err := service.PrepareNativeIntentFinalization(*input)
	if err != nil {
		return empty, noCanonical, nil, err
	}
	if prepared.Evidence.Evidence.Identity.RequestID == "" {
		return empty, noCanonical, nil, &service.NativePreparationError{Reason: billing.NativeCanonicalIdentityUnavailable, Cause: billing.ErrNativePreparationPending}
	}
	tx, err := a.repo.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, noCanonical, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := loadNativeIntentRow(ctx, tx, prepared.Intent.Key, true)
	if err != nil {
		return empty, noCanonical, nil, err
	}
	if row.finalization == nil || row.finalization.Intent != prepared.Intent || !row.proposed.Valid || row.proposed.String != prepared.Evidence.Evidence.Identity.RequestID ||
		(row.status != "pending_preparation" && row.status != "intent_bound") {
		return empty, noCanonical, nil, billing.ErrNativeIntentConflict
	}
	if err = service.ValidateNativeIntentFinalization(row.intent, prepared); err != nil {
		return empty, noCanonical, nil, err
	}
	// Binding may only attach the exact already-preserved observations.
	merged, err := service.MergeNativeIntentFinalization(row.finalization, prepared)
	if err != nil {
		return empty, noCanonical, nil, err
	}
	fingerprint, err := merged.Fingerprint()
	if err != nil {
		return empty, noCanonical, nil, err
	}
	storedFingerprint, err := row.finalization.Fingerprint()
	if err != nil {
		return empty, noCanonical, nil, err
	}
	if fingerprint != storedFingerprint {
		return empty, noCanonical, nil, billing.ErrNativeIntentConflict
	}
	allowLegacy := false
	if policy := row.intent.Request.Billing; policy != nil && policy.KnownCanonical != nil {
		allowLegacy = *policy.KnownCanonical == prepared.Evidence.Evidence.Identity
	}
	canonical, settlement, err := a.preserveNativeUsageTx(ctx, tx, &prepared.Evidence, &nativeCanonicalBinding{key: prepared.Intent.Key, allowLegacy: allowLegacy})
	if err != nil {
		return empty, noCanonical, nil, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE usage_billing_events SET canonical_request_id=$3,status='intent_bound',last_error=NULL,updated_at=CASE WHEN status='intent_bound' THEN updated_at ELSE NOW() END WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2 AND proposed_request_id=$3", prepared.Intent.Key.OperationID, prepared.Intent.Key.APIKeyID, canonical.Identity.RequestID)
	if err != nil {
		return empty, noCanonical, nil, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return empty, noCanonical, nil, err
		}
		return empty, noCanonical, nil, billing.ErrNativeIntentConflict
	}
	if err = tx.Commit(); err != nil {
		return empty, noCanonical, nil, err
	}
	return billing.NativeIntentFinalizationReceipt{NativeIntentReference: prepared.Intent, FactsFingerprint: fingerprint, Disposition: billing.NativeIntentBound}, canonical, settlement, nil
}

func (r *usageBillingRepository) LoadNativeIntentRecovery(ctx context.Context, key billing.NativeIntentKey) (*service.NativeIntentRecoveryInput, error) {
	if ctx == nil || r == nil || r.db == nil {
		return nil, billing.ErrNativeIntentUnavailable
	}
	row, err := loadNativeIntentRow(ctx, r.db, key, false)
	if err != nil {
		return nil, err
	}
	if row.status == "intent_not_sent" {
		return nil, billing.ErrNativeIntentClosed
	}
	if row.finalization == nil {
		return nil, sql.ErrNoRows
	}
	return &service.NativeIntentRecoveryInput{Intent: row.intent, Finalization: row.finalization}, nil
}

func (r *usageBillingRepository) ConfigureNativeIntentPreparation(prepare func(context.Context, *service.NativeIntentRecoveryInput) (*service.NativeIntentFinalizationInput, error)) {
	if prepare == nil {
		r.nativePreparation.Store(nil)
		return
	}
	r.nativePreparation.Store(&nativeIntentPreparationHook{prepare: prepare})
}

func (r *usageBillingRepository) ConfigureSystemOneIntentPreparation(prepare func(context.Context, *service.SystemOneUsagePreparation) (*service.UsageBillingCommand, *service.UsagePricingRequest, error)) {
	if prepare == nil {
		r.systemOnePreparation.Store(nil)
		return
	}
	r.systemOnePreparation.Store(&systemOnePreparationHook{prepare: prepare})
}

func (a *usageBillingLedger) RecoverNativePreparation(ctx context.Context, event billing.Event) (bool, error) {
	if a.repo == nil || event.Kind != billing.NativePreparationEvent || event.IntentKey.OperationID == "" || event.Identity != (billing.Identity{}) {
		return false, billing.ErrNativeIntentUnavailable
	}
	stored, err := a.repo.LoadNativeIntentRecovery(ctx, event.IntentKey)
	if err != nil {
		return false, err
	}
	if stored.Finalization.SystemOne != nil {
		hook := a.repo.systemOnePreparation.Load()
		if hook == nil {
			return false, billing.ErrNativeIntentUnavailable
		}
		projection := *stored.Finalization.SystemOne
		command, pricing, err := hook.prepare(ctx, &projection)
		if err != nil {
			return false, err
		}
		if command == nil || pricing == nil || command.NeedsPricing() {
			return false, billing.ErrNativePreparationPending
		}
		projection.Settlement, projection.SettlementUsage = command, pricing
		receipt, err := a.repo.RecordSystemOneUsage(ctx, &projection)
		return receipt.Billing.Disposition == model.BillingSettled, err
	}
	hook := a.repo.nativePreparation.Load()
	if hook == nil {
		return false, billing.ErrNativeIntentUnavailable
	}
	input, err := hook.prepare(ctx, stored)
	if err != nil {
		return false, err
	}
	if input == nil || input.Intent.Key != event.IntentKey {
		return false, billing.ErrNativeIntentConflict
	}
	result, err := a.repo.FinalizeNativeUsageIntent(ctx, input)
	return result.Canonical != nil && result.Canonical.ActualApplyResult != nil, err
}

func (a *usageBillingLedger) RetryNativePreparation(ctx context.Context, key billing.NativeIntentKey, cause error) {
	reason := string(service.NativePreparationFailureReason(cause))
	if errors.Is(cause, billing.ErrNativeIntentConflict) || errors.Is(cause, billing.ErrNativeUsageConflict) || errors.Is(cause, service.ErrUsageBillingRequestConflict) {
		reason = "identity_conflict"
	}
	_, _ = a.repo.db.ExecContext(ctx, "UPDATE usage_billing_events SET last_error=$3,available_at=NOW()+make_interval(secs=>LEAST(60,POWER(2,LEAST(attempts,6)))::double precision),updated_at=NOW() WHERE event_type='native_intent' AND operation_id=$1 AND api_key_id=$2 AND status='pending_preparation'", key.OperationID, key.APIKeyID, reason)
}

var _ service.NativeIntentFinalizationRepository = (*usageBillingRepository)(nil)
var _ billing.NativeIntentFinalizationJournal = (*usageBillingLedger)(nil)
var _ billing.NativePreparationReplayer = (*usageBillingLedger)(nil)
