package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const capacityErrorBody = `{"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`

func capacityFailoverError() *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode: http.StatusServiceUnavailable, ResponseBody: []byte(capacityErrorBody),
		RetryableOnSameAccount: true, RequestScopedTransient: true,
	}
}

func TestOpenAICapacityRetryBudgetAcrossAccounts(t *testing.T) {
	state := NewFailoverState(10, false, nil)
	unscheduler := &mockTempUnscheduler{}
	for attempt := 0; attempt <= openAICapacityMaxRetries; attempt++ {
		want := FailoverContinue
		if attempt == openAICapacityMaxRetries {
			want = FailoverExhausted
		}
		// An account that disables local retries still cannot restart the
		// request-level budget by switching to a different account.
		require.Equal(t, want, state.HandleFailoverError(context.Background(), unscheduler,
			int64(attempt+1), service.PlatformOpenAI, 0, capacityFailoverError()))
	}
	require.Equal(t, openAICapacityMaxRetries, state.SwitchCount)
}

func TestOpenAICapacityRetryBudgetWindowAndTurnReset(t *testing.T) {
	budget := openAICapacityRetryBudget{started: time.Now().Add(-openAICapacityRetryWindow)}
	require.False(t, budget.allow(nil, capacityFailoverError(), 0))
	require.True(t, budget.allow(nil, &service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}, 0))
	require.True(t, budget.allow(nil, nil, 0))
	completed := &service.OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.completed"}
	budget.observeTurn(completed, nil)
	for retry := 0; retry < openAICapacityMaxRetries; retry++ {
		require.True(t, budget.allow(nil, capacityFailoverError(), 0))
	}
	require.False(t, budget.allow(nil, capacityFailoverError(), 0))
	budget.observeTurn(&service.OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.failed"}, nil)
	require.False(t, budget.allow(nil, capacityFailoverError(), 0), "a failed terminal event must not replenish retries")
	budget.observeTurn(completed, context.Canceled)
	require.False(t, budget.allow(nil, capacityFailoverError(), 0))
	budget.observeTurn(nil, nil)
	require.False(t, budget.allow(nil, capacityFailoverError(), 0))
	budget.observeTurn(completed, nil)
	require.True(t, budget.allow(nil, capacityFailoverError(), 0), "a successful WS turn gives the next turn a fresh budget")
}

type capacityRetryHTTPUpstream struct {
	service.HTTPUpstream
	accountIDs []int64
	stream     bool
}

func (u *capacityRetryHTTPUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	status, contentType, body := http.StatusServiceUnavailable, "application/json", capacityErrorBody
	if u.stream {
		status, contentType = http.StatusOK, "text/event-stream"
		body = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_overload\"}}\n\n" +
			"data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}}\n\n"
	}
	return &http.Response{
		StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}},
		Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestOpenAIResponsesCapacityRetriesAreBounded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		retryLimit int
		stream     bool
		wantIDs    []int64
	}{
		{"HTTP account switches", 0, false, []int64{9910, 9911, 9912, 9913}},
		{"SSE account switches", 0, true, []int64{9910, 9911, 9912, 9913}},
		{"HTTP high local retry setting", 10, false, []int64{9910, 9910, 9910, 9910}},
		{"SSE high local retry setting", 10, true, []int64{9910, 9910, 9910, 9910}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(4203)
			var accounts []service.Account
			for i := 0; i < 11; i++ {
				accounts = append(accounts, service.Account{
					ID: int64(9910 + i), Name: "capacity-test", Platform: service.PlatformOpenAI,
					Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
					Priority: i + 1, GroupIDs: []int64{groupID},
					Credentials: map[string]any{
						"api_key": "sk-test", "base_url": "https://api.example.test",
						"pool_mode": true, "pool_mode_retry_count": tc.retryLimit,
					},
					Extra: map[string]any{"openai_passthrough": true},
				})
			}
			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxAccountSwitches = 10
			accountRepo := &openAIWSFailoverHandlerAccountRepoStub{accounts: accounts}
			upstream := &capacityRetryHTTPUpstream{stream: tc.stream}
			billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billing.Stop)
			gateway := service.NewOpenAIGatewayService(accountRepo, nil, nil, nil, nil, nil, nil,
				cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(nil), billing,
				service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			body := `{"model":"gpt-5.2","input":"hello","stream":false}`
			if tc.stream {
				body = `{"model":"gpt-5.2","input":"hello","stream":true}`
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{
				ID: 1803, GroupID: &groupID, User: &service.User{ID: 1703, Status: service.StatusActive},
				Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
			})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1703})
			h.Responses(c)
			require.Equal(t, tc.wantIDs, upstream.accountIDs)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Contains(t, rec.Body.String(), "Our servers are currently overloaded")
			require.NotContains(t, rec.Body.String(), "server_is_overloaded")
			require.NotContains(t, rec.Body.String(), "response.created")
		})
	}
}
