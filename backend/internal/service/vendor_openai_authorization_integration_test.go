package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// These regressions exercise real local PostgreSQL transactions and Redis locks.
// Persistence starts AFTER trusted upstream verification; fixture credentials do
// not demonstrate real OpenAI OAuth exchange, identity attestation or routing.
func vendorOpenAIAuthorizationPostgres(t *testing.T) (*VendorService, context.Context, *Vendor) {
	t.Helper()
	s, ctx := vendorAccountPostgres(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "278_vendor_openai_authorization_modes.sql"))
	require.NoError(t, err)
	_, err = s.db.Exec(string(migration))
	require.NoError(t, err)
	_, err = s.db.Exec(string(migration))
	require.NoError(t, err, "authorization migration must be re-entrant")
	_, err = s.db.Exec(`UPDATE vendors SET openai_authorization_modes='["native","bps"]'; ALTER TABLE vendor_groups ADD PRIMARY KEY(vendor_id,group_id)`)
	require.NoError(t, err)
	s.openaiOAuth = NewOpenAIOAuthService(nil, nil)
	s.openaiOAuth.excelEncryptor = &liveAttestationAES{key: [32]byte{1}}
	startSessionOwnerTest(t, s.openaiOAuth)
	v, err := s.CurrentVendor(ctx)
	require.NoError(t, err)
	return s, ctx, v
}

func vendorOpenAITestCredential(mode, identity string) *vendorVerifiedCredential {
	client := openai.ClientID
	if mode == VendorOpenAIModeBPS {
		client = openai.ExcelClientID
	}
	return &vendorVerifiedCredential{
		Identity: identity, AccountID: identity, Email: "owner@example.invalid", Method: "oauth",
		Credentials: map[string]any{
			"access_token": mode + "-fixture-access", "refresh_token": mode + "-fixture-refresh", "id_token": mode + "-fixture-id-token",
			"chatgpt_account_id": identity, "chatgpt_user_id": "verified-user", "email": "owner@example.invalid",
			"client_id": client, "expires_at": "2099-01-01T00:00:00Z",
		},
	}
}

func vendorOpenAITestInput(mode, action string, id int64) VendorAccountInput {
	return VendorAccountInput{Name: "verified OpenAI", Platform: PlatformOpenAI, Method: "oauth", GroupIDs: []int64{1}, Direct: true,
		AuthorizationMode: mode, AuthorizationAction: action, TargetAccountID: id}
}

func vendorOpenAITestPersist(ctx context.Context, s *VendorService, v *Vendor, in VendorAccountInput, credential *vendorVerifiedCredential) (int64, error) {
	if err := normalizeVendorAccountInput(&in); err != nil {
		return 0, err
	}
	var id int64
	err := s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		var err error
		id, err = s.persistVendorCredential(ctx, tx, v, in, credential, false)
		return err
	})
	return id, err
}

func vendorOpenAITestGrant(t *testing.T, s *VendorService, id int64) (string, map[string]any) {
	t.Helper()
	var ciphertext string
	require.NoError(t, s.db.QueryRow("SELECT ciphertext FROM openai_excel_grants WHERE account_id=$1", id).Scan(&ciphertext))
	plaintext, err := s.openaiOAuth.excelEncryptor.Decrypt(ciphertext)
	require.NoError(t, err)
	var credentials map[string]any
	require.NoError(t, json.Unmarshal([]byte(plaintext), &credentials))
	return ciphertext, credentials
}

func vendorOpenAITestSnapshot(t *testing.T, s *VendorService, id int64) string {
	t.Helper()
	var snapshot string
	require.NoError(t, s.db.QueryRow(`SELECT jsonb_build_object(
 'account',(SELECT to_jsonb(a) FROM accounts a WHERE id=$1),
 'grant',(SELECT to_jsonb(g) FROM openai_excel_grants g WHERE account_id=$1),
 'groups',(SELECT jsonb_agg(to_jsonb(g) ORDER BY group_id) FROM account_groups g WHERE account_id=$1),
 'claims',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM account_supply_identity_claims c WHERE canonical_account_id=$1),
 'usage',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM usage_logs u WHERE account_id=$1),
 'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM scheduler_outbox o WHERE account_id=$1))::text`, id).Scan(&snapshot))
	return snapshot
}

func vendorOpenAITestUsage(t *testing.T, s *VendorService, id, owner int64) {
	t.Helper()
	_, err := s.db.Exec(`INSERT INTO usage_logs(request_id,vendor_id_snapshot,account_id,account_name_snapshot,platform_snapshot,created_at,model,input_tokens,total_cost) VALUES('authorization-history',$2,$1,'original account','openai',NOW(),'test',7,2.5)`, id, owner)
	require.NoError(t, err)
}

func vendorOpenAITestPublicDTO(t *testing.T, dto *VendorAccountDTO) {
	t.Helper()
	data, err := json.Marshal(dto)
	require.NoError(t, err)
	for _, secret := range []string{"fixture-access", "fixture-refresh", "fixture-id-token", "access_token", "refresh_token", "ciphertext"} {
		require.NotContains(t, string(data), secret, "authorization DTO must remain secret-free")
	}
}

func TestVendorOpenAIDuplicateAttachmentDoesNotRotateExistingTokens(t *testing.T) {
	for _, mode := range []string{VendorOpenAIModeNative, VendorOpenAIModeBPS} {
		t.Run(mode, func(t *testing.T) {
			s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
			id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput(VendorOpenAIModeNative, "create", 0), vendorOpenAITestCredential(VendorOpenAIModeNative, "duplicate-attach"))
			require.NoError(t, err)
			if mode == VendorOpenAIModeBPS {
				_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput(mode, "attach", id), vendorOpenAITestCredential(mode, "duplicate-attach"))
				require.NoError(t, err)
			}
			before := vendorOpenAITestSnapshot(t, s, id)
			in := vendorOpenAITestInput(mode, "attach", id)
			in.Method, in.Token = "refresh_token", mode+"-fixture-refresh"
			// No OAuth client is configured: a provider exchange would panic. A
			// duplicate must stop before that call and preserve both grants.
			_, err = s.CreateVendorAccount(ctx, in)
			require.ErrorIs(t, err, ErrVendorAccountConflict)
			require.Equal(t, before, vendorOpenAITestSnapshot(t, s, id))
		})
	}
}

func TestVendorOpenAIAuthorizationPostgresDualGrantKeepsOneAccount(t *testing.T) {
	s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
	// One existing fixture root plus this new root fills the supplier's capacity.
	_, err := s.db.Exec("UPDATE vendors SET account_limit=2 WHERE id=1")
	require.NoError(t, err)
	v.AccountLimit = 2
	id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "create", 0), vendorOpenAITestCredential("native", "dual-identity"))
	require.NoError(t, err)
	vendorOpenAITestUsage(t, s, id, v.ID)
	_, err = s.db.Exec("UPDATE accounts SET status='disabled',schedulable=false,vendor_paused_at=NOW(),rate_limit_reset_at=NOW()+INTERVAL '1 hour',error_message='existing health' WHERE id=$1", id)
	require.NoError(t, err)
	before, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	again, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), vendorOpenAITestCredential("bps", "dual-identity"))
	require.NoError(t, err, "a second grant does not consume another account slot")
	require.Equal(t, id, again)
	after, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, before.Credentials, after.Credentials)
	require.Equal(t, before.VendorPausedAt, after.VendorPausedAt)
	require.Equal(t, before.RateLimitResetAt, after.RateLimitResetAt)
	require.Equal(t, before.Status, after.Status)
	require.False(t, after.Schedulable)
	ciphertext, grant := vendorOpenAITestGrant(t, s, id)
	require.NotContains(t, ciphertext, "fixture-access")
	require.Equal(t, "bps-fixture-access", grant["access_token"])
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM accounts WHERE vendor_id=1 AND deleted_at IS NULL AND parent_account_id IS NULL").Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM account_supply_identity_claims WHERE canonical_account_id=$1 AND vendor_id=1", id).Scan(&count))
	require.Equal(t, 1, count)
	dto, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"native", "bps"}, dto.AuthorizationModes)
	require.Equal(t, int64(1), dto.AllTimeUsage.Requests)
	vendorOpenAITestPublicDTO(t, dto)
	snapshot := vendorOpenAITestSnapshot(t, s, id)
	_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), vendorOpenAITestCredential("bps", "dual-identity"))
	require.ErrorIs(t, err, ErrVendorAccountConflict, "attach must not silently replace an existing grant")
	require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id))
}

func TestVendorOpenAIAuthorizationPostgresModesCannotSplitOwnership(t *testing.T) {
	s, ctx, first := vendorOpenAIAuthorizationPostgres(t)
	otherCtx := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel1, SessionGeneration: 1})
	second, err := s.CurrentVendor(otherCtx)
	require.NoError(t, err)
	type result struct {
		id  int64
		err error
	}
	results := make(chan result, 2)
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i, v := range []*Vendor{first, second} {
		wg.Add(1)
		go func(i int, v *Vendor) {
			defer wg.Done()
			<-gate
			actor := []context.Context{ctx, otherCtx}[i]
			mode := []string{"native", "bps"}[i]
			id, err := vendorOpenAITestPersist(actor, s, v, vendorOpenAITestInput(mode, "create", 0), vendorOpenAITestCredential(mode, "same-upstream"))
			results <- result{id, err}
		}(i, v)
	}
	close(gate)
	wg.Wait()
	close(results)
	var successes int
	for r := range results {
		if r.err == nil {
			successes++
			require.Positive(t, r.id)
		} else {
			require.ErrorIs(t, r.err, ErrVendorIdentityBound)
		}
	}
	require.Equal(t, 1, successes, "authorization mode must not partition upstream identity ownership")
	var accounts, claims int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM accounts WHERE credentials->>'chatgpt_account_id'='same-upstream'").Scan(&accounts))
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM account_supply_identity_claims").Scan(&claims))
	require.Equal(t, 1, accounts)
	require.Equal(t, 1, claims)
}

func TestVendorOpenAIAuthorizationPostgresAttachRejectsDifferentIdentity(t *testing.T) {
	s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
	native := vendorOpenAITestCredential("native", "verified-account")
	// Existing native supplier rows may have their verified email only in
	// private account metadata, rather than in the credential map.
	delete(native.Credentials, "email")
	id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "create", 0), native)
	require.NoError(t, err)
	snapshot := vendorOpenAITestSnapshot(t, s, id)
	for _, field := range []string{"workspace", "chatgpt_user_id", "email"} {
		t.Run(field, func(t *testing.T) {
			credential := vendorOpenAITestCredential("bps", "verified-account")
			if field == "workspace" {
				credential = vendorOpenAITestCredential("bps", "other-workspace")
			} else {
				credential.Credentials[field] = "other-owner"
			}
			_, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), credential)
			require.ErrorIs(t, err, ErrVendorIdentityBound)
			require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id))
		})
	}
}

func TestVendorOpenAIAuthorizationPostgresPrimaryMigrationAtomic(t *testing.T) {
	s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
	id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "create", 0), vendorOpenAITestCredential("bps", "bps-primary"))
	require.NoError(t, err)
	// Switching the policy to native-only must still permit a native grant on
	// this identity. Retaining the old encrypted BPS grant is not BPS admission.
	_, err = s.db.Exec(`UPDATE vendors SET openai_authorization_modes='["native"]' WHERE id=$1`, v.ID)
	require.NoError(t, err)
	v, err = s.CurrentVendor(ctx)
	require.NoError(t, err)
	vendorOpenAITestUsage(t, s, id, v.ID)
	before, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	snapshot := vendorOpenAITestSnapshot(t, s, id)
	// Fail after credential migration, group binding and identity-claim writes.
	_, err = s.db.Exec("ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_authorization CHECK(FALSE) NOT VALID")
	require.NoError(t, err)
	_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "attach", id), vendorOpenAITestCredential("native", "bps-primary"))
	require.Error(t, err)
	require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id), "rollback must remove the new independent grant and restore the old BPS primary")
	_, err = s.db.Exec("ALTER TABLE scheduler_outbox DROP CONSTRAINT reject_authorization")
	require.NoError(t, err)
	again, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "attach", id), vendorOpenAITestCredential("native", "bps-primary"))
	require.NoError(t, err)
	require.Equal(t, id, again)
	after, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, openai.ClientID, after.GetCredential("client_id"))
	require.Equal(t, "native-fixture-access", after.GetCredential("access_token"))
	_, stored := vendorOpenAITestGrant(t, s, id)
	require.Equal(t, before.Credentials, stored, "the original BPS refresh token survives promotion exactly")
	dto, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"native", "bps"}, dto.AuthorizationModes)
	for _, authorization := range dto.Authorizations {
		if authorization.Mode == VendorOpenAIModeBPS {
			require.Equal(t, "policy_revoked", authorization.Status)
		}
	}
	require.Equal(t, int64(1), dto.AllTimeUsage.Requests)
	vendorOpenAITestPublicDTO(t, dto)
}

func TestVendorOpenAIAuthorizationPostgresRevokePreservesOtherGrant(t *testing.T) {
	for _, mode := range []string{"native", "bps"} {
		t.Run(mode, func(t *testing.T) {
			s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
			id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "create", 0), vendorOpenAITestCredential("native", "revocation"))
			require.NoError(t, err)
			_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), vendorOpenAITestCredential("bps", "revocation"))
			require.NoError(t, err)
			vendorOpenAITestUsage(t, s, id, v.ID)
			before, err := s.accounts.GetByID(ctx, id)
			require.NoError(t, err)
			ciphertext, _ := vendorOpenAITestGrant(t, s, id)
			snapshot := vendorOpenAITestSnapshot(t, s, id)
			_, err = s.db.Exec("ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_revoke CHECK(FALSE) NOT VALID")
			require.NoError(t, err)
			_, err = s.RevokeVendorAuthorization(ctx, id, mode)
			require.Error(t, err)
			require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id))
			_, err = s.db.Exec("ALTER TABLE scheduler_outbox DROP CONSTRAINT reject_revoke")
			require.NoError(t, err)
			dto, err := s.RevokeVendorAuthorization(ctx, id, mode)
			require.NoError(t, err)
			after, err := s.accounts.GetByID(ctx, id)
			require.NoError(t, err)
			if mode == "native" {
				for _, key := range []string{"access_token", "refresh_token", "id_token"} {
					require.Empty(t, after.GetCredential(key))
				}
				retained, _ := vendorOpenAITestGrant(t, s, id)
				require.Equal(t, ciphertext, retained)
				require.Equal(t, []string{"bps"}, dto.AuthorizationModes)
			} else {
				require.Equal(t, before.Credentials, after.Credentials)
				var count int
				require.NoError(t, s.db.QueryRow("SELECT count(*) FROM openai_excel_grants WHERE account_id=$1", id).Scan(&count))
				require.Zero(t, count)
				require.Equal(t, []string{"native"}, dto.AuthorizationModes)
			}
			require.True(t, dto.Schedulable, "the remaining enabled authorization can still supply requests")
			require.Equal(t, int64(1), dto.AllTimeUsage.Requests)
			require.Equal(t, []int64{1}, dto.GroupIDs)
			require.Equal(t, "revocation", after.GetChatGPTAccountID())
			vendorOpenAITestPublicDTO(t, dto)
			// Reauthorizing the removed mode must reuse the retained identity claim.
			again, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput(mode, "attach", id), vendorOpenAITestCredential(mode, "revocation"))
			require.NoError(t, err)
			require.Equal(t, id, again)
		})
	}
}

func TestVendorOpenAIAuthorizationPostgresArchiveRetainsBothGrants(t *testing.T) {
	s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
	id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "create", 0), vendorOpenAITestCredential("native", "archived-identity"))
	require.NoError(t, err)
	_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), vendorOpenAITestCredential("bps", "archived-identity"))
	require.NoError(t, err)
	vendorOpenAITestUsage(t, s, id, v.ID)
	before, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	ciphertext, _ := vendorOpenAITestGrant(t, s, id)
	require.NoError(t, s.DeleteVendorAccount(ctx, id))
	after, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err, "archive is not a soft database deletion")
	require.True(t, after.IsVendorArchived())
	require.Equal(t, before.Credentials, after.Credentials)
	retained, _ := vendorOpenAITestGrant(t, s, id)
	require.Equal(t, ciphertext, retained)
	dto, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"native", "bps"}, dto.AuthorizationModes)
	require.False(t, dto.Schedulable)
	require.Equal(t, int64(1), dto.AllTimeUsage.Requests)
	for _, grant := range dto.Authorizations {
		require.True(t, grant.Authorized)
		require.False(t, grant.CanRefresh)
	}
	snapshot := vendorOpenAITestSnapshot(t, s, id)
	for _, mode := range []string{"native", "bps"} {
		_, err = s.RefreshVendorAccountAuthorization(ctx, id, mode)
		require.ErrorIs(t, err, ErrVendorAccountArchived)
		_, err = s.RevokeVendorAuthorization(ctx, id, mode)
		require.ErrorIs(t, err, ErrVendorAccountArchived)
		_, err = s.StartVendorOAuth(ctx, vendorOpenAITestInput(mode, "reauthorize", id))
		require.ErrorIs(t, err, ErrVendorAccountArchived)
		_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput(mode, "create", 0), vendorOpenAITestCredential(mode, "archived-identity"))
		require.ErrorIs(t, err, ErrVendorAccountArchived, "archived identity cannot be reused under another mode")
	}
	require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id))
}

func vendorOpenAITestChild(t *testing.T, s *VendorService) (context.Context, *Vendor) {
	t.Helper()
	_, err := s.db.Exec("UPDATE vendors SET tier=2,parent_vendor_id=1 WHERE id=2; UPDATE users SET role='vendor_level2' WHERE id=4")
	require.NoError(t, err)
	ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel2, SessionGeneration: 1})
	v, err := s.CurrentVendor(ctx)
	require.NoError(t, err)
	return ctx, v
}

func TestVendorOpenAIAuthorizationPostgresReadOnlyCannotMutate(t *testing.T) {
	s, parentCtx, _ := vendorOpenAIAuthorizationPostgres(t)
	childCtx, child := vendorOpenAITestChild(t, s)
	id, err := vendorOpenAITestPersist(childCtx, s, child, vendorOpenAITestInput("native", "create", 0), vendorOpenAITestCredential("native", "child-account"))
	require.NoError(t, err)
	_, err = vendorOpenAITestPersist(childCtx, s, child, vendorOpenAITestInput("bps", "attach", id), vendorOpenAITestCredential("bps", "child-account"))
	require.NoError(t, err)
	snapshot := vendorOpenAITestSnapshot(t, s, id)
	readCtx := authz.WithVendorScope(parentCtx, authz.VendorScope{VendorID: child.ID, PolicyVersion: child.PolicyVersion, ActorUserID: 3, ReadOnly: true})
	dto, err := s.GetVendorAccount(readCtx, id)
	require.NoError(t, err)
	require.Equal(t, child.ID, dto.VendorID)
	vendorOpenAITestPublicDTO(t, dto)
	for _, mode := range []string{"native", "bps"} {
		_, err = s.RefreshVendorAccountAuthorization(readCtx, id, mode)
		require.ErrorIs(t, err, ErrVendorDenied)
		_, err = s.RevokeVendorAuthorization(readCtx, id, mode)
		require.ErrorIs(t, err, ErrVendorDenied)
		in := vendorOpenAITestInput(mode, "attach", id)
		_, err = s.StartVendorOAuth(readCtx, in)
		require.ErrorIs(t, err, ErrVendorDenied)
		in.Method, in.Token = "refresh_token", "local-untrusted-input"
		_, err = s.CreateVendorAccount(readCtx, in)
		require.ErrorIs(t, err, ErrVendorDenied)
	}
	_, err = s.RefreshVendorAccount(readCtx, id)
	require.ErrorIs(t, err, ErrVendorDenied, "legacy two-argument refresh keeps the same boundary")
	_, err = s.ExportVendorAccounts(readCtx, []int64{id})
	require.ErrorIs(t, err, ErrVendorDenied, "read-only inventory never grants access to exported secrets")
	forged := authz.WithVendorScope(parentCtx, authz.VendorScope{VendorID: child.ID, PolicyVersion: child.PolicyVersion, ActorUserID: 3})
	_, err = s.RevokeVendorAuthorization(forged, id, "bps")
	require.ErrorIs(t, err, ErrVendorDenied, "dropping the read-only marker does not grant ownership")
	require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id))
}

func TestVendorOpenAIAuthorizationPostgresSharesBackgroundRefreshLock(t *testing.T) {
	s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
	id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "create", 0), vendorOpenAITestCredential("native", "locked-account"))
	require.NoError(t, err)
	_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), vendorOpenAITestCredential("bps", "locked-account"))
	require.NoError(t, err)
	a, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	nativeKey := "oauth:refresh_lock:" + OpenAITokenCacheKey(a)
	bpsKey := fmt.Sprintf("oauth:refresh_lock:openai:excel:account:%d", id)
	require.NoError(t, s.rdb.Set(ctx, bpsKey, "background-refresh", time.Minute).Err())
	snapshot := vendorOpenAITestSnapshot(t, s, id)
	operations := []struct {
		name string
		run  func() error
	}{
		{"native refresh", func() error { _, err := s.RefreshVendorAccountAuthorization(ctx, id, "native"); return err }},
		{"BPS refresh", func() error { _, err := s.RefreshVendorAccountAuthorization(ctx, id, "bps"); return err }},
		{"attach", func() error {
			in := vendorOpenAITestInput("bps", "attach", id)
			in.Method, in.Token = "refresh_token", "fixture-new-refresh"
			_, err := s.CreateVendorAccount(ctx, in)
			return err
		}},
		{"revoke", func() error { _, err := s.RevokeVendorAuthorization(ctx, id, "bps"); return err }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			require.ErrorIs(t, operation.run(), ErrVendorAccountConflict)
			require.Equal(t, snapshot, vendorOpenAITestSnapshot(t, s, id))
			nativeHeld, err := s.rdb.Exists(ctx, nativeKey).Result()
			require.NoError(t, err)
			require.Zero(t, nativeHeld, "failure to acquire the second lock must release the first")
			owner, err := s.rdb.Get(ctx, bpsKey).Result()
			require.NoError(t, err)
			require.Equal(t, "background-refresh", owner)
		})
	}
	require.NoError(t, s.rdb.Del(ctx, bpsKey).Err())
	release, err := s.vendorTokenLock(ctx, a)
	require.NoError(t, err)
	held, err := s.rdb.Exists(ctx, nativeKey, bpsKey).Result()
	require.NoError(t, err)
	require.Equal(t, int64(2), held)
	_, err = s.vendorTokenLock(ctx, a)
	require.ErrorIs(t, err, ErrVendorAccountConflict)
	release()
	held, err = s.rdb.Exists(ctx, nativeKey, bpsKey).Result()
	require.NoError(t, err)
	require.Zero(t, held)
	// A stale cleanup must not release a successor's lease after lock expiry.
	release, err = s.vendorTokenLock(ctx, a)
	require.NoError(t, err)
	require.NoError(t, s.rdb.Set(ctx, bpsKey, "successor-refresh", time.Minute).Err())
	release()
	owner, err := s.rdb.Get(ctx, bpsKey).Result()
	require.NoError(t, err)
	require.Equal(t, "successor-refresh", owner)
}

func TestVendorOpenAIAuthorizationPostgresExportRoundTrip(t *testing.T) {
	s, ctx, v := vendorOpenAIAuthorizationPostgres(t)
	primary := vendorOpenAITestCredential("native", "export-account")
	primary.Credentials["private_internal_metadata"] = "native-must-not-export"
	id, err := vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("native", "create", 0), primary)
	require.NoError(t, err)
	bps := vendorOpenAITestCredential("bps", "export-account")
	bps.Credentials["private_internal_metadata"] = "bps-must-not-export"
	_, err = vendorOpenAITestPersist(ctx, s, v, vendorOpenAITestInput("bps", "attach", id), bps)
	require.NoError(t, err)
	bundle, err := s.ExportVendorAccounts(ctx, []int64{id})
	require.NoError(t, err)
	require.Len(t, bundle.Accounts, 1)
	require.Empty(t, bundle.Proxies)
	row := bundle.Accounts[0]
	require.Equal(t, "native-fixture-refresh", row.Credentials["refresh_token"])
	require.Equal(t, "bps-fixture-refresh", row.BPSCredentials["refresh_token"])
	require.Equal(t, openai.ClientID, row.Credentials["client_id"])
	require.Equal(t, openai.ExcelClientID, row.BPSCredentials["client_id"])
	raw, err := json.Marshal(bundle)
	require.NoError(t, err)
	// This explicit export intentionally contains portable credentials, but no
	// private extra metadata, encryption envelope, ownership IDs or token version.
	for _, field := range []string{"private_internal_metadata", "must-not-export", "ciphertext", "_token_version", "vendor_id", "proxy_id", "vendor_identity_hash"} {
		require.NotContains(t, string(raw), field)
	}
	parsed, err := parseVendorAccountBundle(raw)
	require.NoError(t, err)
	require.Equal(t, bundle.Accounts, parsed.Accounts)
	_, err = s.RevokeVendorAuthorization(ctx, id, "native")
	require.NoError(t, err)
	bundle, err = s.ExportVendorAccounts(ctx, []int64{id})
	require.NoError(t, err)
	require.Len(t, bundle.Accounts, 1, "revoked primary must not hide the surviving BPS grant")
	require.Zero(t, bundle.SkippedEmpty)
	require.Equal(t, openai.ExcelClientID, bundle.Accounts[0].Credentials["client_id"])
	require.Equal(t, "bps-fixture-refresh", bundle.Accounts[0].Credentials["refresh_token"])
	require.Empty(t, bundle.Accounts[0].BPSCredentials, "a BPS-only export uses the primary credentials slot")
	// Even retained credentials must not be exported once their mode is revoked.
	_, err = s.db.Exec(`UPDATE vendors SET openai_authorization_modes='["native"]' WHERE id=1`)
	require.NoError(t, err)
	bundle, err = s.ExportVendorAccounts(ctx, []int64{id})
	require.NoError(t, err)
	require.Empty(t, bundle.Accounts)
	require.Equal(t, 1, bundle.SkippedEmpty)
}

func TestVendorOpenAIAuthorizationPostgresImportRejectsBPSOnClaude(t *testing.T) {
	s, ctx, _ := vendorOpenAIAuthorizationPostgres(t)
	_, err := s.db.Exec(`UPDATE vendors SET platforms='["anthropic","openai"]' WHERE id=1; INSERT INTO groups VALUES(3,'Claude','anthropic','active',NULL); INSERT INTO vendor_groups VALUES(1,3,FALSE)`)
	require.NoError(t, err)
	ctx = authz.WithSubject(ctx, authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1, SessionID: "import-shape-test"})
	bundle := VendorAccountBundle{Type: "sub2api-data", Version: 1, Accounts: []VendorAccountBundleRow{{
		Name: "Claude with forged BPS grant", Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Credentials:    map[string]any{"access_token": "claude-fixture-access", "refresh_token": "claude-fixture-refresh"},
		BPSCredentials: vendorOpenAITestCredential("bps", "untrusted").Credentials,
	}}}
	raw, err := json.Marshal(bundle)
	require.NoError(t, err)
	preview, err := s.PreviewVendorAccountImport(ctx, VendorAccountImportInput{Data: raw, GroupIDs: []int64{3}, Direct: true})
	require.NoError(t, err)
	require.Len(t, preview.Rows, 1)
	require.Equal(t, "failed", preview.Rows[0].Status)
	require.Equal(t, "An independent BPS grant requires native and BPS permissions", preview.Rows[0].Message)
	response, err := json.Marshal(preview)
	require.NoError(t, err)
	require.NotContains(t, string(response), "fixture-access")
	require.NotContains(t, string(response), "fixture-refresh")
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM account_supply_identity_claims").Scan(&count))
	require.Zero(t, count, "rejection happens before any upstream verification or identity claim")
}

func TestVendorOpenAIAuthorizationPostgresMigrationProtectsLegacyBaseline(t *testing.T) {
	for _, legacy := range []string{"native", "bps-primary", "bps-independent"} {
		t.Run(legacy, func(t *testing.T) {
			s, _ := vendorAccountPostgres(t)
			_, err := s.db.Exec("ALTER TABLE vendors DROP COLUMN openai_authorization_modes; UPDATE vendors SET platforms='[\"anthropic\"]' WHERE id=2")
			require.NoError(t, err)
			if legacy == "bps-primary" {
				_, err = s.db.Exec("UPDATE accounts SET type='oauth',credentials=jsonb_build_object('client_id',$1::text) WHERE id=11", openai.ExcelClientID)
				require.NoError(t, err)
			} else if legacy == "bps-independent" {
				_, err = s.db.Exec("INSERT INTO openai_excel_grants(account_id,ciphertext) VALUES(11,'preexisting-encrypted-grant')")
				require.NoError(t, err)
			}
			migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "278_vendor_openai_authorization_modes.sql"))
			require.NoError(t, err)
			_, err = s.db.Exec(string(migration))
			if legacy != "native" {
				require.ErrorContains(t, err, "Reconcile existing supplier BPS grants")
				var exists bool
				require.NoError(t, s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='vendors' AND column_name='openai_authorization_modes')").Scan(&exists))
				require.False(t, exists, "migration must fail before silently granting a native-only policy to existing BPS suppliers")
				return
			}
			require.NoError(t, err)
			var native, claude string
			require.NoError(t, s.db.QueryRow("SELECT openai_authorization_modes::text FROM vendors WHERE id=1").Scan(&native))
			require.NoError(t, s.db.QueryRow("SELECT openai_authorization_modes::text FROM vendors WHERE id=2").Scan(&claude))
			require.JSONEq(t, `["native"]`, native)
			require.JSONEq(t, `[]`, claude)
			_, err = s.db.Exec(`UPDATE vendors SET openai_authorization_modes='["forged"]' WHERE id=1`)
			require.Error(t, err, "database also rejects mode values outside the allowlist")
		})
	}
}

func TestVendorOpenAIAuthorizationPostgresParentRevocationInvalidatesInFlight(t *testing.T) {
	s, _, parent := vendorOpenAIAuthorizationPostgres(t)
	childCtx, child := vendorOpenAITestChild(t, s)
	id, err := vendorOpenAITestPersist(childCtx, s, child, vendorOpenAITestInput("bps", "create", 0), vendorOpenAITestCredential("bps", "child-bps"))
	require.NoError(t, err)
	a, err := s.accounts.GetByID(childCtx, id)
	require.NoError(t, err)
	rootCtx := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	committed := false
	err = s.vendorAccountNetwork(childCtx, child, nil, true, a, nil, func(context.Context, string) error {
		parent.OpenAIAuthorizationModes = []string{"native"}
		_, err := s.SaveVendor(rootCtx, parent.ID, *parent)
		return err
	}, func(*sql.Tx) error {
		committed = true
		return nil
	})
	require.ErrorIs(t, err, ErrVendorAccountConflict)
	require.False(t, committed, "a verified in-flight response must not commit against revoked parent policy")
	effective, err := s.CurrentVendor(childCtx)
	require.NoError(t, err)
	require.Greater(t, effective.PolicyVersion, child.PolicyVersion)
	require.Equal(t, []string{"native"}, effective.OpenAIAuthorizationModes)
	raw, err := s.loadVendor(childCtx, child.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"native", "bps"}, raw.OpenAIAuthorizationModes, "revocation intersects policy without destroying the child's configured grant")
	_, err = s.SaveVendor(rootCtx, child.ID, *raw)
	require.ErrorIs(t, err, ErrVendorDenied, "even root cannot grant a child a mode outside its parent")
	for _, entry := range []string{"oauth", "refresh_token"} {
		in := vendorOpenAITestInput("bps", "reauthorize", id)
		in.Method = entry
		if entry == "oauth" {
			_, err = s.StartVendorOAuth(childCtx, in)
		} else {
			in.Token = "untrusted-refresh"
			_, err = s.CreateVendorAccount(childCtx, in)
		}
		require.ErrorIs(t, err, ErrVendorDenied)
	}
	_, err = s.RefreshVendorAccountAuthorization(childCtx, id, "bps")
	require.ErrorIs(t, err, ErrVendorDenied)
	_, err = s.RevokeVendorAuthorization(childCtx, id, "bps")
	require.ErrorIs(t, err, ErrVendorDenied)
	retained, err := s.accounts.GetByID(childCtx, id)
	require.NoError(t, err)
	require.Equal(t, a.Credentials, retained.Credentials)
	dto, err := s.GetVendorAccount(childCtx, id)
	require.NoError(t, err)
	require.False(t, dto.Schedulable)
	for _, grant := range dto.Authorizations {
		if grant.Mode == "bps" {
			require.True(t, grant.Authorized)
			require.Equal(t, "policy_revoked", grant.Status)
			require.False(t, grant.CanRefresh)
		}
	}
}
