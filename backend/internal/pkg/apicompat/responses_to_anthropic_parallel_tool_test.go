package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(v int) *int       { return &v }
func strPtr(s string) *string { return &s }

// TestStreamingParallelToolUseNoGhostDelta reproduces the bug from issue #4193:
// when the CC→Responses→Anthropic bridge finalizes parallel tool calls whose
// arguments arrived packed in function_call_arguments.done (no prior delta),
// resToAnthHandleFuncArgsDone used state.ContentBlockIndex directly instead of
// looking up OutputIndexToBlockIdx. After the first tool's block was closed
// (ContentBlockIndex++), the second tool's .done emitted a content_block_delta
// on an index that was never content_block_start'ed — Claude Code reports
// "Content block not found".
//
// This test drives the full finalize path: CC chunks with two parallel
// tool_calls → ChatCompletionsChunkToResponsesEvents → FinalizeChatCompletionsResponsesStream
// → ResponsesEventToAnthropicEvents, and asserts every content_block_delta
// targets a block that was previously content_block_start'ed.
func TestStreamingParallelToolUseNoGhostDelta(t *testing.T) {
	ccState := NewChatCompletionsToResponsesStreamState("glm-5.2")
	anthropicState := NewResponsesEventToAnthropicState()
	anthropicState.Model = "glm-5.2"

	// Chunk 1: first tool_call arrives with id + name + packed arguments.
	chatChunk1 := &ChatCompletionsChunk{
		ID:    "chatcmpl-1",
		Model: "glm-5.2",
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{
				ToolCalls: []ChatToolCall{{
					Index: intPtr(0),
					ID:    "call_weather",
					Type:  "function",
					Function: ChatFunctionCall{
						Name:      "get_weather",
						Arguments: `{"city":"Tokyo"}`,
					},
				}},
			},
		}},
	}

	// Chunk 2: second tool_call arrives with id + name + packed arguments.
	chatChunk2 := &ChatCompletionsChunk{
		ID:    "chatcmpl-1",
		Model: "glm-5.2",
		Choices: []ChatChunkChoice{{
			Index: 0,
			Delta: ChatDelta{
				ToolCalls: []ChatToolCall{{
					Index: intPtr(1),
					ID:    "call_time",
					Type:  "function",
					Function: ChatFunctionCall{
						Name:      "get_time",
						Arguments: `{}`,
					},
				}},
			},
		}},
	}

	// Chunk 3: finish.
	chatChunk3 := &ChatCompletionsChunk{
		ID:    "chatcmpl-1",
		Model: "glm-5.2",
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatDelta{},
			FinishReason: strPtr("tool_calls"),
		}},
	}

	// Feed chunks through the CC→Responses bridge, then through Responses→Anthropic.
	var allAnthropicEvents []AnthropicStreamEvent
	for _, chunk := range []*ChatCompletionsChunk{chatChunk1, chatChunk2, chatChunk3} {
		responsesEvents := ChatCompletionsChunkToResponsesEvents(chunk, ccState)
		for _, rEvent := range responsesEvents {
			allAnthropicEvents = append(allAnthropicEvents, ResponsesEventToAnthropicEvents(&rEvent, anthropicState)...)
		}
	}

	// Finalize: closeChatToolItems emits function_call_arguments.done for each tool.
	finalResponsesEvents := FinalizeChatCompletionsResponsesStream(ccState)
	for _, rEvent := range finalResponsesEvents {
		allAnthropicEvents = append(allAnthropicEvents, ResponsesEventToAnthropicEvents(&rEvent, anthropicState)...)
	}

	// Build the set of block indices that were content_block_start'ed.
	startedBlocks := make(map[int]string) // index → block type
	for _, e := range allAnthropicEvents {
		if e.Type == "content_block_start" && e.ContentBlock != nil {
			startedBlocks[*e.Index] = e.ContentBlock.Type
		}
	}

	// Assert: every content_block_delta targets a started block (no ghost deltas).
	for _, e := range allAnthropicEvents {
		if e.Type != "content_block_delta" || e.Index == nil {
			continue
		}
		idx := *e.Index
		_, ok := startedBlocks[idx]
		require.Truef(t, ok,
			"content_block_delta on index %d which was never content_block_start'ed (ghost delta bug #4193)", idx)
	}

	// Assert: every content_block_stop targets a started block.
	for _, e := range allAnthropicEvents {
		if e.Type != "content_block_stop" || e.Index == nil {
			continue
		}
		idx := *e.Index
		_, ok := startedBlocks[idx]
		require.Truef(t, ok,
			"content_block_stop on index %d which was never content_block_start'ed", idx)
	}

	// Assert: both tool_use blocks were opened.
	var toolUseBlocks []int
	for idx, blockType := range startedBlocks {
		if blockType == "tool_use" {
			toolUseBlocks = append(toolUseBlocks, idx)
		}
	}
	assert.Len(t, toolUseBlocks, 2, "both parallel tool_use blocks should be opened")

	// Assert: stop_reason is tool_use.
	var sawMessageDelta bool
	for _, e := range allAnthropicEvents {
		if e.Type == "message_delta" {
			sawMessageDelta = true
			assert.Equal(t, "tool_use", e.Delta.StopReason)
		}
	}
	assert.True(t, sawMessageDelta, "message_delta should be emitted")
	require.NoError(t, anthropicState.Err())
	inputs := make(map[int]string)
	for _, e := range allAnthropicEvents {
		if e.Delta != nil && e.Delta.Type == "input_json_delta" {
			inputs[*e.Index] += e.Delta.PartialJSON
		}
	}
	require.JSONEq(t, `{"city":"Tokyo"}`, inputs[0])
	require.JSONEq(t, `{}`, inputs[1])
	requireAnthropicBlockLifecycle(t, allAnthropicEvents)
}

func TestResponsesTools_ParallelDoneAndSnapshotRecovery(t *testing.T) {
	for _, terminal := range []string{"response.completed", "response.done", "response.incomplete"} {
		for _, snapshotOnly := range []bool{false, true} {
			t.Run(terminal+"/snapshot="+strBool(snapshotOnly), func(t *testing.T) {
				chat := NewResponsesEventToChatState()
				anth := NewResponsesEventToAnthropicState()
				acc := NewBufferedResponseAccumulator()
				var chunks []ChatCompletionsChunk
				var blocks []AnthropicStreamEvent
				feed := func(e ResponsesStreamEvent) {
					chunks = append(chunks, ResponsesEventToChatChunks(&e, chat)...)
					blocks = append(blocks, ResponsesEventToAnthropicEvents(&e, anth)...)
					acc.ProcessEvent(&e)
					require.NoError(t, chat.Err())
					require.NoError(t, anth.Err())
					require.NoError(t, acc.Err())
				}
				items := []ResponsesOutput{
					{Type: "function_call", ID: "i_a", CallID: "call_a", Name: "inspect", Arguments: `{"record":42}`},
					{Type: "function_call", ID: "i_b", CallID: "call_b", Name: "compare", Arguments: `{"record":7}`},
				}
				if !snapshotOnly {
					feed(ResponsesStreamEvent{Type: "response.created", Response: &ResponsesResponse{ID: "r"}})
					for i, item := range items {
						item.Arguments = ""
						e := ResponsesStreamEvent{Type: "response.output_item.added", OutputIndex: i + 3, Item: &item}
						feed(e)
						feed(e) // replay must retain the same index
					}
					feed(ResponsesStreamEvent{Type: "response.function_call_arguments.delta", OutputIndex: 3, Delta: `{"record":`})
					// The second tool completes first, with no argument deltas.
					feed(ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: 4, Arguments: items[1].Arguments})
					feed(ResponsesStreamEvent{Type: "response.function_call_arguments.done", OutputIndex: 3, Arguments: items[0].Arguments})
				}
				status := "completed"
				var details *ResponsesIncompleteDetails
				if terminal == "response.incomplete" {
					status = "incomplete"
					details = &ResponsesIncompleteDetails{Reason: "max_output_tokens"}
				}
				end := ResponsesStreamEvent{Type: terminal, Response: &ResponsesResponse{ID: "r", Status: status, IncompleteDetails: details, Output: items, Usage: &ResponsesUsage{InputTokens: 19, OutputTokens: 11}}}
				feed(end)
				feed(end)
				feed(ResponsesStreamEvent{Type: "response.output_text.delta", Delta: "late content"})
				got := make(map[int]string)
				starts, finishes := 0, 0
				for _, c := range chunks {
					for _, choice := range c.Choices {
						if choice.FinishReason != nil {
							finishes++
						}
						for _, tool := range choice.Delta.ToolCalls {
							if tool.ID != "" {
								starts++
							}
							got[*tool.Index] += tool.Function.Arguments
						}
						if choice.Delta.Content != nil {
							require.NotContains(t, *choice.Delta.Content, "late")
						}
					}
				}
				require.Equal(t, 2, starts)
				require.Equal(t, 1, finishes)
				require.Equal(t, items[0].Arguments, got[0])
				require.Equal(t, items[1].Arguments, got[1])
				requireAnthropicBlockLifecycle(t, blocks)
				inputs := make(map[int]string)
				stops := 0
				for _, e := range blocks {
					if e.Delta != nil && e.Delta.Type == "input_json_delta" {
						inputs[*e.Index] += e.Delta.PartialJSON
					}
					if e.Type == "message_stop" {
						stops++
					}
				}
				require.Equal(t, 1, stops)
				require.Equal(t, got, inputs)
				buffered := &ResponsesResponse{}
				acc.SupplementResponseOutput(buffered)
				require.Len(t, buffered.Output, 2)
				require.Equal(t, items[0].Arguments, buffered.Output[0].Arguments)
				require.Equal(t, items[1].Arguments, buffered.Output[1].Arguments)
				require.Empty(t, FinalizeResponsesChatStream(chat))
				require.Empty(t, FinalizeResponsesAnthropicStream(anth))
			})
		}
	}
}

func strBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestResponsesTools_RejectConflictingOrInvalidArguments(t *testing.T) {
	for _, tc := range []struct{ name, prefix, done string }{
		{"conflict", `{"record":3`, `{"record":42}`},
		{"truncated", `{"record":`, ""},
		{"array", "", `[1,2]`},
		{"scalar", "", `"not an object"`},
		{"null", "", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat, anth := NewResponsesEventToChatState(), NewResponsesEventToAnthropicState()
			acc := NewBufferedResponseAccumulator()
			for _, e := range []ResponsesStreamEvent{
				{Type: "response.output_item.added", Item: &ResponsesOutput{Type: "function_call", CallID: "c", Name: "inspect"}},
				{Type: "response.function_call_arguments.delta", Delta: tc.prefix},
				{Type: "response.function_call_arguments.done", Arguments: tc.done},
			} {
				ResponsesEventToChatChunks(&e, chat)
				ResponsesEventToAnthropicEvents(&e, anth)
				acc.ProcessEvent(&e)
			}
			require.Error(t, chat.Err())
			require.Error(t, anth.Err())
			require.Error(t, acc.Err())
			end := &ResponsesStreamEvent{Type: "response.completed", Response: &ResponsesResponse{Status: "completed"}}
			require.Empty(t, ResponsesEventToChatChunks(end, chat))
			require.Empty(t, ResponsesEventToAnthropicEvents(end, anth))
			require.Empty(t, FinalizeResponsesChatStream(chat))
			require.Empty(t, FinalizeResponsesAnthropicStream(anth))
		})
	}
}

func TestResponsesTools_ObjectArgumentsAndCustomSnapshot(t *testing.T) {
	var response ResponsesResponse
	require.NoError(t, json.Unmarshal([]byte(`{"status":"completed","output":[{"type":"function_call","id":"f","call_id":"c","name":"inspect","arguments":{"record":42}},{"type":"custom_tool_call","id":"x","call_id":"d","name":"patch","input":"line one\nline two"}]}`), &response))
	require.NoError(t, ValidateResponsesToolArguments(&response))
	ant := ResponsesToAnthropic(&response, "example")
	require.Len(t, ant.Content, 2)
	require.JSONEq(t, `{"record":42}`, string(ant.Content[0].Input))
	require.JSONEq(t, `{"input":"line one\nline two"}`, string(ant.Content[1].Input))
	state := NewResponsesEventToAnthropicState()
	events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{Type: "response.completed", Response: &response}, state)
	require.NoError(t, state.Err())
	requireAnthropicBlockLifecycle(t, events)
	inputs := make(map[int]string)
	for _, e := range events {
		if e.Delta != nil && e.Delta.Type == "input_json_delta" {
			inputs[*e.Index] += e.Delta.PartialJSON
		}
	}
	require.JSONEq(t, string(ant.Content[0].Input), inputs[0])
	require.JSONEq(t, string(ant.Content[1].Input), inputs[1])
	response.Output[0].Arguments = `[]`
	require.Error(t, ValidateResponsesToolArguments(&response))
}

func TestResponsesDoneOnlyTextReasoningAndUsageRemainSeparate(t *testing.T) {
	chat, anth := NewResponsesEventToChatState(), NewResponsesEventToAnthropicState()
	chat.IncludeUsage = true
	var chunks []ChatCompletionsChunk
	var blocks []AnthropicStreamEvent
	for _, event := range []ResponsesStreamEvent{
		{Type: "response.created", Response: &ResponsesResponse{ID: "r", Model: "example"}},
		{Type: "response.reasoning_summary_text.done", OutputIndex: 0, Text: "analysis"},
		{Type: "response.output_item.done", OutputIndex: 0, Item: &ResponsesOutput{Type: "reasoning", Summary: []ResponsesSummary{{Type: "summary_text", Text: "analysis"}}, EncryptedContent: "opaque-signature"}},
		{Type: "response.output_text.done", OutputIndex: 1, Text: "answer"},
		{Type: "response.done", Response: &ResponsesResponse{Status: "completed", Usage: &ResponsesUsage{InputTokens: 23, OutputTokens: 11, CacheCreationInputTokens: 2, InputTokensDetails: &ResponsesInputTokensDetails{CachedTokens: 5}, OutputTokensDetails: &ResponsesOutputTokensDetails{ReasoningTokens: 4}}}},
	} {
		chunks = append(chunks, ResponsesEventToChatChunks(&event, chat)...)
		blocks = append(blocks, ResponsesEventToAnthropicEvents(&event, anth)...)
	}
	var reasoning, text, thinking, signature string
	for _, chunk := range chunks {
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != nil {
				text += *choice.Delta.Content
			}
			if choice.Delta.ReasoningContent != nil {
				reasoning += *choice.Delta.ReasoningContent
			}
		}
	}
	for _, block := range blocks {
		if block.Delta != nil {
			thinking += block.Delta.Thinking
			signature += block.Delta.Signature
		}
	}
	require.Equal(t, "answer", text)
	require.Equal(t, "analysis", reasoning)
	require.Equal(t, "answer", collectAnthropicText(blocks))
	require.Equal(t, reasoning, thinking)
	require.Equal(t, "opaque-signature", signature)
	require.Equal(t, 23, chat.Usage.PromptTokens)
	require.Equal(t, 11, chat.Usage.CompletionTokens)
	require.Equal(t, 4, chat.Usage.CompletionTokensDetails.ReasoningTokens)
	require.Equal(t, 16, anth.InputTokens)
	require.Equal(t, 11, anth.OutputTokens)
	require.Equal(t, 5, anth.CacheReadInputTokens)
	require.Equal(t, 2, anth.CacheCreationInputTokens)
	requireAnthropicBlockLifecycle(t, blocks)
}

func TestResponsesToolValidationRetainsTerminalUsage(t *testing.T) {
	event := &ResponsesStreamEvent{Type: "response.completed", Response: &ResponsesResponse{Status: "completed", Output: []ResponsesOutput{{Type: "function_call", CallID: "c", Name: "inspect", Arguments: `{"record":`}}, Usage: &ResponsesUsage{InputTokens: 17, OutputTokens: 6}}}
	chat, anth := NewResponsesEventToChatState(), NewResponsesEventToAnthropicState()
	require.Empty(t, ResponsesEventToChatChunks(event, chat))
	require.Empty(t, ResponsesEventToAnthropicEvents(event, anth))
	require.Error(t, chat.Err())
	require.Error(t, anth.Err())
	require.Equal(t, 17, chat.Usage.PromptTokens)
	require.Equal(t, 6, chat.Usage.CompletionTokens)
	require.Equal(t, 17, anth.InputTokens)
	require.Equal(t, 6, anth.OutputTokens)
}

// TestStreamingParallelToolUseSecondToolPackedArgsDone is a focused unit test
// for the exact bug: two tools, the first streams its arguments via deltas
// (argument deltas already delivered), the second has arguments
// packed only in .done (no prior argument delta). Before the fix, the second
// tool's .done emitted content_block_delta on state.ContentBlockIndex which
// had already been incremented past the second tool's block.
func TestStreamingParallelToolUseSecondToolPackedArgsDone(t *testing.T) {
	state := NewResponsesEventToAnthropicState()

	// response.created
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_par", Model: "glm-5.2"},
	}, state)

	// Tool 1: output_item.added (index 0)
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 0,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_a", Name: "tool_a"},
	}, state)

	// Tool 1: arguments streamed via delta.
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.delta",
		OutputIndex: 0,
		Delta:       `{"x":1}`,
	}, state)

	// Tool 1: arguments.done closes only its own block.
	// ContentBlockIndex goes from 0 → 1
	eventsTool1Done := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 0,
		Arguments:   `{"x":1}`,
	}, state)
	// Should only emit content_block_stop (delta already streamed)
	for _, e := range eventsTool1Done {
		assert.NotEqual(t, "content_block_delta", e.Type,
			"tool 1 .done should not re-emit delta (args already streamed)")
	}

	// Tool 2: output_item.added (index 1) → opens block at ContentBlockIndex=1
	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.output_item.added",
		OutputIndex: 1,
		Item:        &ResponsesOutput{Type: "function_call", CallID: "call_b", Name: "tool_b"},
	}, state)

	// Tool 2: NO delta — arguments arrive packed in .done only.
	// This is the exact scenario that triggered the ghost delta bug.
	eventsTool2Done := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:        "response.function_call_arguments.done",
		OutputIndex: 1,
		Arguments:   `{"y":2}`,
	}, state)

	// The fix ensures the delta targets index 1 (the tool 2 block), not
	// state.ContentBlockIndex (which would be 1 before close, matching by
	// luck in this 2-tool case, but wrong in 3+ tool cases or when the
	// block is already closed).
	//
	// The critical assertion: the delta must be on index 1 and must be
	// followed by content_block_stop on the same index.
	var sawDelta, sawStop bool
	var deltaIndex, stopIndex int
	for _, e := range eventsTool2Done {
		if e.Type == "content_block_delta" && e.Index != nil {
			sawDelta = true
			deltaIndex = *e.Index
			assert.Equal(t, "input_json_delta", e.Delta.Type)
			assert.Equal(t, `{"y":2}`, e.Delta.PartialJSON)
		}
		if e.Type == "content_block_stop" && e.Index != nil {
			sawStop = true
			stopIndex = *e.Index
		}
	}
	assert.True(t, sawDelta, "tool 2 .done with packed args should emit content_block_delta")
	assert.True(t, sawStop, "tool 2 .done should close the block")
	assert.Equal(t, 1, deltaIndex, "delta must target the tool 2 block (index 1)")
	assert.Equal(t, deltaIndex, stopIndex, "delta and stop must be on the same block")
}

// TestStreamingThreeParallelToolsAllPackedDone tests the most extreme case:
// three parallel tools, ALL with arguments packed in .done (no deltas at all).
// Before the fix, tool 2 and tool 3 would emit ghost deltas on wrong indices.
func TestStreamingThreeParallelToolsAllPackedDone(t *testing.T) {
	state := NewResponsesEventToAnthropicState()

	ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
		Type:     "response.created",
		Response: &ResponsesResponse{ID: "resp_3par", Model: "glm-5.2"},
	}, state)

	// Open three tool blocks at indices 0, 1, 2.
	for i, name := range []string{"tool_a", "tool_b", "tool_c"} {
		ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
			Type:        "response.output_item.added",
			OutputIndex: i,
			Item:        &ResponsesOutput{Type: "function_call", CallID: "call_" + name, Name: name},
		}, state)
	}

	// Track started blocks.
	started := map[int]bool{0: true, 1: true, 2: true}

	// All three .done events with packed arguments (no prior delta).
	for i, args := range []string{`{"a":1}`, `{"b":2}`, `{"c":3}`} {
		events := ResponsesEventToAnthropicEvents(&ResponsesStreamEvent{
			Type:        "response.function_call_arguments.done",
			OutputIndex: i,
			Arguments:   args,
		}, state)

		for _, e := range events {
			if e.Type == "content_block_delta" && e.Index != nil {
				idx := *e.Index
				require.Truef(t, started[idx],
					"ghost delta: tool %d .done emitted content_block_delta on index %d (never started)", i, idx)
				require.Equal(t, i, idx,
					"tool %d .done delta should target its own block index %d, got %d", i, i, idx)
			}
			if e.Type == "content_block_stop" && e.Index != nil {
				idx := *e.Index
				require.Truef(t, started[idx],
					"ghost stop: tool %d .done emitted content_block_stop on index %d (never started)", i, idx)
			}
		}
	}
}
