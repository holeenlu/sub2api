//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Existing accounts do not need a new save or a successful discovery request
// before the simplified group selection can route to their declared supply.
func TestModelCatalogLegacyAccountSupplySurvivesMissingDiscovery(t *testing.T) {
	for _, platform := range []string{PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformGrok} {
		t.Run(platform, func(t *testing.T) {
			s, _, a, _ := newCatalogTestService()
			a.Platform = platform
			a.Credentials["model_mapping"] = map[string]any{"public-alias": "native-model"}
			allowed, known := s.ModelIsPublished(context.Background(), a, "native-model")
			require.True(t, known)
			require.True(t, allowed, "an explicit legacy mapping still declares supply")
			allowed, _ = s.ModelIsPublished(context.Background(), a, "not-provided")
			require.False(t, allowed)
			a.Credentials["model_mapping"] = map[string]any{}
			model := "native-model"
			if platform == PlatformGrok {
				// Older Grok accounts retain their built-in routing aliases.
				for id := range a.GetModelMapping() {
					model = id
					break
				}
			}
			allowed, known = s.ModelIsPublished(context.Background(), a, model)
			require.True(t, known)
			require.True(t, allowed, "empty restrictions mean accept all on existing accounts too")
		})
	}
}

func TestModelCatalogEmptyGroupSelectionDoesNotRequireDiscovery(t *testing.T) {
	s, _, _, _ := newCatalogTestService()
	group := &Group{ID: 7, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true}}
	channels := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := &GroupModelCatalogService{accounts: s.accounts, channels: channels, registry: s, snapshots: s}
	view, err := catalog.Resolve(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, "ready", view.Status)
	require.Empty(t, view.Models, "no selected models means an empty public catalog, not a discovery failure")
	require.False(t, group.ModelAllowlist.Allows("anything"))
}

func TestModelCatalogMalformedSelectionNeverBecomesUnrestricted(t *testing.T) {
	for _, raw := range []any{`broken`, map[string]any{"models": 42}, json.RawMessage(`{"models":[false]}`)} {
		a := &Account{Extra: map[string]any{ModelCatalogPolicyExtraKey: raw}}
		require.False(t, a.IsModelSupported("any-model"))
		require.False(t, accountDeclaresModelSupply(a, "any-model"))
	}
}

type selectionCatalogRepo struct{ *catalogMemoryRepo }

func (r *selectionCatalogRepo) ListPlatform(_ context.Context, platform string) ([]ModelCatalogSnapshot, error) {
	var rows []ModelCatalogSnapshot
	for _, snapshot := range r.snapshots {
		if platform == "" || snapshot.Platform == platform {
			rows = append(rows, *snapshot)
		}
	}
	return rows, nil
}

func TestModelCatalogSelectionIncludesMediaWithoutGrantingSupply(t *testing.T) {
	ctx := context.Background()
	s, repo, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"chat-current","reasoning":false,"input_modalities":["text"],"context_window":100000}]}`))
	s.repo = &selectionCatalogRepo{repo}
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	s.prices = &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"vendor-picture-future": {LiteLLMProvider: "openai", Mode: "image_generation"},
		"vendor-movie-future":   {LiteLLMProvider: "openai", Mode: "video_generation"},
		"other-provider":        {LiteLLMProvider: "gemini", Mode: "image_generation"},
		"price-only-chat":       {LiteLLMProvider: "openai", Mode: "chat"},
	}}
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"gpt-image-custom-admin"}}}
	for _, account := range []*Account{a, nil} {
		inventory, err := s.SelectionCatalog(ctx, account, PlatformOpenAI)
		require.NoError(t, err)
		byID := map[string]ModelCatalogEntry{}
		for _, entry := range inventory.Models {
			byID[entry.ID] = entry
		}
		require.Equal(t, "image", byID["vendor-picture-future"].Kind)
		require.Equal(t, "video", byID["vendor-movie-future"].Kind)
		require.Equal(t, "candidate", byID["vendor-picture-future"].Access)
		require.Contains(t, byID, "gpt-image-custom-admin", "bulk inventory includes the configured account supply, not only OAuth chat discovery")
		require.NotContains(t, byID, "other-provider")
		require.NotContains(t, byID, "price-only-chat")
	}
	allowed, _ := s.ModelIsPublished(ctx, a, "vendor-picture-future")
	require.False(t, allowed, "selectable price identity does not grant account supply")
	stored, err := repo.Current(ctx, modelCatalogSourceKey(a.ID))
	require.NoError(t, err)
	require.Len(t, stored.Models, 1, "inventory must never contaminate discovery evidence")
}

func TestModelCatalogFixedSupplyIsIndependentOfCandidateEvidence(t *testing.T) {
	ctx := context.Background()
	s, _, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"chat-current","reasoning":false,"input_modalities":["text"],"context_window":100000}]}`))
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	s.settings = &SettingService{settingRepo: &catalogSettingsMemory{values: map[string]string{}}}
	require.NoError(t, setCatalogRegistryFixture(ctx, s, []ModelCatalogEntry{
		{ID: "gpt-image-selected", Platform: PlatformOpenAI, Kind: "image"},
		{ID: "gpt-image-not-selected", Platform: PlatformOpenAI, Kind: "image"},
		{ID: "gpt-image-retired", Platform: PlatformOpenAI, Kind: "image", Lifecycle: "retired"},
	}))
	a.Credentials["model_mapping"] = map[string]any{"public-picture": "gpt-image-selected", "stale-alias": "gpt-image-not-selected"}
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"public-picture", "sora-selected", "gpt-image-retired"}}}
	require.True(t, s.accountRouteAllowed(ctx, a, "public-picture"), "admin selected alias resolves to a candidate image identity")
	require.True(t, s.accountRouteAllowed(ctx, a, "sora-selected"), "fixed media supply need not appear in a Codex chat manifest")
	require.False(t, s.accountRouteAllowed(ctx, a, "stale-alias"))
	require.False(t, s.accountRouteAllowed(ctx, a, "gpt-image-retired"))
	ids, _, found := s.ModelCatalogSnapshot(ctx, a)
	require.True(t, found)
	require.Contains(t, ids, "public-picture")
	require.Contains(t, ids, "sora-selected")
	require.NotContains(t, ids, "gpt-image-not-selected")
	require.NotContains(t, ids, "gpt-image-retired")
	a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{}
	ids, _, found = s.ModelCatalogSnapshot(ctx, a)
	require.True(t, found)
	require.Contains(t, ids, "public-picture", "empty account selection does not restrict candidates")
	require.NotContains(t, ids, "gpt-image-retired")
	require.True(t, s.accountRouteAllowed(ctx, a, "gpt-image-not-selected"))
}

func TestModelCatalogFixedSupplySurvivesDiscoveryExpiryButNotExplicitRemoval(t *testing.T) {
	ctx := context.Background()
	s, repo, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"gpt-image-before"}]}`), catalogResponse(200, `{"data":[]}`))
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"gpt-image-before", "gpt-image-omitted"}}}
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	_, err = s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	allowed, known := s.ModelIsPublished(ctx, a, "gpt-image-before")
	require.True(t, known)
	require.False(t, allowed, "a model explicitly removed from this account's discovery remains denied")
	repo.snapshots[modelCatalogSourceKey(a.ID)].CheckedAt = time.Now().Add(-8 * 24 * time.Hour)
	allowed, known = s.ModelIsPublished(ctx, a, "gpt-image-omitted")
	require.True(t, known)
	require.True(t, allowed, "fixed declarations do not expire with discovery")
	require.False(t, s.accountRouteAllowed(ctx, a, "gpt-image-blocked"))
	a.Status = StatusDisabled
	allowed, _ = s.ModelIsPublished(ctx, a, "gpt-image-omitted")
	require.False(t, allowed)
}

func TestModelCatalogGroupWhitelistLimitsFixedMediaSupply(t *testing.T) {
	ctx := context.Background()
	s, _, a, _ := newCatalogTestService(catalogResponse(200, `{"data":[{"id":"chat-current","reasoning":false,"input_modalities":["text"],"context_window":100000}]}`))
	_, err := s.Refresh(ctx, a.ID, true)
	require.NoError(t, err)
	a.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"chat-current", "gpt-image-selected", "gpt-image-private", "sora-selected"}}}
	channels := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := &GroupModelCatalogService{accounts: s.accounts, channels: channels, snapshots: s, registry: s}
	s.groupCatalog = catalog
	group := &Group{ID: 101, Platform: PlatformOpenAI, AllowImageGeneration: true, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"chat-current", "gpt-image-selected", "sora-selected", "not-provided"}}}
	view, err := catalog.Resolve(ctx, group)
	require.NoError(t, err)
	require.Equal(t, []string{"chat-current", "gpt-image-selected", "sora-selected"}, view.ModelIDs())
	manifest, err := s.CodexManifest(ctx, group)
	require.NoError(t, err)
	models := decodeCodexManifestModels(t, manifest)
	require.Len(t, models, 1)
	require.Equal(t, "chat-current", models[0]["slug"], "dedicated media models cannot serve as Codex conversation models")
	group.ModelAllowlist.Models = []string{"gpt-image-selected"}
	view, err = catalog.Resolve(ctx, group)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-image-selected"}, view.ModelIDs(), "account supply does not override a group whitelist")

	// Removing account restrictions broadens supply, never group permissions.
	a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{}
	require.True(t, a.IsModelSupported("gpt-image-private"))
	require.True(t, s.accountRouteAllowed(ctx, a, "gpt-image-private"))
	view, err = catalog.Resolve(ctx, group)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-image-selected"}, view.ModelIDs())
	require.False(t, group.ModelAllowlist.Allows("gpt-image-private"))
	group.ModelAllowlist.Models = nil
	view, err = catalog.Resolve(ctx, group)
	require.NoError(t, err)
	require.Empty(t, view.ModelIDs(), "empty group selection still denies all")
	require.False(t, group.ModelAllowlist.Allows("gpt-image-selected"))
}
