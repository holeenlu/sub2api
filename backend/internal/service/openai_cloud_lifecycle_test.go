//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Pause the actor after turn identity was confirmed so its inbound channel can
// contain the complete terminal sequence before it processes any event.
type cloudPauseTurnStore struct {
	*cloudLiveRoutingStore
	paused chan struct{}
	resume chan struct{}
	once   sync.Once
}

func (s *cloudPauseTurnStore) SaveCloudSession(ctx context.Context, record *OpenAICloudSessionRecord, refs []string, settlement *OpenAICloudSettlement) error {
	if err := s.cloudLiveRoutingStore.SaveCloudSession(ctx, record, refs, settlement); err != nil {
		return err
	}
	if record.Status == "running" && record.TurnID != "" {
		s.once.Do(func() {
			close(s.paused)
			select {
			case <-s.resume:
			case <-ctx.Done():
			}
		})
	}
	return nil
}

func TestCloudActorPeerClosePreservesTerminalAndSingleSettlement(t *testing.T) {
	for _, status := range []string{"completed", "failed", "interrupted", "missing_terminal"} {
		t.Run(status, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			store := &cloudPauseTurnStore{cloudLiveRoutingStore: newCloudLiveRoutingStore(), paused: make(chan struct{}), resume: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(store.resume) })
			wire := newCloudRPCFakeConn()
			connected := make(chan *cloudRPC, 1)
			sessions := NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
				rpc := newCloudRPC(ctx, &cloudOrderedEOFConn{wire})
				connected <- rpc
				return rpc, rpc.Initialize(ctx)
			}, func(context.Context, OpenAICloudOwner) error { return nil })
			defer sessions.Close()
			owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4, GrantGeneration: "grant", SettingsDigest: "settings", Model: "gpt-5.1"}
			type outcome struct {
				result *OpenAICloudResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := sessions.Run(ctx, OpenAICloudRequest{Owner: owner, RootRequestID: "root", BillingSnapshot: json.RawMessage(`{}`), Body: json.RawMessage(`{"model":"gpt-5.1","input":"hello"}`)}, nil)
				done <- outcome{result, err}
			}()
			cloudWireStart(t, ctx, wire)
			select {
			case <-store.paused:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			rpc := <-connected
			sessions.mu.Lock()
			actor := sessions.actors[store.snapshot().SessionID]
			sessions.mu.Unlock()
			require.NotNil(t, actor)
			cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12}}}})
			queued := 2 // usage then close
			if status != "missing_terminal" {
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": status}}})
				queued++
			}
			wire.reads <- nil
			cloudWaitDisconnected(t, ctx, rpc)
			require.Eventually(t, func() bool { return len(actor.upstream) == queued }, time.Second, time.Millisecond, "terminal error must queue after all received events")
			release.Do(func() { close(store.resume) })
			select {
			case got := <-done:
				require.NoError(t, got.err)
				require.NotNil(t, got.result)
				require.True(t, got.result.Terminal)
				require.Equal(t, 12, got.result.Usage.TotalTokens)
				require.Equal(t, status == "completed", got.result.UsageCredible)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-actor.done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			settlement := store.snapshotSettlement("root")
			require.NotNil(t, settlement)
			want := status
			if status == "missing_terminal" {
				want = "unknown"
			}
			require.Equal(t, want, settlement.Status)
			require.Equal(t, status == "missing_terminal", settlement.Unknown)
			require.Equal(t, status == "failed" || status == "interrupted", settlement.LowerBound)
			require.Equal(t, 12, settlement.Usage.TotalTokens)
			require.Equal(t, 1, store.settlementCount())
			require.Empty(t, wire.writes, "no reconnect, replay or tool effect after EOF")
		})
	}
}

type cloudClosedToolRequestStore struct{ *cloudActorTestStore }

func (s *cloudClosedToolRequestStore) CloudRequestClosed(context.Context, string) (bool, error) {
	return true, nil
}

func TestCloudToolHandoffFencesKnownInvalidation(t *testing.T) {
	for _, failure := range []string{"owner", "peer_close", "request_cancel", "request_deadline"} {
		for _, boundary := range []string{"admission", "publication"} {
			t.Run(failure+"/"+boundary, func(t *testing.T) {
				a, store, _ := cloudHostedActorForTest(t)
				if boundary == "publication" {
					a.tools["pending"] = &cloudPendingTool{RPCID: json.RawMessage(`"rpc"`), CallID: "pending", Name: "echo", Arguments: `{}`}
				}
				switch failure {
				case "owner":
					a.observedOwnerFailure.Store(errCloudAuthorizationChanged)
				case "peer_close":
					a.rpc.shutdown(errCloudRPCClosed, true)
				case "request_cancel":
					a.service.store = &cloudClosedToolRequestStore{store}
				case "request_deadline":
					a.historyRecoveryCommand = OpenAICloudCommand{RequestID: a.requestID, Deadline: time.Now().Add(-time.Second)}
				}
				var err error
				if boundary == "admission" {
					err = cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"rpc"`)
					require.Empty(t, a.tools)
				} else {
					err = a.finishAwaiting()
				}
				require.Error(t, err)
				require.Empty(t, store.results, "an invalidated owner must not hand execution to a client")
				require.Empty(t, a.history)
				// Revoking effects must not discard already confirmed accounting.
				require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "interrupted"}}, ""))
				require.Len(t, store.settlements, 1)
				require.True(t, store.settlements[0].LowerBound)
				require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
			})
		}
	}
}

func TestCloudPendingRPCIDUsesJSONIdentity(t *testing.T) {
	for _, pair := range []struct {
		first, second string
		duplicate     bool
	}{
		{`"a"`, `"\u0061"`, true}, {`1`, `"1"`, false}, {`"1"`, `1`, false},
	} {
		t.Run(pair.first+"/"+pair.second, func(t *testing.T) {
			a, _, _ := cloudHostedActorForTest(t)
			a.requestID = "" // retain multiple pending RPCs without publishing tools
			a.tools["first"] = &cloudPendingTool{RPCID: json.RawMessage(pair.first), CallID: "first", Name: "echo", Arguments: `{}`}
			err := cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": "second", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, pair.second)
			if pair.duplicate {
				require.EqualError(t, err, "Cloud repeated a pending tool RPC")
				require.Len(t, a.tools, 1)
			} else {
				require.NoError(t, err)
				require.Len(t, a.tools, 2)
			}
		})
	}
}

func TestCloudInterruptWaitStopsOnOrderedClose(t *testing.T) {
	for _, closedChannel := range []bool{false, true} {
		a, store, wire := cloudHostedActorForTest(t)
		events := make(chan cloudRPCDelivery, 1)
		a.upstream = events
		if !closedChannel {
			events <- cloudRPCDelivery{err: errCloudRPCClosed}
		}
		close(events)
		done := make(chan bool, 1)
		go func() { done <- a.interruptForContinuation("test") }()
		cloudWireResponse(t, a.ctx, wire, "turn/interrupt", map[string]any{})
		select {
		case continued := <-done:
			require.False(t, continued)
		case <-time.After(time.Second):
			t.Fatal("ordered close must end interruption wait immediately")
		}
		require.Equal(t, "unknown", a.record.Status)
		require.Len(t, store.settlements, 1)
		require.True(t, store.settlements[0].Unknown)
		require.Empty(t, wire.writes, "interrupt ACK alone must never start a continuation")
	}
}

func TestCloudHardFailureFencesEventsAlreadyInActorChannel(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.rpc.fail(errCloudRPCProtocol)
	err := cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, "")
	require.True(t, errors.Is(err, errCloudRPCProtocol))
	require.Empty(t, store.settlements)
}

func TestCloudTerminalFlushDoesNotPublishPendingTools(t *testing.T) {
	for _, invalidated := range []bool{false, true} {
		a, store, _ := cloudHostedActorForTest(t)
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": "text", "type": "agentMessage", "text": ""}}, ""))
		a.tools["pending"] = &cloudPendingTool{RPCID: json.RawMessage(`"rpc"`), CallID: "pending", Name: "echo", Arguments: `{}`}
		a.record.Status = "awaiting_tools"
		if invalidated {
			a.observedOwnerFailure.Store(errCloudAuthorizationChanged)
		}
		require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "interrupted"}}, ""))
		require.False(t, a.tools["pending"].Published)
		require.NotContains(t, cloudHostedWireForTest(t, a, store), "function_call")
		require.Len(t, store.settlements, 1)
		require.True(t, store.settlements[0].LowerBound)
	}
}

func TestCloudStopDrainsTerminalAfterPeerClose(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	events := make(chan cloudRPCDelivery, 2)
	events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"interrupted"}}`)}}
	events <- cloudRPCDelivery{err: errCloudRPCClosed}
	close(events)
	a.upstream = events
	a.rpc.shutdown(errCloudRPCClosed, true)
	a.stop("Cloud client cancelled the request")
	require.Equal(t, "interrupted", a.record.Status)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].LowerBound)
	require.Empty(t, wire.writes)
}
