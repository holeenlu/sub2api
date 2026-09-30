//go:build unit

package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"strings"
	"testing"
)

func TestSecurityWSContinuationFirstAndLaterFrames(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModeShared, service.OpenAIWSIngressModeDedicated, service.OpenAIWSIngressModePassthrough} {
		for _, id := range []string{`"previous_response_id":"resp_other_tenant"`, `"previous_response_id":"","previous_response_id":"resp_other_tenant"`, `"previous_response_id":"","previous_response_\u0069d":"resp_other_tenant"`, `"Previous_Response_ID":"resp_other_tenant"`, `"response":{"previous_response_id":"resp_other_tenant"}`} {
			t.Run(mode+id, func(t *testing.T) {
				reason := "previous_response_id"
				if strings.Contains(id, "Previous_Response_ID") || strings.Count(id, "previous_response") > 1 {
					reason = "ambiguous JSON field"
				}
				runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{firstPayload: `{"type":"response.create","model":"gpt-5.4",` + id + `}`, group: wsAllowlistGroup(false), ingressMode: mode, firstFrameCloseExpected: true, closeReason: reason})
				runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{firstPayload: `{"type":"response.create","model":"gpt-5.4"}`, secondPayload: `{"type":"response.create","model":"gpt-5.4",` + id + `}`, group: wsAllowlistGroup(false), ingressMode: mode, secondTurnCloseExpected: true, closeReason: reason})
			})
		}
	}
}
