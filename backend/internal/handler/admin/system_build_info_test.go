package admin

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSystemBuildCommitEndpoints(t *testing.T) {
	const commit = "2776c84a5ebcc94d2fba1f551c200ab1d9ffe149"
	svc := service.NewUpdateService(nil, nil, "0.2.1", "release").
		WithCheckEnabled(false).WithBuildCommit(commit).WithUpstreamVersion("v0.2.1")
	h := NewSystemHandler(svc, nil)
	for _, handler := range []gin.HandlerFunc{h.GetVersion, h.CheckUpdates} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest("GET", "/", nil)
		handler(ctx)
		require.Equal(t, 200, recorder.Code)
		var result struct {
			Data struct {
				BuildCommit     string `json:"build_commit"`
				UpstreamVersion string `json:"upstream_version"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
		require.Equal(t, commit, result.Data.BuildCommit)
		require.Equal(t, "v0.2.1", result.Data.UpstreamVersion)
	}
}
