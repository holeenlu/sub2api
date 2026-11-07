package sessionstore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func joinOwner(t *testing.T, owner *Lifecycle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := owner.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleConstructionAndStopBeforeStart(t *testing.T) {
	owner := NewLifecycle(time.Hour, func(context.Context) { t.Error("unexpected sweep") })
	if owner.running || owner.ctx != nil {
		t.Fatal("constructor started worker")
	}
	if _, _, err := owner.Begin(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	joinOwner(t, owner)
	if err := owner.Start(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestLifecycleStopTimeoutRetainsActualSweepJoin(t *testing.T) {
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	owner := NewLifecycle(time.Millisecond, func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release // dependency has observed cancel but has not actually returned
	})
	if err := owner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := owner.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	<-canceled
	select {
	case <-owner.done:
		t.Fatal("signal reported as joined")
	default:
	}
	if _, _, err := owner.Begin(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	close(release)
	joinOwner(t, owner)
	joinOwner(t, owner)
}

func TestLifecycleDrainKeepsAcceptedWorkAndParentCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()
	owner := NewLifecycle(time.Hour, func(context.Context) {})
	if err := owner.Start(parent); err != nil {
		t.Fatal(err)
	}
	accepted, finish, err := owner.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := owner.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if accepted.Err() != nil {
		t.Fatal("drain canceled admitted work")
	}
	if _, _, err := owner.Begin(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if _, nestedFinish, err := owner.Begin(accepted); err != nil {
		t.Fatal(err)
	} else {
		nestedFinish()
	}
	cancelParent()
	select {
	case <-accepted.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancel lost after drain")
	}
	finish()
	joinOwner(t, owner)
}

func TestLifecycleDetachedAndNestedRecoveryCancellation(t *testing.T) {
	owner := NewLifecycle(time.Hour, func(context.Context) {})
	if err := owner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	owned, finish, err := owner.BeginDetached(canceled)
	if err != nil || owned.Err() != nil {
		t.Fatalf("detached: %v", err)
	}
	bounded, stop := context.WithTimeout(owned, time.Millisecond)
	defer stop()
	<-bounded.Done()
	if _, _, err := owner.BeginDetached(bounded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("nested recovery lost deadline", err)
	}
	ctx, stopJoin := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopJoin()
	if err := owner.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if !errors.Is(owned.Err(), context.Canceled) {
		t.Fatal("stop did not cancel detached operation")
	}
	finish()
	if _, _, err := owner.BeginDetached(owned); !errors.Is(err, ErrClosed) {
		t.Fatal("ended context bypassed admission", err)
	}
	joinOwner(t, owner)
}

func TestLifecycleConcurrentAdmissionAndStop(t *testing.T) {
	owner := NewLifecycle(time.Hour, func(context.Context) {})
	if err := owner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var completed atomic.Int64
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, finish, err := owner.Begin(context.Background())
			if errors.Is(err, ErrClosed) {
				return
			}
			if err != nil {
				t.Error(err)
				return
			}
			<-ctx.Done()
			completed.Add(1)
			finish()
		}()
	}
	joinOwner(t, owner)
	wg.Wait()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.running || len(owner.active) != 0 {
		t.Fatalf("incomplete join, completed=%d", completed.Load())
	}
}
