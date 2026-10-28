package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudCachedResponseRejectsInvalidItemsBeforeEmitting(t *testing.T) {
	for _, item := range []any{
		map[string]any{"type": "future_tool"},
		map[string]any{"type": "custom_tool_call", "input": 42},
		map[string]any{"type": "function_call", "arguments": nil},
		map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": 42}}},
	} {
		store := &cloudActorTestStore{}
		actor := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, ctx: t.Context()}
		raw, err := json.Marshal(map[string]any{"status": "completed", "output": []any{map[string]any{"id": "old", "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "already received"}}}, item}})
		require.NoError(t, err)
		err = actor.replayCompletedResponse("retry", &OpenAICloudResult{Response: raw})
		require.Error(t, err)
		require.Empty(t, store.results, "a corrupt cached response must not publish its valid prefix and change client history")
		require.Empty(t, store.settlements)
	}
}

func TestCloudCachedToolResultReplaysCompleteStreamingLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name     string
		awaiting bool
		custom   bool
	}{
		{name: "terminal answer"},
		{name: "next tool batch", awaiting: true},
		{name: "next custom tool batch", awaiting: true, custom: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools := append([]any{}, cloudHistoryTools...)
			if tc.custom {
				tools = append(tools, cloudApplyPatchTool)
			}
			protocolBody, err := json.Marshal(map[string]any{"model": "gpt-test", "input": "go", "tools": tools})
			require.NoError(t, err)
			protocol, err := parseCloudProtocolRequest(protocolBody)
			require.NoError(t, err)
			output := []any{map[string]any{"id": "msg_cached", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "retained answer", "annotations": []any{}}}}}
			const patch = "*** Begin Patch\n*** Add File: a.txt\n+hi\n*** End Patch\n"
			if tc.custom {
				output = append(output, map[string]any{"id": "ctc_cached", "type": "custom_tool_call", "namespace": "local", "name": "apply_patch", "call_id": "call_cloud_next", "input": patch, "status": "completed"})
			} else if tc.awaiting {
				output = append(output, map[string]any{"id": "fc_cached", "type": "function_call", "namespace": "local", "name": "echo", "call_id": "call_cloud_next", "arguments": `{"value":"next"}`, "status": "completed"})
			}
			response, err := json.Marshal(map[string]any{"id": "resp_cloud_cached", "status": "completed", "model": "gpt-test", "output": output, "usage": nil})
			require.NoError(t, err)
			result := &OpenAICloudResult{SessionID: "session", RootRequestID: "root", Response: response, AwaitingTools: tc.awaiting, Terminal: !tc.awaiting}
			outputs := map[string]string{"call_cloud_submitted": `"accepted"`}
			canonical, _ := json.Marshal(outputs)
			store := &cloudActorTestStore{refs: map[string]bool{}}
			actor := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "root", Status: "completed"}, Sequence: 17}, protocol: protocol, tools: map[string]*cloudPendingTool{}, history: []any{map[string]any{"type": "function_call_output", "call_id": "call_cloud_submitted", "output": "accepted"}}, completed: map[string]*OpenAICloudResult{"tools:" + cloudDigest(canonical): result}, ctx: t.Context()}
			wire := newCloudRPCFakeConn()
			actor.rpc = newCloudRPC(t.Context(), wire)
			t.Cleanup(func() { _ = actor.rpc.Close() })
			body := json.RawMessage(`{"model":"gpt-test","stream":true,"previous_response_id":"resp_cloud_before","input":[{"type":"function_call_output","call_id":"call_cloud_submitted","output":"accepted"}]}`)
			require.NoError(t, actor.handleCommand(OpenAICloudCommand{RequestID: "retry", Request: OpenAICloudRequest{Body: body}}))
			require.Empty(t, wire.writes, "cached replay must not send an upstream RPC or re-execute a client tool")
			require.Empty(t, store.settlements)
			require.Nil(t, store.record, "cached replay must not rewrite the session or create another bill")
			require.EqualValues(t, 17, actor.record.Sequence)
			require.Empty(t, actor.requestID)
			types := []string{}
			var text, arguments, customInput string
			var terminal json.RawMessage
			for _, event := range store.results {
				if event.Result != nil {
					require.True(t, event.Result.Replayed)
					require.Equal(t, result.Response, event.Result.Response)
					require.Equal(t, result.RootRequestID, event.Result.RootRequestID)
					require.False(t, result.Replayed)
					continue
				}
				sequence := gjson.GetBytes(event.Event, "sequence_number").Int()
				require.EqualValues(t, len(types), sequence)
				typ := gjson.GetBytes(event.Event, "type").String()
				types = append(types, typ)
				switch typ {
				case "response.output_text.delta":
					text += gjson.GetBytes(event.Event, "delta").String()
				case "response.function_call_arguments.delta":
					arguments += gjson.GetBytes(event.Event, "delta").String()
				case "response.custom_tool_call_input.delta":
					require.Equal(t, "ctc_cached", gjson.GetBytes(event.Event, "item_id").String())
					require.EqualValues(t, 1, gjson.GetBytes(event.Event, "output_index").Int())
					customInput += gjson.GetBytes(event.Event, "delta").String()
				case "response.custom_tool_call_input.done":
					require.Equal(t, "ctc_cached", gjson.GetBytes(event.Event, "item_id").String())
					require.EqualValues(t, 1, gjson.GetBytes(event.Event, "output_index").Int())
					require.Equal(t, patch, gjson.GetBytes(event.Event, "input").String())
				case "response.output_item.added":
					if gjson.GetBytes(event.Event, "item.type").String() == "custom_tool_call" {
						require.Equal(t, "in_progress", gjson.GetBytes(event.Event, "item.status").String())
						require.True(t, gjson.GetBytes(event.Event, "item.input").Exists())
						require.Empty(t, gjson.GetBytes(event.Event, "item.input").String())
					}
				case "response.completed":
					terminal = json.RawMessage(gjson.GetBytes(event.Event, "response").Raw)
				}
			}
			require.Equal(t, "response.created", types[0])
			require.Equal(t, "response.in_progress", types[1])
			require.Equal(t, "response.completed", types[len(types)-1])
			require.Equal(t, "retained answer", text)
			require.JSONEq(t, string(response), string(terminal))
			require.Contains(t, types, "response.output_item.added")
			require.Contains(t, types, "response.content_part.added")
			require.Contains(t, types, "response.output_text.done")
			require.Contains(t, types, "response.content_part.done")
			require.Contains(t, types, "response.output_item.done")
			if tc.custom {
				require.Equal(t, []string{"response.output_item.added", "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done", "response.output_item.done", "response.completed"}, types[len(types)-5:])
				require.Equal(t, patch, customInput)
				require.Contains(t, types, "response.custom_tool_call_input.done")
				require.NotContains(t, types, "response.function_call_arguments.delta")
				require.NotContains(t, types, "response.function_call_arguments.done")
				require.Equal(t, "local", gjson.GetBytes(terminal, "output.1.namespace").String())
				require.Equal(t, "apply_patch", gjson.GetBytes(terminal, "output.1.name").String())
				require.Equal(t, patch, gjson.GetBytes(terminal, "output.1.input").String())
			} else if tc.awaiting {
				require.Equal(t, `{"value":"next"}`, arguments)
				require.Contains(t, types, "response.function_call_arguments.done")
				require.Equal(t, "local", gjson.GetBytes(terminal, "output.1.namespace").String())
			} else {
				require.Empty(t, arguments)
			}
			require.NotNil(t, store.results[len(store.results)-1].Result, "only conclude Run after the final public SSE event")
		})
	}
}
