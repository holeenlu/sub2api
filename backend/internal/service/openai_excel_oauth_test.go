package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type excelGrantTestRepo struct {
	authorizationError string
	AccountRepository
	mu          sync.Mutex
	ciphertext  string
	beforeStore func()
}

type accountModesTestRepo struct {
	AccountRepository
	ids []int64
	err error
}

func (r *accountModesTestRepo) ExcelAuthorizationPresence(_ context.Context, ids []int64) (map[int64]bool, error) {
	r.ids = ids
	return map[int64]bool{1: true, 2: true, 99: true}, r.err
}
func TestAccountAuthorizationModesUsesStoredGrantsNotRoutingSwitches(t *testing.T) {
	repo := &accountModesTestRepo{}
	s := &OpenAIOAuthService{excelAccountRepo: repo}
	parent := int64(1)
	accounts := []*Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "native"}, Extra: map[string]any{"openai_excel_bps": false}},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "excel", "client_id": openai.ExcelClientID}},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "native"}, Extra: map[string]any{"openai_excel_bps": true}},
		{ID: 4, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent},
	}
	got, err := s.AccountAuthorizationModes(t.Context(), accounts)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2, 3}, repo.ids)
	require.Equal(t, []string{"native", "bps"}, got[1])
	require.Equal(t, []string{"bps"}, got[2])
	require.Equal(t, []string{"native"}, got[3])
	require.NotContains(t, got, int64(4))
	require.NotContains(t, got, int64(5))
	require.NotContains(t, got, int64(99))
	repo.err = fmt.Errorf("storage unavailable")
	got, err = s.AccountAuthorizationModes(t.Context(), accounts)
	require.Error(t, err)
	require.Nil(t, got)
}

func (r *excelGrantTestRepo) GetExcelCredentials(context.Context, int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ciphertext, nil
}
func (r *excelGrantTestRepo) StoreExcelCredentials(_ context.Context, a *Account, previous, next string, health *string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.beforeStore != nil {
		r.beforeStore()
		r.beforeStore = nil
	}
	if r.ciphertext != previous {
		return false, nil
	}
	r.ciphertext = next
	if health != nil {
		r.authorizationError = *health
		if a.Extra == nil {
			a.Extra = map[string]any{}
		}
		if *health == "" {
			if _, exists := a.Extra[ExcelBPSAuthorizationErrorKey]; exists {
				delete(a.Extra, ExcelBPSAuthorizationErrorKey)
			}
		} else {
			a.Extra[ExcelBPSAuthorizationErrorKey] = *health
		}
	}
	return true, nil
}
func excelGrantFixture(t *testing.T, workspace, user string, expiration time.Time, suffix string) map[string]any {
	t.Helper()
	jwt := func(claims map[string]any) string {
		raw, err := json.Marshal(claims)
		require.NoError(t, err)
		return "header." + base64.RawURLEncoding.EncodeToString(raw) + "." + suffix
	}
	auth := map[string]any{"chatgpt_account_id": workspace, "chatgpt_user_id": user}
	return map[string]any{"client_id": openai.ExcelClientID, "refresh_token": "refresh-" + suffix, "chatgpt_account_id": workspace,
		"access_token": jwt(map[string]any{"client_id": openai.ExcelClientID, "exp": expiration.Unix(), "https://api.openai.com/auth": auth}),
		"id_token":     jwt(map[string]any{"aud": []string{openai.ExcelClientID}, "email": "test@example.com", "https://api.openai.com/auth": auth})}
}
func excelGrantService(t *testing.T) (*OpenAIOAuthService, *excelGrantTestRepo, *Account) {
	t.Helper()
	r := &excelGrantTestRepo{}
	s := NewOpenAIOAuthService(nil, &openaiOAuthClientRefreshStub{})
	s.excelAccountRepo, s.excelEncryptor = r, &liveAttestationAES{key: [32]byte{1}}
	t.Cleanup(s.Stop)
	a := excelAccount()
	delete(a.Credentials, "client_id")
	a.Credentials["chatgpt_user_id"] = "user"
	a.Credentials["email"] = "test@example.com"
	return s, r, a
}
func TestExcelAuthorizationIdentityIsolationAndRevocation(t *testing.T) {
	s, r, a := excelGrantService(t)
	ctx := t.Context()
	primary, _ := json.Marshal(a.Credentials)
	for _, identity := range [][2]string{{"other", "user"}, {"test-account", "other"}} {
		require.Error(t, s.ImportExcelCredentials(ctx, a, excelGrantFixture(t, identity[0], identity[1], time.Now().Add(time.Hour), "bad")))
		require.Empty(t, r.ciphertext)
	}
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "one")
	require.NoError(t, s.ImportExcelCredentials(ctx, a, grant))
	token, err := s.ExcelAccessToken(ctx, a, nil)
	require.NoError(t, err)
	require.Equal(t, grant["access_token"], token)
	status, err := s.ExcelAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.Equal(t, true, status["authorized"])
	raw, _ := json.Marshal(status)
	require.NotContains(t, string(raw), "token")
	require.NotEqual(t, fmt.Sprint(grant), r.ciphertext)
	gateway := &OpenAIGatewayService{openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: s}}
	token, err = gateway.getExcelBPSAccessToken(ctx, a)
	require.NoError(t, err)
	require.Equal(t, grant["access_token"], token)
	require.NoError(t, s.DeleteExcelAuthorization(ctx, a))
	_, err = gateway.getExcelBPSAccessToken(ctx, a)
	require.True(t, IsExcelAuthorizationError(err))
	after, _ := json.Marshal(a.Credentials)
	require.Equal(t, primary, after, "BPS authorization must never mutate primary Codex credentials")
}

type excelRefreshTestClient struct {
	openaiOAuthClientRefreshStub
	refresh func(context.Context, string, string, string) (*openai.TokenResponse, error)
}

func (c *excelRefreshTestClient) RefreshTokenWithClientID(ctx context.Context, token, proxy, client string) (*openai.TokenResponse, error) {
	return c.refresh(ctx, token, proxy, client)
}
func TestExcelAuthorizationRefreshSerializesAndStale401DoesNotRejectNewGrant(t *testing.T) {
	s, _, a := excelGrantService(t)
	ctx := t.Context()
	old := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Minute), "old")
	next := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "new")
	require.NoError(t, s.ImportExcelCredentials(ctx, a, old))
	var calls atomic.Int32
	s.oauthClient = &excelRefreshTestClient{refresh: func(_ context.Context, rt, proxy, client string) (*openai.TokenResponse, error) {
		calls.Add(1)
		require.Equal(t, openai.ExcelClientID, client)
		require.Equal(t, old["refresh_token"], rt)
		return &openai.TokenResponse{AccessToken: next["access_token"].(string), RefreshToken: next["refresh_token"].(string), IDToken: next["id_token"].(string)}, nil
	}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			token, err := s.ExcelAccessToken(ctx, a, nil)
			require.NoError(t, err)
			require.Equal(t, next["access_token"], token)
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), calls.Load())
	require.NoError(t, s.rejectExcelAccessToken(ctx, a, old["access_token"].(string), nil, ""))
	status, err := s.ExcelAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.Equal(t, false, status["refresh_required"])
	require.NoError(t, s.rejectExcelAccessToken(ctx, a, next["access_token"].(string), nil, ""))
	status, err = s.ExcelAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.Equal(t, true, status["refresh_required"])
	require.Equal(t, false, status["authorized"], "a rejected access token is not a usable authorization")
}
func TestExcelAuthorizationConcurrentImportWinsRefreshCAS(t *testing.T) {
	s, _, a := excelGrantService(t)
	ctx := t.Context()
	old := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Minute), "old")
	imported := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "imported")
	stale := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "stale")
	require.NoError(t, s.ImportExcelCredentials(ctx, a, old))
	s.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		require.NoError(t, s.ImportExcelCredentials(ctx, a, imported))
		return &openai.TokenResponse{AccessToken: stale["access_token"].(string), RefreshToken: stale["refresh_token"].(string), IDToken: stale["id_token"].(string)}, nil
	}}
	token, err := s.ExcelAccessToken(ctx, a, nil)
	require.NoError(t, err)
	require.Equal(t, imported["access_token"], token)
}
func TestExcelAuthorizationProfilePinsClientRedirectAndPKCE(t *testing.T) {
	s, _, _ := excelGrantService(t)
	auth, err := s.GenerateAuthURL(t.Context(), nil, "https://untrusted.example", PlatformOpenAI, "excel")
	require.NoError(t, err)
	u, err := url.Parse(auth.AuthURL)
	require.NoError(t, err)
	q := u.Query()
	require.Equal(t, openai.ExcelClientID, q.Get("client_id"))
	require.Equal(t, openai.ExcelRedirectURI, q.Get("redirect_uri"))
	require.Equal(t, "S256", q.Get("code_challenge_method"))
	require.NotEmpty(t, q.Get("state"))
	require.NotEmpty(t, q.Get("code_challenge"))
}

func TestExcelAuthorizationPrimaryAndMissingProvider(t *testing.T) {
	s, r, a := excelGrantService(t)
	_, err := (&OpenAIGatewayService{}).getExcelBPSAccessToken(t.Context(), a)
	require.True(t, IsExcelAuthorizationError(err))
	a.Credentials["client_id"] = openai.ExcelClientID
	status, err := s.ExcelAuthorizationStatus(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, "primary", status["source"])
	require.Equal(t, true, status["authorized"])
	require.Error(t, s.DeleteExcelAuthorization(t.Context(), a))
	require.Error(t, s.ImportExcelCredentials(t.Context(), a, nil))
	require.Empty(t, r.ciphertext)
	_, _, err = (&OpenAIGatewayService{}).GetRequestCredential(t.Context(), nil, a, "gpt-6-astra")
	require.True(t, IsExcelAuthorizationError(err), "primary Excel credentials must never enter native routing")
}

func useExcelBPSMixedGrant(t *testing.T, gateway *OpenAIGatewayService, account *Account) {
	t.Helper()
	delete(account.Credentials, "client_id")
	oauth, _, _ := excelGrantService(t)
	grant := excelGrantFixture(t, account.GetChatGPTAccountID(), "user", time.Now().Add(time.Hour), "mixed")
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	gateway.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth, metrics: &openAITokenRuntimeMetricsStore{}}
}

func TestExcelAuthorizationPermanentFailureStopsOnlyBPSAndReauthorizationRestores(t *testing.T) {
	s, r, a := excelGrantService(t)
	a.Extra = map[string]any{"openai_excel_bps": true}
	a.Status, a.Schedulable = StatusActive, true
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Minute), "bad")
	require.NoError(t, s.ImportExcelCredentials(t.Context(), a, grant))
	var calls int
	s.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
		calls++
		return nil, OpenAIOAuthTokenRefreshFailure(400, []byte(`{"error":"invalid_grant"}`))
	}}
	_, err := s.ExcelAccessToken(t.Context(), a, nil)
	require.Equal(t, "OPENAI_EXCEL_AUTH_REAUTHORIZE_REQUIRED", infraerrors.Reason(err))
	require.Equal(t, "invalid_grant", r.authorizationError)
	require.False(t, a.IsExcelBPSEnabled())
	require.Equal(t, StatusActive, a.Status)
	require.True(t, a.Schedulable)
	status, err := s.ExcelAuthorizationStatus(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, false, status["authorized"])
	require.Equal(t, "invalid_grant", status["authorization_error"])
	_, err = s.ExcelAccessToken(t.Context(), a, nil)
	require.Error(t, err)
	require.Equal(t, 1, calls, "known revoked grant must not hit the token endpoint repeatedly")
	require.NoError(t, s.ImportExcelCredentials(t.Context(), a, excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "new")))
	require.True(t, a.IsExcelBPSEnabled())
	status, err = s.ExcelAuthorizationStatus(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, true, status["authorized"])
}

func TestExcelAuthorizationRefreshFailurePreservesTransientAndConcurrentGrant(t *testing.T) {
	for _, kind := range []string{"transport", "provider_client", "server_error", "reauthorized"} {
		t.Run(kind, func(t *testing.T) {
			s, r, a := excelGrantService(t)
			a.Extra = map[string]any{"openai_excel_bps": true}
			require.NoError(t, s.ImportExcelCredentials(t.Context(), a, excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Minute), "old")))
			newer := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "new")
			s.oauthClient = &excelRefreshTestClient{refresh: func(context.Context, string, string, string) (*openai.TokenResponse, error) {
				switch kind {
				case "reauthorized":
					require.NoError(t, s.ImportExcelCredentials(t.Context(), a, newer))
					return nil, OpenAIOAuthTokenRefreshFailure(400, []byte(`{"error":"invalid_grant"}`))
				case "provider_client":
					return nil, OpenAIOAuthTokenRefreshFailure(400, []byte(`{"error":"invalid_client"}`))
				case "server_error":
					return nil, OpenAIOAuthTokenRefreshFailure(503, []byte(`{"error":"invalid_grant"}`))
				default:
					return nil, fmt.Errorf("network unavailable")
				}
			}}
			token, err := s.ExcelAccessToken(t.Context(), a, nil)
			if kind == "reauthorized" {
				require.NoError(t, err)
				require.Equal(t, newer["access_token"], token)
			} else {
				require.Error(t, err)
			}
			require.Empty(t, r.authorizationError)
			require.True(t, a.IsExcelBPSEnabled())
		})
	}
}

func TestExcelPrimaryCredentialRejectionPreservesNewAuthorization(t *testing.T) {
	for _, rotated := range []bool{false, true} {
		t.Run(fmt.Sprint(rotated), func(t *testing.T) {
			a := nativeRefreshTestAccount()
			a.Credentials["client_id"] = openai.ExcelClientID
			repo := &refreshAPIAccountRepo{account: snapshotOAuthRefreshAccount(a)}
			if rotated {
				repo.account.Credentials["access_token"] = "new-at"
			}
			svc := &OpenAIGatewayService{accountRepo: repo}
			svc.handleExcelBPSUnauthorized(t.Context(), a, 403, nil, []byte(`{"error":{"code":"account_deactivated"}}`), "old-at")
			if rotated {
				require.Equal(t, StatusActive, repo.account.Status)
			} else {
				require.Equal(t, StatusError, repo.account.Status)
			}
		})
	}
}
