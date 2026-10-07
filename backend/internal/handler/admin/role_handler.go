package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetAdminRolePermissions(c *gin.Context) {
	actor, ok := authz.FromContext(c.Request.Context())
	if !ok || actor.Role != service.RoleSuperAdmin {
		response.ErrorFrom(c, service.ErrAdminPermissionDenied)
		return
	}
	policy, err := h.settingService.GetAdminRolePolicyForEdit(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	count, err := h.userService.CountAdministrators(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"policy": policy, "catalogue": authz.Catalogue(), "affected_admin_count": count, "step_up_enabled": h.settingService.IsStepUpEnabled(c.Request.Context())})
}

func (h *SettingHandler) UpdateAdminRolePermissions(c *gin.Context) {
	actor, ok := authz.FromContext(c.Request.Context())
	if !ok || actor.Role != service.RoleSuperAdmin {
		response.ErrorFrom(c, service.ErrAdminPermissionDenied)
		return
	}
	if !middleware.EnforceStepUpAlways(c, h.totpService, h.userService) {
		return
	}
	var req struct {
		ExpectedVersion *int64   `json:"expected_version" binding:"required,min=0"`
		Permissions     []string `json:"permissions" binding:"required,max=128"`
		Reason          string   `json:"reason" binding:"required,max=512"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		response.BadRequest(c, "Invalid role permissions")
		return
	}
	trace := &service.AuditLog{CreatedAt: time.Now().UTC(), ActorUserID: &actor.UserID, ActorEmail: c.GetString(middleware.ContextKeyAuthEmail), ActorRole: actor.Role, Visibility: service.RoleSuperAdmin, AuthMethod: c.GetString("auth_method"), Method: http.MethodPut, Path: c.FullPath(), ClientIP: middleware.SecurityClientIP(c), StatusCode: http.StatusOK}
	trace.Extra = map[string]any{"reason": strings.TrimSpace(req.Reason)}
	policy, err := h.settingService.UpdateAdminRolePolicy(c.Request.Context(), *req.ExpectedVersion, req.Permissions, trace)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// The policy and its audit event were committed together.
	middleware.SkipAudit(c)
	response.Success(c, gin.H{"policy": policy, "catalogue": authz.Catalogue()})
}
