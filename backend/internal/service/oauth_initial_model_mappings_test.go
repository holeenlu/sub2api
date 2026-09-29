//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type oauthInitialModelMappingsRepoStub struct {
	values map[string]string
}

func (r *oauthInitialModelMappingsRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, nil
}
func (r *oauthInitialModelMappingsRepoStub) GetValue(context.Context, string) (string, error) {
	return "", nil
}
func (r *oauthInitialModelMappingsRepoStub) Set(context.Context, string, string) error { return nil }
func (r *oauthInitialModelMappingsRepoStub) GetMultiple(_ context.Context, _ []string) (map[string]string, error) {
	return r.values, nil
}
func (r *oauthInitialModelMappingsRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	if r.values == nil {
		r.values = map[string]string{}
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}
func (r *oauthInitialModelMappingsRepoStub) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}
func (r *oauthInitialModelMappingsRepoStub) Delete(context.Context, string) error { return nil }

func TestOAuthInitialModelMappingsDefaultsAndRoundTrip(t *testing.T) {
	repo := &oauthInitialModelMappingsRepoStub{}
	svc := NewSettingService(repo, nil)
	settings, err := svc.GetOAuthInitialModelMappings(t.Context())
	require.NoError(t, err)
	require.False(t, settings.Enabled, "existing account creation must keep its behavior until enabled")
	require.Equal(t, PlatformOpenAI, settings.Platform)
	require.Equal(t, []OAuthModelMappingRule{{From: "gpt-5.4", To: "gpt-5.5"}}, settings.Rules)

	saved, err := svc.SaveOAuthInitialModelMappings(t.Context(), OAuthInitialModelMappings{
		Enabled: true,
		Rules: []OAuthModelMappingRule{{From: " gpt-5.4* ", To: " gpt-5.5 "}},
	})
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, saved.Platform)
	require.Equal(t, []OAuthModelMappingRule{{From: "gpt-5.4*", To: "gpt-5.5"}}, saved.Rules)

	loaded, err := svc.GetOAuthInitialModelMappings(t.Context())
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
	var raw map[string]any
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyOAuthInitialModelMappings]), &raw))
	require.Equal(t, true, raw["enabled"])
}

func TestValidateOAuthModelMappings(t *testing.T) {
	valid := []OAuthModelMappingRule{{From: "gpt-5.4", To: "gpt-5.5"}, {From: "gpt-4*", To: "gpt-5.5"}}
	require.NoError(t, validateOAuthModelMappings(valid))
	for _, rules := range [][]OAuthModelMappingRule{
		{{From: "", To: "gpt-5.5"}},
		{{From: "gpt 5.4", To: "gpt-5.5"}},
		{{From: "gpt-5.4*foo", To: "gpt-5.5"}},
		{{From: "gpt-5.4", To: "gpt-*"}},
		{{From: "gpt-5.4", To: "gpt-5.5"}, {From: " gpt-5.4 ", To: "other"}},
		{{From: strings.Repeat("a", 257), To: "gpt-5.5"}},
	} {
		require.Error(t, validateOAuthModelMappings(rules))
	}
	overLimit := make([]OAuthModelMappingRule, 101)
	for i := range overLimit {
		overLimit[i] = OAuthModelMappingRule{From: "model-" + strings.Repeat("x", i+1), To: "target"}
	}
	require.Error(t, validateOAuthModelMappings(overLimit))
}

func TestApplyOAuthModelMappingsPreservesExplicitRulesAndCaller(t *testing.T) {
	rules := []OAuthModelMappingRule{{From: "gpt-5.4", To: "gpt-5.5"}, {From: "gpt-4*", To: "gpt-5.5"}}
	existing := map[string]any{"gpt-5.4": "custom", "other": "value"}
	input := &CreateAccountInput{Credentials: map[string]any{"access_token": "token", "model_mapping": existing}}
	require.True(t, applyOAuthModelMappings(input, rules))
	mapping, ok := input.Credentials["model_mapping"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "custom", mapping["gpt-5.4"])
	require.Equal(t, "gpt-5.5", mapping["gpt-4*"])
	_, originalHasNew := existing["gpt-4*"]
	require.False(t, originalHasNew, "caller-owned mappings must not be mutated")

	malformed := &CreateAccountInput{Credentials: map[string]any{"model_mapping": "invalid"}}
	require.False(t, applyOAuthModelMappings(malformed, rules))
	require.Equal(t, "invalid", malformed.Credentials["model_mapping"])
}

func TestOAuthInitialModelMappingsAreOptInAndScoped(t *testing.T) {
	settings := OAuthInitialModelMappings{Enabled: true, Platform: PlatformOpenAI, Rules: defaultOAuthModelMappings(PlatformOpenAI)}
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	svc := &adminServiceImpl{settingService: NewSettingService(&oauthInitialModelMappingsRepoStub{values: map[string]string{SettingKeyOAuthInitialModelMappings: string(raw)}}, nil)}

	openAI := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{}}
	require.NoError(t, svc.applyOAuthInitialModelMappings(t.Context(), openAI))
	require.Equal(t, "gpt-5.5", openAI.Credentials["model_mapping"].(map[string]any)["gpt-5.4"])

	for _, input := range []*CreateAccountInput{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{}},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{}},
	} {
		require.NoError(t, svc.applyOAuthInitialModelMappings(t.Context(), input))
		require.NotContains(t, input.Credentials, "model_mapping")
	}

	disabled := &adminServiceImpl{settingService: NewSettingService(&oauthInitialModelMappingsRepoStub{values: map[string]string{}}, nil)}
	input := &CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{}}
	require.NoError(t, disabled.applyOAuthInitialModelMappings(t.Context(), input))
	require.NotContains(t, input.Credentials, "model_mapping")
}
