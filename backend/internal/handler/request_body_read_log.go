package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RespondRequestBodyReadFailure shares the ingress middleware policy so all
// body-reading stages classify disconnects, timeouts and decoding errors alike.
func RespondRequestBodyReadFailure(c *gin.Context, reqLog *zap.Logger, err error, render func(*gin.Context, int, string, string)) {
	if reqLog == nil {
		reqLog = requestLogger(c, "")
	}
	middleware.RespondRequestBodyReadFailure(c, reqLog, err, render)
}
