package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
)

var errTimingWheelNotStarted = errors.New("timing wheel service has not started")
var newTimingScheduler = func() (*cron.Cron, error) {
	return cron.New(cron.WithLogger(cron.DiscardLogger), cron.WithChain(cron.Recover(cron.DefaultLogger))), nil
}

// TimingWheelService retains the existing named-timer API and ONE existing cron
// engine. go-zero v1.9.4 starts in its constructor and Stop only signals; its
// runTasks goroutines cannot be joined. cron v3.0.1 registers jobs before spawn
// and Stop exposes their real completion context. No new ticker, timer heap or
// business scheduler is introduced here.
type TimingWheelService struct {
	mu         sync.Mutex
	scheduler  *cron.Cron
	closing    bool
	stopDone   chan struct{}
	parent     context.Context
	parentStop func() bool
	// This is only the old name->timer binding, retaining old in-flight
	// occurrences until completion. Actual timing belongs solely to cron.
	names map[string]*namedTimingTask
}
type namedTimingTask struct {
	name    string
	current *timingOccurrence
	active  int
	done    chan struct{}
}
type timingOccurrence struct {
	id       cron.EntryID
	schedule *timingOnceSchedule
	finished bool // TimingWheelService.mu
}

// Next's second call occurs AFTER cron registers/dispatches the callback.
// Remove followed by Entries is a barrier before examining this dispatch fact.
type timingOnceSchedule struct {
	delay      time.Duration
	armed      bool
	dispatched atomic.Bool
}

func (s *timingOnceSchedule) Next(now time.Time) time.Time {
	if s.armed {
		s.dispatched.Store(true)
		return time.Time{}
	}
	s.armed = true
	return now.Add(s.delay)
}

// Construction creates no scheduler, timer, goroutine or callback.
func NewTimingWheelService() (*TimingWheelService, error) {
	return &TimingWheelService{names: make(map[string]*namedTimingTask)}, nil
}

// Start creates the only scheduler and binds its lifetime to parent.
func (s *TimingWheelService) Start(parent context.Context) error {
	if parent == nil {
		return errors.New("timing wheel parent context is nil")
	}
	if err := parent.Err(); err != nil {
		return context.Cause(parent)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errBackgroundWorkStopped
	}
	if s.scheduler != nil {
		if s.parent.Err() != nil {
			return context.Cause(s.parent)
		}
		return nil
	}
	scheduler, err := newTimingScheduler()
	if err != nil {
		return err
	}
	if scheduler == nil {
		return errors.New("timing scheduler is nil")
	}
	if parent.Err() != nil {
		return context.Cause(parent)
	}
	s.scheduler = scheduler
	s.parent = parent
	if s.names == nil {
		s.names = make(map[string]*namedTimingTask)
	}
	s.scheduler.Start()
	s.parentStop = context.AfterFunc(parent, s.RequestStop)
	return nil
}

// Schedule replaces the pending occurrence by name; an executing callback stays
// owned until completion. Scheduling before Start or after closing is rejected.
func (s *TimingWheelService) Schedule(name string, delay time.Duration, fn func()) error {
	return s.schedule(name, delay, fn, false)
}

// ScheduleRecurring preserves the existing completion-relative interval.
func (s *TimingWheelService) ScheduleRecurring(name string, interval time.Duration, fn func()) error {
	return s.schedule(name, interval, fn, true)
}
func (s *TimingWheelService) schedule(name string, delay time.Duration, fn func(), recurring bool) error {
	if delay <= 0 || fn == nil {
		return errors.New("timing task requires positive delay and callback")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return errBackgroundWorkStopped
	}
	if s.scheduler == nil {
		return errTimingWheelNotStarted
	}
	if s.parent.Err() != nil {
		return context.Cause(s.parent)
	}
	task := s.names[name]
	if task == nil {
		task = &namedTimingTask{name: name, done: make(chan struct{})}
		s.names[name] = task
	}
	previous := task.current
	s.addOccurrenceLocked(task, delay, fn, recurring)
	if previous != nil {
		s.removeOccurrenceLocked(task, previous)
	}
	return nil
}
func (s *TimingWheelService) addOccurrenceLocked(task *namedTimingTask, delay time.Duration, fn func(), recurring bool) {
	occ := &timingOccurrence{schedule: &timingOnceSchedule{delay: delay}}
	task.active++
	task.current = occ
	occ.id = s.scheduler.Schedule(occ.schedule, cron.FuncJob(func() {
		defer func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.scheduler.Remove(occ.id)
			s.finishOccurrenceLocked(task, occ)
		}()
		s.mu.Lock()
		allowed := !s.closing && s.parent.Err() == nil && task.current == occ
		s.mu.Unlock()
		if !allowed {
			return
		}
		fn()
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closing && s.parent.Err() == nil && task.current == occ && recurring {
			s.addOccurrenceLocked(task, delay, fn, true)
		}
	}))
}
func (s *TimingWheelService) finishOccurrenceLocked(task *namedTimingTask, occ *timingOccurrence) {
	if occ.finished {
		return
	}
	occ.finished = true
	task.active--
	if task.current == occ {
		task.current = nil
	}
	if task.active == 0 && task.current == nil {
		delete(s.names, task.name)
		close(task.done)
	}
}
func (s *TimingWheelService) removeOccurrenceLocked(task *namedTimingTask, occ *timingOccurrence) {
	s.scheduler.Remove(occ.id)
	// Entries is a barrier behind Remove on this same scheduler loop. Already
	// dispatched callbacks stay counted until their actual deferred completion.
	_ = s.scheduler.Entries()
	if !occ.schedule.dispatched.Load() {
		s.finishOccurrenceLocked(task, occ)
	}
}
func (s *TimingWheelService) cancel(name string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.names[name]
	if task == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	occ := task.current
	task.current = nil
	if occ != nil {
		s.removeOccurrenceLocked(task, occ)
	}
	return task.done
}

// Cancel seals this name's current occurrence without waiting for its body.
func (s *TimingWheelService) Cancel(name string) { s.cancel(name) }

// CancelAndWait also joins replaced, already-dispatched callbacks of this name.
// A deadline error leaves the same completion handle available for a later wait.
func (s *TimingWheelService) CancelAndWait(ctx context.Context, name string) error {
	if ctx == nil {
		return errors.New("timing cancel context is nil")
	}
	done := s.cancel(name)
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// RequestStop seals admission and begins one retained scheduler shutdown.
func (s *TimingWheelService) RequestStop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.stopDone = make(chan struct{})
	scheduler, stop, done := s.scheduler, s.parentStop, s.stopDone
	s.parentStop = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	go func() {
		if scheduler != nil {
			joined := scheduler.Stop()
			<-joined.Done()
			for _, entry := range scheduler.Entries() {
				scheduler.Remove(entry.ID)
			}
		}
		s.mu.Lock()
		// Join proves dispatched callbacks finished. Remaining occurrences were
		// pending and are canceled, never executed early as a pretend drain.
		for name, task := range s.names {
			task.current = nil
			task.active = 0
			close(task.done)
			delete(s.names, name)
		}
		s.mu.Unlock()
		close(done)
	}()
}

// Stop joins the scheduler and every dispatched callback within ctx. A timeout
// reports incomplete work; it does not detach callbacks or claim completion.
func (s *TimingWheelService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("timing stop context is nil")
	}
	s.RequestStop()
	s.mu.Lock()
	done := s.stopDone
	s.mu.Unlock()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// Drain cancels pending timers and joins dispatched callbacks; it never runs
// pending business callbacks early merely to empty the scheduler.
func (s *TimingWheelService) Drain(ctx context.Context) error { return s.Stop(ctx) }
