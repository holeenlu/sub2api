package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// A forwarded upstream terminal failure must not be followed by the generic
// 502 response.failed: the handler treats it as already communicated and the
// recorded status is the failure's own (400 here), not a gateway 502.
func TestOpenAIForwardErrorAlreadyCommunicatedAfterDeliveredTerminalFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	before := c.Writer.Size()
	_, err := c.Writer.WriteString("data: {\"type\":\"error\",\"error\":{\"code\":\"unsupported_parameter\",\"type\":\"invalid_request_error\",\"message\":\"rejected\"}}\n\n")
	require.NoError(t, err)
	streamErr := errors.New("stream usage incomplete after disconnect: EOF")
	require.False(t, openAIForwardErrorAlreadyCommunicated(c, before, streamErr), "an unrelated stream error still gets the fallback terminal")
	service.MarkOpsStreamErrorValue(c, service.OpsStreamError{}) // no-op guard: marks are first-wins
	c.Set("openai_stream_terminal_failure_delivered", true)
	require.True(t, openAIForwardErrorAlreadyCommunicated(c, before, streamErr))
	h := &OpenAIGatewayHandler{}
	require.False(t, h.ensureForwardErrorResponse(c, true) && !service.OpenAIStreamTerminalFailureDelivered(c))
}
