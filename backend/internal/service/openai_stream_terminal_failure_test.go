package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMarkOpenAIStreamTerminalFailureDeliveredKeepsSemanticStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		payload    string
		wantStatus int
		wantCode   string
	}{
		{"request rejection", `{"type":"error","error":{"code":"unsupported_parameter","type":"invalid_request_error","message":"Hosted tool 'web_search' requires authorization and metering that are not supported by rustponsesapi.","param":"tools"},"status":400}`, http.StatusBadRequest, "unsupported_parameter"},
		{"response.failed rate limit", `{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded","type":"rate_limit_error","message":"slow down"}}}`, http.StatusTooManyRequests, "rate_limit_exceeded"},
		{"unclassified", `{"type":"error","error":{"message":"boom"}}`, http.StatusBadGateway, "upstream_error"},
		{"empty payload", ``, http.StatusBadGateway, "upstream_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			require.False(t, OpenAIStreamTerminalFailureDelivered(c))
			markOpenAIStreamTerminalFailureDelivered(c, []byte(tc.payload), extractOpenAISSEErrorMessage([]byte(tc.payload)))
			require.True(t, OpenAIStreamTerminalFailureDelivered(c))
			marked, ok := GetOpsStreamError(c)
			require.True(t, ok)
			require.Equal(t, tc.wantStatus, marked.IntendedStatus)
			require.Equal(t, tc.wantCode, marked.Code)
			require.NotContains(t, marked.Message, "sk-")
		})
	}
}
