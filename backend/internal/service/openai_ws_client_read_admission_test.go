package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/host"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type openAIWSTestAdmissionKey struct{}
type openAIWSTestAdmission struct {
	mu                       sync.Mutex
	enabled, draining        bool
	accepted, released, live int
}

func (a *openAIWSTestAdmission) EnableTurnTracking() { a.mu.Lock(); a.enabled = true; a.mu.Unlock() }
func (a *openAIWSTestAdmission) AdmitTurn() (func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.enabled {
		return nil, errors.New("frame accepted before tracking enabled")
	}
	if a.draining {
		return nil, errors.New("host draining")
	}
	a.accepted++
	a.live++
	// Deliberately not idempotent: the ingress transfer must release exactly once.
	return func() { a.mu.Lock(); a.released++; a.live--; a.mu.Unlock() }, nil
}
func (a *openAIWSTestAdmission) counts() (int, int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.accepted, a.released, a.live
}
func (a *openAIWSTestAdmission) context(ctx context.Context) context.Context {
	return context.WithValue(host.WithTurnAdmission(ctx, a), openAIWSTestAdmissionKey{}, a)
}
func openAIWSTestAdmissionContext(ctx context.Context) context.Context {
	if _, ok := ctx.Value(openAIWSTestAdmissionKey{}).(*openAIWSTestAdmission); ok {
		return ctx
	}
	return (&openAIWSTestAdmission{}).context(ctx)
}
func waitOpenAIWSAdmissions(t *testing.T, owner *openAIWSTestAdmission, accepted, released, live int) {
	t.Helper()
	require.Eventually(t, func() bool { a, r, l := owner.counts(); return a == accepted && r == released && l == live }, time.Second, time.Millisecond)
}

func TestOpenAIWSTurnAdmissionMissingFailsClosed(t *testing.T) {
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096)
	defer ds.CloseNow()
	conn.input <- []byte(`{"type":"response.create","input":"hello"}`)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := ds.Read(ctx)
	require.ErrorIs(t, err, host.ErrTurnAdmissionMissing)
}

func TestOpenAIWSTurnAdmissionQueuedDiscardKeepsActiveUntilCleanup(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096, owner.context(t.Context()))
	defer ds.finishAdmissions()
	defer ds.CloseNow()
	conn.input <- []byte(`{"type":"response.create","input":"one"}`)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := ds.Read(ctx)
	require.NoError(t, err)
	conn.input <- []byte(`{"type":"response.create","input":"two"}`)
	conn.input <- []byte(`{"type":"response.create","input":"three"}`)
	waitOpenAIWSAdmissions(t, owner, 3, 0, 3)
	require.NoError(t, ds.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"r"}}`)))
	waitOpenAIWSAdmissions(t, owner, 3, 0, 3)
	require.NoError(t, ds.CloseNow())
	waitOpenAIWSAdmissions(t, owner, 3, 2, 1)
	ds.finishAdmissions()
	ds.finishAdmissions()
	waitOpenAIWSAdmissions(t, owner, 3, 3, 0)
}

func TestOpenAIWSTurnAdmissionDrainRejectsReadAhead(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096, owner.context(t.Context()))
	defer ds.finishAdmissions()
	defer ds.CloseNow()
	conn.input <- []byte(`{"type":"response.create","input":"one"}`)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, _, err := ds.Read(ctx)
	require.NoError(t, err)
	owner.mu.Lock()
	owner.draining = true
	owner.mu.Unlock()
	conn.input <- []byte(`{"type":"response.create","input":"two"}`)
	select {
	case <-ds.done:
	case <-ctx.Done():
		t.Fatal("read pump did not join")
	}
	waitOpenAIWSAdmissions(t, owner, 1, 0, 1)
	ds.finishAdmissions()
	waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
}

func TestOpenAIWSTurnAdmissionInitialRetryRetainsOneOwner(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	ctx := owner.context(t.Context())
	ds := NewOpenAIWSDownstream(newQueuedWSDownstreamTestConn(), 4096, ctx)
	defer ds.CloseNow()
	require.NoError(t, ds.acceptInitialTurn(ctx))
	require.NoError(t, ds.acceptInitialTurn(ctx))
	waitOpenAIWSAdmissions(t, owner, 1, 0, 1)
	ds.finishAdmissions()
	waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
}

func TestOpenAIWSPassthroughCompletionRequiresSendSettlementAndDelivery(t *testing.T) {
	for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		t.Run(string(rune('a'+order[0]))+string(rune('a'+order[1]))+string(rune('a'+order[2])), func(t *testing.T) {
			calls := 0
			completion := &openAIWSPassthroughTurnCompletion{}
			completion.finish = func() { completion.mu.Lock(); calls++; completion.mu.Unlock() }
			for i, step := range order {
				completion.advance(step == 0, step == 1, step == 2)
				if i < 2 {
					require.Zero(t, calls)
				} else {
					require.Equal(t, 1, calls)
				}
			}
			completion.advance(true, true, true)
			require.Equal(t, 1, calls)
		})
	}
}

func TestOpenAIWSTurnAdmissionAbandonedDirectRetryJoinsOwner(t *testing.T) {
	for _, inFlight := range []bool{false, true} {
		name := "between_attempts"
		if inFlight {
			name = "active_cleanup"
		}
		t.Run(name, func(t *testing.T) {
			owner := &openAIWSTestAdmission{}
			ctx := owner.context(t.Context())
			conn := newQueuedWSDownstreamTestConn()
			ds := NewOpenAIWSDownstream(conn, 4096, ctx)
			defer ds.CloseNow()
			require.NoError(t, ds.beginDirectCoordinator())
			require.NoError(t, ds.acceptInitialTurn(ctx))
			if !inFlight {
				ds.endDirectCoordinator(true)
			}
			require.NoError(t, conn.CloseNow())
			select {
			case <-ds.done:
			case <-time.After(time.Second):
				t.Fatal("reader did not join")
			}
			if inFlight {
				waitOpenAIWSAdmissions(t, owner, 1, 0, 1)
				ds.endDirectCoordinator(false)
			}
			waitOpenAIWSAdmissions(t, owner, 1, 1, 0)
		})
	}
}

func TestOpenAIWSPassthroughCompletionDefersBareErrorUntilChosenTerminal(t *testing.T) {
	for _, terminal := range []string{"error", "response.failed", "response.completed"} {
		t.Run(terminal, func(t *testing.T) {
			calls := 0
			completion := &openAIWSPassthroughTurnCompletion{sent: true, finish: func() { calls++ }}
			completion.deliveredEvent("error")
			require.Zero(t, calls)
			completion.settle(terminal)
			if terminal != "error" {
				require.Zero(t, calls, "the provisional error must not publish a later terminal")
				completion.deliveredEvent(terminal)
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestOpenAIWSTurnAdmissionDoesNotAddASecondReadLimit(t *testing.T) {
	owner := &openAIWSTestAdmission{}
	conn := newQueuedWSDownstreamTestConn()
	ds := NewOpenAIWSDownstream(conn, 4096, owner.context(t.Context()))
	defer ds.finishAdmissions()
	defer ds.CloseNow()
	// Draining the existing bounded queue is not a new business admission point.
	// The coordinator may retain cleanup ownership while consuming buffered work.
	const frames = openAIWSClientQueueLimit + 4
	for range frames {
		conn.input <- []byte("{\"type\":\"response.create\",\"input\":\"work\"}")
	}
	require.Eventually(t, func() bool { return len(ds.frames) == openAIWSClientQueueLimit }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for range frames {
		_, _, err := ds.Read(ctx)
		require.NoError(t, err)
	}
	waitOpenAIWSAdmissions(t, owner, frames, 0, frames)
	require.False(t, ds.ClientClosed())
	ds.finishAdmissions()
	waitOpenAIWSAdmissions(t, owner, frames, frames, 0)
}
