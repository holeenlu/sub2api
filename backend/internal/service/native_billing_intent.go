package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// NativeIntentRequestScope is policy, not a balance/spend or authorization lease.
// Mode is the original shared three-state policy; zero is invalid.
type NativeIntentRequestScope struct {
	Billing                                                           *NativeIntentRequestBillingPolicy `json:",omitempty"`
	Mode                                                              UsageBillingMode
	Protocol                                                          string `json:",omitempty"`
	PricingAt                                                         time.Time
	GroupID                                                           *int64
	SubscriptionID                                                    *int64
	UserRole                                                          string
	BillingModelSource                                                string
	OriginalModel                                                     string
	BaseMultiplier, TokenMultiplier, ImageMultiplier, VideoMultiplier float64
	GroupLongContextEnabled, FreeFast                                 bool
}

// NativeIntentAttemptScope contains actual pre-send policy values. Resolved
// cards reuse the existing calculator snapshots; no usage is invented here.
// ReturnedModelViewRef is a reference supplied by the real immutable-view
// owner, not evidence that an arbitrary historical catalog can be looked up.
type NativeIntentAttemptScope struct {
	Billing                *NativeIntentAttemptBillingPolicy `json:",omitempty"`
	Sequence               uint64
	Prepared               model.PreparedIdentity
	CapturedAt             time.Time
	BaselineModel          string
	ImageBillingModel      string `json:",omitempty"` // actual prepared image producer, not a text fallback
	CandidateModels        []string
	ChannelMappedModel     string
	ServiceTier            string
	ReasoningEffort        string
	LongContextEnabled     *bool
	ModelLongContextExempt bool
	FrozenPricing          *NativeFrozenPricing
	AdditionalPrices       map[string]NativeResolvedPricingSnapshot
	ReturnedModelViewRef   string
}

type NativeUsageIntent struct {
	Key billing.NativeIntentKey

	UserID             int64
	OriginalWireDigest [32]byte
	Request            NativeIntentRequestScope
	Attempt            NativeIntentAttemptScope
}

// NativeUsageIntentRecord lives on the technical event in usage_billing_events.
// It is separate from evidence: storing a scope must not manufacture usage.
type NativeUsageIntentRecord struct {
	Version int
	Key     billing.NativeIntentKey

	UserID             int64
	OriginalWireDigest [32]byte
	Request            NativeIntentRequestScope
	Attempts           []NativeIntentAttemptScope
}

type NativeUsageIntentRepository interface {
	StoreNativeUsageIntent(context.Context, *NativeUsageIntent) (billing.NativeIntentReceipt, error)
	LoadNativeUsageIntent(context.Context, billing.NativeIntentKey) (*NativeUsageIntentRecord, error)
	CloseNativeUsageIntent(context.Context, *NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error)
}

// NativeBillingIntentPort accepts already captured, actual price scopes. Its
// caller must obtain them before sending, not from late current configuration.
type NativeBillingIntentPort interface {
	PreserveBeforeSend(context.Context, NativeUsageIntent) (billing.NativeIntentReceipt, error)
	Load(context.Context, billing.NativeIntentKey) (*NativeUsageIntentRecord, error)
	CloseNotSent(context.Context, NativeIntentNotSentInput) (billing.NativeIntentClosureReceipt, error)
}

type NativeBillingIntentService struct{ repo NativeUsageIntentRepository }

func NewNativeBillingIntentService(repo NativeUsageIntentRepository) (*NativeBillingIntentService, error) {
	if repo == nil || (reflect.ValueOf(repo).Kind() == reflect.Pointer && reflect.ValueOf(repo).IsNil()) {
		return nil, billing.ErrNativeIntentUnavailable
	}
	return &NativeBillingIntentService{repo: repo}, nil
}

func ProvideNativeBillingIntentService(repo UsageBillingRepository) (*NativeBillingIntentService, error) {
	port, ok := repo.(NativeUsageIntentRepository)
	if !ok {
		return nil, billing.ErrNativeIntentUnavailable
	}
	return NewNativeBillingIntentService(port)
}

func (s *NativeBillingIntentService) PreserveBeforeSend(ctx context.Context, input NativeUsageIntent) (billing.NativeIntentReceipt, error) {
	var empty billing.NativeIntentReceipt
	if ctx == nil || s == nil || s.repo == nil {
		return empty, billing.ErrNativeIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	prepared, err := PrepareNativeUsageIntent(input)
	if err != nil {
		return empty, err
	}
	expected, err := prepared.NativeIntentReference()
	if err != nil {
		return empty, err
	}
	receipt, err := s.repo.StoreNativeUsageIntent(ctx, prepared)
	if err != nil {
		return empty, err
	}
	if !receipt.Durable() || receipt.NativeIntentReference != expected {
		return empty, billing.ErrNativeIntentConflict
	}
	return receipt, nil
}

func (s *NativeBillingIntentService) Load(ctx context.Context, id billing.NativeIntentKey) (*NativeUsageIntentRecord, error) {
	if ctx == nil || s == nil || s.repo == nil {
		return nil, billing.ErrNativeIntentUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.repo.LoadNativeUsageIntent(ctx, id)
}

// PrepareNativeUsageIntent detaches data and checks the complete policy boundary.
// It does not resolve a price, normalize quantities or infer monetary effects.
func PrepareNativeUsageIntent(input NativeUsageIntent) (*NativeUsageIntent, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Join(billing.ErrNativeIntentInvalid, err)
	}
	if len(raw) > 1<<20 {
		return nil, billing.ErrNativeIntentInvalid
	}
	var out NativeUsageIntent
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	p, request, attempt := out.Attempt.Prepared, out.Request, out.Attempt
	if out.Key.OperationID == "" || strings.TrimSpace(out.Key.OperationID) != out.Key.OperationID || out.Key.APIKeyID <= 0 || out.UserID <= 0 || out.OriginalWireDigest == ([32]byte{}) ||
		attempt.Sequence == 0 || !p.Valid() || p.Purpose != model.DispatchGeneration || p.Mode != "native" || (p.Transport != "http" && p.Transport != "ws") ||
		p.OperationID != out.Key.OperationID || p.APIKeyID != out.Key.APIKeyID || p.UserID != out.UserID || request.PricingAt.IsZero() || attempt.CapturedAt.IsZero() || !request.Mode.Valid() ||
		request.Protocol != "" && request.Protocol != NativeIntentProtocolSystemOne {
		return nil, billing.ErrNativeIntentInvalid
	}
	if request.GroupID == nil {
		if p.GroupBound || p.GroupID != 0 {
			return nil, billing.ErrNativeIntentConflict
		}
	} else if *request.GroupID <= 0 || !p.GroupBound || p.GroupID != *request.GroupID {
		return nil, billing.ErrNativeIntentConflict
	}
	if request.SubscriptionID != nil && (*request.SubscriptionID <= 0 || request.GroupID == nil) {
		return nil, billing.ErrNativeIntentInvalid
	}
	for _, rate := range []float64{request.BaseMultiplier, request.TokenMultiplier, request.ImageMultiplier, request.VideoMultiplier} {
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 {
			return nil, billing.ErrNativeIntentInvalid
		}
	}
	frozen := attempt.FrozenPricing
	if frozen == nil || frozen.BalanceExempt == nil || frozen.Policy == nil || frozen.Policy.Request != nil ||
		attempt.BaselineModel == "" || frozen.Policy.Model != attempt.BaselineModel || !frozen.Policy.PricingAt.Equal(request.PricingAt) {
		return nil, billing.ErrNativeIntentInvalid
	}
	policy := frozen.Policy
	if math.IsNaN(policy.AccountRateMultiplier) || math.IsInf(policy.AccountRateMultiplier, 0) || policy.AccountRateMultiplier < 0 || (policy.Balance && policy.Subscription) ||
		(*frozen.BalanceExempt && policy.Balance) || (policy.Subscription && request.SubscriptionID == nil) {
		return nil, billing.ErrNativeIntentConflict
	}
	if request.Mode == UsageBillingModeLogOnly && (policy.Balance || policy.Subscription || policy.KeyQuota || policy.KeyRateLimit || policy.AccountQuota) {
		return nil, billing.ErrNativeIntentConflict
	}
	if request.Mode == UsageBillingModeKeyWindowOnly && (policy.Balance || policy.Subscription || policy.KeyQuota || policy.AccountQuota || !policy.KeyRateLimit || *frozen.BalanceExempt) {
		return nil, billing.ErrNativeIntentConflict
	}
	if frozen.Resolved == nil && !policy.Pending {
		return nil, billing.ErrNativeIntentInvalid
	}
	if frozen.Resolved != nil {
		if err := validateNativeIntentPriceCard(*frozen.Resolved); err != nil {
			return nil, err
		}
	}
	baseline := false
	for _, candidate := range attempt.CandidateModels {
		if candidate == "" || strings.TrimSpace(candidate) != candidate {
			return nil, billing.ErrNativeIntentInvalid
		}
		baseline = baseline || candidate == attempt.BaselineModel
	}
	if !baseline {
		return nil, billing.ErrNativeIntentInvalid
	}
	for name, price := range attempt.AdditionalPrices {
		if name == "" || name == attempt.BaselineModel {
			return nil, billing.ErrNativeIntentConflict
		}
		if err := validateNativeIntentPriceCard(price); err != nil {
			return nil, err
		}
	}
	if request.BillingModelSource == BillingModelSourceResponse && attempt.ReturnedModelViewRef == "" {
		return nil, billing.ErrNativeIntentInvalid
	}
	return &out, nil
}

func validateNativeIntentPriceCard(price NativeResolvedPricingSnapshot) error {
	if price.Source == "" {
		return billing.ErrNativeIntentInvalid
	}
	switch price.Mode {
	case BillingModeToken:
		if price.BasePricing == nil && len(price.Intervals) == 0 {
			return billing.ErrNativeIntentInvalid
		}
	case BillingModePerRequest, BillingModeImage, BillingModeVideo:
	default:
		return billing.ErrNativeIntentInvalid
	}
	_, err := price.Restore()
	return err
}

func nativeIntentRootFingerprint(input NativeUsageIntent) (string, error) {
	root := struct {
		Version int
		Key     billing.NativeIntentKey

		UserID             int64
		OriginalWireDigest [32]byte
		Request            NativeIntentRequestScope
	}{1, input.Key, input.UserID, input.OriginalWireDigest, input.Request}
	raw, err := json.Marshal(root)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (i *NativeUsageIntent) NativeIntentReference() (billing.NativeIntentReference, error) {
	if i == nil {
		return billing.NativeIntentReference{}, billing.ErrNativeIntentInvalid
	}
	prepared, err := PrepareNativeUsageIntent(*i)
	if err != nil {
		return billing.NativeIntentReference{}, err
	}
	root, err := nativeIntentRootFingerprint(*prepared)
	if err != nil {
		return billing.NativeIntentReference{}, err
	}
	raw, err := json.Marshal(prepared.Attempt)
	if err != nil {
		return billing.NativeIntentReference{}, err
	}
	sum := sha256.Sum256(raw)
	return billing.NativeIntentReference{Key: prepared.Key, AttemptSequence: prepared.Attempt.Sequence, IntentFingerprint: root, ScopeDigest: hex.EncodeToString(sum[:])}, nil
}

// MergeNativeUsageIntent appends a genuine prepared sequence, never reprices one.
func MergeNativeUsageIntent(prior *NativeUsageIntentRecord, input NativeUsageIntent) (*NativeUsageIntentRecord, error) {
	next, err := PrepareNativeUsageIntent(input)
	if err != nil {
		return nil, err
	}
	record := NativeUsageIntentRecord{Version: 1, Key: next.Key, UserID: next.UserID, OriginalWireDigest: next.OriginalWireDigest, Request: next.Request}
	if prior != nil {
		raw, err := json.Marshal(prior)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		if record.Version != 1 || len(record.Attempts) == 0 {
			return nil, billing.ErrNativeIntentInvalid
		}
		original, err := nativeIntentRootFingerprint(NativeUsageIntent{Key: record.Key, UserID: record.UserID, OriginalWireDigest: record.OriginalWireDigest, Request: record.Request})
		if err != nil {
			return nil, err
		}
		expected, err := nativeIntentRootFingerprint(*next)
		if err != nil {
			return nil, err
		}
		if original != expected {
			return nil, billing.ErrNativeIntentConflict
		}
	}
	for _, old := range record.Attempts {
		if old.Sequence == next.Attempt.Sequence {
			if !reflect.DeepEqual(old, next.Attempt) {
				return nil, billing.ErrNativeIntentConflict
			}
			return &record, nil
		}
	}
	if len(record.Attempts) >= 64 {
		return nil, billing.ErrNativeIntentInvalid
	}
	record.Attempts = append(record.Attempts, next.Attempt)
	sort.Slice(record.Attempts, func(i, j int) bool { return record.Attempts[i].Sequence < record.Attempts[j].Sequence })
	raw, err := json.Marshal(record)
	if err != nil || len(raw) > 1<<20 {
		return nil, billing.ErrNativeIntentInvalid
	}
	return &record, nil
}

// DecodeNativeUsageIntentRecord validates all persisted scopes and root identity.
func DecodeNativeUsageIntentRecord(raw []byte, id billing.NativeIntentKey, fingerprint string) (*NativeUsageIntentRecord, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, billing.ErrNativeIntentInvalid
	}
	var stored NativeUsageIntentRecord
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, err
	}
	if stored.Version != 1 || stored.Key != id || len(stored.Attempts) == 0 || len(stored.Attempts) > 64 {
		return nil, billing.ErrNativeIntentConflict
	}
	var validated *NativeUsageIntentRecord
	seen := make(map[uint64]bool)
	for _, attempt := range stored.Attempts {
		if seen[attempt.Sequence] {
			return nil, billing.ErrNativeIntentConflict
		}
		seen[attempt.Sequence] = true
		input := NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: attempt}
		ref, err := input.NativeIntentReference()
		if err != nil {
			return nil, err
		}
		if ref.IntentFingerprint != fingerprint {
			return nil, billing.ErrNativeIntentConflict
		}
		validated, err = MergeNativeUsageIntent(validated, input)
		if err != nil {
			return nil, err
		}
	}
	return validated, nil
}

var _ NativeBillingIntentPort = (*NativeBillingIntentService)(nil)
var _ billing.NativeIntentCommand = (*NativeUsageIntent)(nil)
