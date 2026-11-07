package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// This peer uses the real prepared frame/permit and pool writer. Only the
// physical socket is substituted here; actual sockets are covered separately.
type responseScopePeer struct {
	openAIWSFakeConn
	writes  atomic.Int32
	response string
	controlEntered chan struct{}
	controlRelease chan struct{}
	failWrite error
}

func (p *responseScopePeer) WriteWS(ctx context.Context, write *native.WSWrite) (model.SendState, error) {
	if _, err := write.Begin(ctx); err != nil {
		return model.SendNotSent, err
	}
	if err := write.RevalidateWrite(ctx); err != nil {
		return model.SendNotSent, err
	}
	p.writes.Add(1)
	if p.failWrite != nil {
		return model.SendAmbiguous, p.failWrite
	}
	return model.SendSent, nil
}

func (p *responseScopePeer) WriteJSON(ctx context.Context, value any) error {
	if p.controlEntered != nil {
		close(p.controlEntered)
		select {
		case <-p.controlRelease:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	return p.openAIWSFakeConn.WriteJSON(ctx, value)
}

func (p *responseScopePeer) ReadMessage(context.Context) ([]byte, error) {
	return json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{
		"id": p.response, "status": "completed", "output": []any{},
	}})
}

func responseScopeLeaseFixture(t *testing.T) (*nativeOperationFixture, *openAIWSConnPool, openAIWSAcquireRequest, *openAIWSConnLease, *responseScopePeer) {
	t.Helper()
	f := newNativeOperationFixture(t, true, 6)
	pool, req := wsLifecyclePool(t)
	req.Account = f.resource
	req.WSURL = "wss://api.openai.com/v1/responses"
	peer := &responseScopePeer{}
	conn := newOpenAIWSConn("actual-generation", f.resource.ID, peer, nil)
	h, err := nativeWSHandshakeSnapshot(f.resource, f.resource, req.WSURL, "", http.Header{"Authorization": {"Bearer synthetic-upstream-key"}})
	require.NoError(t, err)
	h.succeeded = true // This pool test substitutes only the dial and socket.
	conn.nativeHandshake = h
	ap := pool.getOrCreateAccountPool(f.resource.ID)
	ap.mu.Lock()
	ap.conns[conn.id] = conn
	ap.mu.Unlock()
	pool.trackConn(conn)
	req.PreferredConnID = conn.id
	lease, err := pool.Acquire(f.ctx, req)
	require.NoError(t, err)
	t.Cleanup(lease.Release)
	return f, pool, req, lease, peer
}

func responseScopeGenerate(t *testing.T, f *nativeOperationFixture, lease *openAIWSConnLease, peer *responseScopePeer, response string) uint64 {
	t.Helper()
	peer.response = response
	require.NoError(t, f.service.sendNativeWS(f.ctx, f.resource, lease, coderws.MessageText, []byte("{\"type\":\"response.create\",\"model\":\"client-model\",\"input\":\"hello\"}")))
	generation := lease.responseWriteGeneration.Load()
	require.Positive(t, generation)
	_, err := lease.ReadMessageContext(f.ctx)
	require.NoError(t, err)
	lease.ConfirmResponse(response, generation)
	require.True(t, lease.HasResponseContext(response))
	lease.finishNativeAttempt(nil)
	f.service.getOpenAIWSStateStore().BindResponseConn(response, lease.ConnID(), time.Minute, openAIWSResponseScopeForStore(true))
	return generation
}

func TestOpenAIWSResponseGenerationActualWriteAndLateConfirm(t *testing.T) {
	f, pool, req, lease, peer := responseScopeLeaseFixture(t)
	first := responseScopeGenerate(t, f, lease, peer, "parent")
	require.NoError(t, lease.PingWithTimeout(time.Second))
	require.True(t, lease.HasResponseContext("parent"), "idle ping preserves the real generation")
	second := responseScopeGenerate(t, f, lease, peer, "successor")
	require.Greater(t, second, first)
	lease.ConfirmResponse("parent", first)
	require.True(t, lease.HasResponseContext("successor"))
	require.False(t, lease.HasResponseContext("parent"), "late same-lease completion cannot bind to a later write")
	lease.Release()
	req.RequiredResponseID = "successor"
	next, err := pool.Acquire(f.ctx, req)
	require.NoError(t, err)
	defer next.Release()
	lease.ConfirmResponse("old-lane", second)
	next.ConfirmResponse("invented", second)
	require.True(t, next.HasResponseContext("successor"), "neither released lease nor a lease with no write may confirm")
	require.EqualValues(t, 2, peer.writes.Load())
}

func TestOpenAIWSResponseGenerationRechecksAfterPoolQueue(t *testing.T) {
	for _, advance := range []bool{false, true} {
		name := "same_generation"
		if advance {
			name = "different_generation"
		}
		t.Run(name, func(t *testing.T) {
			f, pool, req, lease, peer := responseScopeLeaseFixture(t)
			responseScopeGenerate(t, f, lease, peer, "parent")
			req.RequiredResponseID = "parent"
			type acquired struct {
				lease *openAIWSConnLease
				err error
			}
			done := make(chan acquired, 1)
			ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
			defer cancel()
			go func() {
				l, err := pool.Acquire(ctx, req)
				done <- acquired{l, err}
			}()
			require.Eventually(t, func() bool { return lease.conn.waiters.Load() > 0 }, time.Second, time.Millisecond)
			if advance {
				responseScopeGenerate(t, f, lease, peer, "other-lane")
			}
			lease.Release()
			result := wsLifecycleReceive(t, done)
			if advance {
				require.Nil(t, result.lease)
				require.ErrorIs(t, result.err, ErrOpenAIContinuationUnavailable)
			} else {
				require.NoError(t, result.err)
				require.True(t, result.lease.HasResponseContext("parent"))
				result.lease.Release()
			}
		})
	}
}

func TestOpenAIWSResponseGenerationRechecksAtQueuedSealedWriter(t *testing.T) {
	f, _, _, lease, peer := responseScopeLeaseFixture(t)
	generation := responseScopeGenerate(t, f, lease, peer, "parent")
	payload := []byte("{\"type\":\"response.create\",\"previous_response_id\":\"parent\",\"input\":\"delta\",\"store\":true}")
	ctx, err := f.service.bindOpenAIWSResponseContext(f.ctx, f.resource, lease, payload)
	require.NoError(t, err)
	peer.controlEntered, peer.controlRelease = make(chan struct{}), make(chan struct{})
	controlDone := make(chan error, 1)
	writeCtx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	go func() { controlDone <- lease.WriteJSONContext(writeCtx, map[string]string{"type": "response.cancel"}) }()
	wsLifecycleReceive(t, peer.controlEntered)
	require.Greater(t, lease.conn.writeGeneration.Load(), generation)
	sendDone := make(chan error, 1)
	go func() { sendDone <- f.service.sendNativeWS(ctx, f.resource, lease, coderws.MessageText, payload) }()
	require.Eventually(t, func() bool { return f.op.budget.attemptBudget.Snapshot().Attempts == 2 }, time.Second, time.Millisecond)
	close(peer.controlRelease)
	require.NoError(t, wsLifecycleReceive(t, controlDone))
	err = wsLifecycleReceive(t, sendDone)
	require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	var attempt *OpenAITransportAttemptError
	require.ErrorAs(t, err, &attempt)
	require.Equal(t, model.SendNotSent, attempt.SendState)
	require.EqualValues(t, 1, peer.writes.Load(), "the queued sealed create never reaches the physical writer")
	lease.ConfirmResponse("parent", generation)
	require.False(t, lease.HasResponseContext("parent"))
}

func TestOpenAIWSResponseGenerationAcquireReceiptReachesActualWriter(t *testing.T) {
	f, pool, req, lease, peer := responseScopeLeaseFixture(t)
	responseScopeGenerate(t, f, lease, peer, "parent")
	lease.Release()
	req.RequiredResponseID = "parent"
	next, err := pool.Acquire(f.ctx, req)
	require.NoError(t, err)
	defer next.Release()
	require.NoError(t, next.WriteJSONContext(f.ctx, map[string]string{"type": "response.cancel"}))
	err = f.service.sendNativeWS(f.ctx, f.resource, next, coderws.MessageText, []byte("{\"type\":\"response.create\",\"previous_response_id\":\"parent\"}"))
	require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	var attempt *OpenAITransportAttemptError
	require.ErrorAs(t, err, &attempt)
	require.Equal(t, model.SendNotSent, attempt.SendState)
	require.EqualValues(t, 1, peer.writes.Load(), "the acquired producer receipt is checked by the real writer without a separate context hint")
}

func TestOpenAIWSResponseGenerationRejectsDifferentSocketAndForcedNew(t *testing.T) {
	f, pool, req, lease, peer := responseScopeLeaseFixture(t)
	responseScopeGenerate(t, f, lease, peer, "parent")
	other := newOpenAIWSConn("different", f.resource.ID, &responseScopePeer{}, nil)
	require.True(t, other.tryAcquire())
	otherLease := &openAIWSConnLease{conn: other}
	defer func() { otherLease.Release(); other.close() }()
	_, err := f.service.bindOpenAIWSResponseContext(f.ctx, f.resource, otherLease, []byte("{\"previous_response_id\":\"parent\",\"store\":true}"))
	require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	req.RequiredResponseID, req.ForceNewConn = "parent", true
	denied, err := pool.Acquire(f.ctx, req)
	require.Nil(t, denied)
	require.ErrorIs(t, err, ErrOpenAIContinuationUnavailable)
	require.EqualValues(t, 1, peer.writes.Load())
}

func TestOpenAIWSResponseGenerationPreparationAndFailedWriteInvalidateParent(t *testing.T) {
	for _, kind := range []string{"preparation", "failed_write"} {
		t.Run(kind, func(t *testing.T) {
			f, _, _, lease, peer := responseScopeLeaseFixture(t)
			generation := responseScopeGenerate(t, f, lease, peer, "parent")
			if kind == "preparation" {
				require.NoError(t, f.service.sendNativeWSPreparation(f.ctx, f.resource, lease, []byte("{\"type\":\"response.create\",\"generate\":false}")))
			} else {
				peer.failWrite = errors.New("physical write failed after invocation")
				err := f.service.sendNativeWS(f.ctx, f.resource, lease, coderws.MessageText, []byte("{\"type\":\"response.create\",\"input\":\"another lane\"}"))
				require.Error(t, err)
				var attempt *OpenAITransportAttemptError
				require.ErrorAs(t, err, &attempt)
				require.Equal(t, model.SendAmbiguous, attempt.SendState)
			}
			require.Greater(t, lease.conn.writeGeneration.Load(), generation)
			lease.ConfirmResponse("parent", generation)
			require.False(t, lease.HasResponseContext("parent"))
			require.EqualValues(t, 2, peer.writes.Load())
		})
	}
}
