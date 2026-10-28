package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// A failed dependency is not a successful lookup with no matching price.
// Missing cards are represented by Policy.Pending and Resolve's existing
// ErrModelPricingUnavailable; neither condition manufactures zero usage.
var errNativeIntentPricingDependency = errors.New("native intent pricing dependency unavailable")

// All inputs come from the authenticated request owner. PricingAt must be the
// original request timestamp, never a late settlement timestamp. QuotaPlatform
// must already have been resolved by QuotaPlatform in the request context; an
// empty value retains the original no-platform-quota policy.
type nativeIntentRequestPricingInput struct {
	APIKey             *APIKey
	User               *User
	Subscription       *UserSubscription
	APIKeyService      APIKeyQuotaUpdater
	PricingAt          time.Time
	ChannelUsageFields ChannelUsageFields
	QuotaPlatform      string
	RequestPayloadHash string
	KnownCanonical     *billing.Identity
}

// Only a detached pricing whitelist survives capture. This object is owned by
// the live request, not a historical-view registry or an authorization lease.
// Scope is the durable request projection; the remaining fields are private
// inputs to the attempt capture and original pricing selectors.
type nativeIntentRequestPricing struct {
	Scope              NativeIntentRequestScope
	scope              NativeIntentRequestScope // private authoritative copy; Scope is detached for its recipient
	view               *ModelPricingView
	group              *Group
	userID, apiKeyID   int64
	channelMappedModel string
	pricingPlatform    string
	funding            UsageBillingPricingSnapshot
	balanceExempt      bool
}

// Call only after the actual encoder has sealed these facts. In particular,
// CredentialAccount is the real resolved parent for a shadow. Capture never
// fetches another credential owner or manufactures Prepared/Sequence/ViewRef.
type nativeIntentAttemptPricingInput struct {
	Account, CredentialAccount   *Account
	Prepared                     model.PreparedIdentity
	Sequence                     uint64
	CapturedAt                   time.Time
	Model, UpstreamModel         string
	BillingModel                 string
	ImageBillingModel            string // actual producer candidate; selected only after observed image output
	ServiceTier, ReasoningEffort string
	ReturnedModelViewRef         string // supplied by MAIN's live-view owner; not durable historical lookup
}

// This handle shares the immutable publications, not a per-request catalog
// copy. Keep it until final card selection (or durable unresolved handoff).
// Hosted holds ONLY the finite actual candidate models. Attempt.Billing keeps
// a detached copy for durable preparation; Request.Billing.Hosted contains the
// model-independent tool prices and cannot represent model-dependent defaults.
type nativeIntentLivePricing struct {
	view               *ModelPricingView
	group              *Group
	groupID            *int64
	pricingPlatform    string
	accountPlatform    string
	billingModelSource string
	baselineModel      string
	hosted             map[string]*NativeIntentHostedPricing
}

func (s *OpenAIGatewayService) captureNativeIntentRequestPricing(ctx context.Context, in nativeIntentRequestPricingInput) (*nativeIntentRequestPricing, error) {
	if ctx == nil || s == nil || s.billingService == nil {
		return nil, errNativeIntentPricingDependency
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, user := in.APIKey, in.User
	if key == nil || user == nil || key.ID <= 0 || user.ID <= 0 || key.UserID != user.ID || in.PricingAt.IsZero() {
		return nil, billing.ErrNativeIntentInvalid
	}
	if (key.GroupID == nil) != (key.Group == nil) || (key.GroupID != nil && (*key.GroupID <= 0 || key.Group.ID != *key.GroupID)) {
		return nil, billing.ErrNativeIntentConflict
	}
	if in.Subscription != nil && (in.Subscription.ID <= 0 || key.GroupID == nil || in.Subscription.UserID != user.ID || in.Subscription.GroupID != *key.GroupID) {
		return nil, billing.ErrNativeIntentConflict
	}
	if in.KnownCanonical != nil && (strings.TrimSpace(in.KnownCanonical.RequestID) == "" || in.KnownCanonical.APIKeyID != key.ID) {
		return nil, billing.ErrNativeIntentConflict
	}
	fields := in.ChannelUsageFields
	switch fields.BillingModelSource {
	case "", BillingModelSourceRequested, BillingModelSourceUpstream, BillingModelSourceChannelMapped, BillingModelSourceResponse:
	default:
		return nil, billing.ErrNativeIntentInvalid
	}
	// The request authority supplies a full canonical Group (loadKey/GetByID).
	// Nil overrides and zero independent multipliers are legitimate defaults,
	// not evidence of a missing projection. Only the explicit source contract
	// can distinguish an unhydrated cache object from a complete free policy.
	if key.Group != nil && !key.Group.Hydrated {
		return nil, fmt.Errorf("group pricing source is not hydrated: %w", errNativeIntentPricingDependency)
	}
	mode := resolveUsageBillingMode(s.cfg, key)
	updaterMissing := in.APIKeyService == nil || (reflect.ValueOf(in.APIKeyService).Kind() == reflect.Pointer && reflect.ValueOf(in.APIKeyService).IsNil())
	if mode == UsageBillingModeFull && (key.Quota > 0 || key.HasRateLimits()) && updaterMissing {
		return nil, fmt.Errorf("key quota updater: %w", errNativeIntentPricingDependency)
	}
	resolver := s.resolver
	if key.GroupID == nil {
		// An unbound key cannot consume channels or account-stat overrides.
		// Do not introduce a channel DB dependency into that request path.
		resolver = NewModelPricingResolver(nil, s.billingService)
	} else if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}
	view, err := resolver.Pin(ctx, in.PricingAt)
	if err != nil {
		return nil, fmt.Errorf("pin native prices: %w", errors.Join(errNativeIntentPricingDependency, err))
	}
	base := 1.0
	if s.cfg != nil {
		base = s.cfg.Default.RateMultiplier
	}
	if key.GroupID != nil {
		// Keep the original personal-rate resolver, including its documented
		// group-default fallback. It has no error result to reinterpret here.
		base = s.ResolveUserGroupRateMultiplier(ctx, user.ID, *key.GroupID, key.Group.RateMultiplier)
	}
	token, image := computePeakAwareMultipliers(key, base, in.PricingAt)
	video := resolveVideoRateMultiplier(key, base)
	for _, rate := range []float64{base, token, image, video} {
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
			return nil, billing.ErrNativeIntentInvalid
		}
	}
	isSubscription := in.Subscription != nil && key.Group != nil && key.Group.IsSubscriptionType()
	imageCountBilling := imageCountBillingEnabled(key)
	keyWindow := mode == UsageBillingModeKeyWindowOnly
	full := mode == UsageBillingModeFull
	request := &nativeIntentRequestPricing{
		Scope: NativeIntentRequestScope{
			Mode: mode, PricingAt: in.PricingAt, GroupID: copyPricingValue(key.GroupID), UserRole: user.Role,
			BillingModelSource: fields.BillingModelSource, OriginalModel: strings.TrimSpace(fields.OriginalModel),
			BaseMultiplier: base, TokenMultiplier: token, ImageMultiplier: image, VideoMultiplier: video,
			GroupLongContextEnabled: key.Group == nil || key.Group.LongContextPricingEnabled,
			Billing: &NativeIntentRequestBillingPolicy{Version: 1, QuotaPlatform: in.QuotaPlatform,
				RequestPayloadHash:     resolveUsageBillingPayloadFingerprint(ctx, in.RequestPayloadHash),
				IsSubscriptionBill:     isSubscription && !keyWindow,
				GroupImageCountBilling: &imageCountBilling,
				KnownCanonical:         copyPricingValue(in.KnownCanonical), ChannelID: fields.ChannelID, ModelMappingChain: fields.ModelMappingChain},
		},
		view: view, group: detachNativeIntentPricingGroup(key.Group), userID: user.ID, apiKeyID: key.ID,
		channelMappedModel: strings.TrimSpace(fields.ChannelMappedModel),
		balanceExempt:      personalUsageBalanceExempt(user, isSubscription && !keyWindow, keyWindow) && full,
		// These are the builder's policy predicates, before its positive-cost
		// guards. There is no synthetic CostBreakdown or UsagePricingRequest.
		funding: UsageBillingPricingSnapshot{PricingAt: in.PricingAt,
			Balance: full && !isSubscription && !user.IsSuperAdmin(), Subscription: full && isSubscription,
			KeyQuota:     full && key.Quota > 0 && !updaterMissing,
			KeyRateLimit: key.HasRateLimits() && (keyWindow || (full && !updaterMissing))},
	}
	if key.Group != nil {
		request.Scope.FreeFast = key.Group.FreeOpenAIFast
		request.pricingPlatform = channelLookupPlatform(ctx, key.Group.Platform)
	}
	if in.Subscription != nil {
		request.Scope.SubscriptionID = copyPricingValue(&in.Subscription.ID)
	}
	// Model-independent tool prices are actual unit quotes from the old rules.
	// A unit quote is not observed usage and never enters Funding.Request.
	request.Scope.Billing.Hosted = captureNativeIntentHostedPrices(view, request.group, "", false)
	request.scope = cloneNativeIntentRequestScope(request.Scope)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return request, nil
}

// A successful capture may have Policy.Pending=true when the original resolver
// genuinely has no baseline card. All candidates and hosted defaults are still
// retained. A dependency/identity error returns no usable scope or live handle.
func (s *OpenAIGatewayService) captureNativeIntentAttemptPricing(ctx context.Context, request *nativeIntentRequestPricing, in nativeIntentAttemptPricingInput) (NativeIntentAttemptScope, *nativeIntentLivePricing, error) {
	var empty NativeIntentAttemptScope
	if ctx == nil || request == nil || request.view == nil {
		return empty, nil, errNativeIntentPricingDependency
	}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	scope := request.scope
	account, credential, p := in.Account, in.CredentialAccount, in.Prepared
	if account == nil || credential == nil || in.Sequence == 0 || in.CapturedAt.IsZero() || !p.Valid() ||
		p.Purpose != model.DispatchGeneration || p.Mode != "native" || (p.Transport != "http" && p.Transport != "ws") ||
		strings.TrimSpace(in.Model) == "" || strings.TrimSpace(in.UpstreamModel) == "" {
		return empty, nil, billing.ErrNativeIntentInvalid
	}
	if p.ResourceID != account.ID || p.Owner.AccountID != credential.ID || p.UserID != request.userID || p.APIKeyID != request.apiKeyID ||
		p.Owner.VendorID != nativeIntentVendorID(credential) || (scope.GroupID == nil && (p.GroupBound || p.GroupID != 0)) ||
		(scope.GroupID != nil && (!p.GroupBound || p.GroupID != *scope.GroupID)) {
		return empty, nil, billing.ErrNativeIntentConflict
	}
	if account.IsShadow() {
		if *account.ParentAccountID != credential.ID || credential.IsShadow() || !credential.IsOpenAIOAuth() {
			return empty, nil, billing.ErrNativeIntentConflict
		}
	} else if account.ID != credential.ID {
		return empty, nil, billing.ErrNativeIntentConflict
	}
	if scope.BillingModelSource == BillingModelSourceResponse && strings.TrimSpace(in.ReturnedModelViewRef) == "" {
		return empty, nil, fmt.Errorf("returned-model live owner reference: %w", errNativeIntentPricingDependency)
	}
	live := &nativeIntentLivePricing{view: request.view, group: request.group, groupID: copyPricingValue(scope.GroupID),
		pricingPlatform: request.pricingPlatform, accountPlatform: account.Platform,
		billingModelSource: scope.BillingModelSource, hosted: make(map[string]*NativeIntentHostedPricing)}
	ctx = live.pricingContext(ctx)
	// Same candidate ordering as prepareOpenAIUsage, using its original alias
	// expansion and CN filter. No quote/usage is needed to capture these cards.
	baseline := forwardResultBillingModel(in.Model, in.UpstreamModel)
	if in.BillingModel != "" {
		baseline = strings.TrimSpace(in.BillingModel)
	}
	if scope.BillingModelSource == BillingModelSourceChannelMapped && request.channelMappedModel != "" && request.channelMappedModel != scope.OriginalModel {
		baseline = request.channelMappedModel
	}
	if scope.BillingModelSource == BillingModelSourceRequested && scope.OriginalModel != "" {
		baseline = scope.OriginalModel
	}
	candidates := usageBillingModelCandidates(baseline, in.BillingModel, request.channelMappedModel, scope.OriginalModel, in.UpstreamModel, in.Model)
	candidates = live.filterCandidates(ctx, candidates)
	if len(candidates) == 0 {
		return empty, nil, fmt.Errorf("native billing candidates: %w", ErrModelPricingUnavailable)
	}
	live.baselineModel = firstUsageBillingModel(candidates)
	gate := openAILongContextBillingGate(credential)
	exempt := openAISubscriptionLongContextExempt(credential, &OpenAIForwardResult{Model: in.Model, UpstreamModel: in.UpstreamModel})
	policy := request.funding
	policy.Model, policy.AccountRateMultiplier = live.baselineModel, account.BillingRateMultiplier()
	policy.AccountQuota = scope.Mode == UsageBillingModeFull && account.SupportsAccountQuota() && account.HasAnyQuotaLimit()
	if math.IsNaN(policy.AccountRateMultiplier) || math.IsInf(policy.AccountRateMultiplier, 0) || policy.AccountRateMultiplier < 0 {
		return empty, nil, billing.ErrNativeIntentInvalid
	}
	resolved, err := live.Resolve(ctx, live.baselineModel)
	if err != nil && !errors.Is(err, ErrModelPricingUnavailable) {
		return empty, nil, err
	}
	policy.Pending = errors.Is(err, ErrModelPricingUnavailable)
	frozen, err := FreezeNativeUsagePricing(resolved, &policy, request.balanceExempt)
	if err != nil {
		return empty, nil, err
	}
	attribution := &UsageLog{}
	attribution.CaptureVendorSnapshot(account, request.group)
	stats, err := live.captureStats(ctx, account.ID, in.UpstreamModel, accountStatsLongContextPricingEnabled(gate) && !exempt)
	if err != nil {
		return empty, nil, err
	}
	// Eligibility excludes tier: observed API tiers may lower the final bill.
	// Use the original group/platform/credential policy with a known Fast tier.
	freeFastEligible := groupBillsOpenAIFastAtStandard(&APIKey{Group: request.group}, credential, "priority")
	attempt := NativeIntentAttemptScope{
		Sequence: in.Sequence, Prepared: p, CapturedAt: in.CapturedAt, BaselineModel: live.baselineModel,
		ImageBillingModel: strings.TrimSpace(in.ImageBillingModel),
		CandidateModels:   slices.Clone(candidates), ChannelMappedModel: request.channelMappedModel,
		ServiceTier: strings.TrimSpace(in.ServiceTier), ReasoningEffort: strings.TrimSpace(in.ReasoningEffort),
		LongContextEnabled: copyPricingValue(gate), ModelLongContextExempt: exempt,
		FrozenPricing: frozen, AdditionalPrices: make(map[string]NativeResolvedPricingSnapshot), ReturnedModelViewRef: in.ReturnedModelViewRef,
		Billing: &NativeIntentAttemptBillingPolicy{Version: 1, AccountType: account.Type, AccountPlatform: account.Platform,
			CredentialOAuthLike: credential.IsOpenAIOAuthLike(),
			FreeFastEligible:    copyPricingValue(&freeFastEligible),
			OpenAICacheWriteTTL: account.Type == AccountTypeAPIKey && !account.UsesOpenAICodexProtocol(),
			// This capture accepts Native only. The original cache-as-input rule
			// also requires the BPS endpoint and cannot apply to this attempt.
			CacheCreationAsInput:      false,
			AccountQuotaHasDailyLimit: account.GetQuotaDailyLimit() > 0, AccountQuotaHasWeeklyLimit: account.GetQuotaWeeklyLimit() > 0,
			VendorIDSnapshot: attribution.VendorIDSnapshot, AccountNameSnapshot: attribution.AccountNameSnapshot,
			GroupNameSnapshot: attribution.GroupNameSnapshot, PlatformSnapshot: attribution.PlatformSnapshot, Stats: stats},
	}
	// Responses can select the prepared image producer only after observing
	// image output. Capture its original policy now, but keep CandidateModels
	// as the text baseline's fallback chain: an unused image tool must not
	// supply a price for an otherwise unpriced text-only response.
	captureModels := append(slices.Clone(candidates), in.ImageBillingModel)
	captureModels = live.filterCandidates(ctx, usageBillingModelCandidates("", captureModels...))
	for _, name := range captureModels {
		live.hosted[name] = captureNativeIntentHostedPrices(request.view, request.group, name, true)
		if attempt.Billing.HostedPrices == nil {
			attempt.Billing.HostedPrices = make(map[string]*NativeIntentHostedPricing, len(captureModels))
		}
		// Only this actual attempt's finite candidates enter the journal. The
		// live handle and returned DTO must not share mutable price pointers.
		attempt.Billing.HostedPrices[name] = cloneNativeIntentHostedPricing(live.hosted[name])
		if name == live.baselineModel {
			continue
		}
		price, err := live.Resolve(ctx, name)
		if errors.Is(err, ErrModelPricingUnavailable) {
			continue // confirmed missing candidate, not a dependency failure
		}
		if err != nil {
			return empty, nil, err
		}
		card, err := FreezeNativeUsagePricing(price, nil, false)
		if err != nil {
			return empty, nil, err
		}
		attempt.AdditionalPrices[name] = *card.Resolved
	}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	return attempt, live, nil
}

// Resolve returns an owned card from the original request publication. It can
// resolve previously unknown returned models, but cannot resurrect a lost view.
func (v *nativeIntentLivePricing) Resolve(ctx context.Context, name string) (*ResolvedPricing, error) {
	if ctx == nil || v == nil || v.view == nil {
		return nil, errNativeIntentPricingDependency
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved := v.view.Resolve(v.pricingContext(ctx), PricingInput{Model: name, GroupID: v.groupID, Group: v.group})
	if resolved == nil || (resolved.Mode == BillingModeToken && resolved.BasePricing == nil && len(resolved.Intervals) == 0) {
		return nil, fmt.Errorf("native price for %q: %w", name, ErrModelPricingUnavailable)
	}
	return resolved, nil
}

// ReturnedModelCandidates is the original declaration/identification/CN gate.
// The finalizer quotes these with the original calculator and this view, then
// passes the actual selected candidate and both real costs to AdoptReturnedModel.
func (v *nativeIntentLivePricing) ReturnedModelCandidates(ctx context.Context, declared string, conflict, mediaBilled bool) ([]string, error) {
	if ctx == nil || v == nil || v.view == nil {
		return nil, errNativeIntentPricingDependency
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := responseModelBillingDeclaration(v.billingModelSource, declared, conflict, mediaBilled)
	if name == "" || strings.EqualFold(name, v.baselineModel) {
		return nil, nil
	}
	identified, _ := v.identifiedResponsePricing(ctx, name)
	if !identified {
		return nil, nil
	}
	return v.filterCandidates(v.pricingContext(ctx), usageBillingModelCandidates(name)), nil
}

// AdoptReturnedModel only freezes an actually selected card after the original
// no-increase/no-free/cross-source guards accept the original calculator's
// costs. Rejection returns nil,nil (keep baseline). No money command is built.
func (v *nativeIntentLivePricing) AdoptReturnedModel(ctx context.Context, declared, selected string, conflict, mediaBilled bool, baseline, response *CostBreakdown) (*NativeResolvedPricingSnapshot, error) {
	candidates, err := v.ReturnedModelCandidates(ctx, declared, conflict, mediaBilled)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	if !slices.Contains(candidates, selected) {
		return nil, billing.ErrNativeIntentConflict
	}
	_, baselineChannel := v.identifiedResponsePricing(ctx, v.baselineModel)
	_, responseChannel := v.identifiedResponsePricing(ctx, strings.TrimSpace(declared))
	if !responseModelBillingAdoptable(baseline, response, baselineChannel, responseChannel) {
		return nil, nil
	}
	resolved, err := v.Resolve(ctx, selected)
	if err != nil {
		return nil, err
	}
	card, err := FreezeNativeUsagePricing(resolved, nil, false)
	if err != nil {
		return nil, err
	}
	return card.Resolved, nil
}

// HostedPricing returns detached model-specific values for persistence by MAIN.
// An unrecorded model is an explicit gap, never a request to use current prices.
func (v *nativeIntentLivePricing) HostedPricing(name string) (*NativeIntentHostedPricing, error) {
	if v == nil || v.view == nil || v.hosted[name] == nil {
		return nil, errNativeIntentPricingDependency
	}
	return cloneNativeIntentHostedPricing(v.hosted[name]), nil
}

func (v *nativeIntentLivePricing) pricingContext(ctx context.Context) context.Context {
	if v.pricingPlatform == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.ForcePlatform, v.pricingPlatform)
}

func (v *nativeIntentLivePricing) filterCandidates(ctx context.Context, candidates []string) []string {
	// The facade has only the pinned resolver; it cannot refresh group policy.
	reader := &OpenAIGatewayService{resolver: &v.view.resolver}
	return reader.filterCNProviderBillingModelCandidates(ctx, &Account{Platform: v.accountPlatform}, &APIKey{Group: v.group, GroupID: v.groupID}, candidates)
}

func (v *nativeIntentLivePricing) identifiedResponsePricing(ctx context.Context, name string) (bool, bool) {
	resolved := v.view.Resolve(v.pricingContext(ctx), PricingInput{Model: name, GroupID: v.groupID, Group: v.group})
	if resolved != nil && (resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel) {
		return true, true
	}
	return v.view.HasIdentifiedTokenPricing(name), false
}

func (v *nativeIntentLivePricing) captureStats(ctx context.Context, accountID int64, sentModel string, longContext bool) (*NativeIntentStatsPricing, error) {
	stats := &NativeIntentStatsPricing{State: NativeIntentStatsNotApplicable, Model: sentModel, LongContextEnabled: longContext}
	if v.groupID == nil || v.view.resolver.channelService == nil {
		return stats, nil
	}
	cache := v.view.resolver.channelSnapshot
	if cache == nil || cache.loadErr != nil {
		return nil, errNativeIntentPricingDependency
	}
	lookup := lookupGroupChannelSnapshot(ctx, cache, *v.groupID)
	if lookup == nil {
		return stats, nil // successfully observed absence, not a failed load
	}
	stats.State, stats.ApplyCustomerPrice = NativeIntentStatsCaptured, lookup.channel.ApplyPricingToAccountStats
	for _, rule := range lookup.channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, *v.groupID) {
			continue
		}
		if price := findPricingForModel(rule.Pricing, cache.groupPlatform[*v.groupID], strings.ToLower(sentModel)); price != nil {
			copy := detachChannelModelPricing(*price)
			stats.CustomPricing = &copy
			break
		}
	}
	// The default stats card intentionally has no customer group/channel.
	resolved := v.view.Resolve(ctx, PricingInput{Model: sentModel})
	if resolved != nil && (resolved.BasePricing != nil || len(resolved.Intervals) > 0) {
		resolved.longContextPricingEnabled = longContext
		frozen, err := FreezeNativeUsagePricing(resolved, nil, false)
		if err != nil {
			return nil, err
		}
		stats.DefaultPrice = frozen.Resolved
	}
	return stats, nil
}

func nativeIntentVendorID(account *Account) int64 {
	if account.VendorID == nil {
		return 0
	}
	return *account.VendorID
}

func cloneNativeIntentRequestScope(scope NativeIntentRequestScope) NativeIntentRequestScope {
	copy := scope
	copy.GroupID, copy.SubscriptionID = copyPricingValue(scope.GroupID), copyPricingValue(scope.SubscriptionID)
	if scope.Billing != nil {
		policy := *scope.Billing
		policy.KnownCanonical = copyPricingValue(scope.Billing.KnownCanonical)
		policy.GroupImageCountBilling = copyPricingValue(scope.Billing.GroupImageCountBilling)
		policy.Hosted = cloneNativeIntentHostedPricing(scope.Billing.Hosted)
		copy.Billing = &policy
	}
	return copy
}

func detachNativeIntentPricingGroup(g *Group) *Group {
	if g == nil {
		return nil
	}
	copy := &Group{ID: g.ID, Name: g.Name, Platform: g.Platform, LongContextPricingEnabled: g.LongContextPricingEnabled,
		ImageCountBilling: g.ImageCountBilling,
		FreeOpenAIFast:    g.FreeOpenAIFast,
		ImagePrice1K:      copyPricingValue(g.ImagePrice1K), ImagePrice2K: copyPricingValue(g.ImagePrice2K), ImagePrice4K: copyPricingValue(g.ImagePrice4K),
		VideoPrice480P: copyPricingValue(g.VideoPrice480P), VideoPrice720P: copyPricingValue(g.VideoPrice720P), VideoPrice1080P: copyPricingValue(g.VideoPrice1080P),
		VideoModelPrices:      make(map[string]map[string]float64, len(g.VideoModelPrices)),
		WebSearchPricePerCall: copyPricingValue(g.WebSearchPricePerCall), SearchPricePer1k: copyPricingValue(g.SearchPricePer1k),
		AudioRealtimePricePerMin: copyPricingValue(g.AudioRealtimePricePerMin), AudioTTSPricePerMillionChars: copyPricingValue(g.AudioTTSPricePerMillionChars), AudioSTTPricePerHour: copyPricingValue(g.AudioSTTPricePerHour)}
	for _, price := range g.ModelPricing {
		copy.ModelPricing = append(copy.ModelPricing, detachChannelModelPricing(price))
	}
	for family, prices := range g.VideoModelPrices {
		copy.VideoModelPrices[family] = make(map[string]float64, len(prices))
		for tier, price := range prices {
			copy.VideoModelPrices[family][tier] = price
		}
	}
	return copy
}

func captureNativeIntentHostedPrices(view *ModelPricingView, group *Group, name string, withModelDefaults bool) *NativeIntentHostedPricing {
	// Do not copy BillingService's mutex or catalog. Only this read-only facade
	// substitutes the pinned catalog for the original media/default algorithms.
	calculator := &BillingService{pricingService: view.resolver.pricingSnapshot}
	key := &APIKey{Group: group}
	imageOverrides := imagePriceConfigFromAPIKey(key)
	videoOverrides := videoPriceConfigFromAPIKey(key)
	if videoOverrides != nil {
		// Flatten only this model's inherited family overrides using the existing
		// lookup. Do not persist other families or the whole group's price map.
		videoOverrides = &VideoPriceConfig{Price480P: group.GetVideoPriceForModel(name, VideoBillingResolution480P),
			Price720P: group.GetVideoPriceForModel(name, VideoBillingResolution720P), Price1080P: group.GetVideoPriceForModel(name, VideoBillingResolution1080P)}
	}
	audioConfig := groupAudioPriceConfigFromAPIKey(key)
	web := calculator.CalculateWebSearchCost(1, webSearchPricePerCallFromAPIKey(key), 1).TotalCost
	search := calculator.CalculateSearchCost(1000, groupSearchPricePer1kFromAPIKey(key), 1).TotalCost
	realtime := calculator.CalculateAudioCost("realtime", 1, audioConfig, 1).TotalCost
	tts := calculator.CalculateAudioCost("tts", 1, audioConfig, 1).TotalCost
	stt := calculator.CalculateAudioCost("stt", 1, audioConfig, 1).TotalCost
	hosted := &NativeIntentHostedPricing{Version: 1, ImageOverrides: imageOverrides, VideoOverrides: videoOverrides,
		WebSearchPerCall: &web, SearchPer1k: &search, AudioRealtimePerMin: &realtime, AudioTTSPerMChars: &tts, AudioSTTPerHour: &stt}
	if withModelDefaults {
		image1 := calculator.getDefaultImagePrice(name, ImageBillingSize1K)
		image2 := calculator.getDefaultImagePrice(name, ImageBillingSize2K)
		image4 := calculator.getDefaultImagePrice(name, ImageBillingSize4K)
		video480 := calculator.getDefaultVideoPrice(name, VideoBillingResolution480P)
		video720 := calculator.getDefaultVideoPrice(name, VideoBillingResolution720P)
		video1080 := calculator.getDefaultVideoPrice(name, VideoBillingResolution1080P)
		hosted.ImageDefaults = &ImagePriceConfig{Price1K: &image1, Price2K: &image2, Price4K: &image4}
		hosted.VideoDefaults = &VideoPriceConfig{Price480P: &video480, Price720P: &video720, Price1080P: &video1080}
	}
	return cloneNativeIntentHostedPricing(hosted)
}

func cloneNativeIntentHostedPricing(h *NativeIntentHostedPricing) *NativeIntentHostedPricing {
	if h == nil {
		return nil
	}
	copy := *h
	cloneImage := func(p *ImagePriceConfig) *ImagePriceConfig {
		if p == nil {
			return nil
		}
		return &ImagePriceConfig{Price1K: copyPricingValue(p.Price1K), Price2K: copyPricingValue(p.Price2K), Price4K: copyPricingValue(p.Price4K)}
	}
	cloneVideo := func(p *VideoPriceConfig) *VideoPriceConfig {
		if p == nil {
			return nil
		}
		// Capture flattened the one actual family into these scalar overrides.
		return &VideoPriceConfig{Price480P: copyPricingValue(p.Price480P), Price720P: copyPricingValue(p.Price720P), Price1080P: copyPricingValue(p.Price1080P)}
	}
	copy.ImageOverrides, copy.ImageDefaults = cloneImage(h.ImageOverrides), cloneImage(h.ImageDefaults)
	copy.VideoOverrides, copy.VideoDefaults = cloneVideo(h.VideoOverrides), cloneVideo(h.VideoDefaults)
	copy.WebSearchPerCall, copy.SearchPer1k = copyPricingValue(h.WebSearchPerCall), copyPricingValue(h.SearchPer1k)
	copy.AudioRealtimePerMin, copy.AudioTTSPerMChars, copy.AudioSTTPerHour = copyPricingValue(h.AudioRealtimePerMin), copyPricingValue(h.AudioTTSPerMChars), copyPricingValue(h.AudioSTTPerHour)
	return &copy
}
