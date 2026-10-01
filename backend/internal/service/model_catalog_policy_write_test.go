//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateAccountCatalogPolicyIsValidatedBeforePersistence(t *testing.T) {
	for _, models := range [][]string{{"allowed"}, {}} {
		policy := &ModelCatalogPolicy{Models: models}
		account, err := buildAccountForCreate(&CreateAccountInput{
			Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials:        map[string]any{"model_mapping": map[string]any{"alias": "allowed", "stale": "stale"}},
			ModelCatalogPolicy: policy,
		}, map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{}})
		require.NoError(t, err)
		require.True(t, account.IsModelSupported("alias"))
		require.Equal(t, len(models) == 0, account.IsModelSupported("stale"))
	}
	_, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		ModelCatalogPolicy: &ModelCatalogPolicy{Models: []string{"bad\nmodel"}},
	}, nil)
	require.ErrorContains(t, err, "invalid model pattern")
}

func TestBulkAccountCatalogPolicyPreservesIndividualAliases(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"first-alias": "new", "old": "old"})
	b := catalogAccount(2, PlatformAnthropic, map[string]any{"second-alias": "new"})
	b.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Models: []string{"old"}}}
	for _, models := range [][]string{{"new"}, {}} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{&a, &b}}
		svc := &adminServiceImpl{accountRepo: repo}
		result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
			AccountIDs: []int64{1, 2}, ModelCatalogPolicy: &ModelCatalogPolicy{Models: models},
		})
		require.NoError(t, err)
		require.Equal(t, 2, result.Success)
		require.Equal(t, 1, repo.bulkUpdateCalls)
		require.Empty(t, repo.lastBulkUpdate.Credentials, "a whitelist edit must not replace per-account aliases")
		for _, account := range []*Account{&a, &b} {
			copy := *account
			copy.Extra = repo.lastBulkUpdate.Extra
			require.True(t, copy.IsModelSupported("new"))
			require.Equal(t, len(models) == 0, copy.IsModelSupported("old"))
		}
	}
}

func TestAccountCatalogPolicyPassthroughChecksUnmappedModel(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"alias": "allowed", "allowed": "blocked"})
	a.Extra = map[string]any{"openai_passthrough": true, ModelCatalogPolicyExtraKey: ModelCatalogPolicy{
		Models: []string{"allowed"},
	}}
	require.True(t, a.IsModelSupported("allowed"), "passthrough does not rewrite allowed to blocked")
	require.Contains(t, configuredUpstreamModelsForCapabilitySync(&a), "allowed", "capability discovery must use the same literal model")
	require.False(t, a.IsModelSupported("alias"), "a dormant alias must not authorize a different request name")
	a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Models: []string{}}
	require.True(t, a.IsModelSupported("alias"), "empty selection accepts every literal passthrough request")
}

func TestDuplicateAccountCatalogPolicyNeverBroadensAccess(t *testing.T) {
	for _, mode := range []string{"fixed", "follow"} {
		t.Run(mode, func(t *testing.T) {
			repo := newDuplicateAccountRepoStub()
			source := &Account{Name: "source", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "model_mapping": map[string]any{"stale": "stale"}},
				Extra:       map[string]any{ModelCatalogPolicyExtraKey: map[string]any{"mode": mode, "models": []string{"allowed"}}, "model_catalog_visibility": map[string]any{"models": map[string]bool{"stale": true}}},
			}
			require.NoError(t, repo.Create(context.Background(), source))
			copy, err := (&adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}).DuplicateAccount(context.Background(), source.ID, "admin:1", "")
			require.NoError(t, err)
			require.Equal(t, []string{"allowed"}, accountModelCatalogPolicy(copy).Models)
			require.NotContains(t, copy.Extra, "model_catalog_visibility")
			require.Equal(t, source.IsModelSupported("stale"), copy.IsModelSupported("stale"))
		})
	}
}

func TestRetiredModelModesUseOnlySavedSelections(t *testing.T) {
	for _, mode := range []string{"fixed", "follow"} {
		a := catalogAccount(1, PlatformOpenAI, nil)
		a.Extra = map[string]any{ModelCatalogPolicyExtraKey: map[string]any{"mode": mode, "models": []string{"selected"}, "excluded": []string{"selected"}}}
		require.True(t, a.IsModelSupported("selected"))
		require.False(t, a.IsModelSupported("newly-discovered"))
		a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Models: []string{}}
		require.True(t, a.IsModelSupported("newly-discovered"), "empty account list accepts all")
	}
}
