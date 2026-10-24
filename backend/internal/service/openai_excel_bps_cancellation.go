package service

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
)

// A canceled upstream child context is not proof that the client disconnected.
// Require the matching incoming cancellation, or a recorded downstream write failure.
func isExcelBPSClientCancellation(c *gin.Context, err error) bool {
	if c != nil && downstreamStreamError(c) != nil {
		return true
	}
	return c != nil && c.Request != nil && errors.Is(err, context.Canceled) &&
		errors.Is(c.Request.Context().Err(), context.Canceled)
}
