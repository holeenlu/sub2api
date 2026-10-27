//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSuperAdminStandardGroupKeyCreation(t *testing.T) {
	ctx := context.Background()
	for _, role := range []string{RoleSuperAdmin, RoleAdmin, RoleUser, RoleVendorLevel1} {
		t.Run(role, func(t *testing.T) {
			repo, cache := newCreateLimitStubs()
			svc := newCreateLimitService(repo, cache, 0, 0)
			user := &User{ID: 7, Role: role, Status: StatusActive, RestrictPublicGroups: true}
			groups := []Group{{ID: 42, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true}}
			svc.userRepo = &visibilityUserRepo{user: user}
			svc.groupRepo = &keyAccessGroupRepo{groups: groups}
			svc.userSubRepo = &visibilitySubRepo{}
			available, err := svc.GetAvailableGroups(ctx, 7)
			require.NoError(t, err)
			id := int64(42)
			key, err := svc.Create(ctx, 7, CreateAPIKeyRequest{Name: "test", GroupID: &id})
			if role == RoleSuperAdmin {
				require.Len(t, available, 1)
				require.NoError(t, err)
				require.Equal(t, int64(7), key.UserID)
			} else {
				require.Empty(t, available)
				require.Error(t, err)
				require.Empty(t, repo.created)
			}
		})
	}
	svc := &APIKeyService{userSubRepo: &subscriptionBindRepo{}}
	root := &User{ID: 7, Role: RoleSuperAdmin, Status: StatusActive}
	subscription := &Group{ID: 43, SubscriptionType: SubscriptionTypeSubscription}
	require.False(t, svc.canUserBindGroup(ctx, root, subscription), "standard-group management must not bypass subscription eligibility")
	require.False(t, svc.canUserBindGroupInternal(root, subscription, map[int64]bool{}))
}

type keyAccessGroupRepo struct {
	GroupRepository
	groups []Group
}

func (r *keyAccessGroupRepo) ListActive(context.Context) ([]Group, error) { return r.groups, nil }
func (r *keyAccessGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	for i := range r.groups {
		if r.groups[i].ID == id {
			return &r.groups[i], nil
		}
	}
	return nil, ErrGroupNotFound
}

type subscriptionBindRepo struct{ UserSubscriptionRepository }

func (*subscriptionBindRepo) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, ErrSubscriptionNotFound
}

func TestSupplierOperationalModelReferences(t *testing.T) {
	keys := SupplierOperationalCredentialKeys()
	require.Contains(t, keys, AccountModelReferencesKey)
	for _, secret := range []string{"access_token", "refresh_token", "client_id", "api_key", "chatgpt_account_id"} {
		require.NotContains(t, keys, secret)
	}
}

func TestCodexUnifiedCatalogRetainsMigratedWhitelist(t *testing.T) {
	ctx := context.Background()
	owner := int64(17)
	account := Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, VendorID: &owner, Credentials: map[string]any{
		AccountModelReferencesKey: []string{"gpt-6-astra", "gpt-6-luna", "gpt-6.1-sol", "public-a", "public-b"},
		"model_mapping":           map[string]any{"public-a": "gpt-6-astra", "public-b": "gpt-6-luna"},
	}}
	refs := openAIConfiguredCodexModelIDs([]Account{account})
	require.Contains(t, refs, "gpt-6.1-sol")
	require.Contains(t, refs, "public-a")
	settings := NewSettingService(&settingRepoStub{values: map[string]string{}}, nil)
	group := &Group{ID: 9, Platform: PlatformOpenAI}
	gateway := &OpenAIGatewayService{cfg: &config.Config{}, settingService: settings, accountRepo: codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{9: {account}}}}
	manifest, configured, err := gateway.BuildGroupConfiguredCodexModelsManifest(ctx, group, "")
	require.NoError(t, err)
	require.True(t, configured)
	var payload struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	require.NoError(t, json.Unmarshal(manifest.Body, &payload))
	ids := []string{}
	for _, model := range payload.Models {
		ids = append(ids, model.Slug)
	}
	for _, id := range []string{"gpt-6-astra", "gpt-6-luna", "gpt-6.1-sol"} {
		require.Contains(t, ids, id)
	}
	require.NotContains(t, ids, "gpt-6-sol", "unselected models must not become available")
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"gpt-6.1-sol"}}
	manifest, _, err = gateway.BuildGroupConfiguredCodexModelsManifest(ctx, group, "")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(manifest.Body, &payload))
	require.Len(t, payload.Models, 1)
	require.Equal(t, "gpt-6.1-sol", payload.Models[0].Slug)
	policy := modelPolicySeed(PlatformOpenAI)
	row := policy.Models["gpt-6.1-sol"]
	row.Enabled = false
	policy.Models[row.ID] = row
	policy.Version = 1
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	settings.settingRepo.(*settingRepoStub).values[ModelManagementKeyPrefix+PlatformOpenAI] = string(raw)
	_, err = settings.GetModelPolicy(ctx, PlatformOpenAI, true)
	require.NoError(t, err)
	manifest, configured, err = gateway.BuildGroupConfiguredCodexModelsManifest(ctx, group, "")
	require.NoError(t, err)
	require.True(t, configured, "an empty managed catalog must not fall back to upstream")
	require.NoError(t, json.Unmarshal(manifest.Body, &payload))
	require.Empty(t, payload.Models)

}
