package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type wsLifecycleDialer func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error)

func (f wsLifecycleDialer) Dial(ctx context.Context, u string, h http.Header, p string) (openAIWSClientConn, int, http.Header, error) {
	return f(ctx, u, h, p)
}

func wsLifecyclePool(t *testing.T) (*openAIWSConnPool, openAIWSAcquireRequest) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 1
	p := newOpenAIWSConnPool(cfg)
	p.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return nil, 0, nil, errors.New("unexpected dial in local lifecycle fixture")
	}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, p.Stop(ctx))
	})
	return p, openAIWSAcquireRequest{Account: &Account{ID: 901, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, WSURL: "wss://unused.invalid/responses"}
}

func wsLifecycleReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(2 * time.Second):
		t.Fatal("lifecycle synchronization timed out")
		var z T
		return z
	}
}

func wsLifecycleSealed(t *testing.T, p *openAIWSConnPool) {
	t.Helper()
	p.lifecycle.mu.Lock()
	ctx := p.lifecycle.ctx
	p.lifecycle.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("pool admission did not close")
	}
}

func wsLifecycleStopped(t *testing.T, p *openAIWSConnPool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.NoError(t, p.Stop(ctx))
	p.lifecycle.mu.Lock()
	defer p.lifecycle.mu.Unlock()
	require.Zero(t, p.lifecycle.work)
	require.Empty(t, p.lifecycle.conns)
}

type wsLifecycleConn struct {
	started             chan struct{}
	readRelease         chan struct{}
	closeRelease        chan struct{}
	closed              chan struct{}
	ignoreReadCancel    bool
	onClose             func()
	readOnce, closeOnce sync.Once
	closeCalls          atomic.Int32
}

func newWSLifecycleConn() *wsLifecycleConn {
	return &wsLifecycleConn{started: make(chan struct{}), closed: make(chan struct{})}
}
func (c *wsLifecycleConn) RequiresReaderLoop() bool                   { return true }
func (c *wsLifecycleConn) WriteJSON(ctx context.Context, _ any) error { return ctx.Err() }
func (c *wsLifecycleConn) Ping(ctx context.Context) error             { return ctx.Err() }
func (c *wsLifecycleConn) ReadMessage(ctx context.Context) ([]byte, error) {
	c.readOnce.Do(func() { close(c.started) })
	if c.ignoreReadCancel {
		<-c.readRelease
		return nil, io.EOF
	}
	select {
	case <-c.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.readRelease:
		return nil, io.EOF
	}
}
func (c *wsLifecycleConn) Close() error {
	c.closeOnce.Do(func() {
		c.closeCalls.Add(1)
		if c.onClose != nil {
			c.onClose()
		}
		if c.closeRelease != nil {
			<-c.closeRelease
		}
		close(c.closed)
	})
	return nil
}
func (c *wsLifecycleConn) CloseNow() error { return c.Close() }

func TestOpenAIWSPoolLifecycleDrainKeepsRealLeaseUntilRelease(t *testing.T) {
	peerClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		defer close(peerClosed)
		for {
			kind, data, e := conn.Read(r.Context())
			if e != nil {
				return
			}
			if e = conn.Write(r.Context(), kind, data); e != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	pool, req := wsLifecyclePool(t)
	req.WSURL = "ws" + strings.TrimPrefix(server.URL, "http")
	pool.setClientDialerForTest(newDefaultOpenAIWSClientDialer())
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pool.Drain(ctx) }()
	wsLifecycleSealed(t, pool)
	_, err = pool.Acquire(t.Context(), req)
	require.ErrorIs(t, err, errOpenAIWSPoolDraining)
	require.False(t, lease.conn.isClosed())
	require.NoError(t, lease.WriteJSONWithContextTimeout(t.Context(), map[string]string{"type": "fixture"}, time.Second))
	payload, err := lease.ReadMessageWithContextTimeout(t.Context(), time.Second)
	require.NoError(t, err)
	require.Contains(t, string(payload), "fixture")
	select {
	case err := <-done:
		t.Fatalf("Drain returned while lease active: %v", err)
	default:
	}
	lease.Release()
	lease.Release()
	require.NoError(t, wsLifecycleReceive(t, done))
	wsLifecycleReceive(t, lease.conn.readerDone)
	wsLifecycleReceive(t, lease.conn.closeDone)
	wsLifecycleReceive(t, peerClosed)
	_, _, conns := pool.AccountPoolLoad(req.Account.ID)
	require.Zero(t, conns)
	wsLifecycleStopped(t, pool)
}

func TestOpenAIWSPoolLifecycleDrainDeadlineAbortsLeasedReader(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	raw := newWSLifecycleConn()
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return raw, 101, nil, nil
	}))
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	wsLifecycleReceive(t, raw.started)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, pool.Drain(ctx), context.DeadlineExceeded)
	wsLifecycleStopped(t, pool)
	require.True(t, lease.conn.isClosed())
	require.EqualValues(t, 1, raw.closeCalls.Load())
	wsLifecycleReceive(t, lease.conn.readerDone)
	lease.Release()
	_, err = pool.Acquire(t.Context(), req)
	require.ErrorIs(t, err, errOpenAIWSPoolDraining)
}

func TestOpenAIWSPoolLifecycleQueuedAcquireCannotOutliveDrain(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	pool.setClientDialerForTest(&openAIWSFakeDialer{})
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	waiter := make(chan error, 1)
	go func() {
		l, e := pool.Acquire(t.Context(), req)
		if l != nil {
			l.Release()
		}
		waiter <- e
	}()
	require.Eventually(t, func() bool { return lease.conn.waiters.Load() == 1 }, time.Second, time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	drained := make(chan error, 1)
	go func() { drained <- pool.Drain(ctx) }()
	wsLifecycleSealed(t, pool)
	require.ErrorIs(t, wsLifecycleReceive(t, waiter), errOpenAIWSPoolDraining)
	require.False(t, lease.conn.isClosed())
	lease.Release()
	require.NoError(t, wsLifecycleReceive(t, drained))
	require.Zero(t, lease.conn.waiters.Load())
}

func TestOpenAIWSPoolLifecycleLateDialCannotPublishAfterTimeout(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	raw := newWSLifecycleConn()
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		calls.Add(1)
		close(entered)
		<-release
		return raw, 101, nil, nil
	}))
	done := make(chan error, 1)
	go func() {
		lease, err := pool.Acquire(t.Context(), req)
		if lease != nil {
			lease.Release()
		}
		done <- err
	}()
	wsLifecycleReceive(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, pool.Stop(ctx), context.DeadlineExceeded)
	_, err := pool.Acquire(t.Context(), req)
	require.ErrorIs(t, err, errOpenAIWSPoolDraining)
	require.EqualValues(t, 1, calls.Load())
	close(release)
	require.ErrorIs(t, wsLifecycleReceive(t, done), errOpenAIWSPoolDraining)
	wsLifecycleStopped(t, pool)
	require.EqualValues(t, 1, raw.closeCalls.Load())
	_, _, conns := pool.AccountPoolLoad(req.Account.ID)
	require.Zero(t, conns)
}

func TestOpenAIWSPoolLifecycleWaitsForDelayedPrewarm(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	pool.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 1
	entered, release := make(chan struct{}), make(chan struct{})
	raw := newWSLifecycleConn()
	var calls atomic.Int32
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		calls.Add(1)
		close(entered)
		<-release
		return raw, 101, nil, nil
	}))
	ap := pool.getOrCreateAccountPool(req.Account.ID)
	ap.mu.Lock()
	ap.lastAcquire = cloneOpenAIWSAcquireRequestPtr(&req)
	ap.mu.Unlock()
	pool.ensureTargetIdleAsync(req.Account.ID)
	wsLifecycleReceive(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, pool.Drain(ctx), context.DeadlineExceeded)
	close(release)
	wsLifecycleStopped(t, pool)
	ap.mu.Lock()
	require.Zero(t, ap.creating)
	require.False(t, ap.prewarmActive)
	require.Empty(t, ap.conns)
	ap.mu.Unlock()
	pool.ensureTargetIdleAsync(req.Account.ID)
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 1, raw.closeCalls.Load())
}

func TestOpenAIWSPoolLifecycleDoesNotConfuseCloseSignalWithReaderExit(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	raw := newWSLifecycleConn()
	raw.ignoreReadCancel = true
	raw.readRelease = make(chan struct{})
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return raw, 101, nil, nil
	}))
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	wsLifecycleReceive(t, raw.started)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, pool.Stop(ctx), context.DeadlineExceeded)
	wsLifecycleReceive(t, lease.conn.closeDone)
	select {
	case <-lease.conn.readerDone:
		t.Fatal("uncooperative reader was incorrectly joined")
	default:
	}
	close(raw.readRelease)
	wsLifecycleStopped(t, pool)
	lease.Release()
	require.EqualValues(t, 1, raw.closeCalls.Load())
}

func TestOpenAIWSPoolLifecycleNetworkCloseAndJoinStayOutsideLocks(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	raw := newWSLifecycleConn()
	raw.closeRelease = make(chan struct{})
	inside := make(chan struct{})
	ap := pool.getOrCreateAccountPool(req.Account.ID)
	raw.onClose = func() {
		pool.lifecycle.mu.Lock()
		pool.lifecycle.mu.Unlock()
		ap.mu.Lock()
		ap.mu.Unlock()
		close(inside)
	}
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return raw, 101, nil, nil
	}))
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	wsLifecycleReceive(t, raw.started)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pool.Stop(ctx) }()
	wsLifecycleReceive(t, inside)
	require.ErrorIs(t, wsLifecycleReceive(t, done), context.DeadlineExceeded)
	close(raw.closeRelease)
	wsLifecycleStopped(t, pool)
	lease.Release()
	require.EqualValues(t, 1, raw.closeCalls.Load())
}

func TestOpenAIWSPoolLifecycleConcurrentStopsJoinOneOwner(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	raw := newWSLifecycleConn()
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return raw, 101, nil, nil
	}))
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			errs <- pool.Stop(ctx)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, raw.closeCalls.Load())
	lease.Release()
	wsLifecycleStopped(t, pool)
}

func TestOpenAIWSPoolLifecycleCanceledAndNilContexts(t *testing.T) {
	var nilPool *openAIWSConnPool
	require.NoError(t, nilPool.Stop(t.Context()))
	require.NoError(t, nilPool.Drain(t.Context()))
	pool, req := wsLifecyclePool(t)
	require.Error(t, pool.Stop(nil))
	require.Error(t, pool.Drain(nil))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, pool.Drain(ctx), context.Canceled)
	_, err := pool.Acquire(t.Context(), req)
	require.True(t, errors.Is(err, errOpenAIWSPoolDraining))
	wsLifecycleStopped(t, pool)
}

type wsLifecycleEscalationConn struct {
	*wsLifecycleConn
	graceful                chan struct{}
	abort                   chan struct{}
	gracefulOnce, forceOnce sync.Once
}

func (c *wsLifecycleEscalationConn) Close() error {
	c.gracefulOnce.Do(func() { close(c.graceful) })
	<-c.abort
	return c.wsLifecycleConn.Close()
}
func (c *wsLifecycleEscalationConn) CloseNow() error {
	c.forceOnce.Do(func() { close(c.abort) })
	return c.wsLifecycleConn.Close()
}

func TestOpenAIWSPoolLifecycleForceEscalatesRunningGracefulClose(t *testing.T) {
	pool, req := wsLifecyclePool(t)
	raw := &wsLifecycleEscalationConn{wsLifecycleConn: newWSLifecycleConn(), graceful: make(chan struct{}), abort: make(chan struct{})}
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return raw, 101, nil, nil
	}))
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	wsLifecycleReceive(t, raw.started)
	broken := make(chan struct{})
	go func() { lease.MarkBroken(); close(broken) }()
	wsLifecycleReceive(t, raw.graceful)
	wsLifecycleStopped(t, pool)
	wsLifecycleReceive(t, broken)
	wsLifecycleReceive(t, lease.conn.readerDone)
	lease.Release()
	wsLifecycleStopped(t, pool)
	require.EqualValues(t, 1, raw.closeCalls.Load())
}

type wsLifecycleCloseFailure struct {
	*wsLifecycleConn
	err error
}

func (c *wsLifecycleCloseFailure) Close() error    { return errors.Join(c.wsLifecycleConn.Close(), c.err) }
func (c *wsLifecycleCloseFailure) CloseNow() error { return c.Close() }

func TestOpenAIWSPoolLifecycleReportsNetworkCloseFailure(t *testing.T) {
	pool := newOpenAIWSConnPool(&config.Config{})
	t.Cleanup(pool.Close)
	failure := errors.New("fixture network close failed")
	raw := &wsLifecycleCloseFailure{wsLifecycleConn: newWSLifecycleConn(), err: failure}
	pool.setClientDialerForTest(wsLifecycleDialer(func(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
		return raw, 101, nil, nil
	}))
	req := openAIWSAcquireRequest{Account: &Account{ID: 902, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, WSURL: "wss://unused.invalid/responses"}
	lease, err := pool.Acquire(t.Context(), req)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	require.ErrorIs(t, pool.Stop(ctx), failure)
	pool.lifecycle.mu.Lock()
	require.Zero(t, pool.lifecycle.work)
	require.Empty(t, pool.lifecycle.conns)
	pool.lifecycle.mu.Unlock()
	lease.Release()
	require.EqualValues(t, 1, raw.closeCalls.Load())
}
