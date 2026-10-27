package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

var ErrOpenAICloudProbeSettlementFailed = errors.New("Cloud verification settlement failed")

// V3 fences consumers that accept v1/v2 but ignore SamplingUsage and would
// price the cumulative root as one context. It keeps v1's frozen billing policy;
// the separate v2 policy change is not enabled by this incident fix. Old readers
// durably archive unknown versions without charging. Drain old owners/workers
// before admitting new Cloud turns to avoid archived (unsettled) v3 records.
const cloudBillingSnapshotWriteVersion = 3

type cloudPermanentSettlementError string

func (e cloudPermanentSettlementError) Error() string {
	return "Cloud settlement retained: " + string(e)
}

// OpenAICloudBillingSnapshot is a whitelist, never a serialized account/key/user.
// One snapshot belongs to one upstream turn, including all client tool round trips.
// Pricing is captured before dispatch so a restarted consumer cannot re-price it
// using a changed account, group, channel or model catalog.
type OpenAICloudBillingSnapshot struct {
	Version                  int       `json:"version"`
	UserID                   int64     `json:"user_id"`
	APIKeyID                 int64     `json:"api_key_id"`
	AccountID                int64     `json:"account_id"`
	GroupID                  *int64    `json:"group_id,omitempty"`
	SubscriptionID           *int64    `json:"subscription_id,omitempty"`
	UserRole                 string    `json:"user_role"`
	AccountName              string    `json:"account_name"`
	GroupName                string    `json:"group_name"`
	GroupPlatform            string    `json:"group_platform"`
	SubscriptionType         string    `json:"subscription_type"`
	KeyQuota                 float64   `json:"key_quota"`
	KeyRateLimit5h           float64   `json:"key_rate_limit_5h"`
	KeyRateLimit1d           float64   `json:"key_rate_limit_1d"`
	KeyRateLimit7d           float64   `json:"key_rate_limit_7d"`
	AccountRate              float64   `json:"account_rate"`
	AccountQuota             float64   `json:"account_quota"`
	AccountDailyQuota        float64   `json:"account_daily_quota"`
	AccountWeeklyQuota       float64   `json:"account_weekly_quota"`
	BaseMultiplier           float64   `json:"base_multiplier"`
	TokenMultiplier          float64   `json:"token_multiplier"`
	LongContextEnabled       bool      `json:"long_context_enabled"`
	GroupLongContextEnabled  bool      `json:"group_long_context_enabled"`
	FreeFast                 bool      `json:"free_fast"`
	SimpleMode               bool      `json:"simple_mode"`
	Model                    string    `json:"model"`
	UpstreamModel            string    `json:"upstream_model"`
	ServiceTier              string    `json:"service_tier"`
	ReasoningEffort          string    `json:"reasoning_effort"`
	RequestedReasoningEffort string    `json:"requested_reasoning_effort"`
	Stream                   bool      `json:"stream"`
	PricingAt                time.Time `json:"pricing_at"`
	InboundEndpoint          string    `json:"inbound_endpoint"`
	UserAgent                string    `json:"user_agent"`
	IPAddress                string    `json:"ip_address"`
	SessionID                string    `json:"session_id"`
	RequestPayloadHash       string    `json:"request_payload_hash"`
	QuotaPlatform            string    `json:"quota_platform"`
	// The ingress identified Codex remote compaction, as on native usage rows.
	NativeCompactionV2 bool `json:"native_compaction_v2,omitempty"`
	ChannelUsageFields
	Prices      map[string]openAICloudPriceSnapshot `json:"prices"`
	HostedTools *openAICloudHostedPriceSnapshot     `json:"hosted_tools,omitempty"`
	Stats       *openAICloudStatsSnapshot           `json:"stats,omitempty"`
	Inflight    *openAICloudInflightSnapshot        `json:"inflight,omitempty"`
	// BillingMode is authoritative only in v2. V1 and the sampling-fenced v3
	// retain the legacy frozen policy and leave this field absent.
	BillingMode usageBillingMode `json:"billing_mode,omitempty"`
}

// billingMode decodes the immutable policy, never the consumer's current
// configuration. V1 did not freeze the simple-window opt-in: preserve its
// historical interpretation rather than invent the missing original decision.
// V3 retains that policy while requiring a sampling-aware consumer. Keep the
// older decoders while live or archived v1/v2 settlements remain readable.
func (snapshot *OpenAICloudBillingSnapshot) billingMode() (usageBillingMode, bool) {
	if snapshot == nil {
		return "", false
	}
	switch snapshot.Version {
	case 1, 3:
		if snapshot.SimpleMode {
			return usageBillingKeyWindowOnly, true
		}
		return usageBillingFull, true
	case 2:
		if !snapshot.BillingMode.valid() {
			return "", false
		}
		if snapshot.BillingMode == usageBillingKeyWindowOnly &&
			snapshot.KeyRateLimit5h <= 0 && snapshot.KeyRateLimit1d <= 0 && snapshot.KeyRateLimit7d <= 0 {
			return "", false
		}
		return snapshot.BillingMode, true
	default:
		return "", false
	}
}

// These are the existing retail per-call/per-image policies, not an inferred
// upstream image model, image token rate or provider invoice. Every image tier
// is frozen before dispatch because its actual dimensions arrive at completion.
type openAICloudHostedPriceSnapshot struct {
	WebSearchPerCall    float64            `json:"web_search_per_call"`
	WebSearchMultiplier float64            `json:"web_search_multiplier"`
	ImagePrices         map[string]float64 `json:"image_prices"`
	ImageMultiplier     float64            `json:"image_multiplier"`
}

type openAICloudPriceSnapshot struct {
	Resolved       ResolvedPricing      `json:"resolved"`
	Channel        *ChannelModelPricing `json:"channel,omitempty"`
	LongContext    bool                 `json:"long_context"`
	CacheWriteFlat bool                 `json:"cache_write_flat"`
	// V2 freezes deterministic recognition for a later reported model. Merely
	// having a heuristic fallback price never authorizes a pricing substitution.
	ResponsePriceIdentified bool `json:"response_price_identified,omitempty"`
}
type openAICloudStatsSnapshot struct {
	CustomPricing      *ChannelModelPricing     `json:"custom_pricing,omitempty"`
	ApplyCustomerPrice bool                     `json:"apply_customer_price"`
	DefaultPrice       openAICloudPriceSnapshot `json:"default_price"`
}
type openAICloudInflightSnapshot struct {
	RequestID string   `json:"request_id"`
	UserID    int64    `json:"user_id"`
	Balance   bool     `json:"balance"`
	Scopes    []string `json:"scopes"`
}
type openAICloudBillingContextKey struct{}
type openAICloudSettlingContextKey struct{}

func cloudPriceSnapshot(price *ResolvedPricing) (openAICloudPriceSnapshot, error) {
	if price == nil || (price.Mode != "" && price.Mode != BillingModeToken && price.Mode != BillingModePerRequest) ||
		((price.Mode == "" || price.Mode == BillingModeToken) && price.BasePricing == nil) {
		return openAICloudPriceSnapshot{}, errors.New("Cloud requires resolved token or per-request pricing before dispatch")
	}
	snapshot := openAICloudPriceSnapshot{Resolved: *price, Channel: price.channelPricing, LongContext: price.longContextPricingEnabled}
	if price.BasePricing != nil {
		snapshot.CacheWriteFlat = price.BasePricing.cacheWriteFlatOverride
	}
	return snapshot, nil
}
func (p openAICloudPriceSnapshot) resolved() *ResolvedPricing {
	out := p.Resolved
	out.channelPricing, out.longContextPricingEnabled = p.Channel, p.LongContext
	if out.BasePricing != nil {
		cloned := *out.BasePricing
		cloned.cacheWriteFlatOverride = p.CacheWriteFlat
		out.BasePricing = &cloned
	}
	return &out
}

func cloudBillingContext(ctx context.Context) *OpenAICloudBillingSnapshot {
	if ctx == nil {
		return nil
	}
	snapshot, _ := ctx.Value(openAICloudBillingContextKey{}).(*OpenAICloudBillingSnapshot)
	return snapshot
}
func cloudSettlingSnapshot(ctx context.Context) *OpenAICloudBillingSnapshot {
	if ctx == nil {
		return nil
	}
	settling, _ := ctx.Value(openAICloudSettlingContextKey{}).(bool)
	if !settling {
		return nil
	}
	return cloudBillingContext(ctx)
}
func CloudBillingSnapshotFromContext(ctx context.Context) json.RawMessage {
	snapshot := cloudBillingContext(ctx)
	if snapshot == nil {
		return nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	return data
}

// CaptureCloudBillingSnapshot must run before turn/start. Only non-secret
// pricing/attribution fields survive its JSON round trip.
func (s *OpenAIGatewayService) CaptureCloudBillingSnapshot(ctx context.Context, in *OpenAIRecordUsageInput) (context.Context, error) {
	if s == nil || in == nil || in.Result == nil || in.APIKey == nil || in.User == nil || in.Account == nil || s.billingService == nil || s.usageBillingRepo == nil {
		return ctx, errors.New("Cloud billing is unavailable")
	}
	key, user, account, result := in.APIKey, in.User, in.Account, in.Result
	if key.ID <= 0 || user.ID <= 0 || account.ID <= 0 || key.UserID != user.ID || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.ParentAccountID != nil || account.VendorID != nil || in.PricingAt.IsZero() {
		return ctx, errors.New("invalid Cloud billing owner")
	}
	multiplier := 1.0
	if s.cfg != nil {
		multiplier = s.cfg.Default.RateMultiplier
	}
	snapshot := &OpenAICloudBillingSnapshot{
		Version: cloudBillingSnapshotWriteVersion, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, GroupID: key.GroupID, UserRole: user.Role,
		AccountName: account.Name, AccountRate: account.BillingRateMultiplier(), AccountQuota: account.GetQuotaLimit(),
		AccountDailyQuota: account.GetQuotaDailyLimit(), AccountWeeklyQuota: account.GetQuotaWeeklyLimit(),
		KeyQuota: key.Quota, KeyRateLimit5h: key.RateLimit5h, KeyRateLimit1d: key.RateLimit1d, KeyRateLimit7d: key.RateLimit7d,
		LongContextEnabled: account.IsOpenAILongContextBillingEnabled(), Model: result.Model, UpstreamModel: result.UpstreamModel,
		ServiceTier: optionalStringValue(result.ServiceTier), ReasoningEffort: optionalStringValue(result.ReasoningEffort),
		RequestedReasoningEffort: optionalStringValue(result.RequestedReasoningEffort), Stream: result.Stream, PricingAt: in.PricingAt,
		InboundEndpoint: in.InboundEndpoint, UserAgent: in.UserAgent, IPAddress: in.IPAddress, SessionID: in.SessionID,
		RequestPayloadHash: in.RequestPayloadHash, QuotaPlatform: in.QuotaPlatform, NativeCompactionV2: in.NativeCompactionV2, ChannelUsageFields: in.ChannelUsageFields,
		Prices: make(map[string]openAICloudPriceSnapshot), SimpleMode: resolveUsageBillingMode(s.cfg, key) != usageBillingFull,
	}
	if snapshot.Version == 2 {
		snapshot.BillingMode = resolveUsageBillingMode(s.cfg, key)
	}
	if key.GroupID != nil {
		if key.Group == nil || key.Group.ID != *key.GroupID {
			return ctx, errors.New("Cloud billing group snapshot unavailable")
		}
		group := key.Group
		multiplier = s.ResolveUserGroupRateMultiplier(ctx, user.ID, group.ID, group.RateMultiplier)
		snapshot.GroupName, snapshot.GroupPlatform, snapshot.SubscriptionType = group.Name, group.Platform, group.SubscriptionType
		snapshot.GroupLongContextEnabled, snapshot.FreeFast = group.LongContextPricingEnabled, group.FreeOpenAIFast
	}
	snapshot.BaseMultiplier = multiplier
	var imageMultiplier float64
	snapshot.TokenMultiplier, imageMultiplier = computePeakAwareMultipliers(key, multiplier, in.PricingAt)
	snapshot.HostedTools = &openAICloudHostedPriceSnapshot{
		WebSearchPerCall:    s.billingService.CalculateWebSearchCost(1, webSearchPricePerCallFromAPIKey(key), 1).TotalCost,
		WebSearchMultiplier: multiplier, ImageMultiplier: imageMultiplier,
		ImagePrices: make(map[string]float64, 3),
	}
	for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
		// Empty model deliberately selects the existing generic image policy.
		// The Cloud agent's text model is not evidence of its image backend.
		snapshot.HostedTools.ImagePrices[tier] = s.billingService.CalculateImageCost("", tier, 1, imagePriceConfigFromAPIKey(key), 1).TotalCost
	}
	if !cloudHostedPricesValid(snapshot.HostedTools) {
		return ctx, errors.New("Cloud hosted-tool prices or multipliers are invalid")
	}
	if in.Subscription != nil {
		if key.GroupID == nil || in.Subscription.UserID != user.ID || in.Subscription.GroupID != *key.GroupID {
			return ctx, errors.New("Cloud subscription owner mismatch")
		}
		id := in.Subscription.ID
		snapshot.SubscriptionID = &id
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}
	for _, model := range usageBillingModelCandidates(result.Model, result.UpstreamModel, in.OriginalModel, in.ChannelMappedModel, result.BillingModel) {
		price, err := cloudPriceSnapshot(resolver.Resolve(ctx, PricingInput{Model: model, GroupID: key.GroupID, Group: key.Group}))
		if err == nil {
			if snapshot.Version == 2 {
				price.ResponsePriceIdentified, _ = s.hasIdentifiedOpenAIResponsePricing(ctx, model, key)
			}
			snapshot.Prices[model] = price
		}
	}
	if len(snapshot.Prices) == 0 {
		return ctx, errors.New("Cloud pricing model is missing")
	}
	if key.GroupID != nil && s.channelService != nil {
		channel, err := s.channelService.GetChannelForGroup(ctx, *key.GroupID)
		if err != nil {
			return ctx, fmt.Errorf("freeze Cloud account statistics pricing: %w", err)
		}
		if channel != nil {
			statsPrice, err := cloudPriceSnapshot(NewModelPricingResolver(nil, s.billingService).Resolve(ctx, PricingInput{Model: result.UpstreamModel}))
			if err != nil {
				return ctx, err
			}
			snapshot.Stats = &openAICloudStatsSnapshot{ApplyCustomerPrice: channel.ApplyPricingToAccountStats, DefaultPrice: statsPrice}
			platform := s.channelService.GetGroupPlatform(ctx, *key.GroupID)
			for _, rule := range channel.AccountStatsPricingRules {
				if !matchAccountStatsRule(&rule, account.ID, *key.GroupID) {
					continue
				}
				if price := findPricingForModel(rule.Pricing, platform, strings.ToLower(result.UpstreamModel)); price != nil {
					cloned := price.Clone()
					snapshot.Stats.CustomPricing = &cloned
					break
				}
			}
		}
	}
	if reservation := InflightReservationFromContext(ctx); reservation != nil {
		snapshot.Inflight = &openAICloudInflightSnapshot{RequestID: reservation.requestID, UserID: reservation.userID, Balance: reservation.cache != nil, Scopes: append([]string(nil), reservation.scopes...)}
	}
	data, err := json.Marshal(snapshot)
	if err != nil || len(data) > 256<<10 {
		return ctx, errors.New("Cloud billing snapshot could not be frozen")
	}
	var detached OpenAICloudBillingSnapshot
	if err := json.Unmarshal(data, &detached); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, openAICloudBillingContextKey{}, &detached), nil
}

func (s *OpenAIGatewayService) FinalizeCloudBillingSnapshot(ctx context.Context, actualModel, serviceTier, effort string) (context.Context, error) {
	snapshot := cloudBillingContext(ctx)
	if snapshot == nil {
		return ctx, errors.New("Cloud billing snapshot is missing")
	}
	if _, ok := snapshot.Prices[actualModel]; !ok || actualModel != snapshot.UpstreamModel {
		return ctx, errors.New("Cloud actual model differs from the frozen pricing models")
	}
	copy := *snapshot
	copy.UpstreamModel, copy.ServiceTier, copy.ReasoningEffort = actualModel, serviceTier, effort
	return context.WithValue(ctx, openAICloudBillingContextKey{}, &copy), nil
}

// cloudSamplingUsageValid verifies disjoint measured steps against the root's
// token buckets. Hosted tool counts are recorded separately, once per root.
// Subtraction avoids overflowing a sum from malformed persisted data.
func cloudSamplingUsageValid(total *OpenAICloudUsage, samples []OpenAICloudUsage) bool {
	if samples == nil {
		return true
	} // Legacy owner: pricing checks ambiguity.
	if total == nil {
		return false
	}
	remaining := *total
	for _, sample := range samples {
		if !cloudBillingUsageValid(&sample) || sample.ReasoningOutputTokens < 0 || sample.ReasoningOutputTokens > sample.OutputTokens || sample.WebSearchCalls != 0 || sample.ImageCount != 0 {
			return false
		}
		next, err := cloudUsageDifference(&remaining, &sample)
		if err != nil {
			return false
		}
		remaining = *next
	}
	return remaining.TotalTokens == 0 && remaining.ReasoningOutputTokens == 0
}

func cloudSamplingTokens(sample OpenAICloudUsage) UsageTokens {
	return UsageTokens{InputTokens: sample.InputTokens - sample.CachedInputTokens - sample.CacheWriteInputTokens,
		OutputTokens: sample.OutputTokens, CacheReadTokens: sample.CachedInputTokens, CacheCreationTokens: sample.CacheWriteInputTokens}
}

// Preserve the existing one-request-per-root policy; token prices and context
// tiers are evaluated per sampling operation using the ordinary pricing engine.
func (s *OpenAIGatewayService) cloudSamplingCost(input CostInput, samples []OpenAICloudUsage) (*CostBreakdown, error) {
	if samples == nil || len(samples) == 0 {
		cost, err := s.billingService.CalculateCostUnified(input)
		if err != nil {
			return nil, err
		}
		if samples == nil && (cost.LongContextBillingApplied || len(input.Resolved.RequestTiers) > 0 ||
			(len(input.Resolved.Intervals) > 0 && (input.Resolved.longContextPricingEnabled || input.LongContextBillingEnabled != nil && *input.LongContextBillingEnabled))) {
			// An old total cannot establish any individual context size. Retain it via
			// the existing settlement archive instead of charging a guessed surcharge.
			return nil, cloudPermanentSettlementError("sampling_usage_missing")
		}
		return cost, nil
	}
	if input.Resolved.Mode == BillingModePerRequest {
		largest := samples[0]
		for _, sample := range samples[1:] {
			if sample.InputTokens > largest.InputTokens {
				largest = sample
			}
		}
		input.Tokens = cloudSamplingTokens(largest)
		return s.billingService.CalculateCostUnified(input)
	}
	sum := &CostBreakdown{}
	for _, sample := range samples {
		input.Tokens = cloudSamplingTokens(sample)
		cost, err := s.billingService.CalculateCostUnified(input)
		if err != nil {
			return nil, err
		}
		sum.InputCost += cost.InputCost
		sum.ImageInputCost += cost.ImageInputCost
		sum.OutputCost += cost.OutputCost
		sum.ImageOutputCost += cost.ImageOutputCost
		sum.CacheCreationCost += cost.CacheCreationCost
		sum.CacheReadCost += cost.CacheReadCost
		sum.TotalCost += cost.TotalCost
		sum.ActualCost += cost.ActualCost
		sum.BillingMode = cost.BillingMode
		sum.LongContextBillingApplied = sum.LongContextBillingApplied || cost.LongContextBillingApplied
	}
	return sum, nil
}

func (s *OpenAIGatewayService) cloudFrozenCost(ctx context.Context, snapshot *OpenAICloudBillingSnapshot, result *OpenAIForwardResult, models []string, tokens UsageTokens, multiplier float64, serviceTier string, exempt bool) (*CostBreakdown, error) {
	resolver := NewModelPricingResolver(nil, s.billingService)
	for _, model := range models {
		price, ok := snapshot.Prices[model]
		if !ok {
			continue
		}
		gate := snapshot.LongContextEnabled
		cost, err := s.cloudSamplingCost(CostInput{Ctx: ctx, Model: model, Tokens: tokens, RequestCount: 1,
			RateMultiplier: multiplier, ServiceTier: serviceTier, ReasoningEffort: optionalStringValue(result.ReasoningEffort),
			PricingAt: snapshot.PricingAt, Resolver: resolver, Resolved: price.resolved(),
			LongContextBillingEnabled: &gate, DisableModelLongContextPricing: exempt}, result.cloudSamplingUsage)
		if err != nil {
			return nil, err
		}
		hosted, err := s.cloudFrozenHostedCost(snapshot, result)
		if err != nil {
			return nil, err
		}
		cost.TotalCost += hosted.TotalCost
		cost.ActualCost += hosted.ActualCost
		cost.ImageOutputCost += hosted.ImageOutputCost
		return cost, nil
	}
	return nil, errors.New("Cloud settlement has no frozen price for its billing model")
}

func (s *OpenAIGatewayService) cloudFrozenStatsCost(ctx context.Context, snapshot *OpenAICloudBillingSnapshot, result *OpenAIForwardResult, tokens UsageTokens, total float64) (*float64, error) {
	stats := snapshot.Stats
	if stats == nil || snapshot.GroupID == nil {
		return nil, nil
	}
	hosted, err := s.cloudFrozenHostedCost(snapshot, result)
	if err != nil {
		return nil, err
	}
	if stats.CustomPricing != nil {
		custom := stats.CustomPricing
		batches := []UsageTokens{tokens}
		if custom.BillingMode == "" || custom.BillingMode == BillingModeToken {
			if result.cloudSamplingUsage != nil && len(result.cloudSamplingUsage) > 0 {
				batches = nil
				for _, sample := range result.cloudSamplingUsage {
					batches = append(batches, cloudSamplingTokens(sample))
				}
			} else if result.cloudSamplingUsage == nil && len(custom.Intervals) > 0 {
				return nil, cloudPermanentSettlementError("sampling_usage_missing")
			}
		}
		var sum float64
		matched := false
		for _, batch := range batches {
			if cost := calculateStatsCost(custom, batch, 1); cost != nil {
				sum += *cost
				matched = true
			}
		}
		if matched {
			sum *= reasoningEffortBillingMultiplier(snapshot.ReasoningEffort, custom.ReasoningEffortMultipliers)
			sum += hosted.TotalCost
			return &sum, nil
		}
	}
	if stats.ApplyCustomerPrice {
		if total <= 0 {
			return nil, nil
		}
		return &total, nil
	}
	price := stats.DefaultPrice.resolved()
	price.longContextPricingEnabled = snapshot.LongContextEnabled && !openAISubscriptionLongContextExempt(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, result)
	cost, err := s.cloudSamplingCost(CostInput{Ctx: ctx, Model: snapshot.UpstreamModel, Tokens: tokens, RequestCount: 1,
		RateMultiplier: 1, ServiceTier: optionalStringValue(result.ServiceTier), ReasoningEffort: snapshot.ReasoningEffort,
		PricingAt: snapshot.PricingAt, Resolver: NewModelPricingResolver(nil, s.billingService), Resolved: price}, result.cloudSamplingUsage)
	if err != nil {
		return nil, err
	}
	cost.TotalCost += hosted.TotalCost
	if cost.TotalCost <= 0 {
		return nil, nil
	}
	return &cost.TotalCost, nil
}

func cloudHostedPricesValid(prices *openAICloudHostedPriceSnapshot) bool {
	valid := func(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 }
	if prices == nil || !valid(prices.WebSearchPerCall) || !valid(prices.WebSearchMultiplier) || !valid(prices.ImageMultiplier) || len(prices.ImagePrices) != 3 {
		return false
	}
	for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
		price, exists := prices.ImagePrices[tier]
		if !exists || !valid(price) {
			return false
		}
	}
	return true
}

func (s *OpenAIGatewayService) cloudFrozenHostedCost(snapshot *OpenAICloudBillingSnapshot, result *OpenAIForwardResult) (*CostBreakdown, error) {
	if result == nil || !cloudHostedUsageValid(result.WebSearchCalls, result.ImageCount, result.ImageSizeBreakdown, result.ImageOutputSizes) {
		return nil, errors.New("Cloud hosted-tool usage is incomplete or inconsistent")
	}
	cost := &CostBreakdown{}
	if result.WebSearchCalls == 0 && result.ImageCount == 0 {
		return cost, nil
	}
	if snapshot == nil || !cloudHostedPricesValid(snapshot.HostedTools) {
		return nil, errors.New("Cloud hosted-tool price was not frozen before dispatch")
	}
	prices := snapshot.HostedTools
	search := s.billingService.CalculateWebSearchCost(result.WebSearchCalls, &prices.WebSearchPerCall, prices.WebSearchMultiplier)
	cost.TotalCost, cost.ActualCost = search.TotalCost, search.ActualCost
	p1, p2, p4 := prices.ImagePrices[ImageBillingSize1K], prices.ImagePrices[ImageBillingSize2K], prices.ImagePrices[ImageBillingSize4K]
	imagePrices := &ImagePriceConfig{Price1K: &p1, Price2K: &p2, Price4K: &p4}
	for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
		image := s.billingService.CalculateImageCost("", tier, result.ImageSizeBreakdown[tier], imagePrices, prices.ImageMultiplier)
		cost.TotalCost += image.TotalCost
		cost.ActualCost += image.ActualCost
		cost.ImageOutputCost += image.TotalCost
	}
	if math.IsNaN(cost.TotalCost) || math.IsInf(cost.TotalCost, 0) || math.IsNaN(cost.ActualCost) || math.IsInf(cost.ActualCost, 0) {
		return nil, errors.New("Cloud hosted-tool cost exceeds the supported numeric range")
	}
	return cost, nil
}

func cloudHostedUsageValid(searchCalls, imageCount int, tiers map[string]int, outputSizes []string) bool {
	if searchCalls < 0 || searchCalls > 1024 || imageCount < 0 || imageCount > 1024 || len(outputSizes) != imageCount {
		return false
	}
	remaining := imageCount
	for tier, count := range tiers {
		if (tier != ImageBillingSize1K && tier != ImageBillingSize2K && tier != ImageBillingSize4K) || count <= 0 || count > remaining {
			return false
		}
		remaining -= count
	}
	if remaining != 0 {
		return false
	}
	measured := make(map[string]int, 3)
	for _, size := range outputSizes {
		if _, _, ok := parseImageBillingDimensions(size); !ok {
			return false // A guessed/default tier is not a measured image size.
		}
		tier, ok := ClassifyImageBillingTier(size)
		if !ok {
			return false
		}
		measured[tier]++
	}
	for tier, count := range measured {
		if tiers[tier] != count {
			return false
		}
	}
	return true
}

// cloudLatencyStore keeps the gateway's own client-side latency measurement for
// the request that opened a billing root. The owner persists the settlement
// before that response is written, so the measurement is joined at settlement.
type cloudLatencyStore interface {
	RecordCloudLatency(ctx context.Context, root string, metrics *UsageLatencyMetrics) error
	LoadCloudLatency(ctx context.Context, root string) (*UsageLatencyMetrics, error)
}

// cloudLatencyGrace briefly defers a fresh customer settlement whose gateway
// measurement has not been recorded yet. It never delays it indefinitely.
const cloudLatencyGrace = 2 * time.Second

func (w *openAICloudBillingWorker) awaitingLatency(ctx context.Context, settlement *OpenAICloudSettlement) bool {
	store, ok := w.store.(cloudLatencyStore)
	if !ok || settlement == nil || settlement.Probe || settlement.Unknown || time.Since(settlement.ObservedAt) >= cloudLatencyGrace ||
		(settlement.Telemetry != nil && settlement.Telemetry.LatencyMetrics != nil) {
		return false
	}
	metrics, err := store.LoadCloudLatency(ctx, settlement.RootRequestID)
	return err == nil && metrics == nil
}

type openAICloudHeldReservation struct {
	done    func()
	expires time.Time
}
type openAICloudBillingWorker struct {
	service *OpenAIGatewayService
	store   OpenAICloudSessionStore
	id      string
	cancel  context.CancelFunc
	done    chan struct{}
	mu      sync.Mutex
	holds   map[string]openAICloudHeldReservation
	logMu   sync.Mutex
	logged  map[string]time.Time
}

// cloudBillingLogInterval bounds repeated background failure logs per stage.
const cloudBillingLogInterval = time.Minute

// cloudBillingErrorClass names a failure without its text: storage and SQL
// errors may echo values, so only the category and Go type are logged.
func cloudBillingErrorClass(err error) string {
	var permanent cloudPermanentSettlementError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &permanent):
		return "permanent:" + string(permanent)
	case errors.Is(err, ErrUsageBillingRequestConflict):
		return "conflict"
	default:
		return "error"
	}
}

func (w *openAICloudBillingWorker) shouldLog(stage string, now time.Time) bool {
	w.logMu.Lock()
	defer w.logMu.Unlock()
	if last, ok := w.logged[stage]; ok && now.Sub(last) < cloudBillingLogInterval {
		return false
	}
	if w.logged == nil {
		w.logged = make(map[string]time.Time)
	}
	w.logged[stage] = now
	return true
}

func (w *openAICloudBillingWorker) logFailure(stage, root string, account int64, err error) {
	if err == nil || !w.shouldLog(stage+":"+cloudBillingErrorClass(err), time.Now()) {
		return
	}
	logger.LegacyPrintf("service.openai_cloud", "Cloud billing %s failed (class=%s type=%T root=%s account=%d); retained for retry", stage, cloudBillingErrorClass(err), err, root, account)
}

func (s *OpenAIGatewayService) SetCloudBillingQuotaUpdater(updater APIKeyQuotaUpdater) {
	s.cloudBillingMu.Lock()
	defer s.cloudBillingMu.Unlock()
	s.cloudBillingQuotaUpdater = updater
}
func (s *OpenAIGatewayService) StartCloudBilling(store OpenAICloudSessionStore) {
	if s == nil || store == nil {
		return
	}
	s.cloudBillingMu.Lock()
	defer s.cloudBillingMu.Unlock()
	if s.cloudBillingWorker != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker := &openAICloudBillingWorker{service: s, store: store, id: uuid.NewString(), cancel: cancel, done: make(chan struct{}), holds: make(map[string]openAICloudHeldReservation)}
	s.cloudBillingWorker = worker
	go worker.run(ctx)
}
func (s *OpenAIGatewayService) StopCloudBilling() {
	if s == nil {
		return
	}
	s.cloudBillingMu.Lock()
	worker := s.cloudBillingWorker
	s.cloudBillingWorker = nil
	s.cloudBillingMu.Unlock()
	if worker == nil {
		return
	}
	worker.cancel()
	<-worker.done
	// Shutdown does not release unbilled reservations early. Redis's original
	// bounded TTL protects the debit window until another consumer retries.
}
func (s *OpenAIGatewayService) HoldCloudBillingReservation(ctx context.Context, rootID string) error {
	s.cloudBillingMu.Lock()
	worker := s.cloudBillingWorker
	s.cloudBillingMu.Unlock()
	if worker == nil || strings.TrimSpace(rootID) == "" {
		return errors.New("Cloud billing worker is unavailable")
	}
	reservation := InflightReservationFromContext(ctx)
	if reservation == nil {
		return nil
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if _, exists := worker.holds[rootID]; exists {
		return nil
	}
	worker.holds[rootID] = openAICloudHeldReservation{done: reservation.Acquire(), expires: time.Now().Add(reservation.ttl)}
	return nil
}
func (s *OpenAIGatewayService) WaitCloudProbeSettlement(ctx context.Context, rootID string) error {
	s.cloudBillingMu.Lock()
	worker := s.cloudBillingWorker
	s.cloudBillingMu.Unlock()
	if worker == nil {
		return errors.New("Cloud billing worker is unavailable")
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		acknowledged, err := worker.store.CloudSettlementAcknowledged(ctx, rootID)
		if err != nil {
			return err
		}
		if acknowledged {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (w *openAICloudBillingWorker) run(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := w.service.recoverCloudSessions(ctx, w.store, w.id); err != nil && ctx.Err() == nil {
			w.logFailure("recover", "", 0, err)
		}
		w.consume(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *openAICloudBillingWorker) consume(ctx context.Context) {
	pending, err := w.store.ListCloudSettlements(ctx, OpenAICloudSettlementBatchSize)
	if err != nil {
		if ctx.Err() == nil {
			w.logFailure("list", "", 0, err)
		}
		return
	}
	for _, settlement := range pending {
		if ctx.Err() != nil {
			return
		}
		if settlement == nil || strings.TrimSpace(settlement.RootRequestID) == "" || w.awaitingLatency(ctx, settlement) {
			continue
		}

		claimed, err := w.store.ClaimCloudSettlement(ctx, settlement.RootRequestID, w.id, time.Minute)
		if err != nil {
			w.logFailure("claim", settlement.RootRequestID, settlement.AccountID, err)
		}
		if err != nil || !claimed {
			continue
		}
		itemCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		reason := settlement.ArchiveReason
		if reason == "" && (settlement.ReconciliationOnly || len(settlement.LateUsage) > 0) {
			reason = "billing_conflict"
			if settlement.Unknown || settlement.Usage == nil {
				reason = "unknown_usage"
			}
		}
		if reason == "" && settlement.Unknown && !settlement.Probe {
			reason = "unknown_usage"
		}
		if reason == "" {
			err = w.settle(itemCtx, settlement)
			var permanent cloudPermanentSettlementError
			if errors.As(err, &permanent) {
				reason = string(permanent)
			}
		}
		stage := "settle"
		if reason != "" {
			stage, err = "archive", w.archive(itemCtx, settlement, reason)
		} else if errors.Is(err, ErrOpenAICloudProbeSettlementFailed) && settlement.Probe && settlement.APIKeyID == 0 {
			stage, err = "probe", w.store.FailCloudProbeSettlement(itemCtx, settlement.RootRequestID, w.id)
		} else if err == nil {
			stage = "ack"
			var payload []byte
			payload, err = cloudSettlementPayload(settlement)
			if err == nil {
				err = w.store.AckCloudSettlement(itemCtx, settlement.RootRequestID, w.id, payload)
			}
		}
		if err == nil {
			w.releaseReservation(itemCtx, settlement)
		}
		cancel()
		if err != nil {
			w.logFailure(stage, settlement.RootRequestID, settlement.AccountID, err)
		}
	}
	w.releaseAcknowledgedHolds(ctx)
}
func (s *OpenAIGatewayService) cloudSettlementArchived(ctx context.Context, store OpenAICloudSessionStore, root string) (bool, error) {
	archived, err := store.CloudSettlementArchived(ctx, root)
	if err != nil || archived {
		return archived, err
	}
	if repo, ok := s.usageBillingRepo.(OpenAICloudSettlementArchiveRepository); ok {
		return repo.HasCloudSettlementArchive(ctx, root)
	}
	return false, nil
}

func (w *openAICloudBillingWorker) archive(ctx context.Context, settlement *OpenAICloudSettlement, reason string) error {
	// Map internal sampling diagnostics onto the existing durable archive categories.
	// Preserve the exact payload; neither condition is safe to charge.
	switch reason {
	case "sampling_usage_missing":
		reason = "unknown_usage"
	case "invalid_sampling_usage":
		reason = "invalid_settlement"
	}
	repo, ok := w.service.usageBillingRepo.(OpenAICloudSettlementArchiveRepository)
	if !ok {
		return errors.New("Cloud durable settlement archive is unavailable")
	}
	payload, err := cloudSettlementPayload(settlement)
	if err != nil {
		return err
	}
	if err := repo.StoreCloudSettlementArchive(ctx, settlement.RootRequestID, reason, payload); err != nil {
		return err
	}
	return w.store.ArchiveCloudSettlement(ctx, settlement.RootRequestID, w.id, payload)
}

func cloudSettlementPayload(settlement *OpenAICloudSettlement) ([]byte, error) {
	if settlement.ArchivePayload != nil {
		return settlement.ArchivePayload, nil
	}
	// Redis supplies exact queue bytes; fixtures and other stores can encode
	// their immutable snapshot. Never re-read newer facts to authorize an ACK.
	return json.Marshal(settlement)
}

func (w *openAICloudBillingWorker) settle(ctx context.Context, settlement *OpenAICloudSettlement) error {
	if settlement.ReconciliationOnly || len(settlement.LateUsage) > 0 {
		return cloudPermanentSettlementError("billing_conflict")
	}
	if settlement.Probe && settlement.APIKeyID == 0 {
		// Redis JSON round trips turn a nil RawMessage into the literal null.
		snapshot := strings.TrimSpace(string(settlement.BillingSnapshot))
		if settlement.UserID <= 0 || (snapshot != "" && snapshot != "null") || settlement.Unknown || !cloudBillingUsageValid(settlement.Usage) ||
			(settlement.Status != "completed" && settlement.Status != "failed" && settlement.Status != "interrupted") {
			return ErrOpenAICloudProbeSettlementFailed
		}
		return nil
	}
	if settlement.Unknown || settlement.Usage == nil || !cloudBillingUsageValid(settlement.Usage) {
		return cloudPermanentSettlementError("unknown_usage")
	}
	if !cloudSamplingUsageValid(settlement.Usage, settlement.SamplingUsage) {
		return cloudPermanentSettlementError("invalid_sampling_usage")
	}
	if settlement.Status != "completed" && settlement.Status != "failed" && settlement.Status != "interrupted" {
		return cloudPermanentSettlementError("invalid_settlement")
	}
	if settlement.LowerBound && settlement.Status == "completed" {
		return cloudPermanentSettlementError("invalid_settlement")
	}
	if settlement.Probe {
		return cloudPermanentSettlementError("owner_invalid")
	}

	var snapshot OpenAICloudBillingSnapshot
	if len(settlement.BillingSnapshot) == 0 || json.Unmarshal(settlement.BillingSnapshot, &snapshot) != nil {
		return cloudPermanentSettlementError("invalid_settlement")
	}
	if _, valid := snapshot.billingMode(); !valid ||
		snapshot.APIKeyID <= 0 || snapshot.UserID != settlement.UserID || snapshot.APIKeyID != settlement.APIKeyID || snapshot.AccountID != settlement.AccountID ||
		valueOrZero(snapshot.GroupID) != settlement.GroupID || snapshot.UpstreamModel != settlement.Model || snapshot.PricingAt.IsZero() ||
		((settlement.Usage.WebSearchCalls > 0 || settlement.Usage.ImageCount > 0) && !cloudHostedPricesValid(snapshot.HostedTools)) {
		return cloudPermanentSettlementError("invalid_settlement")
	}
	// Proven rejection before inference has no customer charge, including
	// per-request plans. ACK releases its reservation; it is not an unknown bill.
	if settlement.Status == "failed" && settlement.Usage.TotalTokens == 0 && settlement.Usage.WebSearchCalls == 0 && settlement.Usage.ImageCount == 0 {
		return nil
	}
	service := w.service
	if service.usageBillingRepo == nil {
		return errors.New("Cloud atomic billing repository unavailable")
	}
	service.cloudBillingMu.Lock()
	updater := service.cloudBillingQuotaUpdater
	service.cloudBillingMu.Unlock()
	getter, ok := updater.(interface {
		GetByID(context.Context, int64) (*APIKey, error)
	})
	if !ok || service.userRepo == nil {
		return errors.New("Cloud billing repositories unavailable")
	}
	key, err := getter.GetByID(ctx, snapshot.APIKeyID)
	if errors.Is(err, ErrAPIKeyNotFound) {
		// The accepted root owns immutable attribution even if its key was
		// deleted while a tool ran. The atomic ledger already skips deleted-key
		// counters while settling the original user/subscription/account costs.
		key = &APIKey{ID: snapshot.APIKeyID, UserID: snapshot.UserID}
	} else if err != nil {
		return err // A database outage is not evidence that the key was deleted.
	}
	user, err := service.userRepo.GetByID(ctx, snapshot.UserID)
	if errors.Is(err, ErrUserNotFound) {
		return cloudPermanentSettlementError("owner_missing")
	}
	if err != nil {
		return err
	}
	if key == nil || user == nil || key.ID != snapshot.APIKeyID || key.UserID != snapshot.UserID || user.ID != snapshot.UserID {
		return cloudPermanentSettlementError("owner_invalid")
	}
	// Current business rows supply cache invalidation/notification state only.
	// The frozen IDs, group, quota policy and prices determine the debit.
	keyCopy, userCopy := *key, *user
	userCopy.Role = snapshot.UserRole
	keyCopy.UserID, keyCopy.User, keyCopy.GroupID = snapshot.UserID, &userCopy, snapshot.GroupID
	keyCopy.Quota, keyCopy.RateLimit5h, keyCopy.RateLimit1d, keyCopy.RateLimit7d = snapshot.KeyQuota, snapshot.KeyRateLimit5h, snapshot.KeyRateLimit1d, snapshot.KeyRateLimit7d
	keyCopy.Group = nil
	if snapshot.GroupID != nil {
		keyCopy.Group = &Group{ID: *snapshot.GroupID, Name: snapshot.GroupName, Platform: snapshot.GroupPlatform,
			SubscriptionType: snapshot.SubscriptionType, LongContextPricingEnabled: snapshot.GroupLongContextEnabled, FreeOpenAIFast: snapshot.FreeFast}
	}
	account := &Account{ID: snapshot.AccountID, Name: snapshot.AccountName, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		RateMultiplier: &snapshot.AccountRate, Extra: map[string]any{"openai_long_context_billing_enabled": snapshot.LongContextEnabled,
			"quota_limit": snapshot.AccountQuota, "quota_daily_limit": snapshot.AccountDailyQuota, "quota_weekly_limit": snapshot.AccountWeeklyQuota}}
	var subscription *UserSubscription
	if snapshot.SubscriptionID != nil {
		subscription = &UserSubscription{ID: *snapshot.SubscriptionID, UserID: snapshot.UserID, GroupID: valueOrZero(snapshot.GroupID)}
	}
	usage := settlement.Usage
	result := &OpenAIForwardResult{AuthorizationMode: "cloud", RequestID: "cloud:" + settlement.RootRequestID, Model: snapshot.Model, UpstreamModel: snapshot.UpstreamModel,
		Usage:       OpenAIUsage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens, CacheReadInputTokens: usage.CachedInputTokens, CacheCreationInputTokens: usage.CacheWriteInputTokens},
		ServiceTier: optionalTrimmedStringPtr(snapshot.ServiceTier), ReasoningEffort: optionalTrimmedStringPtr(snapshot.ReasoningEffort), RequestedReasoningEffort: optionalTrimmedStringPtr(snapshot.RequestedReasoningEffort),
		Stream: snapshot.Stream, Duration: settlement.ObservedAt.Sub(snapshot.PricingAt), UpstreamEndpoint: "wss://codex-cloud-backend.chatgpt.com/",
		cloudSamplingUsage: settlement.SamplingUsage, UsageLowerBound: settlement.LowerBound, WebSearchCalls: usage.WebSearchCalls, ImageCount: usage.ImageCount,
		ImageOutputSizes: append([]string(nil), usage.ImageOutputSizes...), ImageSizeBreakdown: cloneImageSizeBreakdown(usage.ImageSizes)}
	if result.Duration < 0 {
		result.Duration = 0
	}
	if observed := settlement.Telemetry; observed != nil {
		result.FirstTokenMs = observed.FirstTokenMs
		result.LatencyMetrics = observed.LatencyMetrics
		result.OpenAIWSMode = observed.OpenAIWSMode
	}
	if result.LatencyMetrics == nil {
		if store, ok := w.store.(cloudLatencyStore); ok {
			if metrics, err := store.LoadCloudLatency(ctx, settlement.RootRequestID); err == nil {
				result.LatencyMetrics = metrics
			}
		}
	}
	// First-event timings refer to the first request, but duration/TPS on this
	// row describe the complete billing root. Never mutate the cached snapshot.
	if result.LatencyMetrics != nil {
		metrics := *result.LatencyMetrics
		metrics.DurationMs = int(result.Duration.Milliseconds())
		result.LatencyMetrics = &metrics
	}
	ctx = context.WithValue(ctx, openAICloudBillingContextKey{}, &snapshot)
	ctx = context.WithValue(ctx, openAICloudSettlingContextKey{}, true)
	err = service.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: &keyCopy, User: &userCopy, Account: account, Subscription: subscription,
		CyberBlocked:    settlement.ErrorCode == "cyber_policy",
		InboundEndpoint: snapshot.InboundEndpoint, UpstreamEndpoint: result.UpstreamEndpoint, UserAgent: snapshot.UserAgent, IPAddress: snapshot.IPAddress,
		SessionID: snapshot.SessionID, RequestPayloadHash: snapshot.RequestPayloadHash, APIKeyService: updater, QuotaPlatform: snapshot.QuotaPlatform,
		PricingAt: snapshot.PricingAt, NativeCompactionV2: snapshot.NativeCompactionV2, ChannelUsageFields: snapshot.ChannelUsageFields})
	if errors.Is(err, ErrUsageBillingRequestConflict) {
		return cloudPermanentSettlementError("billing_conflict")
	}
	return err
}
func cloudBillingUsageValid(usage *OpenAICloudUsage) bool {
	return usage != nil && usage.InputTokens >= 0 && usage.OutputTokens >= 0 && usage.CachedInputTokens >= 0 && usage.CacheWriteInputTokens >= 0 &&
		usage.CachedInputTokens <= usage.InputTokens && usage.CacheWriteInputTokens <= usage.InputTokens-usage.CachedInputTokens &&
		usage.TotalTokens >= usage.InputTokens && usage.TotalTokens-usage.InputTokens == usage.OutputTokens &&
		cloudHostedUsageValid(usage.WebSearchCalls, usage.ImageCount, usage.ImageSizes, usage.ImageOutputSizes)
}
func (w *openAICloudBillingWorker) releaseReservation(ctx context.Context, settlement *OpenAICloudSettlement) {
	w.mu.Lock()
	hold, exists := w.holds[settlement.RootRequestID]
	delete(w.holds, settlement.RootRequestID)
	w.mu.Unlock()
	if exists {
		hold.done()
		return
	}
	var snapshot OpenAICloudBillingSnapshot
	if json.Unmarshal(settlement.BillingSnapshot, &snapshot) != nil || snapshot.Inflight == nil || w.service.billingCacheService == nil {
		return
	}
	reservation := snapshot.Inflight
	if reservation.UserID != settlement.UserID {
		return
	}
	cache := w.service.billingCacheService.cache
	if balance, ok := cache.(InflightBalanceReservationCache); ok && reservation.Balance {
		_ = balance.ReleaseInflightBalance(ctx, reservation.UserID, reservation.RequestID)
	}
	if scopes, ok := cache.(InflightScopeReservationCache); ok {
		for _, scope := range reservation.Scopes {
			_ = scopes.ReleaseInflightScope(ctx, scope, reservation.RequestID)
		}
	}
}
func (w *openAICloudBillingWorker) releaseAcknowledgedHolds(ctx context.Context) {
	w.mu.Lock()
	pending := make(map[string]openAICloudHeldReservation, len(w.holds))
	for id, hold := range w.holds {
		pending[id] = hold
	}
	w.mu.Unlock()
	for id, hold := range pending {
		acknowledged, err := w.store.CloudSettlementAcknowledged(ctx, id)
		if err != nil {
			continue
		}
		if !acknowledged && time.Now().Before(hold.expires) {
			archived, err := w.store.CloudSettlementArchived(ctx, id)
			if err != nil || !archived {
				continue
			}
		}
		w.mu.Lock()
		_, exists := w.holds[id]
		delete(w.holds, id)
		w.mu.Unlock()
		if exists {
			hold.done()
		}
	}
}

// ReleaseCloudBillingReservation requires proof of no inference: either no
// dispatch/persistence, or the fenced owner's persisted zero-usage rejection.
// A nil result, cancellation, timeout or lost response alone is not such proof.
// Releasing a local hold never removes the durable settlement.
func (s *OpenAIGatewayService) ReleaseCloudBillingReservation(rootID string) {
	if s == nil {
		return
	}
	s.cloudBillingMu.Lock()
	worker := s.cloudBillingWorker
	s.cloudBillingMu.Unlock()
	if worker == nil {
		return
	}
	worker.mu.Lock()
	hold, exists := worker.holds[rootID]
	delete(worker.holds, rootID)
	worker.mu.Unlock()
	if exists {
		hold.done()
	}
}
