package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudContinuationRequestScopedClassification(t *testing.T) {
	for _, tc := range []struct {
		code                        string
		status                      int
		requestScoped, localFailure bool
	}{
		{"cloud_history_mismatch", http.StatusBadRequest, true, false},
		{"cloud_continuation_settings_changed", http.StatusBadRequest, true, false},
		{"cloud_cached_response_invalid", http.StatusBadGateway, true, true},
		{"cloud_transport_closed", http.StatusBadGateway, false, false},
		{"cloud_execution_failed", http.StatusBadGateway, false, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			failure := &cloudGatewayError{status: tc.status, code: tc.code, message: "safe failure"}
			require.Equal(t, tc.requestScoped, failure.RequestScoped())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			writer := &responsesEventWriter{header: make(http.Header), status: http.StatusOK, size: -1, maxFrameBytes: 4096}
			c.Writer = writer
			writeCloudGatewayError(c, failure)
			require.NoError(t, writer.err)
			require.Equal(t, tc.status, writer.Status())
			require.Equal(t, tc.code, gjson.GetBytes(writer.pending, "error.code").String())
			require.Equal(t, tc.localFailure, c.GetBool(OpsLocalDependencyFailureKey))
			require.Equal(t, tc.requestScoped && tc.status < 500, writer.failureOrigin == openAIWSFailureRequest)
		})
	}
	var absent *cloudGatewayError
	require.False(t, absent.RequestScoped())
}

func TestCloudLocalReasonReachesJSONAndWS(t *testing.T) {
	for _, transport := range []string{"json", "ws"} {
		t.Run(transport, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			cause := cloudWithFailureReason(&cloudGatewayError{400, "cloud_continuation_settings_changed", "settings changed"}, "current_turn_settings_changed", "reasoning.effort")
			var payload []byte
			if transport == "json" {
				writeCloudGatewayError(c, cause)
				require.Equal(t, 400, rec.Code)
				payload = rec.Body.Bytes()
			} else {
				ctx := context.WithValue(t.Context(), cloudContinuationErrorKey{}, cause)
				var frames [][]byte
				result, err := (&OpenAIGatewayService{}).proxyOpenAIWSCloudTurn(ctx, c, &Account{}, []byte(`{"model":"m"}`), "m", func(raw []byte) error {
					frames = append(frames, append([]byte(nil), raw...))
					return nil
				})
				require.Error(t, err)
				require.Nil(t, result)
				require.Len(t, frames, 1)
				payload = frames[0]
				require.Equal(t, "error", gjson.GetBytes(payload, "type").String())
				require.EqualValues(t, 400, gjson.GetBytes(payload, "status").Int())
				require.Empty(t, rec.Body.String(), "WS errors use the direct event sink")
			}
			require.Equal(t, "cloud_continuation_settings_changed", gjson.GetBytes(payload, "error.code").String())
			require.Equal(t, "current_turn_settings_changed", gjson.GetBytes(payload, "error.reason").String())
			require.Equal(t, "reasoning.effort", gjson.GetBytes(payload, "error.param").String())
		})
	}
}
