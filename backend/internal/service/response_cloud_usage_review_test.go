//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func cloudUsageReviewStart(t *testing.T) *cloudExecutionFixture {
	t.Helper()
	f := newCloudExecutionFixture(t)
	done := make(chan error, 1)
	go func() { done <- f.a.handleCommand(f.command) }()
	cloudWireResponse(t, f.ctx, f.wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
	cloudWireResponse(t, f.ctx, f.wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
	require.NoError(t, <-done)
	require.NotNil(t, f.a.execution.active)
	require.Nil(t, f.a.execution.baseline)
	require.False(t, f.a.execution.baselineUnknown)
	return f
}

func cloudUsageReviewObserve(t *testing.T, f *cloudExecutionFixture, input, output int) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{
		"inputTokens": input, "outputTokens": output, "cachedInputTokens": 0, "cacheWriteInputTokens": 0,
		"reasoningOutputTokens": 0, "totalTokens": input + output,
	}}})
	require.NoError(t, err)
	event := cloudRPCMessage{Method: "thread/tokenUsage/updated", Params: raw}
	require.NoError(t, f.a.handleEvent(event))
	// The live actor observes the accepted record immediately after handleEvent.
	f.a.execution.observeCurrentUsage(&event)
}

func cloudUsageReviewComplete(t *testing.T, f *cloudExecutionFixture, total int) []model.AttemptFact {
	t.Helper()
	terminal := cloudRPCMessage{Method: "turn/completed", Params: json.RawMessage(`{"threadId":"thread","turn":{"id":"turn","status":"completed"}}`)}
	require.NoError(t, f.a.handleEvent(terminal))
	e := f.a.execution
	require.NotNil(t, e.returned, "terminal actor publication must export the actual core facts")
	require.Equal(t, 1, f.cache.settlementCount(), "attempt attribution must not create another monetary root")
	settlement := f.cache.settlementFor("original-root")
	require.NotNil(t, settlement)
	require.NotNil(t, settlement.Usage)
	require.Equal(t, total, settlement.Usage.TotalTokens)
	require.Equal(t, f.command.Request.BillingSnapshot, settlement.BillingSnapshot)
	require.False(t, settlement.Unknown)
	require.Equal(t, model.BillingUsageRecorded, e.returned.Result.Billing.Disposition)
	require.NotEqual(t, model.BillingSettled, e.returned.Result.Billing.Disposition)
	var generations []model.AttemptFact
	for _, attempt := range e.returned.Result.Attempts {
		if attempt.Identity.Purpose == model.DispatchGeneration {
			require.Equal(t, "original-root", attempt.Identity.Binding.BillingRoot)
			require.Equal(t, "original-root", attempt.BillingRoot)
			if attempt.Usage != nil {
				require.Empty(t, attempt.Usage.ReturnedModel, "a selected owner model is not retained upstream-model evidence")
			}
			generations = append(generations, attempt)
		}
	}
	require.Equal(t, "gpt-5.1", gjson.GetBytes(f.a.record.Result.Response, "model").String(), "public response projection remains unchanged")
	require.NoError(t, f.a.handleEvent(terminal))
	require.Equal(t, 1, f.cache.settlementCount(), "duplicate terminal does not repeat root settlement")
	return generations
}

func TestCloudUsageReviewUnknownFirstResumeCannotClaimCumulativeFinal(t *testing.T) {
	f := cloudUsageReviewStart(t)
	require.Nil(t, f.a.record.Usage)
	firstSequence := f.a.execution.active.Sequence()
	// A real summary-tool decline replies on the existing RPC connection and
	// resumes inference before the first sampling usage event has arrived.
	require.NoError(t, f.a.declineCompactionTool(json.RawMessage(`"summary-tool"`)))
	select {
	case reply := <-f.wire.writes:
		require.Equal(t, "summary-tool", gjson.GetBytes(reply, "id").String())
		require.True(t, gjson.GetBytes(reply, "result.success").Exists())
		require.False(t, gjson.GetBytes(reply, "result.success").Bool())
	case <-f.ctx.Done():
		t.Fatal("summary-tool reply did not reach the wire")
	}
	require.Greater(t, f.a.execution.active.Sequence(), firstSequence)
	require.Nil(t, f.a.execution.baseline)
	require.True(t, f.a.execution.baselineUnknown)
	cloudUsageReviewObserve(t, f, 16, 4)
	for _, attempt := range f.a.execution.controller.Snapshot().Attempts {
		if attempt.Identity.Purpose == model.DispatchGeneration {
			require.Nil(t, attempt.Usage, "a later root total cannot retroactively establish the replacement's baseline")
		}
	}
	// The direct conversion must also refuse to turn optional zero fields into
	// measured zero for an attempt whose cumulative baseline is unknown.
	f.a.execution.cacheWriteObserved, f.a.execution.reasoningObserved = true, true
	require.Nil(t, f.a.execution.usageFact(f.a.record.Usage, model.UsageFinal, true))
	attempts := cloudUsageReviewComplete(t, f, 20)
	require.Len(t, attempts, 2)
	require.Equal(t, model.AttemptResume, attempts[1].Identity.Role)
	require.Nil(t, attempts[0].Usage)
	require.Nil(t, attempts[1].Usage, "the root may be final while both attempt attributions remain unknown")
}

func TestCloudUsageReviewKnownTwelveThenTwentyAttributesEight(t *testing.T) {
	f := cloudUsageReviewStart(t)
	cloudUsageReviewObserve(t, f, 10, 2)
	require.NoError(t, f.a.declineCompactionTool(json.RawMessage(`"summary-tool"`)))
	_ = cloudTestMessage(t, f.ctx, f.wire)
	require.False(t, f.a.execution.baselineUnknown)
	require.Equal(t, 12, f.a.execution.baseline.TotalTokens)
	cloudUsageReviewObserve(t, f, 16, 4)
	attempts := cloudUsageReviewComplete(t, f, 20)
	require.Len(t, attempts, 2)
	require.NotNil(t, attempts[0].Usage)
	require.EqualValues(t, 12, attempts[0].Usage.Total.Value)
	require.NotNil(t, attempts[1].Usage)
	require.EqualValues(t, 6, attempts[1].Usage.Input.Value)
	require.EqualValues(t, 2, attempts[1].Usage.Output.Value)
	require.EqualValues(t, 8, attempts[1].Usage.Total.Value)
	require.Equal(t, model.UsageFinal, attempts[1].Usage.Confidence)
	require.True(t, attempts[1].Usage.TerminalConfirmed)
	require.Nil(t, attempts[1].Usage.CacheWrite, "the normalized baseline does not carry optional-field presence")
	require.Nil(t, attempts[1].Usage.Reasoning)
}

func TestCloudUsageReviewFreshRootPreservesMeasuredZero(t *testing.T) {
	f := cloudUsageReviewStart(t)
	cloudUsageReviewObserve(t, f, 0, 0)
	attempts := cloudUsageReviewComplete(t, f, 0)
	require.Len(t, attempts, 1)
	usage := attempts[0].Usage
	require.NotNil(t, usage)
	require.Equal(t, model.UsageFinal, usage.Confidence)
	require.True(t, usage.TerminalConfirmed)
	for _, count := range []*model.TokenMeasurement{usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite, usage.Reasoning, usage.Total} {
		require.NotNil(t, count, "fresh-root initial zero plus an explicit observed counter is known")
		require.Zero(t, count.Value)
	}
}
