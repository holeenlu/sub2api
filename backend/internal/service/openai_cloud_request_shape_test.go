package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func cloudShapeParse(t *testing.T, body map[string]any) (*cloudProtocolRequest, error) {
	t.Helper()
	if _, ok := body["model"]; !ok {
		body["model"] = "gpt-5.1"
	}
	if _, ok := body["input"]; !ok {
		body["input"] = "hi"
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return parseCloudProtocolRequest(raw)
}

// Client-attached identifiers and values equal to the Responses default have
// no execution effect; real SDK clients send them, so they must not fail.
func TestCloudRequestShapeAcceptsNeutralClientFields(t *testing.T) {
	for name, fields := range map[string]map[string]any{
		"user":                 {"user": "end-user-1"},
		"safety_identifier":    {"safety_identifier": "hashed-user"},
		"stream_options":       {"stream": true, "stream_options": map[string]any{"include_obfuscation": false}},
		"service_tier_auto":    {"service_tier": "auto"},
		"service_tier_default": {"service_tier": "default"},
		"truncation_disabled":  {"truncation": "disabled"},
		"background_false":     {"background": false},
		"top_logprobs_zero":    {"top_logprobs": 0},
		"effort_none":          {"reasoning": map[string]any{"effort": "none"}},
		"effort_minimal":       {"reasoning": map[string]any{"effort": "minimal"}},
		// Codex Desktop 0.163+ states the persisted-reasoning mode; the Cloud
		// thread keeps the model's own default, which is what "auto" selects.
		"reasoning_context_auto":    {"reasoning": map[string]any{"effort": "high", "context": "auto"}},
		"reasoning_context_all":     {"reasoning": map[string]any{"effort": "high", "summary": "auto", "context": "all_turns"}},
		"reasoning_context_current": {"reasoning": map[string]any{"context": "current_turn"}},
	} {
		t.Run(name, func(t *testing.T) {
			body := make(map[string]any, len(fields))
			for key, value := range fields {
				body[key] = value
			}
			p, err := cloudShapeParse(t, body)
			require.NoError(t, err)
			for key := range fields {
				if key != "stream" && key != "reasoning" {
					require.NotContains(t, p.body, key, "neutral fields are not generation settings")
				}
			}
		})
	}
}

// Execution semantics without an App Server equivalent stay explicit errors
// that name the field, never a silently different generation.
func TestCloudRequestShapeRejectsUnmappableExecution(t *testing.T) {
	for name, fields := range map[string]map[string]any{
		"max_output_tokens":         {"max_output_tokens": 100},
		"temperature":               {"temperature": 0.2},
		"top_p":                     {"top_p": 0.5},
		"service_tier_flex":         {"service_tier": "flex"},
		"truncation_auto":           {"truncation": "auto"},
		"background_true":           {"background": true},
		"top_logprobs":              {"top_logprobs": 3},
		"stream_options_bad":        {"stream_options": map[string]any{"include_usage": true}},
		"strict_tool":               {"tools": []any{map[string]any{"type": "function", "name": "f", "strict": true, "parameters": map[string]any{"type": "object"}}}},
		"tool_search":               {"tools": []any{map[string]any{"type": "tool_search", "execution": "client", "parameters": map[string]any{"type": "object"}}}},
		"effort_unknown":            {"reasoning": map[string]any{"effort": "ultra"}},
		"reasoning_context_unknown": {"reasoning": map[string]any{"context": "never"}},
		"reasoning_unknown_key":     {"reasoning": map[string]any{"generate_summary": "auto"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := cloudShapeParse(t, fields)
			require.Error(t, err)
		})
	}
}

var cloudApplyPatchTool = map[string]any{"type": "custom", "name": "apply_patch", "description": "Apply a patch.",
	"format": map[string]any{"type": "grammar", "syntax": "lark", "definition": "start: \"*** Begin Patch\" LF"}}

// A custom (free-form) tool becomes a dynamic function with one string input;
// its grammar is stated in the description because Cloud cannot constrain it.
func TestCloudRequestShapeMapsCustomToolsAndHistory(t *testing.T) {
	p, err := cloudShapeParse(t, map[string]any{"tools": []any{cloudApplyPatchTool}, "input": []any{
		cloudUserMessage("edit"),
		map[string]any{"type": "custom_tool_call", "id": "ctc_1", "status": "completed", "call_id": "call_1", "name": "apply_patch", "input": "*** Begin Patch\n"},
		map[string]any{"type": "custom_tool_call_output", "call_id": "call_1", "output": "Done"},
		cloudUserMessage("next"),
	}})
	require.NoError(t, err)
	require.True(t, p.customTools["apply_patch"])
	require.Len(t, p.tools, 1)
	tool := p.tools[0].(map[string]any)
	require.Equal(t, "function", tool["type"])
	require.Contains(t, tool["description"], "lark")
	require.Contains(t, tool["description"], "*** Begin Patch")
	schema, _ := json.Marshal(tool["inputSchema"])
	require.JSONEq(t, `{"type":"object","properties":{"input":{"type":"string"}},"required":["input"],"additionalProperties":false}`, string(schema))
	injected, _ := json.Marshal(cloudInjectionHistory(p))
	call := gjson.GetBytes(injected, `#(type=="function_call")`)
	require.Equal(t, "call_1", call.Get("call_id").String())
	require.Equal(t, cloudCustomToolArguments("*** Begin Patch\n"), call.Get("arguments").String())
	require.Equal(t, cloudClientToolNamespace, call.Get("namespace").String())
	require.Equal(t, "Done", gjson.GetBytes(injected, `#(type=="function_call_output").output`).String())
	_, err = cloudShapeParse(t, map[string]any{"tools": []any{map[string]any{"type": "custom", "name": "x", "format": map[string]any{"type": "grammar", "syntax": "peg", "definition": "a"}}}})
	require.Error(t, err, "unknown grammar syntax is not silently accepted")
}

// End to end: the client sees a custom_tool_call with the raw input, replies
// with custom_tool_call_output, and may replay both as history next turn.
func TestCloudCustomToolRoundTripAndHistoryReplay(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	tools := []any{cloudApplyPatchTool}
	patch := "*** Begin Patch\n*** Add File: a.txt\n+hi\n*** End Patch\n"
	var wire *cloudRPCFakeConn
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		wire = h.nextWire()
		thread := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "thread/start", thread.Method)
		require.Equal(t, "apply_patch", gjson.GetBytes(thread.Params, "dynamicTools.0.tools.0.name").String())
		require.Equal(t, "string", gjson.GetBytes(thread.Params, "dynamicTools.0.tools.0.inputSchema.properties.input.type").String())
		cloudTestRead(wire, map[string]any{"id": thread.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		cloudTestRead(wire, map[string]any{"id": "patch-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "up", "namespace": "sub2api_client", "tool": "apply_patch", "arguments": map[string]any{"input": patch}}})
		var raw []byte
		select {
		case raw = <-wire.writes:
		case <-h.ctx.Done():
			return
		}
		require.Equal(t, "patch-rpc", gjson.GetBytes(raw, "id").String(), string(raw))
		require.Equal(t, "Success", gjson.GetBytes(raw, "result.contentItems.0.text").String(), string(raw))
		cloudWireCompleteTurn(wire, "thread", "turn", 12)
	}()
	first, err := h.invoke(map[string]any{"model": "client-model", "input": []any{cloudUserMessage("edit")}, "tools": tools})
	require.NoError(t, err, first.Raw)
	call := first.Get("output.0")
	require.Equal(t, "custom_tool_call", call.Get("type").String(), first.Raw)
	require.Equal(t, patch, call.Get("input").String())
	require.Equal(t, "apply_patch", call.Get("name").String())
	require.False(t, call.Get("arguments").Exists())
	output := map[string]any{"type": "custom_tool_call_output", "call_id": call.Get("call_id").String(), "output": "Success"}
	last, err := h.invoke(map[string]any{"model": "client-model", "input": []any{cloudUserMessage("edit"), call.Value(), output}, "tools": tools})
	require.NoError(t, err, last.Raw)
	require.Equal(t, "completed", last.Get("status").String(), last.Raw)
	<-upstream

	history := []any{cloudUserMessage("edit"), call.Value(), output}
	for _, item := range last.Get("output").Array() {
		history = append(history, item.Value())
	}
	next := make(chan struct{})
	go func() {
		defer close(next)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "input": append(history, cloudUserMessage("again")), "tools": tools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-next
	h.noNewWire(200 * time.Millisecond)
}

// Streaming clients receive the custom tool input through its own events.
func TestCloudCustomToolStreamingEvents(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	ctx := t.Context()
	go func() {
		cloudWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"id": "rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "up", "namespace": "sub2api_client", "tool": "apply_patch", "arguments": map[string]any{"input": "PATCH"}}})
	}()
	raw, err := json.Marshal(map[string]any{"model": "client-model", "stream": true, "input": "edit", "tools": []any{cloudApplyPatchTool}})
	require.NoError(t, err)
	requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	_, err = svc.forwardOpenAICloud(requestCtx, c, in.Account, raw)
	require.NoError(t, err)
	var kinds []string
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			kinds = append(kinds, gjson.Get(strings.TrimPrefix(line, "data: "), "type").String())
		}
	}
	joined := fmt.Sprint(kinds)
	require.Contains(t, joined, "response.custom_tool_call_input.delta")
	require.Contains(t, joined, "response.custom_tool_call_input.done")
	require.NotContains(t, joined, "function_call_arguments")
}

// store:false clients resend every earlier image. Like native /responses there
// is no gateway image count cap, neither across history nor within a message.
func TestCloudImageCountIsUnboundedLikeNative(t *testing.T) {
	image := map[string]any{"type": "input_image", "image_url": "data:image/png;base64,iVBORw0KGgo=", "detail": "high"}
	message := func(count int) map[string]any {
		content := []any{map[string]any{"type": "input_text", "text": "look"}}
		for i := 0; i < count; i++ {
			content = append(content, image)
		}
		return map[string]any{"type": "message", "role": "user", "content": content}
	}
	_, err := cloudShapeParse(t, map[string]any{"input": []any{message(4), message(4), message(4)}})
	require.NoError(t, err, "twelve images across three turns of history")
	_, err = cloudShapeParse(t, map[string]any{"input": []any{message(12)}})
	require.NoError(t, err, "twelve images in one message")
}

// The App Server TokenUsageBreakdown declares cacheWriteInputTokens with
// serde(default): absent means zero by protocol, not unknown usage. Reasoning
// tokens are part of output and surface as the standard Responses detail.
func TestCloudUsageFollowsAppServerBreakdown(t *testing.T) {
	usage, err := cloudParseUsage(json.RawMessage(`{"inputTokens":10,"outputTokens":4,"cachedInputTokens":2,"reasoningOutputTokens":3,"totalTokens":14}`))
	require.NoError(t, err)
	require.Zero(t, usage.CacheWriteInputTokens)
	require.Equal(t, 3, usage.ReasoningOutputTokens)
	_, err = cloudParseUsage(json.RawMessage(`{"inputTokens":10,"outputTokens":4,"cachedInputTokens":2,"reasoningOutputTokens":5,"totalTokens":14}`))
	require.Error(t, err, "reasoning tokens are a part of output tokens")
	_, err = cloudParseUsage(json.RawMessage(`{"inputTokens":10,"outputTokens":4,"totalTokens":14}`))
	require.Error(t, err, "required counters are still required")
	_, err = cloudParseUsage(json.RawMessage(`{"inputTokens":10,"outputTokens":4,"cachedInputTokens":2,"cacheWriteInputTokens":null,"totalTokens":14}`))
	require.Error(t, err, "serde(default) covers an absent field, not an explicit null")
	turn, err := cloudUsageDifference(&OpenAICloudUsage{InputTokens: 30, OutputTokens: 10, ReasoningOutputTokens: 7, TotalTokens: 40}, &OpenAICloudUsage{InputTokens: 10, OutputTokens: 4, ReasoningOutputTokens: 3, TotalTokens: 14})
	require.NoError(t, err)
	public, _ := json.Marshal(cloudResponseUsage(turn))
	require.Equal(t, int64(4), gjson.GetBytes(public, "output_tokens_details.reasoning_tokens").Int(), string(public))
	record := &OpenAICloudUsage{InputTokens: 20, OutputTokens: 6, ReasoningOutputTokens: 3, TotalTokens: 26}
	require.True(t, cloudRecoveryPublicUsageWithin(json.RawMessage(public), turn))
	require.False(t, cloudRecoveryPublicUsageWithin(json.RawMessage(public), record), "public reasoning above the settlement")
}

// Codex 0.164 (ChatGPT desktop) sends these when its concurrent reasoning
// summaries and output token replay features are on. This is the exact shape
// that produced "Cloud does not support the supplied stream_options semantics".
func TestCloudAcceptsCodexDesktopStreamAndReplayOptions(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	ctx := t.Context()
	go func() {
		cloudWireStart(t, ctx, wire)
		cloudWireComplete(wire)
	}()
	body, err := json.Marshal(map[string]any{"model": "client-model", "stream": true, "input": "hi",
		"reasoning":      map[string]any{"effort": "high", "summary": "auto"},
		"stream_options": map[string]any{"reasoning_summary_delivery": "sequential_cutoff"},
		"include":        []any{"reasoning.encrypted_content", "output.encrypted_content"}})
	require.NoError(t, err)
	requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	_, err = svc.forwardOpenAICloud(requestCtx, c, in.Account, body)
	require.NoError(t, err, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "response.completed")

	_, err = cloudShapeParse(t, map[string]any{"stream_options": map[string]any{"reasoning_summary_delivery": "concurrent"}})
	require.Error(t, err, "an unknown delivery mode is not assumed to be satisfied")
	_, err = cloudShapeParse(t, map[string]any{"include": []any{"message.output_text.logprobs"}})
	require.Error(t, err)
}

// History from a native turn with output token replay carries ciphertext on
// assistant messages and calls. It is opaque and passed through unchanged,
// exactly like reasoning ciphertext; a user message cannot carry it.
func TestCloudPassesOutputCiphertextHistoryThrough(t *testing.T) {
	p, err := cloudShapeParse(t, map[string]any{"tools": []any{map[string]any{"type": "function", "name": "f", "parameters": map[string]any{"type": "object"}}}, "input": []any{
		cloudUserMessage("a"),
		map[string]any{"type": "message", "role": "assistant", "encrypted_content": "opaque-message", "content": []any{map[string]any{"type": "output_text", "text": "ok"}}},
		map[string]any{"type": "function_call", "call_id": "c", "name": "f", "arguments": "{}", "encrypted_content": "opaque-call"},
		map[string]any{"type": "function_call_output", "call_id": "c", "output": "done"},
		cloudUserMessage("b"),
	}})
	require.NoError(t, err)
	injected, _ := json.Marshal(cloudInjectionHistory(p))
	require.Equal(t, "opaque-message", gjson.GetBytes(injected, `#(role=="assistant").encrypted_content`).String())
	require.Equal(t, "opaque-call", gjson.GetBytes(injected, `#(type=="function_call").encrypted_content`).String())
	_, err = cloudShapeParse(t, map[string]any{"input": []any{map[string]any{"type": "message", "role": "user", "encrypted_content": "x", "content": "hi"}}})
	require.Error(t, err)
}

// Codex derives web_search controls from its search mode; Cloud runs the same
// mode through the thread config instead of silently widening it to live.
func TestCloudWebSearchControlsMapToThreadSearchMode(t *testing.T) {
	for name, tc := range map[string]struct {
		tool map[string]any
		mode string
	}{
		"bare":    {map[string]any{"type": "web_search"}, "live"},
		"cached":  {map[string]any{"type": "web_search", "external_web_access": false, "search_content_types": []any{"text", "image"}}, "cached"},
		"live":    {map[string]any{"type": "web_search", "external_web_access": true}, "live"},
		"indexed": {map[string]any{"type": "web_search", "external_web_access": true, "indexed_web_access": true}, "indexed"},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := cloudShapeParse(t, map[string]any{"tools": []any{tc.tool}})
			require.NoError(t, err)
			params, _ := json.Marshal(cloudThreadParams(p))
			require.Equal(t, tc.mode, gjson.GetBytes(params, `config.web_search`).String(), string(params))
		})
	}
	for name, tool := range map[string]map[string]any{
		"filters":         {"type": "web_search", "filters": map[string]any{"allowed_domains": []any{"openai.com"}}},
		"user_location":   {"type": "web_search", "user_location": map[string]any{"type": "approximate", "country": "US"}},
		"context_size":    {"type": "web_search", "search_context_size": "high"},
		"indexed_no_live": {"type": "web_search", "external_web_access": false, "indexed_web_access": true},
		"content_types":   {"type": "web_search", "search_content_types": []any{"video"}},
	} {
		t.Run("rejects_"+name, func(t *testing.T) {
			_, err := cloudShapeParse(t, map[string]any{"tools": []any{tool}})
			require.Error(t, err)
		})
	}
}

// Requests are shaped by the Codex catalog. A Cloud-served model must not
// advertise what the adapter cannot honor: client tool search (tools loaded
// mid-turn), Responses Lite, client code mode, encrypted multi-agent messages
// or non-default service tiers. Mixed groups fall back to the safe values.
func TestCloudAccountCodexCapabilitiesMatchAdapter(t *testing.T) {
	native := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": "https://chatgpt.com/backend-api/codex"}}
	cloud := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": "https://chatgpt.com/backend-api/codex"}, Extra: map[string]any{OpenAICloudEnabledKey: true}}
	model := "gpt-6-astra"
	nativeCaps := accountCodexToolCapabilities(native, model)
	require.Equal(t, "true", string(nativeCaps["supports_search_tool"]), "native OAuth Astra keeps its verified catalog")
	cloudCaps := accountCodexToolCapabilities(cloud, model)
	for field, want := range map[string]string{
		"supports_search_tool": "false", "use_responses_lite": "false", "tool_mode": "null",
		"multi_agent_version": "null", "multi_agent_reasoning_effort": "null", "service_tiers": "[]",
	} {
		require.Equal(t, want, string(cloudCaps[field]), field)
	}
	mixed := intersectUpstreamModelMetadata(model, []UpstreamModelMetadata{{CodexToolCapabilities: nativeCaps}, {CodexToolCapabilities: cloudCaps}})
	require.Equal(t, "false", string(mixed.CodexToolCapabilities["supports_search_tool"]))
	require.Equal(t, "false", string(mixed.CodexToolCapabilities["use_responses_lite"]))
	require.Equal(t, "null", string(mixed.CodexToolCapabilities["tool_mode"]))
}

// A valid 1x1 PNG, encoded by the standard library.
var cloudTinyPNG = func() string {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}()

// The real ChatGPT desktop Codex request, driven by the catalog this gateway
// serves for a Cloud account, completes a tool round trip end to end over
// both JSON and SSE without dropping any of its fields.
func TestCloudCapturedDesktopCodexRequestRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_desktop.json")
	require.NoError(t, err)
	var fixture struct {
		Request map[string]any `json:"request"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.Account.Credentials["model_mapping"].(map[string]any)["gpt-6-astra"] = "gpt-5.1"
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			done := make(chan struct{})
			t.Cleanup(func() { cancel(); <-done })
			go func() {
				defer close(done)
				cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
				require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
				thread := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/start", thread.Method)
				params := string(thread.Params)
				// Namespace tools are registered flattened and restored on output.
				for _, name := range []string{"exec_command", "apply_patch", "multi_agent_v1__spawn_agent", "web__run"} {
					require.Contains(t, params, `"name":"`+name+`"`, "every client tool is registered")
				}
				cloudTestRead(wire, map[string]any{"id": thread.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
				injected := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/inject_items", injected.Method)
				cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
				cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
				cloudTestRead(wire, map[string]any{"id": "rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "up", "namespace": "sub2api_client", "tool": "exec_command", "arguments": map[string]any{"cmd": "printf ok"}}})
				select {
				case <-wire.writes:
				case <-ctx.Done():
					return
				}
				cloudWireComplete(wire)
			}()
			invoke := func(body map[string]any) gjson.Result {
				t.Helper()
				body["stream"] = stream
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
				require.NoError(t, err)
				requestCtx, err = svc.PrepareCloudContinuation(requestCtx, in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, raw)
				require.NoError(t, err)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				c.Set("api_key", in.APIKey)
				SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
				_, err = svc.forwardOpenAICloud(requestCtx, c, in.Account, raw)
				require.NoError(t, err, recorder.Body.String())
				if !stream {
					return gjson.Parse(recorder.Body.String())
				}
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					if event := gjson.Parse(strings.TrimPrefix(line, "data: ")); strings.HasPrefix(line, "data: ") && event.Get("type").String() == "response.completed" {
						return event.Get("response")
					}
				}
				t.Fatal(recorder.Body.String())
				return gjson.Result{}
			}
			body := cloudLiveClone(fixture.Request)
			first := invoke(body)
			call := first.Get(`output.#(type=="function_call")`)
			require.Equal(t, "exec_command", call.Get("name").String(), first.Raw)
			require.Equal(t, "gpt-6-astra", first.Get("model").String())
			follow := cloudLiveClone(fixture.Request)
			follow["input"] = append(follow["input"].([]any), call.Value(), map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "ok"})
			last := invoke(follow)
			require.Equal(t, "completed", last.Get("status").String(), last.Raw)
			require.Equal(t, 1, store.settlementCount())
		})
	}
}

// The same captured desktop request over client WebSocket: full first frame,
// then the incremental previous_response_id continuation Codex sends over WS.
func TestCloudCapturedDesktopCodexRequestOverWebSocket(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_desktop.json")
	require.NoError(t, err)
	var fixture struct {
		Request map[string]any `json:"request"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	svc, in, store, wire := cloudForwardFixture(t, nil)
	in.Account.Credentials["model_mapping"].(map[string]any)["gpt-6-astra"] = "gpt-5.1"
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
		require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
		cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		cloudWireResponse(t, ctx, wire, "thread/inject_items", map[string]any{})
		cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		cloudTestRead(wire, map[string]any{"id": "rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "up", "namespace": "sub2api_client", "tool": "exec_command", "arguments": map[string]any{"cmd": "printf ok"}}})
		select {
		case <-wire.writes:
		case <-ctx.Done():
			return
		}
		cloudWireComplete(wire)
	}()
	ended := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			ended <- err
			return
		}
		defer conn.CloseNow()
		_, first, err := conn.Read(ctx)
		if err != nil {
			ended <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		c.Set("api_key", in.APIKey)
		hooks := &OpenAIWSIngressHooks{PrepareCloudTurn: func(ctx context.Context, _ int, _ []byte, _ string) (context.Context, error) {
			return svc.CaptureCloudBillingSnapshot(ctx, in)
		}}
		ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
	}))
	defer server.Close()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	send := func(body map[string]any) gjson.Result {
		body["type"] = "response.create"
		frame, err := json.Marshal(body)
		require.NoError(t, err)
		require.NoError(t, conn.Write(ctx, coderws.MessageText, frame))
		for {
			_, event, err := conn.Read(ctx)
			require.NoError(t, err)
			kind := gjson.GetBytes(event, "type").String()
			require.NotEqual(t, "error", kind, string(event))
			require.NotEqual(t, "response.failed", kind, string(event))
			if kind == "response.completed" {
				return gjson.GetBytes(event, "response")
			}
		}
	}
	first := send(cloudLiveClone(fixture.Request))
	call := first.Get(`output.#(type=="function_call")`)
	require.Equal(t, "exec_command", call.Get("name").String(), first.Raw)
	follow := cloudLiveClone(fixture.Request)
	follow["previous_response_id"] = first.Get("id").String()
	follow["input"] = []any{map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "ok"}}
	last := send(follow)
	require.Equal(t, "gpt-6-astra", last.Get("model").String())
	<-upstream
	require.Equal(t, 1, store.settlementCount())
	require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-ended:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

// Like native /responses, Cloud parsing does not invent a request size cap: the
// ingress body limit bounds the request. Only a command that must cross to an
// owner on another instance is bounded, by OpenAICloudMaxCommandBytes, and the
// stored encoding does not inflate HTML characters.
func TestCloudRequestSizeIsBoundedByIngressAndCrossInstanceCommandOnly(t *testing.T) {
	text := strings.Repeat("x", 5<<20)
	body := []byte(`{"model":"m","input":"` + text + `"}`)
	parsed, err := parseCloudProtocolRequest(body)
	require.NoError(t, err)
	require.Equal(t, text, parsed.inputMessage["content"].([]any)[0].(map[string]any)["text"])
	previous, calls, err := cloudResponseReferences(body)
	require.NoError(t, err)
	require.Empty(t, previous)
	require.Empty(t, calls)
	normalized, err := (&OpenAIGatewayService{}).normalizeCloudResponsesIngress(body)
	require.NoError(t, err)
	require.Equal(t, len(body), len(normalized))

	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, AccountID: 3, GrantGeneration: "g", Model: "m", SettingsDigest: "d"}
	request := func(body string) OpenAICloudRequest {
		return OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"m","input":"` + body + `"}`), RootRequestID: "root", BillingSnapshot: json.RawMessage(`{"snapshot":true}`)}
	}
	require.True(t, cloudCommandFits(request("small")))
	require.True(t, cloudCommandFits(request(text)), "the former 4 MiB + 512 KiB envelope no longer applies")
	require.True(t, cloudCommandFits(request(strings.Repeat("x", (15<<20)+(512<<10)))))
	require.False(t, cloudCommandFits(request(strings.Repeat("x", 16<<20))), "a cross-instance command is bounded by OpenAICloudMaxCommandBytes")
	// The stored command is compact JSON without HTML escaping: json.Marshal
	// would grow this body sixfold, the store keeps it at its request size.
	angle := request(strings.Repeat("<", 1<<20))
	require.True(t, cloudCommandFits(angle))
	encoded, err := OpenAICloudEncodeCommand(OpenAICloudCommand{RequestID: "id", Request: angle})
	require.NoError(t, err)
	require.Less(t, len(encoded), len(angle.Body)+(4<<10))
	require.NotContains(t, string(encoded), `\u003c`)
	var decoded OpenAICloudCommand
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, string(angle.Body), string(decoded.Request.Body))
	escaped, err := json.Marshal(OpenAICloudCommand{RequestID: "id", Request: angle})
	require.NoError(t, err)
	require.Greater(t, len(escaped), 6<<20)
	// Control commands always fit; a business command this size is refused.
	require.False(t, cloudCommandFits(request(strings.Repeat("x", 17<<20))))
	require.True(t, cloudCommandFits(OpenAICloudRequest{Owner: owner}))
}

// The local inbox carries the same entry bounds as the Redis command stream:
// business commands stop at OpenAICloudMaxCommands, cancel and close keep
// reserved slots, and an exited actor refuses instead of blocking.
func TestCloudLocalInboxKeepsCommandStreamBounds(t *testing.T) {
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, AccountID: 3, GrantGeneration: "g", Model: "m", SettingsDigest: "d"}
	actor := &cloudSessionActor{done: make(chan struct{}), inbox: make(chan OpenAICloudCommand, OpenAICloudMaxCommands+OpenAICloudControlCommands)}
	s := &OpenAICloudSessions{store: &cloudActorTestStore{}, actors: map[string]*cloudSessionActor{"local": actor}}
	business := OpenAICloudCommand{RequestID: "work", Request: OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"m","input":"hi"}`)}}
	for i := 0; i < OpenAICloudMaxCommands; i++ {
		require.NoError(t, s.enqueue(t.Context(), "local", business))
	}
	require.ErrorIs(t, s.enqueue(t.Context(), "local", business), ErrOpenAICloudCommandRejected)
	require.NoError(t, s.enqueue(t.Context(), "local", OpenAICloudCommand{RequestID: "work", Request: OpenAICloudRequest{Owner: owner}, Cancel: true}))
	require.NoError(t, s.enqueue(t.Context(), "local", OpenAICloudCommand{RequestID: "close", Request: OpenAICloudRequest{Owner: owner}, CloseCompleted: true}))
	require.Len(t, actor.inbox, OpenAICloudMaxCommands+2)
	require.Zero(t, s.store.(*cloudActorTestStore).enqueueCalls, "a local owner never receives commands through the store")
	// Without a local actor the command goes to the store, unless it cannot be
	// stored at all: that is a definite rejection, not a transport failure.
	require.NoError(t, s.enqueue(t.Context(), "remote", business))
	require.Equal(t, 1, s.store.(*cloudActorTestStore).enqueueCalls)
	huge := OpenAICloudCommand{RequestID: "huge", Request: OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"m","input":"` + strings.Repeat("x", 16<<20) + `"}`)}}
	require.ErrorIs(t, s.enqueue(t.Context(), "remote", huge), ErrOpenAICloudCommandRejected)
	require.Equal(t, 1, s.store.(*cloudActorTestStore).enqueueCalls)
	close(actor.done)
	for len(actor.inbox) < cap(actor.inbox) {
		actor.inbox <- business
	}
	require.ErrorIs(t, actor.deliver(OpenAICloudCommand{RequestID: "late", Request: OpenAICloudRequest{Owner: owner}, Cancel: true}), ErrOpenAICloudCommandRejected)
}
