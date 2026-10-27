package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountTelemetryHasNoClientCollector(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterCommonRoutes(engine)
	for _, path := range []string{"/api/event_logging/batch", "/api/event_logging/v2/batch"} {
		for _, token := range []string{"", "Bearer invalid"} {
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader("not JSON and never decoded"))
			request.Header.Set("Authorization", token)
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, request)
			require.Equal(t, 200, recorder.Code)
			require.Empty(t, recorder.Body.String())
		}
	}
	for _, path := range []string{"/v1/logs", "/v1/metrics", "/api/v1/admin/ops/client-telemetry"} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, 404, recorder.Code)
	}
}
