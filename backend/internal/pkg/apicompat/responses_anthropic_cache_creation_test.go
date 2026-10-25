package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAICacheWriteLifetimesRoundTrip(t *testing.T) {
	var usage ResponsesUsage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":12000,"output_tokens":5,"input_tokens_details":{"cached_tokens":4000,"cache_write_tokens":7000,"cache_write_tokens_details":{"ttl_30m_tokens":3000,"ttl_12h_tokens":4000}}}`), &usage))
	require.Equal(t, 7000, usage.CacheCreationInputTokens)
	chat := chatUsageFromResponsesUsage(&usage)
	require.Equal(t, &CacheWriteTokensDetails{TTL30mTokens: 3000, TTL12hTokens: 4000}, chat.PromptTokensDetails.CacheWriteTokensDetails)
	back := ChatUsageToResponsesUsage(chat)
	require.Equal(t, usage.InputTokensDetails, back.InputTokensDetails)
	require.Equal(t, usage.CacheCreationInputTokens, back.CacheCreationInputTokens)
	// The two write components never become extra input tokens.
	require.Equal(t, 1000, anthropicUsageFromResponsesUsage(back).InputTokens)
}

func TestOpenAICacheWriteLifetimesNormalize(t *testing.T) {
	previous := CacheWriteTokensDetails{TTL30mTokens: 100, TTL12hTokens: 100}
	require.Equal(t, previous, previous.MergeNonZero(CacheWriteTokensDetails{}, 200))
	require.Equal(t, CacheWriteTokensDetails{TTL30mTokens: 120, TTL12hTokens: 100}, previous.MergeNonZero(CacheWriteTokensDetails{TTL30mTokens: 120}, 220))
	require.Equal(t, CacheWriteTokensDetails{TTL30mTokens: 50, TTL12hTokens: 50}, previous.MergeNonZero(CacheWriteTokensDetails{}, 100))
	require.Equal(t, CacheWriteTokensDetails{}, previous.MergeNonZero(CacheWriteTokensDetails{}, 0))
	for _, tc := range []struct {
		name                                    string
		total, short, long, wantShort, wantLong int
	}{
		{"mixed", 7000, 3000, 4000, 3000, 4000},
		{"partial details keep remainder unknown", 7000, 1000, 2000, 1000, 2000},
		{"aggregate zero is authoritative", 0, 3000, 4000, 0, 0},
		{"negative counters", 100, -3, 40, 0, 40},
		{"contradictory components", 100, 100, 100, 50, 50},
		{"overflow safe", 100, int(^uint(0) >> 1), int(^uint(0) >> 1), 50, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (CacheWriteTokensDetails{TTL30mTokens: tc.short, TTL12hTokens: tc.long}).Normalize(tc.total)
			require.Equal(t, CacheWriteTokensDetails{TTL30mTokens: tc.wantShort, TTL12hTokens: tc.wantLong}, got)
		})
	}
}

func TestAnthropicUsageFromResponsesUsage_CacheCreation(t *testing.T) {
	usage := &ResponsesUsage{
		InputTokens:              20,
		OutputTokens:             5,
		CacheCreationInputTokens: 6,
		InputTokensDetails: &ResponsesInputTokensDetails{
			CachedTokens: 4,
		},
	}

	got := anthropicUsageFromResponsesUsage(usage)

	assert.Equal(t, 10, got.InputTokens, "input = total(20) - cache_read(4) - cache_creation(6)")
	assert.Equal(t, 5, got.OutputTokens)
	assert.Equal(t, 4, got.CacheReadInputTokens)
	assert.Equal(t, 6, got.CacheCreationInputTokens, "cache creation must be preserved")
}

func TestAnthropicUsageFromResponsesUsage_NoCacheCreation(t *testing.T) {
	usage := &ResponsesUsage{
		InputTokens:  10,
		OutputTokens: 5,
		InputTokensDetails: &ResponsesInputTokensDetails{
			CachedTokens: 3,
		},
	}

	got := anthropicUsageFromResponsesUsage(usage)

	assert.Equal(t, 7, got.InputTokens)
	assert.Equal(t, 3, got.CacheReadInputTokens)
	assert.Equal(t, 0, got.CacheCreationInputTokens)
}

func TestResponsesEventToAnthropicEvents_StreamingCacheCreation(t *testing.T) {
	state := NewResponsesEventToAnthropicState()
	state.MessageStartSent = true

	completedEvt := &ResponsesStreamEvent{
		Type: "response.completed",
		Response: &ResponsesResponse{
			Status: "completed",
			Usage: &ResponsesUsage{
				InputTokens:              20,
				OutputTokens:             5,
				CacheCreationInputTokens: 6,
				InputTokensDetails: &ResponsesInputTokensDetails{
					CachedTokens: 4,
				},
			},
		},
	}

	events := ResponsesEventToAnthropicEvents(completedEvt, state)

	var deltaEvt *AnthropicStreamEvent
	for i := range events {
		if events[i].Type == "message_delta" {
			deltaEvt = &events[i]
			break
		}
	}
	require.NotNil(t, deltaEvt, "should have message_delta event")
	require.NotNil(t, deltaEvt.Usage)
	assert.Equal(t, 6, deltaEvt.Usage.CacheCreationInputTokens, "streaming cache_creation must be preserved")
	assert.Equal(t, 10, deltaEvt.Usage.InputTokens, "input = 20 - 4(read) - 6(creation)")
	assert.Equal(t, 4, deltaEvt.Usage.CacheReadInputTokens)
}

func TestAnthropicToResponsesResponse_CacheCreation(t *testing.T) {
	resp := AnthropicResponse{
		ID:    "msg_test",
		Type:  "message",
		Role:  "assistant",
		Model: "claude-opus-4-6",
		Usage: AnthropicUsage{
			InputTokens:              10,
			OutputTokens:             5,
			CacheReadInputTokens:     4,
			CacheCreationInputTokens: 6,
		},
		StopReason: AnthropicStopReasonPtr("end_turn"),
	}

	out := AnthropicToResponsesResponse(&resp)

	require.NotNil(t, out.Usage)
	assert.Equal(t, 20, out.Usage.InputTokens, "total = input(10) + cache_read(4) + cache_creation(6)")
	assert.Equal(t, 6, out.Usage.CacheCreationInputTokens, "cache creation must round-trip")
}
