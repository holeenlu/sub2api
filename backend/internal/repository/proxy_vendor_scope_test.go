package repository

import (
	"context"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProxyOrdinaryAdminScopeWithoutManagementFlag(t *testing.T) {
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin})
	ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 99, Admin: true})
	q := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("proxies"))
	proxyScopePredicate(ctx)(q)
	sql, args := q.Query()
	require.Contains(t, sql, `"vendor_id" IS NULL`)
	require.Empty(t, args)
}
func TestProxySupplierReadScope(t *testing.T) {
	ctx := authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: 17})
	q := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("proxies"))
	proxyScopePredicate(ctx)(q)
	sql, args := q.Query()
	require.Contains(t, sql, `"vendor_id" = $1`)
	require.Equal(t, []any{int64(17)}, args)
}

func TestProxyNativeAndVendorInventoryScopes(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		vendor     bool
		want       string
	}{
		{"internal gateway", "", false, ""},
		{"native super admin", authz.SuperAdmin, false, "platform"},
		{"vendor super admin", authz.SuperAdmin, true, "vendor"},
		{"ordinary admin forged scope", authz.Admin, true, "platform"},
		{"vendor", authz.VendorLevel1, true, "vendor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.role != "" {
				ctx = authz.WithSubject(ctx, authz.Subject{Role: tc.role})
			}
			if tc.vendor {
				ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 17})
			}
			q := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("proxies"))
			proxyScopePredicate(ctx)(q)
			query, args := q.Query()
			var rawArgs []any
			raw := proxySQLScope(ctx, "p.", &rawArgs)
			switch tc.want {
			case "platform":
				require.Contains(t, query, `"vendor_id" IS NULL`)
				require.Equal(t, " AND p.vendor_id IS NULL", raw)
			case "vendor":
				require.Contains(t, query, `"vendor_id" = $1`)
				require.Equal(t, []any{int64(17)}, args)
				require.Equal(t, " AND p.vendor_id=$1", raw)
			default:
				require.NotContains(t, query, "vendor_id")
				require.Empty(t, raw)
			}
			require.Equal(t, args, rawArgs)
		})
	}
}

func TestProxyNativeInventoryPostgresDoesNotReuseSupplierIdentity(t *testing.T) {
	accounts, db := vendorSupplyPostgres(t)
	repo := newProxyRepositoryWithSQL(accounts.client, db)
	ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin})
	_, err := db.Exec("CREATE SEQUENCE proxy_scope_ids START 20; ALTER TABLE proxies ALTER COLUMN id SET DEFAULT nextval('proxy_scope_ids'); UPDATE proxies SET username='owner-user',password='owner-secret'")
	require.NoError(t, err)
	exists, err := repo.ExistsByHostPortAuth(ctx, "proxy.example", 8080, "owner-user", "owner-secret")
	require.NoError(t, err)
	require.False(t, exists, "native import must not reuse a supplier's ID")
	p := &service.Proxy{Name: "platform", Protocol: "http", Host: "proxy.example", Port: 8080, Username: "owner-user", Password: "owner-secret", Status: service.StatusActive, FallbackMode: service.FallbackModeNone}
	require.NoError(t, repo.Create(ctx, p))
	list, page, err := repo.ListWithFiltersAndAccountCount(ctx, pagination.PaginationParams{Page: 1, PageSize: 20}, "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, list, 1)
	require.Equal(t, p.ID, list[0].ID)
	active, err := repo.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, p.ID, active[0].ID)
	selected, err := repo.ListByIDs(ctx, []int64{10, 11, p.ID})
	require.NoError(t, err)
	require.Len(t, selected, 1)
	p.Name = "edited platform"
	require.NoError(t, repo.Update(ctx, p))
	_, err = repo.GetByID(ctx, 10)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	scoped := authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 7, Admin: true})
	owned, err := repo.GetByID(scoped, 10)
	require.NoError(t, err)
	require.EqualValues(t, 7, *owned.VendorID)
	_, err = repo.GetByID(scoped, p.ID)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
	_, err = repo.GetByID(context.Background(), 10)
	require.NoError(t, err, "internal gateway still resolves supplier IDs")
}
