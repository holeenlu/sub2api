//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type catalogAccountRepo struct {
	AccountRepository
	accounts []Account
	byGroup  map[int64][]Account
	calls    []int64
}

func (r *catalogAccountRepo) ListSchedulableByGroupID(ctx context.Context, id int64) ([]Account, error) {
	return r.ListByGroup(ctx, id)
}
func (r *catalogAccountRepo) ListModelAvailabilityCandidates(ctx context.Context, id *int64, _ []string, _ bool) ([]Account, error) {
	return r.ListByGroup(ctx, *id)
}

func (r *catalogAccountRepo) ListByGroup(_ context.Context, id int64) ([]Account, error) {
	r.calls = append(r.calls, id)
	if r.byGroup != nil {
		return r.byGroup[id], nil
	}
	return r.accounts, nil
}

type catalogSnapshotsStub map[int64][]string

func (s catalogSnapshotsStub) ModelCatalogSnapshot(_ context.Context, a *Account) ([]string, time.Time, bool) {
	ids, ok := s[a.ID]
	return ids, time.Now(), ok
}
func catalogAccount(id int64, platform string, mapping map[string]any) Account {
	return Account{ID: id, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": mapping}}
}
func catalogNames(c *GroupModelCatalog) []string {
	var names []string
	for _, m := range c.Models {
		names = append(names, m.Name)
	}
	return names
}

func TestGroupModelCatalogSupportIsIndependentOfPricing(t *testing.T) {
	repo := &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{"free-model": "upstream", "no-price": "unknown", "family-*": "target"})}}
	catalog := &GroupModelCatalogService{accounts: repo}
	group := &Group{ID: 10, Platform: PlatformOpenAI}
	ch := plazaPricedChannel(1, "private-channel", []int64{10}, PlatformOpenAI, "price-only")
	result, err := catalog.resolve(context.Background(), group, []Channel{ch})
	require.NoError(t, err)
	require.Equal(t, []string{"free-model", "no-price"}, catalogNames(result))
	require.Contains(t, result.Issues, GroupCatalogIssue{Model: "price-only", Reason: "pricing_only_or_not_allowed"})
	require.Contains(t, result.Issues, GroupCatalogIssue{Model: "family-*", Reason: "wildcard_requires_concrete_models"})
	// Account mappings remain visible without any channel.
	result, err = catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Len(t, result.Models, 2)
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"no-price", "free-model"}}
	result, err = catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Equal(t, group.ModelAllowlist.Models, catalogNames(result))
	repo.accounts[0].Schedulable = false
	result, err = catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Empty(t, result.Models)
}

func TestGroupModelCatalogUsesChannelRoutingAndWildcardRestriction(t *testing.T) {
	repo := &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{"gpt-target": "gpt-final"})}}
	catalog := &GroupModelCatalogService{accounts: repo}
	group := &Group{ID: 10, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"public"}}}
	ch := plazaPricedChannel(1, "channel", []int64{10}, PlatformOpenAI, "gpt-*")
	ch.ModelMapping = map[string]map[string]string{PlatformOpenAI: {"PUBLIC": "gpt-target"}}
	ch.RestrictModels = true
	for _, tc := range []struct{ source, billing string }{{BillingModelSourceChannelMapped, "gpt-target"}, {BillingModelSourceUpstream, "gpt-final"}, {BillingModelSourceResponse, "gpt-target"}} {
		t.Run(tc.source, func(t *testing.T) {
			ch.BillingModelSource = tc.source
			result, err := catalog.resolve(context.Background(), group, []Channel{ch})
			require.NoError(t, err)
			require.Len(t, result.Models, 1)
			require.Equal(t, []string{tc.billing}, result.Models[0].BillingModels)
			require.Equal(t, tc.source == BillingModelSourceResponse, result.Models[0].ResponseDependent)
		})
	}
	ch.BillingModelSource = BillingModelSourceRequested
	result, err := catalog.resolve(context.Background(), group, []Channel{ch})
	require.NoError(t, err)
	require.Empty(t, result.Models, "requested public name has no permitted pricing")
}

func TestGroupModelCatalogCompositeEndpointsAndAmbiguity(t *testing.T) {
	repo := &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{"ambiguous": "gpt-5", "target": "gpt-5"}), catalogAccount(2, PlatformAnthropic, map[string]any{"ambiguous": "claude-sonnet", "target": "claude-sonnet"})}}
	routes := compositeRouteRepoStub{routes: []CompositeModelRoute{
		{ID: 1, GroupID: 10, PublicModel: "public", UpstreamModel: "target", TargetPlatform: PlatformOpenAI, Endpoint: CompositeRouteEndpointResponses, MatchType: CompositeRouteMatchExact, Enabled: true},
		{ID: 2, GroupID: 10, PublicModel: "public", UpstreamModel: "target", TargetPlatform: PlatformAnthropic, Endpoint: CompositeRouteEndpointMessages, MatchType: CompositeRouteMatchExact, Enabled: true},
	}}
	catalog := &GroupModelCatalogService{accounts: repo, routes: routes}
	result, err := catalog.resolve(context.Background(), &Group{ID: 10, Platform: PlatformComposite}, nil)
	require.NoError(t, err)
	require.Len(t, result.Models, 2)
	require.Equal(t, []string{"public", "public"}, catalogNames(result))
	require.NotEqual(t, result.Models[0].Endpoint, result.Models[1].Endpoint)
	require.NotEqual(t, result.Models[0].BillingModels, result.Models[1].BillingModels)
	require.Contains(t, result.Issues, GroupCatalogIssue{Model: "ambiguous", Reason: "ambiguous_route"})
}

func TestGroupModelCatalogAggregatesAllMembersRegardlessOfPinnedConfig(t *testing.T) {
	repo := &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, nil), catalogAccount(2, PlatformOpenAI, nil)}}
	group := &Group{ID: 10, Platform: PlatformOpenAI}
	group.CodexModelsManifestConfig.Enabled = true
	group.CodexModelsManifestConfig.AccountIDs = []int64{1}
	group.UpdatedAt = time.Now().Add(-time.Hour)
	snapshots := catalogSnapshotsStub{1: {"gpt-5"}, 2: {"gpt-5", "gpt-other"}}
	catalog := &GroupModelCatalogService{accounts: repo, snapshots: snapshots}
	result, err := catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Equal(t, "ready", result.Status)
	require.Equal(t, []string{"gpt-5", "gpt-other"}, catalogNames(result), "every member snapshot contributes to the listing")
	require.Len(t, result.Models[0].AccountModels, 2, "every supporting member stays routable")
	require.True(t, result.UpdatedAt.After(group.UpdatedAt), "fresh discovery must not inherit an old configuration timestamp")

	snapshots[1], snapshots[2] = []string{}, []string{}
	result, err = catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Empty(t, result.Models, "authoritative empty is not default models")
}

func TestModelPlazaSharedCatalogFiltersVisibilityBeforeAccounts(t *testing.T) {
	repo := &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformOpenAI, map[string]any{"unknown-price": "unknown-price"})}}
	groups := []Group{{ID: 10, Platform: PlatformOpenAI, RateMultiplier: 1}, {ID: 20, Platform: PlatformOpenAI, IsExclusive: true, RateMultiplier: 1}}
	plaza := newPlazaServiceWithBilling(nil, groups, nil, nil)
	plaza.catalog = &GroupModelCatalogService{accounts: repo}
	result, err := plaza.ListVisibleGroups(context.Background(), func(g *Group) bool { return !g.IsExclusive })
	require.NoError(t, err)
	require.Equal(t, []int64{10}, repo.calls)
	require.Len(t, result, 1)
	require.Len(t, result[0].Models, 1)
	quote := QuotePlazaModel(&result[0].Models[0], &result[0], nil, false)
	require.Equal(t, "unavailable", quote.Status)
	require.Nil(t, quote.Pricing)
}

func TestModelCatalogSnapshotIsolationExpiryAndAliases(t *testing.T) {
	gateway := &OpenAIGatewayService{}
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"alias": "upstream"})
	response := &OpenAIModelsResponse{Body: []byte(`{"object":"list","data":[{"id":"upstream"},{"id":"unconfigured"}]}`)}
	gateway.rememberModelCatalog(&a, &a, response, false)
	ids, _, ok := gateway.ModelCatalogSnapshot(context.Background(), &a)
	require.True(t, ok)
	require.Equal(t, []string{"alias"}, ids)
	other := catalogAccount(2, PlatformOpenAI, nil)
	_, _, ok = gateway.ModelCatalogSnapshot(context.Background(), &other)
	require.False(t, ok)
	a.Credentials["api_key"] = "changed"
	_, _, ok = gateway.ModelCatalogSnapshot(context.Background(), &a)
	require.False(t, ok)
	gateway.rememberModelCatalog(&a, &a, response, false)
	value, _ := gateway.catalogSnapshots.Load(a.ID)
	old := value.(modelCatalogSnapshot)
	old.UpdatedAt = time.Now().Add(-25 * time.Hour)
	gateway.catalogSnapshots.Store(a.ID, old)
	_, _, ok = gateway.ModelCatalogSnapshot(context.Background(), &a)
	require.False(t, ok)
}

func TestModelCatalogSnapshotIgnoresMetadataButInvalidatesSourceChanges(t *testing.T) {
	ctx := context.Background()
	parent := catalogAccount(100, PlatformOpenAI, nil)
	parent.Type = AccountTypeOAuth
	parent.Credentials["access_token"] = "old-token"
	repo := &stubCredRepo{parent: &parent}
	gateway := &OpenAIGatewayService{accountRepo: repo}
	shadow := catalogAccount(200, PlatformOpenAI, map[string]any{"alias": "upstream"})
	shadow.Type = AccountTypeOAuth
	shadow.ParentAccountID = &parent.ID
	response := &OpenAIModelsResponse{Body: []byte(`{"data":[{"id":"upstream"}]}`)}
	gateway.rememberModelCatalog(&shadow, &parent, response, false)
	shadow.Extra = map[string]any{"upstream_model_metadata": map[string]any{"upstream": "updated"}, "usage": 42}
	parent.Extra = map[string]any{"usage": 99}
	ids, _, ok := gateway.ModelCatalogSnapshot(ctx, &shadow)
	require.True(t, ok, "metadata and usage updates must not invalidate successful discovery")
	require.Equal(t, []string{"alias"}, ids)
	parent.Credentials["access_token"] = "rotated-token"
	_, _, ok = gateway.ModelCatalogSnapshot(ctx, &shadow)
	require.False(t, ok, "parent credential rotation must invalidate shadow discovery")
	gateway.rememberModelCatalog(&shadow, &parent, response, false)
	repo.parent = nil
	_, _, ok = gateway.ModelCatalogSnapshot(ctx, &shadow)
	require.False(t, ok, "missing parent must not reuse a previous source")
	repo.parent = &parent
	shadow.Extra["openai_passthrough"] = true
	_, _, ok = gateway.ModelCatalogSnapshot(ctx, &shadow)
	require.False(t, ok, "passthrough changes alter public model projection")
}

func TestGroupModelCatalogUsesNativeDefaultsButExcludesDisabledImages(t *testing.T) {
	repo := &catalogAccountRepo{accounts: []Account{catalogAccount(1, PlatformGrok, nil)}}
	catalog := &GroupModelCatalogService{accounts: repo}
	result, err := catalog.resolve(context.Background(), &Group{ID: 10, Platform: PlatformGrok}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.Models)
	require.Equal(t, "ready", result.Status)
	repo.accounts = []Account{catalogAccount(2, PlatformOpenAI, map[string]any{"gpt-image-1": "gpt-image-1"})}
	group := &Group{ID: 10, Platform: PlatformOpenAI}
	result, err = catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Empty(t, result.Models)
	group.AllowImageGeneration = true
	result, err = catalog.resolve(context.Background(), group, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-image-1"}, result.ModelIDs())
}

type catalogFallbackGroups struct {
	GroupRepository
	groups map[int64]*Group
}

func (r catalogFallbackGroups) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	return r.groups[id], nil
}

func TestGroupModelCatalogFallbackKeepsOriginAccessAndBilling(t *testing.T) {
	ctx := context.Background()
	groups := noAccountFallbackLinearChain(2, PlatformOpenAI)
	groups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"public", "unpriced"}}
	groups[2].FallbackGroupIDOnNoAccount = int64Ptr(1) // cycle is bounded by the existing chain
	a := catalogAccount(17, PlatformOpenAI, map[string]any{"public": "upstream", "unpriced": "unpriced", "private": "private"})
	accounts := &catalogAccountRepo{byGroup: map[int64][]Account{2: {a}}}
	rootChannel := plazaPricedChannel(1, "origin-prices", []int64{1}, PlatformOpenAI, "public")
	rootChannel.BillingModelSource, rootChannel.RestrictModels = BillingModelSourceRequested, true
	foreignChannel := plazaPricedChannel(2, "target-prices", []int64{2}, PlatformOpenAI, "upstream")
	catalog := &GroupModelCatalogService{groups: catalogFallbackGroups{groups: groups}, accounts: accounts}
	view, err := catalog.resolve(ctx, groups[1], []Channel{rootChannel, foreignChannel})
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, view.ModelIDs())
	require.Equal(t, "fallback", view.Models[0].Source)
	require.Equal(t, "origin-prices", view.Models[0].ChannelName)
	require.Equal(t, []string{"public"}, view.Models[0].BillingModels)
	require.Equal(t, "upstream", view.Models[0].AccountModels[17])
	require.Equal(t, []int64{1, 2}, accounts.calls)
	accounts.byGroup[1] = []Account{catalogAccount(18, PlatformOpenAI, map[string]any{"public": "upstream"})}
	view, err = catalog.resolve(ctx, groups[1], []Channel{rootChannel, foreignChannel})
	require.NoError(t, err)
	require.Equal(t, []string{"public"}, view.ModelIDs(), "origin recovery does not duplicate models")
	require.Len(t, view.Models[0].AccountModels, 2)
	accounts.byGroup[1] = nil
	for _, invalid := range []string{"disabled", "other-platform", "claude-only"} {
		copy := *groups[2]
		switch invalid {
		case "disabled":
			groups[2].Status = StatusDisabled
		case "other-platform":
			groups[2].Platform = PlatformAnthropic
		case "claude-only":
			groups[2].ClaudeCodeOnly = true
		}
		view, err = catalog.resolve(ctx, groups[1], nil)
		require.NoError(t, err)
		require.Empty(t, view.Models, invalid)
		*groups[2] = copy
	}
}

func TestGroupFallbackCodexUsesSupplierHiddenMetadata(t *testing.T) {
	ctx := context.Background()
	groups := noAccountFallbackLinearChain(2, PlatformOpenAI)
	groups[1].ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"vendor-hidden", "vendor-visible"}}
	a := catalogAccount(17, PlatformOpenAI, map[string]any{"vendor-hidden": "vendor-hidden", "vendor-visible": "vendor-visible"})
	no := false
	a.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"vendor-hidden":  {ID: "vendor-hidden", Visibility: "hide", Reasoning: &no, ContextWindow: 100000, InputModalities: []string{"text"}},
		"vendor-visible": {ID: "vendor-visible", Reasoning: &no, ContextWindow: 100000, InputModalities: []string{"text"}},
	}})
	accounts := &catalogAccountRepo{byGroup: map[int64][]Account{2: {a}}}
	channels := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := &GroupModelCatalogService{groups: catalogFallbackGroups{groups: groups}, accounts: accounts, channels: channels}
	registry := &ModelCatalogService{accounts: accounts, groupCatalog: catalog}
	profile, err := registry.SetupProfile(ctx, &APIKey{Group: groups[1]})
	require.NoError(t, err)
	require.Equal(t, "vendor-visible", profile.Model)
	openai := &OpenAIGatewayService{accountRepo: accounts, modelCatalog: registry}
	manifest, configured, err := openai.BuildGroupConfiguredCodexModelsManifest(ctx, groups[1], "")
	require.NoError(t, err)
	require.True(t, configured)
	models := decodeCodexManifestModels(t, manifest.Body)
	require.Len(t, models, 2)
	for _, model := range models {
		if model["slug"] == "vendor-hidden" {
			require.Equal(t, "hide", model["visibility"])
		}
	}
}

func TestGroupModelCatalogSharedCodexPreservesMetadataAndETag(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"alias": "upstream"})
	accounts := splitCodexModelsAccountRepo{schedulable: map[int64][]Account{10: {a}}, catalog: map[int64][]Account{10: {a}}}
	gateway := &OpenAIGatewayService{accountRepo: accounts}
	channels := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := NewGroupModelCatalogService(accounts, channels, nil, gateway)
	group := &Group{ID: 10, Platform: PlatformOpenAI, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: []string{"alias"}}}
	shared, err := catalog.Resolve(context.Background(), group)
	require.NoError(t, err)
	manifest, configured, err := gateway.BuildGroupConfiguredCodexModelsManifest(context.Background(), group, "")
	require.NoError(t, err)
	require.True(t, configured)
	require.Contains(t, string(manifest.Body), `"slug":"alias"`)
	require.Equal(t, []string{"alias"}, shared.ModelIDs())
	unchanged, _, err := gateway.BuildGroupConfiguredCodexModelsManifest(context.Background(), group, manifest.ETag)
	require.NoError(t, err)
	require.True(t, unchanged.NotModified)
	original := &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"alias","custom_metadata":{"preserve":true}},{"slug":"hidden"}],"metadata_version":"keep"}`), ETag: "old"}
	require.NoError(t, gateway.MergeGroupConfiguredCodexModels(context.Background(), group, original, ""))
	require.Contains(t, string(original.Body), `"custom_metadata":{"preserve":true}`)
	require.Contains(t, string(original.Body), `"metadata_version":"keep"`)
	require.NotContains(t, string(original.Body), `"slug":"hidden"`)
	require.NotEqual(t, "old", original.ETag)
	require.NoError(t, gateway.MergeGroupConfiguredCodexModels(context.Background(), group, original, original.ETag))
	require.True(t, original.NotModified)
}
