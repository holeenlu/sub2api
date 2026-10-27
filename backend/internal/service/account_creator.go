package service

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/authz"
)

// AccountCreator is an immutable creation-time snapshot, not the current owner.
// Imports and reauthorization cannot supply or replace this attribution.
type AccountCreator struct {
	ID   *int64
	Name string
	Role string
}

type accountCreatorQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func ResolveAccountCreator(ctx context.Context, db accountCreatorQuerier) (AccountCreator, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.UserID <= 0 {
		return AccountCreator{Name: "System", Role: "system"}, nil
	}
	rows, err := db.QueryContext(ctx, "SELECT COALESCE(NULLIF(username,''),email),role FROM users WHERE id=$1 AND status='active' AND deleted_at IS NULL", actor.UserID)
	if err != nil {
		return AccountCreator{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return AccountCreator{}, err
		}
		return AccountCreator{}, authz.ErrPermissionDenied
	}
	creator := AccountCreator{ID: &actor.UserID}
	if err := rows.Scan(&creator.Name, &creator.Role); err != nil {
		return AccountCreator{}, err
	}
	if creator.Role != string(actor.Role) {
		return AccountCreator{}, authz.ErrPermissionDenied
	}
	return creator, rows.Err()
}
