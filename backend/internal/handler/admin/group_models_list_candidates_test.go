//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type selectionInventoryRepo struct{ service.ModelCatalogRepository }

func (*selectionInventoryRepo) ListPlatform(context.Context, string) ([]service.ModelCatalogSnapshot, error) {
	return nil, nil
}

type selectionInventoryAccounts struct{ service.AccountRepository }

func (*selectionInventoryAccounts) ListActive(context.Context) ([]service.Account, error) {
	return []service.Account{
		{Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-new": "gpt-new", "gpt-image-new": "gpt-image-new", "sora-new": "sora-new"}}},
		{Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-new": "claude-new"}}},
	}, nil
}

type selectionInventoryAdmin struct{ service.AdminService }

func (*selectionInventoryAdmin) GetGroup(context.Context, int64) (*service.Group, error) {
	return &service.Group{ID: 2, Platform: service.PlatformOpenAI}, nil
}

func TestGroupAllowlistCandidatesUseInventoryWithoutMembersOrPrices(t *testing.T) {
	registry := service.NewModelCatalogService(&selectionInventoryRepo{}, &selectionInventoryAccounts{}, nil, nil, nil)
	t.Cleanup(registry.Stop)
	h := &GroupHandler{modelRegistry: registry, adminService: &selectionInventoryAdmin{}}
	router := gin.New()
	router.GET("/groups/:id/models", h.GetGroupModelAllowlistCandidates)
	for _, url := range []string{"/groups/0/models?platform=openai", "/groups/2/models?platform=anthropic"} {
		data := fetchModelsListCandidates(t, router, url)
		for _, id := range []string{"gpt-new", "gpt-image-new", "sora-new"} {
			require.Contains(t, data["models"], id)
		}
		require.NotContains(t, data["models"], "claude-new")
		require.Equal(t, "model_catalog", data["source"])
	}
}

// hangingUpstream 模拟一个吊死的代理：请求一直不返回，直到 ctx 被取消。
type hangingUpstream struct{}

func (u *hangingUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

func (u *hangingUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func newModelsListCandidatesRouter(t *testing.T) *gin.Engine {
	t.Helper()
	// accountTestService 为 nil：上游同步不可用，候选必须降级为静态列表。
	return newModelsListCandidatesRouterWith(t, newStubAdminService(), nil)
}

func newModelsListCandidatesRouterWith(t *testing.T, adminSvc service.AdminService, upstream service.HTTPUpstream) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var accountTestSvc *service.AccountTestService
	if upstream != nil {
		accountTestSvc = service.NewAccountTestService(
			nil, nil, nil, nil, nil, upstream,
			&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
			nil,
		)
	}
	handler := NewGroupHandler(adminSvc, nil, nil, accountTestSvc)
	router := gin.New()
	router.GET("/api/v1/admin/groups/:id/model-allowlist-candidates", handler.GetGroupModelAllowlistCandidates)
	return router
}

func fetchModelsListCandidates(t *testing.T, router *gin.Engine, url string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	return envelope.Data
}

// 候选是给编辑弹窗用的补充信息，不是分组 models_list 的全集。上游 /v1/models
// 拿不到（账号 token 过期、分组还没有账号、服务未接线）时必须降级为静态候选，
// 而不是让整个弹窗打不开——弹窗打不开时前端拿不到候选，保存路径会把已配置的
// models_list 一并带走。
func TestGetGroupModelAllowlistCandidatesFallsBackToStaticForAnthropic(t *testing.T) {
	router := newModelsListCandidatesRouter(t)

	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/2/model-allowlist-candidates?platform=anthropic")

	require.Contains(t, data["models"], "claude-sonnet-4-6")
	require.Equal(t, "static+anthropic_v1_models", data["source"])
	require.Empty(t, data["live_models"])
}

// 实时列表只做补充：静态候选一条不少，上游多出来的追加在后面。
func TestGetGroupModelAllowlistCandidatesMergesLiveModelsIntoStaticCandidates(t *testing.T) {
	stub := newStubAdminService()
	account := anthropicAPIKeyAccount(741, "candidates.example")
	stub.accountSchedulerScoreFilterAccounts = []service.Account{*account}
	upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{
		"candidates.example": `{"data":[{"id":"claude-live-only"}]}`,
	}}
	router := newModelsListCandidatesRouterWith(t, stub, upstream)

	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/2/model-allowlist-candidates?platform=anthropic")

	models, ok := data["models"].([]any)
	require.True(t, ok)
	require.Equal(t, "claude-sonnet-4-6", models[0], "静态候选排在前面")
	require.Contains(t, models, "claude-live-only")
	require.Equal(t, []any{"claude-live-only"}, data["live_models"])
}

// 分组里一个可用账号都没有时，实时补充直接跳过，静态候选照常返回。
func TestGetGroupModelAllowlistCandidatesFallsBackToStaticWhenGroupHasNoAccounts(t *testing.T) {
	stub := newStubAdminService()
	stub.accountSchedulerScoreFilterAccounts = []service.Account{}
	router := newModelsListCandidatesRouterWith(t, stub, &anthropicModelsBulkUpstream{})

	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/2/model-allowlist-candidates?platform=anthropic")

	require.Contains(t, data["models"], "claude-sonnet-4-6")
	require.Empty(t, data["live_models"])
}

// 上游把所有账号都拒了也只是少几项候选：弹窗必须照常打开，否则前端拿不到候选，
// 保存路径会把已配置的 models_list 一并带走。
func TestGetGroupModelAllowlistCandidatesFallsBackToStaticWhenUpstreamRejectsEveryAccount(t *testing.T) {
	stub := newStubAdminService()
	stub.accountSchedulerScoreFilterAccounts = []service.Account{
		*anthropicAPIKeyAccount(751, "rejected.example"),
	}
	upstream := &anthropicModelsBulkUpstream{
		bodies:   map[string]string{"rejected.example": `{"error":"expired"}`},
		statuses: map[string]int{"rejected.example": http.StatusUnauthorized},
	}
	router := newModelsListCandidatesRouterWith(t, stub, upstream)

	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/2/model-allowlist-candidates?platform=anthropic")

	require.Contains(t, data["models"], "claude-sonnet-4-6")
	require.Empty(t, data["live_models"])
	require.Equal(t, "static+anthropic_v1_models", data["source"])
}

// 实时补充的预算必须远小于管理端 HTTP 客户端的 30s 超时：吊死的代理只该让候选
// 少几项，不该让整个请求（含静态候选）超时。
func TestGetGroupModelAllowlistCandidatesGivesTheLiveFetchItsOwnBudget(t *testing.T) {
	previous := anthropicModelCandidateTimeout
	anthropicModelCandidateTimeout = 50 * time.Millisecond
	t.Cleanup(func() { anthropicModelCandidateTimeout = previous })

	stub := newStubAdminService()
	stub.accountSchedulerScoreFilterAccounts = []service.Account{
		*anthropicAPIKeyAccount(752, "hanging.example"),
	}
	router := newModelsListCandidatesRouterWith(t, stub, &hangingUpstream{})

	started := time.Now()
	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/2/model-allowlist-candidates?platform=anthropic")

	require.Less(t, time.Since(started), 5*time.Second, "实时抓取不该拖着整个请求")
	require.Contains(t, data["models"], "claude-sonnet-4-6")
	require.Empty(t, data["live_models"])
}

// 新建分组（groupID=0）没有账号池可言，不该对系统里每个 Anthropic 账号各发一次
// /v1/models，只返回静态候选。
func TestGetGroupModelAllowlistCandidatesSkipsLiveFetchWhenCreatingGroup(t *testing.T) {
	stub := newStubAdminService()
	stub.accountSchedulerScoreFilterAccounts = []service.Account{*anthropicAPIKeyAccount(742, "unused.example")}
	router := newModelsListCandidatesRouterWith(t, stub, &anthropicModelsBulkUpstream{})

	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/0/model-allowlist-candidates?platform=anthropic")

	require.NotEmpty(t, data["models"])
	require.Empty(t, data["live_models"])
	require.Zero(t, stub.schedulerScoreFilterCalls, "新建流程不该去列账号")
}

// 平台解析只有 service 一份：handler 不再为拿一个 group.Platform 去打带账号计数
// 聚合的 GetGroup，同一个请求也就不会对 groups 表查两次。
func TestGetGroupModelAllowlistCandidatesResolvesPlatformInTheServiceOnly(t *testing.T) {
	stub := newStubAdminService()
	router := newModelsListCandidatesRouterWith(t, stub, nil)

	data := fetchModelsListCandidates(t, router, "/api/v1/admin/groups/2/model-allowlist-candidates")

	require.Contains(t, data["models"], "claude-sonnet-4-6")
	require.Equal(t, "static+anthropic_v1_models", data["source"])
	require.Zero(t, stub.getGroupCalls, "平台解析不该再走 GetGroup")
	require.Empty(t, stub.modelsListCandidatesPlatform, "未指定平台时原样透传给 service")
}

// 非 Anthropic 平台的响应形状保持不变。
func TestGetGroupModelAllowlistCandidatesKeepsNonAnthropicShape(t *testing.T) {
	router := newModelsListCandidatesRouter(t)

	data := fetchModelsListCandidates(t, router,
		"/api/v1/admin/groups/0/model-allowlist-candidates?platform=openai")

	require.Contains(t, data["models"], "gpt-5.5")
	require.NotContains(t, data, "source")
	require.NotContains(t, data, "live_models")
}
