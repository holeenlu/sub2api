package service

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var _ NativeOperationBilling = (*OpenAIGatewayService)(nil)

// RecordNativeOperationUsage persists parser facts against actual before-send
// scopes. A technical ACK is never converted into a monetary receipt. Missing
// preparation or canonical identity remains on the original recoverable journal.
func (s *OpenAIGatewayService) RecordNativeOperationUsage(ctx context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
	var empty NativeIntentFinalizationResult
	if s == nil || ctx == nil || in.Identity.APIKeyID != in.Operation.APIKeyID || len(in.CurrentSequences) == 0 {
		return empty, billing.ErrNativeUsageEvidence
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	port, err := ProvideNativeBillingIntentService(s.usageBillingRepo)
	if err != nil {
		return empty, err
	}
	stored, err := port.Load(ctx, billing.NativeIntentKey{OperationID: in.Operation.OperationID, APIKeyID: in.Operation.APIKeyID})
	if err != nil {
		return empty, err
	}
	ref, scope, err := nativeIntentSelectedScope(stored, in.Operation, in.CurrentSequences)
	if err != nil {
		return empty, err
	}
	if in.Intent != ref {
		return empty, billing.ErrNativeIntentConflict
	}
	rootDigest, err := hex.DecodeString(ref.IntentFingerprint)
	if err != nil {
		return empty, err
	}
	root := billing.NativeUsageEvidence{Version: 2, Identity: in.Identity, OperationID: in.Operation.OperationID,
		UserID: in.Operation.UserID, GroupID: in.Operation.GroupID, GroupBound: in.Operation.GroupBound,
		OriginalWireDigest: in.Operation.OriginalWireDigest, PricingDigest: [32]byte(rootDigest)}
	refs := make(map[uint64]billing.NativeIntentReference, len(stored.Attempts))
	for _, attempt := range stored.Attempts {
		r, err := (&NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: attempt}).NativeIntentReference()
		if err != nil {
			return empty, err
		}
		refs[attempt.Sequence] = r
	}
	evidence := NativeUsageEvidenceInput{Intent: &ref, FrozenPricing: scope.FrozenPricing}
	for _, attempt := range in.Operation.Attempts {
		observed, err := NativeUsageEvidenceFromAttempt(root, attempt)
		if err != nil {
			return empty, err
		}
		if r, ok := refs[attempt.Sequence]; ok {
			digest, err := hex.DecodeString(r.ScopeDigest)
			if err != nil {
				return empty, err
			}
			observed.AttemptScopeDigest = [32]byte(digest)
		} else if attempt.Identity.Purpose == model.DispatchGeneration &&
			(attempt.Send != model.SendNotSent || attempt.Usage != nil || attempt.RequestID != "" || attempt.ResponseID != "" || attempt.Terminal != model.TerminalUnknown || attempt.Delivery.BusinessCommitted || attempt.Delivery.PossiblyCommitted) {
			return empty, billing.ErrNativeIntentConflict
		}
		evidence.Observations = append(evidence.Observations, observed)
		if attempt.Sequence == ref.AttemptSequence {
			evidence.Evidence = observed
		}
	}
	final := NativeIntentFinalizationInput{Intent: ref, Evidence: evidence}
	selectedCurrent := false
	for _, sequence := range in.CurrentSequences {
		selectedCurrent = selectedCurrent || sequence == ref.AttemptSequence
	}
	var preparationErr error
	if evidence.Evidence.Confidence == billing.NativeUsageFinal && evidence.Evidence.TerminalConfirmed {
		input := in.UsageInput
		if !selectedCurrent || input == nil || input.Result == nil || input.Account == nil || input.Account.ID != scope.Prepared.ResourceID ||
			input.APIKey == nil || input.APIKey.ID != in.Operation.APIKeyID || input.User == nil || input.User.ID != in.Operation.UserID {
			final.Reason = billing.NativeUsagePreparationUnavailable
		} else {
			final.Preparation, preparationErr = CaptureNativeUsagePreparation(ctx, input)
			if preparationErr != nil {
				final.Reason = billing.NativeUsagePreparationUnavailable
			}
		}
		if final.Preparation != nil {
			final.Reason = billing.NativeBuilderUnavailable
			checked, checkErr := PrepareNativeIntentFinalization(final)
			if checkErr != nil {
				// A malformed billing projection cannot erase valid parser facts. It is
				// discarded, never normalized into invented usage or a zero bill.
				final.Preparation = nil
				final.Reason = billing.NativeUsagePreparationUnavailable
				preparationErr = checkErr
			} else {
				final = *checked
				recovered := NativeIntentRecoveryInput{Intent: stored, Finalization: &NativeIntentFinalizationRecord{Version: 1, Intent: ref, Preparation: final.Preparation, Reason: final.Reason,
					Evidence: &NativeUsageEvidenceRecord{Version: 2, Intent: &ref, Evidence: final.Evidence.Evidence, Observations: final.Evidence.Observations, FrozenPricing: scope.FrozenPricing}}}
				var live *nativeIntentLivePricing
				if owner := nativeHTTPFromContext(ctx); owner != nil && owner.intent != nil {
					live = owner.intent.live[ref.AttemptSequence]
				}
				final.Preparation.FinalPricing, preparationErr = s.MaterializeNativeIntentPricing(ctx, recovered, live)
				if preparationErr == nil {
					var quote NativeIntentPreparedUsage
					quote, preparationErr = s.PrepareNativeIntentUsage(ctx, recovered)
					if preparationErr == nil {
						final.Evidence.Settlement, final.Evidence.SettlementUsage = quote.Settlement, quote.SettlementUsage
						final.Reason = billing.NativePreparationReady
					}
				}
				if preparationErr != nil {
					final.Reason = NativePreparationFailureReason(preparationErr)
				}
			}
		}
	}
	// This instance consumes the journal only. The recovery hook is installed
	// once at host assembly, never replaced by an individual request finalizer.
	repository, ok := s.usageBillingRepo.(NativeIntentFinalizationRepository)
	if !ok {
		return empty, billing.ErrNativeIntentUnavailable
	}
	recorder := &NativeIntentFinalizationService{repo: repository}
	result, recordErr := recorder.FinalizeNativeIntent(ctx, final)
	if !result.Intent.Durable() {
		return result, errors.Join(preparationErr, recordErr, ErrNativeOperationBillingReceipt)
	}
	// A preparation failure with a real technical ACK is a durable pending
	// outcome; its typed reason is stored, not disguised as a settlement.
	if result.Canonical != nil && result.Canonical.ActualApplyResult != nil && final.Evidence.Settlement != nil && in.UsageInput != nil {
		command, input := final.Evidence.Settlement, in.UsageInput
		params := &postUsageBillingParams{User: input.User, APIKey: input.APIKey, Account: input.Account, APIKeyService: input.APIKeyService,
			Subscription: input.Subscription, IsSubscriptionBill: stored.Request.Billing.IsSubscriptionBill,
			AccountRateMultiplier: scope.FrozenPricing.Policy.AccountRateMultiplier, UsageLogOnly: stored.Request.Mode == UsageBillingModeLogOnly,
			Cost: &CostBreakdown{TotalCost: command.UsageLog.TotalCost, ActualCost: command.UsageLog.ActualCost}}
		_, effectsErr := finishUsageBillingEffects(ctx, params, s.billingDeps(), result.Canonical.ActualApplyResult)
		recordErr = errors.Join(recordErr, effectsErr)
	}
	// The canonical command already includes every authorization usage row.
	// Its journal owns their persistence; a technical or monetary ACK must not
	// trigger a second best-effort write outside that transaction.
	return result, recordErr
}

// Selected evidence must be a real preserved generation. Missing-scope
// NotSent attempts are observation-only even when they are newest.
func nativeIntentSelectedScope(stored *NativeUsageIntentRecord, operation model.OperationResult, current []uint64) (billing.NativeIntentReference, NativeIntentAttemptScope, error) {
	var empty billing.NativeIntentReference
	var selected NativeIntentAttemptScope
	if stored == nil || stored.Key.OperationID != operation.OperationID || stored.Key.APIKeyID != operation.APIKeyID || stored.UserID != operation.UserID || stored.OriginalWireDigest != operation.OriginalWireDigest {
		return empty, selected, billing.ErrNativeIntentConflict
	}
	wanted := make(map[uint64]bool, len(current))
	for _, sequence := range current {
		if sequence == 0 || wanted[sequence] {
			return empty, selected, billing.ErrNativeIntentConflict
		}
		wanted[sequence] = true
	}
	seen := make(map[uint64]bool, len(operation.Attempts))
	selectedCurrent := false
	for _, attempt := range operation.Attempts {
		if seen[attempt.Sequence] {
			return empty, selected, billing.ErrNativeIntentConflict
		}
		seen[attempt.Sequence] = true
		for _, scope := range stored.Attempts {
			if scope.Sequence != attempt.Sequence {
				continue
			}
			if scope.Prepared != attempt.Identity {
				return empty, selected, billing.ErrNativeIntentConflict
			}
			isCurrent := wanted[attempt.Sequence]
			if (isCurrent && !selectedCurrent) || (isCurrent == selectedCurrent && scope.Sequence > selected.Sequence) {
				selected, selectedCurrent = scope, isCurrent
			}
		}
	}
	for sequence := range wanted {
		if !seen[sequence] {
			return empty, selected, billing.ErrNativeIntentConflict
		}
	}
	if selected.Sequence == 0 {
		return empty, selected, billing.ErrNativeIntentUnavailable
	}
	ref, err := (&NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: selected}).NativeIntentReference()
	return ref, selected, err
}
