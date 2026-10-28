package host

import (
	"context"
	"errors"
)

var ErrTurnAdmissionMissing = errors.New("turn admission owner is missing")

// TurnAdmission is a technical lifetime port. It carries no authorization,
// provider, pricing or retry policy. Admission covers queued work through usage
// finalization; writing a terminal frame alone must not release it.
type TurnAdmission interface {
	EnableTurnTracking()
	AdmitTurn() (func(), error)
}

type turnAdmissionKey struct{}

// WithTurnAdmission attaches the HTTP/connection owner's lifetime to ingress.
func WithTurnAdmission(ctx context.Context, owner TurnAdmission) context.Context {
	return context.WithValue(ctx, turnAdmissionKey{}, owner)
}

// EnableTurnTracking declares that every accepted create on this connection is
// tracked. Until this is called, shutdown conservatively waits for the whole
// hijacked handler instead of mistaking an uninstrumented connection for idle.
func EnableTurnTracking(ctx context.Context) {
	if owner, ok := ctx.Value(turnAdmissionKey{}).(TurnAdmission); ok {
		owner.EnableTurnTracking()
	}
}

// AdmitTurn rejects work after drain begins. Missing ownership is an explicit
// error: losing the ingress context must not silently bypass shutdown admission.
// Standalone consumers and fixtures install an explicit lifetime owner.
func AdmitTurn(ctx context.Context) (func(), error) {
	if owner, ok := ctx.Value(turnAdmissionKey{}).(TurnAdmission); ok {
		return owner.AdmitTurn()
	}
	return nil, ErrTurnAdmissionMissing
}
