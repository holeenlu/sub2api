package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func runningApplication(t *testing.T, handler http.Handler, life *host.Lifetime) (*Application, string, context.CancelFunc, <-chan error) {
	t.Helper()
	owners := newRequestOwners()
	addr := make(chan string, 1)
	server := &http.Server{Addr: "127.0.0.1:0", Handler: owners.handler(handler), BaseContext: func(l net.Listener) context.Context { addr <- l.Addr().String(); return context.Background() }}
	app := &Application{Server: server, lifetime: life, ingress: owners}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx) }()
	return app, <-addr, cancel, done
}

func TestApplicationHTTPDrainKeepsAcceptedUsageBeforeStorageClose(t *testing.T) {
	life := host.NewLifetime()
	var billed atomic.Bool
	var closed atomic.Bool
	life.Add(host.Component{Name: "db", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error {
		if !billed.Load() {
			t.Error("storage closed before accepted usage")
		}
		closed.Store(true)
		return nil
	}})
	started, release := make(chan struct{}), make(chan struct{})
	app, addr, stop, served := runningApplication(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		if r.Context().Err() != nil {
			t.Error("canceled accepted work during grace")
		}
		billed.Store(true)
		_, _ = w.Write([]byte("done"))
	}), life)
	response := make(chan error, 1)
	go func() {
		res, err := http.Get("http://" + addr)
		if err == nil {
			_, err = io.ReadAll(res.Body)
			res.Body.Close()
		}
		response <- err
	}()
	<-started
	stop()
	require.NoError(t, <-served)
	res, err := http.Get("http://" + addr)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	res.Body.Close()
	require.False(t, closed.Load())
	close(release)
	require.NoError(t, <-response)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, app.Cleanup(ctx))
	require.True(t, closed.Load())
}

func TestApplicationHijackedTurnRemainsOwnedUntilUsageFinalized(t *testing.T) {
	life := host.NewLifetime()
	started, release := make(chan struct{}), make(chan struct{})
	requestCtx := make(chan context.Context, 1)
	var billed atomic.Bool
	app, addr, stop, served := runningApplication(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.EnableTurnTracking(r.Context())
		finish, err := host.AdmitTurn(r.Context())
		if err != nil {
			t.Error(err)
			return
		}
		defer finish()
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = rw.Flush()
		requestCtx <- r.Context()
		close(started)
		<-release
		billed.Store(true)
	}), life)
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	require.NoError(t, err)
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if line == "\r\n" {
			break
		}
	}
	<-started
	ctx := <-requestCtx
	stop()
	require.NoError(t, <-served)
	_, err = host.AdmitTurn(ctx)
	require.Error(t, err)
	require.NoError(t, ctx.Err())
	require.False(t, billed.Load())
	close(release)
	bounded, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, app.Cleanup(bounded))
	require.True(t, billed.Load())
}

func TestApplicationUninstrumentedHijackIsNotMistakenForIdle(t *testing.T) {
	life := host.NewLifetime()
	closed := false
	life.Add(host.Component{Name: "db", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error { closed = true; return nil }})
	upgraded := make(chan struct{})
	app, addr, stop, served := runningApplication(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\n")
		_ = rw.Flush()
		close(upgraded)
	}), life)
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	<-upgraded
	stop()
	require.NoError(t, <-served)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = app.Cleanup(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Contains(t, err.Error(), "connections=1")
	require.False(t, closed)
	bounded, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	require.NoError(t, app.Cleanup(bounded))
	require.True(t, closed)
}

func TestApplicationMissingTurnOwnerFailsExplicitly(t *testing.T) {
	_, err := host.AdmitTurn(context.Background())
	require.True(t, errors.Is(err, host.ErrTurnAdmissionMissing))
	require.False(t, strings.Contains(err.Error(), "token"))
}

// This consumer test executes the actual Wire providers and service lifetimes.
// Only the external subscription socket is controlled so teardown can be paused.
type rootSubscriptionTransport struct {
	service.BillingCache
	handle        *rootSubscriptionHandle
	subscriptions atomic.Int32
}

func (p *rootSubscriptionTransport) InvalidateUserBalance(context.Context, int64) error { return nil }
func (p *rootSubscriptionTransport) PublishSubscriptionCacheInvalidation(context.Context, string) error {
	return nil
}
func (p *rootSubscriptionTransport) SubscribeSubscriptionCacheInvalidation(ctx context.Context, _ func(context.Context, string)) (service.SubscriptionCacheSubscription, error) {
	p.subscriptions.Add(1)
	p.handle.ctx, p.handle.cancel = context.WithCancel(ctx)
	return p.handle, nil
}

type rootSubscriptionHandle struct {
	ctx        context.Context
	cancel     context.CancelFunc
	closeOnce  sync.Once
	closeCalls atomic.Int32
	waitCalls  atomic.Int32
	closed     chan struct{}
	release    chan struct{}
	joined     atomic.Bool
}

func (h *rootSubscriptionHandle) Close() error {
	h.closeCalls.Add(1)
	h.closeOnce.Do(func() { h.cancel(); close(h.closed) })
	return nil
}
func (h *rootSubscriptionHandle) Wait(ctx context.Context) error {
	h.waitCalls.Add(1)
	select {
	case <-h.release:
		h.joined.Store(true)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestApplicationSubscriptionConsumerJoinsBeforeUsageCacheAndStorage(t *testing.T) {
	life := host.NewLifetime()
	h := &rootSubscriptionHandle{closed: make(chan struct{}), release: make(chan struct{})}
	transport := &rootSubscriptionTransport{handle: h}
	cfg := &config.Config{}
	cfg.SubscriptionCache.L1Size = 64
	cfg.SubscriptionCache.L1TTLSeconds = 30
	cfg.SubscriptionMaintenance.WorkerCount = 1
	cfg.SubscriptionMaintenance.QueueSize = 4
	billing := service.ProvideLifetimeBillingCacheService(transport, nil, nil, nil, nil, nil, cfg, nil, life)
	_ = service.ProvideLifetimeSubscriptionService(nil, nil, billing, nil, cfg, life)
	require.Zero(t, transport.subscriptions.Load(), "Wire construction must not start subscribing")
	var usage, storage atomic.Bool
	life.Add(host.Component{Name: "accepted-usage", Phase: host.Usage, StartOrder: 20, Constructed: true, Start: func(context.Context) error {
		if transport.subscriptions.Load() != 1 || h.ctx == nil {
			return errors.New("usage recovery started before the subscription handle was ready")
		}
		return nil
	}, Stop: func(ctx context.Context) error {
		if !h.joined.Load() {
			return errors.New("usage stopped before subscription joined")
		}
		if err := billing.InvalidateUserBalance(ctx, 7); err != nil {
			return err
		}
		require.Contains(t, life.Pending(), "BillingCacheService", "cache owner must remain until usage finalization")
		usage.Store(true)
		return nil
	}})
	life.Add(host.Component{Name: "test-storage", Phase: host.Infrastructure, Constructed: true, Stop: func(ctx context.Context) error {
		if !usage.Load() {
			return errors.New("storage closed before usage")
		}
		require.NotContains(t, life.Pending(), "BillingCacheService", "storage must wait for the cache Stop")
		storage.Store(true)
		return nil
	}})
	require.NoError(t, life.Start(context.Background()))
	require.EqualValues(t, 1, transport.subscriptions.Load())
	app := &Application{lifetime: life, ingress: newRequestOwners()}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, app.Cleanup(short), context.DeadlineExceeded)
	select {
	case <-h.closed:
	default:
		t.Fatal("subscription Close not requested")
	}
	require.ErrorIs(t, h.ctx.Err(), context.Canceled)
	require.False(t, h.joined.Load())
	require.False(t, usage.Load())
	require.False(t, storage.Load())
	require.NoError(t, billing.Start(context.Background()), "unjoined producer must retain downstream cache")
	close(h.release)
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, app.Cleanup(bounded))
	require.True(t, h.joined.Load())
	require.True(t, usage.Load())
	require.True(t, storage.Load())
	require.EqualValues(t, 1, h.closeCalls.Load())
	require.EqualValues(t, 1, h.waitCalls.Load())
	require.Empty(t, life.Pending())
}

// The real cache worker receives a pre-settlement fill that cannot finish until
// released. Final invalidation must wait for it even when an earlier drain times out.
type rootCacheFillGate struct {
	service.BillingCache
	entered       chan context.Context
	release       chan struct{}
	finished      atomic.Bool
	invalidated   atomic.Bool
	stale         atomic.Bool
	invalidations atomic.Int32
}

func (c *rootCacheFillGate) GetUserBalance(context.Context, int64) (float64, error) {
	return 0, errors.New("cache miss")
}
func (c *rootCacheFillGate) SetUserBalance(ctx context.Context, _ int64, _ float64) error {
	c.entered <- ctx
	<-c.release
	if c.invalidated.Load() {
		c.stale.Store(true)
	}
	c.finished.Store(true)
	return nil
}
func (c *rootCacheFillGate) InvalidateUserBalance(context.Context, int64) error {
	if !c.finished.Load() {
		return errors.New("invalidation ran before cache fills joined")
	}
	c.invalidated.Store(true)
	c.invalidations.Add(1)
	return nil
}

type rootBalanceRepository struct{ service.UserRepository }

func (rootBalanceRepository) GetByID(_ context.Context, id int64) (*service.User, error) {
	return &service.User{ID: id, Balance: 99}, nil
}
func TestApplicationCacheFillCheckpointProtectsFinalUsageInvalidation(t *testing.T) {
	life := host.NewLifetime()
	cache := &rootCacheFillGate{entered: make(chan context.Context, 1), release: make(chan struct{})}
	billing := service.ProvideLifetimeBillingCacheService(cache, rootBalanceRepository{}, nil, nil, nil, nil, &config.Config{}, nil, life)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(cache.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = billing.Stop(ctx)
	})
	var usageRequests, usageDrains, usageStops, storageStops atomic.Int32
	life.Add(host.Component{Name: "usage-finalization", Phase: host.Usage, Constructed: true,
		RequestStop: func() { usageRequests.Add(1) },
		Drain:       func(ctx context.Context) error { usageDrains.Add(1); return billing.InvalidateUserBalance(ctx, 7) },
		Stop:        func(ctx context.Context) error { usageStops.Add(1); return billing.InvalidateUserBalance(ctx, 7) }})
	life.Add(host.Component{Name: "storage", Phase: host.Infrastructure, Constructed: true, Stop: func(context.Context) error { storageStops.Add(1); return nil }})
	require.NoError(t, life.Start(context.Background()))
	balance, err := billing.GetUserBalance(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, float64(99), balance)
	var fillCtx context.Context
	select {
	case fillCtx = <-cache.entered:
	case <-time.After(time.Second):
		t.Fatal("cache fill never started")
	}
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, life.Cleanup(short), context.DeadlineExceeded)
	require.Zero(t, usageRequests.Load())
	require.Zero(t, usageDrains.Load())
	require.Zero(t, usageStops.Load())
	require.Zero(t, storageStops.Load())
	require.NoError(t, fillCtx.Err(), "failed cache Drain must not fall through to cache Stop")
	require.Contains(t, life.Pending(), "BillingCacheService")
	require.False(t, cache.invalidated.Load())
	releaseOnce.Do(func() { close(cache.release) })
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, life.Cleanup(join))
	require.EqualValues(t, 1, usageRequests.Load())
	require.EqualValues(t, 1, usageDrains.Load())
	require.EqualValues(t, 1, usageStops.Load())
	require.EqualValues(t, 1, storageStops.Load())
	require.True(t, cache.finished.Load())
	require.False(t, cache.stale.Load())
	require.EqualValues(t, 2, cache.invalidations.Load())
	require.Empty(t, life.Pending())
}
