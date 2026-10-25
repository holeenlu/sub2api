//go:build unit

package repository

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageLogVendorSnapshotInsertAndScan(t *testing.T) {
	id := int64(17)
	name, group, platform := "selected", "served", "openai"
	log := &service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, RequestID: "req", Model: "model", VendorIDSnapshot: &id, AccountNameSnapshot: &name, GroupNameSnapshot: &group, PlatformSnapshot: &platform, CreatedAt: time.Now(), AuthorizationMode: service.UsageAuthorizationModeBPS}
	prepared := prepareUsageLogInsert(log)
	require.Len(t, prepared.args, len(usageLogInsertArgTypes))
	cols := strings.Split(usageLogSelectColumns, ", ")
	for _, field := range []string{"vendor_id_snapshot", "account_name_snapshot", "group_name_snapshot", "platform_snapshot", "authorization_mode"} {
		idx := -1
		for i, c := range cols {
			if c == field {
				idx = i - 1
			}
		}
		require.GreaterOrEqual(t, idx, 0)
		switch field {
		case "vendor_id_snapshot":
			require.Equal(t, sql.NullInt64{Int64: id, Valid: true}, prepared.args[idx])
		case "account_name_snapshot":
			require.Equal(t, sql.NullString{String: name, Valid: true}, prepared.args[idx])
		}
	}
	scan := vendorSnapshotScanner{columns: cols, prepared: prepared.args}
	got, e := scanUsageLog(scan)
	require.NoError(t, e)
	require.Equal(t, id, *got.VendorIDSnapshot)
	require.Equal(t, name, *got.AccountNameSnapshot)
	require.Equal(t, group, *got.GroupNameSnapshot)
	require.Equal(t, platform, *got.PlatformSnapshot)
	require.Equal(t, service.UsageAuthorizationModeBPS, got.AuthorizationMode)
	legacy := prepareUsageLogInsert(&service.UsageLog{UserID: 1, APIKeyID: 2, AccountID: 3, Model: "model", CreatedAt: time.Now()})
	got, e = scanUsageLog(vendorSnapshotScanner{columns: cols, prepared: legacy.args})
	require.NoError(t, e)
	require.Nil(t, got.VendorIDSnapshot)
	require.Empty(t, got.AuthorizationMode)
}

type vendorSnapshotScanner struct {
	columns  []string
	prepared []any
}

func (s vendorSnapshotScanner) Scan(dest ...any) error {
	// Initialize the complete selected shape, then set only attribution. The
	// ordinary scanner tests cover the existing cost/token/request fields.
	for _, d := range dest {
		v := reflect.ValueOf(d).Elem()
		v.Set(reflect.Zero(v.Type()))
	}
	for i, c := range s.columns {
		switch c {
		case "vendor_id_snapshot":
			*dest[i].(*sql.NullInt64) = s.prepared[i-1].(sql.NullInt64)
		case "account_name_snapshot", "group_name_snapshot", "platform_snapshot", "authorization_mode":
			*dest[i].(*sql.NullString) = s.prepared[i-1].(sql.NullString)
		}
	}
	return nil
}
