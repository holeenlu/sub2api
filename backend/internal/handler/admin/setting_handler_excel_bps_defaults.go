package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetExcelBPSDefaults(c *gin.Context) {
	settings, err := h.settingService.GetExcelBPSDefaults(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateExcelBPSDefaults(c *gin.Context) {
	settings, err := h.settingService.GetExcelBPSDefaults(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, "Invalid BPS defaults")
		return
	}
	saved, err := h.settingService.SaveExcelBPSDefaults(c.Request.Context(), settings)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}
