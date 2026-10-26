package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// A disposable schema in the opt-in local test database exercises PostgreSQL
// NULL, JSONB, array, transaction and row-lock behavior without migrations.
func vendorSupplyPostgres(t *testing.T) (*accountRepository, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("VENDOR_SUPPLY_TEST_DSN")
	if dsn == "" {
		t.Skip("VENDOR_SUPPLY_TEST_DSN not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"127.0.0.1", "localhost"}, u.Hostname())
	require.Equal(t, "/vendor_settlement_test", u.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	schema := fmt.Sprintf("vendor_supply_test_%d", time.Now().UnixNano())
	_, err = db.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	params := u.Query()
	params.Set("search_path", schema)
	params.Set("application_name", schema)
	u.RawQuery = params.Encode()
	scoped, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, scoped)))
	t.Cleanup(func() {
		_ = client.Close()
		_, err := db.Exec("DROP SCHEMA " + schema + " CASCADE")
		require.NoError(t, err)
		_ = db.Close()
	})
	_, err = scoped.Exec(`
CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);
CREATE TABLE users(id BIGINT PRIMARY KEY,role TEXT,status TEXT,session_generation BIGINT,deleted_at TIMESTAMPTZ);
CREATE TABLE vendors(id BIGINT PRIMARY KEY,user_id BIGINT,status TEXT,policy_version BIGINT,platforms JSONB,allow_direct BOOL);
CREATE TABLE groups(id BIGINT PRIMARY KEY,platform TEXT,status TEXT,deleted_at TIMESTAMPTZ);
CREATE TABLE vendor_groups(vendor_id BIGINT,group_id BIGINT);
CREATE TABLE proxies(id BIGINT PRIMARY KEY,vendor_id BIGINT,status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ,expires_at TIMESTAMPTZ,config_version BIGINT DEFAULT 1,name TEXT DEFAULT 'proxy',protocol TEXT DEFAULT 'http',host TEXT DEFAULT 'proxy.example',port INT DEFAULT 8080,username TEXT,password TEXT,created_at TIMESTAMPTZ DEFAULT NOW(),updated_at TIMESTAMPTZ DEFAULT NOW(),fallback_mode TEXT DEFAULT 'none',backup_proxy_id BIGINT,expiry_warn_days INT DEFAULT 7,created_by BIGINT,vendor_notes TEXT DEFAULT '',region_label_country_code TEXT DEFAULT '',region_label_city TEXT DEFAULT '');
CREATE TABLE accounts(id BIGINT PRIMARY KEY,vendor_id BIGINT,vendor_paused_at TIMESTAMPTZ,vendor_notes TEXT DEFAULT '',platform TEXT DEFAULT 'openai',type TEXT DEFAULT 'oauth',parent_account_id BIGINT,proxy_id BIGINT,proxy_fallback_origin_id BIGINT,status TEXT DEFAULT 'active',schedulable BOOL DEFAULT TRUE,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW(),expires_at TIMESTAMPTZ,auto_pause_on_expired BOOL DEFAULT TRUE,temp_unschedulable_until TIMESTAMPTZ,rate_limit_reset_at TIMESTAMPTZ,overload_until TIMESTAMPTZ,credentials JSONB DEFAULT '{}',extra JSONB DEFAULT '{}');
CREATE TABLE account_groups(id BIGSERIAL PRIMARY KEY,account_id BIGINT,group_id BIGINT,priority INT DEFAULT 1,created_at TIMESTAMPTZ DEFAULT NOW(),UNIQUE(account_id,group_id));
CREATE TABLE scheduled_test_plans(account_id BIGINT);
CREATE TABLE openai_excel_grants(account_id BIGINT,credentials JSONB);
CREATE TABLE account_supply_identity_claims(canonical_account_id BIGINT,vendor_id BIGINT,identity_hash TEXT);
CREATE TABLE usage_logs(account_id BIGINT,vendor_id_snapshot BIGINT);
CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT,created_at TIMESTAMPTZ DEFAULT NOW());
CREATE UNIQUE INDEX outbox_dedup ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL;
INSERT INTO settings VALUES('vendor_console_enabled','true');
INSERT INTO users VALUES(3,'user','active',1,NULL),(9,'super_admin','active',1,NULL);
INSERT INTO vendors VALUES(7,3,'active',1,'["openai","anthropic"]',TRUE);
INSERT INTO groups VALUES(13,'openai','active',NULL),(14,'anthropic','active',NULL),(15,'openai','active',NULL);
INSERT INTO vendor_groups VALUES(7,13),(7,14);
INSERT INTO proxies(id,vendor_id) VALUES(10,7),(11,8);
INSERT INTO accounts(id,vendor_id,credentials,extra) VALUES(1,7,'{"access_token":"root-secret"}','{"private":"root"}'),(2,NULL,'{"access_token":"native"}','{}');
INSERT INTO accounts(id,vendor_id,parent_account_id,credentials,extra) VALUES(3,7,1,'{"access_token":"shadow-secret"}','{"private":"shadow"}');
INSERT INTO account_groups(account_id,group_id) VALUES(1,13),(2,13),(3,13);
INSERT INTO scheduled_test_plans VALUES(1),(2),(3);
INSERT INTO openai_excel_grants VALUES(1,'{"token":"secret"}'),(2,'{"token":"native"}'),(3,'{"token":"shadow"}');
INSERT INTO account_supply_identity_claims VALUES(1,7,'retained-claim');
INSERT INTO usage_logs VALUES(1,7),(3,7);
`)
	require.NoError(t, err)
	_, err = scoped.Exec("ALTER TABLE vendors ADD COLUMN openai_authorization_modes JSONB NOT NULL DEFAULT '[\"native\"]'; ALTER TABLE openai_excel_grants ADD COLUMN ciphertext TEXT NOT NULL DEFAULT ''; ALTER TABLE vendors ADD COLUMN tier SMALLINT NOT NULL DEFAULT 1; ALTER TABLE vendors ADD COLUMN parent_vendor_id BIGINT; UPDATE users SET role='vendor_level1' WHERE id IN(SELECT user_id FROM vendors) AND role='user'")
	require.NoError(t, err)
	return newAccountRepositoryWithSQL(client, scoped, nil), scoped
}

func TestVendorSupplyPostgresEligibilityMatrix(t *testing.T) {
	_, db := vendorSupplyPostgres(t)
	for _, tc := range []struct {
		name, mutate                           string
		allowed, proxy, staleOwner, staleProxy bool
	}{
		{name: "eligible direct", allowed: true},
		{name: "eligible private proxy", proxy: true, allowed: true},
		{name: "feature disabled", mutate: "UPDATE settings SET value='false'"},
		{name: "vendor disabled", mutate: "UPDATE vendors SET status='disabled'"},
		{name: "login disabled", mutate: "UPDATE users SET status='disabled' WHERE id=3"},
		{name: "login deleted", mutate: "UPDATE users SET deleted_at=NOW() WHERE id=3"},
		{name: "login promoted", mutate: "UPDATE users SET role='admin' WHERE id=3"},
		{name: "platform revoked", mutate: "UPDATE vendors SET platforms=jsonb_build_array('anthropic')"},
		{name: "direct revoked", mutate: "UPDATE vendors SET allow_direct=FALSE"},
		{name: "archive marker overrides stale flags", mutate: "UPDATE accounts SET extra=extra||jsonb_build_object('vendor_archived_at',NOW()) WHERE id=1"},
		{name: "account paused", mutate: "UPDATE accounts SET vendor_paused_at=NOW() WHERE id=1"},
		{name: "account disabled", mutate: "UPDATE accounts SET status='disabled' WHERE id=1"},
		{name: "account deleted", mutate: "UPDATE accounts SET deleted_at=NOW() WHERE id=1"},
		{name: "account unschedulable", mutate: "UPDATE accounts SET schedulable=FALSE WHERE id=1"},
		{name: "account expiry", mutate: "UPDATE accounts SET expires_at=NOW()-INTERVAL '1 minute' WHERE id=1"},
		{name: "account expiry advisory", mutate: "UPDATE accounts SET expires_at=NOW()-INTERVAL '1 minute',auto_pause_on_expired=FALSE WHERE id=1", allowed: true},
		{name: "temporary quarantine", mutate: "UPDATE accounts SET temp_unschedulable_until=NOW()+INTERVAL '1 minute' WHERE id=1"},
		{name: "rate limit", mutate: "UPDATE accounts SET rate_limit_reset_at=NOW()+INTERVAL '1 minute' WHERE id=1"},
		{name: "overload", mutate: "UPDATE accounts SET overload_until=NOW()+INTERVAL '1 minute' WHERE id=1"},
		{name: "unsupported account type", mutate: "UPDATE accounts SET type='apikey' WHERE id=1"},
		{name: "group revoked", mutate: "DELETE FROM vendor_groups WHERE group_id=13"},
		{name: "account membership removed", mutate: "DELETE FROM account_groups WHERE account_id=1"},
		{name: "group disabled", mutate: "UPDATE groups SET status='disabled' WHERE id=13"},
		{name: "group deleted", mutate: "UPDATE groups SET deleted_at=NOW() WHERE id=13"},
		{name: "group platform mismatch", mutate: "UPDATE groups SET platform='anthropic' WHERE id=13"},
		{name: "old snapshot has no vendor", staleOwner: true},
		{name: "proxy owner changed", proxy: true, mutate: "UPDATE proxies SET vendor_id=8 WHERE id=10"},
		{name: "proxy disabled", proxy: true, mutate: "UPDATE proxies SET status='disabled' WHERE id=10"},
		{name: "proxy deleted", proxy: true, mutate: "UPDATE proxies SET deleted_at=NOW() WHERE id=10"},
		{name: "proxy expired", proxy: true, mutate: "UPDATE proxies SET expires_at=NOW()-INTERVAL '1 minute' WHERE id=10"},
		{name: "proxy version changed", proxy: true, mutate: "UPDATE proxies SET config_version=2 WHERE id=10"},
		{name: "proxy binding changed", proxy: true, staleProxy: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			repo := &accountRepository{sql: tx}
			vendorID, groupID, proxyID := int64(7), int64(13), int64(10)
			a := &service.Account{ID: 1, VendorID: &vendorID, Platform: service.PlatformOpenAI}
			if tc.proxy {
				_, err = tx.Exec("UPDATE accounts SET proxy_id=10 WHERE id=1")
				require.NoError(t, err)
				a.ProxyID = &proxyID
				a.Proxy = &service.Proxy{ID: proxyID, VendorID: &vendorID, ConfigVersion: 1}
			}
			if tc.staleOwner {
				a.VendorID = nil
			}
			if tc.staleProxy {
				a.ProxyID = nil
				a.Proxy = nil
			}
			if tc.mutate != "" {
				_, err = tx.Exec(tc.mutate)
				require.NoError(t, err)
			}
			err = repo.ValidateVendorSupply(context.Background(), a, &groupID)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrVendorSupplyUnavailable)
			}
			// Native accounts and API-key consumer eligibility are not supplier policy.
			require.NoError(t, repo.ValidateVendorSupply(context.Background(), &service.Account{ID: 2}, &groupID))
		})
	}
}

func TestVendorSupplyPostgresScopeGroupsAndLegacySnapshots(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	ctx := context.Background()
	groupID, wrongGroup := int64(13), int64(15)
	input := []service.Account{{ID: 1}, {ID: 2}, {ID: 3}}
	got, err := repo.FilterVendorSupply(ctx, input, &groupID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, int64(7), *got[0].VendorID)
	require.Nil(t, input[0].VendorID)
	got, err = repo.FilterVendorSupply(ctx, input, &wrongGroup)
	require.NoError(t, err)
	require.Equal(t, []service.Account{{ID: 2}}, got)
	_, err = db.Exec("UPDATE accounts SET extra=extra||jsonb_build_object('vendor_archived_at',NOW()) WHERE id=1")
	require.NoError(t, err)
	got, err = repo.FilterVendorSupply(ctx, input, &groupID)
	require.NoError(t, err)
	require.Equal(t, []service.Account{{ID: 2}}, got, "archive marker excludes its shadow even with stale pause and schedule flags")

	admin := authz.WithSubject(ctx, authz.Subject{Role: authz.Admin, ManagementRequest: true})
	require.ErrorIs(t, repo.guardAccountScope(admin, 1, 2), service.ErrAccountNotFound)
	require.NoError(t, repo.guardAccountScope(admin, 2))
	root := authz.WithSubject(ctx, authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1, ManagementRequest: true})
	require.NoError(t, repo.guardAccountScope(root, 1))
	stale := authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 7, ActorUserID: 3, PolicyVersion: 2})
	require.ErrorIs(t, repo.guardAccountScope(stale, 1), service.ErrAccountNotFound)
	require.ErrorIs(t, validateVendorGroupBindings(ctx, db, []int64{1}, nil), authz.ErrPermissionDenied)
	require.ErrorIs(t, validateVendorGroupBindings(ctx, db, []int64{1}, []int64{14, 15}), authz.ErrPermissionDenied)
	require.ErrorIs(t, validateVendorGroupBindings(ctx, db, []int64{1}, []int64{13}), authz.ErrPermissionDenied, "archived bindings are immutable")
	require.ErrorIs(t, guardVendorGroupWrite(admin, db, []int64{13}), authz.ErrPermissionDenied)
	require.NoError(t, guardVendorGroupWrite(root, db, []int64{13}))
	_, err = db.Exec("UPDATE users SET session_generation=2 WHERE id=9")
	require.NoError(t, err)
	require.ErrorIs(t, repo.guardAccountScope(root, 1), service.ErrAccountNotFound)
	require.ErrorIs(t, guardVendorGroupWrite(root, db, []int64{13}), authz.ErrPermissionDenied)
}

func TestVendorSupplyPostgresNativeDeleteCannotBypassArchive(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	contexts := []context.Context{
		context.Background(),
		authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1, ManagementRequest: true}),
		authz.WithVendorScope(authz.WithSubject(context.Background(), authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1}), authz.VendorScope{VendorID: 7, ActorUserID: 3, PolicyVersion: 1}),
	}
	for _, ctx := range contexts {
		for _, id := range []int64{1, 3} {
			require.ErrorIs(t, repo.Delete(ctx, id), authz.ErrPermissionDenied)
		}
	}
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM accounts WHERE id IN(1,3) AND deleted_at IS NULL AND credentials->>'access_token'<>'' AND extra->>'private'<>''").Scan(&count))
	require.Equal(t, 2, count)
	for _, table := range []string{"account_groups", "scheduled_test_plans", "openai_excel_grants"} {
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE account_id IN(1,3)").Scan(&count))
		require.Equal(t, 2, count, table)
	}
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, repo.Delete(context.Background(), 2), "native platform deletion remains supported")
}

func TestVendorSupplyPostgresArchiveBlocksLateNativeWrites(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := db.Exec("ALTER TABLE accounts ADD COLUMN error_message TEXT,ADD COLUMN name TEXT; UPDATE accounts SET error_message='retained health',credentials=credentials||'{\"refresh_token\":\"retained-refresh\"}' WHERE id=1")
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "UPDATE accounts SET extra=extra||jsonb_build_object('vendor_archived_at',NOW()),schedulable=FALSE,vendor_paused_at=NOW() WHERE id IN(1,3)")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		done <- repo.UpdateExtra(ctx, 1, map[string]any{"codex_credits_snapshot": map[string]any{"available": 9}})
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE 'UPDATE accounts SET extra%')").Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-done, service.ErrAccountNotFound)
	require.NoError(t, repo.SetSchedulable(ctx, 1, true))
	require.NoError(t, repo.SetError(ctx, 1, "late error"))
	require.NoError(t, repo.ClearError(ctx, 1))
	name := "late edit"
	affected, err := repo.BulkUpdate(ctx, []int64{1}, service.AccountBulkUpdate{Name: &name})
	require.NoError(t, err)
	require.Zero(t, affected)
	var preserved bool
	require.NoError(t, db.QueryRow("SELECT NOT schedulable AND status='active' AND error_message='retained health' AND NOT(extra ? 'codex_credits_snapshot') AND extra->>'private'='root' AND credentials->>'access_token'='root-secret' FROM accounts WHERE id=1").Scan(&preserved))
	require.True(t, preserved)
	require.ErrorIs(t, repo.Delete(ctx, 1), authz.ErrPermissionDenied)
	require.NoError(t, repo.RemoveFromGroup(ctx, 1, 13))
	var bindings int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM account_groups WHERE account_id=1 AND group_id=13").Scan(&bindings))
	require.Equal(t, 1, bindings)
	page, err := repo.ListOAuthRefreshCandidatePage(ctx, service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, Limit: 20, ActiveOnly: true, RequireRefreshToken: true})
	require.NoError(t, err)
	for _, a := range page.Accounts {
		require.NotContains(t, []int64{1, 3}, a.ID)
	}
}

func TestVendorSupplyPostgresGroupPolicyRaceAndUserGroupBypass(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin := authz.WithSubject(ctx, authz.Subject{Role: authz.Admin, ManagementRequest: true, UserID: 4})
	groups := newGroupRepositoryWithSQL(repo.client, db)
	users := newUserRepositoryWithSQL(repo.client, db)
	require.ErrorIs(t, users.AddGroupToAllowedGroups(admin, 3, 15), authz.ErrPermissionDenied)
	require.ErrorIs(t, users.RemoveGroupFromUserAllowedGroups(admin, 3, 15), authz.ErrPermissionDenied)
	require.ErrorIs(t, groups.BindAccountsToGroup(admin, 13, []int64{1}), service.ErrAccountNotFound)
	_, err := db.Exec("INSERT INTO account_groups(account_id,group_id) VALUES(2,15)")
	require.NoError(t, err)
	grant, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer grant.Rollback()
	var id int64
	require.NoError(t, grant.QueryRowContext(ctx, "SELECT id FROM groups WHERE id=15 FOR SHARE").Scan(&id))
	done := make(chan error, 1)
	go func() { _, err := groups.DeleteAccountGroupsByGroupID(admin, 15); done <- err }()
	// Observe the lock wait itself, rather than relying on goroutine timing.
	require.Eventually(t, func() bool {
		var waiting bool
		err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE application_name=current_setting('application_name') AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM groups WHERE id=%FOR UPDATE%')").Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	_, err = grant.ExecContext(ctx, "INSERT INTO vendor_groups VALUES(7,15)")
	require.NoError(t, err)
	require.NoError(t, grant.Commit())
	require.ErrorIs(t, <-done, authz.ErrPermissionDenied, "grant committed while native mutation waited must be rechecked")
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM account_groups WHERE group_id=15").Scan(&count))
	require.Equal(t, 1, count)
}

func TestVendorSupplyPostgresFallbackRevertChecksAndRollsBack(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	ctx := context.Background()
	_, err := db.Exec("UPDATE accounts SET proxy_fallback_origin_id=10 WHERE id=1; UPDATE proxies SET status='disabled' WHERE id=10")
	require.NoError(t, err)
	require.ErrorIs(t, repo.RevertProxyFallback(ctx, 1), service.ErrVendorProxyUnavailable)
	_, err = db.Exec("UPDATE proxies SET status='active',vendor_id=8 WHERE id=10")
	require.NoError(t, err)
	require.ErrorIs(t, repo.RevertProxyFallback(ctx, 1), service.ErrProxyNotFound)
	_, err = db.Exec("UPDATE proxies SET vendor_id=7 WHERE id=10; ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_write CHECK (FALSE)")
	require.NoError(t, err)
	require.Error(t, repo.RevertProxyFallback(ctx, 1))
	var unchanged bool
	require.NoError(t, db.QueryRow("SELECT proxy_id IS NULL AND proxy_fallback_origin_id=10 FROM accounts WHERE id=1").Scan(&unchanged))
	require.True(t, unchanged, "outbox failure must roll back proxy rebinding")
	_, err = db.Exec("ALTER TABLE scheduler_outbox DROP CONSTRAINT reject_write")
	require.NoError(t, err)
	require.NoError(t, repo.RevertProxyFallback(ctx, 1))
	require.NoError(t, db.QueryRow("SELECT proxy_id=10 AND proxy_fallback_origin_id IS NULL FROM accounts WHERE id=1").Scan(&unchanged))
	require.True(t, unchanged)
}

func TestVendorSupplyPostgresLateFailureCannotLiftPlatformDisable(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	_, err := db.Exec("ALTER TABLE accounts ADD COLUMN error_message TEXT; UPDATE accounts SET status='disabled',vendor_paused_at=NOW(),schedulable=FALSE WHERE id=1")
	require.NoError(t, err)
	require.NoError(t, repo.SetError(context.Background(), 1, "late upstream error"))
	require.NoError(t, repo.ClearError(context.Background(), 1))
	require.NoError(t, repo.RefreshVendorSnapshots(context.Background(), 7))
	var preserved bool
	require.NoError(t, db.QueryRow("SELECT status='disabled' AND vendor_paused_at IS NOT NULL AND NOT schedulable FROM accounts WHERE id=1").Scan(&preserved))
	require.True(t, preserved)
}

func TestVendorSupplyPostgresAdminAvailabilityUsesDispatchPolicy(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	_, err := db.Exec("ALTER TABLE groups ADD COLUMN name TEXT DEFAULT 'group'; ALTER TABLE vendors ADD COLUMN name TEXT DEFAULT 'supplier'; INSERT INTO account_groups(account_id,group_id) VALUES(1,15)")
	require.NoError(t, err)
	s := service.NewVendorService(db, repo.client, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	check := func(want int64) {
		t.Helper()
		rows, total, err := s.VendorGroupSupply(ctx, 7, 1, 20, "")
		require.NoError(t, err)
		require.Equal(t, int64(2), total)
		for _, row := range rows {
			if row.GroupID == 13 {
				require.Equal(t, want, row.AvailableCount)
			} else {
				require.Zero(t, row.AvailableCount, "grant to one group must not authorize another bound group")
			}
		}
	}
	check(2)
	_, err = db.Exec("UPDATE users SET role='admin' WHERE id=3")
	require.NoError(t, err)
	check(0)
	_, err = db.Exec("UPDATE users SET role='vendor_level1' WHERE id=3; UPDATE accounts SET vendor_paused_at=NOW() WHERE id=1")
	require.NoError(t, err)
	check(0)
	_, err = db.Exec("UPDATE accounts SET vendor_paused_at=NULL,proxy_id=11 WHERE id=1")
	require.NoError(t, err)
	// Root's foreign proxy is rejected; shadow has its independent authorized egress.
	check(1)
}

func TestVendorSupplyPostgresProjectionAndAtomicSnapshotIsolation(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	_, err := db.Exec("ALTER TABLE accounts ADD COLUMN concurrency INT DEFAULT 3, ADD COLUMN load_factor INT, ADD COLUMN priority INT DEFAULT 50; INSERT INTO vendor_groups VALUES(7,15); INSERT INTO account_groups(account_id,group_id) VALUES(1,15)")
	require.NoError(t, err)
	ctx := context.Background()
	loads, err := repo.ListSchedulableAccountLoads(ctx)
	require.NoError(t, err)
	require.Len(t, loads, 3, "multiple authorized groups must not multiply projection rows")
	ids := make(map[int64]bool)
	for _, load := range loads {
		require.False(t, ids[load.ID])
		ids[load.ID] = true
		require.Equal(t, 3, load.MaxConcurrency)
	}
	admin := authz.WithSubject(ctx, authz.Subject{Role: authz.Admin})
	update := map[string]any{"codex_credits_snapshot": map[string]any{"available": 3}}
	require.ErrorIs(t, repo.UpdateExtra(admin, 1, update), service.ErrAccountNotFound)
	require.NoError(t, repo.UpdateExtra(admin, 2, update))
	var supplierUnchanged, nativeUpdated bool
	require.NoError(t, db.QueryRow("SELECT NOT (extra ? 'codex_credits_snapshot') FROM accounts WHERE id=1").Scan(&supplierUnchanged))
	require.NoError(t, db.QueryRow("SELECT extra ? 'codex_credits_snapshot' FROM accounts WHERE id=2").Scan(&nativeUpdated))
	require.True(t, supplierUnchanged)
	require.True(t, nativeUpdated)
	var events int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&events))
	require.Zero(t, events, "display-only writes must not rebuild scheduling")
	_, err = db.Exec("UPDATE users SET status='disabled' WHERE id=3")
	require.NoError(t, err)
	loads, err = repo.ListSchedulableAccountLoads(ctx)
	require.NoError(t, err)
	require.Len(t, loads, 1)
	require.Equal(t, int64(2), loads[0].ID)
}

func TestVendorSupplyPostgresParentQualificationAndReadOnly(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	_, err := db.Exec("INSERT INTO users VALUES(4,'vendor_level1','active',1,NULL); INSERT INTO vendors(id,user_id,status,policy_version,platforms,allow_direct,tier) VALUES(8,4,'active',1,'[\"openai\",\"anthropic\"]',TRUE,1); INSERT INTO vendor_groups VALUES(8,13),(8,14); UPDATE vendors SET tier=2,parent_vendor_id=8 WHERE id=7; UPDATE users SET role='vendor_level2' WHERE id=3")
	require.NoError(t, err)
	vendorID, groupID := int64(7), int64(13)
	for _, tc := range []struct {
		name, mutation string
		allowed        bool
	}{
		{"eligible child", "", true},
		{"parent disabled", "UPDATE vendors SET status='disabled' WHERE id=8", false},
		{"parent login disabled", "UPDATE users SET status='disabled' WHERE id=4", false},
		{"parent role revoked", "UPDATE users SET role='user' WHERE id=4", false},
		{"parent deleted", "UPDATE users SET deleted_at=NOW() WHERE id=4", false},
		{"parent platform revoked", "UPDATE vendors SET platforms='[\"anthropic\"]' WHERE id=8", false},
		{"parent direct revoked", "UPDATE vendors SET allow_direct=FALSE WHERE id=8", false},
		{"parent group revoked", "DELETE FROM vendor_groups WHERE vendor_id=8 AND group_id=13", false},
		{"child pause retained", "UPDATE vendors SET status='disabled' WHERE id=8; UPDATE accounts SET vendor_paused_at=NOW() WHERE id=1; UPDATE vendors SET status='active' WHERE id=8", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, e := db.BeginTx(context.Background(), nil)
			require.NoError(t, e)
			defer tx.Rollback()
			if tc.mutation != "" {
				_, e = tx.Exec(tc.mutation)
				require.NoError(t, e)
			}
			r := &accountRepository{sql: tx}
			e = r.ValidateVendorSupply(context.Background(), &service.Account{ID: 1, VendorID: &vendorID, Platform: service.PlatformOpenAI}, &groupID)
			if tc.allowed {
				require.NoError(t, e)
			} else {
				require.ErrorIs(t, e, service.ErrVendorSupplyUnavailable)
			}
			filtered, e := r.FilterVendorSupply(context.Background(), []service.Account{{ID: 1}, {ID: 2}}, &groupID)
			require.NoError(t, e)
			expected := 1
			if tc.allowed {
				expected = 2
			}
			require.Len(t, filtered, expected)
			var fresh bool
			require.NoError(t, tx.QueryRow("SELECT "+accountAvailabilitySQL("a")+" AND "+vendorSupplySQL("a", "13::bigint")+" FROM accounts a WHERE id=1").Scan(&fresh))
			require.Equal(t, tc.allowed, fresh)

			// Exercise the native snapshot service with deliberately stale
			// list and sticky caches, after revocation and before any outbox.
			cache := &vendorHierarchyStaleCache{accounts: []*service.Account{{ID: 1, VendorID: &vendorID, Platform: service.PlatformOpenAI}, {ID: 2, Platform: service.PlatformOpenAI}}}
			snapshot := service.NewSchedulerSnapshotService(cache, nil, r, nil, nil)
			listed, _, e := snapshot.ListSchedulableAccounts(context.Background(), &groupID, service.PlatformOpenAI, false)
			require.NoError(t, e)
			require.Len(t, listed, expected)
			got, e := snapshot.GetAccount(context.Background(), 1)
			if tc.allowed {
				require.NoError(t, e)
				require.NotNil(t, got)
			} else {
				require.ErrorIs(t, e, service.ErrVendorSupplyUnavailable)
				require.Nil(t, got)
			}
		})
	}

	cache := &vendorHierarchyStaleCache{}
	repo.schedulerCache = cache
	require.NoError(t, repo.RefreshVendorSnapshots(context.Background(), 8))
	require.ElementsMatch(t, []int64{1, 3}, cache.deleted, "root invalidation covers child canonical and shadow accounts")
	var rebuilds int
	require.NoError(t, db.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE event_type=$1", service.SchedulerOutboxEventFullRebuild).Scan(&rebuilds))
	require.Equal(t, 1, rebuilds)
	parent := authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1}
	scope := authz.VendorScope{VendorID: 7, ActorUserID: 4, PolicyVersion: 1, ReadOnly: true}
	read := authz.WithVendorScope(authz.WithSubject(context.Background(), parent), scope)
	var visible int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM accounts WHERE "+accountVisibilitySQL(read, "vendor_id")).Scan(&visible))
	require.Equal(t, 2, visible)
	require.ErrorIs(t, repo.guardAccountScope(read, 1), authz.ErrPermissionDenied)
	for _, alter := range []func(*authz.Subject, *authz.VendorScope){
		func(a *authz.Subject, _ *authz.VendorScope) { a.SessionGeneration++ },
		func(a *authz.Subject, _ *authz.VendorScope) { a.UserID = 3 },
		func(a *authz.Subject, _ *authz.VendorScope) { a.Role = authz.VendorLevel2 },
		func(_ *authz.Subject, s *authz.VendorScope) { s.ReadOnly = false },
	} {
		a, s := parent, scope
		alter(&a, &s)
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM accounts WHERE "+accountVisibilitySQL(authz.WithVendorScope(authz.WithSubject(context.Background(), a), s), "vendor_id")).Scan(&visible))
		require.Zero(t, visible)
	}
}

type vendorHierarchyStaleCache struct {
	service.SchedulerCache
	accounts []*service.Account
	deleted  []int64
}

func (c *vendorHierarchyStaleCache) GetSnapshot(context.Context, service.SchedulerBucket) ([]*service.Account, bool, error) {
	return c.accounts, true, nil
}
func (c *vendorHierarchyStaleCache) GetAccount(context.Context, int64) (*service.Account, error) {
	return c.accounts[0], nil
}
func (c *vendorHierarchyStaleCache) DeleteAccount(_ context.Context, id int64) error {
	c.deleted = append(c.deleted, id)
	return nil
}

func TestPlatformSuperAdminPostgresAssignsSupplierAccountsToAnyGroup(t *testing.T) {
	repo, db := vendorSupplyPostgres(t)
	_, err := db.Exec(`ALTER TABLE vendors ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW(); ALTER TABLE vendor_groups ADD COLUMN is_default BOOL DEFAULT FALSE, ADD UNIQUE(vendor_id,group_id);
INSERT INTO groups VALUES(16,'composite','active',NULL),(17,'composite','active',NULL);
INSERT INTO users VALUES(4,'vendor_level1','active',1,NULL);
INSERT INTO vendors(id,user_id,status,policy_version,platforms,allow_direct,tier) VALUES(8,4,'active',1,'["openai","anthropic"]',TRUE,1);
INSERT INTO vendor_groups(vendor_id,group_id) VALUES(8,13),(8,14);
UPDATE vendors SET tier=2,parent_vendor_id=8 WHERE id=7; UPDATE users SET role='vendor_level2' WHERE id=3;`)
	require.NoError(t, err)
	ctx := authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, UserID: 9, SessionGeneration: 1, ManagementRequest: true})
	require.NoError(t, repo.BindGroups(ctx, 1, []int64{15, 16}))
	var owner, grants, version int64
	require.NoError(t, db.QueryRow("SELECT vendor_id FROM accounts WHERE id=1").Scan(&owner))
	require.Equal(t, int64(7), owner)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM vendor_groups WHERE vendor_id IN(7,8) AND group_id IN(15,16)").Scan(&grants))
	require.Equal(t, int64(4), grants, "platform assignment is an explicit owner-and-parent grant")
	require.NoError(t, db.QueryRow("SELECT policy_version FROM vendors WHERE id=7").Scan(&version))
	require.Equal(t, int64(2), version)
	group := int64(16)
	a := &service.Account{ID: 1, VendorID: &owner, Platform: service.PlatformOpenAI}
	require.NoError(t, repo.ValidateVendorSupply(context.Background(), a, &group), "Composite assignment must work at dispatch")
	require.NoError(t, repo.BindGroups(ctx, 1, []int64{15, 16}))
	require.NoError(t, db.QueryRow("SELECT policy_version FROM vendors WHERE id=7").Scan(&version))
	require.Equal(t, int64(2), version, "unchanged grants do not churn policy versions")
	_, err = db.Exec(`CREATE FUNCTION reject_assignment_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.group_id=17 THEN RAISE EXCEPTION 'forced assignment event failure'; END IF; RETURN NEW; END; $$;
CREATE TRIGGER reject_assignment_event BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION reject_assignment_event()`)
	require.NoError(t, err)
	require.ErrorContains(t, newGroupRepositoryWithSQL(repo.client, db).BindAccountsToGroup(ctx, 17, []int64{1}), "forced assignment event failure")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM vendor_groups WHERE group_id=17").Scan(&grants))
	require.Zero(t, grants, "failed binding and outbox writes roll back their accompanying grants")
	require.NoError(t, db.QueryRow("SELECT policy_version FROM vendors WHERE id=7").Scan(&version))
	require.Equal(t, int64(2), version)
	_, err = db.Exec("DROP TRIGGER reject_assignment_event ON scheduler_outbox; DROP FUNCTION reject_assignment_event()")
	require.NoError(t, err)
	require.ErrorIs(t, repo.BindGroups(ctx, 1, []int64{999}), service.ErrGroupNotFound)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM vendor_groups WHERE group_id=999").Scan(&grants))
	require.Zero(t, grants, "a failed binding rolls back its accompanying grant")
	require.NoError(t, db.QueryRow("SELECT policy_version FROM vendors WHERE id=7").Scan(&version))
	require.Equal(t, int64(2), version)
	_, err = db.Exec("DELETE FROM vendor_groups WHERE vendor_id=7 AND group_id=16")
	require.NoError(t, err)
	require.ErrorIs(t, repo.ValidateVendorSupply(context.Background(), a, &group), service.ErrVendorSupplyUnavailable, "later explicit revocation remains effective")
	vendor := authz.WithVendorScope(authz.WithSubject(context.Background(), authz.Subject{Role: authz.VendorLevel2, UserID: 3, SessionGeneration: 1}), authz.VendorScope{VendorID: 7, ActorUserID: 3, PolicyVersion: 2})
	require.ErrorIs(t, repo.BindGroups(vendor, 1, []int64{17}), authz.ErrPermissionDenied)
	admin := authz.WithSubject(context.Background(), authz.Subject{Role: authz.Admin, UserID: 5, ManagementRequest: true})
	require.Error(t, repo.BindGroups(admin, 1, []int64{17}))
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM account_groups WHERE account_id=1 AND group_id IN(15,16)").Scan(&grants))
	require.Equal(t, int64(2), grants, "failed supplier/admin edits preserve existing bindings")
	stale := authz.WithSubject(context.Background(), authz.Subject{Role: authz.SuperAdmin, UserID: 9, SessionGeneration: 2, ManagementRequest: true})
	require.Error(t, repo.BindGroups(stale, 1, []int64{17}))
	_, err = db.Exec("UPDATE accounts SET vendor_paused_at=NOW() WHERE id=1")
	require.NoError(t, err)
	require.NoError(t, repo.BindGroups(ctx, 1, []int64{16}))
	require.ErrorIs(t, repo.ValidateVendorSupply(context.Background(), a, &group), service.ErrVendorSupplyUnavailable, "assignment does not resume paused accounts")
}
