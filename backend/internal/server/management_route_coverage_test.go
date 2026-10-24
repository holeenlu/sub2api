package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRealRouterHasCompleteManagementPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	noop := func(c *gin.Context) { c.Next() }
	h := &handler.Handlers{Admin: &handler.AdminHandlers{}, Vendor: handler.NewVendorHandler(nil, nil, nil), VendorAccount: handler.NewVendorAccountHandler(nil), VendorProxy: handler.NewVendorProxyHandler(nil), VendorUsage: handler.NewVendorUsageHandler(nil), VendorSettlement: handler.NewVendorSettlementHandler(nil)}
	registerRoutes(r, h, middleware.JWTAuthMiddleware(noop), middleware.OptionalJWTAuthMiddleware(noop), middleware.AdminAuthMiddleware(noop), middleware.APIKeyAuthMiddleware(noop), middleware.AuditLogMiddleware(noop), middleware.StepUpAuthMiddleware(noop), nil, nil, nil, nil, nil, &config.Config{}, nil)
	count := 0
	capabilityUI := false
	backupRoutes := map[string]bool{
		"GET /api/v1/admin/backups":               false,
		"POST /api/v1/admin/backups":              false,
		"GET /api/v1/admin/backups/image-storage": false,
	}
	for _, route := range r.Routes() {
		require.False(t, strings.HasPrefix(route.Path, "/api/v1/admin/data-management"), "retired data management route: %s %s", route.Method, route.Path)
		if _, retained := backupRoutes[route.Method+" "+route.Path]; retained {
			backupRoutes[route.Method+" "+route.Path] = true
		}
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
	for route, registered := range backupRoutes {
		require.True(t, registered, "native backup/image storage route must remain: %s", route)
	}
	for _, path := range []string{"/api/v1/admin/data-management/agent/health", "/api/v1/admin/data-management/config", "/api/v1/admin/data-management/backups"} {
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	}
}
