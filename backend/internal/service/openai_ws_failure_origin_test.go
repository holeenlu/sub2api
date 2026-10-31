package service

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSFailureOriginUsesTrustedBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin openAIWSFailureOrigin
	}{
		{"provider", openAIWSFailureProvider},
		{"request", openAIWSFailureRequest},
		{"gateway", openAIWSFailureGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			// The identical wire code must not decide who owns the failure.
			payload := []byte("{\"type\":\"error\",\"status\":400,\"error\":{\"type\":\"invalid_request_error\",\"code\":\"previous_response_not_found\",\"message\":\"parent unavailable\"}}")
			markOpenAIWSClientVisibleFailure(c, "error", payload, tc.origin)
			failure, ok := GetOpsStreamError(c)
			require.True(t, ok)
			require.Equal(t, "previous_response_not_found", failure.Code)
			require.Equal(t, http.StatusBadRequest, failure.IntendedStatus)
			require.Equal(t, tc.origin == openAIWSFailureRequest, failure.RequestScoped)
			require.Equal(t, tc.origin != openAIWSFailureRequest, failure.CountTowardsSLA)
			if tc.origin == openAIWSFailureProvider {
				require.Equal(t, http.StatusBadRequest, failure.UpstreamStatus)
				require.Equal(t, "parent unavailable", failure.UpstreamMessage)
			} else {
				require.Zero(t, failure.UpstreamStatus)
				require.Empty(t, failure.UpstreamMessage)
			}
		})
	}
}
