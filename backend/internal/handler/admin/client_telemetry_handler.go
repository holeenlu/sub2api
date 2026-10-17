package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpsHandler) ListClientTelemetry(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	page, size := response.ParsePagination(c)
	start, end, err := parseOpsTimeRange(c, "24h")
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	filter := &service.ClientTelemetryLogFilter{Page: page, PageSize: size, StartTime: &start, EndTime: &end, SessionID: strings.TrimSpace(c.Query("session_id"))}
	// Account copy picker must also reach retained samples older than 30 days.
	if c.Query("time_range") == "all" && c.Query("start_time") == "" && c.Query("end_time") == "" {
		filter.StartTime, filter.EndTime = nil, nil
	}
	if len(filter.SessionID) > 512 {
		response.BadRequest(c, "Invalid session_id")
		return
	}
	for name, target := range map[string]**int64{"user_id": &filter.UserID, "api_key_id": &filter.APIKeyID} {
		if value := strings.TrimSpace(c.Query(name)); value != "" {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				response.BadRequest(c, "Invalid "+name)
				return
			}
			*target = &id
		}
	}
	out, err := h.opsService.ListClientTelemetry(c.Request.Context(), filter)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, out.Items, out.Total, out.Page, out.PageSize)
}

func (h *OpsHandler) GetClientTelemetry(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid telemetry id")
		return
	}
	row, err := h.opsService.GetClientTelemetry(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, row)
}
