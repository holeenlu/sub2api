package service

import (
	"context"
	"errors"
	"sync"
)

var errBackgroundWorkStopped = errors.New("background service is stopping")

// backgroundWork owns cancellation and actual completion, NOT scheduling.
// Register before a goroutine starts. Timers stay with their existing owner.
// Admission and active counts share mu; timeout leaves the same work joinable.
type backgroundWork struct {
	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelCauseFunc
	parentStop     func() bool
	bound, closing bool
	active         int
	changed        chan struct{}
}

func (w *backgroundWork) initLocked() {
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancelCause(context.Background())
		w.changed = make(chan struct{})
	}
}
func (w *backgroundWork) bind(parent context.Context, requestStop func()) error {
	if parent == nil {
		return errors.New("background parent context is nil")
	}
	if parent.Err() != nil {
		return context.Cause(parent)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.initLocked()
	if w.closing || w.ctx.Err() != nil {
		return errBackgroundWorkStopped
	}
	if !w.bound {
		if w.active != 0 {
			return errors.New("cannot bind parent while pre-start work is active")
		}
		w.cancel(context.Canceled)
		w.ctx, w.cancel = context.WithCancelCause(parent)
		w.bound = true
		if requestStop == nil {
			requestStop = w.requestStop
		}
		w.parentStop = context.AfterFunc(parent, requestStop)
	}
	return nil
}
func (w *backgroundWork) begin() (context.Context, func(), error) {
	w.mu.Lock()
	w.initLocked()
	if w.closing || w.ctx.Err() != nil {
		w.mu.Unlock()
		return nil, nil, errBackgroundWorkStopped
	}
	w.active++
	ctx := w.ctx
	w.mu.Unlock()
	var once sync.Once
	return ctx, func() {
		once.Do(func() { w.mu.Lock(); w.active--; close(w.changed); w.changed = make(chan struct{}); w.mu.Unlock() })
	}, nil
}
func (w *backgroundWork) launch(fn func(context.Context)) error {
	ctx, done, err := w.begin()
	if err != nil {
		return err
	}
	go func() { defer done(); fn(ctx) }()
	return nil
}

// beginContext retains caller cancellation/values as well as the service owner.
// It is used by durable commands that can dispatch background work after IO.
func (w *backgroundWork) beginContext(parent context.Context) (context.Context, func(), error) {
	if parent == nil {
		return nil, nil, errors.New("background command context is nil")
	}
	owner, done, err := w.begin()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(owner, func() { cancel(context.Cause(owner)) })
	if owner.Err() != nil {
		cancel(context.Cause(owner))
	}
	return ctx, func() { stop(); cancel(context.Canceled); done() }, nil
}
func (w *backgroundWork) requestStop() {
	w.mu.Lock()
	w.initLocked()
	w.closing = true
	cancel, stop := w.cancel, w.parentStop
	w.parentStop = nil
	w.mu.Unlock()
	if stop != nil {
		stop()
	}
	cancel(errBackgroundWorkStopped)
}
func (w *backgroundWork) join(ctx context.Context) error {
	if ctx == nil {
		return errors.New("background join context is nil")
	}
	for {
		w.mu.Lock()
		w.initLocked()
		active, changed := w.active, w.changed
		w.mu.Unlock()
		if active == 0 {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
}

// beginCleanup is used only for Deferred's final durable flush after normal
// admission is sealed. The caller supplies a bounded cleanup context and owns
// the one execution handle; timeout never detaches it from active accounting.
func (w *backgroundWork) beginCleanup(ctx context.Context) (func(), error) {
	if ctx == nil {
		return nil, errors.New("cleanup context is nil")
	}
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	w.mu.Lock()
	w.initLocked()
	if !w.closing {
		w.mu.Unlock()
		return nil, errors.New("cleanup requires sealed admission")
	}
	w.active++
	w.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { w.mu.Lock(); w.active--; close(w.changed); w.changed = make(chan struct{}); w.mu.Unlock() })
	}, nil
}
