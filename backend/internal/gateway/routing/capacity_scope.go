package routing

import (
	"context"
	"sync"
)

type capacityScopeContextKey struct{}
type capacityScopeKey struct {
	user bool
	id   int64
}

// CapacityScope is a per-logical-operation index of original acquisition results.
// It owns no lease, renewal, cancellation or release. MAIN binds only the selected
// user/account Lease to the actual sending context; rejected candidates cannot
// cancel the operation through this index. Do not reuse a scope across WS turns.
type CapacityScope struct {
	mu      sync.RWMutex
	results map[capacityScopeKey]*AcquireResult
}

// WithCapacityScope explicitly starts a fresh receipt index for one operation.
// Child acquisition/wait contexts inherit the index, not lease lifetime. It
// preserves the parent's cancellation and deadline. A nil parent yields nils.
func WithCapacityScope(ctx context.Context) (context.Context, *CapacityScope) {
	if ctx == nil {
		return nil, nil
	}
	scope := &CapacityScope{results: make(map[capacityScopeKey]*AcquireResult)}
	// MAIN's existing per-operation binding also owns the capacity-wait bound.
	// A new WS turn gets a fresh scope; retries retain the original context.
	ctx = WithWaitBudget(ctx, nil)
	return context.WithValue(ctx, capacityScopeContextKey{}, scope), scope
}

// CapacityScopeFrom returns only an explicitly initialized operation scope.
// Neither this accessor nor AcquireCapacity manufactures a default scope.
func CapacityScopeFrom(ctx context.Context) *CapacityScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(capacityScopeContextKey{}).(*CapacityScope)
	return scope
}

// Lookup implements MAIN's NativeCapacityLookup. The most recent successful
// acquisition of this namespace/ID wins. A released receipt remains queryable
// but Verify rejects it, including unlimited receipts; no old grant is revived.
func (s *CapacityScope) Lookup(user bool, id int64) *AcquireResult {
	if s == nil || id <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.results[capacityScopeKey{user: user, id: id}]
}

func (s *CapacityScope) record(result *AcquireResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[capacityScopeKey{user: result.policy.User, id: result.policy.ResourceID}] = result
}
