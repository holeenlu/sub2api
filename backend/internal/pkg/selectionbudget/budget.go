// Package selectionbudget bounds active storage work during one account selection.
// It is shared by services and repository hooks so nested calls cannot hide I/O.
package selectionbudget

import (
	"context"
	"errors"
	"sync"
	"time"
)

const Limit = 64
const Timeout = 3 * time.Second

var ErrExhausted = errors.New("account selection external operation budget exhausted")

type key struct{}
type scopeKey struct{}
type aggregateKey struct{}
type preparationKey struct{}

// PreparationContext accounts credential storage work in the turn aggregate,
// without applying a selection timeout to the provider refresh RPC itself.
func PreparationContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, preparationKey{}, true)
}

// Aggregate survives individual selections, including protocol/account fallback.
// Cleanup is deliberately excluded so an exhausted turn still releases slots.
type Aggregate struct {
	mu         sync.Mutex
	operations int
}

const TurnLimit = 256

func WithAggregate(ctx context.Context, aggregate *Aggregate) context.Context {
	return context.WithValue(ctx, aggregateKey{}, aggregate)
}

func (a *Aggregate) Operations() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.operations
}

type Snapshot struct {
	Operations       int  `json:"operations"`
	SQL              int  `json:"sql"`
	Redis            int  `json:"redis_executions"`
	Commands         int  `json:"redis_commands"`
	Cleanup          int  `json:"cleanup_operations"`
	Exhausted        bool `json:"exhausted"`
	DependencyErrors int  `json:"dependency_errors"`
}

type Budget struct {
	mu      sync.Mutex
	state   Snapshot
	failure error
}

// RecordError preserves a failed dependency even when legacy eligibility code
// returns only a boolean. A later successful selection remains successful.
func RecordError(ctx context.Context, err error) {
	b := From(ctx)
	if b == nil || err == nil || errors.Is(err, ErrExhausted) || ctx.Err() != nil {
		return
	}
	if s, ok := ctx.Value(scopeKey{}).(scope); ok && s.cleanup {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failure = err
	b.state.DependencyErrors++
}

func (b *Budget) DependencyError() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failure
}

// Start reuses an enclosing selection budget. Cleanup must use CleanupContext.
func Start(ctx context.Context) (context.Context, *Budget, context.CancelFunc) {
	if b := From(ctx); b != nil {
		return ctx, b, func() {}
	}
	b := &Budget{}
	ctx, cancel := context.WithTimeout(context.WithValue(ctx, key{}, b), Timeout)
	return ctx, b, cancel
}

func From(ctx context.Context) *Budget {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(key{}).(*Budget)
	return b
}

func (b *Budget) Snapshot() Snapshot {
	if b == nil {
		return Snapshot{}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Budget) Remaining() int { return Limit - b.Snapshot().Operations }

type scope struct {
	ceiling int
	cleanup bool
}

// Reserve leaves operations for later business layers. The ceiling is absolute
// and can never increase the enclosing scope. No counter is reset on fallback.
func Reserve(ctx context.Context, leave int) context.Context {
	ceiling := Limit
	if old, ok := ctx.Value(scopeKey{}).(scope); ok && old.ceiling < ceiling {
		ceiling = old.ceiling
	}
	return context.WithValue(ctx, scopeKey{}, scope{ceiling: max(0, ceiling-leave)})
}

func Remaining(ctx context.Context) int {
	b := From(ctx)
	if b == nil {
		return Limit
	}
	ceiling := Limit
	if s, ok := ctx.Value(scopeKey{}).(scope); ok {
		ceiling = s.ceiling
	}
	return max(0, ceiling-b.Snapshot().Operations)
}

// CleanupContext retains accounting but never blocks resource release when the
// selection expires or exhausts its budget. Callers still supply a cleanup timeout.
func CleanupContext(ctx context.Context) context.Context {
	return context.WithValue(context.WithoutCancel(ctx), scopeKey{}, scope{cleanup: true})
}

// Take runs once per SQL execution or Redis command/pipeline execution. Commands
// inside a pipeline are counted separately, not misrepresented as network trips.
func Take(ctx context.Context, kind string, commands int) error {
	b := From(ctx)
	if b == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s, _ := ctx.Value(scopeKey{}).(scope); s.cleanup {
			return nil
		}
		if preparation, _ := ctx.Value(preparationKey{}).(bool); preparation {
			if a, _ := ctx.Value(aggregateKey{}).(*Aggregate); a != nil {
				a.mu.Lock()
				defer a.mu.Unlock()
				if a.operations >= TurnLimit {
					return ErrExhausted
				}
				a.operations++
			}
		}
		return nil
	}
	s, ok := ctx.Value(scopeKey{}).(scope)
	if !ok {
		s.ceiling = Limit
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.cleanup {
		b.state.Cleanup++
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.state.Operations >= s.ceiling {
		if b.state.Operations >= Limit {
			b.state.Exhausted = true
		}
		return ErrExhausted
	}
	if a, _ := ctx.Value(aggregateKey{}).(*Aggregate); a != nil {
		a.mu.Lock()
		if a.operations >= TurnLimit {
			a.mu.Unlock()
			b.state.Exhausted = true
			return ErrExhausted
		}
		a.operations++
		a.mu.Unlock()
	}
	b.state.Operations++
	if kind == "sql" {
		b.state.SQL++
	} else {
		b.state.Redis++
		b.state.Commands += commands
	}
	return nil
}
