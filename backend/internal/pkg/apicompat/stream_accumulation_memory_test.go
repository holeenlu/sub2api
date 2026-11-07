package apicompat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type streamAccumulationFieldCase struct {
	name      string
	blockType string
	field     func(*AnthropicEventToResponsesState) *string
	delta     func(string) *AnthropicDelta
}

func streamAccumulationFieldCases() []streamAccumulationFieldCase {
	return []streamAccumulationFieldCase{
		{"text", "text", func(s *AnthropicEventToResponsesState) *string { return &s.TextAccum }, func(v string) *AnthropicDelta { return &AnthropicDelta{Type: "text_delta", Text: v} }},
		{"thinking", "thinking", func(s *AnthropicEventToResponsesState) *string { return &s.CurrentSummary }, func(v string) *AnthropicDelta { return &AnthropicDelta{Type: "thinking_delta", Thinking: v} }},
		{"arguments", "tool_use", func(s *AnthropicEventToResponsesState) *string { return &s.CurrentArgs }, func(v string) *AnthropicDelta { return &AnthropicDelta{Type: "input_json_delta", PartialJSON: v} }},
		{"signature", "thinking", func(s *AnthropicEventToResponsesState) *string { return &s.CurrentThinking.Signature }, func(v string) *AnthropicDelta { return &AnthropicDelta{Type: "signature_delta", Signature: v} }},
	}
}

func TestStreamAccumulationAnthropicPublicStringsAndReset(t *testing.T) {
	for _, tc := range streamAccumulationFieldCases() {
		t.Run(tc.name, func(t *testing.T) {
			state := NewAnthropicEventToResponsesState()
			state.PreserveThinkingSignatures = true
			block := &AnthropicContentBlock{Type: tc.blockType, ID: "call_seed", Name: "tool"}
			if tc.name == "thinking" {
				block.Thinking = "seed-"
			}
			if tc.name == "signature" {
				block.Signature = "seed-"
			}
			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_start", ContentBlock: block}, state)
			if tc.name == "thinking" || tc.name == "signature" {
				require.Equal(t, "seed-", *tc.field(state), "content block seeds remain visible")
			} else {
				*tc.field(state) = "seed-"
			}
			appendDelta := func(value string) {
				AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_delta", Delta: tc.delta(value)}, state)
			}

			appendDelta("one")
			require.Equal(t, "seed-one", *tc.field(state))
			firstSnapshot := *tc.field(state)
			appendDelta("-two")
			require.Equal(t, "seed-one-two", *tc.field(state))
			require.Equal(t, "seed-one", firstSnapshot, "later deltas must not mutate borrowed strings")
			secondSnapshot := *tc.field(state)

			// An external assignment with the same length must replace the old
			// accumulator, not be mistaken for a synchronized public field.
			replacement := strings.Repeat("r", len(secondSnapshot))
			*tc.field(state) = replacement
			appendDelta("!")
			require.Equal(t, replacement+"!", *tc.field(state))
			require.Equal(t, "seed-one-two", secondSnapshot)

			*tc.field(state) = ""
			appendDelta("after-reset")
			require.Equal(t, "after-reset", *tc.field(state))
			closedSnapshot := *tc.field(state)
			closedEvents := AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop"}, state)
			require.Empty(t, *tc.field(state))

			AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_start", ContentBlock: &AnthropicContentBlock{Type: tc.blockType, ID: "call_next", Name: "tool"}}, state)
			appendDelta(strings.Repeat("next", 128))
			require.Equal(t, strings.Repeat("next", 128), *tc.field(state), "a new block must not reuse the previous prefix")
			require.Equal(t, "after-reset", closedSnapshot)
			require.Equal(t, "seed-one", firstSnapshot)
			if tc.name == "text" {
				require.Len(t, closedEvents, 2)
				require.Equal(t, "after-reset", closedEvents[0].Text)
				require.Equal(t, "after-reset", closedEvents[1].Part.Text)
			}
		})
	}
}

func TestStreamAccumulationAnthropicCopiedStateBranches(t *testing.T) {
	for _, tc := range streamAccumulationFieldCases() {
		t.Run(tc.name, func(t *testing.T) {
			original := NewAnthropicEventToResponsesState()
			original.PreserveThinkingSignatures = true
			appendDelta := func(state *AnthropicEventToResponsesState, value string) {
				AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_delta", Delta: tc.delta(value)}, state)
			}
			appendDelta(original, "base")
			baseSnapshot := *tc.field(original)
			copied := *original

			// Sequential copies historically had independent string values.
			// This does not promise concurrent mutation of shared state is safe.
			appendDelta(original, "/left")
			appendDelta(&copied, "/right")
			require.Equal(t, "base/left", *tc.field(original))
			require.Equal(t, "base/right", *tc.field(&copied))
			appendDelta(original, "/again")
			require.Equal(t, "base/left/again", *tc.field(original))
			require.Equal(t, "base/right", *tc.field(&copied))
			require.Equal(t, "base", baseSnapshot)
		})
	}
}

func streamAccumulationChatToolChunk(idx int, name, arguments string) *ChatCompletionsChunk {
	return &ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{ToolCalls: []ChatToolCall{{Index: &idx, ID: fmt.Sprintf("call_%d", idx), Type: "function", Function: ChatFunctionCall{Name: name, Arguments: arguments}}}}}}}
}

func TestStreamAccumulationChatResponsesToolIsolation(t *testing.T) {
	state := NewChatCompletionsToResponsesStreamState("model")
	appendDelta := func(idx int, name, delta, want string) {
		t.Helper()
		ChatCompletionsChunkToResponsesEvents(streamAccumulationChatToolChunk(idx, name, delta), state)
		require.Equal(t, want, state.ToolCalls[idx].Function.Arguments)
	}
	appendDelta(0, "first", `{"a":`, `{"a":`)
	firstSnapshot := state.ToolCalls[0].Function.Arguments
	appendDelta(1, "second", `{"b":`, `{"b":`)
	appendDelta(0, "", "1}", `{"a":1}`)
	appendDelta(1, "", "2}", `{"b":2}`)
	require.Equal(t, `{"a":`, firstSnapshot)
	require.Equal(t, `{"a":1}`, state.ToolCalls[0].Function.Arguments)

	state.ToolCalls[0].Function.Arguments = `{"c":3`
	appendDelta(0, "", "}", `{"c":3}`)
	state.ToolCalls[0].Function.Arguments = ""
	appendDelta(0, "", "{}", "{}")
	oldCall := state.ToolCalls[0]
	delete(state.ToolCalls, 0)
	appendDelta(0, "replacement", `{"new":`, `{"new":`)
	appendDelta(0, "", "4}", `{"new":4}`)
	require.Equal(t, "{}", oldCall.Function.Arguments, "replacing an indexed call must leave the old call immutable")
	require.Equal(t, `{"b":2}`, state.ToolCalls[1].Function.Arguments)
	require.NoError(t, state.ValidateToolCallArguments())
}

func BenchmarkStreamAccumulationAnthropic64ByteDeltas(b *testing.B) {
	for _, tc := range streamAccumulationFieldCases() {
		for _, size := range []int{32 << 10, 128 << 10, 512 << 10} {
			b.Run(fmt.Sprintf("%s/%dKiB", tc.name, size>>10), func(b *testing.B) {
				payload := strings.Repeat("x", 64)
				event := &AnthropicStreamEvent{Type: "content_block_delta", Delta: tc.delta(payload)}
				b.ReportAllocs()
				b.SetBytes(int64(size))
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					state := NewAnthropicEventToResponsesState()
					state.PreserveThinkingSignatures = true
					for received := 0; received < size; received += len(payload) {
						AnthropicEventToResponsesEvents(event, state)
					}
					streamAccumulationBenchmarkSink = *tc.field(state)
					if len(streamAccumulationBenchmarkSink) != size {
						b.Fatalf("accumulated %d bytes, want %d", len(streamAccumulationBenchmarkSink), size)
					}
				}
			})
		}
	}
}

// Read's existing complete-JSON gate inspects the accumulated arguments on
// every delta. Keep the file_path string unfinished to measure that independent
// cost without changing when the converter emits sanitized tool input.

var streamAccumulationBenchmarkSink string
