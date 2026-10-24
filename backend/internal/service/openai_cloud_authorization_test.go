package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type cloudAuthorizationRepoFixture struct {
	AccountRepository
	account *Account
	binding *OpenAICloudAuthorizationBinding
	writes  int
}

func (r *cloudAuthorizationRepoFixture) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *cloudAuthorizationRepoFixture) GetCloudAuthorization(context.Context, int64) (*OpenAICloudAuthorizationBinding, error) {
	if r.binding == nil {
		return nil, nil
	}
	raw, _ := json.Marshal(r.binding)
	var b OpenAICloudAuthorizationBinding
	_ = json.Unmarshal(raw, &b)
	return &b, nil
}
func (r *cloudAuthorizationRepoFixture) StoreCloudAuthorization(_ context.Context, a *Account, previous string, next *OpenAICloudAuthorizationBinding, credentials map[string]any) (bool, error) {
	if r.binding != nil && (r.binding.Generation != previous || r.binding.Revision != next.Revision) {
		return false, nil
	}
	r.writes++
	raw, _ := json.Marshal(next)
	var b OpenAICloudAuthorizationBinding
	_ = json.Unmarshal(raw, &b)
	b.Revision++
	r.binding = &b
	if credentials != nil {
		copy := *r.account
		copy.Credentials = credentials
		r.account = &copy
	}
	return true, nil
}

type cloudAuthorizationTokenLock struct{ GeminiTokenCache }

func (cloudAuthorizationTokenLock) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}
func (cloudAuthorizationTokenLock) ReleaseRefreshLock(context.Context, string) error { return nil }
func (cloudAuthorizationTokenLock) DeleteAccessToken(context.Context, string) error  { return nil }

type cloudAuthorizationOAuthFixture struct {
	OpenAIOAuthClient
	response   *openai.TokenResponse
	calls      int
	onExchange func()
}

func (c *cloudAuthorizationOAuthFixture) ExchangeCode(_ context.Context, _, _, _, _, client string) (*openai.TokenResponse, error) {
	c.calls++
	if c.onExchange != nil {
		c.onExchange()
	}
	if client != openai.ClientID {
		return nil, errors.New("wrong client")
	}
	return c.response, nil
}

func TestOpenAICloudAuthorizationSignedIdentity(t *testing.T) {
	f := newVendorOpenAIIdentityFixture(t)
	s := &OpenAICloudService{keys: f.keys}
	at, id := f.claims(true), f.claims(false)
	at["client_id"] = openai.ClientID
	id["client_id"] = openai.ClientID
	id["aud"] = []string{openai.ClientID}
	tokens := f.tokens(t, at, id)
	info, subject, err := s.verifyAuthorization(t.Context(), tokens, "http://fixture-proxy.invalid:8080")
	require.NoError(t, err)
	require.Equal(t, "provider-subject", subject)
	require.Equal(t, "workspace-signed", info.ChatGPTAccountID)
	require.Equal(t, openai.ClientID, info.ClientID)
	_, _, err = s.verifyAuthorization(t.Context(), f.tokens(t, f.claims(true), f.claims(false)), "http://fixture-proxy.invalid:8080")
	require.ErrorIs(t, err, ErrOpenAICloudAuthorization, "Excel tokens cannot authorize Cloud")
	id["sub"] = "another-subject"
	_, _, err = s.verifyAuthorization(t.Context(), f.tokens(t, at, id), "http://fixture-proxy.invalid:8080")
	require.ErrorIs(t, err, ErrOpenAICloudAuthorization)
	id["sub"] = "provider-subject"
	id["at_hash"] = "not-the-access-token"
	_, _, err = s.verifyAuthorization(t.Context(), f.tokens(t, at, id), "http://fixture-proxy.invalid:8080")
	require.ErrorIs(t, err, ErrOpenAICloudAuthorization)
}

func TestOpenAICloudAuthorizationCallbackLeaseIdentityAndSingleUse(t *testing.T) {
	for _, scenario := range []string{"success", "wrong account", "wrong state", "disconnected", "identity replaced", "permission revoked during exchange"} {
		t.Run(scenario, func(t *testing.T) {
			f := newVendorOpenAIIdentityFixture(t)
			at, id := f.claims(true), f.claims(false)
			at["client_id"] = openai.ClientID
			id["client_id"] = openai.ClientID
			id["aud"] = []string{openai.ClientID}
			client := &cloudAuthorizationOAuthFixture{response: f.tokens(t, at, id)}
			rdb := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			oauthService := NewOpenAIOAuthService(nil, client, rdb)
			t.Cleanup(oauthService.Stop)
			a := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace-signed", "chatgpt_user_id": "user-signed", "refresh_token": "original-refresh"}}
			repo := &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "generation", Revision: 1, Revoked: true}}
			p := NewOpenAITokenProvider(repo, nil, oauthService)
			p.SetRefreshAPI(NewOAuthRefreshAPI(repo, cloudAuthorizationTokenLock{}), NewOpenAITokenRefresher(oauthService, repo))
			s := NewOpenAICloudService(repo, p, oauthService)
			s.keys = f.keys
			ctx := authz.WithSubject(t.Context(), authz.Subject{UserID: 1, Role: authz.SuperAdmin, SessionGeneration: 1, ManagementRequest: true})
			revoked := false
			ctx = authz.WithRevalidation(ctx, func(context.Context) error {
				if revoked {
					return authz.ErrAuthorizationExpired
				}
				return nil
			})
			session := &openAICloudOAuthSession{AccountID: 7, Workspace: "workspace-signed", UserID: "user-signed", Subject: "provider-subject", Generation: "generation", OAuth: openai.OAuthSession{Authorization: authz.CaptureLease(ctx), State: "state", CodeVerifier: "verifier", RedirectURI: openai.DefaultRedirectURI, ProxyURL: "", CreatedAt: time.Now()}}
			// Public keys are already cached by an explicit signature check, so
			// the callback need not use a fixture-only configured account proxy.
			_, _, err := s.verifyAuthorization(ctx, client.response, "http://fixture-proxy.invalid:8080")
			require.NoError(t, err)
			require.NoError(t, oauth.PutSharedSession(ctx, rdb, "oauth:openai:cloud:session", session, time.Minute))
			input := &OpenAIExchangeCodeInput{SessionID: "session", Code: "code", State: "state"}
			accountID := int64(7)
			switch scenario {
			case "wrong account":
				accountID = 8
			case "wrong state":
				input.State = "wrong"
			case "disconnected":
				repo.binding.Generation = "disconnected"
			case "identity replaced":
				a.Credentials["chatgpt_account_id"] = "replacement"
			case "permission revoked during exchange":
				client.onExchange = func() { revoked = true }
			}
			status, err := s.CompleteAuthorization(ctx, accountID, input)
			if scenario == "success" {
				require.NoError(t, err)
				require.True(t, status.Authorized)
				require.Empty(t, status.LastError)
				require.False(t, status.Enabled)
				require.Equal(t, client.response.RefreshToken, repo.account.GetCredential("refresh_token"))
				require.InDelta(t, time.Now().UnixMilli(), repo.account.GetCredentialAsInt64("_token_version"), 2000, "Cloud must use the native millisecond token-version unit")
				raw, err := json.Marshal(repo.binding)
				require.NoError(t, err)
				require.NotContains(t, string(raw), client.response.RefreshToken)
				require.NotContains(t, string(raw), client.response.AccessToken)
				_, err = s.CompleteAuthorization(ctx, accountID, input)
				require.Error(t, err)
				require.Equal(t, 1, client.calls)
			} else {
				require.Error(t, err)
				require.Zero(t, repo.writes)
				require.Equal(t, "original-refresh", repo.account.GetCredential("refresh_token"))
			}
		})
	}
}

func TestCloudAuthorizationActivationRequiresIdentityNotTestEvidence(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}, Extra: map[string]any{OpenAICloudEnabledKey: true, "openai_excel_bps": true}}
	repo := &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "one", AccountID: "workspace", UserID: "user", Subject: "subject"}}
	svc := NewOpenAICloudService(repo, nil, nil)
	svc.SetModelDiscovery(func(context.Context, *OpenAICloudAccess) ([]string, error) {
		t.Fatal("authorization status must not execute a model test or remote discovery")
		return nil, nil
	})
	status, err := svc.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.Authorized)
	require.True(t, status.Enabled)
	require.False(t, status.RuntimeBlocked)
	require.True(t, a.Extra["openai_excel_bps"].(bool), "Cloud activation preserves the saved BPS selection")
	require.False(t, a.IsExcelBPSEnabled(), "Cloud takes precedence over saved BPS settings")
	a.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080}
	a.Credentials["model_mapping"] = map[string]any{"alias": "model"}
	status, err = svc.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.Enabled, "model/proxy edits do not require capability verification")
	require.NoError(t, svc.BlockRuntime(t.Context(), 1, "old-grant", "unknown_usage"))
	status, err = svc.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.Authorized)
	require.False(t, status.Enabled)
	require.True(t, status.RuntimeBlocked)
	require.NoError(t, svc.Disconnect(t.Context(), 1))
	require.True(t, repo.binding.RuntimeBlocked, "disconnect cannot clear unresolved consumption")
	status, err = svc.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.False(t, status.Authorized)
	require.False(t, status.Enabled)
}
