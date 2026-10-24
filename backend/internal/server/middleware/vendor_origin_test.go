package middleware

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

type vendorOriginSettings struct{ enabled bool }

func (s vendorOriginSettings) VendorPortalBoundary(context.Context) (string, bool, error) {
	return "https://supply.example.invalid", s.enabled, nil
}
func TestSupplierOriginWhitelistAndHostWithoutOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		method, path, pattern string
		status                int
	}{
		{"POST", "/api/v1/auth/login", "/api/v1/auth/login", 204},
		{"POST", "/api/v1/auth/register", "/api/v1/auth/register", 403},
		{"GET", "/api/v1/admin/users", "/api/v1/admin/users", 403},
		{"GET", "/api/v1/admin/groups/all", "/api/v1/admin/groups/all", 204},
		{"POST", "/api/v1/admin/groups", "/api/v1/admin/groups", 403},
		{"GET", "/api/v1/admin/vendors", "/api/v1/admin/vendors", 204},
		{"GET", "/api/v1/admin/system/version", "/api/v1/admin/system/version", 204},
		{"POST", "/api/v1/admin/system/update", "/api/v1/admin/system/update", 403},
		{"GET", "/api/v1/admin/compliance", "/api/v1/admin/compliance", 204},
		{"POST", "/api/v1/admin/compliance/accept", "/api/v1/admin/compliance/accept", 204},
		{"GET", "/api/v1/admin/compliance/status", "/api/v1/admin/compliance/status", 403},
		{"POST", "/api/v1/admin/compliance", "/api/v1/admin/compliance", 403},
		{"GET", "/api/v1/admin/settings", "/api/v1/admin/settings", 403},
		{"GET", "/api/v1/admin/accounts", "/api/v1/admin/accounts", 403},
		{"GET", "/api/v1/admin/accounts?vendor_id=0", "/api/v1/admin/accounts", 204},
		{"PUT", "/api/v1/admin/accounts/12?vendor_id=4", "/api/v1/admin/accounts/:id", 204},
		{"PUT", "/api/v1/admin/accounts/12?vendor_id=0", "/api/v1/admin/accounts/:id", 403},

		{"POST", "/api/v1/admin/accounts/12/schedulable?vendor_id=4", "/api/v1/admin/accounts/:id/schedulable", 204},
		{"POST", "/api/v1/admin/accounts/12/schedulable?vendor_id=0", "/api/v1/admin/accounts/:id/schedulable", 403},
		{"POST", "/api/v1/admin/accounts/12/schedulable", "/api/v1/admin/accounts/:id/schedulable", 403},
		{"GET", "/api/v1/admin/accounts/stats?vendor_id=4", "/api/v1/admin/accounts/stats", 403},
		{"GET", "/api/v1/vendor/accounts?vendor_id=4", "/api/v1/vendor/accounts", 204},
		{"GET", "/api/v1/payment/orders", "/api/v1/payment/orders", 403},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			for _, origin := range []bool{false, true} {
				r := gin.New()
				r.Use(VendorOrigin(vendorOriginSettings{enabled: true}))
				r.Handle(tc.method, tc.pattern, func(c *gin.Context) {
					supplier, ok := authz.SupplierOrigin(c.Request.Context())
					require.True(t, ok)
					require.True(t, supplier)
					c.Status(204)
				})
				req := httptest.NewRequest(tc.method, "https://supply.example.invalid"+tc.path, nil)
				if origin {
					req.Host = "api.example.invalid"
					req.Header.Set("Origin", "https://supply.example.invalid")
				}
				out := httptest.NewRecorder()
				r.ServeHTTP(out, req)
				require.Equal(t, tc.status, out.Code, out.Body.String())
			}
		})
	}
}
func TestSupplierRoleCannotReachCustomerOrAdminRoutes(t *testing.T) {
	versionRule, registered := authz.RuleFor(http.MethodGet, "/api/v1/admin/system/version")
	require.True(t, registered)
	for _, role := range []string{authz.SuperAdmin, authz.Admin, authz.User, authz.VendorLevel1, authz.VendorLevel2} {
		require.Equal(t, role == authz.SuperAdmin, versionRule.Allows(authz.Subject{Role: role}), role)
	}
	for _, role := range []string{authz.VendorLevel1, authz.VendorLevel2} {
		for _, path := range []string{"/api/v1/keys", "/api/v1/payment/orders", "/api/v1/user/aff", "/api/v1/groups/available", "/api/v1/usage", "/api/v1/subscriptions", "/api/v1/admin/vendors", "/api/v1/admin/compliance", "/api/v1/admin/compliance/accept"} {
			require.False(t, vendorRoleRouteAllowed(role, path), path)
		}
		for _, path := range []string{"/api/v1/vendor/accounts", "/api/v1/auth/me", "/api/v1/user/profile", "/api/v1/user/password", "/api/v1/user/totp/step-up"} {
			require.True(t, vendorRoleRouteAllowed(role, path), path)
		}
	}
	r := gin.New()
	r.Use(VendorOrigin(vendorOriginSettings{}))
	r.GET("/api/v1/vendor/accounts", func(c *gin.Context) { c.Status(204) })
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "https://supply.example.invalid/api/v1/vendor/accounts", nil))
	require.Equal(t, 403, out.Code)
}

func TestSupplierOriginPreflightUsesSameAllowlist(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		allowed      bool
	}{
		{"PUT", "/api/v1/admin/accounts/12?vendor_id=4", true},
		{"PUT", "/api/v1/admin/accounts/12?vendor_id=0", false},
		{"PUT", "/api/v1/admin/users/12?vendor_id=4", false},
		{"POST", "/api/v1/admin/vendors/settlements/3/payments/5/reverse", true},
		{"GET", "/api/v1/user/passkeys/12", true},
		{"GET", "/api/v1/admin/compliance", true},
		{"GET", "/api/v1/admin/system/version", true},
		{"POST", "/api/v1/admin/system/update", false},
		{"POST", "/api/v1/admin/system/restart", false},
		{"POST", "/api/v1/admin/compliance/accept", true},
		{"GET", "/api/v1/admin/compliance/status", false},
	} {
		r := gin.New()
		r.Use(VendorOrigin(vendorOriginSettings{enabled: true}))
		r.OPTIONS("/*path", func(c *gin.Context) { c.Status(204) })
		req := httptest.NewRequest("OPTIONS", "https://supply.example.invalid"+tc.path, nil)
		req.Header.Set("Access-Control-Request-Method", tc.method)
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		expected := 403
		if tc.allowed {
			expected = 204
		}
		require.Equal(t, expected, out.Code, tc.path)
	}
}

func TestSupplierOriginPreservesComplianceAcknowledgementGuard(t *testing.T) {
	r := gin.New()
	r.Use(VendorOrigin(vendorOriginSettings{enabled: true}))
	r.Use(func(c *gin.Context) { c.Set(string(ContextKeyUser), AuthSubject{UserID: 1}); c.Next() })
	r.Use(AdminComplianceGuard(service.NewSettingService(&complianceGuardRepoStub{}, &config.Config{})))
	r.GET("/api/v1/admin/compliance", func(c *gin.Context) { c.Status(204) })
	r.POST("/api/v1/admin/compliance/accept", func(c *gin.Context) { c.Status(204) })
	r.GET("/api/v1/admin/vendors", func(c *gin.Context) { c.Status(204) })
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/v1/admin/compliance", 204},
		{"POST", "/api/v1/admin/compliance/accept", 204},
		{"GET", "/api/v1/admin/vendors", http.StatusLocked},
	} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(tc.method, "https://supply.example.invalid"+tc.path, nil))
		require.Equal(t, tc.status, out.Code, out.Body.String())
		if tc.status == http.StatusLocked {
			require.Contains(t, out.Body.String(), "ADMIN_COMPLIANCE_ACK_REQUIRED")
		}
	}
}
