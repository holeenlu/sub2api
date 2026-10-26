package service

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/authz"
)

// Financial identity is always the actual login's supplier, never a delegated
// read-only inventory scope. That scope cannot become a purchasing capability.
func (s *VendorService) vendorUsageIdentity(ctx context.Context, q vendorSQL, lock bool) (*Vendor, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || !authz.IsVendorRole(actor.Role) || !s.Enabled(ctx) {
		return nil, ErrVendorUsageForbidden
	}
	if err := authz.Revalidate(ctx); err != nil {
		return nil, err
	}
	query := "SELECT v.id,v.tier,v.parent_vendor_id,v.policy_version FROM vendors v JOIN users u ON u.id=v.user_id WHERE u.id=$1 AND u.role=$2 AND u.session_generation=$3 AND " + authz.ActiveVendorSQL("v", "u")
	if lock {
		query += " FOR SHARE OF v,u"
	}
	v := &Vendor{}
	if err := q.QueryRowContext(ctx, query, actor.UserID, actor.Role, actor.SessionGeneration).Scan(&v.ID, &v.Tier, &v.ParentVendorID, &v.PolicyVersion); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrVendorUsageForbidden
		}
		return nil, err
	}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok {
		if scope.ActorUserID != actor.UserID || scope.Admin || (!scope.ReadOnly && (scope.VendorID != v.ID || scope.PolicyVersion != v.PolicyVersion)) {
			return nil, ErrVendorUsageForbidden
		}
	}
	return v, nil
}

func (s *VendorService) vendorSettlementPayer(ctx context.Context, q vendorSQL, lock bool) (int64, *int64, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return 0, nil, ErrVendorUsageForbidden
	}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok && scope.ReadOnly {
		return 0, nil, ErrVendorUsageForbidden
	}
	if actor.Role == authz.SuperAdmin {
		id, err := s.vendorUsageAdmin(ctx, q, lock)
		return id, nil, err
	}
	if actor.Role != authz.VendorLevel1 {
		return 0, nil, ErrVendorUsageForbidden
	}
	v, err := s.vendorUsageIdentity(ctx, q, lock)
	if err != nil {
		return 0, nil, err
	}
	if v.Tier != 1 || v.ParentVendorID != nil {
		return 0, nil, ErrVendorUsageForbidden
	}
	return actor.UserID, &v.ID, nil
}

func vendorSettlementSamePayer(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func vendorSettlementPayee(ctx context.Context, q vendorSQL, id int64, payer *int64) (*Vendor, error) {
	v, err := scanVendor(q.QueryRowContext(ctx, "SELECT "+vendorColumns+" FROM vendors v WHERE v.id=$1", id))
	if err != nil {
		return nil, err
	}
	if payer == nil {
		if v.Tier != 1 || v.ParentVendorID != nil {
			return nil, ErrVendorUsageForbidden
		}
	} else if v.Tier != 2 || v.ParentVendorID == nil || *v.ParentVendorID != *payer {
		return nil, ErrVendorUsageForbidden
	}
	return v, nil
}

// Both edges serialize on the root before the child; identity/policy edits
// cannot race an authorized mutation. Amounts still belong to separate books.
func vendorSettlementLockEdge(ctx context.Context, tx *sql.Tx, id int64, payer *int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT id FROM vendors WHERE id=$1 OR id=$2 ORDER BY id FOR UPDATE", id, payer)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func vendorSettlementEntryBelongs(e VendorUsageEntry, vendorID int64, payer *int64) bool {
	if payer != nil {
		return e.VendorID == vendorID && e.ParentVendorID != nil && *e.ParentVendorID == *payer && e.RootVendorID != nil && *e.RootVendorID == *payer
	}
	if e.RootVendorID != nil {
		return *e.RootVendorID == vendorID
	}
	// Pre-277 allocation snapshots contain only their original direct owner.
	return e.VendorID == vendorID && e.ParentVendorID == nil
}
