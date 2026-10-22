package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestReadOpenAIWSClientMessage_ControlCloseFrames(t *testing.T) {
	tests := []struct {
		name          string
		timeout       time.Duration
		timeoutStatus coderws.StatusCode
		timeoutReason string
		cancelCause   error
		wantStatus    coderws.StatusCode
		wantReason    string
	}{
		{
			name:          "inter-turn idle sends normal close",
			timeout:       25 * time.Millisecond,
			timeoutStatus: coderws.StatusNormalClosure,
			timeoutReason: "websocket idle timeout",
			wantStatus:    coderws.StatusNormalClosure,
			wantReason:    "websocket idle timeout",
		},
		{
			name:          "first message timeout sends policy close",
			timeout:       25 * time.Millisecond,
			timeoutStatus: coderws.StatusPolicyViolation,
			timeoutReason: "missing first response.create message",
			wantStatus:    coderws.StatusPolicyViolation,
			wantReason:    "missing first response.create message",
		},
		{
			name:        "lease loss sends retry close",
			cancelCause: ErrOpenAIWSIngressLeaseLost,
			wantStatus:  coderws.StatusTryAgainLater,
			wantReason:  "websocket ingress capacity lease lost; please reconnect",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controlCtx, cancelControl := context.WithCancelCause(context.Background())
			defer cancelControl(context.Canceled)
			serverResult := make(chan error, 1)
			readStarted := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					serverResult <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				close(readStarted)
				_, _, err = ReadOpenAIWSClientMessage(
					controlCtx,
					conn,
					tt.timeout,
					tt.timeoutStatus,
					tt.timeoutReason,
				)
				serverResult <- err
			}))
			defer server.Close()

			dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
			clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			cancelDial()
			require.NoError(t, err)
			defer func() { _ = clientConn.CloseNow() }()
			<-readStarted
			if tt.cancelCause != nil {
				cancelControl(tt.cancelCause)
			}

			readCtx, cancelRead := context.WithTimeout(context.Background(), time.Second)
			_, _, err = clientConn.Read(readCtx)
			cancelRead()
			var clientClose coderws.CloseError
			require.ErrorAs(t, err, &clientClose)
			require.Equal(t, tt.wantStatus, clientClose.Code)
			require.Equal(t, tt.wantReason, clientClose.Reason)

			select {
			case serverErr := <-serverResult:
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, serverErr, &closeErr)
				require.Equal(t, tt.wantStatus, closeErr.StatusCode())
				require.Equal(t, tt.wantReason, closeErr.Reason())
			case <-time.After(time.Second):
				t.Fatal("server read goroutine did not exit after close handshake")
			}
		})
	}
}

func TestReadOpenAIWSClientMessage_ParentCancellationStillJoinsRead(t *testing.T) {
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	serverResult := make(chan error, 1)
	readStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverResult <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		close(readStarted)
		_, _, err = ReadOpenAIWSClientMessage(controlCtx, conn, 0, 0, "")
		serverResult <- err
	}))
	defer server.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
	clientConn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = clientConn.CloseNow() }()
	<-readStarted
	cancelControl(errors.New("server shutting down"))
	readCtx, cancelRead := context.WithTimeout(context.Background(), time.Second)
	_, _, err = clientConn.Read(readCtx)
	cancelRead()
	var clientClose coderws.CloseError
	require.ErrorAs(t, err, &clientClose)
	require.Equal(t, coderws.StatusGoingAway, clientClose.Code)
	require.Equal(t, "websocket request canceled", clientClose.Reason)

	select {
	case <-serverResult:
	case <-time.After(time.Second):
		t.Fatal("server read goroutine leaked after parent cancellation")
	}
}

// Deliberately ignores close until CloseNow: exercises the bounded close path.
type queuedWSDownstreamTestConn struct {
	writes      chan []byte
	onWrite     func([]byte)
	input       chan []byte
	closed      chan struct{}
	closeOnce   sync.Once
	closeStatus chan coderws.StatusCode
}

func (c *queuedWSDownstreamTestConn) Read(ctx context.Context) (coderws.MessageType, []byte, error) {
	select {
	case b := <-c.input:
		return coderws.MessageText, b, nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-c.closed:
		return 0, nil, io.EOF
	}
}
func (c *queuedWSDownstreamTestConn) Write(_ context.Context, _ coderws.MessageType, payload []byte) error {
	if c.onWrite != nil {
		c.onWrite(payload)
	}
	if c.writes != nil {
		c.writes <- append([]byte(nil), payload...)
	}
	return nil
}
func (c *queuedWSDownstreamTestConn) Close(status coderws.StatusCode, _ string) error {
	c.closeStatus <- status
	<-c.closed
	return nil
}
func (c *queuedWSDownstreamTestConn) CloseNow() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}
func newQueuedWSDownstreamTestConn() *queuedWSDownstreamTestConn {
	return &queuedWSDownstreamTestConn{input: make(chan []byte, 16), closed: make(chan struct{}), closeStatus: make(chan coderws.StatusCode, 1)}
}

func TestOpenAIWSDownstreamBackpressureAndBoundedClose(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	for i := 0; i < 12; i++ {
		conn.input <- []byte(fmt.Sprintf(`{"type":"response.create","model":"m","input":"%d"}`, i))
	}
	require.Eventually(t, func() bool { return len(ds.frames) == openAIWSClientQueueLimit }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for i := 0; i < 12; i++ {
		_, b, err := ds.Read(ctx)
		require.NoError(t, err)
		require.Contains(t, string(b), fmt.Sprintf(`"input":"%d"`, i))
	}
	require.False(t, ds.ClientClosed(), "queue pressure applies backpressure instead of disconnecting")
	start := time.Now()
	require.NoError(t, ds.Close(coderws.StatusGoingAway, "preempted"))
	require.Less(t, time.Since(start), time.Second)
	require.Equal(t, coderws.StatusGoingAway, <-conn.closeStatus)
}

func TestOpenAIWSDownstreamEarlyCancelPassesThrough(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	conn.input <- []byte(`{"type":"response.create"}`)
	conn.input <- []byte(`{"type":"response.cancel","event_id":"early"}`)
	require.Eventually(t, func() bool { return len(ds.controls) == 1 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := ds.Read(ctx)
	require.NoError(t, err)
	ds.controlsEnabled.Store(false)
	ds.modeSelected.Store(true)
	_, b, err := ds.Read(ctx)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"response.cancel","event_id":"early"}`, string(b))
}

func TestOpenAIWSDownstreamRetryStopsRefusalAndCommittedTurn(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	ctx := ds.TurnBudgetContext(t.Context(), nil, []byte(`{"type":"response.create","model":"gpt-5.1","input":"hello"}`))
	allowed, reason := ds.CanRetry(1, []byte(`{"type":"response.failed","response":{"error":{"code":"content_filter"}}}`))
	require.False(t, allowed)
	require.Equal(t, OpenAIRetryStopContentRefusal, reason)
	MarkTurnOutputCommitted(ctx)
	allowed, reason = ds.CanRetry(1, nil)
	require.False(t, allowed)
	require.Equal(t, OpenAIRetryStopDownstreamCommitted, reason)
	conn.input <- []byte(`{"type":"response.create","input":"next turn"}`)
	next, err := ds.FinishTurnAndReadNext(ctx, nil, "upstream_error", "failed turn")
	require.NoError(t, err)
	ds.TurnBudgetContext(ctx, nil, next)
	allowed, reason = ds.CanRetry(1, nil)
	require.True(t, allowed)
	require.Empty(t, reason)
}

func TestOpenAIWSDownstreamUsageDrainDeadline(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	ctx, finish := ds.turnContext(context.Background())
	defer finish()
	require.NoError(t, conn.CloseNow())
	select {
	case <-ctx.Done():
		require.ErrorIs(t, context.Cause(ctx), errOpenAIWSUsageDrainExpired)
	case <-time.After(2 * time.Second):
		t.Fatal("drain is not bounded")
	}
}

func TestOpenAIWSDownstreamDoesNotConsumeQueuedCreateAfterDisconnect(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	conn.input <- []byte(`{"type":"response.create","input":"queued"}`)
	require.Eventually(t, func() bool { return len(ds.frames) == 1 }, time.Second, time.Millisecond)
	require.NoError(t, conn.CloseNow())
	<-ds.done
	_, payload, err := ds.Read(context.Background())
	require.Error(t, err)
	require.Empty(t, payload)
}

func TestOpenAIWSInterruptAcknowledgementAndTurnIsolation(t *testing.T) {
	for _, scenario := range []string{"active", "idle", "foreign response", "foreign lane", "ack races next turn", "passthrough", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			conn := newQueuedWSDownstreamTestConn()
			conn.writes = make(chan []byte, 16)
			var ds *OpenAIWSDownstream
			if scenario == "ack races next turn" {
				conn.onWrite = func([]byte) { ds.turn.Add(1) }
			}
			ds = NewOpenAIWSDownstream(conn, 4096)
			defer ds.CloseNow()
			ds.turn.Store(7)
			ds.active.Store(scenario != "idle")
			ds.modeSelected.Store(true)
			current, lane := "resp_active", "lane_a"
			ds.currentResponseID.Store(&current)
			ds.streamID.Store(&lane)
			if scenario == "passthrough" {
				ds.controlsEnabled.Store(false)
			}
			payload := `{"type":"response.interrupt","previous_response_id":"resp_active","mode":"stop","event_id":"stop-1","stream_id":"lane_a"}`
			switch scenario {
			case "foreign response":
				payload = strings.ReplaceAll(payload, "resp_active", "resp_other")
			case "foreign lane":
				payload = strings.ReplaceAll(payload, "lane_a", "lane_b")
			case "invalid":
				payload = `{"type":"response.interrupt","response_id":true}`
			}
			conn.input <- []byte(payload)
			select {
			case ack := <-conn.writes:
				if scenario == "invalid" {
					require.Equal(t, "error", gjson.GetBytes(ack, "type").String())
				} else {
					require.Equal(t, "response.interrupt.accepted", gjson.GetBytes(ack, "type").String())
					require.Equal(t, "stop-1", gjson.GetBytes(ack, "event_id").String())
					require.Equal(t, "stop", gjson.GetBytes(ack, "mode").String())
				}
			case <-time.After(time.Second):
				t.Fatal("interrupt was not acknowledged")
			}
			// FIFO barrier proves the preceding control has been processed.
			conn.input <- []byte(`{"type":"barrier"}`)
			select {
			case <-ds.frames:
			case <-time.After(time.Second):
				t.Fatal("reader stalled")
			}
			if scenario == "active" || scenario == "passthrough" {
				require.Len(t, ds.controls, 1)
				if scenario == "passthrough" {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					_, wire, err := ds.Read(ctx)
					require.NoError(t, err)
					require.Equal(t, "response.cancel", gjson.GetBytes(wire, "type").String())
					require.Equal(t, "resp_active", gjson.GetBytes(wire, "response_id").String())
				} else {
					control := <-ds.controls
					require.True(t, control.interrupt)
					require.Equal(t, uint64(7), control.turn)
				}
			} else {
				require.Empty(t, ds.controls, "an obsolete interrupt must not cancel another turn")
			}
			require.Equal(t, uint64(0), ds.terminalTurn.Load(), "ACK/error must not commit output or end a generation")
		})
	}
	for _, raw := range []string{`{"type":"response.interrupt","type":"response.create"}`, `{"type":"response.interrupt","event_id":"a","event_id":"b"}`} {
		_, _, _, err := normalizeOpenAIWSInterrupt([]byte(raw))
		require.Error(t, err)
	}
}
