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
