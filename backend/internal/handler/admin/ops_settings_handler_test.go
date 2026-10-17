package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsAdvancedSettingsAccountTelemetrySendingRoundTrip(t *testing.T) {
	svc := newRuntimeOpsService(t)
	h := NewOpsHandler(svc)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/advanced-settings", h.GetAdvancedSettings)
	r.PUT("/advanced-settings", h.UpdateAdvancedSettings)
	settings, err := svc.GetOpsAdvancedSettings(t.Context())
	require.NoError(t, err)
	require.False(t, settings.AccountTelemetrySendingEnabled)
	for _, enabled := range []bool{true, false} {
		settings.AccountTelemetrySendingEnabled = enabled
		raw, err := json.Marshal(settings)
		require.NoError(t, err)
		for _, method := range []string{http.MethodPut, http.MethodGet} {
			request := httptest.NewRequest(method, "/advanced-settings", bytes.NewReader(raw))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			r.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var result struct{ Data service.OpsAdvancedSettings }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, enabled, result.Data.AccountTelemetrySendingEnabled)
		}
	}
}
