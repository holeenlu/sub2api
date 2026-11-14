package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

// In-memory durable-port substitute. Every scope/fact/closure uses the same
// validation and merge functions as storage. These are not PG commit receipts.
func (r *nativeApplicationJournal) StoreNativeUsageIntent(ctx context.Context, input *NativeUsageIntent) (billing.NativeIntentReceipt, error) {
	r.mu.Lock()
	beforeStore := r.beforeStore
	r.mu.Unlock()
	if beforeStore != nil {
		beforeStore(ctx, input)
	}
	var afterStore func()
	receipt, err := func() (billing.NativeIntentReceipt, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.storeErr != nil {
			return billing.NativeIntentReceipt{}, r.storeErr
		}
		if err := ctx.Err(); err != nil {
			return billing.NativeIntentReceipt{}, err
		}
		if r.intents == nil {
			r.intents = make(map[billing.NativeIntentKey]*NativeUsageIntentRecord)
		}
		stored, err := MergeNativeUsageIntent(r.intents[input.Key], *input)
		if err != nil {
			return billing.NativeIntentReceipt{}, err
		}
		r.intents[input.Key] = stored
		r.stores++
		afterStore = r.afterStore
		if r.storeAckErr != nil {
			return billing.NativeIntentReceipt{}, r.storeAckErr
		}
		ref, err := input.NativeIntentReference()
		return billing.NativeIntentReceipt{NativeIntentReference: ref, Disposition: billing.NativeIntentPreserved}, err
	}()
	// Cancellation/revocation hooks may call back into the port. The durable
	// state is already visible, but no journal mutex is held across the hook.
	if afterStore != nil {
		afterStore()
	}
	return receipt, err
}
func (r *nativeApplicationJournal) LoadNativeUsageIntent(ctx context.Context, key billing.NativeIntentKey) (*NativeUsageIntentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadNativeUsageIntentLocked(ctx, key)
}

// The caller holds mu so recovery can read the intent and its facts atomically.
func (r *nativeApplicationJournal) loadNativeUsageIntentLocked(ctx context.Context, key billing.NativeIntentKey) (*NativeUsageIntentRecord, error) {
	r.loads++
	if r.loadErr != nil {
		return nil, r.loadErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored := r.intents[key]
	if stored == nil {
		return nil, sql.ErrNoRows
	}
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	var detached NativeUsageIntentRecord
	err = json.Unmarshal(raw, &detached)
	return &detached, err
}
func (r *nativeApplicationJournal) CloseNativeUsageIntent(ctx context.Context, input *NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return billing.NativeIntentClosureReceipt{}, err
	}
	if _, err := ValidateNativeIntentNotSent(r.intents[input.Key], *input); err != nil {
		return billing.NativeIntentClosureReceipt{}, err
	}
	ref, err := input.NativeIntentClosureReference()
	if err != nil {
		return billing.NativeIntentClosureReceipt{}, err
	}
	r.closures++
	r.closure = input
	return billing.NativeIntentClosureReceipt{NativeIntentClosureReference: ref, ClosedNotSent: true}, nil
}
func (r *nativeApplicationJournal) ConfigureNativeIntentPreparation(prepare func(context.Context, *NativeIntentRecoveryInput) (*NativeIntentFinalizationInput, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoverPreparation = prepare
}
func (r *nativeApplicationJournal) LoadNativeIntentRecovery(ctx context.Context, key billing.NativeIntentKey) (*NativeIntentRecoveryInput, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	intent, err := r.loadNativeUsageIntentLocked(ctx, key)
	if err != nil {
		return nil, err
	}
	final := r.finals[key]
	if final == nil {
		return nil, sql.ErrNoRows
	}
	raw, err := json.Marshal(final)
	if err != nil {
		return nil, err
	}
	var detached NativeIntentFinalizationRecord
	if err = json.Unmarshal(raw, &detached); err != nil {
		return nil, err
	}
	return &NativeIntentRecoveryInput{Intent: intent, Finalization: &detached}, nil
}
func (r *nativeApplicationJournal) FinalizeNativeUsageIntent(ctx context.Context, input *NativeIntentFinalizationInput) (NativeIntentFinalizationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var empty NativeIntentFinalizationResult
	r.records++
	if r.fail != nil {
		return empty, r.fail
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	prepared, err := PrepareNativeIntentFinalization(*input)
	if err != nil {
		return empty, err
	}
	if err = ValidateNativeIntentFinalization(r.intents[input.Intent.Key], prepared); err != nil {
		return empty, err
	}
	final, err := MergeNativeIntentFinalization(r.finals[prepared.Intent.Key], prepared)
	if err != nil {
		return empty, err
	}
	fingerprint, err := final.Fingerprint()
	if err != nil {
		return empty, err
	}
	if r.finals == nil {
		r.finals = make(map[billing.NativeIntentKey]*NativeIntentFinalizationRecord)
	}
	r.finals[prepared.Intent.Key] = final
	// Keep the last-call projections for existing, joined single-root tests.
	r.final, r.input = final, &prepared.Evidence
	disposition := billing.NativeIntentPendingUsage
	if prepared.Evidence.Evidence.Confidence == billing.NativeUsageFinal {
		disposition = billing.NativeIntentPendingPreparation
	}
	result := NativeIntentFinalizationResult{Intent: billing.NativeIntentFinalizationReceipt{NativeIntentReference: prepared.Intent, FactsFingerprint: fingerprint, Disposition: disposition}}
	if prepared.Evidence.Settlement != nil {
		result.Intent.Disposition = billing.NativeIntentBound
		result.Canonical = &NativeUsageRecordResult{Receipt: billing.NativeUsageReceipt{Identity: prepared.Evidence.Evidence.Identity, EvidenceFingerprint: prepared.Evidence.Evidence.Fingerprint(), Disposition: billing.NativeSettled}, ActualApplyResult: r.effects}
	}
	return result, nil
}

// Rebind only in fixture setup, before any prepared attempt. Tests that replace
// the repository or policy must explicitly bind after that replacement.
func bindNativeRuntimeFixture(t *testing.T, f *nativeOperationFixture) {
	t.Helper()
	owner := nativeHTTPFromContext(f.ctx)
	require.Empty(t, owner.core.Snapshot().Attempts, "fixture policy must precede real send")
	owner.intent = nil
	if f.service.billingService == nil {
		f.service.billingService = NewBillingService(f.service.cfg, nil)
	}
	if f.service.usageBillingRepo == nil {
		f.service.usageBillingRepo = &nativeApplicationJournal{t: t}
	}
	if f.op.request.Transport == "http" && nativeIntentCanonicalID(f.ctx, "", false) == "" {
		f.ctx = context.WithValue(f.ctx, ctxkey.RequestID, "fixture-admitted-http")
	}
	facts := NativeIntentModelFacts{Model: "client-model", UpstreamModel: "mapped-provider-model", BillingModel: "mapped-provider-model"}
	f.ctx = WithNativeIntentModelFacts(f.ctx, facts)
	require.NoError(t, f.service.BindNativeIntentPricing(f.ctx, NativeIntentPricingInput{APIKey: f.key, User: f.key.User,
		APIKeyService: &APIKeyService{}, PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), ChannelUsageFields: ChannelUsageFields{OriginalModel: "client-model"}}))
}

func nativeFixtureTechnicalReceipt(in NativeOperationFinalization) NativeIntentFinalizationResult {
	return NativeIntentFinalizationResult{Intent: billing.NativeIntentFinalizationReceipt{NativeIntentReference: in.Intent,
		FactsFingerprint: in.Intent.IntentFingerprint, Disposition: billing.NativeIntentPendingUsage}}
}

func TestNativeApplicationJournalHooksRunOutsideLock(t *testing.T) {
	_, journal, _, input := nativeApplicationFixture(t)
	stored, err := journal.LoadNativeUsageIntent(t.Context(), input.Intent.Key)
	require.NoError(t, err)
	intent := NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest,
		Request: stored.Request, Attempt: stored.Attempts[0]}
	var hooks []string
	journal.beforeStore = func(ctx context.Context, in *NativeUsageIntent) {
		require.True(t, journal.mu.TryLock(), "beforeStore must not run under the journal lock")
		journal.mu.Unlock()
		_, err := journal.LoadNativeUsageIntent(ctx, in.Key)
		require.NoError(t, err, "beforeStore may reenter the journal")
		hooks = append(hooks, "before")
	}
	journal.afterStore = func() {
		require.True(t, journal.mu.TryLock(), "afterStore must not run under the journal lock")
		journal.mu.Unlock()
		_, err := journal.LoadNativeUsageIntent(t.Context(), intent.Key)
		require.NoError(t, err, "afterStore may read the committed intent")
		hooks = append(hooks, "after")
	}
	receipt, err := journal.StoreNativeUsageIntent(t.Context(), &intent)
	require.NoError(t, err)
	require.True(t, receipt.Durable())
	require.Equal(t, input.Intent, receipt.NativeIntentReference)
	require.Equal(t, []string{"before", "after"}, hooks)
	require.Equal(t, 2, journal.stores)
}

func TestNativeApplicationJournalConcurrentRootsKeepDetachedRecovery(t *testing.T) {
	journal := &nativeApplicationJournal{t: t}
	type fixture struct {
		gateway *OpenAIGatewayService
		input   NativeOperationFinalization
		intent  NativeUsageIntent
	}
	var fixtures []fixture
	for _, operationID := range []string{"journal-first-root", "journal-second-root"} {
		gateway, source, _, input := nativeApplicationFixture(t, func(_ *OpenAIGatewayService, in *NativeOperationFinalization) {
			in.Operation.OperationID = operationID
			in.Operation.Attempts[0].Identity.OperationID = operationID
			in.Identity.RequestID = "billing-" + operationID
		})
		stored, err := source.LoadNativeUsageIntent(t.Context(), input.Intent.Key)
		require.NoError(t, err)
		intent := NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest,
			Request: stored.Request, Attempt: stored.Attempts[0]}
		gateway.usageBillingRepo = journal
		fixtures = append(fixtures, fixture{gateway: gateway, input: input, intent: intent})
	}
	type outcome struct {
		want      billing.NativeIntentReference
		preserved billing.NativeIntentReceipt
		finalized NativeIntentFinalizationResult
		err       error
	}
	const callers = 4
	start := make(chan struct{})
	results := make(chan outcome, callers*len(fixtures))
	for _, f := range fixtures {
		raw, err := json.Marshal(f.input)
		require.NoError(t, err)
		for range callers {
			var input NativeOperationFinalization
			require.NoError(t, json.Unmarshal(raw, &input), "each caller owns its billing projection")
			go func() {
				<-start
				got := outcome{want: input.Intent}
				got.preserved, got.err = journal.StoreNativeUsageIntent(t.Context(), &f.intent)
				if got.err == nil {
					got.finalized, got.err = f.gateway.RecordNativeOperationUsage(t.Context(), input)
				}
				results <- got
			}()
		}
	}
	close(start)
	for range callers * len(fixtures) {
		got := wsLifecycleReceive(t, results)
		require.NoError(t, got.err)
		require.True(t, got.preserved.Durable())
		require.Equal(t, got.want, got.preserved.NativeIntentReference)
		require.True(t, got.finalized.Intent.Durable())
		require.Equal(t, got.want, got.finalized.Intent.NativeIntentReference)
		require.Equal(t, billing.NativeIntentBound, got.finalized.Intent.Disposition)
		require.NotNil(t, got.finalized.Canonical)
		require.Equal(t, billing.NativeSettled, got.finalized.Canonical.Receipt.Disposition)
	}
	require.Equal(t, callers*len(fixtures), journal.stores)
	require.Equal(t, callers*len(fixtures), journal.records)
	require.Len(t, journal.intents, len(fixtures))
	require.Len(t, journal.finals, len(fixtures))
	require.NotNil(t, journal.final)
	require.NotNil(t, journal.input)
	for _, f := range fixtures {
		recovered, err := journal.LoadNativeIntentRecovery(t.Context(), f.intent.Key)
		require.NoError(t, err)
		require.Equal(t, f.input.Intent, recovered.Finalization.Intent)
		require.Equal(t, f.input.Identity, recovered.Finalization.Evidence.Evidence.Identity)
		require.Equal(t, f.input.Operation.OperationID, recovered.Finalization.Evidence.Evidence.OperationID)
		recovered.Intent.Attempts[0].BaselineModel = "caller-mutated-model"
		recovered.Finalization.Evidence.Evidence.UpstreamRequestID = "caller-mutated-request"
		again, err := journal.LoadNativeIntentRecovery(t.Context(), f.intent.Key)
		require.NoError(t, err)
		require.Equal(t, f.intent.Attempt.BaselineModel, again.Intent.Attempts[0].BaselineModel)
		require.Equal(t, f.input.Operation.Attempts[0].RequestID, again.Finalization.Evidence.Evidence.UpstreamRequestID)
	}
}

func TestNativeIntentRuntimeMissingBindingAndPreserveFailureNeverSend(t *testing.T) {
	for _, kind := range []string{"missing_binding", "store_failure", "ack_lost", "ack_lost_load_failure", "cancel_after_preserve", "revoked_after_preserve"} {
		t.Run(kind, func(t *testing.T) {
			f, owner, c := nativeHTTPObservedFixture(t, "", "application/json")
			repo := &nativeApplicationJournal{t: t}
			f.service.usageBillingRepo = repo
			bindNativeRuntimeFixture(t, f)
			failed := errors.New("intent persistence failure")
			switch kind {
			case "missing_binding":
				owner.intent = nil
			case "store_failure":
				repo.storeErr = failed
			case "ack_lost":
				repo.storeAckErr = failed
			case "ack_lost_load_failure":
				repo.storeAckErr, repo.loadErr = failed, errors.New("load unavailable")
			case "cancel_after_preserve":
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				f.ctx = ctx
				// Cancel after the fake durable commit, while its ACK is returning.
				repo.afterStore = cancel
			case "revoked_after_preserve":
				repo.afterStore = func() { f.key.Status = StatusDisabled }
			}
			err := RunNativeHTTPOperation(f.ctx, c, func() NativeOperationStep {
				_, sendErr := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
				require.Error(t, sendErr)
				return NativeOperationStep{Err: sendErr}
			})
			require.Error(t, err)
			require.Zero(t, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			require.False(t, owner.core.Snapshot().Billing.Durable)
			if kind == "ack_lost" || kind == "cancel_after_preserve" || kind == "revoked_after_preserve" {
				require.Equal(t, 1, repo.closures)
				require.Equal(t, model.SendNotSent, repo.closure.Operation.Attempts[0].Send)
			} else {
				require.Zero(t, repo.closures)
			}
			if kind == "ack_lost_load_failure" {
				require.ErrorIs(t, err, repo.loadErr)
			}
		})
	}
}
