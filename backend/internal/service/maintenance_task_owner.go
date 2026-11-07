package service

import (
	"context"
	"errors"
	"sync"
)

var errMaintenanceStopped = errors.New("background service is not accepting work")

// maintenanceGate bounds lifecycle/configuration serialization by the caller.
type maintenanceGate struct {
	once sync.Once
	ch   chan struct{}
}

func (g *maintenanceGate) lock(ctx context.Context) error {
	g.once.Do(func() { g.ch = make(chan struct{}, 1) })
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case g.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (g *maintenanceGate) unlock() { <-g.ch }

// maintenanceTaskOwner is the admission and completion journal used by backup,
// ops maintenance, queue cleanup and notifications. The last actual task closes
// done; shutdown never creates a goroutine merely to wait for another goroutine.
// The mutex protects admission, startup and pending; no IO runs under it.
type maintenanceTaskOwner struct {
	mu        sync.Mutex
	started   bool
	closed    bool
	ctx       context.Context
	cancel    context.CancelFunc
	intake    chan struct{}
	done      chan struct{}
	startDone chan struct{}
	startErr  error
	closeErr  error
	pending   int
}

func (o *maintenanceTaskOwner) start(ctx context.Context, setup func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return errMaintenanceStopped
	}
	if o.started {
		done := o.startDone
		o.mu.Unlock()
		if err := waitMaintenanceDone(ctx, done); err != nil {
			return err
		}
		return o.startErr
	}
	o.started = true
	o.ctx, o.cancel = context.WithCancel(ctx)
	o.intake, o.done, o.startDone = make(chan struct{}), make(chan struct{}), make(chan struct{})
	o.pending = 1 // Startup IO and resource handoff also belong to the owner.
	runCtx := o.ctx
	o.mu.Unlock()
	var err error
	if setup != nil {
		err = setup(runCtx)
	}
	o.mu.Lock()
	o.startErr = err
	close(o.startDone)
	o.finishLocked()
	o.mu.Unlock()
	return err
}

func (o *maintenanceTaskOwner) begin() (context.Context, func(), error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.started || o.closed || o.ctx.Err() != nil {
		return nil, nil, errMaintenanceStopped
	}
	o.pending++
	var once sync.Once
	return o.ctx, func() {
		once.Do(func() {
			o.mu.Lock()
			defer o.mu.Unlock()
			o.finishLocked()
		})
	}, nil
}

func (o *maintenanceTaskOwner) finishLocked() {
	o.pending--
	if o.closed && o.pending == 0 {
		close(o.done)
	}
}

func (o *maintenanceTaskOwner) launch(fn func(context.Context)) error {
	ctx, finish, err := o.begin()
	if err != nil {
		return err
	}
	go func() { defer finish(); fn(ctx) }()
	return nil
}

// attach owns synchronous work while preserving its caller's values/deadline.
// The lifecycle cancellation callback only cancels the child; it never waits or
// accesses a resource after finish releases ownership.
func (o *maintenanceTaskOwner) attach(parent context.Context) (context.Context, func(), error) {
	runCtx, finish, err := o.begin()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := linkMaintenanceContext(parent, runCtx)
	return ctx, func() { cancel(); finish() }, nil
}

func linkMaintenanceContext(parent, owner context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(owner, cancel)
	if owner.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func (o *maintenanceTaskOwner) context() context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ctx
}

func (o *maintenanceTaskOwner) accepting() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.started && !o.closed && o.ctx.Err() == nil
}

func (o *maintenanceTaskOwner) recordCloseError(err error) {
	if err == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closeErr = errors.Join(o.closeErr, err)
}

func (o *maintenanceTaskOwner) run(fn func(context.Context)) bool {
	ctx, finish, err := o.begin()
	if err != nil {
		return false
	}
	defer finish()
	fn(ctx)
	return true
}

func (o *maintenanceTaskOwner) stopping() <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.intake
}

func (o *maintenanceTaskOwner) seal(cancelWork bool) <-chan struct{} {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed {
		o.closed = true
		if o.intake != nil {
			close(o.intake)
		}
		if o.done == nil {
			o.done = make(chan struct{})
		}
		if o.pending == 0 {
			close(o.done)
		}
	}
	if cancelWork && o.cancel != nil {
		o.cancel()
	}
	return o.done
}

func (o *maintenanceTaskOwner) drain(ctx context.Context) error {
	return o.wait(ctx, o.seal(false))
}

func (o *maintenanceTaskOwner) stop(ctx context.Context) error {
	return o.wait(ctx, o.seal(true))
}

func (o *maintenanceTaskOwner) wait(ctx context.Context, done <-chan struct{}) error {
	err := waitMaintenanceDone(ctx, done)
	o.mu.Lock()
	defer o.mu.Unlock()
	return errors.Join(err, o.closeErr)
}

func waitMaintenanceDone(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
