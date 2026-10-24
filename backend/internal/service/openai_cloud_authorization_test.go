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
			discovered := false
			if scenario == "success" {
				s.SetModelDiscovery(func(ctx context.Context, _ *OpenAICloudAccess) ([]string, error) {
					// A 401 discovery retry uses this exact native mutex. The
					// callback must release it after persisting credentials.
					ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
					defer cancel()
					lock := p.refreshAPI.getLocalLock(p.executor.CacheKey(a))
					if err := lock.Lock(ctx); err != nil {
						return nil, err
					}
					defer lock.Unlock()
					discovered = true
					return []string{"verified-candidate"}, nil
				})
			}
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
				require.True(t, discovered, "status discovery must be able to enter the native refresh authority")
				require.Empty(t, status.LastError)
				require.Empty(t, status.VerifiedModels)
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

func completeCloudAuthorizationCapabilities() OpenAICloudCapabilities {
	return OpenAICloudCapabilities{Text: true, Tools: true, Usage: true, Settlement: true, ImageInput: true, EncryptedContext: true, WebSearch: true, ImageGeneration: true}
}

func TestOpenAICloudCapabilitiesRequireEveryFact(t *testing.T) {
	complete := completeCloudAuthorizationCapabilities()
	require.False(t, complete.Complete(), "all capabilities still require a verification timestamp")
	complete.VerifiedAt = time.Now().UTC()
	require.True(t, complete.Complete())
	encoded, err := json.Marshal(complete)
	require.NoError(t, err)
	for _, missing := range []string{"text", "tools", "usage", "settlement", "image_input", "encrypted_context", "web_search", "image_generation"} {
		t.Run(missing, func(t *testing.T) {
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &fields))
			require.JSONEq(t, "true", string(fields[missing]), "complete capabilities must publish the stable JSON key")
			delete(fields, missing)
			raw, err := json.Marshal(fields)
			require.NoError(t, err)
			var incomplete OpenAICloudCapabilities
			require.NoError(t, json.Unmarshal(raw, &incomplete))
			require.False(t, incomplete.Complete(), "missing capability must fail scheduling admission")
			a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}}
			repo := &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "one", Revision: 1, AccountID: "workspace", UserID: "user", Subject: "subject", ProtocolVersion: OpenAICloudProtocolVersion, Models: map[string]OpenAICloudCapabilities{}}}
			s := NewOpenAICloudService(repo, nil, nil)
			require.Error(t, s.RecordVerifiedModel(t.Context(), 1, "one", cloudSettingsDigest(a), "model", incomplete))
			require.Zero(t, repo.writes, "an incomplete probe cannot publish a verified model")
			require.Empty(t, repo.binding.Models)
		})
	}
}

func TestOpenAICloudAuthorizationCapabilitiesFailClosed(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}, Extra: map[string]any{OpenAICloudEnabledKey: true, OpenAICloudModelsKey: []string{"model"}}}
	repo := &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "one", Revision: 1, AccountID: "workspace", UserID: "user", Subject: "subject", ProtocolVersion: OpenAICloudProtocolVersion, Models: map[string]OpenAICloudCapabilities{}}}
	s := NewOpenAICloudService(repo, nil, nil)
	require.Error(t, s.RecordVerifiedModel(t.Context(), 1, "one", cloudSettingsDigest(a), "model", OpenAICloudCapabilities{Text: true}))
	status, err := s.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.Authorized)
	require.False(t, status.Enabled)
	require.NoError(t, s.RecordVerifiedModel(t.Context(), 1, "one", cloudSettingsDigest(a), "model", completeCloudAuthorizationCapabilities()))
	require.False(t, repo.binding.Models["model"].VerifiedAt.IsZero(), "RecordVerifiedModel supplies the verification timestamp")
	require.True(t, repo.binding.Models["model"].Complete())
	status, err = s.AuthorizationStatus(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.Equal(t, []string{"model"}, status.VerifiedModels)
	require.NoError(t, s.BlockRuntime(t.Context(), 1, "one", "unknown_usage"))
	require.True(t, repo.binding.RuntimeBlocked)
	require.Error(t, s.RecordVerifiedModel(t.Context(), 1, "one", cloudSettingsDigest(a), "model", completeCloudAuthorizationCapabilities()))
	require.NoError(t, s.Disconnect(t.Context(), 1))
	require.True(t, repo.binding.Revoked)
	require.True(t, repo.binding.RuntimeBlocked, "disconnect cannot erase an unresolved settlement")
	require.NotEqual(t, "one", repo.binding.Generation)
	require.Error(t, s.RecordVerifiedModel(t.Context(), 1, "one", cloudSettingsDigest(a), "model", completeCloudAuthorizationCapabilities()))
}

func TestCloudVerificationRejectsChangedProxyAndPreservesEnableSelection(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "workspace", "chatgpt_user_id": "user"}, Extra: map[string]any{OpenAICloudModelsKey: []string{"model"}}, Proxy: &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080}}
	repo := &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "one", AccountID: "workspace", UserID: "user", Subject: "subject", Models: map[string]OpenAICloudCapabilities{}}}
	svc := NewOpenAICloudService(repo, nil, nil)
	tested := cloudSettingsDigest(a)
	a.Proxy.Host = "127.0.0.2"
	require.Error(t, svc.RecordVerifiedModel(t.Context(), 1, "one", tested, "model", completeCloudAuthorizationCapabilities()))
	require.Zero(t, repo.writes)
	require.NoError(t, svc.RecordVerifiedModel(t.Context(), 1, "one", cloudSettingsDigest(a), "model", completeCloudAuthorizationCapabilities()))
	a.Extra[OpenAICloudEnabledKey] = true
	require.True(t, cloudModelsVerified(a, repo.binding), "selecting/enabling a verified model is not a new transport")
	a.Proxy.Username = "user"
	a.Proxy.Password = "changed"
	require.False(t, cloudModelsVerified(a, repo.binding), "same proxy ID with different contents invalidates evidence")
	require.Equal(t, "gpt-5.1", normalizeOpenAIModelForUpstream(a, "gpt-5.1"))
}
