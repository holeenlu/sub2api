package service

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const conversionDiagnosticsContextKey = "gateway_conversion_diagnostics"

// Diagnostics are logged for both successful and failed conversions, with the
// request logger's correlation ID. They reuse the bounded Ops attempt fields on
// errors; no payload or new persistent database field is introduced.
func recordConversionDiagnostics(c *gin.Context, source, target, model string, items apicompat.ConversionDiagnostics) {
	if c == nil || c.Request == nil {
		return
	}
	var seen apicompat.ConversionDiagnostics
	if value, ok := c.Get(conversionDiagnosticsContextKey); ok {
		seen, _ = value.(apicompat.ConversionDiagnostics)
	}
	for _, item := range items {
		before := len(seen)
		seen.Add(item.Code, item.Path)
		if before == len(seen) {
			continue
		}
		logger.FromContext(c.Request.Context()).Info("gateway.conversion_adjustment", zap.String("code", item.Code), zap.String("path", item.Path), zap.String("source_protocol", source), zap.String("target_protocol", target), zap.String("model", truncateString(model, 128)))
	}
	c.Set(conversionDiagnosticsContextKey, seen)
}
