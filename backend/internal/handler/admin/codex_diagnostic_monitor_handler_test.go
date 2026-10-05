package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type monitorHandlerRepo struct {
	service.CodexDiagnosticRepository
	plan *service.CodexDiagnosticPlan
}

func (r *monitorHandlerRepo) GetPlan(context.Context, int64) (*service.CodexDiagnosticPlan, error) {
	return r.plan, nil
}
func (r *monitorHandlerRepo) SavePlan(_ context.Context, p *service.CodexDiagnosticPlan) error {
	p.Revision = 1
	r.plan = p
	return nil
}
func (r *monitorHandlerRepo) Summaries(context.Context, []int64) (map[int64]service.CodexDiagnosticSummary, error) {
	return map[int64]service.CodexDiagnosticSummary{}, nil
}
func (r *monitorHandlerRepo) Enqueue(_ context.Context, p *service.CodexDiagnosticPlan, source, name string) (*service.CodexDiagnosticRun, error) {
	return &service.CodexDiagnosticRun{ID: 22, AccountID: p.AccountID, OwnerID: p.OwnerID, APIKeyID: p.APIKeyID, Models: p.Models, APIKeyName: name, Source: source, Status: "queued", WorkerToken: "not-for-api"}, nil
}
func (r *monitorHandlerRepo) GetRun(_ context.Context, account, id int64) (*service.CodexDiagnosticRun, error) {
	if account != 41 || id != 22 {
		return nil, service.ErrDiagnosticNotFound
	}
	return &service.CodexDiagnosticRun{ID: 22, AccountID: 41, WorkerToken: "secret-worker", Items: []service.CodexDiagnosticItem{}}, nil
}

type monitorHandlerUsers struct{ service.UserRepository }

func (monitorHandlerUsers) GetByID(context.Context, int64) (*service.User, error) {
	return &service.User{ID: 17, Role: service.RoleAdmin, Status: service.StatusActive}, nil
}
func monitorRequest(t *testing.T, handler func(*gin.Context), body string, account, run string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: account}, {Key: "run_id", Value: run}}
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 17})
	handler(c)
	return w
}
func TestDiagnosticMonitorHandlerBindsOwnerAndProtectsHistory(t *testing.T) {
	h, _, _ := newCodexChallengeDiagnosticHandler(t, []string{"gpt-5.4"}, nil)
	repo := &monitorHandlerRepo{}
	accounts := &codexDiagnosticChallengeAccountRepo{account: &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}}
	keyService := service.NewAPIKeyService(monitorHandlerKeys{}, nil, nil, nil, nil, nil, nil)
	h.SetCodexDiagnosticMonitor(service.NewCodexDiagnosticMonitor(repo, accounts, keyService, monitorHandlerUsers{}, h.codexTicketGateway))
	w := monitorRequest(t, h.SaveCodexDiagnosticPlan, `{"api_key_id":9,"models":["gpt-5.4"],"enabled":true,"owner_id":999}`, "41", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.EqualValues(t, 17, repo.plan.OwnerID)
	require.True(t, repo.plan.Enabled)
	w = monitorRequest(t, h.StartCodexDiagnosticRun, "", "41", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, "queued", gjson.GetBytes(w.Body.Bytes(), "data.status").String())
	require.NotContains(t, w.Body.String(), "test-key")
	require.NotContains(t, w.Body.String(), "not-for-api")
	w = monitorRequest(t, h.GetCodexDiagnosticRun, "", "42", "22")
	require.Equal(t, 404, w.Code)
	w = monitorRequest(t, h.GetCodexDiagnosticRun, "", "41", "22")
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "secret-worker")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
}

type monitorHandlerKeys struct{ service.APIKeyRepository }

func (monitorHandlerKeys) GetByID(context.Context, int64) (*service.APIKey, error) {
	return &service.APIKey{ID: 9, UserID: 17, Key: "test-key", Status: service.StatusActive, Group: &service.Group{ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}}}, nil
}
