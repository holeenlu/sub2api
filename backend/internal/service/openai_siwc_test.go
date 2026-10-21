package service

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9" //nolint:depguard // Test-only client exercises shared-session CAS and Redis lease ownership.
	"github.com/stretchr/testify/require"
)

type siwcTestRecord struct{ registration, grant string }
type siwcTestRepo struct {
	AccountRepository
	mu         sync.Mutex
	host       string
	records    map[int64]siwcTestRecord
	beforeSave func()
}

func (r *siwcTestRepo) SIWCInstallationID(_ context.Context, host string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.host == "" {
		r.host = host
	}
	return r.host, nil
}
func (r *siwcTestRepo) GetSIWCRecord(_ context.Context, a *Account) (string, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.records[a.ID]
	return v.registration, v.grant, nil
}
func (r *siwcTestRepo) StoreSIWCRecord(_ context.Context, a *Account, pr, pg, nr, ng string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeSave != nil {
		r.beforeSave()
		r.beforeSave = nil
	}
	v := r.records[a.ID]
	if v.registration != pr || v.grant != pg {
		return false, nil
	}
	r.records[a.ID] = siwcTestRecord{nr, ng}
	return true, nil
}

type siwcTestClient struct {
	openaiOAuthClientRefreshStub
	token  func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error)
	revoke func(context.Context, string, string, string, string) (bool, error)
}

func (c *siwcTestClient) SIWCToken(ctx context.Context, f url.Values, p, n string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
	return c.token(ctx, f, p, n)
}
func (c *siwcTestClient) SIWCRevoke(ctx context.Context, r, a, id, p string) (bool, error) {
	if c.revoke == nil {
		return false, errors.New("unavailable")
	}
	return c.revoke(ctx, r, a, id, p)
}
func siwcFixture(t *testing.T) (*OpenAIOAuthService, *siwcTestRepo, *siwcTestClient, *Account) {
	t.Helper()
	r := &siwcTestRepo{records: map[int64]siwcTestRecord{}}
	c := &siwcTestClient{}
	c.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		return siwcOpaqueToken(), siwcVerifiedIdentity("siwc-user"), nil
	}
	s := NewOpenAIOAuthService(nil, c)
	s.excelAccountRepo = r
	s.excelEncryptor = &liveAttestationAES{key: [32]byte{4}}
	s.SetSIWCAgentName("ActualBrand")
	t.Cleanup(s.Stop)
	a := &Account{ID: 70, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "native-secret", "chatgpt_account_id": "native-workspace", "chatgpt_user_id": "native-user", "email": "native@example.com"}, Extra: map[string]any{OpenAISIWCEnabledKey: true}}
	return s, r, c, a
}
func siwcOpaqueToken() *openai.TokenResponse {
	return &openai.TokenResponse{AccessToken: "opaque-auth-metadata-without-codex-account-claims", RefreshToken: "siwc-refresh", IDToken: "verified-in-client", TokenType: "Bearer", Scope: openai.SIWCScopes, ExpiresIn: 3600}
}
func siwcVerifiedIdentity(subject string) *openai.SIWCIdentity {
	return &openai.SIWCIdentity{RegisteredClaims: jwt.RegisteredClaims{Issuer: openai.SIWCIssuer, Subject: subject}, Email: "independent@example.com"}
}
func siwcCallback(t *testing.T, result *OpenAIAuthURLResult) string {
	t.Helper()
	u, err := url.Parse(result.AuthURL)
	require.NoError(t, err)
	return openai.SIWCRedirectURI + "?" + url.Values{"state": {u.Query().Get("state")}, "code": {"one-time-code"}, "client_id": {"oaiapp_stable"}}.Encode()
}
func authorizeSIWC(t *testing.T, s *OpenAIOAuthService, a *Account) {
	t.Helper()
	v, err := s.BeginSIWCAuthorization(t.Context(), a)
	require.NoError(t, err)
	require.NoError(t, s.CompleteSIWCAuthorization(t.Context(), a, v.SessionID, siwcCallback(t, v)))
}

func TestSIWCFirstBindingOpaqueIdentityAndDurableRegistration(t *testing.T) {
	s, r, c, a := siwcFixture(t)
	ctx := t.Context()
	primary := siwcHash(a.Credentials)
	begin, err := s.BeginSIWCAuthorization(ctx, a)
	require.NoError(t, err)
	u, _ := url.Parse(begin.AuthURL)
	q := u.Query()
	require.Equal(t, openai.SIWCDynamicClientID, q.Get("client_id"))
	require.Equal(t, "ActualBrand", q.Get("agent_name_hint"))
	require.NotEmpty(t, q.Get("ext_agent_host_id"))
	require.NotEmpty(t, q.Get("nonce"))
	require.NotEmpty(t, q.Get("code_challenge"))
	c.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		return nil, nil, &openai.SIWCOAuthError{Code: "invalid_grant"}
	}
	require.Error(t, s.CompleteSIWCAuthorization(ctx, a, begin.SessionID, siwcCallback(t, begin)))
	next, err := s.BeginSIWCAuthorization(ctx, a)
	require.NoError(t, err)
	nu, _ := url.Parse(next.AuthURL)
	require.Equal(t, "oaiapp_stable", nu.Query().Get("client_id"))
	require.Equal(t, q.Get("ext_agent_host_id"), nu.Query().Get("ext_agent_host_id"))
	require.NotEqual(t, q.Get("nonce"), nu.Query().Get("nonce"))
	require.NotEqual(t, q.Get("state"), nu.Query().Get("state"))
	c.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		return siwcOpaqueToken(), siwcVerifiedIdentity("siwc-user"), nil
	}
	require.NoError(t, s.CompleteSIWCAuthorization(ctx, a, next.SessionID, siwcCallback(t, next)))
	token, err := s.SIWCAccessToken(ctx, a, nil)
	require.NoError(t, err)
	require.Equal(t, siwcOpaqueToken().AccessToken, token)
	status, err := s.SIWCAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.True(t, status.Bound)
	require.True(t, status.Registered)
	require.True(t, status.IndependentBinding)
	require.Equal(t, "siwc-user", status.Subject)
	require.Equal(t, primary, siwcHash(a.Credentials))
	require.NotContains(t, r.records[a.ID].grant, token)
	other := *a
	other.ID++
	second, err := s.BeginSIWCAuthorization(ctx, &other)
	require.NoError(t, err)
	su, _ := url.Parse(second.AuthURL)
	require.Equal(t, q.Get("ext_agent_host_id"), su.Query().Get("ext_agent_host_id"))
	require.Error(t, s.CompleteSIWCAuthorization(ctx, a, next.SessionID, siwcCallback(t, next)))
}
func TestSIWCRefreshConcurrencyAndGrantOwnership(t *testing.T) {
	s, _, c, a := siwcFixture(t)
	ctx := t.Context()
	authorizeSIWC(t, s, a)
	identity, err := s.SIWCGrantIdentity(ctx, a)
	require.NoError(t, err)
	reg, grant, r, g, err := s.readSIWCRecord(ctx, a)
	require.NoError(t, err)
	g.ExpiresAt = time.Now().Unix() - 1
	_, _, err = s.saveSIWCRecord(ctx, a, reg, grant, r, g)
	require.NoError(t, err)
	var calls atomic.Int64
	c.token = func(_ context.Context, f url.Values, _ string, n string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		calls.Add(1)
		require.Equal(t, "refresh_token", f.Get("grant_type"))
		require.Equal(t, openai.SIWCResource, f.Get("resource"))
		require.Equal(t, "oaiapp_stable", f.Get("client_id"))
		require.Empty(t, n)
		result := siwcOpaqueToken()
		result.AccessToken = "rotated"
		result.RefreshToken = "rotated-refresh"
		return result, nil, nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.SIWCAccessToken(ctx, a, nil)
			if e == nil && v != "rotated" {
				e = errors.New("wrong token")
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, int64(1), calls.Load())
	after, err := s.SIWCGrantIdentity(ctx, a)
	require.NoError(t, err)
	require.Equal(t, identity, after)
	require.NoError(t, s.rejectSIWCAccessToken(ctx, a, siwcOpaqueToken().AccessToken, nil))
	_, _, _, g, err = s.readSIWCRecord(ctx, a)
	require.NoError(t, err)
	require.False(t, g.Rejected)
	require.NoError(t, s.rejectSIWCAccessToken(ctx, a, "rotated", nil))
	_, _, _, g, err = s.readSIWCRecord(ctx, a)
	require.NoError(t, err)
	require.True(t, g.Rejected)
}
func TestSIWCTerminalRefreshKeepsRegistrationTransientKeepsGrant(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmtBool(terminal), func(t *testing.T) {
			s, _, c, a := siwcFixture(t)
			authorizeSIWC(t, s, a)
			reg, grant, r, g, err := s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			g.ExpiresAt = time.Now().Unix() - 1
			_, _, err = s.saveSIWCRecord(t.Context(), a, reg, grant, r, g)
			require.NoError(t, err)
			c.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
				if terminal {
					return nil, nil, &openai.SIWCOAuthError{Code: "invalid_grant"}
				}
				return nil, nil, errors.New("temporary timeout")
			}
			_, err = s.SIWCAccessToken(t.Context(), a, nil)
			require.Error(t, err)
			_, _, r, g, err = s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			require.Equal(t, "oaiapp_stable", r.ClientID)
			if terminal {
				require.Nil(t, g)
			} else {
				require.NotNil(t, g)
			}
		})
	}
}
func fmtBool(v bool) string {
	if v {
		return "terminal"
	}
	return "transient"
}
func TestSIWCSharedCallbackOwnershipAndRejectedRebinding(t *testing.T) {
	s, r, c, a := siwcFixture(t)
	redisServer := miniredis.RunT(t)
	s.rdb = redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = s.rdb.Close() })
	second := NewOpenAIOAuthService(nil, c, s.rdb)
	second.excelAccountRepo = r
	second.excelEncryptor = s.excelEncryptor
	t.Cleanup(second.Stop)
	actor := authz.Subject{UserID: 9, Role: authz.Admin, PolicyVersion: 1, SessionID: "admin-session", SessionGeneration: 1, Permissions: []string{"accounts.authorize"}}
	ctx := authz.WithSubject(t.Context(), actor)
	begin, err := s.BeginSIWCAuthorization(ctx, a)
	require.NoError(t, err)
	bad := actor
	bad.SessionID = "other-session"
	require.Error(t, second.CompleteSIWCAuthorization(authz.WithSubject(t.Context(), bad), a, begin.SessionID, siwcCallback(t, begin)))
	a.Credentials["access_token"] = "native-rotated"
	a.Credentials["refresh_token"] = "native-refresh-rotated"
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Unix()
	require.NoError(t, second.CompleteSIWCAuthorization(ctx, a, begin.SessionID, siwcCallback(t, begin)))
	owner, err := s.SIWCGrantIdentity(ctx, a)
	require.NoError(t, err)
	again, err := s.BeginSIWCAuthorization(ctx, a)
	require.NoError(t, err)
	c.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		return siwcOpaqueToken(), siwcVerifiedIdentity("different-subscriber"), nil
	}
	require.Error(t, s.CompleteSIWCAuthorization(ctx, a, again.SessionID, siwcCallback(t, again)))
	same, err := s.SIWCGrantIdentity(ctx, a)
	require.NoError(t, err)
	require.Equal(t, owner, same)
	result, err := s.DeleteSIWCAuthorization(ctx, a)
	require.NoError(t, err)
	require.False(t, result.RemoteRevocationConfirmed)
	require.Contains(t, result.Message, "not confirmed")
	status, err := s.SIWCAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.False(t, status.Bound)
	require.True(t, status.Registered)
	_, err = s.SIWCGrantIdentity(ctx, a)
	require.Error(t, err)
	next, err := s.BeginSIWCAuthorization(ctx, a)
	require.NoError(t, err)
	nu, _ := url.Parse(next.AuthURL)
	require.Equal(t, "oaiapp_stable", nu.Query().Get("client_id"))
	require.Equal(t, "siwc-user", status.Subject)
}
func TestSIWCRejectsMalformedCallbackAndStalePrimary(t *testing.T) {
	s, _, _, a := siwcFixture(t)
	begin, err := s.BeginSIWCAuthorization(t.Context(), a)
	require.NoError(t, err)
	callback := siwcCallback(t, begin)
	for _, bad := range []string{strings.Replace(callback, "127.0.0.1:1455", "evil.example", 1), callback + "&state=other", callback + "#fragment", callback + "&iss=https%3A%2F%2Fevil.example", callback + "&iss=", callback + "&iss=https%3A%2F%2Fauth.openai.com&iss=https%3A%2F%2Fauth.openai.com", strings.Replace(callback, "code=one-time-code", "error=access_denied&error=other", 1)} {
		require.Error(t, s.CompleteSIWCAuthorization(t.Context(), a, begin.SessionID, bad))
	}
	for _, key := range []string{"chatgpt_account_id", "chatgpt_user_id", "email", "client_id", openAIAuthModeCredentialKey, openAIAuthModeLegacyCredentialKey} {
		original, exists := a.Credentials[key]
		a.Credentials[key] = "changed-primary"
		require.Error(t, s.CompleteSIWCAuthorization(t.Context(), a, begin.SessionID, callback), key)
		if exists {
			a.Credentials[key] = original
		} else {
			delete(a.Credentials, key)
		}
	}
	session, ok := s.sessionStore.Get(begin.SessionID)
	require.True(t, ok)
	session.CreatedAt = time.Now().Add(-openai.SessionTTL - time.Second)
	require.Error(t, s.CompleteSIWCAuthorization(t.Context(), a, begin.SessionID, callback))
	session.CreatedAt = time.Now()
	a.Credentials["access_token"] = "primary-rotated"
	a.Credentials["refresh_token"] = "primary-refresh-rotated"
	a.Credentials["expires_at"] = time.Now().Add(time.Hour).Unix()
	require.NoError(t, s.CompleteSIWCAuthorization(t.Context(), a, begin.SessionID, callback))
	require.Error(t, s.CompleteSIWCAuthorization(t.Context(), a, begin.SessionID, callback), "callback is still single-use")
	a.Credentials["chatgpt_account_id"] = "changed-primary"
	_, err = s.SIWCAccessToken(t.Context(), a, nil)
	require.Error(t, err)
}

func TestSIWCSelectionSurvivesEligibilityChangesWithoutNativeFallback(t *testing.T) {
	s, _, _, a := siwcFixture(t)
	for _, change := range []func(*Account){func(a *Account) { id := int64(3); a.VendorID = &id }, func(a *Account) { id := int64(3); a.ParentAccountID = &id }, func(a *Account) { a.Type = AccountTypeAPIKey }, func(a *Account) { a.Credentials = map[string]any{"client_id": openai.ExcelClientID} }, func(a *Account) {
		a.Credentials = map[string]any{openAIAuthModeCredentialKey: OpenAIAuthModeAgentIdentity}
	}, func(a *Account) {
		a.Credentials = map[string]any{openAIAuthModeLegacyCredentialKey: "personal_access_token"}
	}} {
		copy := *a
		change(&copy)
		require.True(t, copy.IsSIWCEnabled())
		require.False(t, copy.IsSIWCEligible())
		_, err := s.SIWCAccessToken(t.Context(), &copy, nil)
		require.Error(t, err)
	}
}

func TestSIWCRefreshNeverPublishesTokenAfterConcurrentRevocation(t *testing.T) {
	s, repo, client, a := siwcFixture(t)
	authorizeSIWC(t, s, a)
	registration, grant, r, g, err := s.readSIWCRecord(t.Context(), a)
	require.NoError(t, err)
	g.ExpiresAt = time.Now().Unix() - 1
	_, _, err = s.saveSIWCRecord(t.Context(), a, registration, grant, r, g)
	require.NoError(t, err)
	client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		repo.mu.Lock()
		repo.beforeSave = func() { current := repo.records[a.ID]; current.grant = ""; repo.records[a.ID] = current }
		repo.mu.Unlock()
		next := siwcOpaqueToken()
		next.AccessToken = "must-not-escape"
		return next, nil, nil
	}
	token, owner, err := s.SIWCAccessTokenWithIdentity(t.Context(), a, nil)
	require.Error(t, err)
	require.Empty(t, token)
	require.Empty(t, owner)
	_, _, registrationState, grantState, err := s.readSIWCRecord(t.Context(), a)
	require.NoError(t, err)
	require.NotNil(t, registrationState)
	require.Nil(t, grantState)
}
func TestSIWCRejectDoesNotWaitBehindRefresh(t *testing.T) {
	s, _, _, a := siwcFixture(t)
	authorizeSIWC(t, s, a)
	unlock, err := s.lockSIWCCredentials(t.Context(), a.ID, nil, true)
	require.NoError(t, err)
	defer unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, s.rejectSIWCAccessToken(ctx, a, siwcOpaqueToken().AccessToken, nil))
	require.NoError(t, ctx.Err())
	_, _, _, grant, err := s.readSIWCRecord(t.Context(), a)
	require.NoError(t, err)
	require.False(t, grant.Rejected)
}

func TestSIWCIdentityOnlyGrantIsVisibleButCannotInfer(t *testing.T) {
	s, _, client, a := siwcFixture(t)
	client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		token := siwcOpaqueToken()
		token.Scope = "openid profile email"
		return token, siwcVerifiedIdentity("verified-identity-only"), nil
	}
	authorizeSIWC(t, s, a)
	status, err := s.SIWCAuthorizationStatus(t.Context(), a)
	require.NoError(t, err)
	require.True(t, status.Bound)
	require.True(t, status.IdentityVerified)
	require.False(t, status.DirectAccess)
	require.False(t, status.RefreshRequired)
	reconnect, err := s.BeginSIWCAuthorization(t.Context(), a)
	require.NoError(t, err)
	parsed, _ := url.Parse(reconnect.AuthURL)
	require.Equal(t, "consent", parsed.Query().Get("prompt"))
	require.Empty(t, parsed.Query().Get("force_reconsent"))

	token, owner, err := s.SIWCAccessTokenWithIdentity(t.Context(), a, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "chatgpt.tokens.use.direct")
	require.Empty(t, token)
	require.Empty(t, owner)
	_, err = s.SIWCGrantIdentity(t.Context(), a)
	require.Error(t, err)
}
func TestSIWCRefreshNarrowedScopeIsStoredAndStopsInference(t *testing.T) {
	s, _, client, a := siwcFixture(t)
	authorizeSIWC(t, s, a)
	registration, grant, r, g, err := s.readSIWCRecord(t.Context(), a)
	require.NoError(t, err)
	g.ExpiresAt = time.Now().Unix() - 1
	_, _, err = s.saveSIWCRecord(t.Context(), a, registration, grant, r, g)
	require.NoError(t, err)
	client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
		token := siwcOpaqueToken()
		token.Scope = "openid profile email"
		token.AccessToken = "identity-only-new-token"
		return token, nil, nil
	}
	token, err := s.SIWCAccessToken(t.Context(), a, nil)
	require.Error(t, err)
	require.Empty(t, token)
	status, err := s.SIWCAuthorizationStatus(t.Context(), a)
	require.NoError(t, err)
	require.True(t, status.IdentityVerified)
	require.True(t, status.Bound)
	require.False(t, status.DirectAccess)
	_, _, _, g, err = s.readSIWCRecord(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, "openid profile email", g.Scope)
	require.Equal(t, "identity-only-new-token", g.AccessToken)
}

func TestSIWCDistributedLeaseIsFailClosedAndOwnerChecked(t *testing.T) {
	s, _, _, a := siwcFixture(t)
	server := miniredis.RunT(t)
	s.rdb = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = s.rdb.Close() })
	key := "openai:siwc:account:70"
	require.NoError(t, s.rdb.Set(t.Context(), key, "other-replica", time.Minute).Err())
	release, err := s.lockSIWCCredentials(t.Context(), a.ID, nil, true)
	require.Error(t, err)
	require.Nil(t, release)
	require.NoError(t, s.rdb.Del(t.Context(), key).Err())
	release, err = s.lockSIWCCredentials(t.Context(), a.ID, nil, true)
	require.NoError(t, err)
	require.NoError(t, s.rdb.Set(t.Context(), key, "replacement-owner", time.Minute).Err())
	release()
	require.Equal(t, "replacement-owner", s.rdb.Get(t.Context(), key).Val())
}

func TestSIWCOfficialRefreshTerminalCodesAndClientConfigurationFailures(t *testing.T) {
	for _, code := range []string{"invalid_grant", "invalid_refresh_token", "token_expired", "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused", "invalid_client", "unauthorized_client", "invalid_request", "invalid_scope"} {
		t.Run(code, func(t *testing.T) {
			s, _, client, a := siwcFixture(t)
			authorizeSIWC(t, s, a)
			registration, grant, r, g, err := s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			g.ExpiresAt = time.Now().Unix() - 1
			_, _, err = s.saveSIWCRecord(t.Context(), a, registration, grant, r, g)
			require.NoError(t, err)
			providerError := &openai.SIWCOAuthError{Code: code}
			client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
				return nil, nil, providerError
			}
			_, err = s.SIWCAccessToken(t.Context(), a, nil)
			require.Error(t, err)
			_, _, r, g, err = s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			require.Equal(t, "oaiapp_stable", r.ClientID)
			terminal := code != "invalid_client" && code != "unauthorized_client" && code != "invalid_request" && code != "invalid_scope"
			require.Equal(t, terminal, g == nil)
		})
	}
}

func TestSIWCDeleteCancelsPendingCallbacksWithoutGrant(t *testing.T) {
	for _, registered := range []bool{false, true} {
		t.Run(fmtBool(registered), func(t *testing.T) {
			s, _, client, a := siwcFixture(t)
			if registered {
				authorizeSIWC(t, s, a)
				_, err := s.DeleteSIWCAuthorization(t.Context(), a)
				require.NoError(t, err)
			}
			pending, err := s.BeginSIWCAuthorization(t.Context(), a)
			require.NoError(t, err)
			before, _, registration, grant, err := s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			require.Nil(t, grant)
			beforeGeneration := registration.Generation
			clientID, host := registration.ClientID, registration.HostID
			var exchanges atomic.Int64
			client.token = func(context.Context, url.Values, string, string) (*openai.TokenResponse, *openai.SIWCIdentity, error) {
				exchanges.Add(1)
				return siwcOpaqueToken(), siwcVerifiedIdentity("siwc-user"), nil
			}
			result, err := s.DeleteSIWCAuthorization(t.Context(), a)
			require.NoError(t, err)
			require.False(t, result.Bound)
			after, _, registration, grant, err := s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			require.Nil(t, grant)
			require.NotEqual(t, before, after)
			require.NotEmpty(t, registration.Generation)
			require.NotEqual(t, beforeGeneration, registration.Generation)
			require.Equal(t, clientID, registration.ClientID)
			require.Equal(t, host, registration.HostID)
			require.Error(t, s.CompleteSIWCAuthorization(t.Context(), a, pending.SessionID, siwcCallback(t, pending)))
			require.Zero(t, exchanges.Load(), "disconnected callbacks must not consume an upstream code")
			_, _, registration, grant, err = s.readSIWCRecord(t.Context(), a)
			require.NoError(t, err)
			require.NotNil(t, registration)
			require.Nil(t, grant)
		})
	}
}
