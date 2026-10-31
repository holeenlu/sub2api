package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/tidwall/gjson"
)

// NativeIntentPricingInput is supplied once by the authenticated logical turn,
// at its original pricing boundary, before selecting or sending a generation.
type NativeIntentPricingInput = nativeIntentRequestPricingInput

// NativeIntentModelFacts comes from the actual request encoder. BillingModel
// and ImageBillingModel must be its decisions, never inferred from a wire slug.
type NativeIntentModelFacts struct {
	Model, UpstreamModel, BillingModel, ImageBillingModel string
	ServiceTier, ReasoningEffort                          string
}
type nativeIntentModelFactsKey struct{}

// WithNativeIntentModelFacts carries one encoder's facts to its sealed attempt.
// It grants no permission and changes neither the request nor its billing rule.
func WithNativeIntentModelFacts(ctx context.Context, facts NativeIntentModelFacts) context.Context {
	return context.WithValue(ctx, nativeIntentModelFactsKey{}, facts)
}

func nativeIntentWSModelFacts(ctx context.Context, wire []byte) NativeIntentModelFacts {
	facts, _ := ctx.Value(nativeIntentModelFactsKey{}).(NativeIntentModelFacts)
	facts.UpstreamModel = strings.TrimSpace(gjson.GetBytes(wire, "model").String())
	facts.ServiceTier = strings.TrimSpace(gjson.GetBytes(wire, "service_tier").String())
	facts.ReasoningEffort = optionalStringValue(extractOpenAIReasoningEffortFromBody(wire, facts.UpstreamModel, facts.Model))
	return facts
}

// Model facts and wire must come from the same owned, bounded body. GetBody
// is only a replayability signal here, never authority for pricing: a caller's
// replay callback can return bytes different from the body provider.Prepare seals.
// Exclusive streams remain untouched and use their encoder's explicit facts.
func nativeIntentHTTPModelFacts(request *http.Request, limit int64) (NativeIntentModelFacts, error) {
	facts, _ := request.Context().Value(nativeIntentModelFactsKey{}).(NativeIntentModelFacts)
	if request.GetBody == nil || request.Body == nil || request.Body == http.NoBody || request.Header.Get("Content-Encoding") != "" {
		return facts, nil
	}
	if limit <= 0 || limit == math.MaxInt64 {
		return facts, billing.ErrNativeIntentInvalid
	}
	source := request.Body
	var once sync.Once
	var closeErr error
	closeSource := func() { once.Do(func() { closeErr = source.Close() }) }
	stop := context.AfterFunc(request.Context(), closeSource)
	wire, readErr := io.ReadAll(io.LimitReader(source, limit+1))
	stop()
	closeSource()
	request.Body = http.NoBody
	if readErr != nil || closeErr != nil || request.Context().Err() != nil {
		return facts, errors.Join(readErr, closeErr, context.Cause(request.Context()))
	}
	if int64(len(wire)) > limit {
		return facts, native.ErrReplayBodyLimit
	}
	if request.ContentLength > 0 && request.ContentLength != int64(len(wire)) {
		return facts, native.ErrInvalidHTTPPreparation
	}
	request.Body = httputil.NewPrereadBody(wire)
	request.ContentLength = int64(len(wire))
	request.GetBody = func() (io.ReadCloser, error) { return httputil.NewPrereadBody(wire), nil }
	if len(wire) != 0 {
		facts = nativeIntentWSModelFacts(request.Context(), wire)
	}
	return facts, nil
}

type nativeIntentRuntime struct {
	gateway    *OpenAIGatewayService
	request    *nativeIntentRequestPricing
	port       NativeBillingIntentPort
	record     *NativeUsageIntentRecord
	references map[uint64]billing.NativeIntentReference
	live       map[uint64]*nativeIntentLivePricing
	// An uncertain preserve is checked at bounded cleanup even after cancellation.
	uncertain *NativeUsageIntent
	closed    bool
	closeErr  error
}

// BindNativeIntentPricing pins the original policy on the existing operation.
// Re-entry on an account retry keeps that same publication; each new WS turn
// must have its own operation. Other platform roots have no Native owner.
func (s *OpenAIGatewayService) BindNativeIntentPricing(ctx context.Context, in NativeIntentPricingInput) error {
	op := nativeHTTPFromContext(ctx)
	if op == nil {
		return nil
	}
	if op.intent != nil {
		return nil
	}
	if s == nil || in.APIKey == nil || in.User == nil || in.APIKey.ID != op.dispatch.request.APIKeyID || in.User.ID != op.dispatch.request.UserID {
		return billing.ErrNativeIntentConflict
	}
	port, err := ProvideNativeBillingIntentService(s.usageBillingRepo)
	if err != nil {
		return err
	}
	// Only actual HTTP correlation identities are known before sending. The
	// WS connection's correlation IDs are not the turn's monetary identity.
	if in.KnownCanonical == nil && op.dispatch.request.Transport == "http" {
		if id := nativeIntentCanonicalID(ctx, "", false); id != "" {
			in.KnownCanonical = &billing.Identity{RequestID: id, APIKeyID: in.APIKey.ID}
		}
	}
	request, err := s.captureNativeIntentRequestPricing(ctx, in)
	if err != nil {
		return err
	}
	op.intent = &nativeIntentRuntime{gateway: s, request: request, port: port,
		references: make(map[uint64]billing.NativeIntentReference), live: make(map[uint64]*nativeIntentLivePricing)}
	return nil
}

func nativeIntentCanonicalID(ctx context.Context, upstream string, ws bool) string {
	upstream = strings.TrimSpace(upstream)
	if ws {
		return upstream
	}
	// Resolve with the original resolver only when its inputs actually exist;
	// its generated-ID fallback is not evidence of a monetary identity.
	if upstream != "" {
		return resolveUsageBillingRequestID(ctx, upstream)
	}
	for _, key := range []ctxkey.Key{ctxkey.ClientRequestID, ctxkey.RequestID} {
		if value, _ := ctx.Value(key).(string); strings.TrimSpace(value) != "" {
			return resolveUsageBillingRequestID(ctx, "")
		}
	}
	return ""
}

// preserveNativeIntent runs after Admit has assigned the sequence and before
// Open consumes the original permit and rechecks live authority and leases.
func (o *nativeHTTPOperation) preserveNativeIntent(ctx context.Context, id model.PreparedIdentity, sequence uint64, facts NativeIntentModelFacts) error {
	if id.Purpose == model.DispatchInternalPreparation {
		return nil
	}
	runtime := o.intent
	if runtime == nil || runtime.request == nil || runtime.port == nil {
		return billing.ErrNativeIntentUnavailable
	}
	if runtime.uncertain != nil {
		return billing.ErrNativeIntentUnavailable
	}
	account, credential, err := o.dispatch.authority.loadResource(ctx, id.ResourceID)
	if err != nil {
		return err
	}
	scope, live, err := runtime.gateway.captureNativeIntentAttemptPricing(ctx, runtime.request, nativeIntentAttemptPricingInput{
		Account: account, CredentialAccount: credential, Prepared: id, Sequence: sequence, CapturedAt: time.Now(),
		Model: facts.Model, UpstreamModel: facts.UpstreamModel, BillingModel: facts.BillingModel, ImageBillingModel: facts.ImageBillingModel,
		ServiceTier: facts.ServiceTier, ReasoningEffort: facts.ReasoningEffort,
		ReturnedModelViewRef: fmt.Sprintf("native-live:%s:%d", id.OperationID, sequence),
	})
	if err != nil {
		return err
	}
	input := NativeUsageIntent{Key: billing.NativeIntentKey{OperationID: id.OperationID, APIKeyID: id.APIKeyID}, UserID: id.UserID,
		OriginalWireDigest: o.core.Snapshot().OriginalWireDigest, Request: runtime.request.Scope, Attempt: scope}
	detached, err := PrepareNativeUsageIntent(input)
	if err != nil {
		return err
	}
	// Retain the exact candidate for ACK-loss reconciliation, never a receipt.
	runtime.uncertain = detached
	receipt, err := runtime.port.PreserveBeforeSend(ctx, *detached)
	if err != nil {
		return err
	}
	expected, err := detached.NativeIntentReference()
	if err != nil || !receipt.Durable() || receipt.NativeIntentReference != expected {
		return errors.Join(err, billing.ErrNativeIntentConflict)
	}
	record, err := MergeNativeUsageIntent(runtime.record, *detached)
	if err != nil {
		return err
	}
	runtime.record, runtime.uncertain = record, nil
	runtime.references[sequence], runtime.live[sequence] = expected, live
	return nil
}

// reconcileNativeIntent never authorizes sending after a failed preserve. It
// only establishes which exact scopes actually exist for terminal cleanup.
func (r *nativeIntentRuntime) reconcileNativeIntent(ctx context.Context) error {
	if r == nil || r.uncertain == nil {
		return nil
	}
	pending := r.uncertain
	stored, err := r.port.Load(ctx, pending.Key)
	if errors.Is(err, sql.ErrNoRows) {
		if r.record != nil {
			return billing.ErrNativeIntentConflict
		}
		r.uncertain = nil
		return nil
	}
	if err != nil {
		return err
	}
	if stored == nil {
		return billing.ErrNativeIntentUnavailable
	}
	expected, err := pending.NativeIntentReference()
	if err != nil {
		return err
	}
	refs := make(map[uint64]billing.NativeIntentReference, len(stored.Attempts))
	for _, attempt := range stored.Attempts {
		ref, err := (&NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: attempt}).NativeIntentReference()
		if err != nil || ref.Key != expected.Key || ref.IntentFingerprint != expected.IntentFingerprint || stored.OriginalWireDigest != pending.OriginalWireDigest || stored.UserID != pending.UserID {
			return errors.Join(err, billing.ErrNativeIntentConflict)
		}
		if attempt.Sequence == pending.Attempt.Sequence && ref != expected {
			return billing.ErrNativeIntentConflict
		}
		if old, ok := r.references[attempt.Sequence]; ok && old != ref {
			return billing.ErrNativeIntentConflict
		}
		refs[attempt.Sequence] = ref
	}
	for sequence, old := range r.references {
		if refs[sequence] != old {
			return billing.ErrNativeIntentConflict
		}
	}
	r.record, r.references, r.uncertain = stored, refs, nil
	return nil
}

func (o *nativeHTTPOperation) closeNativeIntent(ctx context.Context) error {
	r := o.intent
	if r == nil {
		return nil
	}
	if r.closed {
		return r.closeErr
	}
	r.closed = true
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), usageRecordPoolOptionsFromConfig(r.gateway.cfg).TaskTimeout)
	defer cancel()
	if err := r.reconcileNativeIntent(cleanup); err != nil {
		r.closeErr = err
		return err
	}
	snapshot := o.core.Snapshot()
	if r.record == nil || len(r.record.Attempts) == 0 {
		return nil
	}
	for _, a := range snapshot.Attempts {
		if a.Send != model.SendNotSent {
			return nil
		}
	}
	if snapshot.DelegatedCycles != 0 {
		return nil
	}
	ref := r.references[r.record.Attempts[0].Sequence]
	input := NativeIntentNotSentInput{Key: r.record.Key, IntentFingerprint: ref.IntentFingerprint, Operation: snapshot}
	_, r.closeErr = r.port.CloseNotSent(cleanup, input)
	return r.closeErr
}
