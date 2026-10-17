package handler

import (
	"errors"
	"net/http"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Bound local uploads independently of inference and its account slots.
var clientTelemetrySlots = make(chan struct{}, 16)

func (h *GatewayHandler) ClaudeTelemetry(c *gin.Context) {
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "API key required to save telemetry"})
		return
	}
	c.Header("Cache-Control", "no-store")
	select {
	case clientTelemetrySlots <- struct{}{}:
		defer func() { <-clientTelemetrySlots }()
	default:
		c.Header("Retry-After", "1")
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "telemetry storage busy"})
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPreallocLimit(c.Request, service.ClientTelemetryMaxBodyBytes)
	if err != nil {
		var maxBytes *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &maxBytes) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": "invalid or oversized telemetry body"})
		return
	}
	if err := h.gatewayService.SaveClaudeTelemetry(c.Request.Context(), key, c.Request.URL.Path, c.Request.Header, body); err != nil {
		if errors.Is(err, service.ErrInvalidClientTelemetry) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telemetry batch"})
		} else {
			c.Header("Retry-After", "1")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "telemetry could not be saved"})
		}
		return
	}
	// Same local empty acknowledgement as Wei-Shaw; there is no supplier call.
	c.Status(http.StatusOK)
}
