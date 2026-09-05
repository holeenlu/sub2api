//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Responses 耗尽路径与 /v1/messages、CC 共用同一套约定：上游原始状态码进 ops，
// 映射后的状态码/类型/文案才给客户端。此前这里把最后一次上游状态原样透出。
func TestHandleResponsesFailoverExhaustedMapsUpstreamStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		upstreamStatus int
		wantClientCode int
		wantErrType    string
		wantMessage    string
	}{
		{"overloaded", 529, http.StatusServiceUnavailable, "overloaded_error", "Upstream service overloaded, please retry later"},
		{"rate limited", http.StatusTooManyRequests, http.StatusTooManyRequests, "rate_limit_error", "Upstream rate limit exceeded, please retry later"},
		{"auth", http.StatusUnauthorized, http.StatusBadGateway, "upstream_error", "Upstream authentication failed, please contact administrator"},
		{"forbidden", http.StatusForbidden, http.StatusBadGateway, "upstream_error", "Upstream access forbidden, please contact administrator"},
		{"server error", http.StatusInternalServerError, http.StatusBadGateway, "upstream_error", "Upstream service temporarily unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)

			(&GatewayHandler{}).handleResponsesFailoverExhausted(c, &service.UpstreamFailoverError{
				StatusCode:   tt.upstreamStatus,
				ResponseBody: []byte(`{"error":{"message":"upstream said no"}}`),
			}, false)

			require.Equal(t, tt.wantClientCode, recorder.Code)
			require.Contains(t, recorder.Body.String(), tt.wantErrType)
			require.Contains(t, recorder.Body.String(), tt.wantMessage)
		})
	}
}

// 静默拒绝分支：ops 必须拿到上游真实状态码，而不是网关映射后的 502。
func TestHandleResponsesFailoverExhaustedRecordsRawStatusOnSilentRefusal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	(&GatewayHandler{}).handleResponsesFailoverExhausted(c, &service.UpstreamFailoverError{
		StatusCode:   http.StatusInternalServerError,
		ResponseBody: []byte(`{"error":{"code":"openai_silent_refusal","message":"refused"}}`),
	}, false)

	recorded, ok := c.Get(service.OpsUpstreamStatusCodeKey)
	require.True(t, ok)
	require.Equal(t, http.StatusInternalServerError, recorded)
	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "upstream_error")
}

// 没有上游错误（纯粹选不出账号）时保留原有文案。
func TestHandleResponsesFailoverExhaustedWithoutUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	(&GatewayHandler{}).handleResponsesFailoverExhausted(c, nil, false)

	require.Equal(t, http.StatusBadGateway, recorder.Code)
	require.Contains(t, recorder.Body.String(), "server_error")
	require.Contains(t, recorder.Body.String(), "All available accounts exhausted")
}
