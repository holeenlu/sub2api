//go:build unit

package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 转发阶段的 previous_response_id → 账号粘连按选号分组分命名空间，而 Forward
// 只拿得到 gin.Context：账号准入这个统一出口必须把选号分组记下来，否则无可用
// 账号兜底借来的账号会被绑进 API Key 自己的分组，下一轮续话必然读不到。
func TestAcquireOpenAIAccountSlotStampsSchedulingGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/openai/v1/responses", nil)
		return c
	}
	h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}}
	originGroupID := int64(1)
	fallbackGroupID := int64(2)
	streamStarted := false

	t.Run("records the group the account was borrowed from", func(t *testing.T) {
		c := newContext()
		selection := &service.AccountSelectionResult{
			Account:           &service.Account{ID: 20, Platform: service.PlatformOpenAI},
			Acquired:          true,
			SchedulingGroupID: &fallbackGroupID,
		}

		release, result := h.acquireOpenAIAccountSlot(c, &originGroupID, "", selection, false, &streamStarted, zap.NewNop(), nil)
		require.Equal(t, openAISlotAcquireOK, result)
		if release != nil {
			release()
		}
		require.Equal(t, fallbackGroupID, service.OpenAISchedulingGroupID(c))
	})

	t.Run("records the origin when no fallback was taken", func(t *testing.T) {
		c := newContext()
		selection := &service.AccountSelectionResult{
			Account:           &service.Account{ID: 10, Platform: service.PlatformOpenAI},
			Acquired:          true,
			SchedulingGroupID: &originGroupID,
		}

		release, result := h.acquireOpenAIAccountSlot(c, &originGroupID, "", selection, false, &streamStarted, zap.NewNop(), nil)
		require.Equal(t, openAISlotAcquireOK, result)
		if release != nil {
			release()
		}
		require.Equal(t, originGroupID, service.OpenAISchedulingGroupID(c))
	})
}
