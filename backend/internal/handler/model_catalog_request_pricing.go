package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *GatewayHandler) CheckModelCatalog(c *gin.Context, g *service.Group, models []string) error {
	if h.modelCatalog == nil {
		return nil
	}
	ctx, err := h.modelCatalog.Admit(c.Request.Context(), g, models)
	if err == nil {
		c.Request = c.Request.WithContext(ctx)
	}
	return err
}

func (h *GatewayHandler) PinRequestPricing(c *gin.Context) {
	if h == nil || h.gatewayService == nil || c.Request == nil {
		return
	}
	key, _ := middleware.GetAPIKeyFromContext(c)
	c.Request = c.Request.WithContext(h.gatewayService.PinRequestPricing(c.Request.Context(), key))
}
