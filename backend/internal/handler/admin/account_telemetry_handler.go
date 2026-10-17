package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (h *AccountHandler) SetAccountTelemetryGateway(gateway *service.GatewayService) {
	h.accountTelemetry = gateway
}

// Queues official account initialization; accepts no uploaded identity or event.
func (h *AccountHandler) GenerateAccountTelemetry(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.accountTelemetry == nil {
		response.Error(c, 503, "Account telemetry unavailable")
		return
	}
	state, err := h.accountTelemetry.QueueAccountTelemetry(c.Request.Context(), account)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, state)
}
