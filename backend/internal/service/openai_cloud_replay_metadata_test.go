//go:build unit

package service

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Codex Desktop replays assistant text with the logprobs array the Responses
// API returned (production, account 43). It is output telemetry with no
// execution meaning: accepted, never injected, and ignored by history matching.
func TestCloudAcceptsReplayedOutputLogprobs(t *testing.T) {
	for name, logprobs := range map[string]any{"empty": []any{}, "null": nil, "values": []any{map[string]any{"token": "ok", "logprob": -0.1, "bytes": []any{111, 107}, "top_logprobs": []any{}}}} {
		t.Run(name, func(t *testing.T) {
			p, err := cloudShapeParse(t, map[string]any{"input": []any{
				cloudUserMessage("hello"),
				map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "hi", "annotations": []any{}, "logprobs": logprobs}}},
				cloudUserMessage("again"),
			}})
			require.NoError(t, err)
			injected, err := json.Marshal(cloudInjectionHistory(p))
			require.NoError(t, err)
			require.NotContains(t, string(injected), "logprobs")
			require.Equal(t, "hi", gjson.GetBytes(injected, "1.content.0.text").String())
		})
	}
	_, err := cloudShapeParse(t, map[string]any{"input": []any{
		map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "hi", "logprobs": "not-a-list"}}},
		cloudUserMessage("again"),
	}})
	require.Error(t, err, "only the Responses logprobs shape is accepted")
	_, err = cloudShapeParse(t, map[string]any{"input": []any{
		map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi", "logprobs": []any{}}}},
	}})
	require.Error(t, err, "user input never carries output logprobs")
}

// Production (account 4, a brand-new Codex 0.164 conversation): Cloud discarded
// every thread while injecting only the opening developer/user messages, which
// carried no ciphertext but did carry Codex's per-message attribution metadata.
// That metadata has no execution effect and the thread never needs it; it is
// not injected, while the real 0.164 shape is still accepted.
func TestCloudInjectionOmitsAttributionMetadata(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_desktop.json")
	require.NoError(t, err)
	var fixture struct {
		Request map[string]any `json:"request"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.Contains(t, string(raw), cloudMessageMetadataKey, "the captured request carries attribution metadata")
	body, err := json.Marshal(fixture.Request)
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(body)
	require.NoError(t, err)
	require.NotEmpty(t, p.history)
	injected, err := json.Marshal(cloudInjectionHistory(p))
	require.NoError(t, err)
	require.NotContains(t, string(injected), cloudMessageMetadataKey)
	require.Equal(t, "developer", gjson.GetBytes(injected, "0.role").String(), "the messages themselves are injected")

	history, err := cloudShapeParse(t, map[string]any{"input": []any{
		cloudUserMessage("hello"),
		map[string]any{"type": "function_call", "call_id": "call_1", "name": "echo", "arguments": "{}", cloudMessageMetadataKey: map[string]any{"turn_id": "turn_x"}},
		map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "ok", cloudMessageMetadataKey: map[string]any{"turn_id": "turn_x"}},
		map[string]any{"type": "custom_tool_call", "call_id": "call_2", "name": "apply_patch", "input": "*** Begin Patch", cloudMessageMetadataKey: map[string]any{"turn_id": "turn_x"}},
		map[string]any{"type": "custom_tool_call_output", "call_id": "call_2", "output": "done", cloudMessageMetadataKey: map[string]any{"turn_id": "turn_x"}},
		map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": "gAAAA", cloudMessageMetadataKey: map[string]any{"turn_id": "turn_x"}},
		cloudUserMessage("again"),
	}, "tools": cloudHistoryTools})
	require.NoError(t, err)
	encoded, err := json.Marshal(cloudInjectionHistory(history))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), cloudMessageMetadataKey, "no injected item type carries it")
}

// A new thread receives the opening messages without metadata, and a later
// replay carrying metadata and logprobs still continues that same thread.
func TestCloudReplayWithMetadataAndLogprobsContinuesThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	upstream := make(chan *cloudRPCFakeConn, 1)
	injected := make(chan string, 1)
	go func() {
		wire := h.nextWire()
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		inject := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "thread/inject_items", inject.Method)
		injected <- string(inject.Params)
		cloudTestRead(wire, map[string]any{"id": inject.ID, "result": map[string]any{}})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		cloudWireCompleteTurn(wire, "thread", "turn", 12)
		upstream <- wire
	}()
	metadata := map[string]any{"turn_id": "01a16e2d-362f-7323-99be-694887db4258", "create_time": 1792849821.037444, "content_item_kinds": []any{"generic.developer_instructions"}}
	developer := map[string]any{"type": "message", "id": "msg_01a16e2d-3630-7b90-a8d2-52a3f3d0c2a1", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "instructions"}}, cloudMessageMetadataKey: metadata}
	user := map[string]any{"type": "message", "id": "msg_01a16e2d-3631-7b90-a8d2-52a3f3d0c2a2", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hi"}}, cloudMessageMetadataKey: metadata}
	first := []any{developer, user}
	response, err := h.invoke(map[string]any{"model": "client-model", "input": first, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	require.NotContains(t, <-injected, cloudMessageMetadataKey, "the opening messages reach Cloud without attribution metadata")
	wire := <-upstream

	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	next := append([]any{}, first...)
	for _, item := range response.Get("output").Array() {
		replayed := item.Value().(map[string]any)
		replayed[cloudMessageMetadataKey] = map[string]any{"turn_id": "01a16e2d-362f-7323-99be-694887db4258"}
		if parts, ok := replayed["content"].([]any); ok {
			for _, raw := range parts {
				if part, ok := raw.(map[string]any); ok && part["type"] == "output_text" {
					part["logprobs"] = []any{}
				}
			}
		}
		next = append(next, replayed)
	}
	next = append(next, cloudUserMessage("again"))
	second, err := h.invoke(map[string]any{"model": "client-model", "input": next, "tools": cloudHistoryTools})
	require.NoError(t, err, second.Raw)
	require.Equal(t, "completed", second.Get("status").String(), second.Raw)
	<-done
	h.noNewWire(200 * time.Millisecond)
	require.Equal(t, 2, h.store.settlementCount())
}

// The rejection log must show which item shapes Cloud refused, never content.
func TestOpenAIHistoryShapeListsKeysWithoutContent(t *testing.T) {
	shape := openAIHistoryShape([]any{
		map[string]any{"type": "message", "role": "developer", "id": "msg_secret_id", "content": []any{map[string]any{"type": "input_text", "text": "private instructions"}}, cloudMessageMetadataKey: map[string]any{"turn_id": "t"}},
		map[string]any{"type": "reasoning", "encrypted_content": "gAAAA-private"},
	})
	require.Contains(t, shape, "items=2 encrypted=1")
	require.Contains(t, shape, "message[content,id,"+cloudMessageMetadataKey+",role,type]=1")
	for _, secret := range []string{"private", "msg_secret_id", "gAAAA"} {
		require.False(t, strings.Contains(shape, secret), shape)
	}
}
