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

func TestGetByKeyForAuthCarriesGroupNoAccountFallback(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	target := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name: fmt.Sprintf("no-account-proj-target-%d", suffix), Platform: service.PlatformAnthropic,
		RateMultiplier: 1,
	})
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name: fmt.Sprintf("no-account-proj-group-%d", suffix), Platform: service.PlatformAnthropic,
		RateMultiplier: 1, FallbackGroupIDOnNoAccount: &target.ID,
	})
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email: fmt.Sprintf("no-account-proj-%d@example.com", suffix), Concurrency: 5,
	})
	groupID := group.ID
	keyValue := fmt.Sprintf("sk-no-account-proj-%d", suffix)
	apiKeyRepo := NewAPIKeyRepository(integrationEntClient, integrationDB)
	key := &service.APIKey{UserID: user.ID, GroupID: &groupID, Key: keyValue, Name: "no-account-proj", Status: service.StatusActive}
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
		_, err = integrationDB.ExecContext(ctx, "DELETE FROM groups WHERE id = $1", target.ID)
		require.NoError(t, err)
	})

	got, err := apiKeyRepo.GetByKeyForAuth(ctx, keyValue)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.NotNil(t, got.Group.FallbackGroupIDOnNoAccount)
	require.Equal(t, target.ID, *got.Group.FallbackGroupIDOnNoAccount)
}
