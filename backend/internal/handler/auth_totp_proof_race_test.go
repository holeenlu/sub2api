package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

// The gate pauses an actual repository read, not the production handler. Tests
// can revoke credentials after the original proof passed but before its next use.
type totpProofGate struct {
	reached chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func newTotpProofGate(t *testing.T) *totpProofGate {
	t.Helper()
	g := &totpProofGate{reached: make(chan struct{}), resume: make(chan struct{})}
	t.Cleanup(g.release)
	return g
}
func (g *totpProofGate) release() { g.once.Do(func() { close(g.resume) }) }
func (g *totpProofGate) pause(ctx context.Context) error {
	close(g.reached)
	select {
	case <-g.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (g *totpProofGate) wait(t *testing.T) {
	t.Helper()
	select {
	case <-g.reached:
	case <-time.After(10 * time.Second):
		t.Fatal("proof read did not reach the gate")
	}
}

type totpProofUserRepo struct {
	lifecycleOAuthUserRepo
	beforeUserRead func(context.Context) error
	afterEmailRead func(context.Context) error
}

func (r *totpProofUserRepo) GetByID(ctx context.Context, id int64) (*service.User, error) {
	if r.beforeUserRead != nil {
		if err := r.beforeUserRead(ctx); err != nil {
			return nil, err
		}
	}
	return r.lifecycleOAuthUserRepo.GetByID(ctx, id)
}
func (r *totpProofUserRepo) GetByEmail(ctx context.Context, email string) (*service.User, error) {
	user, err := r.oauthPendingFlowUserRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	entity, err := r.client.User.Get(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.SessionGeneration = entity.SessionGeneration
	if r.afterEmailRead != nil {
		if err := r.afterEmailRead(ctx); err != nil {
			return nil, err
		}
	}
	return user, nil
}

type totpProofFixture struct {
	handler      *AuthHandler
	client       *dbent.Client
	cache        *oauthPendingFlowTotpCacheStub
	passwordRepo *totpProofUserRepo
	finalRepo    *totpProofUserRepo
	user         *service.User
	pending      *dbent.PendingAuthSession
}

func newTotpProofFixture(t *testing.T, pending bool) *totpProofFixture {
	t.Helper()
	cache := &oauthPendingFlowTotpCacheStub{}
	h, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{settingValues: map[string]string{service.SettingKeyTotpEnabled: "true"}, totpCache: cache, totpEncryptor: oauthPendingFlowTotpEncryptorStub{}})
	base := lifecycleOAuthUserRepo{&oauthPendingFlowUserRepo{client: client}}
	passwordRepo := &totpProofUserRepo{lifecycleOAuthUserRepo: base}
	finalRepo := &totpProofUserRepo{lifecycleOAuthUserRepo: base}
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "proof-race-secret", AccessTokenExpireMinutes: 60, RefreshTokenExpireDays: 7}}
	h.authService = service.NewAuthService(client, passwordRepo, nil, &oauthPendingFlowRefreshTokenCacheStub{}, cfg, h.settingSvc, nil, nil, nil, nil, nil, nil, nil)
	h.userService = service.NewUserService(finalRepo, nil, nil, nil)
	h.totpService = service.NewTotpService(base, oauthPendingFlowTotpEncryptorStub{}, cache, h.settingSvc, nil, nil)
	hash, err := h.authService.HashPassword("original-password")
	require.NoError(t, err)
	entity, err := client.User.Create().SetEmail("proof@example.com").SetPasswordHash(hash).SetStatus(service.StatusActive).SetRole(service.RoleUser).SetTotpEnabled(true).SetTotpSecretEncrypted("JBSWY3DPEHPK3PXP").Save(context.Background())
	require.NoError(t, err)
	user, err := base.GetByID(context.Background(), entity.ID)
	require.NoError(t, err)
	f := &totpProofFixture{handler: h, client: client, cache: cache, passwordRepo: passwordRepo, finalRepo: finalRepo, user: user}
	if pending {
		f.pending, err = client.PendingAuthSession.Create().SetSessionToken("pending-proof").SetIntent("adopt_existing_user_by_email").SetProviderType("oidc").SetProviderKey("https://issuer.example").SetProviderSubject("proof-subject").SetTargetUserID(user.ID).SetResolvedEmail(user.Email).SetBrowserSessionKey("proof-browser").SetExpiresAt(time.Now().Add(time.Minute)).Save(context.Background())
		require.NoError(t, err)
	}
	return f
}
func (f *totpProofFixture) request(body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/auth", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	if f.pending != nil {
		c.Request.AddCookie(encodedCookie(oauthPendingSessionCookieName, f.pending.SessionToken))
		c.Request.AddCookie(encodedCookie(oauthPendingBrowserCookieName, f.pending.BrowserSessionKey))
	}
	return c, rec
}
func (f *totpProofFixture) passwordLogin() *httptest.ResponseRecorder {
	c, rec := f.request(`{"email":"proof@example.com","password":"original-password","adopt_display_name":false,"adopt_avatar":false}`)
	if f.pending == nil {
		f.handler.Login(c)
	} else {
		f.handler.BindOIDCOAuthLogin(c)
	}
	return rec
}
func (f *totpProofFixture) challenge(t *testing.T) string {
	t.Helper()
	rec := f.passwordLogin()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	data := decodeJSONResponseData(t, rec)
	require.Equal(t, true, data["requires_2fa"])
	token, ok := data["temp_token"].(string)
	require.True(t, ok)
	return token
}
func (f *totpProofFixture) complete(t *testing.T, token string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	code, err := totp.GenerateCode("JBSWY3DPEHPK3PXP", time.Now())
	require.NoError(t, err)
	return f.request(fmt.Sprintf(`{"temp_token":%q,"totp_code":%q}`, token, code))
}
func (f *totpProofFixture) revoke(t *testing.T, kind string) {
	t.Helper()
	update := f.client.User.UpdateOneID(f.user.ID)
	if kind == "password-reset" {
		hash, err := f.handler.authService.HashPassword("replacement-password")
		require.NoError(t, err)
		update = update.SetPasswordHash(hash)
	} else {
		update = update.AddSessionGeneration(1)
	}
	require.NoError(t, update.Exec(context.Background()))
}
func (f *totpProofFixture) assertNoUpgradedTokens(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code == http.StatusOK {
		data := decodeJSONResponseData(t, rec)
		token, ok := data["access_token"].(string)
		if ok {
			claims, err := f.handler.authService.ValidateToken(token)
			require.NoError(t, err)
			current, err := f.passwordRepo.lifecycleOAuthUserRepo.GetByID(context.Background(), f.user.ID)
			require.NoError(t, err)
			t.Logf("stale proof issued JWT accepted by current credentials: %t", f.handler.authService.ValidateAccessSession(context.Background(), claims, current) == nil)
		}
	}
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), `"access_token"`)
	require.NotContains(t, rec.Body.String(), `"refresh_token"`)
}

func TestTotpProofCompletionRace(t *testing.T) {
	for _, flow := range []string{"native", "pending-before-adoption", "pending-after-adoption"} {
		for _, mutation := range []string{"revoke-all", "password-reset"} {
			t.Run(flow+"/"+mutation, func(t *testing.T) {
				f := newTotpProofFixture(t, flow != "native")
				token := f.challenge(t)
				gate := newTotpProofGate(t)
				read := 0
				pauseAt := 1
				if flow == "pending-after-adoption" {
					pauseAt = 2
				}
				f.finalRepo.beforeUserRead = func(ctx context.Context) error {
					read++
					if read == pauseAt {
						return gate.pause(ctx)
					}
					return nil
				}
				c, rec := f.complete(t, token)
				done := make(chan struct{})
				go func() { defer close(done); f.handler.Login2FA(c) }()
				gate.wait(t)
				f.revoke(t, mutation)
				gate.release()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					t.Fatal("2FA handler failed to resume")
				}
				f.assertNoUpgradedTokens(t, rec)
				if flow == "pending-before-adoption" {
					n, err := f.client.AuthIdentity.Query().Where(authidentity.ProviderTypeEQ("oidc")).Count(context.Background())
					require.NoError(t, err)
					require.Zero(t, n)
				}
			})
		}
	}
}

func TestTotpProofPasswordSnapshotRace(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for _, mutation := range []string{"revoke-all", "password-reset"} {
			t.Run(fmt.Sprintf("pending-%t/%s", pending, mutation), func(t *testing.T) {
				f := newTotpProofFixture(t, pending)
				gate := newTotpProofGate(t)
				f.passwordRepo.afterEmailRead = gate.pause
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() { result <- f.passwordLogin() }()
				gate.wait(t)
				f.revoke(t, mutation)
				gate.release()
				var rec *httptest.ResponseRecorder
				select {
				case rec = <-result:
				case <-time.After(10 * time.Second):
					t.Fatal("password handler failed to resume")
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				data := decodeJSONResponseData(t, rec)
				token, ok := data["temp_token"].(string)
				require.True(t, ok)
				session, err := f.cache.GetLoginSession(context.Background(), token)
				require.NoError(t, err)
				require.NotNil(t, session)
				require.Equal(t, service.CredentialVersion(f.user), session.CredentialVersion, "creating the challenge must retain the version that passed password verification")
				_, err = f.handler.totpService.GetLoginSession(context.Background(), token)
				require.ErrorIs(t, err, service.ErrTokenRevoked)
			})
		}
	}
}

func TestTotpProofLegitimateControls(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending-%t", pending), func(t *testing.T) {
			f := newTotpProofFixture(t, pending)
			token := f.challenge(t)
			c, rec := f.complete(t, token)
			f.handler.Login2FA(c)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			data := decodeJSONResponseData(t, rec)
			access, ok := data["access_token"].(string)
			require.True(t, ok)
			require.NotEmpty(t, data["refresh_token"])
			claims, err := f.handler.authService.ValidateToken(access)
			require.NoError(t, err)
			current, err := f.passwordRepo.GetByID(context.Background(), f.user.ID)
			require.NoError(t, err)
			require.NoError(t, f.handler.authService.ValidateAccessSession(context.Background(), claims, current))
			require.Equal(t, service.CredentialVersion(f.user), claims.TokenVersion)
			stored, err := f.cache.GetLoginSession(context.Background(), token)
			require.NoError(t, err)
			require.Nil(t, stored)
			if pending {
				identity, err := f.client.AuthIdentity.Query().Where(authidentity.ProviderTypeEQ("oidc")).Only(context.Background())
				require.NoError(t, err)
				require.Equal(t, f.user.ID, identity.UserID)
				session, err := f.client.PendingAuthSession.Get(context.Background(), f.pending.ID)
				require.NoError(t, err)
				require.NotNil(t, session.ConsumedAt)
			}
		})
	}
}
