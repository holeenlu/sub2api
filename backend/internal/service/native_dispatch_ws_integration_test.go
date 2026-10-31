package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// This local composition fixture uses the real operation/dispatcher and actual
// routing unlimited-policy receipts. Its peer only records frames; it does not
// stand in for upstream acceptance or end-to-end billing/delivery evidence.
type nativeWSRecordedConn struct {
	handshake     nativeWSHandshake
	frames, bytes int
	operation     *nativeDispatchOperation
	attempt       *nativeWSAttempt
}

func (c *nativeWSRecordedConn) setNativeAttempt(a *nativeWSAttempt) { c.attempt = a }
func (c *nativeWSRecordedConn) nativeHandshake() (nativeWSHandshake, bool) {
	return c.handshake, c.handshake.succeeded
}
func (c *nativeWSRecordedConn) WriteWS(ctx context.Context, write *native.WSWrite) (model.SendState, error) {
	if nativeDispatchFromContext(ctx) != c.operation {
		return model.SendNotSent, execution.ErrDispatchChanged
	}
	payload, err := write.Begin(ctx)
	if err != nil {
		return model.SendNotSent, err
	}
	c.frames++
	c.bytes += len(payload)
	return model.SendSent, nil
}
func TestNativeWSOperationHTTPToWSKeepsOriginalBudget(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}})
	require.NoError(t, err)
	h.succeeded = true
	connection := &nativeWSRecordedConn{handshake: h, operation: f.op}
	payload := []byte(`{"type":"response.create","model":"mapped-provider-model","generate":false}`)
	// Client generate:false remains a normal generation attempt; only the
	// separate root internal-preparation entry may select preparation purpose.
	require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, connection, coderws.MessageText, payload))
	require.Equal(t, 1, connection.frames)
	require.Equal(t, len(payload), connection.bytes)
	require.Same(t, f.op, nativeDispatchFromContext(f.ctx))
	require.Equal(t, "http", f.op.request.Transport)
	snapshot := f.op.budget.attemptBudget.Snapshot()
	require.Equal(t, 1, snapshot.Attempts)
	require.Zero(t, snapshot.PreparationRetries)
	err = f.service.sendNativeWS(f.ctx, f.resource, connection, coderws.MessageText, payload)
	require.ErrorIs(t, err, execution.ErrAttemptBudgetExhausted)
	require.Equal(t, 1, connection.frames)
	require.Zero(t, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
}
func TestNativeWSOperationMissingRootWritesNothing(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	connection := &nativeWSRecordedConn{operation: f.op}
	err := f.service.sendNativeWS(context.Background(), f.resource, connection, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`))
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	var attempt *OpenAITransportAttemptError
	require.ErrorAs(t, err, &attempt)
	require.Equal(t, model.SendNotSent, attempt.SendState)
	require.Zero(t, connection.frames)
	require.Zero(t, connection.bytes)
}

func (c *nativeWSRecordedConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	<-ctx.Done()
	return coderws.MessageText, nil, context.Cause(ctx)
}
func (c *nativeWSRecordedConn) WriteFrame(_ context.Context, _ coderws.MessageType, body []byte) error {
	c.frames++
	c.bytes += len(body)
	return nil
}
func (c *nativeWSRecordedConn) Close() error { return nil }
func TestNativeWSDispatchFrameConnPreservesCallerCancellation(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	transport := &nativeWSRecordedConn{operation: f.op}
	wrapper := &nativeWSDispatchFrameConn{inner: transport, writer: transport, service: f.service, account: f.resource, turnContext: func(context.Context, []byte) context.Context { return f.ctx }}
	caller, cancel := context.WithCancel(t.Context())
	cancel()
	err := wrapper.WriteFrame(caller, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`))
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, transport.frames)
	require.Zero(t, transport.bytes)
}

type nativeWSBlockingKeyReader struct{ APIKeyRepository }

func (*nativeWSBlockingKeyReader) GetByID(ctx context.Context, _ int64) (*APIKey, error) {
	<-ctx.Done()
	return nil, context.Cause(ctx)
}
func TestNativeWSPreparationWaitUsesOriginalFirstOutputDeadline(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	f.op.authority.keys = &APIKeyService{apiKeyRepo: &nativeWSBlockingKeyReader{}}
	f.op.budget.firstOutput.Limit(20*time.Millisecond, time.Now())
	connection := &nativeWSRecordedConn{operation: f.op}
	done := make(chan error, 1)
	go func() {
		done <- f.service.sendNativeWS(f.ctx, f.resource, connection, coderws.MessageText, []byte(`{"type":"response.create","model":"mapped-provider-model"}`))
	}()
	err := wsLifecycleReceive(t, done)
	require.ErrorIs(t, err, ErrOpenAIWSFirstOutputBudgetExhausted)
	require.Zero(t, connection.frames)
	require.Zero(t, connection.bytes)
	require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
}

func TestNativeWSLocalDenialIsNotAnUpstreamHealthFailure(t *testing.T) {
	for _, cause := range []error{ErrNativeDispatchScopeChanged, ErrNativeDispatchGrantUnproven, execution.ErrDispatchChanged} {
		denied := &OpenAITransportAttemptError{Cause: nativeDispatchError("fixture refusal", cause), SendState: model.SendNotSent}
		wrapped := wrapOpenAIWSIngressTurnError(nativeWSFailureStage("write_upstream", denied), denied, false)
		require.False(t, IsOpenAIWSUpstreamTransportError(wrapped))
		require.False(t, isOpenAIWSIngressTurnRetryable(wrapped))
	}
	transport := &OpenAITransportAttemptError{Cause: errors.New("fixture socket unavailable"), SendState: model.SendNotSent}
	require.Equal(t, "write_upstream", nativeWSFailureStage("write_upstream", transport))
}

func TestNativeWSFrameDoesNotInheritHTTPBodyLimit(t *testing.T) {
	f := newNativeOperationFixture(t, false)
	f.service.cfg.Gateway.MaxBodySize = 8
	f.service.cfg.Gateway.OpenAIWS.ClientReadLimitBytes = 1024
	request := f.op.request
	request.OperationID = "ws-frame-scope"
	request.Transport = "ws"
	ctx, err := f.service.BindNativeWSDispatch(EnsureOpenAITurnBudget(t.Context(), f.service.cfg), &APIKeyService{apiKeyRepo: f.keys}, request)
	require.NoError(t, err)
	ctx, err = f.service.BindNativeWSOperation(ctx, []byte("original client frame"))
	require.NoError(t, err)
	f.ctx, f.op = ctx, nativeDispatchFromContext(ctx)
	bindNativeRuntimeFixture(t, f)
	ctx = f.ctx
	user, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true, Limit: 0}, nil)
	require.NoError(t, err)
	defer user.ReleaseFunc()
	account, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: f.resource.ID, Limit: 0}, nil)
	require.NoError(t, err)
	defer account.ReleaseFunc()
	h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}})
	require.NoError(t, err)
	h.succeeded = true
	conn := &nativeWSRecordedConn{handshake: h, operation: nativeDispatchFromContext(ctx)}
	body := []byte(`{"type":"response.create","model":"mapped-provider-model","input":"within the existing WS frame limit"}`)
	require.Greater(t, int64(len(body)), nativeDispatchFromContext(ctx).maxBody)
	require.Less(t, int64(len(body)), ResolveOpenAIWSClientReadLimitBytes(f.service.cfg))
	require.NoError(t, f.service.sendNativeWS(ctx, f.resource, conn, coderws.MessageText, body))
	require.Equal(t, 1, conn.frames)
}

func TestNativeWSOperationFactsCancelAndRejectedRecovery(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel_does_not_confirm", true: "proven_rejection_can_recover"}[rejected], func(t *testing.T) {
			f := newNativeOperationFixture(t, true, 2)
			h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, "wss://api.openai.com/v1/responses", "", http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}})
			require.NoError(t, err)
			h.succeeded = true
			conn := &nativeWSRecordedConn{handshake: h, operation: f.op}
			body := []byte("{\"type\":\"response.create\",\"model\":\"client-model\"}")
			bound, err := f.service.BindNativeWSOperation(f.ctx, body)
			require.NoError(t, err)
			require.Same(t, nativeHTTPFromContext(f.ctx), nativeHTTPFromContext(bound))
			require.NoError(t, f.service.sendNativeWS(bound, f.resource, conn, coderws.MessageText, body))
			if rejected {
				require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failed\",\"status\":\"failed\"}}"), nil))
			} else {
				require.NoError(t, conn.attempt.observe([]byte("{\"type\":\"response.created\",\"response\":{\"id\":\"resp_real\",\"usage\":{\"input_tokens\":0}}}"), nil))
				require.ErrorIs(t, conn.attempt.observe(nil, context.Canceled), context.Canceled)
				// A local timeout/cancel event can be delivered but must never
				// be fed back into the upstream fact decoder.
				conn.attempt.delivered([]byte("{\"type\":\"response.cancelled\",\"response\":{\"id\":\"resp_real\"}}"), nil)
			}
			facts := nativeHTTPFromContext(bound).core.Snapshot()
			require.Len(t, facts.Attempts, 1)
			require.Equal(t, model.SendSent, facts.Attempts[0].Send)
			if rejected {
				require.Equal(t, model.TerminalFailed, facts.Attempts[0].Terminal)
				require.NoError(t, f.service.sendNativeWS(bound, f.resource, conn, coderws.MessageText, body))
				require.Equal(t, 2, conn.frames)
			} else {
				require.Equal(t, model.TerminalUnknown, facts.Attempts[0].Terminal)
				require.Equal(t, model.UsagePartial, facts.Attempts[0].Usage.Confidence)
				require.NotNil(t, facts.Attempts[0].Usage.Input)
				require.Zero(t, facts.Attempts[0].Usage.Input.Value)
				require.Nil(t, facts.Attempts[0].Usage.Output)
				require.False(t, facts.Attempts[0].Usage.TerminalConfirmed)
				require.Error(t, f.service.sendNativeWS(bound, f.resource, conn, coderws.MessageText, body))
				require.Equal(t, 1, conn.frames)
			}
		})
	}
}

func TestNativeWSHTTPBridgeRequiresRegisteredTransport(t *testing.T) {
	for _, name := range []string{"unregistered", "wrong_resource", "wrong_operation", "registered"} {
		t.Run(name, func(t *testing.T) {
			f, _ := nativeWSLifecycleFixture(t, t.Context())
			request := f.request(t)
			if name != "unregistered" {
				account := *f.resource
				if name == "wrong_resource" {
					account.ID++
				}
				ctx, err := withNativeWSHTTPBridge(request.Context(), &account)
				require.NoError(t, err)
				if name == "wrong_operation" {
					other, _ := nativeWSLifecycleFixture(t, t.Context(), "other-bridge-operation")
					ctx = context.WithValue(ctx, nativeWSHTTPBridgeKey{}, nativeWSHTTPBridgeRegistration{operation: other.op, resourceID: account.ID})
				}
				request = request.WithContext(ctx)
			}
			f.transport.inspect = func(sent *http.Request, options native.TransportOptions) {
				require.Same(t, f.op, nativeDispatchFromContext(sent.Context()), "HTTP transport must retain the WS root")
				require.Equal(t, f.resource.ID, options.AccountID)
				require.Equal(t, http.MethodPost, sent.Method)
				require.Equal(t, "/v1/responses", sent.URL.Path)
			}
			response, err := f.service.doOpenAIUpstream(request, "", f.resource, f.key.GroupID)
			if name == "registered" {
				require.NoError(t, err)
				require.NotNil(t, response)
				require.NoError(t, response.Body.Close())
				require.EqualValues(t, 1, f.transport.sends.Load())
				facts := nativeHTTPFromContext(f.ctx).core.Snapshot()
				require.Len(t, facts.Attempts, 1)
				require.Equal(t, "http", facts.Attempts[0].Identity.Transport)
				require.Equal(t, f.op.request.OperationID, facts.Attempts[0].Identity.OperationID)
				require.Equal(t, model.SendSent, facts.Attempts[0].Send)
				require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
			} else {
				require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity)
				require.Zero(t, f.transport.sends.Load())
				require.Empty(t, nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts)
				require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
			}
			require.Zero(t, f.transport.legacy.Load())
			require.Equal(t, "ws", f.op.request.Transport)
			require.Equal(t, "ws", f.op.authority.request.Transport, "registration must not mutate the original authority")
		})
	}
}
