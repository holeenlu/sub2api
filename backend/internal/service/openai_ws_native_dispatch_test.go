package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type nativeWSReadCounter struct {
	net.Conn
	bytes atomic.Int64
}

func (c *nativeWSReadCounter) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.bytes.Add(int64(n))
	return n, err
}

type nativeWSCountListener struct {
	net.Listener
	accepted chan *nativeWSReadCounter
}

func (l *nativeWSCountListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	counted := &nativeWSReadCounter{Conn: c}
	l.accepted <- counted
	return counted, nil
}

type nativeWSObservedFrame struct {
	kind coderws.MessageType
	body string
}
type nativeWSGateFixture struct {
	client  *coderOpenAIWSClientConn
	counter *nativeWSReadCounter
	frames  chan nativeWSObservedFrame
	pings   chan struct{}
	done    chan struct{}
	url     string
}

func nativeWSRealGate(t *testing.T) *nativeWSGateFixture {
	t.Helper()
	f := &nativeWSGateFixture{frames: make(chan nativeWSObservedFrame, 10), pings: make(chan struct{}, 4), done: make(chan struct{})}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(f.done)
		c, err := coderws.Accept(w, r, &coderws.AcceptOptions{OnPingReceived: func(context.Context, []byte) bool { f.pings <- struct{}{}; return true }})
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			kind, body, err := c.Read(r.Context())
			if err != nil {
				return
			}
			f.frames <- nativeWSObservedFrame{kind, string(body)}
		}
	}))
	listener := &nativeWSCountListener{Listener: server.Listener, accepted: make(chan *nativeWSReadCounter, 1)}
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	f.url = "ws" + strings.TrimPrefix(server.URL, "http")
	c, _, _, err := newDefaultOpenAIWSClientDialer().Dial(ctx, f.url, nil, "")
	require.NoError(t, err)
	f.client = c.(*coderOpenAIWSClientConn)
	t.Cleanup(func() { _ = f.client.CloseNow(); wsLifecycleReceive(t, f.done) })
	f.counter = wsLifecycleReceive(t, listener.accepted)
	f.counter.bytes.Store(0)
	return f
}

type nativeWSGateAuthority struct {
	calls        atomic.Int32
	denied       atomic.Bool
	consume      chan struct{}
	atGate       chan struct{}
	continueGate chan struct{}
	beginPassed  chan struct{}
	deadline     time.Time
}

func (a *nativeWSGateAuthority) RevalidateDispatch(ctx context.Context, _ model.PreparedIdentity) (execution.DispatchValidation, error) {
	call := a.calls.Add(1)
	if call == 2 && a.consume != nil {
		close(a.consume)
	}
	if call == 3 && a.atGate != nil {
		close(a.atGate)
		select {
		case <-a.continueGate:
		case <-ctx.Done():
			return execution.DispatchValidation{}, context.Cause(ctx)
		}
	}
	if a.denied.Load() {
		return execution.DispatchValidation{}, ErrNativeDispatchScopeChanged
	}
	if call == 3 && a.beginPassed != nil {
		close(a.beginPassed)
	}
	return execution.DispatchValidation{Authorization: sha256.Sum256([]byte("fixture-current-principal")), Leases: sha256.Sum256([]byte("fixture-current-lease")), ValidUntil: a.deadline}, nil
}
func nativeWSGatePreparation(t *testing.T, ctx context.Context, writer native.WSWriter, url string, auth *nativeWSGateAuthority, kind int, bodies ...[]byte) (*native.PreparedWS, *execution.SendPermit) {
	t.Helper()
	identity := model.PreparedIdentity{OperationID: "gate-op", ResourceID: 7, Owner: model.ResourceOwner{AccountID: 7}, UserID: 3, APIKeyID: 2, Mode: "native", Transport: "ws", GrantDigest: sha256.Sum256([]byte("fixture-handshake-grant")), BodyKind: model.BodyReplayable}
	body := []byte("{\"type\":\"response.create\",\"input\":\"sealed\"}")
	if len(bodies) != 0 {
		body = bodies[0]
	}
	p, err := native.PrepareWS(ctx, native.WSSpec{MessageType: kind, Identity: identity, Payload: body, Target: native.WSTarget{URL: url, HandshakeDigest: sha256.Sum256([]byte("fixture-handshake-target"))}}, writer)
	require.NoError(t, err)
	budget := execution.NewAttemptBudget(execution.AttemptLimits{Total: 1, PerOwner: 1})
	dispatcher, err := execution.NewDispatcher(ctx, "gate-op", budget, execution.NewFirstOutput(time.Now()), auth)
	require.NoError(t, err)
	permit, err := dispatcher.Admit(ctx, p.Identity(), model.AttemptTarget{Owner: identity.Owner, Limit: 1})
	require.NoError(t, err)
	return p, permit
}

func TestNativeWSGateRevokedAfterActualWriterWaitZeroBytes(t *testing.T) {
	f := nativeWSRealGate(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	occupied, err := f.client.conn.Writer(ctx, coderws.MessageText)
	require.NoError(t, err)
	authority := &nativeWSGateAuthority{consume: make(chan struct{}), atGate: make(chan struct{}), continueGate: make(chan struct{})}
	p, permit := nativeWSGatePreparation(t, ctx, f.client, f.url, authority, 1)
	type outcome struct {
		result native.WSResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := native.OpenWS(ctx, p, permit); done <- outcome{result, err} }()
	wsLifecycleReceive(t, authority.consume)
	authority.denied.Store(true)
	_, err = occupied.Write([]byte("prior writer"))
	require.NoError(t, err)
	require.NoError(t, occupied.Close())
	require.Equal(t, "prior writer", wsLifecycleReceive(t, f.frames).body)
	wsLifecycleReceive(t, authority.atGate)
	f.counter.bytes.Store(0)
	close(authority.continueGate)
	result := wsLifecycleReceive(t, done)
	require.ErrorIs(t, result.err, ErrNativeDispatchScopeChanged)
	require.Equal(t, model.SendNotSent, result.result.SendState)
	wsLifecycleReceive(t, f.done)
	require.Zero(t, f.counter.bytes.Load(), "denial must not write even an empty WS frame or close handshake")
	require.Empty(t, f.frames, "no extra response.create or empty data message")
	// Retired coder writer is not left holding a usable connection lock.
	_, err = f.client.conn.Writer(ctx, coderws.MessageText)
	require.Error(t, err)
}

func TestNativeWSPoolGateCancellationIsBoundedAndDoesNotWrite(t *testing.T) {
	f := nativeWSRealGate(t)
	conn := newOpenAIWSConn("native-gate", 7, f.client, nil)
	require.True(t, conn.tryAcquire())
	lease := &openAIWSConnLease{conn: conn, accountID: 7}
	t.Cleanup(func() { conn.abort(); wsLifecycleReceive(t, conn.readerDone) })
	require.NoError(t, conn.writeMu.lock(t.Context()))
	ctx, cancel := context.WithCancel(t.Context())
	p, permit := nativeWSGatePreparation(t, ctx, lease, f.url, &nativeWSGateAuthority{}, 1)
	done := make(chan error, 1)
	go func() {
		result, err := native.OpenWS(ctx, p, permit)
		if result.SendState != model.SendNotSent {
			done <- errors.New("unexpected send evidence")
			return
		}
		done <- err
	}()
	cancel()
	err := wsLifecycleReceive(t, done)
	conn.writeMu.unlock()
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, f.counter.bytes.Load())
	require.Empty(t, f.frames)
	require.False(t, conn.isClosed(), "canceling before the pool gate cannot retire a socket another writer owns")
	lease.Release()
}

func TestNativeWSGateDeniedLeaseJoinsOnlyItsOwnConnection(t *testing.T) {
	denied := nativeWSRealGate(t)
	sibling := nativeWSRealGate(t)
	conn := newOpenAIWSConn("denied-lane", 7, denied.client, nil)
	require.True(t, conn.tryAcquire())
	lease := &openAIWSConnLease{conn: conn, accountID: 7}
	t.Cleanup(func() { conn.abort(); wsLifecycleReceive(t, conn.readerDone) })
	authority := &nativeWSGateAuthority{atGate: make(chan struct{}), continueGate: make(chan struct{})}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	p, permit := nativeWSGatePreparation(t, ctx, lease, denied.url, authority, 1)
	done := make(chan error, 1)
	go func() { _, err := native.OpenWS(ctx, p, permit); done <- err }()
	wsLifecycleReceive(t, authority.atGate)
	authority.denied.Store(true)
	close(authority.continueGate)
	require.ErrorIs(t, wsLifecycleReceive(t, done), ErrNativeDispatchScopeChanged)
	select {
	case <-conn.readerDone:
	default:
		t.Fatal("owned reader not joined before return")
	}
	select {
	case <-conn.closeDone:
	default:
		t.Fatal("owned close not joined before return")
	}
	require.Zero(t, denied.counter.bytes.Load())
	require.Empty(t, denied.frames)
	p, permit = nativeWSGatePreparation(t, ctx, sibling.client, sibling.url, &nativeWSGateAuthority{}, 2)
	result, err := native.OpenWS(ctx, p, permit)
	require.NoError(t, err)
	require.Equal(t, model.SendSent, result.SendState)
	frame := wsLifecycleReceive(t, sibling.frames)
	require.Equal(t, coderws.MessageBinary, frame.kind)
	require.Contains(t, frame.body, "sealed")
}

func TestNativeWSGateDeadlineWaitingForWriterWritesNothing(t *testing.T) {
	f := nativeWSRealGate(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	occupied, err := f.client.conn.Writer(ctx, coderws.MessageText)
	require.NoError(t, err)
	_ = occupied // Socket close retires the held writer without flushing it.
	authority := &nativeWSGateAuthority{deadline: time.Now().Add(30 * time.Millisecond)}
	p, permit := nativeWSGatePreparation(t, ctx, f.client, f.url, authority, 1)
	result, err := native.OpenWS(ctx, p, permit)
	require.Error(t, err)
	require.Equal(t, model.SendNotSent, result.SendState)
	require.Zero(t, f.counter.bytes.Load())
	require.Empty(t, f.frames)
	require.NoError(t, f.client.CloseNow())
}

func TestNativeWSGateSendDeadlineDoesNotBecomeHeldSocketLifetime(t *testing.T) {
	f := nativeWSRealGate(t)
	first, cancelFirst := context.WithCancel(t.Context())
	p, permit := nativeWSGatePreparation(t, first, f.client, f.url, &nativeWSGateAuthority{deadline: time.Now().Add(30 * time.Millisecond)}, 1)
	result, err := native.OpenWS(first, p, permit)
	require.NoError(t, err)
	require.Equal(t, model.SendSent, result.SendState)
	wsLifecycleReceive(t, f.frames)
	cancelFirst()
	<-time.After(40 * time.Millisecond)
	p, permit = nativeWSGatePreparation(t, t.Context(), f.client, f.url, &nativeWSGateAuthority{}, 2)
	result, err = native.OpenWS(t.Context(), p, permit)
	require.NoError(t, err)
	require.Equal(t, model.SendSent, result.SendState)
	require.Equal(t, coderws.MessageBinary, wsLifecycleReceive(t, f.frames).kind)
}

func TestNativeWSRawControlCannotCrossReleasedLeaseAfterWriterWait(t *testing.T) {
	client := &openAIWSFakeConn{}
	conn := newOpenAIWSConn("old-lane", 7, client, nil)
	require.True(t, conn.tryAcquire())
	lease := &openAIWSConnLease{conn: conn, accountID: 7}
	require.NoError(t, conn.writeMu.lock(t.Context()))
	done := make(chan error, 1)
	go func() { done <- conn.writeJSON(map[string]any{"type": "response.cancel"}, t.Context(), lease) }()
	// Simulate release winning the coordinator before the old queued writer.
	// The post-lock owner check must hold even if the connection is now leased.
	lease.released.Store(true)
	conn.writeMu.unlock()
	require.ErrorIs(t, wsLifecycleReceive(t, done), errOpenAIWSConnClosed)
	require.Empty(t, client.payload)
	conn.abort()
}

// Hold a real coder control-frame write under its inner writeFrameMu. No
// private library state is accessed and no application writer is replaced.
type nativeWSHoldControlBody struct {
	io.ReadWriteCloser
	entered, release chan struct{}
}

func (b *nativeWSHoldControlBody) Write(p []byte) (int, error) {
	if len(p) != 0 && p[0]&0x0f == 9 {
		close(b.entered)
		<-b.release
	}
	return b.ReadWriteCloser.Write(p)
}
func TestNativeWSPhysicalFrameGateRevokesAfterControlWriteWait(t *testing.T) {
	f := nativeWSRealGate(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	held := &nativeWSHoldControlBody{ReadWriteCloser: f.client.wire.ReadWriteCloser, entered: make(chan struct{}), release: make(chan struct{})}
	f.client.wire.ReadWriteCloser = held
	pingDone := make(chan error, 1)
	go func() { pingDone <- f.client.Ping(ctx) }()
	wsLifecycleReceive(t, held.entered)
	authority := &nativeWSGateAuthority{beginPassed: make(chan struct{})}
	prepared, permit := nativeWSGatePreparation(t, ctx, f.client, f.url, authority, 1)
	type outcome struct {
		result native.WSResult
		err    error
	}
	sent := make(chan outcome, 1)
	go func() { result, err := native.OpenWS(ctx, prepared, permit); sent <- outcome{result, err} }()
	wsLifecycleReceive(t, authority.beginPassed)
	authority.denied.Store(true)
	close(held.release)
	wsLifecycleReceive(t, f.pings) // exclude the already-authorized control frame
	f.counter.bytes.Store(0)
	outcomeValue := wsLifecycleReceive(t, sent)
	require.ErrorIs(t, outcomeValue.err, ErrNativeDispatchScopeChanged)
	require.Equal(t, model.SendNotSent, outcomeValue.result.SendState)
	require.GreaterOrEqual(t, authority.calls.Load(), int32(4), "recheck must run after the inner frame lock")
	wsLifecycleReceive(t, f.done)
	wsLifecycleReceive(t, pingDone)
	require.Empty(t, f.frames)
	require.Zero(t, f.counter.bytes.Load())
}
