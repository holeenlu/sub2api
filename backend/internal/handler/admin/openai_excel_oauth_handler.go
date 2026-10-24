package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIOAuthHandler) excelAccount(c *gin.Context) *service.Account {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return nil
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil
	}
	if !account.IsExcelBPSEligible() {
		response.BadRequest(c, "Excel BPS requires a regular paid ChatGPT OAuth account")
		return nil
	}
	return account
}

func (h *OpenAIOAuthHandler) GetExcelAuthorization(c *gin.Context) {
	account := h.excelAccount(c)
	if account == nil {
		return
	}
	status, err := h.openaiOAuthService.ExcelAuthorizationStatus(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, status)
}

func (h *OpenAIOAuthHandler) ImportExcelAuthorization(c *gin.Context) {
	account := h.excelAccount(c)
	if account == nil {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var credentials map[string]any
	if err := c.ShouldBindJSON(&credentials); err != nil {
		response.BadRequest(c, "Invalid Excel credential object")
		return
	}
	if err := h.openaiOAuthService.ImportExcelCredentials(c.Request.Context(), account, credentials); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"authorized": true})
}

func (h *OpenAIOAuthHandler) DeleteExcelAuthorization(c *gin.Context) {
	account := h.excelAccount(c)
	if account == nil {
		return
	}
	if err := h.openaiOAuthService.DeleteExcelAuthorization(c.Request.Context(), account); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"authorized": false})
}
