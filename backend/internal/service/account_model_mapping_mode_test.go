//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestOAuthAutoAliasesPreserveDefaultModelScope(t *testing.T) {
	credentials := map[string]any{"access_token": "test-only"}
	input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: credentials}
	require.True(t, applyOAuthModelMappings(input, []OAuthModelMappingRule{{From: "gpt-5.4", To: "gpt-5.5"}}))
	account := &Account{Platform: input.Platform, Type: input.Type, Credentials: input.Credentials}
	require.True(t, account.IsOpenAIModelMappingAliases())
	for _, model := range []string{"gpt-5.5", "gpt-6-sol", "gpt-6.1-sol", "gpt-6-astra"} {
		require.True(t, account.IsModelSupported(model), model)
	}
	require.Equal(t, "gpt-5.5", account.GetMappedModel("gpt-5.4"))
	require.False(t, account.IsModelSupported("deepseek-chat"))
	require.NotContains(t, credentials, "model_mapping")
	require.NotContains(t, credentials, OpenAIModelMappingModeKey)

	for _, mode := range []string{"whitelist", ""} {
		input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{OpenAIModelMappingModeKey: mode}}
		require.True(t, applyOAuthModelMappings(input, defaultOAuthModelMappings(PlatformOpenAI)))
		account := &Account{Platform: input.Platform, Type: input.Type, Credentials: input.Credentials}
		require.False(t, account.IsModelSupported("gpt-6-sol"), "explicit import scope is never widened")
		require.Equal(t, mode, input.Credentials[OpenAIModelMappingModeKey])
	}
}

func TestOAuthAliasModeRetainsExplicitAllowlistAndCatalog(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.5"}},
		Extra:       map[string]any{"auto_config_initial_revision": "legacy"}}
	require.False(t, account.IsModelSupported("gpt-6-sol"), "retired auto-config markers do not enable aliases")
	account.Credentials[OpenAIModelMappingModeKey] = "aliases"
	require.True(t, account.IsModelSupported("gpt-6-sol"))
	for _, codex := range []bool{false, true} {
		body := []byte(`{"object":"list","data":[{"id":"gpt-6-sol","created":42},{"id":"gpt-5.5"},{"id":"deepseek-chat"}]}`)
		field, idField := "data", "id"
		if codex {
			body = []byte(`{"models":[{"slug":"gpt-6-sol","description":"kept"},{"slug":"gpt-5.5"},{"slug":"deepseek-chat"}]}`)
			field, idField = "models", "slug"
		}
		before := string(body)
		projected, err := projectAccountModelsBody(body, account, nil, codex)
		require.NoError(t, err)
		_, entries, err := modelCatalogEntries(projected, field)
		require.NoError(t, err)
		var ids []string
		for _, raw := range entries {
			var entry map[string]any
			require.NoError(t, json.Unmarshal(raw, &entry))
			ids = append(ids, entry[idField].(string))
		}
		require.ElementsMatch(t, []string{"gpt-6-sol", "gpt-5.5", "gpt-5.4"}, ids)
		allowlist := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}
		require.Equal(t, []string{"gpt-5.4"}, allowlist.FilterForListing(ids))
		require.Equal(t, before, string(body), "cached source catalog must be unchanged")
	}
	models := supplementUnmappedOpenAIModels([]Account{*account}, []string{"gpt-5.4"})
	require.Contains(t, models, "gpt-6.1-sol")
	for _, model := range openai.DefaultModelIDs() {
		require.Contains(t, models, model)
	}

	for _, kind := range []string{AccountTypeAPIKey, AccountTypeSetupToken} {
		copy := *account
		copy.Type = kind
		require.False(t, copy.IsOpenAIModelMappingAliases())
		require.False(t, copy.IsModelSupported("gpt-6-sol"))
	}
	parent := int64(9)
	shadow := *account
	shadow.ParentAccountID = &parent
	require.False(t, shadow.IsOpenAIModelMappingAliases())
	require.False(t, shadow.IsModelSupported("gpt-6-sol"))
}

func TestBulkAliasScopeRepairPreservesExistingMappings(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.5"}}}
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Credentials: map[string]any{OpenAIModelMappingModeKey: "aliases"}})
	require.NoError(t, err)
	require.Equal(t, map[string]any{OpenAIModelMappingModeKey: "aliases"}, repo.lastBulkUpdate.Credentials)
	account.Credentials[OpenAIModelMappingModeKey] = "aliases"
	incoming := map[string]any{"model_mapping": map[string]any{"gpt-6-sol": "gpt-6-sol"}}
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Credentials: incoming})
	require.NoError(t, err)
	require.Equal(t, "whitelist", repo.lastBulkUpdate.Credentials[OpenAIModelMappingModeKey])
	require.NotContains(t, incoming, OpenAIModelMappingModeKey)
}

func TestAccountModelScopeValidationBeforePersistence(t *testing.T) {
	for _, invalid := range []any{true, 42, "all"} {
		require.Error(t, ValidateModelMappingMode(map[string]any{OpenAIModelMappingModeKey: invalid}))
		repo := &updateAccountCredsRepoStub{account: &Account{ID: 1, Platform: PlatformOpenAI,
			Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "test-only"}}}
		svc := &adminServiceImpl{accountRepo: repo}
		_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
			Credentials: map[string]any{OpenAIModelMappingModeKey: invalid}})
		require.Error(t, err)
		require.Zero(t, repo.updateCalls)
	}
}

func TestOAuthAliasScopeFingerprintPreservesLegacyDefaults(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.5"}}}
	legacy := openAITurnRouteFingerprint(account)
	for _, mode := range []string{"", "whitelist"} {
		account.Credentials[OpenAIModelMappingModeKey] = mode
		require.Equal(t, legacy, openAITurnRouteFingerprint(account), "saving a legacy default must preserve the WS route binding")
	}
	account.Credentials[OpenAIModelMappingModeKey] = "aliases"
	require.NotEqual(t, legacy, openAITurnRouteFingerprint(account), "a real admission change still invalidates the binding")
}
