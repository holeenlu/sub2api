package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Application owns construction rollback, the actual listener, request/connection
// lifetimes, and the ordered dependency shutdown. Cleanup never closes storage
// while an accepted request, turn, producer or billing worker is still unjoined.
type Application struct {
	Server       *http.Server
	Cloud        *service.OpenAICloudService
	lifetime     *host.Lifetime
	ingress      *requestOwners
	listenerDone chan error
	serveStarted bool
}

func (a *Application) Start(ctx context.Context) error { return a.lifetime.Start(ctx) }

// Serve returns on a signal or listener failure. The listener completion handle
// remains owned by Application until Cleanup; no fatal exit runs in a goroutine.
func (a *Application) Serve(ctx context.Context) error {
	if a.serveStarted {
		return errors.New("HTTP listener already started")
	}
	a.serveStarted = true
	a.listenerDone = make(chan error, 1)
	go func() { a.listenerDone <- a.Server.ListenAndServe() }()
	select {
	case err := <-a.listenerDone:
		a.listenerDone <- err
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		a.ingress.beginDrain()
		return nil
	}
}

func (a *Application) Cleanup(ctx context.Context) error {
	a.ingress.beginDrain()
	var result error
	if a.serveStarted {
		if err := a.Server.Shutdown(ctx); err != nil {
			result = errors.Join(result, fmt.Errorf("HTTP drain: %w", err))
			result = errors.Join(result, a.Server.Close())
			a.ingress.cancelAll()
		}
	}
	if err := a.ingress.join(ctx); err != nil {
		// Signal cancellation, but do not claim the handlers/WS owners have joined.
		a.ingress.cancelAll()
		return errors.Join(result, err)
	}
	if a.serveStarted {
		select {
		case err := <-a.listenerDone:
			a.listenerDone <- err
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				result = errors.Join(result, err)
			}
		case <-ctx.Done():
			return errors.Join(result, ctx.Err())
		}
	}
	return errors.Join(result, a.lifetime.Cleanup(ctx))
}

type requestOwners struct {
	closeErr error

	mu       sync.Mutex
	draining bool
	requests map[*requestLease]struct{}
	idle     chan struct{}
}

type requestLease struct {
	owner       *requestOwners
	cancel      context.CancelFunc
	handlerDone bool
	turnAware   bool
	turns       int
	conns       map[*ownedConnection]struct{}
}

func newRequestOwners() *requestOwners {
	idle := make(chan struct{})
	close(idle)
	return &requestOwners{requests: make(map[*requestLease]struct{}), idle: idle}
}

func (o *requestOwners) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		if o.draining {
			o.mu.Unlock()
			http.Error(w, "server is draining", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		lease := &requestLease{owner: o, cancel: cancel, conns: make(map[*ownedConnection]struct{})}
		if len(o.requests) == 0 {
			o.idle = make(chan struct{})
		}
		o.requests[lease] = struct{}{}
		o.mu.Unlock()
		defer func() { o.mu.Lock(); lease.handlerDone = true; o.releaseLocked(lease); o.mu.Unlock() }()
		next.ServeHTTP(&ownedResponseWriter{ResponseWriter: w, lease: lease}, r.WithContext(host.WithTurnAdmission(ctx, lease)))
	})
}

func (o *requestOwners) releaseLocked(l *requestLease) {
	if !l.handlerDone || len(l.conns) > 0 || l.turns > 0 {
		return
	}
	l.cancel()
	delete(o.requests, l)
	if len(o.requests) == 0 {
		select {
		case <-o.idle:
		default:
			close(o.idle)
		}
	}
}

func (l *requestLease) EnableTurnTracking() {
	o := l.owner
	o.mu.Lock()
	l.turnAware = true
	conns := o.idleConnectionsLocked(l)
	o.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}
func (l *requestLease) AdmitTurn() (func(), error) {
	o := l.owner
	o.mu.Lock()
	if o.draining {
		o.mu.Unlock()
		return nil, errors.New("server is not accepting new turns")
	}
	l.turnAware = true
	l.turns++
	o.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			o.mu.Lock()
			l.turns--
			closeConns := o.idleConnectionsLocked(l)
			o.releaseLocked(l)
			o.mu.Unlock()
			for _, conn := range closeConns {
				_ = conn.Close()
			}
		})
	}, nil
}

// Only explicitly instrumented WS connections can be declared idle. Missing
// per-turn integration remains a visible drain timeout, not a false idle close.
func (o *requestOwners) idleConnectionsLocked(l *requestLease) []*ownedConnection {
	if !o.draining || !l.turnAware || l.turns != 0 {
		return nil
	}
	// All accepted turns have finalized usage. Cancellation now attributes the
	// idle socket close to shutdown, not an upstream account failure.
	l.cancel()
	out := make([]*ownedConnection, 0, len(l.conns))
	for c := range l.conns {
		out = append(out, c)
	}
	return out
}
func (o *requestOwners) beginDrain() {
	o.mu.Lock()
	o.draining = true
	var conns []*ownedConnection
	for l := range o.requests {
		conns = append(conns, o.idleConnectionsLocked(l)...)
	}
	o.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}
func (o *requestOwners) cancelAll() {
	o.mu.Lock()
	var cancels []context.CancelFunc
	var conns []*ownedConnection
	for l := range o.requests {
		cancels = append(cancels, l.cancel)
		for c := range l.conns {
			conns = append(conns, c)
		}
	}
	o.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	for _, c := range conns {
		_ = c.Close()
	}
}
func (o *requestOwners) join(ctx context.Context) error {
	o.mu.Lock()
	done := o.idle
	o.mu.Unlock()
	select {
	case <-done:
		o.mu.Lock()
		err := o.closeErr
		o.mu.Unlock()
		return err
	default:
	}
	select {
	case <-done:
		o.mu.Lock()
		err := o.closeErr
		o.mu.Unlock()
		return err
	case <-ctx.Done():
		o.mu.Lock()
		requests, turns, connections := len(o.requests), 0, 0
		for l := range o.requests {
			turns += l.turns
			connections += len(l.conns)
		}
		o.mu.Unlock()
		return fmt.Errorf("ingress unjoined: requests=%d turns=%d connections=%d: %w", requests, turns, connections, ctx.Err())
	}
}

type ownedResponseWriter struct {
	http.ResponseWriter
	lease *requestLease
}

func (w *ownedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *ownedResponseWriter) Flush()                      { _ = w.FlushError() }
func (w *ownedResponseWriter) FlushError() error {
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *ownedResponseWriter) CloseNotify() <-chan bool {
	if notifier, ok := w.ResponseWriter.(http.CloseNotifier); ok {
		return notifier.CloseNotify()
	}
	return nil
}
func (w *ownedResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}

func (w *ownedResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	o := w.lease.owner
	o.mu.Lock()
	draining := o.draining
	o.mu.Unlock()
	if draining {
		return nil, nil, errors.New("server is draining")
	}
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	owned := &ownedConnection{Conn: conn, lease: w.lease}
	o.mu.Lock()
	if o.draining {
		o.mu.Unlock()
		_ = conn.Close()
		return nil, nil, errors.New("server is draining")
	}
	w.lease.conns[owned] = struct{}{}
	o.mu.Unlock()
	return owned, rw, nil
}

type ownedConnection struct {
	net.Conn
	lease    *requestLease
	once     sync.Once
	closeErr error
}

func (c *ownedConnection) Close() error {
	c.once.Do(func() {
		c.closeErr = c.Conn.Close()
		o := c.lease.owner
		o.mu.Lock()
		if c.closeErr != nil && !errors.Is(c.closeErr, net.ErrClosed) {
			o.closeErr = errors.Join(o.closeErr, c.closeErr)
		}
		delete(c.lease.conns, c)
		o.releaseLocked(c.lease)
		o.mu.Unlock()
	})
	return c.closeErr
}
