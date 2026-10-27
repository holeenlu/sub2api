package service

import (
	"context"
	"errors"
	"fmt"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRejectedFieldConnNeverReplaysUnsafeFrames(t *testing.T) {
	const reject = `{"type":"error","error":{"code":"unsupported_parameter","type":"invalid_request_error","param":"input","message":"Supplied input item IDs require persisted-item lookup that is not supported by rustponsesapi."}}`
	for _, tc := range []struct{ name, prelude, suffix, control string }{
		{name: "output", prelude: `{"type":"response.output_text.delta","delta":"hi"}`},
		{name: "cache_usage", prelude: `{"type":"response.in_progress","response":{"usage":{"input_tokens_details":{"cached_tokens":3}}}}`},
		{name: "error_usage", suffix: `,"usage":{"input_tokens":4}`},
		{name: "tail", suffix: `TAIL`},
		{name: "cancel", control: `{"type":"response.cancel"}`},
		{name: "stored_parent", control: "parent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			inner := newStagedPassthroughConn()
			dialed := false
			c := &openAIWSRejectedFieldConn{ctx: ctx, inner: inner, writeTimeout: time.Second, redial: func(_ context.Context, body []byte) (openaiwsv2.FrameConn, []byte, error) {
				dialed = true
				return nil, nil, errors.New("unexpected replay")
			}}
			defer c.Close()
			request := `{"type":"response.create","input":[{"type":"message","role":"user","id":"m1","content":"hi"}]`
			if tc.control == "parent" {
				request += `,"previous_response_id":"resp_parent"`
			}
			require.NoError(t, c.WriteFrame(ctx, coderws.MessageText, []byte(request+`}`)))
			if tc.control != "" && tc.control != "parent" {
				require.NoError(t, c.WriteFrame(ctx, coderws.MessageText, []byte(tc.control)))
			}
			if tc.prelude != "" {
				inner.Send(tc.prelude)
				_, _, err := c.ReadFrame(ctx)
				require.NoError(t, err)
			}
			event := reject
			if tc.suffix == "TAIL" {
				event += `{"type":"response.completed","response":{"usage":{"input_tokens":8}}}`
			} else if tc.suffix != "" {
				event = event[:len(event)-1] + tc.suffix + `}`
			}
			inner.Send(event)
			_, got, err := c.ReadFrame(ctx)
			require.NoError(t, err)
			require.Equal(t, event, string(got))
			require.False(t, dialed)
		})
	}
}

func TestRejectedFieldConnStagingDeadlineAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	inner := newStagedPassthroughConn()
	c := &openAIWSRejectedFieldConn{ctx: ctx, inner: inner, writeTimeout: time.Second}
	defer c.Close()
	require.NoError(t, c.WriteFrame(ctx, coderws.MessageText, []byte(`{"type":"response.create","input":"hi"}`)))
	inner.Send(`{"type":"response.created","response":{"id":"r1"}}`)
	_, body, err := c.ReadFrame(ctx)
	require.NoError(t, err)
	require.Contains(t, string(body), "r1")
	inner.Send(`{"type":"response.completed","response":{"id":"r1"}}`)
	_, body, err = c.ReadFrame(ctx)
	require.NoError(t, err)
	require.Contains(t, string(body), "response.completed")
	cancel()
	_, _, err = c.ReadFrame(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRejectedFieldConnRetryBoundAndWriteFailure(t *testing.T) {
	for _, writeFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(writeFailure), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			request := `{"type":"response.create","input":[{"role":"user","id":"m1","content":"hi"}]}`
			rejection := `{"type":"error","error":{"code":"unsupported_parameter","type":"invalid_request_error","param":"input","message":"Supplied input item IDs require persisted-item lookup that is not supported by rustponsesapi."}}`
			_, _, repairable, err := normalizeOpenAIResponsesRejectedFieldRetryBody(openAIWSRejectedFieldRetryHTTPStatus([]byte(rejection)), []byte(request), []byte(rejection))
			require.NoError(t, err)
			require.True(t, repairable, "fixture must exercise a repairable rejection")
			first, second := newStagedPassthroughConn(), newStagedPassthroughConn()
			dialed := 0
			c := &openAIWSRejectedFieldConn{ctx: ctx, inner: first, writeTimeout: time.Second, redial: func(_ context.Context, body []byte) (openaiwsv2.FrameConn, []byte, error) {
				dialed++
				return second, body, nil
			}}
			defer c.Close()
			require.NoError(t, c.WriteFrame(ctx, coderws.MessageText, []byte(request)))
			first.Send(rejection)
			if writeFailure {
				require.NoError(t, second.Close())
			} else {
				second.Send(rejection)
			}
			_, body, err := c.ReadFrame(ctx)
			if writeFailure {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, rejection, string(body))
			}
			require.Equal(t, 1, dialed, "an unchanged repair cannot loop")
		})
	}
}
