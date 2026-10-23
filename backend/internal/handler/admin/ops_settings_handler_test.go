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

func TestOpsAdvancedSettingsPartialUpdatePreservesQuotaPolicy(t *testing.T) {
	svc := newRuntimeOpsService(t)
	cfg, err := svc.GetOpsAdvancedSettings(t.Context())
	require.NoError(t, err)
	cfg.QuotaProtection.CriticalBandPercent = 7
	cfg.QuotaProtection.AnthropicTierThresholdsEnabled = true
	_, err = svc.UpdateOpsAdvancedSettings(t.Context(), cfg)
	require.NoError(t, err)
	h := NewOpsHandler(svc)
	r := gin.New()
	r.PUT("/advanced-settings", h.UpdateAdvancedSettings)
	request := httptest.NewRequest(http.MethodPut, "/advanced-settings", bytes.NewBufferString(`{"account_telemetry_sending_enabled":true,"quota_protection":{"large_cold_input_tokens":120000}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	got := svc.OpsAdvancedSettingsSnapshot()
	require.True(t, got.AccountTelemetrySendingEnabled)
	require.Equal(t, 7.0, got.QuotaProtection.CriticalBandPercent)
	require.True(t, got.QuotaProtection.AnthropicTierThresholdsEnabled)
	require.Equal(t, 120000, got.QuotaProtection.LargeColdInputTokens)
}
