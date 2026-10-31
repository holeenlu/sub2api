package host

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ShutdownPhase orders producer joins before billing and storage teardown.
type ShutdownPhase int

const (
	Producers ShutdownPhase = iota
	Usage
	Caches
	Observations
	Infrastructure
)

// Component is one real resource, not a second worker. Constructed resources
// register immediately so an error later in Wire construction cannot orphan them.
// A stop that cannot prove join must return an error; dependent phases stay open.
type Component struct {
	ShutdownOrder int
	RequestStop   func()
	// DrainBeforeUsage stops and joins cache fills before any Usage request,
	// drain or stop can perform final invalidation. Final Stop stays in Caches.
	DrainBeforeUsage bool

	Name        string
	Phase       ShutdownPhase
	StartOrder  int
	Start       func(context.Context) error
	Drain       func(context.Context) error
	Stop        func(context.Context) error
	Constructed bool
}

type ownedComponent struct {
	Component
	active, drained, joined bool
}

// Lifetime is the application's construction journal and ordered runtime owner.
// It never manufactures a goroutine to time out an unjoinable Stop method.
type Lifetime struct {
	cleanupGate     chan struct{}
	registrationErr error

	mu               sync.Mutex
	entries          []*ownedComponent
	started, closing bool
}

func NewLifetime() *Lifetime { return &Lifetime{cleanupGate: make(chan struct{}, 1)} }

// Add records ownership even when the static plan is invalid, so construction
// rollback can still release already-created resources. Start surfaces the error.
func (l *Lifetime) Add(c Component) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started || l.closing {
		l.registrationErr = errors.Join(l.registrationErr, errors.New("late lifecycle registration"))
		return
	}
	if c.Name == "" || c.Stop == nil {
		l.registrationErr = errors.Join(l.registrationErr, errors.New("component requires name and stop"))
	}
	if c.DrainBeforeUsage && (c.Phase != Caches || c.Drain == nil) {
		l.registrationErr = errors.Join(l.registrationErr, fmt.Errorf("%s requires a cache drain for the pre-usage checkpoint", c.Name))
	}
	for _, e := range l.entries {
		if e.Name == c.Name {
			l.registrationErr = errors.Join(l.registrationErr, fmt.Errorf("duplicate lifecycle component %s", c.Name))
		}
	}
	l.entries = append(l.entries, &ownedComponent{Component: c, active: c.Constructed})
}

func (l *Lifetime) Start(ctx context.Context) error {
	l.mu.Lock()
	if l.registrationErr != nil {
		err := l.registrationErr
		l.mu.Unlock()
		return err
	}
	if l.closing {
		l.mu.Unlock()
		return errors.New("application is stopping")
	}
	if l.started {
		l.mu.Unlock()
		return nil
	}
	l.started = true
	entries := append([]*ownedComponent(nil), l.entries...)
	l.mu.Unlock()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].StartOrder < entries[j].StartOrder })
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.Start == nil {
			continue
		}
		l.mu.Lock()
		e.active = true
		l.mu.Unlock()
		if err := e.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", e.Name, err)
		}
	}
	return nil
}

// Cleanup is retryable: a completed component is never closed twice. Failure in
// a phase stops teardown of its dependencies, while independent peers in that
// same phase are still asked to stop. Root ingress must already be joined.
func (l *Lifetime) Cleanup(ctx context.Context) error {
	select {
	case l.cleanupGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-l.cleanupGate }()
	l.mu.Lock()
	l.closing = true
	entries := append([]*ownedComponent(nil), l.entries...)
	l.mu.Unlock()
	// Reverse construction order within the same explicit dependency checkpoint.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Phase != entries[j].Phase {
			return entries[i].Phase < entries[j].Phase
		}
		return entries[i].ShutdownOrder < entries[j].ShutdownOrder
	})
	var result error
	cacheFillsDrained := false
	for first := 0; first < len(entries); {
		if entries[first].Phase >= Usage && !cacheFillsDrained {
			// Use the existing ownership journal: no second registry or worker.
			// Failure blocks even Usage RequestStop, which may itself flush final
			// settlement. Cache Stop and storage close must also remain pending.
			for _, e := range entries {
				if !e.DrainBeforeUsage {
					continue
				}
				if err := l.drain(ctx, e); err != nil {
					return errors.Join(result, fmt.Errorf("pre-usage drain %s: %w", e.Name, err))
				}
			}
			cacheFillsDrained = true
		}
		last := first + 1
		for last < len(entries) && entries[last].Phase == entries[first].Phase && entries[last].ShutdownOrder == entries[first].ShutdownOrder {
			last++
		}
		group := entries[first:last]
		unjoined := false
		// Independent producers receive stop requests before waiting on any peer.
		// Every request has a retained owner/done handle, not a fire-and-forget waiter.
		for _, e := range group {
			l.mu.Lock()
			active := e.active && !e.joined
			l.mu.Unlock()
			if active && e.RequestStop != nil {
				e.RequestStop()
			}
		}
		for _, e := range group {
			l.mu.Lock()
			active := e.active && !e.joined
			l.mu.Unlock()
			if !active {
				continue
			}
			if err := ctx.Err(); err != nil {
				return errors.Join(result, err)
			}
			if err := l.drain(ctx, e); err != nil {
				result = errors.Join(result, fmt.Errorf("drain %s: %w", e.Name, err))
			}
			if e.Stop == nil {
				result = errors.Join(result, fmt.Errorf("%s has no stop owner", e.Name))
				unjoined = true
				continue
			}
			if err := e.Stop(ctx); err != nil {
				result = errors.Join(result, fmt.Errorf("stop %s: %w", e.Name, err))
				unjoined = true
				continue
			}
			l.mu.Lock()
			e.joined = true
			l.mu.Unlock()
		}
		if unjoined {
			return result
		}
		first = last
	}
	return result
}

// drain is serialized by cleanupGate. A completed early drain is retained
// across retries without marking the component stopped or releasing its owner.
func (l *Lifetime) drain(ctx context.Context, e *ownedComponent) error {
	l.mu.Lock()
	needed := e.active && !e.joined && !e.drained
	l.mu.Unlock()
	if !needed {
		return nil
	}
	if e.Drain == nil {
		if e.DrainBeforeUsage {
			return errors.New("cache fill drain is missing")
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.Drain(ctx); err != nil {
		return err
	}
	l.mu.Lock()
	e.drained = true
	l.mu.Unlock()
	return nil
}

// Pending names actual ownership that has not been joined; it is not a count of
// methods invoked. It remains non-empty after timeouts and signal-only stops.
func (l *Lifetime) Pending() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, e := range l.entries {
		if e.active && !e.joined {
			out = append(out, e.Name)
		}
	}
	return out
}
