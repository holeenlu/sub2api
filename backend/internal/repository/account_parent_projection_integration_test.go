//go:build integration

package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/accountgroup"
	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Positive real-repository control. Run only with MAIN's existing isolated PG
// harness. It does not claim the service authority uses real PG in the other
// fixture; it verifies the exact projection/supply assumptions that it relies on.
func TestNativeDispatchRealAccountScopeAndParentProjection(t *testing.T) {
	ctx := t.Context()
	tx := testEntTx(t)
	client := tx.Client()
	group, err := client.Group.Create().SetName(uuid.NewString()).SetPlatform(service.PlatformOpenAI).SetStatus(service.StatusActive).Save(ctx)
	require.NoError(t, err)
	parent, err := client.Account.Create().SetName(uuid.NewString()).SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
		SetStatus(service.StatusActive).SetSchedulable(true).SetConcurrency(0).
		SetCredentials(map[string]any{"access_token": "review-parent-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339Nano)}).Save(ctx)
	require.NoError(t, err)
	shadow, err := client.Account.Create().SetName(uuid.NewString()).SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
		SetStatus(service.StatusActive).SetSchedulable(true).SetConcurrency(0).SetParentAccountID(parent.ID).SetQuotaDimension("spark").Save(ctx)
	require.NoError(t, err)
	_, err = client.AccountGroup.Create().SetAccountID(shadow.ID).SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(client, tx, nil)
	selected, err := repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{group.ID}, selected.GroupIDs)
	require.Len(t, selected.AccountGroups, 1)
	require.Equal(t, parent.ID, *selected.ParentAccountID)
	require.True(t, access.ResourceInGroup(&group.ID, selected.GroupIDs))
	require.False(t, access.ResourceInGroup(nil, selected.GroupIDs))
	zero := int64(0)
	require.False(t, access.ResourceInGroup(&zero, selected.GroupIDs))

	until := time.Now().Add(time.Hour)
	_, err = client.Account.UpdateOneID(parent.ID).SetTempUnschedulableUntil(until).SetTempUnschedulableReason("shared credential/transport failure").Save(ctx)
	require.NoError(t, err)
	owner, err := repo.GetByID(ctx, parent.ID)
	require.NoError(t, err)
	require.False(t, owner.IsCredentialUsableForShadow())
	require.Equal(t, 0, owner.Concurrency, "GetByID preserves explicit unlimited policy")
	// Ordinary supply validators check existence, not credential cooldown.
	require.NoError(t, repo.ValidateVendorSupply(ctx, owner, nil))
	require.NoError(t, repo.ValidateVendorAuthorizationMode(ctx, owner, nil, service.VendorOpenAIModeNative))

	require.NoError(t, func() error {
		_, deleteErr := client.AccountGroup.Delete().Where(accountgroup.AccountIDEQ(shadow.ID), accountgroup.GroupIDEQ(group.ID)).Exec(ctx)
		return deleteErr
	}())
	selected, err = repo.GetByID(ctx, shadow.ID)
	require.NoError(t, err)
	require.Empty(t, selected.GroupIDs)
	require.Empty(t, selected.AccountGroups)
	require.False(t, access.ResourceInGroup(&group.ID, selected.GroupIDs))
	require.True(t, access.ResourceInGroup(nil, selected.GroupIDs))
	require.False(t, access.ResourceInGroup(&zero, selected.GroupIDs), "explicit zero is never an unbound wildcard")
	require.NoError(t, client.Account.DeleteOneID(parent.ID).Exec(ctx))
	_, err = repo.GetByID(ctx, parent.ID)
	require.ErrorIs(t, err, service.ErrAccountNotFound, "Ent soft delete is already filtered")
}
