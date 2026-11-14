package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOnlineUpdaterRoutesRequireSuperAuthorityAndStepUpForStart(t *testing.T) {
	for _, endpoint := range []struct{ method, path string }{{"GET", "/api/v1/admin/system/update"}, {"POST", "/api/v1/admin/system/update/check"}, {"POST", "/api/v1/admin/system/update"}} {
		rule, known := authz.RuleFor(endpoint.method, endpoint.path)
		require.True(t, known)
		for _, role := range []string{authz.Admin, authz.User, authz.VendorLevel1, authz.VendorLevel2} {
			require.False(t, rule.Allows(authz.Subject{Role: role}), endpoint.path+role)
		}
		require.True(t, rule.Allows(authz.Subject{Role: authz.SuperAdmin}))
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_method", "jwt")
		c.Request = c.Request.WithContext(authz.WithSubject(context.Background(), authz.Subject{UserID: 1, Role: authz.SuperAdmin}))
		c.Next()
	})
	system := admin.NewSystemHandler(service.NewBuildStatusService("1.1.0"), nil)
	system.SetOnlineUpdater(service.NewOnlineUpdaterService("", service.BuildStatus{}), nil)
	steps := 0
	stepUp := middleware.StepUpAuthMiddleware(func(c *gin.Context) { steps++; c.AbortWithStatus(http.StatusTeapot) })
	registerSystemRoutes(router.Group("/api/v1/admin"), &handler.Handlers{Admin: &handler.AdminHandlers{System: system}}, stepUp)
	for _, path := range []string{"/update/check", "/update"} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/api/v1/admin/system"+path, strings.NewReader(`{}`)))
		if path == "/update" {
			require.Equal(t, http.StatusTeapot, out.Code)
		} else {
			require.Equal(t, http.StatusServiceUnavailable, out.Code)
		}
	}
	require.Equal(t, 1, steps)
	missingVerification := gin.New()
	registerSystemRoutes(missingVerification.Group("/api/v1/admin"), &handler.Handlers{Admin: &handler.AdminHandlers{System: system}}, nil)
	out := httptest.NewRecorder()
	missingVerification.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update", strings.NewReader(`{}`)))
	require.Equal(t, http.StatusServiceUnavailable, out.Code)
	require.Contains(t, out.Body.String(), "STEP_UP_UNAVAILABLE")
}
