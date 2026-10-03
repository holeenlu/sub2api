//go:build unit

package service

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
	"time"
)

type listingAccounts struct {
	AccountRepository
	accounts []Account
}

func (r *listingAccounts) ListByGroup(context.Context, int64) ([]Account, error) {
	return r.accounts, nil
}
func (r *listingAccounts) ListSchedulableByGroupID(ctx context.Context, id int64) ([]Account, error) {
	return r.ListByGroup(ctx, id)
}
func (r *listingAccounts) ListModelAvailabilityCandidates(ctx context.Context, id *int64, platforms []string, mixed bool) ([]Account, error) {
	return r.accounts, nil
}
func (r *listingAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, ErrAccountNotFound
}

func newListingFixture(t *testing.T) (*GroupModelCatalogService, *catalogMemoryRepo, *listingAccounts, *Group) {
	t.Helper()
	yes := true
	models := []string{"vendor-chat-z", "gpt-image-test", "names-only"}
	accounts := &listingAccounts{}
	repo := &catalogMemoryRepo{snapshots: map[string]*ModelCatalogSnapshot{}}
	for i, typ := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		a := Account{ID: int64(i + 1), Platform: PlatformOpenAI, Type: typ, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "private-key", "access_token": "private-token"}}
		setTestModelWhitelist(&a, models)
		entries := []ModelCatalogEntry{}
		for _, id := range models {
			m := UpstreamModelMetadata{ID: id, ModelKind: "chat", Reasoning: &yes, SupportedReasoningLevels: []string{"low", "high", "max"}, DefaultReasoningLevel: "high", InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, ContextWindow: 200000, MaxContextWindow: 800000, MaxOutputTokens: 16000, Description: "private upstream description", CodexToolCapabilities: map[string]json.RawMessage{"instructions": json.RawMessage(`"private instructions"`)}}
			if i == 1 {
				m.ContextWindow = 100000
				m.MaxContextWindow = 400000
				m.MaxOutputTokens = 8000
				m.InputModalities = []string{"text"}
				m.SupportedReasoningLevels = []string{"low", "high"}
			}
			if id == "gpt-image-test" {
				m = UpstreamModelMetadata{ID: id, ModelKind: "image", InputModalities: []string{"text", "image"}, OutputModalities: []string{"image"}}
			}
			if id == "names-only" {
				m = UpstreamModelMetadata{ID: id, ModelKind: "chat"}
			}
			entries = append(entries, ModelCatalogEntry{ID: id, Platform: PlatformOpenAI, Kind: m.ModelKind, Access: "listed", Lifecycle: "active", Metadata: m, Missing: modelCatalogMissing(m.ModelKind, m)})
		}
		accounts.accounts = append(accounts.accounts, a)
		repo.snapshots[modelCatalogSourceKey(a.ID)] = &ModelCatalogSnapshot{AccountID: a.ID, Platform: a.Platform, ScopeRevision: modelCatalogScope(&a, &a), Status: "ready", CheckedAt: time.Now(), UpdatedAt: time.Now(), Models: entries}
	}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	for _, id := range models {
		prices.pricingData[id] = &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6, ProvidedFields: map[string]bool{"input_cost_per_token": true, "output_cost_per_token": true}}
	}
	registry := NewModelCatalogService(repo, accounts, nil, nil, prices)
	registry.pricingResolver = NewModelPricingResolver(nil, NewBillingService(&config.Config{}, prices))
	channelRepo := &mockChannelRepository{listAllFn: func(context.Context) ([]Channel, error) { return nil, nil }}
	catalog := &GroupModelCatalogService{accounts: accounts, channels: channelRepo, registry: registry}
	group := &Group{ID: 71, Platform: PlatformOpenAI, AllowImageGeneration: true, RateMultiplier: 1, ModelAllowlist: GroupModelAllowlist{Enabled: true, Models: models}}
	return catalog, repo, accounts, group
}

func TestModelListCapabilitiesCascadeThroughAPIKeySync(t *testing.T) {
	catalog, _, _, group := newListingFixture(t)
	view, caps, err := catalog.ResolveForListing(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, group.ModelAllowlist.Models, view.ModelIDs())
	require.Equal(t, int64(100000), caps["vendor-chat-z"].ContextWindow)
	require.Equal(t, int64(400000), caps["vendor-chat-z"].MaxContextWindow)
	require.Equal(t, int64(8000), caps["vendor-chat-z"].MaxOutputTokens)
	require.Equal(t, []string{"low", "high"}, caps["vendor-chat-z"].SupportedReasoningLevels)
	require.Equal(t, []string{"text"}, caps["vendor-chat-z"].InputModalities)
	require.Equal(t, []string{"image"}, caps["gpt-image-test"].OutputModalities)
	require.Nil(t, caps["names-only"].Reasoning)
	entries := []map[string]any{}
	for _, id := range view.ModelIDs() {
		b, e := json.Marshal(caps[id])
		require.NoError(t, e)
		v := map[string]any{}
		require.NoError(t, json.Unmarshal(b, &v))
		v["id"] = id
		v["object"] = "model"
		entries = append(entries, v)
	}
	body, err := json.Marshal(map[string]any{"object": "list", "data": entries})
	require.NoError(t, err)
	require.NotContains(t, string(body), "private")
	// Feed actual API-key discovery/persistence, not a mock of the parser.
	// The unknown row remains covered above; omit it here to avoid models.dev I/O.
	completeBody, err := json.Marshal(map[string]any{"data": entries[:2]})
	require.NoError(t, err)
	downstream, downstreamRepo, a, transport := newCatalogTestService(catalogResponse(200, string(completeBody)))
	a.Schedulable = true
	setTestModelWhitelist(a, group.ModelAllowlist.Models[:2])
	snapshot, err := downstream.Refresh(context.Background(), a.ID, true)
	require.NoError(t, err)
	require.Len(t, snapshot.Models, 2)
	for _, entry := range snapshot.Models {
		require.Empty(t, entry.Missing)
	}
	require.Equal(t, "/v1/models", transport.lastReq.URL.Path)
	// Add a direct OAuth supplier: the enriched API-key account must no longer
	// suppress this model from a mixed downstream pool's Codex manifest.
	oauth := *a
	oauth.ID, oauth.Type = 99, AccountTypeOAuth
	oauth.Credentials = map[string]any{"access_token": "oauth-token"}
	oauthSnapshot := *snapshot
	oauthSnapshot.AccountID = oauth.ID
	oauthSnapshot.ScopeRevision = modelCatalogScope(&oauth, &oauth)
	downstreamRepo.snapshots[modelCatalogSourceKey(oauth.ID)] = &oauthSnapshot
	downstream.accounts = &listingAccounts{accounts: []Account{*a, oauth}}
	downstream.prices = catalog.registry.prices
	downstream.pricingResolver = catalog.registry.pricingResolver
	manifest, err := (&GatewayService{accountRepo: downstream.accounts}).BuildCodexModelsManifestForGroup(context.Background(), group, "", group.ModelAllowlist.Models)
	require.NoError(t, err)
	require.Equal(t, int64(2), gjson.GetBytes(manifest, "models.#").Int(), "native manifest generation retains names without metadata")
	require.True(t, gjson.GetBytes(manifest, `models.#(slug=="vendor-chat-z")`).Exists())
	require.Equal(t, int64(100000), gjson.GetBytes(manifest, `models.#(slug=="vendor-chat-z").context_window`).Int())
}

func TestModelListCapabilitiesDoNotRequireGroupWhitelist(t *testing.T) {
	catalog, _, _, group := newListingFixture(t)
	group.ModelAllowlist = GroupModelAllowlist{}
	view, caps, err := catalog.ResolveForListing(context.Background(), group)
	require.NoError(t, err)
	require.Contains(t, view.ModelIDs(), "vendor-chat-z")
	require.Equal(t, int64(100000), caps["vendor-chat-z"].ContextWindow)
}

func TestModelListCapabilitiesMissingSupplierKeepsNamesAndWithholdsClaims(t *testing.T) {
	for _, failure := range []string{"missing_fields", "expired", "credential_changed", "missing_snapshot"} {
		t.Run(failure, func(t *testing.T) {
			catalog, repo, accounts, group := newListingFixture(t)
			source := repo.snapshots[modelCatalogSourceKey(2)]
			switch failure {
			case "missing_fields":
				source.Models[0].Metadata = UpstreamModelMetadata{}
				source.Models[0].Missing = []string{"reasoning", "input_modalities", "context_window"}
			case "expired":
				source.CheckedAt = time.Now().Add(-48 * time.Hour)
			case "credential_changed":
				accounts.accounts[1].Credentials["api_key"] = "changed-key"
			case "missing_snapshot":
				delete(repo.snapshots, modelCatalogSourceKey(2))
			}
			view, caps, err := catalog.ResolveForListing(context.Background(), group)
			require.NoError(t, err)
			require.Contains(t, view.ModelIDs(), "vendor-chat-z")
			c := caps["vendor-chat-z"]
			require.Nil(t, c.Reasoning)
			require.Empty(t, c.InputModalities)
			require.Zero(t, c.ContextWindow)
		})
	}
}

func TestModelListCapabilitiesRespectAliasGroupAndInactiveAccount(t *testing.T) {
	catalog, repo, accounts, group := newListingFixture(t)
	accounts.accounts[0].Credentials["model_mapping"] = map[string]any{"public-alias": "vendor-chat-z"}
	accounts.accounts[0].Credentials["model_mapping"] = map[string]any{"public-alias": "vendor-chat-z", "vendor-chat-z": "vendor-chat-z"}
	accounts.accounts[1].Schedulable = false
	a := &accounts.accounts[0]
	repo.snapshots[modelCatalogSourceKey(a.ID)].ScopeRevision = modelCatalogScope(a, a)
	group.ModelAllowlist.Models = []string{"public-alias"}
	view, caps, err := catalog.ResolveForListing(context.Background(), group)
	require.NoError(t, err)
	require.Equal(t, []string{"public-alias"}, view.ModelIDs())
	require.Equal(t, int64(200000), caps["public-alias"].ContextWindow)
	require.NotContains(t, caps, "vendor-chat-z")
}

func TestModelListCapabilitiesConflictsAndNonTextModalities(t *testing.T) {
	yes, no := true, false
	shared := UpstreamModelMetadata{Reasoning: &yes, SupportedReasoningLevels: []string{"high"}, InputModalities: []string{"text", "audio"}, OutputModalities: []string{"audio"}, ContextWindow: 100}
	other := shared
	other.Reasoning = &no
	c := intersectModelListCapabilities([]UpstreamModelMetadata{shared, other})
	require.Nil(t, c.Reasoning)
	require.Empty(t, c.SupportedReasoningLevels)
	require.Equal(t, []string{"text", "audio"}, c.InputModalities)
	require.Equal(t, []string{"audio"}, c.OutputModalities)
	require.Equal(t, int64(100), c.ContextWindow)
	other.Reasoning = &yes
	other.SupportedReasoningLevels = []string{"low"}
	require.Nil(t, intersectModelListCapabilities([]UpstreamModelMetadata{shared, other}).Reasoning)
}
