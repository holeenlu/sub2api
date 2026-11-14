package service

import (
	"context"
	"errors"
	"sync"
)

var errOpenAIWSPoolDraining = errors.New("openai ws pool is draining")

const (
	openAIWSPoolRunning = iota
	openAIWSPoolDraining
	openAIWSPoolStopping
)

// Lifecycle is owned by the existing pool. mu never protects network I/O or
// waits. Account locks may precede mu for publication; the reverse is forbidden.
type openAIWSPoolLifecycle struct {
	mu       sync.Mutex
	state    int
	ctx      context.Context
	cancel   context.CancelCauseFunc
	changed  chan struct{}
	work     int
	conns    map[*openAIWSConn]bool // value records a scheduled close owner
	closeErr error                  // first observed shutdown-close failure, never silently cleared
}

func (p *openAIWSConnPool) initLifecycleLocked() {
	l := &p.lifecycle
	if l.ctx == nil {
		l.ctx, l.cancel = context.WithCancelCause(context.Background())
		l.changed = make(chan struct{})
		l.conns = make(map[*openAIWSConn]bool)
	}
}

func (p *openAIWSConnPool) signalLifecycleLocked() {
	close(p.lifecycle.changed)
	p.lifecycle.changed = make(chan struct{})
}

// Registration precedes goroutine launch/connection establishment, so shutdown
// cannot observe an empty owner while delayed work is still about to start.
func (p *openAIWSConnPool) beginPoolWork(parent context.Context) (context.Context, func(), error) {
	if p == nil || parent == nil {
		return nil, nil, errOpenAIWSPoolDraining
	}
	l := &p.lifecycle
	l.mu.Lock()
	p.initLifecycleLocked()
	if l.state != openAIWSPoolRunning {
		l.mu.Unlock()
		return nil, nil, errOpenAIWSPoolDraining
	}
	l.work++
	shutdown := l.ctx
	l.mu.Unlock()
	ctx, cancel := context.WithCancelCause(parent)
	stop := context.AfterFunc(shutdown, func() { cancel(errOpenAIWSPoolDraining) })
	var once sync.Once
	return ctx, func() { once.Do(func() { stop(); cancel(context.Canceled); p.finishPoolWork() }) }, nil
}

func (p *openAIWSConnPool) finishPoolWork() {
	p.lifecycle.mu.Lock()
	p.lifecycle.work--
	p.signalLifecycleLocked()
	p.lifecycle.mu.Unlock()
}

func (p *openAIWSConnPool) poolRunning() bool {
	if p == nil {
		return false
	}
	p.lifecycle.mu.Lock()
	defer p.lifecycle.mu.Unlock()
	return p.lifecycle.state == openAIWSPoolRunning
}

// Called with the account lock held. This is the publication fence shared with
// Drain/Stop, including late dial and prewarm returns.
func (p *openAIWSConnPool) publishConnLocked(ap *openAIWSAccountPool, c *openAIWSConn) bool {
	p.lifecycle.mu.Lock()
	defer p.lifecycle.mu.Unlock()
	if p.lifecycle.state != openAIWSPoolRunning {
		return false
	}
	ap.conns[c.id] = c
	return true
}

func (p *openAIWSConnPool) trackConn(c *openAIWSConn) {
	if c == nil {
		return
	}
	l := &p.lifecycle
	l.mu.Lock()
	p.initLifecycleLocked()
	if _, exists := l.conns[c]; exists {
		l.mu.Unlock()
		return
	}
	added := false
	c.poolWatchOnce.Do(func() { added = true })
	if !added {
		l.mu.Unlock()
		return
	}
	l.conns[c] = false
	p.signalLifecycleLocked()
	l.mu.Unlock()
	go p.joinConn(c)
}

func (p *openAIWSConnPool) joinConn(c *openAIWSConn) {
	// closedCh is only a cancellation signal, never completion evidence.
	<-c.closeDone
	<-c.readerDone
	p.noteCloseFailure(c)
	if ap, ok := p.getAccountPool(c.accountID); ok && ap != nil {
		ap.mu.Lock()
		if ap.conns[c.id] == c {
			delete(ap.conns, c.id)
			delete(ap.pinnedConns, c.id)
			ap.signalChangedLocked()
		}
		ap.mu.Unlock()
	}
	p.lifecycle.mu.Lock()
	delete(p.lifecycle.conns, c)
	p.signalLifecycleLocked()
	p.lifecycle.mu.Unlock()
}

// Close is a tracked task, not an unowned timeout goroutine. A non-cooperative
// transport stays visible to subsequent Stop calls after a deadline error.
func (p *openAIWSConnPool) closeConnAsync(c *openAIWSConn) {
	if c == nil {
		return
	}
	l := &p.lifecycle
	l.mu.Lock()
	p.initLifecycleLocked()
	_, exists := l.conns[c]
	if !exists {
		added := false
		c.poolWatchOnce.Do(func() { added = true })
		if !added {
			l.mu.Unlock()
			return
		}
	}
	if l.conns[c] {
		l.mu.Unlock()
		return
	}
	l.conns[c] = true
	l.work++
	l.mu.Unlock()
	if !exists {
		go p.joinConn(c)
	}
	go func() { defer p.finishPoolWork(); c.abort(); p.noteCloseFailure(c) }()
}

func (p *openAIWSConnPool) noteCloseFailure(c *openAIWSConn) {
	err := c.closeFailure()
	if err == nil {
		return
	}
	p.lifecycle.mu.Lock()
	if p.lifecycle.state != openAIWSPoolRunning && p.lifecycle.closeErr == nil {
		p.lifecycle.closeErr = err
	}
	p.lifecycle.mu.Unlock()
}

func (p *openAIWSConnPool) retireReleasedConn(c *openAIWSConn) {
	if p != nil && !p.poolRunning() {
		p.closeConnAsync(c)
	}
}

func (p *openAIWSConnPool) beginShutdown(force bool) {
	l := &p.lifecycle
	l.mu.Lock()
	p.initLifecycleLocked()
	first := l.state == openAIWSPoolRunning
	if force {
		l.state = openAIWSPoolStopping
	} else if first {
		l.state = openAIWSPoolDraining
	}
	l.cancel(errOpenAIWSPoolDraining)
	if first {
		if p.workerStopCh != nil {
			close(p.workerStopCh)
		}
		l.work += 2 // worker join and account discovery each have a real owner
	}
	conns := make([]*openAIWSConn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	p.signalLifecycleLocked()
	l.mu.Unlock()
	if first {
		go func() { defer p.finishPoolWork(); p.workerWg.Wait() }()
		go func() {
			defer p.finishPoolWork()
			// Also adopt pre-existing/manual pool entries. No lock acquisition or
			// transport closure here can make the caller exceed its context bound.
			p.accounts.Range(func(_, v any) bool {
				ap, ok := v.(*openAIWSAccountPool)
				if !ok || ap == nil {
					return true
				}
				ap.mu.Lock()
				ap.lastAcquire = nil
				ap.signalChangedLocked()
				found := make([]*openAIWSConn, 0, len(ap.conns))
				for _, c := range ap.conns {
					if c != nil {
						found = append(found, c)
					}
				}
				ap.mu.Unlock()
				for _, c := range found {
					p.trackConn(c)
					if p.stopping() || !c.isLeased() {
						p.closeConnAsync(c)
					}
				}
				return true
			})
		}()
	}
	for _, c := range conns {
		if force || !c.isLeased() {
			p.closeConnAsync(c)
		}
	}
}

func (p *openAIWSConnPool) stopping() bool {
	p.lifecycle.mu.Lock()
	defer p.lifecycle.mu.Unlock()
	return p.lifecycle.state == openAIWSPoolStopping
}

func (p *openAIWSConnPool) waitStopped(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			p.beginShutdown(true)
			return context.Cause(ctx)
		}
		p.lifecycle.mu.Lock()
		done := p.lifecycle.work == 0 && len(p.lifecycle.conns) == 0
		closeErr := p.lifecycle.closeErr
		changed := p.lifecycle.changed
		p.lifecycle.mu.Unlock()
		if done {
			return errors.Join(context.Cause(ctx), closeErr)
		}
		select {
		case <-changed:
		case <-ctx.Done():
			p.beginShutdown(true)
			return context.Cause(ctx)
		}
	}
}

// Drain seals new acquisition/publication immediately and lets existing leases
// finish until ctx expires. At expiry it cancels/closes them and returns the
// deadline error, even if forced cleanup subsequently completes. Stop with a
// fresh bounded context can join that cleanup; neither call resets admission.
func (p *openAIWSConnPool) Drain(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("openai ws drain context is nil")
	}
	p.beginShutdown(false)
	return p.waitStopped(ctx)
}

// Stop cancels all owned connections immediately and joins workers, pending
// acquire/dial/prewarm, actual network close and resident readers within ctx.
// It does not wait for callers to return already force-closed lease handles.
func (p *openAIWSConnPool) Stop(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("openai ws stop context is nil")
	}
	p.beginShutdown(true)
	return p.waitStopped(ctx)
}

// Existing callers have no context/error slot. Keep that entry until root
// assembly moves to Stop/Drain; errors remain observable rather than pretending
// an uncooperative dial/reader stopped. The bound reuses the existing probe cap.
func (p *openAIWSConnPool) closeLegacy() {
	ctx, cancel := context.WithTimeout(context.Background(), openAIWSProbePingTO)
	defer cancel()
	if err := p.Stop(ctx); err != nil {
		logOpenAIWSModeWarn("pool_stop_incomplete cause=%s", err.Error())
	}
}
