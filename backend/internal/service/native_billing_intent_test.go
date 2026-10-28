package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func nativeIntentFixture() NativeUsageIntent {
	at := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	h := [32]byte{3}
	exempt := false
	return NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: "operation", APIKeyID: 2}, UserID: 3, OriginalWireDigest: h,
		Request: NativeIntentRequestScope{Mode: UsageBillingModeFull, PricingAt: at, UserRole: "user", OriginalModel: "gpt-5.1", BaseMultiplier: 1, TokenMultiplier: 2, ImageMultiplier: 1, VideoMultiplier: 1},
		Attempt: NativeIntentAttemptScope{Sequence: 1, CapturedAt: at.Add(time.Second), BaselineModel: "gpt-5.1", CandidateModels: []string{"gpt-5.1"},
			Prepared:      model.PreparedIdentity{OperationID: "operation", ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 2, Mode: "native", Transport: "http", GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h},
			FrozenPricing: &NativeFrozenPricing{BalanceExempt: &exempt, Policy: &UsageBillingPricingSnapshot{Model: "gpt-5.1", PricingAt: at, Balance: true, AccountRateMultiplier: 0.5}, Resolved: &NativeResolvedPricingSnapshot{Mode: BillingModeToken, Source: PricingSourceGroup, BasePricing: &ModelPricing{InputPricePerToken: 0.125}, CacheWriteFlatOverride: true}}}}
}

func TestNativeIntentScopePreservesFrozenValuesAndExplicitZero(t *testing.T) {
	original := nativeIntentFixture()
	original.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 0
	prepared, err := PrepareNativeUsageIntent(original)
	require.NoError(t, err)
	original.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 99
	require.Zero(t, prepared.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	restored, err := prepared.Attempt.FrozenPricing.Resolved.Restore()
	require.NoError(t, err)
	require.True(t, restored.BasePricing.cacheWriteFlatOverride)
	require.Nil(t, prepared.Attempt.FrozenPricing.Policy.Request, "pre-send scope must not invent usage")
	encoded, err := json.Marshal(prepared)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "RequestID", "a technical intent cannot carry a preselected monetary request ID")
	require.Equal(t, 0.5, prepared.Attempt.FrozenPricing.Policy.AccountRateMultiplier)
}

func TestNativeIntentMergeKeepsRootAndActualAttemptScopes(t *testing.T) {
	first := nativeIntentFixture()
	record, err := MergeNativeUsageIntent(nil, first)
	require.NoError(t, err)
	replay, err := MergeNativeUsageIntent(record, first)
	require.NoError(t, err)
	require.Len(t, replay.Attempts, 1)
	next, err := PrepareNativeUsageIntent(first)
	require.NoError(t, err)
	next.Attempt.Sequence = 2
	next.Attempt.Prepared.TargetDigest = [32]byte{8}
	next.Attempt.FrozenPricing.Policy.AccountRateMultiplier = 3
	merged, err := MergeNativeUsageIntent(record, *next)
	require.NoError(t, err)
	require.Len(t, merged.Attempts, 2)
	require.EqualValues(t, 1, merged.Attempts[0].Sequence)
	require.Equal(t, 0.5, merged.Attempts[0].FrozenPricing.Policy.AccountRateMultiplier)
	changed, err := PrepareNativeUsageIntent(first)
	require.NoError(t, err)
	changed.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 9
	_, err = MergeNativeUsageIntent(merged, *changed)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	changed, err = PrepareNativeUsageIntent(first)
	require.NoError(t, err)
	changed.Request.BaseMultiplier = 9
	_, err = MergeNativeUsageIntent(merged, *changed)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	ref, err := first.NativeIntentReference()
	require.NoError(t, err)
	raw, err := json.Marshal(merged)
	require.NoError(t, err)
	decoded, err := DecodeNativeUsageIntentRecord(raw, first.Key, ref.IntentFingerprint)
	require.NoError(t, err)
	require.Len(t, decoded.Attempts, 2)
	_, err = DecodeNativeUsageIntentRecord(raw, first.Key, "wrong")
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
}

func TestNativeIntentRejectsIncompleteOrInventedPolicy(t *testing.T) {
	for _, sample := range []string{"mode", "policy", "known_exemption", "unknown_price", "empty_price_card", "fake_usage", "wrong_owner", "wrong_transport", "internal_prepare", "response_view", "log_only_debit", "key_window_debit"} {
		t.Run(sample, func(t *testing.T) {
			in := nativeIntentFixture()
			switch sample {
			case "mode":
				in.Request.Mode = ""
			case "policy":
				in.Attempt.FrozenPricing.Policy = nil
			case "known_exemption":
				in.Attempt.FrozenPricing.BalanceExempt = nil
			case "unknown_price":
				in.Attempt.FrozenPricing.Resolved = nil
			case "empty_price_card":
				in.Attempt.FrozenPricing.Resolved = &NativeResolvedPricingSnapshot{}
			case "fake_usage":
				in.Attempt.FrozenPricing.Policy.Request = &UsagePricingRequest{}
			case "wrong_owner":
				in.Attempt.Prepared.APIKeyID++
			case "wrong_transport":
				in.Attempt.Prepared.Transport = "cloud"
			case "internal_prepare":
				in.Attempt.Prepared.Purpose = model.DispatchInternalPreparation
			case "response_view":
				in.Request.BillingModelSource = BillingModelSourceResponse
			case "log_only_debit":
				in.Request.Mode = UsageBillingModeLogOnly
			case "key_window_debit":
				in.Request.Mode = UsageBillingModeKeyWindowOnly
			}
			_, err := PrepareNativeUsageIntent(in)
			require.Error(t, err)
		})
	}
	for _, mode := range []UsageBillingMode{UsageBillingModeLogOnly, UsageBillingModeKeyWindowOnly, UsageBillingModeFull} {
		in := nativeIntentFixture()
		in.Request.Mode = mode
		in.Attempt.FrozenPricing.Policy.Balance = mode == UsageBillingModeFull
		in.Attempt.FrozenPricing.Policy.KeyRateLimit = mode == UsageBillingModeKeyWindowOnly
		_, err := PrepareNativeUsageIntent(in)
		require.NoError(t, err)
	}
	in := nativeIntentFixture()
	in.Attempt.FrozenPricing.Resolved = nil
	in.Attempt.FrozenPricing.Policy.Pending = true
	_, err := PrepareNativeUsageIntent(in)
	require.NoError(t, err, "explicitly missing price is distinct from a zero quote")
}

type intentServiceRepo struct {
	calls int
	err   error
}

func (r *intentServiceRepo) StoreNativeUsageIntent(_ context.Context, input *NativeUsageIntent) (billing.NativeIntentReceipt, error) {
	r.calls++
	ref, err := input.NativeIntentReference()
	if err != nil {
		return billing.NativeIntentReceipt{}, err
	}
	return billing.NativeIntentReceipt{NativeIntentReference: ref, Disposition: billing.NativeIntentPreserved}, r.err
}
func (*intentServiceRepo) LoadNativeUsageIntent(context.Context, billing.NativeIntentKey) (*NativeUsageIntentRecord, error) {
	return nil, errors.New("no stored fixture")
}
func (r *intentServiceRepo) CloseNativeUsageIntent(_ context.Context, input *NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error) {
	r.calls++
	ref, err := input.NativeIntentClosureReference()
	if err != nil {
		return billing.NativeIntentClosureReceipt{}, err
	}
	return billing.NativeIntentClosureReceipt{NativeIntentClosureReference: ref, ClosedNotSent: true}, r.err
}
func TestNativeIntentServiceNeverClaimsDurabilityOnError(t *testing.T) {
	repo := &intentServiceRepo{err: errors.New("SQL commit uncertain")}
	svc, err := NewNativeBillingIntentService(repo)
	require.NoError(t, err)
	receipt, err := svc.PreserveBeforeSend(t.Context(), nativeIntentFixture())
	require.ErrorIs(t, err, repo.err)
	require.False(t, receipt.Durable())
	require.Equal(t, 1, repo.calls)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.PreserveBeforeSend(ctx, nativeIntentFixture())
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.calls)
	var absent *intentServiceRepo
	_, err = NewNativeBillingIntentService(absent)
	require.Error(t, err)
}
