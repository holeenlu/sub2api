//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

type nativeIntentPricingQuotaUpdater struct{ calls int }

func (u *nativeIntentPricingQuotaUpdater) UpdateQuotaUsed(context.Context, int64, float64) error {
	u.calls++
	return nil
}

func (u *nativeIntentPricingQuotaUpdater) UpdateRateLimitUsage(context.Context, int64, float64) error {
	u.calls++
	return nil
}

// These are synthetic policy/identity fixtures, not evidence of a real permit,
// transport send, durable intent or monetary transaction.
func nativeIntentPricingFixture() (*OpenAIGatewayService, nativeIntentRequestPricingInput, nativeIntentAttemptPricingInput) {
	at := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	group := &Group{ID: 9, Name: "serving group", Platform: PlatformOpenAI, RateMultiplier: 2, Hydrated: true,
		ImageRateMultiplier: 1, VideoRateMultiplier: 1, LongContextPricingEnabled: true,
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"native-group-model"}, BillingMode: BillingModeToken,
			InputPrice: float64Ptr(4e-6), CacheWritePrice: float64Ptr(0), ImageOutputPrice: float64Ptr(0),
			FastMultiplier: float64Ptr(3), ReasoningEffortMultipliers: map[string]float64{"high": 2}}}}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"native-catalog-model":  {InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6},
		"native-returned-model": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6},
		"native-channel-model":  {InputCostPerToken: 3e-6, OutputCostPerToken: 5e-6},
		"native-image-only":     {OutputCostPerImage: 0.25, TokenPricingAbsent: true},
	}}
	channels := newChannelServiceWithPricings(group.ID, []ChannelModelPricing{tokenPricingForModels([]string{"native-channel-model"}, 8)})
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	svc := &OpenAIGatewayService{cfg: cfg, billingService: NewBillingService(cfg, prices), channelService: channels}
	svc.resolver = NewModelPricingResolver(channels, svc.billingService)
	key := &APIKey{ID: 2, UserID: 3, GroupID: &group.ID, Group: group, Quota: 10, RateLimit5h: 5, Key: "secret-key-sentinel"}
	user := &User{ID: 3, Role: "user", Email: "private-email-sentinel", PasswordHash: "password-sentinel"}
	account := &Account{ID: 7, Name: "selected account", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		RateMultiplier: float64Ptr(0.5), Credentials: map[string]any{"api_key": "credential-sentinel"},
		Extra: map[string]any{"quota_daily_limit": 20.0, "quota_weekly_limit": 50.0, openAILongContextBillingEnabledKey: true}}
	h := [32]byte{1}
	request := nativeIntentRequestPricingInput{APIKey: key, User: user, APIKeyService: &nativeIntentPricingQuotaUpdater{}, PricingAt: at,
		ChannelUsageFields: ChannelUsageFields{OriginalModel: "native-catalog-model", ChannelMappedModel: "native-catalog-model", BillingModelSource: BillingModelSourceUpstream, ChannelID: 1},
		QuotaPlatform:      PlatformOpenAI, RequestPayloadHash: "request-owner-hash", KnownCanonical: &billing.Identity{RequestID: "http-owner-id", APIKeyID: key.ID}}
	attempt := nativeIntentAttemptPricingInput{Account: account, CredentialAccount: account, Sequence: 4, CapturedAt: at.Add(time.Second),
		Model: "native-catalog-model", UpstreamModel: "native-catalog-model", ServiceTier: "priority", ReasoningEffort: "high",
		Prepared: model.PreparedIdentity{OperationID: "technical-operation", ResourceID: account.ID, Owner: model.ResourceOwner{AccountID: account.ID},
			UserID: user.ID, APIKeyID: key.ID, GroupID: group.ID, GroupBound: true, Mode: "native", Transport: "http",
			GrantDigest: h, TargetDigest: h, WireDigest: h, BodyKind: model.BodyReplayable, BodyDigest: h}}
	return svc, request, attempt
}

func captureNativeIntentPricingFixture(t *testing.T, svc *OpenAIGatewayService, request nativeIntentRequestPricingInput, attempt nativeIntentAttemptPricingInput) (*nativeIntentRequestPricing, NativeIntentAttemptScope, *nativeIntentLivePricing) {
	t.Helper()
	r, err := svc.captureNativeIntentRequestPricing(t.Context(), request)
	require.NoError(t, err)
	a, live, err := svc.captureNativeIntentAttemptPricing(t.Context(), r, attempt)
	require.NoError(t, err)
	return r, a, live
}

func TestNativeIntentPricingCaptureSurvivesAllPolicyPublications(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	group := in.APIKey.Group
	group.SubscriptionType, group.PeakRateEnabled, group.PeakStart, group.PeakEnd, group.PeakRateMultiplier = SubscriptionTypeSubscription, true, "00:00", "23:59", 3
	group.ImageRateIndependent, group.ImageRateMultiplier = true, 0
	group.VideoRateIndependent, group.VideoRateMultiplier = true, 0.7
	group.WebSearchPricePerCall, group.ImagePrice1K = float64Ptr(0), float64Ptr(0)
	group.VideoModelPrices = map[string]map[string]float64{"grok-imagine-video": {"720p": 0}}
	rate := &userGroupRateResolverRepoStub{rate: float64Ptr(1.25)}
	svc.userGroupRateResolver = newUserGroupRateResolver(rate, nil, time.Minute, nil, "test")
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{group.ID},
		ModelPricing: []ChannelModelPricing{tokenPricingForModels([]string{"native-channel-model"}, 8)},
		AccountStatsPricingRules: []AccountStatsPricingRule{{AccountIDs: []int64{ai.Account.ID}, Pricing: []ChannelModelPricing{
			{Platform: PlatformOpenAI, Models: []string{"native-channel-model"}, BillingMode: BillingModeToken, InputPrice: float64Ptr(0), ReasoningEffortMultipliers: map[string]float64{"high": 0.5}},
		}}}}
	svc.channelService.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{group.ID: PlatformOpenAI}))
	ai.Model, ai.UpstreamModel = "native-channel-model", "native-channel-model"
	request, err := svc.captureNativeIntentRequestPricing(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, 1, rate.calls)
	oldToken, oldImage := computePeakAwareMultipliers(in.APIKey, 1.25, in.PricingAt)

	// Publish between request capture and actual attempt capture. A second Pin
	// here would mix old personal/group policy with the new catalog/channel.
	pricingViewPublicationMu.Lock()
	svc.billingService.pricingService.mu.Lock()
	svc.billingService.pricingService.pricingData = map[string]*LiteLLMModelPricing{"native-channel-model": {InputCostPerToken: 99, OutputCostPerToken: 99}}
	svc.billingService.pricingService.mu.Unlock()
	svc.channelService.cache.Store(populateChannelCache([]Channel{{ID: 1, Status: StatusActive, GroupIDs: []int64{9},
		ModelPricing: []ChannelModelPricing{tokenPricingForModels([]string{"native-channel-model"}, 99)}}}, map[int64]string{9: PlatformOpenAI}))
	pricingViewPublicationMu.Unlock()
	*rate.rate = 90
	svc.userGroupRateResolver.cache.Flush()
	group.RateMultiplier, group.PeakRateMultiplier = 90, 90
	*group.ImagePrice1K, *group.WebSearchPricePerCall = 90, 90
	*group.ModelPricing[0].CacheWritePrice = 90
	group.ModelPricing[0].ReasoningEffortMultipliers["high"] = 90
	in.User.Role = "super_admin"
	scope, live, err := svc.captureNativeIntentAttemptPricing(t.Context(), request, ai)
	require.NoError(t, err)
	require.Equal(t, 1, rate.calls, "attempt capture must not resolve the user rate again")
	require.Equal(t, 1.25, request.Scope.BaseMultiplier)
	require.Equal(t, oldToken, request.Scope.TokenMultiplier)
	require.Equal(t, oldImage, request.Scope.ImageMultiplier)
	require.Equal(t, 0.7, request.Scope.VideoMultiplier)
	require.Equal(t, "user", request.Scope.UserRole)
	require.Equal(t, in.PricingAt, scope.FrozenPricing.Policy.PricingAt)
	require.Equal(t, 8e-6, scope.FrozenPricing.Resolved.BasePricing.InputPricePerToken)
	require.Equal(t, NativeIntentStatsCaptured, scope.Billing.Stats.State)
	require.Zero(t, *scope.Billing.Stats.CustomPricing.InputPrice)
	require.Equal(t, 3e-6, scope.Billing.Stats.DefaultPrice.BasePricing.InputPricePerToken)
	require.True(t, scope.Billing.Stats.LongContextEnabled)
	require.Equal(t, 0.5, scope.FrozenPricing.Policy.AccountRateMultiplier)
	require.True(t, scope.FrozenPricing.Policy.Balance)
	require.Nil(t, scope.FrozenPricing.Policy.Request)
	require.Equal(t, ai.Prepared, scope.Prepared)
	require.Equal(t, ai.Sequence, scope.Sequence)

	*ai.Account.RateMultiplier = 90
	ai.Account.Extra["quota_daily_limit"] = 0.0
	ai.Account.Extra[openAILongContextBillingEnabledKey] = false
	ai.Account.Name = "changed"
	in.APIKey.Quota, in.APIKey.RateLimit5h = 0, 0
	*in.KnownCanonical = billing.Identity{RequestID: "changed", APIKeyID: 99}
	require.Equal(t, 0.5, scope.FrozenPricing.Policy.AccountRateMultiplier)
	require.True(t, *scope.LongContextEnabled)
	require.True(t, scope.Billing.AccountQuotaHasDailyLimit)
	require.True(t, scope.FrozenPricing.Policy.KeyQuota)
	require.True(t, scope.FrozenPricing.Policy.KeyRateLimit)
	require.Equal(t, "selected account", *scope.Billing.AccountNameSnapshot)
	require.Equal(t, "http-owner-id", request.Scope.Billing.KnownCanonical.RequestID)
	card, err := live.Resolve(t.Context(), "native-group-model")
	require.NoError(t, err)
	require.True(t, card.BasePricing.cacheWriteFlatOverride)
	require.Zero(t, card.BasePricing.CacheCreationPricePerToken)
	require.Equal(t, 2.0, card.BasePricing.ReasoningEffortMultipliers["high"])
	hosted, err := live.HostedPricing("native-channel-model")
	require.NoError(t, err)
	require.Zero(t, *hosted.ImageOverrides.Price1K)
	require.Zero(t, *hosted.WebSearchPerCall)
	*hosted.WebSearchPerCall = 80
	still, err := live.HostedPricing("native-channel-model")
	require.NoError(t, err)
	require.Zero(t, *still.WebSearchPerCall)
	require.Zero(t, in.APIKeyService.(*nativeIntentPricingQuotaUpdater).calls)
	encoded, err := json.Marshal(struct {
		Request NativeIntentRequestScope
		Attempt NativeIntentAttemptScope
	}{request.Scope, scope})
	require.NoError(t, err)
	for _, secret := range []string{"secret-key-sentinel", "private-email-sentinel", "password-sentinel", "credential-sentinel", "Credentials", `"Body":`, "Headers"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestNativeIntentPricingHydratedDefaultsAreNotMissing(t *testing.T) {
	for _, perImage := range []bool{false, true} {
		svc, in, ai := nativeIntentPricingFixture()
		in.APIKey.Group = &Group{ID: *in.APIKey.GroupID, Hydrated: true, ImageCountBilling: perImage,
			Platform: PlatformOpenAI, RateMultiplier: 1}
		require.True(t, groupMediaPricingLooksIncomplete(in.APIKey.Group), "legacy heuristic also matches a valid full group")
		r, a, _ := captureNativeIntentPricingFixture(t, svc, in, ai)
		require.NotNil(t, r.Scope.Billing.GroupImageCountBilling)
		require.Equal(t, perImage, *r.Scope.Billing.GroupImageCountBilling)
		require.False(t, a.FrozenPricing.Policy.Pending)
		in.APIKey.Group.ImageCountBilling = !perImage
		require.Equal(t, perImage, *r.Scope.Billing.GroupImageCountBilling)
	}
	svc, in, _ := nativeIntentPricingFixture()
	in.APIKey.Group.Hydrated = false
	r, err := svc.captureNativeIntentRequestPricing(t.Context(), in)
	require.Nil(t, r)
	require.ErrorIs(t, err, errNativeIntentPricingDependency)
}

func TestNativeIntentPricingUnboundKeyDoesNotLoadChannels(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	svc.channelService.storeErrorCache(errors.New("unrelated channel database unavailable"))
	in.APIKey.Group, in.APIKey.GroupID = nil, nil
	in.QuotaPlatform = ""
	ai.Prepared.GroupID, ai.Prepared.GroupBound = 0, false
	r, a, live := captureNativeIntentPricingFixture(t, svc, in, ai)
	require.Nil(t, r.view.resolver.channelService)
	require.Nil(t, r.view.resolver.channelSnapshot)
	require.Nil(t, r.Scope.GroupID)
	require.False(t, *r.Scope.Billing.GroupImageCountBilling)
	require.Equal(t, NativeIntentStatsNotApplicable, a.Billing.Stats.State)
	card, err := live.Resolve(t.Context(), ai.Model)
	require.NoError(t, err)
	require.Equal(t, 2e-6, card.BasePricing.InputPricePerToken)
}

func TestNativeIntentPricingReturnedScopesCannotMutateLiveCapture(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	in.ChannelUsageFields.BillingModelSource = BillingModelSourceRequested
	r, err := svc.captureNativeIntentRequestPricing(t.Context(), in)
	require.NoError(t, err)
	*r.Scope.GroupID = 999
	r.Scope.OriginalModel = "invented-alias"
	r.Scope.Mode = UsageBillingModeLogOnly
	r.Scope.Billing.KnownCanonical.RequestID = "edited-copy"
	*r.Scope.Billing.GroupImageCountBilling = true
	*r.Scope.Billing.Hosted.WebSearchPerCall = 99
	a, live, err := svc.captureNativeIntentAttemptPricing(t.Context(), r, ai)
	require.NoError(t, err)
	require.Equal(t, ai.Model, a.BaselineModel)
	require.True(t, a.FrozenPricing.Policy.Balance)
	require.Equal(t, "http-owner-id", r.scope.Billing.KnownCanonical.RequestID)
	require.False(t, *r.scope.Billing.GroupImageCountBilling)
	require.Equal(t, defaultWebSearchPricePerCall, *r.scope.Billing.Hosted.WebSearchPerCall)
	a.CandidateModels[0] = "edited-copy"
	a.FrozenPricing.Resolved.BasePricing.InputPricePerToken = 99
	card, err := live.Resolve(t.Context(), ai.Model)
	require.NoError(t, err)
	require.Equal(t, 2e-6, card.BasePricing.InputPricePerToken)
	card.BasePricing.InputPricePerToken = 100
	card, err = live.Resolve(t.Context(), ai.Model)
	require.NoError(t, err)
	require.Equal(t, 2e-6, card.BasePricing.InputPricePerToken)
}

func TestNativeIntentPricingExplicitZeroPrivateFlagsAndDetachedGroup(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	in.ChannelUsageFields.OriginalModel, in.ChannelUsageFields.BillingModelSource = "native-group-model", BillingModelSourceRequested
	in.APIKey.Group.ImagePrice1K, in.APIKey.Group.SearchPricePer1k = float64Ptr(0), float64Ptr(0)
	in.APIKey.Group.VideoModelPrices = map[string]map[string]float64{"grok-imagine-video": {"720p": 0}}
	r, a, live := captureNativeIntentPricingFixture(t, svc, in, ai)
	*in.APIKey.Group.ModelPricing[0].CacheWritePrice = 5
	*in.APIKey.Group.ModelPricing[0].FastMultiplier = 5
	*in.APIKey.Group.ImagePrice1K = 5
	*in.APIKey.Group.SearchPricePer1k = 5
	in.APIKey.Group.VideoModelPrices["grok-imagine-video"]["720p"] = 5
	restored, err := a.FrozenPricing.Resolved.Restore()
	require.NoError(t, err)
	require.True(t, restored.BasePricing.cacheWriteFlatOverride)
	require.True(t, restored.BasePricing.CacheCreationPriceExplicit)
	require.True(t, restored.BasePricing.ImageOutputPriceExplicit)
	require.Zero(t, restored.BasePricing.CacheCreationPricePerToken)
	require.Zero(t, restored.BasePricing.ImageOutputPricePerToken)
	require.Equal(t, 3.0, *restored.BasePricing.FastMultiplier)
	require.False(t, a.FrozenPricing.Policy.Pending, "explicit free is a known price")
	require.Zero(t, r.group.VideoModelPrices["grok-imagine-video"]["720p"])
	hosted, err := live.HostedPricing(a.BaselineModel)
	require.NoError(t, err)
	require.Zero(t, *hosted.ImageOverrides.Price1K)
	require.Zero(t, *hosted.SearchPer1k)
	intent := NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: ai.Prepared.OperationID, APIKeyID: in.APIKey.ID},
		UserID: in.User.ID, OriginalWireDigest: [32]byte{2}, Request: r.Scope, Attempt: a}
	durableShape, err := PrepareNativeUsageIntent(intent)
	require.NoError(t, err, "only validates the whitelist shape; no DB or send")
	require.Nil(t, durableShape.Attempt.FrozenPricing.Policy.Request)
}

func TestNativeIntentPricingFundingMatchesOriginalPolicyModes(t *testing.T) {
	for _, tc := range []struct {
		name, role                                            string
		subscription, simple, keyWindow, noWindow             bool
		mode                                                  UsageBillingMode
		balance, sub, keyQuota, keyRate, accountQuota, exempt bool
	}{
		{name: "ordinary", role: "user", mode: UsageBillingModeFull, balance: true, keyQuota: true, keyRate: true, accountQuota: true},
		{name: "super_admin", role: "super_admin", mode: UsageBillingModeFull, keyQuota: true, keyRate: true, accountQuota: true, exempt: true},
		{name: "subscription", role: "user", subscription: true, mode: UsageBillingModeFull, sub: true, keyQuota: true, keyRate: true, accountQuota: true},
		{name: "super_subscription", role: "super_admin", subscription: true, mode: UsageBillingModeFull, sub: true, keyQuota: true, keyRate: true, accountQuota: true},
		{name: "simple_log_only", role: "super_admin", subscription: true, simple: true, mode: UsageBillingModeLogOnly},
		{name: "simple_key_window", role: "super_admin", subscription: true, simple: true, keyWindow: true, mode: UsageBillingModeKeyWindowOnly, keyRate: true},
		{name: "simple_no_window", role: "user", simple: true, keyWindow: true, noWindow: true, mode: UsageBillingModeLogOnly},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, ai := nativeIntentPricingFixture()
			in.User.Role = tc.role
			if tc.simple {
				svc.cfg.RunMode = config.RunModeSimple
			}
			svc.cfg.SimpleModeKeyRateLimitEnabled = tc.keyWindow
			if tc.noWindow {
				in.APIKey.RateLimit5h = 0
			}
			if tc.subscription {
				in.APIKey.Group.SubscriptionType = SubscriptionTypeSubscription
				in.Subscription = &UserSubscription{ID: 11, UserID: in.User.ID, GroupID: *in.APIKey.GroupID}
			}
			r, a, _ := captureNativeIntentPricingFixture(t, svc, in, ai)
			p := a.FrozenPricing.Policy
			require.Equal(t, tc.mode, r.Scope.Mode)
			require.Equal(t, []bool{tc.balance, tc.sub, tc.keyQuota, tc.keyRate, tc.accountQuota, tc.exempt},
				[]bool{p.Balance, p.Subscription, p.KeyQuota, p.KeyRateLimit, p.AccountQuota, *a.FrozenPricing.BalanceExempt})
			require.Nil(t, p.Request)
			// Independent oracle: the original builder with a post-response cost.
			cmd := buildUsageBillingCommand("observed-response", &UsageLog{Model: ai.Model, InputTokens: 10}, &postUsageBillingParams{
				Cost: &CostBreakdown{TotalCost: 2, ActualCost: 3}, User: in.User, APIKey: in.APIKey, Account: ai.Account,
				Subscription: in.Subscription, IsSubscriptionBill: r.Scope.Billing.IsSubscriptionBill,
				AccountRateMultiplier: 0.5, APIKeyService: in.APIKeyService,
				SimpleModeKeyRateLimitOnly: tc.mode == UsageBillingModeKeyWindowOnly, UsageLogOnly: tc.mode == UsageBillingModeLogOnly})
			require.Equal(t, []bool{p.Balance, p.Subscription, p.KeyQuota, p.KeyRateLimit, p.AccountQuota, *a.FrozenPricing.BalanceExempt},
				[]bool{cmd.BalanceCost > 0, cmd.SubscriptionCost > 0, cmd.APIKeyQuotaCost > 0, cmd.APIKeyRateLimitCost > 0, cmd.AccountQuotaCost > 0, cmd.BalanceExempt})
		})
	}
}

func TestNativeIntentPricingShadowUsesSelectedFundingAndCredentialPolicy(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	parent := &Account{ID: 18, Platform: PlatformOpenAI, Type: AccountTypeOAuth, RateMultiplier: float64Ptr(9),
		Extra: map[string]any{openAILongContextBillingEnabledKey: false}}
	ai.Account.ParentAccountID, ai.Account.Type = &parent.ID, AccountTypeOAuth
	ai.CredentialAccount, ai.Prepared.Owner.AccountID = parent, parent.ID
	ai.Model, ai.UpstreamModel = "gpt-6.1-sol", "gpt-6.1-sol"
	_, a, _ := captureNativeIntentPricingFixture(t, svc, in, ai)
	require.Equal(t, ai.Account.ID, a.Prepared.ResourceID)
	require.Equal(t, parent.ID, a.Prepared.Owner.AccountID)
	require.Equal(t, 0.5, a.FrozenPricing.Policy.AccountRateMultiplier)
	require.Equal(t, ai.Account.SupportsAccountQuota() && ai.Account.HasAnyQuotaLimit(), a.FrozenPricing.Policy.AccountQuota)
	require.True(t, a.Billing.CredentialOAuthLike)
	require.False(t, *a.LongContextEnabled)
	require.True(t, a.ModelLongContextExempt)
	require.False(t, a.Billing.Stats.LongContextEnabled)
	require.False(t, a.Billing.OpenAICacheWriteTTL)
	parent.Extra[openAILongContextBillingEnabledKey] = true
	parent.Type = AccountTypeAPIKey
	require.True(t, a.Billing.CredentialOAuthLike)
	require.False(t, *a.LongContextEnabled)
	require.True(t, a.ModelLongContextExempt)
	request, err := svc.captureNativeIntentRequestPricing(t.Context(), in)
	require.NoError(t, err)
	_, _, err = svc.captureNativeIntentAttemptPricing(t.Context(), request, ai)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict, "a changed/incorrect parent cannot be substituted")
}

func TestNativeIntentPricingReturnedModelUsesOldViewAndOriginalGuards(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	in.ChannelUsageFields.BillingModelSource = BillingModelSourceResponse
	ai.ReturnedModelViewRef = "test-live-owner-reference"
	r, a, live := captureNativeIntentPricingFixture(t, svc, in, ai)
	require.NotContains(t, a.AdditionalPrices, "native-returned-model")
	pricingViewPublicationMu.Lock()
	svc.billingService.pricingService.mu.Lock()
	svc.billingService.pricingService.pricingData = map[string]*LiteLLMModelPricing{"native-returned-model": {InputCostPerToken: 99}}
	svc.billingService.pricingService.mu.Unlock()
	pricingViewPublicationMu.Unlock()
	candidates, err := live.ReturnedModelCandidates(t.Context(), "native-returned-model", false, false)
	require.NoError(t, err)
	require.Equal(t, []string{"native-returned-model"}, candidates)
	quote := func(name string) *CostBreakdown {
		card, err := live.Resolve(t.Context(), name)
		require.NoError(t, err)
		cost, err := svc.billingService.CalculateCostUnified(CostInput{Ctx: t.Context(), Model: name, Resolved: card,
			PricingAt: r.Scope.PricingAt, Tokens: UsageTokens{InputTokens: 1_000_000}, RateMultiplier: r.Scope.TokenMultiplier})
		require.NoError(t, err)
		return cost
	}
	baseline, returned := quote(a.BaselineModel), quote(candidates[0])
	selected, err := live.AdoptReturnedModel(t.Context(), "native-returned-model", candidates[0], false, false, baseline, returned)
	require.NoError(t, err)
	require.Equal(t, 1e-6, selected.BasePricing.InputPricePerToken)
	for _, tc := range []struct {
		name, declared  string
		conflict, media bool
		cost            *CostBreakdown
	}{
		{name: "conflict", declared: "native-returned-model", conflict: true, cost: returned},
		{name: "hosted", declared: "native-returned-model", media: true, cost: returned},
		{name: "unidentified_family", declared: "invented-haiku-provider", cost: returned},
		{name: "same_model", declared: a.BaselineModel, cost: baseline},
		{name: "higher_cost", declared: "native-returned-model", cost: &CostBreakdown{TotalCost: baseline.TotalCost + 1}},
		{name: "zero_cost", declared: "native-returned-model", cost: &CostBreakdown{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected, err := live.AdoptReturnedModel(t.Context(), tc.declared, tc.declared, tc.conflict, tc.media, baseline, tc.cost)
			require.NoError(t, err)
			require.Nil(t, selected)
		})
	}
	_, err = live.AdoptReturnedModel(t.Context(), "native-returned-model", "not-selected", false, false, baseline, returned)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	// A cheaper catalog response cannot bypass an explicit channel baseline.
	live.baselineModel = "native-channel-model"
	selected, err = live.AdoptReturnedModel(t.Context(), "native-returned-model", "native-returned-model", false, false, quote(live.baselineModel), returned)
	require.NoError(t, err)
	require.Nil(t, selected)
	var lost *nativeIntentLivePricing
	_, err = lost.ReturnedModelCandidates(t.Context(), "native-returned-model", false, false)
	require.ErrorIs(t, err, errNativeIntentPricingDependency)
}

func TestNativeIntentPricingImageOnlyMissingAndDependencyAreDistinct(t *testing.T) {
	svc, in, ai := nativeIntentPricingFixture()
	in.ChannelUsageFields = ChannelUsageFields{OriginalModel: "native-image-only", BillingModelSource: BillingModelSourceRequested}
	ai.Model, ai.UpstreamModel = "native-image-only", "native-image-only"
	r, a, live := captureNativeIntentPricingFixture(t, svc, in, ai)
	require.True(t, a.FrozenPricing.Policy.Pending, "image-only catalog row must never become a free token card")
	require.Nil(t, a.FrozenPricing.Resolved)
	require.Nil(t, a.FrozenPricing.Policy.Request)
	_, err := live.Resolve(t.Context(), "native-image-only")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.NotErrorIs(t, err, errNativeIntentPricingDependency)
	hosted, err := live.HostedPricing("native-image-only")
	require.NoError(t, err)
	require.Equal(t, 0.25, *hosted.ImageDefaults.Price1K)
	require.Nil(t, r.Scope.Billing.Hosted.ImageDefaults, "request capture must not invent a backend model")
	require.Nil(t, hosted.ImageOverrides.Price1K)
	pricingViewPublicationMu.Lock()
	svc.billingService.pricingService.mu.Lock()
	svc.billingService.pricingService.pricingData = map[string]*LiteLLMModelPricing{"native-image-only": {OutputCostPerImage: 99, TokenPricingAbsent: true}}
	svc.billingService.pricingService.mu.Unlock()
	pricingViewPublicationMu.Unlock()
	still, err := live.HostedPricing("native-image-only")
	require.NoError(t, err)
	require.Equal(t, 0.25, *still.ImageDefaults.Price1K)
	_, err = live.HostedPricing("unobserved-image-backend")
	require.ErrorIs(t, err, errNativeIntentPricingDependency)
	cause := errors.New("channel storage offline")
	svc.channelService.storeErrorCache(cause)
	failed, err := svc.captureNativeIntentRequestPricing(t.Context(), in)
	require.Nil(t, failed)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, errNativeIntentPricingDependency)
	require.NotErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestNativeIntentPricingImageCandidatePreservesBaseline(t *testing.T) {
	for _, tc := range []struct {
		name, imageModel, capturedModel string
		additional                      bool
	}{
		{name: "absent"},
		{name: "blank", imageModel: " \t "},
		{name: "baseline_duplicate", imageModel: " native-catalog-model ", capturedModel: "native-catalog-model"},
		{name: "group", imageModel: " native-group-model ", capturedModel: "native-group-model", additional: true},
		{name: "channel", imageModel: "native-channel-model", capturedModel: "native-channel-model", additional: true},
		{name: "catalog", imageModel: "native-returned-model", capturedModel: "native-returned-model", additional: true},
		{name: "image_only", imageModel: "native-image-only", capturedModel: "native-image-only"},
		{name: "missing", imageModel: "native-missing-image-model", capturedModel: "native-missing-image-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, in, ai := nativeIntentPricingFixture()
			request, baseline, _ := captureNativeIntentPricingFixture(t, svc, in, ai)
			ai.ImageBillingModel = tc.imageModel
			attempt, live, err := svc.captureNativeIntentAttemptPricing(t.Context(), request, ai)
			require.NoError(t, err)
			require.Equal(t, baseline.BaselineModel, attempt.BaselineModel)
			require.Equal(t, baseline.CandidateModels, attempt.CandidateModels, "image candidate must not become a text fallback")
			require.Equal(t, baseline.FrozenPricing, attempt.FrozenPricing)
			require.Nil(t, attempt.FrozenPricing.Policy.Request, "capture must not manufacture usage")
			if tc.capturedModel == "" || tc.capturedModel == baseline.BaselineModel {
				require.Equal(t, baseline.Billing.HostedPrices, attempt.Billing.HostedPrices)
				require.Empty(t, attempt.AdditionalPrices)
				return
			}
			require.Len(t, attempt.Billing.HostedPrices, len(baseline.Billing.HostedPrices)+1)
			hosted, err := live.HostedPricing(tc.capturedModel)
			require.NoError(t, err)
			require.Equal(t, hosted, attempt.Billing.HostedPrices[tc.capturedModel])
			require.NotSame(t, hosted.ImageDefaults.Price1K, attempt.Billing.HostedPrices[tc.capturedModel].ImageDefaults.Price1K)
			require.Equal(t, svc.billingService.getDefaultImagePrice(tc.capturedModel, ImageBillingSize2K), *hosted.ImageDefaults.Price2K)
			if tc.additional {
				require.Len(t, attempt.AdditionalPrices, 1)
				card := attempt.AdditionalPrices[tc.capturedModel]
				price, err := card.Restore()
				require.NoError(t, err)
				original := svc.resolver.Resolve(t.Context(), PricingInput{Model: tc.capturedModel, GroupID: in.APIKey.GroupID, Group: in.APIKey.Group})
				require.Equal(t, original, price, "preserve original source, explicit zeros and private policy flags")
			} else {
				require.NotContains(t, attempt.AdditionalPrices, tc.capturedModel, "missing token card must not become a zero card")
				_, err := live.Resolve(t.Context(), tc.capturedModel)
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
			}
			_, err = PrepareNativeUsageIntent(NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: ai.Prepared.OperationID, APIKeyID: in.APIKey.ID},
				UserID: in.User.ID, OriginalWireDigest: [32]byte{2}, Request: request.Scope, Attempt: attempt})
			require.NoError(t, err, "extra image policies must survive the durable whitelist")
		})
	}
}

func TestNativeIntentPricingRejectsMissingTrustedInputs(t *testing.T) {
	for _, tc := range []string{"timestamp", "group", "subscription_owner", "key_updater", "typed_nil_updater", "canceled", "sequence", "prepared", "credential_owner", "response_view"} {
		t.Run(tc, func(t *testing.T) {
			svc, in, ai := nativeIntentPricingFixture()
			ctx := t.Context()
			switch tc {
			case "timestamp":
				in.PricingAt = time.Time{}
			case "group":
				in.APIKey.Group = nil
			case "subscription_owner":
				in.Subscription = &UserSubscription{ID: 1, UserID: 99, GroupID: 9}
			case "key_updater":
				in.APIKeyService = nil
			case "typed_nil_updater":
				in.APIKeyService = (*nativeIntentPricingQuotaUpdater)(nil)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "sequence":
				ai.Sequence = 0
			case "prepared":
				ai.Prepared = model.PreparedIdentity{}
			case "credential_owner":
				ai.Prepared.Owner.AccountID = 99
			case "response_view":
				in.ChannelUsageFields.BillingModelSource = BillingModelSourceResponse
			}
			r, err := svc.captureNativeIntentRequestPricing(ctx, in)
			if err == nil {
				var a NativeIntentAttemptScope
				var live *nativeIntentLivePricing
				a, live, err = svc.captureNativeIntentAttemptPricing(ctx, r, ai)
				require.Nil(t, live)
				require.Nil(t, a.FrozenPricing)
			}
			require.Error(t, err)
		})
	}
}
