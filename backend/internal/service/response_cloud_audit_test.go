package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudExecutionRunGatewayReclaimsBeforeEventDisposal(t *testing.T) {
	svc, in, _, _ := cloudForwardFixture(t, &cloudGatewayError{429, "cloud_quota_exhausted", "Cloud account quota is exhausted"})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body := []byte(`{"model":"client-model","input":"hello"}`)
	ctx, c, writer := cloudContractContext(t, svc, in, ctx, body)
	_, err := svc.Forward(ctx, c, in.Account, body)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, CloudPreInferenceFailureReason, failure.Reason)
	require.Empty(t, writer.Body.String())
	require.Equal(t, model.OperationRetryPlanning, nativeHTTPFromContext(ctx).core.Snapshot().State)
	store := svc.cache.(*cloudForwardCache).OpenAICloudExecutionStore.(*cloudExecutionTestCache)
	store.execMu.Lock()
	require.Len(t, store.entries, 1)
	command, state := "", ""
	for id, entry := range store.entries {
		command, state = id, entry.state
	}
	store.execMu.Unlock()
	require.Equal(t, "reclaimed", state, "gateway recovery must precede normal disposal's cancellation tombstone")
	closed, err := store.executionSessions().CloudRequestClosed(ctx, command)
	require.NoError(t, err)
	require.True(t, closed, "normal disposal still happens after the handback")
}

func TestCloudExecutionSilentGenerationStopsOnTombstoneOrActualCapacityLoss(t *testing.T) {
	for _, reason := range []string{"cancel_without_command", "capacity_member_lost"} {
		t.Run(reason, func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.User.Concurrency, in.Account.Concurrency = 1, 1
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			body := []byte(`{"model":"client-model","input":"wait without output"}`)
			request, c, _ := cloudContractContext(t, svc, in, ctx, body)
			done := make(chan error, 1)
			go func() { _, err := svc.Forward(request, c, in.Account, body); done <- err }()
			cloudWireStart(t, ctx, wire)
			require.Eventually(t, func() bool { record := store.snapshot(); return record != nil && record.TurnID == "turn" }, time.Second, time.Millisecond)
			if reason == "cancel_without_command" {
				execStore := svc.cache.(*cloudForwardCache).OpenAICloudExecutionStore.(*cloudExecutionTestCache)
				execStore.execMu.Lock()
				command := ""
				for id := range execStore.entries {
					command = id
				}
				execStore.execMu.Unlock()
				require.NotEmpty(t, command)
				// Deliberately do not enqueue Cancel. The owner must observe the
				// authoritative tombstone and wake its otherwise idle event loop.
				require.NoError(t, store.DeleteCloudEvents(ctx, command))
			} else {
				lease := routing.CapacityScopeFrom(request).Lookup(false, in.Account.ID).Lease
				require.NotNil(t, lease)
				capacity := svc.concurrencyService.cache.(*cloudExecutionCapacity)
				require.NoError(t, capacity.ReleaseAccountSlot(ctx, in.Account.ID, lease.Snapshot().Identity.Token))
			}
			interrupt := cloudTestMessage(t, ctx, wire)
			require.Equal(t, "turn/interrupt", interrupt.Method)
			cloudTestRead(wire, map[string]any{"id": interrupt.ID, "result": map[string]any{}})
			cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
			require.Eventually(t, func() bool { return store.settlementCount() > 0 }, time.Second, time.Millisecond)
			select {
			case resultErr := <-done:
				if reason == "cancel_without_command" {
					require.ErrorIs(t, resultErr, context.Canceled)
				}
			case <-ctx.Done():
				t.Fatal("operation did not terminate after interrupt")
			}
			require.NoError(t, ctx.Err(), "the live subscriber must notice its tombstone without canceling the caller context")
			snapshot := nativeHTTPFromContext(request).core.Snapshot()
			require.Equal(t, model.OperationCanceled, snapshot.State)
			require.Equal(t, 1, nativeHTTPFromContext(request).dispatch.budget.attemptBudget.Snapshot().Attempts)
			require.Equal(t, model.BillingPendingUsage, snapshot.Billing.Disposition)
			require.True(t, snapshot.Billing.Durable, "the imported receipt must come from the actual saved owner result")
			for _, attempt := range snapshot.Attempts {
				if attempt.Identity.Purpose == model.DispatchGeneration {
					require.Nil(t, attempt.Usage, "an interrupted turn without measurement must stay unknown")
				}
			}
			require.Error(t, TakeOpenAITurnAttempt(request), "an interrupted/lost owner cannot grant another source send")
		})
	}
}

type cloudClosedReturnReader struct {
	OpenAICloudExecutionStore
	started chan struct{}
	loads   int
	claims  int
}

func (r *cloudClosedReturnReader) LoadCloudExecutionReturn(ctx context.Context, session, command string, digest [32]byte) (*execution.OperationReturn, error) {
	r.loads++
	if r.loads == 1 && r.started != nil {
		close(r.started)
		return nil, nil
	}
	return r.OpenAICloudExecutionStore.LoadCloudExecutionReturn(ctx, session, command, digest)
}
func (r *cloudClosedReturnReader) ClaimCloudExecutionReturn(context.Context, string, string, [32]byte, [32]byte) error {
	r.claims++
	return errors.New("canceled subscriber must never reclaim execution")
}

func TestCloudExecutionClosedSubscriberImportsOnlyPersistedReturn(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.execution.stopMonitor()
	require.NoError(t, f.cache.DeleteCloudEvents(f.ctx, f.command.RequestID))
	reader := &cloudClosedReturnReader{OpenAICloudExecutionStore: f.cache, started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- f.source.completeClosedRequest(f.ctx, reader, "session", *f.command.Execution)
	}()
	select {
	case <-reader.started:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not start reading the persisted return")
	}
	f.a.execution.controller.Cancel(context.Canceled)
	returned, err := f.a.execution.finish(OpenAICloudStreamEvent{Error: "request canceled before generation", NotStarted: true})
	require.NoError(t, err)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("subscriber did not import the saved return")
	}
	require.Zero(t, reader.claims, "even a proved-unsent return cannot authorize replay after a tombstone")
	require.Equal(t, returned.Result.State, f.source.core.Snapshot().State)
	require.Equal(t, model.OperationCanceled, f.source.core.Snapshot().State)
	require.False(t, f.source.transferred)
	require.Error(t, TakeOpenAITurnAttempt(f.ctx))
}

func TestCloudExecutionClosedSubscriberMissingReturnStaysFrozen(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.execution.stopMonitor()
	require.NoError(t, f.cache.DeleteCloudEvents(f.ctx, f.command.RequestID))
	reader := &cloudClosedReturnReader{OpenAICloudExecutionStore: f.cache}
	ctx, cancel := context.WithTimeout(f.ctx, 50*time.Millisecond)
	defer cancel()
	err := f.source.completeClosedRequest(ctx, reader, "session", *f.command.Execution)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorIs(t, err, execution.ErrOperationTransferred)
	require.Zero(t, reader.claims)
	require.True(t, f.source.transferred)
	require.NotEqual(t, model.OperationCompleted, f.source.core.Snapshot().State)
	require.False(t, f.source.core.Snapshot().Billing.Durable)
	require.Error(t, TakeOpenAITurnAttempt(f.ctx))
}

func TestCloudExecutionCachedReplayRevalidatesCurrentPrincipal(t *testing.T) {
	for _, change := range []string{"key", "group"} {
		t.Run(change, func(t *testing.T) {
			f := newCloudExecutionFixture(t)
			f.a.execution.stopMonitor()
			if change == "key" {
				f.keys.key.Status = StatusDisabled
			} else {
				f.keys.key.Group.Status = StatusDisabled
			}
			cached := &OpenAICloudResult{Replayed: true, Response: json.RawMessage(`{"id":"cached","status":"completed","output":[]}`)}
			require.Error(t, f.a.replayCompletedResponse(f.command.RequestID, cached))
			require.Zero(t, f.a.execution.controller.budget.attemptBudget.Snapshot().Attempts)
			f.cache.mu.Lock()
			events := len(f.cache.events[f.command.RequestID])
			f.cache.mu.Unlock()
			require.Zero(t, events, "cached business data cannot bypass current key/group admission")
		})
	}
}

func TestCloudExecutionPublicCachedReplayRejectsRevokedKeyOrGroup(t *testing.T) {
	for _, change := range []string{"key", "group"} {
		t.Run(change, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			wire, history, completedID := h.establish()
			var output any
			for _, raw := range history {
				if item, ok := raw.(map[string]any); ok && item["type"] == "function_call_output" {
					output = item
					break
				}
			}
			require.NotNil(t, output)
			if change == "key" {
				h.in.APIKey.Status = StatusDisabled
			} else {
				h.in.APIKey.Group.Status = StatusDisabled
			}
			response, err := h.invoke(map[string]any{"model": "client-model", "input": []any{output}, "tools": cloudHistoryTools})
			require.Error(t, err)
			require.NotEqual(t, completedID, response.Get("id").String())
			require.NotContains(t, response.Raw, `"text":"ok"`, "revoked callers must not receive the cached business response")
			select {
			case raw := <-wire.writes:
				t.Fatalf("revoked cached request dispatched: %s", raw)
			default:
			}
		})
	}
}

func TestCloudExecutionClaimRejectsRevokedPrincipalBeforeTakingCommand(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.execution.stopMonitor()
	f.keys.key.Status = StatusDisabled
	gate := f.a.execution.authority
	f.cache.execMu.Lock()
	f.cache.entries[f.command.RequestID].state = "offered"
	f.cache.execMu.Unlock()
	_, err := gate.ClaimResponseModeOperation(f.ctx, *f.command.Execution)
	require.Error(t, err)
	f.cache.execMu.Lock()
	state := f.cache.entries[f.command.RequestID].state
	f.cache.execMu.Unlock()
	require.Equal(t, "offered", state, "a revoked principal cannot claim execution even when no RPC follows")
}

func TestCloudExecutionOwnedNegativeRepliesSurviveCommitAndStayNonGenerating(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.execution.controller.budget.firstOutput.Commit()
	f.a.record.ThreadID, f.a.record.TurnID = "thread", "turn"
	event := cloudRPCMessage{ID: json.RawMessage(`"server-request"`), Method: "account/chatgptAuthTokens/refresh", Params: json.RawMessage(`{"reason":"unauthorized"}`)}
	require.NoError(t, f.a.handleEvent(event))
	select {
	case raw := <-f.wire.writes:
		require.Equal(t, "server-request", gjson.GetBytes(raw, "id").String())
		require.EqualValues(t, -32601, gjson.GetBytes(raw, "error.code").Int())
		method, err := cloudRPCFrameMethod(raw)
		require.NoError(t, err)
		require.Equal(t, "server/reject", method)
	case <-time.After(time.Second):
		t.Fatal("negative server reply was blocked after output commitment")
	}
	require.Zero(t, f.a.execution.controller.budget.attemptBudget.Snapshot().Attempts)
	event.Params = json.RawMessage(`{"threadId":"another-thread","turnId":"turn"}`)
	require.ErrorIs(t, f.a.declineCloudServerRequest(event), execution.ErrInvalidPreparedIdentity)
	select {
	case raw := <-f.wire.writes:
		t.Fatalf("foreign server request was answered: %s", raw)
	default:
	}
}
