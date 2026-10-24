package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUsageUnrestrictedIncludesWeeklyWindowStart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)

	weeklyWindowStart := time.Date(2026, time.July, 13, 0, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{
		WeeklyWindowStart: &weeklyWindowStart,
	})

	handler := &GatewayHandler{}
	handler.usageUnrestricted(
		c,
		context.Background(),
		&service.APIKey{Group: &service.Group{
			Name:             "Weekly plan",
			SubscriptionType: service.SubscriptionTypeSubscription,
		}},
		middleware.AuthSubject{},
		nil,
		nil,
		nil,
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Subscription struct {
			WeeklyWindowStart *time.Time `json:"weekly_window_start"`
		} `json:"subscription"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.NotNil(t, response.Subscription.WeeklyWindowStart)
	require.True(t, weeklyWindowStart.Equal(*response.Subscription.WeeklyWindowStart))
}

type anthropicPartialUsageUpstream struct {
	service.HTTPUpstream
	responses      []string
	accountIDs     []int64
	beforeResponse func()
}

func (u *anthropicPartialUsageUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	if u.beforeResponse != nil {
		u.beforeResponse()
	}
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
		_ = req.Body.Close()
	}
	u.accountIDs = append(u.accountIDs, accountID)
	body := u.responses[min(len(u.accountIDs)-1, len(u.responses)-1)]
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{
		"Content-Type": {"text/event-stream"}, "X-Request-Id": {"req_sse_usage"},
	}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func (u *anthropicPartialUsageUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

type anthropicPartialUsageBillingRepo struct {
	service.UsageBillingRepository
	commands []service.UsageBillingCommand
}

type anthropicPartialUsageAccountRepo struct {
	*openAIWSFailoverHandlerAccountRepoStub
	beforeSelect func()
}

func (r *anthropicPartialUsageAccountRepo) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]service.Account, error) {
	if r.beforeSelect != nil {
		r.beforeSelect()
	}
	var accounts []service.Account
	for _, platform := range platforms {
		matched, _ := r.ListSchedulableByPlatform(ctx, platform)
		accounts = append(accounts, matched...)
	}
	return accounts, nil
}

func (r *anthropicPartialUsageBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.commands = append(r.commands, *cmd)
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestGatewayHandlerQuotaGuardStopsBeforeDispatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "responses"} {
		t.Run(endpoint, func(t *testing.T) {
			now := time.Now().UTC().Add(-time.Second)
			identity := fmt.Sprintf("%x", sha256.Sum256([]byte("test-token")))
			account := service.Account{ID: 501, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
				Status: service.StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"access_token": "test-token", "account_scheduling_threshold": 80}, Extra: map[string]any{}}
			for key, value := range map[string]any{"utilization": 79.0, "resets_at": now.Add(time.Hour).Format(time.RFC3339)} {
				account.Extra["anthropic_observation_five_hour."+key] = map[string]any{"value": value, "source": "upstream_headers", "sampled_at": now, "identity": identity}
			}
			accounts := &anthropicPartialUsageAccountRepo{openAIWSFailoverHandlerAccountRepoStub: &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{account}}}
			upstream := &anthropicPartialUsageUpstream{responses: []string{"unexpected dispatch"}}
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			billingCache := service.NewBillingCacheService(newHandlerInflightCache(100), nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			gw := service.NewGatewayService(accounts, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), &service.RateLimitService{}, nil, nil, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			h := &GatewayHandler{gatewayService: gw, billingCacheService: billingCache, cfg: cfg,
				concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(nil), SSEPingFormatClaude, 0)}
			text := strings.Repeat(" word", 100010)
			body := fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":32,"messages":[{"role":"user","content":%q}]}`, text)
			if endpoint == "responses" {
				body = fmt.Sprintf(`{"model":"claude-sonnet-4-5","input":%q}`, text)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 3, Status: service.StatusActive, User: &service.User{ID: 3, Balance: 100}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 3})
			if endpoint == "responses" {
				h.Responses(c)
			} else {
				h.Messages(c)
			}
			require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), service.SchedulingQuotaRequestTooLarge)
			require.Empty(t, upstream.accountIDs, "local protection cannot dispatch or penalize an upstream")
			require.Empty(t, accounts.rateLimitedIDs)
		})
	}
}

func TestGatewayHandlerMessages_SSEErrorPartialUsageSubmittedOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const start = "event: message_start\ndata: {\"type\":\"message_start\",\n" +
		"data: \"message\":{\"id\":\"msg_partial\",\"usage\":{\"input_tokens\":11,\"cache_read_input_tokens\":7,\"output_tokens\":1}}}\n\n"
	const textDelta = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial reply\"}}\n\n"
	const delta = "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n"
	const errorData = `{"type":"error",` + "\n" + `"error":{"type":"permission_error","message":"original failure"},"request_id":"req_error"}`
	errorFrame := ": original error\nid: cc_ses_error\nretry: 250\ndata: " + strings.ReplaceAll(errorData, "\n", "\ndata: ") + "\n\n"
	const stop = "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, tc := range []struct {
		name       string
		prefix     string
		wantOutput int
		wantBills  int
		wantCalls  int
		keepalive  bool
	}{
		{"usage_only", start, 1, 1, 1, false},
		{"cumulative_usage", start + textDelta + delta + delta, 5, 1, 1, false},
		{"text_without_usage", textDelta, 0, 0, 1, false},
		{"tool_without_usage", "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool_1\",\"name\":\"write_file\"}}\n\n", 0, 0, 1, false},
		{"pre_output_retry", "data: {\"type\":\"message_start\",\"message\":{\"id\":\"discarded_identity\"}}\n\n", 5, 1, 2, false},
		{"keepalive_preparation_wait_and_retry", "data: {\"type\":\"message_start\",\"message\":{\"id\":\"discarded_identity\"}}\n\n", 5, 1, 2, true},
		{"keepalive_partial_usage_failure", start, 1, 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			accounts := &anthropicPartialUsageAccountRepo{openAIWSFailoverHandlerAccountRepoStub: &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{
				{ID: 501, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
					Credentials: map[string]any{"access_token": "test-token"}},
				{ID: 502, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
					Credentials: map[string]any{"access_token": "test-token"}},
			}}}
			upstream := &anthropicPartialUsageUpstream{responses: []string{
				tc.prefix + errorFrame + stop,
				start + delta + stop,
			}}
			if tc.keepalive {
				cfg.Gateway.StreamKeepaliveInterval = 1
				accounts.beforeSelect = func() { time.Sleep(1100 * time.Millisecond) }
				upstream.beforeResponse = func() { time.Sleep(1100 * time.Millisecond) }
			}
			usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
			bills := &anthropicPartialUsageBillingRepo{}
			billingCache := service.NewBillingCacheService(newHandlerInflightCache(100), nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			gw := service.NewGatewayService(accounts, nil, usage, bills, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), &service.RateLimitService{}, nil, nil, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
				WorkerCount: 1, QueueSize: 8, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
			})
			t.Cleanup(pool.Stop)
			h := &GatewayHandler{gatewayService: gw, billingCacheService: billingCache, cfg: cfg,
				concurrencyHelper:     NewConcurrencyHelper(service.NewConcurrencyService(nil), SSEPingFormatClaude, 0),
				usageRecordWorkerPool: pool, maxAccountSwitches: 1}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(
				`{"model":"claude-sonnet-4-5","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 3, Status: service.StatusActive,
				User: &service.User{ID: 3, Balance: 100}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 3})

			h.Messages(c)
			pool.Stop() // Wait for every submitted billing task before asserting exact counts.
			responseBody := rec.Body.String()
			if tc.keepalive {
				require.GreaterOrEqual(t, strings.Count(responseBody, ": keepalive\n\n"), 2*tc.wantCalls)
				responseBody = strings.ReplaceAll(responseBody, ": keepalive\n\n", "")
			}

			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, upstream.accountIDs, tc.wantCalls, "committed output must not trigger another request")
			require.Len(t, bills.commands, tc.wantBills, "count actual RecordUsage billing submissions")
			require.Len(t, usage.created, tc.wantBills, "count actual usage log writes")
			if tc.wantBills > 0 {
				log := <-usage.created
				require.Equal(t, 11, log.InputTokens)
				require.Equal(t, 7, log.CacheReadTokens)
				require.Equal(t, tc.wantOutput, log.OutputTokens)
				require.Equal(t, upstream.accountIDs[len(upstream.accountIDs)-1], log.AccountID)
				cmd := bills.commands[0]
				require.Equal(t, "req_sse_usage", cmd.RequestID)
				require.Equal(t, 11, cmd.InputTokens)
				require.Equal(t, 7, cmd.CacheReadTokens)
				require.Equal(t, tc.wantOutput, cmd.OutputTokens)
				require.Greater(t, cmd.BalanceCost, float64(0), "standard billing must charge the partial usage")
			}
			if tc.wantCalls == 1 {
				require.Equal(t, tc.prefix+errorFrame, responseBody, "preserve one original terminal error without fallback or concatenation")
				require.Equal(t, 1, strings.Count(rec.Body.String(), errorFrame))
				require.Equal(t, 1, strings.Count(rec.Body.String(), "original failure"))
				require.NotContains(t, rec.Body.String(), "event: error\n", "a data-only error must not acquire a synthetic event header")
				require.True(t, service.IsResponseCommitted(c))
			} else {
				require.NotEqual(t, upstream.accountIDs[0], upstream.accountIDs[1])
				require.Equal(t, start+delta+stop, responseBody, "discard metadata and error from the retryable attempt")
			}
		})
	}
}

func TestGatewayHandlerResponses_PartialUsageSubmittedOnceWithoutReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const partial = "event: message_start\ndata: {\"type\":\"message_start\",\n" +
		"data: \"message\":{\"id\":\"msg_partial\",\"content\":[],\"usage\":{\"input_tokens\":11,\"cache_read_input_tokens\":7,\"output_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial reply\"}}\n\n"
	const delta = "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n"
	const streamError = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"permission_error\",\"message\":\"original failure\"}}\n\n"
	for _, tc := range []struct {
		name, suffix string
		stream       bool
	}{
		{"json_eof", "", false},
		{"json_error", streamError, false},
		{"sse_eof", "", true},
		{"sse_error", streamError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			accounts := &anthropicPartialUsageAccountRepo{openAIWSFailoverHandlerAccountRepoStub: &openAIWSFailoverHandlerAccountRepoStub{accounts: []service.Account{
				{ID: 501, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
					Credentials: map[string]any{"access_token": "test-token"}},
				{ID: 502, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
					Credentials: map[string]any{"access_token": "test-token"}},
			}}}
			// Repeated cumulative usage must neither add tokens nor bill twice.
			upstream := &anthropicPartialUsageUpstream{responses: []string{partial + delta + delta + tc.suffix}}
			usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
			bills := &anthropicPartialUsageBillingRepo{}
			billingCache := service.NewBillingCacheService(newHandlerInflightCache(100), nil, nil, nil, nil, nil, cfg, nil)
			t.Cleanup(billingCache.Stop)
			gw := service.NewGatewayService(accounts, nil, usage, bills, nil, nil, nil, nil, cfg, nil, nil,
				service.NewBillingService(cfg, nil), &service.RateLimitService{}, nil, nil, upstream,
				&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			pool := service.NewUsageRecordWorkerPoolWithOptions(service.UsageRecordWorkerPoolOptions{
				WorkerCount: 1, QueueSize: 8, TaskTimeout: time.Second, OverflowPolicy: config.UsageRecordOverflowPolicySync,
			})
			t.Cleanup(pool.Stop)
			h := &GatewayHandler{gatewayService: gw, billingCacheService: billingCache, cfg: cfg,
				concurrencyHelper:     NewConcurrencyHelper(service.NewConcurrencyService(nil), SSEPingFormatClaude, 0),
				usageRecordWorkerPool: pool, maxAccountSwitches: 1}
			body, err := json.Marshal(map[string]any{"model": "claude-sonnet-4-5", "input": "hello", "stream": tc.stream})
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 7, UserID: 3, Status: service.StatusActive,
				User: &service.User{ID: 3, Balance: 100}})
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 3})

			h.Responses(c)
			pool.Stop() // Join the real accounting worker before checking submissions.

			if tc.stream {
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.failed\n"))
			} else {
				require.Equal(t, http.StatusBadGateway, rec.Code)
			}
			require.NotContains(t, rec.Body.String(), "response.completed")
			require.NotContains(t, rec.Body.String(), "[DONE]")
			require.Len(t, upstream.accountIDs, 1, "partial output must not be replayed on either account")
			require.Len(t, bills.commands, 1, "actual RecordUsage submits billing exactly once")
			require.Len(t, usage.created, 1, "actual RecordUsage writes the usage log exactly once")
			cmd, log := bills.commands[0], <-usage.created
			require.Equal(t, "req_sse_usage", cmd.RequestID)
			require.Equal(t, int64(7), cmd.APIKeyID)
			require.Equal(t, upstream.accountIDs[0], log.AccountID)
			require.Equal(t, 11, cmd.InputTokens)
			require.Equal(t, 7, cmd.CacheReadTokens)
			require.Equal(t, 5, cmd.OutputTokens)
			require.Equal(t, 11, log.InputTokens)
			require.Equal(t, 7, log.CacheReadTokens)
			require.Equal(t, 5, log.OutputTokens)
			require.Greater(t, cmd.BalanceCost, float64(0), "partial usage follows normal billing")
		})
	}
}
