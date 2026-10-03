package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) PinRequestPricing(c *gin.Context) {
	if h == nil || h.gatewayService == nil || c.Request == nil {
		return
	}
	key, _ := middleware.GetAPIKeyFromContext(c)
	c.Request = c.Request.WithContext(h.gatewayService.PinRequestPricing(c.Request.Context(), key))
}
