package service

import (
	"io"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Retains the fixed maintenance branch's actual flush regression.
func TestOpenAIResponseFlush_FirstSemanticProgressDoesNotWaitForText(t *testing.T) {
	prelude := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_progress\"}}\n\n"
	progress := "data: {\"type\":\"response.web_search_call.in_progress\",\"item_id\":\"search_1\"}\n\n"
	text := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n"
	terminal := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_progress\",\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"
	recorder := newOpenAIResponseFlushRecorder()
	result, err := runOpenAIResponseFlushTest(recorder, io.NopCloser(strings.NewReader(prelude+progress+text+terminal)), config.GatewayConfig{})
	require.NoError(t, err)
	require.Equal(t, 2, result.usage.InputTokens)
	_, flushes := recorder.snapshot()
	require.NotEmpty(t, flushes)
	require.Contains(t, flushes[0], "web_search_call.in_progress")
	require.NotContains(t, flushes[0], "answer")
}

func TestOpenAIResponseFailedPreservesSafeParameter(t *testing.T) {
	for _, source := range []string{
		`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_value","param":"input[2].id","message":"invalid item"}}`,
		`{"type":"response.failed","response":{"error":{"type":"invalid_request_error","code":"invalid_value","param":"input[2].id","message":"invalid item"}}}`,
	} {
		wire := buildOpenAIResponseFailedSSE("resp_1", "gpt-5.1", 1700000000, []byte(source), "")
		payloads := sseDataPayloads(wire)
		require.Len(t, payloads, 1)
		require.Equal(t, "input[2].id", gjson.Get(payloads[0], "response.error.param").String())
		require.Equal(t, "invalid_value", gjson.Get(payloads[0], "response.error.code").String())
	}
}
