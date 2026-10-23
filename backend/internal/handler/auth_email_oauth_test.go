package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/authidentity"
	dbuser "github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEmailOAuthCallbackExistingEmailLogsInWhenInvitationEnabled(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, true)
	ctx := context.Background()

	user, err := client.User.Create().
		SetEmail("existing@example.com").
		SetUsername("existing").
		SetPasswordHash("hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/google/callback", nil)

	handler.emailOAuthCallbackWithProfile(c, "google", config.EmailOAuthProviderConfig{
		Enabled:             true,
		ClientID:            "google-client",
		ClientSecret:        "google-secret",
		RedirectURL:         "https://app.example/api/v1/auth/oauth/google/callback",
		FrontendRedirectURL: "/auth/oauth/callback",
	}, "/auth/oauth/callback", "/dashboard", &emailOAuthProfile{
		Subject:       "google-123",
		Email:         "existing@example.com",
		EmailVerified: true,
		Username:      "existing",
	})

	require.Equal(t, http.StatusFound, recorder.Code)
	location := recorder.Header().Get("Location")
	require.Contains(t, location, "access_token=")
	require.Contains(t, location, "redirect=%252Fdashboard")

	sessionCount, err := client.PendingAuthSession.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, sessionCount)

	identityCount, err := client.AuthIdentity.Query().Where(
		authidentity.ProviderTypeEQ("google"),
		authidentity.ProviderSubjectEQ("google-123"),
	).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, identityCount)
	_ = user
}

func TestEmailOAuthCallbackCreatesPasswordRegistrationSessionForNewEmail(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{
		settingValues: map[string]string{},
	})
	ctx := context.Background()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/callback", nil)
	c.Request = req

	handler.emailOAuthCallbackWithProfile(c, "github", config.EmailOAuthProviderConfig{
		Enabled:             true,
		ClientID:            "github-client",
		ClientSecret:        "github-secret",
		RedirectURL:         "https://app.example/api/v1/auth/oauth/github/callback",
		FrontendRedirectURL: "/auth/oauth/callback",
	}, "/auth/oauth/callback", "/dashboard", &emailOAuthProfile{
		Subject:       "github-aff-user",
		Email:         "aff-user@example.com",
		EmailVerified: true,
		Username:      "aff-user",
	})

	require.Equal(t, http.StatusFound, recorder.Code)
	require.NotContains(t, recorder.Header().Get("Location"), "access_token=")
	userCount, err := client.User.Query().Where(dbuser.EmailEQ("aff-user@example.com")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, userCount)

	session, err := client.PendingAuthSession.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "aff-user@example.com", session.ResolvedEmail)

	completion, ok := readCompletionResponse(session.LocalFlowState)
	require.True(t, ok)
	require.Equal(t, oauthPendingChoiceStep, completion["step"])
	require.Equal(t, "registration_completion_required", completion["error"])
	require.Equal(t, true, completion["create_account_allowed"])
	require.Equal(t, true, completion["force_email_on_signup"])
	require.Equal(t, "aff-user@example.com", completion["resolved_email"])
}

func TestEmailOAuthStartPreservesPromoCodeInPendingSession(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandlerWithDependencies(t, oauthPendingFlowTestHandlerOptions{
		settingValues: map[string]string{
			service.SettingKeyGitHubOAuthEnabled:      "true",
			service.SettingKeyGitHubOAuthClientID:     "github-client",
			service.SettingKeyGitHubOAuthClientSecret: "github-secret",
			service.SettingKeyGitHubOAuthRedirectURL:  "https://app.example/api/v1/auth/oauth/github/callback",
		},
	})
	ctx := context.Background()

	startRecorder := httptest.NewRecorder()
	startCtx, _ := gin.CreateTestContext(startRecorder)
	startCtx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/start?promo_code=WELCOME2024", nil)

	handler.GitHubOAuthStart(startCtx)

	require.Equal(t, http.StatusFound, startRecorder.Code)
	promoCookie := findCookie(startRecorder.Result().Cookies(), oauthPromoCodeCookieName)
	require.NotNil(t, promoCookie)
	require.Equal(t, "WELCOME2024", decodeCookieValueForTest(t, promoCookie.Value))

	callbackRecorder := httptest.NewRecorder()
	callbackCtx, _ := gin.CreateTestContext(callbackRecorder)
	callbackReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/github/callback", nil)
	callbackReq.AddCookie(promoCookie)
	callbackCtx.Request = callbackReq

	handler.emailOAuthCallbackWithProfile(callbackCtx, "github", config.EmailOAuthProviderConfig{
		FrontendRedirectURL: "/auth/oauth/callback",
	}, "/auth/oauth/callback", "/dashboard", &emailOAuthProfile{
		Subject:       "github-promo-user",
		Email:         "promo-user@example.com",
		EmailVerified: true,
		Username:      "promo-user",
	})

	require.Equal(t, http.StatusFound, callbackRecorder.Code)
	session, err := client.PendingAuthSession.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "WELCOME2024", pendingOAuthPromoCode(session))
}

func TestCompleteEmailOAuthRegistrationRequiresPassword(t *testing.T) {
	handler, client := newOAuthPendingFlowTestHandler(t, false)
	ctx := context.Background()

	session, err := client.PendingAuthSession.Create().
		SetSessionToken("email-oauth-password-session-token").
		SetIntent(oauthIntentLogin).
		SetProviderType("github").
		SetProviderKey("github").
		SetProviderSubject("github-password-user").
		SetResolvedEmail("password-required@example.com").
		SetRedirectTo("/dashboard").
		SetBrowserSessionKey("browser-password-key").
		SetUpstreamIdentityClaims(map[string]any{
			"email":            "password-required@example.com",
			"email_verified":   true,
			"username":         "password-required",
			"provider":         "github",
			"provider_key":     "github",
			"provider_subject": "github-password-user",
		}).
		SetLocalFlowState(map[string]any{
			"step":  oauthPendingChoiceStep,
			"error": "registration_completion_required",
		}).
		SetExpiresAt(time.Now().UTC().Add(10 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oauth/github/complete-registration", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: oauthPendingSessionCookieName, Value: encodeCookieValue(session.SessionToken)})
	req.AddCookie(&http.Cookie{Name: oauthPendingBrowserCookieName, Value: encodeCookieValue("browser-password-key")})
	c.Request = req

	handler.completeEmailOAuthRegistration(c, "github")

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	userCount, err := client.User.Query().Where(dbuser.EmailEQ("password-required@example.com")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, userCount)
}

func TestParseGitHubOAuthProfileRejectsPublicEmailWhenEmailsEndpointFails(t *testing.T) {
	emailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "missing scope", http.StatusForbidden)
	}))
	t.Cleanup(emailServer.Close)

	profile, err := parseGitHubOAuthProfile(context.Background(), config.EmailOAuthProviderConfig{
		EmailsURL: emailServer.URL,
	}, &emailOAuthTokenResponse{AccessToken: "token"}, `{"id":123,"login":"octo","email":"public@example.com"}`)

	require.Error(t, err)
	require.Nil(t, profile)
	require.Contains(t, err.Error(), "github emails endpoint status 403")
}
