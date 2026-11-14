package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
)

// Preparation contains the original measured result, never an account, key,
// network capability, header map, body, or newly resolved monetary quote.
type NativeUsagePreparation struct {
	FinalPricing *NativeIntentFinalPricing
	// Existing usage-log attribution only, never a header map or credential.
	// Omit absent values so previously persisted preparation digests stay valid.
	UserAgent          string `json:",omitempty"`
	IPAddress          string `json:",omitempty"`
	Result             NativeUsagePreparationResult
	PriorAuthorization []NativeUsagePreparationResult
	InboundEndpoint    string
	QuotaPlatform      string
	RequestPayloadHash string
	NativeCompactionV2 bool
	CyberBlocked       bool
	ChannelUsageFields
}

type NativeUsagePreparationResult struct {
	Usage                                                       OpenAIUsage
	AuthorizationUsage                                          *OpenAIUsage
	AuthorizationMode                                           string
	RequestID, ResponseID                                       string
	UsageUpstreamRequestID                                      *string
	Model, BillingModel, UpstreamModel, UpstreamResponseModel   string
	UpstreamResponseModelConflict                               bool
	UpstreamResponseServiceTier, UpstreamEndpoint               string
	ServiceTier, ReasoningEffort, RequestedReasoningEffort      *string
	Stream, OpenAIWSMode                                        bool
	ImageCount                                                  int
	ImageEndpoint                                               bool
	ImageSize, ImageInputSize, ImageOutputSize, ImageSizeSource string
	ImageOutputSizes                                            []string
	ImageSizeBreakdown                                          map[string]int
	VideoCount, VideoDurationSeconds                            int
	VideoResolution                                             string
	WebSearchCalls, SearchCount                                 int
	AudioUsage                                                  *AudioUsage
	Duration                                                    time.Duration
	FirstTokenMs                                                *int
	LatencyMetrics                                              *UsageLatencyMetrics
}

// CaptureNativeUsagePreparation must run before mutable billing normalization.
// It copies only existing metering/attribution fields. Quantity interpretation
// still belongs to the original builder, including cache and reasoning rules.
func CaptureNativeUsagePreparation(ctx context.Context, in *OpenAIRecordUsageInput) (*NativeUsagePreparation, error) {
	if ctx == nil || in == nil || in.Result == nil {
		return nil, billing.ErrNativeIntentInvalid
	}
	project := func(source *OpenAIForwardResult) (NativeUsagePreparationResult, error) {
		if source == nil || source.CloudManagedBilling || source.UsageLowerBound {
			return NativeUsagePreparationResult{}, billing.ErrNativeIntentInvalid
		}
		return NativeUsagePreparationResult{
			Usage: source.Usage, AuthorizationUsage: source.authorizationUsage, AuthorizationMode: source.AuthorizationMode,
			RequestID: source.RequestID, ResponseID: source.ResponseID, UsageUpstreamRequestID: usageUpstreamRequestIDPtr(in.Account, source.UpstreamHeaders, source.OpenAIWSMode),
			Model: source.Model, BillingModel: source.BillingModel, UpstreamModel: source.UpstreamModel, UpstreamResponseModel: source.UpstreamResponseModel,
			UpstreamResponseModelConflict: source.UpstreamResponseModelConflict, UpstreamResponseServiceTier: source.UpstreamResponseServiceTier, UpstreamEndpoint: source.UpstreamEndpoint,
			ServiceTier: source.ServiceTier, ReasoningEffort: source.ReasoningEffort, RequestedReasoningEffort: source.RequestedReasoningEffort,
			Stream: source.Stream, OpenAIWSMode: source.OpenAIWSMode, ImageCount: source.ImageCount, ImageEndpoint: source.ImageEndpoint, ImageSize: source.ImageSize,
			ImageInputSize: source.ImageInputSize, ImageOutputSize: source.ImageOutputSize, ImageSizeSource: source.ImageSizeSource, ImageOutputSizes: source.ImageOutputSizes, ImageSizeBreakdown: source.ImageSizeBreakdown,
			VideoCount: source.VideoCount, VideoDurationSeconds: source.VideoDurationSeconds, VideoResolution: source.VideoResolution,
			WebSearchCalls: source.WebSearchCalls, SearchCount: source.SearchCount, AudioUsage: source.AudioUsage,
			Duration: source.Duration, FirstTokenMs: source.FirstTokenMs, LatencyMetrics: source.LatencyMetrics,
		}, nil
	}
	result, err := project(in.Result)
	if err != nil {
		return nil, err
	}
	p := &NativeUsagePreparation{Result: result, InboundEndpoint: in.InboundEndpoint, QuotaPlatform: in.QuotaPlatform, UserAgent: in.UserAgent, IPAddress: in.IPAddress,
		RequestPayloadHash: resolveUsageBillingPayloadFingerprint(ctx, in.RequestPayloadHash), NativeCompactionV2: in.NativeCompactionV2, CyberBlocked: in.CyberBlocked, ChannelUsageFields: in.ChannelUsageFields}
	if len(in.Result.PriorAuthorizationResults) > 64 {
		return nil, billing.ErrNativeIntentInvalid
	}
	for _, source := range in.Result.PriorAuthorizationResults {
		if source == nil || len(source.PriorAuthorizationResults) > 0 {
			return nil, billing.ErrNativeIntentInvalid
		}
		prior, err := project(source)
		if err != nil {
			return nil, err
		}
		p.PriorAuthorization = append(p.PriorAuthorization, prior)
	}
	return detachNativePreparation(p)
}

func detachNativePreparation(in *NativeUsagePreparation) (*NativeUsagePreparation, error) {
	if in == nil {
		return nil, nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	if len(raw) > 1<<20 {
		return nil, billing.ErrNativeIntentInvalid
	}
	var result NativeUsagePreparation
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ForwardResult restores only observations; it grants no transport/authority.
func (p NativeUsagePreparationResult) ForwardResult() *OpenAIForwardResult {
	return &OpenAIForwardResult{Usage: p.Usage, authorizationUsage: p.AuthorizationUsage, AuthorizationMode: p.AuthorizationMode,
		RequestID: p.RequestID, ResponseID: p.ResponseID, Model: p.Model, BillingModel: p.BillingModel, UpstreamModel: p.UpstreamModel, UpstreamResponseModel: p.UpstreamResponseModel,
		UpstreamResponseModelConflict: p.UpstreamResponseModelConflict, UpstreamResponseServiceTier: p.UpstreamResponseServiceTier, UpstreamEndpoint: p.UpstreamEndpoint,
		ServiceTier: p.ServiceTier, ReasoningEffort: p.ReasoningEffort, RequestedReasoningEffort: p.RequestedReasoningEffort,
		Stream: p.Stream, OpenAIWSMode: p.OpenAIWSMode, ImageCount: p.ImageCount, ImageEndpoint: p.ImageEndpoint, ImageSize: p.ImageSize, ImageInputSize: p.ImageInputSize, ImageOutputSize: p.ImageOutputSize,
		ImageSizeSource: p.ImageSizeSource, ImageOutputSizes: p.ImageOutputSizes, ImageSizeBreakdown: p.ImageSizeBreakdown,
		VideoCount: p.VideoCount, VideoDurationSeconds: p.VideoDurationSeconds, VideoResolution: p.VideoResolution,
		WebSearchCalls: p.WebSearchCalls, SearchCount: p.SearchCount, AudioUsage: p.AudioUsage, Duration: p.Duration, FirstTokenMs: p.FirstTokenMs, LatencyMetrics: p.LatencyMetrics}
}

type NativeIntentFinalizationInput struct {
	SystemOne   *SystemOneUsagePreparation `json:",omitempty"`
	Intent      billing.NativeIntentReference
	Evidence    NativeUsageEvidenceInput
	Preparation *NativeUsagePreparation
	Reason      billing.NativePreparationReason
}

func (i *NativeIntentFinalizationInput) NativeIntentFinalizationReference() (billing.NativeIntentReference, error) {
	if i == nil {
		return billing.NativeIntentReference{}, billing.ErrNativeIntentInvalid
	}
	return i.Intent, nil
}
func (i *NativeIntentFinalizationInput) NativeCanonicalEvidence() billing.NativeEvidenceCommand {
	return &i.Evidence
}

type NativeIntentPreparationObservation struct {
	Sequence    uint64
	Preparation *NativeUsagePreparation
}

type NativeIntentFinalizationRecord struct {
	Version  int
	Intent   billing.NativeIntentReference
	Evidence *NativeUsageEvidenceRecord
	// SystemOne is a protocol-specific projection stored in the same native
	// intent journal. Its presence is the discriminator used by recovery; it
	// must never be interpreted by the OpenAI pricing preparer.
	SystemOne    *SystemOneUsagePreparation `json:",omitempty"`
	Preparation  *NativeUsagePreparation
	Preparations []NativeIntentPreparationObservation
	Reason       billing.NativePreparationReason
}

func (r *NativeIntentFinalizationRecord) Fingerprint() (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	if len(raw) > 1<<20 {
		return "", billing.ErrNativeIntentInvalid
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

type NativeIntentFinalizationResult struct {
	Intent    billing.NativeIntentFinalizationReceipt
	Canonical *NativeUsageRecordResult
}

type NativeIntentRecoveryInput struct {
	Intent       *NativeUsageIntentRecord
	Finalization *NativeIntentFinalizationRecord
}
type NativeIntentPreparedUsage struct {
	Settlement      *UsageBillingCommand
	SettlementUsage *UsagePricingRequest
}
type NativeIntentPreparer interface {
	PrepareNativeIntentUsage(context.Context, NativeIntentRecoveryInput) (NativeIntentPreparedUsage, error)
}
type NativeIntentFinalizationRepository interface {
	FinalizeNativeUsageIntent(context.Context, *NativeIntentFinalizationInput) (NativeIntentFinalizationResult, error)
	LoadNativeIntentRecovery(context.Context, billing.NativeIntentKey) (*NativeIntentRecoveryInput, error)
	ConfigureNativeIntentPreparation(func(context.Context, *NativeIntentRecoveryInput) (*NativeIntentFinalizationInput, error))
}
type NativeIntentFinalizationService struct {
	repo     NativeIntentFinalizationRepository
	preparer NativeIntentPreparer
}

// Configure the original worker before Start; this callback is a calculator
// port. Work and all recovery inputs remain in the existing SQL journal.
func NewNativeIntentFinalizationService(repo NativeIntentFinalizationRepository, preparer NativeIntentPreparer) (*NativeIntentFinalizationService, error) {
	if nativeIntentNil(repo) || nativeIntentNil(preparer) {
		return nil, billing.ErrNativeIntentUnavailable
	}
	s := &NativeIntentFinalizationService{repo: repo, preparer: preparer}
	repo.ConfigureNativeIntentPreparation(s.prepareRecovered)
	return s, nil
}
func ProvideNativeIntentFinalizationService(repo UsageBillingRepository, preparer NativeIntentPreparer) (*NativeIntentFinalizationService, error) {
	port, ok := repo.(NativeIntentFinalizationRepository)
	if !ok {
		return nil, billing.ErrNativeIntentUnavailable
	}
	return NewNativeIntentFinalizationService(port, preparer)
}
func nativeIntentNil(v any) bool {
	return v == nil || (reflect.ValueOf(v).Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil())
}

func (s *NativeIntentFinalizationService) FinalizeNativeIntent(ctx context.Context, input NativeIntentFinalizationInput) (NativeIntentFinalizationResult, error) {
	if s == nil || s.repo == nil || ctx == nil {
		return NativeIntentFinalizationResult{}, billing.ErrNativeIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return NativeIntentFinalizationResult{}, err
	}
	prepared, err := PrepareNativeIntentFinalization(input)
	if err != nil {
		return NativeIntentFinalizationResult{}, err
	}
	result, err := s.repo.FinalizeNativeUsageIntent(ctx, prepared)
	if result.Intent.Durable() && result.Intent.NativeIntentReference != prepared.Intent {
		result.Intent = billing.NativeIntentFinalizationReceipt{}
		return result, errors.Join(err, billing.ErrNativeIntentConflict)
	}
	if result.Canonical != nil && result.Canonical.Receipt.Durable() && result.Canonical.Receipt.Identity != prepared.Evidence.Evidence.Identity {
		result.Canonical.Receipt = billing.NativeUsageReceipt{}
		return result, errors.Join(err, billing.ErrNativeIntentConflict)
	}
	if !result.Intent.Durable() && err == nil {
		return result, billing.ErrNativeIntentUnavailable
	}
	return result, err
}

func (s *NativeIntentFinalizationService) prepareRecovered(ctx context.Context, in *NativeIntentRecoveryInput) (*NativeIntentFinalizationInput, error) {
	if in == nil || in.Intent == nil || in.Finalization == nil || in.Finalization.Evidence == nil {
		return nil, billing.ErrNativeIntentInvalid
	}
	if in.Finalization.Evidence.Evidence.Identity.RequestID == "" {
		return nil, &NativePreparationError{Reason: billing.NativeCanonicalIdentityUnavailable, Cause: billing.ErrNativePreparationPending}
	}
	quote, err := s.preparer.PrepareNativeIntentUsage(ctx, *in)
	if err != nil {
		return nil, err
	}
	if quote.Settlement == nil || quote.SettlementUsage == nil || quote.Settlement.NeedsPricing() {
		return nil, billing.ErrNativePreparationPending
	}
	record := in.Finalization
	return PrepareNativeIntentFinalization(NativeIntentFinalizationInput{Intent: record.Intent, Preparation: record.Preparation,
		Evidence: NativeUsageEvidenceInput{Intent: &record.Intent, Evidence: record.Evidence.Evidence, Observations: record.Evidence.Observations, FrozenPricing: record.Evidence.FrozenPricing,
			SettlementUsage: quote.SettlementUsage, Settlement: quote.Settlement, Protocol: record.Evidence.Protocol}})
}

func PrepareNativeIntentFinalization(input NativeIntentFinalizationInput) (*NativeIntentFinalizationInput, error) {
	if !input.Reason.Valid() || !(billing.NativeIntentReceipt{NativeIntentReference: input.Intent, Disposition: billing.NativeIntentPreserved}).Durable() {
		return nil, billing.ErrNativeIntentInvalid
	}
	if input.Evidence.Intent == nil || *input.Evidence.Intent != input.Intent {
		return nil, billing.ErrNativeIntentConflict
	}
	evidence, err := PrepareNativeUsageEvidence(input.Evidence)
	if err != nil {
		return nil, err
	}
	if input.SystemOne != nil || evidence.Protocol != "" {
		return prepareSystemOneFinalization(input, evidence)
	}
	e := evidence.Evidence
	if e.Version != 2 || e.OperationID != input.Intent.Key.OperationID || e.Identity.APIKeyID != input.Intent.Key.APIKeyID || e.AttemptSequence != input.Intent.AttemptSequence {
		return nil, billing.ErrNativeIntentConflict
	}
	preparation, err := detachNativePreparation(input.Preparation)
	if err != nil {
		return nil, err
	}
	if preparation != nil && preparation.FinalPricing != nil {
		decision := preparation.FinalPricing
		digest, err := nativePreparationDigest(preparation)
		if err != nil {
			return nil, err
		}
		if e.Confidence != billing.NativeUsageFinal || !e.TerminalConfirmed || decision.Version != 1 ||
			decision.AttemptSequence != input.Intent.AttemptSequence || decision.ScopeDigest != input.Intent.ScopeDigest || decision.PreparationDigest != digest {
			return nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, billing.ErrNativeIntentConflict)
		}
		if decision.Price != nil {
			if err := validateNativeIntentPriceCard(*decision.Price); err != nil {
				return nil, nativePreparationFailure(billing.NativeReturnedPriceViewUnavailable, err)
			}
		}
	}
	if e.Confidence == billing.NativeUsageFinal && e.TerminalConfirmed {
		if preparation == nil {
			// The parser's final facts exist independently of the downstream
			// billing projection. Preserve them without inventing that projection
			// or downgrading measured usage. Only an actual later preparation may
			// make this record eligible for canonical binding and recovery.
			if evidence.Settlement != nil || evidence.SettlementUsage != nil {
				return nil, billing.ErrNativeIntentConflict
			}
			input.Reason = billing.NativeUsagePreparationUnavailable
		} else {
			if input.Reason == billing.NativeUsagePreparationUnavailable {
				return nil, billing.ErrNativeIntentConflict
			}
			mapping := &UsagePricingRequest{OpenAI: preparation.Result.ForwardResult()}
			if e.GroupBound {
				id := e.GroupID
				mapping.GroupID = &id
			}
			if err = validateNativeObservedUsageMapping(e, mapping); err != nil {
				return nil, err
			}
		}
	}
	if evidence.Settlement != nil && input.Reason != billing.NativePreparationReady {
		return nil, billing.ErrNativeIntentConflict
	}
	return &NativeIntentFinalizationInput{Intent: input.Intent, Evidence: *evidence, Preparation: preparation, Reason: input.Reason}, nil
}

type NativePreparationError struct {
	Reason billing.NativePreparationReason
	Cause  error
}

func (e *NativePreparationError) Error() string { return string(e.Reason) }
func (e *NativePreparationError) Unwrap() error { return e.Cause }
func NativePreparationFailureReason(err error) billing.NativePreparationReason {
	var pending *NativePreparationError
	if errors.As(err, &pending) && pending.Reason.Valid() && pending.Reason != billing.NativePreparationReady {
		return pending.Reason
	}
	return billing.NativeBuilderUnavailable
}

var _ billing.NativeIntentFinalizationCommand = (*NativeIntentFinalizationInput)(nil)
