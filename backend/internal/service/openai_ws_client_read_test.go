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
	"github.com/gin-gonic/gin"
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

func TestOpenAIWSDownstreamTurnTimingIncludesQueuedCreate(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	ds.modeSelected.Store(true)
	ds.controlsEnabled.Store(false) // passthrough also returns control frames to its owner
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	conn.input <- []byte(`{"type":"response.create","model":"gpt-5","generate":false}`)
	require.Eventually(t, func() bool { return len(ds.frames) == 1 }, time.Second, time.Millisecond)
	_, _, err := ds.Read(ctx)
	require.NoError(t, err)
	firstReceived := ds.TurnReceivedAt()
	require.False(t, firstReceived.IsZero())

	// The physical reader accepts a new create while the previous turn still owns
	// the gateway. Its clock must survive that queue instead of starting at Read.
	conn.input <- []byte(`{"type":"response.create","model":"gpt-5","input":"next"}`)
	require.Eventually(t, func() bool { return len(ds.frames) == 1 }, time.Second, time.Millisecond)
	queuedAt := time.Now()
	require.Equal(t, firstReceived, ds.TurnReceivedAt(), "read-ahead must not replace the active turn clock")
	conn.input <- []byte(`{"type":"response.cancel","response_id":"old"}`)
	require.Eventually(t, func() bool { return len(ds.frames) == 2 }, time.Second, time.Millisecond)
	time.Sleep(25 * time.Millisecond)
	_, payload, err := ds.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "response.create", gjson.GetBytes(payload, "type").String())
	secondReceived := ds.TurnReceivedAt()
	require.False(t, secondReceived.After(queuedAt))
	_, _, err = ds.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, secondReceived, ds.TurnReceivedAt(), "control frames must not reset generation timing")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	BeginOpenAIRequestTiming(c, firstReceived)
	BeginOpsStreamTurn(c, 2, secondReceived)
	timing := newOpenAITransportTiming(c, &Account{ID: 1}, "websocket", 1)
	timing.ObserveEvent([]byte(`{"type":"response.output_text.delta","delta":"text"}`))
	metrics := timing.UsageLatencyMetrics()
	require.NotNil(t, metrics.FirstTextMs)
	require.GreaterOrEqual(t, *metrics.FirstTextMs, 25)
	require.Equal(t, secondReceived, timing.root.started)
	frozen := *metrics.FirstTextMs

	// Idle before another create belongs to neither that turn nor the preceding
	// prewarm. Starting the next root cannot mutate an already frozen snapshot.
	idleStarted := time.Now()
	time.Sleep(25 * time.Millisecond)
	conn.input <- []byte(`{"type":"response.create","model":"gpt-5","input":"later"}`)
	_, _, err = ds.Read(ctx)
	require.NoError(t, err)
	require.True(t, ds.TurnReceivedAt().After(idleStarted))
	BeginOpsStreamTurn(c, 3, ds.TurnReceivedAt())
	require.Equal(t, ds.TurnReceivedAt(), openAIRequestTimingFromContext(c.Request.Context()).started)
	require.Equal(t, frozen, *metrics.FirstTextMs)
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

func TestOpenAIWSDownstreamMissingContinuationKeepsRecoveryContract(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprintf("published=%t", published), func(t *testing.T) {
			conn := newQueuedWSDownstreamTestConn()
			conn.writes = make(chan []byte, 2)
			ds := NewOpenAIWSDownstream(conn, 4096)
			defer ds.CloseNow()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			first := []byte(`{"type":"response.create","event_id":"missing","model":"gpt-5.1","previous_response_id":"resp_old","input":"next"}`)
			conn.input <- first
			_, received, err := ds.Read(ctx)
			require.NoError(t, err)
			require.NoError(t, ds.bindRequestEnvelope(received))
			ds.TurnBudgetContext(ctx, nil, received)
			previousTurn := ds.turn.Load()
			if published {
				require.NoError(t, ds.Write(ctx, coderws.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_empty_prelude"}}`)))
				<-conn.writes
				ds.publishedResponseID = "resp_empty_prelude"
				ds.publishedTurnEvents = map[string]bool{"response.created": true}
			}
			full := []byte(`{"type":"response.create","event_id":"restored","model":"gpt-5.1","input":[{"role":"user","content":"full history"}]}`)
			conn.onWrite = func(event []byte) {
				if gjson.GetBytes(event, "type").String() == "error" {
					conn.input <- full
					// Queue the new turn before the old error write returns.
					require.Eventually(t, func() bool { return ds.turn.Load() > previousTurn && len(ds.frames) > 0 }, time.Second, time.Millisecond)
				}
			}
			next, err := ds.FinishTurnAndReadNext(ctx, nil, "previous_response_not_found", "Resend the complete conversation without previous_response_id.")
			require.NoError(t, err)
			require.JSONEq(t, string(full), string(next))
			var event []byte
			select {
			case event = <-conn.writes:
			case <-ctx.Done():
				t.Fatal("missing recovery event")
			}
			require.Equal(t, "error", gjson.GetBytes(event, "type").String())
			require.Equal(t, int64(400), gjson.GetBytes(event, "status").Int())
			require.Equal(t, "invalid_request_error", gjson.GetBytes(event, "error.type").String())
			require.Equal(t, "previous_response_not_found", gjson.GetBytes(event, "error.code").String())
			require.Equal(t, "previous_response_id", gjson.GetBytes(event, "error.param").String())
			require.Equal(t, "missing", gjson.GetBytes(event, "event_id").String())
			require.Less(t, ds.terminalTurn.Load(), ds.turn.Load())
			require.True(t, ds.active.Load())
			require.Empty(t, ds.publishedResponseID)
			require.Empty(t, ds.publishedTurnEvents)
			require.False(t, ds.ClientClosed())
		})
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

func TestOpenAIWSInterruptPreservesPayloadAndTurnIsolation(t *testing.T) {
	for _, scenario := range []string{"active", "idle", "foreign response", "foreign lane", "stale response", "passthrough", "invalid", "missing response"} {
		t.Run(scenario, func(t *testing.T) {
			conn := newQueuedWSDownstreamTestConn()
			conn.writes = make(chan []byte, 16)
			ds := NewOpenAIWSDownstream(conn, 4096)
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
			payload := `{"type":"response.interrupt","response_id":"resp_active","mode":"discard_partial_items","event_id":"stop-1","stream_id":"lane_a"}`
			switch scenario {
			case "foreign response":
				payload = strings.ReplaceAll(payload, "resp_active", "resp_other")
			case "foreign lane":
				payload = strings.ReplaceAll(payload, "lane_a", "lane_b")
			case "stale response":
				ds.lastTerminalID.Store(&current)
			case "invalid":
				payload = `{"type":"response.interrupt","response_id":true}`
			case "missing response":
				payload = `{"type":"response.interrupt","previous_response_id":"resp_active"}`
			}
			conn.input <- []byte(payload)
			// FIFO barrier proves the preceding control has been processed.
			conn.input <- []byte(`{"type":"barrier"}`)
			select {
			case <-ds.frames:
			case <-time.After(time.Second):
				t.Fatal("reader stalled")
			}
			if scenario == "active" || scenario == "passthrough" {
				require.Empty(t, conn.writes, "only the upstream may acknowledge an interrupt")
				require.Len(t, ds.controls, 1)
				if scenario == "passthrough" {
					ctx, cancel := context.WithTimeout(t.Context(), time.Second)
					defer cancel()
					_, wire, err := ds.Read(ctx)
					require.NoError(t, err)
					require.Equal(t, payload, string(wire))
				} else {
					control := <-ds.controls
					require.True(t, control.interrupt)
					require.Equal(t, uint64(7), control.turn)
					require.Equal(t, payload, string(control.payload))
				}
			} else {
				require.Empty(t, ds.controls, "an obsolete interrupt must not cancel another turn")
				require.Len(t, conn.writes, 1)
				failure := <-conn.writes
				require.Equal(t, "response.interrupt.failed", gjson.GetBytes(failure, "type").String())
				require.Equal(t, "response_not_active", gjson.GetBytes(failure, "error.code").String())
			}
			require.Equal(t, uint64(0), ds.terminalTurn.Load(), "control failure must not end a generation")
		})
	}
	for _, raw := range []string{`{"type":"response.interrupt","type":"response.create"}`, `{"type":"response.interrupt","event_id":"a","event_id":"b"}`} {
		_, err := validateOpenAIWSInterrupt([]byte(raw))
		require.Error(t, err)
	}
}
