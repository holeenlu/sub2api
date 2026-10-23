package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIOAuthHandler) siwcAccount(c *gin.Context) *service.Account {
	ctx := c.Request.Context()
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		response.ErrorFrom(c, authz.ErrPermissionDenied)
		return nil
	}
	if err := authz.CheckLease(ctx, authz.CaptureLease(ctx), "accounts.authorize"); err != nil {
		response.ErrorFrom(c, err)
		return nil
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return nil
	}
	account, err := h.adminService.GetAccount(ctx, id)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil
	}
	if !account.IsSIWCEligible() {
		response.BadRequest(c, "SIWC requires an ordinary administrator-owned OpenAI OAuth parent account")
		return nil
	}
	return account
}
func (h *OpenAIOAuthHandler) GetSIWCAuthorization(c *gin.Context) {
	account := h.siwcAccount(c)
	if account == nil {
		return
	}
	status, err := h.openaiOAuthService.SIWCAuthorizationStatus(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}
func (h *OpenAIOAuthHandler) BeginSIWCAuthorization(c *gin.Context) {
	account := h.siwcAccount(c)
	if account == nil {
		return
	}
	result, err := h.openaiOAuthService.BeginSIWCAuthorization(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
func (h *OpenAIOAuthHandler) CompleteSIWCAuthorization(c *gin.Context) {
	account := h.siwcAccount(c)
	if account == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 48<<10)
	var input struct {
		SessionID   string `json:"session_id" binding:"required"`
		CallbackURL string `json:"callback_url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || len(input.SessionID) > 256 {
		response.BadRequest(c, "Invalid SIWC callback request")
		return
	}
	if err := h.openaiOAuthService.CompleteSIWCAuthorization(c.Request.Context(), account, input.SessionID, input.CallbackURL); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	status, err := h.openaiOAuthService.SIWCAuthorizationStatus(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}
func (h *OpenAIOAuthHandler) DeleteSIWCAuthorization(c *gin.Context) {
	account := h.siwcAccount(c)
	if account == nil {
		return
	}
	result, err := h.openaiOAuthService.DeleteSIWCAuthorization(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
