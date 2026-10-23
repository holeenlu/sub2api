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

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type excelGrantTestRepo struct {
	AccountRepository
	mu          sync.Mutex
	ciphertext  string
	beforeStore func()
}

func (r *excelGrantTestRepo) GetExcelCredentials(context.Context, int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ciphertext, nil
}
func (r *excelGrantTestRepo) StoreExcelCredentials(_ context.Context, _ *Account, previous, next string) (bool, error) {
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
	require.NoError(t, s.rejectExcelAccessToken(ctx, a, old["access_token"].(string), nil))
	status, err := s.ExcelAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.Equal(t, false, status["refresh_required"])
	require.NoError(t, s.rejectExcelAccessToken(ctx, a, next["access_token"].(string), nil))
	status, err = s.ExcelAuthorizationStatus(ctx, a)
	require.NoError(t, err)
	require.Equal(t, true, status["refresh_required"])
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
