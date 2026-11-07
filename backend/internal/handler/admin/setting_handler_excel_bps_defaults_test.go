package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSettingsExcelBPSDefaultsRoundTripAndIsolation(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{"site_name": "existing"})
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(r)
		c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/excel-bps-defaults", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		if method == http.MethodGet {
			h.GetExcelBPSDefaults(c)
		} else {
			h.UpdateExcelBPSDefaults(c)
		}
		return r
	}
	r := call(http.MethodGet, "")
	require.Equal(t, http.StatusOK, r.Code)
	require.NotContains(t, repo.values, service.SettingKeyExcelBPSDefaults)
	r = call(http.MethodPut, "{\"models\":[\"custom-model\"],\"all_models\":false}")
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	r = call(http.MethodGet, "")
	require.Equal(t, "custom-model", gjson.Get(r.Body.String(), "data.models.0").String())
	before := repo.values[service.SettingKeyExcelBPSDefaults]
	for _, body := range []string{"{\"models\":[]}", "{\"recovery_interval_minutes\":0}", "{\"models\":\"invalid\"}"} {
		r = call(http.MethodPut, body)
		require.Equal(t, http.StatusBadRequest, r.Code, r.Body.String())
		require.Equal(t, before, repo.values[service.SettingKeyExcelBPSDefaults])
	}
	r = doUpdateSettings(t, h, map[string]any{"site_name": "other-setting"}, nil)
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	require.Equal(t, before, repo.values[service.SettingKeyExcelBPSDefaults], "unrelated system settings must preserve the template")
}
