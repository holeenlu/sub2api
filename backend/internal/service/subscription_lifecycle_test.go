//go:build unit

package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func lifecycleSubscriptionConfig() *config.Config {
	cfg := &config.Config{}
	cfg.SubscriptionCache.L1Size = 64
	cfg.SubscriptionCache.L1TTLSeconds = 30
	cfg.SubscriptionMaintenance.WorkerCount = 1
	cfg.SubscriptionMaintenance.QueueSize = 4
	return cfg
}
func TestSubscriptionLifecycleStartsExplicitlyAndDrainsBeforeCacheClose(t *testing.T) {
	s := NewSubscriptionService(nil, nil, nil, nil, lifecycleSubscriptionConfig())
	require.Nil(t, s.maintenanceQueue)
	require.Nil(t, s.subCacheL1)
	require.NoError(t, s.Start(context.Background()))
	require.NotNil(t, s.maintenanceQueue)
	require.NotNil(t, s.subCacheL1)
	started, release := make(chan struct{}), make(chan struct{})
	require.NoError(t, s.maintenanceQueue.TryEnqueue(func() { close(started); <-release }))
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Drain(ctx), context.DeadlineExceeded)
	close(release)
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, s.Drain(bounded))
	require.NoError(t, s.Stop(bounded))
	require.Nil(t, s.subCacheL1)
	require.Error(t, s.Start(context.Background()))
	require.NotPanics(t, func() { s.DoWindowMaintenance(&UserSubscription{}) })
}
func TestSubscriptionLifecycleParentCancellationJoinsMaintenanceAndWatcher(t *testing.T) {
	s := NewSubscriptionService(nil, nil, nil, nil, lifecycleSubscriptionConfig())
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, s.Start(ctx))
	cancel()
	bounded, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, s.Stop(bounded))
	select {
	case <-s.lifetimeWatchDone:
	default:
		t.Fatal("parent watcher not joined")
	}
	require.Nil(t, s.subCacheL1)
}

// The fixture exposes the real handle boundary, including a receive callback
// that can remain owned after cancellation until its caller releases it.
type subscriptionLifecycleTransport struct {
	BillingCache
	handle    *subscriptionLifecycleHandle
	subscribe func(context.Context, func(context.Context, string)) (SubscriptionCacheSubscription, error)
}

func (p *subscriptionLifecycleTransport) PublishSubscriptionCacheInvalidation(context.Context, string) error {
	return nil
}
func (p *subscriptionLifecycleTransport) SubscribeSubscriptionCacheInvalidation(ctx context.Context, cb func(context.Context, string)) (SubscriptionCacheSubscription, error) {
	if p.subscribe != nil {
		return p.subscribe(ctx, cb)
	}
	p.handle.ctx, p.handle.cancel = context.WithCancel(ctx)
	p.handle.callback = cb
	return p.handle, nil
}

type subscriptionLifecycleHandle struct {
	ctx        context.Context
	cancel     context.CancelFunc
	callback   func(context.Context, string)
	closeOnce  sync.Once
	closed     chan struct{}
	joined     chan struct{}
	closeCalls atomic.Int32
	waitCalls  atomic.Int32
	closeErr   error
	waitErr    error
}

func (h *subscriptionLifecycleHandle) Close() error {
	h.closeCalls.Add(1)
	h.closeOnce.Do(func() {
		if h.cancel != nil {
			h.cancel()
		}
		close(h.closed)
	})
	return h.closeErr
}
func (h *subscriptionLifecycleHandle) Wait(ctx context.Context) error {
	h.waitCalls.Add(1)
	select {
	case <-h.joined:
		return h.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
func newLifecycleSubscriptionHandle() *subscriptionLifecycleHandle {
	return &subscriptionLifecycleHandle{closed: make(chan struct{}), joined: make(chan struct{})}
}
func assertSubscriptionCachePresent(t *testing.T, s *SubscriptionService) {
	t.Helper()
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	require.NotNil(t, s.subCacheL1)
}
func TestSubscriptionLifecycleOwnsSubscriptionThroughTimedOutDrain(t *testing.T) {
	h := newLifecycleSubscriptionHandle()
	p := &subscriptionLifecycleTransport{handle: h}
	s := NewSubscriptionService(nil, nil, &BillingCacheService{cache: p}, nil, lifecycleSubscriptionConfig())
	require.NoError(t, s.Start(context.Background()))
	require.Same(t, h, s.cacheSubscription)
	key := subCacheKey(12, 34)
	s.subCacheL1.Set(key, true, 1)
	s.subCacheL1.Wait()
	h.callback(h.ctx, key)
	_, ok := s.subCacheL1.Get(key)
	require.False(t, ok, "context-aware callback must invalidate the L1 entry")
	started, release := make(chan struct{}), make(chan struct{})
	require.NoError(t, s.maintenanceQueue.TryEnqueue(func() { close(started); <-release }))
	<-started
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Drain(short), context.DeadlineExceeded)
	require.Zero(t, h.closeCalls.Load(), "accepted maintenance precedes subscription closure")
	assertSubscriptionCachePresent(t, s)
	close(release)
	select {
	case <-h.closed:
	case <-time.After(time.Second):
		t.Fatal("subscription close was not requested")
	}
	short2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	require.ErrorIs(t, s.Stop(short2), context.DeadlineExceeded)
	assertSubscriptionCachePresent(t, s)
	close(h.joined)
	join, cancelJoin := context.WithTimeout(context.Background(), time.Second)
	defer cancelJoin()
	require.NoError(t, s.Stop(join))
	require.Nil(t, s.subCacheL1)
	require.EqualValues(t, 1, h.closeCalls.Load())
	require.EqualValues(t, 1, h.waitCalls.Load())
}
func TestSubscriptionLifecycleStopJoinsLateStartupHandle(t *testing.T) {
	h := newLifecycleSubscriptionHandle()
	entered, release := make(chan struct{}), make(chan struct{})
	p := &subscriptionLifecycleTransport{subscribe: func(ctx context.Context, cb func(context.Context, string)) (SubscriptionCacheSubscription, error) {
		close(entered)
		<-release
		h.ctx, h.cancel = context.WithCancel(ctx)
		h.callback = cb
		return h, nil
	}}
	s := NewSubscriptionService(nil, nil, &BillingCacheService{cache: p}, nil, lifecycleSubscriptionConfig())
	started := make(chan error, 1)
	go func() { started <- s.Start(context.Background()) }()
	<-entered
	short, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, s.Stop(short), context.DeadlineExceeded)
	assertSubscriptionCachePresent(t, s)
	close(release)
	require.NoError(t, <-started)
	select {
	case <-h.closed:
	case <-time.After(time.Second):
		t.Fatal("late handle leaked")
	}
	assertSubscriptionCachePresent(t, s)
	close(h.joined)
	join, cancelJoin := context.WithTimeout(context.Background(), time.Second)
	defer cancelJoin()
	require.NoError(t, s.Stop(join))
	require.Nil(t, s.subCacheL1)
	require.EqualValues(t, 1, h.closeCalls.Load())
	require.EqualValues(t, 1, h.waitCalls.Load())
}
func TestSubscriptionLifecyclePropagatesStartAndJoinErrors(t *testing.T) {
	startErr, closeErr, waitErr := errors.New("subscribe failed"), errors.New("close failed"), errors.New("callback failed")
	h := newLifecycleSubscriptionHandle()
	h.closeErr = closeErr
	h.waitErr = waitErr
	close(h.joined)
	p := &subscriptionLifecycleTransport{subscribe: func(context.Context, func(context.Context, string)) (SubscriptionCacheSubscription, error) {
		return h, startErr
	}}
	s := NewSubscriptionService(nil, nil, &BillingCacheService{cache: p}, nil, lifecycleSubscriptionConfig())
	require.ErrorIs(t, s.Start(context.Background()), startErr)
	join, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Stop(join)
	require.ErrorIs(t, err, closeErr)
	require.ErrorIs(t, err, waitErr)
	require.Nil(t, s.subCacheL1)
	require.ErrorIs(t, s.Stop(join), waitErr)
	require.EqualValues(t, 1, h.closeCalls.Load())
	require.EqualValues(t, 1, h.waitCalls.Load())
}
