//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A turn that the upstream confirms ended early (interrupted or failed) has
// reported the usage of every completed sampling step. Like the native gateway
// bills a response.incomplete/response.failed usage block, that measured usage
// is billed; the discarded in-flight step is not estimated. Only a turn whose
// end was never confirmed stays unknown.
func TestCloudConfirmedEarlyEndSettlesMeasuredUsageAsLowerBound(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		turn       map[string]any
		withUsage  bool
		status     string
		lowerBound bool
	}{
		{name: "interrupted with usage", turn: map[string]any{"id": "turn", "status": "interrupted"}, withUsage: true, status: "interrupted", lowerBound: true},
		{name: "failed with usage", turn: map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"message": "upstream gave up"}}, withUsage: true, status: "failed", lowerBound: true},
		{name: "interrupted without usage", turn: map[string]any{"id": "turn", "status": "interrupted"}, status: "interrupted"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a, store, _ := cloudHostedActorForTest(t)
			if !scenario.withUsage {
				a.record.Usage = nil
			}
			measured := a.record.Usage
			require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": scenario.turn}, ""))
			require.Equal(t, scenario.status, a.record.Status)
			require.Len(t, store.settlements, 1)
			settlement := store.settlements[0]
			require.Equal(t, scenario.status, settlement.Status)
			require.Equal(t, scenario.lowerBound, settlement.LowerBound)
			require.Equal(t, !scenario.lowerBound, settlement.Unknown, "confirmed early end with measured usage is billable; without usage it stays unknown")
			require.Equal(t, measured, settlement.Usage, "the measured usage is preserved as reported")
			require.NotNil(t, store.record.Result)
			require.False(t, store.record.Result.UsageCredible, "a lower bound is not a completed turn's final usage for health, probes or replay")
			failure := gjson.GetBytes(store.record.Result.Response, "error")
			require.Equal(t, scenario.status, failure.Get("upstream_turn_status").String())
			require.Equal(t, "turn/completed", failure.Get("upstream_method").String())
			require.Equal(t, scenario.withUsage, failure.Get("usage_observed").Bool())
			facts := cloudDiagnosticFailure(string(store.record.Result.Response))
			require.Equal(t, scenario.status, facts["upstream_turn_status"])
			raw, err := json.Marshal(settlement)
			require.NoError(t, err)
			require.Equal(t, scenario.lowerBound, gjson.GetBytes(raw, "lower_bound").Bool(), "the marker must survive the Redis JSON round trip")
		})
	}
}

// Losing the connection or routing never confirms how the upstream turn ended,
// so its watermark remains unknown and unbilled even when usage was measured.
func TestCloudUnconfirmedEndKeepsMeasuredUsageUnknown(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.rpc = nil // no interrupt can be sent, exactly like a lost socket
	a.stop("Cloud connection or routing was lost; execution will not be replayed")
	require.Equal(t, "unknown", a.record.Status)
	require.Len(t, store.settlements, 1)
	require.True(t, store.settlements[0].Unknown)
	require.False(t, store.settlements[0].LowerBound)
	require.Equal(t, 12, store.settlements[0].Usage.TotalTokens, "the last watermark is retained as a fact without being billed")
}

func TestCloudTerminalDiagnosticsPreserveLocalCancellation(t *testing.T) {
	a, store, _ := cloudHostedActorForTest(t)
	a.publicFailure = map[string]any{"gateway_stop_reason": "client_cancelled"}
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "interrupted"}}, ""))
	facts := cloudDiagnosticFailure(string(store.record.Result.Response))
	require.Equal(t, "client_cancelled", facts["gateway_stop_reason"])
	require.Equal(t, "interrupted", facts["upstream_turn_status"])
	payload, status := cloudTerminalFailureFacts(store.record.Result)
	require.Empty(t, payload)
	require.Zero(t, status)
	require.Len(t, store.settlements, 1)
}

func TestCloudBillingWorkerSettlesLowerBoundAndMarksUsageLog(t *testing.T) {
	svc, input, repo := cloudBillingFixture(t)
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	store := &cloudBillingStoreStub{}
	worker := &openAICloudBillingWorker{service: svc, store: store, id: "worker", holds: make(map[string]openAICloudHeldReservation)}
	owner := OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}
	usage := &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}
	roundTrip := func(settlement *OpenAICloudSettlement) *OpenAICloudSettlement {
		raw, err := json.Marshal(settlement)
		require.NoError(t, err)
		var persisted OpenAICloudSettlement
		require.NoError(t, json.Unmarshal(raw, &persisted))
		return &persisted
	}
	interrupted := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "interrupted-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "interrupted", Usage: usage, LowerBound: true, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{interrupted}
	worker.consume(context.Background())
	require.Equal(t, 1, repo.calls, "a confirmed interrupt bills its measured usage")
	require.Equal(t, 1, store.acks)
	require.Zero(t, store.retained)
	require.NotNil(t, repo.lastCmd.UsageLog)
	require.True(t, repo.lastCmd.UsageLog.UsageLowerBound)
	require.Equal(t, "lower_bound", SettledUsageBillingStatus(repo.lastCmd.UsageLog), "the usage row is marked as a lower bound, not an ordinary settlement")
	require.Equal(t, 10, repo.lastCmd.UsageLog.InputTokens)
	require.Equal(t, 2, repo.lastCmd.UsageLog.OutputTokens)
	require.InDelta(t, 0.000056, repo.lastCmd.BalanceCost, 1e-12, "the lower bound is priced exactly like confirmed consumption")

	completed := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "completed-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: usage, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{completed}
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls)
	require.False(t, repo.lastCmd.UsageLog.UsageLowerBound)
	require.Equal(t, "settled", SettledUsageBillingStatus(repo.lastCmd.UsageLog), "completed turns keep the ordinary settlement status")

	contradictory := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "contradictory-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: usage, LowerBound: true, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{contradictory}
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls, "a completed turn cannot also be a lower bound; the fact is archived instead of billed")
	require.Equal(t, 1, store.retained)
	require.Equal(t, "invalid_settlement", svc.usageBillingRepo.(*cloudBillingArchiveRepoStub).reasons["contradictory-root"])

	unknown := roundTrip(&OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, RootRequestID: "unknown-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "unknown", Usage: usage, Unknown: true, ObservedAt: time.Now().Add(-cloudLatencyGrace)})
	store.pending = []*OpenAICloudSettlement{unknown}
	worker.consume(context.Background())
	require.Equal(t, 2, repo.calls, "an unconfirmed end is still never billed")
	require.Equal(t, "unknown_usage", svc.usageBillingRepo.(*cloudBillingArchiveRepoStub).reasons["unknown-root"])
}

func TestSettledUsageBillingStatusMarksLowerBound(t *testing.T) {
	require.Equal(t, "settled", SettledUsageBillingStatus(&UsageLog{ActualCost: 0.01}))
	require.Equal(t, "free", SettledUsageBillingStatus(&UsageLog{}))
	require.Equal(t, "lower_bound", SettledUsageBillingStatus(&UsageLog{ActualCost: 0.01, UsageLowerBound: true}))
	require.Equal(t, "lower_bound", SettledUsageBillingStatus(&UsageLog{UsageLowerBound: true}), "a free lower bound still tells the reader the usage is partial")
	require.Equal(t, "lower_bound", UsageBillingStatus(&UsageLog{BillingStatus: "lower_bound"}))
}

func (s *cloudLiveRoutingStore) settlementFor(root string) *OpenAICloudSettlement {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, settle := range s.settlements {
		if settle.RootRequestID == root {
			return settle
		}
	}
	return nil
}

// The client leaves mid-turn; the gateway interrupts the upstream and the
// upstream confirms the interruption after reporting the completed sampling
// steps. That confirmed usage is settled once, as a lower bound.
func TestCloudClientCancellationBillsConfirmedInterruptAsLowerBound(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go func() {
		cloudWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "answer", "type": "agentMessage"}}})
		cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "answer", "delta": "partial"}})
		for {
			select {
			case raw := <-wire.writes:
				var msg cloudRPCMessage
				if json.Unmarshal(raw, &msg) != nil || msg.Method != "turn/interrupt" {
					continue
				}
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
				cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12}}}})
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	requestCtx, err = svc.FinalizeCloudBillingSnapshot(requestCtx, "gpt-5.1", "", "")
	require.NoError(t, err)
	clientCtx, disconnect := context.WithCancel(requestCtx)
	defer disconnect()
	owner := OpenAICloudOwner{UserID: in.User.ID, APIKeyID: in.APIKey.ID, GroupID: *in.APIKey.GroupID, AccountID: in.Account.ID, GrantGeneration: "grant", Model: "gpt-5.1", SettingsDigest: cloudSettingsDigest(in.Account)}
	_, err = svc.cloudSessions.Run(clientCtx, OpenAICloudRequest{Owner: owner, Body: json.RawMessage(`{"model":"gpt-5.1","input":"hi"}`), RootRequestID: "cloud_root_lower_bound", BillingSnapshot: CloudBillingSnapshotFromContext(requestCtx)}, func(event json.RawMessage) error {
		if gjson.GetBytes(event, "type").String() == "response.output_text.delta" {
			disconnect()
		}
		return nil
	})
	require.Error(t, err)
	require.Eventually(t, func() bool { return store.settlementFor("cloud_root_lower_bound") != nil }, 5*time.Second, 10*time.Millisecond)
	settlement := store.settlementFor("cloud_root_lower_bound")
	require.Equal(t, "interrupted", settlement.Status)
	require.False(t, settlement.Unknown)
	require.True(t, settlement.LowerBound)
	require.Equal(t, &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}, settlement.Usage)
	require.Equal(t, 1, store.settlementCount(), "one confirmed interrupt settles exactly once")
}

// App Server reports one usage total per sampling step, and only after that
// step's client tools return; these feed a scripted turn in that order.
func cloudStepUsage(wire *cloudRPCFakeConn, turn string, input, output int) {
	cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": turn, "tokenUsage": map[string]any{
		"total": map[string]any{"inputTokens": input, "outputTokens": output, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": input + output}}}})
}

func cloudStepToolCall(wire *cloudRPCFakeConn, turn, rpc string) {
	cloudTestRead(wire, map[string]any{"id": rpc, "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": turn, "callId": "upstream-" + rpc, "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}})
}

func cloudStepAnswer(wire *cloudRPCFakeConn, turn, text string) {
	item := "answer-" + turn
	cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": turn, "item": map[string]any{"id": item, "type": "agentMessage"}}})
	cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": turn, "itemId": item, "delta": text}})
	cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": turn, "item": map[string]any{"id": item, "type": "agentMessage", "text": text}}})
}

func cloudStepTurnCompleted(wire *cloudRPCFakeConn, turn string) {
	cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": turn, "status": "completed"}}})
}

func cloudStepReplied(ctx context.Context, wire *cloudRPCFakeConn, rpc string) error {
	reply, err := cloudExpect(ctx, wire, "")
	if err != nil {
		return err
	}
	return cloudCheck(string(reply.ID) == `"`+rpc+`"`, "replied to %s, want %s", reply.ID, rpc)
}

func cloudStepStart(ctx context.Context, h *cloudHistoryRoutingHarness) (*cloudRPCFakeConn, error) {
	wire, err := cloudScriptWire(ctx, h)
	if err != nil {
		return nil, err
	}
	if _, err = cloudExpectReply(ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}); err != nil {
		return nil, err
	}
	_, err = cloudExpectReply(ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
	return wire, err
}

func cloudRequireStepUsage(t *testing.T, response gjson.Result, input, output int, msg string) {
	t.Helper()
	require.Equal(t, int64(input), response.Get("usage.input_tokens").Int(), msg+": "+response.Raw)
	require.Equal(t, int64(output), response.Get("usage.output_tokens").Int(), msg+": "+response.Raw)
	require.Equal(t, int64(input+output), response.Get("usage.total_tokens").Int(), msg+": "+response.Raw)
}

func cloudStepRequest(previous gjson.Result, item map[string]any) map[string]any {
	body := map[string]any{"model": "client-model", "input": []any{item}, "tools": cloudHistoryTools, "parallel_tool_calls": false}
	if previous.Exists() {
		body["previous_response_id"] = previous.Get("id").String()
	}
	return body
}

func cloudStepOutput(call gjson.Result) map[string]any {
	return map[string]any{"type": "function_call_output", "call_id": call.Get("output.0.call_id").String(), "output": "ok"}
}

func cloudStepUser(text string) map[string]any {
	return map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}
}

// Codex takes a response's usage as one sampling step: its total_tokens is the
// context Codex compacts by. A Cloud turn runs several steps under one billing
// root, and each step's usage arrives only after its tool results. A tool-wait
// response therefore reports the step confirmed since the previous response,
// the final response reports the last step, and the root settles the whole
// turn once. Reporting the turn's sum instead made Codex compact far too early.
func TestCloudPublicUsageReportsSamplingSteps(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	var wire *cloudRPCFakeConn
	script := runCloudScript(func() (err error) {
		if wire, err = cloudStepStart(h.ctx, h); err != nil {
			return err
		}
		cloudStepToolCall(wire, "turn", "rpc-1")
		return nil
	})
	first, err := h.invoke(cloudStepRequest(gjson.Result{}, cloudStepUser("hello")))
	require.NoError(t, err, first.Raw)
	script.join(t)
	require.Equal(t, "function_call", first.Get("output.0.type").String(), first.Raw)
	require.Equal(t, gjson.Null, first.Get("usage").Type, "no step is confirmed before the first tool call: "+first.Raw)
	// invoke has finished its ordinary request-slot lifecycle. The Cloud actor
	// still owns the upstream tool wait and must accept the next HTTP request.
	h.svc.cloudSessions.mu.Lock()
	actors := make([]*cloudSessionActor, 0, len(h.svc.cloudSessions.actors))
	for _, actor := range h.svc.cloudSessions.actors {
		actors = append(actors, actor)
	}
	h.svc.cloudSessions.mu.Unlock()
	require.NotEmpty(t, actors)
	for _, actor := range actors {
		require.NoError(t, actor.ctx.Err(), "HTTP slot cleanup must not cancel the Cloud owner during a tool wait")
	}

	// Step 1 is confirmed after its result. Step 2 calls two tools, which a
	// client without parallel calls receives one per response.
	script = runCloudScript(func() error {
		if err := cloudStepReplied(h.ctx, wire, "rpc-1"); err != nil {
			return err
		}
		cloudStepUsage(wire, "turn", 100, 10)
		cloudStepUsage(wire, "turn", 100, 10) // an unchanged total confirms no step
		cloudStepToolCall(wire, "turn", "rpc-2")
		cloudStepToolCall(wire, "turn", "rpc-3")
		return nil
	})
	second, err := h.invoke(cloudStepRequest(first, cloudStepOutput(first)))
	require.NoError(t, err, second.Raw)
	script.join(t)
	require.Equal(t, "function_call", second.Get("output.0.type").String(), second.Raw)
	cloudRequireStepUsage(t, second, 100, 10, "a tool-wait response reports the step confirmed since the previous response")

	script = runCloudScript(func() error { return cloudStepReplied(h.ctx, wire, "rpc-2") })
	third, err := h.invoke(cloudStepRequest(second, cloudStepOutput(second)))
	require.NoError(t, err, third.Raw)
	script.join(t)
	require.Equal(t, "function_call", third.Get("output.0.type").String(), third.Raw)
	require.Equal(t, gjson.Null, third.Get("usage").Type, "no step was confirmed since the previous response: "+third.Raw)

	script = runCloudScript(func() error {
		if err := cloudStepReplied(h.ctx, wire, "rpc-3"); err != nil {
			return err
		}
		cloudStepUsage(wire, "turn", 230, 22)
		cloudStepAnswer(wire, "turn", "done")
		cloudStepUsage(wire, "turn", 380, 37)
		cloudStepTurnCompleted(wire, "turn")
		return nil
	})
	final, err := h.invoke(cloudStepRequest(third, cloudStepOutput(third)))
	require.NoError(t, err, final.Raw)
	script.join(t)
	require.Equal(t, "completed", final.Get("status").String(), final.Raw)
	cloudRequireStepUsage(t, final, 150, 15, "the final response reports its last step, the context Codex compacts by")
	session := h.store.all()[0]
	record := session.snapshot()
	require.Equal(t, 1, h.store.settlementCount())
	settlement := session.snapshotSettlement(record.RootRequestID)
	require.NotNil(t, settlement)
	require.Equal(t, "completed", settlement.Status)
	require.False(t, settlement.Unknown || settlement.LowerBound)
	require.Equal(t, []int{380, 37, 417}, []int{settlement.Usage.InputTokens, settlement.Usage.OutputTokens, settlement.Usage.TotalTokens}, "the root still settles the whole turn once")
	require.Equal(t, 417, record.Result.Usage.TotalTokens, "health, probes and recovery keep the whole turn's usage")
	_, proven := cloudRecoveryCompletedProof(record)
	require.True(t, proven, "a lost settlement is still rebuilt from the published final response")

	// The next turn's step is measured from the thread total, not the turn.
	script = runCloudScript(func() error {
		if _, err := cloudExpectReply(h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}}); err != nil {
			return err
		}
		cloudStepAnswer(wire, "turn-two", "again")
		cloudStepUsage(wire, "turn-two", 580, 57)
		cloudStepTurnCompleted(wire, "turn-two")
		return nil
	})
	next, err := h.invoke(cloudStepRequest(final, cloudStepUser("next")))
	require.NoError(t, err, next.Raw)
	script.join(t)
	require.Equal(t, "completed", next.Get("status").String(), next.Raw)
	cloudRequireStepUsage(t, next, 200, 20, "a single-step turn reports that step")
	require.Equal(t, 2, h.store.settlementCount())
}

func TestCloudFailedTerminalDoesNotRepublishConfirmedSamplingUsage(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	var wire *cloudRPCFakeConn
	script := runCloudScript(func() (err error) {
		if wire, err = cloudStepStart(h.ctx, h); err != nil {
			return err
		}
		cloudStepToolCall(wire, "turn", "rpc-1")
		return nil
	})
	first, err := h.invoke(cloudStepRequest(gjson.Result{}, cloudStepUser("hello")))
	require.NoError(t, err, first.Raw)
	script.join(t)
	require.Equal(t, gjson.Null, first.Get("usage").Type, first.Raw)

	script = runCloudScript(func() error {
		if err := cloudStepReplied(h.ctx, wire, "rpc-1"); err != nil {
			return err
		}
		cloudStepUsage(wire, "turn", 100, 10)
		cloudStepToolCall(wire, "turn", "rpc-2")
		return nil
	})
	second, err := h.invoke(cloudStepRequest(first, cloudStepOutput(first)))
	require.NoError(t, err, second.Raw)
	script.join(t)
	cloudRequireStepUsage(t, second, 100, 10, "the confirmed step is published while the next tool waits")
	session := h.store.all()[0]
	root := session.snapshot().RootRequestID
	require.Zero(t, h.store.settlementCount(), "tool waiting does not settle the active root")

	script = runCloudScript(func() error {
		if err := cloudStepReplied(h.ctx, wire, "rpc-2"); err != nil {
			return err
		}
		// A repeated watermark confirms no new sampling before the failure.
		cloudStepUsage(wire, "turn", 100, 10)
		cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": "serverOverloaded"}}}})
		return nil
	})
	final, err := h.invoke(cloudStepRequest(second, cloudStepOutput(second)))
	require.NoError(t, err, final.Raw)
	script.join(t)
	require.Equal(t, "failed", final.Get("status").String(), final.Raw)

	record := session.snapshot()
	require.Equal(t, root, record.RootRequestID, "the failed continuation retains the original billing root")
	require.Equal(t, 1, h.store.settlementCount())
	settlement := session.snapshotSettlement(root)
	require.NotNil(t, settlement)
	require.Equal(t, "failed", settlement.Status)
	require.True(t, settlement.LowerBound)
	require.False(t, settlement.Unknown)
	measured := OpenAICloudUsage{InputTokens: 100, OutputTokens: 10, TotalTokens: 110}
	require.Equal(t, &measured, settlement.Usage, "the confirmed work remains billable exactly once")
	require.Equal(t, []OpenAICloudUsage{measured}, settlement.SamplingUsage, "a duplicate watermark is not another sampling step")
	require.Equal(t, &measured, record.Result.Usage, "internal accounting retains the measured root usage")
	require.False(t, record.Result.UsageCredible, "a failed lower bound is not a completed turn")
	require.Equal(t, gjson.Null, final.Get("usage").Type, "the failed response must not expose the step already published in the tool-wait response: "+final.Raw)
}

func TestCloudFailedRecoveredHandoffDoesNotRepublishRootUsage(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	measured := OpenAICloudUsage{InputTokens: 230, CachedInputTokens: 100, OutputTokens: 22, TotalTokens: 252}
	samples := []OpenAICloudUsage{
		{InputTokens: 100, CachedInputTokens: 20, OutputTokens: 10, TotalTokens: 110},
		{InputTokens: 130, CachedInputTokens: 80, OutputTokens: 12, TotalTokens: 142},
	}
	prior := measured
	a.record.Usage, a.previousThreadUsage = &prior, &prior
	a.record.SamplingUsage = append([]OpenAICloudUsage(nil), samples...)
	a.record.ThreadID, a.record.TurnID = "replacement-thread", ""
	a.handoffBeforeInference = true
	// Recovery resets the fresh thread's counters and last step, but the old
	// segment's already published usage and settlement facts remain intact.
	a.threadTotal, a.turnBase, a.lastStep = nil, nil, nil
	a.lastStepPublished = true

	a.terminateError(&cloudGatewayError{400, "cloud_history_rejected", "Cloud rejected the replacement thread's history before inference."}, "Cloud history recovery failed", false)

	require.Len(t, store.settlements, 1)
	settlement := store.settlements[0]
	require.Equal(t, "root", settlement.RootRequestID)
	require.Equal(t, "failed", settlement.Status)
	require.True(t, settlement.LowerBound)
	require.False(t, settlement.Unknown)
	require.Equal(t, &measured, settlement.Usage, "the completed segment remains billable exactly once")
	require.Equal(t, samples, settlement.SamplingUsage, "recovery must not add or discard measured sampling facts")
	require.NotNil(t, store.record.Result)
	require.Equal(t, &measured, store.record.Result.Usage)
	require.False(t, store.record.Result.UsageCredible)
	require.Empty(t, wire.writes, "failure before inference must not replay the completed segment")
	response := gjson.ParseBytes(store.record.Result.Response)
	require.Equal(t, "failed", response.Get("status").String(), response.Raw)
	require.Equal(t, gjson.Null, response.Get("usage").Type, "a cleared last step must not expose already published cumulative root usage: "+response.Raw)
}

// Public usage describes the latest measured model step even after upstream
// compaction. Repeated context reads are billed in the root, not advertised as
// the size of the client's current context to force another compaction.
func TestCloudThreadSelfCompactionSeparatesPublicStepFromRootUsage(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	var wire *cloudRPCFakeConn
	script := runCloudScript(func() (err error) {
		if wire, err = cloudStepStart(h.ctx, h); err != nil {
			return err
		}
		cloudStepToolCall(wire, "turn", "rpc-1")
		return nil
	})
	first, err := h.invoke(cloudStepRequest(gjson.Result{}, cloudStepUser("hello")))
	require.NoError(t, err, first.Raw)
	script.join(t)
	script = runCloudScript(func() error {
		if err := cloudStepReplied(h.ctx, wire, "rpc-1"); err != nil {
			return err
		}
		cloudStepUsage(wire, "turn", 100, 10)
		compaction := map[string]any{"id": "compaction-1", "type": "contextCompaction"}
		cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": compaction}})
		cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": compaction}})
		cloudStepUsage(wire, "turn", 220, 40)
		cloudStepAnswer(wire, "turn", "done")
		cloudStepUsage(wire, "turn", 240, 45)
		cloudStepTurnCompleted(wire, "turn")
		return nil
	})
	final, err := h.invoke(cloudStepRequest(first, cloudStepOutput(first)))
	require.NoError(t, err, final.Raw)
	script.join(t)
	require.Equal(t, "completed", final.Get("status").String(), final.Raw)
	cloudRequireStepUsage(t, final, 20, 5, "upstream compaction must not turn cumulative root usage into current context size")
	session := h.store.all()[0]
	record := session.snapshot()
	require.Equal(t, 285, session.snapshotSettlement(record.RootRequestID).Usage.TotalTokens)
	_, proven := cloudRecoveryCompletedProof(record)
	require.True(t, proven)
}
