package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingSlotCleanupCache struct {
	ConcurrencyCache
	entered, exit chan struct{}
	once          sync.Once
}

func (c *blockingSlotCleanupCache) CleanupExpiredAccountSlotKeys(ctx context.Context) error {
	c.once.Do(func() { close(c.entered) })
	<-ctx.Done()
	<-c.exit
	return ctx.Err()
}
func TestSlotCleanupStopCancelsAndJoinsActiveCleanup(t *testing.T) {
	cache := &blockingSlotCleanupCache{entered: make(chan struct{}), exit: make(chan struct{})}
	svc := NewConcurrencyService(cache)
	worker := svc.StartSlotCleanupWorker(nil, time.Hour)
	<-cache.entered
	stopped := make(chan struct{})
	go func() { svc.StopSlotCleanupWorker(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("stop did not wait for cleanup")
	case <-time.After(10 * time.Millisecond):
	}
	close(cache.exit)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("worker did not join")
	}
	worker.Stop()
	svc.StopSlotCleanupWorker()
}

type slotCleanupCache struct {
	ConcurrencyCache
	calls atomic.Int64
}

func (c *slotCleanupCache) CleanupExpiredAccountSlotKeys(context.Context) error {
	c.calls.Add(1)
	return nil
}

func TestStartSlotCleanupWorker_UsesCacheWideCleanupWithoutAccountRepo(t *testing.T) {
	cache := &slotCleanupCache{}
	svc := NewConcurrencyService(cache)

	worker := svc.StartSlotCleanupWorker(nil, time.Hour)
	t.Cleanup(worker.Stop)

	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if cache.calls.Load() > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("cleanup worker did not call cache-wide account slot cleanup")
		case <-ticker.C:
		}
	}
}
