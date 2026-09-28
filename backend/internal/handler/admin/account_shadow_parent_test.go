package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestEnrichShadowParentInfo(t *testing.T) {
	pid := int64(100)
	parent := &service.Account{
		ID: 100,
		Credentials: map[string]any{
			"email":                   "owner@example.com",
			"plan_type":               "pro",
			"subscription_expires_at": "2026-12-31T00:00:00Z",
			"chatgpt_account_id":      "acct_123",
		},
		Extra: map[string]any{"privacy_mode": "training_off"},
	}
	parents := map[int64]*service.Account{100: parent}

	shadow := AccountWithConcurrency{Account: &dto.Account{ID: 200, ParentAccountID: &pid}}
	normal := AccountWithConcurrency{Account: &dto.Account{ID: 1}}
	orphan := AccountWithConcurrency{Account: &dto.Account{ID: 201, ParentAccountID: ptrInt64(999)}}
	items := []AccountWithConcurrency{shadow, normal, orphan}

	enrichShadowParentInfo(items, parents)

	require.Equal(t, "owner@example.com", items[0].ParentEmail, "影子回填母账号邮箱")
	require.Equal(t, "pro", items[0].ParentPlanType)
	require.Equal(t, "training_off", items[0].ParentPrivacyMode)
	require.Equal(t, "2026-12-31T00:00:00Z", items[0].ParentSubscriptionExpiresAt)
	require.Equal(t, "acct_123", items[0].ParentChatGPTAccountID)

	require.Empty(t, items[1].ParentEmail, "非影子不回填")
	require.Empty(t, items[2].ParentEmail, "母账号缺失时优雅留空")
}

func ptrInt64(v int64) *int64 { return &v }

func TestEnrichShadowParentRPMKeepsConfiguredValue(t *testing.T) {
	parent := &service.Account{ID: 100, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: map[string]any{"base_rpm": 10}}
	for _, own := range []int{0, 5, 20} {
		limit := own
		items := []AccountWithConcurrency{{Account: &dto.Account{ID: 200, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parent.ID, BaseRPM: &limit}}}
		enrichShadowParentInfo(items, map[int64]*service.Account{parent.ID: parent})
		want := 10
		if own > 0 && own < want {
			want = own
		}
		require.NotNil(t, items[0].EffectiveRPMLimit)
		require.Equal(t, want, *items[0].EffectiveRPMLimit)
		require.Equal(t, own, *items[0].BaseRPM, "displaying inheritance must not rewrite the stored limit")
	}
}
