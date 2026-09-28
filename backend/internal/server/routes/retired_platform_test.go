package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRetiredPlatformGuard(t *testing.T) {
	for _, tc := range []struct {
		name, group, resolved, forced string
		status                        int
	}{
		{name: "retired group", group: "openai_bps", status: http.StatusGone},
		{name: "forced alias cannot bypass retirement", group: "openai_bps", forced: service.PlatformAntigravity, status: http.StatusGone},
		{name: "resolved legacy route", group: service.PlatformComposite, resolved: "openai_bps", status: http.StatusGone},
		{name: "OpenAI OAuth BPS remains available", group: service.PlatformOpenAI, status: http.StatusNoContent},
		{name: "normal composite OpenAI", group: service.PlatformComposite, resolved: service.PlatformOpenAI, status: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{Group: &service.Group{Platform: tc.group}})
				if tc.resolved != "" {
					c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), tc.resolved))
				}
				c.Next()
			})
			if tc.forced != "" {
				r.Use(middleware.ForcePlatform(tc.forced))
			}
			r.Use(retiredPlatformGuard)
			forwarded := false
			r.POST("/v1/responses", func(c *gin.Context) { forwarded = true; c.Status(http.StatusNoContent) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/responses", nil))
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.status == http.StatusNoContent, forwarded)
			if tc.status == http.StatusGone {
				require.Contains(t, w.Body.String(), "platform_retired")
			}
		})
	}
}

func TestRetiredPlatformMountedRoutesRejectWithoutForwarding(t *testing.T) {
	router := newGatewayRoutesTestRouterWithGroup(&service.Group{Platform: "openai_bps"})
	for _, path := range []string{"/v1/responses", "/v1/responses/compact", "/responses", "/responses/compact", "/chat/completions", "/v1/chat/completions", "/v1/messages", "/backend-api/codex/responses", "/antigravity/v1/messages"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
			require.Equal(t, http.StatusGone, w.Code)
			require.Contains(t, w.Body.String(), "platform_retired")
		})
	}
}
