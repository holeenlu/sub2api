package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudProtocolExpandedHistoryAndToolBudgets(t *testing.T) {
	tools := make([]any, 256)
	for i := range tools {
		tools[i] = map[string]any{"type": "function", "name": fmt.Sprintf("tool_%d", i), "parameters": map[string]any{"type": "object"}}
	}
	history := make([]any, 8192)
	for i := range history {
		history[i] = map[string]any{"role": "user", "content": "hello"}
	}
	request := map[string]any{"model": "m", "input": history, "tools": []any{map[string]any{"type": "namespace", "name": "client", "tools": tools}}}
	raw, err := json.Marshal(request)
	require.NoError(t, err)
	parsed, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	require.Len(t, parsed.tools, 256)
	request["input"] = append(history, history[0])
	raw, err = json.Marshal(request)
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err, "D7 removes the old history count limit")
	request["input"] = "hello"
	request["tools"].([]any)[0].(map[string]any)["tools"] = append(tools, map[string]any{"type": "function", "name": "extra", "parameters": map[string]any{"type": "object"}})
	raw, err = json.Marshal(request)
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err, "D7 removes the old flattened tool count limit")
}

func TestCloudProtocolPreservesRolesAndNamespaceHistory(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}],"input":[{"role":"developer","content":"rule"},{"role":"user","content":"before"},{"role":"assistant","content":"answer"},{"type":"function_call","call_id":"old","namespace":"local","name":"echo","arguments":"{}"},{"type":"function_call_output","call_id":"old","output":"old answer"},{"role":"user","content":"next"}]}`))
	require.NoError(t, err)
	require.Len(t, p.history, 5)
	require.Len(t, p.input, 1)
	require.Equal(t, "developer", p.history[0].(map[string]any)["role"])
	require.Equal(t, "assistant", p.history[2].(map[string]any)["role"])
	name := p.tools[0].(map[string]any)["name"].(string)
	require.NotEqual(t, "echo", name)
	require.Equal(t, "echo", p.history[3].(map[string]any)["name"])
	require.Equal(t, "local", p.names[name].Namespace)
	params := cloudThreadParams(p)
	require.Equal(t, []any{}, params["environments"])
	require.Equal(t, false, params["allowProviderModelFallback"])
	config := params["config"].(map[string]any)
	require.Equal(t, false, config["features.shell_tool"])
	require.Equal(t, "disabled", config["web_search"])
	require.Equal(t, []string{cloudClientToolNamespace}, config["features.code_mode.direct_only_tool_namespaces"])
	tools := params["dynamicTools"].([]any)
	require.Len(t, tools, 1)
	namespace := tools[0].(map[string]any)
	require.Equal(t, "namespace", namespace["type"])
	require.Equal(t, cloudClientToolNamespace, namespace["name"])
	require.Equal(t, p.tools, namespace["tools"])
	require.Equal(t, "function", p.tools[0].(map[string]any)["type"])
	injected := cloudInjectionHistory(p)
	require.Equal(t, name, injected[3].(map[string]any)["name"])
	require.Equal(t, cloudClientToolNamespace, injected[3].(map[string]any)["namespace"])
	require.Equal(t, "local", p.history[3].(map[string]any)["namespace"], "injection must not mutate canonical public history")
	public, err := json.Marshal(map[string]any{"output": []any{p.history[3]}})
	require.NoError(t, err)
	restored := cloudRestorePayload(public, p.names)
	require.Contains(t, string(restored), `"namespace":"local"`)
	require.Contains(t, string(restored), `"name":"echo"`)
	require.NotContains(t, string(restored), cloudClientToolNamespace)
}

func TestCloudDirectNamespaceDoesNotChangeHostedOnlyOrUnrelatedHistory(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","input":"go","tools":[{"type":"web_search"},{"type":"image_generation"}]}`,
		`{"model":"m","input":"go","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}],"tool_choice":"none"}`,
	} {
		p, err := parseCloudProtocolRequest([]byte(body))
		require.NoError(t, err)
		params := cloudThreadParams(p)
		require.Empty(t, params["dynamicTools"])
		require.NotContains(t, params["config"].(map[string]any), "features.code_mode.direct_only_tool_namespaces")
	}
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":[{"type":"function_call","call_id":"past","name":"old_tool","namespace":"old_namespace","arguments":"{}"},{"type":"function_call_output","call_id":"past","output":"ok"},{"role":"user","content":"go"}],"tools":[{"type":"function","name":"current_tool","parameters":{"type":"object"}}]}`))
	require.NoError(t, err)
	require.Equal(t, "old_namespace", cloudInjectionHistory(p)[0].(map[string]any)["namespace"])
	require.Equal(t, p.history, cloudInjectionHistory(p))
	p.history[0].(map[string]any)["name"] = "current_tool"
	require.Equal(t, "old_namespace", cloudInjectionHistory(p)[0].(map[string]any)["namespace"], "an unrelated historical namespace is not overwritten just because a name matches")
}

func TestCloudThreadKeepsClientPermissionContext(t *testing.T) {
	for _, mode := range []string{"read-only", "workspace-write", "danger-full-access"} {
		t.Run(mode, func(t *testing.T) {
			permissions := "<permissions instructions>Filesystem sandboxing is `" + mode + "`. Approval policy is on-request.</permissions instructions>"
			environment := "<environment_context><cwd>/client/workspace</cwd><filesystem>" + mode + "</filesystem></environment_context>"
			history := []any{
				map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": permissions}}},
				map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": environment}}},
			}
			raw, err := json.Marshal(map[string]any{"model": "m", "input": append(append([]any{}, history...), map[string]any{"role": "user", "content": "Edit the project using the client tool."}), "tools": []any{map[string]any{"type": "function", "name": "apply_patch", "parameters": map[string]any{"type": "object"}}}})
			require.NoError(t, err)
			p, err := parseCloudProtocolRequest(raw)
			require.NoError(t, err)
			require.Equal(t, history, cloudInjectionHistory(p), "the client's permission instructions and environment must survive unchanged")
			params := cloudThreadParams(p)
			config := params["config"].(map[string]any)
			require.Equal(t, false, config["include_permissions_instructions"], "Cloud's sandbox must not become instructions for tools executed by the client")
			require.Equal(t, false, config["include_environment_context"], "the Cloud host is not the client's filesystem")
			require.Equal(t, ":read-only", params["permissions"], "do not expand the Cloud host's execution permissions")
			require.Equal(t, "never", params["approvalPolicy"])
			for _, feature := range []string{"shell_tool", "unified_exec", "apply_patch", "js_repl", "plugins", "apps", "multi_agent"} {
				require.Equal(t, false, config["features."+feature])
			}
			require.Empty(t, params["environments"])
			require.Len(t, params["dynamicTools"], 1, "client tools remain available under the client executor's policy")
		})
	}
}

func TestCloudProtocolRejectsAmbiguityAndUnsupportedSemantics(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate_model":            `{"model":"first","model":"second","input":"x"}`,
		"nested_duplicate":           `{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"a","text":"b"}]}]}`,
		"hosted_controls":            `{"model":"m","input":"x","tools":[{"type":"web_search","search_context_size":"high"}]}`,
		"hosted_image_controls":      `{"model":"m","input":"x","tools":[{"type":"image_generation","size":"1024x1024"}]}`,
		"unverified_hosted":          `{"model":"m","input":"x","tools":[{"type":"web_search_preview"}]}`,
		"duplicate_hosted":           `{"model":"m","input":"x","tools":[{"type":"web_search"},{"type":"web_search"}]}`,
		"namespace_unsupported_tool": `{"model":"m","input":"x","tools":[{"type":"namespace","name":"hidden","tools":[{"type":"file_search","name":"shell"}]}]}`,
		"forced_tool":                `{"model":"m","input":"x","tool_choice":{"type":"function","name":"x"}}`,
		"implicit_execution":         `{"model":"m","input":"x","reasoning":{"effort":"ultra"}}`,
		"unsupported_image":          `{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image"}]}]}`,
		"unpaired_unicode":           `{"model":"m","input":"\ud800"}`,
	} {
		t.Run(name, func(t *testing.T) { _, err := parseCloudProtocolRequest([]byte(body)); require.Error(t, err) })
	}
}

func TestCloudProtocolHostedToolsRequireExplicitOptIn(t *testing.T) {
	for _, choice := range []string{"auto", "none"} {
		t.Run(choice, func(t *testing.T) {
			raw := `{"model":"m","input":"x","include":["reasoning.encrypted_content","web_search_call.action.sources"],"tools":[{"type":"web_search"},{"type":"image_generation"},{"type":"function","name":"echo","parameters":{"type":"object"}}],"tool_choice":"` + choice + `"}`
			p, err := parseCloudProtocolRequest([]byte(raw))
			require.NoError(t, err)
			require.Contains(t, p.body["include"], "reasoning.encrypted_content")
			require.True(t, p.includeSearchSources)
			enabled := choice == "auto"
			require.Equal(t, enabled, p.webSearch)
			require.Equal(t, enabled, p.imageGeneration)
			require.Equal(t, enabled, p.toolNames["echo"])
			params := cloudThreadParams(p)
			require.Equal(t, true, params["experimentalRawEvents"])
			config := params["config"].(map[string]any)
			require.Equal(t, enabled, config["features.imagegenext"])
			require.Equal(t, enabled, config["features.image_generation"])
			require.Equal(t, false, config["features.omit_app_server_notification_media"])
			if enabled {
				require.Equal(t, "live", config["web_search"])
				require.Len(t, p.tools, 1)
			} else {
				require.Equal(t, "disabled", config["web_search"])
				require.Empty(t, p.tools)
			}
		})
	}
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"x"}`))
	require.NoError(t, err)
	require.NotContains(t, p.body, "include")
	require.False(t, p.includeSearchSources)
	require.False(t, p.webSearch)
	require.False(t, p.imageGeneration)
	require.Equal(t, map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "x"}}}, p.inputMessage)
}

func TestCloudProtocolPreservesOpaqueEncryptedHistory(t *testing.T) {
	raw := `{"model":"m","input":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"public summary"}],"encrypted_content":" opaque+/=\\\n<密文> "},{"type":"compaction","id":"cmp_1","status":"completed","encrypted_content":" preserve compaction exactly "},{"role":"user","content":"next"}]}`
	p, err := parseCloudProtocolRequest([]byte(raw))
	require.NoError(t, err)
	var original map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &original))
	require.Equal(t, original["input"].([]any)[:2], p.history)
	for name, item := range map[string]string{
		"private_content":        `{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"private"}]}`,
		"invalid_metadata":       `{"type":"reasoning","summary":[],"internal_chat_message_metadata_passthrough":"not-an-object"}`,
		"invalid_summary":        `{"type":"reasoning","summary":[{"type":"input_text","text":"x"}]}`,
		"invalid_cipher":         `{"type":"reasoning","summary":[],"encrypted_content":123}`,
		"missing_cipher":         `{"type":"compaction"}`,
		"empty_cipher":           `{"type":"compaction","encrypted_content":""}`,
		"compaction_summary":     `{"type":"compaction","encrypted_content":"opaque","summary":[]}`,
		"compaction_pending":     `{"type":"compaction","encrypted_content":"opaque","status":"in_progress"}`,
		"compaction_incomplete":  `{"type":"compaction","encrypted_content":"opaque","status":"incomplete"}`,
		"compaction_null_status": `{"type":"compaction","encrypted_content":"opaque","status":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":[` + item + `,{"role":"user","content":"next"}]}`))
			if name == "private_content" || name == "compaction_summary" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// Codex's Responses passthrough metadata (turn ID, creation time, content
// kinds) is attribution, not private reasoning content: App Server items carry
// the same field and the real desktop client sends it, so it passes through.
// Attribution metadata is accepted on every item but never injected: Cloud
// discarded new threads whose opening messages carried it (production
// 2026-10-24), and it has no execution effect.
func TestCloudProtocolAcceptsMessageMetadataWithoutInjectingIt(t *testing.T) {
	metadata := map[string]any{"turn_id": "turn-1", "create_time": json.Number("1792834259.8"), "content_item_kinds": []any{"user.text"}}
	reasoning := map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "opaque", "internal_chat_message_metadata_passthrough": metadata}
	developer := map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "rules"}}, "internal_chat_message_metadata_passthrough": metadata}
	developer["id"], reasoning["id"] = "msg_client", "rs_client"
	raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{developer, reasoning, map[string]any{"role": "user", "content": "next", "internal_chat_message_metadata_passthrough": metadata}}})
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	injected, _ := json.Marshal(cloudInjectionHistory(p))
	require.NotContains(t, string(injected), "internal_chat_message_metadata_passthrough")
	require.Equal(t, "rules", gjson.GetBytes(injected, "0.content.0.text").String())
	require.Equal(t, "opaque", gjson.GetBytes(injected, "1.encrypted_content").String())
	require.False(t, gjson.GetBytes(injected, "0.id").Exists())
	require.False(t, gjson.GetBytes(injected, "1.id").Exists())
	require.Equal(t, "msg_client", p.history[0].(map[string]any)["id"])
	require.Equal(t, "rs_client", p.history[1].(map[string]any)["id"])
	// Apply the native completeness rule independently: an opaque reference
	// cannot lose its ID, and removing an item's ID never removes tool pairing.
	public := &cloudProtocolRequest{history: []any{
		map[string]any{"type": "reasoning", "id": "rs_reference", "summary": []any{}},
		map[string]any{"type": "function_call", "id": "fc_item", "call_id": "call_pair", "name": "echo", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "id": "fc_output", "call_id": "call_pair", "output": "ok"},
	}}
	items := cloudInjectionHistory(public)
	require.Len(t, items, 2, "no-cipher reasoning is retained publicly, never injected")
	for _, item := range items {
		require.NotContains(t, item, "id")
		require.Equal(t, "call_pair", item.(map[string]any)["call_id"])
	}
	turn, _ := json.Marshal(p.input)
	require.NotContains(t, string(turn), "internal_chat_message_metadata_passthrough", "turn/start has no field for it")
}

func cloudProtocolTestPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, image.NewNRGBA(image.Rect(0, 0, 2, 2))))
	return out.Bytes()
}

func TestCloudProtocolInlineImagesPreserveHistoryAndMapTurnInput(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(cloudProtocolTestPNG(t))
	part := map[string]any{"type": "input_image", "image_url": url, "detail": "auto"}
	previous := map[string]any{"role": "user", "type": "message", "content": []any{part}}
	last := map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "describe"}, map[string]any{"type": "input_image", "image_url": url}}}
	raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{previous, map[string]any{"role": "assistant", "content": "before"}, last}})
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	require.Equal(t, previous, p.history[0])
	require.Equal(t, last, p.inputMessage)
	require.Equal(t, []any{map[string]any{"type": "text", "text": "describe", "text_elements": []any{}}, map[string]any{"type": "image", "url": url, "detail": "auto"}}, p.input)
}

func TestCloudProtocolRejectsUnsupportedImageEnvelopes(t *testing.T) {
	data := cloudProtocolTestPNG(t)
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	for name, part := range map[string]map[string]any{
		"remote":        {"type": "input_image", "image_url": "https://example.com/image.png"},
		"file_id":       {"type": "input_image", "file_id": "file_foreign"},
		"extra_file_id": {"type": "input_image", "image_url": url, "file_id": "file_foreign"},
		"detail":        {"type": "input_image", "image_url": url, "detail": "unknown"},
		"low_detail":    {"type": "input_image", "image_url": url, "detail": "low"},
		"bad_base64":    {"type": "input_image", "image_url": "data:image/png;base64,!!=="},
		"base64_lines":  {"type": "input_image", "image_url": url + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"role": "user", "content": []any{part}}}})
			require.NoError(t, err)
			_, err = parseCloudProtocolRequest(raw)
			require.Error(t, err)
		})
	}
	for _, role := range []string{"assistant", "developer", "system"} {
		raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"role": role, "content": []any{map[string]any{"type": "input_image", "image_url": url}}}}})
		require.NoError(t, err)
		_, err = parseCloudProtocolRequest(raw)
		require.Error(t, err)
	}
	// Like native /responses there is no gateway per-message image count cap.
	var images []any
	for range 12 {
		images = append(images, map[string]any{"type": "input_image", "image_url": url})
	}
	raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"role": "user", "content": images}}})
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err)
}

func TestCloudProtocolOriginalImageDetailBoundaries(t *testing.T) {
	// Extract the actual CLI image part only: this gpt-5.5 capture has a
	// different custom-tool catalog, whose compatibility is not claimed here.
	raw, err := os.ReadFile("testdata/codex_0_159_3_images.json")
	require.NoError(t, err)
	frames := gjson.GetBytes(raw, "scenarios.ws_original.frames").Array()
	output := gjson.Get(frames[len(frames)-1].Raw, "request.input.0.output")
	var parts []any
	require.NoError(t, json.Unmarshal([]byte(output.Raw), &parts))
	require.Equal(t, "original", parts[0].(map[string]any)["detail"])
	message := map[string]any{"role": "user", "content": parts}
	tool := map[string]any{"type": "function_call_output", "call_id": "call", "output": parts}
	for _, test := range []struct {
		name  string
		items []any
	}{
		{"user", []any{message}},
		{"history", []any{message, tool, message}},
		{"pending_tool", []any{tool}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"model": "m", "input": test.items})
			require.NoError(t, err)
			p, err := parseCloudProtocolRequest(body)
			require.NoError(t, err)
			if test.name == "pending_tool" {
				require.True(t, p.originalToolImages["call"], "actor decides whether this is history or an active tool reply")
				return
			}
			require.Equal(t, "original", p.input[0].(map[string]any)["detail"])
			if test.name == "history" {
				history, err := json.Marshal(cloudInjectionHistory(p))
				require.NoError(t, err)
				require.Equal(t, "original", gjson.GetBytes(history, "0.content.0.detail").String())
				require.Equal(t, "original", gjson.GetBytes(history, "1.output.0.detail").String())
			}
		})
	}
}

func TestCloudProtocolToolImagesAndTypedOutputIdempotence(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(cloudProtocolTestPNG(t))
	output := []any{map[string]any{"type": "input_text", "text": "screenshot"}, map[string]any{"type": "input_image", "image_url": url}}
	request := func(value any) []byte {
		raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"type": "function_call_output", "call_id": "call", "output": value}}})
		require.NoError(t, err)
		return raw
	}
	p, err := parseCloudProtocolRequest(request(output))
	require.NoError(t, err)
	require.Equal(t, output, p.outputHistory["call"])
	require.Equal(t, []any{map[string]any{"type": "inputText", "text": "screenshot"}, map[string]any{"type": "inputImage", "imageUrl": url}}, p.outputItems["call"])
	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.Equal(t, string(encoded), p.outputs["call"])
	text, err := parseCloudProtocolRequest(request(string(encoded)))
	require.NoError(t, err)
	require.NotEqual(t, p.outputs["call"], text.outputs["call"])
	require.Equal(t, string(encoded), text.outputHistory["call"])
	require.Equal(t, []any{map[string]any{"type": "inputText", "text": string(encoded)}}, text.outputItems["call"])
	conflict, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"type": "function_call_output", "call_id": "call", "output": output}, map[string]any{"type": "function_call_output", "call_id": "call", "output": string(encoded)}}})
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(conflict)
	require.Error(t, err)
	for _, invalid := range []any{
		[]any{},
		[]any{map[string]any{"type": "output_text", "text": "wrong role"}},
		[]any{map[string]any{"type": "input_image", "image_url": "https://example.com/screenshot"}},
		[]any{map[string]any{"type": "input_image", "file_id": "file_foreign"}},
	} {
		_, err := parseCloudProtocolRequest(request(invalid))
		require.Error(t, err)
	}
}

func TestCloudUsageRequiresExactCompleteMonotonicFacts(t *testing.T) {
	usage, err := cloudParseUsage(json.RawMessage(`{"inputTokens":3415.0,"outputTokens":31.0,"cachedInputTokens":3200.0,"cacheWriteInputTokens":0.0,"totalTokens":3446.0}`))
	require.NoError(t, err)
	require.Equal(t, 3415, usage.InputTokens)
	require.Equal(t, 3200, usage.CachedInputTokens)
	for _, raw := range []string{`{"inputTokens":0}`, `{"inputTokens":2,"outputTokens":1,"cachedInputTokens":3,"cacheWriteInputTokens":0,"totalTokens":3}`, `{"inputTokens":1.5,"outputTokens":1,"cachedInputTokens":0,"cacheWriteInputTokens":0,"totalTokens":2.5}`, `{"inputTokens":1,"outputTokens":1,"cachedInputTokens":0,"cacheWriteInputTokens":null,"totalTokens":2}`} {
		_, err := cloudParseUsage(json.RawMessage(raw))
		require.Error(t, err)
	}
	_, err = cloudUsageDifference(&OpenAICloudUsage{InputTokens: 1, TotalTokens: 1}, usage)
	require.Error(t, err)
}

type cloudActorTestStore struct {
	OpenAICloudSessionStore
	results      []OpenAICloudStreamEvent
	settlements  []*OpenAICloudSettlement
	refs         map[string]bool
	record       *OpenAICloudSessionRecord
	createErr    error
	enqueueErr   error
	readErr      error
	createCalls  int
	enqueueCalls int
}

func (s *cloudActorTestStore) FailCloudProbeSettlement(context.Context, string, string) error {
	return nil
}
func (s *cloudActorTestStore) CloudRequestClosed(context.Context, string) (bool, error) {
	return false, nil
}
func (s *cloudActorTestStore) DeleteCloudEvents(context.Context, string) error { return nil }
func (s *cloudActorTestStore) CreateCloudSession(context.Context, *OpenAICloudSessionRecord) error {
	s.createCalls++
	return s.createErr
}
func (s *cloudActorTestStore) EnqueueCloudCommand(context.Context, string, OpenAICloudCommand) error {
	s.enqueueCalls++
	return s.enqueueErr
}
func (s *cloudActorTestStore) ReadCloudEvents(context.Context, string, string) ([]OpenAICloudStreamMessage, error) {
	return nil, s.readErr
}

func (s *cloudActorTestStore) SaveCloudSession(_ context.Context, r *OpenAICloudSessionRecord, refs []string, settle *OpenAICloudSettlement) error {
	s.record = r
	for _, ref := range refs {
		s.refs[ref] = true
	}
	if settle != nil {
		if CloudUsesStageBilling(settle.BillingSnapshot) {
			for i, old := range s.settlements {
				if old.RootRequestID == settle.RootRequestID {
					if !CloudBillingStagesExtend(old.BillingStages, settle.BillingStages) {
						return errors.New("changed stage")
					}
					s.settlements[i] = settle
					return nil
				}
			}
		}
		s.settlements = append(s.settlements, settle)
	}
	return nil
}
func (s *cloudActorTestStore) PublishCloudEvent(_ context.Context, _ string, event OpenAICloudStreamEvent) error {
	s.results = append(s.results, event)
	return nil
}

func (s *cloudActorTestStore) PreserveCloudLateUsage(_ context.Context, r *OpenAICloudSessionRecord, original *OpenAICloudSettlement) error {
	for i, pending := range s.settlements {
		if pending.RootRequestID == r.RootRequestID {
			original = pending
			s.settlements = append(s.settlements[:i], s.settlements[i+1:]...)
			break
		}
	}
	next := cloudLiveClone(original)
	if !next.ReconciliationOnly {
		next.OriginalPayload, _ = json.Marshal(original)
	}
	next.ReconciliationOnly = true
	next.LateUsage = cloudLiveClone(r.LateUsage)
	s.record = cloudLiveClone(r)
	s.settlements = append(s.settlements, next)
	return nil
}
func (s *cloudActorTestStore) FindCloudSession(_ context.Context, user, key, group int64, ref string) (*OpenAICloudSessionRecord, error) {
	if s.record != nil && s.record.UserID == user && s.record.APIKeyID == key && s.record.GroupID == group && s.refs[ref] {
		return s.record, nil
	}
	return nil, nil
}

func TestCloudActorToolsAcrossResponsesAndOneRootSettlement(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	defer rpc.Close()
	cloudTestInitialize(t, ctx, rpc, wire)
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
	store := &cloudActorTestStore{refs: make(map[string]bool)}
	frames := capturedCodexFrames(t, "http_tools")
	protocol, err := parseCloudProtocolRequest(capturedCodexBody(t, frames[0], true))
	outputs := frames[1].Get(`request.input.#(type=="function_call_output")#`).Array()
	calls := frames[1].Get(`request.input.#(type=="function_call")#`).Array()
	require.Len(t, outputs, 2)
	require.Len(t, calls, 2)
	require.NoError(t, err)
	a := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "session", RootRequestID: "root-original", ThreadID: "thread", TurnID: "turn", Status: "running"}, BillingSnapshot: json.RawMessage(`{"root":"original"}`)}, rpc: rpc, protocol: protocol, requestID: "http1", requestDigest: "initial", responseID: "resp_cloud_first", items: map[string]int{}, tools: map[string]*cloudPendingTool{}, completed: map[string]*OpenAICloudResult{}, ctx: ctx}
	event := func(method string, params string, id string) error {
		return a.handleEvent(cloudRPCMessage{Method: method, Params: json.RawMessage(params), ID: json.RawMessage(id)})
	}
	require.NoError(t, event("item/tool/call", fmt.Sprintf(`{"threadId":"thread","turnId":"turn","callId":"upstream-call1","namespace":"sub2api_client","tool":"exec_command","arguments":%s}`, calls[0].Get("arguments").String()), `"rpc-one"`))
	require.Empty(t, a.requestID)
	require.Empty(t, store.settlements)
	var firstCall string
	for id := range a.tools {
		firstCall = id
	}
	require.True(t, store.refs[firstCall])
	require.Nil(t, a.record.Result.Usage)
	require.True(t, a.record.Result.AwaitingTools)
	// A second parallel call arrives after the first HTTP response has ended.
	require.NoError(t, event("item/tool/call", fmt.Sprintf(`{"threadId":"thread","turnId":"turn","callId":"upstream-call2","namespace":"sub2api_client","tool":"exec_command","arguments":%s}`, calls[1].Get("arguments").String()), `"rpc-two"`))
	var secondCall string
	for id := range a.tools {
		if id != firstCall {
			secondCall = id
		}
	}
	request := func(call, previous, output string) OpenAICloudRequest {
		raw, _ := json.Marshal(map[string]any{"model": "gpt-5.1", "previous_response_id": previous, "input": []any{map[string]any{"type": "function_call_output", "call_id": call, "output": output}}})
		return OpenAICloudRequest{Owner: owner, Body: raw, RootRequestID: "must-not-replace-original", BillingSnapshot: json.RawMessage(`{"root":"other"}`)}
	}
	firstOutput := request(firstCall, a.lastResponseID, outputs[0].Get("output").String())
	require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "http2", Request: firstOutput}))
	msg := cloudTestMessage(t, ctx, wire)
	require.JSONEq(t, `"rpc-one"`, string(msg.ID))
	require.Empty(t, msg.Method)
	require.True(t, a.record.Result.AwaitingTools)
	require.Equal(t, "root-original", a.record.RootRequestID)
	require.Empty(t, store.settlements)
	secondOutput := request(secondCall, a.lastResponseID, outputs[1].Get("output").String())
	require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "http3", Request: secondOutput}))
	msg = cloudTestMessage(t, ctx, wire)
	require.JSONEq(t, `"rpc-two"`, string(msg.ID))
	require.Empty(t, msg.Method)
	require.NoError(t, event("thread/tokenUsage/updated", `{"threadId":"thread","turnId":"turn","tokenUsage":{"total":{"inputTokens":100.0,"outputTokens":4.0,"cachedInputTokens":20.0,"cacheWriteInputTokens":0.0,"totalTokens":104.0}}}`, ""))
	require.NoError(t, event("item/started", `{"threadId":"thread","turnId":"turn","item":{"id":"answer","type":"agentMessage"}}`, ""))
	require.NoError(t, event("item/agentMessage/delta", `{"threadId":"thread","turnId":"turn","itemId":"answer","delta":"ok"}`, ""))
	require.NoError(t, event("item/completed", `{"threadId":"thread","turnId":"turn","item":{"id":"answer","type":"agentMessage","text":"ok"}}`, ""))
	require.NoError(t, event("turn/completed", `{"threadId":"thread","turn":{"id":"turn","status":"completed","error":null}}`, ""))
	require.Len(t, store.settlements, 1)
	require.Equal(t, "root-original", store.settlements[0].RootRequestID)
	require.False(t, store.settlements[0].Unknown)
	require.Equal(t, 100, store.settlements[0].Usage.InputTokens)
	require.True(t, a.record.Result.Terminal)
	require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "http3-retry", Request: secondOutput}))
	require.Len(t, store.settlements, 1)
	select {
	case raw := <-wire.writes:
		t.Fatalf("tool replayed: %s", raw)
	default:
	}
	conflict := request(secondCall, a.lastResponseID, "different")
	require.Error(t, a.handleCommand(OpenAICloudCommand{RequestID: "conflict", Request: conflict}))
	sessions := &OpenAICloudSessions{store: store}
	binding, err := sessions.Resolve(ctx, 1, 2, 3, secondOutput.Body)
	require.NoError(t, err)
	require.Equal(t, int64(4), binding.AccountID)
	_, err = sessions.Resolve(ctx, 999, 2, 3, secondOutput.Body)
	require.Error(t, err)
}

// cloudLongTurnActor drives an owner actor on a live thread the way its run
// loop delivers client commands and upstream events.
type cloudLongTurnActor struct {
	t        *testing.T
	ctx      context.Context
	a        *cloudSessionActor
	store    *cloudActorTestStore
	wire     *cloudRPCFakeConn
	owner    OpenAICloudOwner
	answered map[string]bool
	settings map[string]any // extra fields of each user turn request
}

func newCloudLongTurnActor(t *testing.T) *cloudLongTurnActor {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	t.Cleanup(func() { _ = rpc.Close() })
	cloudTestInitialize(t, ctx, rpc, wire)
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: "settings"}
	store := &cloudActorTestStore{refs: make(map[string]bool)}
	a := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "session", ThreadID: "thread", Status: "completed"}}, rpc: rpc, tools: map[string]*cloudPendingTool{}, completed: map[string]*OpenAICloudResult{}, ctx: ctx}
	return &cloudLongTurnActor{t: t, ctx: ctx, a: a, store: store, wire: wire, owner: owner, answered: map[string]bool{}}
}

func (h *cloudLongTurnActor) request(root string, body map[string]any) OpenAICloudRequest {
	body["model"], body["tools"] = "gpt-5.1", cloudHistoryTools
	raw, _ := json.Marshal(body)
	return OpenAICloudRequest{Owner: h.owner, Body: raw, RootRequestID: root, BillingSnapshot: json.RawMessage(`{}`)}
}

func (h *cloudLongTurnActor) event(method, params, id string) error {
	return h.a.handleEvent(cloudRPCMessage{Method: method, Params: json.RawMessage(params), ID: json.RawMessage(id)})
}

func (h *cloudLongTurnActor) startTurn(turn int) {
	h.t.Helper()
	id := fmt.Sprintf("turn-%d", turn)
	started := make(chan error, 1)
	body := map[string]any{"input": []any{cloudUserMessage("question " + id)}}
	for key, value := range h.settings {
		body[key] = value
	}
	go func() {
		started <- h.a.handleCommand(OpenAICloudCommand{RequestID: id, Request: h.request("root-"+id, body)})
	}()
	cloudWireResponse(h.t, h.ctx, h.wire, "turn/start", map[string]any{"turn": map[string]any{"id": id}})
	require.NoError(h.t, <-started)
}

// call makes the upstream turn ask for one client tool.
func (h *cloudLongTurnActor) call(turn int, name string) {
	h.t.Helper()
	params := fmt.Sprintf(`{"threadId":"thread","turnId":"turn-%d","callId":"up-%s","namespace":"sub2api_client","tool":"echo","arguments":{}}`, turn, name)
	require.NoError(h.t, h.event("item/tool/call", params, fmt.Sprintf(`"rpc-%s"`, name)), "turn %d call %s", turn, name)
}

// reply answers the calls the latest response published, chained to that
// response as a client WS does. It returns nil when none is waiting.
func (h *cloudLongTurnActor) reply() *OpenAICloudRequest {
	h.t.Helper()
	var outputs []any
	for callID, tool := range h.a.tools {
		if tool.Published && !h.answered[callID] {
			h.answered[callID] = true
			outputs = append(outputs, map[string]any{"type": "function_call_output", "call_id": callID, "output": "answer"})
		}
	}
	if len(outputs) == 0 {
		return nil
	}
	reply := h.request("unused", map[string]any{"previous_response_id": h.a.lastResponseID, "input": outputs})
	require.NoError(h.t, h.a.handleCommand(OpenAICloudCommand{RequestID: fmt.Sprintf("reply-%d", len(h.answered)), Request: reply}))
	for range outputs {
		require.Empty(h.t, cloudTestMessage(h.t, h.ctx, h.wire).Method, "only tool results go upstream")
	}
	return &reply
}

// answer replies until no published call is left unanswered.
func (h *cloudLongTurnActor) answer() []OpenAICloudRequest {
	h.t.Helper()
	var sent []OpenAICloudRequest
	for reply := h.reply(); reply != nil; reply = h.reply() {
		sent = append(sent, *reply)
	}
	return sent
}

func (h *cloudLongTurnActor) completeTurn(turn int) {
	h.t.Helper()
	require.NoError(h.t, h.event("thread/tokenUsage/updated", fmt.Sprintf(`{"threadId":"thread","turnId":"turn-%d","tokenUsage":{"total":{"inputTokens":%d,"outputTokens":1,"cachedInputTokens":0,"cacheWriteInputTokens":0,"totalTokens":%d}}}`, turn, 10*turn, 10*turn+1), ""))
	require.NoError(h.t, h.event("turn/completed", fmt.Sprintf(`{"threadId":"thread","turn":{"id":"turn-%d","status":"completed","error":null}}`, turn), ""))
	require.Equal(h.t, "completed", h.a.record.Status, "turn %d", turn)
}

// Review regression: long conversations keep running on one thread. Replay
// results and tool registrations used to grow with the thread and then with
// the turn: the 129th cached response failed after it was published,
// interrupting the turn whose call the client was about to answer, and the
// 257th tool call failed its turn. A turn now keeps only its recent replay
// results, and an answered call leaves the registry for the thread history.
func TestCloudActorLongConversationsKeepRunning(t *testing.T) {
	for name, tc := range map[string]struct{ turns, batches, size int }{
		"calls_across_turns":            {70, 1, 1},
		"parallel_calls_across_turns":   {14, 1, 20},
		"serial_round_trips_in_a_turn":  {1, 130, 1},
		"parallel_tool_calls_in_a_turn": {1, 15, 20},
	} {
		t.Run(name, func(t *testing.T) {
			h := newCloudLongTurnActor(t)
			for turn := 1; turn <= tc.turns; turn++ {
				h.startTurn(turn)
				for batch := 0; batch < tc.batches; batch++ {
					// The first call ends the open response; the rest wait for the reply.
					for i := 0; i < tc.size; i++ {
						h.call(turn, fmt.Sprintf("%d-%d-%d", turn, batch, i))
					}
					h.answer()
				}
				h.completeTurn(turn)
			}
			require.Empty(t, h.a.tools, "answered calls leave the registry")
			require.LessOrEqual(t, len(h.a.completed), 128)
			require.Len(t, h.store.settlements, tc.turns)
			require.Empty(t, h.wire.writes)
		})
	}
}

// With parallel_tool_calls false the gateway hands out one call at a time in
// the order the model asked for them, also after answered calls have left the
// registry: a call that arrives later is never handed out first.
func TestCloudActorSerialToolsKeepRequestOrder(t *testing.T) {
	h := newCloudLongTurnActor(t)
	h.settings = map[string]any{"parallel_tool_calls": false}
	h.startTurn(1)
	waiting := func() string {
		t.Helper()
		var rpcs []string
		for callID, tool := range h.a.tools {
			if tool.Published && !h.answered[callID] {
				rpcs = append(rpcs, string(tool.RPCID))
			}
		}
		require.Len(t, rpcs, 1, "one call at a time")
		return rpcs[0]
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		h.call(1, name)
	}
	require.Equal(t, `"rpc-a"`, waiting())
	for _, next := range []string{"b", "c"} {
		require.NotNil(t, h.reply())
		require.Equal(t, `"rpc-`+next+`"`, waiting())
	}
	h.call(1, "e") // arrives while c waits for its result and d is queued
	for _, next := range []string{"d", "e"} {
		require.NotNil(t, h.reply())
		require.Equal(t, `"rpc-`+next+`"`, waiting())
	}
	h.answer()
	h.completeTurn(1)
	require.Empty(t, h.a.tools)
}

// Within a long turn the latest request still replays its own result. A
// request that has left the replay window, re-sent in previous_response_id or
// full-history form, is refused before it reaches the thread and never
// receives the call now pending.
func TestCloudActorLongTurnReplaysRecentAndRefusesEvictedRequests(t *testing.T) {
	h := newCloudLongTurnActor(t)
	h.startTurn(1)
	var replies []OpenAICloudRequest
	var firstCall string
	for i := 0; i < 130; i++ {
		h.call(1, fmt.Sprint(i))
		if i == 0 {
			for id := range h.a.tools {
				firstCall = id
			}
		}
		replies = append(replies, h.answer()...)
	}
	h.call(1, "pending")
	require.Equal(t, "awaiting_tools", h.a.record.Status)
	published := h.a.record.Result.Response

	require.NoError(t, h.a.handleCommand(OpenAICloudCommand{RequestID: "retry-latest", Request: replies[len(replies)-1]}))
	replay := h.store.results[len(h.store.results)-1].Result
	require.NotNil(t, replay)
	require.True(t, replay.Replayed)
	require.Equal(t, published, replay.Response, "the retry gets the response it missed")
	require.Empty(t, h.a.requestID)

	results := len(h.store.results)
	fullHistory := h.request("unused", map[string]any{"input": []any{
		cloudUserMessage("question turn-1"),
		map[string]any{"type": "function_call", "call_id": firstCall, "name": "echo", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": firstCall, "output": "answer"},
	}})
	for name, stale := range map[string]OpenAICloudRequest{"previous_response_id": replies[0], "full_history": fullHistory} {
		require.Error(t, h.a.handleCommand(OpenAICloudCommand{RequestID: "stale-" + name, Request: stale}), name)
	}
	require.Len(t, h.store.results, results, "a request that left the window gets no response")
	require.Empty(t, h.wire.writes, "nothing reaches the thread")

	h.answer()
	h.completeTurn(1)
	require.Empty(t, h.a.tools)
	require.Len(t, h.store.settlements, 1)
}

func TestCloudActorUnknownUsageRemainsUnsettled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &cloudActorTestStore{refs: map[string]bool{}}
	a := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "unknown-root", ThreadID: "t", TurnID: "turn", Status: "running"}}, ctx: ctx, tools: map[string]*cloudPendingTool{}}
	err := a.handleEvent(cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"t","turn":{"id":"turn","status":"completed"}}`)})
	require.NoError(t, err)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].Unknown)
	require.Nil(t, store.settlements[0].Usage)
	_, err = parseCloudProtocolRequest([]byte(`{"previous_response_id":"first","previous_response_id":"second"}`))
	require.Error(t, err)
}

func TestCloudLocalCapacityPreservesPendingActors(t *testing.T) {
	for _, count := range []int{16, 128, 256} {
		t.Run(fmt.Sprintf("retained_%d", count), func(t *testing.T) {
			store := &cloudActorTestStore{refs: map[string]bool{}, createErr: errors.New("reservation reached")}
			sessions := NewOpenAICloudSessions(store, nil, nil)
			defer sessions.Close()
			for i := range count {
				id := fmt.Sprintf("pending-%d", i)
				sessions.actors[id] = &cloudSessionActor{record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{SessionID: id, Status: "awaiting_tools"}}, tools: map[string]*cloudPendingTool{"pending-call": {CallID: "pending-call"}}}
			}
			id, err := sessions.createSession(t.Context(), OpenAICloudRequest{Owner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4}, RootRequestID: "next-root"})
			require.Error(t, err)
			require.Equal(t, 1, store.createCalls, "retained Cloud owners must not impose a second concurrency admission limit")
			require.NotEmpty(t, id, "storage may have reserved a session before returning its error")
			require.Len(t, sessions.actors, count)
			for _, actor := range sessions.actors {
				require.Equal(t, "awaiting_tools", actor.record.Status)
				require.Contains(t, actor.tools, "pending-call", "capacity pressure must not answer or discard pending RPCs")
			}
		})
	}
}

func TestCloudRunNotStartedStopsAtFirstPossiblePersistence(t *testing.T) {
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-test", SettingsDigest: "settings"}
	base := OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-test","input":"hello"}`), RootRequestID: "root", BillingSnapshot: json.RawMessage(`{"root":"bill"}`)}
	for _, scenario := range []string{"unavailable", "parameters", "owner", "authorization", "foreign_response", "unpaired_tool_result", "missing_snapshot", "create_network_error", "enqueue_network_error", "read_after_enqueue"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			store := &cloudActorTestStore{refs: map[string]bool{}}
			sessions := NewOpenAICloudSessions(store, func(context.Context, OpenAICloudOwner) (*cloudRPC, error) {
				return nil, errors.New("connect must not run")
			}, func(context.Context, OpenAICloudOwner) error { return nil })
			defer sessions.Close()
			request := base
			switch scenario {
			case "unavailable":
				sessions.store = nil
			case "parameters":
				request.Body = json.RawMessage(`{"model":"gpt-test","model":"ambiguous","input":"hello"}`)
			case "owner":
				request.Owner.AccountID = 0
			case "authorization":
				sessions.validate = func(context.Context, OpenAICloudOwner) error { return errors.New("revoked") }
			case "foreign_response":
				request.Body = json.RawMessage(`{"model":"gpt-test","input":"hello","previous_response_id":"resp_native"}`)
			case "unpaired_tool_result":
				request.Body = json.RawMessage(`{"model":"gpt-test","input":[{"type":"function_call_output","call_id":"call_native_missing","output":"orphan"}]}`)
			case "missing_snapshot":
				request.BillingSnapshot = nil
			case "create_network_error":
				store.createErr = errors.New("write may have persisted")
			case "enqueue_network_error", "read_after_enqueue":
				store.record = &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "existing", ExpiresAt: time.Now().Add(time.Minute), Status: "completed"}}
				store.refs["resp_cloud_existing"] = true
				request.Body = json.RawMessage(`{"model":"gpt-test","input":"hello","previous_response_id":"resp_cloud_existing"}`)
				if scenario == "enqueue_network_error" {
					store.enqueueErr = errors.New("command may have persisted")
				} else {
					store.readErr = errors.New("event delivery failed after enqueue")
				}
			}
			_, err := sessions.Run(ctx, request, nil)
			require.Error(t, err)
			if scenario == "foreign_response" || scenario == "unpaired_tool_result" {
				status, _, _, mapped := cloudPublicError(err)
				require.True(t, mapped, "local history failures must not become opaque 502 errors")
				require.Equal(t, 400, status)
			}
			mayHaveStarted := scenario == "create_network_error" || scenario == "enqueue_network_error" || scenario == "read_after_enqueue"
			require.Equal(t, !mayHaveStarted, errors.Is(err, ErrOpenAICloudNotStarted))
			if !mayHaveStarted {
				require.Zero(t, store.createCalls)
				require.Zero(t, store.enqueueCalls)
			}
		})
	}
}

func TestCloudProtocolRejectsUnverifiedMessagePhases(t *testing.T) {
	for name, item := range map[string]any{
		"unknown":    map[string]any{"role": "assistant", "content": "answer", "phase": "future_phase"},
		"empty":      map[string]any{"role": "assistant", "content": "answer", "phase": ""},
		"null":       map[string]any{"role": "assistant", "content": "answer", "phase": nil},
		"non-string": map[string]any{"role": "assistant", "content": "answer", "phase": 1},
		"user":       map[string]any{"role": "user", "content": "answer", "phase": "final_answer"},
		"developer":  map[string]any{"role": "developer", "content": "answer", "phase": "commentary"},
		"tool":       map[string]any{"type": "function_call_output", "call_id": "call", "output": "answer", "phase": "final_answer"},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{item, map[string]any{"role": "user", "content": "next"}}})
			require.NoError(t, err)
			_, err = parseCloudProtocolRequest(raw)
			require.Error(t, err)
		})
	}
}

func TestCloudProtocolAcceptsBoundedClientMetadataWithoutChangingOwnershipOrSettings(t *testing.T) {
	plain, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","metadata":{"application":"kept"}}`))
	require.NoError(t, err)
	request := `{"model":"m","input":"hello","metadata":{"application":"kept"},"client_metadata":{"session_id":"untrusted","thread_id":"untrusted","turn_id":"different-each-turn","x-codex-turn-metadata":"{\"thread_id\":\"private\"}","authorization":"not-an-upstream-credential","features.shell_tool":"true"}}`
	got, err := parseCloudProtocolRequest([]byte(request))
	require.NoError(t, err)
	require.Equal(t, plain.settings, got.settings)
	require.Equal(t, plain.body, got.body)
	require.Equal(t, cloudThreadParams(plain), cloudThreadParams(got))
	for _, metadata := range []string{`null`, `{}`} {
		_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","client_metadata":` + metadata + `}`))
		require.NoError(t, err)
	}
	for _, metadata := range []string{`[]`, `true`, `"text"`, `{"field":1}`, `{"field":null}`, `{"field":{}}`, `{"":"empty-key"}`, `{"x":"` + strings.Repeat("x", 64<<10) + `"}`} {
		_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","client_metadata":` + metadata + `}`))
		require.ErrorContains(t, err, "client_metadata")
	}
	_, err = parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","client_metadata":{"turn_id":"one","turn_id":"two"}}`))
	require.Error(t, err)
}

func TestCloudProtocolCacheHintDoesNotChangeExecutionIdentity(t *testing.T) {
	plain, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello"}`))
	require.NoError(t, err)
	for _, value := range []string{`null`, `""`, `"client-cache-one"`, `"client-cache-two"`} {
		got, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","prompt_cache_key":` + value + `}`))
		require.NoError(t, err)
		require.Equal(t, plain.body, got.body)
		require.Equal(t, plain.settings, got.settings)
		require.Equal(t, cloudThreadParams(plain), cloudThreadParams(got))
	}
	for _, value := range []string{`[]`, `{}`, `true`, `123`} {
		_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","prompt_cache_key":` + value + `}`))
		require.ErrorContains(t, err, "prompt_cache_key")
	}
}

func TestCloudProtocolNativeSummaryAndVerbosityMapping(t *testing.T) {
	for _, summary := range []string{"auto", "concise", "detailed", "none"} {
		for _, verbosity := range []string{"low", "medium", "high"} {
			raw, err := json.Marshal(map[string]any{"model": "m", "input": "hello", "reasoning": map[string]any{"effort": "high", "summary": summary}, "text": map[string]any{"verbosity": verbosity, "format": map[string]any{"type": "text"}}})
			require.NoError(t, err)
			got, err := parseCloudProtocolRequest(raw)
			require.NoError(t, err)
			require.Equal(t, summary, got.summary)
			require.Equal(t, verbosity, cloudThreadParams(got)["config"].(map[string]any)["model_verbosity"])
		}
	}
	for _, field := range []string{`"reasoning":{"summary":"unknown"}`, `"reasoning":{"summary":42}`, `"text":{"verbosity":"unknown"}`, `"text":{"verbosity":true}`} {
		_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello",` + field + `}`))
		require.Error(t, err)
	}
	_, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"hello","reasoning":{"summary":null},"text":{"format":null,"verbosity":null}}`))
	require.NoError(t, err)
}

func TestCloudProtocolMapsStrictSchemaAndSerialToolPolicy(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"x","parallel_tool_calls":false,"store":false,"text":{"format":{"type":"json_schema","name":"answer","strict":true,"schema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}}`))
	require.NoError(t, err)
	require.True(t, p.serialTools)
	require.Equal(t, "object", p.outputSchema["type"])
	require.Equal(t, true, cloudThreadParams(p)["ephemeral"])
	for _, format := range []string{`{"type":"json_schema","name":"x","strict":false,"schema":{}}`, `{"type":"json_schema","name":"x","strict":true,"description":"special instructions","schema":{"type":"object"}}`} {
		_, err = parseCloudProtocolRequest([]byte(`{"model":"m","input":"x","text":{"format":` + format + `}}`))
		if strings.Contains(format, "description") {
			require.NoError(t, err)
		} else {
			require.Error(t, err, "unsupported execution semantics cannot be silently dropped")
		}
	}
}

func TestCloudProtocolClientToolSearchParameters(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"go","tools":[{"type":"tool_search","execution":"client","description":"Client source discovery","parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"number"}},"required":["query"],"additionalProperties":false}}]}`))
	require.NoError(t, err)
	require.True(t, p.toolSearch)
	raw, err := json.Marshal(p.tools)
	require.NoError(t, err)
	require.Equal(t, "number", gjson.GetBytes(raw, "0.inputSchema.properties.limit.type").String())
	require.Equal(t, "Client source discovery", gjson.GetBytes(raw, "0.description").String())
}

func TestCloudProtocolAcceptsNonStrictSchemaWithoutForcingStrict(t *testing.T) {
	for _, strict := range []string{"false", "null"} {
		t.Run(strict, func(t *testing.T) {
			p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":"Return an answer","text":{"format":{"type":"json_schema","name":"codex_output_schema","strict":` + strict + `,"schema":{"type":"object","properties":{"answer":{"type":"string"},"rationale":{"type":"string"}},"required":["answer"],"additionalProperties":false}}}}`))
			require.NoError(t, err)
			require.Equal(t, "object", p.outputSchema["type"])
		})
	}
}

func TestCloudInjectionDropsCompletedDeliveryStatusOnly(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}","status":"completed"},{"type":"function_call_output","call_id":"call_1","output":"ok","status":"completed"},{"role":"user","content":"next"}]}`))
	require.NoError(t, err)
	injected := cloudInjectionHistory(p)
	for i := 0; i < 2; i++ {
		require.NotContains(t, injected[i].(map[string]any), "status")
		require.Equal(t, "completed", p.history[i].(map[string]any)["status"])
	}
}

func TestCloudHistoryRepeatedCallIDPreservesEachIdentity(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"m","input":[{"type":"function_call","call_id":"same","name":"old_tool","arguments":"{}"},{"type":"function_call","call_id":"same","name":"new_tool","arguments":"{}"},{"role":"user","content":"next"}]}`))
	require.NoError(t, err)
	require.Equal(t, "old_tool", p.history[0].(map[string]any)["name"])
	require.Equal(t, "new_tool", p.history[1].(map[string]any)["name"])
}

func TestCloudSamplingWatermarksPersistAcrossHandoffAndResetPerRoot(t *testing.T) {
	h := newCloudLongTurnActor(t)
	h.startTurn(1)
	update := func(input, output, cached int) {
		raw := fmt.Sprintf(`{"threadId":"thread","turnId":"%s","tokenUsage":{"total":{"inputTokens":%d,"outputTokens":%d,"cachedInputTokens":%d,"totalTokens":%d}}}`, h.a.record.TurnID, input, output, cached, input+output)
		require.NoError(t, h.event("thread/tokenUsage/updated", raw, ""))
	}
	update(100, 2, 20)
	update(100, 2, 20)
	require.Len(t, h.a.record.SamplingUsage, 1, "duplicate watermark is not a second sampling charge")
	// A confirmed thread handoff resets the upstream counter, not the root.
	h.a.previousThreadUsage = h.a.record.Usage
	h.a.threadTotal, h.a.turnBase = nil, nil
	update(200, 3, 100)
	require.Len(t, h.a.record.SamplingUsage, 2)
	require.True(t, cloudSamplingUsageValid(h.a.record.Usage, h.a.record.SamplingUsage))
	require.NoError(t, h.event("turn/completed", `{"threadId":"thread","turn":{"id":"turn-1","status":"completed"}}`, ""))
	require.Len(t, h.store.settlements, 1)
	persisted, _ := json.Marshal(h.store.settlements[0])
	var settlement OpenAICloudSettlement
	require.NoError(t, json.Unmarshal(persisted, &settlement))
	require.Len(t, settlement.SamplingUsage, 2)
	h.startTurn(2)
	require.NotNil(t, h.a.record.SamplingUsage)
	require.Empty(t, h.a.record.SamplingUsage)
	update(250, 4, 110)
	require.Equal(t, 50, h.a.record.SamplingUsage[0].InputTokens, "new root subtracts previous thread counter")
	require.Len(t, settlement.SamplingUsage, 2, "older settlement is immutable")
}

func TestCloudActorReplayCacheReleasesRetainedBytes(t *testing.T) {
	t.Run("evicted responses", func(t *testing.T) {
		h := newCloudLongTurnActor(t)
		h.startTurn(1)
		for i := 0; i < 128; i++ {
			h.a.metadata = map[string]any{"padding": strings.Repeat("x", 4096)}
			h.call(1, fmt.Sprint(i))
			h.answer()
		}
		require.Len(t, h.a.completed, 128)
		// Other live history occupies the rest of the budget. Replacing an old
		// response of the same size must leave room for several more tool replies.
		h.a.retainedBytes = OpenAICloudMaxRetainedBytes - len(h.a.record.Result.Response) - 512
		for i := 128; i < 132; i++ {
			h.a.metadata = map[string]any{"padding": strings.Repeat("x", 4096)}
			h.call(1, fmt.Sprint(i))
			h.answer()
		}
		h.completeTurn(1)
		require.Len(t, h.a.completed, 128)
		require.Len(t, h.store.settlements, 1)
		require.Empty(t, h.a.tools)
		require.Empty(t, h.wire.writes)
	})
	t.Run("new billing root keeps history but drops replay copies", func(t *testing.T) {
		h := newCloudLongTurnActor(t)
		h.startTurn(1)
		h.a.metadata = map[string]any{"padding": strings.Repeat("x", 4096)}
		require.NoError(t, h.event("item/started", `{"threadId":"thread","turnId":"turn-1","item":{"id":"message","type":"agentMessage"}}`, ""))
		require.NoError(t, h.event("item/agentMessage/delta", `{"threadId":"thread","turnId":"turn-1","itemId":"message","delta":"retained answer"}`, ""))
		require.NoError(t, h.event("item/completed", `{"threadId":"thread","turnId":"turn-1","item":{"id":"message","type":"agentMessage","text":"retained answer"}}`, ""))
		require.NoError(t, h.event("rawResponseItem/completed", `{"threadId":"thread","turnId":"turn-1","item":{"id":"reasoning","type":"reasoning","summary":[],"encrypted_content":"opaque-history"}}`, ""))
		h.completeTurn(1)
		previous, history := h.a.record.Result, append([]any(nil), h.a.history...)
		historyBytes := 0
		for _, item := range history {
			raw, err := json.Marshal(item)
			require.NoError(t, err)
			historyBytes += len(raw)
		}
		require.GreaterOrEqual(t, h.a.retainedBytes-len(previous.Response), historyBytes, "text and opaque history need their own charge after the replay copy is released")
		h.a.retainedBytes = OpenAICloudMaxRetainedBytes - 1
		overflow := h.request("root-too-large", map[string]any{"input": strings.Repeat("x", len(previous.Response)+1024)})
		require.ErrorContains(t, h.a.handleCommand(OpenAICloudCommand{RequestID: "too-large", Request: overflow}), "retained")
		require.Same(t, previous, h.a.record.Result, "a rejected request must not discard the retryable response")
		require.Len(t, h.a.completed, 1)
		require.Equal(t, history, h.a.history)
		require.Empty(t, h.wire.writes)

		request := h.request("root-turn-2", map[string]any{"input": []any{cloudUserMessage("question turn-2")}})
		done := make(chan error, 1)
		go func() { done <- h.a.handleCommand(OpenAICloudCommand{RequestID: "turn-2", Request: request}) }()
		select {
		case err := <-done:
			t.Fatalf("new turn rejected before dispatch: %v", err)
		case raw := <-h.wire.writes:
			var start cloudRPCMessage
			require.NoError(t, json.Unmarshal(raw, &start))
			require.Equal(t, "turn/start", start.Method)
			cloudTestRead(h.wire, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-2"}}})
		case <-h.ctx.Done():
			t.Fatal("new turn did not reach dispatch")
		}
		require.NoError(t, <-done)
		require.Empty(t, h.a.completed)
		require.Equal(t, history, h.a.history[:len(history)], "releasing replay bytes cannot release conversation history")
		h.completeTurn(2)
		require.Len(t, h.store.settlements, 2)
		require.Less(t, h.a.retainedBytes, OpenAICloudMaxRetainedBytes)
	})
}
