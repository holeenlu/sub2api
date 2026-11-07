package service

import (
	"context"
	"errors"
	"os"
	"sync"
)

var errGatewayRuntimeStopped = errors.New("gateway runtime is stopping")

// The telemetry worker keeps its own actual completion. This state serializes
// startup/admission and retains the debug file close until every writer returns.
type gatewayRuntimeState struct {
	mu             sync.Mutex
	stopping       bool
	debugStartDone chan struct{}
	debugStartErr  error
	debugWriters   sync.WaitGroup
	debugCloseDone chan struct{}
	debugCloseErr  error // published by closing debugCloseDone
}

func (s *GatewayService) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("gateway start context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.runtime.mu.Lock()
	if s.runtime.stopping {
		s.runtime.mu.Unlock()
		return errGatewayRuntimeStopped
	}
	start := s.runtime.debugStartDone == nil
	if start {
		s.runtime.debugStartDone = make(chan struct{})
	}
	done := s.runtime.debugStartDone
	s.runtime.mu.Unlock()
	if start {
		// Do not hold the admission mutex across filesystem IO. A concurrent
		// Stop owns this initialization and can return its own deadline.
		if s.debugGatewayBodyPath != "" {
			s.runtime.debugStartErr = s.initDebugGatewayBodyFile(s.debugGatewayBodyPath)
		}
		close(done)
	} else {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	err := s.runtime.debugStartErr
	if err != nil {
		return err
	}
	return s.StartAccountTelemetry(ctx)
}

func (s *GatewayService) Drain(ctx context.Context) error {
	return s.StopAccountTelemetry(ctx)
}

// Stop joins all telemetry jobs before closing their shared debug output. A
// deadline leaves the same worker and file-close handles available to retry.
func (s *GatewayService) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("gateway stop context is nil")
	}
	telemetryErr := s.StopAccountTelemetry(ctx)
	if telemetryErr != nil {
		s.runtime.mu.Lock()
		worker := s.accountTelemetryWorker
		s.runtime.mu.Unlock()
		if worker != nil {
			select {
			case <-worker.done:
				// A failed leader release must stay visible, but the senders
				// have joined and no longer need their debug output open.
			default:
				return telemetryErr
			}
		}
	}
	s.runtime.mu.Lock()
	if s.runtime.debugCloseDone == nil {
		s.runtime.debugCloseDone = make(chan struct{})
		done, started := s.runtime.debugCloseDone, s.runtime.debugStartDone
		// Admission was sealed under mu before this Wait. No new writer can
		// increment the count or obtain the detached file.
		go func() {
			if started != nil {
				<-started
			}
			s.runtime.debugWriters.Wait()
			f := s.debugGatewayBodyFile.Swap(nil)
			if f != nil {
				s.runtime.debugCloseErr = f.Close()
			}
			close(done)
		}()
	}
	done := s.runtime.debugCloseDone
	s.runtime.mu.Unlock()
	select {
	case <-done:
		return errors.Join(telemetryErr, s.runtime.debugCloseErr)
	default:
	}
	select {
	case <-done:
		return errors.Join(telemetryErr, s.runtime.debugCloseErr)
	case <-ctx.Done():
		return errors.Join(telemetryErr, ctx.Err())
	}
}

func (s *GatewayService) acquireDebugGatewayBodyFile() (*os.File, func()) {
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	if s.runtime.stopping {
		return nil, nil
	}
	f := s.debugGatewayBodyFile.Load()
	if f == nil {
		return nil, nil
	}
	s.runtime.debugWriters.Add(1)
	var once sync.Once
	return f, func() { once.Do(s.runtime.debugWriters.Done) }
}
