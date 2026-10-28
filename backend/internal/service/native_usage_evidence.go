package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// NativeResolvedPricingSnapshot preserves existing pricing, including private
// rule switches that ordinary JSON on ResolvedPricing would silently discard.
// This is evidence only; existing billing services remain the sole calculator.
type NativeResolvedPricingSnapshot struct {
	ManagedPrice              *ManagedPrice `json:",omitempty"`
	Mode                      BillingMode
	BasePricing               *ModelPricing
	CacheWriteFlatOverride    bool
	Intervals, RequestTiers   []PricingInterval
	DefaultPerRequestPrice    float64
	Source                    string
	SupportsCacheBreakdown    bool
	ChannelPricing            *ChannelModelPricing
	LongContextPricingEnabled bool
}

type NativeFrozenPricing struct {
	BalanceExempt *bool // Nil is unknown policy, never permission to charge personal balance.
	Policy        *UsageBillingPricingSnapshot
	Resolved      *NativeResolvedPricingSnapshot
}

// Restore returns a detached instance for the existing billing calculator. It
// preserves explicit-zero/private rule switches; it does not resolve new prices.
func (s *NativeResolvedPricingSnapshot) Restore() (*ResolvedPricing, error) {
	if s == nil {
		return nil, nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var copy NativeResolvedPricingSnapshot
	if err := json.Unmarshal(raw, &copy); err != nil {
		return nil, err
	}
	if copy.BasePricing != nil {
		copy.BasePricing.cacheWriteFlatOverride = copy.CacheWriteFlatOverride
		copy.BasePricing.managedPrice = copy.ManagedPrice
	}
	return &ResolvedPricing{Mode: copy.Mode, BasePricing: copy.BasePricing, Intervals: copy.Intervals, RequestTiers: copy.RequestTiers, DefaultPerRequestPrice: copy.DefaultPerRequestPrice, Source: copy.Source, SupportsCacheBreakdown: copy.SupportsCacheBreakdown, channelPricing: copy.ChannelPricing, longContextPricingEnabled: copy.LongContextPricingEnabled}, nil
}

func FreezeNativeUsagePricing(resolved *ResolvedPricing, policy *UsageBillingPricingSnapshot, balanceExempt bool) (*NativeFrozenPricing, error) {
	frozen := &NativeFrozenPricing{Policy: policy, BalanceExempt: &balanceExempt}
	if resolved != nil {
		frozen.Resolved = &NativeResolvedPricingSnapshot{Mode: resolved.Mode, BasePricing: resolved.BasePricing, Intervals: resolved.Intervals, RequestTiers: resolved.RequestTiers, DefaultPerRequestPrice: resolved.DefaultPerRequestPrice, Source: resolved.Source, SupportsCacheBreakdown: resolved.SupportsCacheBreakdown, ChannelPricing: resolved.channelPricing, LongContextPricingEnabled: resolved.longContextPricingEnabled}
		if resolved.BasePricing != nil {
			frozen.Resolved.CacheWriteFlatOverride = resolved.BasePricing.cacheWriteFlatOverride
			frozen.Resolved.ManagedPrice = resolved.BasePricing.managedPrice
		}
	}
	frozen.Policy = sanitizeNativePricingPolicy(policy)
	raw, err := json.Marshal(frozen)
	if err != nil {
		return nil, err
	}
	var result NativeFrozenPricing
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Only billing facts from nested forward results may enter durable evidence.
func sanitizeNativePricingPolicy(policy *UsageBillingPricingSnapshot) *UsageBillingPricingSnapshot {
	if policy == nil {
		return nil
	}
	p := *policy
	if policy.Request == nil {
		return &p
	}
	r := *policy.Request
	if source := r.OpenAI; source != nil {
		r.OpenAI = &OpenAIForwardResult{Model: source.Model, Usage: source.Usage, ImageCount: source.ImageCount, ImageEndpoint: source.ImageEndpoint, ImageSize: source.ImageSize, VideoCount: source.VideoCount, VideoResolution: source.VideoResolution, VideoDurationSeconds: source.VideoDurationSeconds, SearchCount: source.SearchCount, WebSearchCalls: source.WebSearchCalls, AudioUsage: source.AudioUsage, ReasoningEffort: source.ReasoningEffort, ServiceTier: source.ServiceTier}
	}
	if source := r.Gateway; source != nil {
		r.Gateway = &ForwardResult{Model: source.Model, Usage: source.Usage, ImageCount: source.ImageCount, ImageEndpoint: source.ImageEndpoint, ImageSize: source.ImageSize, SearchCount: source.SearchCount, AudioUsage: source.AudioUsage, CacheStorageTokenHours: source.CacheStorageTokenHours, ReasoningEffort: source.ReasoningEffort, ServiceTier: source.ServiceTier}
	}
	p.Request = &r
	return &p
}

// Identity.RequestID is the existing billing request identity, resolved once by
// the ingress owner. OperationID is separate; neither is a fabricated provider ID.
type NativeUsageEvidenceInput struct {
	// Protocol selects the billing projection validator. Empty retains the
	// historical OpenAI/native evidence contract; SystemOne uses Gateway
	// pricing facts and must never be accepted by the OpenAI validator.
	Protocol string `json:",omitempty"`
	// The actual persisted technical root, never a replacement money identity.
	// Nil retains the original V1 evidence representation.
	Intent   *billing.NativeIntentReference `json:",omitempty"`
	Evidence billing.NativeUsageEvidence
	// One atomic operation batch. Evidence selects an actual member.
	Observations []billing.NativeUsageEvidence
	// Original RecordUsage builder snapshot, never recalculated by this recorder.
	SettlementUsage *UsagePricingRequest
	FrozenPricing   *NativeFrozenPricing
	Settlement      *UsageBillingCommand
}

func (i *NativeUsageEvidenceInput) NativeEvidence() billing.NativeUsageEvidence { return i.Evidence }
func (i *NativeUsageEvidenceInput) NativeSettlement() billing.Command {
	if i.Settlement == nil {
		return nil
	}
	return i.Settlement
}

// Observations remain immutable evidence. The V1 financial payload is kept in
// the existing event payload column, never duplicated as a second charge.
type NativeUsageEvidenceRecord struct {
	Protocol           string                         `json:",omitempty"`
	Intent             *billing.NativeIntentReference `json:",omitempty"`
	SettlementUsage    *UsagePricingRequest
	SettlementSequence *uint64
	Version            int
	Evidence           billing.NativeUsageEvidence
	Observations       []billing.NativeUsageEvidence
	FrozenPricing      *NativeFrozenPricing
	Receipt            billing.NativeUsageReceipt `json:"-"`
}

// NativeUsageRecordResult separates durable journal state from this call's
// actual transaction outcome. A settled receipt alone must never imply Applied.
// ActualApplyResult is nil when no transaction result was observed, including
// unknown usage, pending pricing, rollback and an already-settled replay.
type NativeUsageRecordResult struct {
	Receipt           billing.NativeUsageReceipt
	ActualApplyResult *UsageBillingApplyResult
}

type NativeUsageEvidenceRepository interface {
	RecordNativeUsageEvidence(context.Context, *NativeUsageEvidenceInput) (NativeUsageRecordResult, error)
	LoadNativeUsageEvidence(context.Context, billing.Identity) (*NativeUsageEvidenceRecord, error)
}

type NativeUsageEvidenceService struct{ repo NativeUsageEvidenceRepository }

func NewNativeUsageEvidenceService(repo NativeUsageEvidenceRepository) (*NativeUsageEvidenceService, error) {
	if repo == nil || (reflect.ValueOf(repo).Kind() == reflect.Pointer && reflect.ValueOf(repo).IsNil()) {
		return nil, errors.New("native usage evidence repository unavailable")
	}
	return &NativeUsageEvidenceService{repo: repo}, nil
}

// The actual Finalizer supplies its bounded cleanup context. A canceled context
// is not silently replaced, and a failed write cannot yield a durable receipt.
func ProvideNativeUsageEvidenceService(repo UsageBillingRepository) (*NativeUsageEvidenceService, error) {
	port, ok := repo.(NativeUsageEvidenceRepository)
	if !ok {
		return nil, errors.New("usage billing repository does not implement native evidence persistence")
	}
	return NewNativeUsageEvidenceService(port)
}

func (s *NativeUsageEvidenceService) Record(ctx context.Context, input NativeUsageEvidenceInput) (NativeUsageRecordResult, error) {
	if ctx == nil || s == nil || s.repo == nil {
		return NativeUsageRecordResult{}, billing.ErrNativeUsageEvidence
	}
	if err := ctx.Err(); err != nil {
		return NativeUsageRecordResult{}, err
	}
	prepared, err := PrepareNativeUsageEvidence(input)
	if err != nil {
		return NativeUsageRecordResult{}, err
	}
	result, err := s.repo.RecordNativeUsageEvidence(ctx, prepared)
	receipt := result.Receipt
	if receipt.Durable() {
		if receipt.Identity != prepared.Evidence.Identity || receipt.EvidenceFingerprint != prepared.Evidence.Fingerprint() {
			result.Receipt = billing.NativeUsageReceipt{}
			return result, errors.Join(err, billing.ErrNativeUsageConflict)
		}
	} else if err == nil {
		return result, billing.ErrNativeUsageEvidence
	}
	// A later receipt read failure does not erase a transaction outcome that the
	// real repository already observed. Preserve both it and the original error.
	return result, err
}
func (s *NativeUsageEvidenceService) Load(ctx context.Context, id billing.Identity) (*NativeUsageEvidenceRecord, error) {
	return s.repo.LoadNativeUsageEvidence(ctx, id)
}

// PrepareNativeUsageEvidence makes a detached snapshot without generating any
// identity, observed count or price. Repository also uses this at its boundary.
func PrepareNativeUsageEvidence(input NativeUsageEvidenceInput) (*NativeUsageEvidenceInput, error) {
	if input.Protocol != "" && (input.Protocol != NativeIntentProtocolSystemOne || input.Intent == nil || input.Evidence.Version != 2) {
		return nil, billing.ErrNativeUsageEvidence
	}
	if input.SettlementUsage != nil {
		input.SettlementUsage = sanitizeNativePricingPolicy(&UsageBillingPricingSnapshot{Request: input.SettlementUsage}).Request
	}
	if input.FrozenPricing != nil {
		p := *input.FrozenPricing
		p.Policy = sanitizeNativePricingPolicy(p.Policy)
		input.FrozenPricing = &p
	}
	if input.Settlement != nil {
		c := *input.Settlement
		// UsageLowerBound is not a JSON field; check it before detaching. Native
		// partial evidence must not borrow Cloud's confirmed-lower-bound policy.
		for _, log := range append([]*UsageLog{c.UsageLog}, c.UsageLogs...) {
			if log != nil && (log.UsageLowerBound || log.BillingStatus == "lower_bound") {
				return nil, billing.ErrNativeUsageEvidence
			}
		}
		c.Pricing = sanitizeNativePricingPolicy(c.Pricing)
		input.Settlement = &c
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, errors.New("native usage evidence exceeds size limit")
	}
	var detached NativeUsageEvidenceInput
	if err = json.Unmarshal(raw, &detached); err != nil {
		return nil, err
	}
	var hash [32]byte
	if ref := detached.Intent; ref != nil {
		if !(billing.NativeIntentReceipt{NativeIntentReference: *ref, Disposition: billing.NativeIntentPreserved}).Durable() ||
			detached.Evidence.Version != 2 ||
			ref.Key.OperationID != detached.Evidence.OperationID || ref.Key.APIKeyID != detached.Evidence.Identity.APIKeyID ||
			ref.AttemptSequence != detached.Evidence.AttemptSequence || detached.FrozenPricing == nil {
			return nil, billing.ErrNativeUsageConflict
		}
		root, decodeErr := hex.DecodeString(ref.IntentFingerprint)
		scope, scopeErr := hex.DecodeString(ref.ScopeDigest)
		if decodeErr != nil || scopeErr != nil || len(root) != 32 || len(scope) != 32 ||
			detached.Evidence.AttemptScopeDigest != [32]byte(scope) {
			return nil, billing.ErrNativeUsageConflict
		}
		hash = [32]byte(root)
	} else {
		if detached.Evidence.Version > 1 {
			return nil, billing.ErrNativeUsageConflict
		}
		pricing, err := json.Marshal(detached.FrozenPricing)
		if err != nil {
			return nil, err
		}
		hash = sha256.Sum256(pricing)
		if detached.Evidence.AttemptScopeDigest != ([32]byte{}) {
			return nil, billing.ErrNativeUsageConflict
		}
	}
	if detached.Evidence.PricingDigest != ([32]byte{}) && detached.Evidence.PricingDigest != hash {
		return nil, billing.ErrNativeUsageConflict
	}
	detached.Evidence.PricingDigest = hash
	if err = detached.Evidence.Validate(); err != nil {
		return nil, err
	}
	if len(detached.Observations) == 0 {
		detached.Observations = []billing.NativeUsageEvidence{detached.Evidence}
	}
	if len(detached.Observations) > 64 {
		return nil, billing.ErrNativeUsageEvidence
	}
	selected := false
	for i := range detached.Observations {
		e := &detached.Observations[i]
		if e.PricingDigest != ([32]byte{}) && e.PricingDigest != hash {
			return nil, billing.ErrNativeUsageConflict
		}
		e.PricingDigest = hash
		if detached.Intent == nil {
			if e.Version > 1 || e.AttemptScopeDigest != ([32]byte{}) {
				return nil, billing.ErrNativeUsageConflict
			}
		} else if e.Version != 2 || e.Prepared == nil || (e.Prepared.Purpose == model.DispatchGeneration && e.AttemptScopeDigest == ([32]byte{}) && !nativeUnpreservedNotSentEvidence(*e)) ||
			(e.Prepared.Purpose == model.DispatchInternalPreparation && e.AttemptScopeDigest != ([32]byte{})) {
			return nil, billing.ErrNativeUsageConflict
		}
		if e.Validate() != nil || e.Fingerprint() != detached.Evidence.Fingerprint() {
			return nil, billing.ErrNativeUsageConflict
		}
		if reflect.DeepEqual(*e, detached.Evidence) {
			selected = true
		}
	}
	if !selected {
		return nil, billing.ErrNativeUsageConflict
	}
	if detached.Settlement == nil && detached.SettlementUsage != nil {
		if err := validateObservedUsageMapping(detached.Protocol, detached.Evidence, detached.SettlementUsage); err != nil {
			return nil, err
		}
	}
	if c := detached.Settlement; c != nil {
		e := detached.Evidence
		if e.Identity.RequestID == "" {
			return nil, billing.ErrNativeUsageConflict
		}
		if detached.FrozenPricing == nil || detached.FrozenPricing.BalanceExempt == nil || *detached.FrozenPricing.BalanceExempt != c.BalanceExempt {
			return nil, billing.ErrNativeUsageConflict
		}
		if policy := detached.FrozenPricing.Policy; policy != nil {
			if (!policy.Balance && c.BalanceCost > 0) || (!policy.Subscription && c.SubscriptionCost > 0) || (!policy.KeyQuota && c.APIKeyQuotaCost > 0) || (!policy.KeyRateLimit && c.APIKeyRateLimitCost > 0) || (!policy.AccountQuota && c.AccountQuotaCost > 0) {
				return nil, billing.ErrNativeUsageConflict
			}
		}
		if e.Confidence != billing.NativeUsageFinal || e.Usage == nil || e.Usage.Semantics != billing.NativeUsageCumulative || e.Prepared == nil || c.UsageLog == nil ||
			c.RequestID != e.Identity.RequestID || c.APIKeyID != e.Identity.APIKeyID || c.UserID != e.UserID || c.AccountID != e.Prepared.ResourceID ||
			c.UsageLog.RequestID != c.RequestID || c.UsageLog.APIKeyID != c.APIKeyID || c.UsageLog.UserID != c.UserID || c.UsageLog.AccountID != c.AccountID {
			return nil, billing.ErrNativeUsageEvidence
		}
		if c.PayloadVersion != 0 && c.PayloadVersion != 1 {
			return nil, billing.ErrNativeUsageEvidence
		}
		if (e.GroupBound && (c.UsageLog.GroupID == nil || *c.UsageLog.GroupID != e.GroupID)) || (!e.GroupBound && c.UsageLog.GroupID != nil) {
			return nil, billing.ErrNativeUsageConflict
		}
		if err := validateSettlementUsage(detached.Protocol, e, detached.SettlementUsage, c); err != nil {
			return nil, err
		}

		if c.BalanceExempt && c.BalanceCost != 0 {
			return nil, billing.ErrNativeUsageConflict
		}
		for _, amount := range []float64{c.BalanceCost, c.SubscriptionCost, c.APIKeyQuotaCost, c.APIKeyRateLimitCost, c.AccountQuotaCost} {
			if amount < 0 {
				return nil, billing.ErrNativeUsageEvidence
			}
		}
		c.Normalize()
	}
	if _, err := MergeNativeUsageRecord(nil, &detached); err != nil {
		return nil, err
	}
	return &detached, nil
}

// An attempt is registered before its price scope is persisted. If that step
// fails, retain the actual unsent attempt without inventing a scope digest.
// This exception is observation-only: the selected evidence still requires a
// preserved reference, and any upstream fact makes the exception invalid.
func nativeUnpreservedNotSentEvidence(e billing.NativeUsageEvidence) bool {
	return e.Version == 2 && e.Prepared != nil && e.Prepared.Purpose == model.DispatchGeneration &&
		e.AttemptScopeDigest == ([32]byte{}) && e.SendState == model.SendNotSent &&
		e.Confidence == billing.NativeUsageUnknown && e.Usage == nil &&
		e.UpstreamRequestID == "" && e.UpstreamResponseID == "" &&
		e.Terminal == model.TerminalUnknown && !e.TerminalConfirmed && !e.ConfirmedLowerBound
}

var _ billing.NativeEvidenceCommand = (*NativeUsageEvidenceInput)(nil)

// NativeUsageEvidenceFromAttempt copies facts only. RequestID is taken from
// root, never inferred from OperationID or an optional upstream response ID.
func NativeUsageEvidenceFromAttempt(root billing.NativeUsageEvidence, fact model.AttemptFact) (billing.NativeUsageEvidence, error) {
	if fact.Sequence == 0 || !fact.Identity.Valid() {
		return billing.NativeUsageEvidence{}, billing.ErrNativeUsageEvidence
	}
	e := root
	identity := fact.Identity
	e.AttemptSequence, e.Prepared, e.SendState = fact.Sequence, &identity, fact.Send
	e.UpstreamRequestID, e.UpstreamResponseID = fact.RequestID, fact.ResponseID
	e.Terminal, e.TerminalConfirmed, e.ConfirmedLowerBound = fact.Terminal, false, false
	e.Confidence, e.Usage = billing.NativeUsageUnknown, nil
	if u := fact.Usage; u != nil {
		if u.ResponseID != "" {
			if e.UpstreamResponseID != "" && e.UpstreamResponseID != u.ResponseID {
				return billing.NativeUsageEvidence{}, billing.ErrNativeUsageConflict
			}
			e.UpstreamResponseID = u.ResponseID
		}
		e.TerminalConfirmed = u.TerminalConfirmed
		switch u.Confidence {
		case model.UsageUnknown:
			e.Confidence = billing.NativeUsageUnknown
		case model.UsagePartial:
			e.Confidence = billing.NativeUsagePartial
		case model.UsageConfirmedLowerBound:
			e.Confidence, e.ConfirmedLowerBound = billing.NativeUsagePartial, true
		case model.UsageFinal:
			e.Confidence = billing.NativeUsageFinal
		default:
			return billing.NativeUsageEvidence{}, billing.ErrNativeUsageEvidence
		}
		count := func(n *model.TokenMeasurement) *int64 {
			if n == nil {
				return nil
			}
			v := n.Value
			return &v
		}
		if u.Input != nil || u.Output != nil || u.CacheRead != nil || u.CacheWrite != nil || u.ImageInput != nil || u.ImageOutput != nil || u.HostedImageInput != nil || u.HostedImageOutput != nil || u.ImageCount != nil {
			e.Usage = &billing.NativeUsageMeasurements{Semantics: billing.NativeUsageCumulative, Source: "operation.usage", ReturnedModel: u.ReturnedModel,
				TotalTokens: count(u.Total), ReasoningTokens: count(u.Reasoning), InputTokens: count(u.Input), OutputTokens: count(u.Output), CacheReadTokens: count(u.CacheRead), CacheCreationTokens: count(u.CacheWrite),
				ImageInputTokens: count(u.ImageInput), ImageOutputTokens: count(u.ImageOutput),
				HostedImageInputTokens: count(u.HostedImageInput), HostedImageOutputTokens: count(u.HostedImageOutput)}
			if u.ImageCount != nil {
				images := *u.ImageCount
				e.Usage.ImageCount = &images
			}
		}
	}
	// PricingDigest is assigned by PrepareNativeUsageEvidence after the frozen
	// policy is detached. Do not generate one here or manufacture absent usage.
	return e, nil
}

// validateNativeSettlementUsage consumes the existing builder's mapping. The
// raw observation and normalized charge buckets have different meanings; this
// boundary neither subtracts cache tokens nor recalculates prices.
func validateNativeObservedUsageMapping(e billing.NativeUsageEvidence, mapping *UsagePricingRequest) error {
	if e.Usage == nil || mapping == nil || mapping.OpenAI == nil || mapping.Gateway != nil || (e.AttemptSequence > 0 && !e.TerminalConfirmed) {
		return billing.ErrNativeUsageEvidence
	}
	if (e.GroupBound && (mapping.GroupID == nil || *mapping.GroupID != e.GroupID)) || (!e.GroupBound && mapping.GroupID != nil) {
		return billing.ErrNativeUsageConflict
	}
	raw := mapping.OpenAI.Usage
	// The existing parser may include independently reported reasoning in its
	// billed output. Retain raw evidence and validate only the same projection.
	output := e.Usage.OutputTokens
	if u := e.Usage; u.InputTokens != nil && output != nil && u.TotalTokens != nil && u.ReasoningTokens != nil {
		const maxInt64 = int64(^uint64(0) >> 1)
		if *u.InputTokens < 0 || *output < 0 || *u.TotalTokens < 0 || *u.ReasoningTokens < 0 || *u.InputTokens > maxInt64-*output {
			return billing.ErrNativeUsageConflict
		}
		canonical := xai.IncludeIndependentReasoningTokens(*u.InputTokens, *output, *u.TotalTokens, *u.ReasoningTokens)
		output = &canonical
	}
	imageProjection := func(primary, hosted *int64) (*int64, error) {
		if primary == nil && hosted == nil {
			return nil, nil
		}
		if (primary != nil && *primary < 0) || (hosted != nil && *hosted < 0) {
			return nil, billing.ErrNativeUsageConflict
		}
		// Missing stays missing in evidence. Only the original parser's
		// canonical projection uses its established zero/default convention.
		var p, h int64
		if primary != nil {
			p = *primary
		}
		if hosted != nil {
			h = *hosted
		}
		canonical := projectOpenAIHostedImageCount(p, h)
		return &canonical, nil
	}
	imageInput, err := imageProjection(e.Usage.ImageInputTokens, e.Usage.HostedImageInputTokens)
	if err != nil {
		return err
	}
	imageOutput, err := imageProjection(e.Usage.ImageOutputTokens, e.Usage.HostedImageOutputTokens)
	if err != nil {
		return err
	}
	for _, p := range []struct {
		observed *int64
		raw      int
	}{
		{e.Usage.InputTokens, raw.InputTokens}, {output, raw.OutputTokens},
		{e.Usage.CacheReadTokens, raw.CacheReadInputTokens}, {e.Usage.CacheCreationTokens, raw.CacheCreationInputTokens},
		{e.Usage.CacheCreation5mTokens, raw.CacheCreation5mTokens}, {e.Usage.CacheCreation1hTokens, raw.CacheCreation1hTokens},
		{imageInput, raw.ImageInputTokens}, {imageOutput, raw.ImageOutputTokens}, {e.Usage.ImageCount, mapping.OpenAI.ImageCount},
	} {
		if p.raw < 0 || (p.observed != nil && *p.observed != int64(p.raw)) {
			return billing.ErrNativeUsageConflict
		}
	}
	return nil
}

func validateObservedUsageMapping(protocol string, e billing.NativeUsageEvidence, mapping *UsagePricingRequest) error {
	if protocol != NativeIntentProtocolSystemOne {
		return validateNativeObservedUsageMapping(e, mapping)
	}
	if e.Usage == nil || e.Usage.InputTokens == nil || e.Usage.OutputTokens == nil ||
		mapping == nil || mapping.Gateway == nil || mapping.OpenAI != nil || !e.TerminalConfirmed {
		return billing.ErrNativeUsageEvidence
	}
	if (e.GroupBound && (mapping.GroupID == nil || *mapping.GroupID != e.GroupID)) || (!e.GroupBound && mapping.GroupID != nil) {
		return billing.ErrNativeUsageConflict
	}
	raw := mapping.Gateway.Usage
	// System One only measures input/output tokens. Reject unexplained
	// cache/media additions instead of interpreting missing quantities as zero.
	if raw != (ClaudeUsage{InputTokens: raw.InputTokens, OutputTokens: raw.OutputTokens}) ||
		mapping.Gateway.ImageCount != 0 || mapping.Gateway.ImageEndpoint || mapping.Gateway.SearchCount != 0 ||
		mapping.Gateway.AudioUsage != nil || mapping.Gateway.CacheStorageTokenHours != 0 {
		return billing.ErrNativeUsageConflict
	}
	for _, p := range []struct {
		observed *int64
		raw      int
	}{{e.Usage.InputTokens, raw.InputTokens}, {e.Usage.OutputTokens, raw.OutputTokens}, {e.Usage.CacheReadTokens, raw.CacheReadInputTokens}, {e.Usage.CacheCreationTokens, raw.CacheCreationInputTokens}, {e.Usage.ImageCount, mapping.Gateway.ImageCount}} {
		if p.raw < 0 || (p.observed != nil && *p.observed != int64(p.raw)) {
			return billing.ErrNativeUsageConflict
		}
	}
	return nil
}

func validateNativeSettlementUsage(e billing.NativeUsageEvidence, mapping *UsagePricingRequest, c *UsageBillingCommand) error {
	if err := validateNativeObservedUsageMapping(e, mapping); err != nil {
		return err
	}
	t, log := mapping.Tokens, c.UsageLog
	for _, p := range []struct{ normalized, command, log int }{
		{t.InputTokens, c.InputTokens, log.InputTokens}, {t.OutputTokens, c.OutputTokens, log.OutputTokens},
		{t.CacheReadTokens, c.CacheReadTokens, log.CacheReadTokens}, {t.CacheCreationTokens, c.CacheCreationTokens, log.CacheCreationTokens},
		{mapping.OpenAI.ImageCount, c.ImageCount, log.ImageCount},
	} {
		if p.normalized < 0 || p.normalized != p.command || p.normalized != p.log {
			return billing.ErrNativeUsageConflict
		}
	}
	return nil
}

func validateSettlementUsage(protocol string, e billing.NativeUsageEvidence, mapping *UsagePricingRequest, c *UsageBillingCommand) error {
	if protocol != NativeIntentProtocolSystemOne {
		return validateNativeSettlementUsage(e, mapping, c)
	}
	if err := validateObservedUsageMapping(protocol, e, mapping); err != nil {
		return err
	}
	if mapping == nil || mapping.Gateway == nil || c == nil || c.UsageLog == nil {
		return billing.ErrNativeUsageEvidence
	}
	for _, p := range []struct{ normalized, command, log int }{
		{mapping.Gateway.Usage.InputTokens, c.InputTokens, c.UsageLog.InputTokens},
		{mapping.Gateway.Usage.OutputTokens, c.OutputTokens, c.UsageLog.OutputTokens},
		{mapping.Gateway.Usage.CacheReadInputTokens, c.CacheReadTokens, c.UsageLog.CacheReadTokens},
		{mapping.Gateway.Usage.CacheCreationInputTokens, c.CacheCreationTokens, c.UsageLog.CacheCreationTokens},
		{mapping.Gateway.ImageCount, c.ImageCount, c.UsageLog.ImageCount},
	} {
		if p.normalized < 0 || p.normalized != p.command || p.normalized != p.log {
			return billing.ErrNativeUsageConflict
		}
	}
	return nil
}

// MergeNativeUsageRecord retains original observations, merges only within the
// same actual sequence, and keeps one selected monetary settlement for the root.
// Caller must supply detached/validated input. No amount is added across attempts.
func MergeNativeUsageRecord(prior *NativeUsageEvidenceRecord, input *NativeUsageEvidenceInput) (*NativeUsageEvidenceRecord, error) {
	if input == nil || len(input.Observations) == 0 {
		return nil, billing.ErrNativeUsageEvidence
	}
	record := NativeUsageEvidenceRecord{Version: 1, FrozenPricing: input.FrozenPricing, Intent: input.Intent}
	record.Protocol = input.Protocol
	if input.Intent != nil {
		record.Version = 2
	}
	if prior != nil {
		if prior.Version != record.Version || prior.Protocol != record.Protocol || (prior.Intent == nil) != (input.Intent == nil) {
			return nil, billing.ErrNativeUsageConflict
		}
		if prior.Intent != nil && (prior.Intent.Key != input.Intent.Key || prior.Intent.IntentFingerprint != input.Intent.IntentFingerprint) {
			return nil, billing.ErrNativeUsageConflict
		}
		if prior.Evidence.Fingerprint() != input.Evidence.Fingerprint() {
			return nil, billing.ErrNativeUsageConflict
		}
		record = *prior
		record.Observations = nil
	}
	states := make(map[uint64]billing.NativeUsageEvidence)
	appendObservation := func(e billing.NativeUsageEvidence) error {
		if e.Validate() != nil || e.Fingerprint() != input.Evidence.Fingerprint() {
			return billing.ErrNativeUsageConflict
		}
		for _, old := range record.Observations {
			if reflect.DeepEqual(old, e) {
				return nil
			}
		}
		if len(record.Observations) >= 64 {
			return errors.New("native usage observation limit exceeded")
		}
		if old, ok := states[e.AttemptSequence]; ok {
			merged, err := billing.MergeNativeUsageEvidence(old, e)
			if err != nil {
				return err
			}
			states[e.AttemptSequence] = merged
		} else {
			states[e.AttemptSequence] = e
		}
		record.Observations = append(record.Observations, e)
		return nil
	}
	if prior != nil {
		for _, e := range prior.Observations {
			if err := appendObservation(e); err != nil {
				return nil, err
			}
		}
	}
	for _, e := range input.Observations {
		if err := appendObservation(e); err != nil {
			return nil, err
		}
	}
	selected, ok := states[input.Evidence.AttemptSequence]
	if !ok {
		return nil, billing.ErrNativeUsageConflict
	}
	// A stale replay or a different failed attempt cannot erase the recorded
	// final. A bound settlement cannot be redirected to another account/attempt.
	if prior != nil && prior.Evidence.Confidence == billing.NativeUsageFinal {
		selected = states[prior.Evidence.AttemptSequence]
		if input.Settlement != nil && input.Evidence.AttemptSequence != selected.AttemptSequence {
			return nil, billing.ErrNativeUsageConflict
		}
	}
	if prior != nil && prior.SettlementSequence != nil && input.Settlement != nil && *prior.SettlementSequence != input.Evidence.AttemptSequence {
		return nil, billing.ErrNativeUsageConflict
	}
	record.Evidence = selected
	if input.Intent != nil && selected.AttemptSequence == input.Intent.AttemptSequence {
		if prior != nil && prior.Intent.AttemptSequence == input.Intent.AttemptSequence &&
			(prior.Intent.ScopeDigest != input.Intent.ScopeDigest || !reflect.DeepEqual(prior.FrozenPricing, input.FrozenPricing)) {
			return nil, billing.ErrNativeUsageConflict
		}
		record.Intent, record.FrozenPricing = input.Intent, input.FrozenPricing
	}
	if input.Settlement != nil {
		if record.SettlementUsage != nil && !reflect.DeepEqual(record.SettlementUsage, input.SettlementUsage) {
			return nil, billing.ErrNativeUsageConflict
		}
		sequence := selected.AttemptSequence
		record.SettlementSequence, record.SettlementUsage = &sequence, input.SettlementUsage
	}
	return &record, nil
}
