package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

type lifecycleAdminService struct {
	service.AdminService
	user    *service.User
	updated bool
}

func (s *lifecycleAdminService) GetUser(context.Context, int64) (*service.User, error) {
	return s.user, nil
}
func (s *lifecycleAdminService) UpdateUser(context.Context, int64, *service.UpdateUserInput) (*service.User, error) {
	s.updated = true
	return s.user, nil
}
func TestAuthLifecycleAdminCredentialReplacementGate(t *testing.T) {
	for _, test := range []struct {
		body    string
		allowed bool
	}{
		{`{"password":"replacement"}`, false},
		{`{"reset_totp":true}`, false},
		{`{"email":"attacker@example.com"}`, false},
		{`{"role":"user"}`, false},
		{`{"username":"ordinary edit","email":"admin@example.com","role":"admin"}`, true},
	} {
		t.Run(test.body, func(t *testing.T) {
			settings, _ := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyStepUpEnabled: "true"})
			admin := &lifecycleAdminService{user: &service.User{ID: 2, Email: "admin@example.com", Role: service.RoleAdmin, Status: service.StatusActive}}
			h := &UserHandler{adminService: admin, settingService: settings.settingService}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("PUT", "/users/2", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: "2"}}
			h.Update(c)
			require.Equal(t, test.allowed, admin.updated, rec.Body.String())
			if test.allowed {
				require.Equal(t, http.StatusOK, rec.Code)
			} else {
				require.Equal(t, http.StatusUnauthorized, rec.Code)
			}
		})
	}
}
func TestAuthLifecycleGlobalMFAAndAdminKeyGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyStepUpEnabled: "true", service.SettingKeyTotpEnabled: "true"})
	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true, "totp_enabled": false}, nil)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyTotpEnabled])
	rec = httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/admin-api-key/regenerate", nil)
	h.RegenerateAdminAPIKey(c)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// These endpoint-mapping controls run through real JWT middleware and a real TOTP grant.
// Only persistence/encryption are in-memory fixtures; the gates are not bypassed.
type lifecycleAdminUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r lifecycleAdminUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	u := *r.user
	return &u, nil
}
func (lifecycleAdminUserRepo) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}
func (lifecycleAdminUserRepo) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	return nil
}

type lifecycleAdminFactorCache struct {
	service.TotpCache
	grants map[string]bool
}

func (c *lifecycleAdminFactorCache) GetVerifyAttempts(context.Context, int64) (int, error) {
	return 0, nil
}
func (c *lifecycleAdminFactorCache) ClearVerifyAttempts(context.Context, int64) error { return nil }
func (c *lifecycleAdminFactorCache) SetStepUpGrant(_ context.Context, _ int64, key string, _ time.Duration) error {
	c.grants[key] = true
	return nil
}
func (c *lifecycleAdminFactorCache) HasStepUpGrant(_ context.Context, _ int64, key string) (bool, error) {
	return c.grants[key], nil
}

type lifecycleAdminEncryptor struct{}

func (lifecycleAdminEncryptor) Encrypt(s string) (string, error) { return s, nil }
func (lifecycleAdminEncryptor) Decrypt(s string) (string, error) { return s, nil }
func lifecycleAuthenticatedAdminHandler(t *testing.T, router *gin.Engine, admin service.AdminService) *UserHandler {
	t.Helper()
	ctx := context.Background()
	secret := "JBSWY3DPEHPK3PXP"
	actor := &service.User{ID: 1001, Email: "verified-admin@example.com", PasswordHash: "hash", Role: service.RoleSuperAdmin, Status: service.StatusActive, TotpEnabled: true, TotpSecretEncrypted: &secret}
	repo := lifecycleAdminUserRepo{user: actor}
	users := service.NewUserService(repo, nil, nil, nil)
	settings := service.NewSettingService(&settingHandlerRepoStub{values: map[string]string{service.SettingKeyStepUpEnabled: "true"}}, nil)
	factors := service.NewTotpService(repo, lifecycleAdminEncryptor{}, &lifecycleAdminFactorCache{grants: map[string]bool{}}, settings, nil, nil)
	auth := service.NewAuthService(nil, repo, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "fixture-secret", AccessTokenExpireMinutes: 60}}, settings, nil, nil, nil, nil, nil, nil, nil)
	token, err := auth.GenerateToken(ctx, actor)
	require.NoError(t, err)
	claims, err := auth.ValidateToken(token)
	require.NoError(t, err)
	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	_, err = factors.VerifyStepUp(ctx, actor.ID, claims.SessionID, code)
	require.NoError(t, err)
	router.Use(func(c *gin.Context) { c.Request.Header.Set("Authorization", "Bearer "+token); c.Next() }, gin.HandlerFunc(middleware.NewAdminAuthMiddleware(auth, users, settings, nil)))
	return NewUserHandler(admin, nil, nil, nil, factors, users, settings)
}
