// Package routing owns dispatch qualification and capacity lifetimes. Storage
// supplies fenced operations; neither Redis nor service entities enter this package.
package routing

import (
	"context"
	"errors"
	"sync"
	"time"
)

// leaseLifetime is the single owner of renewal, cancellation and cleanup. The
// cache's token fences remote operations; this deadline is a conservative local
// permission bound, measured from the start of the last confirmed operation.
type leaseLifetime struct {
	ctx                    context.Context
	cancel                 context.CancelCauseFunc
	stop                   chan struct{}
	done                   chan struct{}
	once                   sync.Once
	ttl, interval, timeout time.Duration
	failureGrace           time.Duration // zero uses ttl; recovery retains its shorter error grace
	confirmedUntil         time.Time
	lost                   error
	renew                  func(context.Context) (bool, error)
	cleanup                func(context.Context) error
	observe                func(error, bool)
	onLost                 func(error)
	cleanupErr             error // written before done closes, read only after joining
}

func (l *leaseLifetime) start(parent context.Context) {
	l.ctx, l.cancel = context.WithCancelCause(parent)
	l.stop = make(chan struct{})
	l.done = make(chan struct{})
	go l.run()
}

func (l *leaseLifetime) release() error {
	if l == nil || l.done == nil {
		return nil
	}
	l.once.Do(func() { close(l.stop); l.cancel(nil) })
	<-l.done
	return l.cleanupErr
}

func (l *leaseLifetime) run() {
	defer close(l.done)
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(l.ctx), l.timeout)
		defer cancel()
		l.cleanupErr = l.cleanup(ctx)
	}()
	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	expires := time.NewTimer(time.Until(l.confirmedUntil))
	defer expires.Stop()
	lose := func(cause error) {
		l.cancel(l.lost)
		// Release or parent cancellation may win the race with expiry. In that
		// case there is no lease-loss event to attribute or abort again.
		if !errors.Is(context.Cause(l.ctx), l.lost) {
			return
		}
		if l.observe != nil {
			l.observe(cause, true)
		}
		if l.onLost != nil {
			l.onLost(cause)
		}
	}
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.stop:
			return
		case <-expires.C:
			lose(l.lost)
			return
		case <-ticker.C:
			started := time.Now()
			deadline := minTime(started.Add(l.timeout), l.confirmedUntil)
			ctx, cancel := context.WithDeadline(l.ctx, deadline)
			owned, err := l.renew(ctx)
			cancel()
			if l.ctx.Err() != nil {
				return
			}
			// A delayed confirmation never restores a permission already expired.
			if !time.Now().Before(l.confirmedUntil) || (err == nil && !owned) {
				if err == nil {
					err = l.lost
				}
				lose(err)
				return
			}
			if err != nil {
				grace := l.failureGrace
				if grace <= 0 {
					grace = l.ttl
				}
				if time.Since(l.confirmedUntil.Add(-l.ttl)) >= grace {
					lose(err)
					return
				}
				if l.observe != nil {
					l.observe(err, false)
				}
				continue
			}
			l.confirmedUntil = started.Add(l.ttl)
			expires.Reset(time.Until(l.confirmedUntil))
		}
	}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
