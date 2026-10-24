package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func cloudCatalogHistoryFixture(t *testing.T) (map[string]map[string]any, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("testdata/cloud_codex_catalog_history.json")
	require.NoError(t, err)
	var fixture struct {
		Requests            map[string]map[string]any
		History             map[string]any
		ConfigurationUpdate map[string]any `json:"configuration_update"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	fixture.History["configuration_update"] = fixture.ConfigurationUpdate
	return fixture.Requests, fixture.History
}

// Exercise the final directory used by both pinned and scheduler OAuth paths,
// including groups with no mappings (the production regression).
func TestCloudCatalogOverridesProxyManifest(t *testing.T) {
	for _, tc := range []struct {
		name                                                       string
		cloud, mapped, pinned, nativeRoute, temporarilyUnavailable bool
	}{
		{name: "native"},
		{name: "unmapped Cloud", cloud: true},
		{name: "mapped Cloud", cloud: true, mapped: true},
		{name: "pinned native discovery", cloud: true, pinned: true},
		{name: "native route", cloud: true, nativeRoute: true},
		{name: "temporary limit", cloud: true, temporarilyUnavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			cloud := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{OpenAICloudEnabledKey: true}}
			if tc.mapped {
				cloud.Credentials = map[string]any{"model_mapping": map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}}
			}
			accounts := []Account{native}
			if tc.cloud {
				accounts = append(accounts, cloud)
			}
			visible := accounts
			if tc.temporarilyUnavailable {
				visible = []Account{native}
			}
			svc := &OpenAIGatewayService{accountRepo: splitCodexModelsAccountRepo{schedulable: map[int64][]Account{2: visible}, catalog: map[int64][]Account{2: accounts}}}
			group := &Group{ID: 2, Platform: PlatformOpenAI}
			group.CodexModelsManifestConfig.Enabled = tc.pinned
			if tc.nativeRoute {
				group.ModelRoutingEnabled = true
				group.ModelRouting = map[string][]int64{"gpt-6.1-sol": {1}, "gpt-6-luna": {1}}
			}
			models := []map[string]any{}
			for _, slug := range []string{"gpt-6.1-sol", "gpt-6-luna"} {
				models = append(models, map[string]any{"slug": slug, "use_responses_lite": true, "supports_search_tool": true, "tool_mode": "code_mode_only", "multi_agent_version": "v2", "multi_agent_reasoning_effort": "low", "service_tiers": []any{map[string]any{"id": "priority"}}, "supports_reasoning_effort_updates": true, "unknown": "preserved"})
			}
			body, err := json.Marshal(map[string]any{"models": models, "unknown_envelope": true})
			require.NoError(t, err)
			oldETag := codexModelsManifestBodyETag(body)
			manifest := &OpenAIModelsResponse{Body: body, ETag: oldETag}
			require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, manifest, ""))
			for _, model := range decodeCodexManifestModels(t, manifest.Body) {
				slug, _ := model["slug"].(string)
				if slug != "gpt-6.1-sol" && slug != "gpt-6-luna" {
					continue
				}
				restricted := tc.cloud && !tc.nativeRoute && (!tc.mapped || slug == "gpt-6.1-sol")
				for _, field := range []string{"use_responses_lite", "supports_search_tool", "supports_reasoning_effort_updates"} {
					require.Equal(t, !restricted, model[field], slug+"/"+field)
				}
				if restricted {
					for _, field := range []string{"tool_mode", "multi_agent_version", "multi_agent_reasoning_effort"} {
						require.Nil(t, model[field], field)
					}
					require.Empty(t, model["service_tiers"])
				}
				require.Equal(t, "preserved", model["unknown"])
			}
			if tc.cloud && !tc.nativeRoute {
				require.NotEqual(t, oldETag, manifest.ETag)
				stale := &OpenAIModelsResponse{Body: body, ETag: oldETag}
				require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, stale, oldETag))
				require.False(t, stale.NotModified, "old upstream ETag cannot hide changed capabilities")
			}
			fresh := &OpenAIModelsResponse{Body: body, ETag: oldETag}
			require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, fresh, manifest.ETag))
			require.True(t, fresh.NotModified)
		})
	}
}

func TestCloudCatalogCapturedRequestsAndMigratedHistory(t *testing.T) {
	requests, history := cloudCatalogHistoryFixture(t)
	for shape := range requests {
		t.Run(shape, func(t *testing.T) {
			requests, _ := cloudCatalogHistoryFixture(t)
			p, err := cloudShapeParse(t, requests[shape])
			require.NoError(t, err)
			require.NotEmpty(t, p.tools)
			require.NotEmpty(t, p.input)
		})
	}
	for name := range history {
		t.Run(name, func(t *testing.T) {
			requests, history := cloudCatalogHistoryFixture(t)
			body := requests["standard"]
			items := body["input"].([]any)
			last := items[len(items)-1]
			body["input"] = append(append(items[:len(items)-1], history[name]), last)
			p, err := cloudShapeParse(t, body)
			require.NoError(t, err)
			require.NotEmpty(t, p.input)
			injected, err := json.Marshal(cloudInjectionHistory(p))
			require.NoError(t, err)
			require.NotContains(t, string(injected), cloudMessageMetadataKey)
			for _, item := range cloudInjectionHistory(p) {
				require.NotContains(t, item.(map[string]any), "encrypted_function_args")
			}
			require.NotContains(t, string(injected), "gAAAAAcaptured-agent-ciphertext")
			if name == "agent_message/mixed" || name == "agent_message/plain" {
				require.Contains(t, string(injected), "Captured agent message text.")
			}
		})
	}
	t.Run("delivered task starts new turn", func(t *testing.T) {
		requests, history := cloudCatalogHistoryFixture(t)
		body := requests["standard"]
		body["input"] = append(body["input"].([]any), history["function_call_output/no_call_id"])
		p, err := cloudShapeParse(t, body)
		require.NoError(t, err)
		require.Equal(t, "user", p.inputMessage["role"])
		require.Contains(t, p.input[1].(map[string]any)["text"], "Captured delivered task text.")
		require.Empty(t, p.outputs, "a delivered task must never satisfy a pending tool call")
	})
}

// The handover contains one item per shape, not a contiguous conversation.
// Add a synthetic result to the sampled call to form valid completed history.
func cloudCapturedMigratedHistory(t *testing.T) []any {
	_, history := cloudCatalogHistoryFixture(t)
	call := history["function_call/encrypted_function_args"].(map[string]any)
	return []any{
		history["agent_message/plain"], history["agent_message/mixed"],
		call, map[string]any{"type": "function_call_output", "call_id": call["call_id"], "output": "Captured historical result"},
		history["configuration_update"], history["function_call_output/no_call_id"],
	}
}

func TestCloudMigratedInputKeepsExecutionBoundaries(t *testing.T) {
	t.Run("references and mid-turn input use the same classification", func(t *testing.T) {
		_, history := cloudCatalogHistoryFixture(t)
		for _, kind := range []string{"agent_message/mixed", "function_call_output/no_call_id"} {
			output := map[string]any{"type": "function_call_output", "call_id": "call_cloud_pending", "output": "ok"}
			body := map[string]any{"input": []any{output, history[kind], output}}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			_, calls, err := cloudResponseReferences(raw)
			require.NoError(t, err)
			require.Equal(t, []string{"call_cloud_pending", "call_cloud_pending"}, calls)
			p, err := cloudShapeParse(t, body)
			require.NoError(t, err)
			require.True(t, cloudMessageAfterToolOutput(p, map[string]*cloudPendingTool{"call_cloud_pending": {Published: true}}), kind)
		}
	})
	for _, name := range []string{"sealed agent", "invalid call id", "unknown configuration", "missing plaintext arguments", "unnamed output"} {
		t.Run(name, func(t *testing.T) {
			_, history := cloudCatalogHistoryFixture(t)
			var item map[string]any
			switch name {
			case "sealed agent":
				item = history["agent_message/mixed"].(map[string]any)
				item["content"] = item["content"].([]any)[1:]
			case "invalid call id":
				item = history["function_call_output/no_call_id"].(map[string]any)
				item["call_id"] = 42
			case "unknown configuration":
				item = history["configuration_update"].(map[string]any)
				item["tools"] = []any{}
			case "missing plaintext arguments":
				item = history["function_call/encrypted_function_args"].(map[string]any)
				delete(item, "arguments")
			case "unnamed output":
				item = history["function_call_output/no_call_id"].(map[string]any)
				delete(item, "name")
			}
			_, err := cloudShapeParse(t, map[string]any{"input": []any{item, cloudUserMessage("continue")}})
			require.Error(t, err)
		})
	}
	t.Run("historical declarations do not expand current catalog", func(t *testing.T) {
		requests, _ := cloudCatalogHistoryFixture(t)
		body := requests["standard"]
		baseline, err := cloudShapeParse(t, body)
		require.NoError(t, err)
		body["input"] = append([]any{requests["lite"]["input"].([]any)[0]}, body["input"].([]any)...)
		p, err := cloudShapeParse(t, body)
		require.NoError(t, err)
		require.Equal(t, baseline.tools, p.tools)
		require.Equal(t, baseline.threadSettings, p.threadSettings)
		body["tools"] = []any{}
		p, err = cloudShapeParse(t, body)
		require.NoError(t, err)
		require.Empty(t, p.tools, "an explicitly empty catalog cannot reactivate old tools")
	})
}
