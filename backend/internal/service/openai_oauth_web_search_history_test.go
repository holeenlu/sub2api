package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Codex local compaction replays hosted web_search_call history with tools:[];
// ChatGPT then fails with "response protection is unavailable" (#7927).
const openAIWebSearchHistoryCompactionBody = `{"model":"gpt-5.5","instructions":"x","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"look it up"}]},{"type":"web_search_call","id":"ws_123","status":"completed","action":{"type":"search","query":"kwin inputmethod"}},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"found"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"summarize"}]}],"tools":[],"tool_choice":"auto","parallel_tool_calls":false,"stream":true,"store":false}`

func TestOAuthWebSearchHistoryToolAcrossPaths(t *testing.T) {
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	transform := func(compact bool) func([]byte) ([]byte, bool, error) {
		return func(b []byte) ([]byte, bool, error) {
			var req map[string]any
			if err := json.Unmarshal(b, &req); err != nil {
				return nil, false, err
			}
			result := applyCodexOAuthTransform(req, true, compact)
			out, err := json.Marshal(req)
			return out, result.Modified, err
		}
	}
	tests := []struct {
		name      string
		normalize func([]byte) ([]byte, bool, error)
		inject    bool
	}{
		{"transformed OAuth", transform(false), true},
		{"transformed OAuth compact", transform(true), false},
		{"OAuth websocket", func(b []byte) ([]byte, bool, error) {
			return normalizeOpenAIResponsesWebSocketCompatibilityBody(b, oauth, false)
		}, true},
		{"OAuth passthrough compact", func(b []byte) ([]byte, bool, error) {
			return normalizeOpenAIResponsesCompatibilityBodyWithOptions(b, oauth, openAIResponsesCompatibilityOptions{Compact: true})
		}, false},
		{"API key websocket", func(b []byte) ([]byte, bool, error) {
			return normalizeOpenAIResponsesWebSocketCompatibilityBody(b, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, false)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.inject {
				const standaloneTools = `[{"type":"namespace","name":"web","tools":[{"type":"function","name":"run","parameters":{"type":"object"}}]}]`
				body, err := sjson.SetRawBytes([]byte(openAIWebSearchHistoryCompactionBody), "tools", []byte(standaloneTools))
				require.NoError(t, err)
				out, _, err := tt.normalize(body)
				require.NoError(t, err)
				require.JSONEq(t, standaloneTools, gjson.GetBytes(out, "tools").Raw)
				require.Equal(t, "web_search_call", gjson.GetBytes(out, "input.1.type").String())
				require.Equal(t, "auto", gjson.GetBytes(out, "tool_choice").String())
			}
			out, _, err := tt.normalize([]byte(openAIWebSearchHistoryCompactionBody))
			require.NoError(t, err)
			require.Equal(t, "web_search_call", gjson.GetBytes(out, "input.1.type").String())
			if !tt.inject {
				require.False(t, gjsonToolsContainWebSearch(gjson.GetBytes(out, "tools"), ""))
				return
			}
			tools := gjson.GetBytes(out, "tools").Array()
			require.Len(t, tools, 1)
			require.Equal(t, "web_search", tools[0].Get("type").String())
			require.False(t, tools[0].Get("external_web_access").Bool())
			require.True(t, tools[0].Get("external_web_access").Exists())
			require.Equal(t, "none", gjson.GetBytes(out, "tool_choice").String())
		})
	}
}

func TestEnsureOpenAIOAuthWebSearchToolForHistory(t *testing.T) {
	const (
		user    = `{"type":"message","role":"user","content":[{"type":"input_text","text":"q"}]}`
		search  = `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"q"}}`
		trigger = `{"type":"compaction_trigger"}`
		history = `[` + user + `,` + search + `]`
	)
	tests := []struct {
		name       string
		body       string
		lite       bool
		changed    bool
		toolsPath  string
		toolCount  int
		toolChoice string
	}{
		{"tools absent", `{"input":` + history + `}`, false, true, "tools", 1, "none"},
		{"tools null", `{"input":` + history + `,"tools":null,"tool_choice":null}`, false, true, "tools", 1, "none"},
		{"explicit none kept", `{"input":` + history + `,"tools":[],"tool_choice":"none"}`, false, true, "tools", 1, "none"},
		{"required not rewritten", `{"input":` + history + `,"tools":[],"tool_choice":"required"}`, false, true, "tools", 1, "required"},
		{"caller tools keep choice", `{"input":` + history + `,"tools":[{"type":"function","name":"echo"}],"tool_choice":"auto"}`, false, true, "tools", 2, "auto"},
		{"web_search already declared", `{"input":` + history + `,"tools":[{"type":"web_search"}]}`, false, false, "", 0, ""},
		{"web_search_preview declared", `{"input":` + history + `,"tools":[{"type":"web_search_preview"}]}`, false, false, "", 0, ""},
		{"standalone dotted function", `{"input":` + history + `,"tools":[{"type":"function","name":"web.run"}],"tool_choice":"auto"}`, false, false, "", 0, ""},
		{"standalone custom tool", `{"input":` + history + `,"tools":[{"type":"namespace","name":"web","tools":[{"type":"custom","name":"run"}]}]}`, false, false, "", 0, ""},
		{"standalone namespace function", `{"input":` + history + `,"tools":[{"type":"namespace","name":"web","tools":[{"type":"function","name":"run"}]}]}`, false, false, "", 0, ""},
		{"standalone namespace in Lite", `{"input":` + history + `,"tools":[{"type":"namespace","name":"web","tools":[{"type":"function","name":"run"}]}]}`, true, false, "", 0, ""},
		{"standalone additional tools", `{"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"web","tools":[{"type":"function","name":"run"}]}]},` + search + `,` + trigger + `]}`, true, false, "", 0, ""},
		{"different namespace still injects", `{"input":` + history + `,"tools":[{"type":"namespace","name":"shell","tools":[{"type":"function","name":"run"}]}]}`, false, true, "tools", 2, ""},
		{"declared via additional_tools", `{"input":[{"type":"additional_tools","tools":[{"type":"web_search"}]},` + search + `]}`, false, false, "", 0, ""},
		{"no web_search_call item", `{"input":[{"type":"message","role":"user","content":"web_search_call"}],"tools":[]}`, false, false, "", 0, ""},
		{"string input", `{"input":"web_search_call"}`, false, false, "", 0, ""},
		{"lite preserves compaction trigger", `{"input":[` + user + `,` + search + `,` + trigger + `]}`, true, false, "", 0, ""},
		{"lite preserves tool choice", `{"input":` + history + `,"tool_choice":"auto"}`, true, false, "", 0, ""},
		{"lite preserves additional client tools", `{"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"echo"}]},` + search + `,` + trigger + `],"tool_choice":"auto"}`, true, false, "", 0, ""},
		{"lite preserves empty additional tools", `{"input":[{"type":"additional_tools","role":"developer"},` + search + `]}`, true, false, "", 0, ""},
		{"lite preserves top level client tools", `{"input":` + history + `,"tools":[{"type":"function","name":"echo"}]}`, true, false, "", 0, ""},
		{"lite preserves encrypted reasoning", `{"input":[` + search + `,{"type":"reasoning","id":"rs_history","encrypted_content":"cipher","summary":[]}],"previous_response_id":"resp_previous"}`, true, false, "", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed, err := ensureOpenAIOAuthWebSearchToolForHistoryBody([]byte(tt.body), tt.lite)
			require.NoError(t, err)
			require.Equal(t, tt.changed, changed)
			var req map[string]any
			require.NoError(t, json.Unmarshal([]byte(tt.body), &req))
			require.Equal(t, tt.changed, ensureOpenAIOAuthWebSearchToolForHistory(req, tt.lite))
			if !tt.changed {
				require.JSONEq(t, tt.body, string(out))
				mapOut, err := json.Marshal(req)
				require.NoError(t, err)
				require.JSONEq(t, tt.body, string(mapOut))
				return
			}
			require.True(t, gjson.ValidBytes(out))
			tools := gjson.GetBytes(out, tt.toolsPath).Array()
			require.Len(t, tools, tt.toolCount)
			require.Equal(t, "web_search", tools[len(tools)-1].Get("type").String())
			require.Equal(t, tt.toolChoice, gjson.GetBytes(out, "tool_choice").String())

			// The map variant used by the Codex transform must agree.
			mapOut, err := json.Marshal(req)
			require.NoError(t, err)
			require.JSONEq(t, string(out), string(mapOut))
		})
	}
}

func TestEnsureOpenAIOAuthWebSearchToolForHistoryDoesNotShareToolMap(t *testing.T) {
	first := map[string]any{"input": []any{map[string]any{"type": "web_search_call"}}}
	second := map[string]any{"input": []any{map[string]any{"type": "web_search_call"}}}
	require.True(t, ensureOpenAIOAuthWebSearchToolForHistory(first, false))
	require.True(t, ensureOpenAIOAuthWebSearchToolForHistory(second, false))
	firstTools, ok := first["tools"].([]any)
	require.True(t, ok)
	firstTool, ok := firstTools[0].(map[string]any)
	require.True(t, ok)
	firstTool["external_web_access"] = true
	secondTools, ok := second["tools"].([]any)
	require.True(t, ok)
	secondTool, ok := secondTools[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, secondTool["external_web_access"])
	require.Equal(t, false, openAIWebSearchHistoryTool["external_web_access"])
}
