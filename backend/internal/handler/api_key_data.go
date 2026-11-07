package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *APIKeyHandler) ExportData(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	var ids []int64
	if raw, present := c.GetQuery("ids"); present {
		for _, value := range strings.Split(raw, ",") {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				response.BadRequest(c, "Invalid key selection")
				return
			}
			ids = append(ids, id)
		}
	}
	filters := service.APIKeyListFilters{Search: strings.TrimSpace(c.Query("search")), Status: c.Query("status")}
	if len(filters.Search) > 100 {
		response.BadRequest(c, "Search is too long")
		return
	}
	if raw := c.Query("group_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			response.BadRequest(c, "Invalid group filter")
			return
		}
		filters.GroupID = &id
	}
	data, err := h.apiKeyService.ExportData(c.Request.Context(), subject.UserID, ids, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, data)
}

func (h *APIKeyHandler) ImportData(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
	var req struct {
		Data service.APIKeyData `json:"data"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid key backup or file exceeds 5 MiB")
		return
	}
	result, err := h.apiKeyService.ImportData(c.Request.Context(), subject.UserID, req.Data)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
