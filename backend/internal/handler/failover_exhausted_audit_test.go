//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFailoverExhaustedPreservesRetryAudit(t *testing.T) {
	gateway := &GatewayHandler{}
	openai := &OpenAIGatewayHandler{}
	renderers := map[string]func(*gin.Context, *service.UpstreamFailoverError){
		"messages": func(c *gin.Context, err *service.UpstreamFailoverError) {
			gateway.handleFailoverExhausted(c, err, service.PlatformAnthropic, false)
		},
		"chat completions": func(c *gin.Context, err *service.UpstreamFailoverError) {
			gateway.handleCCFailoverExhausted(c, err, false)
		},
		"responses": func(c *gin.Context, err *service.UpstreamFailoverError) {
			gateway.handleResponsesFailoverExhausted(c, err, false)
		},
		"gemini": gateway.handleGeminiFailoverExhausted,
		"openai": func(c *gin.Context, err *service.UpstreamFailoverError) {
			openai.handleFailoverExhausted(c, err, false)
		},
	}
	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			service.RecordOpsUpstreamError(c, service.OpsUpstreamErrorEvent{
				AccountID: 42, UpstreamStatusCode: http.StatusBadGateway, Kind: "failover",
			})
			service.AnnotateLastOpsUpstreamError(c, "account_switch", 2, 3, 4, 500*time.Millisecond, true)
			render(c, &service.UpstreamFailoverError{
				StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":{"message":"unavailable"}}`),
			})

			value, exists := c.Get(service.OpsUpstreamErrorsKey)
			require.True(t, exists)
			events, ok := value.([]*service.OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.NotEmpty(t, events)
			event := events[0]
			require.Equal(t, "exhausted", event.RetryAction)
			require.Equal(t, 2, event.RetryCount)
			require.Equal(t, 3, event.RetryLimit)
			require.Equal(t, 4, event.SwitchCount)
			require.Zero(t, event.RetryDelayMs)
			require.False(t, event.Retryable)
		})
	}
}
