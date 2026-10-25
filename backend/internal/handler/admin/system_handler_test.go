//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSystemHandlerVersionReportsRunningBuild(t *testing.T) {
	svc := service.ProvideBuildStatusService(service.BuildInfo{
		Version:         "rebuild-dev",
		BuildType:       "source",
		UpstreamVersion: "v0.2.17",
		BuildCommit:     "abcdef0123456789",
	})
	handler := NewSystemHandler(svc, nil)
	router := gin.New()
	router.GET("/api/v1/admin/system/version", handler.GetVersion)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/version", nil))
	require.Equal(t, http.StatusOK, out.Code)
	require.Equal(t, "rebuild-dev", out.Header().Get("X-Portal-Backend-Version"))

	var body struct {
		Code int               `json:"code"`
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(out.Body.Bytes(), &body))
	require.Zero(t, body.Code)
	require.Equal(t, map[string]string{
		"version":          "rebuild-dev",
		"upstream_version": "v0.2.17",
		"build_commit":     "abcdef0123456789",
	}, body.Data)
}

func TestSystemHandlerCheckUpdatesReportsManualDeployment(t *testing.T) {
	handler := NewSystemHandler(service.NewBuildStatusService("rebuild-dev", "release"), nil)
	router := gin.New()
	router.GET("/api/v1/admin/system/check-updates", handler.CheckUpdates)
	for _, query := range []string{"", "?force=true"} {
		t.Run(query, func(t *testing.T) {
			out := httptest.NewRecorder()
			router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/check-updates"+query, nil))
			require.Equal(t, http.StatusOK, out.Code)

			var body struct {
				Code int            `json:"code"`
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &body))
			require.Zero(t, body.Code)
			require.Equal(t, "manual", body.Data["update_method"])
			require.Equal(t, true, body.Data["disabled"])
			require.Equal(t, false, body.Data["has_update"])
			require.Equal(t, false, body.Data["cached"])
			require.Equal(t, "release", body.Data["build_type"])
			require.Equal(t, "rebuild-dev", body.Data["current_version"])
			require.Equal(t, body.Data["current_version"], body.Data["latest_version"])
			require.NotEmpty(t, body.Data["warning"])
			for _, absent := range []string{"release_info", "container_update", "release_channel", "build_commit", "upstream_version"} {
				require.NotContains(t, body.Data, absent)
			}
		})
	}
}
