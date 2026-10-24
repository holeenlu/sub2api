package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/gin-gonic/gin"
)

// opsRequestBodyReadFailureKey carries the classified body-read failure to the
// Ops error logger, which appends it to the persisted error body.
const opsRequestBodyReadFailureKey = "ops_request_body_read_failure"

// writeRequestBodyReadError answers a failed request-body read with the status
// the failure kind deserves and records the classification for Ops:
//
//   - max_bytes → 413 with the enforced limit
//   - timeout / deadline_exceeded → 408 request_timeout (stalled upload)
//   - client abandoned (truncated body, reset, canceled) → 499 with a minimal body
//   - decoding and other malformed bodies → 400 "Failed to read request body"
//
// write keeps each API style's own error envelope (OpenAI, Anthropic, Google).
func writeRequestBodyReadError(c *gin.Context, err error, write func(status int, errType, message string)) {
	failure := httputil.DescribeRequestBodyReadFailure(c.Request, err)
	c.Set(opsRequestBodyReadFailureKey, failure)
	status, errType, message := failure.Response()
	write(status, errType, message)
}

// opsRequestBodyReadFailure returns the body-read failure recorded for this
// request, by the handler helper or by the shared reader used in middleware.
func opsRequestBodyReadFailure(c *gin.Context) (httputil.RequestBodyReadFailure, bool) {
	if c == nil {
		return httputil.RequestBodyReadFailure{}, false
	}
	if v, ok := c.Get(opsRequestBodyReadFailureKey); ok {
		if failure, ok := v.(httputil.RequestBodyReadFailure); ok {
			return failure, true
		}
	}
	return httputil.RequestBodyReadFailureFromRequest(c.Request)
}
