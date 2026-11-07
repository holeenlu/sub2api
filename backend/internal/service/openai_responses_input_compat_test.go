package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSanitizeOpenAIResponsesOrphanToolOutputs(t *testing.T) {
	t.Run("named standalone inputs do not legitimize orphan results", func(t *testing.T) {
		named := map[string]any{"type": "function_call_output", "name": "send_message_to_thread", "namespace": "codex_app", "output": "delegation"}
		input := []any{
			named,
			map[string]any{"type": "function_call_output", "output": "missing name and call id"},
			map[string]any{"type": "function_call_output", "name": " ", "output": "blank name"},
			map[string]any{"type": "function_call_output", "name": "send_message_to_thread", "call_id": "missing", "output": "orphan result"},
			map[string]any{"type": "custom_tool_call_output", "name": "apply_patch", "output": "missing call id"},
		}
		reqBody := map[string]any{"input": input}

		changed, err := sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, input, false)
		require.NoError(t, err)
		require.True(t, changed)
		require.Equal(t, []any{named}, reqBody["input"])
	})

	t.Run("preserves matches regardless of item order", func(t *testing.T) {
		input := []any{
			map[string]any{"type": "tool_search_output", "call_id": "search_1", "output": "first"},
			map[string]any{"type": "tool_search_call", "id": "search_1", "query": "docs"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "custom_1", "output": "second"},
			map[string]any{"type": "item_reference", "id": "custom_1"},
		}
		reqBody := map[string]any{"input": input}

		changed, err := sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, input, false)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, input, reqBody["input"])
	})

	t.Run("outputs do not legitimize each other", func(t *testing.T) {
		input := []any{
			map[string]any{"type": "function_call_output", "call_id": "missing", "output": "one"},
			map[string]any{"type": "tool_search_output", "call_id": "missing", "output": "two"},
			map[string]any{"type": "custom_tool_call_output", "call_id": "missing", "output": "three"},
			map[string]any{"type": "mcp_tool_call_output", "call_id": "missing", "output": "four"},
		}
		reqBody := map[string]any{"input": input}

		changed, err := sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, input, false)
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, input, reqBody["input"], "a rejection must not erase client input")
	})

	t.Run("preserves all output variants with matching calls", func(t *testing.T) {
		pairs := []struct {
			callType   string
			outputType string
		}{
			{callType: "function_call", outputType: "function_call_output"},
			{callType: "tool_search_call", outputType: "tool_search_output"},
			{callType: "custom_tool_call", outputType: "custom_tool_call_output"},
			{callType: "mcp_tool_call", outputType: "mcp_tool_call_output"},
		}
		input := make([]any, 0, len(pairs)*2)
		for index, pair := range pairs {
			callID := string(rune('a' + index))
			input = append(input,
				map[string]any{"type": pair.callType, "call_id": callID},
				map[string]any{"type": pair.outputType, "call_id": callID, "output": "ok"},
			)
		}
		reqBody := map[string]any{"input": input}

		changed, err := sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, input, false)
		require.NoError(t, err)
		require.False(t, changed)
	})

	t.Run("previous response may contain the missing call", func(t *testing.T) {
		input := []any{map[string]any{"type": "function_call_output", "call_id": "remote", "output": "ok"}}
		reqBody := map[string]any{"input": input, "previous_response_id": "resp_1"}

		changed, err := sanitizeOpenAIResponsesOrphanToolOutputs(reqBody, input, true)
		require.NoError(t, err)
		require.False(t, changed)
		require.Equal(t, input, reqBody["input"])
	})
}

func TestWebSocketCompatibilityPreservesNamedDelegation(t *testing.T) {
	for _, toolName := range []string{"create_thread", "send_message_to_thread"} {
		for _, withHistory := range []bool{false, true} {
			for _, previousResponseID := range []string{"", "resp_previous"} {
				name := fmt.Sprintf("%s/history=%t/previous=%t", toolName, withHistory, previousResponseID != "")
				t.Run(name, func(t *testing.T) {
					input := []any{}
					if withHistory {
						input = append(input,
							map[string]any{"type": "function_call", "call_id": "fc_history", "name": "lookup", "arguments": "{}"},
							map[string]any{"type": "function_call_output", "call_id": "fc_history", "output": "historical result"},
						)
					}
					input = append(input,
						map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Updated environment context."}}},
						map[string]any{"type": "function_call_output", "name": toolName, "namespace": "codex_app", "output": "<codex_delegation>\n  <source_thread_id>source-task</source_thread_id>\n  <input>Reply with DELEGATION_OK.</input>\n</codex_delegation>"},
					)
					reqBody := map[string]any{"type": "response.create", "model": "gpt-5.5", "input": input}
					if previousResponseID != "" {
						reqBody["previous_response_id"] = previousResponseID
					}
					body, err := json.Marshal(reqBody)
					require.NoError(t, err)
					account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}

					normalized, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
					require.NoError(t, err)
					var got map[string]any
					require.NoError(t, json.Unmarshal(normalized, &got))
					require.Equal(t, input, got["input"], "preserve the delegation envelope, native type and history in order")
					require.Equal(t, reqBody["previous_response_id"], got["previous_response_id"])

					again, changed, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(normalized, account, false)
					require.NoError(t, err)
					require.False(t, changed, "normalization must be idempotent")
					require.JSONEq(t, string(normalized), string(again))
				})
			}
		}
	}
}

func TestOpenAIResponsesInputTextIsNeverSilentlyTruncated(t *testing.T) {
	atLimit := strings.Repeat("z", openAIResponsesInputTextMaxChars)
	oversized := strings.Repeat("a", openAIResponsesInputTextMaxChars) + "中"
	input := []any{
		map[string]any{"type": "function_call_output", "call_id": "limit", "output": atLimit},
		map[string]any{"type": "function_call_output", "call_id": "a", "output": oversized},
		map[string]any{"type": "tool_search_output", "call_id": "b", "output": oversized},
		map[string]any{"type": "custom_tool_call_output", "call_id": "c", "output": oversized},
		map[string]any{"type": "mcp_tool_call_output", "call_id": "d", "output": oversized},
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "short"},
				map[string]any{"type": "input_text", "text": oversized},
			},
		},
	}
	reqBody := map[string]any{"input": input}

	require.False(t, truncateOpenAIResponsesInputText(reqBody))
	first, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, atLimit, first["output"])
	for _, rawItem := range input[1:5] {
		item, ok := rawItem.(map[string]any)
		require.True(t, ok)
		require.Equal(t, oversized, item["output"])
	}
	last, ok := input[5].(map[string]any)
	require.True(t, ok)
	content, ok := last["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 2)
	shortPart, ok := content[0].(map[string]any)
	require.True(t, ok)
	oversizedPart, ok := content[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "short", shortPart["text"])
	require.Equal(t, oversized, oversizedPart["text"])
}

func TestOpenAIResponsesInputNeverRequestsPreemptiveTruncation(t *testing.T) {
	short := []byte(`{"input":[{"type":"function_call_output","output":"ok"}]}`)
	largeUnrelated := []byte(`{"input":"` + strings.Repeat("x", openAIResponsesInputTextMaxChars+1) + `"}`)
	largeOutput := []byte(`{"input":[{"type":"function_call_output","output":"` + strings.Repeat("x", openAIResponsesInputTextMaxChars+1) + `"}]}`)

	require.False(t, openAIResponsesInputMayNeedTruncation(short))
	require.False(t, openAIResponsesInputMayNeedTruncation(largeUnrelated))
	require.False(t, openAIResponsesInputMayNeedTruncation(largeOutput))
}

func TestOpenAIGatewayService_OAuthPreservesContinuationContext(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"previous_response_id":"resp_missing","input":[{"type":"function_call_output","call_id":"fc_missing","output":"keep this result"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIOAuthNamespaceTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, "resp_missing", gjson.GetBytes(upstream.bodies[0], "previous_response_id").String())
	require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(upstream.bodies[0], "input").Raw)
}

func TestOpenAIGatewayService_PreservesOversizedToolOutputForUpstream(t *testing.T) {
	oversized := strings.Repeat("x", openAIResponsesInputTextMaxChars) + "中"
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"` + oversized + `"}]}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_ok","output":[],"usage":{"input_tokens":1,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`),
	}}

	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(
		context.Background(),
		newOpenAIRejectedFieldTestContext(body),
		newOpenAIRejectedFieldTestAccount(),
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.bodies, 1)
	require.Equal(t, oversized, gjson.GetBytes(upstream.bodies[0], "input.1.output").String())
}

func TestOpenAIResponsesContextCarriers(t *testing.T) {
	for _, fields := range []string{
		`"input":"hello"`, `"input":[{"type":"future_multimodal_item","payload":{"opaque":true}}]`,
		`"previous_response_id":"resp_prior"`, `"prompt":{"id":"pmpt_template"}`,
		`"conversation":"conv_saved"`, `"conversation":{"id":"conv_saved"}`,
		`"input":[],"previous_response_id":"resp_prior"`,
	} {
		t.Run(fields, func(t *testing.T) { require.NoError(t, validateOpenAIResponsesInput([]byte(`{`+fields+`}`))) })
	}
	for _, fields := range []string{``, `"input":null`, `"input":[]`, `"input":" "`, `"previous_response_id":""`, `"prompt":{}`, `"conversation":null`} {
		t.Run(fields, func(t *testing.T) {
			var inputErr *openAIResponsesInputError
			require.ErrorAs(t, validateOpenAIResponsesInput([]byte(`{`+fields+`}`)), &inputErr)
			require.Equal(t, "missing_required_parameter", inputErr.Code)
		})
	}
}

func TestOpenAIResponsesForwardContextPreservation(t *testing.T) {
	cases := []struct{ name, fields, code string }{
		{"missing", ``, "missing_required_parameter"},
		{"null", `,"input":null`, "missing_required_parameter"},
		{"empty", `,"input":[]`, "missing_required_parameter"},
		{"orphan", `,"input":[{"type":"function_call_output","call_id":"unknown","output":"result"}]`, "missing_tool_context"},
		{"system_only", `,"input":[{"role":"system","content":"Answer concisely"}]`, ""},
		{"previous_only", `,"previous_response_id":"resp_prior"`, ""},
		{"previous_delta", `,"previous_response_id":"resp_prior","input":[{"role":"user","content":"continue this task"}]`, ""},
		{"tool_continuation", `,"previous_response_id":"resp_prior","input":[{"type":"function_call_output","call_id":"fc_prior","output":"result"}]`, ""},
		{"template", `,"prompt":{"id":"pmpt_template"}`, ""},
		{"conversation", `,"conversation":"conv_saved"`, ""},
		{"legacy_prompt", `,"prompt":"the original task"`, ""},
	}
	for _, passthrough := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/passthrough=%t", tc.name, passthrough), func(t *testing.T) {
				body := []byte(`{"model":"gpt-6.1-sol","stream":false,"instructions":"Follow the request"` + tc.fields + `}`)
				original := string(body)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"resp_context","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)}}
				svc := newOpenAIRejectedFieldTestService(upstream)
				c := newOpenAIRejectedFieldTestContext(body)
				account := newOpenAIOAuthNamespaceTestAccount()
				if passthrough {
					account.Extra = map[string]any{"openai_passthrough": true}
				}
				result, err := svc.Forward(context.Background(), c, account, body)
				require.Equal(t, original, string(body), "attempt preparation cannot mutate the caller's canonical bytes")
				if tc.code != "" {
					var inputErr *openAIResponsesInputError
					require.ErrorAs(t, err, &inputErr)
					require.Equal(t, tc.code, inputErr.Code)
					require.Equal(t, http.StatusBadRequest, c.Writer.Status())
					require.Nil(t, result, "local rejection has no billable usage")
					require.Empty(t, upstream.requests, "invalid context must not reach an upstream or failover")
					_, hasUpstreamError := c.Get(OpsUpstreamStatusCodeKey)
					require.False(t, hasUpstreamError, "a local input error is not an upstream failure")
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.bodies, 1)
				wire := upstream.bodies[0]
				require.NoError(t, validateOpenAIResponsesInput(wire))
				for _, field := range []string{"previous_response_id", "conversation"} {
					require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(wire, field).Raw)
				}
				if tc.name == "system_only" {
					require.Equal(t, "Answer concisely", gjson.GetBytes(wire, "input.0.content").String())
				}
				if tc.name == "tool_continuation" || tc.name == "previous_delta" {
					require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(wire, "input").Raw)
				}
			})
		}
	}
}

func TestOpenAIResponsesContextLossIsGatewayFailure(t *testing.T) {
	original := []byte(`{"input":[{"role":"user","content":"private original input"}]}`)
	c := newOpenAIRejectedFieldTestContext(original)
	err := validateOpenAIResponsesInputTransition(original, []byte(`{"input":[]}`))
	var rejected *openAIResponsesInputError
	require.ErrorAs(t, err, &rejected)
	require.Equal(t, "gateway_input_context_lost", rejected.Code)
	writeOpenAIResponsesInputError(c, err)
	require.Equal(t, http.StatusInternalServerError, c.Writer.Status())
	event, encodeErr := rejected.websocketEvent([]byte(`{"event_id":"request1","stream_id":"lane1"}`))
	require.NoError(t, encodeErr)
	require.Equal(t, "server_error", gjson.GetBytes(event, "error.type").String())
	require.NotContains(t, string(event), "private original input")
	require.NoError(t, validateOpenAIResponsesInputTransition(original, original))
}

func TestOpenAIResponsesInputErrorAfterKeepaliveTerminatesSSE(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":[]}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	_, err := c.Writer.WriteString(": keepalive\n\n")
	require.NoError(t, err)
	c.Writer.Flush()
	upstream := &httpUpstreamRecorder{}
	result, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), c, newOpenAIOAuthNamespaceTestAccount(), body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Empty(t, upstream.requests)
	require.Equal(t, http.StatusOK, recorder.Code, "HTTP headers were already committed by the queue")
	require.Contains(t, recorder.Body.String(), "event: response.failed\n")
	parts := strings.Split(recorder.Body.String(), "data: ")
	require.Len(t, parts, 2, "exactly one authoritative failure event")
	require.Equal(t, "missing_required_parameter", gjson.Get(parts[1], "response.error.code").String())
	require.Equal(t, "failed", gjson.Get(parts[1], "response.status").String())
}
