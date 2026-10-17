package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type vendorExcelOAuthFixture struct {
	identity *vendorOpenAIIdentityFixture
	oauth    *OpenAIOAuthService
	repo     *excelGrantTestRepo
	account  *Account
}

func newVendorExcelOAuthFixture(t *testing.T) *vendorExcelOAuthFixture {
	t.Helper()
	oauth, repo, account := excelGrantService(t)
	vendorID := int64(7)
	account.VendorID = &vendorID
	account.Credentials["chatgpt_account_id"] = "workspace-signed"
	account.Credentials["chatgpt_user_id"] = "user-signed"
	account.Credentials["email"] = "supplier@example.invalid"
	account.Credentials["plan_type"] = "plus"
	account.Proxy = &Proxy{Protocol: "http", Host: "fixture-proxy.invalid", Port: 8080}
	return &vendorExcelOAuthFixture{newVendorOpenAIIdentityFixture(t), oauth, repo, account}
}

func (f *vendorExcelOAuthFixture) encrypt(t *testing.T, credentials map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(credentials)
	require.NoError(t, err)
	encrypted, err := f.oauth.excelEncryptor.Encrypt(string(raw))
	require.NoError(t, err)
	return encrypted
}

func (f *vendorExcelOAuthFixture) storeVerified(t *testing.T, response *openai.TokenResponse) map[string]any {
	t.Helper()
	verified, err := f.identity.verify(t.Context(), response)
	require.NoError(t, err)
	f.repo.ciphertext = f.encrypt(t, verified.Credentials)
	return verified.Credentials
}

func (f *vendorExcelOAuthFixture) accessToken(ctx context.Context) (string, error) {
	return f.oauth.excelAccessToken(ctx, f.account, nil, func(ctx context.Context, response *openai.TokenResponse, method, proxy, mode string) (*vendorVerifiedCredential, error) {
		// This is the real signature verifier using an instance-local provider
		// fixture. No global cache or production transport is replaced.
		if mode != VendorOpenAIModeBPS || method != "refresh_token" {
			return nil, ErrVendorIdentityUnverified
		}
		return f.identity.service.vendorVerifyOpenAIBPS(ctx, response, method, proxy, f.identity.keys)
	})
}

func TestVendorExcelStoredATOnlyRunsUntilExpiry(t *testing.T) {
	f := newVendorExcelOAuthFixture(t)
	at := f.identity.claims(true)
	at["exp"] = time.Now().Add(time.Minute).Unix() // Inside the normal refresh skew.
	response := f.identity.tokens(t, at, nil)
	response.RefreshToken = ""
	f.storeVerified(t, response)
	before := f.repo.ciphertext
	f.oauth.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		t.Fatal("a valid AT-only grant must not attempt refresh")
		return nil, nil
	}}
	token, err := f.oauth.ExcelAccessToken(t.Context(), f.account, nil)
	require.NoError(t, err)
	require.Equal(t, response.AccessToken, token)
	require.Equal(t, before, f.repo.ciphertext)
	status, err := f.oauth.ExcelAuthorizationStatus(t.Context(), f.account)
	require.NoError(t, err)
	require.Equal(t, true, status["authorized"])
	require.Equal(t, false, status["refresh_required"])
	_, saved, err := f.oauth.readExcelGrant(t.Context(), f.account)
	require.NoError(t, err)
	require.NotContains(t, saved, "id_token")
	require.NotContains(t, saved, "refresh_token")
}

func TestVendorExcelNoRefreshTokenRequiresReauthorization(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "rejected by upstream"}[rejected], func(t *testing.T) {
			f := newVendorExcelOAuthFixture(t)
			if !rejected {
				f.identity.now = f.identity.now.Add(-2 * time.Hour)
			}
			response := f.identity.tokens(t, f.identity.claims(true), nil)
			response.RefreshToken = ""
			credentials := f.storeVerified(t, response)
			if rejected {
				credentials[excelAccessTokenRejectedKey] = true
				f.repo.ciphertext = f.encrypt(t, credentials)
			}
			before := f.repo.ciphertext
			f.oauth.oauthClient = nil // Must not attempt a token exchange with an empty RT.
			token, err := f.oauth.ExcelAccessToken(t.Context(), f.account, nil)
			require.Empty(t, token)
			require.Equal(t, "OPENAI_EXCEL_AUTH_REAUTHORIZE_REQUIRED", infraerrors.Reason(err))
			require.True(t, IsExcelAuthorizationError(err))
			require.Equal(t, before, f.repo.ciphertext)
			status, err := f.oauth.ExcelAuthorizationStatus(t.Context(), f.account)
			require.NoError(t, err)
			require.Equal(t, true, status["refresh_required"])
		})
	}
}

func TestVendorExcelRefreshOptionalIDPreservesIndependentGrant(t *testing.T) {
	for _, test := range []struct {
		name  string
		oldID bool
		newID bool
	}{
		{"no ID Token at either issuance", false, false},
		{"retains historical expired ID Token", true, false},
		{"new ID Token replaces historical token", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newVendorExcelOAuthFixture(t)
			f.identity.now = f.identity.now.Add(-2 * time.Hour)
			oldAT := f.identity.claims(true)
			old := f.identity.tokens(t, oldAT, nil)
			if test.oldID {
				id := f.identity.claims(false)
				sum := sha256.Sum256([]byte(old.AccessToken))
				id["at_hash"] = base64.RawURLEncoding.EncodeToString(sum[:sha256.Size/2])
				old = f.identity.tokens(t, oldAT, id)
			}
			f.storeVerified(t, old)
			primary, err := json.Marshal(f.account.Credentials)
			require.NoError(t, err)
			f.identity.now = time.Now().UTC().Truncate(time.Second)
			nextAT := f.identity.claims(true)
			delete(nextAT, "email") // Refresh must retain already verified display metadata.
			next := f.identity.tokens(t, nextAT, nil)
			next.RefreshToken = ""
			if test.newID {
				next = f.identity.tokens(t, nextAT, f.identity.claims(false))
				next.RefreshToken = "rotated-refresh"
			}
			calls := 0
			f.oauth.oauthClient = &excelRefreshTestClient{refresh: func(_ context.Context, rt, proxy, client string) (*openai.TokenResponse, error) {
				calls++
				require.Equal(t, old.RefreshToken, rt)
				require.Equal(t, openai.ExcelClientID, client)
				return next, nil
			}}
			token, err := f.accessToken(t.Context())
			require.NoError(t, err)
			require.Equal(t, next.AccessToken, token)
			require.Equal(t, 1, calls)
			_, saved, err := f.oauth.readExcelGrant(t.Context(), f.account)
			require.NoError(t, err)
			require.Equal(t, excelFirstNonEmpty(next.IDToken, old.IDToken), excelMapString(saved, "id_token"))
			require.Equal(t, excelFirstNonEmpty(next.RefreshToken, old.RefreshToken), excelMapString(saved, "refresh_token"))
			require.Equal(t, "supplier@example.invalid", saved["email"])
			require.Equal(t, f.identity.now.Add(time.Hour).Format(time.RFC3339), saved["expires_at"])
			after, err := json.Marshal(f.account.Credentials)
			require.NoError(t, err)
			require.Equal(t, primary, after, "refreshing the BPS grant cannot change native credentials")
			token, err = f.oauth.ExcelAccessToken(t.Context(), f.account, nil)
			require.NoError(t, err, "the persisted grant must run without treating old ID Token as fresh proof")
			require.Equal(t, next.AccessToken, token)
			require.Equal(t, 1, calls)
		})
	}
}

func TestVendorExcelRefreshRejectsUntrustedResponseWithoutWriting(t *testing.T) {
	f := newVendorExcelOAuthFixture(t)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		mutate func(*openai.TokenResponse)
	}{
		{"forged access signature", func(r *openai.TokenResponse) {
			r.AccessToken = vendorOpenAISignFixture(t, f.identity.claims(true), other, jwt.SigningMethodRS256, "fixture-key", nil)
		}},
		{"forged ID signature", func(r *openai.TokenResponse) {
			r.IDToken = vendorOpenAISignFixture(t, f.identity.claims(false), other, jwt.SigningMethodRS256, "fixture-key", nil)
		}},
		{"new expired ID Token", func(r *openai.TokenResponse) {
			id := f.identity.claims(false)
			id["exp"] = time.Now().Add(-time.Second).Unix()
			r.IDToken = vendorOpenAISignFixture(t, id, f.identity.key, jwt.SigningMethodRS256, "fixture-key", nil)
		}},
		{"different workspace", func(r *openai.TokenResponse) {
			at := f.identity.claims(true)
			at["https://api.openai.com/auth"].(map[string]any)["chatgpt_account_id"] = "another-workspace"
			r.AccessToken = f.identity.tokens(t, at, nil).AccessToken
		}},
		{"different saved user with no primary user", func(r *openai.TokenResponse) {
			delete(f.account.Credentials, "chatgpt_user_id")
			at := f.identity.claims(true)
			at["https://api.openai.com/auth"].(map[string]any)["chatgpt_user_id"] = "another-user"
			at["https://api.openai.com/auth"].(map[string]any)["user_id"] = "another-user"
			r.AccessToken = f.identity.tokens(t, at, nil).AccessToken
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.identity.now = time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
			f.storeVerified(t, f.identity.tokens(t, f.identity.claims(true), nil))
			before := f.repo.ciphertext
			f.identity.now = time.Now().UTC().Truncate(time.Second)
			next := f.identity.tokens(t, f.identity.claims(true), nil)
			test.mutate(next)
			f.oauth.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) { return next, nil }}
			token, err := f.accessToken(t.Context())
			require.Empty(t, token)
			require.Error(t, err)
			require.Equal(t, before, f.repo.ciphertext)
			require.NotContains(t, err.Error(), next.AccessToken)
		})
	}
}

func TestVendorExcelStoredIdentityAndImportBoundaries(t *testing.T) {
	f := newVendorExcelOAuthFixture(t)
	response := f.identity.tokens(t, f.identity.claims(true), nil)
	response.RefreshToken = ""
	verified := f.storeVerified(t, response)
	for _, key := range []string{"client_id", "chatgpt_account_id", "chatgpt_user_id"} {
		t.Run("stored "+key, func(t *testing.T) {
			credentials := make(map[string]any)
			for k, value := range verified {
				credentials[k] = value
			}
			credentials[key] = "other"
			_, err := decodeStoredExcelOAuthTokenInfo(f.account, credentials, true)
			require.Error(t, err)
			delete(credentials, key)
			_, err = decodeStoredExcelOAuthTokenInfo(f.account, credentials, true)
			require.Error(t, err)
		})
	}
	for _, key := range []string{"chatgpt_account_id", "chatgpt_user_id"} {
		t.Run("account "+key, func(t *testing.T) {
			prior := f.account.Credentials[key]
			f.account.Credentials[key] = "other"
			_, err := decodeStoredExcelOAuthTokenInfo(f.account, verified, true)
			require.Error(t, err)
			f.account.Credentials[key] = prior
		})
	}
	for _, vendor := range []bool{false, true} {
		account := *f.account
		if !vendor {
			account.VendorID = nil
			_, err := decodeStoredExcelOAuthTokenInfo(&account, verified, true)
			require.Error(t, err, "non-supplier private storage retains the original RT/ID requirements")
		}
		for _, missing := range []string{"refresh_token", "id_token"} {
			credentials := excelGrantFixture(t, "workspace-signed", "user-signed", time.Now().Add(time.Hour), "legacy")
			delete(credentials, missing)
			credentials["email"] = "supplier@example.invalid"
			credentials["chatgpt_user_id"] = "user-signed"
			credentials["verified"] = true // An input flag is never a trust grant.
			before := f.repo.ciphertext
			require.Error(t, f.oauth.ImportExcelCredentials(t.Context(), &account, credentials))
			require.Equal(t, before, f.repo.ciphertext)
		}
	}
}

func TestVendorExcelRefreshCASPreservesNewerGrant(t *testing.T) {
	f := newVendorExcelOAuthFixture(t)
	f.identity.now = f.identity.now.Add(-2 * time.Hour)
	f.storeVerified(t, f.identity.tokens(t, f.identity.claims(true), nil))
	f.identity.now = time.Now().UTC().Truncate(time.Second)
	fresh := f.identity.tokens(t, f.identity.claims(true), nil)
	importClaims := f.identity.claims(true)
	importClaims["exp"] = f.identity.now.Add(2 * time.Hour).Unix()
	imported := f.identity.tokens(t, importClaims, nil)
	verified, err := f.identity.verify(t.Context(), imported)
	require.NoError(t, err)
	concurrentGrant := f.encrypt(t, verified.Credentials)
	f.oauth.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) { return fresh, nil }}
	f.repo.beforeStore = func() {
		f.repo.ciphertext = concurrentGrant
		f.repo.beforeStore = nil
	}
	token, err := f.accessToken(t.Context())
	require.NoError(t, err)
	require.Equal(t, imported.AccessToken, token)
	require.Equal(t, concurrentGrant, f.repo.ciphertext)
}

type vendorExcelCiphertextCapture struct {
	value string
	after func(string)
}

func (capture *vendorExcelCiphertextCapture) Match(value driver.Value) bool {
	var ok bool
	capture.value, ok = value.(string)
	if ok && capture.after != nil {
		capture.after(capture.value)
	}
	return ok && capture.value != ""
}

type vendorExcelManualAccountRepo struct {
	AccountRepository
	get func(context.Context, int64) (*Account, error)
}

func (r *vendorExcelManualAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	return r.get(ctx, id)
}

func vendorExcelExpectManualRead(mock sqlmock.Sqlmock, v *Vendor, grantRows *sqlmock.Rows) {
	mock.ExpectQuery("SELECT id FROM vendors WHERE user_id").WithArgs(int64(9)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(v.ID))
	columns := []string{"id", "user_id", "name", "status", "version", "platforms", "account_limit", "proxy_limit", "direct", "concurrency", "priority", "rate", "contact", "settlement_email", "binance", "coefficient", "coefficients", "contract", "created", "updated", "tier", "parent", "email", "username", "modes"}
	mock.ExpectQuery("SELECT v.id,v.user_id").WithArgs(v.ID).WillReturnRows(sqlmock.NewRows(columns).
		AddRow(v.ID, 3, "supplier", StatusActive, v.PolicyVersion, []byte(`["openai"]`), 1000, 1000, true, 15, 1, 1, "", "", "", "1", []byte(`{}`), 1, time.Now(), time.Now(), 1, nil, "supplier@example.invalid", "supplier", []byte(`["native","bps"]`)))
	mock.ExpectQuery("SELECT g.id,g.name,g.platform,vg.is_default").WithArgs(v.ID).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "default"}))
	mock.ExpectQuery("WITH owners AS").WithArgs(v.ID).WillReturnRows(sqlmock.NewRows([]string{"accounts", "proxies", "tree_accounts", "tree_proxies"}).AddRow(1, 0, 1, 0))
	mock.ExpectQuery("SELECT EXISTS.*role='super_admin'").WithArgs(int64(9), int64(1)).WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
	mock.ExpectQuery("SELECT g.account_id,g.ciphertext").WillReturnRows(grantRows)
	mock.ExpectQuery("SELECT COALESCE.*MAX.*ul.id").WillReturnRows(sqlmock.NewRows([]string{"watermark"}).AddRow(0))
	for range 3 {
		mock.ExpectQuery("SELECT ul.account_id,").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	}
}

// Exercise the real manual refresh transactions and Redis locks without a live
// database or OpenAI grant. Only SQL persistence and provider transport are local
// fixtures; JWT signatures and encrypted storage use the production code.
func vendorExcelExpectManualTransaction(t *testing.T, mock sqlmock.Sqlmock, v *Vendor, a *Account, policyChanged bool) {
	t.Helper()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT COALESCE.*parent_vendor_id,id").WithArgs(v.ID).WillReturnRows(sqlmock.NewRows([]string{"root"}).AddRow(v.ID))
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WithArgs(v.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT u.id FROM users u WHERE").WithArgs(v.ID, int64(9)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectQuery("SELECT role,status,session_generation FROM users").WillReturnRows(sqlmock.NewRows([]string{"role", "status", "generation"}).AddRow(authz.SuperAdmin, StatusActive, 1))
	version := v.PolicyVersion
	if policyChanged {
		version++
	}
	mock.ExpectQuery("SELECT policy_version,status FROM vendors").WithArgs(v.ID).WillReturnRows(sqlmock.NewRows([]string{"version", "status"}).AddRow(version, StatusActive))
	if policyChanged {
		return
	}
	mock.ExpectQuery("SELECT COALESCE.*vendor_archived_at.*FOR UPDATE").WithArgs(a.ID, v.ID).WillReturnRows(sqlmock.NewRows([]string{"archived"}).AddRow(false))
	groups, err := pq.Array(a.GroupIDs).Value()
	require.NoError(t, err)
	mock.ExpectQuery("SELECT proxy_id,COALESCE").WithArgs(a.ID).WillReturnRows(sqlmock.NewRows([]string{"proxy", "access", "refresh", "name", "notes", "version", "groups"}).
		AddRow(nil, a.GetCredential("access_token"), a.GetCredential("refresh_token"), a.Name, a.VendorNotes, a.GetExtraString("vendor_authorization_version"), groups))
}

func TestVendorExcelManualRefreshVerifiesNewProofAndPreservesSavedGrant(t *testing.T) {
	for _, scenario := range []string{
		"retain expired ID and old at_hash", "no ID at either issuance", "replace with fresh ID",
		"forged access signature", "forged new ID signature", "expired new ID", "new ID with old at_hash",
		"changed workspace", "changed saved user", "changed primary email",
		"concurrent grant replacement", "policy changed during refresh", "read-only scope", "missing refresh token",
	} {
		t.Run(scenario, func(t *testing.T) {
			f := newVendorExcelOAuthFixture(t)
			f.account.Proxy, f.account.ProxyID = nil, nil
			f.identity.now = f.identity.now.Add(-2 * time.Hour)
			oldAT := f.identity.claims(true)
			old := f.identity.tokens(t, oldAT, nil)
			id := f.identity.claims(false)
			hash := sha256.Sum256([]byte(old.AccessToken))
			id["at_hash"] = base64.RawURLEncoding.EncodeToString(hash[:sha256.Size/2])
			if scenario != "no ID at either issuance" {
				old = f.identity.tokens(t, oldAT, id)
			}
			if scenario == "missing refresh token" {
				old.RefreshToken = ""
			}
			saved := f.storeVerified(t, old)
			saved[excelAccessTokenRejectedKey] = true
			before := f.encrypt(t, saved)
			f.identity.now = time.Now().UTC().Truncate(time.Second)
			factory := f.identity.keys.clientFactory
			f.identity.keys.clientFactory = func(proxy string) (*http.Client, error) {
				require.Empty(t, proxy, "manual refresh preserves the validated direct egress")
				return factory("http://fixture-proxy.invalid:8080")
			}
			nextAT := f.identity.claims(true)
			delete(nextAT, "email")
			switch scenario {
			case "changed workspace":
				nextAT["https://api.openai.com/auth"].(map[string]any)["chatgpt_account_id"] = "different-workspace"
			case "changed saved user":
				delete(f.account.Credentials, "chatgpt_user_id")
				nextAT["https://api.openai.com/auth"].(map[string]any)["chatgpt_user_id"] = "different-user"
				nextAT["https://api.openai.com/auth"].(map[string]any)["user_id"] = "different-user"
			case "changed primary email":
				nextAT["email"] = "different@example.invalid"
			}
			next := f.identity.tokens(t, nextAT, nil)
			next.RefreshToken = ""
			switch scenario {
			case "replace with fresh ID", "expired new ID", "new ID with old at_hash":
				newID := f.identity.claims(false)
				if scenario == "expired new ID" {
					newID["exp"] = f.identity.now.Add(-time.Second).Unix()
				} else if scenario == "new ID with old at_hash" {
					newID["at_hash"] = id["at_hash"]
				}
				next = f.identity.tokens(t, nextAT, newID)
				next.RefreshToken = "rotated-refresh-token"
			case "forged access signature", "forged new ID signature":
				other, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				if scenario == "forged access signature" {
					next.AccessToken = vendorOpenAISignFixture(t, nextAT, other, jwt.SigningMethodRS256, "fixture-key", nil)
				} else {
					next.IDToken = vendorOpenAISignFixture(t, f.identity.claims(false), other, jwt.SigningMethodRS256, "fixture-key", nil)
				}
			}
			db, mock := vendorTestDB(t)
			server := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { rdb.Close() })
			s := &VendorService{db: db, rdb: rdb, openaiOAuth: f.oauth}
			v := &Vendor{ID: *f.account.VendorID, PolicyVersion: 7, AllowDirect: true}
			ctx := authz.WithSubject(t.Context(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
			if scenario == "read-only scope" {
				ctx = authz.WithVendorScope(ctx, authz.VendorScope{VendorID: v.ID, ReadOnly: true})
			}
			noRequest := scenario == "missing refresh token" || scenario == "read-only scope"
			success := scenario == "retain expired ID and old at_hash" || scenario == "no ID at either issuance" || scenario == "replace with fresh ID"
			mock.ExpectQuery("SELECT ciphertext FROM openai_excel_grants.*ciphertext<>''").WithArgs(f.account.ID).WillReturnRows(sqlmock.NewRows([]string{"ciphertext"}).AddRow(before))
			if !noRequest {
				vendorExcelExpectManualTransaction(t, mock, v, f.account, false)
				mock.ExpectCommit()
			}
			grantRows := sqlmock.NewRows([]string{"account_id", "ciphertext"})
			capture := &vendorExcelCiphertextCapture{after: func(ciphertext string) { grantRows.AddRow(f.account.ID, ciphertext) }}
			if success || scenario == "concurrent grant replacement" || scenario == "policy changed during refresh" {
				policyChanged := scenario == "policy changed during refresh"
				vendorExcelExpectManualTransaction(t, mock, v, f.account, policyChanged)
				if !policyChanged {
					current := before
					if scenario == "concurrent grant replacement" {
						current = "newer-ciphertext"
					}
					mock.ExpectQuery("SELECT ciphertext FROM openai_excel_grants").WithArgs(f.account.ID).WillReturnRows(sqlmock.NewRows([]string{"ciphertext"}).AddRow(current))
				}
				if success {
					mock.ExpectExec("INSERT INTO openai_excel_grants").WithArgs(f.account.ID, capture).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectCommit()
					vendorExcelExpectManualRead(mock, v, grantRows)
				} else {
					mock.ExpectRollback()
				}
			}
			locks := []string{"oauth:refresh_lock:" + OpenAITokenCacheKey(f.account), fmt.Sprintf("oauth:refresh_lock:openai:excel:account:%d", f.account.ID)}
			s.accounts = &vendorExcelManualAccountRepo{get: func(_ context.Context, id int64) (*Account, error) {
				require.Equal(t, f.account.ID, id)
				for _, key := range locks {
					require.True(t, server.Exists(key), "preserve the original lock boundary through the final DTO read")
				}
				return f.account, nil
			}}
			requests, verifications := 0, 0
			f.oauth.oauthClient = &excelRefreshTestClient{refresh: func(ctx context.Context, rt, proxy, client string) (*openai.TokenResponse, error) {
				requests++
				require.False(t, noRequest)
				require.Equal(t, old.RefreshToken, rt)
				require.Equal(t, openai.ExcelClientID, client)
				require.Empty(t, proxy)
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				for _, key := range locks {
					require.True(t, server.Exists(key), "both existing refresh locks must cover the provider request")
				}
				return next, nil
			}}
			primary, err := json.Marshal(f.account.Credentials)
			require.NoError(t, err)
			dto, err := s.refreshVendorExcelGrantWithVerifier(ctx, v, f.account, func(ctx context.Context, response *openai.TokenResponse, method, proxy, mode string) (*vendorVerifiedCredential, error) {
				verifications++
				require.Same(t, next, response, "verify the provider response before merging historical credentials")
				require.Equal(t, "refresh_token", method)
				require.Equal(t, VendorOpenAIModeBPS, mode)
				return f.identity.service.vendorVerifyOpenAIBPS(ctx, response, method, proxy, f.identity.keys)
			})
			if success {
				require.NoError(t, err)
				require.Equal(t, f.account.ID, dto.ID)
				require.Equal(t, "active", dto.Authorizations[1].Status)
				plaintext, err := f.oauth.excelEncryptor.Decrypt(capture.value)
				require.NoError(t, err)
				var actual map[string]any
				require.NoError(t, json.Unmarshal([]byte(plaintext), &actual))
				require.Equal(t, next.AccessToken, actual["access_token"])
				require.Equal(t, excelFirstNonEmpty(next.IDToken, old.IDToken), excelMapString(actual, "id_token"))
				require.Equal(t, excelFirstNonEmpty(next.RefreshToken, old.RefreshToken), actual["refresh_token"])
				require.Equal(t, "supplier@example.invalid", actual["email"])
				require.NotContains(t, actual, excelAccessTokenRejectedKey)
				f.repo.ciphertext = capture.value
				token, err := f.oauth.ExcelAccessToken(t.Context(), f.account, nil)
				require.NoError(t, err, "a manually refreshed grant remains runnable with its historical ID Token")
				require.Equal(t, next.AccessToken, token)
			} else {
				require.Error(t, err)
				require.Nil(t, dto)
				require.Empty(t, capture.value, "failed validation or a changed snapshot cannot write credentials")
				switch scenario {
				case "missing refresh token":
					require.ErrorContains(t, err, "reauthorization")
				case "read-only scope":
					require.ErrorIs(t, err, ErrVendorDenied)
				case "concurrent grant replacement", "policy changed during refresh":
					require.ErrorIs(t, err, ErrVendorAccountConflict)
				case "changed workspace", "changed saved user", "changed primary email":
					require.ErrorIs(t, err, ErrVendorIdentityBound)
				default:
					require.ErrorIs(t, err, ErrVendorIdentityUnverified)
				}
				require.NotContains(t, err.Error(), next.AccessToken)
			}
			wantCalls := 1
			if noRequest {
				wantCalls = 0
			}
			require.Equal(t, wantCalls, requests)
			require.Equal(t, wantCalls, verifications)
			after, err := json.Marshal(f.account.Credentials)
			require.NoError(t, err)
			require.Equal(t, primary, after)
			for _, key := range locks {
				require.False(t, server.Exists(key), "release both locks on success and every failure")
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
