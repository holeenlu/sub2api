package handler

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func wsNativeFixtureContext(ctx context.Context, cleanup bool) error {
	if ctx == nil {
		return billing.ErrNativeIntentInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := ctx.Deadline(); cleanup && !ok {
		return errors.New("native finalizer must use bounded cleanup")
	}
	return nil
}

// These are the original acquisition receipts, not fabricated lease evidence.
// They must still be live both before send and while final facts are recorded.
func verifyWSNativeFixtureCapacity(ctx context.Context, userID, accountID int64) error {
	scope := routing.CapacityScopeFrom(ctx)
	if scope == nil {
		return errors.New("native journal lost its operation capacity scope")
	}
	for _, target := range []struct {
		user bool
		id   int64
	}{{true, userID}, {false, accountID}} {
		lease := scope.Lookup(target.user, target.id)
		if lease == nil {
			return errors.New("native journal lost its acquired receipt")
		}
		if _, err := lease.Verify(ctx, lease.Policy()); err != nil {
			return err
		}
	}
	return nil
}

func (r *wsNativeFixtureJournal) StoreNativeUsageIntent(ctx context.Context, input *service.NativeUsageIntent) (billing.NativeIntentReceipt, error) {
	var empty billing.NativeIntentReceipt
	if err := wsNativeFixtureContext(ctx, false); err != nil {
		return empty, err
	}
	if input == nil {
		return empty, billing.ErrNativeIntentInvalid
	}
	prepared, err := service.PrepareNativeUsageIntent(*input)
	if err != nil {
		return empty, err
	}
	if err := verifyWSNativeFixtureCapacity(ctx, prepared.UserID, prepared.Attempt.Prepared.ResourceID); err != nil {
		return empty, err
	}
	ref, err := prepared.NativeIntentReference()
	if err != nil {
		return empty, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closures[prepared.Key] != nil {
		return empty, billing.ErrNativeIntentClosed
	}
	stored, err := service.MergeNativeUsageIntent(r.intents[prepared.Key], *prepared)
	if err != nil {
		return empty, err
	}
	if r.intents == nil {
		r.intents = make(map[billing.NativeIntentKey]*service.NativeUsageIntentRecord)
	}
	r.intents[prepared.Key] = stored
	return billing.NativeIntentReceipt{NativeIntentReference: ref, Disposition: billing.NativeIntentPreserved}, nil
}

func (r *wsNativeFixtureJournal) LoadNativeUsageIntent(ctx context.Context, key billing.NativeIntentKey) (*service.NativeUsageIntentRecord, error) {
	if err := wsNativeFixtureContext(ctx, false); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := r.intents[key]
	if stored == nil {
		return nil, sql.ErrNoRows
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	var detached service.NativeUsageIntentRecord
	if err = json.Unmarshal(raw, &detached); err != nil {
		return nil, err
	}
	return &detached, nil
}

func (r *wsNativeFixtureJournal) CloseNativeUsageIntent(ctx context.Context, input *service.NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error) {
	var empty billing.NativeIntentClosureReceipt
	if err := wsNativeFixtureContext(ctx, true); err != nil {
		return empty, err
	}
	if input == nil {
		return empty, billing.ErrNativeIntentInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	closed, err := service.ValidateNativeIntentNotSent(r.intents[input.Key], *input)
	if err != nil {
		return empty, err
	}
	ref, err := input.NativeIntentClosureReference()
	if err != nil {
		return empty, err
	}
	if r.finalizations[input.Key] != nil || (r.closures[input.Key] != nil && !reflect.DeepEqual(r.closures[input.Key], closed)) {
		return empty, billing.ErrNativeIntentConflict
	}
	if r.closures == nil {
		r.closures = make(map[billing.NativeIntentKey]*service.NativeIntentNotSentRecord)
	}
	r.closures[input.Key] = closed
	return billing.NativeIntentClosureReceipt{NativeIntentClosureReference: ref, ClosedNotSent: true}, nil
}

func (r *wsNativeFixtureJournal) ConfigureNativeIntentPreparation(prepare func(context.Context, *service.NativeIntentRecoveryInput) (*service.NativeIntentFinalizationInput, error)) {
	r.mu.Lock()
	r.prepareRecovery = prepare
	r.mu.Unlock()
}

func (r *wsNativeFixtureJournal) LoadNativeIntentRecovery(ctx context.Context, key billing.NativeIntentKey) (*service.NativeIntentRecoveryInput, error) {
	if err := wsNativeFixtureContext(ctx, false); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closures[key] != nil {
		return nil, billing.ErrNativeIntentClosed
	}
	if r.intents[key] == nil || r.finalizations[key] == nil {
		return nil, sql.ErrNoRows
	}
	raw, err := json.Marshal(service.NativeIntentRecoveryInput{Intent: r.intents[key], Finalization: r.finalizations[key]})
	if err != nil {
		return nil, err
	}
	var detached service.NativeIntentRecoveryInput
	if err = json.Unmarshal(raw, &detached); err != nil {
		return nil, err
	}
	return &detached, nil
}

func (r *wsNativeFixtureJournal) FinalizeNativeUsageIntent(ctx context.Context, input *service.NativeIntentFinalizationInput) (service.NativeIntentFinalizationResult, error) {
	var empty service.NativeIntentFinalizationResult
	if err := wsNativeFixtureContext(ctx, true); err != nil {
		return empty, err
	}
	if input == nil {
		return empty, billing.ErrNativeIntentInvalid
	}
	prepared, err := service.PrepareNativeIntentFinalization(*input)
	if err != nil {
		return empty, err
	}
	evidence := prepared.Evidence.Evidence
	if evidence.Prepared == nil {
		return empty, billing.ErrNativeIntentInvalid
	}
	if err := verifyWSNativeFixtureCapacity(ctx, evidence.UserID, evidence.Prepared.ResourceID); err != nil {
		return empty, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := prepared.Intent.Key
	if r.closures[key] != nil {
		return empty, billing.ErrNativeIntentClosed
	}
	// Preserve valid technical facts even if binding the monetary command fails.
	factsOnly := *prepared
	factsOnly.Evidence.Settlement = nil
	if err := service.ValidateNativeIntentFinalization(r.intents[key], &factsOnly); err != nil {
		return empty, err
	}
	record, err := service.MergeNativeIntentFinalization(r.finalizations[key], prepared)
	if err != nil {
		return empty, err
	}
	proposal, err := service.NativeIntentCanonicalProposal(record)
	if err != nil {
		return empty, err
	}
	disposition := billing.NativeIntentPendingUsage
	if record.Evidence.Evidence.Confidence == billing.NativeUsageFinal && record.Evidence.Evidence.TerminalConfirmed && proposal.RequestID != "" {
		disposition = billing.NativeIntentPendingPreparation
		for _, scope := range r.intents[key].Attempts {
			if scope.Sequence == prepared.Intent.AttemptSequence {
				if reason := service.NativeIntentMissingPreparationPolicy(r.intents[key], scope, record.Preparation); reason != billing.NativePreparationReady {
					record.Reason = reason
				}
			}
		}
		if prepared.Evidence.Settlement != nil && prepared.Evidence.Settlement.NeedsPricing() {
			record.Reason = billing.NativeOriginalPolicyUnavailable
		}
	}
	if proposal.RequestID == "" {
		record.Reason = billing.NativeCanonicalIdentityUnavailable
	}
	prior := r.records[proposal]
	bound := prior != nil && prior.Intent != nil && prior.Intent.Key == key
	if bound {
		disposition = billing.NativeIntentBound
	}
	fingerprint, err := record.Fingerprint()
	if err != nil {
		return empty, err
	}
	if r.finalizations == nil {
		r.finalizations = make(map[billing.NativeIntentKey]*service.NativeIntentFinalizationRecord)
	}
	r.finalizations[key] = record
	r.calls++
	result := service.NativeIntentFinalizationResult{Intent: billing.NativeIntentFinalizationReceipt{
		NativeIntentReference: prepared.Intent, FactsFingerprint: fingerprint, Disposition: disposition,
	}}
	if prepared.Evidence.Settlement == nil && !bound {
		return result, nil
	}
	if prepared.Evidence.Settlement != nil && prepared.Evidence.Settlement.NeedsPricing() {
		return result, billing.ErrNativePreparationPending
	}
	if err := service.ValidateNativeIntentFinalization(r.intents[key], prepared); err != nil {
		return result, err
	}
	canonical, err := r.recordCanonicalLocked(ctx, &prepared.Evidence, r.intents[key].Request.Mode)
	if canonical.Receipt.Durable() {
		result.Intent.Disposition = billing.NativeIntentBound
		result.Canonical = &canonical
	}
	return result, err
}

// Synthetic policy/facts for the fixture's own port tests. Redis acquisition
// uses the same cache implementation as the existing handler consumer tests.
// These tests neither exercise PostgreSQL nor claim real upstream evidence.
func wsNativeIntentFixtureInput() service.NativeUsageIntent {
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	digest := [32]byte{3}
	exempt, imageCountBilling, freeFast := false, false, false
	return service.NativeUsageIntent{
		Key: billing.NativeIntentKey{OperationID: "ws-fixture-operation", APIKeyID: 2}, UserID: 3, OriginalWireDigest: digest,
		Request: service.NativeIntentRequestScope{
			Mode: service.UsageBillingModeLogOnly, PricingAt: at, UserRole: "user", OriginalModel: "gpt-5.1",
			BaseMultiplier: 1, TokenMultiplier: 1, ImageMultiplier: 1, VideoMultiplier: 1,
			Billing: &service.NativeIntentRequestBillingPolicy{Version: 1, GroupImageCountBilling: &imageCountBilling},
		},
		Attempt: service.NativeIntentAttemptScope{
			Sequence: 1, CapturedAt: at, BaselineModel: "gpt-5.1", CandidateModels: []string{"gpt-5.1"},
			Prepared: model.PreparedIdentity{
				OperationID: "ws-fixture-operation", ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 2,
				Purpose: model.DispatchGeneration, Mode: "native", Transport: "ws", GrantDigest: digest, TargetDigest: digest, WireDigest: digest,
				BodyKind: model.BodyReplayable, BodyDigest: digest,
			},
			FrozenPricing: &service.NativeFrozenPricing{BalanceExempt: &exempt,
				Policy: &service.UsageBillingPricingSnapshot{Model: "gpt-5.1", PricingAt: at, AccountRateMultiplier: 1},
				Resolved: &service.NativeResolvedPricingSnapshot{Mode: service.BillingModeToken, Source: service.PricingSourceGroup,
					BasePricing: &service.ModelPricing{InputPricePerToken: 0.125}, CacheWriteFlatOverride: true}},
			Billing: &service.NativeIntentAttemptBillingPolicy{Version: 1, AccountType: service.AccountTypeAPIKey, AccountPlatform: service.PlatformOpenAI,
				FreeFastEligible: &freeFast, Stats: &service.NativeIntentStatsPricing{State: service.NativeIntentStatsNotApplicable}},
		},
	}
}

func wsNativeIntentFixtureContext(t *testing.T, intent service.NativeUsageIntent) (context.Context, *wsNativeFixtureCapacity) {
	t.Helper()
	cache := newWSNativeFixtureCapacity(t, nil, nil)
	ctx, _ := routing.WithCapacityScope(t.Context())
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	t.Cleanup(cancel)
	for _, request := range []routing.CapacityRequest{
		{ID: intent.UserID, User: true, Limit: 1, Token: "ws-fixture-user"},
		{ID: intent.Attempt.Prepared.ResourceID, Limit: 1, Token: "ws-fixture-account"},
	} {
		lease, err := routing.AcquireCapacity(ctx, cache, request, nil)
		require.NoError(t, err)
		require.True(t, lease.Acquired)
		t.Cleanup(lease.ReleaseFunc)
	}
	return ctx, cache
}

func wsNativeIntentFixtureFinalization(t *testing.T, intent service.NativeUsageIntent, canonical string, confidence billing.NativeUsageConfidence) service.NativeIntentFinalizationInput {
	t.Helper()
	ref, err := intent.NativeIntentReference()
	require.NoError(t, err)
	digest, err := hex.DecodeString(ref.ScopeDigest)
	require.NoError(t, err)
	p := intent.Attempt.Prepared
	evidence := billing.NativeUsageEvidence{Version: 2, Identity: billing.Identity{RequestID: canonical, APIKeyID: intent.Key.APIKeyID},
		OperationID: intent.Key.OperationID, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest,
		Prepared: &p, AttemptSequence: intent.Attempt.Sequence, AttemptScopeDigest: [32]byte(digest), SendState: model.SendSent, Confidence: confidence}
	if confidence != billing.NativeUsageUnknown {
		input := int64(10)
		evidence.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "response.usage", InputTokens: &input}
	}
	if confidence == billing.NativeUsageFinal {
		evidence.Terminal, evidence.TerminalConfirmed = model.TerminalCompleted, true
	}
	return service.NativeIntentFinalizationInput{Intent: ref,
		Evidence: service.NativeUsageEvidenceInput{Intent: &ref, Evidence: evidence, FrozenPricing: intent.Attempt.FrozenPricing}}
}

func wsNativeIntentFixtureSettlement(t *testing.T, intent service.NativeUsageIntent) service.NativeIntentFinalizationInput {
	t.Helper()
	input := wsNativeIntentFixtureFinalization(t, intent, "observed-canonical", billing.NativeUsageFinal)
	input.Preparation = &service.NativeUsagePreparation{Result: service.NativeUsagePreparationResult{Model: "gpt-5.1", Usage: service.OpenAIUsage{InputTokens: 10}}}
	input.Evidence.SettlementUsage = &service.UsagePricingRequest{OpenAI: input.Preparation.Result.ForwardResult(), Tokens: service.UsageTokens{InputTokens: 10}}
	log := &service.UsageLog{RequestID: "observed-canonical", APIKeyID: intent.Key.APIKeyID, UserID: intent.UserID, AccountID: intent.Attempt.Prepared.ResourceID, InputTokens: 10}
	input.Evidence.Settlement = &service.UsageBillingCommand{UsageLog: log, RequestID: log.RequestID, APIKeyID: log.APIKeyID, UserID: log.UserID,
		AccountID: log.AccountID, AccountType: intent.Attempt.Billing.AccountType, InputTokens: 10, Model: "gpt-5.1", Platform: intent.Request.Billing.QuotaPlatform}
	return input
}

func TestWSNativeIntentFixturePreservesDetachedScopeBeforeSend(t *testing.T) {
	input := wsNativeIntentFixtureInput()
	ctx, _ := wsNativeIntentFixtureContext(t, input)
	journal := &wsNativeFixtureJournal{}
	port, err := service.ProvideNativeBillingIntentService(journal)
	require.NoError(t, err)
	receipt, err := port.PreserveBeforeSend(ctx, input)
	require.NoError(t, err)
	require.True(t, receipt.Durable(), "receipt certifies only this in-memory fixture")
	input.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 99
	input.Attempt.CandidateModels[0] = "changed-after-store"
	stored, err := port.Load(ctx, input.Key)
	require.NoError(t, err)
	require.Equal(t, 0.125, stored.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, []string{"gpt-5.1"}, stored.Attempts[0].CandidateModels)
	require.Nil(t, stored.Attempts[0].FrozenPricing.Policy.Request, "preserving policy must not invent usage")
	stored.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken = 88
	again, err := port.Load(ctx, input.Key)
	require.NoError(t, err)
	require.Equal(t, 0.125, again.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	_, err = journal.LoadNativeIntentRecovery(ctx, input.Key)
	require.ErrorIs(t, err, sql.ErrNoRows)
	require.Empty(t, journal.identities())
	rows, writes, applies := journal.counts()
	require.Zero(t, rows)
	require.Zero(t, writes, "before-send storage is not a finalization write")
	require.Zero(t, applies)

	conflict := wsNativeIntentFixtureInput()
	conflict.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 2
	rejected, err := port.PreserveBeforeSend(ctx, conflict)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	require.False(t, rejected.Durable())
	conflict.Attempt.Sequence = 2
	retried, err := port.PreserveBeforeSend(ctx, conflict)
	require.NoError(t, err)
	require.Equal(t, receipt.IntentFingerprint, retried.IntentFingerprint)
	require.NotEqual(t, receipt.ScopeDigest, retried.ScopeDigest)
	again, err = port.Load(ctx, input.Key)
	require.NoError(t, err)
	require.Len(t, again.Attempts, 2)
	require.Equal(t, 0.125, again.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, float64(2), again.Attempts[1].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
}

func TestWSNativeIntentFixtureTechnicalFactsDoNotInventCanonical(t *testing.T) {
	for _, tc := range []struct {
		name       string
		canonical  string
		confidence billing.NativeUsageConfidence
	}{
		{"unknown", "", billing.NativeUsageUnknown},
		{"partial", "", billing.NativeUsagePartial},
		{"final_without_identity", "", billing.NativeUsageFinal},
		{"partial_with_identity", "observed-canonical", billing.NativeUsagePartial},
		{"final_without_preparation", "observed-canonical", billing.NativeUsageFinal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			intent := wsNativeIntentFixtureInput()
			ctx, _ := wsNativeIntentFixtureContext(t, intent)
			journal := &wsNativeFixtureJournal{}
			_, err := journal.StoreNativeUsageIntent(ctx, &intent)
			require.NoError(t, err)
			input := wsNativeIntentFixtureFinalization(t, intent, tc.canonical, tc.confidence)
			result, err := journal.FinalizeNativeUsageIntent(ctx, &input)
			require.NoError(t, err)
			require.True(t, result.Intent.Durable())
			require.Nil(t, result.Canonical)
			want := billing.NativeIntentPendingUsage
			if tc.canonical != "" && tc.confidence == billing.NativeUsageFinal {
				want = billing.NativeIntentPendingPreparation
			}
			require.Equal(t, want, result.Intent.Disposition)
			recovered, err := journal.LoadNativeIntentRecovery(ctx, intent.Key)
			require.NoError(t, err)
			evidence := recovered.Finalization.Evidence
			require.Equal(t, tc.canonical, evidence.Evidence.Identity.RequestID)
			require.Equal(t, tc.confidence, evidence.Evidence.Confidence)
			require.Nil(t, evidence.SettlementSequence)
			require.Nil(t, evidence.SettlementUsage)
			require.False(t, evidence.Receipt.Durable(), "technical evidence has no canonical receipt")
			if tc.confidence == billing.NativeUsageUnknown {
				require.Nil(t, evidence.Evidence.Usage)
			} else {
				require.EqualValues(t, 10, *evidence.Evidence.Usage.InputTokens)
				require.Nil(t, evidence.Evidence.Usage.OutputTokens, "unmeasured output must not become zero")
			}
			if tc.canonical == "" {
				require.Equal(t, billing.NativeCanonicalIdentityUnavailable, recovered.Finalization.Reason)
			} else if tc.confidence == billing.NativeUsageFinal {
				require.Equal(t, billing.NativeUsagePreparationUnavailable, recovered.Finalization.Reason)
			}
			fingerprint, err := recovered.Finalization.Fingerprint()
			require.NoError(t, err)
			require.Equal(t, fingerprint, result.Intent.FactsFingerprint)
			recovered.Intent.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken = 9
			recovered.Finalization.Evidence.Evidence.Identity.RequestID = "mutated-copy"
			fresh, err := journal.LoadNativeIntentRecovery(ctx, intent.Key)
			require.NoError(t, err)
			require.Equal(t, tc.canonical, fresh.Finalization.Evidence.Evidence.Identity.RequestID)
			require.Equal(t, 0.125, fresh.Intent.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
			rows, writes, applies := journal.counts()
			require.Zero(t, rows)
			require.Equal(t, 1, writes)
			require.Zero(t, applies)
			require.Empty(t, journal.identities())
		})
	}
}

func TestWSNativeIntentFixtureOnlyLogOnlySettlesThroughExistingSink(t *testing.T) {
	for _, kind := range []string{"log_only", "full", "full_zero", "key_window"} {
		t.Run(kind, func(t *testing.T) {
			intent := wsNativeIntentFixtureInput()
			switch kind {
			case "full", "full_zero":
				intent.Request.Mode = service.UsageBillingModeFull
				intent.Attempt.FrozenPricing.Policy.Balance = true
			case "key_window":
				intent.Request.Mode = service.UsageBillingModeKeyWindowOnly
				intent.Attempt.FrozenPricing.Policy.KeyRateLimit = true
			}
			ctx, _ := wsNativeIntentFixtureContext(t, intent)
			sink := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
			journal := &wsNativeFixtureJournal{logSink: sink}
			_, err := journal.StoreNativeUsageIntent(ctx, &intent)
			require.NoError(t, err)
			input := wsNativeIntentFixtureSettlement(t, intent)
			if kind == "full" {
				input.Evidence.Settlement.BalanceCost = 1.25
			} else if kind == "key_window" {
				input.Evidence.Settlement.APIKeyRateLimitCost = 1.25
			}
			for attempt := 0; attempt < 2; attempt++ {
				result, err := journal.FinalizeNativeUsageIntent(ctx, &input)
				require.NoError(t, err)
				require.Equal(t, billing.NativeIntentBound, result.Intent.Disposition)
				require.True(t, result.Intent.Durable())
				require.NotNil(t, result.Canonical)
				require.Nil(t, result.Canonical.ActualApplyResult, "no actual monetary transaction ran")
				want := billing.SettlementPending
				if kind == "log_only" {
					want = billing.NativeSettled
				}
				require.Equal(t, want, result.Canonical.Receipt.Disposition)
				canonical, err := journal.LoadNativeUsageEvidence(ctx, input.Evidence.Evidence.Identity)
				require.NoError(t, err)
				require.Equal(t, result.Canonical.Receipt, canonical.Receipt)
				require.Equal(t, canonical.Evidence.Fingerprint(), canonical.Receipt.EvidenceFingerprint)
				require.NotNil(t, canonical.SettlementSequence)
			}
			if kind == "log_only" {
				require.Len(t, sink.created, 1, "settled replay must not duplicate the log")
				journal.assertUsageLogs(t, []*service.UsageLog{<-sink.created})
			} else {
				require.Empty(t, sink.created, "zero monetary amounts alone do not grant log-only policy")
			}
			rows, writes, applies := journal.counts()
			require.Equal(t, 1, rows)
			require.Equal(t, 2, writes)
			require.Zero(t, applies)
		})
	}
}

func TestWSNativeIntentFixtureCanceledCleanupAndNotSentClosure(t *testing.T) {
	intent := wsNativeIntentFixtureInput()
	ctx, _ := wsNativeIntentFixtureContext(t, intent)
	journal := &wsNativeFixtureJournal{}
	preserved, err := journal.StoreNativeUsageIntent(ctx, &intent)
	require.NoError(t, err)
	input := service.NativeIntentNotSentInput{Key: intent.Key, IntentFingerprint: preserved.IntentFingerprint,
		Operation: model.OperationResult{OperationID: intent.Key.OperationID, APIKeyID: intent.Key.APIKeyID, UserID: intent.UserID,
			OriginalWireDigest: intent.OriginalWireDigest, State: model.OperationCanceled,
			Attempts: []model.AttemptFact{{Sequence: 1, Identity: intent.Attempt.Prepared, Send: model.SendNotSent}}}}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	rejected, err := journal.CloseNativeUsageIntent(canceled, &input)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, rejected.Durable())
	cleanup, stop := context.WithTimeout(context.WithoutCancel(canceled), time.Second)
	defer stop()
	for _, send := range []model.SendState{model.SendUnknown, model.SendSent, model.SendAmbiguous} {
		input.Operation.Attempts[0].Send = send
		rejected, err = journal.CloseNativeUsageIntent(cleanup, &input)
		require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
		require.False(t, rejected.Durable())
	}
	input.Operation.Attempts[0].Send = model.SendNotSent
	// A registered later attempt may have failed before its scope was stored.
	input.Operation.Attempts = append(input.Operation.Attempts, model.AttemptFact{Sequence: 2, Identity: intent.Attempt.Prepared, Send: model.SendNotSent})
	closed, err := journal.CloseNativeUsageIntent(cleanup, &input)
	require.NoError(t, err)
	require.True(t, closed.Durable())
	again, err := journal.CloseNativeUsageIntent(cleanup, &input)
	require.NoError(t, err)
	require.Equal(t, closed, again)
	require.Len(t, journal.closures[intent.Key].Attempts, 2)
	stored, err := journal.LoadNativeUsageIntent(cleanup, intent.Key)
	require.NoError(t, err)
	require.Len(t, stored.Attempts, 1, "closure must not manufacture an unsaved price scope")
	_, err = journal.StoreNativeUsageIntent(cleanup, &intent)
	require.ErrorIs(t, err, billing.ErrNativeIntentClosed)
	final := wsNativeIntentFixtureFinalization(t, intent, "", billing.NativeUsageUnknown)
	result, err := journal.FinalizeNativeUsageIntent(cleanup, &final)
	require.ErrorIs(t, err, billing.ErrNativeIntentClosed)
	require.False(t, result.Intent.Durable())
	require.Nil(t, result.Canonical)
	_, err = journal.LoadNativeIntentRecovery(cleanup, intent.Key)
	require.ErrorIs(t, err, billing.ErrNativeIntentClosed)
	require.Empty(t, journal.identities())
	journal.assertNoLegacyApply(t)
}

func TestWSNativeIntentFixtureRequiresLiveCapacity(t *testing.T) {
	for _, phase := range []string{"store", "finalize"} {
		for _, failure := range []string{"missing_scope", "released_user", "released_account", "lost_user_token", "lost_account_token", "canceled"} {
			t.Run(phase+"/"+failure, func(t *testing.T) {
				intent := wsNativeIntentFixtureInput()
				ctx, cache := wsNativeIntentFixtureContext(t, intent)
				journal := &wsNativeFixtureJournal{}
				if phase == "finalize" {
					_, err := journal.StoreNativeUsageIntent(ctx, &intent)
					require.NoError(t, err)
				}
				scope := routing.CapacityScopeFrom(ctx)
				switch failure {
				case "missing_scope":
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
					defer cancel()
				case "released_user":
					scope.Lookup(true, intent.UserID).ReleaseFunc()
				case "released_account":
					scope.Lookup(false, intent.Attempt.Prepared.ResourceID).ReleaseFunc()
				case "lost_user_token":
					require.NoError(t, cache.ReleaseUserSlot(ctx, intent.UserID, "ws-fixture-user"))
				case "lost_account_token":
					require.NoError(t, cache.ReleaseAccountSlot(ctx, intent.Attempt.Prepared.ResourceID, "ws-fixture-account"))
				case "canceled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				if phase == "store" {
					receipt, err := journal.StoreNativeUsageIntent(ctx, &intent)
					require.Error(t, err)
					require.False(t, receipt.Durable())
					require.Empty(t, journal.intents)
				} else {
					input := wsNativeIntentFixtureSettlement(t, intent)
					result, err := journal.FinalizeNativeUsageIntent(ctx, &input)
					require.Error(t, err)
					require.False(t, result.Intent.Durable())
					require.Nil(t, result.Canonical)
					require.Empty(t, journal.finalizations)
				}
				rows, writes, applies := journal.counts()
				require.Zero(t, rows)
				require.Zero(t, writes)
				require.Zero(t, applies)
			})
		}
	}
}

func TestWSNativeIntentFixtureFinalizationRequiresPreservedScopeAndBoundedCleanup(t *testing.T) {
	for _, failure := range []string{"missing_intent", "changed_price", "unbounded", "canceled", "bounded_after_cancel"} {
		t.Run(failure, func(t *testing.T) {
			intent := wsNativeIntentFixtureInput()
			ctx, _ := wsNativeIntentFixtureContext(t, intent)
			journal := &wsNativeFixtureJournal{}
			if failure != "missing_intent" {
				_, err := journal.StoreNativeUsageIntent(ctx, &intent)
				require.NoError(t, err)
			}
			input := wsNativeIntentFixtureFinalization(t, intent, "", billing.NativeUsageUnknown)
			switch failure {
			case "changed_price":
				input.Evidence.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 8
			case "unbounded":
				ctx = context.WithoutCancel(ctx)
			case "canceled", "bounded_after_cancel":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
				if failure == "bounded_after_cancel" {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(context.WithoutCancel(canceled), time.Second)
					defer stop()
				}
			}
			result, err := journal.FinalizeNativeUsageIntent(ctx, &input)
			if failure == "bounded_after_cancel" {
				require.NoError(t, err)
				require.True(t, result.Intent.Durable())
				require.Len(t, journal.finalizations, 1)
			} else {
				require.Error(t, err)
				require.False(t, result.Intent.Durable())
				require.Empty(t, journal.finalizations)
			}
			require.Nil(t, result.Canonical)
			require.Empty(t, journal.identities())
		})
	}
}

type wsNativeIntentFixtureFailingLogSink struct {
	service.UsageLogRepository
	err error
}

func (s *wsNativeIntentFixtureFailingLogSink) Create(ctx context.Context, log *service.UsageLog) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.UsageLogRepository.Create(ctx, log)
}

func TestWSNativeIntentFixtureSinkFailureKeepsReceiptsSeparate(t *testing.T) {
	intent := wsNativeIntentFixtureInput()
	ctx, _ := wsNativeIntentFixtureContext(t, intent)
	logs := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 2)}
	failure := errors.New("fixture log sink unavailable")
	sink := &wsNativeIntentFixtureFailingLogSink{UsageLogRepository: logs, err: failure}
	journal := &wsNativeFixtureJournal{logSink: sink}
	_, err := journal.StoreNativeUsageIntent(ctx, &intent)
	require.NoError(t, err)
	input := wsNativeIntentFixtureSettlement(t, intent)
	result, err := journal.FinalizeNativeUsageIntent(ctx, &input)
	require.ErrorIs(t, err, failure)
	require.True(t, result.Intent.Durable(), "failed log delivery does not erase preserved technical facts")
	require.Equal(t, billing.NativeIntentBound, result.Intent.Disposition)
	require.NotNil(t, result.Canonical)
	require.Equal(t, billing.SettlementPending, result.Canonical.Receipt.Disposition)
	require.Nil(t, result.Canonical.ActualApplyResult)
	require.Empty(t, logs.created)
	recovered, err := journal.LoadNativeIntentRecovery(ctx, intent.Key)
	require.NoError(t, err)
	require.False(t, recovered.Finalization.Evidence.Receipt.Durable())
	canonical, err := journal.LoadNativeUsageEvidence(ctx, input.Evidence.Evidence.Identity)
	require.NoError(t, err)
	require.Equal(t, billing.SettlementPending, canonical.Receipt.Disposition)
	sink.err = nil
	result, err = journal.FinalizeNativeUsageIntent(ctx, &input)
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, result.Canonical.Receipt.Disposition)
	require.Nil(t, result.Canonical.ActualApplyResult)
	require.Len(t, logs.created, 1)
	journal.assertUsageLogs(t, []*service.UsageLog{<-logs.created})
}
