//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexCatalogInvalidEvidenceNeverRestoresDefaultTrue(t *testing.T) {
	for _, shape := range []string{"models", "data"} {
		for _, field := range []string{"supports_search_tool", "use_responses_lite", "supports_reasoning_effort_updates"} {
			for _, value := range []any{nil, "true", 1, map[string]any{"enabled": true}, []bool{true}} {
				t.Run(fmt.Sprintf("%s/%s/%v", shape, field, value), func(t *testing.T) {
					account := newCodexModelsAPIKeyTestAccount("https://api.openai.com")
					entry := map[string]any{field: value}
					if shape == "models" {
						entry["slug"] = "gpt-6-astra"
					} else {
						entry["id"] = "gpt-6-astra"
					}
					body, err := json.Marshal(map[string]any{shape: []any{entry}})
					require.NoError(t, err)
					manifest := &OpenAIModelsResponse{Body: convertOpenAIModelListToCodexManifestForAccount(body, account)}
					require.NoError(t, (&OpenAIGatewayService{}).CompleteAPIKeyCodexModelsManifestForClient(manifest, account))
					model := decodeCodexManifestModels(t, manifest.Body)[0]
					require.Contains(t, model, field)
					require.Nil(t, model[field], "explicit unknown must not become local true or false")
				})
			}
		}
	}
}

func TestCodexCatalogStoredInvalidEvidenceDoesNotMutateAccount(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://api.openai.com")
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"gpt-6-astra": {ID: "gpt-6-astra", CodexToolCapabilities: map[string]json.RawMessage{"supports_search_tool": json.RawMessage("1")}},
	}})
	before, err := json.Marshal(account.Extra)
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{"data": []any{map[string]any{"id": "gpt-6-astra"}}})
	require.NoError(t, err)
	manifest := &OpenAIModelsResponse{Body: convertOpenAIModelListToCodexManifestForAccount(body, account)}
	require.NoError(t, (&OpenAIGatewayService{}).CompleteAPIKeyCodexModelsManifestForClient(manifest, account))
	require.Nil(t, decodeCodexManifestModels(t, manifest.Body)[0]["supports_search_tool"])
	after, err := json.Marshal(account.Extra)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "normalization owns a detached projection")
}

func TestCodexCatalogCapabilityEvidenceConsumer(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      any
	}{
		{"supported", "true", true}, {"unsupported", "false", false}, {"unknown", "null", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
			account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
				"custom": {ID: "custom", CodexToolCapabilities: map[string]json.RawMessage{"supports_search_tool": json.RawMessage(tc.raw)}},
			}})
			manifest := &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"custom"}]}`)}
			svc := &OpenAIGatewayService{}
			require.NoError(t, svc.CompleteAPIKeyCodexModelsManifestForClient(manifest, account))
			model := decodeCodexManifestModels(t, manifest.Body)[0]
			require.Equal(t, tc.want, model["supports_search_tool"])
		})
	}
}

func TestCodexCatalogCompletionCannotInheritOldCredentialCapabilities(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://upstream.example")
	account.SetUpstreamModelMetadataSnapshot(UpstreamModelMetadataSnapshot{Models: map[string]UpstreamModelMetadata{
		"custom": {ID: "custom", CodexToolCapabilities: map[string]json.RawMessage{
			"supports_search_tool": json.RawMessage("true"), "experimental_supported_tools": json.RawMessage(`["client_tool"]`),
		}},
	}})
	account.Credentials["api_key"] = "new-identity"
	for _, body := range []string{`{"models":[{"slug":"custom"}]}`, `{"object":"list","data":[{"id":"custom"}]}`} {
		manifest := &OpenAIModelsResponse{Body: convertOpenAIModelListToCodexManifestForAccount([]byte(body), account)}
		svc := &OpenAIGatewayService{}
		require.NoError(t, svc.CompleteAPIKeyCodexModelsManifestForClient(manifest, account))
		model := decodeCodexManifestModels(t, manifest.Body)[0]
		require.NotEqual(t, true, model["supports_search_tool"])
		require.Empty(t, model["experimental_supported_tools"])
		require.True(t, account.IsModelSupported("custom"), "unknown capability is not model denial")
	}
	live := &OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"custom","supports_search_tool":true}]}`)}
	require.NoError(t, (&OpenAIGatewayService{}).CompleteAPIKeyCodexModelsManifestForClient(live, account))
	require.Equal(t, true, decodeCodexManifestModels(t, live.Body)[0]["supports_search_tool"], "current live evidence wins")
}
