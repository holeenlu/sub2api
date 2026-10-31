package service

import (
	"context"
	"errors"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

type responseModeExecutionKey struct{}

// ResponseModeExecution integrates BPS/Cloud adapters with the original core.
// It owns no second budget, retry loop, output clock, price or billing root.
// Producers remain owned by the adapter, whose PreparedAttempt.Close must join
// them before Finish. No network work or goroutines start in its constructor.
type ResponseModeExecution struct {
	core   *execution.Operation
	budget *OpenAITurnBudget
	native *nativeHTTPOperation
	mode   string
	mu     sync.Mutex // registered preparations only; never held across I/O
	closed bool
	items  []*ResponseModeAttempt
}

// BindResponseModeExecution is a trusted composition call after mode selection.
// The required authority must check current key/user/group, grant, policy and
// actual owned capacity, plus the role/billing/Cloud owner binding in each seal.
// Calling the Native-only authority or passing nil is not a mode permission.
// Repeated consumers retrieve the existing binding using ResponseModeExecutionFromContext.
func BindResponseModeExecution(ctx context.Context, mode string, authority execution.DispatchAuthority) (context.Context, *ResponseModeExecution, error) {
	owner := nativeHTTPFromContext(ctx)
	if ctx == nil || owner == nil || owner.core == nil || owner.dispatch == nil || owner.dispatch.budget == nil || ResponseModeExecutionFromContext(ctx) != nil {
		return nil, nil, execution.ErrDispatchUnavailable
	}
	controller, err := NewResponseModeExecution(owner.core, owner.dispatch.budget, mode, authority)
	if err != nil {
		return nil, nil, err
	}
	controller.native = owner
	return context.WithValue(ctx, responseModeExecutionKey{}, controller), controller, nil
}

// NewResponseModeExecution also serves a trusted admin/background probe root.
// That host supplies its actual core and existing bounded turn budget, with an
// explicit probe principal/claim and probe binding. It must Begin the core once
// and validate its real admin/claim/resource authority, never fake a user key.
func NewResponseModeExecution(core *execution.Operation, budget *OpenAITurnBudget, mode string, authority execution.DispatchAuthority) (*ResponseModeExecution, error) {
	if core == nil || budget == nil || !core.UsesResources(budget.attemptBudget, budget.firstOutput) {
		return nil, execution.ErrDispatchUnavailable
	}
	if err := core.BindModeAuthority(mode, authority); err != nil {
		return nil, err
	}
	return &ResponseModeExecution{core: core, budget: budget, mode: mode}, nil
}

func ResponseModeExecutionFromContext(ctx context.Context) *ResponseModeExecution {
	if ctx == nil {
		return nil
	}
	controller, _ := ctx.Value(responseModeExecutionKey{}).(*ResponseModeExecution)
	return controller
}

// Register takes ownership of the actual sealed transport preparation. The
// provider's Open consumes the concrete permit at its final write boundary.
// Register is not permission and cannot replace token/owner revalidation.
func (c *ResponseModeExecution) Register(prepared execution.PreparedAttempt) (*ResponseModeAttempt, error) {
	if c == nil || nativeDispatchNil(prepared) {
		return nil, execution.ErrDispatchUnavailable
	}
	identity, target := prepared.Identity(), prepared.Target()
	input := c.core.Snapshot()
	purpose, valid := identity.Role.Purpose()
	if !identity.Valid() || identity.Mode != c.mode || !valid || purpose != identity.Purpose || !identity.Binding.Valid(c.mode) ||
		identity.Principal != input.Principal || identity.ProbeID != input.ProbeID || identity.ProbeClaimDigest != input.ProbeClaimDigest || identity.OperationID != input.OperationID || identity.UserID != input.UserID || identity.APIKeyID != input.APIKeyID || identity.GroupID != input.GroupID || identity.GroupBound != input.GroupBound || target.Owner != identity.Owner {
		return nil, errors.Join(execution.ErrInvalidPreparedIdentity, prepared.Close())
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.Join(execution.ErrOperationConsumed, prepared.Close())
	}
	a := &ResponseModeAttempt{controller: c, source: prepared, identity: identity, target: target}
	c.items = append(c.items, a)
	c.mu.Unlock()
	return a, nil
}

// ResponseModeAttempt is the handle to one registered send and its observations.
// It cannot reprepare or grant a retry. A new seal and a new Start on the SAME
// controller are required for each authorized generation/repair/compaction.
type ResponseModeAttempt struct {
	controller *ResponseModeExecution
	source     execution.PreparedAttempt
	identity   model.PreparedIdentity
	target     model.AttemptTarget
	mu         sync.Mutex
	started    bool
	sequence   uint64
	closeOnce  sync.Once
	closeErr   error
	closed     bool
}

func (a *ResponseModeAttempt) Start(ctx context.Context) error {
	if a == nil || ctx == nil {
		return execution.ErrDispatchUnavailable
	}
	a.mu.Lock()
	if a.started || a.closed {
		a.mu.Unlock()
		return execution.ErrOperationConsumed
	}
	a.started = true
	a.mu.Unlock()
	_, err := a.controller.core.OpenPreparedOnce(ctx, &responseModePrepared{a})
	return err
}

func (a *ResponseModeAttempt) Sequence() uint64 {
	if a == nil {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sequence
}

func (a *ResponseModeAttempt) Observe(facts model.FactDelta) error {
	if sequence := a.Sequence(); sequence != 0 {
		return a.controller.core.Observe(sequence, facts)
	}
	return execution.ErrOperationFacts
}

// Delivered consumes the real HTTP/WS writer receipt. Publishing to Redis does
// not prove client delivery: use only PossiblyCommitted when publication makes
// a business frame observable, then forward the real writer's receipt. Metadata
// alone is not a business fence. Any possibly delivered output blocks recovery.
func (a *ResponseModeAttempt) Delivered(receipt model.DeliveryReceipt) error {
	if sequence := a.Sequence(); sequence != 0 && receipt.BytesWritten >= 0 {
		a.controller.core.Delivered(sequence, receipt)
		return nil
	}
	return execution.ErrOperationFacts
}

func (a *ResponseModeAttempt) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()
		a.closeErr = a.source.Close()
		if sequence := a.Sequence(); sequence != 0 {
			a.controller.core.Closed(sequence, a.closeErr)
		}
	})
	return a.closeErr
}

type responseModePrepared struct{ attempt *ResponseModeAttempt }

func (p *responseModePrepared) Identity() model.PreparedIdentity { return p.attempt.identity }
func (p *responseModePrepared) Target() model.AttemptTarget      { return p.attempt.target }
func (p *responseModePrepared) Close() error                     { return p.attempt.Close() }
func (p *responseModePrepared) Reprepare(context.Context) (execution.PreparedAttempt, error) {
	return nil, execution.ErrDispatchUnavailable
}
func (p *responseModePrepared) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	a := p.attempt
	a.mu.Lock()
	a.sequence = permit.AttemptSequence()
	closed := a.closed
	a.mu.Unlock()
	if closed || a.source.Identity() != a.identity || a.source.Target() != a.target {
		a.controller.core.Closed(a.Sequence(), nil)
		return model.SendNotSent, execution.PreRequestFailure{}, execution.ErrInvalidPreparedIdentity
	}
	if a.controller.native != nil {
		// The existing nativeOperationWriter reports the real writer against
		// this core's current sequence. Main must keep mode amount settlement
		// under its existing owner rather than invoking native pricing here.
		a.controller.native.active.Store(a.Sequence())
	}
	return a.source.Open(ctx, permit)
}

func (c *ResponseModeExecution) RecordBilling(receipt model.ModeBillingReceipt) error {
	if c == nil {
		return execution.ErrOperationFacts
	}
	return c.core.RecordModeBilling(receipt)
}

func (c *ResponseModeExecution) Snapshot() model.OperationResult { return c.core.Snapshot() }

func (c *ResponseModeExecution) Cancel(cause error) {
	if c != nil && c.budget != nil {
		c.budget.mu.Lock()
		cancel := c.budget.controlCancel
		c.budget.mu.Unlock()
		if cancel != nil {
			cancel(cause)
		}
	}
}

// Close releases only preparations, never user/account leases or money. The
// original ingress/actor retains its existing single renewal/release owner.
func (c *ResponseModeExecution) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.closed = true
	items := append([]*ResponseModeAttempt(nil), c.items...)
	c.mu.Unlock()
	var err error
	for _, item := range items {
		err = errors.Join(err, item.Close())
	}
	return err
}

// Finish is for the actor that owns a resumed operation. For local BPS attached
// to RunNativeHTTPOperation, call Close and let that existing root Finish once.
func (c *ResponseModeExecution) Finish(ctx context.Context, cause error, suspended bool) error {
	if c == nil {
		return execution.ErrDispatchUnavailable
	}
	cause = errors.Join(cause, c.Close())
	if suspended && cause == nil {
		return c.core.Suspend(ctx)
	}
	return c.core.Finish(ctx, cause)
}

// ResponseModeHTTPAttempt reuses the existing immutable HTTP preparation and
// RoundTripOnce transport. It deliberately cannot call HTTPUpstream.Do (which
// would debit the legacy turn budget again or hide another send).
type ResponseModeHTTPAttempt struct {
	prepared *native.PreparedHTTP
	target   model.AttemptTarget
	result   *native.Result
}

func PrepareResponseModeHTTP(ctx context.Context, spec native.Spec, transport native.Transport, target model.AttemptTarget) (*ResponseModeHTTPAttempt, error) {
	p, err := native.PrepareHTTP(ctx, spec, transport)
	if err != nil {
		return nil, err
	}
	return &ResponseModeHTTPAttempt{prepared: p, target: target}, nil
}

func (p *ResponseModeHTTPAttempt) Identity() model.PreparedIdentity { return p.prepared.Identity() }
func (p *ResponseModeHTTPAttempt) Target() model.AttemptTarget      { return p.target }
func (p *ResponseModeHTTPAttempt) Close() error                     { return p.prepared.Close() }
func (p *ResponseModeHTTPAttempt) Result() *native.Result           { return p.result }
func (p *ResponseModeHTTPAttempt) Reprepare(context.Context) (execution.PreparedAttempt, error) {
	return nil, execution.ErrDispatchUnavailable
}
func (p *ResponseModeHTTPAttempt) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	result, err := native.Open(ctx, p.prepared, permit)
	p.result = result
	return result.SendState, result.PreRequest, err
}
