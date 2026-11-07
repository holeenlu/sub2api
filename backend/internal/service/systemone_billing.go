package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	protocol "github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
)

var ErrSystemOneJournalUnavailable = errors.New("System One settlement journal is not integrated")

const NativeIntentProtocolSystemOne = "systemone"

// SystemOneSettlementJournal extends the original technical-intent store.
// RecordSystemOneUsage uses the shared finalizer and ledger. It
// must atomically preserve this projection and validate any monetary command
// against its stored scope and observations, using the original money dedup.
// This interface never configures a second global preparation callback.
type SystemOneSettlementJournal interface {
	NativeUsageIntentRepository
	RecordSystemOneUsage(context.Context, *SystemOneUsagePreparation) (SystemOneBillingReceipt, error)
}

type SystemOneBillingReceipt struct {
	Intent            billing.NativeIntentReference
	Billing           model.BillingReceipt
	ActualApplyResult *UsageBillingApplyResult
}

// SystemOneUsagePreparation is the native System One projection for both live
// finalization and the existing recovery dispatch. It contains no credential,
// body, header map, live catalog or service/account/key pointer. Nil counters
// remain unknown. The shared finalizer dispatches using its persisted protocol;
// OpenAI preparation cannot represent this projection.
type SystemOneUsagePreparation struct {
	Version                           int
	Intent                            billing.NativeIntentReference
	Scope                             NativeUsageIntent
	Operation                         model.OperationResult
	Identity                          billing.Identity
	ObservedAt                        time.Time
	StatusCode                        int
	InputTokens, OutputTokens         *int64
	ReturnedModel                     string
	UpstreamRequestID                 *string
	Duration                          time.Duration
	UserAgent, IPAddress, SessionID   string
	InboundEndpoint, UpstreamEndpoint string
	ChannelUsageFields
	// FinalPrice is resolved from the original pinned publication, never from
	// current configuration during recovery. Baseline price is already in Scope.
	FinalPrice           *NativeResolvedPricingSnapshot
	FinalModel           string
	FinalPriceIdentified bool
	Settlement           *UsageBillingCommand
	SettlementUsage      *UsagePricingRequest
}

type systemOnePricing struct {
	request       NativeIntentRequestScope
	group         *Group
	view          *ModelPricingView
	fields        ChannelUsageFields
	funding       UsageBillingPricingSnapshot
	balanceExempt bool
	platform      string
}

func (s *GatewayService) captureSystemOnePricing(ctx context.Context, in SystemOneExecutionInput) (*systemOnePricing, error) {
	key := in.APIKey
	if key == nil || key.User == nil || key.Group == nil || key.GroupID == nil || !key.Group.Hydrated || in.PricingAt.IsZero() || s.resolver == nil || s.billingService == nil {
		return nil, ErrUsageBillingUnavailable
	}
	view, err := s.resolver.Pin(ctx, in.PricingAt)
	if err != nil {
		return nil, err
	}
	base := s.ResolveUserGroupRateMultiplier(ctx, key.UserID, *key.GroupID, key.Group.RateMultiplier)
	token, image := computePeakAwareMultipliers(key, base, in.PricingAt)
	mode := resolveUsageBillingMode(s.cfg, key)
	full, windows := mode == UsageBillingModeFull, mode == UsageBillingModeKeyWindowOnly
	isSub := in.Subscription != nil && key.Group.IsSubscriptionType()
	count := imageCountBillingEnabled(key)
	p := &systemOnePricing{view: view, group: detachNativeIntentPricingGroup(key.Group), fields: in.ChannelUsageFields, platform: channelLookupPlatform(ctx, key.Group.Platform),
		request: NativeIntentRequestScope{Mode: mode, PricingAt: in.PricingAt, GroupID: copyPricingValue(key.GroupID), UserRole: key.User.Role, BillingModelSource: in.BillingModelSource, OriginalModel: in.OriginalModel,
			BaseMultiplier: base, TokenMultiplier: token, ImageMultiplier: image, VideoMultiplier: resolveVideoRateMultiplier(key, base), GroupLongContextEnabled: key.Group.LongContextPricingEnabled,
			Billing: &NativeIntentRequestBillingPolicy{Version: 1, QuotaPlatform: in.QuotaPlatform, RequestPayloadHash: resolveUsageBillingPayloadFingerprint(ctx, in.RequestPayloadHash), IsSubscriptionBill: isSub && !windows, GroupImageCountBilling: &count, ChannelID: in.ChannelID, ModelMappingChain: in.ModelMappingChain}},
		funding:       UsageBillingPricingSnapshot{PricingAt: in.PricingAt, Balance: full && !isSub && !key.User.IsSuperAdmin(), Subscription: full && isSub, KeyQuota: full && key.Quota > 0, KeyRateLimit: key.HasRateLimits() && (windows || full)},
		balanceExempt: personalUsageBalanceExempt(key.User, isSub && !windows, windows) && full}
	if in.Subscription != nil {
		p.request.SubscriptionID = copyPricingValue(&in.Subscription.ID)
	}
	p.request.Protocol = NativeIntentProtocolSystemOne
	return p, nil
}

// capture freezes only the selected attempt, using the shared immutable view
// and original Gateway model selector. It performs no amount transaction.
func (p *systemOnePricing) capture(ctx context.Context, s *GatewayService, in SystemOneExecutionInput, account *Account, identity model.PreparedIdentity, sequence uint64) (NativeUsageIntent, error) {
	key := &APIKey{ID: in.APIKey.ID, UserID: in.APIKey.UserID, GroupID: p.request.GroupID, Group: p.group}
	// This read-only calculator facade contains no copied mutex/runtime state.
	selector := &GatewayService{resolver: &p.view.resolver, billingService: &BillingService{cfg: s.billingService.cfg, pricingService: p.view.resolver.pricingSnapshot, fallbackPrices: s.billingService.fallbackPrices}}
	baseline := selector.recordUsageBillingModel(ctx, key, &ForwardResult{Model: protocol.JevLatestModel}, p.fields)
	resolved := p.view.Resolve(ctx, PricingInput{Model: baseline, GroupID: key.GroupID, Group: p.group})
	policy := p.funding
	policy.Model, policy.AccountRateMultiplier = baseline, account.BillingRateMultiplier()
	policy.AccountQuota = p.request.Mode == UsageBillingModeFull && account.SupportsAccountQuota() && account.HasAnyQuotaLimit()
	policy.Pending = resolved == nil || resolved.Mode == BillingModeToken && resolved.BasePricing == nil && len(resolved.Intervals) == 0
	if policy.Pending {
		resolved = nil
	}
	frozen, err := FreezeNativeUsagePricing(resolved, &policy, p.balanceExempt)
	if err != nil {
		return NativeUsageIntent{}, err
	}
	live := &nativeIntentLivePricing{view: p.view, group: p.group, groupID: p.request.GroupID, pricingPlatform: p.platform, accountPlatform: PlatformTypeSafe}
	stats, err := live.captureStats(ctx, account.ID, protocol.JevLatestModel, true)
	if err != nil {
		return NativeUsageIntent{}, err
	}
	attribution := &UsageLog{}
	attribution.CaptureVendorSnapshot(account, p.group)
	freeFast := false
	viewRef := ""
	if p.request.BillingModelSource == BillingModelSourceResponse {
		viewRef = in.OperationID
	}
	intent := NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: in.OperationID, APIKeyID: key.ID}, UserID: key.UserID, OriginalWireDigest: in.OriginalWireDigest, Request: p.request,
		Attempt: NativeIntentAttemptScope{Sequence: sequence, Prepared: identity, CapturedAt: time.Now(), BaselineModel: baseline, CandidateModels: []string{baseline}, ChannelMappedModel: p.fields.ChannelMappedModel, FrozenPricing: frozen, ReturnedModelViewRef: viewRef,
			Billing: &NativeIntentAttemptBillingPolicy{Version: 1, AccountType: account.Type, AccountPlatform: account.Platform, FreeFastEligible: &freeFast, AccountQuotaHasDailyLimit: account.GetQuotaDailyLimit() > 0, AccountQuotaHasWeeklyLimit: account.GetQuotaWeeklyLimit() > 0,
				VendorIDSnapshot: attribution.VendorIDSnapshot, AccountNameSnapshot: attribution.AccountNameSnapshot, GroupNameSnapshot: attribution.GroupNameSnapshot, PlatformSnapshot: attribution.PlatformSnapshot, Stats: stats}}}
	checked, err := PrepareNativeUsageIntent(intent)
	if err != nil {
		return NativeUsageIntent{}, err
	}
	return *checked, nil
}

// quote uses the original shared token calculator with an explicit restored
// card. A non-nil Resolved and group prevent live catalog fallback.
func (s *GatewayService) quoteSystemOne(ctx context.Context, p *SystemOneUsagePreparation, price *NativeResolvedPricingSnapshot, name string) (*CostBreakdown, error) {
	if p.InputTokens == nil || p.OutputTokens == nil || price == nil {
		return nil, ErrUsagePricingPending
	}
	resolved, err := price.Restore()
	if err != nil || resolved == nil {
		return nil, errors.Join(ErrUsagePricingPending, err)
	}
	group := &Group{ID: *p.Scope.Request.GroupID, LongContextPricingEnabled: p.Scope.Request.GroupLongContextEnabled}
	return s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: name, Group: group, GroupID: p.Scope.Request.GroupID, Tokens: UsageTokens{InputTokens: int(*p.InputTokens), OutputTokens: int(*p.OutputTokens)}, RequestCount: 1,
		RateMultiplier: p.Scope.Request.TokenMultiplier, PricingAt: p.Scope.Request.PricingAt, Resolved: resolved})
}

func (p *systemOnePricing) resolveFinalPrice(ctx context.Context, s *GatewayService, out *SystemOneUsagePreparation) (err error) {
	// Empty FinalModel is an unresolved live pricing decision. Recovery must
	// not replace a lost returned-model decision with a successful baseline bill.
	defer func() {
		if err != nil {
			out.FinalModel = ""
			out.FinalPrice = nil
			out.FinalPriceIdentified = false
		}
	}()
	out.FinalModel = out.Scope.Attempt.BaselineModel
	if out.InputTokens == nil || out.OutputTokens == nil {
		return nil
	}
	if responseModelBillingDeclaration(p.request.BillingModelSource, out.ReturnedModel, false, false) == "" || out.ReturnedModel == out.FinalModel {
		return nil
	}
	price := p.view.Resolve(ctx, PricingInput{Model: out.ReturnedModel, GroupID: p.request.GroupID, Group: p.group})
	if price == nil {
		return nil
	}
	channel := price.Source == PricingSourceGroup || price.Source == PricingSourceChannel
	if !channel && !p.view.HasIdentifiedTokenPricing(out.ReturnedModel) {
		return nil
	}
	frozen, err := FreezeNativeUsagePricing(price, nil, false)
	if err != nil {
		return err
	}
	baseline, err := s.quoteSystemOne(ctx, out, out.Scope.Attempt.FrozenPricing.Resolved, out.FinalModel)
	if err != nil {
		return err
	}
	response, err := s.quoteSystemOne(ctx, out, frozen.Resolved, out.ReturnedModel)
	if err != nil {
		return err
	}
	baseCard := out.Scope.Attempt.FrozenPricing.Resolved
	baseChannel := baseCard.Source == PricingSourceGroup || baseCard.Source == PricingSourceChannel
	if responseModelBillingAdoptable(baseline, response, baseChannel, channel) {
		out.FinalModel, out.FinalPrice = out.ReturnedModel, frozen.Resolved
		out.FinalPriceIdentified = true
	}
	return nil
}

// PrepareSystemOneUsage is the dedicated, side-effect-free recovery calculator.
// MAIN's sole recovery dispatch calls it only after matching this projection to
// the persisted intent and final measurements. No registry mutation occurs.
func (s *GatewayService) PrepareSystemOneUsage(ctx context.Context, in SystemOneUsagePreparation) (*UsageBillingCommand, *UsagePricingRequest, error) {
	if ctx == nil || s == nil || s.billingService == nil || in.Version != 1 || in.Scope.Request.GroupID == nil || in.Identity.RequestID == "" || in.Identity.APIKeyID != in.Scope.Key.APIKeyID || in.ObservedAt.IsZero() {
		return nil, nil, billing.ErrNativeIntentInvalid
	}
	checked, err := PrepareNativeUsageIntent(in.Scope)
	if err != nil {
		return nil, nil, err
	}
	in.Scope = *checked
	if err := ValidateSystemOneUsagePreparation(in); err != nil {
		return nil, nil, err
	}
	a, r := in.Scope.Attempt, in.Scope.Request
	if in.FinalModel == "" {
		return nil, nil, ErrUsagePricingPending
	}
	if in.FinalPrice == nil && in.FinalModel != a.BaselineModel {
		return nil, nil, billing.ErrNativeIntentConflict
	}
	if a.Billing == nil || r.Billing == nil || a.Billing.AccountPlatform != PlatformTypeSafe || in.InputTokens == nil || in.OutputTokens == nil || *in.InputTokens < 0 || *in.OutputTokens < 0 || in.StatusCode < 200 || in.StatusCode >= 300 {
		return nil, nil, ErrUsagePricingPending
	}
	if in.Identity.APIKeyID != a.Prepared.APIKeyID || in.Intent.Key != in.Scope.Key || in.Intent.AttemptSequence != a.Sequence {
		return nil, nil, billing.ErrNativeIntentConflict
	}
	price, name := a.FrozenPricing.Resolved, a.BaselineModel
	if in.FinalPrice != nil {
		if !in.FinalPriceIdentified || r.BillingModelSource != BillingModelSourceResponse || in.FinalModel != in.ReturnedModel {
			return nil, nil, billing.ErrNativeIntentConflict
		}
		price, name = in.FinalPrice, in.FinalModel
	}
	cost, err := s.quoteSystemOne(ctx, &in, price, name)
	if err != nil {
		return nil, nil, err
	}
	if in.FinalPrice != nil {
		baseline, quoteErr := s.quoteSystemOne(ctx, &in, a.FrozenPricing.Resolved, a.BaselineModel)
		if quoteErr != nil {
			return nil, nil, quoteErr
		}
		baseChannel := a.FrozenPricing.Resolved.Source == PricingSourceGroup || a.FrozenPricing.Resolved.Source == PricingSourceChannel
		responseChannel := price.Source == PricingSourceGroup || price.Source == PricingSourceChannel
		if !responseModelBillingAdoptable(baseline, cost, baseChannel, responseChannel) {
			return nil, nil, billing.ErrNativeIntentConflict
		}
	}
	key := &APIKey{ID: in.Identity.APIKeyID, UserID: in.Scope.UserID, GroupID: r.GroupID, Group: &Group{ID: *r.GroupID, LongContextPricingEnabled: r.GroupLongContextEnabled}}
	user := &User{ID: in.Scope.UserID, Role: r.UserRole}
	account := &Account{ID: a.Prepared.ResourceID, Platform: a.Billing.AccountPlatform, Type: a.Billing.AccountType}
	var sub *UserSubscription
	if r.SubscriptionID != nil {
		sub = &UserSubscription{ID: *r.SubscriptionID}
	}
	result := &ForwardResult{Model: protocol.JevLatestModel, RequestID: in.Identity.RequestID, UpstreamResponseModel: in.ReturnedModel, Usage: ClaudeUsage{InputTokens: int(*in.InputTokens), OutputTokens: int(*in.OutputTokens)}, Duration: in.Duration}
	input := &recordUsageCoreInput{Result: result, APIKey: key, User: user, Account: account, Subscription: sub, PricingAt: r.PricingAt, UserAgent: in.UserAgent, IPAddress: in.IPAddress, SessionID: in.SessionID, InboundEndpoint: in.InboundEndpoint, UpstreamEndpoint: in.UpstreamEndpoint, ChannelUsageFields: in.ChannelUsageFields}
	billingType := BillingTypeBalance
	if r.Billing.IsSubscriptionBill {
		billingType = BillingTypeSubscription
	}
	requested := r.OriginalModel
	if requested == "" {
		requested = result.Model
	}
	row := s.buildRecordUsageLog(ctx, input, result, key, user, account, sub, requested, r.TokenMultiplier, r.ImageMultiplier, a.FrozenPricing.Policy.AccountRateMultiplier, billingType, false, cost)
	row.RequestID, row.CreatedAt, row.UpstreamRequestID = in.Identity.RequestID, in.ObservedAt, copyPricingValue(in.UpstreamRequestID)
	row.VendorIDSnapshot, row.AccountNameSnapshot, row.GroupNameSnapshot, row.PlatformSnapshot = a.Billing.VendorIDSnapshot, a.Billing.AccountNameSnapshot, a.Billing.GroupNameSnapshot, a.Billing.PlatformSnapshot
	stats, err := s.systemOneStatsCost(ctx, a.Billing.Stats, UsageTokens{InputTokens: int(*in.InputTokens), OutputTokens: int(*in.OutputTokens)}, cost.TotalCost, r.PricingAt)
	if err != nil {
		return nil, nil, err
	}
	row.AccountStatsCost = stats
	mapping := gatewayPricingRequest(result, key, name, r.TokenMultiplier, r.ImageMultiplier, r.PricingAt)
	mapping.GroupLongContextEnabled = r.GroupLongContextEnabled
	cost.PricingRequest = mapping
	funding := &nativeIntentFrozenFunding{Policy: *a.FrozenPricing.Policy, BalanceExempt: *a.FrozenPricing.BalanceExempt, AccountDaily: a.Billing.AccountQuotaHasDailyLimit, AccountWeekly: a.Billing.AccountQuotaHasWeeklyLimit}
	cmd := buildUsageBillingCommand(in.Identity.RequestID, row, &postUsageBillingParams{Cost: cost, User: user, APIKey: key, Account: account, Subscription: sub, RequestPayloadHash: r.Billing.RequestPayloadHash, IsSubscriptionBill: r.Billing.IsSubscriptionBill,
		BillingModel: name, PricingAt: r.PricingAt, Platform: r.Billing.QuotaPlatform, AccountRateMultiplier: a.FrozenPricing.Policy.AccountRateMultiplier, FrozenFunding: funding, SimpleModeKeyRateLimitOnly: r.Mode == UsageBillingModeKeyWindowOnly, UsageLogOnly: r.Mode == UsageBillingModeLogOnly})
	if cmd == nil {
		return nil, nil, ErrUsageBillingUnavailable
	}
	return cmd, mapping, nil
}

// ValidateSystemOneUsagePreparation checks the projection against the actual
// selected attempt. The storage adapter must additionally load/lock the saved
// intent and compare its fingerprint/scopes; caller-supplied Scope is not proof
// of a durable record. This validator never converts unknown usage to zero.
func ValidateSystemOneUsagePreparation(in SystemOneUsagePreparation) error {
	want, err := in.Scope.NativeIntentReference()
	if err != nil || want != in.Intent || in.Version != 1 || in.Scope.Request.Protocol != NativeIntentProtocolSystemOne || in.Operation.Protocol != model.ProtocolSystemOne || in.Operation.OperationID != in.Scope.Key.OperationID ||
		in.Operation.APIKeyID != in.Scope.Key.APIKeyID || in.Operation.UserID != in.Scope.UserID || in.Operation.OriginalWireDigest != in.Scope.OriginalWireDigest {
		return billing.ErrNativeIntentConflict
	}
	var selected *model.AttemptFact
	for i := range in.Operation.Attempts {
		if in.Operation.Attempts[i].Sequence == in.Intent.AttemptSequence {
			if selected != nil {
				return billing.ErrNativeIntentConflict
			}
			selected = &in.Operation.Attempts[i]
		}
	}
	if selected == nil || selected.Identity != in.Scope.Attempt.Prepared {
		return billing.ErrNativeIntentConflict
	}
	u := selected.Usage
	if u == nil {
		if in.InputTokens != nil || in.OutputTokens != nil || in.Settlement != nil || in.SettlementUsage != nil {
			return billing.ErrNativeUsageConflict
		}
		return nil
	}
	for _, pair := range []struct {
		projected *int64
		observed  *model.TokenMeasurement
	}{{in.InputTokens, u.Input}, {in.OutputTokens, u.Output}} {
		if (pair.projected == nil) != (pair.observed == nil) || pair.projected != nil && (*pair.projected < 0 || *pair.projected != pair.observed.Value) {
			return billing.ErrNativeUsageConflict
		}
	}
	if in.ReturnedModel != selected.ReturnedModel {
		return billing.ErrNativeUsageConflict
	}
	if in.InputTokens != nil && in.OutputTokens != nil && (u.Confidence != model.UsageFinal || !u.TerminalConfirmed || selected.Terminal != model.TerminalCompleted || selected.Send == model.SendNotSent) {
		return billing.ErrNativeUsageConflict
	}
	return nil
}

func (s *GatewayService) systemOneStatsCost(ctx context.Context, stats *NativeIntentStatsPricing, tokens UsageTokens, total float64, at time.Time) (*float64, error) {
	if stats == nil {
		return nil, ErrUsagePricingPending
	}
	if stats.State == NativeIntentStatsNotApplicable {
		return nil, nil
	}
	if stats.State != NativeIntentStatsCaptured {
		return nil, ErrUsagePricingPending
	}
	if stats.CustomPricing != nil {
		if cost := calculateStatsCost(stats.CustomPricing, tokens, 1); cost != nil {
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
	}
	price, err := stats.DefaultPrice.Restore()
	if err != nil {
		return nil, err
	}
	price.longContextPricingEnabled = stats.LongContextEnabled
	cost, err := s.billingService.CalculateCostUnified(CostInput{Ctx: ctx, Model: stats.Model, Tokens: tokens, RequestCount: 1, RateMultiplier: 1, PricingAt: at, Resolved: price})
	if err != nil {
		return nil, err
	}
	if cost == nil || cost.TotalCost <= 0 {
		return nil, nil
	}
	return &cost.TotalCost, nil
}
