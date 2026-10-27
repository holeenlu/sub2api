package authz

import (
	"context"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"
)

// ManagedUserSQL is the shared delegated-management boundary. column is a
// repository-owned SQL identifier, never request input. Login/gateway access
// does not carry ManagementRequest and retains its ordinary self-access rules.
func ManagedUserSQL(ctx context.Context, column string) string {
	actor, ok := FromContext(ctx)
	if !ok || !actor.ManagementRequest || actor.Role != Admin {
		return ""
	}
	return fmt.Sprintf("%s IN (SELECT managed_user.id FROM users managed_user WHERE managed_user.role='user' AND managed_user.created_by=%d)", column, actor.UserID)
}
func ManagedUserPredicate(ctx context.Context, column string) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		if clause := ManagedUserSQL(ctx, s.C(column)); clause != "" {
			s.Where(entsql.ExprP(clause))
		}
	}
}
