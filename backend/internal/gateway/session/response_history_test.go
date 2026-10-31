package session

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func historyIdentity() ContinuationIdentity {
	return ContinuationIdentity{AccountID: 10, APIKeyID: 20, GroupID: 30, Credentials: [32]byte{1}}
}
func publishHistory(t *testing.T, h *ResponseHistory, id string, input []json.RawMessage) {
	t.Helper()
	h.Observe(id, "left")
	p, ok := h.BeginPublication(id, "left")
	require.True(t, ok)
	require.True(t, p.Finish(historyIdentity(), input, true, true))
}

func TestResponseHistoryRequiresDeliveredCompleteBoundIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                string
		delivered, complete bool
		identity            ContinuationIdentity
		known               bool
	}{
		{"delivered", true, true, historyIdentity(), true},
		{"write-failed", false, true, historyIdentity(), false},
		{"incomplete", true, false, historyIdentity(), false},
		{"canceled-partial", false, false, historyIdentity(), false},
		{"unbound-account", true, true, ContinuationIdentity{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewResponseHistory(256, 1024)
			t.Cleanup(h.Close)
			h.Observe("r", "left")
			p, ok := h.BeginPublication("r", "left")
			require.True(t, ok)
			require.Equal(t, tc.known, p.Finish(tc.identity, []json.RawMessage{[]byte("[]")}, tc.delivered, tc.complete))
			snapshot, err := h.Resolve(context.Background(), nil, "r", "right", historyIdentity())
			if tc.known {
				require.NoError(t, err)
				require.True(t, snapshot.Known)
				require.True(t, snapshot.CrossStream)
			} else {
				require.ErrorIs(t, err, ErrContinuationUnavailable)
			}
		})
	}
	h := NewResponseHistory(2, 100)
	h.Observe("r", "left")
	_, err := h.Resolve(context.Background(), nil, "r", "right", historyIdentity())
	require.ErrorIs(t, err, ErrContinuationUnavailable)
}

func TestResponseHistoryIdentityIsolationAndSnapshotCopies(t *testing.T) {
	h := NewResponseHistory(256, 1024)
	defer h.Close()
	input := []json.RawMessage{[]byte("[1]")}
	publishHistory(t, h, "r", input)
	input[0][1] = '9'
	for _, identity := range []ContinuationIdentity{
		{11, 20, 30, [32]byte{1}}, {10, 21, 30, [32]byte{1}}, {10, 20, 31, [32]byte{1}}, {10, 20, 30, [32]byte{2}},
	} {
		_, err := h.Resolve(context.Background(), nil, "r", "right", identity)
		require.ErrorIs(t, err, ErrContinuationUnavailable)
		snapshot, err := h.Resolve(context.Background(), nil, "r", "left", identity)
		require.NoError(t, err)
		require.False(t, snapshot.Known)
	}
	snapshot, err := h.Resolve(context.Background(), nil, "r", "right", historyIdentity())
	require.NoError(t, err)
	require.Equal(t, "[1]", string(snapshot.Input[0]))
	snapshot.Input[0][1] = '8'
	next, err := h.Resolve(context.Background(), nil, "r", "right", historyIdentity())
	require.NoError(t, err)
	require.Equal(t, "[1]", string(next.Input[0]))
	unknown, err := h.Resolve(context.Background(), nil, "not-found", "right", historyIdentity())
	require.NoError(t, err)
	require.False(t, unknown.Known)
	require.Nil(t, unknown.Input)
}

func TestResponseHistoryPublicationWaitAndCancellation(t *testing.T) {
	for _, action := range []string{"commit", "failed-write", "request-cancel", "lane-close", "connection-close"} {
		t.Run(action, func(t *testing.T) {
			h := NewResponseHistory(256, 1024)
			defer h.Close()
			h.Observe("r", "left")
			p, ok := h.BeginPublication("r", "left")
			require.True(t, ok)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			lane := make(chan struct{})
			type result struct {
				snapshot ContinuationSnapshot
				err      error
			}
			done := make(chan result, 1)
			go func() { s, err := h.Resolve(ctx, lane, "r", "right", historyIdentity()); done <- result{s, err} }()
			select {
			case <-done:
				t.Fatal("publication lookup returned before delivery result")
			default:
			}
			switch action {
			case "commit":
				require.True(t, p.Finish(historyIdentity(), []json.RawMessage{[]byte("[]")}, true, true))
			case "failed-write":
				require.False(t, p.Finish(historyIdentity(), nil, false, false))
			case "request-cancel":
				cancel()
			case "lane-close":
				close(lane)
			case "connection-close":
				h.Close()
			}
			select {
			case result := <-done:
				switch action {
				case "commit":
					require.NoError(t, result.err)
					require.True(t, result.snapshot.Known)
				case "failed-write":
					require.ErrorIs(t, result.err, ErrContinuationUnavailable)
				case "request-cancel":
					require.ErrorIs(t, result.err, context.Canceled)
				default:
					require.ErrorIs(t, result.err, io.EOF)
				}
			case <-time.After(time.Second):
				t.Fatal("lookup did not join")
			}
		})
	}
}

func TestResponseHistoryFencesDuplicateStaleAndForeignPublications(t *testing.T) {
	h := NewResponseHistory(1, 10)
	defer h.Close()
	h.Observe("r", "left")
	_, ok := h.BeginPublication("r", "foreign")
	require.False(t, ok)
	p, ok := h.BeginPublication("r", "left")
	require.True(t, ok)
	_, ok = h.BeginPublication("r", "left")
	require.False(t, ok)
	require.False(t, p.Finish(historyIdentity(), nil, false, false))
	h.Observe("next", "left")
	h.Observe("r", "left")
	require.False(t, p.Finish(historyIdentity(), nil, true, true), "old token cannot publish into a replacement record")
	newPub, ok := h.BeginPublication("r", "left")
	require.True(t, ok)
	require.True(t, newPub.Finish(historyIdentity(), nil, true, true))
	require.False(t, newPub.Finish(historyIdentity(), nil, true, true), "single use")
	h.Close()
	require.False(t, newPub.Finish(historyIdentity(), nil, true, true))
	require.Zero(t, h.bytes)
	require.Empty(t, h.entries)
}

func TestResponseHistoryUsesExistingCountAndByteBudgets(t *testing.T) {
	h := NewResponseHistory(2, 4)
	defer h.Close()
	publishHistory(t, h, "a", []json.RawMessage{[]byte("[1]")})
	publishHistory(t, h, "b", []json.RawMessage{[]byte("[2]")})
	snapshot, err := h.Resolve(context.Background(), nil, "a", "right", historyIdentity())
	require.NoError(t, err)
	require.False(t, snapshot.Known)
	require.LessOrEqual(t, h.bytes, int64(4))
	require.LessOrEqual(t, len(h.entries), 2)
	h.Observe("large", "left")
	p, ok := h.BeginPublication("large", "left")
	require.True(t, ok)
	require.False(t, p.Finish(historyIdentity(), []json.RawMessage{[]byte("[12345]")}, true, true))
	_, err = h.Resolve(context.Background(), nil, "large", "right", historyIdentity())
	require.ErrorIs(t, err, ErrContinuationUnavailable)
	bounded := NewResponseHistory(1, 100)
	bounded.Observe("publishing", "left")
	_, ok = bounded.BeginPublication("publishing", "left")
	require.True(t, ok)
	bounded.Observe("overflow", "right")
	require.Len(t, bounded.entries, 1)
	bounded.Close()
}

func TestResponseHistoryConcurrentReadersOwnTheirSnapshot(t *testing.T) {
	h := NewResponseHistory(256, 1024)
	defer h.Close()
	publishHistory(t, h, "r", []json.RawMessage{[]byte("[1]")})
	var workers sync.WaitGroup
	for i := 0; i < 32; i++ {
		workers.Go(func() {
			snapshot, err := h.Resolve(context.Background(), nil, "r", "right", historyIdentity())
			if err == nil && snapshot.Known {
				snapshot.Input[0][1] = '9'
			}
		})
	}
	workers.Wait()
	snapshot, err := h.Resolve(context.Background(), nil, "r", "right", historyIdentity())
	require.NoError(t, err)
	require.Equal(t, "[1]", string(snapshot.Input[0]))
}
