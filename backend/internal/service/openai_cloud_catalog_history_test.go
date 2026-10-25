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

func TestCloudCompositeCatalogDoesNotMergeOpenAIConfiguredAliases(t *testing.T) {
	const groupID int64 = 2
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
			"model_mapping": map[string]any{"my-alias": "gpt-6.1-sol"},
		}},
		{ID: 2, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
	}
	svc := &OpenAIGatewayService{accountRepo: splitCodexModelsAccountRepo{
		schedulable: map[int64][]Account{groupID: accounts}, catalog: map[int64][]Account{groupID: accounts},
	}}
	group := &Group{ID: groupID, Platform: PlatformComposite}
	body := []byte(`{"models":[{"slug":"gpt-6.1-sol","use_responses_lite":true,"supports_search_tool":true}],"unknown_envelope":true}`)
	etag := codexModelsManifestBodyETag(body)
	manifest := &OpenAIModelsResponse{Body: body, ETag: etag}
	require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, manifest, ""))
	var slugs []string
	for _, model := range decodeCodexManifestModels(t, manifest.Body) {
		slugs = append(slugs, model["slug"].(string))
	}
	require.Equal(t, []string{"gpt-6.1-sol"}, slugs)
	require.Equal(t, body, manifest.Body, "a composite group without Cloud must keep its directory byte-for-byte")
	require.Equal(t, etag, manifest.ETag)

	fresh := &OpenAIModelsResponse{Body: body, ETag: etag}
	require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, fresh, etag))
	require.True(t, fresh.NotModified)
}

func TestCloudCompositeCatalogDoesNotApplyBPSRestrictions(t *testing.T) {
	account := excelBPSManifestAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-6-astra": "gpt-6-astra"}
	svc := &OpenAIGatewayService{
		accountRepo: codexModelsVisibilityAccountRepo{byGroup: map[int64][]Account{2: {account}}},
		settingService: &SettingService{settingRepo: &excelBPSImageSettingsRepo{values: map[string]string{
			SettingKeyExcelBPSEnabled: "true",
		}}},
	}
	group := &Group{ID: 2, Platform: PlatformComposite}
	body := []byte(`{"models":[{"slug":"gpt-6-astra","multi_agent_version":"v2","use_responses_lite":true,"context_window":1050000}]}`)
	manifest := &OpenAIModelsResponse{Body: body, ETag: codexModelsManifestBodyETag(body)}
	require.NoError(t, svc.MergeGroupConfiguredCodexModels(context.Background(), group, manifest, ""))
	require.JSONEq(t, string(body), string(manifest.Body))
	require.Equal(t, codexModelsManifestBodyETag(body), manifest.ETag)
}

// Exercise the final directory used by both pinned and scheduler OAuth paths,
// including groups with no mappings (the production regression).
func TestCloudCatalogOverridesProxyManifest(t *testing.T) {
	for _, tc := range []struct {
		name                                                                  string
		cloud, mapped, pinned, nativeRoute, temporarilyUnavailable, composite bool
	}{
		{name: "native"},
		{name: "unmapped Cloud", cloud: true},
		{name: "mapped Cloud", cloud: true, mapped: true},
		{name: "pinned native discovery", cloud: true, pinned: true},
		{name: "native route", cloud: true, nativeRoute: true},
		{name: "temporary limit", cloud: true, temporarilyUnavailable: true},
		{name: "composite native", composite: true},
		{name: "composite Cloud", cloud: true, composite: true},
		{name: "composite mapped Cloud", cloud: true, mapped: true, composite: true},
		{name: "composite native route", cloud: true, nativeRoute: true, composite: true},
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
			if tc.composite {
				group.Platform = PlatformComposite
			}
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
	sealed := map[string]any{"type": "agent_message", "author": "/root/worker", "recipient": "/root", "content": []any{map[string]any{"type": "encrypted_content", "encrypted_content": "sealed-history"}}}
	return []any{
		history["agent_message/plain"], history["agent_message/mixed"], sealed,
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
			p, err := cloudShapeParse(t, body)
			require.NoError(t, err)
			actor := &cloudSessionActor{tools: map[string]*cloudPendingTool{"call_cloud_pending": {Published: true}}}
			messages, delta, order, err := actor.splitToolContinuation(p)
			require.NoError(t, err, kind)
			require.Len(t, messages, 1, kind)
			require.Equal(t, "user", messages[0].(map[string]any)["role"])
			require.Len(t, delta, 2, "duplicate tool result is not submitted twice")
			require.Equal(t, []string{"call_cloud_pending"}, order)
			require.Equal(t, []string{"call_cloud_pending", "call_cloud_pending"}, p.continuation.CallIDs)
		}
	})
	for _, name := range []string{"invalid agent ciphertext", "invalid call id", "unknown configuration", "missing plaintext arguments", "unnamed output"} {
		t.Run(name, func(t *testing.T) {
			_, history := cloudCatalogHistoryFixture(t)
			var item map[string]any
			switch name {
			case "invalid agent ciphertext":
				item = history["agent_message/mixed"].(map[string]any)
				item["content"] = []any{map[string]any{"type": "encrypted_content", "encrypted_content": 42}}
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
			p, err := cloudShapeParse(t, map[string]any{"input": []any{item, cloudUserMessage("continue")}})
			if name == "unknown configuration" {
				require.NoError(t, err)
				require.Empty(t, p.tools)
			} else {
				require.Error(t, err)
			}
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

func TestCloudMigratedAgentMessageReplay(t *testing.T) {
	t.Run("sealed history does not poison later turns", func(t *testing.T) {
		requests, history := cloudCatalogHistoryFixture(t)
		sealed := history["agent_message/mixed"].(map[string]any)
		sealed["content"] = sealed["content"].([]any)[1:]
		body := requests["standard"]
		items := body["input"].([]any)
		last := items[len(items)-1]
		body["input"] = append(append(items[:len(items)-1], sealed), last)
		for _, message := range []string{"continue", "continue again"} {
			body["input"] = append(body["input"].([]any), cloudUserMessage(message))
			p, err := cloudShapeParse(t, body)
			require.NoError(t, err)
			require.Equal(t, message, p.input[0].(map[string]any)["text"])
			injected, err := json.Marshal(cloudInjectionHistory(p))
			require.NoError(t, err)
			require.NotContains(t, string(injected), "gAAAAAcaptured-agent-ciphertext")
			require.NotContains(t, string(injected), "Agent message from")
		}
	})
	t.Run("plaintext parts preserve line boundaries", func(t *testing.T) {
		_, history := cloudCatalogHistoryFixture(t)
		item := history["agent_message/plain"].(map[string]any)
		item["content"] = []any{
			map[string]any{"type": "input_text", "text": "First line."},
			map[string]any{"type": "input_text", "text": ""},
			map[string]any{"type": "input_text", "text": "Second line."},
		}
		p, err := cloudShapeParse(t, map[string]any{"input": []any{item, cloudUserMessage("continue")}})
		require.NoError(t, err)
		message := cloudInjectionHistory(p)[0].(map[string]any)
		require.Equal(t, "Agent message from /root/worker_1 to /root:\nFirst line.\n\nSecond line.", message["content"].([]any)[0].(map[string]any)["text"])
	})
}

func TestCloudLiteIncrementalTools(t *testing.T) {
	t.Run("captured child session baseline", func(t *testing.T) {
		requests, history := cloudCatalogHistoryFixture(t)
		body := requests["lite"]
		items := body["input"].([]any)
		last := items[len(items)-1]
		body["input"] = append(append(items[:len(items)-1], history["additional_tools"]), last)
		p, err := cloudShapeParse(t, body)
		require.NoError(t, err)
		require.Len(t, p.tools, 14)
		for _, name := range []string{"functions__exec", "functions__request_user_input_async", "clock__sleep", "mcp__cua_repl__js"} {
			require.True(t, p.toolNames[name], name)
		}
		injected, err := json.Marshal(cloudInjectionHistory(p))
		require.NoError(t, err)
		require.NotContains(t, string(injected), "additional_tools")
	})
	t.Run("namespace deltas preserve unchanged members", func(t *testing.T) {
		function := func(name, description string) map[string]any {
			return map[string]any{"type": "function", "name": name, "description": description, "parameters": map[string]any{"type": "object"}}
		}
		carrier := func(tools ...any) map[string]any {
			return map[string]any{"type": "additional_tools", "role": "developer", "tools": tools}
		}
		namespace := func(tools ...any) map[string]any {
			return map[string]any{"type": "namespace", "name": "functions", "tools": tools}
		}
		body := map[string]any{"input": []any{
			carrier(function("top", "old"), namespace(function("old", "keep"), function("changed", "before"))),
			carrier(namespace(function("new", "added"))),
			carrier(function("top", "latest"), namespace(function("changed", "after"))),
			cloudUserMessage("continue"),
		}}
		p, err := cloudShapeParse(t, body)
		require.NoError(t, err)
		require.Len(t, p.tools, 4)
		for index, expected := range []struct{ name, description string }{
			{"top", "latest"}, {"functions__old", "keep"}, {"functions__changed", "after"}, {"functions__new", "added"},
		} {
			tool := p.tools[index].(map[string]any)
			require.Equal(t, expected.name, tool["name"])
			require.Equal(t, expected.description, tool["description"])
		}
	})
	t.Run("top-level identity includes type but namespace members use name", func(t *testing.T) {
		function := map[string]any{"type": "function", "name": "run", "parameters": map[string]any{"type": "object"}}
		custom := map[string]any{"type": "custom", "name": "run", "format": map[string]any{"type": "text"}}
		body := map[string]any{"input": []any{
			map[string]any{"type": "additional_tools", "tools": []any{function, custom,
				map[string]any{"type": "namespace", "name": "functions", "tools": []any{function}},
				map[string]any{"type": "namespace", "name": "functions", "tools": []any{custom}},
			}}, cloudUserMessage("continue"),
		}}
		require.NoError(t, cloudPromoteAdditionalTools(body))
		tools := body["tools"].([]any)
		require.Len(t, tools, 3, "different top-level types must not silently replace each other")
		require.Equal(t, function, tools[0])
		require.Equal(t, custom, tools[1])
		require.Equal(t, []any{custom}, tools[2].(map[string]any)["tools"])
	})
}
