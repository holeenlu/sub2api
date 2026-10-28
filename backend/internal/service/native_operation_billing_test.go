package service

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

type nativeApplicationJournal struct {
	UsageBillingRepository
	// Port methods serialize mutable journal state. Hooks run outside mu;
	// direct fixture assertions read final/input/counters after callers join.
	mu                             sync.Mutex
	t                              *testing.T
	input                          *NativeUsageEvidenceInput
	records, old                   int
	fail                           error
	effects                        *UsageBillingApplyResult
	intents                        map[billing.NativeIntentKey]*NativeUsageIntentRecord
	final                          *NativeIntentFinalizationRecord
	finals                         map[billing.NativeIntentKey]*NativeIntentFinalizationRecord
	stores, loads, closures        int
	closure                        *NativeIntentNotSentInput
	storeErr, storeAckErr, loadErr error
	beforeStore                    func(context.Context, *NativeUsageIntent)
	afterStore                     func()
	recoverPreparation             func(context.Context, *NativeIntentRecoveryInput) (*NativeIntentFinalizationInput, error)
}

func (r *nativeApplicationJournal) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.old++
	return nil, errors.New("unexpected second monetary Apply")
}
func (r *nativeApplicationJournal) RecordNativeUsageEvidence(_ context.Context, input *NativeUsageEvidenceInput) (NativeUsageRecordResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records++
	if r.fail != nil {
		return NativeUsageRecordResult{}, r.fail
	}
	copy, err := PrepareNativeUsageEvidence(*input)
	if err != nil {
		return NativeUsageRecordResult{}, err
	}
	r.input = copy
	disposition := billing.DurablePendingUsage
	if copy.Evidence.Confidence == billing.NativeUsageFinal {
		disposition = billing.UsageRecorded
	}
	if copy.Settlement != nil {
		disposition = billing.NativeSettled
		if copy.Settlement.NeedsPricing() {
			disposition = billing.NativePendingPricing
		}
	}
	return NativeUsageRecordResult{Receipt: billing.NativeUsageReceipt{Identity: copy.Evidence.Identity,
		EvidenceFingerprint: copy.Evidence.Fingerprint(), Disposition: disposition}, ActualApplyResult: r.effects}, nil
}
func (r *nativeApplicationJournal) LoadNativeUsageEvidence(context.Context, billing.Identity) (*NativeUsageEvidenceRecord, error) {
	r.t.Fatal("unexpected evidence lookup in one finalization")
	return nil, nil
}

func nativeApplicationFixture(t *testing.T, configure ...func(*OpenAIGatewayService, *NativeOperationFinalization)) (*OpenAIGatewayService, *nativeApplicationJournal, *openAIRecordUsageLogRepoStub, NativeOperationFinalization) {
	t.Helper()
	logs := &openAIRecordUsageLogRepoStub{inserted: true}
	gateway := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	repo := &nativeApplicationJournal{t: t}
	gateway.usageBillingRepo = repo
	h := [32]byte{3}
	prepared := model.PreparedIdentity{OperationID: "actual-operation", ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 2,
		Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}
	usage := &model.UsageFact{Input: &model.TokenMeasurement{Value: 1000}, Output: &model.TokenMeasurement{Value: 12},
		CacheRead: &model.TokenMeasurement{Value: 100}, CacheWrite: &model.TokenMeasurement{Value: 200}, Confidence: model.UsageFinal, TerminalConfirmed: true}
	input := &OpenAIRecordUsageInput{APIKey: &APIKey{ID: 2, UserID: 3}, User: &User{ID: 3, Role: RoleUser}, Account: &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI},
		PricingAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), Result: &OpenAIForwardResult{RequestID: "observed-provider-request", Model: "gpt-5.1",
			Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 12, CacheReadInputTokens: 100, CacheCreationInputTokens: 200}}}
	finalization := NativeOperationFinalization{Identity: billing.Identity{RequestID: "original-billing-request", APIKeyID: 2},
		Operation: model.OperationResult{OperationID: "actual-operation", UserID: 3, APIKeyID: 2, Protocol: model.ProtocolResponses, OriginalWireDigest: h,
			Attempts: []model.AttemptFact{{Sequence: 1, Identity: prepared, Send: model.SendSent, RequestID: "observed-provider-request", ResponseID: "observed-response", Terminal: model.TerminalCompleted, Usage: usage}}},
		CurrentSequences: []uint64{1}, UsageInput: input}
	for _, setup := range configure {
		setup(gateway, &finalization)
	}
	// This component fixture does not grant a send permit or claim a network
	// send. It captures the original policy through production builders and
	// preserves each generation scope through the real in-memory journal port
	// before handing any observed facts to the application finalizer.
	request, err := gateway.captureNativeIntentRequestPricing(t.Context(), NativeIntentPricingInput{
		APIKey: input.APIKey, User: input.User, Subscription: input.Subscription, APIKeyService: input.APIKeyService,
		PricingAt: input.PricingAt, ChannelUsageFields: input.ChannelUsageFields, QuotaPlatform: input.QuotaPlatform,
		RequestPayloadHash: input.RequestPayloadHash, KnownCanonical: &finalization.Identity,
	})
	require.NoError(t, err)
	port, err := ProvideNativeBillingIntentService(repo)
	require.NoError(t, err)
	for _, attempt := range finalization.Operation.Attempts {
		if attempt.Identity.Purpose == model.DispatchInternalPreparation {
			continue
		}
		result := input.Result
		scope, _, err := gateway.captureNativeIntentAttemptPricing(t.Context(), request, nativeIntentAttemptPricingInput{
			Account: input.Account, CredentialAccount: input.Account, Prepared: attempt.Identity, Sequence: attempt.Sequence, CapturedAt: input.PricingAt,
			Model: result.Model, UpstreamModel: upstreamSentModel(result.Model, result.UpstreamModel),
			BillingModel: result.BillingModel,
		})
		require.NoError(t, err)
		receipt, err := port.PreserveBeforeSend(t.Context(), NativeUsageIntent{
			Key:    billing.NativeIntentKey{OperationID: finalization.Operation.OperationID, APIKeyID: input.APIKey.ID},
			UserID: input.User.ID, OriginalWireDigest: finalization.Operation.OriginalWireDigest, Request: request.Scope, Attempt: scope,
		})
		require.NoError(t, err)
		require.True(t, receipt.Durable())
		for _, sequence := range finalization.CurrentSequences {
			if sequence == attempt.Sequence && sequence > finalization.Intent.AttemptSequence {
				finalization.Intent = receipt.NativeIntentReference
			}
		}
	}
	return gateway, repo, logs, finalization
}

func TestNativeApplicationFinalizerUsesOriginalBuilderAndSingleJournal(t *testing.T) {
	gateway, repo, logs, input := nativeApplicationFixture(t)
	expected, err := gateway.prepareOpenAIUsage(t.Context(), input.UsageInput)
	require.NoError(t, err)
	require.Zero(t, logs.calls, "preparing billing must not persist a usage row")
	expected.log.RequestID = input.Identity.RequestID
	expected.params.UsageLogs = splitOpenAIUsageAuthorization(expected.log, input.UsageInput.Result, input.UsageInput.Account)
	command := buildUsageBillingCommand(input.Identity.RequestID, expected.log, expected.params)
	repo.effects = &UsageBillingApplyResult{Applied: true}
	receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
	require.NoError(t, err)
	require.True(t, receipt.Intent.Durable())
	require.Equal(t, billing.NativeIntentBound, receipt.Intent.Disposition)
	require.NotNil(t, receipt.Canonical)
	require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
	require.Equal(t, 1, repo.records)
	require.Zero(t, repo.old, "the Native ledger must not be followed by a second Apply")
	require.Equal(t, command.RequestFingerprint, repo.input.Settlement.RequestFingerprint)
	require.Equal(t, 700, repo.input.Settlement.InputTokens)
	require.Equal(t, int64(1000), *repo.input.Evidence.Usage.InputTokens)
	require.Equal(t, 1000, repo.input.SettlementUsage.OpenAI.Usage.InputTokens)
	require.Equal(t, 700, repo.input.SettlementUsage.Tokens.InputTokens)
	require.Equal(t, input.Identity.RequestID, repo.input.Settlement.RequestID)
	require.Equal(t, "observed-provider-request", repo.input.Evidence.UpstreamRequestID)
	require.Zero(t, logs.calls, "the real transaction acknowledgement already covers usage insertion")
}

func TestNativeApplicationFinalizerPersistsUnknownWithoutZeroUsage(t *testing.T) {
	for _, confidence := range []model.UsageConfidence{model.UsageUnknown, model.UsagePartial} {
		t.Run(map[model.UsageConfidence]string{model.UsageUnknown: "unknown", model.UsagePartial: "partial"}[confidence], func(t *testing.T) {
			gateway, repo, logs, input := nativeApplicationFixture(t)
			input.Operation.Attempts[0].Terminal = model.TerminalUnknown
			input.Operation.Attempts[0].Usage.Confidence = confidence
			input.Operation.Attempts[0].Usage.TerminalConfirmed = false
			if confidence == model.UsageUnknown {
				input.Operation.Attempts[0].Usage = nil
			}
			input.UsageInput.Result = nil
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
			require.NoError(t, err)
			require.Equal(t, billing.NativeIntentPendingUsage, receipt.Intent.Disposition)
			require.Nil(t, receipt.Canonical)
			require.Nil(t, repo.input.Settlement)
			require.Zero(t, repo.old)
			require.Zero(t, logs.calls)
			if confidence == model.UsageUnknown {
				require.Nil(t, repo.input.Evidence.Usage)
			}
		})
	}
}

func TestNativeApplicationFinalizerArchivesEveryActualAttempt(t *testing.T) {
	gateway, repo, _, input := nativeApplicationFixture(t, func(_ *OpenAIGatewayService, input *NativeOperationFinalization) {
		prior := input.Operation.Attempts[0]
		prior.Send, prior.Usage, prior.Terminal = model.SendAmbiguous, nil, model.TerminalUnknown
		prior.RequestID, prior.ResponseID = "first-upstream-request", ""
		current := input.Operation.Attempts[0]
		current.Sequence = 2
		current.Identity.TargetDigest = [32]byte{7}
		input.Operation.Attempts = []model.AttemptFact{prior, current}
		input.CurrentSequences = []uint64{2}
	})
	require.Equal(t, 2, repo.stores)
	_, err := gateway.RecordNativeOperationUsage(t.Context(), input)
	require.NoError(t, err)
	require.Len(t, repo.input.Observations, 2)
	require.EqualValues(t, 1, repo.input.Observations[0].AttemptSequence)
	require.EqualValues(t, 2, repo.input.Evidence.AttemptSequence)
	require.Equal(t, model.SendAmbiguous, repo.input.Observations[0].SendState)
	require.Nil(t, repo.input.Observations[0].Usage)
	require.Zero(t, repo.old)
}

func TestNativeApplicationFinalizerFailureNeverUsesLegacyApply(t *testing.T) {
	gateway, repo, logs, input := nativeApplicationFixture(t)
	repo.fail = errors.New("fixture journal unavailable")
	receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
	require.ErrorIs(t, err, repo.fail)
	require.False(t, receipt.Intent.Durable())
	require.Zero(t, repo.old)
	require.Zero(t, logs.calls)
}

func TestNativeApplicationFinalizerPreservesSimpleModeNonBilling(t *testing.T) {
	for _, role := range []string{RoleUser, RoleSuperAdmin} {
		t.Run(role, func(t *testing.T) {
			gateway, repo, logs, input := nativeApplicationFixture(t, func(gateway *OpenAIGatewayService, input *NativeOperationFinalization) {
				gateway.cfg.RunMode = config.RunModeSimple
				input.UsageInput.User.Role = role
			})
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
			require.NoError(t, err)
			require.True(t, receipt.Intent.Durable())
			require.Equal(t, billing.NativeIntentBound, receipt.Intent.Disposition)
			require.NotNil(t, receipt.Canonical)
			require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
			command := repo.input.Settlement
			require.NotNil(t, command)
			require.False(t, command.BalanceExempt)
			require.Empty(t, command.Platform)
			require.Nil(t, command.SubscriptionID)
			for _, amount := range []float64{command.BalanceCost, command.SubscriptionCost, command.APIKeyQuotaCost, command.APIKeyRateLimitCost, command.AccountQuotaCost} {
				require.Zero(t, amount, "original log-only command must never acquire monetary effects")
			}
			require.Zero(t, logs.calls, "only the journal transaction may write the log")
			require.Zero(t, repo.old)
		})
	}
}

func TestNativeApplicationLogOnlyJournalFailureDoesNotWriteBestEffortLog(t *testing.T) {
	gateway, repo, logs, input := nativeApplicationFixture(t, func(gateway *OpenAIGatewayService, _ *NativeOperationFinalization) {
		gateway.cfg.RunMode = config.RunModeSimple
	})
	repo.fail = errors.New("log transaction unavailable")
	receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
	require.ErrorIs(t, err, repo.fail)
	require.False(t, receipt.Intent.Durable())
	require.Zero(t, logs.calls)
	require.Zero(t, repo.old)
}

func TestNativeApplicationFinalizerWSSelectsGenerationAndArchivesPreparation(t *testing.T) {
	for _, onlyPreparation := range []bool{false, true} {
		name := "generation"
		if onlyPreparation {
			name = "preparation_only"
		}
		t.Run(name, func(t *testing.T) {
			gateway, repo, logs, input := nativeApplicationFixture(t, func(_ *OpenAIGatewayService, input *NativeOperationFinalization) {
				input.UsageInput.Result.OpenAIWSMode = true
				generation := input.Operation.Attempts[0]
				generation.Identity.Transport = "ws"
				preparation := generation
				preparation.Sequence = 2
				preparation.Identity.Purpose = model.DispatchInternalPreparation
				preparation.Identity.WireDigest = [32]byte{9}
				if onlyPreparation {
					input.Operation.Attempts = []model.AttemptFact{preparation}
					input.CurrentSequences = []uint64{2}
				} else {
					input.Operation.Attempts = []model.AttemptFact{generation, preparation}
					input.CurrentSequences = []uint64{1, 2}
				}
			})
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
			require.Zero(t, repo.old)
			if onlyPreparation {
				require.ErrorIs(t, err, sql.ErrNoRows, "internal preparation has no generation intent to finalize")
				require.False(t, receipt.Intent.Durable())
				require.Nil(t, receipt.Canonical)
				require.Zero(t, repo.stores)
				require.Zero(t, repo.records)
				require.Nil(t, repo.input)
				require.Zero(t, logs.calls)
			} else {
				require.NoError(t, err)
				require.True(t, receipt.Intent.Durable())
				require.NotNil(t, receipt.Canonical)
				require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
				require.Equal(t, 1, repo.stores)
				require.Equal(t, 1, repo.records)
				require.NotNil(t, repo.input.Settlement)
				require.EqualValues(t, 1, repo.input.Evidence.AttemptSequence)
				require.Len(t, repo.input.Observations, 2)
			}
		})
	}
}

func TestNativeApplicationFinalizerPreservesImageOnlyMeasurements(t *testing.T) {
	gateway, repo, _, input := nativeApplicationFixture(t, func(_ *OpenAIGatewayService, input *NativeOperationFinalization) {
		input.UsageInput.Result.Model = "gpt-image-2"
	})
	images := int64(1)
	input.Operation.Attempts[0].Usage = &model.UsageFact{
		ImageInput: &model.TokenMeasurement{Value: 0}, ImageOutput: &model.TokenMeasurement{Value: 9},
		ImageCount: &images, Confidence: model.UsageFinal, TerminalConfirmed: true,
	}
	input.UsageInput.Result.Usage = OpenAIUsage{ImageOutputTokens: 9}
	input.UsageInput.Result.ImageCount = 1
	receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
	require.NoError(t, err)
	require.True(t, receipt.Intent.Durable())
	require.Nil(t, repo.input.Evidence.Usage.InputTokens)
	require.Nil(t, repo.input.Evidence.Usage.OutputTokens)
	require.NotNil(t, repo.input.Evidence.Usage.ImageInputTokens)
	require.Zero(t, *repo.input.Evidence.Usage.ImageInputTokens)
	require.EqualValues(t, 9, *repo.input.Evidence.Usage.ImageOutputTokens)
	require.EqualValues(t, 1, *repo.input.Evidence.Usage.ImageCount)
	require.Equal(t, billing.NativeIntentPendingPreparation, receipt.Intent.Disposition)
	require.Nil(t, receipt.Canonical)
	require.Nil(t, repo.input.Settlement)
	require.Nil(t, repo.input.SettlementUsage)
	require.NotNil(t, repo.final.Preparation)
	require.Equal(t, 9, repo.final.Preparation.Result.Usage.ImageOutputTokens)
	require.Equal(t, 1, repo.final.Preparation.Result.ImageCount)
	require.Zero(t, repo.old)
}

func TestNativeApplicationFinalizerSimpleUsageRejectsStaleCycleMapping(t *testing.T) {
	gateway, repo, logs, input := nativeApplicationFixture(t, func(gateway *OpenAIGatewayService, _ *NativeOperationFinalization) {
		gateway.cfg.RunMode = config.RunModeSimple
	})
	// The current operation observed 2000 tokens; an earlier result still says 1000.
	current := *input.Operation.Attempts[0].Usage
	current.Input = &model.TokenMeasurement{Value: 2000}
	input.Operation.Attempts[0].Usage = &current
	receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
	require.NoError(t, err)
	require.True(t, receipt.Intent.Durable())
	require.Equal(t, billing.NativeIntentPendingPreparation, receipt.Intent.Disposition)
	require.Nil(t, receipt.Canonical)
	require.Equal(t, billing.NativeUsagePreparationUnavailable, repo.final.Reason)
	require.Nil(t, repo.final.Preparation)
	require.Equal(t, billing.NativeUsageFinal, repo.input.Evidence.Confidence)
	require.EqualValues(t, 2000, *repo.input.Evidence.Usage.InputTokens)
	require.Nil(t, repo.input.Settlement)
	require.Nil(t, repo.input.SettlementUsage)
	require.ErrorIs(t, validateNativeObservedUsageMapping(repo.input.Evidence, &UsagePricingRequest{OpenAI: input.UsageInput.Result}), billing.ErrNativeUsageConflict)
	require.Zero(t, logs.calls, "stale-cycle usage must not be written as the current row")
	require.Zero(t, repo.old, "validation must not create a monetary command")
}

func TestNativeApplicationFinalizerKeepsRawReasoningAndValidatesOriginalProjection(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		output, total, normalized int64
		withProof, conflict       bool
	}{
		{"independent", 2, 10, 5, true, false},
		{"included", 5, 10, 5, true, false},
		{"missing_proof", 2, 10, 5, false, true},
		{"wrong_mapping", 2, 10, 6, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gateway, repo, logs, input := nativeApplicationFixture(t)
			usage := &model.UsageFact{Input: &model.TokenMeasurement{Value: 5}, Output: &model.TokenMeasurement{Value: tc.output},
				Confidence: model.UsageFinal, TerminalConfirmed: true}
			if tc.withProof {
				usage.Total, usage.Reasoning = &model.TokenMeasurement{Value: tc.total}, &model.TokenMeasurement{Value: 3}
			}
			input.Operation.Attempts[0].Usage = usage
			input.UsageInput.Result.Usage = OpenAIUsage{InputTokens: 5, OutputTokens: int(tc.normalized)}
			receipt, err := gateway.RecordNativeOperationUsage(t.Context(), input)
			if tc.conflict {
				require.NoError(t, err)
				require.True(t, receipt.Intent.Durable())
				require.Equal(t, billing.NativeIntentPendingPreparation, receipt.Intent.Disposition)
				require.Nil(t, receipt.Canonical)
				require.Equal(t, billing.NativeUsagePreparationUnavailable, repo.final.Reason)
				require.Nil(t, repo.final.Preparation)
				require.Equal(t, billing.NativeUsageFinal, repo.input.Evidence.Confidence)
				require.Equal(t, tc.output, *repo.input.Evidence.Usage.OutputTokens)
				require.Nil(t, repo.input.Settlement)
				require.Nil(t, repo.input.SettlementUsage)
				require.ErrorIs(t, validateNativeObservedUsageMapping(repo.input.Evidence, &UsagePricingRequest{OpenAI: input.UsageInput.Result}), billing.ErrNativeUsageConflict)
				require.Zero(t, logs.calls)
			} else {
				require.NoError(t, err)
				require.True(t, receipt.Intent.Durable())
				require.NotNil(t, receipt.Canonical)
				require.Equal(t, billing.NativeSettled, receipt.Canonical.Receipt.Disposition)
				require.Equal(t, tc.output, *repo.input.Evidence.Usage.OutputTokens, "raw output is not rewritten to a canonical charge")
				require.Equal(t, tc.total, *repo.input.Evidence.Usage.TotalTokens)
				require.EqualValues(t, 3, *repo.input.Evidence.Usage.ReasoningTokens)
				require.EqualValues(t, tc.normalized, repo.input.Settlement.OutputTokens)
			}
			require.Zero(t, repo.old)
		})
	}
}
