package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type telemetryImportHandlerRepo struct {
	service.AccountRepository
	account *service.Account
	saves   int
}

func (r *telemetryImportHandlerRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return r.account, nil
}
func (r *telemetryImportHandlerRepo) ListAccountTelemetry(context.Context, int64, int) ([]*service.Account, error) {
	return nil, nil
}
func (r *telemetryImportHandlerRepo) SaveAccountTelemetry(_ context.Context, _ int64, _ string, revision string, state service.AccountTelemetryState) (bool, error) {
	if service.ReadAccountTelemetry(r.account).Revision != revision {
		return false, nil
	}
	r.saves++
	r.account.Extra[service.AccountTelemetryExtraKey] = state
	return true, nil
}
func TestAccountTelemetryAdminImportRouteAndBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := service.Account{ID: 42, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"account_uuid": "account-fixture", "email": "fixture@example.test", "org_uuid": "org-fixture"}, Extra: map[string]any{}}
	service.PrepareAccountTelemetryImport(&account)
	repo := &telemetryImportHandlerRepo{account: &account}
	gateway := service.NewGatewayService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	adminSvc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: account}
	h := &AccountHandler{adminService: adminSvc, accountTelemetry: gateway}
	engine := gin.New()
	engine.POST("/accounts/:id/telemetry/import", h.ImportAccountTelemetry)
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/accounts/42/telemetry/import", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, req)
		return recorder
	}
	tooLarge := request(strings.Repeat("x", int(service.AccountTelemetryMaxBodyBytes)+1))
	require.Equal(t, 413, tooLarge.Code)
	require.Zero(t, repo.saves)
	raw, err := os.ReadFile("../../service/testdata/account_telemetry_device.json")
	require.NoError(t, err)
	response := request(string(raw))
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var envelope struct{ Data service.AccountTelemetryState }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
	require.Equal(t, "sub2api-account-telemetry", envelope.Data.DeviceInstallation.Format)
	require.Equal(t, "ready", envelope.Data.Status)
	rule, ok := authz.RuleFor(http.MethodPost, "/api/v1/admin/accounts/:id/telemetry/import")
	require.True(t, ok)
	require.True(t, rule.SuperOnly)
	require.True(t, rule.Mutates)
	require.True(t, rule.Allows(authz.Subject{Role: authz.SuperAdmin}))
	require.False(t, rule.Allows(authz.Subject{Role: authz.Admin, Permissions: []string{"accounts.read", "accounts.update"}}))
	require.False(t, rule.Allows(authz.Subject{Role: authz.User}))
}
