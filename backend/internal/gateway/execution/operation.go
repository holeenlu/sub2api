package execution

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

var (
	ErrOperationConsumed  = errors.New("operation already executed")
	ErrOperationFacts     = errors.New("inconsistent operation facts")
	ErrOperationCommitted = errors.New("operation cannot replay committed output")
)

// CycleResult is one application selection/acquisition/forward/finalization
// step. Recovery policy is the existing Recovery object; only this owner loops.
type CycleResult struct {
	Retry     bool
	Delegated bool
	Err       error
}

// ProtocolAttemptResult reports the existing provider-specific recovery
// decision. It is not permission to send: the next attempt must prepare a new
// seal and pass this operation's unchanged Dispatcher and AttemptBudget.
type ProtocolAttemptResult struct {
	ReprepareReason string
	Err             error
}

// RunProtocolAttempts owns explicit wire/credential repair transitions. The
// callback performs one attempt only and closes its stream before proposing
// recovery. Provider-specific recovery eligibility remains with that callback.
func (o *Operation) RunProtocolAttempts(ctx context.Context, attempt func() ProtocolAttemptResult) error {
	for {
		if err := o.dispatcher.check(ctx); err != nil {
			return err
		}
		step := attempt()
		if step.ReprepareReason == "" {
			return step.Err
		}
		if err := o.AllowProtocolRecovery(ctx); err != nil {
			return errors.Join(step.Err, err)
		}
		o.mu.Lock()
		o.result.State = model.OperationRetryPlanning
		o.mu.Unlock()
	}
}

// Operation shares the existing dispatcher, AttemptBudget and FirstOutput. It
// never constructs another budget, changes a seal, or calculates money.
type Operation struct {
	mu         sync.Mutex
	dispatcher *Dispatcher
	result     model.OperationResult
	running    bool
	finished   bool
	inFlight   int
	transfer   *OperationTransfer
	received   *OperationTransfer
	returning  bool
	cycleStart int
}

func NewOperation(dispatcher *Dispatcher, input model.OperationResult) (*Operation, error) {
	if dispatcher == nil || input.OperationID == "" || dispatcher.operationID != input.OperationID {
		return nil, ErrDispatchUnavailable
	}
	input.State = model.OperationAccepted
	input.Attempts = nil
	return &Operation{dispatcher: dispatcher, result: input}, nil
}

// UsesResources checks composition identity without exposing mutable owners.
func (o *Operation) UsesResources(budget *AttemptBudget, output *FirstOutput) bool {
	return o != nil && o.dispatcher.budget == budget && o.dispatcher.output == output
}

// Begin joins push-driven transports to this same operation. It acquires no
// permission or budget; every actual send still goes through OpenPrepared.
func (o *Operation) Begin(ctx context.Context) error {
	if o == nil || ctx == nil {
		return ErrDispatchUnavailable
	}
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return ErrOperationConsumed
	}
	o.running = true
	o.cycleStart = len(o.result.Attempts)
	o.result.State = model.OperationWaiting
	o.mu.Unlock()
	if err := ctx.Err(); err != nil {
		o.finish(model.OperationCanceled, context.Cause(ctx))
		return context.Cause(ctx)
	}
	if err := o.dispatcher.control.Err(); err != nil {
		o.finish(model.OperationCanceled, context.Cause(o.dispatcher.control))
		return context.Cause(o.dispatcher.control)
	}
	return nil
}

// Finish records completion after the transport owner has joined its producers
// and handled actual billing evidence. It cannot replace either responsibility.
// Repeated completion returns the first result without changing its facts.
func (o *Operation) Finish(ctx context.Context, cause error) error {
	if o == nil {
		return ErrDispatchUnavailable
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.running {
		return ErrOperationConsumed
	}
	if o.finished {
		return o.result.Failure
	}
	if o.transfer != nil {
		return ErrOperationTransferred
	}
	state := model.OperationCompleted
	if cause != nil {
		state = model.OperationFailed
	}
	if ctx == nil {
		cause = errors.Join(cause, ErrDispatchUnavailable)
		state = model.OperationFailed
	} else if ctx.Err() != nil {
		cause = errors.Join(cause, context.Cause(ctx))
		state = model.OperationCanceled
	} else if o.dispatcher.control.Err() != nil {
		cause = errors.Join(cause, context.Cause(o.dispatcher.control))
		state = model.OperationCanceled
	}
	o.dispatcher.stop()
	o.result.State, o.result.Failure, o.finished = state, cause, true
	return cause
}

func (o *Operation) Execute(ctx context.Context, cycle func() CycleResult) error {
	if err := o.Begin(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			o.finish(model.OperationCanceled, context.Cause(ctx))
			return context.Cause(ctx)
		}
		o.mu.Lock()
		o.cycleStart = len(o.result.Attempts)
		o.result.State = model.OperationWaiting
		o.mu.Unlock()
		step := cycle()
		o.mu.Lock()
		native := len(o.result.Attempts) > o.cycleStart
		if step.Delegated {
			o.result.DelegatedCycles++
		}
		o.mu.Unlock()
		if !step.Retry {
			state := model.OperationCompleted
			if step.Err != nil {
				state = model.OperationFailed
			}
			if ctx.Err() != nil {
				state = model.OperationCanceled
				step.Err = errors.Join(step.Err, context.Cause(ctx))
			}
			o.finish(state, step.Err)
			return step.Err
		}
		if native && o.Committed() {
			o.finish(model.OperationFailed, ErrOperationCommitted)
			return ErrOperationCommitted
		}
		if err := ctx.Err(); err != nil {
			o.finish(model.OperationCanceled, context.Cause(ctx))
			return context.Cause(ctx)
		}
		o.mu.Lock()
		o.result.State = model.OperationRetryPlanning
		o.mu.Unlock()
	}
}

func (o *Operation) finish(state model.OperationState, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.finished {
		return
	}
	if o.transfer != nil {
		// An enqueue/HTTP return is not completion by the remote owner.
		o.result.Failure = errors.Join(err, ErrOperationTransferred)
		return
	}
	o.dispatcher.stop()
	o.result.State = state
	o.result.Failure = err
	o.finished = true
}
func (o *Operation) Committed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.result.Delivery.BusinessCommitted || o.result.Delivery.PossiblyCommitted || o.dispatcher.output.Snapshot().Committed
}

// PreparedAttempt uses the real provider seal and concrete execution permit.
// It carries no authorization callback and cannot mint or reset a budget.
type PreparedAttempt interface {
	Identity() model.PreparedIdentity
	Target() model.AttemptTarget
	Open(context.Context, *SendPermit) (model.SendState, PreRequestFailure, error)
	Reprepare(context.Context) (PreparedAttempt, error)
	Close() error
}

// OpenPrepared owns the pre-request retry loop. The caller supplies the
// existing wait-budget port and the retry count shared with redirect hops.
func (o *Operation) OpenPrepared(ctx context.Context, prepared PreparedAttempt, retries *int, wait func(context.Context) error) (PreparedAttempt, uint64, error) {
	o.mu.Lock()
	if o.transfer != nil {
		o.mu.Unlock()
		return prepared, 0, errors.Join(ErrOperationTransferred, prepared.Close())
	}
	o.inFlight++
	o.mu.Unlock()
	defer func() { o.mu.Lock(); o.inFlight--; o.mu.Unlock() }()
	for {
		identity, target := prepared.Identity(), prepared.Target()
		o.mu.Lock()
		if o.finished {
			o.mu.Unlock()
			return prepared, 0, errors.Join(ErrOperationConsumed, prepared.Close())
		}
		o.result.State = model.OperationPreparing
		o.mu.Unlock()
		permit, err := o.dispatcher.Admit(ctx, identity, target)
		if err != nil {
			return prepared, 0, errors.Join(err, prepared.Close())
		}
		sequence := permit.AttemptSequence()
		o.mu.Lock()
		if o.finished {
			o.mu.Unlock()
			return prepared, 0, errors.Join(ErrOperationConsumed, prepared.Close())
		}
		o.result.State = model.OperationDispatching
		o.result.Attempts = append(o.result.Attempts, model.AttemptFact{Sequence: sequence, Identity: identity, Send: model.SendUnknown})
		o.mu.Unlock()
		send, proof, err := prepared.Open(ctx, permit)
		if identity.Role != "" && send != model.SendNotSent && !permit.state.validated.Load() {
			// A mode adapter cannot replace consuming the concrete permission
			// with its own successful callback. Preserve unknown send evidence.
			send, err = model.SendUnknown, errors.Join(err, ErrDispatchUnavailable)
		}
		o.mu.Lock()
		a := o.attempt(sequence)
		if a != nil {
			a.Send = send
			a.Failure = err
		}
		if send != model.SendNotSent && identity.Purpose == model.DispatchGeneration {
			// A new possible generation is not covered by a previous attempt's
			// receipt until the real journal acknowledges the new observations.
			o.result.Billing = model.BillingReceipt{Disposition: model.BillingPendingUsage}
		}
		if err == nil && !o.finished {
			o.result.State = model.OperationReceiving
		}
		o.mu.Unlock()
		if err == nil {
			return prepared, sequence, nil
		}
		if !o.RetryBeforeSend(ctx, proof, *retries) {
			return prepared, sequence, err
		}
		*retries++
		if waitErr := wait(ctx); waitErr != nil {
			return prepared, sequence, errors.Join(err, waitErr)
		}
		prepared, err = prepared.Reprepare(ctx)
		if err != nil {
			return prepared, sequence, err
		}
	}
}

func (o *Operation) RetryBeforeSend(ctx context.Context, f PreRequestFailure, count int) bool {
	return !o.Committed() && PreRequestRetryAllowed(ctx, f, count)
}
func (o *Operation) AllowProtocolRecovery(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}
	if err := o.dispatcher.control.Err(); err != nil {
		return context.Cause(o.dispatcher.control)
	}
	if o.Committed() {
		return ErrOperationCommitted
	}
	return nil
}
func (o *Operation) attempt(sequence uint64) *model.AttemptFact {
	for i := len(o.result.Attempts) - 1; i >= 0; i-- {
		if o.result.Attempts[i].Sequence == sequence {
			return &o.result.Attempts[i]
		}
	}
	return nil
}

func (o *Operation) RequestID(sequence uint64, id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if a := o.attempt(sequence); a != nil {
		a.RequestID = id
	}
}
func (o *Operation) Observe(sequence uint64, f model.FactDelta) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	a := o.attempt(sequence)
	if a == nil {
		return ErrOperationFacts
	}
	if o.transfer != nil {
		return ErrOperationTransferred
	}
	if f.ProviderTurnID != "" && a.ProviderTurnID != "" && a.ProviderTurnID != f.ProviderTurnID {
		return ErrOperationFacts
	}
	if f.Generation > model.GenerationStarted || (f.Generation != model.GenerationUnknown && a.Generation != model.GenerationUnknown && a.Generation != f.Generation) || (f.Generation == model.GenerationRejected && f.RejectionEvidenceID == "") || (f.RejectionEvidenceID != "" && f.Generation != model.GenerationRejected) {
		return ErrOperationFacts
	}
	if a.RejectionEvidenceID != "" && f.RejectionEvidenceID != "" && a.RejectionEvidenceID != f.RejectionEvidenceID {
		return ErrOperationFacts
	}
	if f.ResponseID != "" {
		if a.ResponseID != "" && a.ResponseID != f.ResponseID {
			return ErrOperationFacts
		}
		a.ResponseID = f.ResponseID
	}
	if f.ReturnedModel != "" {
		a.ReturnedModel = f.ReturnedModel
	}
	if f.Terminal != model.TerminalUnknown {
		if a.Terminal != model.TerminalUnknown && a.Terminal != f.Terminal {
			return ErrOperationFacts
		}
		a.Terminal = f.Terminal
	}
	if f.Usage != nil {
		u := f.Usage
		if u.Confidence == model.UsageUnknown || u.Confidence > model.UsageFinal || (u.Input == nil && u.Output == nil && u.CacheRead == nil && u.CacheWrite == nil && u.ImageInput == nil && u.ImageOutput == nil && u.HostedImageInput == nil && u.HostedImageOutput == nil && u.ImageCount == nil) {
			return ErrOperationFacts
		}
		if u.Confidence == model.UsageConfirmedLowerBound && !u.TerminalConfirmed {
			return ErrOperationFacts
		}
		if u.Confidence == model.UsageFinal && !u.TerminalConfirmed {
			return ErrOperationFacts
		}
		for _, v := range []*model.TokenMeasurement{u.Input, u.Output, u.CacheRead, u.CacheWrite, u.ImageInput, u.ImageOutput, u.HostedImageInput, u.HostedImageOutput, u.Total, u.Reasoning} {
			if v != nil && v.Value < 0 {
				return ErrOperationFacts
			}
		}
		if u.ImageCount != nil && *u.ImageCount < 0 {
			return ErrOperationFacts
		}
		if a.Usage == nil || u.Confidence >= a.Usage.Confidence {
			a.Usage = model.CloneUsageFact(u)
		}
	}
	if f.ProviderTurnID != "" {
		a.ProviderTurnID = f.ProviderTurnID
	}
	if f.Generation != model.GenerationUnknown {
		a.Generation, a.RejectionEvidenceID = f.Generation, f.RejectionEvidenceID
	}
	if a.Identity.Role != "" {
		o.result.Billing = modeBillingSummary(o.result.Attempts)
	}
	a.EndOfStream = a.EndOfStream || f.EndOfStream
	return nil
}

func mergeReceipt(dst *model.DeliveryReceipt, r model.DeliveryReceipt) {
	dst.BytesWritten += r.BytesWritten
	dst.HeadersCommitted = dst.HeadersCommitted || r.HeadersCommitted
	dst.BusinessCommitted = dst.BusinessCommitted || r.BusinessCommitted
	dst.PossiblyCommitted = dst.PossiblyCommitted || r.PossiblyCommitted
	dst.FlushAttempted = dst.FlushAttempted || r.FlushAttempted
	dst.Flushed = dst.Flushed || r.Flushed
	dst.Closed = dst.Closed || r.Closed
}
func (o *Operation) Delivered(sequence uint64, r model.DeliveryReceipt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if a := o.attempt(sequence); a != nil {
		mergeReceipt(&a.Delivery, r)
		mergeReceipt(&o.result.Delivery, r)
		if r.BusinessCommitted || r.PossiblyCommitted {
			o.dispatcher.output.Commit()
		}
	}
}
func (o *Operation) Closed(sequence uint64, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if a := o.attempt(sequence); a != nil {
		a.Closed = true
		a.Failure = errors.Join(a.Failure, err)
	}
}
func (o *Operation) Billed(r model.BillingReceipt) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.result.Billing = r
}
func (o *Operation) CycleResult() model.OperationResult {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshot(o.cycleStart)
}
func (o *Operation) Snapshot() model.OperationResult {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.snapshot(0)
}
func (o *Operation) snapshot(start int) model.OperationResult {
	r := o.result
	r.Attempts = append([]model.AttemptFact(nil), o.result.Attempts[start:]...)
	for i := range r.Attempts {
		r.Attempts[i].Usage = model.CloneUsageFact(r.Attempts[i].Usage)
	}
	return r
}

// AttemptStream is the actual upstream reader used by the protocol consumers.
// No second reader is introduced, and raw bytes are preserved across Next.
type AttemptStream interface {
	Next(context.Context) (model.WireFrame, model.FactDelta, error)
	Close() error
}
type observedStream struct {
	owner    *Operation
	sequence uint64
	source   AttemptStream
}

func (o *Operation) ObserveStream(sequence uint64, source AttemptStream) AttemptStream {
	return &observedStream{o, sequence, source}
}
func (s *observedStream) Next(ctx context.Context) (model.WireFrame, model.FactDelta, error) {
	frame, facts, err := s.source.Next(ctx)
	if errors.Is(err, io.EOF) {
		facts.EndOfStream = true
	}
	factErr := s.owner.Observe(s.sequence, facts)
	if factErr == nil {
		// io.ReadAll compares EOF directly; observation must preserve it.
		return frame, facts, err
	}
	return frame, facts, errors.Join(err, factErr)
}
func (s *observedStream) Close() error {
	err := s.source.Close()
	s.owner.Closed(s.sequence, err)
	return err
}
