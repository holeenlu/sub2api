package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
)

var _ NativeIntentPreparer = (*OpenAIGatewayService)(nil)

// PrepareNativeIntentUsage prepares the original V1 monetary command from a
// selected durable intent and final parser evidence. It performs no reads of
// current policy and no ledger, observation, quota, or transport side effects.
// All unresolved inputs return NativePreparationError, never a nil command
// disguised as successful log-only completion. The caller owns persistence.
func (s *OpenAIGatewayService) PrepareNativeIntentUsage(ctx context.Context, in NativeIntentRecoveryInput) (NativeIntentPreparedUsage, error) {
	var empty NativeIntentPreparedUsage
	input, scope, err := s.nativeIntentUsageInput(ctx, in, true)
	if err != nil {
		return empty, err
	}
	record, err := s.prepareOpenAIUsage(ctx, input)
	if err != nil {
		return empty, nativeSettlementError(err)
	}
	if record == nil || record.params == nil || record.params.Cost == nil || record.params.Cost.PricingRequest == nil || record.params.Cost.PricingError != "" {
		return empty, nativePreparationFailure(billing.NativeBuilderUnavailable, ErrUsagePricingPending)
	}
	// No header reconstruction. Restore the whitelist's observed ID after the
	// original row allocator, which normally reads live upstream headers.
	p := in.Finalization.Preparation
	record.log.UpstreamRequestID = copyPricingValue(p.Result.UsageUpstreamRequestID)
	for i, row := range record.params.UsageLogs {
		if len(p.PriorAuthorization) == 0 {
			break
		}
		if i < len(p.PriorAuthorization) {
			row.UpstreamRequestID = copyPricingValue(p.PriorAuthorization[i].UsageUpstreamRequestID)
		} else {
			row.UpstreamRequestID = copyPricingValue(p.Result.UsageUpstreamRequestID)
		}
	}
	command := buildUsageBillingCommand(scope.identity.RequestID, record.log, record.params)
	if command == nil || command.NeedsPricing() {
		return empty, nativePreparationFailure(billing.NativeBuilderUnavailable, ErrUsagePricingPending)
	}
	command.PayloadVersion = 1
	e := in.Finalization.Evidence
	checked, err := PrepareNativeUsageEvidence(NativeUsageEvidenceInput{Intent: &in.Finalization.Intent, Evidence: e.Evidence,
		Observations: e.Observations, FrozenPricing: scope.attempt.FrozenPricing, Settlement: command, SettlementUsage: record.params.Cost.PricingRequest})
	if err != nil {
		return empty, nativeSettlementError(err)
	}
	if err = ValidateNativeIntentFinalization(in.Intent, &NativeIntentFinalizationInput{Intent: in.Finalization.Intent,
		Evidence: *checked, Preparation: p, Reason: billing.NativePreparationReady}); err != nil {
		return empty, nativeSettlementError(err)
	}
	if err := ctx.Err(); err != nil {
		return empty, nativeSettlementError(err)
	}
	return NativeIntentPreparedUsage{Settlement: checked.Settlement, SettlementUsage: checked.SettlementUsage}, nil
}

// MaterializeNativeIntentPricing runs the SAME preparation/calculator with the
// actual retained live view. MAIN must persist this returned decision on
// Preparation before invoking the recovery preparer. It cannot recover a lost
// view and never alters the already-preserved before-send scope.
func (s *OpenAIGatewayService) MaterializeNativeIntentPricing(ctx context.Context, in NativeIntentRecoveryInput, live *nativeIntentLivePricing) (*NativeIntentFinalPricing, error) {
	input, scope, err := s.nativeIntentUsageInput(ctx, in, false)
	if err != nil {
		return nil, err
	}
	r := input.Result
	declared := responseModelBillingDeclaration(scope.request.BillingModelSource, r.UpstreamResponseModel, r.UpstreamResponseModelConflict,
		r.ImageCount > 0 || r.VideoCount > 0 || r.WebSearchCalls > 0 || r.AudioUsage != nil || r.SearchCount > 0)
	if declared == "" || strings.EqualFold(declared, scope.attempt.BaselineModel) {
		return nil, nil
	}
	if live == nil || live.view == nil || live.baselineModel != scope.attempt.BaselineModel ||
		live.billingModelSource != scope.request.BillingModelSource || !live.view.resolver.pricingAt.Equal(scope.request.PricingAt) ||
		live.accountPlatform != scope.attempt.Billing.AccountPlatform || (live.groupID == nil) != (scope.request.GroupID == nil) ||
		(live.groupID != nil && *live.groupID != *scope.request.GroupID) {
		return nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
	}
	scope.materializing, scope.live = true, live
	_, err = s.prepareOpenAIUsage(ctx, input)
	if err != nil {
		return nil, nativeSettlementError(err)
	}
	return scope.decision, nil
}

func nativeSettlementError(err error) error {
	var pending *NativePreparationError
	if errors.As(err, &pending) {
		return err
	}
	return nativePreparationFailure(billing.NativeBuilderUnavailable, err)
}

func (s *OpenAIGatewayService) nativeIntentUsageInput(ctx context.Context, in NativeIntentRecoveryInput, requireCanonical bool) (*OpenAIRecordUsageInput, *nativeIntentSettlementScope, error) {
	fail := func(reason billing.NativePreparationReason, cause error) (*OpenAIRecordUsageInput, *nativeIntentSettlementScope, error) {
		return nil, nil, nativePreparationFailure(reason, cause)
	}
	if ctx == nil || s == nil || s.billingService == nil {
		return fail(billing.NativeBuilderUnavailable, errNativeIntentPricingDependency)
	}
	if err := ctx.Err(); err != nil {
		return fail(billing.NativeBuilderUnavailable, err)
	}
	if in.Intent == nil || in.Finalization == nil || in.Finalization.Evidence == nil {
		return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentInvalid)
	}
	final, intent := in.Finalization, in.Intent
	e := final.Evidence.Evidence
	if (requireCanonical && strings.TrimSpace(e.Identity.RequestID) == "") || e.Identity.APIKeyID != intent.Key.APIKeyID {
		return fail(billing.NativeCanonicalIdentityUnavailable, billing.ErrNativeIntentConflict)
	}
	if e.Confidence != billing.NativeUsageFinal || !e.TerminalConfirmed || e.Usage == nil {
		return fail(billing.NativeBuilderUnavailable, billing.ErrNativePreparationPending)
	}
	var selected *NativeIntentAttemptScope
	for i := range intent.Attempts {
		if intent.Attempts[i].Sequence == final.Intent.AttemptSequence {
			if selected != nil {
				return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
			}
			selected = &intent.Attempts[i]
		}
	}
	if selected == nil {
		return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
	}
	if reason := NativeIntentMissingPreparationPolicy(intent, *selected, final.Preparation); reason != billing.NativePreparationReady {
		return fail(reason, billing.ErrNativePreparationPending)
	}
	validated, err := PrepareNativeIntentFinalization(NativeIntentFinalizationInput{Intent: final.Intent, Preparation: final.Preparation, Reason: billing.NativeBuilderUnavailable,
		Evidence: NativeUsageEvidenceInput{Intent: &final.Intent, Evidence: e, Observations: final.Evidence.Observations, FrozenPricing: final.Evidence.FrozenPricing}})
	if err != nil {
		var preparationErr *NativePreparationError
		if errors.As(err, &preparationErr) {
			return nil, nil, err
		}
		return fail(billing.NativeOriginalPolicyUnavailable, err)
	}
	if err = ValidateNativeIntentFinalization(intent, validated); err != nil {
		var preparationErr *NativePreparationError
		if errors.As(err, &preparationErr) {
			return nil, nil, err
		}
		return fail(billing.NativeOriginalPolicyUnavailable, err)
	}
	detached, err := PrepareNativeUsageIntent(NativeUsageIntent{Key: intent.Key, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest, Request: intent.Request, Attempt: *selected})
	if err != nil {
		return fail(billing.NativeOriginalPolicyUnavailable, err)
	}
	request, attempt, p := detached.Request, detached.Attempt, validated.Preparation
	if attempt.Billing.AccountPlatform == PlatformOpenAI && attempt.LongContextEnabled == nil {
		return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativePreparationPending)
	}
	if request.SubscriptionID == nil && request.Billing.IsSubscriptionBill {
		return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
	}
	if attempt.Billing.Stats.Model != "" && upstreamSentModel(p.Result.Model, p.Result.UpstreamModel) != attempt.Billing.Stats.Model {
		return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
	}
	for _, prior := range p.PriorAuthorization {
		if prior.AuthorizationMode == UsageAuthorizationModeBPS || prior.AuthorizationMode == UsageAuthorizationModeCloud {
			return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
		}
	}
	if p.Result.AuthorizationMode == UsageAuthorizationModeBPS || p.Result.AuthorizationMode == UsageAuthorizationModeCloud {
		return fail(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
	}
	digest, err := nativePreparationDigest(p)
	if err != nil {
		return fail(billing.NativeBuilderUnavailable, err)
	}
	f := &nativeIntentSettlementScope{request: request, attempt: attempt, identity: e.Identity, scopeDigest: final.Intent.ScopeDigest, preparationDigest: digest,
		final: p.FinalPricing, responseCards: make(map[string]*ResolvedPricing)}
	// IDs and attribution are actual captured values. In particular there is
	// no fabricated Quota/RateLimit, parent, subscription type or credential.
	key := &APIKey{ID: intent.Key.APIKeyID, UserID: intent.UserID, GroupID: copyPricingValue(request.GroupID)}
	if request.GroupID != nil {
		key.Group = &Group{ID: *request.GroupID, LongContextPricingEnabled: request.GroupLongContextEnabled, ImageCountBilling: *request.Billing.GroupImageCountBilling}
	}
	account := &Account{ID: attempt.Prepared.ResourceID, Type: attempt.Billing.AccountType, Platform: attempt.Billing.AccountPlatform, RateMultiplier: copyPricingValue(&attempt.FrozenPricing.Policy.AccountRateMultiplier)}
	result := p.Result.ForwardResult()
	for _, prior := range p.PriorAuthorization {
		result.PriorAuthorizationResults = append(result.PriorAuthorizationResults, prior.ForwardResult())
	}
	var subscription *UserSubscription
	if request.SubscriptionID != nil {
		subscription = &UserSubscription{ID: *request.SubscriptionID, UserID: intent.UserID, GroupID: *request.GroupID}
	}
	input := &OpenAIRecordUsageInput{Result: result, APIKey: key, User: &User{ID: intent.UserID, Role: request.UserRole}, Account: account, Subscription: subscription,
		InboundEndpoint: p.InboundEndpoint, UpstreamEndpoint: p.Result.UpstreamEndpoint, PricingAt: request.PricingAt, UserAgent: p.UserAgent, IPAddress: p.IPAddress,
		RequestPayloadHash: request.Billing.RequestPayloadHash, QuotaPlatform: request.Billing.QuotaPlatform, NativeCompactionV2: p.NativeCompactionV2, CyberBlocked: p.CyberBlocked, nativePricing: f,
		ChannelUsageFields: ChannelUsageFields{OriginalModel: request.OriginalModel, BillingModelSource: request.BillingModelSource, ChannelMappedModel: attempt.ChannelMappedModel, ChannelID: request.Billing.ChannelID, ModelMappingChain: request.Billing.ModelMappingChain}}
	return input, f, nil
}

func nativePreparationDigest(p *NativeUsagePreparation) (string, error) {
	copy := *p
	copy.FinalPricing = nil
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func nativeIntentPricingArgument(options []*nativeIntentSettlementScope) *nativeIntentSettlementScope {
	if len(options) == 0 {
		return nil
	}
	return options[0]
}

func nativeIntentPricingAt(f *nativeIntentSettlementScope) time.Time {
	if f == nil {
		return time.Time{}
	}
	return f.request.PricingAt
}

func (f *nativeIntentSettlementScope) freeFast(tier string) (bool, error) {
	if !f.request.FreeFast {
		return false, nil
	}
	if f.attempt.Billing.FreeFastEligible == nil {
		return false, nativePreparationFailure(billing.NativeOriginalPolicyUnavailable, nil)
	}
	return *f.attempt.Billing.FreeFastEligible && openAIFastAtStandardTier(tier), nil
}

func nativePricePresent(price *float64) bool {
	return price != nil && !math.IsNaN(*price) && !math.IsInf(*price, 0) && *price >= 0
}

func (f *nativeIntentSettlementScope) validateHosted(result *OpenAIForwardResult, models []string) error {
	if result == nil {
		return nativePreparationFailure(billing.NativeBuilderUnavailable, billing.ErrNativeIntentInvalid)
	}
	if result.AudioUsage != nil {
		switch strings.ToLower(result.AudioUsage.Mode) {
		case "realtime", "tts", "stt":
		default:
			return nativePreparationFailure(billing.NativeHostedPolicyUnavailable, billing.ErrNativeIntentInvalid)
		}
		if math.IsNaN(result.AudioUsage.DurationOrUnits) || math.IsInf(result.AudioUsage.DurationOrUnits, 0) || result.AudioUsage.DurationOrUnits < 0 {
			return nativePreparationFailure(billing.NativeBuilderUnavailable, billing.ErrNativeIntentInvalid)
		}
	}
	h := f.request.Billing.Hosted
	if result.WebSearchCalls > 0 || result.SearchCount > 0 || result.AudioUsage != nil {
		if h == nil || h.Version != 1 || (result.WebSearchCalls > 0 && !nativePricePresent(h.WebSearchPerCall)) ||
			(result.SearchCount > 0 && !nativePricePresent(h.SearchPer1k)) ||
			(result.AudioUsage != nil && (!nativePricePresent(h.AudioRealtimePerMin) || !nativePricePresent(h.AudioTTSPerMChars) || !nativePricePresent(h.AudioSTTPerHour))) {
			return nativePreparationFailure(billing.NativeHostedPolicyUnavailable, nil)
		}
	}
	if result.ImageCount > 0 || result.VideoCount > 0 {
		for _, name := range models {
			if result.ImageCount > 0 {
				if _, _, err := f.imagePrices(name, NormalizeImageBillingTierOrDefault(result.ImageSize)); err != nil {
					return err
				}
			}
			if result.VideoCount > 0 {
				if _, _, err := f.videoPrices(name, NormalizeVideoBillingResolutionOrDefault(result.VideoResolution)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *nativeIntentSettlementScope) imagePrices(name, tier string) (*ImagePriceConfig, bool, error) {
	h := f.attempt.Billing.HostedPrices[name]
	if h == nil || h.Version != 1 || h.ImageDefaults == nil || !nativePricePresent(h.ImageDefaults.Price1K) || !nativePricePresent(h.ImageDefaults.Price2K) || !nativePricePresent(h.ImageDefaults.Price4K) {
		return nil, false, nativePreparationFailure(billing.NativeHostedPolicyUnavailable, nil)
	}
	copy := cloneNativeIntentHostedPricing(h)
	defaults, overrides := copy.ImageDefaults, copy.ImageOverrides
	if overrides == nil {
		return defaults, false, nil
	}
	g := &Group{ImagePrice1K: overrides.Price1K, ImagePrice2K: overrides.Price2K, ImagePrice4K: overrides.Price4K}
	for _, pair := range []struct {
		source *float64
		target **float64
	}{{overrides.Price1K, &defaults.Price1K}, {overrides.Price2K, &defaults.Price2K}, {overrides.Price4K, &defaults.Price4K}} {
		if pair.source != nil {
			if !nativePricePresent(pair.source) {
				return nil, false, nativePreparationFailure(billing.NativeHostedPolicyUnavailable, nil)
			}
			*pair.target = pair.source
		}
	}
	return defaults, g.GetImagePrice(tier) != nil, nil
}

func (f *nativeIntentSettlementScope) videoPrices(name, resolution string) (*VideoPriceConfig, bool, error) {
	h := f.attempt.Billing.HostedPrices[name]
	if h == nil || h.Version != 1 || h.VideoDefaults == nil || !nativePricePresent(h.VideoDefaults.Price480P) || !nativePricePresent(h.VideoDefaults.Price720P) || !nativePricePresent(h.VideoDefaults.Price1080P) {
		return nil, false, nativePreparationFailure(billing.NativeHostedPolicyUnavailable, nil)
	}
	copy := cloneNativeIntentHostedPricing(h)
	defaults, overrides := copy.VideoDefaults, copy.VideoOverrides
	if overrides == nil {
		return defaults, false, nil
	}
	g := &Group{VideoPrice480P: overrides.Price480P, VideoPrice720P: overrides.Price720P, VideoPrice1080P: overrides.Price1080P}
	for _, pair := range []struct {
		source *float64
		target **float64
	}{{overrides.Price480P, &defaults.Price480P}, {overrides.Price720P, &defaults.Price720P}, {overrides.Price1080P, &defaults.Price1080P}} {
		if pair.source != nil {
			if !nativePricePresent(pair.source) {
				return nil, false, nativePreparationFailure(billing.NativeHostedPolicyUnavailable, nil)
			}
			*pair.target = pair.source
		}
	}
	return defaults, g.GetVideoPrice(resolution) != nil, nil
}

func (f *nativeIntentSettlementScope) statsCost(ctx context.Context, calculator *BillingService, result *OpenAIForwardResult, tokens UsageTokens, total float64, tier string) (*float64, error) {
	stats := f.attempt.Billing.Stats
	if stats == nil {
		return nil, nativePreparationFailure(billing.NativeStatsPolicyUnavailable, nil)
	}
	if stats.State == NativeIntentStatsNotApplicable {
		return nil, nil
	}
	if stats.State != NativeIntentStatsCaptured {
		return nil, nativePreparationFailure(billing.NativeStatsPolicyUnavailable, nil)
	}
	count := 1
	if result.ImageCount > 0 {
		count = result.ImageCount
	}
	if stats.CustomPricing != nil {
		if cost := calculateStatsCost(stats.CustomPricing, tokens, count); cost != nil {
			*cost *= reasoningEffortBillingMultiplier(f.attempt.ReasoningEffort, stats.CustomPricing.ReasoningEffortMultipliers)
			return cost, nil
		}
	}
	if stats.ApplyCustomerPrice {
		if total <= 0 {
			return nil, nil
		}
		return &total, nil
	}
	if stats.DefaultPrice == nil {
		return nil, nil
	} // captured absence preserves the original nil fallback
	price, err := stats.DefaultPrice.Restore()
	if err != nil {
		return nil, nativePreparationFailure(billing.NativeStatsPolicyUnavailable, err)
	}
	price.longContextPricingEnabled = stats.LongContextEnabled
	cost, err := calculator.CalculateCostUnified(CostInput{Ctx: ctx, Model: stats.Model, Tokens: tokens, RateMultiplier: 1,
		ServiceTier: normalizeBillingServiceTier(tier), ReasoningEffort: f.attempt.ReasoningEffort, PricingAt: f.request.PricingAt, Resolved: price})
	if err != nil {
		return nil, nativePreparationFailure(billing.NativeStatsPolicyUnavailable, err)
	}
	if cost == nil || cost.TotalCost <= 0 {
		return nil, nil
	}
	return &cost.TotalCost, nil
}

// NativeIntentFinalPricing is the live owner's terminal selection evidence. It
// belongs to Preparation, never to the immutable before-send attempt. A live
// view reference alone is not a historical lookup. Only an actually adopted
// response card is stored; rejection retains the attempt's original cards.
type NativeIntentFinalPricing struct {
	Version                                      int
	AttemptSequence                              uint64
	ScopeDigest, PreparationDigest               string
	DeclaredModel, SelectedModel                 string
	Adopted, ResponseIdentified                  bool
	BaselineChannelPriced, ResponseChannelPriced bool
	Price                                        *NativeResolvedPricingSnapshot
}

type nativeIntentFrozenFunding struct {
	Policy                      UsageBillingPricingSnapshot
	BalanceExempt               bool
	AccountDaily, AccountWeekly bool
}

// This explicit input is private to the existing preparation/calculator. It
// never holds current accounts, repositories, or a recovered live view.
type nativeIntentSettlementScope struct {
	request                        NativeIntentRequestScope
	attempt                        NativeIntentAttemptScope
	identity                       billing.Identity
	scopeDigest, preparationDigest string
	final                          *NativeIntentFinalPricing
	live                           *nativeIntentLivePricing // set only by MaterializeNativeIntentPricing
	materializing                  bool
	decision                       *NativeIntentFinalPricing
	quotedModel                    string
	responseCards                  map[string]*ResolvedPricing
}

func nativePreparationFailure(reason billing.NativePreparationReason, cause error) error {
	if cause == nil {
		cause = billing.ErrNativePreparationPending
	}
	return &NativePreparationError{Reason: reason, Cause: cause}
}

// Preserve explicit request/channel policy. Otherwise, observed image output
// may select only the image producer captured on this actual attempt. Missing
// image prices stay pending rather than borrowing an unrelated text price.
func (f *nativeIntentSettlementScope) billingModels(result *OpenAIForwardResult) ([]string, error) {
	models := slices.Clone(f.attempt.CandidateModels)
	if result == nil || result.ImageCount <= 0 || strings.TrimSpace(result.BillingModel) == "" {
		return models, nil
	}
	if f.request.BillingModelSource == BillingModelSourceRequested && f.request.OriginalModel != "" ||
		f.request.BillingModelSource == BillingModelSourceChannelMapped && f.attempt.ChannelMappedModel != "" && f.attempt.ChannelMappedModel != f.request.OriginalModel {
		return models, nil
	}
	producer := strings.TrimSpace(result.BillingModel)
	if producer == f.attempt.BaselineModel {
		return models, nil
	}
	if producer != f.attempt.ImageBillingModel {
		return nil, nativePreparationFailure(billing.NativeOriginalPolicyUnavailable, billing.ErrNativeIntentConflict)
	}
	models = nil
	for _, candidate := range usageBillingModelCandidates(producer) {
		if f.attempt.Billing.HostedPrices[candidate] != nil {
			models = append(models, candidate)
		}
	}
	if len(models) == 0 {
		return nil, nativePreparationFailure(billing.NativeHostedPolicyUnavailable, billing.ErrNativeIntentConflict)
	}
	return models, nil
}

func (f *nativeIntentSettlementScope) price(name string) (*ResolvedPricing, error) {
	if price := f.responseCards[name]; price != nil {
		return detachResolvedPricing(price), nil
	}
	var card *NativeResolvedPricingSnapshot
	if name == f.attempt.BaselineModel {
		card = f.attempt.FrozenPricing.Resolved
	} else if price, ok := f.attempt.AdditionalPrices[name]; ok {
		card = &price
	}
	if card == nil {
		return nil, fmt.Errorf("frozen native price %q: %w", name, ErrModelPricingUnavailable)
	}
	if err := validateNativeIntentPriceCard(*card); err != nil {
		return nil, err
	}
	return card.Restore()
}

func (f *nativeIntentSettlementScope) channelPrice(name string) *ResolvedPricing {
	price, err := f.price(name)
	if err == nil && (price.Source == PricingSourceGroup || price.Source == PricingSourceChannel) {
		return price
	}
	return nil
}

func (f *nativeIntentSettlementScope) funding() *nativeIntentFrozenFunding {
	return &nativeIntentFrozenFunding{Policy: *f.attempt.FrozenPricing.Policy, BalanceExempt: *f.attempt.FrozenPricing.BalanceExempt,
		AccountDaily: f.attempt.Billing.AccountQuotaHasDailyLimit, AccountWeekly: f.attempt.Billing.AccountQuotaHasWeeklyLimit}
}

func (f *nativeIntentSettlementScope) selectResponsePricing(ctx context.Context, service *OpenAIGatewayService, result *OpenAIForwardResult, key *APIKey, tokens UsageTokens, tier string, baselineModels []string, baseline *CostBreakdown) ([]string, *CostBreakdown, error) {
	declared := responseModelBillingDeclaration(f.request.BillingModelSource, result.UpstreamResponseModel, result.UpstreamResponseModelConflict,
		result.ImageCount > 0 || result.VideoCount > 0 || result.WebSearchCalls > 0 || result.AudioUsage != nil || result.SearchCount > 0)
	if declared == "" || strings.EqualFold(declared, f.attempt.BaselineModel) {
		return baselineModels, baseline, nil
	}
	baselineSelected := f.quotedModel
	if !f.materializing {
		d := f.final
		if d == nil {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, nil)
		}
		if d.Version != 1 || d.AttemptSequence != f.attempt.Sequence || d.ScopeDigest != f.scopeDigest || d.PreparationDigest != f.preparationDigest || d.DeclaredModel != declared {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
		}
		if !d.Adopted {
			if d.Price != nil || d.SelectedModel != baselineSelected {
				return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
			}
			return baselineModels, baseline, nil
		}
		if !d.ResponseIdentified || d.Price == nil || !slices.Contains(usageBillingModelCandidates(declared), d.SelectedModel) {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
		}
		if err := validateNativeIntentPriceCard(*d.Price); err != nil {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, err)
		}
		baselineChannel := f.channelPrice(f.attempt.BaselineModel) != nil
		if baselineChannel != d.BaselineChannelPriced || (d.ResponseChannelPriced && d.Price.Source != PricingSourceGroup && d.Price.Source != PricingSourceChannel) {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
		}
		price, err := d.Price.Restore()
		if err != nil {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, err)
		}
		f.responseCards[d.SelectedModel] = price
		models := []string{d.SelectedModel}
		cost, err := f.quoteResponse(ctx, service, result, key, tokens, tier, models)
		if err != nil {
			return nil, nil, nativeSettlementError(err)
		}
		// Recheck the original monetary guard with final quantities and the
		// persisted source-identification facts. Never re-identify on live data.
		if !responseModelBillingAdoptable(baseline, cost, d.BaselineChannelPriced, d.ResponseChannelPriced) {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
		}
		return models, cost, nil
	}
	if f.live == nil {
		return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, nil)
	}
	d := &NativeIntentFinalPricing{Version: 1, AttemptSequence: f.attempt.Sequence, ScopeDigest: f.scopeDigest, PreparationDigest: f.preparationDigest,
		DeclaredModel: declared, SelectedModel: baselineSelected}
	f.decision = d
	_, d.BaselineChannelPriced = f.live.identifiedResponsePricing(ctx, f.attempt.BaselineModel)
	d.ResponseIdentified, d.ResponseChannelPriced = f.live.identifiedResponsePricing(ctx, declared)
	models, err := f.live.ReturnedModelCandidates(ctx, declared, false, false)
	if err != nil {
		return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, err)
	}
	if len(models) == 0 {
		return baselineModels, baseline, nil
	}
	for _, name := range models {
		price, err := f.live.Resolve(ctx, name)
		if errors.Is(err, ErrModelPricingUnavailable) {
			continue
		}
		if err != nil {
			return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, err)
		}
		f.responseCards[name] = price
	}
	cost, err := f.quoteResponse(ctx, service, result, key, tokens, tier, models)
	if err != nil {
		if errors.Is(err, ErrModelPricingUnavailable) {
			f.quotedModel = baselineSelected
			return baselineModels, baseline, nil
		}
		return nil, nil, nativeSettlementError(err)
	}
	selected := f.quotedModel
	card, err := f.live.AdoptReturnedModel(ctx, declared, selected, false, false, baseline, cost)
	if err != nil {
		return nil, nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, err)
	}
	if card == nil {
		f.quotedModel = baselineSelected
		return baselineModels, baseline, nil
	}
	d.Adopted, d.SelectedModel, d.Price = true, selected, card
	return []string{selected}, cost, nil
}

func (f *nativeIntentSettlementScope) quoteResponse(ctx context.Context, service *OpenAIGatewayService, result *OpenAIForwardResult, key *APIKey, tokens UsageTokens, tier string, models []string) (*CostBreakdown, error) {
	r, a := f.request, f.attempt
	return service.calculateOpenAIRecordUsageCost(ctx, result, key, models, r.TokenMultiplier, r.ImageMultiplier, r.VideoMultiplier, r.BaseMultiplier,
		tokens, tier, a.LongContextEnabled, a.ModelLongContextExempt, r.PricingAt, f)
}
