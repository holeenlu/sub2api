package middleware

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type vendorPortalSettings interface {
	VendorPortalBoundary(context.Context) (string, bool, error)
}

// Supplier host and Origin share a narrow API surface. Headers never grant a
// role; the ordinary JWT/admin middleware still checks every allowed endpoint.
func VendorOrigin(settings vendorPortalSettings) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settings == nil || !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		origin, enabled, err := settings.VendorPortalBoundary(c.Request.Context())
		if err != nil {
			AbortWithError(c, 503, "VENDOR_ORIGIN_UNAVAILABLE", "Portal policy is unavailable")
			return
		}
		if origin == "" {
			c.Next()
			return
		}
		expected, e := url.Parse(origin)
		if e != nil || expected.Host == "" {
			AbortWithError(c, 503, "VENDOR_ORIGIN_UNAVAILABLE", "Portal policy is unavailable")
			return
		}
		requestOrigin := strings.TrimSuffix(strings.TrimSpace(c.GetHeader("Origin")), "/")
		supplier := sameSupplierOrigin(expected.Scheme+"://"+c.Request.Host, origin) || sameSupplierOrigin(requestOrigin, origin)
		// Referrer covers same-host GETs whose browsers omit Origin.
		if !supplier && c.GetHeader("Referer") != "" {
			if ref, e := url.Parse(c.GetHeader("Referer")); e == nil {
				supplier = sameSupplierOrigin(ref.Scheme+"://"+ref.Host, origin)
			}
		}
		c.Request = c.Request.WithContext(authz.WithSupplierOrigin(c.Request.Context(), supplier))
		if supplier && (!enabled || !supplierOriginRoute(c)) {
			AbortWithError(c, 403, "VENDOR_ORIGIN_FORBIDDEN", "This endpoint is not available on the supplier portal")
			return
		}
		c.Next()
	}
}
func sameSupplierOrigin(a, b string) bool {
	x, e := url.Parse(a)
	if e != nil || x.Host == "" {
		return false
	}
	y, e := url.Parse(b)
	if e != nil {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(x.Scheme, y.Scheme) && strings.EqualFold(x.Hostname(), y.Hostname()) && port(x) == port(y) && x.User == nil
}
func supplierOriginRoute(c *gin.Context) bool {
	path, method := c.FullPath(), c.Request.Method
	if method == http.MethodOptions {
		method = c.GetHeader("Access-Control-Request-Method")
		path = supplierPreflightPath(c.Request.URL.Path)
	}
	if vendorProfileRoute(path) {
		return true
	}
	switch method + " " + path {
	case "GET /api/v1/settings/public", "GET /api/v1/settings/favicon", "POST /api/v1/auth/login", "POST /api/v1/auth/login/2fa", "POST /api/v1/auth/refresh", "POST /api/v1/auth/logout", "POST /api/v1/auth/passkey/login/begin", "POST /api/v1/auth/passkey/login/finish", "POST /api/v1/auth/forgot-password", "POST /api/v1/auth/reset-password":
		return true
	case "GET /api/v1/admin/groups", "GET /api/v1/admin/groups/all",
		"GET /api/v1/admin/system/version",
		"GET /api/v1/admin/settings/web-search-emulation",
		"GET /api/v1/admin/tls-fingerprint-profiles",
		"GET /api/v1/admin/compliance", "POST /api/v1/admin/compliance/accept":
		return true
	}
	if strings.HasPrefix(path, "/api/v1/vendor/") || path == "/api/v1/vendor" {
		return true
	}
	if strings.HasPrefix(path, "/api/v1/admin/vendors") {
		_, ok := authz.RuleFor(method, path)
		return ok
	}
	// Never allow an unfiltered native pool, statistics or unrelated admin API.
	id, err := strconv.ParseInt(c.Query("vendor_id"), 10, 64)
	if err != nil || id < 0 || (method != http.MethodGet && id == 0 && !(method == http.MethodPost && path == "/api/v1/admin/accounts/bulk-update")) {
		return false
	}
	switch method + " " + path {
	case "POST /api/v1/admin/accounts/bulk-update":
		return true
	case "GET /api/v1/admin/accounts", "GET /api/v1/admin/accounts/:id", "GET /api/v1/admin/accounts/:id/usage", "POST /api/v1/admin/accounts", "PUT /api/v1/admin/accounts/:id", "DELETE /api/v1/admin/accounts/:id",
		"POST /api/v1/admin/accounts/import/preview", "POST /api/v1/admin/accounts/import", "POST /api/v1/admin/accounts/export", "POST /api/v1/admin/accounts/:id/schedulable", "POST /api/v1/admin/accounts/:id/pause", "POST /api/v1/admin/accounts/:id/resume", "POST /api/v1/admin/accounts/:id/refresh", "POST /api/v1/admin/accounts/:id/usage-refresh", "POST /api/v1/admin/accounts/:id/probe", "POST /api/v1/admin/accounts/:id/sync", "DELETE /api/v1/admin/accounts/:id/authorizations/:mode",
		"GET /api/v1/admin/proxies", "GET /api/v1/admin/proxies/:id", "POST /api/v1/admin/proxies", "PUT /api/v1/admin/proxies/:id", "DELETE /api/v1/admin/proxies/:id", "POST /api/v1/admin/proxies/import/preview", "POST /api/v1/admin/proxies/import", "POST /api/v1/admin/proxies/export", "POST /api/v1/admin/proxies/batch-delete", "POST /api/v1/admin/proxies/:id/test", "POST /api/v1/admin/proxies/:id/quality-check":
		return true
	}
	return false
}

// The shared profile/security APIs remain available to dedicated supplier users.
// Consumer keys, balances, subscriptions, payments and reports are not included.
func vendorProfileRoute(path string) bool {
	switch path {
	case "/api/v1/auth/me", "/api/v1/auth/revoke-all-sessions",
		"/api/v1/user", "/api/v1/user/profile", "/api/v1/user/password",
		"/api/v1/user/notify-email/send-code", "/api/v1/user/notify-email/verify", "/api/v1/user/notify-email/toggle", "/api/v1/user/notify-email",
		"/api/v1/user/totp/status", "/api/v1/user/totp/verification-method", "/api/v1/user/totp/send-code", "/api/v1/user/totp/setup", "/api/v1/user/totp/enable", "/api/v1/user/totp/disable", "/api/v1/user/totp/step-up",
		"/api/v1/user/passkeys", "/api/v1/user/passkeys/register/begin", "/api/v1/user/passkeys/register/finish", "/api/v1/user/passkeys/:id":
		return true
	}
	return false
}
func vendorRoleRouteAllowed(role, path string) bool {
	return !authz.IsVendorRole(role) || strings.HasPrefix(path, "/api/v1/vendor/") || vendorProfileRoute(path)
}

// Preflight has no Gin match. Normalize only the identifiers used by the fixed
// route allowlist, then apply the same method and scope checks as the request.
func supplierPreflightPath(path string) string {
	parts := strings.Split(path, "/")
	for i := 1; i < len(parts); i++ {
		if parts[i-1] == "authorizations" && (parts[i] == "native" || parts[i] == "bps") {
			parts[i] = ":mode"
			continue
		}
		if parts[i-1] == "sessions" && i > 1 && parts[i-2] != "auth" {
			parts[i] = ":session_id"
			continue
		}
		if _, err := strconv.ParseInt(parts[i], 10, 64); err == nil {
			if parts[i-1] == "payments" {
				parts[i] = ":payment_id"
			} else {
				parts[i] = ":id"
			}
		}
	}
	return strings.Join(parts, "/")
}
