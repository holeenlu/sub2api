//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/host"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSMultiplexAdmissionMissingFailsClosed(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	conn.input <- multiplexFrame("a", "a1", "")
	called := false
	err := RunOpenAIWSMultiplexer(context.Background(), conn, nil, func(context.Context, *OpenAIWSDownstream) { called = true })
	require.ErrorIs(t, err, host.ErrTurnAdmissionMissing)
	require.False(t, called)
}

func TestOpenAIWSMultiplexAdmissionTracksQueueAndFinalCleanup(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	conn := newQueuedWSDownstreamTestConn()
	started := make(chan string, 3)
	done := make(chan error, 1)
	cleanupA, cleanupB := make(chan struct{}), make(chan struct{})
	var finishA, finishB sync.Once
	releaseA := func() { finishA.Do(func() { close(cleanupA) }) }
	releaseB := func() { finishB.Do(func() { close(cleanupB) }) }
	defer releaseA()
	defer releaseB()
	defer conn.CloseNow()
	go func() {
		done <- RunOpenAIWSMultiplexer(owner.context(t.Context()), conn, nil, func(ctx context.Context, ds *OpenAIWSDownstream) {
			_, payload, err := ds.Read(ctx)
			if err != nil {
				return
			}
			event := gjson.GetBytes(payload, "event_id").String()
			if err = ds.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"r"}}`)); err != nil {
				return
			}
			started <- event
			if event == "a1" {
				<-cleanupA
			} else {
				<-cleanupB
			}
			ds.completeMultiplexTurn()
		})
	}()
	conn.input <- multiplexFrame("a", "a1", "")
	conn.input <- multiplexFrame("a", "a2", "")
	conn.input <- multiplexFrame("b", "b1", "")
	events := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case event := <-started:
			events[event] = true
		case <-time.After(time.Second):
			t.Fatal("independent stream blocked")
		}
	}
	require.Equal(t, map[string]bool{"a1": true, "b1": true}, events)
	waitOpenAIWSAdmissions(t, owner, 3, 0, 3)
	releaseB()
	waitOpenAIWSAdmissions(t, owner, 3, 1, 2)
	require.NoError(t, conn.CloseNow())
	waitOpenAIWSAdmissions(t, owner, 3, 2, 1)
	select {
	case <-done:
		t.Fatal("joined before active cleanup")
	default:
	}
	releaseA()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("multiplexer did not join")
	}
	waitOpenAIWSAdmissions(t, owner, 3, 3, 0)
}

func TestOpenAIWSMultiplexAdmissionCanceledWhileWaitingForCapacity(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	ctx := owner.context(t.Context())
	owner.EnableTurnTracking()
	admission, err := admitOpenAIWSTurn(ctx)
	require.NoError(t, err)
	m := &openAIWSMultiplexer{ctx: ctx, active: make(chan struct{}, 1), pending: 1}
	m.active <- struct{}{}
	lane := &openAIWSStreamConn{mux: m, id: "a", done: make(chan struct{}), incoming: make(chan openAIWSClientReadResult, 1), pending: 1, dispatched: true}
	lane.incoming <- openAIWSClientReadResult{messageType: coderws.MessageText, payload: multiplexFrame("a", "a1", ""), admission: admission}
	ds := NewOpenAIWSDownstream(lane, 4096, ctx)
	defer ds.finishAdmissions()
	defer ds.CloseNow()
	readCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, _, err = ds.Read(readCtx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
}
