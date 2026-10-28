package apicompat

import (
	"bufio"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatResponsesCacheCreationInputAliasJSONAndSSE(t *testing.T) {
	for _, tt := range []struct {
		name, details string
		want          int64
	}{
		{"alias_only", `{"cache_creation_input_tokens":7}`, 7},
		{"alias_largest", `{"cached_tokens":5,"cache_write_tokens":3,"cache_creation_tokens":6,"cache_creation_input_tokens":7}`, 7},
		{"canonical_largest", `{"cache_write_tokens":3,"cache_creation_tokens":11,"cache_creation_input_tokens":7}`, 11},
		{"duplicates", `{"cache_write_tokens":7,"cache_creation_tokens":7,"cache_creation_input_tokens":7}`, 7},
		{"no_alias_keeps_write_priority", `{"cache_write_tokens":3,"cache_creation_tokens":11}`, 3},
		{"zero_alias_keeps_write_priority", `{"cache_write_tokens":3,"cache_creation_tokens":11,"cache_creation_input_tokens":0}`, 3},
		{"typed_zero_write_keeps_creation_fallback", `{"cache_write_tokens":0,"cache_creation_tokens":6}`, 6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usage := `{"prompt_tokens":20,"completion_tokens":2,"total_tokens":22,"prompt_tokens_details":` + tt.details + `}`
			checkResponse := func(t *testing.T, response gjson.Result) {
				t.Helper()
				require.Equal(t, "completed", response.Get("status").String())
				require.Equal(t, "hi", response.Get("output.0.content.0.text").String())
				require.EqualValues(t, 20, response.Get("usage.input_tokens").Int(), "cache writes are part of the prompt aggregate")
				require.EqualValues(t, 2, response.Get("usage.output_tokens").Int())
				require.EqualValues(t, 22, response.Get("usage.total_tokens").Int())
				require.Equal(t, tt.want, response.Get("usage.cache_creation_input_tokens").Int())
				for _, field := range []string{"cached_tokens", "cache_write_tokens", "cache_creation_tokens", "cache_creation_input_tokens"} {
					if reported := gjson.Get(tt.details, field); reported.Exists() {
						require.Equal(t, reported.Int(), response.Get("usage.input_tokens_details."+field).Int(), field)
					}
				}
				var decoded ResponsesResponse
				require.NoError(t, json.Unmarshal([]byte(response.Raw), &decoded))
				require.NotNil(t, decoded.Usage)
				require.EqualValues(t, tt.want, decoded.Usage.CacheCreationInputTokens, "wire decoding must not lose the selected total")
			}
			t.Run("JSON", func(t *testing.T) {
				payload := `{"id":"chatcmpl-alias","model":"example","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":` + usage + `}`
				var chat ChatCompletionsResponse
				require.NoError(t, json.Unmarshal([]byte(payload), &chat))
				for round := 0; round < 3; round++ {
					response := ChatCompletionsResponseToResponses(&chat, "example", nil, nil, false, nil)
					wire, err := json.Marshal(response)
					require.NoError(t, err)
					checkResponse(t, gjson.ParseBytes(wire))
					var decoded ResponsesResponse
					require.NoError(t, json.Unmarshal(wire, &decoded))
					back := ResponsesToChatCompletions(&decoded, "example")
					wire, err = json.Marshal(back)
					require.NoError(t, err)
					require.Equal(t, gjson.Get(tt.details, "cache_creation_input_tokens").Int(), gjson.GetBytes(wire, "usage.prompt_tokens_details.cache_creation_input_tokens").Int())
					require.NoError(t, json.Unmarshal(wire, &chat))
					require.NotNil(t, chat.Usage)
					require.NotNil(t, chat.Usage.PromptTokensDetails)
					require.EqualValues(t, tt.want, chat.Usage.PromptTokensDetails.CacheCreationTokensTotal(), "round %d must preserve the maximum without accumulation", round)
				}
			})
			t.Run("SSE", func(t *testing.T) {
				// include_usage arrives after finish_reason with empty choices.
				// Repeated cumulative snapshots must not add the aliases again.
				stream := "data: " + `{"id":"chatcmpl-alias","model":"example","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"}}]}` + "\n\n" +
					"data: " + `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}` + "\n\n" +
					"data: " + `{"choices":[],"usage":` + usage + "}\n\n" +
					"data: " + `{"choices":[],"usage":` + usage + "}\n\n" +
					"data: [DONE]\n\n"
				state := NewChatCompletionsToResponsesStreamState("example")
				var events []ResponsesStreamEvent
				scanner := bufio.NewScanner(strings.NewReader(stream))
				for scanner.Scan() {
					data, ok := strings.CutPrefix(scanner.Text(), "data: ")
					if !ok {
						continue
					}
					if data == "[DONE]" {
						events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
						break
					}
					var chunk ChatCompletionsChunk
					require.NoError(t, json.Unmarshal([]byte(data), &chunk))
					events = append(events, ChatCompletionsChunkToResponsesEvents(&chunk, state)...)
				}
				require.NoError(t, scanner.Err())
				completed := 0
				for _, event := range events {
					wire, err := ResponsesEventToSSE(event)
					require.NoError(t, err)
					if event.Type != "response.completed" {
						continue
					}
					completed++
					header, data, ok := strings.Cut(wire, "\ndata: ")
					require.True(t, ok)
					require.Equal(t, "event: response.completed", header)
					checkResponse(t, gjson.Get(strings.TrimSpace(data), "response"))

					// Exercise the reverse streaming consumer, including its wire
					// encoding, then feed its usage chunk back through Responses.
					var terminal ResponsesStreamEvent
					require.NoError(t, json.Unmarshal([]byte(data), &terminal))
					for round := 0; round < 3; round++ {
						reverse := NewResponsesEventToChatState()
						reverse.IncludeUsage = true
						back := NewChatCompletionsToResponsesStreamState("example")
						usageChunks := 0
						for _, chunk := range ResponsesEventToChatChunks(&terminal, reverse) {
							chatWire, err := ChatChunkToSSE(chunk)
							require.NoError(t, err)
							chatData, ok := strings.CutPrefix(chatWire, "data: ")
							require.True(t, ok)
							var decoded ChatCompletionsChunk
							require.NoError(t, json.Unmarshal([]byte(chatData), &decoded))
							if decoded.Usage != nil {
								usageChunks++
								require.NotNil(t, decoded.Usage.PromptTokensDetails)
								require.EqualValues(t, gjson.Get(tt.details, "cache_creation_input_tokens").Int(), decoded.Usage.PromptTokensDetails.CacheCreationInputTokens)
								require.EqualValues(t, tt.want, decoded.Usage.PromptTokensDetails.CacheCreationTokensTotal())
							}
							ChatCompletionsChunkToResponsesEvents(&decoded, back)
						}
						require.Equal(t, 1, usageChunks)
						var next *ResponsesResponse
						for _, event := range FinalizeChatCompletionsResponsesStream(back) {
							if event.Type == "response.completed" {
								next = event.Response
							}
						}
						require.NotNil(t, next)
						require.NotNil(t, next.Usage)
						require.EqualValues(t, tt.want, next.Usage.CacheCreationInputTokens, "round %d must not accumulate aliases", round)
						require.Equal(t, 20, next.Usage.InputTokens)
						require.Equal(t, 22, next.Usage.TotalTokens)
						terminal.Response = next
					}
				}
				require.Equal(t, 1, completed)
				require.Empty(t, FinalizeChatCompletionsResponsesStream(state))
			})
		})
	}
}

func TestResponsesInputToChatMessages_DeveloperRoleMapsToSystem(t *testing.T) {
	messages, err := responsesInputToChatMessages("", json.RawMessage(`[{"role":"developer","content":"follow project instructions"}]`))
	require.NoError(t, err)
	require.Len(t, messages, 1)

	assert.Equal(t, "system", messages[0].Role)
	assert.JSONEq(t, `"follow project instructions"`, string(messages[0].Content))
}

func TestResponsesInputToChatMessages_SkipsInvalidHistoricalFunctionCall(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"function_call","call_id":"call_bad","name":"exec_command","arguments":"{\"cmd\": \"ssh root@HOST"},
		{"type":"function_call_output","call_id":"call_bad","output":"failed to parse function arguments"},
		{"type":"function_call","call_id":"call_ok","name":"exec_command","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_ok","output":"ok"},
		{"role":"user","content":"continue"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 3)
	require.Equal(t, "assistant", messages[0].Role)
	require.Len(t, messages[0].ToolCalls, 1)
	require.Equal(t, "call_ok", messages[0].ToolCalls[0].ID)
	require.Equal(t, "tool", messages[1].Role)
	require.Equal(t, "call_ok", messages[1].ToolCallID)
	require.Equal(t, "user", messages[2].Role)
}

func TestResponsesInputToChatMessages_SkipsInvalidEmptyCallIDOutput(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"function_call","call_id":"","name":"exec_command","arguments":"{\"cmd\": \"ssh root@HOST"},
		{"type":"function_call_output","call_id":"","output":"failed to parse function arguments"},
		{"role":"user","content":"continue"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 1)
	require.Equal(t, "user", messages[0].Role)
}

func TestChatCompletionsResponseToResponses_SkipsInvalidFunctionArguments(t *testing.T) {
	resp := &ChatCompletionsResponse{
		Model: "deepseek-v4-flash",
		Choices: []ChatChoice{{
			Message: ChatMessage{
				Role: "assistant",
				ToolCalls: []ChatToolCall{
					{ID: "call_bad", Type: "function", Function: ChatFunctionCall{Name: "exec_command", Arguments: `{"cmd": "ssh root@HOST`}},
					{ID: "call_ok", Type: "function", Function: ChatFunctionCall{Name: "exec_command", Arguments: `{}`}},
				},
			},
			FinishReason: "length",
		}},
	}

	out := ChatCompletionsResponseToResponses(resp, "deepseek-v4-flash", nil, nil, false, nil)
	require.Equal(t, "incomplete", out.Status)
	require.Len(t, out.Output, 1)
	require.Equal(t, "function_call", out.Output[0].Type)
	require.Equal(t, "call_ok", out.Output[0].CallID)
	require.Equal(t, `{}`, out.Output[0].Arguments)
}

func TestResponsesInputToChatMessages_KeepsChatCompletionRoles(t *testing.T) {
	input := json.RawMessage(`[
		{"role":"system","content":"system message"},
		{"role":"user","content":"user message"},
		{"role":"assistant","content":"assistant message"},
		{"role":"tool","content":"tool message"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 4)

	assert.Equal(t, []string{"system", "user", "assistant", "tool"}, chatMessageRoles(messages))
}

func TestResponsesInputToChatMessages_EmptyRoleFallsBackToUser(t *testing.T) {
	messages, err := responsesInputToChatMessages("", json.RawMessage(`[{"role":"","content":"hello"}]`))
	require.NoError(t, err)
	require.Len(t, messages, 1)

	assert.Equal(t, "user", messages[0].Role)
}

func TestResponsesInputToChatMessages_LeadingDeveloperRolesMergeIntoOneSystem(t *testing.T) {
	input := json.RawMessage(`[
		{"role":" Developer ","content":"one"},
		{"role":"\tDEVELOPER\n","content":"two"}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 1)

	assert.Equal(t, []string{"system"}, chatMessageRoles(messages))
	assert.JSONEq(t, `"one\n\ntwo"`, string(messages[0].Content))
}

func TestResponsesToChatCompletionsRequest_InstructionsAndInputDeveloperRole(t *testing.T) {
	req := &ResponsesRequest{
		Model:        "gpt-4o",
		Instructions: "Use concise answers.",
		Input: json.RawMessage(`[
			{"role":"developer","content":[{"type":"input_text","text":"Prefer JSON."}]},
			{"role":"user","content":"Hello"}
		]`),
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 2)

	assert.Equal(t, []string{"system", "user"}, chatMessageRoles(out.Messages))
	assert.JSONEq(t, `"Use concise answers.\n\nPrefer JSON."`, string(out.Messages[0].Content))
	assert.JSONEq(t, `"Hello"`, string(out.Messages[1].Content))
}

func TestResponsesInputToChatMessages_MidConversationDeveloperBecomesUser(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]},
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"<model_switch> switched model"}]},
		{"type":"message","role":"system","content":[{"type":"input_text","text":"be terse"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
	]`)

	messages, err := responsesInputToChatMessages("", input)
	require.NoError(t, err)
	require.Len(t, messages, 5)

	assert.Equal(t, []string{"user", "assistant", "user", "user", "user"}, chatMessageRoles(messages))
	assert.JSONEq(t, `"<model_switch> switched model"`, string(messages[2].Content))
	assert.JSONEq(t, `"be terse"`, string(messages[3].Content))
}

func TestResponsesToChatCompletionsRequest_TextFormatJsonObject(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"role":"user","content":"Return JSON"}
		]`),
		Text: &ResponsesText{
			Format: json.RawMessage(`{"type":"json_object"}`),
		},
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"json_object"}`, string(out.ResponseFormat))
}

func TestResponsesToChatCompletionsRequest_TextFormatJsonSchema(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"role":"user","content":"Return structured JSON"}
		]`),
		Text: &ResponsesText{
			Format: json.RawMessage(`{
				"type":"json_schema",
				"name":"answer",
				"schema":{
					"type":"object",
					"properties":{"ok":{"type":"boolean"}},
					"required":["ok"],
					"additionalProperties":false
				},
				"strict":true
			}`),
		},
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type":"json_schema",
		"json_schema":{
			"name":"answer",
			"schema":{
				"type":"object",
				"properties":{"ok":{"type":"boolean"}},
				"required":["ok"],
				"additionalProperties":false
			},
			"strict":true
		}
	}`, string(out.ResponseFormat))
}

func TestResponsesToChatCompletionsRequest_ParallelToolCalls(t *testing.T) {
	parallel := false
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: json.RawMessage(`[
			{"role":"user","content":"Use tools"}
		]`),
		ParallelToolCalls: &parallel,
	}

	out, err := ResponsesToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.NotNil(t, out.ParallelToolCalls)
	assert.False(t, *out.ParallelToolCalls)

	payload, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"parallel_tool_calls":false`)
}

func chatMessageRoles(messages []ChatMessage) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	return roles
}
