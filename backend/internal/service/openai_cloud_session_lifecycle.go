package service

import (
	"context"
	"errors"
)

var (
	ErrOpenAICloudClosing          = &cloudGatewayError{status: 503, code: "cloud_gateway_stopping", message: "Cloud sessions are stopping"}
	ErrOpenAICloudFactsUnavailable = errors.New("Cloud session facts could not be persisted")
)

// addPendingLocked registers work before I/O or goroutine publication. The
// one counter belongs to this existing sessions owner; it includes admitted
// operations, in-flight reservations and actors, not a second actor registry.
func (s *OpenAICloudSessions) addPendingLocked() {
	if s.pending == 0 {
		s.idle = make(chan struct{})
	}
	s.pending++
}

func (s *OpenAICloudSessions) finishPendingLocked(err error) {
	// Completed ordinary requests cannot poison a subsequent shutdown.
	if s.closing {
		if errors.Is(err, ErrOpenAICloudFactsUnavailable) {
			s.shutdownErr = ErrOpenAICloudFactsUnavailable
		} else if err != nil && s.shutdownErr == nil {
			s.shutdownErr = err
		}
	}
	s.pending--
	if s.pending == 0 {
		close(s.idle)
	}
}

func (s *OpenAICloudSessions) operationContext(parent context.Context) (context.Context, func()) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	if s.ctx == nil {
		return ctx, cancel
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(s.ctx, func() { cancel(); close(joined) })
	return ctx, func() {
		if !stop() {
			<-joined
		}
		cancel()
	}
}

func (s *OpenAICloudSessions) beginOperation(parent context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil, nil, ErrOpenAICloudClosing
	}
	s.addPendingLocked()
	s.mu.Unlock()
	ctx, cancel := s.operationContext(parent)
	return ctx, func() {
		cancel()
		s.mu.Lock()
		s.finishPendingLocked(nil)
		s.mu.Unlock()
	}, nil
}

// Stop closes admission before cancellation, then waits for actual operations,
// reservations, actors and their child tasks. Timeout does not release an owner
// or mark unknown usage as zero: it returns incomplete while that work remains
// tracked, and a later Stop may join it. Keep billing, Redis and PG available
// until this returns; a persistence error is not a successful drain.
func (s *OpenAICloudSessions) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	s.closing = true
	if s.idle == nil {
		s.idle = make(chan struct{})
		close(s.idle)
	}
	done, cancel := s.idle, s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
	default:
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdownErr
}

// Drain has the same terminal-fact and join contract as Stop. Neither method
// stops the Cloud billing consumer or closes shared infrastructure.
func (s *OpenAICloudSessions) Drain(ctx context.Context) error { return s.Stop(ctx) }

// Only initialization is linked to actor cancellation. After successful
// handoff, keep the RPC lifetime alive for interrupt/terminal usage collection.
// Stop the cancellation callback and join it before deciding that handoff won.
func (a *cloudSessionActor) connectUntilOwned(command OpenAICloudCommand, lifetime context.Context, cancelLifetime context.CancelFunc) (*cloudRPC, error) {
	if err := a.ctx.Err(); err != nil {
		return nil, err
	}
	joined := make(chan struct{})
	stop := context.AfterFunc(a.ctx, func() { cancelLifetime(); close(joined) })
	rpc, err := a.prepareConnection(command, lifetime)
	if !stop() {
		<-joined
	}
	if a.ctx.Err() != nil {
		if rpc != nil {
			a.cleanupErr = errors.Join(a.cleanupErr, rpc.Close())
		}
		return nil, a.ctx.Err()
	}
	if err != nil && rpc != nil {
		a.cleanupErr = errors.Join(a.cleanupErr, rpc.Close())
		rpc = nil
	}
	return rpc, err
}
