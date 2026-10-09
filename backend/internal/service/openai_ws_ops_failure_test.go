package service

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarkOpenAIWSClientVisibleFailure_ResponseFailedNestedError(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	markOpenAIWSClientVisibleFailure(c, "response.failed", []byte(`{"type":"response.failed","response":{"error":{"type":"invalid_request_error","code":"context_length_exceeded","message":"too long","status_code":400}}}`))

	got, ok := GetOpsStreamError(c)
	require.True(t, ok)
	require.True(t, got.CountTowardsSLA)
	require.Equal(t, http.StatusBadRequest, got.IntendedStatus)
	require.Equal(t, "invalid_request_error", got.ErrType)
	require.Equal(t, "context_length_exceeded", got.Code)
	require.Equal(t, "too long", got.Message)
}

func TestMarkOpenAIWSClientVisibleFailure_ErrorAndSuccessBoundary(t *testing.T) {
	t.Run("local client restriction is not an upstream error", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalPolicyDenied)
		markOpenAIWSClientVisibleFailure(c, "error", []byte(`{"type":"error","status":403,"error":{"type":"permission_error","message":"client restricted"}}`))
		got, ok := GetOpsStreamError(c)
		require.True(t, ok)
		require.Zero(t, got.UpstreamStatus)
		require.Empty(t, got.UpstreamMessage)
	})
	t.Run("error", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		markOpenAIWSClientVisibleFailure(c, "error", []byte(`{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"slow down"}}`))
		got, ok := GetOpsStreamError(c)
		require.True(t, ok)
		require.Equal(t, http.StatusTooManyRequests, got.IntendedStatus)
	})

	for _, tc := range []struct {
		name, eventType, payload string
		status                   int
	}{
		{"bare overload", "error", `{"type":"error","error":{"type":"upstream_error","code":"server_is_overloaded","message":"overloaded"}}`, http.StatusServiceUnavailable},
		{"failed overload", "response.failed", `{"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"overloaded"}}}`, http.StatusServiceUnavailable},
		{"explicit provider status", "response.failed", `{"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"overloaded","status_code":502}}}`, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			setOpsUpstreamError(c, http.StatusBadGateway, "overloaded", "provisional websocket error")
			markOpenAIWSClientVisibleFailure(c, tc.eventType, []byte(tc.payload))
			got, ok := GetOpsStreamError(c)
			require.True(t, ok)
			require.True(t, got.CountTowardsSLA)
			require.Equal(t, tc.status, got.IntendedStatus)
			require.Equal(t, tc.status, got.UpstreamStatus)
			require.Equal(t, "server_is_overloaded", got.Code, "Ops retains the original provider code")
			require.Equal(t, "client-visible websocket "+tc.eventType, got.UpstreamDetail)
		})
	}

	t.Run("completed does not mark", func(t *testing.T) {
		c, _ := gin.CreateTestContext(nil)
		markOpenAIWSClientVisibleFailure(c, "response.completed", []byte(`{"type":"response.completed"}`))
		_, ok := GetOpsStreamError(c)
		require.False(t, ok)
	})
}

func TestSetOpsUpstreamModelStoresOnlyTrimmedModelSlug(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetOpsUpstreamModel(c, "  gpt-5.6-sol  ")
	value, ok := c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "gpt-5.6-sol", value)

	SetOpsUpstreamModel(c, "  ")
	value, ok = c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "gpt-5.6-sol", value)

	ClearOpsUpstreamModel(c)
	value, ok = c.Get(OpsUpstreamModelKey)
	require.True(t, ok)
	require.Equal(t, "", value)
}
