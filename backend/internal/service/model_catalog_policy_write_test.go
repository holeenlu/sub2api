//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreateAccountCatalogPolicyIsValidatedBeforePersistence(t *testing.T) {
	for _, models := range [][]string{{"allowed"}, {}} {
		policy := &ModelCatalogPolicy{Mode: "fixed", Models: models}
		account, err := buildAccountForCreate(&CreateAccountInput{
			Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
			Credentials:        map[string]any{"model_mapping": map[string]any{"alias": "allowed", "stale": "stale"}},
			ModelCatalogPolicy: policy,
		}, map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Mode: "legacy"}})
		require.NoError(t, err)
		require.Equal(t, len(models) > 0, account.IsModelSupported("alias"))
		require.False(t, account.IsModelSupported("stale"))
	}
	_, err := buildAccountForCreate(&CreateAccountInput{
		Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		ModelCatalogPolicy: &ModelCatalogPolicy{Mode: "follow"},
	}, map[string]any{modelCatalogVisibilityExtraKey: map[string]any{"expires_at": time.Now().Add(time.Hour)}})
	require.ErrorContains(t, err, "refresh the account catalog")
}

func TestBulkAccountCatalogPolicyPreservesIndividualAliases(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"first-alias": "new", "old": "old"})
	b := catalogAccount(2, PlatformAnthropic, map[string]any{"second-alias": "new"})
	b.Extra = map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Mode: "fixed", Models: []string{"old"}}}
	for _, models := range [][]string{{"new"}, {}} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{&a, &b}}
		svc := &adminServiceImpl{accountRepo: repo}
		result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
			AccountIDs: []int64{1, 2}, ModelCatalogPolicy: &ModelCatalogPolicy{Mode: "fixed", Models: models},
		})
		require.NoError(t, err)
		require.Equal(t, 2, result.Success)
		require.Equal(t, 1, repo.bulkUpdateCalls)
		require.Empty(t, repo.lastBulkUpdate.Credentials, "a whitelist edit must not replace per-account aliases")
		for _, account := range []*Account{&a, &b} {
			copy := *account
			copy.Extra = repo.lastBulkUpdate.Extra
			require.Equal(t, len(models) > 0, copy.IsModelSupported("new"))
			require.False(t, copy.IsModelSupported("old"))
		}
	}
}

func TestBulkAccountCatalogFollowValidatesAllProposedScopesBeforeWrite(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, nil)
	b := catalogAccount(2, PlatformOpenAI, nil)
	for _, account := range []*Account{&a, &b} {
		account.Credentials["base_url"] = "https://provider.example/v1"
		account.Credentials["api_key"] = "test-key"
		account.Extra = map[string]any{modelCatalogVisibilityExtraKey: map[string]any{
			"scope": modelCatalogScope(account, account), "expires_at": time.Now().Add(time.Hour),
		}}
	}
	for _, invalid := range []string{"missing", "expired", "credentials", "proxy"} {
		t.Run(invalid, func(t *testing.T) {
			second := b
			input := &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, ModelCatalogPolicy: &ModelCatalogPolicy{Mode: "follow"}}
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{&a, &second}}
			switch invalid {
			case "missing":
				repo.getByIDsAccounts = []*Account{&a}
			case "expired":
				second.Extra = map[string]any{modelCatalogVisibilityExtraKey: map[string]any{"scope": modelCatalogScope(&b, &b), "expires_at": time.Now().Add(-time.Hour)}}
			case "credentials":
				input.Credentials = map[string]any{"api_key": "new-credential"}
			case "proxy":
				id := int64(77)
				input.ProxyID = &id
			}
			_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), input)
			require.Error(t, err)
			require.Zero(t, repo.bulkUpdateCalls)
		})
	}
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{&a, &b}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2}, ModelCatalogPolicy: &ModelCatalogPolicy{Mode: "follow"},
		Credentials: map[string]any{"model_mapping": map[string]any{"alias": "new"}},
	})
	require.NoError(t, err)
	require.Equal(t, 1, repo.bulkUpdateCalls)
	require.Contains(t, repo.lastBulkUpdate.Extra, ModelCatalogPolicyExtraKey)
	require.Contains(t, repo.lastBulkUpdate.Credentials, "model_mapping")
}

func TestAccountCatalogPolicyPassthroughChecksUnmappedModel(t *testing.T) {
	a := catalogAccount(1, PlatformOpenAI, map[string]any{"alias": "allowed", "allowed": "blocked"})
	a.Extra = map[string]any{"openai_passthrough": true, ModelCatalogPolicyExtraKey: ModelCatalogPolicy{
		Mode: "fixed", Models: []string{"allowed"}, Excluded: []string{"blocked"},
	}}
	require.True(t, a.IsModelSupported("allowed"), "passthrough does not rewrite allowed to blocked")
	require.Contains(t, configuredUpstreamModelsForCapabilitySync(&a), "allowed", "capability discovery must use the same literal model")
	require.False(t, a.IsModelSupported("alias"), "a dormant alias must not authorize a different request name")
	a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Mode: "follow"}
	a.Extra[modelCatalogVisibilityExtraKey] = map[string]any{
		"scope": modelCatalogScope(&a, &a), "expires_at": time.Now().Add(time.Hour), "models": map[string]bool{"allowed": true},
	}
	require.True(t, a.IsModelSupported("allowed"))
	require.False(t, a.IsModelSupported("alias"))
	a.Extra[ModelCatalogPolicyExtraKey] = ModelCatalogPolicy{Mode: "legacy"}
	require.True(t, a.IsModelSupported("alias"), "legacy passthrough retains its previous behavior")
}

func TestDedicatedAccountCatalogPolicyUsesSameVisibilityValidation(t *testing.T) {
	svc, _, account, _ := newCatalogTestService()
	follow := ModelCatalogPolicy{Mode: "follow"}
	require.Error(t, svc.SaveAccountPolicy(context.Background(), account.ID, follow))
	account.Extra = map[string]any{modelCatalogVisibilityExtraKey: map[string]any{
		"scope": modelCatalogScope(account, account), "expires_at": time.Now().Add(time.Hour), "models": map[string]bool{"new": true},
	}}
	require.NoError(t, svc.SaveAccountPolicy(context.Background(), account.ID, follow))
	account.Credentials["api_key"] = "replaced"
	require.Error(t, svc.SaveAccountPolicy(context.Background(), account.ID, follow))
	require.False(t, account.IsModelSupported("new"))
}

func TestDuplicateAccountCatalogPolicyNeverBroadensAccess(t *testing.T) {
	for _, mode := range []string{"fixed", "follow"} {
		t.Run(mode, func(t *testing.T) {
			repo := newDuplicateAccountRepoStub()
			source := &Account{Name: "source", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "model_mapping": map[string]any{"stale": "stale"}},
				Extra:       map[string]any{ModelCatalogPolicyExtraKey: ModelCatalogPolicy{Mode: mode, Models: []string{}}, modelCatalogVisibilityExtraKey: map[string]any{"models": map[string]bool{"stale": true}}},
			}
			require.NoError(t, repo.Create(context.Background(), source))
			copy, err := (&adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}).DuplicateAccount(context.Background(), source.ID, "admin:1", "")
			require.NoError(t, err)
			require.Equal(t, mode, accountModelCatalogPolicy(copy).Mode)
			require.NotContains(t, copy.Extra, modelCatalogVisibilityExtraKey)
			require.False(t, copy.IsModelSupported("stale"))
		})
	}
}
