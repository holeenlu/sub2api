package apicompat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const refusalText = "I can't help with that."

func decodeResponsesEvent(t *testing.T, raw string) ResponsesStreamEvent {
	t.Helper()
	var evt ResponsesStreamEvent
	require.NoError(t, json.Unmarshal([]byte(raw), &evt))
	return evt
}

func chatMessageJSON(t *testing.T, resp *ChatCompletionsResponse) map[string]any {
	t.Helper()
	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	var decoded struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.Choices, 1)
	return decoded.Choices[0].Message
}

// A refusal arrives as a "refusal" content part, not output_text. Dropping it
// leaves the Chat client with an empty assistant message.
func TestResponsesToChatCompletions_RefusalPreserved(t *testing.T) {
	var resp ResponsesResponse
	require.NoError(t, json.Unmarshal([]byte(`{
		"id":"resp_refusal",
		"status":"completed",
		"output":[{"type":"message","role":"assistant","content":[{"type":"refusal","refusal":"`+refusalText+`"}]}]
	}`), &resp))

	chat := ResponsesToChatCompletions(&resp, "gpt-4o")

	msg := chatMessageJSON(t, chat)
	require.Equal(t, refusalText, msg["refusal"])
	require.NotContains(t, msg, "content")
	require.Equal(t, "stop", chat.Choices[0].FinishReason)
}

func TestResponsesEventToChatChunks_RefusalDelta(t *testing.T) {
	state := NewResponsesEventToChatState()
	events := []string{
		`{"type":"response.created","response":{"id":"resp_refusal","model":"gpt-4o"}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"I can't "}`,
		`{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"help with that."}`,
		`{"type":"response.refusal.done","output_index":0,"content_index":0,"refusal":"` + refusalText + `"}`,
		`{"type":"response.completed","response":{"id":"resp_refusal","status":"completed"}}`,
	}

	var refusal strings.Builder
	var finishReason string
	for _, raw := range events {
		evt := decodeResponsesEvent(t, raw)
		for _, chunk := range ResponsesEventToChatChunks(&evt, state) {
			encoded, err := json.Marshal(chunk)
			require.NoError(t, err)
			var decoded struct {
				Choices []struct {
					Delta struct {
						Refusal *string `json:"refusal"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			for _, choice := range decoded.Choices {
				if choice.Delta.Refusal != nil {
					_, _ = refusal.WriteString(*choice.Delta.Refusal)
				}
				if choice.FinishReason != nil {
					finishReason = *choice.FinishReason
				}
			}
		}
	}

	require.Equal(t, refusalText, refusal.String())
	require.Equal(t, "stop", finishReason)
}

// Non-streaming Chat requests are served from a forced upstream stream; when
// the terminal event carries an empty output array the message is rebuilt from
// the accumulated deltas, which must include refusal deltas.
func TestBufferedResponseAccumulator_RefusalRebuiltFromDeltas(t *testing.T) {
	acc := NewBufferedResponseAccumulator()
	for _, raw := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"I can't "}`,
		`{"type":"response.refusal.delta","output_index":0,"content_index":0,"delta":"help with that."}`,
	} {
		evt := decodeResponsesEvent(t, raw)
		acc.ProcessEvent(&evt)
	}
	require.True(t, acc.HasContent())

	final := &ResponsesResponse{ID: "resp_refusal", Status: "completed"}
	acc.SupplementResponseOutput(final)
	chat := ResponsesToChatCompletions(final, "gpt-4o")

	require.Equal(t, refusalText, chatMessageJSON(t, chat)["refusal"])
}

func TestRefusal_RoundTripsWithoutLosingWireSemantics(t *testing.T) {
	chat := &ChatCompletionsResponse{Choices: []ChatChoice{{Message: ChatMessage{Role: "assistant", Refusal: refusalText}, FinishReason: "stop"}}}
	response := ChatCompletionsResponseToResponses(chat, "example", nil, nil, false, nil)
	require.Equal(t, refusalText, ResponsesToChatCompletions(response, "example").Choices[0].Message.Refusal)
	message := ChatCompletionsResponseToAnthropic(chat, "example")
	require.Equal(t, "refusal", AnthropicStopReasonString(message.StopReason))
	require.Equal(t, refusalText, ResponsesToChatCompletions(AnthropicToResponsesResponse(message), "example").Choices[0].Message.Refusal)

	state := NewChatCompletionsToResponsesStreamState("example")
	var events []ResponsesStreamEvent
	for _, fragment := range []string{"I can't ", "help with that."} {
		events = append(events, ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Refusal: &fragment}}}}, state)...)
	}
	events = append(events, FinalizeChatCompletionsResponsesStream(state)...)
	chatState, anthState := NewResponsesEventToChatState(), NewResponsesEventToAnthropicState()
	var got string
	var blocks []AnthropicStreamEvent
	for _, event := range events {
		// Exercise the actual SSE JSON shape, especially refusal parts at index 0.
		wire, err := json.Marshal(event)
		require.NoError(t, err)
		var decoded ResponsesStreamEvent
		require.NoError(t, json.Unmarshal(wire, &decoded))
		for _, chunk := range ResponsesEventToChatChunks(&decoded, chatState) {
			for _, choice := range chunk.Choices {
				if choice.Delta.Refusal != nil {
					got += *choice.Delta.Refusal
				}
			}
		}
		blocks = append(blocks, ResponsesEventToAnthropicEvents(&decoded, anthState)...)
	}
	require.NoError(t, chatState.Err())
	require.NoError(t, anthState.Err())
	require.Equal(t, refusalText, got)
	require.Equal(t, refusalText, collectAnthropicText(blocks))
	requireAnthropicBlockLifecycle(t, blocks)
	require.Equal(t, "refusal", blocks[len(blocks)-2].Delta.StopReason)
	require.Empty(t, ChatCompletionsChunkToResponsesEvents(&ChatCompletionsChunk{Choices: []ChatChunkChoice{{Delta: ChatDelta{Refusal: strPtr("late")}}}}, state))
}

func TestRefusal_MessagesStreamRetainsUsageAndTerminalSemantics(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	chat := NewResponsesEventToChatState()
	var refusal string
	for _, event := range []AnthropicStreamEvent{
		{Type: "message_start", Message: &AnthropicResponse{ID: "m", Model: "example", Usage: AnthropicUsage{InputTokens: 7, CacheReadInputTokens: 3, CacheCreationInputTokens: 2}}},
		{Type: "content_block_start", Index: intPtr(0), ContentBlock: &AnthropicContentBlock{Type: "text"}},
		{Type: "content_block_delta", Index: intPtr(0), Delta: &AnthropicDelta{Type: "text_delta", Text: refusalText}},
		{Type: "content_block_stop", Index: intPtr(0)},
		{Type: "message_delta", Delta: &AnthropicDelta{StopReason: "refusal"}, Usage: &AnthropicUsage{OutputTokens: 5}},
		{Type: "message_stop"},
	} {
		for _, out := range AnthropicEventToResponsesEvents(&event, state) {
			for _, chunk := range ResponsesEventToChatChunks(&out, chat) {
				for _, choice := range chunk.Choices {
					if choice.Delta.Refusal != nil {
						refusal += *choice.Delta.Refusal
					}
				}
			}
		}
	}
	require.Equal(t, refusalText, refusal)
	require.Equal(t, 12, chat.Usage.PromptTokens)
	require.Equal(t, 5, chat.Usage.CompletionTokens)
	require.Equal(t, 3, chat.Usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 2, chat.Usage.PromptTokensDetails.CacheCreationTokens)
	require.Empty(t, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_delta", Delta: &AnthropicDelta{Type: "text_delta", Text: "late"}}, state))
}
