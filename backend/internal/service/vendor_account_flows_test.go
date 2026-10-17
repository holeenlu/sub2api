package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type vendorSuccessOpenAIClient struct {
	OpenAIOAuthClient
	exchanges, refreshes atomic.Int32
}

func vendorAccountTransferPostgres(t *testing.T) (*VendorService, context.Context) {
	t.Helper()
	s, ctx := vendorAccountPostgres(t)
	_, err := s.db.Exec(`
CREATE SEQUENCE account_transfer_proxy_ids START 100;
ALTER TABLE proxies ALTER COLUMN id SET DEFAULT nextval('account_transfer_proxy_ids');
ALTER TABLE proxies ADD COLUMN name TEXT NOT NULL DEFAULT '', ADD COLUMN protocol TEXT NOT NULL DEFAULT 'http', ADD COLUMN host TEXT NOT NULL DEFAULT 'public.invalid', ADD COLUMN port INT NOT NULL DEFAULT 8080, ADD COLUMN username TEXT, ADD COLUMN password TEXT, ADD COLUMN created_at TIMESTAMPTZ DEFAULT NOW(), ADD COLUMN updated_at TIMESTAMPTZ DEFAULT NOW(), ADD COLUMN fallback_mode TEXT NOT NULL DEFAULT 'none', ADD COLUMN backup_proxy_id BIGINT REFERENCES proxies(id), ADD COLUMN expiry_warn_days INT NOT NULL DEFAULT 7, ADD COLUMN created_by BIGINT, ADD COLUMN vendor_notes TEXT NOT NULL DEFAULT '', ADD COLUMN region_label_country_code TEXT NOT NULL DEFAULT '', ADD COLUMN region_label_city TEXT NOT NULL DEFAULT '', ADD COLUMN config_version BIGINT NOT NULL DEFAULT 1;
UPDATE vendors SET proxy_limit=20, allow_direct=FALSE;
`)
	require.NoError(t, err)
	return s, authz.WithSubject(ctx, authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1, SessionID: "native-import"})
}

func nativeVendorAccountTransferInput(t *testing.T) VendorAccountImportInput {
	t.Helper()
	key := NativeDataProxyKey("http", "public.invalid", 8080, "transfer-user", "transfer-password")
	// Exactly the native administrator format: no type/version, with secrets
	// and portable proxy_key references rather than local resource IDs.
	raw, err := json.Marshal(map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"accounts":    []map[string]any{{"name": "from admin", "platform": "openai", "type": "oauth", "proxy_key": key, "vendor_id": 99, "proxy_id": 1, "priority": 0, "credentials": map[string]any{"access_token": "local-verified-access", "refresh_token": "local-refresh"}}},
		"proxies":     []AccountDataProxy{{ProxyKey: key, Name: "imported proxy", Protocol: "http", Host: "public.invalid", Port: 8080, Username: "transfer-user", Password: "transfer-password", Status: StatusActive, FallbackMode: FallbackModeProxy, BackupProxyName: "backup"}, {Name: "backup", Protocol: "socks5h", Host: "backup.invalid", Port: 1080, Status: StatusActive}},
	})
	require.NoError(t, err)
	return VendorAccountImportInput{Data: raw, GroupIDs: []int64{1}, Mode: "update"}
}

func TestVendorAccountNativeBundleProxyRoundTrip(t *testing.T) {
	s, ctx := vendorAccountTransferPostgres(t)
	client := vendorAccountFlowOAuth(t, s)
	_, err := s.db.Exec(`INSERT INTO proxies(vendor_id,name,host,port,username,password,status) VALUES(2,'foreign proxy','public.invalid',8080,'transfer-user','transfer-password','active')`)
	require.NoError(t, err)
	input := nativeVendorAccountTransferInput(t)
	preview, err := s.PreviewVendorAccountImport(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "create", preview.Rows[0].Status, preview.Rows)
	require.Nil(t, preview.Rows[0].ProxyID)
	require.NotNil(t, preview.Rows[0].Proxy)
	require.False(t, preview.Rows[0].Direct)
	safe, err := json.Marshal(preview)
	require.NoError(t, err)
	for _, secret := range []string{"transfer-user", "transfer-password", "local-refresh", "proxy_key"} {
		require.NotContains(t, string(safe), secret)
	}
	var count int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM proxies WHERE vendor_id=1").Scan(&count))
	require.Zero(t, count, "preview must not persist proxies")
	require.Zero(t, client.refreshes.Load())
	sealed, err := s.rdb.Get(ctx, "oauth:vendor-import:"+preview.PreviewID).Result()
	require.NoError(t, err)
	require.NotContains(t, sealed, "transfer-password")
	result, err := s.ImportVendorAccounts(ctx, VendorAccountImportInput{PreviewID: preview.PreviewID})
	require.NoError(t, err)
	require.Equal(t, "created", result.Rows[0].Status, result.Rows)
	require.NotNil(t, result.Rows[0].ProxyID)
	var proxyID, owner int64
	var password, fallback string
	var backupID int64
	require.NoError(t, s.db.QueryRow("SELECT a.proxy_id,p.vendor_id,p.password,p.fallback_mode,p.backup_proxy_id FROM accounts a JOIN proxies p ON p.id=a.proxy_id WHERE a.id=$1", result.Rows[0].AccountID).Scan(&proxyID, &owner, &password, &fallback, &backupID))
	require.Equal(t, *result.Rows[0].ProxyID, proxyID)
	require.EqualValues(t, 1, owner, "do not reuse another supplier's proxy")
	require.Equal(t, "transfer-password", password)
	require.Equal(t, FallbackModeProxy, fallback)
	require.Positive(t, backupID)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM proxies WHERE vendor_id=1").Scan(&count))
	require.Equal(t, 2, count)
	again, err := s.ImportVendorAccounts(ctx, VendorAccountImportInput{PreviewID: preview.PreviewID})
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, int32(1), client.refreshes.Load())
	bundle, err := s.ExportVendorAccounts(ctx, []int64{result.Rows[0].AccountID})
	require.NoError(t, err)
	require.Len(t, bundle.Proxies, 2)
	require.NotNil(t, bundle.Accounts[0].ProxyKey)
	require.Equal(t, bundle.Proxies[1].ProxyKey, *bundle.Accounts[0].ProxyKey)
	require.Equal(t, password, bundle.Proxies[1].Password)
	require.Equal(t, bundle.Proxies[0].Name, bundle.Proxies[1].BackupProxyName)
	input.Data, err = json.Marshal(bundle)
	require.NoError(t, err)
	require.NoError(t, s.rdb.Del(ctx, "vendor:account:rate:1:0:import_preview").Err())
	preview, err = s.PreviewVendorAccountImport(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "update", preview.Rows[0].Status, preview.Rows)
	require.Equal(t, proxyID, *preview.Rows[0].ProxyID)
	result, err = s.ImportVendorAccounts(ctx, VendorAccountImportInput{PreviewID: preview.PreviewID})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Rows[0].Status, result.Rows)
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM proxies WHERE vendor_id=1").Scan(&count))
	require.Equal(t, 2, count, "reimport must reuse the owned endpoint")
}

func TestVendorAccountBundledProxyConfirmationRechecksCapacity(t *testing.T) {
	s, ctx := vendorAccountTransferPostgres(t)
	client := vendorAccountFlowOAuth(t, s)
	preview, err := s.PreviewVendorAccountImport(ctx, nativeVendorAccountTransferInput(t))
	require.NoError(t, err)
	require.Equal(t, "create", preview.Rows[0].Status, preview.Rows)
	_, err = s.db.Exec("UPDATE vendors SET proxy_limit=1 WHERE id=1; INSERT INTO proxies(vendor_id,name,host,status) VALUES(1,'occupied','occupied.invalid','active')")
	require.NoError(t, err)
	result, err := s.ImportVendorAccounts(ctx, VendorAccountImportInput{PreviewID: preview.PreviewID})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Rows[0].Status)
	require.Zero(t, result.Rows[0].AccountID)
	require.Zero(t, client.refreshes.Load(), "proxy failure must not rotate authorization or use direct egress")
	var count int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM proxies WHERE vendor_id=1").Scan(&count))
	require.Equal(t, 1, count)
}

func (c *vendorSuccessOpenAIClient) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	c.exchanges.Add(1)
	return &openai.TokenResponse{AccessToken: "local-verified-access", RefreshToken: "local-refresh", ExpiresIn: 3600}, nil
}
func (c *vendorSuccessOpenAIClient) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	c.refreshes.Add(1)
	return &openai.TokenResponse{AccessToken: "local-refreshed-access", RefreshToken: "local-next-refresh", ExpiresIn: 3600}, nil
}
func vendorAccountFlowOAuth(t *testing.T, s *VendorService) *vendorSuccessOpenAIClient {
	t.Helper()
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{accountsCheck: map[string]any{"accounts": map[string]any{"verified-workspace": map[string]any{"account": map[string]any{"account_id": "verified-workspace", "plan_type": "plus", "is_default": true}}}}})
	t.Cleanup(server.Close)
	client := &vendorSuccessOpenAIClient{}
	native := NewOpenAIOAuthService(nil, client, s.rdb)
	t.Cleanup(native.Stop)
	native.privacyClientFactory = newTestPrivacyClientFactory()
	native.excelEncryptor = &liveAttestationAES{key: [32]byte{1}}
	s.openaiOAuth = native
	return client
}
func TestVendorAccountPostgresOAuthAndImportRoundTripOneShot(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	client := vendorAccountFlowOAuth(t, s)
	ctx = authz.WithSubject(ctx, authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1, SessionID: "login-one"})
	start, err := s.StartVendorOAuth(ctx, VendorAccountInput{Name: "original", Platform: PlatformOpenAI, Method: "oauth", GroupIDs: []int64{1}, Direct: true})
	require.NoError(t, err)
	u, err := url.Parse(start.AuthURL)
	require.NoError(t, err)
	complete := VendorOAuthCompleteInput{Code: "one-use-code", State: u.Query().Get("state")}
	otherSession := authz.WithSubject(ctx, authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1, SessionID: "login-two"})
	_, err = s.CompleteVendorOAuth(otherSession, PlatformOpenAI, start.SessionID, complete)
	require.ErrorIs(t, err, authz.ErrAuthorizationExpired)
	require.Zero(t, client.exchanges.Load())
	var wg sync.WaitGroup
	gate := make(chan struct{})
	results := make(chan *VendorAccountDTO, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			dto, e := s.CompleteVendorOAuth(ctx, PlatformOpenAI, start.SessionID, complete)
			if e == nil {
				results <- dto
			} else {
				require.ErrorIs(t, e, ErrVendorAccountConflict)
			}
		}()
	}
	close(gate)
	wg.Wait()
	close(results)
	var saved *VendorAccountDTO
	for result := range results {
		if saved != nil {
			require.Equal(t, saved.ID, result.ID)
		}
		saved = result
	}
	require.NotNil(t, saved)
	require.Equal(t, int32(1), client.exchanges.Load())
	replay, err := s.CompleteVendorOAuth(ctx, PlatformOpenAI, start.SessionID, complete)
	require.NoError(t, err)
	require.Equal(t, saved.ID, replay.ID)
	require.Equal(t, int32(1), client.exchanges.Load())
	_, err = s.SetVendorAccountPaused(ctx, saved.ID, true)
	require.NoError(t, err)
	bundle, err := s.ExportVendorAccounts(ctx, []int64{saved.ID})
	require.NoError(t, err)
	raw, err := json.Marshal(bundle)
	require.NoError(t, err)
	preview, err := s.PreviewVendorAccountImport(ctx, VendorAccountImportInput{Data: raw, GroupIDs: []int64{1}, Direct: true, Mode: "update"})
	require.NoError(t, err)
	require.Len(t, preview.Rows, 1)
	require.Equal(t, "update", preview.Rows[0].Status)
	require.Zero(t, client.refreshes.Load(), "preview must not rotate tokens")
	imported, err := s.ImportVendorAccounts(ctx, VendorAccountImportInput{PreviewID: preview.PreviewID})
	require.NoError(t, err)
	require.Equal(t, "updated", imported.Rows[0].Status)
	require.Equal(t, saved.ID, imported.Rows[0].AccountID)
	require.Equal(t, int32(1), client.refreshes.Load())
	again, err := s.ImportVendorAccounts(ctx, VendorAccountImportInput{PreviewID: preview.PreviewID})
	require.NoError(t, err)
	require.Equal(t, imported, again)
	require.Equal(t, int32(1), client.refreshes.Load())
	var paused bool
	require.NoError(t, s.db.QueryRow("SELECT vendor_paused_at IS NOT NULL FROM accounts WHERE id=$1", saved.ID).Scan(&paused))
	require.True(t, paused)
}
func TestVendorNativeIdentityFeatureOffPreservesNativeUntilSupplyExists(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	_, err := s.db.Exec("DELETE FROM accounts")
	require.NoError(t, err)
	guard := s.NativeSupplyIdentityGuard()
	a := &Account{ID: 900, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "legacy-native-token"}}
	proof, err := guard.Verify(context.Background(), a)
	require.NoError(t, err)
	require.True(t, proof.deferred)
	tx, err := s.db.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO accounts(id,name,platform,type,credentials) VALUES(900,'native','openai','oauth','{"access_token":"legacy-native-token"}')`)
	require.NoError(t, err)
	require.NoError(t, guard.Claim(context.Background(), tx, a, proof))
	require.NoError(t, tx.Commit())
	var claims int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM account_supply_identity_claims").Scan(&claims))
	require.Zero(t, claims)
	_, err = s.db.Exec("INSERT INTO settings(key,value) VALUES('vendor_console_enabled','true')")
	require.NoError(t, err)
	tx, err = s.db.Begin()
	require.NoError(t, err)
	require.ErrorIs(t, guard.Claim(context.Background(), tx, a, proof), ErrVendorAccountConflict)
	require.NoError(t, tx.Rollback())
}
func TestVendorNativeDeferredWriteSerializesBeforeFirstSupplierClaim(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	_, err := s.db.Exec("DELETE FROM accounts")
	require.NoError(t, err)
	guard := s.NativeSupplyIdentityGuard()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a := &Account{ID: 900, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "legacy-native-token"}}
	proof, err := guard.Verify(ctx, a)
	require.NoError(t, err)
	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO accounts(id,name,platform,type,credentials) VALUES(900,'native','openai','oauth','{"access_token":"legacy-native-token"}')`)
	require.NoError(t, err)
	require.NoError(t, guard.Claim(ctx, tx, a, proof))
	result := make(chan error, 1)
	go func() {
		supplierCtx := authz.WithSubject(ctx, authz.Subject{UserID: 3, Role: authz.VendorLevel1, SessionGeneration: 1})
		supplierCtx = authz.WithVendorScope(supplierCtx, authz.VendorScope{VendorID: 1, PolicyVersion: 1, ActorUserID: 3})
		_, e := vendorAccountTestWrite(supplierCtx, s, vendorAccountTestVendor(1, 10), "unknown-native-identity", 0)
		result <- e
	}()
	select {
	case e := <-result:
		t.Fatalf("supplier claim passed pending native commit: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-result, ErrVendorIdentityBound)
}
func TestVendorNativeTombstoneResolutionVerifiesIdentityWithoutRestoringCredentials(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	vendorAccountFlowOAuth(t, s)
	_, err := s.db.Exec(`INSERT INTO accounts(id,name,platform,type,deleted_at,credentials) VALUES(800,'legacy tombstone','openai','oauth',NOW(),'{"chatgpt_account_id":"verified-workspace"}')`)
	require.NoError(t, err)
	admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	result, err := s.ResolveSupplyIdentity(admin, 800, "fresh-verified-token", nil, true)
	require.NoError(t, err)
	require.Equal(t, "claimed", result.Rows[0].Status)
	var deleted sql.NullTime
	var raw string
	require.NoError(t, s.db.QueryRow("SELECT deleted_at,credentials::text FROM accounts WHERE id=800").Scan(&deleted, &raw))
	require.True(t, deleted.Valid)
	require.NotContains(t, raw, "fresh-verified-token")
	require.NotContains(t, raw, "access_token")
	_, err = s.db.Exec(`INSERT INTO accounts(id,name,platform,type,deleted_at,credentials) VALUES(801,'unidentifiable tombstone','openai','oauth',NOW(),'{}')`)
	require.NoError(t, err)
	_, err = s.ResolveSupplyIdentity(admin, 801, "fresh-verified-token", nil, true)
	require.ErrorContains(t, err, "Historical stable identity is missing")
}

func TestVendorNativeReconciliationReportsVerifiedAndUnrecoverableRows(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	vendorAccountFlowOAuth(t, s)
	_, err := s.db.Exec("INSERT INTO accounts(id,name,platform,type,credentials) VALUES(800,'verifiable native','openai','oauth','{\"access_token\":\"local-verified-access\"}'),(801,'erased native','openai','oauth','{}')")
	require.NoError(t, err)
	admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	first, err := s.ReconcileSupplyIdentities(admin, 0, 1)
	require.NoError(t, err)
	require.Len(t, first.Rows, 1)
	require.Equal(t, "claimed", first.Rows[0].Status)
	require.False(t, first.Done)
	require.False(t, first.BaselineReady)
	second, err := s.ReconcileSupplyIdentities(admin, first.NextAfterID, 10)
	require.NoError(t, err)
	require.Len(t, second.Rows, 1)
	require.Equal(t, "failed", second.Rows[0].Status)
	require.True(t, second.Done)
	require.False(t, second.BaselineReady, "cursor completion cannot conceal an unresolved baseline")
	var claims int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM account_supply_identity_claims").Scan(&claims))
	require.Equal(t, 1, claims)
}

func TestVendorAccountPostgresTreeLimitAcrossCreateImportAndOAuth(t *testing.T) {
	s, parent := vendorAccountPostgres(t)
	vendorAccountFlowOAuth(t, s)
	_, err := s.db.Exec("UPDATE vendors SET account_limit=3 WHERE id=1; UPDATE vendors SET tier=2,parent_vendor_id=1 WHERE id=2; UPDATE users SET role='vendor_level2' WHERE id=4")
	require.NoError(t, err)
	child := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.VendorLevel2, SessionGeneration: 1, SessionID: "tree-flows"})
	child = authz.WithVendorScope(child, authz.VendorScope{VendorID: 2, ActorUserID: 4, PolicyVersion: 1})
	in := VendorAccountInput{Name: "child", Platform: PlatformOpenAI, Method: "oauth", GroupIDs: []int64{1}, Direct: true}
	start, err := s.StartVendorOAuth(child, in)
	require.NoError(t, err)
	u, err := url.Parse(start.AuthURL)
	require.NoError(t, err)
	preview, err := s.PreviewVendorAccountImport(child, VendorAccountImportInput{Data: json.RawMessage(`{"type":"sub2api-bundle","version":1,"accounts":[{"name":"imported","platform":"openai","type":"oauth","credentials":{"access_token":"local-verified-access","refresh_token":"local-refresh"}}]}`), GroupIDs: []int64{1}, Direct: true, Mode: "skip"})
	require.NoError(t, err)
	require.Equal(t, "create", preview.Rows[0].Status)
	// A different owner takes the final slot after preview and OAuth initiation.
	_, err = vendorAccountTestWrite(parent, s, vendorAccountTestVendor(1, 3), "root-last-slot", 0)
	require.NoError(t, err)
	results := make(chan error, 3)
	networkSlots := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for _, entry := range []string{"create", "import", "oauth"} {
		wg.Add(1)
		go func(entry string) {
			defer wg.Done()
			networkSlots <- struct{}{}
			defer func() { <-networkSlots }()
			var e error
			switch entry {
			case "create":
				create := in
				create.Method = "refresh_token"
				create.Token = "local-refresh"
				_, e = s.CreateVendorAccount(child, create)
			case "oauth":
				_, e = s.CompleteVendorOAuth(child, PlatformOpenAI, start.SessionID, VendorOAuthCompleteInput{Code: "code", State: u.Query().Get("state")})
			case "import":
				var result *VendorAccountImportResult
				result, e = s.ImportVendorAccounts(child, VendorAccountImportInput{PreviewID: preview.PreviewID})
				if e == nil && len(result.Rows) == 1 && result.Rows[0].Status == "failed" {
					e = fmt.Errorf("%s", result.Rows[0].Message)
				}
			}
			results <- e
		}(entry)
	}
	wg.Wait()
	close(results)
	for e := range results {
		require.ErrorContains(t, e, "limit")
	}
	var count int
	require.NoError(t, s.db.QueryRow("SELECT count(*) FROM accounts WHERE vendor_id IN(1,2) AND deleted_at IS NULL AND parent_account_id IS NULL").Scan(&count))
	require.Equal(t, 3, count)
}
