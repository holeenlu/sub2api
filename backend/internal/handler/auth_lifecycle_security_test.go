package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	"github.com/Wei-Shaw/sub2api/ent/pendingauthsession"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type lifecycleOAuthUserRepo struct{ *oauthPendingFlowUserRepo }

func (r lifecycleOAuthUserRepo) GetByID(ctx context.Context, id int64) (*service.User, error) {
	u, err := r.oauthPendingFlowUserRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	entity, err := r.client.User.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	u.SessionGeneration = entity.SessionGeneration
	return u, nil
}
func lifecycleOAuthHandler(t *testing.T) (*AuthHandler, *dbent.Client, *service.User) {
	h, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{settingValues: map[string]string{service.SettingKeySessionBindingEnabled: "true"}})
	h.userService = service.NewUserService(lifecycleOAuthUserRepo{&oauthPendingFlowUserRepo{client: client}}, nil, nil, nil)
	e, err := client.User.Create().SetEmail("lifecycle@example.com").SetPasswordHash("hash").SetStatus(service.StatusActive).Save(context.Background())
	require.NoError(t, err)
	u, err := h.userService.GetByID(context.Background(), e.ID)
	require.NoError(t, err)
	return h, client, u
}
func lifecycleOAuthContext(method, path string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(method, path, nil)
	c.Request = c.Request.WithContext(service.WithSessionBinding(c.Request.Context(), &service.SessionBinding{IP: "192.0.2.1", UserAgent: "browser"}))
	return c
}
func TestAuthLifecycleOAuthCookieFingerprint(t *testing.T) {
	h, _, u := lifecycleOAuthHandler(t)
	ctx := service.WithSessionBinding(context.Background(), &service.SessionBinding{IP: "192.0.2.1", UserAgent: "browser"})
	token, err := h.authService.GenerateToken(ctx, u)
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		binding *service.SessionBinding
		valid   bool
	}{
		{"same", &service.SessionBinding{IP: "192.0.2.1", UserAgent: "browser"}, true},
		{"different-ip", &service.SessionBinding{IP: "192.0.2.2", UserAgent: "browser"}, false},
		{"different-ua", &service.SessionBinding{IP: "192.0.2.1", UserAgent: "other"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := lifecycleOAuthContext("GET", "/start")
			c.Request = c.Request.WithContext(service.WithSessionBinding(c.Request.Context(), test.binding))
			c.Request.AddCookie(&http.Cookie{Name: oauthBindAccessTokenCookieName, Value: url.QueryEscape(token)})
			_, err := h.resolveOAuthBindTargetUserID(c)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrSessionBindingMismatch)
			}
		})
	}
}
func TestAuthLifecycleOAuthBindCapability(t *testing.T) {
	for _, variant := range []string{"legitimate", "provider", "state", "browser", "expired", "revoked", "replay", "password", "legacy"} {
		t.Run(variant, func(t *testing.T) {
			h, client, u := lifecycleOAuthHandler(t)
			start := lifecycleOAuthContext("GET", "/start")
			token, err := h.authService.GenerateToken(start.Request.Context(), u)
			require.NoError(t, err)
			start.Request.Header.Set("Authorization", "Bearer "+token)
			capability, err := h.buildOAuthBindUserCookieFromContext(start, linuxDoOAuthBindUserCookieName, "state", "browser")
			require.NoError(t, err)
			provider, state, browser := linuxDoOAuthBindUserCookieName, "state", "browser"
			switch variant {
			case "provider":
				provider = oidcOAuthBindUserCookieName
			case "state":
				state = "other"
			case "browser":
				browser = "other"
			case "expired":
				session, err := client.PendingAuthSession.Query().Only(context.Background())
				require.NoError(t, err)
				require.NoError(t, client.PendingAuthSession.UpdateOneID(session.ID).SetExpiresAt(time.Now().Add(-time.Second)).Exec(context.Background()))
			case "revoked":
				require.NoError(t, client.User.UpdateOneID(u.ID).AddSessionGeneration(1).Exec(context.Background()))
			case "legacy":
				capability = buildEncodedOAuthBindUserCookie(t, u.ID, "test-secret")
			case "password":
				require.NoError(t, client.User.UpdateOneID(u.ID).SetPasswordHash("new-hash").Exec(context.Background()))
			}
			callback := func() (int64, error) {
				c := lifecycleOAuthContext("GET", "/callback?state="+state)
				c.Request.AddCookie(&http.Cookie{Name: provider, Value: encodeCookieValue(capability)})
				c.Request.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue(browser)})
				return h.readOAuthBindUserIDFromCookie(c, provider)
			}
			id, err := callback()
			if variant == "legitimate" || variant == "replay" {
				require.NoError(t, err)
				require.Equal(t, u.ID, id)
			} else {
				require.Error(t, err)
			}
			if variant == "replay" {
				_, err = callback()
				require.ErrorIs(t, err, service.ErrPendingAuthSessionConsumed)
			}
		})
	}
}
func TestAuthLifecyclePendingLoginCannotReviveUnlinkedIdentity(t *testing.T) {
	for _, unlink := range []bool{false, true} {
		t.Run(map[bool]string{true: "unlinked", false: "linked"}[unlink], func(t *testing.T) {
			h, client, u := lifecycleOAuthHandler(t)
			ctx := context.Background()
			identity, err := client.AuthIdentity.Create().SetUserID(u.ID).SetProviderType("linuxdo").SetProviderKey("linuxdo").SetProviderSubject("subject").Save(ctx)
			require.NoError(t, err)
			pending := service.NewAuthPendingIdentityService(client)
			session, err := pending.CreatePendingSession(ctx, service.CreatePendingAuthSessionInput{Intent: "login", Identity: service.PendingAuthIdentityKey{ProviderType: "linuxdo", ProviderKey: "linuxdo", ProviderSubject: "subject"}, TargetUserID: &u.ID, BrowserSessionKey: "browser", LocalFlowState: map[string]any{oauthCompletionResponseKey: map[string]any{"redirect": "/dashboard"}}})
			require.NoError(t, err)
			if unlink {
				require.NoError(t, client.AuthIdentity.DeleteOneID(identity.ID).Exec(ctx))
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/exchange", strings.NewReader(`{"adopt_display_name":false,"adopt_avatar":false}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
			c.Request.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("browser")})
			h.ExchangePendingOAuthCompletion(c)
			if unlink {
				require.NotEqual(t, 200, rec.Code)
				n, err := client.AuthIdentity.Query().Count(ctx)
				require.NoError(t, err)
				require.Zero(t, n)
			} else {
				require.Equal(t, 200, rec.Code, rec.Body.String())
				var body map[string]any
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				require.Contains(t, rec.Body.String(), "access_token")
			}
		})
	}
}

// Seed the same credential context supplied by JWT middleware in production.
type lifecycleOAuthBareUserRepo struct{ lifecycleOAuthUserRepo }

func (lifecycleOAuthBareUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func lifecycleSeedAuth(t *testing.T, h *AuthHandler, c *gin.Context, id int64) int64 {
	if h.userService == nil {
		h.userService = service.NewUserService(lifecycleOAuthBareUserRepo{lifecycleOAuthUserRepo{&oauthPendingFlowUserRepo{client: h.entClient()}}}, nil, nil, nil)
	}
	t.Helper()
	ctx := context.Background()
	entity, err := h.entClient().User.Get(ctx, id)
	if dbent.IsNotFound(err) {
		entity, err = h.entClient().User.Create().SetEmail(fmt.Sprintf("seed%d@example.com", id)).SetPasswordHash("hash").Save(ctx)
	}
	require.NoError(t, err)
	token, err := h.authService.GenerateToken(ctx, &service.User{ID: entity.ID, Email: entity.Email, Role: entity.Role, PasswordHash: entity.PasswordHash, Status: entity.Status, SessionGeneration: entity.SessionGeneration})
	require.NoError(t, err)
	c.Request.Header.Set("Authorization", "Bearer "+token)
	return entity.ID
}
func lifecycleBindCookieUser(t *testing.T, h *AuthHandler, value string) int64 {
	t.Helper()
	session, err := h.entClient().PendingAuthSession.Query().Where(pendingauthsession.SessionTokenEQ(value)).Only(context.Background())
	require.NoError(t, err)
	require.NotNil(t, session.TargetUserID)
	return *session.TargetUserID
}
func lifecycleAddBindCapability(t *testing.T, h *AuthHandler, req *http.Request, cookie string, id int64) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req.Clone(req.Context())
	lifecycleSeedAuth(t, h, c, id)
	browser, err := readOAuthPendingBrowserCookie(c)
	require.NoError(t, err)
	capability, err := h.buildOAuthBindUserCookieFromContext(c, cookie, req.URL.Query().Get("state"), browser)
	require.NoError(t, err)
	req.AddCookie(encodedCookie(cookie, capability))
}
func lifecycleAuthorizePendingFixture(t *testing.T, h *AuthHandler, session *dbent.PendingAuthSession) {
	t.Helper()
	ctx := context.Background()
	require.NotNil(t, session.TargetUserID)
	u, err := h.userService.GetByID(ctx, *session.TargetUserID)
	require.NoError(t, err)
	flow := clonePendingMap(session.LocalFlowState)
	flow["credential_version"] = fmt.Sprintf("%d", service.CredentialVersion(u))
	switch session.Intent {
	case oauthIntentBindCurrentUser:
		token, err := h.authService.GenerateToken(ctx, u)
		require.NoError(t, err)
		claims, err := h.authService.ValidateToken(token)
		require.NoError(t, err)
		raw, err := json.Marshal(claims)
		require.NoError(t, err)
		flow["bind_claims"] = string(raw)
	case oauthIntentLogin:
		exists, err := h.entClient().AuthIdentity.Query().Where(authidentity.ProviderTypeEQ(session.ProviderType), authidentity.ProviderKeyEQ(session.ProviderKey), authidentity.ProviderSubjectEQ(session.ProviderSubject)).Exist(ctx)
		require.NoError(t, err)
		if !exists {
			_, err = h.entClient().AuthIdentity.Create().SetUserID(u.ID).SetProviderType(session.ProviderType).SetProviderKey(session.ProviderKey).SetProviderSubject(session.ProviderSubject).Save(ctx)
			require.NoError(t, err)
		}
	}
	require.NoError(t, h.entClient().PendingAuthSession.UpdateOneID(session.ID).SetLocalFlowState(flow).Exec(ctx))
	session.LocalFlowState = flow
}

func TestAuthLifecycleWeChatRepairRejectsStaleUnlinkedSnapshot(t *testing.T) {
	h, client, u := lifecycleOAuthHandler(t)
	ctx := context.Background()
	expected, err := client.User.Get(ctx, u.ID)
	require.NoError(t, err)
	require.NoError(t, client.User.UpdateOneID(u.ID).AddSessionGeneration(1).Exec(ctx))
	err = h.ensureWeChatRuntimeIdentityBinding(ctx, expected, service.PendingAuthIdentityKey{ProviderType: "wechat", ProviderKey: wechatOAuthProviderKey, ProviderSubject: "removed-subject"}, nil)
	require.ErrorIs(t, err, service.ErrTokenRevoked)
	count, err := client.AuthIdentity.Query().Where(authidentity.UserIDEQ(u.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
