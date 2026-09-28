package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Old standalone BPS groups must not fall through to another upstream protocol.
func retiredPlatformGuard(c *gin.Context) {
	key, _ := middleware.GetAPIKeyFromContext(c)
	legacyGroup := key != nil && key.Group != nil && service.IsRetiredPlatform(key.Group.Platform)
	if !legacyGroup && !service.IsRetiredPlatform(getGroupPlatform(c)) {
		c.Next()
		return
	}
	service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
	c.AbortWithStatusJSON(http.StatusGone, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "platform_retired", "message": "The standalone OpenAI BPS platform has been removed; configure an OpenAI OAuth account with Excel / BPS instead"}})
}
