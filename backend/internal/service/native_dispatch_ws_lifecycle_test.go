package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// This is an explicit unit journal port, not evidence of a PG transaction.
// Production uses OpenAIGatewayService.RecordNativeOperationUsage unchanged.
func nativeWSFixtureJournal() NativeOperationBilling {
	return nativeOperationBillingFunc(func(_ context.Context, input NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
		return nativeFixtureTechnicalReceipt(input), nil
	})
}

func nativeWSLifecycleFixture(t *testing.T, parent context.Context, operationID ...string) (*nativeOperationFixture, *nativeWSRecordedConn) {
	t.Helper()
	f := newNativeOperationFixture(t, false, 3)
	request := f.op.request
	request.Transport = "ws"
	request.OperationID = "ws-lifecycle"
	if len(operationID) > 0 {
		request.OperationID = operationID[0]
	}
	ctx, err := f.service.BindNativeWSDispatch(EnsureOpenAITurnBudget(parent, f.service.cfg), &APIKeyService{apiKeyRepo: f.keys}, request)
	require.NoError(t, err)
	ctx, err = f.service.BindNativeWSOperation(ctx, []byte("original client response.create"))
	require.NoError(t, err)
	f.ctx, f.op = ctx, nativeDispatchFromContext(ctx)
	bindNativeRuntimeFixture(t, f)
	user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
	require.NoError(t, err)
	t.Cleanup(user.ReleaseFunc)
	account, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.resource.ID}, nil)
	require.NoError(t, err)
	t.Cleanup(account.ReleaseFunc)
	h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}})
	require.NoError(t, err)
	h.succeeded = true
	return f, &nativeWSRecordedConn{handshake: h, operation: f.op}
}

func TestNativeWSLifecycleBeginAndHTTPExecuteOwnership(t *testing.T) {
	f, _ := nativeWSLifecycleFixture(t, t.Context())
	owner := nativeHTTPFromContext(f.ctx)
	require.Equal(t, model.OperationWaiting, owner.core.Snapshot().State)
	require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
	bound, err := f.service.BindNativeWSOperation(f.ctx, nil)
	require.NoError(t, err)
	require.Same(t, owner, nativeHTTPFromContext(bound))
	require.ErrorIs(t, owner.core.Begin(bound), execution.ErrOperationConsumed)
	require.NoError(t, FinishNativeWSOperation(bound, nil))
	require.Equal(t, model.OperationCompleted, owner.core.Snapshot().State)
	require.ErrorIs(t, prepareNativeWSAttempt(bound), execution.ErrOperationConsumed)
	httpFixture := newNativeOperationFixture(t, false)
	httpOwner := nativeHTTPFromContext(httpFixture.ctx)
	require.NoError(t, httpOwner.core.Execute(httpFixture.ctx, func() execution.CycleResult {
		bound, err := httpFixture.service.BindNativeWSOperation(httpFixture.ctx, nil)
		require.NoError(t, err)
		require.Same(t, httpOwner, nativeHTTPFromContext(bound))
		require.NoError(t, FinishNativeWSOperation(bound, nil))
		require.Equal(t, model.OperationWaiting, httpOwner.core.Snapshot().State)
		return execution.CycleResult{}
	}))
}

func TestNativeWSLifecycleFinalizationBeforeFinishAndRelease(t *testing.T) {
	for _, cancelTurn := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "canceled_unknown"}[cancelTurn], func(t *testing.T) {
			parent, cancel := context.WithCancel(t.Context())
			defer cancel()
			f, conn := nativeWSLifecycleFixture(t, parent)
			require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`)))
			if !cancelTurn {
				require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_life\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}"), nil))
			}
			conn.attempt.close(nil)
			if cancelTurn {
				cancel()
			}
			entered, ack := make(chan struct{}), make(chan struct{})
			var released atomic.Bool
			port := nativeOperationBillingFunc(func(ctx context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
				require.NoError(t, ctx.Err())
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				require.False(t, released.Load())
				require.Len(t, in.Operation.Attempts, 1)
				require.True(t, in.Operation.Attempts[0].Closed)
				if cancelTurn {
					require.Nil(t, in.Operation.Attempts[0].Usage)
					require.Equal(t, model.TerminalUnknown, in.Operation.Attempts[0].Terminal)
				}
				close(entered)
				<-ack
				return nativeFixtureTechnicalReceipt(in), nil
			})
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, cause error) {
				_, _ = finalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource, Result: r}, cause, port, time.Second)
			}, ReleaseTurn: func(int) {
				snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
				expected := model.OperationCompleted
				if cancelTurn {
					expected = model.OperationCanceled
				}
				require.Equal(t, expected, snapshot.State)
				require.True(t, snapshot.Billing.Durable)
				released.Store(true)
			}}
			done := make(chan error, 1)
			go func() { done <- completeOpenAIWSTurn(f.ctx, hooks, 1, nil, context.Cause(parent)) }()
			wsLifecycleReceive(t, entered)
			require.False(t, released.Load())
			require.Equal(t, model.OperationReceiving, nativeHTTPFromContext(f.ctx).core.Snapshot().State)
			close(ack)
			err := wsLifecycleReceive(t, done)
			if cancelTurn {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
			require.True(t, released.Load())
			// A late frame cannot mutate a finished attempt's terminal/usage.
			before := nativeHTTPFromContext(f.ctx).core.Snapshot()
			require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.failed\",\"response\":{\"id\":\"late\"}}"), nil))
			require.Equal(t, before.Attempts, nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts)
		})
	}
}

func TestNativeWSLifecycleFailedFinalizerBlocksRecovery(t *testing.T) {
	f, conn := nativeWSLifecycleFixture(t, t.Context())
	payload := []byte(`{"type":"response.create","model":"mapped-provider-model"}`)
	require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
	conn.attempt.close(nil)
	journalErr := errors.New("journal unavailable")
	upstreamErr := &UpstreamFailoverError{StatusCode: 503, RetryableOnSameAccount: true, RejectedBeforeGeneration: true}
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, cause error) {
		_, _ = finalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource}, cause, nativeOperationBillingFunc(func(context.Context, NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
			return NativeIntentFinalizationResult{}, journalErr
		}), time.Second)
	}}
	err := completeOpenAIWSTurn(f.ctx, hooks, 1, nil, upstreamErr)
	require.ErrorIs(t, err, journalErr)
	var retry *UpstreamFailoverError
	require.False(t, errors.As(err, &retry))
	snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Equal(t, model.OperationFailed, snapshot.State)
	require.ErrorIs(t, snapshot.Failure, upstreamErr)
	require.Error(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
	require.Equal(t, 1, conn.frames)
	require.ErrorIs(t, FinishNativeWSOperation(f.ctx, nil), journalErr)
}

type nativeWSDelayedSend struct {
	*nativeWSRecordedConn
	entered, done chan struct{}
}

func (c *nativeWSDelayedSend) WriteWS(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
	state, err := c.nativeWSRecordedConn.WriteWS(ctx, w)
	close(c.entered)
	<-c.done
	return state, err
}

func TestNativeWSLifecyclePassthroughWaitsForActualSendAndDelivery(t *testing.T) {
	f, recorded := nativeWSLifecycleFixture(t, t.Context())
	conn := &nativeWSDelayedSend{nativeWSRecordedConn: recorded, entered: make(chan struct{}), done: make(chan struct{})}
	sent := make(chan error, 1)
	go func() {
		sent <- f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`))
	}()
	wsLifecycleReceive(t, conn.entered)
	require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"fast\"}}"), nil))
	finalized, released := false, false
	completion := &openAIWSPassthroughTurnCompletion{finalize: func(cause error) error {
		require.Equal(t, model.SendSent, nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts[0].Send)
		conn.attempt.close(cause)
		finalized = true
		return completeOpenAIWSTurn(f.ctx, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) {
			_, _ = finalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource}, e, nativeWSFixtureJournal(), time.Second)
		}}, 1, nil, cause)
	}, finish: func() {
		require.True(t, finalized)
		require.Equal(t, model.OperationCompleted, nativeHTTPFromContext(f.ctx).core.Snapshot().State)
		released = true
	}}
	completion.settle("response.completed")
	completion.deliveredEvent("response.completed")
	require.False(t, finalized)
	require.False(t, released)
	close(conn.done)
	require.NoError(t, wsLifecycleReceive(t, sent))
	completion.advance(true, false, false)
	require.True(t, finalized)
	require.True(t, released)
	completion.afterJoin(errors.New("late close"))
	require.NoError(t, completion.err())
}

type nativeWSUnacknowledgedSend struct {
	*nativeWSRecordedConn
	state model.SendState
	calls int
}

func (c *nativeWSUnacknowledgedSend) WriteWS(ctx context.Context, w *native.WSWrite) (model.SendState, error) {
	if _, err := w.Begin(ctx); err != nil {
		return model.SendNotSent, err
	}
	c.calls++
	return c.state, io.ErrUnexpectedEOF
}

func TestNativeWSLifecycleActualAdapterUnknownWithoutResult(t *testing.T) {
	for _, state := range []model.SendState{model.SendUnknown, model.SendAmbiguous} {
		t.Run(map[model.SendState]string{model.SendUnknown: "unknown", model.SendAmbiguous: "ambiguous"}[state], func(t *testing.T) {
			f, recorded := nativeWSLifecycleFixture(t, t.Context())
			journal := &nativeApplicationJournal{t: t}
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			f.service.usageBillingRepo = journal
			bindNativeRuntimeFixture(t, f)
			f.service.usageLogRepo = logs
			conn := &nativeWSUnacknowledgedSend{nativeWSRecordedConn: recorded, state: state}
			cause := f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`))
			require.Error(t, cause)
			var sendErr *OpenAITransportAttemptError
			require.ErrorAs(t, cause, &sendErr)
			require.Equal(t, state, sendErr.SendState)
			released := 0
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, e error) {
				require.Nil(t, result)
				handled, err := f.service.FinalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource, Result: result}, e)
				require.True(t, handled)
				require.NoError(t, err)
			}, ReleaseTurn: func(int) {
				snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
				require.Equal(t, model.OperationFailed, snapshot.State)
				require.True(t, snapshot.Billing.Durable)
				require.Equal(t, 1, journal.records)
				released++
			}}
			require.ErrorIs(t, completeOpenAIWSTurn(f.ctx, hooks, 1, nil, cause), io.ErrUnexpectedEOF)
			require.Equal(t, 1, released)
			require.Equal(t, 1, conn.calls)
			require.NotNil(t, journal.input)
			require.Equal(t, state, journal.input.Evidence.SendState)
			require.Equal(t, billing.NativeUsageUnknown, journal.input.Evidence.Confidence)
			require.Nil(t, journal.input.Evidence.Usage)
			require.Nil(t, journal.input.Settlement)
			require.Nil(t, journal.input.SettlementUsage)
			require.Zero(t, logs.calls)
			require.Zero(t, journal.old)
			require.ErrorIs(t, FinishNativeWSOperation(f.ctx, nil), io.ErrUnexpectedEOF)
			require.Equal(t, 1, released, "idempotent Finish cannot release a lease twice")
		})
	}
}

func TestNativeWSLifecycleReceiptFailureOrdersReleaseAndBlocksRecovery(t *testing.T) {
	for _, name := range []string{"journal_error", "missing_receipt", "wrong_identity", "timeout"} {
		t.Run(name, func(t *testing.T) {
			f, conn := nativeWSLifecycleFixture(t, t.Context())
			payload := []byte(`{"type":"response.create","model":"mapped-provider-model"}`)
			require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
			require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.failed\",\"response\":{\"id\":\"refused\",\"status\":\"failed\"}}"), nil))
			conn.attempt.close(nil)
			entered, ack := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(ack) }) }
			defer unblock()
			var releases atomic.Int32
			port := nativeOperationBillingFunc(func(ctx context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
				close(entered)
				<-ack
				switch name {
				case "journal_error":
					return NativeIntentFinalizationResult{}, errors.New("journal failed")
				case "missing_receipt":
					return NativeIntentFinalizationResult{}, nil
				case "wrong_identity":
					receipt := nativeFixtureTechnicalReceipt(in)
					receipt.Intent.Key.APIKeyID++
					return receipt, nil
				default:
					<-ctx.Done()
					return NativeIntentFinalizationResult{}, context.Cause(ctx)
				}
			})
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, cause error) {
				_, _ = finalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource, Result: r}, cause, port, 50*time.Millisecond)
			}, ReleaseTurn: func(int) {
				snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
				require.Equal(t, model.OperationFailed, snapshot.State)
				require.False(t, snapshot.Billing.Durable)
				releases.Add(1)
			}}
			upstream := &UpstreamFailoverError{StatusCode: 503, RetryableOnSameAccount: true, RejectedBeforeGeneration: true}
			done := make(chan error, 1)
			go func() { done <- completeOpenAIWSTurn(f.ctx, hooks, 1, nil, upstream) }()
			wsLifecycleReceive(t, entered)
			require.Zero(t, releases.Load())
			require.Equal(t, model.OperationReceiving, nativeHTTPFromContext(f.ctx).core.Snapshot().State)
			unblock()
			err := wsLifecycleReceive(t, done)
			require.Error(t, err)
			var retry *UpstreamFailoverError
			require.False(t, errors.As(err, &retry))
			require.EqualValues(t, 1, releases.Load())
			require.Error(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
			require.Equal(t, 1, conn.frames)
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
		})
	}
}

func TestNativeWSLifecycleDurableRecoveryKeepsOriginalOperation(t *testing.T) {
	f, conn := nativeWSLifecycleFixture(t, t.Context())
	owner := nativeHTTPFromContext(f.ctx)
	payload := []byte(`{"type":"response.create","model":"mapped-provider-model"}`)
	require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
	require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.failed\",\"response\":{\"id\":\"refused\",\"status\":\"failed\"}}"), nil))
	conn.attempt.close(nil)
	var billingIDs []billing.Identity
	var releasedStates []model.OperationState
	journal := nativeOperationBillingFunc(func(_ context.Context, in NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
		billingIDs = append(billingIDs, in.Identity)
		return nativeFixtureTechnicalReceipt(in), nil
	})
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, cause error) {
		handled, err := finalizeNativeWSUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource, Result: r}, cause, journal, time.Second)
		require.True(t, handled)
		require.NoError(t, err)
	}, ReleaseTurn: func(int) { releasedStates = append(releasedStates, owner.core.Snapshot().State) }}
	retry := &UpstreamFailoverError{StatusCode: 429, RetryableOnSameAccount: true, RejectedBeforeGeneration: true}
	require.ErrorIs(t, completeOpenAIWSTurn(f.ctx, hooks, 1, nil, retry), retry)
	require.Equal(t, model.OperationReceiving, owner.core.Snapshot().State)
	require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, conn, coderws.MessageText, payload))
	require.Same(t, owner, nativeHTTPFromContext(f.ctx))
	require.False(t, owner.finalized)
	require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"done\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}"), nil))
	conn.attempt.close(nil)
	require.NoError(t, completeOpenAIWSTurn(f.ctx, hooks, 1, nil, nil))
	require.Equal(t, []model.OperationState{model.OperationReceiving, model.OperationCompleted}, releasedStates)
	require.Len(t, billingIDs, 2)
	require.Equal(t, billingIDs[0], billingIDs[1])
	require.Equal(t, 2, f.op.budget.attemptBudget.Snapshot().Attempts)
	require.Equal(t, 2, conn.frames)
	require.Len(t, owner.core.Snapshot().Attempts, 2)
}
