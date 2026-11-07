package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
)

func responseModeFromAuthorization(mode string) session.ResponseMode {
	switch mode {
	case UsageAuthorizationModeBPS:
		return session.ResponseModeBPS
	case UsageAuthorizationModeCloud:
		return session.ResponseModeCloud
	default:
		return session.ResponseModeNative
	}
}

// responseExecutionPlan is a per-candidate, per-turn decision. It is not an
// authorization grant: the existing dispatch boundary still revalidates the
// account, credential and lease immediately before sending. Client transports
// consume this value; they do not independently choose an authorization mode.
type responseExecutionPlan struct {
	accountID     int64
	mode          session.ResponseMode
	upstreamModel string
	native        OpenAIWSProtocolDecision
}

// requestedModel has already passed ingress permissions and channel mapping.
// Account mapping is applied exactly once before matching the BPS selection.
func (s *OpenAIGatewayService) planResponseExecution(ctx context.Context, account *Account, requestedModel string) (responseExecutionPlan, error) {
	if account == nil {
		return responseExecutionPlan{}, errors.New("response account is missing")
	}
	if err := s.settingService.checkManagedAccountModels(ctx, account, requestedModel, account.GetMappedModel(requestedModel)); err != nil {
		return responseExecutionPlan{}, err
	}
	return s.planResponseUpstreamRoute(ctx, account, account.GetMappedModel(requestedModel))
}

// The WS adapter receives an already mapped payload. This entry deliberately
// does not remap it, including when model mappings contain chained aliases.
func (s *OpenAIGatewayService) planResponseUpstreamExecution(ctx context.Context, account *Account, upstreamModel string) (responseExecutionPlan, error) {
	if account == nil {
		return responseExecutionPlan{}, errors.New("response account is missing")
	}
	if err := s.settingService.CheckManagedModel(ctx, account.Platform, upstreamModel); err != nil {
		return responseExecutionPlan{}, err
	}
	return s.planResponseUpstreamRoute(ctx, account, upstreamModel)
}

// Both callers have checked every applicable model name in their stage snapshot.
func (s *OpenAIGatewayService) planResponseUpstreamRoute(ctx context.Context, account *Account, upstreamModel string) (responseExecutionPlan, error) {
	plan := responseExecutionPlan{accountID: account.ID, mode: session.ResponseModeNative, upstreamModel: upstreamModel}
	if account.IsOpenAICloudConfigured() {
		plan.mode = session.ResponseModeCloud
		return plan, nil
	}
	bps, err := s.excelBPSSelectedByPolicy(ctx, account.isExcelBPSUpstreamModelEnabled(upstreamModel))
	if err != nil {
		return responseExecutionPlan{}, err
	}
	if bps {
		plan.mode = session.ResponseModeBPS
		return plan, nil
	}
	plan.native = s.getOpenAIWSProtocolResolver().Resolve(account)
	return plan, nil
}

// Planning a future WS turn cannot pin the preceding operation's budget.
// Admit only after that turn owns its budget, and before removing/replaying a
// continuation reference. The budget survives account/transport retries.
func (s *OpenAIGatewayService) admitResponseExecutionPlan(ctx context.Context, groupID int64, previous string, plan responseExecutionPlan, localReplay ...bool) error {
	if err := s.checkResponseContinuationMode(ctx, groupID, previous, plan.mode, localReplay...); err != nil {
		return err
	}
	if excelBPSExecutionPinned(ctx) && plan.mode != session.ResponseModeBPS {
		return newExcelBPSRouteChangedError()
	}
	if operation := nativeDispatchFromContext(ctx); operation != nil {
		if err := operation.admitResponsePlan(plan); err != nil {
			return err
		}
	}
	if plan.mode == session.ResponseModeBPS {
		pinExcelBPSExecution(ctx)
	}
	return nil
}

// Admit freezes one candidate's routing facts on the actual operation, after
// continuation validation. Speculative parsing of a future WS frame must not
// publish a plan here. Distinct candidates may map differently; re-entering the
// same candidate in this operation cannot silently change mode/model/transport.
func (o *nativeDispatchOperation) admitResponsePlan(plan responseExecutionPlan) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if previous, exists := o.responsePlans[plan.accountID]; exists && previous != plan {
		return &UpstreamFailoverError{
			Cause: ErrNativeDispatchScopeChanged, StatusCode: http.StatusServiceUnavailable,
			ClientStatusCode: http.StatusServiceUnavailable, Stage: "selection",
			Scope: GatewayFailureScopeRequest, Reason: "response_execution_plan_changed",
			RequestNotSent: true, NextAccountAction: NextAccountStop,
			ClientMessage: "The selected response execution plan changed during this request",
		}
	}
	if o.responsePlans == nil {
		o.responsePlans = make(map[int64]responseExecutionPlan)
	}
	o.responsePlans[plan.accountID] = plan
	return nil
}

func (s *OpenAIGatewayService) bindResponseMode(ctx context.Context, groupID int64, id, mode string, ttl time.Duration) error {
	return s.getOpenAIWSStateStore().BindResponseMode(ctx, groupID, id, responseModeFromAuthorization(mode), ttl)
}

// Ownership is checked separately by ingress. A producer mode only constrains
// routing; it cannot authorize a response, an account or a credential.
func (s *OpenAIGatewayService) checkResponseContinuationMode(ctx context.Context, groupID int64, previous string, selected session.ResponseMode, localReplay ...bool) error {
	if strings.TrimSpace(previous) == "" {
		return nil
	}
	mode, err := s.getOpenAIWSStateStore().GetResponseMode(ctx, groupID, previous)
	// A locally reconstructed cross-stream history is known gateway output,
	// even after its shared owner/mode indexes expire. It must never take the
	// external-unknown-response exception or lose its producer-mode constraint.
	if err == nil && mode == session.ResponseModeUnknown && len(localReplay) > 0 && localReplay[0] {
		err = session.ErrResponseModeUnavailable
	}
	if err != nil || mode != session.ResponseModeUnknown && mode != selected {
		failure := &UpstreamFailoverError{
			Cause:      errors.Join(ErrOpenAIContinuationUnavailable, err),
			StatusCode: http.StatusBadRequest, ClientStatusCode: http.StatusBadRequest,
			Stage: "selection", Scope: GatewayFailureScopeRequest,
			Reason: "continuation_mode_unavailable", RequestNotSent: true,
			NextAccountAction: NextAccountStop,
			ClientMessage:     "The original response mode is no longer available for this continuation",
		}
		if err != nil && !errors.Is(err, session.ErrResponseModeUnavailable) {
			failure.Cause = err // dependency failure is not a missing response
			failure.StatusCode, failure.ClientStatusCode = http.StatusServiceUnavailable, http.StatusServiceUnavailable
			failure.Scope = GatewayFailureScopeProvider
			failure.Reason = "continuation_state_unavailable"
			failure.ClientMessage = "Response continuation state is temporarily unavailable"
		}
		return failure
	}
	if mode == session.ResponseModeBPS {
		pinExcelBPSExecution(ctx)
	}
	return nil
}
