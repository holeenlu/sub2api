//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const cloudInjectionTestWireLimit = 4 << 20

type cloudInjectionTestOutcome struct {
	frames, starts, turns int
	accepted              []any
	maxFrame              int
}

// Model the measured real-host boundary on the complete JSON-RPC frame. A
// failed injection does not start inference, and an accepted prefix must never
// be resent to the same thread. All contents here are synthetic.
func cloudRunInjectionLimit(t *testing.T, history []any, failSecond bool) (gjson.Result, cloudInjectionTestOutcome) {
	t.Helper()
	h := newCloudHistoryRoutingHarness(t)
	h.allow(30 * time.Second)
	done := make(chan cloudInjectionTestOutcome, 1)
	stop := make(chan struct{})
	go func() {
		var got cloudInjectionTestOutcome
		defer func() { done <- got }()
		wire := h.nextWire()
		if wire == nil {
			return
		}
		for {
			select {
			case <-stop:
				return
			case <-h.ctx.Done():
				return
			case raw := <-wire.writes:
				var msg cloudRPCMessage
				if json.Unmarshal(raw, &msg) != nil {
					t.Error("outgoing injection RPC is invalid JSON")
					return
				}
				switch msg.Method {
				case "thread/start":
					got.starts++
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "size-thread"}}})
				case "thread/inject_items":
					got.frames++
					got.maxFrame = max(got.maxFrame, len(raw))
					if len(raw) > cloudInjectionTestWireLimit {
						cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32602, "message": fmt.Sprintf("Operation was attempted past the valid range: Error, decoded message length too large: found %d bytes, the limit is: 4194304 bytes", len(raw)), "data": map[string]any{"grpcStatusCode": 11}}})
						continue
					}
					if failSecond && got.frames == 2 {
						cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32603, "message": "injected batch failed"}})
						continue
					}
					var params struct {
						Items []any `json:"items"`
					}
					if json.Unmarshal(msg.Params, &params) != nil {
						t.Error("injection params are invalid")
						return
					}
					got.accepted = append(got.accepted, params.Items...)
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				case "turn/start":
					got.turns++
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": "size-turn"}}})
					cloudWireCompleteTurn(wire, "size-thread", "size-turn", 12)
				case "thread/archive":
					cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				}
			}
		}
	}()
	input := append(append([]any(nil), history...), cloudUserMessage("continue"))
	response, err := h.invoke(map[string]any{"model": "client-model", "input": input})
	close(stop)
	got := <-done
	require.NoError(t, err, "the actor terminal owns injection failure")
	return response, got
}

func TestCloudInjectionBatchesPreserveHistoryWithinUpstreamFrame(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		count      int
	}{
		{"six MiB", strings.Repeat("a", 1<<20), 6},
		{"JSON HTML escaping", strings.Repeat("<>&", 196608), 3},
		{"quotes newlines backslashes", strings.Repeat("\"\n\\", 500000), 3},
		{"UTF8", strings.Repeat("界😊", 350000), 3},
		{"one item above target below upstream limit", strings.Repeat("b", 3<<20), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := make([]any, tc.count)
			for i := range history {
				history[i] = cloudUserMessage(fmt.Sprintf("%d:", i) + tc.text)
			}
			want, err := json.Marshal(history)
			require.NoError(t, err)
			response, got := cloudRunInjectionLimit(t, history, false)
			require.Equal(t, "completed", response.Get("status").String(), response.Raw)
			require.Equal(t, 1, got.starts)
			require.Equal(t, 1, got.turns)
			require.LessOrEqual(t, got.maxFrame, cloudInjectionTestWireLimit)
			if tc.count > 1 {
				require.Greater(t, got.frames, 1)
			} else {
				require.Equal(t, 1, got.frames, "the batching target is not an item admission limit")
			}
			actual, err := json.Marshal(got.accepted)
			require.NoError(t, err)
			require.True(t, bytes.Equal(want, actual), "injection must preserve complete ordered history bytes")
		})
	}
}

func TestCloudInjectionOversizedItemRejectsBeforeAnyBatch(t *testing.T) {
	history := []any{cloudUserMessage("small prefix"), cloudUserMessage(strings.Repeat("x", 4<<20))}
	response, got := cloudRunInjectionLimit(t, history, false)
	require.Equal(t, "failed", response.Get("status").String())
	require.EqualValues(t, 413, response.Get("error.status_code").Int())
	require.Zero(t, got.frames, "all items must be sized before dispatching a prefix")
	require.Zero(t, got.turns)
}

func TestCloudInjectionSecondBatchFailureDoesNotReplayPrefix(t *testing.T) {
	history := make([]any, 6)
	for i := range history {
		history[i] = cloudUserMessage(strings.Repeat(string(rune('a'+i)), 1<<20))
	}
	response, got := cloudRunInjectionLimit(t, history, true)
	require.Equal(t, "failed", response.Get("status").String())
	require.Equal(t, 2, got.frames)
	require.Equal(t, 1, got.starts, "unclassified failure must not create a new execution")
	require.Zero(t, got.turns)
	require.NotEmpty(t, got.accepted)
	require.Less(t, len(got.accepted), len(history))
}

func TestCloudInjectionCancellationStopsBeforeNextBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	t.Cleanup(func() { rpc.Close(); wire.Close() })
	cloudTestInitialize(t, ctx, rpc, wire)
	history := make([]any, 6)
	for i := range history {
		history[i] = cloudUserMessage(strings.Repeat("x", 1<<20))
	}
	actor := &cloudSessionActor{ctx: ctx, rpc: rpc, historyRecoveryItems: history, record: &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{ThreadID: "cancel-thread", Status: "running"}}}
	done := make(chan bool, 1)
	checks := 0
	go func() {
		done <- actor.injectHistory(&cloudProtocolRequest{}, func() bool {
			checks++
			return checks == 1
		})
	}()
	msg := cloudTestMessage(t, ctx, wire)
	require.Equal(t, "thread/inject_items", msg.Method)
	cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
	select {
	case ok := <-done:
		require.False(t, ok, "the cancellation fence must run before every following batch")
	case <-time.After(5 * time.Second):
		t.Fatal("injection did not observe the cancellation fence")
	}
	require.Equal(t, 2, checks)
	require.Empty(t, wire.writes, "no batch or turn may follow a closed request")
}
