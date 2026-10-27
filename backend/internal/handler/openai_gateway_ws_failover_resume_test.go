package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIResponsesWebSocket_MissingContinuationResendsFullHistory(t *testing.T) {
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		ingressMode: service.OpenAIWSIngressModeCtxPool, firstTurnContinuationError: true,
		firstAttemptErrorCode: "previous_response_not_found",
		firstPayload:          `{"type":"response.create","event_id":"missing","model":"gpt-5.1","store":false,"previous_response_id":"resp_old_socket","input":[{"role":"user","content":"continue"}]}`,
		secondPayload:         `{"type":"response.create","event_id":"restored","model":"gpt-5.1","store":false,"input":[{"role":"user","content":"original question"},{"role":"assistant","content":"original answer"},{"role":"user","content":"continue"}]}`,
	})
	require.Len(t, got.clientEvents, 2)
	rejected := got.clientEvents[0]
	require.Equal(t, int64(400), gjson.GetBytes(rejected, "status").Int())
	require.Equal(t, "previous_response_not_found", gjson.GetBytes(rejected, "error.code").String())
	require.Equal(t, "previous_response_id", gjson.GetBytes(rejected, "error.param").String())
	require.Equal(t, "missing", gjson.GetBytes(rejected, "event_id").String())
	require.Equal(t, "response.completed", gjson.GetBytes(got.clientEvents[1], "type").String())
	require.Len(t, got.upstreamPayloads, 1)
	require.False(t, gjson.GetBytes(got.upstreamFirstPayload, "previous_response_id").Exists())
	require.Contains(t, string(got.upstreamFirstPayload), "original answer")
	require.Len(t, got.logs, 1)
}

func TestOpenAIWSNextAttemptMessageUsesCurrentTurnPayload(t *testing.T) {
	firstMessage := []byte(`{"type":"response.create","input":"first"}`)
	currentTurn := []byte(`{"type":"response.create","input":"turn-281"}`)

	next, ok := openAIWSNextAttemptMessage(firstMessage, currentTurn, true)

	require.True(t, ok)
	require.Equal(t, currentTurn, next)
	next[0] = 'x'
	require.Equal(t, byte('{'), currentTurn[0], "retry payload must be cloned")
}

func TestOpenAIWSNextAttemptMessageRejectsMissingCurrentTurnPayload(t *testing.T) {
	next, ok := openAIWSNextAttemptMessage([]byte(`{"type":"response.create"}`), nil, true)

	require.False(t, ok)
	require.Nil(t, next)
}

func TestOpenAIWSNextAttemptMessageKeepsInitialMessageForFirstTurnFailover(t *testing.T) {
	firstMessage := []byte(`{"type":"response.create","input":"first"}`)

	next, ok := openAIWSNextAttemptMessage(firstMessage, nil, false)

	require.True(t, ok)
	require.Equal(t, firstMessage, next)
}
