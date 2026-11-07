package service

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
)

// A canceled upstream child context is not proof that the client disconnected.
// Require the matching incoming cancellation, a recorded downstream write
// failure, or a client WebSocket turn cancellation. On the WS bridge the Gin
// request context belongs to the whole connection and never cancels for one
// turn; response.cancel reaches the forwarder only as the cancellation cause of
// its own context, so read that cause instead of the request context.
func isExcelBPSClientCancellation(ctx context.Context, c *gin.Context, err error) bool {
	if c != nil && downstreamStreamError(c) != nil {
		return true
	}
	if ctx != nil && errors.Is(context.Cause(ctx), errOpenAIWSTurnCancelled) {
		return true
	}
	return c != nil && c.Request != nil && errors.Is(err, context.Canceled) &&
		errors.Is(c.Request.Context().Err(), context.Canceled)
}
