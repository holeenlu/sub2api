//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// anthropicModelsBulkAdminService 让测试自己决定 GetAccountsByIDs 返回什么账号。
type anthropicModelsBulkAdminService struct {
	*stubAdminService
	accounts []*service.Account
}

func (s *anthropicModelsBulkAdminService) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	out := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		for _, account := range s.accounts {
			if account.ID == id {
				out = append(out, account)
			}
		}
	}
	return out, nil
}

// anthropicModelsBulkUpstream 按 base_url 的 host 决定每个账号看到什么响应，
// 这样并发拉取的结果与 goroutine 的完成顺序无关。
type anthropicModelsBulkUpstream struct {
	mu       sync.Mutex
	bodies   map[string]string
	statuses map[string]int
}

func (u *anthropicModelsBulkUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	host := req.URL.Host
	status := http.StatusOK
	if override, ok := u.statuses[host]; ok {
		status = override
	}
	body, ok := u.bodies[host]
	if !ok {
		return nil, fmt.Errorf("no stubbed response for %s", host)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func (u *anthropicModelsBulkUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func setupAnthropicModelsBulkRouter(adminSvc service.AdminService, upstream service.HTTPUpstream) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var accountTestSvc *service.AccountTestService
	if upstream != nil {
		accountTestSvc = service.NewAccountTestService(
			nil, nil, nil, nil, nil, upstream,
			&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
			nil,
		)
	}
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, accountTestSvc, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/models/sync-anthropic-bulk", handler.SyncAnthropicModelsBulk)
	return router
}

func anthropicAPIKeyAccount(id int64, host string) *service.Account {
	return &service.Account{
		ID:       id,
		Name:     fmt.Sprintf("anthropic-%d", id),
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeAPIKey,
		Status:   service.StatusActive,
		// 缓存键含账号 ID 与凭据指纹，各用例的账号 ID 与 host 互不相同，
		// 同包用例不会互相命中缓存。
		Credentials: map[string]any{"api_key": "key", "base_url": "https://" + host + "/v1"},
	}
}

func postAnthropicModelsBulk(router *gin.Engine, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/accounts/models/sync-anthropic-bulk", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

// requireAll 默认 false：一个账号的 token 过期不该让整批同步失败，而是带着
// 逐账号明细返回其余账号的交集。
func TestSyncAnthropicModelsBulkReportsPerAccountFailures(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{
		bodies: map[string]string{
			"ok-a.example": `{"data":[{"id":"claude-sonnet-5"},{"id":"claude-opus-5"}]}`,
			"ok-b.example": `{"data":[{"id":"claude-sonnet-5"},{"id":"claude-haiku-5"}]}`,
			"dead.example": `{"error":"expired"}`,
		},
		statuses: map[string]int{"dead.example": http.StatusUnauthorized},
	}
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			anthropicAPIKeyAccount(701, "ok-a.example"),
			anthropicAPIKeyAccount(702, "ok-b.example"),
			anthropicAPIKeyAccount(703, "dead.example"),
		},
	}
	router := setupAnthropicModelsBulkRouter(svc, upstream)

	rec := postAnthropicModelsBulk(router, `{"account_ids":[701,702,703]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data struct {
			Models       []string                       `json:"models"`
			Failures     []anthropicAccountModelFailure `json:"failures"`
			AccountCount int                            `json:"account_count"`
			Aggregation  string                         `json:"aggregation"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []string{"claude-sonnet-5"}, resp.Data.Models)
	require.Equal(t, "intersection", resp.Data.Aggregation)
	require.Equal(t, 3, resp.Data.AccountCount)
	require.Len(t, resp.Data.Failures, 1)
	require.Equal(t, int64(703), resp.Data.Failures[0].AccountID)
	require.Equal(t, "anthropic-703", resp.Data.Failures[0].Name)
	require.NotEmpty(t, resp.Data.Failures[0].Error)
}

func TestFetchAnthropicModelsFromAccountsRequireAllRejectsIneligibleAccount(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{
		bodies: map[string]string{
			"require-all-active.example": `{"data":[{"id":"claude-sonnet-5"}]}`,
		},
	}
	accountTestSvc := service.NewAccountTestService(
		nil, nil, nil, nil, nil, upstream,
		&config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		nil,
	)
	active := anthropicAPIKeyAccount(704, "require-all-active.example")
	inactive := anthropicAPIKeyAccount(705, "require-all-inactive.example")
	inactive.Status = service.StatusDisabled

	result, err := fetchAnthropicModelsFromAccounts(
		context.Background(), accountTestSvc, []*service.Account{active, inactive},
		anthropicModelAggregationIntersection, true,
	)

	require.Error(t, err)
	require.Empty(t, result.Models)
	require.Len(t, result.Failures, 1)
	require.Equal(t, inactive.ID, result.Failures[0].AccountID)
}

func TestSyncAnthropicModelsBulkUnionKeepsEveryAccountModel(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{
		"union-a.example": `{"data":[{"id":"claude-sonnet-5"}]}`,
		"union-b.example": `{"data":[{"id":"claude-opus-5"}]}`,
	}}
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			anthropicAPIKeyAccount(711, "union-a.example"),
			anthropicAPIKeyAccount(712, "union-b.example"),
		},
	}
	router := setupAnthropicModelsBulkRouter(svc, upstream)

	rec := postAnthropicModelsBulk(router, `{"account_ids":[711,712],"aggregation":"union"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data struct {
			Models []string `json:"models"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.ElementsMatch(t, []string{"claude-sonnet-5", "claude-opus-5"}, resp.Data.Models)
}

// 筛选模式复用批量编辑的 ResolveBulkUpdateTargetIDs，两个入口对 "ungrouped"、
// 隐私模式这类边角条件的理解不能各写一份。
func TestSyncAnthropicModelsBulkResolvesFiltersThroughBulkUpdateTargets(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{
		"filtered.example": `{"data":[{"id":"claude-sonnet-5"}]}`,
	}}
	stub := newStubAdminService()
	stub.bulkUpdateTargetIDs = []int64{721}
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: stub,
		accounts:         []*service.Account{anthropicAPIKeyAccount(721, "filtered.example")},
	}
	router := setupAnthropicModelsBulkRouter(svc, upstream)

	rec := postAnthropicModelsBulk(router,
		`{"filters":{"platform":"anthropic","group":"ungrouped"},"aggregation":"union"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotNil(t, stub.lastBulkUpdateTargetFilters)
	require.Equal(t, "anthropic", stub.lastBulkUpdateTargetFilters.Platform)
	require.Equal(t, "ungrouped", stub.lastBulkUpdateTargetFilters.Group)
}

// 整批失败时管理员最需要的恰恰是逐账号明细，而错误响应带不了它：改成 200 +
// error + failures，模型列表为空。
func TestSyncAnthropicModelsBulkKeepsFailuresWhenEveryAccountFails(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{
		bodies: map[string]string{
			"dead-a.example": `{"error":"expired"}`,
			"dead-b.example": `{"error":"expired"}`,
		},
		statuses: map[string]int{
			"dead-a.example": http.StatusUnauthorized,
			"dead-b.example": http.StatusUnauthorized,
		},
	}
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			anthropicAPIKeyAccount(761, "dead-a.example"),
			anthropicAPIKeyAccount(762, "dead-b.example"),
		},
	}
	router := setupAnthropicModelsBulkRouter(svc, upstream)

	rec := postAnthropicModelsBulk(router, `{"account_ids":[761,762]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data struct {
			Models   []string                       `json:"models"`
			Failures []anthropicAccountModelFailure `json:"failures"`
			Error    string                         `json:"error"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Data.Models)
	require.NotEmpty(t, resp.Data.Error)
	require.Len(t, resp.Data.Failures, 2)
	require.ElementsMatch(t, []int64{761, 762},
		[]int64{resp.Data.Failures[0].AccountID, resp.Data.Failures[1].AccountID})
}

// require_all 下的部分失败同样要带明细：管理员得知道该去修哪个账号。
func TestSyncAnthropicModelsBulkKeepsFailuresWhenRequireAllRejectsTheBatch(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{
		bodies: map[string]string{
			"strict-ok.example":   `{"data":[{"id":"claude-sonnet-5"}]}`,
			"strict-dead.example": `{"error":"expired"}`,
		},
		statuses: map[string]int{"strict-dead.example": http.StatusUnauthorized},
	}
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			anthropicAPIKeyAccount(763, "strict-ok.example"),
			anthropicAPIKeyAccount(764, "strict-dead.example"),
		},
	}
	router := setupAnthropicModelsBulkRouter(svc, upstream)

	rec := postAnthropicModelsBulk(router, `{"account_ids":[763,764],"require_all":true}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data struct {
			Models   []string                       `json:"models"`
			Failures []anthropicAccountModelFailure `json:"failures"`
			Error    string                         `json:"error"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Data.Models)
	require.Contains(t, resp.Data.Error, "1 of 2 accounts")
	require.Len(t, resp.Data.Failures, 1)
	require.Equal(t, int64(764), resp.Data.Failures[0].AccountID)
}

// 交集为空是另一条错误分支，同样要能让前端分辨出「失败」而不是「上游真的没模型」。
func TestSyncAnthropicModelsBulkReportsAnEmptyIntersection(t *testing.T) {
	upstream := &anthropicModelsBulkUpstream{bodies: map[string]string{
		"disjoint-a.example": `{"data":[{"id":"claude-sonnet-5"}]}`,
		"disjoint-b.example": `{"data":[{"id":"claude-opus-5"}]}`,
	}}
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			anthropicAPIKeyAccount(765, "disjoint-a.example"),
			anthropicAPIKeyAccount(766, "disjoint-b.example"),
		},
	}
	router := setupAnthropicModelsBulkRouter(svc, upstream)

	rec := postAnthropicModelsBulk(router, `{"account_ids":[765,766]}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data struct {
			Models []string `json:"models"`
			Error  string   `json:"error"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Empty(t, resp.Data.Models)
	require.Contains(t, resp.Data.Error, "no common models")
}

// 展开筛选条件的失败来自数据库，错误原文不能透给客户端。
func TestSyncAnthropicModelsBulkHidesTargetResolutionErrors(t *testing.T) {
	stub := newStubAdminService()
	stub.resolveBulkUpdateTargetErr = errors.New("pq: relation \"accounts\" does not exist")
	router := setupAnthropicModelsBulkRouter(
		&anthropicModelsBulkAdminService{stubAdminService: stub},
		&anthropicModelsBulkUpstream{},
	)

	rec := postAnthropicModelsBulk(router, `{"filters":{"platform":"anthropic"}}`)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "does not exist")
}

func TestSyncAnthropicModelsBulkRejectsNonAnthropicSelection(t *testing.T) {
	svc := &anthropicModelsBulkAdminService{
		stubAdminService: newStubAdminService(),
		accounts: []*service.Account{
			anthropicAPIKeyAccount(731, "mixed.example"),
			{ID: 732, Name: "openai", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive},
		},
	}
	router := setupAnthropicModelsBulkRouter(svc, &anthropicModelsBulkUpstream{})

	rec := postAnthropicModelsBulk(router, `{"account_ids":[731,732]}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Anthropic-only")
}

func TestSyncAnthropicModelsBulkRequiresASelection(t *testing.T) {
	router := setupAnthropicModelsBulkRouter(newStubAdminService(), &anthropicModelsBulkUpstream{})

	rec := postAnthropicModelsBulk(router, `{}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "account_ids or filters is required")
}
