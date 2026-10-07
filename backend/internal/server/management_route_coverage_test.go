package server

import (
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestRealRouterHasCompleteManagementPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	noop := func(c *gin.Context) { c.Next() }
	h := &handler.Handlers{Admin: &handler.AdminHandlers{}}
	registerRoutes(r, h, middleware.JWTAuthMiddleware(noop), middleware.OptionalJWTAuthMiddleware(noop), middleware.AdminAuthMiddleware(noop), middleware.APIKeyAuthMiddleware(noop), middleware.AuditLogMiddleware(noop), middleware.StepUpAuthMiddleware(noop), nil, nil, nil, nil, nil, &config.Config{}, nil)
	count := 0
	capabilityUI := false
	for _, route := range r.Routes() {
		if route.Path == "/api/v1/plugin-ui/:token/*path" {
			capabilityUI = true
			continue
		} // Independently signed short-lived capability, not a staff endpoint.
		if !strings.HasPrefix(route.Path, "/api/v1/admin/") && route.Path != "/api/v1/pages" {
			continue
		}
		rule, ok := authz.RuleFor(route.Method, route.Path)
		require.True(t, ok, "missing policy %s %s", route.Method, route.Path)
		if route.Method != "GET" {
			require.True(t, rule.Mutates || rule.ReadOnly, "classify side effects: %s %s", route.Method, route.Path)
		}
		if rule.RequestSchema != "" {
			_, exists := authz.RequestSchemas()[rule.RequestSchema]
			require.True(t, exists, "missing request schema for %s %s", route.Method, route.Path)
		}
		count++
	}
	require.True(t, capabilityUI)
	require.Equal(t, len(authz.DeclaredRoutes()), count)
}
