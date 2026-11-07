package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/golang-jwt/jwt/v5"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

func TestVendorOpenAIOwnershipProofDistinguishesStoredID(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	f.service.cfg = &config.Config{JWT: config.JWTConfig{Secret: strings.Repeat("fixture-key", 4)}}
	factory := f.keys.clientFactory
	f.keys.clientFactory = func(proxy string) (*http.Client, error) {
		require.Equal(t, "http://fixture-proxy.invalid:8080#vendor-proxy:7:9:0", proxy)
		return factory("http://fixture-proxy.invalid:8080")
	}
	oldAT := f.tokens(t, f.claims(true), nil)
	oldID := f.claims(false)
	oldID["exp"] = f.now.Add(-time.Hour).Unix()
	oldID["iat"] = f.now.Add(-2 * time.Hour).Unix()
	oldHash := sha256.Sum256([]byte(oldAT.AccessToken))
	oldID["at_hash"] = base64.RawURLEncoding.EncodeToString(oldHash[:sha256.Size/2])
	historicalID := vendorOpenAISignFixture(t, oldID, f.key, jwt.SigningMethodRS256, "fixture-key", nil)
	oldID["exp"] = f.now.Add(time.Hour).Unix()
	historicalHashID := vendorOpenAISignFixture(t, oldID, f.key, jwt.SigningMethodRS256, "fixture-key", nil)
	currentClaims := f.claims(true)
	currentClaims["jti"] = "refreshed-access-token"
	currentAT := f.tokens(t, currentClaims, nil).AccessToken
	for _, test := range []struct {
		name     string
		baseline bool
		stored   bool
		id       string
		wantErr  bool
	}{
		{"baseline ignores historical expired ID and old at_hash", true, false, historicalID, false},
		{"merged refresh retains historical ID", false, true, historicalID, false},
		{"merged refresh retains old at_hash", false, true, historicalHashID, false},
		{"new expired ID is rejected", false, false, historicalID, true},
		{"new ID bound to another AT is rejected", false, false, historicalHashID, true},
		{"new forged ID is rejected", false, false, "forged-id-token", true},
		{"fresh signed ID is verified", false, false, f.tokens(t, currentClaims, f.claims(false)).IDToken, false},
		{"AT-only ownership proof", false, false, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, mock := vendorTestDB(t)
			f.service.db = db
			owner, proxyID := int64(7), int64(9)
			a := &Account{ID: 42, VendorID: &owner, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				ProxyID: &proxyID, Proxy: &Proxy{ID: proxyID, VendorID: &owner, Protocol: "http", Host: "fixture-proxy.invalid", Port: 8080},
				Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": currentAT, "id_token": test.id},
			}
			if !test.baseline {
				mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"enforced"}).AddRow(true))
				if test.id != "" {
					rows := sqlmock.NewRows([]string{"id_token"})
					if test.stored {
						rows.AddRow(test.id)
					}
					mock.ExpectQuery("SELECT COALESCE.*id_token.*vendor_id IS NOT DISTINCT FROM.*client_id").
						WithArgs(a.ID, PlatformOpenAI, AccountTypeOAuth, owner, openai.ExcelClientID).WillReturnRows(rows)
				}
			}
			guard := &nativeSupplyIdentityGuard{f.service}
			proof, err := guard.verifyWithOpenAIProfile(t.Context(), a, test.baseline, func(ctx context.Context, response *openai.TokenResponse, method, proxy, mode string) (*vendorVerifiedCredential, error) {
				require.Equal(t, VendorOpenAIModeBPS, mode)
				require.Equal(t, currentAT, response.AccessToken)
				if test.baseline || test.stored {
					require.Empty(t, response.IDToken)
				} else {
					require.Equal(t, test.id, response.IDToken)
				}
				return f.service.vendorVerifyOpenAIBPS(ctx, response, method, proxy, f.keys)
			})
			if test.wantErr {
				require.ErrorIs(t, err, ErrVendorIdentityUnverified)
				require.Nil(t, proof)
			} else {
				require.NoError(t, err)
				require.Equal(t, "workspace-signed", proof.identityAccount)
				require.Equal(t, VendorSupplyIdentityHash(f.service.cfg.JWT.Secret, PlatformOpenAI, "workspace-signed"), proof.hash)
				require.Equal(t, &owner, proof.vendorID)
				digest, err := vendorSupplyCredentialDigest(a.Credentials)
				require.NoError(t, err)
				require.Equal(t, digest, proof.credentialsDigest, "the proof still binds the entire persisted map")
				require.Equal(t, test.id, a.GetCredential("id_token"), "proof selection cannot erase the stored token")
				a.Credentials["id_token"] = "changed-after-proof"
				require.ErrorIs(t, guard.Claim(t.Context(), nil, a, proof), ErrVendorAccountConflict)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestVendorOpenAIOwnershipBaselineStillVerifiesAccess(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, test := range []struct {
		name   string
		mutate func(jwt.MapClaims)
		key    *rsa.PrivateKey
	}{
		{"expired AT", func(c jwt.MapClaims) { c["exp"] = f.now.Add(-time.Second).Unix() }, f.key},
		{"forged AT", func(jwt.MapClaims) {}, otherKey},
		{"wrong client", func(c jwt.MapClaims) { c["client_id"] = openai.ClientID }, f.key},
		{"missing workspace", func(c jwt.MapClaims) { delete(c, "https://api.openai.com/auth") }, f.key},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := f.claims(true)
			test.mutate(claims)
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"client_id": openai.ExcelClientID, "id_token": f.tokens(t, f.claims(true), f.claims(false)).IDToken,
				"access_token": vendorOpenAISignFixture(t, claims, test.key, jwt.SigningMethodRS256, "fixture-key", nil),
			}}
			proof, err := (&nativeSupplyIdentityGuard{f.service}).verifyWithOpenAIProfile(t.Context(), a, true, func(ctx context.Context, response *openai.TokenResponse, method, _, mode string) (*vendorVerifiedCredential, error) {
				require.Empty(t, response.IDToken)
				require.Equal(t, VendorOpenAIModeBPS, mode)
				return f.service.vendorVerifyOpenAIBPS(ctx, response, method, "http://fixture-proxy.invalid:8080", f.keys)
			})
			require.ErrorIs(t, err, ErrVendorIdentityUnverified)
			require.Nil(t, proof)
		})
	}
}

func TestVendorOpenAIOwnershipStoredIDReadFailsClosed(t *testing.T) {
	db, mock := vendorTestDB(t)
	s := &VendorService{db: db}
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"enforced"}).AddRow(true))
	mock.ExpectQuery("SELECT COALESCE.*id_token").WillReturnError(errors.New("storage unavailable"))
	a := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": "untrusted", "id_token": "untrusted"}}
	proof, err := (&nativeSupplyIdentityGuard{s}).verifyWithOpenAIProfile(t.Context(), a, false, func(context.Context, *openai.TokenResponse, string, string, string) (*vendorVerifiedCredential, error) {
		t.Fatal("cannot treat an unreadable token as trusted historical data")
		return nil, nil
	})
	require.Error(t, err)
	require.Nil(t, proof)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVendorOpenAIOwnershipResolutionKeepsStoredBPSProfile(t *testing.T) {
	db, mock := vendorTestDB(t)
	nativeRequests := 0
	s := &VendorService{db: db, openaiOAuth: &OpenAIOAuthService{privacyClientFactory: func(string) (*req.Client, error) {
		nativeRequests++
		return nil, errors.New("native metadata must not receive BPS proof")
	}}}
	mock.ExpectQuery("SELECT EXISTS.*role='super_admin'").WithArgs(int64(9), int64(1)).WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
	stored, err := json.Marshal(map[string]any{"client_id": openai.ExcelClientID, "chatgpt_account_id": "workspace-signed", "id_token": "historical-id"})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT platform,type,credentials,extra,proxy_id FROM accounts").WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "credentials", "extra", "proxy"}).AddRow(PlatformOpenAI, AccountTypeOAuth, stored, []byte("{}"), nil))
	ctx := authz.WithSubject(t.Context(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	result, err := s.ResolveSupplyIdentity(ctx, 42, "invalid-not-a-jwt", nil, true)
	require.ErrorIs(t, err, ErrVendorIdentityUnverified)
	require.Nil(t, result)
	require.Zero(t, nativeRequests, "resolving a saved BPS account must keep its fixed BPS profile")
	require.NoError(t, mock.ExpectationsWereMet())
}

type vendorOpenAIIdentityTransport func(*http.Request) (*http.Response, error)

func (f vendorOpenAIIdentityTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type vendorOpenAIIdentityFixture struct {
	service *VendorService
	keys    *vendorOpenAIJWKSCache
	key     *rsa.PrivateKey
	now     time.Time
	calls   atomic.Int32
}

func vendorOpenAITestJWK(key *rsa.PublicKey, kid string) map[string]any {
	return map[string]any{
		"kty": "RSA", "kid": kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}
}

func newVendorOpenAIIdentityFixture(t *testing.T) *vendorOpenAIIdentityFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &vendorOpenAIIdentityFixture{
		service: &VendorService{openaiOAuth: &OpenAIOAuthService{}},
		keys:    newVendorOpenAIJWKSCache(), key: key, now: time.Now().UTC().Truncate(time.Second),
	}
	f.keys.now = func() time.Time { return f.now }
	f.serve(t, http.StatusOK, map[string]any{"keys": []any{vendorOpenAITestJWK(&key.PublicKey, "fixture-key")}})
	return f
}

func (f *vendorOpenAIIdentityFixture) serve(t *testing.T, status int, document any) {
	t.Helper()
	body, err := json.Marshal(document)
	require.NoError(t, err)
	f.keys.clientFactory = func(proxy string) (*http.Client, error) {
		require.Equal(t, "http://fixture-proxy.invalid:8080", proxy)
		return &http.Client{Transport: vendorOpenAIIdentityTransport(func(r *http.Request) (*http.Response, error) {
			f.calls.Add(1)
			require.Equal(t, vendorOpenAIJWKSURL, r.URL.String())
			require.Equal(t, http.MethodGet, r.Method)
			require.Empty(t, r.Header.Get("Authorization"), "JWKS fetches must never receive customer tokens")
			require.Empty(t, r.Header.Get("Cookie"))
			deadline, ok := r.Context().Deadline()
			require.True(t, ok)
			require.LessOrEqual(t, time.Until(deadline), vendorOpenAIVerifyTimeout)
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
		})}, nil
	}
}

func (f *vendorOpenAIIdentityFixture) claims(access bool) jwt.MapClaims {
	aud := openai.ExcelClientID
	if access {
		aud = vendorOpenAIAccessAudience
	}
	return jwt.MapClaims{
		"iss": vendorOpenAIIssuer, "aud": []string{aud}, "sub": "provider-subject",
		"iat": f.now.Add(-time.Minute).Unix(), "exp": f.now.Add(time.Hour).Unix(),
		"client_id": openai.ExcelClientID, "email": "supplier@example.invalid",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "workspace-signed", "chatgpt_user_id": "user-signed", "user_id": "user-signed", "chatgpt_plan_type": "plus",
		},
	}
}

func vendorOpenAISignFixture(t *testing.T, claims jwt.MapClaims, key any, method jwt.SigningMethod, kid string, header map[string]any) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	for name, value := range header {
		token.Header[name] = value
	}
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	return raw
}

func (f *vendorOpenAIIdentityFixture) tokens(t *testing.T, at, id jwt.MapClaims) *openai.TokenResponse {
	t.Helper()
	response := &openai.TokenResponse{
		AccessToken:  vendorOpenAISignFixture(t, at, f.key, jwt.SigningMethodRS256, "fixture-key", nil),
		RefreshToken: "fixture-refresh-token", TokenType: "Bearer", ExpiresIn: 10 * 365 * 86400,
	}
	if id != nil {
		response.IDToken = vendorOpenAISignFixture(t, id, f.key, jwt.SigningMethodRS256, "fixture-key", nil)
	}
	return response
}

func (f *vendorOpenAIIdentityFixture) verify(ctx context.Context, response *openai.TokenResponse) (*vendorVerifiedCredential, error) {
	return f.service.vendorVerifyOpenAIBPS(ctx, response, "oauth", "http://fixture-proxy.invalid:8080", f.keys)
}

func TestVendorOpenAIProfileSignedExcelIdentity(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	at, id := f.claims(true), f.claims(false)
	response := f.tokens(t, at, nil)
	sum := sha256.Sum256([]byte(response.AccessToken))
	id["at_hash"] = base64.RawURLEncoding.EncodeToString(sum[:sha256.Size/2])
	response = f.tokens(t, at, id)
	verified, err := f.verify(t.Context(), response)
	require.NoError(t, err)
	require.Equal(t, "workspace-signed", verified.Identity)
	require.Equal(t, verified.Identity, verified.AccountID)
	require.Equal(t, "supplier@example.invalid", verified.Email)
	require.Equal(t, "oauth", verified.Method)
	require.Equal(t, map[string]any{
		"access_token": response.AccessToken, "refresh_token": response.RefreshToken, "id_token": response.IDToken,
		"client_id": openai.ExcelClientID, "chatgpt_account_id": "workspace-signed", "chatgpt_user_id": "user-signed",
		"email": "supplier@example.invalid", "plan_type": "plus", "expires_at": f.now.Add(time.Hour).Format(time.RFC3339),
	}, verified.Credentials, "expiry must come from the signed JWT, not supplied expires_in")
	require.Equal(t, int32(1), f.calls.Load(), "AT and ID Token share the public-key cache")
	for _, withoutWorkspace := range []bool{true, false} {
		id := f.claims(false)
		if withoutWorkspace {
			delete(id, "https://api.openai.com/auth")
		} else {
			id = nil // Refresh responses may omit ID Token; signed AT remains authoritative.
		}
		verified, err = f.verify(t.Context(), f.tokens(t, f.claims(true), id))
		require.NoError(t, err)
		require.Equal(t, "workspace-signed", verified.Identity)
	}
	at = f.claims(true)
	delete(at, "client_id")
	at["azp"] = openai.ExcelClientID
	at["aud"] = vendorOpenAIAccessAudience
	delete(at, "email")
	at["https://api.openai.com/profile"] = map[string]any{"email": "access@example.invalid"}
	verified, err = f.verify(t.Context(), f.tokens(t, at, nil))
	require.NoError(t, err)
	require.Equal(t, "access@example.invalid", verified.Email)
	delete(at, "https://api.openai.com/auth")
	_, err = f.verify(t.Context(), f.tokens(t, at, f.claims(false)))
	require.ErrorIs(t, err, ErrVendorIdentityUnverified, "an ID Token cannot substitute for missing access-token workspace")
}

func TestVendorOpenAIProfileRejectsSignedMaliciousClaims(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	for _, target := range []string{"access", "id"} {
		for _, test := range []struct {
			name   string
			mutate func(jwt.MapClaims)
		}{
			{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "https://attacker.invalid" }},
			{"issuer path", func(c jwt.MapClaims) { c["iss"] = vendorOpenAIIssuer + "/" }},
			{"absent issuer", func(c jwt.MapClaims) { delete(c, "iss") }},
			{"wrong audience", func(c jwt.MapClaims) { c["aud"] = "https://attacker.invalid" }},
			{"absent audience", func(c jwt.MapClaims) { delete(c, "aud") }},
			{"additional audience", func(c jwt.MapClaims) { c["aud"] = append(c["aud"].([]string), "https://attacker.invalid") }},
			{"wrong client", func(c jwt.MapClaims) { c["client_id"] = openai.ClientID }},
			{"wrong authorized party", func(c jwt.MapClaims) { c["azp"] = openai.ClientID }},
			{"client wrong type", func(c jwt.MapClaims) { c["client_id"] = []string{openai.ExcelClientID} }},
			{"expired", func(c jwt.MapClaims) { c["exp"] = f.now.Add(-time.Second).Unix() }},
			{"exact expiry", func(c jwt.MapClaims) { c["exp"] = f.now.Unix() }},
			{"absent expiry", func(c jwt.MapClaims) { delete(c, "exp") }},
			{"zero expiry", func(c jwt.MapClaims) { c["exp"] = 0 }},
			{"expiry before issue", func(c jwt.MapClaims) { c["exp"] = f.now.Add(-2 * time.Minute).Unix() }},
			{"not yet valid", func(c jwt.MapClaims) { c["nbf"] = f.now.Add(time.Minute).Unix() }},
			{"future issued", func(c jwt.MapClaims) { c["iat"] = f.now.Add(time.Minute).Unix() }},
			{"absent issued", func(c jwt.MapClaims) { delete(c, "iat") }},
			{"absent subject", func(c jwt.MapClaims) { delete(c, "sub") }},
			{"subject controls", func(c jwt.MapClaims) { c["sub"] = "user\x00admin" }},
			{"user aliases disagree", func(c jwt.MapClaims) { c["https://api.openai.com/auth"].(map[string]any)["user_id"] = "other" }},
			{"invalid user", func(c jwt.MapClaims) {
				c["https://api.openai.com/auth"].(map[string]any)["chatgpt_user_id"] = " bad-user "
				c["https://api.openai.com/auth"].(map[string]any)["user_id"] = " bad-user "
			}},
		} {
			t.Run(target+"/"+test.name, func(t *testing.T) {
				at, id := f.claims(true), f.claims(false)
				if target == "access" {
					test.mutate(at)
				} else {
					test.mutate(id)
				}
				response := f.tokens(t, at, id)
				verified, err := f.verify(t.Context(), response)
				require.Nil(t, verified)
				require.ErrorIs(t, err, ErrVendorIdentityUnverified)
				require.Equal(t, ErrVendorIdentityUnverified.Error(), err.Error(), "do not expose tokens, claims or provider error details")
			})
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(jwt.MapClaims, jwt.MapClaims)
	}{
		{"missing access client", func(at, _ jwt.MapClaims) { delete(at, "client_id") }},
		{"missing access identity", func(at, _ jwt.MapClaims) { delete(at, "https://api.openai.com/auth") }},
		{"missing workspace", func(at, _ jwt.MapClaims) {
			delete(at["https://api.openai.com/auth"].(map[string]any), "chatgpt_account_id")
		}},
		{"default workspace", func(at, _ jwt.MapClaims) {
			at["https://api.openai.com/auth"].(map[string]any)["chatgpt_account_id"] = "default"
		}},
		{"workspace controls", func(at, _ jwt.MapClaims) {
			at["https://api.openai.com/auth"].(map[string]any)["chatgpt_account_id"] = "ws\x00other"
		}},
		{"missing user", func(at, _ jwt.MapClaims) {
			delete(at["https://api.openai.com/auth"].(map[string]any), "chatgpt_user_id")
			delete(at["https://api.openai.com/auth"].(map[string]any), "user_id")
		}},
		{"subject mismatch", func(_, id jwt.MapClaims) { id["sub"] = "another-subject" }},
		{"workspace mismatch", func(_, id jwt.MapClaims) {
			id["https://api.openai.com/auth"].(map[string]any)["chatgpt_account_id"] = "other-workspace"
		}},
		{"user mismatch", func(_, id jwt.MapClaims) {
			id["https://api.openai.com/auth"].(map[string]any)["chatgpt_user_id"] = "other-user"
			id["https://api.openai.com/auth"].(map[string]any)["user_id"] = "other-user"
		}},
		{"access hash mismatch", func(_, id jwt.MapClaims) { id["at_hash"] = "wrong-hash" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			at, id := f.claims(true), f.claims(false)
			test.mutate(at, id)
			verified, err := f.verify(t.Context(), f.tokens(t, at, id))
			require.Nil(t, verified)
			require.ErrorIs(t, err, ErrVendorIdentityUnverified)
		})
	}
}

func TestVendorOpenAIProfileRejectsForgedSignaturesAndHeaders(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for _, target := range []string{"access", "id"} {
		for _, test := range []struct {
			name   string
			key    any
			method jwt.SigningMethod
			header map[string]any
		}{
			{"forged RSA signature", other, jwt.SigningMethodRS256, nil},
			{"unsigned", jwt.UnsafeAllowNoneSignatureType, jwt.SigningMethodNone, nil},
			{"symmetric key confusion", []byte(f.key.N.String()), jwt.SigningMethodHS256, nil},
			{"unapproved algorithm", f.key, jwt.SigningMethodRS512, nil},
			{"missing kid", f.key, jwt.SigningMethodRS256, map[string]any{"kid": nil}},
			{"unknown kid", f.key, jwt.SigningMethodRS256, map[string]any{"kid": "other-key"}},
			{"key URL", f.key, jwt.SigningMethodRS256, map[string]any{"jku": "http://127.0.0.1/private"}},
			{"certificate URL", f.key, jwt.SigningMethodRS256, map[string]any{"x5u": "http://169.254.169.254/"}},
			{"embedded key", f.key, jwt.SigningMethodRS256, map[string]any{"jwk": vendorOpenAITestJWK(&other.PublicKey, "fixture-key")}},
			{"unsupported critical header", f.key, jwt.SigningMethodRS256, map[string]any{"crit": []string{"b64"}}},
		} {
			t.Run(target+"/"+test.name, func(t *testing.T) {
				response := f.tokens(t, f.claims(true), f.claims(false))
				raw := vendorOpenAISignFixture(t, f.claims(target == "access"), test.key, test.method, "fixture-key", test.header)
				if target == "access" {
					response.AccessToken = raw
				} else {
					response.IDToken = raw
				}
				verified, err := f.verify(t.Context(), response)
				require.Nil(t, verified)
				require.ErrorIs(t, err, ErrVendorIdentityUnverified)
			})
		}
	}
	require.Equal(t, int32(1), f.calls.Load(), "untrusted key IDs must not force unbounded network calls")
}

func TestVendorOpenAIProfileKeyRotationAndExpiry(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	response := f.tokens(t, f.claims(true), f.claims(false))
	_, err := f.verify(t.Context(), response)
	require.NoError(t, err)
	nextKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f.now = f.now.Add(vendorOpenAIKeyRetry)
	f.serve(t, http.StatusOK, map[string]any{"keys": []any{vendorOpenAITestJWK(&nextKey.PublicKey, "rotated-key")}})
	rotated := &openai.TokenResponse{AccessToken: vendorOpenAISignFixture(t, f.claims(true), nextKey, jwt.SigningMethodRS256, "rotated-key", nil)}
	_, err = f.verify(t.Context(), rotated)
	require.NoError(t, err, "new kid triggers a bounded rotation refresh")
	require.Equal(t, int32(2), f.calls.Load())
	_, err = f.verify(t.Context(), response)
	require.ErrorIs(t, err, ErrVendorIdentityUnverified, "removed keys must not survive refresh")
	f.now = f.now.Add(vendorOpenAIKeyTTL)
	f.serve(t, http.StatusServiceUnavailable, map[string]string{"error": "provider unavailable"})
	_, err = f.verify(t.Context(), rotated)
	require.ErrorIs(t, err, ErrVendorIdentityUnverified, "never use stale keys after TTL even for a previously verified JWT")
	_, err = f.verify(t.Context(), rotated)
	require.ErrorIs(t, err, ErrVendorIdentityUnverified)
	require.Equal(t, int32(3), f.calls.Load(), "failed fetch is rate limited without extending key validity")
	f.now = f.now.Add(vendorOpenAIKeyRetry)
	f.serve(t, http.StatusOK, map[string]any{"keys": []any{vendorOpenAITestJWK(&nextKey.PublicKey, "rotated-key")}})
	_, err = f.verify(t.Context(), rotated)
	require.NoError(t, err)
}

func TestVendorOpenAIProfileRejectsInvalidJWKS(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	response := f.tokens(t, f.claims(true), nil)
	for _, test := range []struct {
		name   string
		mutate func(map[string]any) any
	}{
		{"empty set", func(map[string]any) any { return map[string]any{"keys": []any{}} }},
		{"duplicate kid", func(k map[string]any) any { return map[string]any{"keys": []any{k, k}} }},
		{"too many keys", func(k map[string]any) any { return map[string]any{"keys": make([]any, 33)} }},
		{"weak modulus", func(k map[string]any) any { k["n"] = "AQAB"; return map[string]any{"keys": []any{k}} }},
		{"bad modulus", func(k map[string]any) any { k["n"] = "not a modulus"; return map[string]any{"keys": []any{k}} }},
		{"bad exponent", func(k map[string]any) any { k["e"] = "Ag"; return map[string]any{"keys": []any{k}} }},
		{"non signature key", func(k map[string]any) any { k["use"] = "enc"; return map[string]any{"keys": []any{k}} }},
		{"different algorithm", func(k map[string]any) any { k["alg"] = "RS512"; return map[string]any{"keys": []any{k}} }},
		{"wrong operations", func(k map[string]any) any { k["key_ops"] = []string{"sign"}; return map[string]any{"keys": []any{k}} }},
		{"missing kid", func(k map[string]any) any { delete(k, "kid"); return map[string]any{"keys": []any{k}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f.keys = newVendorOpenAIJWKSCache()
			f.serve(t, http.StatusOK, test.mutate(vendorOpenAITestJWK(&f.key.PublicKey, "fixture-key")))
			verified, err := f.verify(t.Context(), response)
			require.Nil(t, verified)
			require.ErrorIs(t, err, ErrVendorIdentityUnverified)
		})
	}
}

func TestVendorOpenAIProfileTransportLimitsAndCancellation(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	response := f.tokens(t, f.claims(true), nil)
	for _, test := range []struct {
		name string
		run  func(*http.Request) (*http.Response, error)
	}{
		{"redirect", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}},
		{"oversize body", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", vendorOpenAIJWKSBodyLimit+1)))}, nil
		}},
		{"oversize length", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, ContentLength: vendorOpenAIJWKSBodyLimit + 1, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}},
		{"bad JSON", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("not JSON"))}, nil
		}},
		{"network error", func(*http.Request) (*http.Response, error) { return nil, errors.New("sensitive-proxy-detail") }},
		{"timeout", func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls int
			cache := newVendorOpenAIJWKSCache()
			cache.clientFactory = func(string) (*http.Client, error) {
				return &http.Client{Transport: vendorOpenAIIdentityTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, vendorOpenAIJWKSURL, r.URL.String(), "never follow a redirect")
					return test.run(r)
				})}, nil
			}
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			verified, err := f.service.vendorVerifyOpenAIBPS(ctx, response, "import", "", cache)
			require.Nil(t, verified)
			require.ErrorIs(t, err, ErrVendorIdentityUnverified)
			require.Equal(t, ErrVendorIdentityUnverified.Error(), err.Error())
			require.Equal(t, 1, calls)
		})
	}
	t.Run("invalid proxy never falls back", func(t *testing.T) {
		cache := newVendorOpenAIJWKSCache()
		_, err := f.service.vendorVerifyOpenAIBPS(t.Context(), response, "import", "not-a-proxy", cache)
		require.ErrorIs(t, err, ErrVendorIdentityUnverified)
	})
	t.Run("canceled caller waiting for keys", func(t *testing.T) {
		cache := newVendorOpenAIJWKSCache()
		require.NoError(t, cache.mu.Lock(t.Context()))
		defer cache.mu.Unlock()
		ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
		defer cancel()
		_, err := f.service.vendorVerifyOpenAIBPS(ctx, response, "oauth", "", cache)
		require.ErrorIs(t, err, ErrVendorIdentityUnverified)
	})
}

func TestVendorOpenAIProfileConcurrentCache(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	response := f.tokens(t, f.claims(true), f.claims(false))
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Go(func() {
			_, err := f.verify(t.Context(), response)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), f.calls.Load())
}

func TestVendorOpenAIProfileDispatchAndInputBounds(t *testing.T) {
	server := newChatGPTBackendTestServer(t, chatGPTBackendTestServerConfig{accountsCheck: map[string]any{"accounts": map[string]any{"workspace-signed": map[string]any{"account": map[string]any{"account_id": "workspace-signed", "plan_type": "plus", "is_default": true}}}}})
	defer server.Close()
	f := newVendorOpenAIIdentityFixture(t)
	f.service.openaiOAuth.privacyClientFactory = newTestPrivacyClientFactory()
	native, err := f.service.vendorVerifyOpenAIProfile(t.Context(), &openai.TokenResponse{AccessToken: "authenticated-native", IDToken: "unsigned"}, "oauth", "", VendorOpenAIModeNative)
	require.NoError(t, err)
	require.Equal(t, "workspace-signed", native.Identity)
	require.Equal(t, openai.ClientID, native.Credentials["client_id"])
	verified, err := f.verify(t.Context(), f.tokens(t, f.claims(true), nil))
	require.NoError(t, err)
	require.Equal(t, native.Identity, verified.Identity, "mode changes must preserve the native ownership key")
	for _, mode := range []string{"", "invalid", VendorOpenAIModeBPS} {
		_, err := f.service.vendorVerifyOpenAIProfile(t.Context(), nil, "oauth", "", mode)
		require.ErrorIs(t, err, ErrVendorIdentityUnverified)
	}
	for _, response := range []*openai.TokenResponse{
		nil, {}, {AccessToken: strings.Repeat("a", vendorOpenAITokenLimit+1)},
		{AccessToken: "malformed"}, {AccessToken: "a.b.c", IDToken: strings.Repeat("a", vendorOpenAITokenLimit+1)},
		{AccessToken: "a.b.c", RefreshToken: strings.Repeat("r", vendorOpenAITokenLimit+1)},
		{AccessToken: "a.b.c", TokenType: "Other"},
	} {
		_, err := f.verify(t.Context(), response)
		require.ErrorIs(t, err, ErrVendorIdentityUnverified)
	}
	_, err = (&VendorService{}).vendorVerifyOpenAIBPS(t.Context(), f.tokens(t, f.claims(true), nil), "oauth", "", f.keys)
	require.ErrorIs(t, err, ErrVendorIdentityUnverified)
}
