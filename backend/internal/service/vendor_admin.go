package service

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func (s *VendorService) ResourceVendorID(ctx context.Context, kind string, id int64) (int64, error) {
	if err := s.RequireSuperAdmin(ctx); err != nil {
		return 0, err
	}
	if kind != "accounts" && kind != "proxies" {
		return 0, ErrVendorInput
	}
	var vendor sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT vendor_id FROM "+kind+" WHERE id=$1", id).Scan(&vendor)
	if err == sql.ErrNoRows {
		return 0, ErrVendorNotFound
	}
	if err != nil {
		return 0, err
	}
	if !vendor.Valid {
		return 0, ErrVendorNotFound
	}
	return vendor.Int64, nil
}

type VendorAuditEntry struct {
	ID          int64     `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	ActorUserID *int64    `json:"actor_user_id"`
	ActorRole   string    `json:"actor_role"`
	Action      string    `json:"action"`
	VendorID    int64     `json:"vendor_id"`
	ObjectID    string    `json:"object_id"`
	Result      string    `json:"result"`
}

func (s *VendorService) VendorAuditLogs(ctx context.Context, vID int64, page, size int, action, start, end string) ([]VendorAuditEntry, int64, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return nil, 0, ErrVendorDenied
	}
	if actor.Role == authz.SuperAdmin {
		if err := s.RequireSuperAdmin(ctx); err != nil {
			return nil, 0, err
		}
	} else {
		v, err := s.CurrentVendor(ctx)
		if err != nil {
			return nil, 0, err
		}
		if vID != 0 && vID != v.ID {
			return nil, 0, ErrVendorDenied
		}
		vID = v.ID
	}
	if vID < 0 || page < 1 || page > 100000 || size < 1 || size > 100 {
		return nil, 0, ErrVendorInput
	}
	var from, to any
	if start != "" {
		t, e := time.Parse(time.RFC3339, start)
		if e != nil {
			t, e = time.ParseInLocation(time.DateOnly, start, timezone.Location())
		}
		if e != nil {
			return nil, 0, ErrVendorInput
		}
		from = t
	}
	if end != "" {
		t, e := time.Parse(time.RFC3339, end)
		if e != nil {
			t, e = time.ParseInLocation(time.DateOnly, end, timezone.Location())
			t = t.AddDate(0, 0, 1)
		}
		if e != nil {
			return nil, 0, ErrVendorInput
		}
		to = t
	}
	if from != nil && to != nil && !from.(time.Time).Before(to.(time.Time)) {
		return nil, 0, ErrVendorInput
	}
	// VendorAudit also writes native account/proxy and export action names. Its
	// explicit ownership/result metadata identifies the safe supplier projection;
	// action names alone must not admit platform or consumer audit records.
	// Owner zero is reserved here for aggregate exports, visible only to the live
	// super administrator's unfiltered scope (supplier scope was fixed above).
	where := ` WHERE visibility='super_admin'
		AND (action LIKE 'vendor.%' OR action LIKE 'account.%' OR action LIKE 'proxy.%' OR action LIKE 'settlement.%' OR action='usage.export')
		AND extra ?& ARRAY['vendor_id','object_id','result']
		AND extra->>'object_id' ~ '^[0-9]+$'
		AND jsonb_typeof(extra->'result')='string'
		AND ($1::bigint=0 OR extra->>'vendor_id'=$1::text)
		AND (EXISTS (SELECT 1 FROM vendors v WHERE v.id>0 AND v.id::text=audit_logs.extra->>'vendor_id')
			OR ($1::bigint=0 AND extra->>'vendor_id'='0' AND action IN ('usage.export','proxy.export')))
		AND ($2='' OR action=$2)
		AND ($3::timestamptz IS NULL OR created_at>=$3)
		AND ($4::timestamptz IS NULL OR created_at<$4)`
	args := []any{vID, action, from, to}
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id,created_at,actor_user_id,actor_role,action,extra->>'vendor_id',COALESCE(extra->>'object_id',''),COALESCE(extra->>'result','') FROM audit_logs"+where+" ORDER BY id DESC LIMIT $5 OFFSET $6", append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VendorAuditEntry{}
	for rows.Next() {
		var e VendorAuditEntry
		var vendor string
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.ActorUserID, &e.ActorRole, &e.Action, &vendor, &e.ObjectID, &e.Result); err != nil {
			return nil, 0, err
		}
		e.VendorID, _ = strconv.ParseInt(vendor, 10, 64)
		out = append(out, e)
	}
	return out, total, rows.Err()
}

type VendorGroupSupply struct {
	GroupID        int64  `json:"group_id"`
	GroupName      string `json:"group_name"`
	Platform       string `json:"platform"`
	VendorID       *int64 `json:"vendor_id"`
	VendorName     string `json:"vendor_name"`
	AccountCount   int64  `json:"account_count"`
	AvailableCount int64  `json:"available_count"`
}

func (s *VendorService) VendorGroupSupply(ctx context.Context, vID int64, page, size int, search string) ([]VendorGroupSupply, int64, error) {
	if err := s.RequireSuperAdmin(ctx); err != nil {
		return nil, 0, err
	}
	if page < 1 || page > 100000 || size < 1 || size > 100 {
		return nil, 0, ErrVendorInput
	}
	// Group entitlement stays native: this is supply visibility, not consumer permission.
	availability, ok := s.accounts.(interface{ VendorGroupAvailabilitySQL() string })
	if !ok {
		return nil, 0, ErrVendorSupplyUnavailable
	}
	// Repository owns the live scheduling rule, including shadow health and
	// supplier login, group and private-proxy authorization.
	q := `SELECT g.id,g.name,g.platform,a.vendor_id,COALESCE(v.name,''),COUNT(DISTINCT a.id),
 COUNT(DISTINCT a.id) FILTER(WHERE ` + availability.VendorGroupAvailabilitySQL() + `)
 FROM groups g JOIN account_groups ag ON ag.group_id=g.id JOIN accounts a ON a.id=ag.account_id AND a.deleted_at IS NULL LEFT JOIN vendors v ON v.id=a.vendor_id
 WHERE g.deleted_at IS NULL AND ($1::bigint=0 OR a.vendor_id=$1) AND ($2='' OR g.name ILIKE '%'||$2||'%') GROUP BY g.id,g.name,g.platform,a.vendor_id,v.name`
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+q+") supply", vID, strings.TrimSpace(search)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY g.id,a.vendor_id NULLS FIRST LIMIT $3 OFFSET $4", vID, strings.TrimSpace(search), size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VendorGroupSupply{}
	for rows.Next() {
		var v VendorGroupSupply
		if err := rows.Scan(&v.GroupID, &v.GroupName, &v.Platform, &v.VendorID, &v.VendorName, &v.AccountCount, &v.AvailableCount); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}
