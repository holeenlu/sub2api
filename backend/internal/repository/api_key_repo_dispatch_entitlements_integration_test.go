//go:build integration

package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyGetByIDCarriesFreshEntitlements(t *testing.T) {
	for _, exclusive := range []bool{false, true} {
		name := "restricted_public"
		if exclusive {
			name = "exclusive"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			tx := testEntTx(t)
			client := tx.Client()
			user, err := client.User.Create().SetEmail(uuid.NewString() + "@fake.invalid").
				SetPasswordHash("synthetic-password").SetRole(service.RoleUser).
				SetStatus(service.StatusActive).SetRestrictPublicGroups(!exclusive).Save(ctx)
			require.NoError(t, err)
			group, err := client.Group.Create().SetName(uuid.NewString()).SetPlatform(service.PlatformOpenAI).
				SetStatus(service.StatusActive).SetIsExclusive(exclusive).Save(ctx)
			require.NoError(t, err)
			_, err = client.User.UpdateOneID(user.ID).AddAllowedGroupIDs(group.ID).Save(ctx)
			require.NoError(t, err)
			repo := newAPIKeyRepositoryWithSQL(client, tx)
			key := &service.APIKey{UserID: user.ID, GroupID: &group.ID, Key: uuid.NewString(), Name: "fresh-dispatch-scope", Status: service.StatusActive}
			require.NoError(t, repo.Create(ctx, key))
			allowed := func(got *service.APIKey) bool {
				require.NotNil(t, got.User)
				require.NotNil(t, got.Group)
				return access.APIKeyGroupAllowed(true, &access.GroupBinding{
					Vendor: got.User.IsVendor(), RestrictPublicGroups: got.User.RestrictPublicGroups,
					AllowedGroups: got.User.AllowedGroups,
				}, &access.Group{ID: got.Group.ID, Status: got.Group.Status, IsExclusive: got.Group.IsExclusive})
			}
			auth, err := repo.GetByKeyForAuth(ctx, key.Key)
			require.NoError(t, err)
			require.True(t, allowed(auth), "the authenticated principal has a live grant")
			fresh, err := repo.GetByID(ctx, key.ID)
			require.NoError(t, err)
			require.Equal(t, auth.User.AllowedGroups, fresh.User.AllowedGroups)
			require.True(t, allowed(fresh), "fresh dispatch lookup must preserve valid entitlements")
			_, err = client.User.UpdateOneID(user.ID).RemoveAllowedGroupIDs(group.ID).Save(ctx)
			require.NoError(t, err)
			fresh, err = repo.GetByID(ctx, key.ID)
			require.NoError(t, err)
			require.Empty(t, fresh.User.AllowedGroups)
			require.False(t, allowed(fresh), "subsequent lookup must observe actual revocation")
		})
	}
}
