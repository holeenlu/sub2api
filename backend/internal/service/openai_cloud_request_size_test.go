package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// cloudLargeImageMessage is a user message with text and one inline image
// whose base64 payload alone is about size bytes; the gateway validates the
// encoding only and never decodes the image.
func cloudLargeImageMessage(size int) (map[string]any, string) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, size/4*3/4))
	return map[string]any{"type": "message", "role": "user", "content": []any{
		map[string]any{"type": "input_text", "text": "what is in this screenshot?"},
		map[string]any{"type": "input_image", "image_url": url, "detail": "high"},
	}}, url
}

func (s *cloudLiveRoutingStore) commandQueue(id string) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.commands[id]), s.bytes["command:"+id]
}

// A new session's owner is always this process, so a request far above the
// former 4 MiB + 512 KiB Redis command envelope reaches its actor through the
// local inbox and the upstream turn carries the inline image unchanged. Like
// native, only the ingress body limit bounds it. No command is written to the
// session's Redis stream.
func TestCloudLargeInlineImageRequestReachesLocalOwnerWithoutRedisCommand(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	message, url := cloudLargeImageMessage(24 << 20)
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		wire := h.nextWire()
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		turn := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "turn/start", turn.Method)
		require.Greater(t, len(turn.Params), 8<<20)
		require.Equal(t, url, gjson.GetBytes(turn.Params, `input.#(type=="image").url`).String())
		cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
		cloudWireCompleteTurn(wire, "thread", "turn", 2000)
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "input": []any{message}})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	sessions := h.store.all()
	require.Len(t, sessions, 1)
	record := sessions[0].snapshot()
	entries, queued := sessions[0].commandQueue(record.SessionID)
	require.Zero(t, entries)
	require.Zero(t, queued, "the local owner received the request without a Redis command")
	require.Equal(t, 1, h.store.settlementCount())
}

// cloudCommandSizeStore records every command another instance writes for an
// owner elsewhere; the shared fake store still delivers them to that owner.
type cloudCommandSizeStore struct {
	*cloudMultiRoutingStore
	mu    sync.Mutex
	sizes []int
}

func (s *cloudCommandSizeStore) EnqueueCloudCommand(ctx context.Context, id string, command OpenAICloudCommand) error {
	if !command.Cancel && !command.CloseCompleted {
		raw, err := OpenAICloudEncodeCommand(command)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.sizes = append(s.sizes, len(raw))
		s.mu.Unlock()
	}
	return s.cloudMultiRoutingStore.EnqueueCloudCommand(ctx, id, command)
}

func (s *cloudCommandSizeStore) recorded() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.sizes...)
}

// A continuation that reaches the gateway on an instance other than the
// session owner's still crosses Redis. That command is bounded by
// OpenAICloudMaxCommandBytes: a 24 MiB request is delivered and run on the
// owner's socket; a request over the bound is refused before any dispatch with
// a not-started 413 that is never replayed on another account.
func TestCloudCrossInstanceContinuationKeepsCommandBound(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	h.allow(time.Minute)
	wire, _, lastID := h.establish()
	owner := h.svc.cloudSessions
	remote := &cloudCommandSizeStore{cloudMultiRoutingStore: h.store}
	other := NewOpenAICloudSessions(remote, func(context.Context, OpenAICloudOwner) (*cloudRPC, error) {
		return nil, errors.New("the non-owner instance never connects upstream")
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	t.Cleanup(other.Close)
	h.svc.cloudSessions = other
	require.Nil(t, other.localActor(h.store.all()[0].snapshot().SessionID))
	require.NotNil(t, owner.localActor(h.store.all()[0].snapshot().SessionID))

	message, url := cloudLargeImageMessage(24 << 20)
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		turn := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "turn/start", turn.Method)
		require.Equal(t, url, gjson.GetBytes(turn.Params, `input.#(type=="image").url`).String())
		cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-two"}}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 3000)
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "previous_response_id": lastID, "input": []any{message}, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	sizes := remote.recorded()
	require.Len(t, sizes, 1)
	require.Greater(t, sizes[0], 8<<20, "the cross-instance command carried the whole request")
	require.LessOrEqual(t, sizes[0], OpenAICloudMaxCommandBytes)
	require.Equal(t, 2, h.store.settlementCount())

	oversized, _ := cloudLargeImageMessage(OpenAICloudMaxCommandBytes + (1 << 20))
	_, err = h.invoke(map[string]any{"model": "client-model", "previous_response_id": response.Get("id").String(), "input": []any{oversized}, "tools": cloudHistoryTools})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrOpenAICloudNotStarted)
	status, code, _, known := cloudPublicError(err)
	require.True(t, known, "%v", err)
	require.Equal(t, http.StatusRequestEntityTooLarge, status)
	require.Equal(t, "request_too_large", code)
	require.Len(t, remote.recorded(), 1, "no command was written for the oversized request")
	require.Nil(t, cloudPreInferenceFailover(t.Context(), nil, status, code, "", err, true), "an oversized request is not replayed on other accounts")
	select {
	case raw := <-wire.writes:
		t.Fatalf("the owner received upstream work for the refused request: %s", gjson.GetBytes(raw, "method").String())
	case <-time.After(200 * time.Millisecond):
	}
	require.Equal(t, 2, h.store.settlementCount())

	// The same request on the owner's instance is bounded by ingress only.
	h.svc.cloudSessions = owner
	upstream = make(chan struct{})
	go func() {
		defer close(upstream)
		turn := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "turn/start", turn.Method)
		cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-three"}}})
		cloudWireCompleteTurn(wire, "thread", "turn-three", 4000)
	}()
	small, _ := cloudLargeImageMessage(1 << 20)
	response, err = h.invoke(map[string]any{"model": "client-model", "previous_response_id": response.Get("id").String(), "input": []any{small}, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	require.Len(t, remote.recorded(), 1, "the owner instance does not write commands to Redis")
}
