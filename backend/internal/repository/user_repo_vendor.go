package repository

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func userSupplyScopePredicate(ctx context.Context) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		actor, ok := authz.FromContext(ctx)
		if !ok {
			return
		}
		if actor.Role == authz.SuperAdmin && actor.ManagementRequest {
			s.Where(entsql.ExprP("(NOT EXISTS (SELECT 1 FROM vendors vu WHERE vu.user_id=" + s.C("id") + ") OR " + liveSupplyAdminSQL(actor) + ")"))
			return
		}
		if actor.Role != authz.Admin {
			return
		}
		s.Where(entsql.ExprP("NOT EXISTS (SELECT 1 FROM vendors vu WHERE vu.user_id=" + s.C("id") + ")"))
	}
}

// ValidateVendorIdentity runs on every access/refresh/login, without auth caches.
func (r *userRepository) ValidateVendorIdentity(ctx context.Context, id int64, role string) error {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, "SELECT EXISTS(SELECT 1 FROM vendors v JOIN users u ON u.id=v.user_id WHERE u.id=$1 AND u.role=$2 AND "+authz.ActiveVendorSQL("v", "u")+" AND EXISTS(SELECT 1 FROM settings WHERE key='vendor_console_enabled' AND value='true'))", id, role)
	if err != nil {
		return service.ErrServiceUnavailable
	}
	defer rows.Close()
	var valid bool
	if !rows.Next() {
		return service.ErrServiceUnavailable
	}
	if err := rows.Scan(&valid); err != nil {
		return service.ErrServiceUnavailable
	}
	if !valid {
		return service.ErrVendorDenied
	}
	return rows.Err()
}
