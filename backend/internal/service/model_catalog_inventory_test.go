//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type inventoryMemoryRepo struct {
	*selectionCatalogRepo
	settings *catalogSettingsMemory
}

func (r *inventoryMemoryRepo) UpdateRegistry(ctx context.Context, update func([]ModelCatalogEntry) ([]ModelCatalogEntry, error)) error {
	var entries []ModelCatalogEntry
	raw, _ := r.settings.GetValue(ctx, ModelCatalogRegistryKey)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return err
		}
	}
	entries, err := update(entries)
	if err != nil {
		return err
	}
	rawBytes, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	return r.settings.Set(ctx, ModelCatalogRegistryKey, string(rawBytes))
}
func TestModelCatalogInventoryMaintenanceNeverGrantsAccessOrLosesDisabledState(t *testing.T) {
	ctx := context.Background()
	s, repo, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"new-chat","model_kind":"chat","reasoning":false,"context_window":100000,"input_modalities":["text"]}]}`))
	settings := &catalogSettingsMemory{values: map[string]string{}}
	s.settings = &SettingService{settingRepo: settings}
	s.repo = &inventoryMemoryRepo{selectionCatalogRepo: &selectionCatalogRepo{repo}, settings: settings}
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"new-chat"}}}
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	require.NoError(t, s.SaveInventoryModel(ctx, CatalogModelInput{ID: "future-image", Platform: PlatformOpenAI, Kind: "image", DisplayName: "Future picture"}))
	allowed, _ := s.ModelIsPublished(ctx, a, "future-image")
	require.False(t, allowed, "editing the inventory cannot expand account supply")
	require.NoError(t, s.SaveInventoryModel(ctx, CatalogModelInput{ID: "new-chat", Platform: PlatformOpenAI, Kind: "chat", DisplayName: "Curated name", Disabled: true}))
	inventory, err := s.Inventory(ctx, PlatformOpenAI)
	require.NoError(t, err)
	require.Len(t, inventory.Models, 2)
	for _, model := range inventory.Models {
		if model.ID == "new-chat" {
			require.True(t, model.Disabled)
			require.Equal(t, "Curated name", model.DisplayName)
		}
	}
	for _, account := range []*Account{nil, a} {
		candidates, err := s.SelectionCatalog(ctx, account, PlatformOpenAI)
		require.NoError(t, err)
		require.Len(t, candidates.Models, 1)
		require.Equal(t, "future-image", candidates.Models[0].ID)
	}
	allowed, _ = s.ModelIsPublished(ctx, a, "new-chat")
	require.True(t, allowed, "inventory maintenance cannot revoke a saved group/account selection")
	stored, err := repo.Current(ctx, modelCatalogSourceKey(a.ID))
	require.NoError(t, err)
	require.Len(t, stored.Models, 1)
	require.False(t, stored.Models[0].Disabled, "the maintained overlay stays separate from upstream facts")
	// Editing a different entry preserves the disable overlay on a discovered model.
	require.NoError(t, s.SaveInventoryModel(ctx, CatalogModelInput{ID: "future-image", Platform: PlatformOpenAI, Kind: "image", DisplayName: "Updated"}))
	restarted := NewModelCatalogService(s.repo, s.accounts, nil, s.settings, nil)
	candidates, err := restarted.SelectionCatalog(ctx, nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Len(t, candidates.Models, 1)
	require.Equal(t, "Updated", candidates.Models[0].DisplayName)
	require.NoError(t, s.SaveInventoryModel(ctx, CatalogModelInput{ID: "new-chat", Platform: PlatformOpenAI, Kind: "chat"}))
	candidates, err = s.SelectionCatalog(ctx, nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Len(t, candidates.Models, 2)
}
func TestModelCatalogInventoryRejectsInvalidIdentityWithoutWriting(t *testing.T) {
	s := &ModelCatalogService{}
	for _, input := range []CatalogModelInput{
		{ID: "*", Platform: PlatformOpenAI, Kind: "chat"},
		{ID: "future", Platform: "missing", Kind: "chat"},
		{ID: "future", Platform: PlatformOpenAI, Kind: "wrong"},
	} {
		require.Error(t, s.SaveInventoryModel(context.Background(), input))
	}
}
