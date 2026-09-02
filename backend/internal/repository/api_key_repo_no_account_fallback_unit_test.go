package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 无可用账号兜底分组由调度层在选号失败时从认证快照读取。GetByKeyForAuth 的
// 分组投影是显式列清单，漏列会让配置静默失效（选号永远不兜底），因此在这里对账。
func TestGroupEntityToService_PreservesNoAccountFallbackGroupID(t *testing.T) {
	fallbackID := int64(42)
	got := groupEntityToService(&dbent.Group{
		ID:                         1,
		Name:                       "anthropic-primary",
		Platform:                   service.PlatformAnthropic,
		Status:                     service.StatusActive,
		SubscriptionType:           service.SubscriptionTypeStandard,
		RateMultiplier:             1,
		FallbackGroupIDOnNoAccount: &fallbackID,
	})
	require.NotNil(t, got)
	require.NotNil(t, got.FallbackGroupIDOnNoAccount)
	require.Equal(t, fallbackID, *got.FallbackGroupIDOnNoAccount)
}

func TestAPIKeyRepository_GetByKeyForAuth_PreservesNoAccountFallbackGroupID_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-no-account-fallback@test.com")

	fallbackGroup, err := client.Group.Create().
		SetName("g-auth-no-account-fallback-target").
		SetPlatform(service.PlatformAnthropic).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)

	group, err := client.Group.Create().
		SetName("g-auth-no-account-fallback-source").
		SetPlatform(service.PlatformAnthropic).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetFallbackGroupIDOnNoAccount(fallbackGroup.ID).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-no-account-fallback",
		Name:    "No Account Fallback Key",
		GroupID: &group.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.NotNil(t, got.Group.FallbackGroupIDOnNoAccount)
	require.Equal(t, fallbackGroup.ID, *got.Group.FallbackGroupIDOnNoAccount)
}
