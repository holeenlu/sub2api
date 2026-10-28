package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestCloudExecutionKnownPreInferenceReturnRequiresSeparateRecoveryDecision(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.requestID = f.command.RequestID
	f.a.record.ThreadID = "thread"
	done := make(chan error, 1)
	go func() {
		_, err := f.a.rpc.Call(f.ctx, "turn/start", map[string]any{"threadId": "thread", "input": []any{}})
		done <- err
	}()
	message := cloudTestMessage(t, f.ctx, f.wire)
	cloudTestRead(f.wire, map[string]any{"id": message.ID, "error": map[string]any{"code": -32602, "message": "invalid encrypted content", "data": map[string]any{"code": "invalid_encrypted_content"}}})
	require.Error(t, <-done)
	// Feed the actor's existing explicit-rejection decision, not a status-code
	// inference. It remains separate from the amount settlement acknowledgement.
	f.a.rejectedBeforeInference = true
	f.a.record.Usage = &OpenAICloudUsage{}
	f.a.record.Status = "failed"
	f.a.execution.stopMonitor()
	require.NoError(t, f.a.save(nil, nil))
	result := &OpenAICloudResult{RootRequestID: "original-root", SessionID: "session", Terminal: true, RejectedBeforeInference: true, UsageCredible: true, Usage: &OpenAICloudUsage{}, Response: json.RawMessage(`{"status":"failed"}`)}
	returned, err := f.a.execution.finish(OpenAICloudStreamEvent{Result: result})
	require.NoError(t, err)
	require.Equal(t, model.GenerationRejected, returned.Result.Attempts[0].Generation)
	event := OpenAICloudStreamEvent{Result: result, Execution: returned}
	require.NoError(t, f.source.accept(f.ctx, f.cache, "session", *f.command.Execution, event, f.command.Request, false))
	require.ErrorIs(t, TakeOpenAITurnAttempt(f.ctx), execution.ErrOperationTransferred, "a received reply alone must not re-enable source sends")
	require.NoError(t, f.source.recover(f.ctx))
	require.Equal(t, 1, f.source.budget.attemptBudget.Snapshot().Attempts)
	require.Equal(t, model.OperationRetryPlanning, f.source.core.Snapshot().State)
	require.ErrorIs(t, f.source.recover(f.ctx), execution.ErrOperationTransferred)
}

func TestCloudExecutionUncertainAcceptanceReturnsFactsWithoutReplay(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.requestID = f.command.RequestID
	f.a.record.ThreadID = "thread"
	done := make(chan error, 1)
	go func() {
		_, err := f.a.rpc.Call(f.ctx, "turn/start", map[string]any{"threadId": "thread", "input": []any{}})
		done <- err
	}()
	cloudTestMessage(t, f.ctx, f.wire)
	require.NoError(t, f.wire.Close())
	require.Error(t, <-done)
	f.a.uncertain = true
	f.a.record.Status = "unknown"
	f.a.execution.stopMonitor()
	require.NoError(t, f.a.save(nil, nil))
	result := &OpenAICloudResult{RootRequestID: "original-root", SessionID: "session", Terminal: true, Response: json.RawMessage(`{"status":"failed"}`)}
	returned, err := f.a.execution.finish(OpenAICloudStreamEvent{Result: result})
	require.NoError(t, err)
	require.Equal(t, model.GenerationUnknown, returned.Result.Attempts[0].Generation)
	require.Nil(t, returned.Result.Attempts[0].Usage)
	require.NoError(t, f.source.accept(f.ctx, f.cache, "session", *f.command.Execution, OpenAICloudStreamEvent{Result: result, Execution: returned}, f.command.Request, false))
	require.Equal(t, model.OperationFailed, f.source.core.Snapshot().State)
	require.Error(t, f.source.recover(f.ctx))
	require.ErrorIs(t, TakeOpenAITurnAttempt(f.ctx), execution.ErrOperationTransferred)
}

func TestCloudExecutionCanceledGenerationStillAllowsOwnedInterrupt(t *testing.T) {
	f := newCloudExecutionFixture(t)
	f.a.record.ThreadID, f.a.record.TurnID = "thread", "turn"
	f.a.execution.controller.Cancel(errors.New("user canceled"))
	done := make(chan error, 1)
	go func() {
		_, err := f.a.cleanupCloudCall(f.ctx, "turn/interrupt", map[string]any{"threadId": "thread", "turnId": "turn"})
		done <- err
	}()
	cloudWireResponse(t, f.ctx, f.wire, "turn/interrupt", map[string]any{})
	require.NoError(t, <-done)
	require.Zero(t, f.a.execution.controller.budget.attemptBudget.Snapshot().Attempts)
	_, err := f.a.cleanupCloudCall(context.WithoutCancel(f.ctx), "turn/start", map[string]any{"threadId": "thread"})
	require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity)
}

func TestCloudExecutionMissingOptionalUsageDoesNotBecomeMeasuredZero(t *testing.T) {
	f := newCloudExecutionFixture(t)
	e := f.a.execution
	usage, err := cloudParseUsage(json.RawMessage(`{"inputTokens":10,"outputTokens":2,"cachedInputTokens":0,"totalTokens":12}`))
	require.NoError(t, err)
	fact := e.usageFact(usage, model.UsageFinal, true)
	require.NotNil(t, fact.CacheRead)
	require.Zero(t, fact.CacheRead.Value)
	require.Nil(t, fact.CacheWrite)
	require.Nil(t, fact.Reasoning)
	e.cacheWriteObserved, e.reasoningObserved = true, true
	fact = e.usageFact(usage, model.UsageFinal, true)
	require.NotNil(t, fact.CacheWrite)
	require.NotNil(t, fact.Reasoning)
	e.baseline = &OpenAICloudUsage{}
	fact = e.usageFact(usage, model.UsageFinal, true)
	require.Nil(t, fact.CacheWrite, "a legacy normalized baseline does not prove optional-field presence")
}
