//go:build unit

package routes

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelRouteStore struct {
	service.SettingRepository
	mu     sync.Mutex
	values map[string]string
}

func (r *modelRouteStore) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return v, nil
}
func (r *modelRouteStore) Get(ctx context.Context, key string) (*service.Setting, error) {
	v, e := r.GetValue(ctx, key)
	return &service.Setting{Key: key, Value: v}, e
}
func (r *modelRouteStore) CompareAndSwapModelSettings(_ context.Context, key string, expected int64, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var old, next struct {
		Version int64 `json:"version"`
	}
	if raw := r.values[key]; raw != "" {
		if e := json.Unmarshal([]byte(raw), &old); e != nil {
			return e
		}
	}
	if e := json.Unmarshal([]byte(value), &next); e != nil {
		return e
	}
	if old.Version != expected || next.Version != expected+1 {
		return service.ErrModelPolicyConflict
	}
	r.values[key] = value
	return nil
}

type modelRouteUsers struct {
	service.UserRepository
	user *service.User
}

func (r *modelRouteUsers) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

func (r *modelRouteUsers) GetByID(context.Context, int64) (*service.User, error) { return r.user, nil }
func (r *modelRouteUsers) GetByIDIncludeDeleted(context.Context, int64) (*service.User, error) {
	return r.user, nil
}
func (r *modelRouteUsers) ResolveAdminTargetUsers(context.Context, string, []int64) ([]int64, error) {
	return nil, nil
}
func (r *modelRouteUsers) CheckAdminAccountTransport(context.Context, []int64, map[string]any) error {
	return nil
}
func (r *modelRouteUsers) CheckAdminProxyTransport(context.Context, []int64, map[string]any) error {
	return nil
}

type modelRouteAccounts struct {
	service.AccountRepository
	row           *service.Account
	reads, writes int
}

func (r *modelRouteAccounts) ListByPlatform(context.Context, string) ([]service.Account, error) {
	r.reads++
	return []service.Account{*r.row}, nil
}
func (r *modelRouteAccounts) UpdateModelReferences(_ context.Context, _ *service.Account, _ []string, _ map[string]any) error {
	r.writes++
	return nil
}

type modelRouteAdmin struct {
	service.AdminService
	accounts *modelRouteAccounts
}

func (r *modelRouteAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	r.accounts.reads++
	v := *r.accounts.row
	return &v, nil
}

type modelRouteFixture struct {
	router   *gin.Engine
	token    string
	store    *modelRouteStore
	accounts *modelRouteAccounts
	steps    *int
}

func newModelRouteFixture(t *testing.T, role string, permissions []string) modelRouteFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	raw, e := json.Marshal(authz.Policy{Version: 1, Permissions: permissions})
	require.NoError(t, e)
	store := &modelRouteStore{values: map[string]string{authz.PolicyKey: string(raw)}}
	settings := service.NewSettingService(store, nil)
	user := &service.User{ID: 1, Role: role, Status: service.StatusActive, Email: "model-admin@example.com"}
	users := &modelRouteUsers{user: user}
	auth := service.NewAuthService(nil, users, nil, &config.Config{JWT: config.JWTConfig{Secret: "models-route-fixture", ExpireHour: 1}}, settings, nil, nil, nil, nil, nil)
	token, e := auth.GenerateToken(context.Background(), user)
	require.NoError(t, e)
	accounts := &modelRouteAccounts{row: &service.Account{ID: 9, Name: "fact source", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"model_mapping": map[string]any{"alias": "target"}, service.AccountModelReferencesKey: []string{}}}}
	models := adminhandler.NewModelManagementHandler(settings, accounts, &modelRouteAdmin{accounts: accounts}, nil, service.NewBillingService(nil, nil))
	router := gin.New()
	steps := new(int)
	RegisterAdminRoutes(router.Group("/api/v1"), &handler.Handlers{Admin: &handler.AdminHandlers{Models: models}}, middleware.NewAdminAuthMiddleware(auth, service.NewUserService(users, nil, nil, nil), settings, nil), middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() }), middleware.StepUpAuthMiddleware(func(c *gin.Context) { *steps++; c.Next() }), nil, nil)
	return modelRouteFixture{router, token, store, accounts, steps}
}
func (f modelRouteFixture) request(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/models"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func TestModelManagementRegisteredTwelveEndpointsAndPermissions(t *testing.T) {
	cases := []struct {
		method, path, body string
		grants             []string
		status             int
	}{
		{"GET", "/policies/openai", "", []string{"models.read"}, 200},
		{"PUT", "/policies/openai", `{}`, []string{"models.read"}, 403},
		{"POST", "/policies/openai/reset", `{"version":0}`, []string{"models.manage", "models.read"}, 200},
		{"POST", "/policies/openai/sync", `{"version":0,"account_ids":[]}`, []string{"models.manage", "models.read"}, 403},
		{"POST", "/policies/openai/sync", `{"version":0,"account_ids":[]}`, []string{"models.manage", "models.read", "accounts.read"}, 400},
		{"GET", "/catalog?platform=gemini", "", []string{"groups.read"}, 200},
		{"GET", "/sources?platform=openai", "", []string{"models.manage", "models.read"}, 403},
		{"GET", "/sources?platform=openai", "", []string{"models.manage", "models.read", "accounts.read"}, 200},
		{"GET", "/mappings", "", []string{"models.read"}, 200},
		{"PUT", "/mappings", `{"version":0,"mappings":{}}`, []string{"models.manage", "models.read"}, 200},
		{"GET", "/pricing?platform=openai", "", []string{"models.read"}, 403},
		{"GET", "/pricing?platform=openai", "", []string{"models.read", "billing.cost.read"}, 200},
		{"PUT", "/pricing", `{"platform":"openai","model":"gpt-6.1-sol","version":0,"price":null}`, []string{"models.read", "models.manage"}, 403},
		{"PUT", "/pricing", `{"platform":"openai","model":"gpt-6.1-sol","version":0,"price":null}`, []string{"models.read", "models.manage", "billing.rates.update"}, 200},
		{"POST", "/pricing/sync", `{"platform":"openai","version":1}`, []string{"models.read", "models.manage", "billing.rates.update"}, 409},
		{"PUT", "/accounts/9/allowlist", `{"expected":[],"models":["gpt-6.1-sol"]}`, []string{"accounts.read", "accounts.manage"}, 200},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+strings.Join(tc.grants, ","), func(t *testing.T) {
			f := newModelRouteFixture(t, service.RoleAdmin, tc.grants)
			w := f.request(tc.method, tc.path, tc.body)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 409 {
				require.Contains(t, w.Body.String(), "MODEL_POLICY_CONFLICT")
			}
			if tc.status == 403 {
				require.Equal(t, 0, f.accounts.reads)
			}
			if tc.method == "PUT" && tc.path == "/pricing" && tc.status == 200 {
				require.Equal(t, 1, *f.steps)
			}
		})
	}
	f := newModelRouteFixture(t, service.RoleSuperAdmin, nil)
	count := 0
	for _, r := range f.router.Routes() {
		if strings.HasPrefix(r.Path, "/api/v1/admin/models/") {
			count++
			_, ok := authz.RuleFor(r.Method, r.Path)
			require.True(t, ok, r.Path)
		}
	}
	require.Equal(t, 12, count)
}
func TestModelManagementHTTPSeedZeroCASAndProjection(t *testing.T) {
	f := newModelRouteFixture(t, service.RoleSuperAdmin, nil)
	w := f.request("GET", "/policies/gemini", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"aliases":null`)
	require.NotContains(t, w.Body.String(), `"context_1m_whitelist":null`)
	var got struct {
		Data service.ModelPolicy `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, int64(0), got.Data.Version)
	body, e := json.Marshal(got.Data)
	require.NoError(t, e)
	w = f.request("PUT", "/policies/gemini", string(body))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = f.request("PUT", "/policies/gemini", string(body))
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "MODEL_POLICY_CONFLICT")
	w = f.request("PUT", "/accounts/9/allowlist", `{"expected":[],"models":null}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"models":null`)
	require.Equal(t, map[string]any{"alias": "target"}, f.accounts.row.Credentials["model_mapping"])
}
func TestModelManagementSupplierSourceRejectedWithoutScope(t *testing.T) {
	f := newModelRouteFixture(t, service.RoleSuperAdmin, nil)
	vendor := int64(8)
	f.accounts.row.VendorID = &vendor
	w := f.request("POST", "/policies/openai/sync", `{"version":0,"account_ids":[9]}`)
	require.Equal(t, 403, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "scope")
}
func TestModelManagementRolePolicyRevocation(t *testing.T) {
	f := newModelRouteFixture(t, service.RoleAdmin, []string{"models.read"})
	require.Equal(t, 200, f.request("GET", "/mappings", "").Code)
	f.store.mu.Lock()
	f.store.values[authz.PolicyKey] = `{"version":2,"permissions":[]}`
	f.store.mu.Unlock()
	require.Equal(t, 403, f.request("GET", "/mappings", "").Code)
}
