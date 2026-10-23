package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWSUncertainReplayChecksNamespaceChildren(t *testing.T) {
	for _, tc := range []struct {
		name, payload, blocker string
	}{
		{"no tools", `{}`, ""},
		{"disabled tools", `{"tool_choice":"none","tools":[{"type":"mcp"}]}`, ""},
		{"namespaced functions", `{"tools":[{"type":"namespace","name":"local","tools":[{"type":"function","name":"shell"},{"type":"custom","name":"edit"}]}]}`, ""},
		{"nested namespace", `{"tools":[{"type":"namespace","tools":[{"type":"namespace","tools":[{"type":"function"}]}]}]}`, ""},
		{"nested interpreter", `{"tools":[{"type":"namespace","tools":[{"type":"function"},{"type":"code_interpreter"}]}]}`, "code_interpreter"},
		{"unknown child is not logged verbatim", `{"tools":[{"type":"namespace","tools":[{"type":"private-key-in-type"}]}]}`, "unknown_tool"},
		{"malformed namespace", `{"tools":[{"type":"namespace","tools":{"type":"mcp"}}]}`, "malformed_tools"},
		{"missing namespace definition", `{"tools":[{"type":"namespace","name":"remote"}]}`, "malformed_tools"},
		{"malformed top-level list", `{"tools":{"type":"mcp"}}`, "malformed_tools"},
		{"client discovery", `{"tools":[{"type":"tool_search","execution":"client"}]}`, ""},
		{"hosted discovery of client functions", `{"tools":[{"type":"tool_search"},{"type":"namespace","tools":[{"type":"function"}]}]}`, ""},
		{"unknown discovery execution", `{"tools":[{"type":"tool_search","execution":"elsewhere"}]}`, "tool_search_execution"},
		{"dynamic MCP", `{"input":[{"type":"additional_tools","tools":[{"type":"mcp"}]}]}`, "mcp"},
		{"discovered execution tool", `{"input":[{"type":"tool_search_output","tools":[{"type":"namespace","tools":[{"type":"code_interpreter"}]}]}]}`, "code_interpreter"},
		{"discovered client functions", `{"input":[{"type":"tool_search_output","tools":[{"type":"function"}]}]}`, ""},
		{"unknown discovered inventory", `{"input":[{"type":"tool_search_output"}]}`, "malformed_tools"},
		{"opaque predecessor", `{"previous_response_id":"resp_remote"}`, "unresolved_tool_context"},
		{"opaque compaction", `{"input":[{"type":"compaction","encrypted_content":"opaque"}]}`, "unresolved_tool_context"},
		{"duplicate tools cannot hide MCP", `{"tools":[{"type":"function"}],"tools":[{"type":"mcp"}]}`, "mcp"},
		{"duplicate choice cannot disable guard", `{"tool_choice":"none","tool_choice":"auto","tools":[{"type":"mcp"}]}`, "ambiguous_tool_choice"},
		{"ambiguous tool type", `{"tools":[{"type":"function","type":"mcp"}]}`, "malformed_tools"},
		{"deep namespace", `{"tools":` + strings.Repeat(`[{"type":"namespace","tools":`, 17) + `[]` + strings.Repeat(`}]`, 17) + `}`, "namespace_depth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.blocker, openAIWSUncertainExecutionReplayBlocker([]byte(tc.payload)))
			require.Equal(t, tc.blocker == "", openAIWSUncertainExecutionReplaySafe([]byte(tc.payload)))
		})
	}
}

func TestOpenAIWSReplayUsesResolvedHistoryForTools(t *testing.T) {
	payload := []byte(`{"previous_response_id":"resp_known","tools":[{"type":"tool_search","execution":"client"}]}`)
	local := []json.RawMessage{json.RawMessage(`{"type":"additional_tools","tools":[{"type":"function","name":"client_tool"}]}`)}
	require.Empty(t, openAIWSUncertainExecutionReplayBlocker(payload, local))
	remote := []json.RawMessage{json.RawMessage(`{"type":"tool_search_output","tools":[{"type":"mcp"}]}`)}
	require.Equal(t, "mcp", openAIWSUncertainExecutionReplayBlocker(payload, remote))
}

func TestOpenAIResponsesAccountReplayPreservesOwnership(t *testing.T) {
	for _, tc := range []struct{ payload, blocker string }{
		{`{"input":[{"type":"reasoning","id":"rs_1","encrypted_content":"private"}]}`, "encrypted_content"},
		{`{"previous_response_id":"resp_owner","input":"delta"}`, "previous_response_id"},
		{`{"previous_response_id":null,"previous_response_id":"resp_owner"}`, "previous_response_id"},
		{`{"input":[{"type":"message","type":"item_reference","id":"rs_owner"}]}`, "item_reference"},
		{`{"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_owner"}]}]}`, "file_id"},
		{`{"tools":[{"type":"file_search","vector_store_ids":["vs_owner"]}]}`, "vector_store_ids"},
		{`{"prompt":{"id":"pmpt_owner"}}`, "prompt"},
		{`{"conversation":"conv_owner"}`, "conversation"},
		{`{"input":[{"container_id":"private"}]}`, "container_id"},
		{`{"input":[{"vector_store_id":"private"}]}`, "vector_store_id"},
		{`{"input":`, "malformed_request"},
		{`[]`, "malformed_request"},
		{`{"input":` + strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65) + "}", "context_depth"},
	} {
		require.False(t, OpenAIResponsesAccountReplayAllowed([]byte(tc.payload)), tc.payload)
		require.Equal(t, tc.blocker, openAIResponsesAccountReplayBlocker([]byte(tc.payload)))
	}
	input := []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"rs_1","encrypted_content":"private","summary":[]}`)}
	retry, safe, err := buildOpenAIWSCurrentTurnRetryPayload([]byte(`{"store":false,"previous_response_id":"resp_owner"}`), input, true, "gpt-6.1-sol", true)
	require.NoError(t, err)
	require.True(t, safe, "same-account complete replay must remain available")
	require.Contains(t, string(retry), `"encrypted_content":"private"`)
	require.NotContains(t, string(retry), "previous_response_id")
	require.True(t, OpenAIResponsesAccountReplayAllowed([]byte(`{"input":[{"role":"user","content":"full text history"}],"tools":[{"type":"web_search"}]}`)))
	require.Empty(t, openAIResponsesAccountReplayBlocker([]byte(`{"previous_response_id":null,"conversation":" ","prompt":"","input":[{"type":"reasoning","encrypted_content":""}]}`)))
}

func TestBuildOpenAIWSCurrentTurnRetryPayloadRequiresCompleteDependencies(t *testing.T) {
	for _, input := range []string{
		`{"type":"item_reference","id":"rs_missing"}`,
		`{"type":"reasoning","id":"rs_missing","summary":[]}`,
		`{"type":"function_call_output","call_id":"missing_call","output":"done"}`,
		`{"role":"user","content":[{"type":"input_file","file_id":"file_account_scoped"}]}`,
	} {
		payload, safe, err := buildOpenAIWSCurrentTurnRetryPayload([]byte(`{"previous_response_id":"resp_old"}`), []json.RawMessage{json.RawMessage(input)}, true, "gpt-6.1-sol")
		require.NoError(t, err)
		require.False(t, safe, input)
		require.Empty(t, payload)
	}
}

func TestBuildOpenAIWSCurrentTurnRetryPayloadPreservesHostedItems(t *testing.T) {
	for _, store := range []string{"true", "false"} {
		for _, typ := range []string{"web_search_call", "mcp_call", "local_shell_call", "image_generation_call", "future_hosted_call"} {
			t.Run(store+"/"+typ, func(t *testing.T) {
				item := json.RawMessage(`{"type":"` + typ + `","id":"hosted_1","status":"completed","result":"retained"}`)
				payload, safe, err := buildOpenAIWSCurrentTurnRetryPayload([]byte(`{"store":`+store+`,"previous_response_id":"resp_old"}`), []json.RawMessage{item}, true, "gpt-6.1-sol")
				require.NoError(t, err)
				require.True(t, safe)
				var decoded struct {
					Input []json.RawMessage `json:"input"`
				}
				require.NoError(t, json.Unmarshal(payload, &decoded))
				require.Len(t, decoded.Input, 1)
				require.JSONEq(t, string(item), string(decoded.Input[0]))
			})
		}
	}
}

func TestApplyOpenAIWSRetryPayloadStrategy_KeepPromptCacheKey(t *testing.T) {
	payload := map[string]any{
		"model":            "gpt-5.3-codex",
		"prompt_cache_key": "pcache_123",
		"include":          []any{"reasoning.encrypted_content"},
		"text": map[string]any{
			"verbosity": "low",
		},
		"tools": []any{map[string]any{"type": "function"}},
	}

	strategy, removed := applyOpenAIWSRetryPayloadStrategy(payload, 3)
	require.Equal(t, "trim_optional_fields", strategy)
	require.Contains(t, removed, "include")
	require.NotContains(t, removed, "prompt_cache_key")
	require.Equal(t, "pcache_123", payload["prompt_cache_key"])
	require.NotContains(t, payload, "include")
	require.Contains(t, payload, "text")
}

func TestApplyOpenAIWSRetryPayloadStrategy_AttemptSixKeepsSemanticFields(t *testing.T) {
	payload := map[string]any{
		"prompt_cache_key":    "pcache_456",
		"instructions":        "long instructions",
		"tools":               []any{map[string]any{"type": "function"}},
		"parallel_tool_calls": true,
		"tool_choice":         "auto",
		"include":             []any{"reasoning.encrypted_content"},
		"text":                map[string]any{"verbosity": "high"},
	}

	strategy, removed := applyOpenAIWSRetryPayloadStrategy(payload, 6)
	require.Equal(t, "trim_optional_fields", strategy)
	require.Contains(t, removed, "include")
	require.NotContains(t, removed, "prompt_cache_key")
	require.Equal(t, "pcache_456", payload["prompt_cache_key"])
	require.Contains(t, payload, "instructions")
	require.Contains(t, payload, "tools")
	require.Contains(t, payload, "tool_choice")
	require.Contains(t, payload, "parallel_tool_calls")
	require.Contains(t, payload, "text")
}
