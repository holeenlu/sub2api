package service

import (
	"context"
	"net/http"
	"time"
)

// Consume the original HTTP/terminal envelope exactly once, before bridge
// normalization drops scope/reset evidence. Contradictory scope means no write.
func (s *OpenAIGatewayService) observeExcelBPSRateLimit(ctx context.Context, account *Account, status int, headers http.Header, raw []byte, model, endpoint string) error {
	// A trusted content-policy code keeps its security semantics even when
	// wrapped in HTTP/terminal 429. Observe the original envelope before any
	// cooldown mutation; later stream classification cannot undo that write.
	if cyber, _, _ := detectOpenAICyberPolicy(raw); cyber {
		return nil
	}
	fact, ok := parseExcelBPSRateLimitFailure(status, headers, raw, model, endpoint, time.Now())
	if !ok {
		return nil
	}
	_, err := s.coolDownExcelBPSScope(ctx, account, fact.scope, fact.retryAfter, fact.resetAt)
	return err
}

func newExcelBPSStateUnavailable(err error, notSent bool) *UpstreamFailoverError {
	return &UpstreamFailoverError{
		Cause: err, StatusCode: http.StatusServiceUnavailable, ClientStatusCode: http.StatusServiceUnavailable,
		Stage: "selection", Scope: GatewayFailureScopeProvider, Reason: "basispoints_state_unavailable",
		RequestNotSent: notSent, NextAccountAction: NextAccountStop,
		ClientMessage: "BPS state is temporarily unavailable",
	}
}
