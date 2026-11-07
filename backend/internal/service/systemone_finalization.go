package service

import (
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/Wei-Shaw/sub2api/internal/billing"
)

// System One uses the same technical finalizer and canonical ledger. Only its
// measured projection differs from OpenAI; no storage or recovery loop lives here.
func prepareSystemOneFinalization(input NativeIntentFinalizationInput, evidence *NativeUsageEvidenceInput) (*NativeIntentFinalizationInput, error) {
	if input.SystemOne == nil || input.Preparation != nil || evidence.Protocol != NativeIntentProtocolSystemOne {
		return nil, billing.ErrNativeIntentConflict
	}
	source := *input.SystemOne
	source.Settlement, source.SettlementUsage = nil, nil
	source.Operation.Failure = nil
	raw, err := json.Marshal(source)
	if err != nil || len(raw) > 1<<20 {
		return nil, billing.ErrNativeIntentInvalid
	}
	// Decode into a zero value: unmarshalling over a shallow copy reuses
	// nested pointers and mutates a caller shared with a concurrent replay.
	var p SystemOneUsagePreparation
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if err = ValidateSystemOneUsagePreparation(p); err != nil {
		return nil, err
	}
	e := evidence.Evidence
	if p.Intent != input.Intent || p.Identity != e.Identity || e.Version != 2 ||
		e.OperationID != p.Operation.OperationID || e.UserID != p.Scope.UserID ||
		e.GroupID != p.Operation.GroupID || e.GroupBound != p.Operation.GroupBound ||
		e.OriginalWireDigest != p.Scope.OriginalWireDigest ||
		!reflect.DeepEqual(evidence.FrozenPricing, p.Scope.Attempt.FrozenPricing) {
		return nil, billing.ErrNativeIntentConflict
	}
	matched := false
	for _, attempt := range p.Operation.Attempts {
		if attempt.Sequence != p.Intent.AttemptSequence {
			continue
		}
		root := billing.NativeUsageEvidence{Version: 2, Identity: p.Identity, OperationID: p.Operation.OperationID,
			UserID: p.Scope.UserID, GroupID: p.Operation.GroupID, GroupBound: p.Operation.GroupBound,
			OriginalWireDigest: p.Scope.OriginalWireDigest, PricingDigest: e.PricingDigest}
		actual, err := NativeUsageEvidenceFromAttempt(root, attempt)
		if err != nil {
			return nil, err
		}
		digest, err := hex.DecodeString(p.Intent.ScopeDigest)
		if err != nil {
			return nil, err
		}
		actual.AttemptScopeDigest = [32]byte(digest)
		if !reflect.DeepEqual(actual, e) {
			return nil, billing.ErrNativeUsageConflict
		}
		matched = true
	}
	if !matched {
		return nil, billing.ErrNativeIntentConflict
	}
	if evidence.Settlement != nil {
		if input.Reason != billing.NativePreparationReady || p.StatusCode < 200 || p.StatusCode >= 300 ||
			p.InputTokens == nil || p.OutputTokens == nil {
			return nil, billing.ErrNativeUsageConflict
		}
	} else if e.Confidence == billing.NativeUsageFinal && e.TerminalConfirmed {
		// A final projection exists, so a transient builder failure is recoverable.
		// Missing original price/view remains explicitly unrepairable.
		if p.FinalModel == "" {
			input.Reason = billing.NativeReturnedPriceViewUnavailable
		} else if p.Scope.Attempt.FrozenPricing.Resolved == nil {
			input.Reason = billing.NativeOriginalPolicyUnavailable
		} else {
			input.Reason = billing.NativeBuilderUnavailable
		}
	}
	input.SystemOne, input.Evidence = &p, *evidence
	return &input, nil
}

func mergeSystemOnePreparation(record, prior *NativeIntentFinalizationRecord, input *NativeIntentFinalizationInput) error {
	if prior != nil && prior.SystemOne == nil {
		return billing.ErrNativeIntentConflict
	}
	if prior != nil && prior.Intent.AttemptSequence == input.Intent.AttemptSequence {
		if prior.Evidence.Evidence.Confidence == billing.NativeUsageFinal && input.Evidence.Evidence.Confidence != billing.NativeUsageFinal {
			// A delayed unknown/partial delivery cannot erase the already
			// confirmed projection or make its original bill unrecoverable.
			record.SystemOne, record.Reason = prior.SystemOne, prior.Reason
			return nil
		}
		old, next := *prior.SystemOne, *input.SystemOne
		// Execution state/closeout timestamps can advance without changing the
		// measured bill. MergeNativeUsageRecord checks every prior observation.
		old.Operation, next.Operation = input.SystemOne.Operation, input.SystemOne.Operation
		old.ObservedAt, next.ObservedAt = input.SystemOne.ObservedAt, input.SystemOne.ObservedAt
		old.Duration, next.Duration = input.SystemOne.Duration, input.SystemOne.Duration
		if old.Identity.RequestID == "" {
			old.Identity = next.Identity
		} else if next.Identity.RequestID == "" {
			next.Identity = old.Identity
		}
		if prior.Evidence.Evidence.Confidence == billing.NativeUsageFinal && !reflect.DeepEqual(old, next) {
			return billing.ErrNativeIntentConflict
		}
	}
	copy := *input.SystemOne
	copy.Identity = record.Evidence.Evidence.Identity
	record.SystemOne = &copy
	return nil
}
