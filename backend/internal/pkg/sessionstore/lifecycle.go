package sessionstore

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrClosed rejects new work before Start and after Drain or Stop.
var ErrClosed = errors.New("session owner is not accepting work")

// Lifecycle owns one periodic session/cache task and its admitted operations.
// OAuth stores and cached-content recovery share this lifecycle, not their data or CAS.
// Construction creates no goroutine. A stopped owner cannot be restarted.
type Lifecycle struct {
	mu       sync.Mutex
	interval time.Duration
	sweep    func(context.Context)
	ctx      context.Context
	cancel   context.CancelFunc
	running  bool
	draining bool
	joined   bool
	closeErr error // first owned-resource close failure; published before finish
	drain    chan struct{}
	done     chan struct{}
	idle     chan struct{}
	active   map[*sessionOperation]context.CancelFunc
}

type sessionOperation struct{ owner *Lifecycle }
type sessionOperationKey struct{}

// NewLifecycle only allocates ownership state. Start validates interval and sweep.
func NewLifecycle(interval time.Duration, sweep func(context.Context)) *Lifecycle {
	idle := make(chan struct{})
	close(idle)
	return &Lifecycle{
		interval: interval, sweep: sweep,
		drain: make(chan struct{}), done: make(chan struct{}),
		active: make(map[*sessionOperation]context.CancelFunc), idle: idle,
	}
}

// Start starts the janitor after its dependencies exist. ctx owns the lifetime.
func (s *Lifecycle) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.interval <= 0 || s.sweep == nil {
		return errors.New("session owner requires a positive interval and sweep")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining {
		return ErrClosed
	}
	if s.running {
		return s.ctx.Err()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.running = true
	go s.run()
	return nil
}

// Begin admits and owns a session/cache operation until finish is called. Nested calls
// may finish an already admitted operation during Drain; ended contexts do not
// bypass admission. Stop cancels every admitted context and waits for finish.
func (s *Lifecycle) Begin(ctx context.Context) (context.Context, func(), error) {
	return s.begin(ctx, false)
}

// BeginDetached permits a bounded cache mutation to outlive its HTTP caller.
// Nested recovery retains its existing cancellation/deadline. Owner Stop always
// cancels the returned context; callers must also apply their operation timeout.
func (s *Lifecycle) BeginDetached(ctx context.Context) (context.Context, func(), error) {
	return s.begin(ctx, true)
}

func (s *Lifecycle) begin(ctx context.Context, detach bool) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if op, ok := ctx.Value(sessionOperationKey{}).(*sessionOperation); ok && op.owner == s {
		if _, active := s.active[op]; active {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			return ctx, func() {}, nil
		}
	}
	if !s.running || s.draining || s.ctx.Err() != nil {
		return nil, nil, ErrClosed
	}
	if detach {
		ctx = context.WithoutCancel(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	op := &sessionOperation{owner: s}
	owned, cancel := context.WithCancel(ctx)
	if len(s.active) == 0 {
		s.idle = make(chan struct{})
	}
	s.active[op] = cancel
	var once sync.Once
	return context.WithValue(owned, sessionOperationKey{}, op), func() {
		once.Do(func() {
			cancel()
			s.mu.Lock()
			delete(s.active, op)
			if len(s.active) == 0 {
				close(s.idle)
			}
			s.finishLocked()
			s.mu.Unlock()
		})
	}, nil
}

// Drain seals admission and stops future sweeps, allowing accepted work to end.
// A deadline does not discard the completion handle; another call can join it.
func (s *Lifecycle) Drain(ctx context.Context) error {
	s.mu.Lock()
	s.sealLocked()
	s.finishLocked()
	s.mu.Unlock()
	return s.wait(ctx)
}

// Stop seals admission, cancels accepted work, and joins the actual janitor and
// all operations. No waiter goroutine or new shutdown execution is created.
func (s *Lifecycle) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.sealLocked()
	if s.cancel != nil {
		s.cancel()
	}
	for _, cancel := range s.active {
		cancel()
	}
	s.finishLocked()
	s.mu.Unlock()
	return s.wait(ctx)
}

func (s *Lifecycle) sealLocked() {
	if !s.draining {
		s.draining = true
		close(s.drain)
	}
}

func (s *Lifecycle) finishLocked() {
	if s.draining && !s.running && len(s.active) == 0 && !s.joined {
		s.joined = true
		if s.cancel != nil {
			s.cancel()
		}
		close(s.done)
	}
}

// RecordCloseError preserves the first resource-close failure for Drain/Stop.
// The resource owner calls this before its finish callback. It does not replace
// a successfully committed business result or close borrowed clients.
func (s *Lifecycle) RecordCloseError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closeErr == nil {
		s.closeErr = err
	}
}

func (s *Lifecycle) closeResult() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeErr
}

func (s *Lifecycle) wait(ctx context.Context) error {
	select {
	case <-s.done:
		return s.closeResult()
	default:
	}
	select {
	case <-s.done:
		return s.closeResult()
	case <-ctx.Done():
		return errors.Join(ctx.Err(), s.closeResult())
	}
}

func (s *Lifecycle) run() {
	defer func() {
		s.mu.Lock()
		s.sealLocked()
		idle := s.idle
		s.mu.Unlock()
		// Retain this owner while draining so parent cancellation still cancels
		// accepted operations after the periodic loop has stopped admitting work.
		select {
		case <-idle:
		case <-s.ctx.Done():
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.running = false
		if s.ctx.Err() != nil {
			for _, cancel := range s.active {
				cancel()
			}
		}
		s.finishLocked()
	}()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.drain:
			return
		case <-ticker.C:
			s.mu.Lock()
			closing := s.draining || s.ctx.Err() != nil
			s.mu.Unlock()
			if closing {
				return
			}
			s.sweep(s.ctx)
		}
	}
}
