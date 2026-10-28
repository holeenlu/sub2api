package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// DeferredService keeps the existing last-used batch, with one flush owner.
// Updates and failed restoration share mu; a failed old batch cannot overwrite
// a newer timestamp. flushGate serializes DB writes without holding mu.
type DeferredService struct {
	accountRepo     AccountRepository
	timingWheel     *TimingWheelService
	interval        time.Duration
	lastUsedUpdates sync.Map
	mu              sync.Mutex
	flushGate       chan struct{}
	stopping        bool
	work            backgroundWork
	startOnce       sync.Once
	startErr        error
	finalFlush      *deferredFinalFlush
}
type deferredFinalFlush struct {
	done chan struct{}
	err  error
}

func NewDeferredService(repo AccountRepository, wheel *TimingWheelService, interval time.Duration) *DeferredService {
	return &DeferredService{accountRepo: repo, timingWheel: wheel, interval: interval, flushGate: make(chan struct{}, 1)}
}
func (s *DeferredService) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.timingWheel == nil || s.accountRepo == nil {
		return errors.New("deferred service dependencies unavailable")
	}
	if err := s.work.bind(ctx, s.RequestStop); err != nil {
		return err
	}
	owner, done, err := s.work.begin()
	if err != nil {
		return err
	}
	defer done() // Include timer publication in shutdown ownership.
	s.startOnce.Do(func() {
		s.startErr = s.timingWheel.ScheduleRecurring("deferred:last_used", s.interval, s.flushLastUsed)
	})
	if owner.Err() != nil {
		s.timingWheel.Cancel("deferred:last_used")
		return context.Cause(owner)
	}
	return s.startErr
}
func (s *DeferredService) RequestStop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
	s.work.requestStop()
	if s.timingWheel != nil {
		s.timingWheel.Cancel("deferred:last_used")
	}
}

// Stop first joins normal flushes, then owns a single final flush. A timed-out
// caller leaves that same execution retained, never a competing retry. A later
// call may retry a completed failed flush; pending timestamps remain available.
func (s *DeferredService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("deferred stop context is nil")
	}
	s.RequestStop()
	if err := s.work.join(ctx); err != nil {
		return err
	}
	if s.timingWheel != nil {
		if err := s.timingWheel.CancelAndWait(ctx, "deferred:last_used"); err != nil {
			return err
		}
	}
	s.mu.Lock()
	run := s.finalFlush
	if run != nil {
		select {
		case <-run.done:
			if run.err == nil {
				s.mu.Unlock()
				return nil
			}
			run = nil
		default:
		}
	}
	if run == nil {
		pending := false
		s.lastUsedUpdates.Range(func(_, _ any) bool { pending = true; return false })
		if !pending {
			s.mu.Unlock()
			return nil
		}
		if ctx.Err() != nil {
			s.mu.Unlock()
			return context.Cause(ctx)
		}
		done, err := s.work.beginCleanup(ctx)
		if err != nil {
			s.mu.Unlock()
			return err
		}
		run = &deferredFinalFlush{done: make(chan struct{})}
		s.finalFlush = run
		go func(run *deferredFinalFlush) { defer close(run.done); defer done(); run.err = s.flush(ctx) }(run)
	}
	s.mu.Unlock()
	select {
	case <-run.done:
		return run.err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
func (s *DeferredService) Drain(ctx context.Context) error { return s.Stop(ctx) }

func (s *DeferredService) ScheduleLastUsedUpdate(accountID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return errBackgroundWorkStopped
	}
	s.lastUsedUpdates.Store(accountID, time.Now())
	return nil
}
func (s *DeferredService) flushLastUsed() {
	parent, done, err := s.work.begin()
	if err != nil {
		return
	}
	defer done()
	if err = s.flush(parent); err != nil {
		log.Printf("[DeferredService] last-used flush failed: %v", err)
	}
}
func (s *DeferredService) flush(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	s.mu.Lock()
	if s.flushGate == nil {
		s.flushGate = make(chan struct{}, 1)
	}
	gate := s.flushGate
	s.mu.Unlock()
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return context.Cause(ctx)
	}
	defer func() { <-gate }()
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	updates := make(map[int64]time.Time)
	s.mu.Lock()
	s.lastUsedUpdates.Range(func(key, value any) bool {
		id, ok := key.(int64)
		if !ok {
			return true
		}
		ts, ok := value.(time.Time)
		if !ok {
			return true
		}
		updates[id] = ts
		s.lastUsedUpdates.Delete(key)
		return true
	})
	s.mu.Unlock()
	if len(updates) == 0 {
		return nil
	}
	var err error
	if s.accountRepo == nil {
		err = errors.New("deferred repository unavailable")
	} else {
		err = s.accountRepo.BatchUpdateLastUsed(ctx, updates)
	}
	if err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		for id, ts := range updates {
			current, ok := s.lastUsedUpdates.Load(id)
			latest, typed := current.(time.Time)
			if !ok || !typed || ts.After(latest) {
				s.lastUsedUpdates.Store(id, ts)
			}
		}
		return err
	}
	log.Printf("[DeferredService] BatchUpdateLastUsed flushed %d accounts", len(updates))
	return nil
}
