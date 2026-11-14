package service

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type turnRecoveryBlockedWrite struct {
	*queuedWSDownstreamTestConn
	entered, resume chan struct{}
	calls           atomic.Int32
	failure         error
}

func (c *turnRecoveryBlockedWrite) Write(ctx context.Context, kind coderws.MessageType, body []byte) error {
	if gjson.GetBytes(body, "error.code").String() == "previous_response_not_found" && c.calls.Add(1) == 1 {
		close(c.entered)
		select {
		case <-c.resume:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
		if c.failure != nil {
			return c.failure
		}
	}
	return c.queuedWSDownstreamTestConn.Write(ctx, kind, body)
}

func TestOpenAIWSFinishTurnQueuedCreateKeepsTerminalOwnership(t *testing.T) {
	for _, scenario := range []string{"no_prelude", "with_prelude", "write_failed", "write_canceled"} {
		t.Run(scenario, func(t *testing.T) {
			admission := &openAIWSTestAdmission{}
			conn := &turnRecoveryBlockedWrite{queuedWSDownstreamTestConn: newQueuedWSDownstreamTestConn(), entered: make(chan struct{}), resume: make(chan struct{})}
			conn.writes = make(chan []byte, 8)
			if scenario == "write_failed" {
				conn.failure = io.ErrClosedPipe
			}
			var once sync.Once
			resume := func() { once.Do(func() { close(conn.resume) }) }
			defer resume()
			ds := NewOpenAIWSDownstream(conn, 4096, admission.context(t.Context()))
			defer ds.finishAdmissions()
			defer ds.CloseNow()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			conn.input <- []byte("{\"type\":\"response.create\",\"event_id\":\"first\",\"input\":\"old\"}")
			_, first, err := ds.Read(ctx)
			require.NoError(t, err)
			require.NoError(t, ds.bindRequestEnvelope(first))
			firstTurn := ds.turn.Load()
			require.True(t, ds.active.Load())
			if scenario == "with_prelude" {
				require.NoError(t, ds.Write(ctx, coderws.MessageText, []byte("{\"type\":\"response.created\",\"response\":{\"id\":\"resp_old\"}}")))
				wsLifecycleReceive(t, conn.writes)
			}
			type result struct {
				body []byte
				err  error
			}
			done := make(chan result, 1)
			writeCtx, cancelWrite := context.WithCancel(ctx)
			defer cancelWrite()
			go func() {
				b, e := ds.FinishTurnAndReadNext(writeCtx, nil, "previous_response_not_found", "resend complete history")
				done <- result{b, e}
			}()
			wsLifecycleReceive(t, conn.entered)
			// The sole real readLoop admits the next create while the old writer is
			// held. No direct turn/active mutation manufactures this interleaving.
			conn.input <- []byte("{\"type\":\"response.create\",\"event_id\":\"second\",\"input\":\"complete history\"}")
			require.Eventually(t, func() bool { return ds.turn.Load() == firstTurn+1 }, time.Second, time.Millisecond)
			require.True(t, ds.active.Load())
			require.Equal(t, firstTurn, ds.terminalTurn.Load())
			if scenario == "write_canceled" {
				cancelWrite()
			} else {
				resume()
			}
			out := wsLifecycleReceive(t, done)
			require.Equal(t, firstTurn, ds.terminalTurn.Load(), "old callback must not mark the queued new turn terminal")
			require.True(t, ds.active.Load(), "old callback must not clear the accepted new turn")
			if scenario == "write_failed" || scenario == "write_canceled" {
				require.Error(t, out.err)
				if scenario == "write_failed" {
					require.ErrorIs(t, out.err, io.ErrClosedPipe)
				} else {
					require.ErrorIs(t, out.err, context.Canceled)
				}
				require.NoError(t, ds.CloseNow())
				ds.finishAdmissions()
				waitOpenAIWSAdmissions(t, admission, 2, 2, 0)
				return
			}
			require.NoError(t, out.err)
			require.Equal(t, "second", gjson.GetBytes(out.body, "event_id").String())
			firstError := wsLifecycleReceive(t, conn.writes)
			require.Equal(t, "error", gjson.GetBytes(firstError, "type").String())
			require.EqualValues(t, 400, gjson.GetBytes(firstError, "status").Int())
			require.Equal(t, "invalid_request_error", gjson.GetBytes(firstError, "error.type").String())
			require.Equal(t, "previous_response_id", gjson.GetBytes(firstError, "error.param").String())
			require.Equal(t, "first", gjson.GetBytes(firstError, "event_id").String())
			conn.input <- []byte("{\"type\":\"response.create\",\"event_id\":\"third\",\"input\":\"new full input\"}")
			third, err := ds.FinishTurnAndReadNext(ctx, nil, "previous_response_not_found", "resend complete history")
			require.NoError(t, err)
			require.Equal(t, "third", gjson.GetBytes(third, "event_id").String())
			secondError := wsLifecycleReceive(t, conn.writes)
			require.Equal(t, "second", gjson.GetBytes(secondError, "event_id").String())
			require.EqualValues(t, 2, conn.calls.Load(), "one failure signal for each ended turn")
			require.NoError(t, ds.CloseNow())
			ds.finishAdmissions()
			waitOpenAIWSAdmissions(t, admission, 3, 3, 0)
		})
	}
}
