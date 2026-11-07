//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type faviconRouteSettingsRepo struct {
	service.SettingRepository
	values map[string]string
	reads  int
}

func (r *faviconRouteSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.reads++
	values := make(map[string]string)
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

func (r *faviconRouteSettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func newFaviconRouteRouter(t *testing.T, repo *faviconRouteSettingsRepo, client *redis.Client) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	settings := service.NewSettingService(repo, &config.Config{})
	var limiter *servermiddleware.PanelRateLimiter
	if client != nil {
		limiter = servermiddleware.NewPanelRateLimiter(client, settings)
	}
	RegisterAuthRoutes(
		router.Group("/api/v1"),
		&handler.Handlers{Auth: &handler.AuthHandler{}, Passkey: &handler.PasskeyHandler{}, Setting: handler.NewSettingHandler(settings, "test")},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			t.Error("public favicon must not invoke JWT authentication")
			c.AbortWithStatus(http.StatusUnauthorized)
		}),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
			t.Error("public favicon must not enter the auth audit group")
			c.AbortWithStatus(http.StatusForbidden)
		}),
		client, settings, limiter,
	)
	return router
}

func TestAuthRoutesFaviconPublicUsesConfiguredBrand(t *testing.T) {
	repo := &faviconRouteSettingsRepo{values: map[string]string{
		service.SettingKeySiteLogo:           "data:image/png;base64,cG5n",
		service.SettingKeyBackendModeEnabled: "true",
		"vendor_console_enabled":             "false",
	}}
	router := newFaviconRouteRouter(t, repo, nil)
	registered := false
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/settings/favicon" {
			require.Equal(t, http.MethodGet, route.Method)
			registered = true
		}
	}
	require.True(t, registered)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "png", recorder.Body.String())
	require.Equal(t, "image/png", recorder.Header().Get("Content-Type"))
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "default-src 'none'; sandbox", recorder.Header().Get("Content-Security-Policy"))
	require.Equal(t, 1, repo.reads)

	repo.values[service.SettingKeySiteLogo] = "/configured-site.svg"
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil))
	require.Equal(t, http.StatusTemporaryRedirect, recorder.Code)
	require.Equal(t, "/configured-site.svg", recorder.Header().Get("Location"))
	require.Equal(t, 2, repo.reads, "the endpoint must resolve current branding each time")
}

func TestAuthRoutesFaviconKeepsPublicIPRateLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	repo := &faviconRouteSettingsRepo{values: map[string]string{
		service.SettingKeySiteLogo:               "/configured-site.svg",
		service.SettingKeyPanelRateLimitSettings: `{"enabled":true,"public_ip_rpm":1}`,
	}}
	router := newFaviconRouteRouter(t, repo, client)
	request := func(peer, forwarded string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/settings/favicon", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", forwarded)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		return recorder
	}
	require.Equal(t, http.StatusTemporaryRedirect, request("203.0.113.10:4321", "198.51.100.1").Code)
	limited := request("203.0.113.10:4321", "198.51.100.2")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.NotEmpty(t, limited.Header().Get("Retry-After"))
	require.Empty(t, limited.Header().Get("Location"))
	require.Equal(t, 1, repo.reads, "rate limiting must precede the settings query")
	require.Equal(t, http.StatusTemporaryRedirect, request("203.0.113.11:4321", "198.51.100.2").Code)
	require.Equal(t, 2, repo.reads, "a different trusted client IP retains its own bucket")
}
