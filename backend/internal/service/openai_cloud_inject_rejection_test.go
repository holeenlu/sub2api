//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Production 2026-10-25: Cloud answered thread/inject_items with JSON-RPC
// -32602 (gRPC INVALID_ARGUMENT) for three migrated Codex conversations. The
// rejection happened before any turn, yet no reduction was attempted and every
// retry repeated the same refusal. Both JSON-RPC history refusals now take the
// approved pre-inference reductions on a fresh thread of the same account.
var cloudInjectRejections = map[string]map[string]any{
	"grpc invalid argument": {"code": -32602, "message": "Client specified an invalid argument: history item rejected", "data": map[string]any{"grpcStatusCode": 3}},
	"invalid request":       {"code": -32600, "message": "items[1] is not a valid response item: unknown variant"},
}

func TestCloudInjectRejectionRecoversBeforeInference(t *testing.T) {
	for name, rejection := range cloudInjectRejections {
		t.Run(name, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			type attempts struct {
				starts, turns, archives int
				injections              []string
			}
			done := make(chan attempts, 1)
			go func() {
				wire := h.nextWire()
				got := attempts{}
				defer func() { done <- got }()
				thread := ""
				for {
					msg := cloudTestMessage(t, h.ctx, wire)
					switch msg.Method {
					case "thread/start":
						got.starts++
						thread = fmt.Sprintf("inject-thread-%d", got.starts)
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": thread}}})
					case "thread/inject_items":
						got.injections = append(got.injections, string(msg.Params))
						if got.starts == 1 {
							cloudTestRead(wire, map[string]any{"id": msg.ID, "error": rejection})
						} else {
							cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
						}
					case "thread/archive":
						got.archives++
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
					case "turn/start":
						got.turns++
						turn := fmt.Sprintf("inject-turn-%d", got.turns)
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": turn}}})
						cloudWireCompleteTurn(wire, thread, turn, 12)
						return
					}
				}
			}()
			body := map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": append(cloudForeignHistory(), cloudUserMessage("continue"))}
			response, err := h.invoke(body)
			// Wait for the fake upstream first: on a regression it is still
			// expecting the replacement thread when the gateway gives up.
			got := <-done
			require.NoError(t, err)
			require.Equal(t, "completed", response.Get("status").String(), response.Raw)
			require.Equal(t, 2, got.starts, "the refused thread is replaced, not reused")
			require.Equal(t, 1, got.archives, "an explicitly refused thread still exists and is archived")
			require.Equal(t, 1, got.turns, "nothing ran before the refusal")
			require.Len(t, got.injections, 2)
			require.Contains(t, got.injections[0], "gAAAA-bps-reasoning-ciphertext")
			require.NotContains(t, got.injections[1], "encrypted_content")
			require.Contains(t, got.injections[1], "hi there")
			require.Contains(t, got.injections[1], "echoed")
			require.Equal(t, 1, h.store.settlementCount(), "recovery keeps one billing root")
			require.EqualValues(t, 12, response.Get("usage.total_tokens").Int())
		})
	}
}

// After both approved reductions are refused, the client receives the
// history-specific 400 instead of a generic parameter or protocol error, and no
// turn is ever started.
func TestCloudInjectRejectionExhaustedReportsHistory(t *testing.T) {
	for name, rejection := range cloudInjectRejections {
		t.Run(name, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			done := make(chan []string, 1)
			turns := 0
			go func() {
				wire := h.nextWire()
				starts := 0
				var injections []string
				defer func() { done <- injections }()
				for {
					msg := cloudTestMessage(t, h.ctx, wire)
					switch msg.Method {
					case "thread/start":
						starts++
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": fmt.Sprintf("refused-thread-%d", starts)}}})
					case "thread/inject_items":
						injections = append(injections, string(msg.Params))
						cloudTestRead(wire, map[string]any{"id": msg.ID, "error": rejection})
						if len(injections) == 3 {
							return
						}
					case "thread/archive":
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
					case "turn/start":
						turns++
						cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32603, "message": "unexpected turn"}})
						return
					}
				}
			}()
			history := append(cloudForeignHistory(), map[string]any{"type": "compaction", "encrypted_content": "opaque-compaction"}, cloudUserMessage("continue"))
			response, err := h.invoke(map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": history})
			injections := <-done
			require.NoError(t, err)
			require.Len(t, injections, 3, "original history plus two approved reductions")
			require.NotContains(t, injections[2], "encrypted_content")
			require.Contains(t, injections[2], openAIOmittedCompactionNotice)
			require.Zero(t, turns)
			require.Equal(t, "failed", response.Get("status").String(), response.Raw)
			require.Equal(t, "cloud_history_rejected", response.Get("error.code").String(), response.Raw)
			require.EqualValues(t, 400, response.Get("error.status_code").Int(), response.Raw)
			require.EqualValues(t, 2, response.Get("error.history_recovery_attempts").Int(), response.Raw)
		})
	}
}

// Only the history batch is reduced. A turn/start refusal with the same RPC
// code is not evidence that injected history was unreadable.
func TestCloudTurnStartInvalidArgumentIsNotHistoryRecovery(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	starts := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		wire := h.nextWire()
		for {
			msg := cloudTestMessage(t, h.ctx, wire)
			switch msg.Method {
			case "thread/start":
				starts++
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": fmt.Sprintf("turn-thread-%d", starts)}}})
			case "thread/inject_items":
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
			case "turn/start":
				cloudTestRead(wire, map[string]any{"id": msg.ID, "error": cloudInjectRejections["grpc invalid argument"]})
				return
			}
		}
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": append(cloudForeignHistory(), cloudUserMessage("continue"))})
	<-done
	require.NoError(t, err)
	require.Equal(t, "failed", response.Get("status").String(), response.Raw)
	require.Equal(t, 1, starts)
	require.NotEqual(t, "cloud_history_rejected", response.Get("error.code").String())
	require.EqualValues(t, 0, response.Get("error.history_recovery_attempts").Int())
}

// The upstream reason is kept for operators in server logs only. It must never
// carry prompt text, ciphertext or credentials that an error message may echo.
func TestCloudRPCRejectionReasonIsSanitized(t *testing.T) {
	prompt := strings.Repeat("my private prompt ", 8)
	raw := json.RawMessage(`{"code":-32602,"message":"Client specified an invalid argument: items[12] is not a valid response item: invalid type: string \"` + prompt + `\", expected a sequence\u0007","data":{"grpcStatusCode":3}}`)
	reason := cloudRPCRejectionReason(raw)
	require.Contains(t, reason, "grpc=3")
	require.Contains(t, reason, "items[12] is not a valid response item")
	require.Contains(t, reason, "expected a sequence")
	require.NotContains(t, reason, "my private prompt")
	require.NotContains(t, reason, "\u0007")

	secrets := cloudSanitizeUpstreamText("auth Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.c2lnbmF0dXJl key sk-abcdefghijklmnop blob " + strings.Repeat("QUJD", 30))
	require.NotContains(t, secrets, "eyJhbGci")
	require.NotContains(t, secrets, "sk-abcdefghijklmnop")
	require.NotContains(t, secrets, "QUJDQUJD")

	long := cloudSanitizeUpstreamText(strings.Repeat("word ", 200))
	require.LessOrEqual(t, len([]rune(long)), 301)

	require.Empty(t, cloudRPCRejectionReason(json.RawMessage(`{"code":-32602}`)))
	require.Empty(t, cloudRPCRejectionReason(json.RawMessage(`not json`)))
}

// A reason that names one injected item is logged with that item's structure,
// never its values, so the next refusal identifies the incompatible shape.
func TestCloudRejectedItemShapeHasNoValues(t *testing.T) {
	items := cloudForeignHistory()
	shape := cloudRejectedItemShape(items, "Client specified an invalid argument: items[2] is not a valid response item")
	require.Contains(t, shape, "items[2]")
	require.Contains(t, shape, "message")
	require.Contains(t, shape, "encrypted_content")
	require.NotContains(t, shape, "hi there")
	require.NotContains(t, shape, "gAAAA")
	require.Empty(t, cloudRejectedItemShape(items, "no index here"))
	require.Empty(t, cloudRejectedItemShape(items, "items[99] out of range"))
}
