package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

type cacheFillGate struct {
	billingCacheWorkerStub
	entered      chan struct{}
	release      chan struct{}
	ignoreCancel bool
}

func (c *cacheFillGate) SetUserBalance(ctx context.Context, _ int64, _ float64) error {
	close(c.entered)
	if c.ignoreCancel {
		<-c.release
		return nil
	}
	select {
	case <-c.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestBillingCacheLifecycleExplicitStartAndDrain(t *testing.T) {
	c := &cacheFillGate{entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewBillingCacheService(c, nil, nil, nil, nil, nil, &config.Config{}, nil)
	require.Nil(t, svc.cacheWriteChan, "constructor must not create workers")
	require.False(t, svc.enqueueCacheWrite(cacheWriteTask{kind: cacheWriteSetBalance}))
	require.NoError(t, svc.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Stop(ctx))
	})
	first := svc.cacheWriteDone
	require.NoError(t, svc.Start(context.Background()))
	require.Equal(t, first, svc.cacheWriteDone)
	require.True(t, svc.enqueueCacheWrite(cacheWriteTask{kind: cacheWriteSetBalance}))
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("fill not admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Drain(ctx), context.DeadlineExceeded)
	require.False(t, svc.enqueueCacheWrite(cacheWriteTask{kind: cacheWriteSetBalance}))
	close(c.release)
	join, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	require.NoError(t, svc.Drain(join))
	require.NoError(t, svc.Stop(join))
	require.Error(t, svc.Start(context.Background()))
}
func TestBillingCacheLifecycleStopCancelsAndJoins(t *testing.T) {
	c := &cacheFillGate{entered: make(chan struct{}), release: make(chan struct{})}
	svc := NewBillingCacheService(c, nil, nil, nil, nil, nil, &config.Config{}, nil)
	require.NoError(t, svc.Start(context.Background()))
	require.True(t, svc.enqueueCacheWrite(cacheWriteTask{kind: cacheWriteSetBalance}))
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("fill not admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.Stop(ctx))
	require.NoError(t, svc.Stop(ctx))
	select {
	case <-svc.cacheWriteDone:
	default:
		t.Fatal("Stop returned before workers joined")
	}
}
func TestBillingCacheLifecycleStopDeadlineRetainsJoinOwnership(t *testing.T) {
	c := &cacheFillGate{entered: make(chan struct{}), release: make(chan struct{}), ignoreCancel: true}
	svc := NewBillingCacheService(c, nil, nil, nil, nil, nil, &config.Config{}, nil)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(c.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Stop(ctx))
	})
	require.NoError(t, svc.Start(context.Background()))
	require.True(t, svc.enqueueCacheWrite(cacheWriteTask{kind: cacheWriteSetBalance}))
	select {
	case <-c.entered:
	case <-time.After(time.Second):
		t.Fatal("fill not admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err := svc.Stop(ctx)
	releaseOnce.Do(func() { close(c.release) })
	require.True(t, errors.Is(err, context.DeadlineExceeded))
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	require.NoError(t, svc.Stop(ctx2))
}

func TestBillingCacheDrainPreservesFinalUsageInvalidation(t *testing.T) {
	cache := &billingRecoveryCache{}
	// Explicit invalidation is independent of the stopped fill queue.
	recorder := &lifecycleInvalidationCache{BillingCache: cache}
	svc := NewBillingCacheService(recorder, nil, nil, nil, nil, nil, &config.Config{}, nil)
	require.NoError(t, svc.Start(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, svc.Drain(ctx))
	require.NoError(t, svc.InvalidateUserBalance(ctx, 1))
	require.Equal(t, 1, recorder.calls)
	require.NoError(t, svc.Stop(ctx))
}

type lifecycleInvalidationCache struct {
	BillingCache
	calls int
}

func (c *lifecycleInvalidationCache) InvalidateUserBalance(context.Context, int64) error {
	c.calls++
	return nil
}

func TestBillingCacheLifecycleConcurrentStartAndClose(t *testing.T) {
	for _, drain := range []bool{false, true} {
		for i := 0; i < 10; i++ {
			svc := NewBillingCacheService(&billingCacheWorkerStub{}, nil, nil, nil, nil, nil, &config.Config{}, nil)
			started := make(chan error, 1)
			go func() { started <- svc.Start(context.Background()) }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if drain {
				require.NoError(t, svc.Drain(ctx))
			} else {
				require.NoError(t, svc.Stop(ctx))
			}
			select {
			case err := <-started:
				if err != nil {
					require.ErrorContains(t, err, "stopped")
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.NoError(t, svc.Stop(ctx))
			cancel()
		}
	}
}
