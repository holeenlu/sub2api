package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestVendorOAuthStateParsing(t *testing.T) {
	for _, tc := range []struct {
		name, platform, code, state string
		ok                          bool
	}{
		{"claude native", PlatformAnthropic, "code#state", "", true},
		{"openai callback", PlatformOpenAI, "http://localhost:1455/auth/callback?code=valid&state=state", "", true},
		{"openai explicit", PlatformOpenAI, "code", "state", true},
		{"missing state", PlatformOpenAI, "code", "", false},
		{"wrong state", PlatformAnthropic, "code#attacker", "", false},
		{"conflicting state", PlatformAnthropic, "code#state", "attacker", false},
		{"wrong redirect", PlatformOpenAI, "https://attacker.example/cb?code=valid&state=state", "", false},
		{"duplicate state", PlatformOpenAI, "http://localhost:1455/auth/callback?code=valid&state=state&state=state", "", false},
		{"fragment smuggling", PlatformOpenAI, "http://localhost:1455/auth/callback?code=valid&state=state#other", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := parseVendorOAuthCode(tc.platform, tc.code, tc.state, "state")
			if tc.ok {
				require.NoError(t, err)
				require.NotEmpty(t, code)
			} else {
				require.Error(t, err)
			}
		})
	}
}
func TestVendorInputNeedsExplicitEgressAndValidGroups(t *testing.T) {
	in := VendorAccountInput{Name: " account ", Platform: PlatformOpenAI, GroupIDs: []int64{2, 2, 1}}
	require.Error(t, normalizeVendorAccountInput(&in))
	in.Direct = true
	require.NoError(t, normalizeVendorAccountInput(&in))
	require.Equal(t, []int64{1, 2}, in.GroupIDs)
	require.Equal(t, "account", in.Name)
	in.Name = "  "
	require.NoError(t, normalizeVendorAccountInput(&in), "display name may be derived after trusted authorization")
	require.Empty(t, in.Name)
	proxy := int64(1)
	in.ProxyID = &proxy
	require.Error(t, normalizeVendorAccountInput(&in))
	in.Direct = false
	in.GroupIDs = nil
	require.Error(t, normalizeVendorAccountInput(&in))
}
func TestVendorAccountDTODoesNotLeakSecretsOrNativeBilling(t *testing.T) {
	now := time.Now()
	vendorID := int64(3)
	notes := "internal-secret"
	a := &Account{ID: 1, VendorID: &vendorID, Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Notes: &notes, VendorNotes: "supplier note", Status: StatusDisabled, Schedulable: false, VendorPausedAt: &now, Credentials: map[string]any{"access_token": "secret-at", "refresh_token": "secret-rt", "email": "unverified@example.org"}, Extra: map[string]any{"route-secret": "hidden", "vendor_verified_email": "verified@example.org", "codex_5h_used_percent": 42.0, "codex_5h_reset_at": now.Add(time.Hour).Format(time.RFC3339)}, ErrorMessage: "Bearer secret-at at 10.0.0.1", Groups: []*Group{{ID: 4, Name: "visible", Description: "private policy"}}}
	dto := vendorAccountDTO(a, &Vendor{AllowDirect: true})
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	for _, secret := range []string{"secret-at", "secret-rt", "unverified@example.org", "internal-secret", "10.0.0.1", "route-secret", "private policy", "user_cost"} {
		require.NotContains(t, string(raw), secret)
	}
	require.Contains(t, string(raw), "supplier note")
	require.Equal(t, "verified@example.org", dto.Email)
	require.Nil(t, dto.Quota.SevenDayFable)
	require.False(t, dto.Schedulable)
	require.Contains(t, dto.Blockers, "vendor_paused")
	require.Contains(t, dto.Blockers, "platform_disabled")
	require.Equal(t, 42.0, dto.Quota.FiveHour.Utilization)
}

func TestVendorAccountArchiveOverridesStaleFlagsWithoutChangingHealth(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Microsecond)
	a := &Account{Status: StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "secret", "refresh_token": "refresh"}}
	dto := vendorAccountDTO(a, nil)
	require.False(t, dto.Archived)
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"archived_at":null`)
	a.Extra = map[string]any{VendorArchivedAtKey: at.Format(time.RFC3339Nano)}
	require.False(t, a.IsSchedulable(), "archive must work even without cached owner/pause fields")
	require.False(t, a.IsCredentialUsableForShadow())
	dto = vendorAccountDTO(a, nil)
	require.True(t, dto.Archived)
	require.Equal(t, &at, dto.ArchivedAt)
	require.Equal(t, StatusActive, dto.Status)
	require.Contains(t, dto.Blockers, "archived")
	require.False(t, dto.CanRefresh)
	for _, malformed := range []any{"invalid timestamp", true, map[string]any{"at": "invalid"}} {
		a.Extra[VendorArchivedAtKey] = malformed
		require.True(t, a.IsVendorArchived())
		require.False(t, a.IsSchedulable())
		require.Nil(t, a.VendorArchivedAt())
	}
}

func TestVendorAccountFailureAndQuotaClassification(t *testing.T) {
	for _, tc := range []struct {
		name     string
		message  string
		category string
	}{
		{name: "kyc", message: "provider KYC review failed", category: "kyc"},
		{name: "terms", message: "terms not accepted", category: "terms"},
		{name: "tier", message: "subscription plan mismatch", category: "tier"},
		{name: "duplicate", message: "duplicate account", category: "duplicate"},
		{name: "email", message: "re-login email mismatch", category: "email_mismatch"},
		{name: "connection", message: "connection refused", category: "connection"},
		{name: "unknown", message: "provider returned an unclassified failure", category: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Status: StatusError, ErrorMessage: tc.message}
			require.Equal(t, tc.category, vendorAccountFailureCategory(a))
		})
	}

	d := VendorQuotaDistribution{}
	require.False(t, vendorCountQuota(&d, &VendorQuotaWindow{Utilization: 89.99}))
	require.True(t, vendorCountQuota(&d, &VendorQuotaWindow{Utilization: 90}))
	require.True(t, vendorCountQuota(&d, &VendorQuotaWindow{Utilization: 99}))
	require.False(t, vendorCountQuota(&d, nil))
	require.Equal(t, int64(1), d.Normal)
	require.Equal(t, int64(1), d.NearLimit)
	require.Equal(t, int64(1), d.Full)
	require.Equal(t, int64(1), d.Unknown)
}

func TestVendorIdentityHashIsKeyedAndPlatformSeparated(t *testing.T) {
	a := VendorSupplyIdentityHash("key-a", PlatformOpenAI, "account-1")
	require.Len(t, a, 64)
	require.NotEqual(t, a, VendorSupplyIdentityHash("key-b", PlatformOpenAI, "account-1"))
	require.NotEqual(t, a, VendorSupplyIdentityHash("key-a", PlatformAnthropic, "account-1"))
	require.NotContains(t, a, "account-1")
}
func TestVendorBundleRejectsDemoAndUnknownVersion(t *testing.T) {
	for _, raw := range []string{
		`{"accounts":[{"name":"native admin export"}],"proxies":[]}`,
		`{"type":"","version":0,"accounts":[{}],"proxies":[]}`,
	} {
		_, err := parseVendorAccountBundle([]byte(raw))
		require.NoError(t, err, "native optional headers must match admin import")
	}
	for _, raw := range []string{`{"type":"sub2api-vendor-demo","version":1,"accounts":[{}]}`, `{"type":"sub2api-data","version":3,"accounts":[{}]}`, `{"type":"sub2api-data","version":2,"accounts":[{"siwc_authorization":{"registration":{}}}]}`, `{"type":"sub2api-data","version":1,"accounts":[]}`} {
		_, err := parseVendorAccountBundle([]byte(raw))
		require.Error(t, err)
	}
	raw := `{"type":"sub2api-bundle","version":1,"vendor_id":99,"accounts":[{"name":"mine","platform":"openai","type":"oauth","role":"super_admin","priority":0,"credentials":{"refresh_token":"rt"}}]}`
	b, err := parseVendorAccountBundle([]byte(raw))
	require.NoError(t, err)
	out, err := json.Marshal(b)
	require.NoError(t, err)
	require.NotContains(t, string(out), "vendor_id")
	require.NotContains(t, string(out), "priority")
	require.NotContains(t, string(out), "super_admin")
}

func TestVendorAccountBundleProxyUsesNativeKeysAndSafeBackupMapping(t *testing.T) {
	primary := AccountDataProxy{ProxyKey: "source-primary", Name: "primary", Protocol: "HTTP", Host: "PUBLIC.invalid", Port: 8080, Username: "user", Password: "password", Status: StatusActive, FallbackMode: FallbackModeProxy, BackupProxyName: "backup"}
	backup := AccountDataProxy{Name: "backup", Protocol: "socks5h", Host: "backup.invalid", Port: 1080, Status: StatusActive}
	plan, err := vendorAccountBundleProxy([]AccountDataProxy{primary, backup}, primary.ProxyKey, nil)
	require.NoError(t, err)
	require.Equal(t, "http", plan.Input.Protocol)
	require.Equal(t, "public.invalid", plan.Input.Host)
	require.Equal(t, primary.Password, plan.Input.Password)
	require.Zero(t, *plan.Input.ExpiryWarnDays)
	require.NotNil(t, plan.Backup)
	require.Nil(t, plan.Input.BackupProxyID, "backup IDs are assigned only at confirmation")
	require.Contains(t, plan.proxy(7).URL(), "#vendor-proxy:7:0:0", "preview must retain native vendor transport restrictions")
	_, err = vendorAccountBundleProxy([]AccountDataProxy{primary, primary, backup, backup}, primary.ProxyKey, nil)
	require.NoError(t, err, "repeated native proxy rows use the same endpoint")
	conflicting := primary
	conflicting.Host = "different.invalid"
	conflictingBackup := backup
	conflictingBackup.Host = "different-backup.invalid"
	for _, test := range []struct {
		name    string
		proxies []AccountDataProxy
		key     string
	}{
		{"missing", []AccountDataProxy{primary}, "missing"},
		{"conflicting key", []AccountDataProxy{primary, conflicting, backup}, primary.ProxyKey},
		{"missing backup", []AccountDataProxy{primary}, primary.ProxyKey},
		{"ambiguous backup", []AccountDataProxy{primary, backup, conflictingBackup}, primary.ProxyKey},
	} {
		t.Run(test.name, func(t *testing.T) { _, e := vendorAccountBundleProxy(test.proxies, test.key, nil); require.Error(t, e) })
	}
	primary.BackupProxyName = primary.Name
	_, err = vendorAccountBundleProxy([]AccountDataProxy{primary}, primary.ProxyKey, nil)
	require.Error(t, err)
	primary.FallbackMode = FallbackModeDirect
	_, err = vendorAccountBundleProxy([]AccountDataProxy{primary}, primary.ProxyKey, nil)
	require.Error(t, err)
	primary.FallbackMode, primary.Host = FallbackModeNone, "127.0.0.1"
	_, err = vendorAccountBundleProxy([]AccountDataProxy{primary}, primary.ProxyKey, nil)
	require.Error(t, err)
}
func TestVendorExportCredentialWhitelist(t *testing.T) {
	out := vendorExportCredentials(&Account{Credentials: map[string]any{"access_token": "owned", "refresh_token": "owned-rt", "base_url": "https://private", "model_mapping": "secret", "openai_excel_credentials": "encrypted", "client_id": "codex"}})
	require.Len(t, out, 3)
	require.Equal(t, "owned", out["access_token"])
	require.NotContains(t, out, "base_url")
	require.NotContains(t, out, "openai_excel_credentials")
}
func vendorTestDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db, mock
}
func TestVendorTransactionRejectsStalePolicyAndRollsBack(t *testing.T) {
	db, mock := vendorTestDB(t)
	s := &VendorService{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"root_id"}).AddRow(3))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT u.id FROM users u WHERE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectQuery("SELECT role,status,session_generation FROM users").WillReturnRows(sqlmock.NewRows([]string{"role", "status", "generation"}).AddRow(authz.SuperAdmin, StatusActive, 1))
	mock.ExpectQuery("SELECT policy_version,status FROM vendors").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"policy_version", "status"}).AddRow(8, StatusActive))
	mock.ExpectRollback()
	called := false
	err := s.vendorAccountTx(authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1}), &Vendor{ID: 3, PolicyVersion: 7}, func(*sql.Tx) error { called = true; return nil })
	require.ErrorIs(t, err, ErrVendorAccountConflict)
	require.False(t, called)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVendorTransactionRevalidatesBeforeCommit(t *testing.T) {
	db, mock := vendorTestDB(t)
	s := &VendorService{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"root_id"}).AddRow(3))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT u.id FROM users u WHERE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectQuery("SELECT role,status,session_generation FROM users").WillReturnRows(sqlmock.NewRows([]string{"role", "status", "generation"}).AddRow(authz.SuperAdmin, StatusActive, 1))
	mock.ExpectQuery("SELECT policy_version,status FROM vendors").WillReturnRows(sqlmock.NewRows([]string{"version", "status"}).AddRow(7, StatusActive))
	mock.ExpectRollback()
	calls := 0
	ctx := authz.WithRevalidation(authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1}), func(context.Context) error {
		calls++
		if calls > 1 {
			return errors.New("revoked")
		}
		return nil
	})
	require.Error(t, s.vendorAccountTx(ctx, &Vendor{ID: 3, PolicyVersion: 7}, func(*sql.Tx) error { return nil }))
	require.Equal(t, 2, calls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVendorIdentityTombstoneCannotBeTakenByAnotherOwner(t *testing.T) {
	db, mock := vendorTestDB(t)
	s := &VendorService{cfg: &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("k", 32)}}}
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	verified := &vendorVerifiedCredential{Identity: "account", AccountID: "account"}
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectVendorIdentityKey(mock, strings.Repeat("k", 32))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT canonical_account_id,vendor_id FROM account_supply_identity_claims WHERE platform=$1 AND identity_hash=$2 FOR UPDATE")).WillReturnRows(sqlmock.NewRows([]string{"canonical_account_id", "vendor_id"}).AddRow(50, 99))
	_, _, err = s.reserveVendorIdentity(context.Background(), tx, &Vendor{ID: 3}, VendorAccountInput{Platform: PlatformOpenAI}, verified)
	require.ErrorIs(t, err, ErrVendorIdentityBound)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVendorAccountNetworkCommitsPreflightBeforeRequestAndRejectsRevocation(t *testing.T) {
	db, mock := vendorTestDB(t)
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer rdb.Close()
	s := &VendorService{db: db, rdb: rdb}
	v := &Vendor{ID: 3, PolicyVersion: 7, AllowDirect: true}
	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT COALESCE").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"root_id"}).AddRow(3))
		mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT u.id FROM users u WHERE").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
		mock.ExpectQuery("SELECT role,status,session_generation FROM users").WillReturnRows(sqlmock.NewRows([]string{"role", "status", "generation"}).AddRow(authz.SuperAdmin, StatusActive, 1))
		mock.ExpectQuery("SELECT policy_version,status FROM vendors").WillReturnRows(sqlmock.NewRows([]string{"policy_version", "status"}).AddRow(7, StatusActive))
		if i == 0 {
			mock.ExpectCommit()
		} else {
			mock.ExpectRollback()
		}
	}
	revoked := false
	ctx := authz.WithRevalidation(authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1}), func(context.Context) error {
		if revoked {
			return authz.ErrAuthorizationExpired
		}
		return nil
	})
	called, committed := false, false
	err := s.vendorAccountNetwork(ctx, v, nil, true, nil, nil, func(ctx context.Context, _ string) error {
		called = true
		_, ok := ctx.Deadline()
		require.True(t, ok)
		revoked = true
		return nil
	}, func(*sql.Tx) error { committed = true; return nil })
	require.ErrorIs(t, err, authz.ErrAuthorizationExpired)
	require.True(t, called)
	require.False(t, committed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVendorAccountCapacityRejectsBeforeInsert(t *testing.T) {
	db, mock := vendorTestDB(t)
	s := &VendorService{db: db, cfg: &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("k", 32)}}}
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	v := &Vendor{ID: 3, Platforms: []string{PlatformOpenAI}, OpenAIAuthorizationModes: []string{VendorOpenAIModeNative}, Groups: []VendorGroup{{ID: 1, Platform: PlatformOpenAI}}, AccountLimit: 1}
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectVendorIdentityKey(mock, strings.Repeat("k", 32))
	mock.ExpectQuery("SELECT canonical_account_id,vendor_id").WillReturnRows(sqlmock.NewRows([]string{"canonical_account_id", "vendor_id"}))
	mock.ExpectQuery("SELECT id,vendor_id,deleted_at").WillReturnRows(sqlmock.NewRows([]string{"id", "vendor_id", "deleted_at"}))
	mock.ExpectQuery("SELECT a.id,a.vendor_id.*account_supply_identity_claims").WillReturnRows(sqlmock.NewRows([]string{"id", "vendor_id", "hash", "attestation", "claimed"}))
	mock.ExpectQuery("SELECT EXISTS.*FROM vendors").WillReturnRows(sqlmock.NewRows([]string{"found", "full"}).AddRow(true, true))
	_, err = s.persistVendorCredential(context.Background(), tx, v, VendorAccountInput{Name: "new", Platform: PlatformOpenAI, Method: "oauth", AuthorizationMode: VendorOpenAIModeNative, GroupIDs: []int64{1}, Direct: true}, &vendorVerifiedCredential{Identity: "verified", AccountID: "verified", Credentials: map[string]any{"access_token": "secret"}}, false)
	require.ErrorContains(t, err, "limit")
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestVendorIdentityUnverifiedLegacyBaselineFailsClosed(t *testing.T) {
	db, mock := vendorTestDB(t)
	s := &VendorService{cfg: &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("k", 32)}}}
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	expectVendorIdentityKey(mock, strings.Repeat("k", 32))
	mock.ExpectQuery("SELECT canonical_account_id,vendor_id").WillReturnRows(sqlmock.NewRows([]string{"canonical_account_id", "vendor_id"}))
	mock.ExpectQuery("SELECT id,vendor_id,deleted_at").WillReturnRows(sqlmock.NewRows([]string{"id", "vendor_id", "deleted_at"}))
	mock.ExpectQuery("SELECT a.id,a.vendor_id.*account_supply_identity_claims").WillReturnRows(sqlmock.NewRows([]string{"id", "vendor_id", "hash", "attestation", "claimed"}).AddRow(10, nil, "", "", false))
	_, _, err = s.reserveVendorIdentity(context.Background(), tx, &Vendor{ID: 3}, VendorAccountInput{Platform: PlatformOpenAI}, &vendorVerifiedCredential{Identity: "verified", AccountID: "verified"})
	require.ErrorIs(t, err, ErrVendorIdentityBound)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestVendorQuotaUnknownAndPlatformWindowsStayDistinct(t *testing.T) {
	now := time.Now()
	v := &Vendor{Status: StatusActive, AllowDirect: true, Platforms: []string{PlatformAnthropic, PlatformOpenAI}}
	a := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"session_window_utilization": 0.6, "passive_usage_7d_oi_utilization": 0.8, "passive_usage_7d_oi_reset": float64(now.Add(time.Hour).Unix())}, SessionWindowEnd: &now}
	q := vendorAccountDTO(a, v).Quota
	require.Equal(t, 60.0, q.FiveHour.Utilization)
	require.Nil(t, q.SevenDay)
	require.Equal(t, 80.0, q.SevenDayFable.Utilization)
	a.Platform = PlatformOpenAI
	q = vendorAccountDTO(a, v).Quota
	require.Nil(t, q.FiveHour)
	require.Nil(t, q.SevenDayFable)
	var distribution VendorQuotaDistribution
	require.False(t, vendorCountQuota(&distribution, nil))
	require.Equal(t, int64(1), distribution.Unknown)
	require.Zero(t, distribution.Normal)
}

func expectVendorIdentityKey(mock sqlmock.Sqlmock, key string) {
	mock.ExpectExec("INSERT INTO settings").WithArgs(vendorIdentityKeyFingerprint(key)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT value FROM settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(vendorIdentityKeyFingerprint(key)))
}
func TestVendorIdentityKeyRotationFailsClosed(t *testing.T) {
	db, mock := vendorTestDB(t)
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	mock.ExpectExec("INSERT INTO settings").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT value FROM settings").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(vendorIdentityKeyFingerprint(strings.Repeat("a", 32))))
	require.Error(t, ValidateSupplyIdentityKey(context.Background(), tx, strings.Repeat("b", 32)))
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVendorAccountImportMappingsRejectAmbiguousRows(t *testing.T) {
	proxy := int64(4)
	input := VendorAccountImportInput{Mappings: []VendorAccountImportMapping{{Index: 0, GroupIDs: []int64{2}, ProxyID: &proxy}, {Index: 1, GroupIDs: []int64{3}, Direct: true, Mode: "update"}}}
	mappings, err := vendorImportMappings(input, 2)
	require.NoError(t, err)
	require.Equal(t, &proxy, mappings[0].ProxyID)
	require.True(t, mappings[1].Direct)
	require.Equal(t, "update", mappings[1].Mode)
	input.Mappings[1].Index = 0
	_, err = vendorImportMappings(input, 2)
	require.Error(t, err)
	input.Mappings[1].Index = 2
	_, err = vendorImportMappings(input, 2)
	require.Error(t, err)
	input.Mappings[1].Index = 1
	input.Mappings[1].Mode = "overwrite"
	_, err = vendorImportMappings(input, 2)
	require.Error(t, err)
}
func TestVendorAccountPendingAuthorizationBindsEditableSnapshot(t *testing.T) {
	a := &Account{Name: "before", Platform: PlatformOpenAI, VendorNotes: "note", GroupIDs: []int64{2, 1}, Credentials: map[string]any{"access_token": "old"}}
	before := vendorAccountEditFingerprint(a)
	a.GroupIDs = []int64{1, 2}
	a.Credentials["access_token"] = "background-rotation"
	require.Equal(t, before, vendorAccountEditFingerprint(a))
	a.Name = "changed"
	require.NotEqual(t, before, vendorAccountEditFingerprint(a))
	a.Name = "before"
	a.ProxyID = new(int64)
	*a.ProxyID = 9
	require.NotEqual(t, before, vendorAccountEditFingerprint(a))
}
func TestVendorAccountDetailsProjectNativeLimitsAndSafeProxy(t *testing.T) {
	owner := int64(2)
	a := &Account{Platform: PlatformOpenAI, Proxy: &Proxy{VendorID: &owner, ID: 3, Name: "own", Protocol: "https", Host: "proxy.example.org", Port: 443, Username: "secret-user", Password: "secret-pass", RegionLabelCountryCode: "TW", RegionLabelCity: "Taipei"}, Extra: map[string]any{}}
	reset := time.Now().Add(time.Hour)
	setAccountModelRateLimitSnapshot(a, "gpt-5.3-codex-spark", reset, "private upstream reason", time.Now())
	dto := vendorAccountDTO(a, &Vendor{ID: owner})
	require.Equal(t, "https://proxy.example.org:443", dto.Proxy.Endpoint)
	require.Equal(t, "TW", dto.Proxy.CountryCode)
	require.Len(t, dto.ModelLimits, 1)
	require.Equal(t, "gpt-5.3-codex-spark", dto.ModelLimits[0].Scope)
	raw, err := json.Marshal(dto)
	require.NoError(t, err)
	for _, secret := range []string{"secret-user", "secret-pass", "private upstream reason"} {
		require.NotContains(t, string(raw), secret)
	}
}

func TestVendorIdentityAttestationBindsExactLegacyOwnerAndRecord(t *testing.T) {
	key := strings.Repeat("k", 32)
	att := vendorSupplyIdentityAttestation(key, 1, nil, PlatformOpenAI, "identity")
	owner := int64(4)
	require.NotEqual(t, att, vendorSupplyIdentityAttestation(key, 2, nil, PlatformOpenAI, "identity"))
	require.NotEqual(t, att, vendorSupplyIdentityAttestation(key, 1, &owner, PlatformOpenAI, "identity"))
	require.NotEqual(t, att, vendorSupplyIdentityAttestation(key, 1, nil, PlatformAnthropic, "identity"))
}
