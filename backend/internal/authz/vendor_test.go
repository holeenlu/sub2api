package authz

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVendorAuthorizationLeaseIsolation(t *testing.T) {
	actor := Subject{UserID: 31, Role: VendorLevel1, SessionGeneration: 2, SessionID: "session-a"}
	scope := VendorScope{VendorID: 7, PolicyVersion: 4, ActorUserID: 31}
	ctx := WithVendorScope(WithSubject(context.Background(), actor), scope)
	lease := CaptureLease(ctx)
	require.NoError(t, CheckLease(ctx, lease, "accounts.authorize"))
	require.ErrorIs(t, CheckLease(ctx, lease, "accounts.manage"), ErrAuthorizationExpired)
	require.ErrorIs(t, CheckLease(WithSubject(context.Background(), actor), lease, "accounts.authorize"), ErrAuthorizationExpired)
	for _, alter := range []func(*Subject, *VendorScope){
		func(a *Subject, _ *VendorScope) { a.SessionID = "session-b" },
		func(a *Subject, _ *VendorScope) { a.SessionGeneration++ },
		func(a *Subject, _ *VendorScope) { a.Role = SuperAdmin },
		func(_ *Subject, s *VendorScope) { s.VendorID++ },
		func(_ *Subject, s *VendorScope) { s.PolicyVersion++ },
		func(_ *Subject, s *VendorScope) { s.ActorUserID++ },
		func(_ *Subject, s *VendorScope) { s.ReadOnly = true },
	} {
		a, s := actor, scope
		alter(&a, &s)
		require.ErrorIs(t, CheckLease(WithVendorScope(WithSubject(context.Background(), a), s), lease, "accounts.authorize"), ErrAuthorizationExpired)
	}
	for _, role := range []string{User, Admin} {
		a := actor
		a.Role = role
		forged := WithVendorScope(WithSubject(context.Background(), a), scope)
		require.ErrorIs(t, CheckLease(forged, CaptureLease(forged), "accounts.authorize"), ErrAuthorizationExpired)
	}
	require.ErrorIs(t, CheckLease(WithRevalidation(ctx, func(context.Context) error { return ErrAuthorizationChanged }), lease, "accounts.authorize"), ErrAuthorizationChanged)
}
