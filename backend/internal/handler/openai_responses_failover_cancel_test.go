//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// openAIResponsesFailoverCancelUpstream 固定返回 HTTP 520，可在首次上游调用时
// 触发回调（用于模拟“上游在途期间客户端断开”）。
type openAIResponsesFailoverCancelUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
	onFirstDo  func()
	firstError error
	status     int
}

func (u *openAIResponsesFailoverCancelUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	first := len(u.accountIDs) == 1
	u.mu.Unlock()
	if first && u.onFirstDo != nil {
		u.onFirstDo()
	}
	if first && u.firstError != nil {
		return nil, u.firstError
	}
	status := u.status
	if status == 0 {
		status = 520
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/html"}},
		Body:       io.NopCloser(bytes.NewBufferString("<html>520: unknown error</html>")),
	}, nil
}

func (u *openAIResponsesFailoverCancelUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func newOpenAIResponsesFailoverTestHandler(t *testing.T, upstream service.HTTPUpstream) *OpenAIGatewayHandler {
	t.Helper()
	accounts := []service.Account{
		{
			ID:          1,
			Name:        "responses-account-1",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    0,
			Credentials: map[string]any{"access_token": "token-1"},
		},
		{
			ID:          2,
			Name:        "responses-account-2",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    1,
			Credentials: map[string]any{"access_token": "token-2"},
		},
	}
	accountRepo := openAIImagesFailoverAccountRepo{accounts: accounts}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	concurrencyService := service.NewConcurrencyService(nil)
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	handler.maxAccountSwitches = 10
	return handler
}

func newOpenAIResponsesFailoverTestContext(t *testing.T, ctx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	groupID := int64(3131)
	body := []byte(`{"model":"gpt-5.1","stream":false,"input":"hello"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
		},
		User: &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
	return c, rec
}

// TestOpenAIGatewayHandlerResponses_FailoverAbortsWhenClientDisconnected 复现
// #4257：客户端在上游请求在途期间断开，上游随后返回可 failover 的 520。
// 期望：不再用已取消的 context 重新选号（不触达账号 2）、不把取消误报成
// 502 账号耗尽、请求按 499 归类。
func TestOpenAIGatewayHandlerResponses_FailoverAbortsWhenClientDisconnected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &openAIResponsesFailoverCancelUpstream{onFirstDo: cancel}
	handler := newOpenAIResponsesFailoverTestHandler(t, upstream)
	c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)

	handler.Responses(c)

	require.Equal(t, []int64{1}, upstream.calls(), "客户端断开后不应再切换到账号 2")
	require.Equal(t, statusClientClosedRequest, c.Writer.Status(), "应按 499 归类")
	require.Zero(t, rec.Body.Len(), "不应写入 502 错误响应体")

	_, hasFinalUpstreamErr := c.Get(service.OpsUpstreamStatusCodeKey)
	require.False(t, hasFinalUpstreamErr, "不应记录 failover 耗尽的上游错误终态")

	// 真实发生过的 520 应保留 failover 事件（service 层在返回 failover 错误前记录）
	rawEvents, ok := c.Get(service.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, 520, events[0].UpstreamStatusCode)
}

// TestOpenAIGatewayHandlerResponses_FailoverContinuesForConnectedClient 回归
// 守卫：客户端在线时 failover 行为不变——切换到账号 2，两个账号都 520 后按
// 耗尽返回 502。
func TestOpenAIGatewayHandlerResponses_FailoverContinuesForConnectedClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := &openAIResponsesFailoverCancelUpstream{}
	handler := newOpenAIResponsesFailoverTestHandler(t, upstream)
	c, rec := newOpenAIResponsesFailoverTestContext(t, nil)

	handler.Responses(c)

	require.Equal(t, []int64{1, 2}, upstream.calls(), "在线客户端应正常切换账号")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
}

func TestOpenAIHostedToolsFailoverRequiresSendEvidence(t *testing.T) {
	for _, unsent := range []bool{false, true} {
		for _, plugin := range []bool{false, true} {
			upstream := &openAIResponsesFailoverCancelUpstream{firstError: &service.OpenAITransportAttemptError{Cause: errors.New("dial tcp: i/o timeout"), Unsent: unsent}}
			if plugin {
				upstream.firstError = &service.PluginTransportError{Code: "dial_failure", Message: "dial tcp: i/o timeout", RequestSent: !unsent}
			}
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			c, _ := newOpenAIResponsesFailoverTestContext(t, nil)
			c.Request.Body = io.NopCloser(bytes.NewBufferString(`{"model":"gpt-5.1","stream":false,"input":"hello","tools":[{"type":"code_interpreter","container":{"type":"auto"}}]}`))
			h.Responses(c)
			if unsent {
				require.Equal(t, []int64{1, 2}, upstream.calls())
			} else {
				require.Equal(t, []int64{1}, upstream.calls())
			}
		}
	}
}

func TestOpenAIResponsesReadOnlySearchFailoverStopReasons(t *testing.T) {
	for _, tc := range []struct {
		name, tools string
		budget      int
		switchLimit int
		calls       []int64
		reason      service.OpenAIRetryStopReason
	}{
		{"web search", `[{"type":"web_search"}]`, 0, 10, []int64{1, 2}, service.OpenAIRetryStopNoAvailableAccount},
		{"file search", `[{"type":"file_search","vector_store_ids":["vs_test"]}]`, 0, 10, []int64{1, 2}, service.OpenAIRetryStopNoAvailableAccount},
		{"local namespace", `[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"shell"},{"type":"custom","name":"apply_patch"}]}]`, 0, 10, []int64{1, 2}, service.OpenAIRetryStopNoAvailableAccount},
		{"preview search", `[{"type":"web_search_preview"}]`, 0, 10, []int64{1, 2}, service.OpenAIRetryStopNoAvailableAccount},
		{"versioned preview search", `[{"type":"web_search_preview_2025_03_11"}]`, 0, 10, []int64{1, 2}, service.OpenAIRetryStopNoAvailableAccount},
		{"namespace containing mcp", `[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"shell"},{"type":"mcp"}]}]`, 0, 10, []int64{1}, service.OpenAIRetryStopReplayUnsafe},
		{"mixed read-only and mcp", `[{"type":"web_search"},{"type":"mcp"}]`, 0, 10, []int64{1}, service.OpenAIRetryStopReplayUnsafe},
		{"unknown tool", `[{"type":"future_tool"}]`, 0, 10, []int64{1}, service.OpenAIRetryStopReplayUnsafe},
		{"turn budget", `[{"type":"web_search"}]`, 1, 10, []int64{1}, service.OpenAIRetryStopTurnBudget},
		{"switch limit", `[{"type":"web_search"}]`, 0, 1, []int64{1, 2}, service.OpenAIRetryStopAccountSwitchLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &openAIResponsesFailoverCancelUpstream{status: http.StatusInternalServerError}
			h := newOpenAIResponsesFailoverTestHandler(t, upstream)
			h.cfg.Gateway.OpenAITurnMaxAttempts = tc.budget
			h.maxAccountSwitches = tc.switchLimit
			c, _ := newOpenAIResponsesFailoverTestContext(t, nil)
			c.Request.Body = io.NopCloser(bytes.NewBufferString(`{"model":"gpt-5.1","stream":true,"input":"hello","tools":` + tc.tools + `}`))
			h.Responses(c)
			require.Equal(t, tc.calls, upstream.calls())
			raw, ok := c.Get(service.OpsUpstreamErrorsKey)
			require.True(t, ok)
			events := raw.([]*service.OpsUpstreamErrorEvent)
			require.Len(t, events, len(tc.calls)+1)
			stop := events[len(events)-1]
			require.Equal(t, service.OpsUpstreamRetryStopped, stop.Kind)
			require.Equal(t, string(tc.reason), stop.Reason)
			if tc.reason == service.OpenAIRetryStopReplayUnsafe {
				want := "mcp"
				if tc.name == "unknown tool" {
					want = "unknown_tool"
				}
				require.Equal(t, want, gjson.Get(stop.Detail, "blocking_tool_kind").String())
				require.NotContains(t, stop.Detail, "future_tool")
			}
			require.Zero(t, stop.UpstreamStatusCode, "a stop decision is not another provider failure")
			require.Equal(t, http.StatusInternalServerError, service.LastOpsUpstreamAttempt(events).UpstreamStatusCode)
		})
	}
}

func TestOpenAIMessagesCanceledFailoverDoesNotMarkExhaustion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &openAIResponsesFailoverCancelUpstream{onFirstDo: cancel}
	h := newOpenAIResponsesFailoverTestHandler(t, upstream)
	h.cfg.Gateway.OpenAITurnMaxAttempts = 3
	c, rec := newOpenAIResponsesFailoverTestContext(t, ctx)
	key, _ := middleware2.GetAPIKeyFromContext(c)
	key.Group.AllowMessagesDispatch = true
	c.Request.Body = io.NopCloser(bytes.NewBufferString(`{"model":"gpt-5.1","max_tokens":100,"messages":[{"role":"user","content":"hello"}]}`))
	h.Messages(c)
	require.Equal(t, []int64{1}, upstream.calls())
	require.Equal(t, statusClientClosedRequest, c.Writer.Status())
	require.Zero(t, rec.Body.Len())
	require.Empty(t, service.GetOpsStreamErrors(c))
	_, final := c.Get(service.OpsUpstreamStatusCodeKey)
	require.False(t, final)
}
