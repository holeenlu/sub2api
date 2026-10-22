package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Explicitly opt in to a disposable local database, never a configured app DB.
func vendorHierarchyPostgres(t *testing.T, legacy bool) (*VendorService, context.Context) {
	t.Helper()
	dsn := os.Getenv("VENDOR_HIERARCHY_TEST_DSN")
	if dsn == "" {
		t.Skip("VENDOR_HIERARCHY_TEST_DSN not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1"}, u.Hostname())
	require.Equal(t, "/vendor_settlement_test", u.Path)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	schema := fmt.Sprintf("vendor_hierarchy_%d", time.Now().UnixNano())
	_, err = db.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	scoped, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		scoped.Close()
		_, e := db.Exec("DROP SCHEMA " + schema + " CASCADE")
		require.NoError(t, e)
		db.Close()
	})
	_, err = scoped.Exec(`
 CREATE TABLE users(id BIGSERIAL PRIMARY KEY,email TEXT UNIQUE NOT NULL,username TEXT NOT NULL DEFAULT '',password_hash TEXT NOT NULL DEFAULT '',role TEXT NOT NULL DEFAULT 'user',status TEXT NOT NULL DEFAULT 'active',session_generation BIGINT NOT NULL DEFAULT 0,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW(),restrict_public_groups BOOL DEFAULT FALSE,concurrency INT DEFAULT 5,balance NUMERIC DEFAULT 0);
 CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,platform TEXT,status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ);
 CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT,updated_at TIMESTAMPTZ DEFAULT NOW());
 CREATE TABLE accounts(id BIGSERIAL PRIMARY KEY,name TEXT DEFAULT '',platform TEXT DEFAULT 'openai',type TEXT DEFAULT 'oauth',status TEXT DEFAULT 'active',schedulable BOOL DEFAULT TRUE,deleted_at TIMESTAMPTZ,parent_account_id BIGINT,proxy_id BIGINT,extra JSONB DEFAULT '{}');
 CREATE TABLE proxies(id BIGSERIAL PRIMARY KEY,backup_proxy_id BIGINT,fallback_mode TEXT DEFAULT 'none',deleted_at TIMESTAMPTZ,status TEXT DEFAULT 'active');
 CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT);
 CREATE TABLE api_keys(id BIGSERIAL PRIMARY KEY,user_id BIGINT,status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW());
 CREATE TABLE user_allowed_groups(user_id BIGINT,group_id BIGINT);
 CREATE TABLE user_subscriptions(id BIGSERIAL PRIMARY KEY,user_id BIGINT,status TEXT DEFAULT 'active',updated_at TIMESTAMPTZ DEFAULT NOW());
 CREATE TABLE pending_auth_sessions(id BIGSERIAL PRIMARY KEY,target_user_id BIGINT,consumed_at TIMESTAMPTZ);
 INSERT INTO users(id,email,role,session_generation) VALUES(9,'root@example.invalid','super_admin',1),(90,'customer@example.invalid','user',1),(91,'legacy@example.invalid','user',4),(92,'legacy-admin@example.invalid','admin',1);
 SELECT setval('users_id_seq',100);
 INSERT INTO groups(id,name,platform) VALUES(1,'OpenAI','openai'),(2,'Anthropic','anthropic'),(3,'Other OpenAI','openai');
 `)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "273_vendor_supply.sql"))
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	_, err = scoped.Exec("UPDATE settings SET value='true' WHERE key='vendor_console_enabled'")
	require.NoError(t, err)
	if legacy {
		_, err = scoped.Exec(`INSERT INTO vendors(user_id,name,platforms) VALUES(91,'Legacy','["openai"]'),(92,'Legacy admin','["openai"]');
  UPDATE users SET balance=12 WHERE id IN(90,91);
  INSERT INTO api_keys(user_id) VALUES(90),(91),(92);
  INSERT INTO user_allowed_groups VALUES(90,1),(91,1);
  INSERT INTO user_subscriptions(user_id) VALUES(90),(91);
  INSERT INTO pending_auth_sessions(target_user_id) VALUES(90),(91);`)
		require.NoError(t, err)
	}
	migration, err = os.ReadFile(filepath.Join("..", "..", "migrations", "276_vendor_hierarchy_roles.sql"))
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	migration, err = os.ReadFile(filepath.Join("..", "..", "migrations", "280_vendor_unlimited_resource_limits.sql"))
	require.NoError(t, err)
	_, err = scoped.Exec(string(migration))
	require.NoError(t, err)
	_, err = scoped.Exec(`ALTER TABLE vendors ADD COLUMN openai_authorization_modes JSONB NOT NULL DEFAULT '["native"]';`)
	require.NoError(t, err)
	s := &VendorService{db: scoped, settings: &SettingService{settingRepo: vendorPolicySettings{values: map[string]string{"vendor_console_enabled": "true"}}}}
	root := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	return s, root
}
func hierarchyCreate(t *testing.T, s *VendorService, ctx context.Context, email string, tier int, parent *int64) *Vendor {
	t.Helper()
	in := VendorCreateInput{Email: email, Password: "secure-test-password", Vendor: NewVendorPolicy()}
	in.Name = "  " + email + "  "
	in.Tier = tier
	in.ParentVendorID = parent
	in.Platforms = []string{"openai"}
	in.GroupIDs = []int64{1}
	in.AllowDirect = true
	v, err := s.CreateVendor(ctx, in)
	require.NoError(t, err)
	return v
}
func hierarchyActor(v *Vendor) context.Context {
	role := authz.VendorLevel1
	if v.Tier == 2 {
		role = authz.VendorLevel2
	}
	return authz.WithSubject(context.Background(), authz.Subject{UserID: v.UserID, Role: role})
}

func TestVendorHierarchyEnrollmentAtomicAndConcurrent(t *testing.T) {
	s, root := vendorHierarchyPostgres(t, false)
	parent := hierarchyCreate(t, s, root, "parent@example.invalid", 1, nil)
	require.Zero(t, parent.AccountLimit)
	require.Zero(t, parent.ProxyLimit)
	require.Equal(t, DefaultAccountConcurrency, parent.CreationDefaults.Concurrency)
	require.Equal(t, DefaultAccountPriority, parent.CreationDefaults.Priority)
	require.Equal(t, int64(1), parent.ContractVersion)
	require.Equal(t, "parent@example.invalid", parent.Email)
	require.Equal(t, parent.Name, parent.Username)
	require.Equal(t, "parent@example.invalid", parent.Username)
	var hash, role string
	var concurrency int
	require.NoError(t, s.db.QueryRow("SELECT password_hash,role,concurrency FROM users WHERE id=$1", parent.UserID).Scan(&hash, &role, &concurrency))
	require.Equal(t, RoleVendorLevel1, role)
	require.Zero(t, concurrency)
	require.True(t, (&User{PasswordHash: hash}).CheckPassword("secure-test-password"))
	payload, err := json.Marshal(parent)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "password")
	child := hierarchyCreate(t, s, hierarchyActor(parent), "child@example.invalid", 2, nil)
	require.Equal(t, parent.ID, *child.ParentVendorID)
	require.Equal(t, 2, child.Tier)
	require.Equal(t, child.Name, child.Username)
	items, total, err := s.ListVendors(hierarchyActor(parent), 1, 20, "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, child.ID, items[0].ID)
	_, _, err = s.ListVendors(hierarchyActor(child), 1, 20, "", "")
	require.ErrorIs(t, err, ErrVendorDenied)
	// Failure after inserting a user must roll the entire enrollment back.
	bad := VendorCreateInput{Email: "rollback@example.invalid", Password: "secure-test-password", Vendor: NewVendorPolicy()}
	bad.Name = "rollback"
	bad.Platforms = []string{"openai"}
	bad.GroupIDs = []int64{999}
	_, err = s.CreateVendor(root, bad)
	require.ErrorIs(t, err, ErrVendorInput)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM users WHERE email=$1", bad.Email).Scan(&count))
	require.Zero(t, count)
	bad.GroupIDs = []int64{1}
	bad.Email = "CUSTOMER@example.invalid"
	_, err = s.CreateVendor(root, bad)
	require.ErrorIs(t, err, ErrEmailExists)
	require.NoError(t, s.db.QueryRow("SELECT role FROM users WHERE id=90").Scan(&role))
	require.Equal(t, RoleUser, role)
	bad.Email = "race@example.invalid"
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.CreateVendor(root, bad); results <- e }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else {
			require.ErrorIs(t, e, ErrEmailExists)
		}
	}
	require.Equal(t, 1, success)
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM vendors v JOIN users u ON u.id=v.user_id WHERE u.email=$1", bad.Email).Scan(&count))
	require.Equal(t, 1, count)
}

func TestVendorHierarchyScopeAndParentRevocation(t *testing.T) {
	s, root := vendorHierarchyPostgres(t, false)
	parent := hierarchyCreate(t, s, root, "root1@example.invalid", 1, nil)
	other := hierarchyCreate(t, s, root, "root2@example.invalid", 1, nil)
	child := hierarchyCreate(t, s, hierarchyActor(parent), "child@example.invalid", 2, nil)
	sibling := hierarchyCreate(t, s, hierarchyActor(parent), "sibling@example.invalid", 2, nil)
	ctx := hierarchyActor(parent)
	_, err := s.RequireVendor(ctx, child.ID)
	require.ErrorIs(t, err, ErrVendorDenied, "a parent is not a general child capability")
	_, err = s.ReadVendorScope(ctx, other.ID)
	require.ErrorIs(t, err, ErrVendorDenied)
	_, err = s.ReadVendorScope(hierarchyActor(child), sibling.ID)
	require.ErrorIs(t, err, ErrVendorDenied)
	read, err := s.ReadVendorScope(ctx, child.ID)
	require.NoError(t, err)
	readCtx := authz.WithVendorScope(ctx, authz.VendorScope{VendorID: child.ID, PolicyVersion: read.PolicyVersion, ActorUserID: parent.UserID, ReadOnly: true})
	_, err = s.RequireVendor(readCtx, child.ID)
	require.NoError(t, err)
	_, err = s.CurrentVendorForWrite(readCtx)
	require.ErrorIs(t, err, ErrVendorDenied)
	_, err = s.SaveVendor(readCtx, child.ID, *child)
	require.ErrorIs(t, err, ErrVendorDenied)
	_, err = s.ProbeVendorAccount(readCtx, 1, nil)
	require.Error(t, err)
	_, _, err = s.ExportVendorProxies(readCtx, VendorProxyExportRequest{})
	require.ErrorIs(t, err, ErrVendorDenied)
	// Immutable parent/tier and parent subset enforced for both managers.
	moved := *child
	moved.ParentVendorID = &other.ID
	_, err = s.SaveVendor(root, child.ID, moved)
	require.ErrorIs(t, err, ErrVendorConflict)
	_, err = s.db.Exec("UPDATE vendors SET parent_vendor_id=$1 WHERE id=$2", other.ID, child.ID)
	require.Error(t, err)
	_, err = s.db.Exec("UPDATE vendors SET tier=1,parent_vendor_id=NULL WHERE id=$1", child.ID)
	require.Error(t, err)
	_, err = s.db.Exec("UPDATE users SET role='user' WHERE id=$1", child.UserID)
	require.Error(t, err)
	forbidden := VendorCreateInput{Email: "forbidden@example.invalid", Password: "secure-test-password", Vendor: NewVendorPolicy()}
	forbidden.Name = "forbidden"
	forbidden.Tier = 2
	forbidden.ParentVendorID = &other.ID
	forbidden.Platforms = []string{"openai"}
	forbidden.GroupIDs = []int64{1}
	_, err = s.CreateVendor(ctx, forbidden)
	require.ErrorIs(t, err, ErrVendorDenied)
	forbidden.ParentVendorID = &parent.ID
	forbidden.GroupIDs = []int64{3}
	_, err = s.CreateVendor(root, forbidden)
	require.ErrorIs(t, err, ErrVendorDenied)
	// Revocation invalidates captured child policies, but does not overwrite state.
	child.Status = StatusDisabled
	child, err = s.SaveVendor(ctx, child.ID, *child)
	require.NoError(t, err)
	_, err = s.db.Exec("INSERT INTO accounts(vendor_id) VALUES($1),($2)", child.ID, sibling.ID)
	require.NoError(t, err)
	parent.AllowDirect = false
	parent, err = s.SaveVendor(root, parent.ID, *parent)
	require.NoError(t, err)
	fresh, err := s.loadVendor(root, sibling.ID)
	require.NoError(t, err)
	require.Greater(t, fresh.PolicyVersion, sibling.PolicyVersion)
	live, err := s.CurrentVendor(hierarchyActor(sibling))
	require.NoError(t, err)
	require.False(t, live.AllowDirect)
	parent.Status = StatusDisabled
	parent, err = s.SaveVendor(root, parent.ID, *parent)
	require.NoError(t, err)
	_, err = s.CurrentVendor(hierarchyActor(sibling))
	require.ErrorIs(t, err, ErrVendorDenied)
	_, err = s.ReadVendorScope(ctx, sibling.ID)
	require.ErrorIs(t, err, ErrVendorDenied)
	parent.Status = StatusActive
	_, err = s.SaveVendor(root, parent.ID, *parent)
	require.NoError(t, err)
	_, err = s.CurrentVendor(hierarchyActor(sibling))
	require.NoError(t, err)
	_, err = s.CurrentVendor(hierarchyActor(child))
	require.ErrorIs(t, err, ErrVendorDenied)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM scheduler_outbox WHERE account_id IN(SELECT id FROM accounts WHERE vendor_id=$1)", sibling.ID).Scan(&count))
	require.Positive(t, count)
	_, err = s.db.Exec("UPDATE users SET status='disabled' WHERE id=$1", parent.UserID)
	require.NoError(t, err)
	_, err = s.CurrentVendor(hierarchyActor(sibling))
	require.ErrorIs(t, err, ErrVendorDenied)
}

func TestVendorHierarchyMigrationOnlyConvertsExistingOrdinarySuppliers(t *testing.T) {
	s, _ := vendorHierarchyPostgres(t, true)
	var role string
	var generation int64
	var balance float64
	require.NoError(t, s.db.QueryRow("SELECT role,session_generation,balance FROM users WHERE id=91").Scan(&role, &generation, &balance))
	require.Equal(t, RoleVendorLevel1, role)
	require.Equal(t, int64(5), generation)
	require.Equal(t, float64(12), balance)
	for _, id := range []int64{90, 92} {
		require.NoError(t, s.db.QueryRow("SELECT role FROM users WHERE id=$1", id).Scan(&role))
		if id == 90 {
			require.Equal(t, RoleUser, role)
		} else {
			require.Equal(t, RoleAdmin, role)
		}
	}
	var active bool
	require.NoError(t, s.db.QueryRow("SELECT status='disabled' FROM api_keys WHERE user_id=91").Scan(&active))
	require.True(t, active)
	require.NoError(t, s.db.QueryRow("SELECT status='active' FROM api_keys WHERE user_id=90").Scan(&active))
	require.True(t, active)
	require.NoError(t, s.db.QueryRow("SELECT consumed_at IS NOT NULL FROM pending_auth_sessions WHERE target_user_id=91").Scan(&active))
	require.True(t, active)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM user_allowed_groups WHERE user_id=91").Scan(&count))
	require.Zero(t, count)
	require.NoError(t, s.db.QueryRow("SELECT status='expired' FROM user_subscriptions WHERE user_id=91").Scan(&active))
	require.True(t, active)
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM vendors WHERE tier<>1 OR parent_vendor_id IS NOT NULL").Scan(&count))
	require.Zero(t, count)
	var accountLimit, proxyLimit int
	require.NoError(t, s.db.QueryRow("SELECT account_limit,proxy_limit FROM vendors WHERE user_id=91").Scan(&accountLimit, &proxyLimit))
	require.Equal(t, 100, accountLimit, "new unlimited defaults must not rewrite an existing configured cap")
	require.Equal(t, 100, proxyLimit)
	require.NoError(t, s.db.QueryRow("WITH owner AS (INSERT INTO users(email,role) VALUES('caps-defaults@example.invalid','vendor_level1') RETURNING id) INSERT INTO vendors(user_id,name) SELECT id,'New default caps' FROM owner RETURNING account_limit,proxy_limit").Scan(&accountLimit, &proxyLimit))
	require.Zero(t, accountLimit)
	require.Zero(t, proxyLimit)
	for _, column := range []string{"account_limit", "proxy_limit"} {
		_, err := s.db.Exec("UPDATE vendors SET " + column + "=0 WHERE user_id=91")
		require.NoError(t, err)
		_, err = s.db.Exec("UPDATE vendors SET " + column + "=-1 WHERE user_id=91")
		require.Error(t, err, "negative resource caps must be rejected by PostgreSQL")
	}
	migration, err := os.ReadFile("../../migrations/276_vendor_hierarchy_roles.sql")
	require.NoError(t, err)
	_, err = s.db.Exec(string(migration))
	require.NoError(t, err)
	require.NoError(t, s.db.QueryRow("SELECT session_generation FROM users WHERE id=91").Scan(&generation))
	require.Equal(t, int64(5), generation)
}

func TestVendorHierarchyParentRevocationSerializesResourceCommit(t *testing.T) {
	s, root := vendorHierarchyPostgres(t, false)
	parent := hierarchyCreate(t, s, root, "lock-parent@example.invalid", 1, nil)
	child := hierarchyCreate(t, s, hierarchyActor(parent), "lock-child@example.invalid", 2, nil)
	admitted := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.vendorAccountTx(hierarchyActor(child), child, func(*sql.Tx) error { close(admitted); <-release; return nil })
	}()
	select {
	case <-admitted:
	case e := <-done:
		t.Fatalf("resource admission failed: %v", e)
	case <-time.After(3 * time.Second):
		t.Fatal("resource admission timed out")
	}
	// A credential/status revocation must serialize behind the admitted commit.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err := s.db.ExecContext(ctx, "UPDATE users SET status='disabled' WHERE id=$1", parent.UserID)
	cancel()
	close(release)
	require.NoError(t, <-done)
	require.Error(t, err)
	_, err = s.db.Exec("UPDATE users SET status='disabled' WHERE id=$1", parent.UserID)
	require.NoError(t, err)
	called := false
	err = s.vendorAccountTx(hierarchyActor(child), child, func(*sql.Tx) error { called = true; return nil })
	require.ErrorIs(t, err, ErrVendorDenied)
	require.False(t, called)
	var status string
	require.NoError(t, s.db.QueryRow("SELECT status FROM vendors WHERE id=$1", child.ID).Scan(&status))
	require.Equal(t, StatusActive, status)
}
