package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type nativeEvidenceRepositoryStub struct {
	input   *NativeUsageEvidenceInput
	receipt billing.NativeUsageReceipt
	actual  *UsageBillingApplyResult
	err     error
	calls   int
}

func (s *nativeEvidenceRepositoryStub) RecordNativeUsageEvidence(_ context.Context, input *NativeUsageEvidenceInput) (NativeUsageRecordResult, error) {
	s.calls++
	s.input = input
	return NativeUsageRecordResult{Receipt: s.receipt, ActualApplyResult: s.actual}, s.err
}
func (s *nativeEvidenceRepositoryStub) LoadNativeUsageEvidence(context.Context, billing.Identity) (*NativeUsageEvidenceRecord, error) {
	return nil, s.err
}
func nativeServiceEvidence() NativeUsageEvidenceInput {
	h := sha256.Sum256([]byte("wire"))
	return NativeUsageEvidenceInput{Evidence: billing.NativeUsageEvidence{Identity: billing.Identity{RequestID: "native-root", APIKeyID: 2}, OperationID: "real-operation", UserID: 3, OriginalWireDigest: h, SendState: model.SendUnknown}}
}
func TestNativeUsageServicePreservesUnknownAndContext(t *testing.T) {
	input := nativeServiceEvidence()
	prepared, err := PrepareNativeUsageEvidence(input)
	require.NoError(t, err)
	store := &nativeEvidenceRepositoryStub{receipt: billing.NativeUsageReceipt{Identity: input.Evidence.Identity, EvidenceFingerprint: prepared.Evidence.Fingerprint(), Disposition: billing.DurablePendingUsage}}
	svc, err := NewNativeUsageEvidenceService(store)
	require.NoError(t, err)
	receipt, err := svc.Record(t.Context(), input)
	require.NoError(t, err)
	require.True(t, receipt.Receipt.Durable())
	require.Nil(t, store.input.Evidence.Usage)
	require.Nil(t, store.input.Settlement)
	require.Empty(t, store.input.Evidence.UpstreamRequestID)
	require.Empty(t, store.input.Evidence.UpstreamResponseID)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	receipt, err = svc.Record(ctx, input)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, receipt.Receipt.Durable())
	require.Equal(t, 1, store.calls)
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer stop()
	receipt, err = svc.Record(cleanup, input)
	require.NoError(t, err)
	require.True(t, receipt.Receipt.Durable())
	require.Equal(t, 2, store.calls, "the actual Finalizer owns the bounded cancellation cleanup")
}
func TestNativeUsageServiceCannotFabricateDurability(t *testing.T) {
	for _, name := range []string{"SQL failure", "empty success", "foreign receipt"} {
		t.Run(name, func(t *testing.T) {
			store := &nativeEvidenceRepositoryStub{}
			if name == "SQL failure" {
				store.err = errors.New("SQL failed")
			}
			if name == "foreign receipt" {
				store.receipt = billing.NativeUsageReceipt{Identity: billing.Identity{RequestID: "foreign", APIKeyID: 2}, EvidenceFingerprint: "unrelated", Disposition: billing.NativeSettled}
			}
			svc, err := NewNativeUsageEvidenceService(store)
			require.NoError(t, err)
			receipt, err := svc.Record(t.Context(), nativeServiceEvidence())
			require.Error(t, err)
			require.False(t, receipt.Receipt.Durable())
		})
	}
	var typedNil *nativeEvidenceRepositoryStub
	_, err := NewNativeUsageEvidenceService(typedNil)
	require.Error(t, err)
	_, err = ProvideNativeUsageEvidenceService(nil)
	require.Error(t, err)
}
func TestNativeUsageFrozenPricingPreservesPrivateRulesAndOwnership(t *testing.T) {
	original := &ResolvedPricing{Mode: BillingModeToken, Source: PricingSourceGroup, BasePricing: &ModelPricing{cacheWriteFlatOverride: true, InputPricePerToken: 0.25, ReasoningEffortMultipliers: map[string]float64{"high": 1.5}}, channelPricing: &ChannelModelPricing{}, longContextPricingEnabled: true}
	frozen, err := FreezeNativeUsagePricing(original, &UsageBillingPricingSnapshot{Balance: true}, false)
	require.NoError(t, err)
	original.BasePricing.InputPricePerToken = 9
	original.BasePricing.ReasoningEffortMultipliers["high"] = 9
	restored, err := frozen.Resolved.Restore()
	require.NoError(t, err)
	require.Equal(t, 0.25, restored.BasePricing.InputPricePerToken)
	require.Equal(t, 1.5, restored.BasePricing.ReasoningEffortMultipliers["high"])
	require.True(t, restored.BasePricing.cacheWriteFlatOverride)
	require.True(t, restored.longContextPricingEnabled)
	restored.BasePricing.InputPricePerToken = 8
	require.Equal(t, 0.25, frozen.Resolved.BasePricing.InputPricePerToken)
	input := nativeServiceEvidence()
	input.FrozenPricing = frozen
	prepared, err := PrepareNativeUsageEvidence(input)
	require.NoError(t, err)
	frozen.Resolved.BasePricing.InputPricePerToken = 7
	require.Equal(t, 0.25, prepared.FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	prepared.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 6
	_, err = PrepareNativeUsageEvidence(*prepared)
	require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
}

func TestNativeUsageServiceRejectsUnprovenSettlement(t *testing.T) {
	for _, name := range []string{"unknown", "partial", "native_lower_bound", "unfrozen_exemption"} {
		t.Run(name, func(t *testing.T) {
			input := nativeServiceEvidence()
			e := &input.Evidence
			h := e.OriginalWireDigest
			e.Prepared = &model.PreparedIdentity{OperationID: e.OperationID, ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: e.UserID, APIKeyID: e.Identity.APIKeyID, Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}
			zero := int64(0)
			exempt := false
			e.SendState = model.SendSent
			e.Confidence = billing.NativeUsageFinal
			e.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, InputTokens: &zero, OutputTokens: &zero}
			input.FrozenPricing = &NativeFrozenPricing{BalanceExempt: &exempt}
			input.Settlement = &UsageBillingCommand{RequestID: e.Identity.RequestID, APIKeyID: e.Identity.APIKeyID, UserID: e.UserID, AccountID: 7, UsageLog: &UsageLog{RequestID: e.Identity.RequestID, APIKeyID: e.Identity.APIKeyID, UserID: e.UserID, AccountID: 7}}
			switch name {
			case "unknown":
				e.Confidence = billing.NativeUsageUnknown
			case "partial":
				e.Confidence = billing.NativeUsagePartial
			case "native_lower_bound":
				input.Settlement.UsageLog.UsageLowerBound = true
			case "unfrozen_exemption":
				input.FrozenPricing.BalanceExempt = nil
			}
			store := &nativeEvidenceRepositoryStub{}
			svc, err := NewNativeUsageEvidenceService(store)
			require.NoError(t, err)
			receipt, err := svc.Record(t.Context(), input)
			require.Error(t, err)
			require.False(t, receipt.Receipt.Durable())
			require.Zero(t, store.calls)
		})
	}
}

func nativeBatchTestInput(t *testing.T) NativeUsageEvidenceInput {
	t.Helper()
	in := nativeServiceEvidence()
	h := in.Evidence.OriginalWireDigest
	p := model.PreparedIdentity{OperationID: in.Evidence.OperationID, ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: in.Evidence.UserID, APIKeyID: in.Evidence.Identity.APIKeyID, Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}
	first, err := NativeUsageEvidenceFromAttempt(in.Evidence, model.AttemptFact{Sequence: 1, Identity: p, Send: model.SendAmbiguous})
	require.NoError(t, err)
	second := first
	second.AttemptSequence = 2
	second.UpstreamRequestID = "actual-redirect-request"
	third := second
	third.AttemptSequence = 3
	third.Prepared = &p
	p.ResourceID = 8
	p.Owner.AccountID = 8
	p.TargetDigest = sha256.Sum256([]byte("redirect-target"))
	third.UpstreamRequestID = "actual-account-request"
	third.Terminal = model.TerminalCompleted
	third.TerminalConfirmed = true
	third.Confidence = billing.NativeUsageFinal
	third.SendState = model.SendSent
	third.UpstreamResponseID = "actual-response"
	n, o := int64(10), int64(0)
	third.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "operation.usage", InputTokens: &n, OutputTokens: &o}
	in.Evidence = third
	in.Observations = []billing.NativeUsageEvidence{first, second, third}
	v, err := PrepareNativeUsageEvidence(in)
	require.NoError(t, err)
	return *v
}

func TestNativeUsageBatchSeparatesActualAttemptsAndReplays(t *testing.T) {
	input := nativeBatchTestInput(t)
	saved, err := MergeNativeUsageRecord(nil, &input)
	require.NoError(t, err)
	require.Len(t, saved.Observations, 3)
	require.NotEqual(t, saved.Evidence.Identity.RequestID, saved.Evidence.OperationID)
	require.Equal(t, uint64(3), saved.Evidence.AttemptSequence)
	require.Equal(t, int64(7), saved.Observations[0].Prepared.ResourceID)
	require.Equal(t, int64(8), saved.Observations[2].Prepared.ResourceID)
	require.Empty(t, saved.Observations[0].UpstreamResponseID)
	replay, err := MergeNativeUsageRecord(saved, &input)
	require.NoError(t, err)
	require.Equal(t, saved, replay)
	for _, field := range []string{"prepare", "request", "root", "pricing", "usage"} {
		t.Run(field, func(t *testing.T) {
			copy, err := PrepareNativeUsageEvidence(input)
			require.NoError(t, err)
			switch field {
			case "prepare":
				copy.Observations[0].Prepared.ResourceID++
			case "request":
				copy.Observations[1].UpstreamRequestID = "wrong-request"
			case "root":
				copy.Observations[0].Identity.RequestID = "other-root"
			case "pricing":
				copy.Observations[0].PricingDigest = sha256.Sum256([]byte("other-price"))
			case "usage":
				*copy.Observations[2].Usage.InputTokens = 99
			}
			_, err = MergeNativeUsageRecord(saved, copy)
			require.Error(t, err)
			require.Len(t, saved.Observations, 3)
		})
	}
	// An earlier unknown attempt remains a replay; it cannot erase final usage.
	stale := input
	stale.Evidence = input.Observations[0]
	stale.Observations = []billing.NativeUsageEvidence{stale.Evidence}
	replay, err = MergeNativeUsageRecord(saved, &stale)
	require.NoError(t, err)
	require.Equal(t, uint64(3), replay.Evidence.AttemptSequence)
}

func TestNativeUsageAttemptFactsKeepConfidenceAndPresence(t *testing.T) {
	batch := nativeBatchTestInput(t)
	root := nativeServiceEvidence().Evidence
	p := *batch.Observations[0].Prepared
	for _, confidence := range []model.UsageConfidence{model.UsageUnknown, model.UsagePartial, model.UsageConfirmedLowerBound, model.UsageFinal} {
		images := int64(2)
		u := &model.UsageFact{Total: &model.TokenMeasurement{Value: 9}, Reasoning: &model.TokenMeasurement{Value: 0}, Input: &model.TokenMeasurement{Value: 0}, ImageInput: &model.TokenMeasurement{Value: 0}, ImageOutput: &model.TokenMeasurement{Value: 9}, ImageCount: &images, Confidence: confidence, TerminalConfirmed: false}
		e, err := NativeUsageEvidenceFromAttempt(root, model.AttemptFact{Sequence: 1, Identity: p, Send: model.SendSent, Usage: u})
		require.NoError(t, err)
		require.NotNil(t, e.Usage.InputTokens)
		require.Nil(t, e.Usage.OutputTokens)
		require.EqualValues(t, 9, *e.Usage.TotalTokens)
		require.NotNil(t, e.Usage.ReasoningTokens)
		require.Zero(t, *e.Usage.ReasoningTokens)
		require.NotNil(t, e.Usage.ImageInputTokens)
		require.Zero(t, *e.Usage.ImageInputTokens)
		require.EqualValues(t, 9, *e.Usage.ImageOutputTokens)
		require.EqualValues(t, 2, *e.Usage.ImageCount)
		require.False(t, e.TerminalConfirmed)
		require.Equal(t, confidence == model.UsageConfirmedLowerBound, e.ConfirmedLowerBound)
		u.Input.Value = 7
		u.ImageOutput.Value = 10
		u.Total.Value, u.Reasoning.Value = 10, 1
		images = 3
		require.Zero(t, *e.Usage.InputTokens)
		require.EqualValues(t, 9, *e.Usage.ImageOutputTokens)
		require.EqualValues(t, 2, *e.Usage.ImageCount)
		require.EqualValues(t, 9, *e.Usage.TotalTokens)
		require.Zero(t, *e.Usage.ReasoningTokens)
	}
	e, err := NativeUsageEvidenceFromAttempt(root, model.AttemptFact{Sequence: 1, Identity: p, Send: model.SendAmbiguous})
	require.NoError(t, err)
	require.Nil(t, e.Usage)
	require.Empty(t, e.UpstreamResponseID)
}

func TestNativeUsageBuilderMapsRawCacheCountsWithoutRewritingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		raw, read, write, want int
	}{{"cache_read_and_write", 1000, 100, 200, 700}, {"clamped", 2, 5, 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			gateway := newOpenAIRecordUsageServiceForTest(logs, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			result := &OpenAIForwardResult{RequestID: "observed-upstream-id", Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: tc.raw, OutputTokens: 1, CacheReadInputTokens: tc.read, CacheCreationInputTokens: tc.write}}
			key := &APIKey{ID: 2}
			user := &User{ID: 3}
			account := &Account{ID: 7}
			require.NoError(t, gateway.RecordUsage(t.Context(), &OpenAIRecordUsageInput{Result: result, APIKey: key, User: user, Account: account}))
			cmd := recordUsageBillingCommand(t, gateway.usageBillingRepo)
			require.Equal(t, tc.want, cmd.InputTokens)
			input := nativeServiceEvidence()
			e := &input.Evidence
			e.Identity.RequestID = cmd.RequestID
			h := e.OriginalWireDigest
			e.Prepared = &model.PreparedIdentity{OperationID: e.OperationID, ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 2, Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}
			raw, out, read, write := int64(tc.raw), int64(1), int64(tc.read), int64(tc.write)
			e.AttemptSequence = 1
			e.SendState = model.SendSent
			e.Confidence = billing.NativeUsageFinal
			e.TerminalConfirmed = true
			e.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, InputTokens: &raw, OutputTokens: &out, CacheReadTokens: &read, CacheCreationTokens: &write}
			input.Settlement = cmd
			var err error
			input.FrozenPricing, err = FreezeNativeUsagePricing(nil, nil, cmd.BalanceExempt)
			require.NoError(t, err)
			// Snapshot the same original builder's normalized output; no new normalization formula.
			input.SettlementUsage = openAIPricingRequest(result, key, []string{result.Model}, UsageTokens{InputTokens: cmd.InputTokens, OutputTokens: cmd.OutputTokens, CacheReadTokens: cmd.CacheReadTokens, CacheCreationTokens: cmd.CacheCreationTokens}, 1, 1, 1, 1, "", nil, false, false, time.Now())
			fingerprint := cmd.RequestFingerprint
			prepared, err := PrepareNativeUsageEvidence(input)
			require.NoError(t, err)
			require.Equal(t, int64(tc.raw), *prepared.Evidence.Usage.InputTokens)
			require.Equal(t, tc.want, prepared.Settlement.UsageLog.InputTokens)
			require.Equal(t, fingerprint, prepared.Settlement.RequestFingerprint)
			for _, kind := range []string{"raw", "normalized", "unconfirmed", "missing_mapping"} {
				t.Run(kind, func(t *testing.T) {
					changed, err := PrepareNativeUsageEvidence(input)
					require.NoError(t, err)
					switch kind {
					case "raw":
						changed.SettlementUsage.OpenAI.Usage.InputTokens++
					case "normalized":
						changed.SettlementUsage.Tokens.InputTokens++
					case "unconfirmed":
						changed.Evidence.TerminalConfirmed = false
						changed.Observations = nil
					case "missing_mapping":
						changed.SettlementUsage = nil
					}
					_, err = PrepareNativeUsageEvidence(*changed)
					require.Error(t, err)
				})
			}
		})
	}
}

func TestNativeUsageRecordResultPreservesActualApplyOutcome(t *testing.T) {
	input, err := PrepareNativeUsageEvidence(nativeServiceEvidence())
	require.NoError(t, err)
	balance := -0.25
	actual := &UsageBillingApplyResult{Applied: true, APIKeyQuotaExhausted: true, NewBalance: &balance, BalanceOverdrafted: true, QuotaState: &AccountQuotaState{TotalUsed: 8, TotalLimit: 7, DailyUsed: 3, DailyLimit: 2, WeeklyUsed: 5, WeeklyLimit: 4}}
	failure := errors.New("receipt read failed after commit")
	for _, tc := range []struct {
		name        string
		actual      *UsageBillingApplyResult
		disposition billing.NativeUsageDisposition
		err         error
	}{
		{"committed", actual, billing.NativeSettled, nil},
		{"post_commit_read_failed", actual, billing.SettlementPending, failure},
		{"deduplicated_transaction", &UsageBillingApplyResult{Applied: false}, billing.NativeSettled, nil},
		{"settled_replay_without_commit", nil, billing.NativeSettled, nil},
		{"unknown", nil, billing.DurablePendingUsage, nil},
		{"pricing_pending", nil, billing.NativePendingPricing, nil},
		{"rollback", nil, billing.SettlementPending, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &nativeEvidenceRepositoryStub{receipt: billing.NativeUsageReceipt{Identity: input.Evidence.Identity, EvidenceFingerprint: input.Evidence.Fingerprint(), Disposition: tc.disposition}, actual: tc.actual, err: tc.err}
			svc, err := NewNativeUsageEvidenceService(store)
			require.NoError(t, err)
			result, err := svc.Record(t.Context(), *input)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, store.receipt, result.Receipt)
			if tc.actual == nil {
				require.Nil(t, result.ActualApplyResult)
			} else {
				require.Same(t, tc.actual, result.ActualApplyResult)
			}
			require.Equal(t, tc.actual, result.ActualApplyResult)
		})
	}
	t.Run("invalid_receipt_does_not_erase_actual_commit_or_original_error", func(t *testing.T) {
		store := &nativeEvidenceRepositoryStub{receipt: billing.NativeUsageReceipt{Identity: billing.Identity{RequestID: "other", APIKeyID: 2}, EvidenceFingerprint: "foreign", Disposition: billing.NativeSettled}, actual: actual, err: failure}
		svc, err := NewNativeUsageEvidenceService(store)
		require.NoError(t, err)
		result, err := svc.Record(t.Context(), *input)
		require.ErrorIs(t, err, billing.ErrNativeUsageConflict)
		require.ErrorIs(t, err, failure)
		require.False(t, result.Receipt.Durable())
		require.Same(t, actual, result.ActualApplyResult)
	})
}
