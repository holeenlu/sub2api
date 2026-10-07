package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// step-up 开关转换的门控测试。
// 测试环境不注入认证上下文/userService，因此一旦触发校验会以 401/403/500 中止；
// 借此区分「触发了转换校验」与「直接放行到常规保存（200）」。

func newStepUpSwitchTestHandler(t *testing.T, stored map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: stored}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

func doUpdateSettings(t *testing.T, h *SettingHandler, body map[string]any, prepare func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	if prepare != nil {
		prepare(c)
	}

	h.UpdateSettings(c)
	return rec
}

// 开启开关（false→true）：无认证上下文时拒绝，且带专用错误标记。
func TestUpdateSettingsEnableStepUpRejectsWithoutSession(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ENABLE_REQUIRES_TOTP")
	require.NotEqual(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 开启开关：admin API key（机器凭证）一律拒绝，reason 与门控保持一致便于前端分流。
func TestUpdateSettingsEnableStepUpRejectsAdminAPIKey(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
}

// 开启开关：有认证会话但 userService 未注入时 fail-closed（500），不得放行。
func TestUpdateSettingsEnableStepUpFailsClosedWithoutUserService(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	})

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotEqual(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭开关（true→false）本身是敏感操作：无认证上下文时被 step-up 门控以 401 拦截。
func TestUpdateSettingsDisableStepUpRequiresStepUp(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭开关：admin API key 被 step-up 门控以 403 拦截。
func TestUpdateSettingsDisableStepUpRejectsAdminAPIKey(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 无状态转换（false→false）：不触发任何转换校验，常规保存成功且默认持久化为 false。
func TestUpdateSettingsStepUpNoTransitionSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	// 会话 IP/UA 绑定默认关闭：未显式提交时持久化 false。
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

// 保持开启（true→true）：不触发转换校验，常规保存不被打断。
func TestUpdateSettingsStepUpKeepEnabledSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 省略字段=保持现值：不含 step_up_enabled/session_binding_enabled 的旧客户端全量保存
// 不得把已开启的安全开关静默重置，也不触发任何转换门控。
func TestUpdateSettingsOmittedSecuritySwitchesKeepStoredValues(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled:         "true",
		service.SettingKeySessionBindingEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeySessionBindingEnabled])
}

// 省略字段在开关本就关闭时同样保持关闭（默认值路径）。
func TestUpdateSettingsOmittedSecuritySwitchesKeepDisabled(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

func TestUpdateSettingsForwardedClientIPHeadersOmittedPreservesAndEmptyClears(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyForwardedClientIPHeaders: `["X-Cdn-Ip","True-Client-Ip"]`,
	})

	preserved := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)
	require.Equal(t, http.StatusOK, preserved.Code)
	require.JSONEq(t, `["X-Cdn-Ip","True-Client-Ip"]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	require.Contains(t, preserved.Body.String(), `"forwarded_client_ip_headers":["X-Cdn-Ip","True-Client-Ip"]`)

	cleared := doUpdateSettings(t, h, map[string]any{"forwarded_client_ip_headers": []string{}}, nil)
	require.Equal(t, http.StatusOK, cleared.Code)
	require.JSONEq(t, `[]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	require.Contains(t, cleared.Body.String(), `"forwarded_client_ip_headers":[]`)
}

func TestUpdateSettingsMalformedForwardedClientIPHeadersRemainFailClosedWhenOmitted(t *testing.T) {
	cfg := &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}}
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyAPIKeyACLTrustForwardedIP: "true",
		service.SettingKeyForwardedClientIPHeaders:  `{"not":"an array"}`,
	}}
	svc := service.NewSettingService(repo, cfg)
	require.ErrorContains(t, svc.LoadForwardedClientIPSettings(context.Background()), "load forwarded client ip headers")
	require.False(t, cfg.ForwardedClientIPSettings().TrustForwardedIP)
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyAPIKeyACLTrustForwardedIP])
	require.JSONEq(t, `[]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	runtimeSettings := cfg.ForwardedClientIPSettings()
	require.False(t, runtimeSettings.TrustForwardedIP)
	require.Empty(t, runtimeSettings.Headers)
	require.Contains(t, rec.Body.String(), `"api_key_acl_trust_forwarded_ip":false`)
	require.Contains(t, rec.Body.String(), `"forwarded_client_ip_headers":[]`)
}

func TestUpdateSettingsRejectsInvalidForwardedClientIPHeader(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyForwardedClientIPHeaders: `["X-Existing-IP"]`,
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"forwarded_client_ip_headers": []string{"X Invalid"},
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `["X-Existing-IP"]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
}

// Keep policy edits separate from the remaining sensitive settings transitions.
// Nil user/TOTP services ensure this path does not depend on enrollment or grants.
type rolePolicyHandlerRepoStub struct {
	settingHandlerRepoStub
	policy *authz.Policy
	trace  *service.AuditLog
}

func (r *rolePolicyHandlerRepoStub) UpdateAdminRolePolicy(_ context.Context, expected int64, policy *authz.Policy, trace *service.AuditLog) error {
	if expected != r.policy.Version {
		return service.ErrAdminPolicyConflict
	}
	r.policy, r.trace = policy, trace
	return nil
}

func TestUpdateAdminRolePermissionsWithoutStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []string{"false", "true"} {
		t.Run("step_up="+enabled, func(t *testing.T) {
			for _, tc := range []struct {
				name, role, method string
				status             int
			}{
				{"super_admin_session", service.RoleSuperAdmin, service.AuditAuthMethodJWT, http.StatusOK},
				{"administrator", service.RoleAdmin, service.AuditAuthMethodJWT, http.StatusForbidden},
				{"user", service.RoleUser, service.AuditAuthMethodJWT, http.StatusForbidden},
				{"machine_key", service.RoleSuperAdmin, service.AuditAuthMethodAdminAPIKey, http.StatusForbidden},
				{"missing_subject", "", "", http.StatusForbidden},
			} {
				t.Run(tc.name, func(t *testing.T) {
					repo := &rolePolicyHandlerRepoStub{
						settingHandlerRepoStub: settingHandlerRepoStub{values: map[string]string{service.SettingKeyStepUpEnabled: enabled}},
						policy:                 &authz.Policy{Version: 7, Permissions: []string{}},
					}
					h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil, nil, nil, nil, nil)
					request := func() *httptest.ResponseRecorder {
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/roles/admin/permissions", bytes.NewBufferString(`{"expected_version":7,"permissions":["accounts.read"],"reason":"Enable operations visibility"}`))
						c.Request.Header.Set("Content-Type", "application/json")
						c.Set("auth_method", tc.method)
						if tc.role != "" {
							c.Request = c.Request.WithContext(authz.WithSubject(c.Request.Context(), authz.Subject{UserID: 1, Role: tc.role, AuthMethod: tc.method}))
						}
						h.UpdateAdminRolePermissions(c)
						return rec
					}
					rec := request()
					require.Equal(t, tc.status, rec.Code, rec.Body.String())
					if tc.status != http.StatusOK {
						require.Nil(t, repo.trace)
						require.EqualValues(t, 7, repo.policy.Version)
						return
					}
					require.EqualValues(t, 8, repo.policy.Version)
					require.Equal(t, []string{"accounts.read"}, repo.policy.Permissions)
					require.Equal(t, "admin.roles.permissions.update", repo.trace.Action)
					require.Equal(t, service.RoleSuperAdmin, repo.trace.Visibility)
					require.Equal(t, "Enable operations visibility", repo.trace.Extra["reason"])
					rec = request()
					require.Equal(t, http.StatusConflict, rec.Code)
					require.Contains(t, rec.Body.String(), "ADMIN_POLICY_CONFLICT")
					require.EqualValues(t, 8, repo.policy.Version)
				})
			}
		})
	}
}
