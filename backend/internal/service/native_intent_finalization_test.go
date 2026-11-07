package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func nativeI2ServiceIntent() NativeUsageIntent {
	in := nativeIntentFixture()
	imageCountBilling := false
	freeFastEligible := false
	in.Request.Billing = &NativeIntentRequestBillingPolicy{Version: 1, QuotaPlatform: PlatformOpenAI, GroupImageCountBilling: &imageCountBilling}
	in.Attempt.Billing = &NativeIntentAttemptBillingPolicy{Version: 1, AccountType: AccountTypeAPIKey, AccountPlatform: PlatformOpenAI, FreeFastEligible: &freeFastEligible, Stats: &NativeIntentStatsPricing{State: NativeIntentStatsNotApplicable}}
	return in
}

func nativeI2ServiceInput(t *testing.T, intent NativeUsageIntent, final bool) NativeIntentFinalizationInput {
	t.Helper()
	ref, err := intent.NativeIntentReference()
	require.NoError(t, err)
	digest, err := hex.DecodeString(ref.ScopeDigest)
	require.NoError(t, err)
	p := intent.Attempt.Prepared
	e := billing.NativeUsageEvidence{Version: 2, Identity: billing.Identity{RequestID: "actual-canonical", APIKeyID: intent.Key.APIKeyID}, OperationID: intent.Key.OperationID,
		UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest, Prepared: &p, AttemptSequence: intent.Attempt.Sequence,
		AttemptScopeDigest: [32]byte(digest), SendState: model.SendNotSent}
	in := NativeIntentFinalizationInput{Intent: ref, Evidence: NativeUsageEvidenceInput{Intent: &ref, Evidence: e, FrozenPricing: intent.Attempt.FrozenPricing}}
	if final {
		count := int64(10)
		in.Evidence.Evidence.SendState = model.SendSent
		in.Evidence.Evidence.Confidence = billing.NativeUsageFinal
		in.Evidence.Evidence.TerminalConfirmed = true
		in.Evidence.Evidence.Terminal = model.TerminalCompleted
		in.Evidence.Evidence.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "response.usage", InputTokens: &count}
		in.Preparation = &NativeUsagePreparation{Result: NativeUsagePreparationResult{Model: "gpt-5.1", Usage: OpenAIUsage{InputTokens: 10}}}
		in.Reason = billing.NativeBuilderUnavailable
	}
	prepared, err := PrepareNativeIntentFinalization(in)
	require.NoError(t, err)
	return *prepared
}

func TestNativeIntentFinalizationKeepsRootAcrossNotSentRepreparePrices(t *testing.T) {
	first := nativeI2ServiceIntent()
	scopes, err := MergeNativeUsageIntent(nil, first)
	require.NoError(t, err)
	initial := nativeI2ServiceInput(t, first, false)
	saved, err := MergeNativeIntentFinalization(nil, &initial)
	require.NoError(t, err)
	second, err := PrepareNativeUsageIntent(first)
	require.NoError(t, err)
	second.Attempt.Sequence = 2
	second.Attempt.Prepared.ResourceID = 8
	second.Attempt.Prepared.Owner.AccountID = 8
	second.Attempt.Prepared.TargetDigest = sha256.Sum256([]byte("actual second target"))
	second.Attempt.FrozenPricing.Policy.AccountRateMultiplier = 3
	second.Attempt.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 0.75
	scopes, err = MergeNativeUsageIntent(scopes, *second)
	require.NoError(t, err)
	final := nativeI2ServiceInput(t, *second, true)
	final.Evidence.Observations = []billing.NativeUsageEvidence{initial.Evidence.Evidence, final.Evidence.Evidence}
	normalized, err := PrepareNativeIntentFinalization(final)
	require.NoError(t, err)
	require.NoError(t, ValidateNativeIntentFinalization(scopes, normalized))
	require.Equal(t, initial.Evidence.Evidence.Fingerprint(), normalized.Evidence.Evidence.Fingerprint(), "root price identity is the request intent, not the selected account card")
	require.NotEqual(t, normalized.Evidence.Observations[0].AttemptScopeDigest, normalized.Evidence.Observations[1].AttemptScopeDigest)
	merged, err := MergeNativeIntentFinalization(saved, normalized)
	require.NoError(t, err)
	require.Len(t, merged.Evidence.Observations, 2)
	require.Equal(t, model.SendNotSent, merged.Evidence.Observations[0].SendState)
	require.Equal(t, second.Attempt.FrozenPricing, merged.Evidence.FrozenPricing)
	require.Equal(t, 2, merged.Evidence.Version)
	require.Equal(t, 0.125, scopes.Attempts[0].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, 0.75, scopes.Attempts[1].FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	raw, err := json.Marshal(merged)
	require.NoError(t, err)
	fingerprint, err := merged.Fingerprint()
	require.NoError(t, err)
	restored, err := DecodeNativeIntentFinalization(raw, first.Key, fingerprint)
	require.NoError(t, err)
	require.Equal(t, merged, restored)

	for _, kind := range []string{"old_attempt_selected_digest", "selected_old_price", "changed_root_price", "changed_old_prepare"} {
		t.Run(kind, func(t *testing.T) {
			bad, err := PrepareNativeIntentFinalization(*normalized)
			require.NoError(t, err)
			switch kind {
			case "old_attempt_selected_digest":
				bad.Evidence.Observations[0].AttemptScopeDigest = bad.Evidence.Evidence.AttemptScopeDigest
			case "selected_old_price":
				bad.Evidence.FrozenPricing = first.Attempt.FrozenPricing
			case "changed_root_price":
				bad.Evidence.Observations[0].PricingDigest = sha256.Sum256([]byte("selected-price"))
			case "changed_old_prepare":
				bad.Evidence.Observations[0].Prepared.TargetDigest = [32]byte{99}
			}
			require.ErrorIs(t, ValidateNativeIntentFinalization(scopes, bad), billing.ErrNativeIntentConflict)
		})
	}
}

func TestNativeIntentFinalizationRetainsUnpreservedNotSentObservation(t *testing.T) {
	for _, kind := range []string{"unsent", "sent", "ambiguous", "unknown", "response", "terminal", "usage", "scope"} {
		t.Run(kind, func(t *testing.T) {
			intent := nativeI2ServiceIntent()
			stored, err := MergeNativeUsageIntent(nil, intent)
			require.NoError(t, err)
			input := nativeI2ServiceInput(t, intent, true)
			unsaved := input.Evidence.Evidence
			p := *unsaved.Prepared
			p.TargetDigest = [32]byte{42}
			unsaved.Prepared = &p
			unsaved.AttemptSequence, unsaved.AttemptScopeDigest = 2, [32]byte{}
			unsaved.SendState, unsaved.Confidence, unsaved.Usage = model.SendNotSent, billing.NativeUsageUnknown, nil
			unsaved.Terminal, unsaved.TerminalConfirmed = model.TerminalUnknown, false
			switch kind {
			case "sent":
				unsaved.SendState = model.SendSent
			case "ambiguous":
				unsaved.SendState = model.SendAmbiguous
			case "unknown":
				unsaved.SendState = model.SendUnknown
			case "response":
				unsaved.UpstreamResponseID = "actual-response"
			case "terminal":
				unsaved.Terminal, unsaved.TerminalConfirmed = model.TerminalFailed, true
			case "usage":
				zero := int64(0)
				unsaved.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, InputTokens: &zero}
			case "scope":
				unsaved.AttemptScopeDigest = [32]byte{42}
			}
			input.Evidence.Observations = append(input.Evidence.Observations, unsaved)
			prepared, err := PrepareNativeIntentFinalization(input)
			if err == nil {
				err = ValidateNativeIntentFinalization(stored, prepared)
			}
			if kind != "unsent" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			merged, err := MergeNativeIntentFinalization(nil, prepared)
			require.NoError(t, err)
			require.Len(t, merged.Evidence.Observations, 2)
			require.Equal(t, unsaved, merged.Evidence.Observations[1])
			require.Equal(t, uint64(1), merged.Intent.AttemptSequence, "unsaved observation cannot become the pricing authority")
		})
	}
}

func TestNativeIntentPreparationWhitelistPreservesEndpointAndCompleteMeasurements(t *testing.T) {
	usage := OpenAIUsage{InputTokens: 100, OutputTokens: 10, ImageInputTokens: 7, ImageCacheReadTokens: 3, ImageOutputTokens: 5,
		CacheCreationInputTokens: 20, CacheCreation5mTokens: 2, CacheCreation1hTokens: 3, CacheCreation30mTokens: 7, CacheCreation12hTokens: 8, CacheReadInputTokens: 30}
	in := &OpenAIRecordUsageInput{User: &User{Email: "must-not-persist-email", PasswordHash: "must-not-persist-password"},
		APIKey: &APIKey{Key: "must-not-persist-key"}, Account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"access_token": "must-not-persist-token"}},
		UserAgent: "codex-client/test", IPAddress: "127.0.0.1", RequestPayloadHash: "original-payload-fingerprint",
		Result: &OpenAIForwardResult{RequestID: "observed-request", ResponseID: "observed-response", Model: "public-alias", UpstreamModel: "actual-sent", BillingModel: "actual-billing",
			UpstreamResponseModel: "actual-returned", UpstreamResponseServiceTier: "default", Usage: usage, ImageEndpoint: true,
			ImageCount: 2, ImageSize: "1K", ImageInputSize: "1024x1024", ImageOutputSize: "1536x1024", ImageOutputSizes: []string{"1536x1024", "1024x1536"}, ImageSizeBreakdown: map[string]int{"2K": 2},
			SearchCount: 3, WebSearchCalls: 4, AudioUsage: &AudioUsage{Mode: "tts", DurationOrUnits: 2}, VideoCount: 1, VideoDurationSeconds: 6, VideoResolution: "720p",
			UpstreamHeaders: http.Header{"Authorization": []string{"must-not-persist-header"}}, ResponseHeaders: http.Header{"Set-Cookie": []string{"must-not-persist-cookie"}},
			wsReplayInput: []json.RawMessage{json.RawMessage("\"must-not-persist-body\"")}}}
	captured, err := CaptureNativeUsagePreparation(t.Context(), in)
	require.NoError(t, err)
	raw, err := json.Marshal(captured)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "must-not-persist")
	var recovered NativeUsagePreparation
	require.NoError(t, json.Unmarshal(raw, &recovered))
	require.Equal(t, in.UserAgent, recovered.UserAgent, "preserve the existing usage-log attribution, not the request header map")
	require.Equal(t, in.IPAddress, recovered.IPAddress)
	result := recovered.Result.ForwardResult()
	require.Equal(t, usage, result.Usage)
	require.True(t, result.ImageEndpoint)
	require.Equal(t, in.Result.ImageOutputSizes, result.ImageOutputSizes)
	require.Equal(t, in.Result.ImageSizeBreakdown, result.ImageSizeBreakdown)
	require.Equal(t, in.Result.AudioUsage, result.AudioUsage)
	require.Equal(t, 3, result.SearchCount)
	require.Equal(t, 4, result.WebSearchCalls)
	require.Equal(t, "actual-returned", result.UpstreamResponseModel)
	require.Equal(t, "default", result.UpstreamResponseServiceTier)
	require.Nil(t, result.UpstreamHeaders)
	require.Nil(t, result.ResponseHeaders)
	in.Result.ImageSizeBreakdown["2K"] = 900
	require.Equal(t, 2, captured.Result.ImageSizeBreakdown["2K"])
}

func TestNativeIntentMissingPolicyUsesAttemptHostedQuotes(t *testing.T) {
	intent := nativeI2ServiceIntent()
	intent.Request.Billing.Hosted = &NativeIntentHostedPricing{Version: 1}
	zero := 0.0
	intent.Attempt.Billing.HostedPrices = map[string]*NativeIntentHostedPricing{"gpt-5.1": {Version: 1, ImageDefaults: &ImagePriceConfig{Price1K: &zero, Price2K: &zero, Price4K: &zero}}}
	record, err := MergeNativeUsageIntent(nil, intent)
	require.NoError(t, err)
	preparation := &NativeUsagePreparation{Result: NativeUsagePreparationResult{Model: "gpt-5.1", ImageEndpoint: false, ImageCount: 1}}
	require.Equal(t, billing.NativePreparationReady, NativeIntentMissingPreparationPolicy(record, record.Attempts[0], preparation), "model-independent request prices intentionally omit model defaults")
	selected := record.Attempts[0]
	selected.Billing.HostedPrices = nil
	require.Equal(t, billing.NativeHostedPolicyUnavailable, NativeIntentMissingPreparationPolicy(record, selected, preparation))
	record.Request.Billing.GroupImageCountBilling = nil
	require.Equal(t, billing.NativeOriginalPolicyUnavailable, NativeIntentMissingPreparationPolicy(record, selected, preparation), "missing image policy is not false")
}

func TestNativeIntentDecodeCannotImplicitlyUpgradeEmbeddedV1Evidence(t *testing.T) {
	intent := nativeI2ServiceIntent()
	input := nativeI2ServiceInput(t, intent, true)
	record, err := MergeNativeIntentFinalization(nil, &input)
	require.NoError(t, err)
	record.Evidence.Version = 1
	record.Evidence.Intent = nil
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	fingerprint, err := record.Fingerprint()
	require.NoError(t, err)
	_, err = DecodeNativeIntentFinalization(raw, intent.Key, fingerprint)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict, "a valid hash is not permission to upgrade the embedded evidence format")
}

func TestNativeIntentFinalizationPreservesFinalWithoutPreparation(t *testing.T) {
	for _, count := range []int64{0, 10} {
		intent := nativeI2ServiceIntent()
		input := nativeI2ServiceInput(t, intent, true)
		input.Preparation = nil
		input.Evidence.Evidence.Usage.InputTokens = &count
		input.Evidence.Observations = nil
		prepared, err := PrepareNativeIntentFinalization(input)
		require.NoError(t, err)
		require.Equal(t, billing.NativeUsageFinal, prepared.Evidence.Evidence.Confidence)
		require.True(t, prepared.Evidence.Evidence.TerminalConfirmed)
		require.Equal(t, count, *prepared.Evidence.Evidence.Usage.InputTokens)
		require.Nil(t, prepared.Evidence.Evidence.Usage.OutputTokens, "an absent field is not measured zero")
		require.Equal(t, billing.NativeUsagePreparationUnavailable, prepared.Reason)
		require.Nil(t, prepared.Preparation)
		require.Nil(t, prepared.Evidence.Settlement)
		require.Nil(t, prepared.Evidence.SettlementUsage)
		scopes, err := MergeNativeUsageIntent(nil, intent)
		require.NoError(t, err)
		require.NoError(t, ValidateNativeIntentFinalization(scopes, prepared))
		record, err := MergeNativeIntentFinalization(nil, prepared)
		require.NoError(t, err)
		raw, err := json.Marshal(record)
		require.NoError(t, err)
		fingerprint, err := record.Fingerprint()
		require.NoError(t, err)
		restored, err := DecodeNativeIntentFinalization(raw, intent.Key, fingerprint)
		require.NoError(t, err)
		require.Equal(t, record, restored)
	}
}

func TestNativeIntentFinalizationLatePreparationMustMatchPreservedFinal(t *testing.T) {
	intent := nativeI2ServiceIntent()
	full := nativeI2ServiceInput(t, intent, true)
	missing := full
	missing.Preparation = nil
	facts, err := PrepareNativeIntentFinalization(missing)
	require.NoError(t, err)
	record, err := MergeNativeIntentFinalization(nil, facts)
	require.NoError(t, err)
	for _, kind := range []string{"different_usage", "missing_raw", "stale_reason"} {
		t.Run(kind, func(t *testing.T) {
			bad, err := PrepareNativeIntentFinalization(full)
			require.NoError(t, err)
			switch kind {
			case "different_usage":
				bad.Preparation.Result.Usage.InputTokens++
			case "missing_raw":
				bad.Preparation = nil
				bad.Evidence.SettlementUsage = &UsagePricingRequest{OpenAI: full.Preparation.Result.ForwardResult()}
			case "stale_reason":
				bad.Reason = billing.NativeUsagePreparationUnavailable
			}
			_, err = PrepareNativeIntentFinalization(*bad)
			require.Error(t, err)
		})
	}
	updated, err := MergeNativeIntentFinalization(record, &full)
	require.NoError(t, err)
	require.Equal(t, record.Evidence.Evidence, updated.Evidence.Evidence)
	require.Equal(t, full.Preparation, updated.Preparation)
	require.Equal(t, billing.NativeBuilderUnavailable, updated.Reason)
	duplicate, err := MergeNativeIntentFinalization(updated, facts)
	require.NoError(t, err)
	require.Equal(t, updated, duplicate, "late facts-only replay cannot erase an available preparation")
}

func TestNativeIntentPreparationMissingAttributionKeepsLegacyShape(t *testing.T) {
	preparation := &NativeUsagePreparation{Result: NativeUsagePreparationResult{Model: "gpt-5.1"}}
	raw, err := json.Marshal(preparation)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "UserAgent")
	require.NotContains(t, string(raw), "IPAddress")
	var restored NativeUsagePreparation
	require.NoError(t, json.Unmarshal(raw, &restored))
	before, err := nativePreparationDigest(preparation)
	require.NoError(t, err)
	after, err := nativePreparationDigest(&restored)
	require.NoError(t, err)
	require.Equal(t, before, after)
}
