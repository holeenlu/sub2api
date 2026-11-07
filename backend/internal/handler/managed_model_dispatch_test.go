package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestManagedModelDispatchDenialRetainsModelNotFoundEnvelope(t *testing.T) {
	for _, anthropic := range []bool{false, true} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		handled := (&OpenAIGatewayHandler{}).writeNativeHTTPDispatchError(c, fmt.Errorf("send gate: %w", service.ErrManagedModelNotFound), false, anthropic)
		require.True(t, handled)
		require.Equal(t, http.StatusNotFound, recorder.Code)
		require.Equal(t, "model_not_found", gjson.GetBytes(recorder.Body.Bytes(), "error.code").String())
	}
}
