//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminAuthKeyRepo struct {
	service.ScopedAdminKeyRepository
	item *service.ScopedAdminKey
}

func (r *adminAuthKeyRepo) InsertScopedAdminKey(_ context.Context, k *service.ScopedAdminKey) error {
	r.item = k
	return nil
}
func (r *adminAuthKeyRepo) GetScopedAdminKey(context.Context, string) (*service.ScopedAdminKey, error) {
	return r.item, nil
}
func (r *adminAuthKeyRepo) TouchScopedAdminKey(context.Context, string, string, time.Time, string, time.Time) error {
	return nil
}
func TestScopedAdminKeyAuthDefaultDenyAndCreatorStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &adminAuthKeyRepo{}
	settings := service.NewSettingService(nil, nil)
	settings.SetScopedAdminKeyRepository(repo)
	key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{Name: "只读", Enabled: true, Policy: adminaccess.Policy{GlobalPermissions: []string{"users.read"}}}, 9)
	require.NoError(t, err)
	active := true
	role := service.RoleSuperAdmin
	users := service.NewUserService(&stubUserRepo{getByID: func(context.Context, int64) (*service.User, error) {
		status := service.StatusActive
		if !active {
			status = "disabled"
		}
		return &service.User{ID: 9, Role: role, Status: status}, nil
	}}, nil, nil, nil)
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAdminAuthMiddleware(nil, users, settings, nil)))
	for _, path := range []string{"/api/v1/admin/users", "/api/v1/admin/unknown", "/api/v1/admin/settings/admin-api-keys"} {
		r.GET(path, func(c *gin.Context) { c.Status(204) })
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/api/v1/admin/users", 204}, {"/api/v1/admin/unknown", 403}, {"/api/v1/admin/settings/admin-api-keys", 403}} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Header.Set("x-api-key", key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, tc.status, w.Code, tc.path)
	}
	role = service.RoleAdmin
	reqOrdinary := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
	reqOrdinary.Header.Set("x-api-key", key)
	deniedOrdinary := httptest.NewRecorder()
	r.ServeHTTP(deniedOrdinary, reqOrdinary)
	require.Equal(t, 401, deniedOrdinary.Code, "普通管理员不能继承全站机器密钥")
	role = service.RoleSuperAdmin
	active = false
	req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
	req.Header.Set("x-api-key", key)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 401, w.Code)
}
func TestLegacyAdminKeyCannotManageKeys(t *testing.T) {
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAdminAuthMiddleware(nil, nil, nil, nil)))
	r.POST("/api/v1/admin/settings/admin-api-key/regenerate", func(c *gin.Context) { t.Fatal("不应执行 handler") })
	req := httptest.NewRequest("POST", "/api/v1/admin/settings/admin-api-key/regenerate", nil)
	req.Header.Set("x-api-key", "admin-legacy")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, 403, w.Code)
}

func TestScopedAdminKeyAuditDenialOmitsSecrets(t *testing.T) {
	repo := &auditCaptureRepository{}
	audit := service.NewAuditLogService(repo, nil)
	audit.Start()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/admin/accounts/123/forbidden?token=private-query", nil)
	c.Request.Header.Set("x-api-key", "adm_sc_private-secret")
	c.Status(403)
	c.Set("admin_key_denial_reason", "permission_denied")
	recordAdminKeyRequest(c, audit, &service.ScopedAdminKey{ID: "key-id", Hash: "private-hash", CreatedBy: 9, ScopedAdminKeyInput: service.ScopedAdminKeyInput{Name: "审计身份"}})
	audit.Stop()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.logs, 1)
	entry := repo.logs[0]
	require.Equal(t, "/api/v1/admin/accounts/123/forbidden", entry.Path)
	require.Equal(t, "permission_denied", entry.Extra["denial_reason"])
	require.Equal(t, 403, entry.StatusCode)
	require.Equal(t, "key-id", entry.Extra["admin_key_id"])
	require.Equal(t, "审计身份", entry.Extra["admin_key_name"])
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NotContains(t, string(data), "private-secret")
	require.NotContains(t, string(data), "private-hash")
}

func TestScopedAdminKeyMissingDependenciesFailClosed(t *testing.T) {
	r := gin.New()
	r.Use(gin.HandlerFunc(NewAdminAuthMiddleware(nil, nil, nil, nil)))
	r.GET("/api/v1/admin/users", func(c *gin.Context) { t.Fatal("不应执行") })
	req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
	req.Header.Set("x-api-key", "adm_sc_invalid")
	w := httptest.NewRecorder()
	require.NotPanics(t, func() { r.ServeHTTP(w, req) })
	require.Equal(t, 401, w.Code)
}

func TestScopedAdminKeyRevalidatesScopeWithoutLegacyKey(t *testing.T) {
	repo := &adminAuthKeyRepo{}
	settings := service.NewSettingService(nil, nil)
	settings.SetScopedAdminKeyRepository(repo)
	key, _, err := settings.CreateScopedAdminKey(context.Background(), service.ScopedAdminKeyInput{Name: "scope", Enabled: true, Policy: adminaccess.Policy{GlobalPermissions: []string{"users.read"}}}, 9)
	require.NoError(t, err)
	users := service.NewUserService(&stubUserRepo{getByID: func(context.Context, int64) (*service.User, error) {
		return &service.User{ID: 9, Role: service.RoleSuperAdmin, Status: service.StatusActive}, nil
	}}, nil, nil, nil)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAdminAuthMiddleware(nil, users, settings, nil)))
	router.GET("/api/v1/admin/users", func(c *gin.Context) {
		actor, ok := authz.FromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.AuditAuthMethodAdminAPIKey, actor.AuthMethod)
		require.NoError(t, authz.Revalidate(c.Request.Context()))
		changed := *repo.item
		changed.Policy = adminaccess.Policy{}
		repo.item = &changed
		require.Error(t, authz.Revalidate(c.Request.Context()), "策略撤销后不能继续输出")
		c.Status(204)
	})
	req := httptest.NewRequest("GET", "/api/v1/admin/users", nil)
	req.Header.Set("x-api-key", key)
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	require.Equal(t, 204, out.Code)
}
