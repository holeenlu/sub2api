//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSelectionGroupID(t *testing.T) {
	origin := int64Ptr(1)
	fallback := int64Ptr(2)

	require.Equal(t, origin, SelectionGroupID(nil, origin))
	require.Equal(t, origin, SelectionGroupID(&AccountSelectionResult{}, origin))
	require.Equal(t, fallback, SelectionGroupID(&AccountSelectionResult{SchedulingGroupID: fallback}, origin))
}

// selectionStickyBindCacheStub 记录粘性绑定落进了哪个分组命名空间。
type selectionStickyBindCacheStub struct {
	GatewayCache

	boundGroupID int64
	boundAccount int64
}

func (s *selectionStickyBindCacheStub) SetSessionAccountID(_ context.Context, groupID int64, _ string, accountID int64, _ time.Duration) error {
	s.boundGroupID = groupID
	s.boundAccount = accountID
	return nil
}

// 借来的账号必须绑在实际选号所用分组的命名空间下：写进 API Key 自己分组的
// 命名空间，下次请求会从原分组预取到一个它并不拥有的账号，绑定必然 miss，
// 直到自然过期。
func TestBindSelectionStickySessionUsesSchedulingGroup(t *testing.T) {
	origin := int64Ptr(1)
	fallback := int64Ptr(2)

	cases := []struct {
		name      string
		selection *AccountSelectionResult
		wantGroup int64
	}{
		{"无兜底时用原分组", &AccountSelectionResult{}, 1},
		{"兜底生效时用兜底分组", &AccountSelectionResult{SchedulingGroupID: fallback}, 2},
		{"选号结果缺失时退回原分组", nil, 1},
	}

	t.Run("gateway", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cache := &selectionStickyBindCacheStub{}
				svc := &GatewayService{cache: cache}

				require.NoError(t, svc.BindSelectionStickySession(context.Background(), tc.selection, origin, "session-hash", 42))
				require.Equal(t, tc.wantGroup, cache.boundGroupID)
				require.Equal(t, int64(42), cache.boundAccount)

				cache = &selectionStickyBindCacheStub{}
				svc = &GatewayService{cache: cache}
				require.NoError(t, svc.BindSelectionStickySessionAfterProfitAdmission(context.Background(), tc.selection, origin, "session-hash", 42))
				require.Equal(t, tc.wantGroup, cache.boundGroupID)
			})
		}
	})

	t.Run("openai", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cache := &selectionStickyBindCacheStub{}
				svc := &OpenAIGatewayService{cache: cache}

				require.NoError(t, svc.BindSelectionStickySession(context.Background(), tc.selection, origin, "session-hash", 42))
				require.Equal(t, tc.wantGroup, cache.boundGroupID)
				require.Equal(t, int64(42), cache.boundAccount)

				cache = &selectionStickyBindCacheStub{}
				svc = &OpenAIGatewayService{cache: cache}
				require.NoError(t, svc.BindSelectionStickySessionAfterProfitAdmission(context.Background(), tc.selection, origin, "session-hash", 42))
				require.Equal(t, tc.wantGroup, cache.boundGroupID)
			})
		}
	})
}
