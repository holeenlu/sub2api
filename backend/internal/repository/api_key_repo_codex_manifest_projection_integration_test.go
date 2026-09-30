//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetByKeyForAuthCarriesGroupCodexModelsManifestConfig(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name: fmt.Sprintf("codex-manifest-proj-group-%d", suffix), Platform: service.PlatformOpenAI,
		RateMultiplier: 1,
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{
			Enabled:             true,
			AccountIDs:          []int64{11, 22},
			FallbackToScheduler: true,
		},
	})
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("codex-manifest-proj-%d@example.com", suffix), Concurrency: 5,
	})
	groupID := group.ID
	keyValue := fmt.Sprintf("sk-codex-manifest-proj-%d", suffix)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	key := &service.APIKey{UserID: user.ID, GroupID: &groupID, Key: keyValue, Name: "codex-manifest-proj", Status: service.StatusActive}
	require.NoError(t, apiKeyRepo.Create(ctx, key))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')", keyValue)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id = $1", key.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", group.ID)
		require.NoError(t, err)
	})

	got, err := apiKeyRepo.GetByKeyForAuth(ctx, keyValue)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	// 固定目录来源账号已退役：投影保留历史账号 ID 供审计，但读取时一律不启用。
	require.False(t, got.Group.CodexModelsManifestConfig.Enabled)
	require.Equal(t, []int64{11, 22}, got.Group.CodexModelsManifestConfig.AccountIDs, "codex_models_manifest_config 仍须进入认证投影")
	require.False(t, got.Group.CodexModelsManifestConfig.FallbackToScheduler)
}
