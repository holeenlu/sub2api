//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type managementUserRepo struct {
	*stubUserRepo
	users map[int64]*service.User
}

func (r *managementUserRepo) GetByIDIncludeDeleted(_ context.Context, id int64) (*service.User, error) {
	return r.users[id], nil
}
func (r *managementUserRepo) ResolveAdminTargetUsers(_ context.Context, _ string, ids []int64) ([]int64, error) {
	return ids, nil
}
func (r *managementUserRepo) CheckAdminAccountTransport(context.Context, []int64, map[string]any) error {
	return nil
}
func (r *managementUserRepo) CheckAdminProxyTransport(context.Context, []int64, map[string]any) error {
	return nil
}

func managementHTTPFixture(t *testing.T, permissions []string) (*gin.Engine, string, *bmSettingRepo, *service.AuthService, *service.User) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	value, err := json.Marshal(authz.Policy{Version: 1, Permissions: permissions})
	require.NoError(t, err)
	policy := &bmSettingRepo{values: map[string]string{authz.PolicyKey: string(value)}}
	actor := &service.User{ID: 1, Email: "operator@example.com", PasswordHash: "hash", Role: service.RoleAdmin, Status: service.StatusActive}
	repo := &managementUserRepo{users: map[int64]*service.User{1: actor, 2: {ID: 2, Role: service.RoleUser, Status: service.StatusActive}, 3: {ID: 3, Role: service.RoleSuperAdmin, Status: service.StatusActive}}}
	repo.stubUserRepo = &stubUserRepo{getByID: func(_ context.Context, id int64) (*service.User, error) {
		u, ok := repo.users[id]
		if !ok {
			return nil, service.ErrUserNotFound
		}
		v := *u
		return &v, nil
	}}
	settings := service.NewSettingService(policy, nil)
	auth := service.NewAuthService(nil, repo, nil, nil, &config.Config{JWT: config.JWTConfig{Secret: "rbac-fixture", ExpireHour: 1}}, settings, nil, nil, nil, nil, nil, nil, nil)
	token, err := auth.GenerateToken(context.Background(), actor)
	require.NoError(t, err)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAdminAuthMiddleware(auth, service.NewUserService(repo, nil, nil, nil), settings, nil)))
	return router, token, policy, auth, actor
}
func managementRequest(router *gin.Engine, token, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}
func TestAdminAuthorizationHTTPProjectionAndRevocation(t *testing.T) {
	router, token, settings, _, _ := managementHTTPFixture(t, []string{"usage.read"})
	router.Group("").GET("/api/v1/admin/usage", func(c *gin.Context) {
		response.Success(c, gin.H{"actual_cost": 12.3, "account_stats_cost": 4.2, "account_rate_multiplier": .1, "api_key": gin.H{"id": 4, "name": "customer key"}})
	})
	rec := managementRequest(router, token, "GET", "/api/v1/admin/usage", "")
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "actual_cost")
	require.NotContains(t, rec.Body.String(), "account_stats_cost")
	require.NotContains(t, rec.Body.String(), "account_rate_multiplier")
	require.Contains(t, rec.Body.String(), "customer key")
	settings.values[authz.PolicyKey] = `{"version":2,"permissions":["usage.read"]}`
	rec = managementRequest(router, token, "GET", "/api/v1/admin/usage", "")
	require.Equal(t, 200, rec.Code)
	settings.values[authz.PolicyKey] = `{"version":3,"permissions":["usage.read","retired.permission"]}`
	require.Equal(t, 200, managementRequest(router, token, "GET", "/api/v1/admin/usage", "").Code)
	settings.values[authz.PolicyKey] = `{"version":4,"permissions":[]}`
	require.Equal(t, 403, managementRequest(router, token, "GET", "/api/v1/admin/usage", "").Code)
	settings.values[authz.PolicyKey] = "invalid"
	rec = managementRequest(router, token, "GET", "/api/v1/admin/usage", "")
	require.Equal(t, 503, rec.Code)
}

func TestAdminPolicyCannotBlockPersonalAuthentication(t *testing.T) {
	_, token, store, auth, actor := managementHTTPFixture(t, []string{"users.read"})
	store.values[authz.PolicyKey] = "broken"
	repo := &stubUserRepo{getByID: func(context.Context, int64) (*service.User, error) { return actor, nil }}
	r := gin.New()
	r.GET("/api/v1/keys", jwtAuth(auth, repo, nil, service.NewSettingService(store, nil), nil), func(c *gin.Context) { c.Status(204) })
	require.Equal(t, 204, managementRequest(r, token, "GET", "/api/v1/keys", "").Code)
}

func TestAdminCostSortAndStaffSwitch(t *testing.T) {
	for _, staff := range []bool{false, true} {
		permissions := []string{"users.read", "users.create", "users.update", "users.delete", "accounts.read"}
		if staff {
			permissions = append(permissions, "staff.manage")
		}
		r, token, _, _, _ := managementHTTPFixture(t, permissions)
		r.Group("").POST("/api/v1/admin/users", func(c *gin.Context) { c.Status(204) })
		r.Group("").PUT("/api/v1/admin/users/:id", func(c *gin.Context) { c.Status(204) })
		r.Group("").GET("/api/v1/admin/accounts", func(c *gin.Context) { c.Status(204) })
		expected := 403
		if staff {
			expected = 204
		}
		require.Equal(t, expected, managementRequest(r, token, "POST", "/api/v1/admin/users", `{"role":"admin"}`).Code)
		require.Equal(t, expected, managementRequest(r, token, "PUT", "/api/v1/admin/users/1", `{"username":"peer profile"}`).Code)
		require.Equal(t, expected, managementRequest(r, token, "PUT", "/api/v1/admin/users/2", `{"role":"admin"}`).Code)
		require.Equal(t, 204, managementRequest(r, token, "POST", "/api/v1/admin/users", `{"role":"user"}`).Code)
		require.Equal(t, 403, managementRequest(r, token, "POST", "/api/v1/admin/users", `{"role":"user","balance":0}`).Code)
		require.Equal(t, 204, managementRequest(r, token, "PUT", "/api/v1/admin/users/2", `{"username":"customer"}`).Code)
		require.Equal(t, 403, managementRequest(r, token, "GET", "/api/v1/admin/accounts?sort_by=rate_multiplier", "").Code)
	}
}

func TestIdleManagementStreamClosesWhenPermissionRevoked(t *testing.T) {
	ctx, cancel := context.WithCancel(authz.WithRevalidation(context.Background(), func(context.Context) error { return service.ErrAdminPermissionDenied }))
	defer cancel()
	go watchManagementStream(ctx, cancel, time.Millisecond)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("idle stream did not close")
	}
}

func TestSSEChecksOncePerEventRatherThanPerWriteOrFlush(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	checks := 0
	ctx, cancel := context.WithCancel(authz.WithRevalidation(context.Background(), func(context.Context) error { checks++; return nil }))
	defer cancel()
	w := &managementStreamWriter{ResponseWriter: c.Writer, context: ctx, cancel: cancel}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeaderNow()
	_, err := w.WriteString("event: usage\n")
	require.NoError(t, err)
	_, err = w.WriteString("data: {\"requests\":1}")
	require.NoError(t, err)
	_, err = w.WriteString("\n\n")
	require.NoError(t, err)
	w.Flush()
	require.Equal(t, 1, checks)
	_, err = w.WriteString("data: {}\n\n")
	require.NoError(t, err)
	w.Flush()
	require.Equal(t, 2, checks)
}
func TestAdminAuthorizationRejectsHiddenFieldsAndProtectedTargets(t *testing.T) {
	router, token, _, _, _ := managementHTTPFixture(t, []string{"users.read", "users.update", "staff.manage"})
	writes := 0
	router.Group("").PUT("/api/v1/admin/users/:id", func(c *gin.Context) { writes++; c.JSON(200, gin.H{"ok": true}) })
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/admin/users/2", `{"balance":0}`},
		{"/api/v1/admin/users/2", `{"reset_totp":true}`},
		{"/api/v1/admin/users/2", `{"group_rates":{}}`},
		{"/api/v1/admin/users/2", `{"role":"super_admin"}`},
		{"/api/v1/admin/users/3", `{"username":"overwrite root"}`},
		{"/api/v1/admin/users/1", `{"status":"inactive"}`},
		{"/api/v1/admin/users/2", `{"username":"one","UserName":"two"}`},
		{"/api/v1/admin/users/2", `{"extra":{"api_key":"one","ApiKey":"two"}}`},
	} {
		rec := managementRequest(router, token, "PUT", tc.path, tc.body)
		require.Equal(t, 403, rec.Code, tc.body+rec.Body.String())
	}
	require.Zero(t, writes)
	rec := managementRequest(router, token, "PUT", "/api/v1/admin/users/1", `{"role":"admin","status":"active","username":"unchanged identity"}`)
	require.Equal(t, 200, rec.Code, rec.Body.String())
	require.Equal(t, 1, writes)
}
func TestAdminAuthorizationUnknownAndRootRoutesRemainClosed(t *testing.T) {
	all := []string{}
	for _, p := range authz.Catalogue() {
		all = append(all, p.Key)
	}
	router, token, _, _, _ := managementHTTPFixture(t, all)
	for _, path := range []string{"/api/v1/admin/new-route", "/api/v1/admin/settings/admin-api-key", "/api/v1/admin/roles/admin/permissions"} {
		router.GET(path, func(c *gin.Context) { t.Error("unauthorized handler reached"); c.Status(204) })
		rec := managementRequest(router, token, "GET", path, "")
		require.Equal(t, 403, rec.Code, path)
	}
}
func TestAdminAuthorizationAllowsAdmittedRequestToFinish(t *testing.T) {
	router, token, settings, _, _ := managementHTTPFixture(t, []string{"usage.read"})
	router.Group("").GET("/api/v1/admin/usage", func(c *gin.Context) {
		settings.values[authz.PolicyKey] = `{"version":2,"permissions":[]}`
		c.JSON(http.StatusOK, gin.H{"data": "sensitive result"})
	})
	rec := managementRequest(router, token, "GET", "/api/v1/admin/usage", "")
	require.Equal(t, 200, rec.Code)
	require.Contains(t, rec.Body.String(), "sensitive result")
}

func TestUserAttributeBatchReadsProtectSuperAdministrators(t *testing.T) {
	r, token, _, _, _ := managementHTTPFixture(t, []string{"users.read"})
	reads := 0
	r.POST("/api/v1/admin/user-attributes/batch", func(c *gin.Context) { reads++; c.Status(204) })
	require.Equal(t, 403, managementRequest(r, token, "POST", "/api/v1/admin/user-attributes/batch", `{"user_ids":[2,3]}`).Code)
	require.Zero(t, reads)
	require.Equal(t, 204, managementRequest(r, token, "POST", "/api/v1/admin/user-attributes/batch", `{"user_ids":[2]}`).Code)
	require.Equal(t, 1, reads)
}
