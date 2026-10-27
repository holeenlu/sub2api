package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

func responseModeFromAuthorization(mode string) ResponseMode {
	switch mode {
	case UsageAuthorizationModeBPS:
		return ResponseModeBPS
	case UsageAuthorizationModeCloud:
		return ResponseModeCloud
	default:
		return ResponseModeNative
	}
}

// responseExecutionPlan is a per-candidate, per-turn decision. It is not an
// authorization grant: the existing dispatch boundary still revalidates the
// account, credential and lease immediately before sending. Client transports
// consume this value; they do not independently choose an authorization mode.
type responseExecutionPlan struct {
	mode          ResponseMode
	upstreamModel string
}

// requestedModel has already passed ingress permissions and channel mapping.
// Account mapping is applied exactly once before matching the BPS selection.
func (s *OpenAIGatewayService) planResponseExecution(ctx context.Context, account *Account, requestedModel string) (responseExecutionPlan, error) {
	if account == nil {
		return responseExecutionPlan{}, errors.New("response account is missing")
	}
	return s.planResponseUpstreamExecution(ctx, account, account.GetMappedModel(requestedModel))
}

// The WS adapter receives an already mapped payload. This entry deliberately
// does not remap it, including when model mappings contain chained aliases.
func (s *OpenAIGatewayService) planResponseUpstreamExecution(ctx context.Context, account *Account, upstreamModel string) (responseExecutionPlan, error) {
	if account == nil {
		return responseExecutionPlan{}, errors.New("response account is missing")
	}
	plan := responseExecutionPlan{mode: ResponseModeNative, upstreamModel: upstreamModel}
	if account.IsOpenAICloudConfigured() {
		plan.mode = ResponseModeCloud
		return plan, nil
	}
	bps, err := s.excelBPSSelectedByPolicy(ctx, account.isExcelBPSUpstreamModelEnabled(upstreamModel))
	if err != nil {
		return responseExecutionPlan{}, err
	}
	if bps {
		plan.mode = ResponseModeBPS
		return plan, nil
	}
	return plan, nil
}

// Planning a future WS turn cannot pin the preceding operation's budget.
// Admit only after that turn owns its budget, and before removing/replaying a
// continuation reference. The budget survives account/transport retries.
func (s *OpenAIGatewayService) admitResponseExecutionPlan(ctx context.Context, groupID int64, previous string, plan responseExecutionPlan, localReplay ...bool) error {
	if err := s.checkResponseContinuationMode(ctx, groupID, previous, plan.mode, localReplay...); err != nil {
		return err
	}
	if excelBPSExecutionPinned(ctx) && plan.mode != ResponseModeBPS {
		return newExcelBPSRouteChangedError()
	}
	if plan.mode == ResponseModeBPS {
		pinExcelBPSExecution(ctx)
	}
	return nil
}

func (s *OpenAIGatewayService) bindResponseMode(ctx context.Context, groupID int64, id, mode string, ttl time.Duration) error {
	return s.getOpenAIWSStateStore().BindResponseMode(ctx, groupID, id, responseModeFromAuthorization(mode), ttl)
}

// Ownership is checked separately by ingress. A producer mode only constrains
// routing; it cannot authorize a response, an account or a credential.
func (s *OpenAIGatewayService) checkResponseContinuationMode(ctx context.Context, groupID int64, previous string, selected ResponseMode, localReplay ...bool) error {
	if strings.TrimSpace(previous) == "" {
		return nil
	}
	mode, err := s.getOpenAIWSStateStore().GetResponseMode(ctx, groupID, previous)
	// A locally reconstructed cross-stream history is known gateway output,
	// even after its shared owner/mode indexes expire. It must never take the
	// external-unknown-response exception or lose its producer-mode constraint.
	if err == nil && mode == ResponseModeUnknown && len(localReplay) > 0 && localReplay[0] {
		err = ErrResponseModeUnavailable
	}
	if err != nil || mode != ResponseModeUnknown && mode != selected && (mode == ResponseModeBPS || selected == ResponseModeBPS) {
		failure := &UpstreamFailoverError{
			Cause:      errors.Join(ErrOpenAIContinuationUnavailable, err),
			StatusCode: http.StatusBadRequest, ClientStatusCode: http.StatusBadRequest,
			Stage: "selection", Scope: GatewayFailureScopeRequest,
			Reason: "continuation_mode_unavailable", RequestNotSent: true,
			NextAccountAction: NextAccountStop,
			ClientMessage:     "The original response mode is no longer available for this continuation",
		}
		if err != nil && !errors.Is(err, ErrResponseModeUnavailable) {
			failure.Cause = err // dependency failure is not a missing response
			failure.StatusCode, failure.ClientStatusCode = http.StatusServiceUnavailable, http.StatusServiceUnavailable
			failure.Scope = GatewayFailureScopeProvider
			failure.Reason = "continuation_state_unavailable"
			failure.ClientMessage = "Response continuation state is temporarily unavailable"
		}
		return failure
	}
	if mode == ResponseModeBPS {
		pinExcelBPSExecution(ctx)
	}
	return nil
}
