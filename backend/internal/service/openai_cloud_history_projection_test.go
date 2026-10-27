package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestCloudAnthropicReadHistoryRoundTripUsesPublicProjection(t *testing.T) {
	rawArgs := `{"file_path":"/tmp/example.txt","pages":""}`
	wire := apicompat.ResponsesToAnthropic(&apicompat.ResponsesResponse{Status: "completed", Output: []apicompat.ResponsesOutput{{Type: "function_call", CallID: "call_cloud_read", Name: "Read", Arguments: rawArgs}}}, "gpt-5.1")
	content, err := json.Marshal(wire.Content)
	require.NoError(t, err)
	replay, err := apicompat.AnthropicToResponses(&apicompat.AnthropicRequest{Model: "gpt-5.1", Messages: []apicompat.AnthropicMessage{{Role: "assistant", Content: content}}})
	require.NoError(t, err)
	var history []any
	require.NoError(t, json.Unmarshal(replay.Input, &history))
	a := &cloudSessionActor{record: &OpenAICloudSessionRecord{BillingSnapshot: json.RawMessage(`{"inbound_endpoint":"/v1/messages"}`)}, history: []any{map[string]any{"type": "function_call", "call_id": "call_cloud_read", "name": "Read", "arguments": rawArgs}}}
	p := &cloudProtocolRequest{history: history, outputs: map[string]string{}}
	require.NoError(t, a.checkHistoryMatch(p, true), "the existing public bridge drops empty Read.pages; its own output must remain replayable")
	require.Equal(t, rawArgs, a.history[0].(map[string]any)["arguments"], "comparison must not rewrite retained RPC history")
	history[0].(map[string]any)["arguments"] = `{"file_path":"/tmp/changed.txt"}`
	require.Error(t, a.checkHistoryMatch(p, true), "changed file paths still reject")
	history[0].(map[string]any)["arguments"] = `{"file_path":"/tmp/example.txt","pages":"2"}`
	require.Error(t, a.checkHistoryMatch(p, true), "nonempty page selections still matter")
}

func TestCloudEmptyReasoningDoesNotShiftHistoryComparison(t *testing.T) {
	call := map[string]any{"type": "function_call", "call_id": "call_cloud_1", "name": "echo", "arguments": "{}"}
	for _, cipher := range []string{"", "opaque"} {
		a := &cloudSessionActor{history: []any{map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": cipher}, call}}
		err := a.checkHistoryMatch(&cloudProtocolRequest{history: []any{call}}, true)
		if cipher == "" {
			require.NoError(t, err, "an empty reasoning placeholder carries no replay semantics")
		} else {
			require.Error(t, err, "encrypted reasoning cannot be dropped")
		}
	}
	emptySummary := &cloudSessionActor{history: []any{map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": ""}}}, call}}
	require.NoError(t, emptySummary.checkHistoryMatch(&cloudProtocolRequest{history: []any{call}}, true))
	a := &cloudSessionActor{history: []any{map[string]any{"type": "reasoning", "summary": []any{map[string]any{"type": "summary_text", "text": "visible summary"}}}, call}}
	require.Error(t, a.checkHistoryMatch(&cloudProtocolRequest{history: []any{call}}, true))
}

func TestCloudReadProjectionDoesNotLoosenPlainResponsesOrTrustUserAgent(t *testing.T) {
	a := &cloudSessionActor{record: &OpenAICloudSessionRecord{BillingSnapshot: json.RawMessage(`{"inbound_endpoint":"/v1/responses","user_agent":"claude-cli/2.1.308"}`)}, history: []any{map[string]any{"type": "function_call", "call_id": "c", "name": "Read", "arguments": `{"file_path":"/tmp/a","pages":""}`}}}
	require.Error(t, a.checkHistoryMatch(&cloudProtocolRequest{history: []any{map[string]any{"type": "function_call", "call_id": "c", "name": "Read", "arguments": `{"file_path":"/tmp/a"}`}}}, true))
}
