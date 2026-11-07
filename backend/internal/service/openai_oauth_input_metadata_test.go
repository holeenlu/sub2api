package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOAuthInputInternalMetadata(t *testing.T) {
	const field = "internal_chat_message_metadata_passthrough"
	// The identically named field in user content must not be recursively stripped.
	const body = `{"model":"gpt-5.5","input":[{"role":"user","content":[{"type":"input_text","text":"hello","internal_chat_message_metadata_passthrough":{"keep":true}}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["text"]}},{"type":"function_call","call_id":"fc_123","name":"echo","arguments":"{\"internal_chat_message_metadata_passthrough\":true}","internal_chat_message_metadata_passthrough":null},{"type":"function_call_output","call_id":"fc_123","output":"result"}],"internal_chat_message_metadata_passthrough":{"keep":true}}`
	tests := []struct {
		name      string
		normalize func([]byte) ([]byte, bool, error)
		strip     bool
	}{
		{"transformed OAuth", func(b []byte) ([]byte, bool, error) {
			var req map[string]any
			if err := json.Unmarshal(b, &req); err != nil {
				return nil, false, err
			}
			result := applyCodexOAuthTransform(req, false, false)
			out, err := json.Marshal(req)
			return out, result.Modified, err
		}, true},
		{"OAuth passthrough", func(b []byte) ([]byte, bool, error) { return normalizeOpenAIPassthroughOAuthBody(b, false) }, true},
		{"OAuth compact", func(b []byte) ([]byte, bool, error) { return normalizeOpenAIPassthroughOAuthBody(b, true) }, true},
		{"OAuth websocket", func(b []byte) ([]byte, bool, error) {
			return normalizeOpenAIResponsesWebSocketCompatibilityBody(b, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, false)
		}, true},
		{"API key websocket", func(b []byte) ([]byte, bool, error) {
			return normalizeOpenAIResponsesWebSocketCompatibilityBody(b, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, false)
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _, err := tt.normalize([]byte(body))
			require.NoError(t, err)
			require.Equal(t, !tt.strip, gjson.GetBytes(out, "input.0."+field).Exists())
			require.Equal(t, !tt.strip, gjson.GetBytes(out, "input.1."+field).Exists())
			require.Equal(t, "hello", gjson.GetBytes(out, "input.0.content.0.text").String())
			require.True(t, gjson.GetBytes(out, "input.0.content.0."+field+".keep").Bool())
			require.True(t, gjson.GetBytes(out, field+".keep").Bool())
			require.Equal(t, `{"internal_chat_message_metadata_passthrough":true}`, gjson.GetBytes(out, "input.1.arguments").String())
			require.Equal(t, "fc_123", gjson.GetBytes(out, "input.1.call_id").String())
			require.Equal(t, "result", gjson.GetBytes(out, "input.2.output").String())
		})
	}
}

func TestOAuthInputInternalMetadataCompatibility(t *testing.T) {
	for _, body := range []string{
		`{"input":"hello"}`,
		`{"input":{"internal_chat_message_metadata_passthrough":true}}`,
		`{"input":[null,"hello",{"role":"user","content":"hello"}]}`,
		`{"input":[{"content":{"internal_chat_message_metadata_passthrough":true}}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			out, changed, err := normalizeOpenAIOAuthResponsesCompatibilityBody([]byte(body))
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, body, string(out))
			var req map[string]any
			require.NoError(t, json.Unmarshal([]byte(body), &req))
			require.False(t, normalizeOpenAIOAuthResponsesCompatibilityFields(req))
		})
	}
	// Legacy prompt aliases and later-turn payloads use the same boundary.
	body := []byte(`{"prompt":[{"role":"user","content":"hello","internal_chat_message_metadata_passthrough":{}}],"previous_response_id":"resp_123"}`)
	out, changed, err := normalizeOpenAIOAuthResponsesCompatibilityBody(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.False(t, gjson.GetBytes(out, "input.0.internal_chat_message_metadata_passthrough").Exists())
	require.Equal(t, "resp_123", gjson.GetBytes(out, "previous_response_id").String())
	var req map[string]any
	require.NoError(t, json.Unmarshal(body, &req))
	require.True(t, normalizeOpenAIOAuthResponsesCompatibilityFields(req))
	input, ok := req["input"].([]any)
	require.True(t, ok)
	require.NotContains(t, input[0], "internal_chat_message_metadata_passthrough")
}

func TestOAuthUnsupportedOptionsPreserveEffectiveIntent(t *testing.T) {
	for _, tc := range []struct{ name, body, output string }{
		{"rejected defaults", `{"top_p":1,"frequency_penalty":0,"presence_penalty":0,"metadata":{},"user":"x","safety_identifier":"s","chat_template_kwargs":{},"input":"hi"}`, `{"input":"hi"}`},
		{"native limit wins", `{"max_output_tokens":12,"max_completion_tokens":20,"max_tokens":30}`, `{"max_output_tokens":12}`},
		{"chat limit fills null", `{"max_output_tokens":null,"max_completion_tokens":20,"max_tokens":30}`, `{"max_output_tokens":20}`},
		{"empty no-ops", `{"truncation":"disabled","stop_sequences":[],"prompt_cache_options":{},"stream_options":{"include_usage":true,"reasoning_summary":"auto"}}`, `{"stream_options":{"reasoning_summary":"auto"}}`},
		{"meaningful options and nested content", `{"truncation":"auto","stop_sequences":["STOP"],"prompt_cache_options":{"retention":"24h"},"input":[{"role":"user","content":"metadata","metadata":{"keep":true}}],"future":9007199254740993}`, `{"truncation":"auto","stop_sequences":["STOP"],"prompt_cache_options":{"retention":"24h"},"input":[{"role":"user","content":"metadata","metadata":{"keep":true}}],"future":9007199254740993}`},
		{"escaped option", `{"top_\u0070":1,"max_tokens":5}`, `{"max_output_tokens":5}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := normalizeOpenAIOAuthResponsesCompatibilityBody([]byte(tc.body))
			require.NoError(t, err)
			require.JSONEq(t, tc.output, string(out))
			if gjson.GetBytes(out, "future").Exists() {
				require.Equal(t, "9007199254740993", gjson.GetBytes(out, "future").Raw)
			}
			var fields map[string]any
			require.NoError(t, json.Unmarshal(out, &fields))
			require.False(t, normalizeOpenAIOAuthResponsesCompatibilityFields(fields))
			second, changed, err := normalizeOpenAIOAuthResponsesCompatibilityBody(out)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, string(out), string(second))
		})
	}
}

func TestOAuthEffectivePipelineKeepsSupportedIntent(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-terra","input":[{"role":"user","content":"hello"}],"max_output_tokens":77,"max_tokens":120,"truncation":"auto","stop_sequences":["STOP"],"stream_options":{"include_usage":true,"reasoning_summary":"auto"}}`)
	for _, mode := range []string{"raw", "map"} {
		t.Run(mode, func(t *testing.T) {
			var out []byte
			if mode == "raw" {
				var err error
				out, _, err = normalizeOpenAIPassthroughOAuthBody(body, false)
				require.NoError(t, err)
			} else {
				var fields map[string]any
				require.NoError(t, json.Unmarshal(body, &fields))
				applyCodexOAuthTransform(fields, false, false)
				var err error
				out, err = json.Marshal(fields)
				require.NoError(t, err)
			}
			require.Equal(t, int64(77), gjson.GetBytes(out, "max_output_tokens").Int())
			require.False(t, gjson.GetBytes(out, "max_tokens").Exists())
			require.Equal(t, "auto", gjson.GetBytes(out, "truncation").String())
			require.Equal(t, "STOP", gjson.GetBytes(out, "stop_sequences.0").String())
			require.False(t, gjson.GetBytes(out, "stream_options.include_usage").Exists())
			require.Equal(t, "auto", gjson.GetBytes(out, "stream_options.reasoning_summary").String())
		})
	}
}
