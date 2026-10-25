package service

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const openAIStreamTerminalFailureDeliveredKey = "openai_stream_terminal_failure_delivered"

// markOpenAIStreamTerminalFailureDelivered records that the upstream's own
// terminal failure (a bare `error` or `response.failed`) has reached the client
// on an HTTP/SSE stream. The handler must not append its generic 502
// `response.failed` behind it, and Ops keeps the failure's semantic status
// (for example a 400 request rejection) instead of a gateway 502.
func markOpenAIStreamTerminalFailureDelivered(c *gin.Context, payload []byte, message string) {
	if c == nil {
		return
	}
	c.Set(openAIStreamTerminalFailureDeliveredKey, true)
	status := http.StatusBadGateway
	if len(payload) > 0 && gjson.ValidBytes(payload) {
		status = openAIStreamFailedEventSemanticStatus(payload, message)
	}
	code := openAIStreamFailedEventErrorCode(payload)
	if code == "" {
		code = "upstream_error"
	}
	errType := strings.TrimSpace(gjson.GetBytes(payload, "response.error.type").String())
	if errType == "" {
		errType = strings.TrimSpace(gjson.GetBytes(payload, "error.type").String())
	}
	if errType == "" {
		errType = "upstream_error"
	}
	message = sanitizeUpstreamErrorMessage(strings.TrimSpace(message))
	if message == "" {
		message = "Upstream response failed"
	}
	MarkOpsStreamFailure(c, errType, code, message, status)
}

// OpenAIStreamTerminalFailureDelivered reports whether the upstream terminal
// failure already reached the client on this request's stream.
func OpenAIStreamTerminalFailureDelivered(c *gin.Context) bool {
	if c == nil {
		return false
	}
	return c.GetBool(openAIStreamTerminalFailureDeliveredKey)
}
