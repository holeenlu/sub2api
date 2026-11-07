package service

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"regexp"
	"testing"
	"time"
)

func TestVendorProxyBindingLocksOnlyMatchingLiveOwner(t *testing.T) {
	db, m, e := sqlmock.New()
	require.NoError(t, e)
	defer db.Close()
	m.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(parent_vendor_id,id) FROM vendors WHERE id=$1")).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"root"}).AddRow(7))
	m.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtextextended('vendor-proxy:' || $1::text,0))")).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, AcquireVendorProxyLock(context.Background(), db, 7))
	columns := []string{"id", "name", "protocol", "host", "port", "username", "password", "status", "created_at", "updated_at", "expires_at", "fallback_mode", "backup_proxy_id", "expiry_warn_days", "vendor_id", "created_by", "vendor_notes", "region_label_country_code", "region_label_city", "config_version"}
	now := time.Now()
	values := []driver.Value{int64(2), "p", "http", "public.invalid", 8080, "u", "p", "active", now, now, nil, "none", nil, 7, int64(7), nil, "", "", "", int64(3)}
	m.ExpectQuery(regexp.QuoteMeta("SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id=$2 AND p.deleted_at IS NULL FOR KEY SHARE")).WithArgs(int64(2), int64(7)).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
	p, e := ValidateVendorProxyBinding(context.Background(), db, 7, 2)
	require.NoError(t, e)
	require.Equal(t, int64(3), p.ConfigVersion)
	m.ExpectQuery("SELECT .* FOR KEY SHARE").WithArgs(int64(2), int64(8)).WillReturnRows(sqlmock.NewRows(columns))
	_, e = ValidateVendorProxyBinding(context.Background(), db, 8, 2)
	require.ErrorIs(t, e, ErrProxyNotFound)
	values[7] = "inactive"
	m.ExpectQuery("SELECT .* FOR KEY SHARE").WithArgs(int64(2), int64(7)).WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
	_, e = ValidateVendorProxyBinding(context.Background(), db, 7, 2)
	require.ErrorIs(t, e, ErrVendorProxyUnavailable)
	require.NoError(t, m.ExpectationsWereMet())
}

func TestVendorProxyCommitRejectsRevokedSession(t *testing.T) {
	db, m, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	m.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	denied := errors.New("revoked")
	ctx := authz.WithRevalidation(context.Background(), func(context.Context) error { return denied })
	require.ErrorIs(t, commitVendorProxyMutation(ctx, tx), denied)
	m.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, m.ExpectationsWereMet())
}
func TestVendorProxyReferencesIncludeOAuthAndInflightAcrossServices(t *testing.T) {
	r := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: r.Addr()})
	defer client.Close()
	s := &VendorService{rdb: client}
	other := &VendorService{rdb: client}
	ctx := context.Background()
	require.NoError(t, s.RegisterVendorProxyReference(ctx, 7, 2, "oauth", "session", time.Now().Add(time.Minute)))
	require.NoError(t, s.RegisterVendorProxyReference(ctx, 7, 2, "inflight", "exchange", time.Now().Add(time.Minute)))
	db, m, e := sqlmock.New()
	require.NoError(t, e)
	defer db.Close()
	m.ExpectQuery("SELECT.*FROM accounts.*UNION ALL.*FROM proxies").WithArgs(int64(2), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "kind"}).AddRow(11, "account").AddRow(3, "backup"))
	refs, e := other.vendorProxyReferences(ctx, db, 7, 2)
	require.NoError(t, e)
	require.Equal(t, int64(4), refs.Total)
	require.Equal(t, int64(1), refs.OAuth)
	require.Equal(t, int64(1), refs.Inflight)
	require.NoError(t, s.ReleaseVendorProxyReference(ctx, 7, 2, "oauth", "session"))
	require.Equal(t, int64(0), client.ZCard(ctx, vendorProxyReferenceKey(7, 2, "oauth")).Val())
	require.NoError(t, m.ExpectationsWereMet())
}
