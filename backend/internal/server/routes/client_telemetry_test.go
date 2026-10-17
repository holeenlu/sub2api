package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClientTelemetryRoutesAcknowledgeAnonymouslyButAuthenticateStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterCommonRoutes(router)
	calls := 0
	RegisterGatewayRoutes(router, &handler.Handlers{
		Gateway: &handler.GatewayHandler{}, OpenAIGateway: &handler.OpenAIGatewayHandler{},
		AsyncImage: handler.NewAsyncImageHandler(nil, nil),
	}, middleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		calls++
		c.AbortWithStatus(http.StatusUnauthorized)
	}), nil, nil, nil, nil, nil, &config.Config{})
	for _, path := range []string{service.ClaudeTelemetryLegacyPath, service.ClaudeTelemetryBatchPath} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}")))
		require.Equal(t, http.StatusOK, w.Code, path)
		require.Empty(t, w.Body.String())
		for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
			req.Header.Set(header, "invalid-key")
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusUnauthorized, w.Code, path)
		}
		for _, query := range []string{"key", "api_key"} {
			w = httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path+"?"+query+"=invalid-key", nil))
			require.Equal(t, http.StatusUnauthorized, w.Code, path)
		}
	}
	require.Equal(t, 10, calls)
}

func TestClientTelemetryRoutesValidateBeforeDispatch(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformAnthropic)
	RegisterCommonRoutes(router)
	for _, path := range []string{service.ClaudeTelemetryLegacyPath, service.ClaudeTelemetryBatchPath} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("X-Api-Key", "authenticated-by-test-middleware")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, "must not silently acknowledge discarded events")
	}
}
