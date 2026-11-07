package admin

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsRejectRetiredSchedulerWrites(t *testing.T) {
	for _, key := range []string{"enabled", "sticky_weighted_enabled", "lb_top_k", "weight_priority", "weight_load", "weight_queue", "weight_error_rate", "weight_ttft", "weight_reset", "weight_quota_headroom", "weight_upstream_cost", "weight_previous_response", "weight_session_sticky"} {
		for _, value := range []string{"null", "0", "true"} {
			t.Run(key+value, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(fmt.Sprintf("{\"openai_advanced_scheduler_%s\":%s}", key, value)))
				ctx.Request.Header.Set("Content-Type", "application/json")
				handler := &SettingHandler{}
				handler.UpdateSettings(ctx)
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "retired")
			})
		}
	}
}
