package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func ProvideOpenAIOAuthHandler(oauth *service.OpenAIOAuthService, admin service.AdminService, quota *service.OpenAIQuotaService, rate *service.RateLimitService, cloud *service.OpenAICloudService) *OpenAIOAuthHandler {
	h := NewOpenAIOAuthHandler(oauth, admin, quota, rate)
	h.cloudService = cloud
	return h
}

// Cloud controls live alongside the existing account editor and OAuth handler.
// No endpoint returns native credentials or accepts a client capability claim.
func (h *OpenAIOAuthHandler) cloudAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	if h.cloudService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Cloud authorization is unavailable")
		return 0, false
	}
	return id, true
}

func (h *OpenAIOAuthHandler) GetCloudAuthorization(c *gin.Context) {
	id, ok := h.cloudAccountID(c)
	if !ok {
		return
	}
	status, err := h.cloudService.AuthorizationStatus(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

func (h *OpenAIOAuthHandler) AuthorizeCloud(c *gin.Context) {
	id, ok := h.cloudAccountID(c)
	if !ok {
		return
	}
	result, err := h.cloudService.GenerateAuthURL(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

func (h *OpenAIOAuthHandler) CompleteCloudAuthorization(c *gin.Context) {
	id, ok := h.cloudAccountID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	var req struct {
		SessionID string `json:"session_id" binding:"required"`
		Code      string `json:"code" binding:"required"`
		State     string `json:"state" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid Cloud callback")
		return
	}
	status, err := h.cloudService.CompleteAuthorization(c.Request.Context(), id, &service.OpenAIExchangeCodeInput{SessionID: req.SessionID, Code: req.Code, State: req.State})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

func (h *OpenAIOAuthHandler) DeleteCloudAuthorization(c *gin.Context) {
	id, ok := h.cloudAccountID(c)
	if !ok {
		return
	}
	if err := h.cloudService.Disconnect(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"authorized": false, "remote_revoked": false})
}

func (h *OpenAIOAuthHandler) GetCloudUnsettled(c *gin.Context) {
	id, ok := h.cloudAccountID(c)
	if !ok {
		return
	}
	items, err := h.cloudService.Unsettled(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, items)
}

func (h *OpenAIOAuthHandler) ReconcileCloud(c *gin.Context) {
	id, ok := h.cloudAccountID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var input service.OpenAICloudReconciliationInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "Invalid Cloud reconciliation")
		return
	}
	if err := h.cloudService.Reconcile(c.Request.Context(), id, input); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"accepted": true, "settled": false})
}
