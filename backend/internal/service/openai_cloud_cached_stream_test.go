package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudCachedToolResultReplaysCompleteStreamingLifecycle(t *testing.T) {
	for _, awaiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "terminal answer", true: "next tool batch"}[awaiting], func(t *testing.T) {
			protocol, err := parseCloudProtocolRequest([]byte(`{"model":"gpt-test","input":"go","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`))
			require.NoError(t, err)
			output := []any{map[string]any{"id": "msg_cached", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "retained answer", "annotations": []any{}}}}}
			if awaiting {
				output = append(output, map[string]any{"id": "fc_cached", "type": "function_call", "namespace": "local", "name": "echo", "call_id": "call_cloud_next", "arguments": `{"value":"next"}`, "status": "completed"})
			}
			response, err := json.Marshal(map[string]any{"id": "resp_cloud_cached", "status": "completed", "model": "gpt-test", "output": output, "usage": nil})
			require.NoError(t, err)
			result := &OpenAICloudResult{SessionID: "session", RootRequestID: "root", Response: response, AwaitingTools: awaiting, Terminal: !awaiting}
			outputs := map[string]string{"call_cloud_submitted": `"accepted"`}
			canonical, _ := json.Marshal(outputs)
			store := &cloudActorTestStore{refs: map[string]bool{}}
			actor := &cloudSessionActor{service: &OpenAICloudSessions{store: store}, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{RootRequestID: "root", Status: "completed"}, Sequence: 17}, protocol: protocol, tools: map[string]*cloudPendingTool{"call_cloud_submitted": {Submitted: true, Digest: cloudDigest([]byte(`"accepted"`))}}, completed: map[string]*OpenAICloudResult{"tools:" + cloudDigest(canonical): result}, ctx: t.Context()}
			body := json.RawMessage(`{"model":"gpt-test","stream":true,"previous_response_id":"resp_cloud_before","input":[{"type":"function_call_output","call_id":"call_cloud_submitted","output":"accepted"}]}`)
			require.NoError(t, actor.handleCommand(OpenAICloudCommand{RequestID: "retry", Request: OpenAICloudRequest{Body: body}}))
			require.Empty(t, store.settlements)
			require.Nil(t, store.record, "cached replay must not rewrite the session or create another bill")
			require.EqualValues(t, 17, actor.record.Sequence)
			require.Empty(t, actor.requestID)
			types := []string{}
			var text, arguments string
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
			if awaiting {
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
