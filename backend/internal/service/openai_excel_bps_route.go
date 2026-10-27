package service

import (
	"context"
	"net/http"
	"strings"
)

type excelBPSUpstreamModelKey struct{}

func excelBPSResponsesCapability(capability OpenAIEndpointCapability) bool {
	switch capability {
	case "", OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityResponsesCompatible, OpenAIEndpointCapabilityChatCompletions:
		return true
	default:
		return false
	}
}

// The mapper supplies this before attachment preparation. Upload payloads do
// not contain a model and must still obey the producing request's model scope.
func withExcelBPSUpstreamModel(ctx context.Context, upstreamModel string) context.Context {
	return context.WithValue(ctx, excelBPSUpstreamModelKey{}, strings.TrimSpace(upstreamModel))
}

// The turn budget already survives account and transport retries. Pinning its
// mode must not create a second operation or grant another attempt allowance.
func pinExcelBPSExecution(ctx context.Context) {
	if budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); budget != nil {
		budget.mu.Lock()
		budget.bpsSelected = true
		budget.mu.Unlock()
	}
}

func excelBPSExecutionPinned(ctx context.Context) bool {
	budget, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	if budget == nil {
		return false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.bpsSelected
}

func newExcelBPSRouteChangedError() *UpstreamFailoverError {
	return &UpstreamFailoverError{
		StatusCode: http.StatusServiceUnavailable, Stage: "selection",
		Scope: GatewayFailureScopeAccount, Reason: "basispoints_route_unavailable",
		RequestNotSent: true, NextAccountAction: NextAccountRetry,
		ClientStatusCode: http.StatusServiceUnavailable,
		ClientMessage:    "The selected BPS route is no longer available",
	}
}

// requestedModel is the channel-mapped model, before account mapping. WS calls
// the upstream-model check after its ingress has already applied that mapping.
func (s *OpenAIGatewayService) checkExcelBPSExecutionRoute(ctx context.Context, account *Account, requestedModel string) error {
	_, err := s.checkExcelBPSExecutionSelection(ctx, account != nil && account.IsExcelBPSEnabledForModel(requestedModel))
	return err
}

func (s *OpenAIGatewayService) checkExcelBPSExecutionSelection(ctx context.Context, selected bool) (bool, error) {
	selected, err := s.excelBPSSelectedByPolicy(ctx, selected)
	if err != nil {
		return false, err
	}
	if excelBPSExecutionPinned(ctx) && !selected {
		return false, newExcelBPSRouteChangedError()
	}
	if selected {
		pinExcelBPSExecution(ctx)
	}
	return selected, nil
}

// Parsing a subsequent WS frame must not mutate the preceding turn's budget.
// The new turn consumes this same policy decision through its execution guard.
func (s *OpenAIGatewayService) excelBPSSelectedByPolicy(ctx context.Context, selected bool) (bool, error) {
	if selected {
		var err error
		selected, err = s.settingService.excelBPSEnabled(ctx)
		if err != nil {
			return false, &UpstreamFailoverError{
				Cause: err, StatusCode: http.StatusServiceUnavailable,
				ClientStatusCode: http.StatusServiceUnavailable,
				Stage:            "selection", Scope: GatewayFailureScopeProvider,
				Reason: "basispoints_policy_unavailable", RequestNotSent: true,
				NextAccountAction: NextAccountStop,
				ClientMessage:     "BPS routing policy is temporarily unavailable",
			}
		}
	}
	return selected, nil
}
