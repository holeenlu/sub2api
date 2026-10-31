package openai

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
)

// sendDeadlineGuard owns only the pre-write timer. It never extends a permit
// and never applies a pre-write deadline to a response already being received.
type sendDeadlineGuard struct {
	mu       sync.Mutex
	deadline time.Time
	timer    *time.Timer
	closed   bool
	cancel   context.CancelCauseFunc
}

func (g *sendDeadlineGuard) tighten(deadline time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || deadline.IsZero() {
		return
	}
	if !g.deadline.IsZero() && !deadline.Before(g.deadline) {
		return
	}
	g.deadline = deadline
	if !time.Now().Before(deadline) {
		g.expireLocked()
		return
	}
	if g.timer == nil {
		g.timer = time.AfterFunc(time.Until(deadline), g.expire)
	} else {
		g.timer.Reset(time.Until(deadline))
	}
}

func (g *sendDeadlineGuard) expireLocked() {
	g.closed = true
	if g.timer != nil {
		g.timer.Stop()
	}
	// Context cancellation publishes the failure synchronously. PreparedHTTP's
	// existing lifetime callback owns closing the body; do not perform I/O here.
	g.cancel(execution.ErrDispatchExpired)
}

func (g *sendDeadlineGuard) expire() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		g.expireLocked()
	}
}

func (g *sendDeadlineGuard) wroteHeaders() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	if !g.deadline.IsZero() && !time.Now().Before(g.deadline) {
		g.expireLocked()
		return
	}
	g.closed = true
	if g.timer != nil {
		g.timer.Stop()
	}
}

func (g *sendDeadlineGuard) stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.timer != nil {
		g.timer.Stop()
	}
}
