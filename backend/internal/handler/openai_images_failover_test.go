//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type openAIImagesFailoverAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r openAIImagesFailoverAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r openAIImagesFailoverAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r openAIImagesFailoverAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r openAIImagesFailoverAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r openAIImagesFailoverAccountRepo) accountsForPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

type openAIImagesFailoverHTTPUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
	respond    func(*http.Request) (*http.Response, error)
}

func (u *openAIImagesFailoverHTTPUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.accountIDs = append(u.accountIDs, accountID)
	u.mu.Unlock()
	if u.respond != nil {
		return u.respond(req)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_img_failover"},
		},
		Body: io.NopCloser(bytes.NewBufferString(
			"data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"image backend unavailable\"}}\n\n",
		)),
	}, nil
}

func (u *openAIImagesFailoverHTTPUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func TestOpenAIGatewayHandlerImages_ServerErrorFailsOverAndReturnsClearErrorWhenExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(3130)
	accounts := []service.Account{
		{
			ID:          1,
			Name:        "image-account-1",
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
			Name:        "image-account-2",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    1,
			Credentials: map[string]any{"access_token": "token-2"},
		},
	}
	upstream := &openAIImagesFailoverHTTPUpstream{}
	handler := newOpenAIEndpointRetryTestHandler(t, accounts, upstream, &config.Config{RunMode: config.RunModeSimple})

	body := []byte(`{"model":"gpt-image-1","prompt":"draw a cat","quality":"high","size":"1536x1024"}`)
	core, observedLogs := observer.New(zap.DebugLevel)
	requestCtx := logger.IntoContext(context.Background(), zap.New(core))
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body)).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})

	handler.Images(c)

	accountSelectingLogs := observedLogs.FilterMessage("openai.images.account_selecting").All()
	require.NotEmpty(t, accountSelectingLogs)
	loggedFields := make(map[string]string)
	for _, field := range accountSelectingLogs[0].Context {
		loggedFields[field.Key] = field.String
	}
	require.Equal(t, "high", loggedFields["img_quality"])
	require.Equal(t, "1536x1024", loggedFields["img_size"])
	require.NotContains(t, loggedFields, "prompt")

	require.Equal(t, []int64{1, 2}, upstream.calls())
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, "upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
	require.Equal(t, "Upstream service temporarily unavailable", gjson.GetBytes(rec.Body.Bytes(), "error.message").String())

	rawEvents, ok := c.Get(service.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 2)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, "failover", events[1].Kind)
}

func newOpenAIEndpointRetryTestHandler(t *testing.T, accounts []service.Account, upstream service.HTTPUpstream, cfg *config.Config) *OpenAIGatewayHandler {
	t.Helper()
	accountRepo := openAIImagesFailoverAccountRepo{accounts: accounts}
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
	cleanupHandlerBillingCache(t, billingService)
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

type imagePartialUsageBillingRepo struct {
	service.UsageBillingRepository
	commands chan *service.UsageBillingCommand
}

type partialImageReadErrorBody struct {
	data   []byte
	offset int
}

func (r *partialImageReadErrorBody) Read(p []byte) (int, error) {
	if r.offset < len(r.data) {
		n := copy(p, r.data[r.offset:])
		r.offset += n
		return n, nil
	}
	return 0, io.ErrUnexpectedEOF
}

func (r *partialImageReadErrorBody) Close() error { return nil }

func (r *imagePartialUsageBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.commands <- cmd
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestOpenAIGatewayHandlerImages_PartialImageResultIsBilledOnStreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(3131)
	imagePrice := 0.5
	billingRepo := &imagePartialUsageBillingRepo{commands: make(chan *service.UsageBillingCommand, 1)}
	accounts := []service.Account{{
		ID: 1, Name: "image-account", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "token-1"},
	}}
	upstream := &openAIImagesFailoverHTTPUpstream{respond: func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"req_partial_image"}},
			Body: &partialImageReadErrorBody{data: []byte("data: {\"type\":\"image_generation.completed\",\"b64_json\":\"aW1hZ2U=\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"output_tokens_details\":{\"image_tokens\":2}}}\n\n")},
		}, nil
	}}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
	gateway := service.NewOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: accounts}, usage, billingRepo, nil, nil, nil, nil,
		cfg, nil, nil, service.NewBillingService(cfg, nil), nil, nil, upstream, &service.DeferredService{},
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(newHandlerInflightCache(100), nil, nil, nil, nil, nil, cfg, nil)
	cleanupHandlerBillingCache(t, billingCache)
	pool := service.ProvideUsageRecordWorkerPool(cfg, nil, billingRepo, service.NewBillingService(cfg, nil), nil, nil, nil, nil)
	startHandlerUsageWorker(t, pool)
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billingCache, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), pool, nil, nil, nil, cfg)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"draw","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 99, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true, ImageCountBilling: true, ImageRateIndependent: true, ImagePrice1K: &imagePrice}, User: &service.User{ID: 100, Balance: 100}})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100})
	h.Images(c)
	stopHandlerUsageWorker(t, pool)

	select {
	case cmd := <-billingRepo.commands:
		require.Equal(t, "req_partial_image", cmd.RequestID)
		require.Equal(t, 1, cmd.ImageCount)
	case <-time.After(time.Second):
		t.Fatal("partial image result was not billed")
	}
}

func TestOpenAIGatewayHandlerRetryAfterWaitBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct {
		name string
		path string
		body string
	}{
		{name: "images", path: "/v1/images/generations", body: `{"model":"gpt-image-1","prompt":"draw a cat","size":"1024x1024"}`},
		{name: "alpha search", path: "/v1/alpha/search", body: `{"model":"gpt-5.6-sol","commands":{}}`},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, scenario := range []string{"long retry after", "first output deadline", "client cancels wait"} {
				t.Run(scenario, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
					defer cancel()
					cfg := &config.Config{RunMode: config.RunModeSimple}
					retryAfter := "90"
					if scenario == "first output deadline" {
						cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
						retryAfter = "2"
					}
					if scenario == "client cancels wait" {
						retryAfter = "2"
					}
					upstream := &openAIImagesFailoverHTTPUpstream{}
					upstream.respond = func(_ *http.Request) (*http.Response, error) {
						if scenario == "client cancels wait" {
							timer := time.AfterFunc(20*time.Millisecond, cancel)
							t.Cleanup(func() { timer.Stop() })
						}
						return &http.Response{
							StatusCode: http.StatusTooManyRequests,
							Header:     http.Header{"Content-Type": {"application/json"}, "Retry-After": {retryAfter}},
							Body:       io.NopCloser(bytes.NewBufferString(`{"error":{"code":"rate_limit_exceeded","message":"temporarily limited"}}`)),
						}, nil
					}
					accounts := []service.Account{{
						ID: 1, Name: "retry-account", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Status: service.StatusActive, Schedulable: true,
						Credentials: map[string]any{"access_token": "token-1", "chatgpt_account_id": "chatgpt-account"},
					}}
					handler := newOpenAIEndpointRetryTestHandler(t, accounts, upstream, cfg)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, endpoint.path, bytes.NewBufferString(endpoint.body)).WithContext(ctx)
					c.Request.Header.Set("Content-Type", "application/json")
					groupID := int64(3130)
					c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
						ID: 99, GroupID: &groupID,
						Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, AllowImageGeneration: true},
						User:  &service.User{ID: 100},
					})
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
					if endpoint.name == "images" {
						handler.Images(c)
					} else {
						handler.AlphaSearch(c)
					}
					require.Equal(t, []int64{1}, upstream.calls(), "no upstream request may start after the wait is rejected or cancelled")
					if scenario == "client cancels wait" {
						require.ErrorIs(t, ctx.Err(), context.Canceled)
						require.Equal(t, statusClientClosedRequest, c.Writer.Status())
					} else {
						require.NoError(t, ctx.Err(), "the handler must stop before the client timeout")
						require.Equal(t, http.StatusServiceUnavailable, rec.Code)
						require.Equal(t, "retry_budget_exhausted", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
					}
				})
			}
		})
	}
}
