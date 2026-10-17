package service

import (
	"context"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ValidateVendorResourceCapacity must run under AcquireVendorProxyLock in the
// inserting transaction. The root lock serializes its own and every child's
// inserts with policy changes. Limits count live resources, not allocated quotas;
// paused/archived rows still consume capacity and credential shadows do not.
func ValidateVendorResourceCapacity(ctx context.Context, tx VendorProxySQL, vendorID int64, resource string) error {
	table, column, extra := "", "", ""
	var exhausted error
	switch resource {
	case "accounts":
		table, column, extra = "accounts", "account_limit", " AND r.parent_account_id IS NULL"
		exhausted = infraVendorAccountLimit()
	case "proxies":
		table, column = "proxies", "proxy_limit"
		exhausted = infraerrors.Conflict("PROXY_LIMIT", "proxy limit reached")
	default:
		return ErrVendorInput
	}
	rows, err := tx.QueryContext(ctx, `SELECT EXISTS(SELECT 1 FROM vendors v WHERE v.id=$1),
 EXISTS(SELECT 1 FROM vendors quota WHERE (quota.id=$1 OR quota.id=(SELECT parent_vendor_id FROM vendors WHERE id=$1))
 AND (quota.`+column+`<=0 OR (SELECT count(*) FROM `+table+` r JOIN vendors owner ON owner.id=r.vendor_id
 WHERE r.deleted_at IS NULL`+extra+` AND (owner.id=quota.id OR owner.parent_vendor_id=quota.id))>=quota.`+column+`))`, vendorID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return ErrVendorNotFound
	}
	var found, full bool
	if err := rows.Scan(&found, &full); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !found {
		return ErrVendorNotFound
	}
	if full {
		return exhausted
	}
	return nil
}
