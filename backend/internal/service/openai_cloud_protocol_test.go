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

func TestCloudProtocolPreservesRolesAndNamespaceHistory(t *testing.T) {
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}],"input":[{"role":"developer","content":"rule"},{"role":"user","content":"before"},{"role":"assistant","content":"answer"},{"type":"function_call","call_id":"old","namespace":"local","name":"echo","arguments":"{}"},{"type":"function_call_output","call_id":"old","output":"old answer"},{"role":"user","content":"next"}]}`))
	require.NoError(t, err)
	require.Len(t, p.history, 5)
	require.Len(t, p.input, 1)
	require.Equal(t, "developer", p.history[0].(map[string]any)["role"])
	require.Equal(t, "assistant", p.history[2].(map[string]any)["role"])
	name := p.tools[0].(map[string]any)["name"].(string)
	require.NotEqual(t, "echo", name)
	require.Equal(t, name, p.history[3].(map[string]any)["name"])
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
	require.NotContains(t, p.history[3].(map[string]any), "namespace", "injection must not mutate canonical public history")
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

func TestCloudProtocolRejectsAmbiguityAndUnsupportedSemantics(t *testing.T) {
	for name, body := range map[string]string{
		"duplicate_model":       `{"model":"first","model":"second","input":"x"}`,
		"nested_duplicate":      `{"model":"m","input":[{"role":"user","content":[{"type":"input_text","text":"a","text":"b"}]}]}`,
		"hosted_controls":       `{"model":"m","input":"x","tools":[{"type":"web_search","search_context_size":"high"}]}`,
		"hosted_image_controls": `{"model":"m","input":"x","tools":[{"type":"image_generation","size":"1024x1024"}]}`,
		"unverified_hosted":     `{"model":"m","input":"x","tools":[{"type":"web_search_preview"}]}`,
		"duplicate_hosted":      `{"model":"m","input":"x","tools":[{"type":"web_search"},{"type":"web_search"}]}`,
		"unverified_include":    `{"model":"m","input":"x","include":["code_interpreter_call.outputs"]}`,
		"namespace_hidden_tool": `{"model":"m","input":"x","tools":[{"type":"namespace","name":"hidden","tools":[{"type":"custom","name":"shell"}]}]}`,
		"unknown_generation":    `{"model":"m","input":"x","temperature":0}`,
		"forced_tool":           `{"model":"m","input":"x","tool_choice":{"type":"function","name":"x"}}`,
		"implicit_execution":    `{"model":"m","input":"x","reasoning":{"effort":"ultra"}}`,
		"unsupported_image":     `{"model":"m","input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image"}]}]}`,
		"unpaired_unicode":      `{"model":"m","input":"\ud800"}`,
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
			require.True(t, p.includeEncryptedReasoning)
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
	require.False(t, p.includeEncryptedReasoning)
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
			require.Error(t, err)
		})
	}
}

// Codex's Responses passthrough metadata (turn ID, creation time, content
// kinds) is attribution, not private reasoning content: App Server items carry
// the same field and the real desktop client sends it, so it passes through.
func TestCloudProtocolPassesMessageMetadataThrough(t *testing.T) {
	metadata := map[string]any{"turn_id": "turn-1", "create_time": json.Number("1792834259.8"), "content_item_kinds": []any{"user.text"}}
	reasoning := map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "opaque", "internal_chat_message_metadata_passthrough": metadata}
	developer := map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "rules"}}, "internal_chat_message_metadata_passthrough": metadata}
	raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{developer, reasoning, map[string]any{"role": "user", "content": "next", "internal_chat_message_metadata_passthrough": metadata}}})
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	injected, _ := json.Marshal(cloudInjectionHistory(p))
	require.Equal(t, "turn-1", gjson.GetBytes(injected, "0.internal_chat_message_metadata_passthrough.turn_id").String())
	require.Equal(t, "turn-1", gjson.GetBytes(injected, "1.internal_chat_message_metadata_passthrough.turn_id").String())
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
	var images []any
	for range 9 {
		images = append(images, map[string]any{"type": "input_image", "image_url": url})
	}
	raw, err := json.Marshal(map[string]any{"model": "m", "input": []any{map[string]any{"role": "user", "content": images}}})
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(raw)
	require.Error(t, err)
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
		[]any{map[string]any{"type": "input_text", "text": "x", "annotations": []any{}}},
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
		s.settlements = append(s.settlements, settle)
	}
	return nil
}
func (s *cloudActorTestStore) PublishCloudEvent(_ context.Context, _ string, event OpenAICloudStreamEvent) error {
	s.results = append(s.results, event)
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
	_, _, err = cloudResponseReferences([]byte(`{"previous_response_id":"first","previous_response_id":"second"}`))
	require.Error(t, err)
}

func TestCloudRunNotStartedStopsAtFirstPossiblePersistence(t *testing.T) {
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-test", SettingsDigest: "settings"}
	base := OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-test","input":"hello"}`), RootRequestID: "root", BillingSnapshot: json.RawMessage(`{"root":"bill"}`)}
	for _, scenario := range []string{"unavailable", "parameters", "owner", "authorization", "foreign_response", "missing_user", "missing_snapshot", "local_capacity", "create_network_error", "enqueue_network_error", "read_after_enqueue"} {
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
			case "missing_user":
				request.Body = json.RawMessage(`{"model":"gpt-test","input":[{"role":"assistant","content":"history only"}]}`)
			case "missing_snapshot":
				request.BillingSnapshot = nil
			case "local_capacity":
				for i := range 16 {
					sessions.actors[string(rune('a'+i))] = &cloudSessionActor{}
				}
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
		require.Error(t, err, "unsupported execution semantics cannot be silently dropped")
	}
}
