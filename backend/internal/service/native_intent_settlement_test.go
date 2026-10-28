//go:build unit

package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type nativeSettlementNoAccountReads struct{ AccountRepository }

func (*nativeSettlementNoAccountReads) GetByID(context.Context, int64) (*Account, error) {
	panic("settlement read current account")
}

// Synthetic fixtures exercise the actual capture/preparation/builder functions.
// They do not claim a real transport, PG transaction, or upstream validation.
func nativeSettlementFixture(t *testing.T, configure func(*OpenAIGatewayService, *nativeIntentRequestPricingInput, *nativeIntentAttemptPricingInput, *OpenAIForwardResult)) (*OpenAIGatewayService, NativeIntentRecoveryInput, *nativeIntentLivePricing, *OpenAIRecordUsageInput) {
	t.Helper()
	svc, r, a := nativeIntentPricingFixture()
	result := &OpenAIForwardResult{RequestID: r.KnownCanonical.RequestID, Model: a.Model, UpstreamModel: a.UpstreamModel,
		ServiceTier: copyPricingValue(&a.ServiceTier), ReasoningEffort: copyPricingValue(&a.ReasoningEffort),
		Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 200, CacheReadInputTokens: 100, CacheCreationInputTokens: 60, CacheCreation5mTokens: 40, CacheCreation1hTokens: 20}}
	if configure != nil {
		configure(svc, &r, &a, result)
	}
	request, attempt, live := captureNativeIntentPricingFixture(t, svc, r, a)
	// The actual before-send integration, owned by MAIN, persists these fields.
	eligible := r.APIKey.Group != nil && r.APIKey.Group.FreeOpenAIFast && groupSupportsOpenAIFast(r.APIKey.Group.Platform) && a.CredentialAccount.IsOpenAI()
	require.NotNil(t, attempt.Billing.FreeFastEligible)
	require.Equal(t, eligible, *attempt.Billing.FreeFastEligible)
	for _, name := range attempt.CandidateModels {
		require.Contains(t, attempt.Billing.HostedPrices, name)
	}
	for name := range attempt.Billing.HostedPrices {
		h, err := live.HostedPricing(name)
		require.NoError(t, err)
		require.Equal(t, h, attempt.Billing.HostedPrices[name])
		require.NotSame(t, h, attempt.Billing.HostedPrices[name])
	}
	original := &OpenAIRecordUsageInput{Result: result, APIKey: r.APIKey, User: r.User, Account: a.Account, Subscription: r.Subscription, APIKeyService: r.APIKeyService,
		PricingAt: r.PricingAt, ChannelUsageFields: r.ChannelUsageFields, RequestPayloadHash: r.RequestPayloadHash, QuotaPlatform: r.QuotaPlatform}
	p, err := CaptureNativeUsagePreparation(t.Context(), original)
	require.NoError(t, err)
	intent := NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: a.Prepared.OperationID, APIKeyID: r.APIKey.ID}, UserID: r.User.ID,
		OriginalWireDigest: [32]byte{8}, Request: request.Scope, Attempt: attempt}
	record, err := MergeNativeUsageIntent(nil, intent)
	require.NoError(t, err)
	input := NativeIntentRecoveryInput{Intent: record, Finalization: &NativeIntentFinalizationRecord{Version: 1, Preparation: p, Reason: billing.NativeBuilderUnavailable,
		Evidence: &NativeUsageEvidenceRecord{Version: 2, Evidence: billing.NativeUsageEvidence{Version: 2, Identity: *r.KnownCanonical, OperationID: intent.Key.OperationID, UserID: intent.UserID,
			GroupID: a.Prepared.GroupID, GroupBound: a.Prepared.GroupBound, OriginalWireDigest: intent.OriginalWireDigest, Prepared: &a.Prepared, AttemptSequence: a.Sequence,
			SendState: model.SendSent, Confidence: billing.NativeUsageFinal, TerminalConfirmed: true, Terminal: model.TerminalCompleted,
			Usage: &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "response.usage",
				InputTokens: int64Ptr(int64(result.Usage.InputTokens)), OutputTokens: int64Ptr(int64(result.Usage.OutputTokens)), CacheReadTokens: int64Ptr(int64(result.Usage.CacheReadInputTokens)),
				CacheCreationTokens: int64Ptr(int64(result.Usage.CacheCreationInputTokens)), ImageInputTokens: int64Ptr(int64(result.Usage.ImageInputTokens)), ImageOutputTokens: int64Ptr(int64(result.Usage.ImageOutputTokens)), ImageCount: int64Ptr(int64(result.ImageCount))}}}}}
	nativeSettlementRebind(t, &input)
	return svc, input, live, original
}

func nativeSettlementRebind(t *testing.T, in *NativeIntentRecoveryInput) {
	t.Helper()
	a := in.Intent.Attempts[0]
	ref, err := (&NativeUsageIntent{Key: in.Intent.Key, UserID: in.Intent.UserID, OriginalWireDigest: in.Intent.OriginalWireDigest, Request: in.Intent.Request, Attempt: a}).NativeIntentReference()
	require.NoError(t, err)
	in.Finalization.Intent, in.Finalization.Evidence.Intent = ref, &ref
	in.Finalization.Evidence.FrozenPricing = a.FrozenPricing
	root, err := hex.DecodeString(ref.IntentFingerprint)
	require.NoError(t, err)
	scope, err := hex.DecodeString(ref.ScopeDigest)
	require.NoError(t, err)
	e := &in.Finalization.Evidence.Evidence
	e.PricingDigest, e.AttemptScopeDigest = [32]byte(root), [32]byte(scope)
	in.Finalization.Evidence.Observations = []billing.NativeUsageEvidence{*e}
}

func TestNativeIntentSettlementUsesOriginalBuilderWithoutLiveReads(t *testing.T) {
	svc, in, _, original := nativeSettlementFixture(t, nil)
	old, err := svc.prepareOpenAIUsage(t.Context(), original)
	require.NoError(t, err)
	expected := buildUsageBillingCommand(in.Finalization.Evidence.Evidence.Identity.RequestID, old.log, old.params)
	// Current objects, resolver and config now deliberately disagree. A re-read
	// either panics or produces a different command/fingerprint.
	original.APIKey.Quota, original.APIKey.RateLimit5h = 0, 0
	original.User.Role = RoleSuperAdmin
	original.Account.RateMultiplier = float64Ptr(999)
	original.APIKey.Group.RateMultiplier = 999
	svc.cfg.RunMode = config.RunModeSimple
	svc.accountRepo = &nativeSettlementNoAccountReads{}
	svc.channelService.storeErrorCache(errors.New("no channel reads allowed"))
	svc.billingService = NewBillingService(svc.cfg, &PricingService{pricingData: map[string]*LiteLLMModelPricing{"native-catalog-model": {InputCostPerToken: 999}}})
	svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
	ctx := context.WithValue(t.Context(), ctxkey.ClientRequestID, "not-the-money-identity")
	quote, err := svc.PrepareNativeIntentUsage(ctx, in)
	require.NoError(t, err)
	require.Equal(t, expected.RequestFingerprint, quote.Settlement.RequestFingerprint)
	require.Equal(t, expected.BalanceCost, quote.Settlement.BalanceCost)
	require.Equal(t, expected.AccountQuotaCost, quote.Settlement.AccountQuotaCost)
	require.Equal(t, expected.APIKeyQuotaCost, quote.Settlement.APIKeyQuotaCost)
	require.Equal(t, in.Finalization.Evidence.Evidence.Identity.RequestID, quote.Settlement.RequestID)
	require.NotEqual(t, in.Intent.Key.OperationID, quote.Settlement.RequestID)
	require.Equal(t, 1, quote.Settlement.PayloadVersion)
	require.Equal(t, 840, quote.SettlementUsage.Tokens.InputTokens)
	require.Equal(t, 1000, quote.SettlementUsage.OpenAI.Usage.InputTokens)
	require.Nil(t, in.Intent.Attempts[0].FrozenPricing.Policy.Request)
	replayed, err := svc.PrepareNativeIntentUsage(ctx, in)
	require.NoError(t, err)
	require.Equal(t, quote.Settlement.RequestFingerprint, replayed.Settlement.RequestFingerprint)
	require.Zero(t, original.APIKeyService.(*nativeIntentPricingQuotaUpdater).calls)
}

func TestNativeIntentSettlementFundsAllModesWithCapturedBooleans(t *testing.T) {
	for _, tc := range []struct {
		name                                 string
		super, subscription, simple, windows bool
	}{
		{name: "ordinary"}, {name: "super", super: true}, {name: "subscription", subscription: true},
		{name: "super_subscription", super: true, subscription: true},
		{name: "log_only", simple: true}, {name: "key_window", simple: true, windows: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, _, original := nativeSettlementFixture(t, func(s *OpenAIGatewayService, r *nativeIntentRequestPricingInput, _ *nativeIntentAttemptPricingInput, _ *OpenAIForwardResult) {
				if tc.super {
					r.User.Role = RoleSuperAdmin
				}
				if tc.simple {
					s.cfg.RunMode = config.RunModeSimple
					s.cfg.SimpleModeKeyRateLimitEnabled = tc.windows
				}
				if tc.subscription {
					r.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
					r.Subscription = &UserSubscription{ID: 11, UserID: r.User.ID, GroupID: *r.APIKey.GroupID}
				}
			})
			originalRecord, err := svc.prepareOpenAIUsage(t.Context(), original)
			require.NoError(t, err)
			expected := buildUsageBillingCommand(in.Finalization.Evidence.Evidence.Identity.RequestID, originalRecord.log, originalRecord.params)
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			require.NoError(t, err)
			require.NotNil(t, quote.Settlement, "log-only still requires a canonical command")
			require.Equal(t, expected.RequestFingerprint, quote.Settlement.RequestFingerprint)
			require.Equal(t, expected.BalanceExempt, quote.Settlement.BalanceExempt)
			require.Equal(t, expected.SubscriptionID, quote.Settlement.SubscriptionID)
			require.Equal(t, expected.Platform, quote.Settlement.Platform)
		})
	}
}

func TestNativeIntentSettlementFrozenHostedUsesOriginalMediaRules(t *testing.T) {
	for _, dimension := range []string{"image", "video", "web_search", "search", "audio", "mixed"} {
		t.Run(dimension, func(t *testing.T) {
			svc, in, _, original := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
				g := r.APIKey.Group
				g.ImageCountBilling = true
				g.ImagePrice1K, g.ImagePrice2K, g.ImagePrice4K = float64Ptr(0), float64Ptr(0.7), float64Ptr(1)
				g.VideoPrice480P, g.VideoPrice720P, g.VideoPrice1080P = float64Ptr(0.2), float64Ptr(0.3), float64Ptr(0.4)
				g.WebSearchPricePerCall, g.SearchPricePer1k, g.AudioTTSPricePerMillionChars = float64Ptr(0), float64Ptr(8), float64Ptr(4)
				switch dimension {
				case "image":
					result.ImageEndpoint, result.ImageCount, result.ImageSize = true, 2, ImageBillingSize1K
				case "video":
					result.VideoCount, result.VideoDurationSeconds, result.VideoResolution = 2, 5, VideoBillingResolution720P
				case "web_search":
					result.WebSearchCalls = 3
				case "search":
					result.SearchCount = 5
				case "audio":
					result.AudioUsage = &AudioUsage{Mode: "tts", DurationOrUnits: 0.25}
				case "mixed":
					r.ChannelUsageFields.OriginalModel = "native-group-model"
					r.ChannelUsageFields.BillingModelSource = BillingModelSourceRequested
					result.ImageCount, result.Usage.ImageOutputTokens = 1, 100
				}
				a.ServiceTier, a.ReasoningEffort = "", ""
				result.ServiceTier, result.ReasoningEffort = nil, nil
			})
			before, err := svc.prepareOpenAIUsage(t.Context(), original)
			require.NoError(t, err)
			want := before.params.Cost
			g := original.APIKey.Group
			g.ImagePrice1K, g.VideoPrice720P, g.WebSearchPricePerCall, g.SearchPricePer1k, g.AudioTTSPricePerMillionChars = float64Ptr(999), float64Ptr(999), float64Ptr(999), float64Ptr(999), float64Ptr(999)
			svc.channelService.storeErrorCache(errors.New("offline"))
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			require.NoError(t, err)
			require.InDelta(t, want.TotalCost, quote.Settlement.UsageLog.TotalCost, 1e-10)
			require.InDelta(t, want.ActualCost, quote.Settlement.UsageLog.ActualCost, 1e-10)
			if dimension == "mixed" {
				require.Equal(t, string(BillingModeToken), *quote.Settlement.UsageLog.BillingMode)
			}
		})
	}
}

func TestNativeIntentSettlementStatsRetainsOriginalPriorityAndEffort(t *testing.T) {
	for _, choice := range []string{"custom", "customer", "default", "none"} {
		t.Run(choice, func(t *testing.T) {
			svc, in, _, _ := nativeSettlementFixture(t, nil)
			a := &in.Intent.Attempts[0]
			stats := a.Billing.Stats
			stats.State = NativeIntentStatsCaptured
			switch choice {
			case "custom":
				stats.CustomPricing = &ChannelModelPricing{BillingMode: BillingModeToken, InputPrice: float64Ptr(0.2), ReasoningEffortMultipliers: map[string]float64{"high": 3}}
				stats.ApplyCustomerPrice = true
			case "customer":
				stats.ApplyCustomerPrice = true
			case "none":
				stats.DefaultPrice = nil
			}
			nativeSettlementRebind(t, &in)
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			require.NoError(t, err)
			got := quote.Settlement.UsageLog.AccountStatsCost
			switch choice {
			case "custom":
				want := calculateStatsCost(stats.CustomPricing, quote.SettlementUsage.Tokens, 1)
				require.NotNil(t, got)
				require.InDelta(t, *want*3, *got, 1e-10)
			case "customer":
				require.Equal(t, quote.Settlement.UsageLog.TotalCost, *got)
			case "default":
				require.NotNil(t, got)
			case "none":
				require.Nil(t, got)
			}
		})
	}
}

func TestNativeIntentSettlementTierAndFreeFastUseCredentialPolicy(t *testing.T) {
	for _, oauth := range []bool{false, true} {
		svc, in, _, original := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
			r.APIKey.Group.FreeOpenAIFast = true
			r.ChannelUsageFields.OriginalModel, r.ChannelUsageFields.BillingModelSource = "native-group-model", BillingModelSourceRequested
			result.UpstreamResponseServiceTier = "default"
			if oauth {
				a.Account.Type = AccountTypeOAuth
			}
		})
		want, err := svc.prepareOpenAIUsage(t.Context(), original)
		require.NoError(t, err)
		svc.accountRepo = &nativeSettlementNoAccountReads{}
		quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
		require.NoError(t, err)
		require.Equal(t, want.log.ServiceTier, quote.Settlement.UsageLog.ServiceTier)
		require.InDelta(t, want.params.Cost.ActualCost, quote.Settlement.UsageLog.ActualCost, 1e-10)
		require.InDelta(t, want.params.Cost.TotalCost, quote.Settlement.UsageLog.TotalCost, 1e-10)
		if oauth {
			require.Less(t, quote.Settlement.UsageLog.ActualCost, quote.Settlement.UsageLog.TotalCost*in.Intent.Request.TokenMultiplier)
		}
	}
}

func TestNativeIntentSettlementReturnedSelectionSurvivesRestart(t *testing.T) {
	svc, in, live, _ := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
		r.ChannelUsageFields.BillingModelSource = BillingModelSourceResponse
		a.ReturnedModelViewRef = "fixture-owned-live-view"
		result.UpstreamResponseModel = "native-returned-model"
	})
	quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
	require.Equal(t, billing.NativeReturnedPriceViewUnavailable, NativePreparationFailureReason(err))
	require.Nil(t, quote.Settlement)
	decision, err := svc.MaterializeNativeIntentPricing(t.Context(), in, live)
	require.NoError(t, err)
	require.True(t, decision.Adopted)
	require.NotNil(t, decision.Price)
	require.NotContains(t, in.Intent.Attempts[0].AdditionalPrices, decision.SelectedModel, "terminal selection cannot rewrite sealed scope")
	in.Finalization.Preparation.FinalPricing = decision
	raw, err := json.Marshal(in)
	require.NoError(t, err)
	var restored NativeIntentRecoveryInput
	require.NoError(t, json.Unmarshal(raw, &restored))
	svc = &OpenAIGatewayService{billingService: NewBillingService(nil, nil), accountRepo: &nativeSettlementNoAccountReads{}}
	quote, err = svc.PrepareNativeIntentUsage(t.Context(), restored)
	require.NoError(t, err)
	require.Equal(t, []string{"native-returned-model"}, quote.SettlementUsage.Models)
	for _, tc := range []struct {
		name   string
		mutate func(*NativeIntentRecoveryInput)
		reason billing.NativePreparationReason
		cause  error
	}{
		{"preparation digest", func(v *NativeIntentRecoveryInput) {
			v.Finalization.Preparation.FinalPricing.PreparationDigest = "another-observation"
		}, billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict},
		{"scope digest", func(v *NativeIntentRecoveryInput) {
			v.Finalization.Preparation.FinalPricing.ScopeDigest = "another-scope"
		}, billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict},
		{"attempt sequence", func(v *NativeIntentRecoveryInput) { v.Finalization.Preparation.FinalPricing.AttemptSequence++ }, billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict},
		{"declared model", func(v *NativeIntentRecoveryInput) {
			v.Finalization.Preparation.FinalPricing.DeclaredModel = "another-model"
		}, billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict},
		{"invalid price card", func(v *NativeIntentRecoveryInput) {
			v.Finalization.Preparation.FinalPricing.Price.Mode = "invalid-mode"
		}, billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentInvalid},
		{"original request policy", func(v *NativeIntentRecoveryInput) { v.Intent.Request.OriginalModel = "changed-policy" }, billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var changed NativeIntentRecoveryInput
			require.NoError(t, json.Unmarshal(raw, &changed))
			tc.mutate(&changed)
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), changed)
			require.Equal(t, tc.reason, NativePreparationFailureReason(err))
			require.ErrorIs(t, err, tc.cause)
			require.Nil(t, quote.Settlement)
			require.Nil(t, quote.SettlementUsage)
		})
	}
}

func TestNativeIntentSettlementReturnsTypedPendingWithoutCommand(t *testing.T) {
	for _, failure := range []string{"unknown", "canonical", "policy", "stats", "hosted", "price", "freefast", "raw_mismatch", "missing_preparation"} {
		t.Run(failure, func(t *testing.T) {
			svc, in, _, _ := nativeSettlementFixture(t, nil)
			want := billing.NativeOriginalPolicyUnavailable
			switch failure {
			case "unknown":
				in.Finalization.Evidence.Evidence.Confidence = billing.NativeUsageUnknown
				want = billing.NativeBuilderUnavailable
			case "canonical":
				in.Finalization.Evidence.Evidence.Identity.RequestID = ""
				want = billing.NativeCanonicalIdentityUnavailable
			case "policy":
				in.Intent.Request.Billing.GroupImageCountBilling = nil
			case "stats":
				in.Intent.Attempts[0].Billing.Stats = nil
				want = billing.NativeStatsPolicyUnavailable
			case "hosted":
				in.Finalization.Preparation.Result.WebSearchCalls = 1
				in.Intent.Request.Billing.Hosted = nil
				want = billing.NativeHostedPolicyUnavailable
			case "price":
				in.Intent.Attempts[0].FrozenPricing.Resolved = nil
				in.Intent.Attempts[0].FrozenPricing.Policy.Pending = true
				in.Intent.Attempts[0].AdditionalPrices = nil
				nativeSettlementRebind(t, &in)
				want = billing.NativeBuilderUnavailable
			case "freefast":
				in.Intent.Request.FreeFast = true
				in.Intent.Attempts[0].Billing.FreeFastEligible = nil
				nativeSettlementRebind(t, &in)
			case "raw_mismatch":
				in.Finalization.Preparation.Result.Usage.InputTokens++
			case "missing_preparation":
				in.Finalization.Preparation = nil
				want = billing.NativeUsagePreparationUnavailable
			}
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			var typed *NativePreparationError
			require.ErrorAs(t, err, &typed)
			require.Equal(t, want, typed.Reason)
			require.Nil(t, quote.Settlement)
			require.Nil(t, quote.SettlementUsage)
		})
	}
}

func TestNativeIntentSettlementImageOnlyUsesCapturedDefaultNotTokenZero(t *testing.T) {
	svc, in, _, _ := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
		r.ChannelUsageFields = ChannelUsageFields{OriginalModel: "native-image-only", BillingModelSource: BillingModelSourceRequested}
		a.Model, a.UpstreamModel, result.Model, result.UpstreamModel = "native-image-only", "native-image-only", "native-image-only", "native-image-only"
		r.APIKey.Group.ImageCountBilling = true
		result.Usage = OpenAIUsage{} // explicit final image count, not invented token usage
		result.ImageEndpoint, result.ImageCount, result.ImageSize = true, 1, ImageBillingSize1K
	})
	require.True(t, in.Intent.Attempts[0].FrozenPricing.Policy.Pending)
	require.Nil(t, in.Intent.Attempts[0].FrozenPricing.Resolved)
	svc.billingService = NewBillingService(nil, nil)
	svc.resolver = nil
	quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
	require.NoError(t, err)
	require.InDelta(t, 0.25, quote.Settlement.UsageLog.TotalCost, 1e-12)
	require.False(t, quote.Settlement.NeedsPricing())
	require.Equal(t, string(BillingModeImage), *quote.Settlement.UsageLog.BillingMode)
}

func TestNativeIntentSettlementImageCandidateDoesNotPriceTextOnly(t *testing.T) {
	for _, tc := range []struct {
		name            string
		missingBaseline bool
	}{{name: "priced_baseline"}, {name: "missing_baseline", missingBaseline: true}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, _, original := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
				a.ImageBillingModel = "native-group-model"
				if tc.missingBaseline {
					a.Model, a.UpstreamModel = "native-missing-text-model", "native-missing-text-model"
					r.ChannelUsageFields.OriginalModel, r.ChannelUsageFields.ChannelMappedModel = a.Model, a.Model
					result.Model, result.UpstreamModel = a.Model, a.Model
				}
			})
			require.Contains(t, in.Intent.Attempts[0].AdditionalPrices, "native-group-model")
			require.NotContains(t, in.Intent.Attempts[0].CandidateModels, "native-group-model")
			require.Zero(t, in.Finalization.Preparation.Result.ImageCount)
			var expected *CostBreakdown
			if !tc.missingBaseline {
				before, err := svc.prepareOpenAIUsage(t.Context(), original)
				require.NoError(t, err)
				expected = before.params.Cost
			}
			original.APIKey.Group.ModelPricing = nil
			svc.channelService.storeErrorCache(errors.New("no current channel lookup"))
			svc.billingService = NewBillingService(nil, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				original.Result.Model: {InputCostPerToken: 99, OutputCostPerToken: 99},
			}})
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			if tc.missingBaseline {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Equal(t, billing.NativeBuilderUnavailable, NativePreparationFailureReason(err))
				require.Nil(t, quote.Settlement, "missing price remains pending, not a zero or image bill")
				require.Nil(t, quote.SettlementUsage)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{original.Result.Model}, quote.SettlementUsage.Models)
			require.InDelta(t, expected.TotalCost, quote.Settlement.UsageLog.TotalCost, 1e-12)
			require.Equal(t, QuantizeUsageBillingAmount(expected.ActualCost), quote.Settlement.UsageLog.ActualCost)
			require.Equal(t, string(BillingModeToken), *quote.Settlement.UsageLog.BillingMode)
		})
	}
}

// Exercise the actual preparation and command builder, including model
// selection, after mutable pricing has changed and the original view is gone.
func TestNativeIntentSettlementImageCandidateUsesCapturedPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, imageModel  string
		endpoint, missing bool
	}{
		{name: "mixed_token", imageModel: "native-group-model"},
		{name: "dedicated_image", imageModel: "native-image-only", endpoint: true},
		{name: "missing_image_token_price", imageModel: "native-image-only", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, _, original := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
				a.ImageBillingModel = tc.imageModel
				a.ServiceTier, a.ReasoningEffort = "", ""
				result.ServiceTier, result.ReasoningEffort = nil, nil
				r.APIKey.Group.ImageCountBilling = true
				result.BillingModel, result.ImageEndpoint, result.ImageCount, result.ImageSize = tc.imageModel, tc.endpoint, 2, ImageBillingSize2K
				result.Usage.ImageOutputTokens = 100
			})
			before, err := svc.prepareOpenAIUsage(t.Context(), original)
			require.NoError(t, err)
			expected := before.params.Cost
			raw, err := json.Marshal(in)
			require.NoError(t, err)
			var restored NativeIntentRecoveryInput
			require.NoError(t, json.Unmarshal(raw, &restored))
			original.APIKey.Group.ModelPricing = nil
			original.APIKey.Group.ImagePrice2K = float64Ptr(999)
			svc.accountRepo = &nativeSettlementNoAccountReads{}
			svc.channelService.storeErrorCache(errors.New("no current channel lookup"))
			svc.billingService = NewBillingService(nil, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				tc.imageModel: {InputCostPerToken: 99, OutputCostPerToken: 99, OutputCostPerImage: 999},
			}})
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			require.Equal(t, "native-catalog-model", restored.Intent.Attempts[0].BaselineModel)
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), restored)
			if tc.missing {
				require.ErrorIs(t, err, ErrModelPricingUnavailable, "a new live token price cannot fill a missing captured card")
				require.Equal(t, billing.NativeBuilderUnavailable, NativePreparationFailureReason(err))
				require.Nil(t, quote.Settlement, "missing token price cannot become a free quote")
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{tc.imageModel}, quote.SettlementUsage.Models)
			require.InDelta(t, expected.TotalCost, quote.Settlement.UsageLog.TotalCost, 1e-12)
			require.Equal(t, QuantizeUsageBillingAmount(expected.ActualCost), quote.Settlement.UsageLog.ActualCost)
			require.Equal(t, string(expected.BillingMode), *quote.Settlement.UsageLog.BillingMode)
			require.Greater(t, quote.Settlement.UsageLog.TotalCost, 0.0)
		})
	}
}

func TestNativeIntentSettlementImageSelectionPreservesExplicitPolicy(t *testing.T) {
	for _, tc := range []struct {
		source, model string
	}{
		{source: BillingModelSourceRequested, model: "native-catalog-model"},
		{source: BillingModelSourceChannelMapped, model: "native-channel-model"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			svc, in, _, original := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
				r.ChannelUsageFields.BillingModelSource = tc.source
				if tc.source == BillingModelSourceChannelMapped {
					r.ChannelUsageFields.ChannelMappedModel = tc.model
				}
				a.ImageBillingModel = "native-group-model"
				result.BillingModel, result.ImageCount = a.ImageBillingModel, 1
			})
			// Mixed Responses output retains token billing, even when it contains
			// images. The legacy non-frozen image branch falls back to a per-image
			// price for catalog-only models, so it is not a valid oracle here.
			// Price the same token facts without the image count; keep the actual
			// captured image facts and competing producer unchanged.
			reference := *original
			referenceResult := *original.Result
			referenceResult.ImageCount = 0
			reference.Result = &referenceResult
			expected, err := svc.prepareOpenAIUsage(t.Context(), &reference)
			require.NoError(t, err)
			require.Equal(t, string(BillingModeToken), expected.params.Cost.BillingMode)
			if tc.source == BillingModelSourceRequested {
				// Catalog input/output prices, uncached input, and priority x2.
				require.InDelta(t, (840*2e-6+200*4e-6)*2, expected.params.Cost.TotalCost, 1e-12)
			}
			require.False(t, original.Result.ImageEndpoint)
			require.Equal(t, 1, original.Result.ImageCount)
			require.Equal(t, tc.model, in.Intent.Attempts[0].BaselineModel)
			require.NotEqual(t, tc.model, original.Result.BillingModel)
			svc.channelService.storeErrorCache(errors.New("no current channel lookup"))
			svc.billingService = NewBillingService(nil, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				tc.model: {InputCostPerToken: 99, OutputCostPerToken: 99},
			}})
			svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			require.NoError(t, err)
			require.Equal(t, tc.model, quote.SettlementUsage.Models[0])
			require.Equal(t, string(BillingModeToken), *quote.Settlement.UsageLog.BillingMode)
			require.Equal(t, 1, quote.Settlement.UsageLog.ImageCount)
			require.Equal(t, expected.params.Cost.PricingRequest.Tokens, quote.SettlementUsage.Tokens)
			require.InDelta(t, expected.params.Cost.TotalCost, quote.Settlement.UsageLog.TotalCost, 1e-12)
			require.Equal(t, QuantizeUsageBillingAmount(expected.params.Cost.ActualCost), quote.Settlement.UsageLog.ActualCost)
		})
	}
}

func TestNativeIntentSettlementImageSelectionRejectsUncapturedProducer(t *testing.T) {
	svc, in, _, _ := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, _ *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
		a.ImageBillingModel = "native-group-model"
		result.BillingModel, result.ImageCount = "another-image-producer", 1
	})
	quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	require.Nil(t, quote.Settlement)
	require.Nil(t, quote.SettlementUsage)
}

func TestNativeIntentSettlementReturnedRejectionsRemainFrozen(t *testing.T) {
	for _, reason := range []string{"unidentified", "free", "increase", "cross_source"} {
		t.Run(reason, func(t *testing.T) {
			svc, in, live, _ := nativeSettlementFixture(t, func(s *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
				r.ChannelUsageFields.BillingModelSource = BillingModelSourceResponse
				a.ReturnedModelViewRef = "same-request-live-view"
				result.UpstreamResponseModel = "native-returned-model"
				switch reason {
				case "unidentified":
					result.UpstreamResponseModel = "made-up-haiku"
				case "free":
					s.billingService.pricingService.pricingData["native-returned-model"] = &LiteLLMModelPricing{}
				case "increase":
					s.billingService.pricingService.pricingData["native-returned-model"] = &LiteLLMModelPricing{InputCostPerToken: 0.5, OutputCostPerToken: 0.5}
				case "cross_source":
					a.Model, a.UpstreamModel, result.Model, result.UpstreamModel = "native-channel-model", "native-channel-model", "native-channel-model", "native-channel-model"
				}
			})
			decision, err := svc.MaterializeNativeIntentPricing(t.Context(), in, live)
			require.NoError(t, err)
			require.NotNil(t, decision)
			require.False(t, decision.Adopted)
			require.Nil(t, decision.Price)
			in.Finalization.Preparation.FinalPricing = decision
			svc.billingService = NewBillingService(nil, nil)
			svc.resolver = nil
			quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
			require.NoError(t, err)
			require.NotNil(t, quote.Settlement)
			require.Greater(t, quote.Settlement.UsageLog.TotalCost, 0.0)
		})
	}
}

func TestNativeIntentSettlementMaterializesWithoutInventingCanonicalID(t *testing.T) {
	svc, in, live, _ := nativeSettlementFixture(t, func(_ *OpenAIGatewayService, r *nativeIntentRequestPricingInput, a *nativeIntentAttemptPricingInput, result *OpenAIForwardResult) {
		r.ChannelUsageFields.BillingModelSource = BillingModelSourceResponse
		a.ReturnedModelViewRef = "ws-view"
		result.UpstreamResponseModel = "native-returned-model"
	})
	in.Intent.Request.Billing.KnownCanonical = nil
	in.Finalization.Evidence.Evidence.Identity.RequestID = ""
	nativeSettlementRebind(t, &in)
	decision, err := svc.MaterializeNativeIntentPricing(t.Context(), in, live)
	require.NoError(t, err)
	require.True(t, decision.Adopted)
	require.Empty(t, in.Finalization.Evidence.Evidence.Identity.RequestID)
	in.Finalization.Preparation.FinalPricing = decision
	quote, err := svc.PrepareNativeIntentUsage(t.Context(), in)
	require.Equal(t, billing.NativeCanonicalIdentityUnavailable, NativePreparationFailureReason(err))
	require.Nil(t, quote.Settlement)
}
