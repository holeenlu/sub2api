//go:build unit

package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 无可用账号兜底借用兜底分组 B 的账号池后，previous_response_id → 账号的粘连
// 必须写在 B 的命名空间下：下一轮续话在 B 跳内按 B 读取，写在 API Key 自己的
// 分组 A 下就是必然 miss，换到别的账号后上游报 previous response not found。
func TestBindHTTPResponseAccountUsesSchedulingGroup(t *testing.T) {
	const (
		originGroupID   = int64(1)
		fallbackGroupID = int64(2)
		responseID      = "resp_fallback_1"
	)
	account := &Account{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	newFixture := func() (*OpenAIGatewayService, OpenAIWSStateStore, *gin.Context) {
		// 不给 Redis：进程内绑定表按分组分命名空间，正是本用例要断言的那一层。
		store := NewOpenAIWSStateStore(nil)
		svc := &OpenAIGatewayService{cfg: newOpenAIWSV2TestConfig(), openaiWSStateStore: store}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		originID := originGroupID
		c.Set("api_key", &APIKey{ID: 9, GroupID: &originID})
		SetOpenAIHTTPResponseOwner(c, 5, 9)
		return svc, store, c
	}

	t.Run("binds in the group the account was scheduled from", func(t *testing.T) {
		ctx := context.Background()
		svc, store, c := newFixture()
		schedulingGroupID := fallbackGroupID
		SetOpenAISchedulingGroup(c, &schedulingGroupID)

		svc.bindHTTPResponseAccount(ctx, c, account, responseID)

		boundInFallback, err := store.GetResponseAccount(ctx, fallbackGroupID, responseID)
		require.NoError(t, err)
		require.Equal(t, account.ID, boundInFallback)

		boundInOrigin, err := store.GetResponseAccount(ctx, originGroupID, responseID)
		require.NoError(t, err)
		require.Zero(t, boundInOrigin)

		// 续话鉴权在请求入口按 API Key 自己的分组查，响应归属不能跟着跳走。
		ownerUserID, _, found, err := store.GetHTTPResponseOwner(ctx, originGroupID, responseID)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, int64(5), ownerUserID)
	})

	t.Run("falls back to the api key group when nothing was scheduled", func(t *testing.T) {
		ctx := context.Background()
		svc, store, c := newFixture()

		svc.bindHTTPResponseAccount(ctx, c, account, responseID)

		bound, err := store.GetResponseAccount(ctx, originGroupID, responseID)
		require.NoError(t, err)
		require.Equal(t, account.ID, bound)
	})
}
