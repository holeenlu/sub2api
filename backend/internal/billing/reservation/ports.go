package reservation

import (
	"context"
	"time"
)

// BalanceReleaser removes one balance reservation idempotently. Implementations
// must honor ctx so cleanup remains bounded independently of request cancellation.
type BalanceReleaser interface {
	ReleaseInflightBalance(ctx context.Context, userID int64, requestID string) error
}

// BalanceCache atomically expires stale entries, checks the existing balance
// admission policy, and registers requestID for ttl. The returned total is the
// amount reserved before this request; cache errors remain the caller's policy.
type BalanceCache interface {
	BalanceReleaser
	ReserveInflightBalance(ctx context.Context, userID int64, requestID string, amount, balance float64, ttl time.Duration) (bool, float64, error)
}

// ScopeReleaser removes one subscription or API Key reservation idempotently.
// Implementations must honor ctx; releasing a scope never changes billed usage.
type ScopeReleaser interface {
	ReleaseInflightScope(ctx context.Context, scope, requestID string) error
}

// ScopeCache atomically expires stale entries and registers requestID for ttl,
// returning the other reservations' total. Scopes do not renew.
type ScopeCache interface {
	ScopeReleaser
	ReserveInflightScope(ctx context.Context, scope, requestID string, amount float64, ttl time.Duration) (float64, error)
}

// BalanceRenewer extends only a still-live reservation and reports whether it
// exists. It must honor ctx and must not resurrect an expired or released entry.
type BalanceRenewer interface {
	RenewInflightBalance(ctx context.Context, userID int64, requestID string, ttl time.Duration) (bool, error)
}
