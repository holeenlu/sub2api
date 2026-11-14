package host

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// BlockingShutdown temporarily owns one existing no-context shutdown call.
// There is exactly one retained shutdown execution and completion handle, even
// across timed-out retries. A caller timeout NEVER means the worker joined.
// Remove each use when that concrete service supplies Stop(ctx) with real join.
// Signal-only legacy methods are explicitly rejected as proof of completion.
type BlockingShutdown struct {
	name           string
	stop           func() error
	joinedOnReturn bool
	once           sync.Once
	done           chan struct{}
	result         error // published by closing done
}

func OwnBlockingShutdown(name string, stop func() error, joinedOnReturn bool) *BlockingShutdown {
	return &BlockingShutdown{name: name, stop: stop, joinedOnReturn: joinedOnReturn, done: make(chan struct{})}
}

// RequestStop starts this owned shutdown once; Stop only waits on that same execution.
func (s *BlockingShutdown) RequestStop() {
	s.once.Do(func() {
		go func() {
			defer close(s.done)
			defer func() {
				if r := recover(); r != nil {
					s.result = fmt.Errorf("%s shutdown panic: %v", s.name, r)
				}
			}()
			s.result = s.stop()
			if !s.joinedOnReturn {
				s.result = errors.Join(s.result, fmt.Errorf("%s stopped admission but exposes no verifiable join", s.name))
			}
		}()
	})
}

func (s *BlockingShutdown) Stop(ctx context.Context) error {
	s.RequestStop()
	select {
	case <-s.done:
		return s.result
	default:
	}
	select {
	case <-s.done:
		return s.result
	case <-ctx.Done():
		return errors.Join(ctx.Err(), fmt.Errorf("%s shutdown is still owned and unjoined", s.name))
	}
}
