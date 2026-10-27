package service

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

func decayedSchedulingError(rate float64, age time.Duration) float64 {
	return clamp01(rate * math.Exp2(-math.Max(0, age.Seconds())/(2*time.Minute).Seconds()))
}

func schedulingHealthMode(mode string) string {
	switch mode {
	case UsageAuthorizationModeCloud:
		return UsageAuthorizationModeCloud
	case "bps":
		return "bps"
	case "websocket":
		return "native_ws"
	case "":
		return "unknown"
	default:
		return "native_http"
	}
}

// The input byte band is an inexpensive, finite proxy for request size, not a
// token or billing estimate. Missing input/mode evidence stays neutral.
func schedulingHealthCohort(ctx context.Context, mode string) string {
	risk, _ := ctx.Value(schedulingQuotaRequestKey{}).(*schedulingQuotaRequest)
	if risk == nil {
		return ""
	}
	risk.healthOnce.Do(func() {
		size := "small"
		if len(risk.body) > 400000 {
			size = "large"
		} else if len(risk.body) > 64000 {
			size = "medium"
		}
		if gjson.GetBytes(risk.body, "previous_response_id").String() != "" {
			size = "continuation"
		}
		effort := strings.ToLower(gjson.GetBytes(risk.body, "reasoning.effort").String())
		switch effort {
		case "none", "minimal", "low":
			effort = "low"
		case "high", "xhigh", "max":
			effort = "high"
		default:
			effort = "medium"
		}
		risk.healthBand = size + ":" + effort
	})
	return mode + ":" + risk.healthBand
}

func (s *defaultOpenAIAccountScheduler) schedulingHealthCohort(ctx context.Context, account *Account, req OpenAIAccountScheduleRequest) string {
	mode := "native_http"

	if account.IsOpenAICloudConfigured() {
		mode = UsageAuthorizationModeCloud
	} else if account.IsExcelBPSEnabledForModel(req.RequestedModel) {
		mode = "bps"
	} else if req.RequiredTransport != OpenAIUpstreamTransportAny && req.RequiredTransport != OpenAIUpstreamTransportHTTPSSE {
		mode = "native_ws"
		if req.RequiredTransport == OpenAIUpstreamTransportResponsesWebsocketV2Ingress && s.service != nil && s.service.getOpenAIWSProtocolResolver().Resolve(account).Transport == OpenAIUpstreamTransportHTTPSSE {
			mode = "native_http"
		}
	} else if req.RequiredTransport == OpenAIUpstreamTransportAny && !req.RequireCompact &&
		account.IsOpenAIOAuthLike() && !account.IsOpenAIPassthroughEnabled() && s.service != nil &&
		(req.RequiredCapability == "" || req.RequiredCapability == OpenAIEndpointCapabilityResponses || req.RequiredCapability == OpenAIEndpointCapabilityChatCompletions) &&
		s.service.getOpenAIWSProtocolResolver().Resolve(account).Transport == OpenAIUpstreamTransportResponsesWebsocketV2 {
		mode = "native_ws"
	}
	return schedulingHealthCohort(ctx, mode)
}

func reportedSchedulingHealthCohort(ctx context.Context, account *Account) string {
	if account != nil && account.IsOpenAICloudConfigured() {
		return schedulingHealthCohort(ctx, UsageAuthorizationModeCloud)
	}
	if b, _ := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget); b != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.healthAccountID == recoveryOwner(account).account {
			return b.healthCohort
		}
	}
	return schedulingHealthCohort(ctx, "unknown")
}
