package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"strings"
	"sync"
	"testing"
	"time"
)

// Reuse the existing opt-in, localhost-only disposable PostgreSQL fixture. Every
// test owns a fresh schema; no application database or migration runner is used.
func vendorAccountPostgres(t *testing.T) (*VendorService, context.Context) {
	t.Helper()
	s, _, ctx := vendorSettlementPostgres(t)
	_, err := s.db.Exec(`
CREATE SEQUENCE account_test_ids START 100;
ALTER TABLE accounts ALTER COLUMN id SET DEFAULT nextval('account_test_ids');
ALTER TABLE accounts ADD COLUMN created_by BIGINT,ADD COLUMN created_by_name TEXT NOT NULL DEFAULT '',ADD COLUMN created_by_role TEXT NOT NULL DEFAULT '',ADD COLUMN type TEXT NOT NULL DEFAULT 'apikey',ADD COLUMN credentials JSONB NOT NULL DEFAULT '{}',ADD COLUMN proxy_id BIGINT,ADD COLUMN proxy_fallback_origin_id BIGINT,ADD COLUMN concurrency INT NOT NULL DEFAULT 3,ADD COLUMN priority INT NOT NULL DEFAULT 50,ADD COLUMN rate_multiplier NUMERIC NOT NULL DEFAULT 1,ADD COLUMN schedulable BOOL NOT NULL DEFAULT TRUE,ADD COLUMN auto_pause_on_expired BOOL NOT NULL DEFAULT TRUE,ADD COLUMN vendor_paused_at TIMESTAMPTZ,ADD COLUMN vendor_notes TEXT NOT NULL DEFAULT '',ADD COLUMN error_message TEXT NOT NULL DEFAULT '',ADD COLUMN expires_at TIMESTAMPTZ,ADD COLUMN rate_limit_reset_at TIMESTAMPTZ,ADD COLUMN overload_until TIMESTAMPTZ,ADD COLUMN temp_unschedulable_until TIMESTAMPTZ,ADD COLUMN temp_unschedulable_reason TEXT NOT NULL DEFAULT '',ADD COLUMN session_window_end TIMESTAMPTZ,ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE proxies ADD COLUMN status TEXT DEFAULT 'active',ADD COLUMN expires_at TIMESTAMPTZ;
CREATE TABLE account_groups(account_id BIGINT REFERENCES accounts(id),group_id BIGINT REFERENCES groups(id),priority INT,PRIMARY KEY(account_id,group_id));
CREATE TABLE account_supply_identity_claims(id BIGSERIAL PRIMARY KEY,platform TEXT,identity_hash TEXT,canonical_account_id BIGINT REFERENCES accounts(id),vendor_id BIGINT REFERENCES vendors(id),UNIQUE(platform,identity_hash));
CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);
CREATE TABLE scheduler_outbox(id BIGSERIAL PRIMARY KEY,event_type TEXT,account_id BIGINT,payload JSONB);
CREATE TABLE scheduled_test_plans(id BIGSERIAL PRIMARY KEY,account_id BIGINT,enabled BOOL NOT NULL DEFAULT TRUE,next_run_at TIMESTAMPTZ,updated_at TIMESTAMPTZ DEFAULT NOW());
CREATE TABLE scheduled_test_results(id BIGSERIAL PRIMARY KEY,plan_id BIGINT REFERENCES scheduled_test_plans(id) ON DELETE CASCADE,result TEXT);
CREATE TABLE openai_excel_grants(account_id BIGINT PRIMARY KEY,credentials TEXT,ciphertext TEXT NOT NULL DEFAULT '',updated_at TIMESTAMPTZ DEFAULT NOW());
UPDATE vendors SET allow_direct=TRUE;
`)
	require.NoError(t, err)
	server := miniredis.RunT(t)
	s.rdb = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { s.rdb.Close() })
	s.accounts = &vendorAccountPostgresRepo{db: s.db}
	return s, authz.WithVendorScope(ctx, authz.VendorScope{VendorID: 1, PolicyVersion: 1, ActorUserID: 3})
}

type vendorAccountPostgresRepo struct {
	AccountRepository
	db *sql.DB
}

func (r *vendorAccountPostgresRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	a := &Account{}
	var cred, extra []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,vendor_id,name,platform,type,status,schedulable,vendor_paused_at,vendor_notes,proxy_id,parent_account_id,credentials,extra,concurrency,priority,created_at,updated_at,rate_limit_reset_at FROM accounts WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&a.ID, &a.VendorID, &a.Name, &a.Platform, &a.Type, &a.Status, &a.Schedulable, &a.VendorPausedAt, &a.VendorNotes, &a.ProxyID, &a.ParentAccountID, &cred, &extra, &a.Concurrency, &a.Priority, &a.CreatedAt, &a.UpdatedAt, &a.RateLimitResetAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(cred, &a.Credentials); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(extra, &a.Extra); err != nil {
		return nil, err
	}
	a.GroupIDs = []int64{1}
	a.Groups = []*Group{{ID: 1, Name: "group one", Platform: PlatformOpenAI}}
	return a, nil
}
func (r *vendorAccountPostgresRepo) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	out := []*Account{}
	for _, id := range ids {
		a, err := r.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
func vendorAccountTestVendor(id int64, limit int) *Vendor {
	return &Vendor{ID: id, PolicyVersion: 1, Status: StatusActive, Platforms: []string{PlatformOpenAI}, OpenAIAuthorizationModes: []string{VendorOpenAIModeNative}, Groups: []VendorGroup{{ID: 1, Platform: PlatformOpenAI}}, AccountLimit: limit, AllowDirect: true, CreationDefaults: NewAccountCreationDefaults()}
}
func vendorAccountTestWrite(ctx context.Context, s *VendorService, v *Vendor, identity string, target int64) (int64, error) {
	var id int64
	err := s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		var e error
		id, e = s.persistVendorCredential(ctx, tx, v, VendorAccountInput{Name: "verified account", VendorNotes: "supplier note", Platform: PlatformOpenAI, Method: "oauth", AuthorizationMode: VendorOpenAIModeNative, GroupIDs: []int64{1}, Direct: true, TargetAccountID: target}, &vendorVerifiedCredential{Identity: identity, AccountID: identity, Method: "oauth", Credentials: map[string]any{"access_token": "verified-access", "refresh_token": "verified-refresh", "chatgpt_account_id": identity}}, false)
		return e
	})
	return id, err
}

func TestVendorAccountPostgresOptionalNameUsesVerifiedMetadata(t *testing.T) {
	for _, tc := range []struct {
		label, email, expected string
	}{
		{"verified email", "trusted@example.invalid", "trusted"},
		{"no email", "", "openai-verified-identity"},
		{"long email", strings.Repeat("a", 100) + "@example.invalid", strings.Repeat("a", 100)},
	} {
		t.Run(tc.label, func(t *testing.T) {
			s, ctx := vendorAccountPostgres(t)
			v := vendorAccountTestVendor(1, 10)
			in := VendorAccountInput{Platform: PlatformOpenAI, Method: "oauth", AuthorizationMode: VendorOpenAIModeNative, GroupIDs: []int64{1}, Direct: true, VendorNotes: "source channel"}
			verified := &vendorVerifiedCredential{Identity: "verified-identity", AccountID: "verified-identity", Email: tc.email, Method: "oauth", Credentials: map[string]any{"access_token": "verified-access", "chatgpt_account_id": "verified-identity", "email": "untrusted@example.invalid", "plan_type": "plus", "billing_type": "apple_subscription", "rate_limit_tier": "default_claude_max_20x"}}
			var id int64
			require.NoError(t, s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
				var err error
				id, err = s.persistVendorCredential(ctx, tx, v, in, verified, false)
				return err
			}))
			var name, email, notes string
			require.NoError(t, s.db.QueryRow("SELECT name,extra->>'vendor_verified_email',vendor_notes FROM accounts WHERE id=$1", id).Scan(&name, &email, &notes))
			require.Equal(t, tc.expected, name)
			require.Equal(t, tc.email, email, "display truncation cannot change verified identity metadata")
			require.Equal(t, "source channel", notes)
			var plan, billing, tier string
			require.NoError(t, s.db.QueryRow("SELECT credentials->>'plan_type',credentials->>'billing_type',credentials->>'rate_limit_tier' FROM accounts WHERE id=$1", id).Scan(&plan, &billing, &tier))
			require.Equal(t, "plus", plan)
			require.Equal(t, "apple_subscription", billing)
			require.Equal(t, "default_claude_max_20x", tier)

			_, err := s.db.Exec("UPDATE accounts SET name='Custom name' WHERE id=$1", id)
			require.NoError(t, err)
			in.TargetAccountID = id
			require.NoError(t, s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
				_, err := s.persistVendorCredential(ctx, tx, v, in, verified, false)
				return err
			}))
			require.NoError(t, s.db.QueryRow("SELECT name FROM accounts WHERE id=$1", id).Scan(&name))
			require.Equal(t, "Custom name", name, "omitted name on reauthorization preserves an existing custom label")
		})
	}
}

func TestVendorAccountPostgresConcurrentLimitHasOneWinner(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	v := vendorAccountTestVendor(1, 2)
	_, err := s.db.Exec("UPDATE vendors SET account_limit=2 WHERE id=1")
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	gate := make(chan struct{})
	for _, identity := range []string{"identity-a", "identity-b"} {
		wg.Add(1)
		go func(identity string) {
			defer wg.Done()
			<-gate
			_, err := vendorAccountTestWrite(ctx, s, v, identity, 0)
			results <- err
		}(identity)
	}
	close(gate)
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	require.Equal(t, 1, success)
	var accounts, claims, outbox int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE vendor_id=1 AND parent_account_id IS NULL AND deleted_at IS NULL").Scan(&accounts))
	require.Equal(t, 2, accounts)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM account_supply_identity_claims").Scan(&claims))
	require.Equal(t, 1, claims)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&outbox))
	require.Equal(t, 1, outbox)
}
func TestVendorAccountPostgresIdentityCannotRaceAcrossOwners(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	ctx := context.Background()
	results := make(chan error, 2)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for _, v := range []*Vendor{vendorAccountTestVendor(1, 10), vendorAccountTestVendor(2, 10)} {
		wg.Add(1)
		go func(v *Vendor) {
			defer wg.Done()
			<-gate
			owner := authz.WithSubject(ctx, authz.Subject{UserID: v.ID + 2, Role: authz.VendorLevel1, SessionGeneration: 1})
			_, e := vendorAccountTestWrite(owner, s, v, "same-upstream", 0)
			results <- e
		}(v)
	}
	close(gate)
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else {
			require.ErrorIs(t, e, ErrVendorIdentityBound)
		}
	}
	require.Equal(t, 1, success)
	var count int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM account_supply_identity_claims").Scan(&count))
	require.Equal(t, 1, count)
}
func TestVendorAccountPostgresArchiveRetainsHistoryAndIsImmutable(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	v := vendorAccountTestVendor(1, 10)
	id, err := vendorAccountTestWrite(ctx, s, v, "stable-identity", 0)
	require.NoError(t, err)
	_, err = s.db.Exec("UPDATE accounts SET status='disabled',schedulable=FALSE,vendor_paused_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '1 hour',error_message='original health' WHERE id=$1", id)
	require.NoError(t, err)
	again, err := vendorAccountTestWrite(ctx, s, v, "stable-identity", id)
	require.NoError(t, err)
	require.Equal(t, id, again)
	for _, query := range []string{
		`UPDATE accounts SET proxy_id=50,proxy_fallback_origin_id=51,extra=extra||'{"private":"retained"}' WHERE id=$1`,
		`INSERT INTO usage_logs(request_id,vendor_id_snapshot,account_id,account_name_snapshot,platform_snapshot,created_at,model,input_tokens,total_cost) VALUES('history',1,$1,'retained','openai',NOW(),'test',7,2.5)`,
		`INSERT INTO accounts(id,vendor_id,name,platform,status,type,parent_account_id,credentials,extra) VALUES(1001,1,'shadow','openai','active','oauth',$1,'{"access_token":"retained-shadow"}','{"private":"retained-shadow"}')`,
		`INSERT INTO openai_excel_grants(account_id,credentials) VALUES($1,'retained-grant'),(1001,'retained-shadow-grant')`,
		`INSERT INTO scheduled_test_plans(account_id,next_run_at) VALUES($1,NOW()),(1001,NOW())`,
	} {
		_, err = s.db.Exec(query, id)
		require.NoError(t, err)
	}
	_, err = s.db.Exec("INSERT INTO account_groups(account_id,group_id) VALUES(1001,1); INSERT INTO scheduled_test_results(plan_id,result) SELECT id,'retained-result' FROM scheduled_test_plans")
	require.NoError(t, err)
	snapshot := func() string {
		t.Helper()
		var raw string
		require.NoError(t, s.db.QueryRow(`SELECT jsonb_agg((to_jsonb(a)-ARRAY['updated_at','schedulable','vendor_paused_at','extra'])||jsonb_build_object('extra',extra-'vendor_archived_at') ORDER BY id)::text FROM accounts a WHERE id=$1 OR parent_account_id=$1`, id).Scan(&raw))
		return raw
	}
	before := snapshot()
	_, err = s.db.Exec("ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_archive CHECK(FALSE) NOT VALID")
	require.NoError(t, err)
	require.Error(t, s.DeleteVendorAccount(ctx, id))
	var archived, enabled bool
	require.NoError(t, s.db.QueryRow("SELECT extra ? 'vendor_archived_at' FROM accounts WHERE id=$1", id).Scan(&archived))
	require.False(t, archived, "outbox failure must roll back archive")
	require.NoError(t, s.db.QueryRow("SELECT bool_and(enabled) FROM scheduled_test_plans").Scan(&enabled))
	require.True(t, enabled)
	_, err = s.db.Exec("ALTER TABLE scheduler_outbox DROP CONSTRAINT reject_archive")
	require.NoError(t, err)
	require.NoError(t, s.DeleteVendorAccount(ctx, id))
	require.Equal(t, before, snapshot(), "provider health, credentials, extra, proxy and parent links are retained")
	var retained int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE (id=$1 OR parent_account_id=$1) AND deleted_at IS NULL AND extra ? 'vendor_archived_at' AND vendor_paused_at IS NOT NULL AND NOT schedulable", id).Scan(&retained))
	require.Equal(t, 2, retained)
	for _, table := range []string{"openai_excel_grants", "scheduled_test_plans", "scheduled_test_results", "account_groups"} {
		require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM "+table).Scan(&retained))
		require.Equal(t, 2, retained, table)
	}
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM scheduled_test_plans WHERE enabled OR next_run_at IS NOT NULL").Scan(&retained))
	require.Zero(t, retained)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE account_id=$1 AND vendor_id_snapshot=1", id).Scan(&retained))
	require.Equal(t, 1, retained)
	var claimed int64
	require.NoError(t, s.db.QueryRow("SELECT canonical_account_id FROM account_supply_identity_claims WHERE vendor_id=1").Scan(&claimed))
	require.Equal(t, id, claimed)
	detail, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.True(t, detail.Archived)
	require.NotNil(t, detail.ArchivedAt)
	require.Equal(t, StatusDisabled, detail.Status)
	require.False(t, detail.Schedulable)
	require.False(t, detail.CanRefresh)
	require.Equal(t, int64(1), detail.AllTimeUsage.Requests)
	usage, err := s.VendorAccountUsage(ctx, VendorUsageFilter{AccountID: id}, false)
	require.NoError(t, err)
	require.True(t, usage.AccountArchived)
	require.Equal(t, int64(1), usage.Summary.Requests)
	list, err := s.ListVendorAccounts(ctx, VendorAccountListInput{PageSize: 100})
	require.NoError(t, err)
	require.Len(t, list.Items, 2, "archive stays in the default account pool")
	list, err = s.ListVendorAccounts(ctx, VendorAccountListInput{Status: "archived"})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, id, list.Items[0].ID)
	var updatedAt time.Time
	var outbox int
	require.NoError(t, s.db.QueryRow("SELECT updated_at FROM accounts WHERE id=$1", id).Scan(&updatedAt))
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&outbox))
	require.NoError(t, s.DeleteVendorAccount(ctx, id))
	replayed, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.Equal(t, detail.ArchivedAt, replayed.ArchivedAt)
	require.True(t, updatedAt.Equal(replayed.UpdatedAt))
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&retained))
	require.Equal(t, outbox, retained)
	name := "must not change"
	_, err = s.UpdateVendorAccount(ctx, id, VendorAccountUpdate{Name: &name})
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	for _, paused := range []bool{false, true} {
		_, err = s.SetVendorAccountPaused(ctx, id, paused)
		require.ErrorIs(t, err, ErrVendorAccountArchived)
	}
	for _, operation := range []func(context.Context, int64) (*VendorAccountDTO, error){s.RefreshVendorAccount, s.RefreshVendorAccountUsage, s.SyncVendorAccount} {
		_, err = operation(ctx, id)
		require.ErrorIs(t, err, ErrVendorAccountArchived)
	}
	_, err = s.ProbeVendorAccount(ctx, id, &AccountTestService{})
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	input := VendorAccountInput{Name: "archived", Platform: PlatformOpenAI, Method: "oauth", AuthorizationMode: VendorOpenAIModeNative, GroupIDs: []int64{1}, Direct: true, TargetAccountID: id}
	_, err = s.StartVendorOAuth(ctx, input)
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	_, err = s.CreateVendorAccount(ctx, input)
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	_, err = vendorAccountTestWrite(ctx, s, v, "stable-identity", id)
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	_, err = vendorAccountTestWrite(ctx, s, v, "stable-identity", 0)
	require.ErrorIs(t, err, ErrVendorAccountArchived, "reimport cannot replace the original identity claim")
	_, err = vendorAccountTestWrite(authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1}), s, vendorAccountTestVendor(2, 10), "stable-identity", 0)
	require.ErrorIs(t, err, ErrVendorIdentityBound)
	_, err = s.ExportVendorAccounts(ctx, []int64{id})
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	_, err = s.ExportVendorAccounts(ctx, []int64{21})
	require.ErrorIs(t, err, ErrVendorAccountNotFound)
	other := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1})
	other = authz.WithVendorScope(other, authz.VendorScope{VendorID: 2, PolicyVersion: 1, ActorUserID: 4})
	require.ErrorIs(t, s.DeleteVendorAccount(other, id), ErrVendorAccountNotFound)
	require.Equal(t, before, snapshot())
}

func TestVendorAccountPostgresArchiveDuringNetworkDiscardsResult(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	v := vendorAccountTestVendor(1, 10)
	id, err := vendorAccountTestWrite(ctx, s, v, "inflight-archive", 0)
	require.NoError(t, err)
	a, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	committed := false
	err = s.vendorAccountNetwork(ctx, v, nil, true, a, nil, func(context.Context, string) error {
		return s.DeleteVendorAccount(ctx, id)
	}, func(*sql.Tx) error { committed = true; return nil })
	require.ErrorIs(t, err, ErrVendorAccountArchived)
	require.False(t, committed)
	// The marker remains authoritative if native pause/health flags are stale.
	_, err = s.db.Exec("UPDATE accounts SET schedulable=TRUE,vendor_paused_at=NULL,status='active' WHERE id=$1", id)
	require.NoError(t, err)
	list, err := s.ListVendorAccounts(ctx, VendorAccountListInput{Status: "active"})
	require.NoError(t, err)
	require.Empty(t, list.Items)
	detail, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.False(t, detail.Schedulable)
}

func TestVendorAccountPostgresPolicyChangeDuringNetworkDiscardsResult(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	v := vendorAccountTestVendor(1, 10)
	committed := false
	err := s.vendorAccountNetwork(ctx, v, nil, true, nil, nil, func(context.Context, string) error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, e := s.db.ExecContext(ctx, "UPDATE vendors SET policy_version=policy_version+1 WHERE id=1")
		return e
	}, func(*sql.Tx) error { committed = true; return nil })
	require.ErrorIs(t, err, ErrVendorAccountConflict)
	require.False(t, committed)
}

func TestVendorAccountPostgresAllSuppliersRequiresLiveSuperAdmin(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	admin = authz.WithVendorScope(admin, authz.VendorScope{VendorID: 0, ActorUserID: 9, Admin: true})
	result, err := s.ListVendorAccounts(admin, VendorAccountListInput{Page: 1, PageSize: 100})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(result.Items), 2)
	seen := map[int64]bool{}
	for _, item := range result.Items {
		require.Positive(t, item.VendorID)
		require.NotEmpty(t, item.VendorName)
		seen[item.VendorID] = true
	}
	require.True(t, seen[1])
	require.True(t, seen[2])
	_, err = s.db.Exec("UPDATE users SET role='admin' WHERE id=9")
	require.NoError(t, err)
	_, err = s.ListVendorAccounts(admin, VendorAccountListInput{})
	require.Error(t, err)
}
func TestVendorNativeIdentityClaimRejectsForeignOwnerStaleProofAndForgedLegacyMetadata(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	ctx := context.Background()
	guard := s.NativeSupplyIdentityGuard()
	makeProof := func(a *Account, identity string) *VerifiedSupplyIdentity {
		hash, err := s.vendorIdentityHash(a.Platform, identity)
		require.NoError(t, err)
		digest, err := vendorSupplyCredentialDigest(a.Credentials)
		require.NoError(t, err)
		return &VerifiedSupplyIdentity{platform: a.Platform, hash: hash, credentialsDigest: digest, accountID: a.ID, vendorID: a.VendorID}
	}
	_, err := s.db.Exec("INSERT INTO accounts(id,name,platform,type,credentials) VALUES(800,'native','openai','oauth','{\"access_token\":\"trusted\"}'),(801,'legacy duplicate','openai','oauth','{\"access_token\":\"trusted-other-session\"}')")
	require.NoError(t, err)
	a := &Account{ID: 800, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "trusted"}}
	proof := makeProof(a, "canonical-native")
	tx, err := s.db.Begin()
	require.NoError(t, err)
	require.NoError(t, guard.Claim(ctx, tx, a, proof))
	require.NoError(t, tx.Commit())
	b := &Account{ID: 801, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "trusted-other-session"}}
	otherProof := makeProof(b, "canonical-native")
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, guard.Claim(ctx, tx, b, otherProof), ErrVendorIdentityBound)
	require.NoError(t, tx.Rollback())
	otherProof.baseline = true
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.NoError(t, guard.Claim(ctx, tx, b, otherProof))
	require.NoError(t, tx.Commit())
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.NoError(t, s.vendorLegacyIdentityBaseline(ctx, tx, PlatformOpenAI, 1))
	require.NoError(t, tx.Rollback())
	_, err = s.db.Exec("UPDATE accounts SET extra=extra-'vendor_identity_attestation' WHERE id=801")
	require.NoError(t, err)
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, s.vendorLegacyIdentityBaseline(ctx, tx, PlatformOpenAI, 1), ErrVendorIdentityBound)
	require.NoError(t, tx.Rollback())
	_, err = s.db.Exec("UPDATE accounts SET credentials='{\"access_token\":\"changed\"}' WHERE id=800")
	require.NoError(t, err)
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, guard.Claim(ctx, tx, a, proof), ErrVendorAccountConflict)
	require.NoError(t, tx.Rollback())
	owner := int64(1)
	_, err = s.db.Exec("INSERT INTO accounts(id,vendor_id,name,platform,type,credentials) VALUES(802,1,'attempt takeover','openai','oauth','{\"access_token\":\"trusted\"}')")
	require.NoError(t, err)
	c := &Account{ID: 802, VendorID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "trusted"}}
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, guard.Claim(ctx, tx, c, makeProof(c, "canonical-native")), ErrVendorIdentityBound)
	require.NoError(t, tx.Rollback())
}

func TestVendorAccountPostgresTreeLimitSerializesOwners(t *testing.T) {
	s, parentCtx := vendorAccountPostgres(t)
	_, err := s.db.Exec(`UPDATE vendors SET account_limit=3 WHERE id=1;
 UPDATE vendors SET tier=2,parent_vendor_id=1,account_limit=0 WHERE id=2;
 UPDATE users SET role='vendor_level2' WHERE id=4;`)
	require.NoError(t, err)
	childCtx := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel2, SessionGeneration: 1})
	childCtx = authz.WithVendorScope(childCtx, authz.VendorScope{VendorID: 2, PolicyVersion: 1, ActorUserID: 4})
	gate := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	// Creation, import and OAuth completion share this credential writer.
	// Different owners must compete for the same final parent slot.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gate
			ctx, v := parentCtx, vendorAccountTestVendor(1, 3)
			if i%2 == 1 {
				ctx, v = childCtx, vendorAccountTestVendor(2, 10)
			}
			_, e := vendorAccountTestWrite(ctx, s, v, fmt.Sprintf("tree-%d", i), 0)
			results <- e
		}(i)
	}
	close(gate)
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else {
			require.Contains(t, e.Error(), "limit")
		}
	}
	require.Equal(t, 1, successes, "the root limit includes its own and all direct child accounts")
	var count, claims int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM accounts WHERE vendor_id IN(1,2) AND deleted_at IS NULL AND parent_account_id IS NULL").Scan(&count))
	require.Equal(t, 3, count)
	policy, err := s.loadVendor(parentCtx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(3), policy.AccountTreeCount, "L1 capacity includes all direct children")
	childPolicy, err := s.loadVendor(parentCtx, 2)
	require.NoError(t, err)
	require.Equal(t, int64(3), policy.AccountCount+childPolicy.AccountCount, "direct ownership totals do not count child accounts twice")
	require.Equal(t, childPolicy.AccountCount, childPolicy.AccountTreeCount)

	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM account_supply_identity_claims").Scan(&claims))
	require.Equal(t, 1, claims, "failed capacity checks roll back identity claims")
	var owner, id int64
	var identity string
	require.NoError(t, s.db.QueryRow("SELECT vendor_id,canonical_account_id FROM account_supply_identity_claims").Scan(&owner, &id))
	require.NoError(t, s.db.QueryRow("SELECT credentials->>'chatgpt_account_id' FROM accounts WHERE id=$1", id).Scan(&identity))
	ctx, v := parentCtx, vendorAccountTestVendor(1, 3)
	if owner == 2 {
		ctx, v = childCtx, vendorAccountTestVendor(2, 10)
	}
	_, err = vendorAccountTestWrite(ctx, s, v, identity, id)
	require.NoError(t, err, "reauthorizing an existing identity consumes no new capacity")
	_, err = vendorAccountTestWrite(childCtx, s, vendorAccountTestVendor(2, 0), "parent-cap-blocks-unlimited-child", 0)
	require.Error(t, err, "unlimited child must still obey a full finite parent cap")
	_, err = s.db.Exec("UPDATE vendors SET account_limit=0 WHERE id=1; UPDATE vendors SET account_limit=(SELECT count(*) FROM accounts WHERE vendor_id=2 AND deleted_at IS NULL AND parent_account_id IS NULL) WHERE id=2")
	require.NoError(t, err)
	_, err = vendorAccountTestWrite(parentCtx, s, vendorAccountTestVendor(1, 0), "unlimited-parent", 0)
	require.NoError(t, err)
	_, err = vendorAccountTestWrite(childCtx, s, vendorAccountTestVendor(2, 1), "finite-child-full", 0)
	require.Error(t, err, "finite child cap remains effective under an unlimited parent")
	_, err = s.db.Exec("UPDATE vendors SET account_limit=0 WHERE id=2")
	require.NoError(t, err)
	_, err = vendorAccountTestWrite(childCtx, s, vendorAccountTestVendor(2, 0), "both-unlimited", 0)
	require.NoError(t, err)
}

func TestClaudeAuthorizationPostgresCreateReauthorizeAndOwnership(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	_, err := s.db.Exec(`UPDATE groups SET platform='anthropic'; UPDATE vendors SET platforms='["anthropic"]'::jsonb`)
	require.NoError(t, err)
	vendor := vendorAccountTestVendor(1, 10)
	vendor.Platforms = []string{PlatformAnthropic}
	vendor.Groups = []VendorGroup{{ID: 1, Platform: PlatformAnthropic}}
	write := func(v *Vendor, target int64, token string) (int64, error) {
		verified, e := s.vendorVerifyToken(ctx, VendorAccountInput{Platform: PlatformAnthropic, Method: "setup-token-manual", Token: token}, "invalid://no-network")
		if e != nil {
			return 0, e
		}
		owner := authz.WithSubject(context.Background(), authz.Subject{UserID: v.ID + 2, Role: authz.VendorLevel1, SessionGeneration: 1})
		var id int64
		e = s.vendorAccountTx(owner, v, func(tx *sql.Tx) error {
			var failure error
			id, failure = s.persistVendorCredential(owner, tx, v, VendorAccountInput{Platform: PlatformAnthropic, Method: "setup-token-manual", GroupIDs: []int64{1}, Direct: true, TargetAccountID: target}, verified, false)
			return failure
		})
		return id, e
	}
	id, err := write(vendor, 0, "sk-ant-oat01-first")
	require.NoError(t, err)
	_, err = write(vendor, id, "sk-ant-oat01-second")
	require.NoError(t, err, "rotating a manual token keeps the same local account")
	var raw []byte
	require.NoError(t, s.db.QueryRow("SELECT credentials FROM accounts WHERE id=$1", id).Scan(&raw))
	var credentials map[string]any
	require.NoError(t, json.Unmarshal(raw, &credentials))
	require.Equal(t, "sk-ant-oat01-second", credentials["access_token"])
	require.NotContains(t, credentials, "refresh_token")
	require.NotContains(t, credentials, "expires_at")
	require.NotContains(t, credentials, "account_uuid")
	_, err = write(vendor, 0, "sk-ant-oat01-second")
	require.ErrorIs(t, err, ErrVendorIdentityBound)
	other := *vendor
	other.ID = 2
	_, err = write(&other, 0, "sk-ant-oat01-first")
	require.ErrorIs(t, err, ErrVendorIdentityBound, "old tokens remain reserved to the original owner")
	// Acquiring full OAuth identity later must not be blocked by credential-only claims.
	verified := vendorClaudeCredentials(&TokenInfo{AccessToken: "sk-ant-oat01-full", AccountUUID: "account", OrgUUID: "org", RefreshToken: "new-refresh", ExpiresAt: 2000000000}, "oauth")
	err = s.vendorAccountTx(ctx, vendor, func(tx *sql.Tx) error {
		_, e := s.persistVendorCredential(ctx, tx, vendor, VendorAccountInput{Platform: PlatformAnthropic, Method: "oauth", GroupIDs: []int64{1}, Direct: true, TargetAccountID: id}, verified, false)
		return e
	})
	require.NoError(t, err)
	// A native/platform account cannot claim a supplier's token.
	native := &Account{ID: 800, Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Credentials: map[string]any{"access_token": "sk-ant-oat01-first"}}
	raw, err = json.Marshal(native.Credentials)
	require.NoError(t, err)
	_, err = s.db.Exec("INSERT INTO accounts(id,name,platform,type,credentials) VALUES($1,'native',$2,$3,$4)", native.ID, native.Platform, native.Type, string(raw))
	require.NoError(t, err)
	guard := &nativeSupplyIdentityGuard{s}
	proof, err := guard.verify(context.Background(), native, true)
	require.NoError(t, err)
	tx, err := s.db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, guard.Claim(context.Background(), tx, native, proof), ErrVendorIdentityBound)
	require.NoError(t, tx.Rollback())
}

func TestClaudeAuthorizationPostgresConcurrentMixedMethodsReserveSameToken(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	const token = "sk-ant-oat01-same-token"
	guard := &nativeSupplyIdentityGuard{s}
	start := make(chan struct{})
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-start
			a := &Account{ID: int64(850 + i), Platform: PlatformAnthropic, Type: AccountTypeSetupToken, Credentials: map[string]any{"access_token": token}}
			digest, e := vendorSupplyCredentialDigest(a.Credentials)
			if e != nil {
				result <- e
				return
			}
			identity := claudeCredentialIdentityPrefix + claudeTokenDigest(token)
			if i == 1 {
				identity = "verified-account\x00verified-org"
			}
			hash, e := s.vendorIdentityHash(a.Platform, identity)
			if e != nil {
				result <- e
				return
			}
			tx, e := s.db.Begin()
			if e != nil {
				result <- e
				return
			}
			defer tx.Rollback()
			raw, _ := json.Marshal(a.Credentials)
			_, e = tx.Exec("INSERT INTO accounts(id,name,platform,type,credentials) VALUES($1,'native',$2,$3,$4)", a.ID, a.Platform, a.Type, string(raw))
			if e == nil {
				e = guard.Claim(context.Background(), tx, a, &VerifiedSupplyIdentity{platform: a.Platform, hash: hash, credentialsDigest: digest, accountID: a.ID})
			}
			if e == nil {
				e = tx.Commit()
			}
			result <- e
		}(i)
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-result
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrVendorIdentityBound)
		}
	}
	require.Equal(t, 1, successes)
}

func TestVendorAccountQuotaMatchesNativeBatchAndOverview(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	at := time.Now().UTC()
	a := &Account{ID: 11, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "local-fixture"}}
	fields := map[string]UsageObservation{}
	putUsageObservation(fields, "five_hour.utilization", 100.0, "upstream_api", &at)
	putUsageObservation(fields, "five_hour.resets_at", at.Add(time.Hour).Format(time.RFC3339), "upstream_api", &at)
	extra := persistableAnthropicObservations(fields, anthropicCredentialIdentity(a))
	raw, err := json.Marshal(extra)
	require.NoError(t, err)
	_, err = s.db.Exec("UPDATE accounts SET platform='anthropic',type='oauth',credentials='{\"access_token\":\"local-fixture\"}',extra=$1::jsonb WHERE id=11", string(raw))
	require.NoError(t, err)
	s.accountUsage = &AccountUsageService{accountRepo: s.accounts}
	native, _, err := s.accountUsage.GetUsageBatch(ctx, []int64{11}, false)
	require.NoError(t, err)
	row := &VendorAccountDTO{ID: 11, Platform: PlatformAnthropic}
	s.populateVendorUsageSnapshots(ctx, []*VendorAccountDTO{row})
	require.Equal(t, vendorQuotaWindow(native[11].FiveHour), row.Quota.FiveHour)
	require.Equal(t, 100.0, row.Quota.FiveHour.Utilization)
	overview := &VendorAccountOverview{}
	require.NoError(t, s.vendorAccountPoolOverview(ctx, &Vendor{ID: 1}, overview))
	require.Equal(t, int64(1), overview.QuotaDistribution["five_hour"].Full)
}

func TestVendorAccountPostgresCompositeBindingAndOverview(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	_, err := s.db.Exec(`INSERT INTO groups VALUES(3,'composite','composite','active',NULL);
INSERT INTO vendor_groups VALUES(1,3,FALSE);
UPDATE accounts SET status='disabled' WHERE id=11;`)
	require.NoError(t, err)
	v := vendorAccountTestVendor(1, 10)
	v.Groups = append(v.Groups, VendorGroup{ID: 3, Name: "composite", Platform: PlatformComposite})
	id, err := vendorAccountTestWrite(ctx, s, v, "composite-account-identity", 0)
	require.NoError(t, err)
	require.NoError(t, s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if err := s.vendorLockAccount(ctx, tx, v, id); err != nil {
			return err
		}
		if err := s.vendorBindGroups(ctx, tx, v, id, PlatformOpenAI, []int64{3}); err != nil {
			return err
		}
		return vendorAccountOutbox(ctx, tx, id, []int64{3})
	}))
	var bindings int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=3", id).Scan(&bindings))
	require.Equal(t, 1, bindings)
	var eventGroups string
	require.NoError(t, s.db.QueryRow("SELECT payload->>'group_ids' FROM scheduler_outbox WHERE account_id=$1 ORDER BY id DESC LIMIT 1", id).Scan(&eventGroups))
	require.JSONEq(t, "[3]", eventGroups)
	overview := func() VendorAccountOverview {
		t.Helper()
		var out VendorAccountOverview
		require.NoError(t, s.vendorAccountPoolOverview(ctx, v, &out))
		return out
	}
	require.EqualValues(t, 1, overview().Available)
	_, err = s.db.Exec("UPDATE accounts SET vendor_paused_at=NOW() WHERE id=$1", id)
	require.NoError(t, err)
	require.EqualValues(t, 0, overview().Available)
	require.EqualValues(t, 1, overview().Paused)
	_, err = s.db.Exec("UPDATE accounts SET vendor_paused_at=NULL WHERE id=$1", id)
	require.NoError(t, err)
	_, err = s.db.Exec("DELETE FROM vendor_groups WHERE vendor_id=1 AND group_id=3")
	require.NoError(t, err)
	require.EqualValues(t, 0, overview().Available, "a Composite binding is not itself a supplier grant")
	_, err = s.db.Exec("INSERT INTO vendor_groups VALUES(1,3,FALSE); UPDATE users SET role='admin' WHERE id=3")
	require.NoError(t, err)
	require.EqualValues(t, 0, overview().Available)
	require.Error(t, s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error { return s.vendorBindGroups(ctx, tx, v, id, PlatformOpenAI, []int64{1}) }), "stale role cannot write through an old supplier view")
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM account_groups WHERE account_id=$1 AND group_id=3", id).Scan(&bindings))
	require.Equal(t, 1, bindings)
}
