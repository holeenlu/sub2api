package service

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

func TestVendorAuditPostgresScopeFiltersAndProjection(t *testing.T) {
	s, root, supplier := vendorSettlementPostgres(t)
	_, err := s.db.Exec(`CREATE TABLE audit_logs (
		id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL,
		actor_user_id BIGINT, actor_role TEXT, action TEXT, visibility TEXT,
		extra JSONB, client_ip TEXT, request_body TEXT
	)`)
	require.NoError(t, err)
	// Use the native event producer and its metadata unchanged. Only persistence
	// is synchronous here; all reads run the production query against PostgreSQL.
	s.audit = NewAuditLogService(nil, nil)
	t.Cleanup(s.audit.Stop)
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	insert := func(entry *AuditLog) int64 {
		t.Helper()
		extra, e := json.Marshal(entry.Extra)
		require.NoError(t, e)
		var id int64
		e = s.db.QueryRow(`INSERT INTO audit_logs(created_at,actor_user_id,actor_role,action,visibility,extra,client_ip,request_body)
			VALUES($1,$2,$3,$4,$5,$6::jsonb,'PRIVATE_IP','PRIVATE_BODY') RETURNING id`,
			entry.CreatedAt, entry.ActorUserID, entry.ActorRole, entry.Action, entry.Visibility, string(extra)).Scan(&id)
		require.NoError(t, e)
		return id
	}
	record := func(ctx context.Context, vendorID int64, action string, when time.Time) int64 {
		t.Helper()
		objectID := int64(12)
		if vendorID == 0 {
			objectID = 0
		}
		result := "success"
		if action == "usage.export" {
			result = "rows=7"
		}
		s.VendorAudit(ctx, vendorID, action, objectID, result)
		entry := <-s.audit.queue
		entry.CreatedAt = when
		return insert(entry)
	}
	actions := []string{"account.export", "account.delete", "proxy.update", "proxy.export", "vendor.policy.update", "usage.export", "settlement.confirm"}
	var own, other, aggregate []int64
	for _, action := range actions {
		actor := supplier
		if action == "vendor.policy.update" || action == "settlement.confirm" {
			actor = root
		}
		own = append(own, record(actor, 1, action, at))
	}
	// Account 12 is already deleted in the fixture: audit ownership is historical
	// metadata and must not depend on joining a currently live resource.
	oldExport := record(supplier, 1, "account.export", at.AddDate(0, 0, -1))
	own = append(own, oldExport)
	for _, action := range actions[:4] {
		other = append(other, record(root, 2, action, at))
	}
	for _, action := range []string{"usage.export", "proxy.export"} {
		aggregate = append(aggregate, record(root, 0, action, at))
	}
	for _, tc := range []struct {
		action string
		extra  map[string]any
	}{
		{"account.export", nil},
		{"proxy.update", map[string]any{"vendor_id": 1}},
		{"vendor.policy.update", map[string]any{"vendor_id": nil}},
		{"account.export", map[string]any{"vendor_id": nil, "object_id": 12, "result": "success"}},
		{"account.export", map[string]any{"vendor_id": -1, "object_id": 12, "result": "success"}},
		{"account.export", map[string]any{"vendor_id": 999999, "object_id": 12, "result": "success"}},
		{"account.export", map[string]any{"vendor_id": "9223372036854775808", "object_id": 12, "result": "success"}},
		{"account.export", map[string]any{"vendor_id": 0, "object_id": 12, "result": "success"}},
		{"auth.login", map[string]any{"vendor_id": 1, "object_id": 3, "result": "PRIVATE_CONSUMER"}},
		{"api_key.create", map[string]any{"vendor_id": 1, "object_id": 3, "result": "PRIVATE_CONSUMER"}},
		{"proxy.update", map[string]any{"vendor_id": 1, "object_id": "PRIVATE_OBJECT", "result": "success"}},
		{"account.export", map[string]any{"vendor_id": 1, "object_id": 12, "result": map[string]any{"request": "PRIVATE_RESULT"}}},
	} {
		insert(&AuditLog{CreatedAt: at, Action: tc.action, Visibility: RoleSuperAdmin, Extra: tc.extra})
	}
	insert(&AuditLog{CreatedAt: at, Action: "account.export", Visibility: "staff", Extra: map[string]any{"vendor_id": 1, "object_id": 12, "result": "PRIVATE_STAFF"}})
	ids := func(items []VendorAuditEntry) []int64 {
		out := make([]int64, len(items))
		for i := range items {
			out[i] = items[i].ID
		}
		return out
	}
	slices.Reverse(own)
	slices.Reverse(other)
	t.Run("supplier pagination and count share the ownership predicate", func(t *testing.T) {
		var paged []int64
		for page := 1; page <= 4; page++ {
			items, total, e := s.VendorAuditLogs(supplier, 0, page, 3, "", "", "")
			require.NoError(t, e)
			require.Equal(t, int64(len(own)), total)
			for _, item := range items {
				require.Equal(t, int64(1), item.VendorID)
			}
			paged = append(paged, ids(items)...)
		}
		require.Equal(t, own, paged)
		items, total, e := s.VendorAuditLogs(root, 1, 1, 100, "", "", "")
		require.NoError(t, e)
		require.Equal(t, int64(len(own)), total)
		require.Equal(t, own, ids(items))
	})
	t.Run("only the live super administrator sees aggregate exports", func(t *testing.T) {
		expected := append(append(slices.Clone(own), other...), aggregate...)
		slices.Sort(expected)
		slices.Reverse(expected)
		items, total, e := s.VendorAuditLogs(root, 0, 1, 100, "", "", "")
		require.NoError(t, e)
		require.Equal(t, int64(len(expected)), total)
		require.Equal(t, expected, ids(items))
		items, total, e = s.VendorAuditLogs(root, 2, 1, 100, "", "", "")
		require.NoError(t, e)
		require.Equal(t, int64(len(other)), total)
		require.Equal(t, other, ids(items))
		secondSupplier := authz.WithVendorScope(authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1}), authz.VendorScope{VendorID: 2, PolicyVersion: 1, ActorUserID: 4})
		items, total, e = s.VendorAuditLogs(secondSupplier, 0, 1, 100, "", "", "")
		require.NoError(t, e)
		require.Equal(t, int64(len(other)), total)
		require.Equal(t, other, ids(items))
		items, total, e = s.VendorAuditLogs(root, 0, 1, 100, "usage.export", "", "")
		require.NoError(t, e)
		require.Equal(t, int64(2), total)
		require.Len(t, items, 2)
		require.Equal(t, int64(0), items[0].VendorID)
		require.Equal(t, "0", items[0].ObjectID)
		require.Equal(t, "rows=7", items[0].Result)
	})
	t.Run("action and date filters retain deleted resource history", func(t *testing.T) {
		items, total, e := s.VendorAuditLogs(supplier, 1, 1, 100, "account.export", at.Format(time.RFC3339), at.Add(24*time.Hour).Format(time.RFC3339))
		require.NoError(t, e)
		require.Equal(t, int64(1), total)
		require.Len(t, items, 1)
		require.Equal(t, "12", items[0].ObjectID)
		require.Equal(t, "account.export", items[0].Action)
		require.NotEqual(t, oldExport, items[0].ID)
		items, total, e = s.VendorAuditLogs(root, 0, 1, 100, "auth.login", "", "")
		require.NoError(t, e)
		require.Zero(t, total)
		require.Empty(t, items)
		items, total, e = s.VendorAuditLogs(supplier, 1, 1, 100, "usage.export", "", "")
		require.NoError(t, e)
		require.Equal(t, int64(1), total)
		require.Len(t, items, 1)
		require.Equal(t, int64(1), items[0].VendorID)
	})
	t.Run("safe DTO excludes raw audit fields", func(t *testing.T) {
		items, _, e := s.VendorAuditLogs(root, 0, 1, 100, "", "", "")
		require.NoError(t, e)
		payload, e := json.Marshal(items)
		require.NoError(t, e)
		require.NotContains(t, string(payload), "PRIVATE")
		var rows []map[string]any
		require.NoError(t, json.Unmarshal(payload, &rows))
		require.NotEmpty(t, rows)
		for _, row := range rows {
			keys := make([]string, 0, len(row))
			for key := range row {
				keys = append(keys, key)
			}
			require.ElementsMatch(t, []string{"id", "created_at", "actor_user_id", "actor_role", "action", "vendor_id", "object_id", "result"}, keys)
		}
	})
	t.Run("foreign scope ordinary administrator and revoked sessions fail closed", func(t *testing.T) {
		items, total, e := s.VendorAuditLogs(supplier, 2, 1, 100, "", "", "")
		require.ErrorIs(t, e, ErrVendorDenied)
		require.Empty(t, items)
		require.Zero(t, total)
		admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.Admin, SessionGeneration: 1})
		items, total, e = s.VendorAuditLogs(admin, 0, 1, 100, "", "", "")
		require.Error(t, e)
		require.Empty(t, items)
		require.Zero(t, total)
		_, e = s.db.Exec("UPDATE users SET role='admin' WHERE id=9; UPDATE users SET session_generation=2 WHERE id=3")
		require.NoError(t, e)
		for _, ctx := range []context.Context{root, supplier} {
			items, total, e = s.VendorAuditLogs(ctx, 0, 1, 100, "", "", "")
			require.ErrorIs(t, e, ErrVendorDenied)
			require.Empty(t, items)
			require.Zero(t, total)
		}
	})
}
