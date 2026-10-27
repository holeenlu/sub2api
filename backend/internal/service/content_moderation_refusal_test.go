package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsUpstreamContentRefusal(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"content_policy_violation"}}`,
		`{"error":{"type":"safety_error"}}`,
		`{"error":{"code":"content_policy_error"}}`,
		`{"error":{"type":"usage_policy"}}`,
		`{"type":"error","code":"cyber_policy"}`,
		`{"stop_reason":"refusal"}`,
		`{"type":"message_delta","delta":{"stop_reason":"refusal"}}`,
		`{"content":[{"type":"refusal","refusal":""}]}`,
		`{"type":"content_block_start","content_block":{"type":"refusal"}}`,
		`{"choices":[{"finish_reason":"content_filter"}]}`,
		`{"choices":[{"message":{"refusal":"declined"}}]}`,
		`{"choices":[{"delta":{"refusal":"declined"}}]}`,
		`{"response":{"output":[{"type":"message","content":[{"type":"refusal"}]}]}}`,
		`{"incomplete_details":{"reason":"content_filter"}}`,
		`{"type":"response.refusal.delta","delta":""}`,
		`{"type":"response.content_part.done","part":{"type":"refusal"}}`,
		`{"type":"response.output_item.done","item":{"type":"message","content":[{"type":"refusal"}]}}`,
		`{"response":{"error":{"code":"moderation_blocked"}}}`,
		`{"candidates":[{"finishReason":"SAFETY"}]}`,
		`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`,
	} {
		require.True(t, IsUpstreamContentRefusal([]byte(body)), body)
	}
	for _, body := range []string{
		``, `[DONE]`, `data: {"stop_reason":"refusal"}`, `not JSON`, `[]`,
		`{"error":{"message":"content_policy usage_policy refusal"}}`,
		`{"error":{"type":"permission_error","code":"invalid_api_key"}}`,
		`{"code":"content_policy_violation"}`,
		`{"stop_reason":"end_turn"}`,
		`{"choices":[{"finish_reason":"stop","message":{"content":"I refuse: content_filter","refusal":null}}]}`,
		`{"choices":[{"message":{"refusal":""}}]}`,
		`{"output":[{"type":"function_call","arguments":"{\"error\":{\"code\":\"content_policy_violation\"}}"}]}`,
		`{"input":{"stop_reason":"refusal"}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","content":[{"type":"refusal"}]}}`,
		`{"content":[{"type":"text","text":"refusal"}]}`,
		`{"response":{"incomplete_details":{"reason":"max_output_tokens"}}}`,
	} {
		require.False(t, IsUpstreamContentRefusal([]byte(body)), body)
	}
}
