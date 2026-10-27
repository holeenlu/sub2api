package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/lib/pq"
	"regexp"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func supplyRepoMock(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func TestExcelAuthorizationPresenceUsesVisibilityAndOneBatch(t *testing.T) {
	r, mock := supplyRepoMock(t)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin})
	query := "SELECT g.account_id FROM openai_excel_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=ANY($1) AND a.deleted_at IS NULL AND g.ciphertext<>'' AND (" + accountVisibilitySQL(ctx, "a.vendor_id") + ")"
	require.Contains(t, query, "vendor_id IS NULL")
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs(pq.Array([]int64{1, 2})).WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(2))
	got, err := r.ExcelAuthorizationPresence(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, map[int64]bool{2: true}, got)
	got, err = r.ExcelAuthorizationPresence(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, got)
}

// Existing SQL-expression tests use a recording writer. Supply qualification
// must still read real typed rows before a credential/proxy batch mutation.
func allowNativeBulkPreflight(t *testing.T, exec *recordingSQLExecutor) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	exec.queryDB = db
	mock.ExpectQuery("native_supply_credential_preflight").WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "vendor_id", "parent_account_id", "proxy_id", "credentials"}).AddRow(17, service.PlatformOpenAI, service.AccountTypeAPIKey, nil, nil, nil, []byte("{}")))
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); _ = db.Close() })
}

func expectNativeAPIKeyCredentialPreflight(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=").WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "vendor_id", "parent_account_id", "proxy_id", "unchanged"}).AddRow(service.PlatformOpenAI, service.AccountTypeAPIKey, nil, nil, nil, false))
}

func TestNativeAccountSupplierScopeIsAppliedInSQL(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want []string
	}{
		{"admin", authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin}), []string{"vendor_id", "IS NULL"}},
		{"supplier", authz.WithVendorScope(authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.VendorLevel1}), authz.VendorScope{VendorID: 7, ActorUserID: 9, PolicyVersion: 3}), []string{"vendor_id", "=7", "vs.user_id=9", "vs.policy_version=3", "vu.deleted_at IS NULL", "vendor_console_enabled"}},
		{"invalid scope", authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: 7}), []string{"FALSE"}},
		{"old root task", authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, ManagementRequest: true, UserID: 9, SessionGeneration: 4}), []string{"IS NULL OR", "va.id=9", "va.role='super_admin'", "va.session_generation=4"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("accounts"))
			accountScopePredicate(tc.ctx)(query)
			sql, _ := query.Query()
			for _, want := range tc.want {
				require.Contains(t, sql, want)
			}
		})
	}
}

func TestNativeSupplierMutationGuardHidesForeignIDs(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin, ManagementRequest: true})
	mock.ExpectQuery("SELECT COUNT.*vendor_id.*IS NULL").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	require.ErrorIs(t, repo.SetSchedulable(ctx, 77, true), service.ErrAccountNotFound)
}

func TestNativeVendorCreationRequiresVerifiedFlow(t *testing.T) {
	id := int64(7)
	a := &service.Account{VendorID: &id}
	require.ErrorIs(t, createAccountRecord(context.Background(), nil, a), authz.ErrPermissionDenied)
	ctx := authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: id, ActorUserID: 9, PolicyVersion: 1})
	require.ErrorIs(t, createAccountRecord(ctx, nil, a), authz.ErrPermissionDenied)
	a.VendorID = nil
	require.ErrorIs(t, createAccountRecord(ctx, nil, a), authz.ErrPermissionDenied)
}

func TestNativeVendorMetadataEditPreservesConcurrentLifecycle(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(map[bool]string{true: "metadata", false: "credential replacement"}[same], func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			id := int64(7)
			paused := time.Now().UTC()
			a := &service.Account{ID: 1, VendorID: &id, Status: service.StatusActive, Schedulable: true, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "new"}}
			mock.ExpectQuery("SELECT vendor_paused_at,vendor_notes,status,schedulable").WillReturnRows(sqlmock.NewRows([]string{"vendor_paused_at", "vendor_notes", "status", "schedulable", "same_identity", "archived"}).AddRow(paused, "supplier note", service.StatusDisabled, false, same, false))
			err := preserveNativeVendorAccount(context.Background(), repo.client, a)
			if same {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, authz.ErrPermissionDenied)
			}
			require.Equal(t, service.StatusDisabled, a.Status)
			require.False(t, a.Schedulable)
			require.Equal(t, &paused, a.VendorPausedAt)
			require.Equal(t, "supplier note", a.VendorNotes)
		})
	}
}

func TestNativeAccountOwnershipCannotBeDroppedByLegacyDTO(t *testing.T) {
	query := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("accounts"))
	vendorOwnerUnchangedPredicate(nil)(query)
	sql, _ := query.Query()
	require.Contains(t, sql, "IS NULL")
}

func TestVendorSupplyQueryUsesCompleteCurrentPolicy(t *testing.T) {
	query := vendorSupplySQL("a", "$4::bigint")
	for _, part := range []string{"a.vendor_id IS NULL", "vendor_paused_at IS NULL", "vendor_console_enabled", "v.status='active'", "vu.status='active'", "vu.role='vendor_level1'", "vu.role='vendor_level2'", "vp_parent.status='active'", "vu_parent.role='vendor_level1'", "parent_g.vendor_id=v.parent_vendor_id", "vu.deleted_at IS NULL", "v.platforms @>", "v.allow_direct", "vp.vendor_id=v.id", "vp.deleted_at IS NULL", "vp.status='active'", "vp.expires_at>NOW()", "vendor_groups", "ag.account_id=a.id", "vsg.status='active'", "vsg.deleted_at IS NULL", "vsg.platform=a.platform", "vsg.id=$4::bigint", "parent.vendor_paused_at IS NULL"} {
		require.Contains(t, query, part)
	}
	require.NotContains(t, query, "user_subscriptions", "consumer authorization stays in the native API-key path")
}

func TestVendorSupplyLiveReadFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rows    *sqlmock.Rows
		err     error
		allowed bool
	}{
		{name: "allowed", rows: sqlmock.NewRows([]string{"exists"}).AddRow(true), allowed: true},
		{name: "revoked", rows: sqlmock.NewRows([]string{"exists"}).AddRow(false)},
		{name: "missing", rows: sqlmock.NewRows([]string{"exists"})},
		{name: "database failure", err: errors.New("connection lost")},
		{name: "decode failure", rows: sqlmock.NewRows([]string{"exists"}).AddRow("invalid-bool")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			id, group := int64(7), int64(13)
			a := &service.Account{ID: 1, VendorID: &id, Platform: service.PlatformOpenAI}
			exp := mock.ExpectQuery("SELECT EXISTS.*a.proxy_id IS NOT DISTINCT.*p.config_version.*vendor_groups").WithArgs(a.ID, id, a.Platform, group, nil, nil)
			if tc.err != nil {
				exp.WillReturnError(tc.err)
			} else {
				exp.WillReturnRows(tc.rows)
			}
			err := repo.ValidateVendorSupply(context.Background(), a, &group)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrVendorSupplyUnavailable)
			}
		})
	}
}

func TestVendorSupplyPrivateProxyRequiresCurrentOwnerAndVersion(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	id, proxy := int64(7), int64(9)
	a := &service.Account{ID: 1, VendorID: &id, ProxyID: &proxy, Platform: service.PlatformAnthropic}
	require.ErrorIs(t, repo.ValidateVendorSupply(context.Background(), a, nil), service.ErrVendorSupplyUnavailable)
	other := int64(8)
	a.Proxy = &service.Proxy{ID: proxy, VendorID: &other, ConfigVersion: 4}
	require.ErrorIs(t, repo.ValidateVendorSupply(context.Background(), a, nil), service.ErrVendorSupplyUnavailable)
	a.Proxy.VendorID = &id
	mock.ExpectQuery("p.config_version=\\$6").WithArgs(a.ID, id, a.Platform, nil, proxy, int64(4)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	require.ErrorIs(t, repo.ValidateVendorSupply(context.Background(), a, nil), service.ErrVendorSupplyUnavailable)
}

func TestVendorBatchQualificationRepairsOwnerlessCachedMetadata(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	group := int64(13)
	mock.ExpectQuery("SELECT a.id,a.vendor_id,a.vendor_paused_at").WithArgs("{1,2,3,4}", group).WillReturnRows(sqlmock.NewRows([]string{"id", "vendor_id", "vendor_paused_at", "allowed", "native", "bps"}).AddRow(1, nil, nil, true, false, false).AddRow(2, 7, nil, false, false, false).AddRow(3, 8, nil, true, true, false))
	input := []service.Account{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	out, err := repo.FilterVendorSupply(context.Background(), input, &group)
	require.NoError(t, err)
	require.Len(t, out, 2)
	require.Equal(t, int64(1), out[0].ID)
	require.Equal(t, int64(3), out[1].ID)
	require.Equal(t, int64(8), *out[1].VendorID)
	require.Nil(t, input[2].VendorID)
}

func TestVendorSnapshotRefreshPersistsOutboxAndNeverRewritesLifecycle(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.RefreshVendorSnapshots(context.Background(), 7))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnError(errors.New("outbox unavailable"))
	require.EqualError(t, repo.RefreshVendorSnapshots(context.Background(), 7), "outbox unavailable")
}

func TestNativeGroupBindingsCannotExpandSupplierPolicy(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	mock.ExpectQuery("SELECT id FROM vendors.*FOR SHARE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery("SELECT EXISTS.*vendor_groups.*g.platform=a.platform").WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(true))
	require.ErrorIs(t, validateVendorGroupBindings(context.Background(), repo.sql, []int64{1}, []int64{13}), authz.ErrPermissionDenied)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin, ManagementRequest: true})
	mock.ExpectQuery("SELECT .*EXISTS.*vendor_groups").WillReturnRows(sqlmock.NewRows([]string{"denied"}).AddRow(true))
	require.ErrorIs(t, guardVendorGroupWrite(ctx, repo.sql, []int64{13}), authz.ErrPermissionDenied)
}

func TestNativeUserReadFiltersSupplierLoginWithoutRemovingConsumerGroups(t *testing.T) {
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin})
	query := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("users"))
	userSupplyScopePredicate(ctx)(query)
	sql, _ := query.Query()
	require.Contains(t, sql, "NOT EXISTS (SELECT 1 FROM vendors")
	require.False(t, strings.Contains(sql, "vendor_groups"))
}

func TestPlatformSuperAdminGroupBindingsGrantAuthority(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, UserID: 9, SessionGeneration: 1, ManagementRequest: true})
	mock.ExpectQuery("SELECT id FROM users.*FOR SHARE").WithArgs(int64(9), int64(1)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectQuery("SELECT id FROM vendors.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery("SELECT EXISTS.*vendor_archived_at").WillReturnRows(sqlmock.NewRows([]string{"archived"}).AddRow(false))
	mock.ExpectQuery("account_group_live_group_lock").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(16))
	mock.ExpectExec("WITH owners AS.*INSERT INTO vendor_groups.*ON CONFLICT.*UPDATE vendors.*INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, validateVendorGroupBindings(ctx, repo.sql, []int64{1}, []int64{16}))
}

func TestPlatformGroupBindingGrantRejectsStaleRootAndArchives(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprint(live), func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, UserID: 9, SessionGeneration: 1, ManagementRequest: true})
			rows := sqlmock.NewRows([]string{"id"})
			if live {
				rows.AddRow(9)
			}
			mock.ExpectQuery("SELECT id FROM users.*FOR SHARE").WillReturnRows(rows)
			if live {
				mock.ExpectQuery("SELECT id FROM vendors.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
				mock.ExpectQuery("SELECT EXISTS.*vendor_archived_at").WillReturnRows(sqlmock.NewRows([]string{"archived"}).AddRow(true))
				require.ErrorIs(t, validateVendorGroupBindings(ctx, repo.sql, []int64{1}, []int64{16}), service.ErrVendorAccountArchived)
			} else {
				require.ErrorIs(t, validateVendorGroupBindings(ctx, repo.sql, []int64{1}, []int64{16}), authz.ErrPermissionDenied)
			}
		})
	}
}

func TestScopedManagementKeyCannotGrantSupplierGroups(t *testing.T) {
	repo, mock := supplyRepoMock(t)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, UserID: 9, ManagementRequest: true})
	ctx = adminaccess.WithPrincipal(ctx, &adminaccess.Principal{KeyID: "limited"})
	mock.ExpectQuery("SELECT id FROM vendors.*FOR SHARE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery("SELECT EXISTS.*vendor_groups").WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(true))
	require.ErrorIs(t, validateVendorGroupBindings(ctx, repo.sql, []int64{1}, []int64{16}), authz.ErrPermissionDenied)
}
