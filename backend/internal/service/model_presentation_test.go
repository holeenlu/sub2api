//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHiddenModelsCascadeWithoutChangingAccess(t *testing.T) {
	ctx := context.Background()
	upstream, repo, accounts, group := newListingFixture(t)
	ids := []string{"codex-auto-review", "gpt-reserve", "vendor-hidden"}
	group.ModelAllowlist.Models = ids
	for i := range accounts.accounts {
		a := &accounts.accounts[i]
		a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Models: ids}
		snapshot := repo.snapshots[modelCatalogSourceKey(a.ID)]
		base := snapshot.Models[0]
		snapshot.Models = nil
		for _, id := range ids {
			entry := base
			entry.ID, entry.Metadata.ID = id, id
			entry.CodexModel = json.RawMessage(`{"visibility":"hide","model_messages":{"auto_review":{"enabled":true}}}`)
			snapshot.Models = append(snapshot.Models, entry)
			upstream.registry.prices.pricingData[id] = upstream.registry.prices.pricingData["vendor-chat-z"]
		}
	}
	view, caps, err := upstream.ResolveForListing(ctx, group)
	require.NoError(t, err)
	require.ElementsMatch(t, ids, view.ModelIDs())
	entries := []map[string]any{}
	for _, id := range ids {
		require.Equal(t, "hide", caps[id].Visibility)
		if id != "vendor-hidden" {
			require.Equal(t, "background", caps[id].ModelPurpose)
		}
		raw, err := json.Marshal(caps[id])
		require.NoError(t, err)
		entry := map[string]any{}
		require.NoError(t, json.Unmarshal(raw, &entry))
		entry["id"] = id
		entries = append(entries, entry)
	}
	body, err := json.Marshal(map[string]any{"data": entries})
	require.NoError(t, err)
	require.NotContains(t, string(body), "private")
	// Real API-key discovery persists the upstream's safe display metadata.
	downstream, _, account, transport := newCatalogTestService(catalogResponse(200, string(body)))
	account.Schedulable = true
	account.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: ids}}
	snapshot, err := downstream.Refresh(ctx, account.ID, true)
	require.NoError(t, err)
	require.Equal(t, "/v1/models", transport.lastReq.URL.Path)
	require.Len(t, snapshot.Models, 3)
	downstream.prices, downstream.pricingResolver = upstream.registry.prices, upstream.registry.pricingResolver
	downstream.groupCatalog = &GroupModelCatalogService{accounts: downstream.accounts, channels: upstream.channels, snapshots: downstream, registry: downstream}
	manifest, err := downstream.CodexManifest(ctx, group)
	require.NoError(t, err)
	require.EqualValues(t, 3, gjson.GetBytes(manifest, "models.#").Int())
	for _, model := range gjson.GetBytes(manifest, "models").Array() {
		require.Equal(t, "hide", model.Get("visibility").String())
	}
	profile, err := downstream.SetupProfile(ctx, &APIKey{Group: group})
	require.NoError(t, err)
	require.Empty(t, profile.Model, "background-only catalogs cannot generate a main model")
	for _, catalog := range []*GroupModelCatalogService{upstream, downstream.groupCatalog} {
		supplier := account
		if catalog == upstream {
			supplier = &accounts.accounts[0]
		}
		for _, id := range ids {
			admitted, err := catalog.Admit(ctx, group, []string{id})
			require.NoError(t, err)
			require.True(t, CatalogAccountAllowed(admitted, supplier))
		}
		denied := *group
		denied.ModelAllowlist.Models = []string{"other"}
		_, err := catalog.Admit(ctx, &denied, []string{"codex-auto-review"})
		require.Error(t, err, "hidden status is not an authorization bypass")
	}
	account.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Models: []string{"other"}}
	_, err = downstream.groupCatalog.Admit(ctx, group, []string{"gpt-reserve"})
	require.Error(t, err, "account restrictions still apply")
	account.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Models: ids}
	delete(downstream.prices.pricingData, "vendor-hidden")
	_, err = downstream.groupCatalog.Admit(ctx, group, []string{"vendor-hidden"})
	require.Error(t, err, "hidden models still require valid pricing")
}

func TestHiddenManifestContractsSurviveConversionAndSelection(t *testing.T) {
	body := []byte(`{"models":[{"slug":"codex-auto-review","visibility":"hide","model_messages":{"auto_review":{"enabled":true}}},{"slug":"gpt-reserve","visibility":"list"},{"slug":"private-alias","visibility":"list","model_purpose":"background"}]}`)
	merged, _, err := mergeConfiguredCodexModelsManifest(body, nil, []string{"*"}, true)
	require.NoError(t, err)
	require.Contains(t, string(merged), `"auto_review":{"enabled":true}`)
	for _, m := range gjson.GetBytes(merged, "models").Array() {
		require.Equal(t, "hide", m.Get("visibility").String())
	}
	standard, err := standardOpenAIModelsBody(merged, true)
	require.NoError(t, err)
	require.NotContains(t, string(standard), "model_messages")
	_, metadata, err := extractUpstreamModelCatalog(standard, false)
	require.NoError(t, err)
	for _, id := range []string{"codex-auto-review", "gpt-reserve", "private-alias"} {
		require.Equal(t, "hide", metadata[id].Visibility)
		require.Equal(t, "background", metadata[id].ModelPurpose)
	}
	apiKey := newCodexModelsAPIKeyTestAccount("https://cascade.example/v1")
	converted := convertOpenAIModelListToCodexManifestForAccount(standard, apiKey)
	for _, m := range gjson.GetBytes(converted, "models").Array() {
		require.Equal(t, "hide", m.Get("visibility").String())
	}
	filtered, _, err := mergeConfiguredCodexModelsManifest(body, nil, []string{"gpt-reserve"}, true)
	require.NoError(t, err)
	require.EqualValues(t, 1, gjson.GetBytes(filtered, "models.#").Int())
	require.Equal(t, "gpt-reserve", gjson.GetBytes(filtered, "models.0.slug").String())
}

func TestHiddenNamesPreserveUpstreamModelIdentity(t *testing.T) {
	for _, model := range []string{"codex-auto-review", "gpt-reserve"} {
		for _, typ := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
			t.Run(fmt.Sprintf("%s/%s", model, typ), func(t *testing.T) {
				a := &Account{Platform: PlatformOpenAI, Type: typ}
				require.Equal(t, model, normalizeOpenAIModelForUpstream(a, model))
			})
		}
	}
}
