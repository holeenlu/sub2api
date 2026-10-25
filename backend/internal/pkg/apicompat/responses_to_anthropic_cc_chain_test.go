package apicompat

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// These tests drive the exact production path for Chat Completions clients on an
// Anthropic-platform group: ForwardAsChatCompletions runs
// ChatCompletionsToResponses → ResponsesToAnthropicRequest
// (gateway_forward_as_chat_completions.go), then forwards the Anthropic body
// upstream. They assert the tool-pairing repair holds through that full chain,
// not only for codex-style Responses input.
func ccChainToAnthropic(t *testing.T, ccReq *ChatCompletionsRequest) []AnthropicMessage {
	t.Helper()
	respReq, err := ChatCompletionsToResponses(ccReq)
	require.NoError(t, err)
	anthReq, err := ResponsesToAnthropicRequest(respReq)
	require.NoError(t, err)
	assertAnthropicPairing(t, anthReq.Messages)
	return anthReq.Messages
}

// Reproduces the production 400:
//
//	unexpected ...content.0: tool_use_id found in tool_result blocks:
//	call_00_TgfbRvKlnD7oK6Dg00sL1661. Each tool_result block must have a
//	corresponding tool_use block in the previous message.
//
// A Chat Completions client trimmed its history and kept a tool result whose
// announcing assistant tool_calls message was dropped (sliding-window context
// management). The orphan tool_result has no matching tool_use → upstream 400.
// The repair drops the orphan so the request is valid.
func TestCCChain_OrphanToolResultFromTrimmedHistory(t *testing.T) {
	orphanID := "call_00_TgfbRvKlnD7oK6Dg00sL1661"
	msgs := ccChainToAnthropic(t, &ChatCompletionsRequest{
		Model: "deepseek-v4-pro",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"search the web for X"`)},
			// The assistant tool_calls message that announced orphanID was trimmed.
			{Role: "tool", ToolCallID: orphanID, Content: json.RawMessage(`"stale search results"`)},
			{Role: "assistant", Content: json.RawMessage(`"Here is what I found."`)},
			{Role: "user", Content: json.RawMessage(`"thanks, now do Y"`)},
		},
	})
	for _, m := range msgs {
		require.Falsef(t, hasToolResult(parseContentBlocks(m.Content), orphanID),
			"orphan tool_result %s should have been dropped", orphanID)
	}
}

// A parallel web_search where one sibling's result never came back (the tool
// failed/was skipped). The unanswered tool_use would otherwise trip Anthropic's
// "tool_use without tool_result" check; the repair drops it.
func TestCCChain_ParallelToolOneResultMissing(t *testing.T) {
	msgs := ccChainToAnthropic(t, &ChatCompletionsRequest{
		Model: "deepseek-v4-pro",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"search A and B"`)},
			{Role: "assistant", Content: json.RawMessage(`"searching both"`), ToolCalls: []ChatToolCall{
				{ID: "call_a", Type: "function", Function: ChatFunctionCall{Name: "web_search", Arguments: `{"q":"A"}`}},
				{ID: "call_b", Type: "function", Function: ChatFunctionCall{Name: "web_search", Arguments: `{"q":"B"}`}},
			}},
			{Role: "tool", ToolCallID: "call_a", Content: json.RawMessage(`"result A"`)},
			// call_b's result is missing.
		},
	})
	for _, m := range msgs {
		require.Falsef(t, hasToolUse(parseContentBlocks(m.Content), "call_b"),
			"unanswered tool_use call_b should have been dropped")
	}
}

// Baseline: a well-formed multi-round tool history (text + tool_calls per
// assistant turn) converts and pairs correctly through the full chain.
func TestCCChain_WellFormedMultiRound(t *testing.T) {
	msgs := ccChainToAnthropic(t, &ChatCompletionsRequest{
		Model: "deepseek-v4-pro",
		Messages: []ChatMessage{
			{Role: "user", Content: json.RawMessage(`"do A then B"`)},
			{Role: "assistant", Content: json.RawMessage(`"running A"`), ToolCalls: []ChatToolCall{
				{ID: "call_a", Type: "function", Function: ChatFunctionCall{Name: "exec", Arguments: `{"cmd":"A"}`}},
			}},
			{Role: "tool", ToolCallID: "call_a", Content: json.RawMessage(`"A ok"`)},
			{Role: "assistant", Content: json.RawMessage(`"A done, running B"`), ToolCalls: []ChatToolCall{
				{ID: "call_b", Type: "function", Function: ChatFunctionCall{Name: "exec", Arguments: `{"cmd":"B"}`}},
			}},
			{Role: "tool", ToolCallID: "call_b", Content: json.RawMessage(`"B ok"`)},
			{Role: "assistant", Content: json.RawMessage(`"all done"`)},
		},
	})
	// Both calls survive and stay paired (assertAnthropicPairing already checks).
	var sawA, sawB bool
	for _, m := range msgs {
		blocks := parseContentBlocks(m.Content)
		sawA = sawA || hasToolUse(blocks, "call_a")
		sawB = sawB || hasToolUse(blocks, "call_b")
	}
	require.True(t, sawA && sawB, "both well-formed calls should be preserved")
}

func TestCCChain_AdjacentAssistantToolsPreserveAllCalls(t *testing.T) {
	msgs := ccChainToAnthropic(t, &ChatCompletionsRequest{Model: "claude-sonnet-5-5", Messages: []ChatMessage{
		{Role: "user", Content: json.RawMessage(`"run both"`)},
		{Role: "assistant", Content: json.RawMessage(`"..."`), ToolCalls: []ChatToolCall{{ID: "a", Type: "function", Function: ChatFunctionCall{Name: "lookup", Arguments: `{}`}}}},
		{Role: "assistant", ToolCalls: []ChatToolCall{{ID: "b", Type: "function", Function: ChatFunctionCall{Name: "lookup", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: "a", Content: json.RawMessage(`"A"`)},
		{Role: "tool", ToolCallID: "b", Content: json.RawMessage(`"B"`)},
	}})
	var ids, texts []string
	for _, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		for _, block := range parseContentBlocks(msg.Content) {
			if block.Type == "tool_use" {
				ids = append(ids, block.ID)
			}
			if block.Type == "text" {
				texts = append(texts, block.Text)
			}
		}
	}
	require.Equal(t, []string{"toolu_a", "toolu_b"}, ids)
	require.Equal(t, []string{"..."}, texts)
}

func TestResponsesFormatAndCompletedTailFidelity(t *testing.T) {
	input := json.RawMessage(`[{"role":"user","content":"question"},{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer","annotations":[]}]}]`)
	req := &ResponsesRequest{Model: "claude-sonnet-5-5", Input: input, Text: &ResponsesText{Format: json.RawMessage(`{"type":"json_schema","name":"result","strict":true,"schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"additionalProperties":false}}`)}}
	out, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 1)
	require.Len(t, out.Diagnostics, 1)
	require.JSONEq(t, `{"type":"json_schema","schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"additionalProperties":false}}`, string(out.OutputConfig.Format))
	require.Equal(t, string(input), string(req.Input))
	req.FromChatCompletions = true
	out, err = ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 2)
	req.FromChatCompletions = false
	req.Input = json.RawMessage(`[{"role":"user","content":"question"},{"type":"message","role":"assistant","status":"in_progress","content":[{"type":"output_text","text":"partial"}]}]`)
	out, err = ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, out.Messages, 2)
	req.Input = json.RawMessage(`[{"role":"user","content":"question"},{"type":"message","role":"assistant","status":"completed","provider_signature":"opaque","content":[{"type":"output_text","text":"answer"}]}]`)
	var diagnostics ConversionDiagnostics
	require.Equal(t, req.Input, omitCompletedResponsesTail(req.Input, &diagnostics))
	require.Empty(t, diagnostics)
}
func TestConversionDiagnosticsBounded(t *testing.T) {
	var d ConversionDiagnostics
	for i := 0; i < 100; i++ {
		d.Add("schema_legacy_conversion", fmt.Sprintf("tools[%d]", i))
		d.Add("schema_legacy_conversion", fmt.Sprintf("tools[%d]", i))
	}
	require.Len(t, d, 32)
}
