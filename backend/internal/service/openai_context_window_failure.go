package service

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// openAIResponsesClientStreamKey records whether the client asked for a
// streamed /responses answer. The upstream wire format can differ (Codex OAuth
// forces SSE upstream even for JSON clients, BPS always streams), so error
// writers consult the client's own request instead of the body sent upstream.
const openAIResponsesClientStreamKey = "openai_responses_client_stream"

func markOpenAIResponsesClientStream(c *gin.Context, stream bool) {
	if c == nil {
		return
	}
	c.Set(openAIResponsesClientStreamKey, stream)
}

func openAIResponsesClientWantsStream(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if openAICompactClientWantsStream(c) {
		return true
	}
	value, ok := c.Get(openAIResponsesClientStreamKey)
	if !ok {
		return false
	}
	stream, _ := value.(bool)
	return stream
}

// writeOpenAIContextWindowSSEFailure delivers an upstream HTTP 400
// context-window rejection to a streaming Responses client as a terminal
// response.failed event on an HTTP 200 SSE stream. Codex maps
// context_length_exceeded to its "context window exhausted" state only from
// response.failed (codex-rs codex-api/src/sse/responses_error.rs); the same code
// on an HTTP 400 is shown as a plain invalid request and the next turn never
// compacts. Shared by the native, passthrough and Excel/BPS HTTP paths.
//
// Returns false for JSON clients or when a response was already written, and
// the caller keeps its JSON error. Ops records the semantic 400 through
// MarkOpsStreamError; the actual upstream status stays in setOpsUpstreamError.
func writeOpenAIContextWindowSSEFailure(c *gin.Context, upstreamStatus int, message, param string) bool {
	if c == nil || !openAIResponsesClientWantsStream(c) {
		return false
	}
	if !StopOpenAICompactSSEKeepaliveCommitted(c) {
		if c.Writer.Written() {
			return false
		}
		header := c.Writer.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("Connection", "keep-alive")
		header.Set("X-Accel-Buffering", "no")
		c.Writer.WriteHeader(http.StatusOK)
	}
	errorBody := gin.H{"type": "invalid_request_error", "code": "context_length_exceeded", "message": message}
	if param != "" {
		errorBody["param"] = param
	}
	writeOpenAIResponsesSSEFailure(c, upstreamStatus, errorBody)
	return true
}
