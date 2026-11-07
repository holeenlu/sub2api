package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// --- mock repo ---

type apiKeyBreakdownRepoCapture struct {
	service.UsageLogRepository
	capturedDim   usagestats.UserBreakdownDimension
	capturedLimit int
	result        []usagestats.APIKeyBreakdownItem
	err           error
}

func (r *apiKeyBreakdownRepoCapture) GetAPIKeyBreakdownStats(
	_ context.Context, _, _ time.Time,
	dim usagestats.UserBreakdownDimension, limit int,
) ([]usagestats.APIKeyBreakdownItem, error) {
	r.capturedDim = dim
	r.capturedLimit = limit
	if r.err != nil {
		return nil, r.err
	}
	if r.result != nil {
		return r.result, nil
	}
	return []usagestats.APIKeyBreakdownItem{}, nil
}

func newAPIKeyBreakdownRouter(repo *apiKeyBreakdownRepoCapture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	svc := service.NewDashboardService(repo, nil, nil, nil)
	h := NewDashboardHandler(svc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/api-key-breakdown", h.GetAPIKeyBreakdown)
	return router
}

func doAPIKeyBreakdown(t *testing.T, repo *apiKeyBreakdownRepoCapture, query string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard/api-key-breakdown"+query, nil)
	newAPIKeyBreakdownRouter(repo).ServeHTTP(w, req)
	return w
}

// --- tests ---

// 与 user-breakdown 共用 parseBreakdownRequest，筛选参数必须一字不差地透传下去。
func TestGetAPIKeyBreakdown_PassesThroughSharedFilters(t *testing.T) {
	repo := &apiKeyBreakdownRepoCapture{}
	w := doAPIKeyBreakdown(t, repo,
		"?group_id=3&model=claude-fable-5&model_source=upstream&endpoint=/v1/messages&endpoint_type=path"+
			"&user_id=7&api_key_id=9&account_id=11&stream=true&native_compaction_v2=false&billing_type=2&sort_by=requests&limit=100")

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, int64(3), repo.capturedDim.GroupID)
	require.Equal(t, "claude-fable-5", repo.capturedDim.Model)
	require.Equal(t, usagestats.ModelSourceUpstream, repo.capturedDim.ModelType)
	require.Equal(t, "/v1/messages", repo.capturedDim.Endpoint)
	require.Equal(t, "path", repo.capturedDim.EndpointType)
	require.Equal(t, int64(7), repo.capturedDim.UserID)
	require.Equal(t, int64(9), repo.capturedDim.APIKeyID)
	require.Equal(t, int64(11), repo.capturedDim.AccountID)
	require.NotNil(t, repo.capturedDim.Stream)
	require.True(t, *repo.capturedDim.Stream)
	require.NotNil(t, repo.capturedDim.NativeCompactionV2)
	require.False(t, *repo.capturedDim.NativeCompactionV2)
	require.NotNil(t, repo.capturedDim.BillingType)
	require.Equal(t, int8(2), *repo.capturedDim.BillingType)
	require.Equal(t, "requests", repo.capturedDim.SortBy)
	require.Equal(t, 100, repo.capturedLimit)
}

func TestGetAPIKeyBreakdown_ReturnsRowsUnderAPIKeysField(t *testing.T) {
	repo := &apiKeyBreakdownRepoCapture{
		result: []usagestats.APIKeyBreakdownItem{
			{APIKeyID: 5, KeyName: "prod", UserID: 2, Email: "a@test.com", Requests: 3, TotalTokens: 900},
			{APIKeyID: 6, KeyName: "gone", KeyDeleted: true, UserID: 2, Email: "a@test.com"},
		},
	}
	w := doAPIKeyBreakdown(t, repo, "?start_date=2026-03-01&end_date=2026-03-16")
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Data struct {
			APIKeys []struct {
				APIKeyID   int64  `json:"api_key_id"`
				KeyName    string `json:"key_name"`
				KeyDeleted bool   `json:"key_deleted"`
			} `json:"api_keys"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data.APIKeys, 2)
	require.Equal(t, int64(5), body.Data.APIKeys[0].APIKeyID)
	require.Equal(t, "prod", body.Data.APIKeys[0].KeyName)
	require.True(t, body.Data.APIKeys[1].KeyDeleted, "软删除的 Key 必须带标记返回，不能被过滤掉")
	require.Equal(t, "2026-03-01", body.Data.StartDate)
	require.Equal(t, "2026-03-16", body.Data.EndDate)
}

// limit 越界回落默认 50，与 user-breakdown 行为一致。
func TestGetAPIKeyBreakdown_LimitOutOfRangeFallsBack(t *testing.T) {
	for _, q := range []string{"?limit=0", "?limit=201", "?limit=abc"} {
		repo := &apiKeyBreakdownRepoCapture{}
		require.Equal(t, http.StatusOK, doAPIKeyBreakdown(t, repo, q).Code)
		require.Equal(t, 50, repo.capturedLimit, "query=%s", q)
	}
}

// 非法枚举值必须 400，而不是被静默忽略。
func TestGetAPIKeyBreakdown_RejectsInvalidEnums(t *testing.T) {
	for _, q := range []string{
		"?model_source=bogus",
		"?request_type=bogus",
		"?native_compaction_v2=bogus",
	} {
		repo := &apiKeyBreakdownRepoCapture{}
		require.Equal(t, http.StatusBadRequest, doAPIKeyBreakdown(t, repo, q).Code, "query=%s", q)
	}
}

// sort_by 非法值不报错，由 repo 层 allowlist 回退默认排序。
func TestGetAPIKeyBreakdown_InvalidSortByIsDeferredToRepo(t *testing.T) {
	repo := &apiKeyBreakdownRepoCapture{}
	require.Equal(t, http.StatusOK, doAPIKeyBreakdown(t, repo, "?sort_by=drop_table").Code)
	require.Equal(t, "drop_table", repo.capturedDim.SortBy)
}

func TestGetAPIKeyBreakdown_ServiceErrorReturns500(t *testing.T) {
	repo := &apiKeyBreakdownRepoCapture{err: errors.New("db down")}
	w := doAPIKeyBreakdown(t, repo, "")
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), "db down", "内部错误细节不能泄露给客户端")
}
