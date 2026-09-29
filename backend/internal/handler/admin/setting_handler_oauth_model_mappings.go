package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetOAuthInitialModelMappings returns the opt-in model mapping template used
// only when a new OpenAI OAuth account is created.
func (h *SettingHandler) GetOAuthInitialModelMappings(c *gin.Context) {
	settings, err := h.settingService.GetOAuthInitialModelMappings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateOAuthInitialModelMappings(c *gin.Context) {
	var settings service.OAuthInitialModelMappings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, "Invalid OAuth model mappings")
		return
	}
	saved, err := h.settingService.SaveOAuthInitialModelMappings(c.Request.Context(), settings)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}
