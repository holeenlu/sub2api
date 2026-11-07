package admin

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) SetAccountTelemetryGateway(gateway *service.GatewayService) {
	h.accountTelemetry = gateway
}

// Explicit, administrator-only device import; never registered on client event
// endpoints. The ordinary account update/import API cannot write these fields.
func (h *AccountHandler) ImportAccountTelemetry(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, service.AccountTelemetryMaxBodyBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			response.Error(c, http.StatusRequestEntityTooLarge, "Telemetry bundle exceeds 1 MiB")
		} else {
			response.BadRequest(c, "Invalid telemetry bundle")
		}
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if h.accountTelemetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account telemetry unavailable")
		return
	}
	state, err := h.accountTelemetry.ImportAccountTelemetry(c.Request.Context(), account, raw)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, state)
}
