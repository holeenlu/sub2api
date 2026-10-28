package service

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
)

// ForwardAsAnthropic keeps the public compatibility contract while execution
// owns any declared continuation repair. A provider attempt no longer calls
// itself recursively to send another Native Responses request.
func (s *OpenAIGatewayService) ForwardAsAnthropic(ctx context.Context, c *gin.Context, account *Account, body []byte, promptCacheKey, defaultMappedModel string) (result *OpenAIForwardResult, err error) {
	BeginOpenAIRequestTiming(c)
	defer func() {
		if result != nil {
			result.LatencyMetrics = SnapshotOpenAIUsageLatency(c, result.Stream)
		}
		openAIForwardTimingFromContext(c).Finish(err)
	}()
	attemptCtx, attemptBody := ctx, body
	return runNativeHTTPProtocolAttempts(ctx, func() (*OpenAIForwardResult, error) {
		result, err := s.forwardAsAnthropicAttempt(attemptCtx, c, account, attemptBody, promptCacheKey, defaultMappedModel)
		if nextCtx, nextBody, reason, ok := applyNativeCompatibilityRepair(attemptCtx, attemptBody, err); ok {
			attemptCtx, attemptBody = nextCtx, nextBody
			return result, requestNativeHTTPReprepare(reason)
		}
		return result, err
	})
}

type nativeCompatibilityRepair struct {
	agentTaskRecovered bool
	grokBody           []byte
}

func (*nativeCompatibilityRepair) Error() string { return "compatibility preparation requires repair" }

// Existing one-time recovery marks are projected only after that attempt has
// returned and closed. These are typed decisions, not an arbitrary context bag.
func applyNativeCompatibilityRepair(ctx context.Context, body []byte, err error) (context.Context, []byte, string, bool) {
	var repair *nativeCompatibilityRepair
	if !errors.As(err, &repair) {
		return ctx, body, "", false
	}
	if repair.agentTaskRecovered {
		return markAgentIdentityTaskRecoveryTried(ctx), body, "agent_task_recovery", true
	}
	if repair.grokBody != nil {
		return markGrokEncryptedContentStripRetried(ctx), repair.grokBody, "grok_thinking_signature_repair", true
	}
	return ctx, body, "", false
}

// The first response supplies this existing fallback's original start time.
// It conveys no account/grant/permit and never resets the shared turn budget.
type nativeRawChatFallback struct{ started time.Time }

func (*nativeRawChatFallback) Error() string {
	return "native compatibility requires raw chat fallback"
}

func (s *OpenAIGatewayService) forwardAsChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte, promptCacheKey, defaultMappedModel string, compatPromptCacheTenantIsolated bool) (result *OpenAIForwardResult, err error) {
	BeginOpenAIRequestTiming(c)
	defer func() {
		if result != nil {
			result.LatencyMetrics = SnapshotOpenAIUsageLatency(c, result.Stream)
		}
		openAIForwardTimingFromContext(c).Finish(err)
	}()
	var fallback *nativeRawChatFallback
	attemptCtx, attemptBody := ctx, body
	return runNativeHTTPProtocolAttempts(ctx, func() (*OpenAIForwardResult, error) {
		if fallback != nil {
			return s.forwardAsRawChatCompletionsWithStartTime(attemptCtx, c, account, attemptBody, defaultMappedModel, fallback.started)
		}
		result, err := s.forwardAsChatCompletionsAttempt(attemptCtx, c, account, attemptBody, promptCacheKey, defaultMappedModel, compatPromptCacheTenantIsolated)
		if errors.As(err, &fallback) {
			return result, requestNativeHTTPReprepare("compat_raw_chat_fallback")
		}
		if nextCtx, nextBody, reason, ok := applyNativeCompatibilityRepair(attemptCtx, attemptBody, err); ok {
			attemptCtx, attemptBody = nextCtx, nextBody
			return result, requestNativeHTTPReprepare(reason)
		}
		return result, err
	})
}
