package service

import (
	"context"
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
)

// sendPrepared is the single production consumer used by both the initial
// request and the existing redirect controller. Execution now owns TLS retries.
func (o *nativeDispatchOperation) sendPrepared(ctx context.Context, prepared *native.PreparedHTTP, target model.AttemptTarget, retries *int, state *model.SendState) (*http.Response, error) {
	owner := nativeHTTPFromContext(ctx)
	if owner == nil || owner.dispatch != o {
		return nil, errors.Join(execution.ErrDispatchUnavailable, prepared.Close())
	}
	port := &nativeHTTPPreparedAttempt{owner: owner, dispatch: o, prepared: prepared, target: target, state: state, family: nativeUpstreamFamilyFromContext(ctx)}
	completed, _, err := owner.core.OpenPrepared(ctx, port, retries, func(waitCtx context.Context) error {
		return WaitOpenAITurnRetry(waitCtx, execution.PreRequestRetryDelay)
	})
	if err != nil {
		return nil, err
	}
	actual, ok := completed.(*nativeHTTPPreparedAttempt)
	if !ok || actual.response == nil {
		return nil, native.ErrMissingHTTPResponse
	}
	return actual.response, nil
}

type nativeHTTPPreparedAttempt struct {
	owner    *nativeHTTPOperation
	dispatch *nativeDispatchOperation
	prepared *native.PreparedHTTP
	target   model.AttemptTarget
	state    *model.SendState
	response *http.Response
	family   nativeUpstreamFamily
}

func (p *nativeHTTPPreparedAttempt) Identity() model.PreparedIdentity { return p.prepared.Identity() }
func (p *nativeHTTPPreparedAttempt) Target() model.AttemptTarget      { return p.target }
func (p *nativeHTTPPreparedAttempt) Close() error                     { return p.prepared.Close() }
func (p *nativeHTTPPreparedAttempt) Reprepare(ctx context.Context) (execution.PreparedAttempt, error) {
	next, err := native.ReprepareHTTP(ctx, p.prepared)
	if err != nil {
		return nil, err
	}
	return &nativeHTTPPreparedAttempt{owner: p.owner, dispatch: p.dispatch, prepared: next, target: p.target, state: p.state, family: p.family}, nil
}
func (p *nativeHTTPPreparedAttempt) Open(ctx context.Context, permit *execution.SendPermit) (model.SendState, execution.PreRequestFailure, error) {
	o := p.dispatch
	o.budget.mu.Lock()
	o.budget.healthMode = schedulingHealthMode(p.target.Mode)
	o.budget.healthCohort = schedulingHealthCohort(ctx, o.budget.healthMode)
	o.budget.healthAccountID = p.target.Owner.AccountID
	o.budget.mu.Unlock()
	p.owner.active.Store(permit.AttemptSequence())
	o.mu.Lock()
	facts := o.pricingFacts[p.Identity()]
	o.mu.Unlock()
	if err := p.owner.preserveNativeIntent(ctx, p.Identity(), permit.AttemptSequence(), facts); err != nil {
		closeErr := p.Close()
		p.owner.core.Closed(permit.AttemptSequence(), closeErr)
		return model.SendNotSent, execution.PreRequestFailure{}, nativeDispatchError("native intent preservation", errors.Join(err, closeErr))
	}
	result, err := native.Open(ctx, p.prepared, permit)
	*p.state = combineNativeSendState(*p.state, result.SendState)
	p.response = result.Response
	if err != nil {
		// Open owns close-on-failure. Its idempotent acknowledgement preserves
		// that actual cleanup fact even when there is no response body to wrap.
		p.owner.core.Closed(permit.AttemptSequence(), result.Close())
	}
	if err == nil && p.response != nil {
		sequence := permit.AttemptSequence()
		p.owner.core.RequestID(sequence, p.response.Header.Get("x-request-id"))
		if p.response.Body == nil {
			err = errors.Join(err, result.Close())
		} else {
			p.response.Body = nativeOperationResponseBody(ctx, p.owner, sequence, p.response, p.family)
		}
	}
	return result.SendState, result.PreRequest, err
}
