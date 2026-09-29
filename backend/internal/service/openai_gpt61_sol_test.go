//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGPT61SolConfiguredCatalogCapabilities(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		t.Run(accountType, func(t *testing.T) {
			account := Account{Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"},
			}}
			body, err := buildCodexModelsManifestForAccounts(PlatformOpenAI, []string{"gpt-6.1-sol"}, []Account{account}, nil, nil, true)
			require.NoError(t, err)
			models := decodeCodexManifestModels(t, body)
			require.Len(t, models, 1)
			model := models[0]
			require.Equal(t, "GPT-6.1 Sol", model["display_name"])
			require.Contains(t, model["description"], "GPT-6.1 Sol")
			require.Equal(t, []any{"text", "image"}, model["input_modalities"])
			require.Equal(t, []string{"low", "medium", "high", "xhigh", "max"}, effortsFromManifestModel(t, model))
			require.Equal(t, "medium", model["default_reasoning_level"])
			require.Equal(t, float64(1_050_000), model["context_window"])
			require.Equal(t, float64(1_050_000), model["max_context_window"])
			require.Equal(t, true, model["supports_search_tool"])
			require.Equal(t, "freeform", model["apply_patch_tool_type"])
			require.Equal(t, false, model["use_responses_lite"])
			require.Nil(t, model["multi_agent_version"], "do not invent an Ultra workflow")
			messages := model["model_messages"].(map[string]any)
			require.Contains(t, messages["instructions_template"], "based on GPT-6")
			require.NotContains(t, messages["instructions_template"], "based on GPT-5")
		})
	}
}

func TestGPT61SolManifestKeepsProviderMetadata(t *testing.T) {
	account := newCodexModelsAPIKeyTestAccount("https://relay.example/v1")
	body := []byte(`{"models":[{"slug":"gpt-6.1-sol","description":"Provider-specific Sol","input_modalities":["text"],"supported_reasoning_levels":[{"effort":"high"}],"default_reasoning_level":"high","context_window":64000,"max_context_window":128000,"model_messages":{"instructions_template":"Provider instructions"}}]}`)
	complete, err := completeAPIKeyCodexModelsManifestMetadata(body, true, account)
	require.NoError(t, err)
	model := decodeCodexManifestModels(t, complete)[0]
	require.Equal(t, "Provider-specific Sol", model["description"])
	require.Equal(t, []any{"text"}, model["input_modalities"])
	require.Equal(t, []string{"high"}, effortsFromManifestModel(t, model))
	require.Equal(t, float64(64000), model["context_window"])
	require.Equal(t, "Provider instructions", model["model_messages"].(map[string]any)["instructions_template"])
}

func TestGPT61SolOfficialPricingAndModelIdentity(t *testing.T) {
	for _, pricing := range []*PricingService{nil, newStubPricingServiceFromJSON(t, `{"gpt-5.5":{"input_cost_per_token":0.000005,"output_cost_per_token":0.00003}}`)} {
		billing := NewBillingService(&config.Config{}, pricing)
		for _, id := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol", "GPT-6.1_SOL", "gpt-6.1-sol-max"} {
			require.Equal(t, "gpt-6.1-sol", normalizeKnownOpenAICodexModel(id))
			p, err := billing.GetModelPricing(id)
			require.NoError(t, err, id)
			require.InDelta(t, 2e-6, p.InputPricePerToken, 1e-12, id)
			require.InDelta(t, 10e-6, p.OutputPricePerToken, 1e-12, id)
			require.InDelta(t, 0.1e-6, p.CacheReadPricePerToken, 1e-12, id)
			require.InDelta(t, 2.5e-6, p.CacheCreationPricePerToken, 1e-12, id)
		}
		for _, tier := range []struct {
			name  string
			scale float64
		}{{"", 1}, {"priority", 2}, {"flex", 0.5}} {
			for _, input := range []int{272_000, 272_001} {
				cost, err := billing.CalculateCostWithServiceTier("gpt-6.1-sol", UsageTokens{InputTokens: input, OutputTokens: 10}, 0.5, tier.name)
				require.NoError(t, err)
				inScale, outScale := 1.0, 1.0
				if input > 272_000 {
					inScale, outScale = 2, 1.5
				}
				want := (float64(input)*2e-6*inScale + 10*10e-6*outScale) * tier.scale * 0.5
				require.InDelta(t, want, cost.ActualCost, 1e-10)
			}
		}
	}
	for _, id := range []string{"gpt-6.1-solitude", "gpt-6.1-unknown", "gpt-6.1-sol-preview"} {
		require.False(t, isOpenAIGPT6Model(id), id)
	}
}

func TestGPT61SolDynamicCatalogCacheWriteFallbackPreservesExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		want  float64
	}{
		{"missing", "", 2.5e-6},
		{"explicit_zero", `,"cache_creation_input_token_cost":0`, 0},
		{"explicit_price", `,"cache_creation_input_token_cost":0.000004`, 4e-6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := newStubPricingServiceFromJSON(t, `{"gpt-6.1-sol":{"input_cost_per_token":0.000002,"output_cost_per_token":0.00001`+tc.field+`}}`)
			billing := NewBillingService(&config.Config{}, catalog)
			p, err := billing.GetModelPricing("gpt-6.1-sol")
			require.NoError(t, err)
			require.InDelta(t, tc.want, p.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, tc.want*2, p.CacheCreationPricePerTokenPriority, 1e-12)
		})
	}
}
