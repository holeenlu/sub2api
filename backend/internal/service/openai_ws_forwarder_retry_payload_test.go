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
		{"deep namespace", `{"tools":` + strings.Repeat(`[{"type":"namespace","tools":`, 17) + `[]` + strings.Repeat(`}]`, 17) + `}`, "namespace_depth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.blocker, openAIWSUncertainExecutionReplayBlocker([]byte(tc.payload)))
			require.Equal(t, tc.blocker == "", openAIWSUncertainExecutionReplaySafe([]byte(tc.payload)))
		})
	}
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
