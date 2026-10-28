package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
)

// RequestBodyLimit bounds wire bytes and shares the route budget with decoded
// and normalized readers, including middleware that prereads the body.
func RequestBodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(httputil.WithRequestBodyLimit(c.Request.Context(), maxBytes))
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		// Observe the route-limited wire reader, before any preread. An inner
		// EOF accompanying an outer limit error is not a complete body proof.
		httputil.CaptureRequestWire(c.Request)
		c.Next()
	}
}
