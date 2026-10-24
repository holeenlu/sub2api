package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func cloudHostedActorForTest(t *testing.T) (*cloudSessionActor, *cloudActorTestStore, *cloudRPCFakeConn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	t.Cleanup(func() { rpc.Close() })
	cloudTestInitialize(t, ctx, rpc, wire)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"web_search"},{"type":"image_generation"},{"type":"function","name":"echo","parameters":{"type":"object"}}],"include":["reasoning.encrypted_content","web_search_call.action.sources"]}`))
	require.NoError(t, err)
	store := &cloudActorTestStore{refs: make(map[string]bool)}
	owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", Model: "gpt-test", SettingsDigest: "settings"}
	a := &cloudSessionActor{service: &OpenAICloudSessions{store: store},
		record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "session", RootRequestID: "root", ThreadID: "thread", TurnID: "turn", Status: "running"},
			Usage: &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}},
		rpc: rpc, protocol: p, ctx: ctx, requestID: "http", requestDigest: "digest", responseID: "resp_cloud_test",
		items: map[string]int{}, tools: map[string]*cloudPendingTool{}, completed: map[string]*OpenAICloudResult{}}
	return a, store, wire
}

func cloudHostedEventForTest(t *testing.T, a *cloudSessionActor, method string, fields map[string]any, rpcID string) error {
	t.Helper()
	if _, exists := fields["threadId"]; !exists {
		fields["threadId"] = a.record.ThreadID
	}
	if method != "turn/completed" {
		if _, exists := fields["turnId"]; !exists {
			fields["turnId"] = a.record.TurnID
		}
	}
	raw, err := json.Marshal(fields)
	require.NoError(t, err)
	return a.handleEvent(cloudRPCMessage{Method: method, Params: raw, ID: json.RawMessage(rpcID)})
}

func cloudHostedWireForTest(t *testing.T, a *cloudSessionActor, store *cloudActorTestStore) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Output  []map[string]any
		History []any
		Events  []OpenAICloudStreamEvent
	}{a.output, a.history, store.results})
	require.NoError(t, err)
	return string(raw)
}

func cloudHostedEventTypesForTest(store *cloudActorTestStore) []string {
	var kinds []string
	for _, event := range store.results {
		if len(event.Event) > 0 {
			kinds = append(kinds, gjson.GetBytes(event.Event, "type").String())
		}
	}
	return kinds
}

func TestCloudHostedIgnoresForeignThreadsAndStaleTurnsWithoutRPCReplies(t *testing.T) {
	for _, foreign := range []string{"thread", "turn"} {
		t.Run(foreign, func(t *testing.T) {
			a, store, wire := cloudHostedActorForTest(t)
			for _, event := range []struct {
				method, rpcID string
				fields        map[string]any
			}{
				{"item/tool/call", `"other-rpc"`, map[string]any{"callId": "other-call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{"text": "other-owner"}}},
				{"undeclared/server/request", `"other-server-rpc"`, map[string]any{}},
				{"model/rerouted", "", map[string]any{}},
				{"error", "", map[string]any{"willRetry": false, "error": map[string]any{"codexErrorInfo": "unauthorized", "message": "other-owner"}}},
				{"thread/tokenUsage/updated", "", map[string]any{"tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10000}}}},
				{"rawResponseItem/completed", "", map[string]any{"item": map[string]any{"type": "reasoning", "id": "foreign", "encrypted_content": "other-owner"}}},
				{"item/agentMessage/delta", "", map[string]any{"itemId": "foreign", "delta": "other-owner"}},
				{"item/completed", "", map[string]any{"item": map[string]any{"type": "webSearch", "id": "foreign", "action": map[string]any{"type": "search", "query": "other-owner"}}}},
				{"item/completed", "", map[string]any{"item": map[string]any{"type": "futureItem", "id": map[string]any{"future": "identity"}, "text": []any{"foreign future payload"}}}},
				{"turn/completed", "", map[string]any{"turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": "serverOverloaded"}}}},
			} {
				if foreign == "thread" {
					event.fields["threadId"] = "another-thread"
				} else if event.method == "turn/completed" {
					event.fields["turn"].(map[string]any)["id"] = "old-turn"
				} else {
					event.fields["turnId"] = "old-turn"
				}
				require.NoError(t, cloudHostedEventForTest(t, a, event.method, event.fields, event.rpcID), event.method)
			}
			require.Equal(t, "running", a.record.Status)
			require.Equal(t, "http", a.requestID)
			require.Equal(t, 10, a.record.Usage.InputTokens)
			require.False(t, a.usageTainted)
			require.Empty(t, a.output)
			require.Empty(t, a.history)
			require.Empty(t, a.tools)
			require.Empty(t, a.publicFailure)
			require.Empty(t, store.results)
			require.Empty(t, store.settlements)
			select {
			case raw := <-wire.writes:
				t.Fatalf("foreign server request received a reply: %s", raw)
			default:
			}
		})
	}
}

func TestCloudRawItemsWhitelistEncryptedHistoryAndHideHostedImplementation(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	for _, role := range []string{"system", "developer", "user"} {
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{
			"type": "message", "role": role, "content": []any{map[string]any{"type": "input_text", "text": "private-" + role}}}}, ""))
	}
	for _, kind := range []string{"custom_tool_call", "custom_tool_call_output", "function_call", "function_call_output"} {
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{
			"type": kind, "id": "internal", "name": "exec", "input": "private-command", "output": "private-tool-output"}}, ""))
	}
	require.Empty(t, a.output)
	require.Empty(t, a.tools, "hosted exec is never exposed as a client function")
	require.Empty(t, store.results)
	for _, kind := range []string{"reasoning", "compaction"} {
		item := map[string]any{"type": kind, "id": kind + "-id", "encrypted_content": "opaque-" + kind,
			"content": []any{map[string]any{"type": "reasoning_text", "text": "private-raw-reasoning"}},
			"internal_chat_message_metadata_passthrough": map[string]any{"secret": "private-metadata"}, "summary": []any{map[string]any{"type": "summary_text", "text": "Public summary", "private": "private-summary-metadata"}}}
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": item}, ""))
		before := len(store.results)
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": item}, ""))
		require.Len(t, store.results, before, "duplicate raw completion is not emitted twice")
	}
	require.Len(t, a.output, 2)
	require.Equal(t, "opaque-reasoning", a.output[0]["encrypted_content"])
	require.Equal(t, "opaque-compaction", a.output[1]["encrypted_content"])
	require.Equal(t, []any{map[string]any{"type": "summary_text", "text": "Public summary"}}, a.output[0]["summary"])
	require.NotContains(t, a.output[0], "content")
	require.NotContains(t, a.output[0], "internal_chat_message_metadata_passthrough")
	require.NotContains(t, a.output[1], "summary")
	request, err := json.Marshal(map[string]any{"model": "gpt-test", "input": []any{a.output[1], map[string]any{"role": "user", "content": "continue"}}})
	require.NoError(t, err)
	replayed, err := parseCloudProtocolRequest(request)
	require.NoError(t, err, "the public compaction output must be accepted as stateless history")
	require.Equal(t, a.output[1], replayed.history[0])
	wireOutput := cloudHostedWireForTest(t, a, store)
	for _, secret := range []string{"private-system", "private-developer", "private-user", "private-command", "private-tool-output", "private-raw-reasoning", "private-metadata", "private-summary-metadata"} {
		require.NotContains(t, wireOutput, secret)
	}
	require.Error(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{
		"type": "reasoning", "id": "reasoning-id", "summary": []any{}, "encrypted_content": "different"}}, ""))
	select {
	case raw := <-wire.writes:
		t.Fatalf("raw hosted tool execution produced an RPC reply: %s", raw)
	default:
	}
}

func TestCloudRawStoreFalseReasoningKeepsOpaqueContextWithoutLegacyInclude(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.protocol.includeEncryptedReasoning = false
	require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{
		"type": "reasoning", "id": "rs", "summary": []any{}, "encrypted_content": "opaque-history"}}, ""))
	require.Len(t, a.output, 1)
	require.Equal(t, "opaque-history", a.output[0]["encrypted_content"], "Cloud store:false preserves encrypted continuation state without requiring the legacy include flag")
	require.Contains(t, cloudHostedWireForTest(t, a, store), "opaque-history")
	require.Len(t, a.history, 1)
	require.Equal(t, "opaque-history", a.history[0].(map[string]any)["encrypted_content"], "the retained context keeps the original opaque state")
}

func TestCloudHostedExecCannotBecomeAnUndeclaredClientFunction(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	require.Error(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{
		"callId": "internal-call", "namespace": cloudClientToolNamespace, "tool": "exec", "arguments": map[string]any{"code": "private-hosted-code"}}, `"owned-rpc"`))
	require.Empty(t, a.tools)
	require.Empty(t, a.output)
	require.Empty(t, store.results)
	select {
	case raw := <-wire.writes:
		require.Equal(t, "owned-rpc", gjson.GetBytes(raw, "id").String())
		require.Equal(t, int64(-32601), gjson.GetBytes(raw, "error.code").Int())
		require.NotContains(t, string(raw), "private-hosted-code")
	default:
		t.Fatal("an undeclared RPC owned by this turn must receive a safe rejection")
	}
}

func TestCloudClientToolNamespaceMustMatchAndNeverLeaksIntoPublicCalls(t *testing.T) {
	for name, namespace := range map[string]any{"missing": nil, "empty": "", "public_namespace": "local", "different_private": "sub2api_client_other"} {
		t.Run(name, func(t *testing.T) {
			a, store, wire := cloudHostedActorForTest(t)
			fields := map[string]any{"callId": "call", "tool": "echo", "arguments": map[string]any{}}
			if namespace != nil {
				fields["namespace"] = namespace
			}
			require.Error(t, cloudHostedEventForTest(t, a, "item/tool/call", fields, `"rpc"`))
			require.Empty(t, a.tools)
			require.Empty(t, a.history)
			require.Empty(t, store.results)
			select {
			case raw := <-wire.writes:
				require.Equal(t, int64(-32601), gjson.GetBytes(raw, "error.code").Int())
			default:
				t.Fatal("an owned RPC with the wrong namespace must receive a safe rejection")
			}
		})
	}
	a, _, _ := cloudHostedActorForTest(t)
	p, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}]}`))
	require.NoError(t, err)
	a.protocol = p
	name := p.tools[0].(map[string]any)["name"].(string)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{
		"callId": "upstream-call", "namespace": cloudClientToolNamespace, "tool": name, "arguments": map[string]any{"text": "ok"}}, `"rpc"`))
	require.True(t, a.record.Result.AwaitingTools)
	response := a.record.Result.Response
	require.Equal(t, "function_call", gjson.GetBytes(response, "output.0.type").String())
	require.Equal(t, "local", gjson.GetBytes(response, "output.0.namespace").String())
	require.Equal(t, "echo", gjson.GetBytes(response, "output.0.name").String())
	require.NotContains(t, string(response), cloudClientToolNamespace)
	require.True(t, strings.HasPrefix(gjson.GetBytes(response, "output.0.call_id").String(), "call_cloud_"))
	require.Equal(t, name, a.history[0].(map[string]any)["name"])
	require.NotContains(t, a.history[0].(map[string]any), "namespace")
}

func TestCloudHostedSearchLifecycleSourcesAndCitations(t *testing.T) {
	for _, includeSources := range []bool{true, false} {
		a, store, _ := cloudHostedActorForTest(t)
		a.protocol.includeSearchSources = includeSources
		item := map[string]any{"id": "search-ui", "type": "webSearch", "query": "docs",
			"action": map[string]any{"type": "search", "query": "docs", "internal": "private-action"},
			"results": []any{map[string]any{"url": "https://openai.com/docs", "title": "Official docs", "private": "private-result"},
				map[string]any{"url": "javascript:alert(1)", "title": "bad"}, map[string]any{"url": "https://secret:token@example.com", "title": "bad"}}}
		require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": item}, ""))
		require.Zero(t, a.hostedUsage.WebSearchCalls)
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""))
		before := len(store.results)
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""))
		require.Len(t, store.results, before)
		require.Equal(t, 1, a.hostedUsage.WebSearchCalls)
		require.Equal(t, 1, a.record.Usage.WebSearchCalls)
		require.Equal(t, []string{"response.output_item.added", "response.web_search_call.in_progress", "response.web_search_call.completed", "response.output_item.done"}, cloudHostedEventTypesForTest(store))
		action := a.output[0]["action"].(map[string]any)
		if includeSources {
			require.Equal(t, []any{map[string]any{"type": "url", "url": "https://openai.com/docs", "title": "Official docs"}}, action["sources"])
		} else {
			require.NotContains(t, action, "sources")
		}
		text := "官方文档"
		require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "message-ui", "delta": text}, ""))
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "message-ui", "type": "agentMessage", "text": text}}, ""))
		require.Equal(t, text, a.pendingMessages["message-ui"])
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{
			"type": "message", "id": "different-raw-id", "role": "assistant", "internal": "private-raw-message",
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{map[string]any{
				"type": "url_citation", "url": "https://openai.com/docs", "title": "Official docs", "start_index": 0, "end_index": 4, "private": "private-citation"}}}}}}, ""))
		require.Empty(t, a.pendingMessages)
		require.Contains(t, cloudHostedEventTypesForTest(store), "response.output_text.annotation.added")
		require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed", "error": nil}}, ""))
		require.Equal(t, "completed", a.record.Status)
		require.Len(t, store.settlements, 1)
		require.Equal(t, 1, store.settlements[0].Usage.WebSearchCalls)
		require.False(t, store.settlements[0].Unknown)
		require.Equal(t, "https://openai.com/docs", gjson.GetBytes(a.record.Result.Response, "output.1.content.0.annotations.0.url").String())
		for _, secret := range []string{"private-action", "private-result", "private-raw-message", "private-citation", "javascript:", "secret:token"} {
			require.NotContains(t, cloudHostedWireForTest(t, a, store), secret)
		}
	}
}

func TestCloudHostedImageUsesMeasuredDimensionsAndDeduplicatesBilling(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	for i, size := range []image.Point{{X: 24, Y: 16}, {X: 1025, Y: 8}} {
		var buffer bytes.Buffer
		require.NoError(t, png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, size.X, size.Y))))
		encoded := base64.StdEncoding.EncodeToString(buffer.Bytes())
		item := map[string]any{"type": "imageGeneration", "id": []string{"image-one", "image-two"}[i], "status": "completed", "result": encoded,
			"savedPath": "/private/cloud/generated.png", "internal": "private-image-metadata", "revisedPrompt": "A simple image"}
		require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": item}, ""))
		require.Equal(t, i, a.hostedUsage.ImageCount)
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""))
		before := len(store.results)
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""))
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"type": "image_generation_call", "id": "raw-image", "result": encoded}}, ""))
		require.Len(t, store.results, before, "UI duplicates and raw hosted mirrors do not add a charge or output")
		require.Equal(t, i+1, a.hostedUsage.ImageCount)
		require.Equal(t, encoded, a.output[i]["result"])
		require.Equal(t, "png", a.output[i]["output_format"])
		require.NotContains(t, a.output[i], "savedPath")
	}
	require.Equal(t, map[string]int{"1K": 1, "2K": 1}, a.record.Usage.ImageSizes)
	require.Equal(t, []string{"24x16", "1025x8"}, a.record.Usage.ImageOutputSizes)
	require.True(t, cloudBillingUsageValid(a.record.Usage))
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
	require.Len(t, store.settlements, 1)
	require.Equal(t, 2, store.settlements[0].Usage.ImageCount)
	require.False(t, store.settlements[0].Unknown)
	require.NotContains(t, cloudHostedWireForTest(t, a, store), "/private/cloud")
	require.NotContains(t, cloudHostedWireForTest(t, a, store), "private-image-metadata")
}

func TestCloudHostedImageFailureNeverInventsGenerationUsage(t *testing.T) {
	data := cloudProtocolTestPNG(t)
	pixelBomb := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(pixelBomb[16:20], 8192)
	binary.BigEndian.PutUint32(pixelBomb[20:24], 8192)
	binary.BigEndian.PutUint32(pixelBomb[29:33], crc32.ChecksumIEEE(pixelBomb[12:29]))
	_, err := png.DecodeConfig(bytes.NewReader(data[:33]))
	require.NoError(t, err, "a valid PNG header alone does not prove decodable pixels")
	for name, result := range map[string]string{
		"empty": "", "invalid_base64": "not-base64", "not_image": base64.StdEncoding.EncodeToString([]byte("private-invalid-image")),
		"header_only":      base64.StdEncoding.EncodeToString(data[:33]),
		"truncated_tail":   base64.StdEncoding.EncodeToString(data[:len(data)-5]),
		"trailing_data":    base64.StdEncoding.EncodeToString(append(append([]byte(nil), data...), 'x')),
		"pixel_bomb":       base64.StdEncoding.EncodeToString(pixelBomb),
		"base64_linebreak": base64.StdEncoding.EncodeToString(data) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			a, _, _ := cloudHostedActorForTest(t)
			require.Error(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{
				"id": "image", "type": "imageGeneration", "status": "completed", "result": result}}, ""))
			require.Zero(t, a.hostedUsage.ImageCount)
			require.Zero(t, a.record.Usage.ImageCount)
			require.Empty(t, a.output)
			require.Empty(t, a.hostedReady)
		})
	}
}

func TestCloudHostedCompletionRemainsDeduplicatedAcrossClientResponses(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	item := map[string]any{"id": "search", "type": "webSearch", "action": map[string]any{"type": "search", "query": "docs"}}
	require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""))
	require.Equal(t, 1, a.hostedUsage.WebSearchCalls)
	// A client function round trip resets the current HTTP output while the
	// same Cloud turn, tool root and completion ownership stay alive.
	a.output, a.items = nil, make(map[string]int)
	a.requestID, a.responseID = "next-http", "resp_cloud_next"
	before := len(store.results)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""))
	require.Empty(t, a.output)
	require.Len(t, store.results, before)
	require.Equal(t, 1, a.hostedUsage.WebSearchCalls)
	item["action"] = map[string]any{"type": "search", "query": "different"}
	require.Error(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": item}, ""), "conflicting completion facts are never silently substituted")
	require.Equal(t, 1, a.hostedUsage.WebSearchCalls)
}

func TestCloudHostedCompletionAndClientRPCInterleaveWithoutDeadlockOrRebilling(t *testing.T) {
	for _, completeBeforeReply := range []bool{true, false} {
		t.Run(map[bool]string{true: "completion during HTTP gap", false: "completion needs client reply"}[completeBeforeReply], func(t *testing.T) {
			a, store, wire := cloudHostedActorForTest(t)
			search := map[string]any{"id": "search", "type": "webSearch", "action": map[string]any{"type": "search", "query": "docs"}}
			require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": search}, ""))
			require.True(t, a.hasPendingHosted())
			require.Empty(t, a.output, "unverified hosted starts must not escape in a completed tool response")
			require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "upstream-call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{"text": "reply"}}, `"mixed-rpc"`))
			require.Empty(t, a.requestID, "a hosted operation cannot deadlock a server RPC that needs the client")
			require.True(t, a.record.Result.AwaitingTools)
			require.Empty(t, store.settlements)
			for _, item := range gjson.GetBytes(a.record.Result.Response, "output").Array() {
				require.Equal(t, "function_call", item.Get("type").String())
				require.Equal(t, "completed", item.Get("status").String())
			}
			previous := gjson.GetBytes(a.record.Result.Response, "id").String()
			callID := ""
			for id := range a.tools {
				callID = id
			}
			complete := func() {
				require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": search}, ""))
				require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": search}, ""))
				require.Equal(t, 1, a.hostedUsage.WebSearchCalls)
				require.Equal(t, 1, a.record.Usage.WebSearchCalls)
			}
			if completeBeforeReply {
				before := len(store.results)
				complete()
				require.Len(t, a.hostedReady, 1)
				require.Positive(t, a.hostedBufferedBytes)
				require.Len(t, store.results, before, "no public event may target the ended HTTP response")
			}
			raw, err := json.Marshal(map[string]any{"model": "gpt-test", "previous_response_id": previous, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": "reply"}}})
			require.NoError(t, err)
			require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "continued-http", Request: OpenAICloudRequest{Owner: a.record.OpenAICloudOwner, Body: raw, RootRequestID: "must-not-change-root"}}))
			message := cloudTestMessage(t, a.ctx, wire)
			require.JSONEq(t, `"mixed-rpc"`, string(message.ID))
			require.Equal(t, "root", a.record.RootRequestID)
			if !completeBeforeReply {
				complete()
			}
			require.Empty(t, a.hostedReady)
			require.Zero(t, a.hostedBufferedBytes)
			require.False(t, a.hasPendingHosted())
			require.Len(t, a.output, 1)
			require.Equal(t, "web_search_call", a.output[0]["type"])
			require.Equal(t, "completed", a.output[0]["status"])
			before := len(store.results)
			complete()
			require.Len(t, store.results, before, "repeated completion after flushing cannot republish or rebill")
			require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
			require.Len(t, store.settlements, 1)
			require.False(t, store.settlements[0].Unknown)
			require.Equal(t, 1, store.settlements[0].Usage.WebSearchCalls)
			require.True(t, a.record.Result.Terminal)
			require.True(t, a.record.Result.UsageCredible)
			counts := make(map[string]int)
			for _, kind := range cloudHostedEventTypesForTest(store) {
				counts[kind]++
			}
			require.Equal(t, 1, counts["response.web_search_call.in_progress"])
			require.Equal(t, 1, counts["response.web_search_call.completed"])
		})
	}
}

func TestCloudHostedGapFailureRetainsObservedConsumptionAsUnknown(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	search := map[string]any{"id": "search", "type": "webSearch", "action": map[string]any{"type": "search", "query": "docs"}}
	require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": search}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": map[string]any{"id": "image", "type": "imageGeneration"}}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "upstream-call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"pending-rpc"`))
	require.Empty(t, a.requestID)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": search}, ""))
	require.Error(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "image", "type": "imageGeneration", "status": "failed", "result": ""}}, ""))
	// The actor's bounded stop ends in this unknown settlement when no trusted
	// terminal meter arrives. A buffered valid result is still a billing fact.
	a.terminate("unknown", "hosted operation failed before final usage", true)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].Unknown)
	require.Equal(t, "root", store.settlements[0].RootRequestID)
	require.Equal(t, 1, store.settlements[0].Usage.WebSearchCalls)
	require.Zero(t, store.settlements[0].Usage.ImageCount)
}

func TestCloudUnfinishedHostedItemCannotSettleSuccessfulTurn(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": map[string]any{"id": "pending", "type": "webSearch"}}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
	require.Equal(t, "unknown", a.record.Status)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].Unknown)
	require.False(t, a.record.Result.UsageCredible)
	require.Equal(t, "failed", gjson.GetBytes(a.record.Result.Response, "status").String())
}

func TestCloudHostedOutputIdentityCollisionFailsWithoutPanicking(t *testing.T) {
	a, _, _ := cloudHostedActorForTest(t)
	require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": map[string]any{"id": "reused", "type": "webSearch"}}, ""))
	require.NotPanics(t, func() {
		require.Error(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "reused", "delta": "text"}, ""))
	})
}

func TestCloudHostedRetryNoticeWaitsAndTerminalFailureKeepsSafeCode(t *testing.T) {
	for _, success := range []bool{false, true} {
		a, store, _ := cloudHostedActorForTest(t)
		errorValue := map[string]any{"codexErrorInfo": "serverOverloaded", "message": "private-upstream-prompt", "additionalDetails": "private-token"}
		require.NoError(t, cloudHostedEventForTest(t, a, "error", map[string]any{"willRetry": true, "error": errorValue}, ""))
		require.Equal(t, "running", a.record.Status)
		require.Equal(t, "http", a.requestID)
		require.Empty(t, a.publicFailure)
		require.Empty(t, store.results)
		require.Empty(t, store.settlements)
		status := "completed"
		var finalError any
		if !success {
			status, finalError = "failed", errorValue
			require.NoError(t, cloudHostedEventForTest(t, a, "error", map[string]any{"willRetry": false, "error": errorValue}, ""))
			require.Equal(t, "running", a.record.Status, "an error notification alone does not prove termination")
			require.Empty(t, store.settlements)
		}
		require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": status, "error": finalError}}, ""))
		require.Len(t, store.settlements, 1)
		require.Equal(t, status, a.record.Status)
		require.Contains(t, cloudHostedEventTypesForTest(store), "response."+status)
		if success {
			require.Zero(t, a.record.Result.ErrorStatus)
			require.False(t, gjson.GetBytes(a.record.Result.Response, "error.code").Exists())
			require.Empty(t, store.settlements[0].ErrorCode)
		} else {
			require.Equal(t, 503, a.record.Result.ErrorStatus)
			require.Equal(t, "server_is_overloaded", gjson.GetBytes(a.record.Result.Response, "error.code").String())
			require.True(t, store.settlements[0].Unknown, "a final error does not prove complete metering")
		}
		require.NotContains(t, cloudHostedWireForTest(t, a, store), "private-upstream-prompt")
		require.NotContains(t, cloudHostedWireForTest(t, a, store), "private-token")
	}
}

func TestCloudAuthoritativeRejectionDoesNotCreateUnknownDebt(t *testing.T) {
	for _, observed := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejected input", true: "prior model work"}[observed], func(t *testing.T) {
			actor, store, _ := cloudHostedActorForTest(t)
			actor.record.Usage = nil
			actor.turnProducedOutput = observed
			err := cloudHostedEventForTest(t, actor, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"code": "invalid_encrypted_content", "codexErrorInfo": "badRequest"}}}, "")
			require.NoError(t, err)
			require.Len(t, store.settlements, 1)
			require.Equal(t, observed, store.settlements[0].Unknown)
			require.Equal(t, 400, actor.record.Result.ErrorStatus)
			require.Equal(t, "invalid_encrypted_content", gjson.GetBytes(actor.record.Result.Response, "error.code").String())
			require.Equal(t, gjson.Null, gjson.GetBytes(actor.record.Result.Response, "usage").Type)
			if !observed {
				require.NotNil(t, store.settlements[0].Usage)
				require.Zero(t, store.settlements[0].Usage.TotalTokens)
			}
		})
	}
}

func TestCloudRawMessagePhaseSurvivesPublicAndStatelessHistory(t *testing.T) {
	for _, phase := range []string{"commentary", "partial_answer", "final_answer"} {
		t.Run(phase, func(t *testing.T) {
			a, store, _ := cloudHostedActorForTest(t)
			require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "ui", "delta": "answer"}, ""))
			require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "ui", "type": "agentMessage", "text": "answer"}}, ""))
			require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"id": "raw", "type": "message", "role": "assistant", "phase": phase, "content": []any{map[string]any{"type": "output_text", "text": "answer"}}}}, ""))
			require.Equal(t, phase, a.output[0]["phase"])
			require.Equal(t, phase, a.history[0].(map[string]any)["phase"])
			var emitted bool
			for _, event := range store.results {
				if gjson.GetBytes(event.Event, "type").String() == "response.output_item.done" {
					require.Equal(t, phase, gjson.GetBytes(event.Event, "item.phase").String())
					emitted = true
				}
			}
			require.True(t, emitted)
			for _, history := range []any{a.output[0], a.history[0]} {
				body, err := json.Marshal(map[string]any{"model": "m", "input": []any{history, map[string]any{"role": "user", "content": "next"}}})
				require.NoError(t, err)
				parsed, err := parseCloudProtocolRequest(body)
				require.NoError(t, err)
				require.Equal(t, phase, parsed.history[0].(map[string]any)["phase"])
			}
		})
	}
}

func TestCloudRawMessagePhaseRemainsUnknownUnlessObserved(t *testing.T) {
	for name, phase := range map[string]any{"null": nil, "unknown": "future_phase", "empty": "", "non-string": 1} {
		t.Run(name, func(t *testing.T) {
			a, _, _ := cloudHostedActorForTest(t)
			require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "ui", "delta": "answer"}, ""))
			require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "ui", "type": "agentMessage", "text": "answer"}}, ""))
			err := cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"id": "ui", "type": "message", "role": "assistant", "phase": phase, "content": []any{map[string]any{"type": "output_text", "text": "answer"}}}}, "")
			if phase == nil {
				require.NoError(t, err)
				require.NotContains(t, a.output[0], "phase")
				require.NotContains(t, a.history[0].(map[string]any), "phase")
			} else {
				require.Error(t, err)
				require.Empty(t, a.history)
			}
		})
	}
}

func TestCloudRawPhaseAfterUIFallbackUpdatesExactRetainedMessage(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	for _, id := range []string{"first", "second"} {
		require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": id, "delta": "same text"}, ""))
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": id, "type": "agentMessage", "text": "same text"}}, ""))
	}
	require.NoError(t, a.flushPendingMessages())
	before := len(store.results)
	require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"id": "second", "type": "message", "role": "assistant", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": "same text"}}}}, ""))
	require.NotContains(t, a.output[0], "phase")
	require.NotContains(t, a.history[0].(map[string]any), "phase")
	require.Equal(t, "final_answer", a.output[1]["phase"])
	require.Equal(t, "final_answer", a.history[1].(map[string]any)["phase"])
	require.Len(t, store.results, before, "late metadata must not duplicate text/completion events")
}

func TestCloudSettlementRetainsSanitizedPolicyCode(t *testing.T) {
	actor, store, _ := cloudHostedActorForTest(t)
	require.NoError(t, cloudHostedEventForTest(t, actor, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": "cyberPolicy", "message": "private-error", "additionalDetails": "private-details"}}}, ""))
	require.Len(t, store.settlements, 1)
	settlement := store.settlements[0]
	require.Equal(t, "cyber_policy", settlement.ErrorCode)
	require.True(t, settlement.Unknown, "the error code never proves final consumption")
	raw, err := json.Marshal(settlement)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-error")
	require.NotContains(t, string(raw), "private-details")
	var persisted OpenAICloudSettlement
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.Equal(t, "cyber_policy", persisted.ErrorCode)
	require.True(t, persisted.Unknown)
}

func TestCloudPublicMessageHistoryRoundTrip(t *testing.T) {
	for _, cited := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty annotations", true: "search citation"}[cited], func(t *testing.T) {
			a, _, _ := cloudHostedActorForTest(t)
			annotations := []any{}
			if cited {
				annotations = append(annotations, map[string]any{"type": "url_citation", "url": "https://openai.com/docs", "title": "Docs", "start_index": 0, "end_index": 2})
			}
			require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "answer", "delta": "ok"}, ""))
			require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"type": "message", "id": "answer", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "ok", "annotations": annotations}}}}, ""))
			body, err := json.Marshal(map[string]any{"model": "gpt-test", "input": []any{a.output[0], map[string]any{"role": "user", "content": "next"}}})
			require.NoError(t, err)
			parsed, err := parseCloudProtocolRequest(body)
			require.NoError(t, err)
			require.NoError(t, a.checkHistory(parsed), "the public response must be valid unmodified history")
			part := parsed.history[0].(map[string]any)["content"].([]any)[0].(map[string]any)
			if !cited {
				delete(part, "annotations")
				require.NoError(t, a.checkHistory(parsed))
			}
			part["text"] = "changed"
			require.Error(t, a.checkHistory(parsed), "normalization cannot permit changed content")
		})
	}
}

func TestCloudSerialToolsBufferInterleavedTextAndReasoning(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	raw, err := json.Marshal(a.protocol.body)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	body["parallel_tool_calls"] = false
	raw, err = json.Marshal(body)
	require.NoError(t, err)
	a.protocol, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	for i, rpc := range []string{`"rpc-one"`, `"rpc-two"`, `"rpc-three"`} {
		require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": rpc, "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{"n": i}}, rpc))
	}
	require.Len(t, a.tools, 3)
	require.Len(t, gjson.GetBytes(a.record.Result.Response, `output.#(type=="function_call")#`).Array(), 1)
	require.Len(t, a.history, 1, "queued calls must not appear in the client's retained history before publication")
	require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "comment", "delta": "working"}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "comment", "type": "agentMessage", "text": "working"}}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"id": "comment", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "working", "annotations": []any{}}}}}, ""))
	require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"type": "reasoning", "id": "rs-gap", "summary": []any{}, "encrypted_content": "opaque"}}, ""))
	require.Len(t, a.bufferedEvents, 4)
	for i, rpc := range []string{`"rpc-one"`, `"rpc-two"`, `"rpc-three"`} {
		result := a.record.Result
		calls := gjson.GetBytes(result.Response, `output.#(type=="function_call")#`).Array()
		require.Len(t, calls, 1)
		raw, err = json.Marshal(map[string]any{"model": "gpt-test", "previous_response_id": a.lastResponseID, "input": []any{map[string]any{"type": "function_call_output", "call_id": calls[0].Get("call_id").String(), "output": "done"}}})
		require.NoError(t, err)
		command := OpenAICloudCommand{RequestID: rpc, Request: OpenAICloudRequest{Owner: a.record.OpenAICloudOwner, Body: raw}}
		require.NoError(t, a.handleCommand(command))
		require.JSONEq(t, rpc, string(cloudTestMessage(t, a.ctx, wire).ID))
		require.Empty(t, a.bufferedEvents)
		if i == 0 {
			require.Equal(t, "working", gjson.GetBytes(a.record.Result.Response, `output.#(type=="message").content.0.text`).String())
			require.Equal(t, "opaque", gjson.GetBytes(a.record.Result.Response, `output.#(type=="reasoning").encrypted_content`).String())
			command.RequestID = "retry"
			require.NoError(t, a.handleCommand(command))
			require.Empty(t, wire.writes, "retry cannot execute the tool again")
		}
	}
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
	require.Len(t, store.settlements, 1)
	require.False(t, store.settlements[0].Unknown)
}

func TestCloudLateRawMessageAfterToolResponsePreservesPublicHistory(t *testing.T) {
	for _, rawID := range []string{"ui-comment", "raw-comment"} {
		for _, duringWait := range []bool{true, false} {
			t.Run(rawID+map[bool]string{true: "/tool-wait", false: "/continued"}[duringWait], func(t *testing.T) {
				a, store, wire := cloudHostedActorForTest(t)
				require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": "ui-comment", "delta": "working"}, ""))
				require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "ui-comment", "type": "agentMessage", "text": "working"}}, ""))
				require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "rpc", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"rpc"`))
				require.Empty(t, a.requestID)
				var public struct {
					Output []any `json:"output"`
				}
				require.NoError(t, json.Unmarshal(a.record.Result.Response, &public))
				callID := gjson.GetBytes(a.record.Result.Response, `output.#(type=="function_call").call_id`).String()
				lateRaw := map[string]any{"item": map[string]any{"id": rawID, "type": "message", "role": "assistant", "phase": "commentary", "content": []any{map[string]any{"type": "output_text", "text": "working"}}}}
				if duringWait {
					require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", lateRaw, ""))
					require.Len(t, a.bufferedEvents, 1)
				}
				input := append(public.Output, map[string]any{"type": "function_call_output", "call_id": callID, "output": "done"})
				body, err := json.Marshal(map[string]any{"model": "gpt-test", "previous_response_id": a.lastResponseID, "input": input})
				require.NoError(t, err)
				require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: "tool-result", Request: OpenAICloudRequest{Owner: a.record.OpenAICloudOwner, Body: body}}))
				require.JSONEq(t, `"rpc"`, string(cloudTestMessage(t, a.ctx, wire).ID))
				before := len(store.results)
				require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", lateRaw, ""))
				require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", lateRaw, ""))
				require.Len(t, store.results, before, "late raw completions cannot repeat output from a closed response")
				require.Empty(t, a.output)
				require.Empty(t, a.fallbackMessages)
				require.Zero(t, a.fallbackMessageBytes)
				parsed, err := parseCloudProtocolRequest(body)
				require.NoError(t, err)
				require.NoError(t, a.checkHistory(parsed), "late metadata cannot change the client's already published history")
				require.Empty(t, store.settlements)
				require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
				require.Len(t, store.settlements, 1)
				require.False(t, store.settlements[0].Unknown)
			})
		}
	}
}

// A completed reasoning item also publishes the native summary event sequence.
// Clients in sequential-cutoff mode display summaries only from these events.
func TestCloudReasoningItemPublishesSummaryEvents(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{
		"type": "reasoning", "id": "rs_1", "encrypted_content": "opaque",
		"summary": []any{map[string]any{"type": "summary_text", "text": "first"}, map[string]any{"type": "summary_text", "text": "second"}}}}, ""))
	require.Equal(t, []string{
		"response.output_item.added",
		"response.reasoning_summary_part.added", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_summary_part.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_summary_part.done",
		"response.output_item.done",
	}, cloudHostedEventTypesForTest(store))
	var texts []string
	for _, event := range store.results {
		if gjson.GetBytes(event.Event, "type").String() == "response.reasoning_summary_text.done" {
			require.Equal(t, "rs_1", gjson.GetBytes(event.Event, "item_id").String())
			texts = append(texts, gjson.GetBytes(event.Event, "text").String())
		}
	}
	require.Equal(t, []string{"first", "second"}, texts)
}
