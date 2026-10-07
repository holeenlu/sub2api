package authz

import (
	"context"
	"errors"
)

var ErrAuthorizationExpired = errors.New("management authorization expired")

type revalidationKey struct{}

func WithRevalidation(ctx context.Context, check func(context.Context) error) context.Context {
	return context.WithValue(ctx, revalidationKey{}, check)
}
func Revalidate(ctx context.Context) error {
	if check, ok := ctx.Value(revalidationKey{}).(func(context.Context) error); ok {
		return check(ctx)
	}
	return nil
}

// Lease records the identity which authorized an OAuth exchange or scheduled
// action. It contains no credentials and never accepts client-supplied grants.
type Lease struct {
	System            bool   `json:"system,omitempty"`
	UserID            int64  `json:"user_id,omitempty"`
	Role              string `json:"role,omitempty"`
	SessionGeneration int64  `json:"session_generation,omitempty"`
	SessionID         string `json:"session_id,omitempty"`
}

func CaptureLease(ctx context.Context) *Lease {
	actor, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	return &Lease{UserID: actor.UserID, Role: actor.Role, SessionGeneration: actor.SessionGeneration, SessionID: actor.SessionID}
}

func (l *Lease) Allows(actor Subject, permission string) bool {
	return l != nil && !l.System && l.UserID == actor.UserID && l.Role == actor.Role && l.SessionGeneration == actor.SessionGeneration && (l.SessionID == "" || l.SessionID == actor.SessionID) && actor.Can(permission)
}

func CheckLease(ctx context.Context, l *Lease, permission string) error {
	actor, ok := FromContext(ctx)
	if !ok && l == nil {
		return nil
	} // Internal tests/services do not issue HTTP authority.
	if !ok || !l.Allows(actor, permission) {
		return ErrAuthorizationExpired
	}
	return Revalidate(ctx)
}
