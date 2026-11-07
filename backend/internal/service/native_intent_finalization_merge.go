package service

import (
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// Validation compares an incoming batch to the scopes read under the journal
// lock. No new price, amount, canonical identity or measurement is inferred.
func ValidateNativeIntentFinalization(intent *NativeUsageIntentRecord, input *NativeIntentFinalizationInput) error {
	if intent == nil || input == nil || intent.Key != input.Intent.Key || intent.UserID != input.Evidence.Evidence.UserID ||
		intent.OriginalWireDigest != input.Evidence.Evidence.OriginalWireDigest ||
		intent.Request.Protocol != input.Evidence.Protocol || (input.SystemOne != nil) != (intent.Request.Protocol == NativeIntentProtocolSystemOne) {
		return billing.ErrNativeIntentConflict
	}
	scopes := make(map[uint64]NativeIntentAttemptScope, len(intent.Attempts))
	for _, attempt := range intent.Attempts {
		scopes[attempt.Sequence] = attempt
	}
	selected, ok := scopes[input.Intent.AttemptSequence]
	if !ok {
		return billing.ErrNativeIntentConflict
	}
	scope := NativeUsageIntent{Key: intent.Key, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest, Request: intent.Request, Attempt: selected}
	ref, err := scope.NativeIntentReference()
	if err != nil {
		return err
	}
	if ref != input.Intent {
		return billing.ErrNativeIntentConflict
	}
	if input.SystemOne != nil && !reflect.DeepEqual(input.SystemOne.Scope, scope) {
		return billing.ErrNativeIntentConflict
	}
	if input.Evidence.Intent == nil || *input.Evidence.Intent != ref ||
		!reflect.DeepEqual(input.Evidence.FrozenPricing, selected.FrozenPricing) {
		return billing.ErrNativeIntentConflict
	}
	rootDigest, err := hex.DecodeString(ref.IntentFingerprint)
	if err != nil || len(rootDigest) != 32 {
		return billing.ErrNativeIntentConflict
	}
	observed := make(map[uint64]bool, len(input.Evidence.Observations))
	for _, e := range input.Evidence.Observations {
		if e.Prepared == nil {
			return billing.ErrNativeIntentConflict
		}
		if e.PricingDigest != [32]byte(rootDigest) {
			return billing.ErrNativeIntentConflict
		}
		attempt, ok := scopes[e.AttemptSequence]
		if !ok {
			if e.Prepared.Purpose == model.DispatchInternalPreparation && e.AttemptScopeDigest == ([32]byte{}) {
				continue
			}
			if nativeUnpreservedNotSentEvidence(e) {
				continue
			}
			return billing.ErrNativeIntentConflict
		}
		if attempt.Prepared != *e.Prepared {
			return billing.ErrNativeIntentConflict
		}
		attemptInput := NativeUsageIntent{Key: intent.Key, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest, Request: intent.Request, Attempt: attempt}
		attemptRef, err := attemptInput.NativeIntentReference()
		if err != nil {
			return err
		}
		digest, err := hex.DecodeString(attemptRef.ScopeDigest)
		if err != nil || len(digest) != 32 || e.AttemptScopeDigest != [32]byte(digest) {
			return billing.ErrNativeIntentConflict
		}
		observed[e.AttemptSequence] = true
	}
	for sequence := range scopes {
		if !observed[sequence] {
			return billing.ErrNativeIntentConflict
		}
	}
	if p := intent.Request.Billing; p != nil && p.KnownCanonical != nil {
		if p.KnownCanonical.APIKeyID != intent.Key.APIKeyID || (input.Evidence.Evidence.Identity.RequestID != "" && *p.KnownCanonical != input.Evidence.Evidence.Identity) {
			return billing.ErrNativeIntentConflict
		}
	}
	if input.Preparation != nil && input.Preparation.FinalPricing != nil {
		raw, decision := input.Preparation.Result, input.Preparation.FinalPricing
		declared := responseModelBillingDeclaration(intent.Request.BillingModelSource, raw.UpstreamResponseModel, raw.UpstreamResponseModelConflict,
			raw.ImageCount > 0 || raw.VideoCount > 0 || raw.WebSearchCalls > 0 || raw.AudioUsage != nil || raw.SearchCount > 0)
		if declared == "" || decision.DeclaredModel != declared || (decision.Adopted && (!decision.ResponseIdentified || decision.Price == nil)) ||
			(!decision.Adopted && decision.Price != nil) {
			return nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
		}
	}
	if command := input.Evidence.Settlement; command != nil {
		policy := selected.FrozenPricing.Policy
		if command.BalanceExempt != *selected.FrozenPricing.BalanceExempt ||
			(!policy.Balance && command.BalanceCost != 0) || (!policy.Subscription && command.SubscriptionCost != 0) ||
			(!policy.KeyQuota && command.APIKeyQuotaCost != 0) || (!policy.KeyRateLimit && command.APIKeyRateLimitCost != 0) ||
			(!policy.AccountQuota && command.AccountQuotaCost != 0) {
			return billing.ErrNativeIntentConflict
		}
		if reason := NativeIntentMissingPreparationPolicy(intent, selected, input.Preparation); reason != billing.NativePreparationReady {
			return &NativePreparationError{Reason: reason, Cause: billing.ErrNativePreparationPending}
		}
		request, attempt := intent.Request.Billing, selected.Billing
		if command.AccountType != attempt.AccountType || command.RequestPayloadHash != request.RequestPayloadHash ||
			!reflect.DeepEqual(command.UsageLog.VendorIDSnapshot, attempt.VendorIDSnapshot) ||
			!reflect.DeepEqual(command.UsageLog.AccountNameSnapshot, attempt.AccountNameSnapshot) ||
			!reflect.DeepEqual(command.UsageLog.GroupNameSnapshot, attempt.GroupNameSnapshot) ||
			!reflect.DeepEqual(command.UsageLog.PlatformSnapshot, attempt.PlatformSnapshot) {
			return billing.ErrNativeIntentConflict
		}
		if intent.Request.Mode == UsageBillingModeFull {
			if command.Platform != request.QuotaPlatform || (command.AccountQuotaCost > 0 && (command.AccountQuotaHasDailyLimit != attempt.AccountQuotaHasDailyLimit ||
				command.AccountQuotaHasWeeklyLimit != attempt.AccountQuotaHasWeeklyLimit)) {
				return billing.ErrNativeIntentConflict
			}
		} else if command.Platform != "" {
			return billing.ErrNativeIntentConflict
		}
		if intent.Request.Mode == UsageBillingModeLogOnly && (command.BalanceExempt || command.SubscriptionID != nil ||
			command.BalanceCost != 0 || command.SubscriptionCost != 0 || command.APIKeyQuotaCost != 0 || command.APIKeyRateLimitCost != 0 || command.AccountQuotaCost != 0) {
			return billing.ErrNativeIntentConflict
		}
	}
	return nil
}

// Only the real capture owner can fill missing policy. Recovery must not turn
// a nil historical whitelist into current configuration or explicit free use.
func NativeIntentMissingPreparationPolicy(intent *NativeUsageIntentRecord, selected NativeIntentAttemptScope, preparation *NativeUsagePreparation) billing.NativePreparationReason {
	if intent == nil || intent.Request.Billing == nil || intent.Request.Billing.Version != 1 ||
		selected.Billing == nil || selected.Billing.Version != 1 || selected.Billing.AccountType == "" || selected.Billing.AccountPlatform == "" {
		return billing.NativeOriginalPolicyUnavailable
	}
	if selected.Billing.FreeFastEligible == nil {
		return billing.NativeOriginalPolicyUnavailable
	}
	stats := selected.Billing.Stats
	if stats == nil || (stats.State != NativeIntentStatsCaptured && stats.State != NativeIntentStatsNotApplicable) {
		return billing.NativeStatsPolicyUnavailable
	}
	if intent.Request.Protocol == NativeIntentProtocolSystemOne {
		if selected.Billing.AccountPlatform != PlatformTypeSafe {
			return billing.NativeOriginalPolicyUnavailable
		}
		return billing.NativePreparationReady
	}
	if preparation == nil {
		return billing.NativeUsagePreparationUnavailable
	}
	r := preparation.Result
	if intent.Request.Billing.GroupImageCountBilling == nil {
		return billing.NativeOriginalPolicyUnavailable
	}
	hosted := intent.Request.Billing.Hosted
	if r.ImageCount > 0 || r.VideoCount > 0 || r.WebSearchCalls > 0 || r.SearchCount > 0 || r.AudioUsage != nil {
		if hosted == nil || hosted.Version != 1 {
			return billing.NativeHostedPolicyUnavailable
		}
		if (r.WebSearchCalls > 0 && hosted.WebSearchPerCall == nil) || (r.SearchCount > 0 && hosted.SearchPer1k == nil) ||
			(r.AudioUsage != nil && (hosted.AudioRealtimePerMin == nil || hosted.AudioTTSPerMChars == nil || hosted.AudioSTTPerHour == nil)) {
			return billing.NativeHostedPolicyUnavailable
		}
		if r.ImageCount > 0 || r.VideoCount > 0 {
			for _, model := range selected.CandidateModels {
				price := selected.Billing.HostedPrices[model]
				if price == nil || price.Version != 1 || (r.ImageCount > 0 && price.ImageDefaults == nil) || (r.VideoCount > 0 && price.VideoDefaults == nil) {
					return billing.NativeHostedPolicyUnavailable
				}
			}
		}
	}
	return billing.NativePreparationReady
}

func MergeNativeIntentFinalization(prior *NativeIntentFinalizationRecord, input *NativeIntentFinalizationInput) (*NativeIntentFinalizationRecord, error) {
	if input == nil {
		return nil, billing.ErrNativeIntentInvalid
	}
	var oldEvidence *NativeUsageEvidenceRecord
	record := &NativeIntentFinalizationRecord{Version: 1, Intent: input.Intent, Reason: input.Reason}
	if prior != nil {
		raw, err := json.Marshal(prior)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, record); err != nil {
			return nil, err
		}
		if record.Version != 1 || record.Intent.Key != input.Intent.Key || record.Intent.IntentFingerprint != input.Intent.IntentFingerprint {
			return nil, billing.ErrNativeIntentConflict
		}
		oldEvidence = record.Evidence
	}
	// All amounts remain in the canonical V1 payload. The technical record
	// stores the validated quantity mapping and actual observation history.
	evidence, err := MergeNativeUsageRecord(oldEvidence, &input.Evidence)
	if err != nil {
		return nil, err
	}
	record.Evidence = evidence
	if _, err := NativeIntentCanonicalProposal(record); err != nil {
		return nil, err
	}
	if evidence.Evidence.AttemptSequence != input.Intent.AttemptSequence {
		return nil, billing.ErrNativeIntentConflict
	}
	record.Intent = input.Intent
	record.Reason = input.Reason
	if input.SystemOne != nil {
		if err := mergeSystemOnePreparation(record, prior, input); err != nil {
			return nil, err
		}
	}
	if input.Preparation == nil && record.Preparation != nil {
		if prior.Intent.AttemptSequence == input.Intent.AttemptSequence {
			// A duplicate facts-only delivery cannot erase a later successfully
			// captured projection or make it permanently unrecoverable again.
			record.Reason = prior.Reason
		} else {
			// Preparations are attempt-scoped. Keep the historical entry, but
			// never apply another attempt's projection to new final facts.
			record.Preparation = nil
		}
	}
	if input.Preparation != nil {
		if prior != nil && prior.Evidence.Evidence.Confidence == billing.NativeUsageFinal && prior.Preparation != nil &&
			!reflect.DeepEqual(prior.Preparation, input.Preparation) {
			// Final pricing may be materialized once from the retained live
			// view. The measured preparation itself is already immutable.
			oldRaw, newRaw := *prior.Preparation, *input.Preparation
			oldRaw.FinalPricing, newRaw.FinalPricing = nil, nil
			if !reflect.DeepEqual(oldRaw, newRaw) || (prior.Preparation.FinalPricing != nil && !reflect.DeepEqual(prior.Preparation.FinalPricing, input.Preparation.FinalPricing)) {
				return nil, billing.ErrNativeIntentConflict
			}
		}
		found := false
		for _, p := range record.Preparations {
			if p.Sequence == input.Intent.AttemptSequence && reflect.DeepEqual(p.Preparation, input.Preparation) {
				found = true
				break
			}
		}
		if !found {
			if len(record.Preparations) >= 64 {
				return nil, billing.ErrNativeIntentInvalid
			}
			record.Preparations = append(record.Preparations, NativeIntentPreparationObservation{Sequence: input.Intent.AttemptSequence, Preparation: input.Preparation})
		}
		record.Preparation = input.Preparation
	}
	if _, err = PrepareNativeIntentFinalization(NativeIntentFinalizationInput{
		Intent: record.Intent, Preparation: record.Preparation, SystemOne: record.SystemOne, Reason: record.Reason,
		Evidence: NativeUsageEvidenceInput{Protocol: record.Evidence.Protocol, Intent: &record.Intent, Evidence: record.Evidence.Evidence,
			Observations: record.Evidence.Observations, FrozenPricing: record.Evidence.FrozenPricing, SettlementUsage: record.Evidence.SettlementUsage},
	}); err != nil {
		return nil, err
	}
	if _, err = record.Fingerprint(); err != nil {
		return nil, err
	}
	return record, nil
}

// Canonical proposals are root facts, not metering samples. Check every
// original observation so two different nonempty IDs cannot hide on separate
// attempts. Missing IDs remain missing; no provider or operation ID is made up.
func NativeIntentCanonicalProposal(record *NativeIntentFinalizationRecord) (billing.Identity, error) {
	if record == nil || record.Evidence == nil {
		return billing.Identity{}, billing.ErrNativeIntentInvalid
	}
	id := billing.Identity{APIKeyID: record.Intent.Key.APIKeyID}
	for _, e := range append([]billing.NativeUsageEvidence{record.Evidence.Evidence}, record.Evidence.Observations...) {
		if e.Identity.APIKeyID != id.APIKeyID {
			return billing.Identity{}, billing.ErrNativeIntentConflict
		}
		if e.Identity.RequestID == "" {
			continue
		}
		if id.RequestID != "" && id.RequestID != e.Identity.RequestID {
			return billing.Identity{}, billing.ErrNativeIntentConflict
		}
		id.RequestID = e.Identity.RequestID
	}
	return id, nil
}

func DecodeNativeIntentFinalization(raw []byte, ref billing.NativeIntentKey, fingerprint string) (*NativeIntentFinalizationRecord, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, billing.ErrNativeIntentInvalid
	}
	var record NativeIntentFinalizationRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, err
	}
	if record.Version != 1 || record.Intent.Key != ref {
		return nil, billing.ErrNativeIntentConflict
	}
	if record.Evidence == nil || record.Evidence.Version != 2 ||
		record.Evidence.Intent == nil || *record.Evidence.Intent != record.Intent ||
		len(record.Evidence.Observations) == 0 || len(record.Preparations) > 64 {
		return nil, billing.ErrNativeIntentConflict
	}
	hash, err := record.Fingerprint()
	if err != nil {
		return nil, err
	}
	if hash != fingerprint {
		return nil, billing.ErrNativeIntentConflict
	}
	if _, err := NativeIntentCanonicalProposal(&record); err != nil {
		return nil, err
	}
	_, err = PrepareNativeIntentFinalization(NativeIntentFinalizationInput{Intent: record.Intent, Preparation: record.Preparation, SystemOne: record.SystemOne, Reason: record.Reason,
		Evidence: NativeUsageEvidenceInput{Protocol: record.Evidence.Protocol, Intent: &record.Intent, Evidence: record.Evidence.Evidence, Observations: record.Evidence.Observations, FrozenPricing: record.Evidence.FrozenPricing, SettlementUsage: record.Evidence.SettlementUsage}})
	if err != nil {
		return nil, err
	}
	return &record, nil
}
