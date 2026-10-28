package billing

import (
	"context"
	"errors"
)

type NativePreparationReason string

const (
	NativePreparationReady             NativePreparationReason = ""
	NativeOriginalPolicyUnavailable    NativePreparationReason = "original_policy_unavailable"
	NativeHostedPolicyUnavailable      NativePreparationReason = "hosted_policy_unavailable"
	NativeStatsPolicyUnavailable       NativePreparationReason = "account_stats_policy_unavailable"
	NativeReturnedPriceViewUnavailable NativePreparationReason = "returned_price_view_unavailable"
	NativeBuilderUnavailable           NativePreparationReason = "builder_unavailable"
	NativeUsagePreparationUnavailable  NativePreparationReason = "usage_preparation_unavailable"
	NativeCanonicalIdentityUnavailable NativePreparationReason = "canonical_identity_unavailable"
)

func (r NativePreparationReason) Valid() bool {
	switch r {
	case NativePreparationReady, NativeOriginalPolicyUnavailable, NativeHostedPolicyUnavailable, NativeStatsPolicyUnavailable,
		NativeReturnedPriceViewUnavailable, NativeBuilderUnavailable, NativeUsagePreparationUnavailable, NativeCanonicalIdentityUnavailable:
		return true
	default:
		return false
	}
}

var ErrNativePreparationPending = errors.New("native usage preparation is pending")

type NativeIntentFinalizationDisposition uint8

const (
	NativeIntentNotFinalized NativeIntentFinalizationDisposition = iota
	NativeIntentPendingUsage
	NativeIntentPendingPreparation
	NativeIntentBound
)

// NativeIntentFinalizationReceipt confirms technical facts only. A canonical
// receipt exists separately, after the real canonical row has been committed.
type NativeIntentFinalizationReceipt struct {
	NativeIntentReference
	FactsFingerprint string
	Disposition      NativeIntentFinalizationDisposition
}

func (r NativeIntentFinalizationReceipt) Durable() bool {
	return r.NativeIntentReference.valid() && r.Disposition >= NativeIntentPendingUsage && r.Disposition <= NativeIntentBound &&
		(NativeIntentReference{Key: r.Key, AttemptSequence: 1, IntentFingerprint: r.FactsFingerprint, ScopeDigest: r.ScopeDigest}).valid()
}

type NativeIntentFinalizationCommand interface {
	NativeIntentFinalizationReference() (NativeIntentReference, error)
	NativeCanonicalEvidence() NativeEvidenceCommand
}

type NativeIntentFinalizationJournal interface {
	PreserveNativeFinalization(context.Context, NativeIntentFinalizationCommand) (NativeIntentFinalizationReceipt, error)
	BindNativeFinalization(context.Context, NativeIntentFinalizationCommand) (NativeIntentFinalizationReceipt, NativeUsageReceipt, Command, error)
}

type NativeIntentFinalizationResult struct {
	Intent    NativeIntentFinalizationReceipt
	Canonical *NativeUsageReceipt
}

// FinalizeNativeIntent first preserves facts, including preparation failures.
// Binding and canonical payload persistence share one database transaction.
// Only the original settlement workflow below performs monetary effects.
func (l Ledger) FinalizeNativeIntent(ctx context.Context, input NativeIntentFinalizationCommand) (NativeIntentFinalizationResult, error) {
	var result NativeIntentFinalizationResult
	if ctx == nil || input == nil {
		return result, ErrNativeIntentInvalid
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	ref, err := input.NativeIntentFinalizationReference()
	if err != nil {
		return result, err
	}
	if !ref.valid() {
		return result, ErrNativeIntentInvalid
	}
	journal, ok := l.Journal.(NativeIntentFinalizationJournal)
	if !ok {
		return result, ErrNativeIntentUnavailable
	}
	result.Intent, err = journal.PreserveNativeFinalization(ctx, input)
	if err != nil {
		return NativeIntentFinalizationResult{}, err
	}
	if !result.Intent.Durable() || result.Intent.NativeIntentReference != ref {
		return NativeIntentFinalizationResult{}, ErrNativeIntentConflict
	}
	evidence := input.NativeCanonicalEvidence()
	if evidence == nil || (evidence.NativeSettlement() == nil && result.Intent.Disposition != NativeIntentBound) {
		return result, nil
	}
	if evidence.NativeSettlement() != nil && evidence.NativeSettlement().NeedsPricing() {
		return result, ErrNativePreparationPending
	}
	if l.Transaction == nil {
		return result, ErrNativeIntentUnavailable
	}
	intent, canonical, command, err := journal.BindNativeFinalization(ctx, input)
	if err != nil {
		return result, err
	}
	if !intent.Durable() || intent.Disposition != NativeIntentBound || intent.NativeIntentReference != ref ||
		!canonical.Durable() || canonical.Identity != evidence.NativeEvidence().Identity {
		return result, ErrNativeIntentConflict
	}
	result.Intent, result.Canonical = intent, &canonical
	if canonical.Disposition == NativeSettled {
		return result, nil
	}
	if command == nil || !command.HasUsageRecord() || command.BillingIdentity() != canonical.Identity || command.NeedsPricing() {
		return result, ErrNativeIntentConflict
	}
	if err = l.settleRecorded(ctx, command); err != nil {
		return result, err
	}
	reader, ok := l.Journal.(NativeEvidenceJournal)
	if !ok {
		return result, ErrNativeIntentUnavailable
	}
	current, err := reader.ReadNativeReceipt(ctx, canonical.Identity)
	if err != nil {
		return result, err
	}
	if !current.Durable() || current.Identity != canonical.Identity || current.EvidenceFingerprint != canonical.EvidenceFingerprint {
		return result, ErrNativeIntentConflict
	}
	result.Canonical = &current
	return result, nil
}
