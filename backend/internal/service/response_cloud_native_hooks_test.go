package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestCloudExecutionNativeHooksPreserveImportedReceiptAndPublicFailure(t *testing.T) {
	for _, state := range []string{"completed", "failed", "awaiting_tools"} {
		t.Run(state, func(t *testing.T) {
			f := newCloudExecutionFixture(t)
			require.NoError(t, f.a.replyCloudBatch(f.ctx, []json.RawMessage{json.RawMessage(`{"id":"tool","result":{"contentItems":[],"success":true}}`)}))
			f.a.execution.stored, f.a.execution.storedSequence = true, 1
			status := state
			if state == "awaiting_tools" {
				status = "completed"
			}
			result := &OpenAICloudResult{RootRequestID: "original-root", SessionID: "session", Terminal: state != "awaiting_tools", AwaitingTools: state == "awaiting_tools", Response: json.RawMessage(`{"status":"` + status + `"}`)}
			returned, err := f.a.execution.finish(OpenAICloudStreamEvent{Result: result})
			require.NoError(t, err)
			require.NoError(t, f.source.accept(f.ctx, f.cache, "session", *f.command.Execution, OpenAICloudStreamEvent{Result: result, Execution: returned}, f.command.Request, false))
			before := f.source.core.Snapshot()
			var forwardErr error
			if state == "failed" {
				forwardErr = errors.New("real public upstream failure")
			}
			_, err = TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
				return &OpenAIForwardResult{CloudManagedBilling: true, AuthorizationMode: UsageAuthorizationModeCloud}, forwardErr
			})
			require.Equal(t, forwardErr, err)
			TrackNativeHTTPAuthorization(f.ctx, UsageAuthorizationModeCloud)
			owner := nativeHTTPFromContext(f.ctx)
			require.False(t, owner.delegated)
			require.False(t, NativeHTTPFinalizesUsage(f.ctx))
			calls := 0
			port := nativeOperationBillingFunc(func(context.Context, NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
				calls++
				return NativeIntentFinalizationResult{}, errors.New("unexpected native pricing")
			})
			handled, err := finalizeNativeHTTPUsage(f.ctx, nil, port, time.Second)
			require.True(t, handled)
			require.NoError(t, err)
			require.Zero(t, calls)
			require.Equal(t, before.Billing, f.source.core.Snapshot().Billing)
			owner.dispatch.request.Transport = "ws"
			err = completeOpenAIWSTurn(f.ctx, nil, 1, &OpenAIForwardResult{CloudManagedBilling: true}, forwardErr)
			if forwardErr != nil {
				require.ErrorIs(t, err, forwardErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, before.State, f.source.core.Snapshot().State)
			require.Equal(t, before.Billing, f.source.core.Snapshot().Billing)
			require.Zero(t, f.source.core.Snapshot().DelegatedCycles)
		})
	}
}

func TestCloudExecutionNativeHooksLostReplyCannotCompleteSuccessfully(t *testing.T) {
	f := newCloudExecutionFixture(t)
	owner := nativeHTTPFromContext(f.ctx)
	TrackNativeHTTPAuthorization(f.ctx, UsageAuthorizationModeCloud)
	handled, err := finalizeNativeHTTPUsage(f.ctx, nil, nil, time.Second)
	require.True(t, handled)
	require.ErrorIs(t, err, execution.ErrOperationTransferred)
	owner.dispatch.request.Transport = "ws"
	require.ErrorIs(t, completeOpenAIWSTurn(f.ctx, nil, 1, nil, nil), execution.ErrOperationTransferred)
	require.NotEqual(t, model.OperationCompleted, f.source.core.Snapshot().State)
	require.NotEqual(t, ErrNativeOperationBillingUnavailable, owner.finalizeErr)
}

func TestCloudExecutionNativeHooksHandbackOnlyExemptsItsOwnCycle(t *testing.T) {
	f := newCloudExecutionFixture(t)
	returned, err := f.a.execution.finish(OpenAICloudStreamEvent{Error: "history rejected before send", NotStarted: true})
	require.NoError(t, err)
	require.NoError(t, f.source.accept(f.ctx, f.cache, "session", *f.command.Execution, OpenAICloudStreamEvent{Error: "history rejected before send", NotStarted: true, Execution: returned}, f.command.Request, false))
	require.NoError(t, f.source.recover(f.ctx))
	require.Nil(t, responseCloudFromContext(f.ctx))
	TrackNativeHTTPAuthorization(f.ctx, UsageAuthorizationModeCloud)
	require.NotNil(t, nativeCloudCycle(f.ctx), "the completed Cloud forward still owns its callback")
	handled, err := finalizeNativeHTTPUsage(f.ctx, nil, nil, time.Second)
	require.True(t, handled)
	require.NoError(t, err)
	_, err = TrackNativeHTTPForward(f.ctx, func() (*OpenAIForwardResult, error) {
		return &OpenAIForwardResult{AuthorizationMode: UsageAuthorizationModeNative}, nil
	})
	require.NoError(t, err)
	TrackNativeHTTPAuthorization(f.ctx, UsageAuthorizationModeNative)
	require.Nil(t, nativeCloudCycle(f.ctx), "a new native candidate must not inherit Cloud billing ownership")
	owner := nativeHTTPFromContext(f.ctx)
	owner.dispatch.request.Transport = "ws"
	require.NoError(t, prepareNativeWSAttempt(f.ctx))
	require.False(t, owner.finalized)
}

func TestCloudExecutionNativeBillingProjectionPreservesOwnSequences(t *testing.T) {
	operation := model.OperationResult{OperationID: "same-operation", Attempts: []model.AttemptFact{
		{Sequence: 1, Identity: model.PreparedIdentity{Mode: UsageAuthorizationModeCloud}, Send: model.SendSent, Generation: model.GenerationRejected, BillingRoot: "cloud-root"},
		{Sequence: 2, Identity: model.PreparedIdentity{Mode: UsageAuthorizationModeNative}, Send: model.SendNotSent},
		{Sequence: 3, Identity: model.PreparedIdentity{Mode: UsageAuthorizationModeNative}, Send: model.SendSent, Usage: &model.UsageFact{Input: &model.TokenMeasurement{Value: 7}, Confidence: model.UsagePartial}},
	}}
	projection := nativeBillingOperation(operation)
	require.Len(t, projection.Attempts, 2)
	require.EqualValues(t, 2, projection.Attempts[0].Sequence)
	require.EqualValues(t, 3, projection.Attempts[1].Sequence)
	require.Len(t, operation.Attempts, 3, "core keeps the complete Cloud and native facts")
	require.Equal(t, "cloud-root", operation.Attempts[0].BillingRoot)
}
