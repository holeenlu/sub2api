package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// BindNativeWSOperation uses the existing core owner. HTTP-to-WS retains the
// original HTTP identity and wire digest; only a new authenticated client turn
// may create a core under its already-bound dispatcher.
func (s *OpenAIGatewayService) BindNativeWSOperation(ctx context.Context, payload []byte) (context.Context, error) {
	dispatch := nativeDispatchFromContext(ctx)
	if dispatch == nil {
		return nil, execution.ErrDispatchUnavailable
	}
	if owner := nativeHTTPFromContext(ctx); owner != nil {
		if owner.dispatch != dispatch {
			return nil, execution.ErrDispatchChanged
		}
		return ctx, nil
	}
	bound, err := s.BindNativeHTTPOperation(ctx, model.ProtocolResponses, sha256.Sum256(payload))
	if err != nil {
		return nil, err
	}
	// Only client-WS roots are push-driven. HTTP-to-WS already belongs to
	// Execute; neither transport adaptation nor a retry may Begin it again.
	if dispatch.request.Transport == "ws" {
		if err := nativeHTTPFromContext(bound).core.Begin(bound); err != nil {
			return nil, err
		}
	}
	return bound, nil
}

// nativeWSAttempt is a reader/writer observation adapter, not an execution or
// billing owner. It records facts on the same core used by HTTP attempts. Each
// exclusive upstream lease holds only its current attempt; no second read pump
// or process registry is introduced.
type nativeWSAttempt struct {
	owner    *nativeHTTPOperation
	sequence uint64
	mu       sync.Mutex
	decoder  nativeOperationDecoder
	closed   bool
}

func (a *nativeWSAttempt) observe(body []byte, readErr error) error {
	if a == nil {
		return readErr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return readErr
	}
	documents := [][]byte{body}
	if split, ok := splitOpenAIConcatenatedJSONDocuments(body); ok {
		documents = split
	}
	var factErr error
	for _, document := range documents {
		if len(document) == 0 {
			continue
		}
		factErr = errors.Join(factErr, a.owner.core.Observe(a.sequence, a.decoder.payload(document)))
	}
	// A canceled/timed-out read is not upstream terminal confirmation. EOF is
	// recorded independently and cannot promote partial/missing usage.
	if errors.Is(readErr, io.EOF) {
		factErr = errors.Join(factErr, a.owner.core.Observe(a.sequence, model.FactDelta{EndOfStream: true}))
	}
	if factErr == nil {
		return readErr
	}
	return errors.Join(readErr, factErr)
}

func (a *nativeWSAttempt) delivered(body []byte, err error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	// Conn.Write either completes one flushed frame or may have written a
	// prefix. Do not fabricate a byte count for failed writes. Local protocol
	// errors/control metadata do not commit a business response.
	business := nativeClientBytesCommit(append([]byte("data: "), body...), "text/event-stream")
	r := model.DeliveryReceipt{FlushAttempted: true, Flushed: err == nil, BusinessCommitted: business && err == nil, PossiblyCommitted: business && err != nil}
	if err == nil {
		r.BytesWritten = int64(len(body))
	}
	a.owner.core.Delivered(a.sequence, r)
}

func (a *nativeWSAttempt) close(err error) {
	if a != nil {
		a.mu.Lock()
		defer a.mu.Unlock()
		if !a.closed {
			a.closed = true
			a.owner.core.Closed(a.sequence, err)
		}
	}
}

type nativeWSPreparedAttempt struct {
	owner        *nativeHTTPOperation
	prepared     *native.PreparedWS
	connection   nativeWSOwnedConnection
	target       model.AttemptTarget
	state        model.SendState
	pricingFacts NativeIntentModelFacts
}

func (p *nativeWSPreparedAttempt) Identity() model.PreparedIdentity { return p.prepared.Identity() }
func (p *nativeWSPreparedAttempt) Target() model.AttemptTarget      { return p.target }
func (p *nativeWSPreparedAttempt) Close() error                     { return nil }
func (p *nativeWSPreparedAttempt) Reprepare(context.Context) (execution.PreparedAttempt, error) {
	// WS does not possess HTTP's TLS-before-request proof. Only its existing
	// root may select an explicitly proven unsent/rejected recovery.
	return nil, execution.ErrDispatchUnavailable
}
func (p *nativeWSPreparedAttempt) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	p.owner.active.Store(permit.AttemptSequence())
	if err := p.owner.preserveNativeIntent(ctx, p.Identity(), permit.AttemptSequence(), p.pricingFacts); err != nil {
		p.owner.core.Closed(permit.AttemptSequence(), nil)
		return model.SendNotSent, execution.PreRequestFailure{}, nativeDispatchError("native intent preservation", err)
	}
	a := &nativeWSAttempt{owner: p.owner, sequence: permit.AttemptSequence(), decoder: nativeOperationDecoder{sse: true}}
	p.connection.setNativeAttempt(a)
	op := p.owner.dispatch
	op.budget.mu.Lock()
	op.budget.healthMode = schedulingHealthMode(p.target.Mode)
	op.budget.healthCohort = schedulingHealthCohort(ctx, op.budget.healthMode)
	op.budget.healthAccountID = p.target.Owner.AccountID
	op.budget.mu.Unlock()
	result, err := native.OpenWS(ctx, p.prepared, permit)
	p.state = result.SendState
	if err != nil {
		a.close(err)
	}
	return result.SendState, execution.PreRequestFailure{}, err
}

func (l *openAIWSConnLease) setNativeAttempt(a *nativeWSAttempt) {
	if previous := l.nativeAttempt.Swap(a); previous != nil {
		previous.close(nil)
	}
}

func (l *openAIWSConnLease) finishNativeAttempt(err error) {
	if l != nil {
		l.nativeAttempt.Swap(nil).close(err)
	}
}

func nativeWSProvenUnsent(err error) bool {
	var attempt *OpenAITransportAttemptError
	return errors.As(err, &attempt) && attempt.SendState == model.SendNotSent && nativeWSFailureStage("write_upstream", err) != "before_upstream_write"
}

// FinalizeNativeWSUsage invokes the original durable finalizer synchronously.
// It does not Finish: an explicit pre-generation refusal may still return to
// the root's existing recovery controller within this same operation.
func (s *OpenAIGatewayService) FinalizeNativeWSUsage(ctx context.Context, input *OpenAIRecordUsageInput, cause error) (bool, error) {
	if s == nil {
		return finalizeNativeWSUsage(ctx, input, cause, nil, 0)
	}
	return finalizeNativeWSUsage(ctx, input, cause, s, usageRecordPoolOptionsFromConfig(s.cfg).TaskTimeout)
}

func finalizeNativeWSUsage(ctx context.Context, input *OpenAIRecordUsageInput, cause error, finalizer NativeOperationBilling, timeout time.Duration) (bool, error) {
	op := nativeHTTPFromContext(ctx)
	if op == nil || op.dispatch.request.Transport != "ws" {
		return false, nil
	}
	op.cycleErr = cause
	return finalizeNativeHTTPUsage(ctx, input, finalizer, timeout)
}

// FinishNativeWSOperation is called only after joined transport work and the
// synchronous usage callback. The HTTP Execute owner is deliberately untouched.
// It also closes an accepted but unsent turn on an ingress early return.
func FinishNativeWSOperation(ctx context.Context, cause error) error {
	op := nativeHTTPFromContext(ctx)
	if op == nil || op.dispatch.request.Transport != "ws" {
		return cause
	}
	cause = errors.Join(cause, op.finalizeErr, responseCloudCompletionError(ctx))
	control := op.dispatch.budget.control
	if control != nil && control.Err() != nil {
		ctx = control
	}
	// An imported terminal snapshot may have no Go error (Redis excludes it).
	// Preserve the actual public forward error while Finish keeps that first
	// authoritative terminal state and its already recorded billing receipt.
	return errors.Join(cause, op.core.Finish(ctx, cause), op.closeNativeIntent(ctx))
}

// prepareNativeWSAttempt uses the existing operation's finalizer fields. A
// completed durable attempt may recover on the same root; an unconfirmed
// finalization may not dispatch again. No additional receipt owner is created.
func prepareNativeWSAttempt(ctx context.Context) error {
	op := nativeHTTPFromContext(ctx)
	if op == nil || op.dispatch.request.Transport != "ws" {
		return nil
	}
	if op.finalizeErr != nil {
		return op.finalizeErr
	}
	switch op.core.Snapshot().State {
	case model.OperationCompleted, model.OperationFailed, model.OperationCanceled, model.OperationSuspended:
		return execution.ErrOperationConsumed
	}
	op.finalized = false
	op.finalizedCloud = false
	op.cycleErr = nil
	op.resetForwardMode()
	op.modeMu.Lock()
	op.cycleMode = UsageAuthorizationModeNative
	op.modeMu.Unlock()
	return nil
}

func nativeWSRootMayRecover(cause error) bool {
	var failure *UpstreamFailoverError
	return errors.As(cause, &failure) && (failure.RetryableOnSameAccount || failure.ShouldRetryNextAccount())
}

// completeOpenAIWSTurn is the real common WS callback boundary. The release
// callback is deliberately separate from billing so it runs after core.Finish.
// Recoverable account attempts remain with their original root; only that root
// can decide to end the logical turn once recovery is exhausted.
func completeOpenAIWSTurn(ctx context.Context, hooks *OpenAIWSIngressHooks, turn int, result *OpenAIForwardResult, cause error) error {
	if hooks != nil && hooks.AfterTurn != nil {
		hooks.AfterTurn(turn, result, cause)
	}
	op := nativeHTTPFromContext(ctx)
	finalErr := cause
	if op != nil && op.dispatch.request.Transport == "ws" {
		cloudErr := responseCloudCompletionError(ctx)
		if cloudErr != nil {
			op.finalizeErr = errors.Join(op.finalizeErr, cloudErr)
		}
		if NativeHTTPFinalizesUsage(ctx) && !op.finalized {
			op.finalizeErr = ErrNativeOperationBillingUnavailable
		}
		finalErr = errors.Join(cause, op.finalizeErr)
		if op.finalizeErr != nil || !nativeWSRootMayRecover(cause) {
			finalErr = FinishNativeWSOperation(ctx, finalErr)
		}
		if op.finalizeErr != nil {
			// Keep both errors on core.Failure, but never return a nested
			// failover instruction after an unconfirmed journal write.
			finalErr = op.finalizeErr
		}
	}
	if hooks != nil && hooks.ReleaseTurn != nil {
		hooks.ReleaseTurn(turn)
	}
	return finalErr
}
