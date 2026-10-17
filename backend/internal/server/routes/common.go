package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）
func RegisterCommonRoutes(r *gin.Engine) {
	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode)
	// This is used by the frontend to detect when the service has restarted after setup
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}

// Anonymous events retain Wei-Shaw's empty 200 response. Authentication remains
// mandatory for sample storage; caller-supplied user/account IDs are never trusted.
func acknowledgeAnonymousClientTelemetry(c *gin.Context) {
	if c.GetHeader("Authorization") == "" && c.GetHeader("x-api-key") == "" &&
		c.GetHeader("x-goog-api-key") == "" && c.Query("key") == "" && c.Query("api_key") == "" {
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatus(http.StatusOK)
	}
}
