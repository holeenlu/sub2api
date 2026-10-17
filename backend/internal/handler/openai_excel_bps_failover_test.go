//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/alicebob/miniredis/v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The client history carries a completed tool round trip; a switched account
// must receive exactly the same converted history.
const excelBPSFailoverRequestBody = `{"model":"gpt-6-astra","stream":%t,"input":[` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"list the files"}]},` +
	`{"type":"function_call","call_id":"call_ls","name":"shell","arguments":"{\"cmd\":\"ls\"}"},` +
	`{"type":"function_call_output","call_id":"call_ls","output":"a.txt"}],` +
	`"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{"cmd":{"type":"string"}},"required":["cmd"]}}]}`

// excelBPSFailoverUpstream answers in call order and records the account of
// every BPS attempt.
type excelBPSFailoverUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
	bodies     [][]byte
	urls       []string
	answer     func(call int) *http.Response
	onDo       func(call int)
}

func (u *excelBPSFailoverUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	call := len(u.accountIDs)
	u.accountIDs = append(u.accountIDs, accountID)
	u.bodies = append(u.bodies, body)
	u.urls = append(u.urls, req.URL.String())
	u.mu.Unlock()
	if u.onDo != nil {
		u.onDo(call)
	}
	return u.answer(call), nil
}

func (u *excelBPSFailoverUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func excelBPS429(retryAfter string) *http.Response {
	header := http.Header{"Content-Type": {"application/json"}}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return &http.Response{StatusCode: http.StatusTooManyRequests, Header: header,
		Body: io.NopCloser(strings.NewReader(`{"error":{"code":"basispoints_rate_limited","message":"PRIVATE_UPSTREAM workspace detail"}}`))}
}

func excelBPSCompleted() *http.Response {
	wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_bps_failover\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"item_id\":\"msg_bps\",\"delta\":\"done\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_bps_failover\",\"status\":\"completed\",\"model\":\"gpt-6-astra\"," +
		"\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
}

// excelBPSFailoverAccountRepo also answers the no-account diagnosis query.
type excelBPSFailoverAccountRepo struct {
	openAIImagesFailoverAccountRepo
}

func (r excelBPSFailoverAccountRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	var out []service.Account
	for _, platform := range platforms {
		out = append(out, r.accountsForPlatform(platform)...)
	}
	return out, nil
}

// loadBatch selects the production default (load-aware) scheduling path;
// otherwise the simple priority path is used.
type excelBPSFailoverTestOptions struct {
	usage    service.UsageLogRepository
	cache    service.GatewayCache
	settings map[string]string
	accounts []service.Account
}

func newExcelBPSFailoverTestHandler(t *testing.T, upstream service.HTTPUpstream, loadBatch bool, options ...excelBPSFailoverTestOptions) *OpenAIGatewayHandler {
	t.Helper()
	var opts excelBPSFailoverTestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	settings := map[string]string{service.SettingKeyExcelBPSEnabled: "true"}
	for key, value := range opts.settings {
		settings[key] = value
	}
	var accounts []service.Account
	for _, id := range []int64{1, 2} {
		accounts = append(accounts, service.Account{
			ID: id, Name: fmt.Sprintf("bps-account-%d", id), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{3131},
			Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": fmt.Sprintf("token-%d", id), "chatgpt_account_id": fmt.Sprintf("chatgpt-%d", id)},
			Extra:       map[string]any{"openai_excel_bps": true},
		})
	}
	if opts.accounts != nil {
		accounts = opts.accounts
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	concurrencyService := service.NewConcurrencyService(nil)
	var schedulingConcurrency *service.ConcurrencyService
	if loadBatch {
		cfg.Gateway.Scheduling.LoadBatchEnabled = true
		schedulingConcurrency = concurrencyService
	}
	gatewayService := service.NewOpenAIGatewayService(
		excelBPSFailoverAccountRepo{openAIImagesFailoverAccountRepo{accounts: accounts}},
		opts.usage, nil, nil, nil, nil, opts.cache,
		cfg,
		nil, schedulingConcurrency, service.NewBillingService(cfg, nil), nil, nil,
		upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, service.NewSettingService(&contentModerationHandlerSettingRepo{values: settings}, cfg), nil,
	)
	t.Cleanup(func() { require.NoError(t, gatewayService.CloseExcelBPSImages()) })
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil,
		cfg,
	)
	handler.maxAccountSwitches = 10
	return handler
}

func TestExcelBPSCompactionUsageSurvivesAccountFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("DATA_DIR", t.TempDir())
	for _, protocol := range []string{"json", "sse", "messages", "messages-json", "chat", "chat-json", "ws"} {
		t.Run(protocol, func(t *testing.T) {
			stream := protocol != "json" && !strings.HasSuffix(protocol, "-json")
			anthropic := strings.HasPrefix(protocol, "messages")
			chat := strings.HasPrefix(protocol, "chat")
			redisServer := miniredis.RunT(t)
			rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
			t.Cleanup(func() { _ = rdb.Close() })
			usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 3)}
			upstream := &excelBPSFailoverUpstream{answer: func(call int) *http.Response {
				if call == 1 {
					return excelBPS429("30")
				}
				if call == 0 || call == 2 {
					wire := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_compact_%d\",\"status\":\"completed\",\"output\":[{\"type\":\"compaction\",\"encrypted_content\":\"opaque-state\"}],\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n", call)
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
				}
				return excelBPSCompleted()
			}}
			h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{
				usage: usage, cache: repository.NewGatewayCache(rdb), settings: map[string]string{
					service.SettingKeyExcelBPSImageRelayEnabled: "true", service.SettingKeyExcelBPSImageMode: "relay",
					service.SettingKeyExcelBPSImageBaseURL: "https://images.example", service.SettingKeyExcelBPSImageLimitPolicy: "auto_compact",
				},
			})
			var pixels bytes.Buffer
			require.NoError(t, png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 1, 1))))
			encoded := base64.StdEncoding.EncodeToString(pixels.Bytes())
			message := func(count int) map[string]any {
				content := make([]any, count)
				for i := range content {
					if anthropic {
						content[i] = map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": encoded}}
					} else if chat {
						content[i] = map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64," + encoded}}
					} else {
						content[i] = map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + encoded}
					}
				}
				return map[string]any{"role": "user", "content": content}
			}
			input := []any{message(19), map[string]any{"role": "assistant", "content": "seen"}, message(2)}
			body := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": input}
			path := "/v1/responses"
			if anthropic {
				delete(body, "input")
				body["messages"], body["max_tokens"], path = input, 100, "/v1/messages"
			}
			if chat {
				delete(body, "input")
				body["messages"], path = input, "/v1/chat/completions"
			}
			if protocol == "ws" {
				body["type"] = "response.create"
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			c, rec := newExcelBPSFailoverTestContext(t.Context(), stream)
			c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
			c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.ClientRequestID, "bps-compaction-failover-client"))
			c.MustGet(string(middleware2.ContextKeyAPIKey)).(*service.APIKey).Group.AllowMessagesDispatch = true
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
			c.Request.Header.Set("session-id", "compaction-failover")
			if protocol == "ws" {
				router := gin.New()
				router.GET(path, func(wsCtx *gin.Context) {
					wsCtx.Request = wsCtx.Request.WithContext(context.WithValue(wsCtx.Request.Context(), ctxkey.ClientRequestID, "bps-compaction-failover-client"))
					wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
					wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
					h.ResponsesWebSocket(wsCtx)
				})
				server := httptest.NewServer(router)
				defer server.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, &coderws.DialOptions{HTTPHeader: c.Request.Header})
				require.NoError(t, err)
				defer client.CloseNow()
				require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
				for {
					_, event, err := client.Read(ctx)
					require.NoError(t, err)
					require.NotEqual(t, "error", gjson.GetBytes(event, "type").String(), string(event))
					if gjson.GetBytes(event, "type").String() == "response.completed" {
						break
					}
				}
				require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			} else {
				if anthropic {
					h.Messages(c)
				} else if chat {
					h.ChatCompletions(c)
				} else {
					h.Responses(c)
				}
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "done")
			}
			calls := upstream.calls()
			for _, endpoint := range upstream.urls {
				require.Equal(t, basispoints.ResponsesURL, endpoint)
			}
			require.Len(t, calls, 4)
			require.Equal(t, calls[0], calls[1])
			require.Equal(t, calls[2], calls[3])
			require.NotEqual(t, calls[0], calls[2])
			logs := map[int64]*service.UsageLog{}
			for range 2 {
				select {
				case log := <-usage.created:
					logs[log.AccountID] = log
				case <-time.After(3 * time.Second):
					t.Fatal("both metered account attempts must be recorded")
				}
			}
			require.Equal(t, 7, logs[calls[0]].InputTokens)
			require.Equal(t, 3, logs[calls[0]].OutputTokens)
			require.Equal(t, 10, logs[calls[2]].InputTokens)
			require.Equal(t, 4, logs[calls[2]].OutputTokens)
			require.NotEqual(t, logs[calls[0]].RequestID, logs[calls[2]].RequestID)
			require.Empty(t, usage.created, "failover must not bill one attempt twice")
		})
	}
}

func newExcelBPSFailoverTestContext(ctx context.Context, stream bool) (*gin.Context, *httptest.ResponseRecorder) {
	groupID := int64(3131)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(fmt.Sprintf(excelBPSFailoverRequestBody, stream))))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
		User:    &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
	return c, rec
}

// Issue #146: A -> B within the first request, then B directly while A cools.
func TestOpenAIResponsesExcelBPS429SwitchesAccountAndCoolsIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		loadBatch, stream bool
	}{{false, false}, {false, true}, {true, false}, {true, true}} {
		stream := tc.stream
		t.Run(fmt.Sprintf("load_batch=%t/stream=%t", tc.loadBatch, stream), func(t *testing.T) {
			upstream := &excelBPSFailoverUpstream{answer: func(call int) *http.Response {
				if call == 0 {
					return excelBPS429("30")
				}
				return excelBPSCompleted()
			}}
			handler := newExcelBPSFailoverTestHandler(t, upstream, tc.loadBatch)

			c, rec := newExcelBPSFailoverTestContext(context.Background(), stream)
			handler.Responses(c)

			calls := upstream.calls()
			require.Len(t, calls, 2)
			throttled, healthy := calls[0], calls[1]
			require.NotEqual(t, throttled, healthy, "the throttled account must not be retried in this request")
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), "done")
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
			require.NotContains(t, rec.Body.String(), "basispoints_rate_limited")
			for _, url := range upstream.urls {
				require.Equal(t, basispoints.ResponsesURL, url)
			}
			require.NotEmpty(t, gjson.GetBytes(upstream.bodies[0], "input").Raw)
			require.Equal(t, gjson.GetBytes(upstream.bodies[0], "input").Raw, gjson.GetBytes(upstream.bodies[1], "input").Raw,
				"messages, tool calls and tool outputs must survive the account switch unchanged")
			rawEvents, ok := c.Get(service.OpsUpstreamErrorsKey)
			require.True(t, ok)
			events, ok := rawEvents.([]*service.OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.Len(t, events, 1)
			require.Equal(t, "failover", events[0].Kind)
			require.Equal(t, throttled, events[0].AccountID)
			require.Equal(t, http.StatusTooManyRequests, events[0].UpstreamStatusCode)

			c, rec = newExcelBPSFailoverTestContext(context.Background(), stream)
			handler.Responses(c)

			require.Equal(t, []int64{throttled, healthy, healthy}, upstream.calls(), "the cooling account is skipped by the next request")
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestOpenAIResponsesExcelBPS429OnEveryAccountReturnsRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, retryAfter, wantRetryAfter string
		stream, loadBatch                bool
	}{
		{name: "json", retryAfter: "30", wantRetryAfter: "30"},
		{name: "stream request", retryAfter: "30", wantRetryAfter: "30", stream: true},
		{name: "load-aware scheduling", retryAfter: "30", wantRetryAfter: "30", loadBatch: true},
		{name: "invalid retry after", retryAfter: "soon"},
		{name: "retry after beyond a week", retryAfter: "999999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &excelBPSFailoverUpstream{answer: func(int) *http.Response { return excelBPS429(tc.retryAfter) }}
			handler := newExcelBPSFailoverTestHandler(t, upstream, tc.loadBatch)

			c, rec := newExcelBPSFailoverTestContext(context.Background(), tc.stream)
			handler.Responses(c)

			calls := upstream.calls()
			require.Len(t, calls, 2)
			require.NotEqual(t, calls[0], calls[1])
			require.Equal(t, http.StatusTooManyRequests, rec.Code)
			require.Equal(t, "rate_limit_error", gjson.Get(rec.Body.String(), "error.type").String())
			require.Equal(t, "basispoints_rate_limited", gjson.Get(rec.Body.String(), "error.code").String())
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
			require.NotContains(t, rec.Body.String(), "workspace")
			require.Equal(t, tc.wantRetryAfter, rec.Header().Get("Retry-After"))

			// Both accounts cool down: the next request is answered without an upstream call.
			c, rec = newExcelBPSFailoverTestContext(context.Background(), tc.stream)
			handler.Responses(c)

			require.Len(t, upstream.calls(), 2)
			require.Equal(t, http.StatusTooManyRequests, rec.Code)
			require.Equal(t, "rate_limit_error", gjson.Get(rec.Body.String(), "error.type").String())
			require.Contains(t, rec.Body.String(), "rate-limited")
		})
	}
}

func TestOpenAIResponsesExcelBPS429StopsWhenClientCancels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &excelBPSFailoverUpstream{
		answer: func(int) *http.Response { return excelBPS429("30") },
		onDo:   func(int) { cancel() },
	}
	handler := newExcelBPSFailoverTestHandler(t, upstream, false)
	c, rec := newExcelBPSFailoverTestContext(ctx, false)

	handler.Responses(c)

	require.Len(t, upstream.calls(), 1, "a gone client must not be replayed on another account")
	require.Equal(t, statusClientClosedRequest, c.Writer.Status())
	require.Zero(t, rec.Body.Len())
}

func TestExcelBPSPermissionAnd500FailoverAcrossClientProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, failure := range []struct {
		name, code, event, errorType string
		status                       int
		exhausted, implicitStatus    bool
	}{
		{name: "model permission HTTP", code: "basispoints_model_access_changed", status: 403},
		{name: "model permission SSE", code: "model_not_allowed", event: "error", status: 403},
		{name: "server error HTTP", code: "server_error", status: 500},
		{name: "server error SSE", code: "server_error", event: "error", status: 500},
		{name: "server failure SSE", code: "internal_server_error", event: "response.failed", status: 500},
		{name: "model permission pool exhausted", code: "basispoints_model_access_changed", status: 403, exhausted: true},
		{name: "server error pool exhausted", code: "server_error", event: "error", status: 500, exhausted: true},
		{name: "BPS overloaded SSE", code: "server_is_overloaded", errorType: "service_unavailable_error", event: "error", status: 503, implicitStatus: true},
		{name: "BPS overloaded failed response", code: "server_is_overloaded", errorType: "service_unavailable_error", event: "response.failed", status: 503, implicitStatus: true},
		{name: "BPS unavailable type only", errorType: "service_unavailable_error", event: "error", status: 503, implicitStatus: true},
		{name: "BPS overloaded pool exhausted", code: "server_is_overloaded", errorType: "service_unavailable_error", event: "error", status: 503, implicitStatus: true, exhausted: true},
	} {
		for _, protocol := range []string{"json", "sse", "messages-json", "messages", "chat-json", "chat", "ws"} {
			t.Run(failure.name+"/"+protocol, func(t *testing.T) {
				upstream := &excelBPSFailoverUpstream{answer: func(call int) *http.Response {
					if call != 0 && !failure.exhausted {
						return excelBPSCompleted()
					}
					errorType := failure.errorType
					if errorType == "" {
						errorType = "server_error"
					}
					detail := fmt.Sprintf(`{"code":%q,"type":%q,"message":"PRIVATE_UPSTREAM cause"}`, failure.code, errorType)
					if failure.event == "" {
						return &http.Response{StatusCode: failure.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":` + detail + `}`))}
					}
					responseID := fmt.Sprintf("resp_rejected_attempt_%d", call)
					wire := "data: " + fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"usage":{"input_tokens":7,"output_tokens":2}}}`, responseID) + "\n\n"
					var terminal string
					if failure.event == "error" {
						terminal = fmt.Sprintf(`{"type":"error","status":%d,"error":%s}`, failure.status, detail)
						if failure.implicitStatus {
							terminal = fmt.Sprintf(`{"type":"error","error":%s}`, detail)
						}
					} else {
						terminal = fmt.Sprintf(`{"type":"response.failed","response":{"id":%q,"status":"failed","error":%s}}`, responseID, detail)
					}
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire + "data: " + terminal + "\n\n"))}
				}}
				redisServer := miniredis.RunT(t)
				rdb := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
				t.Cleanup(func() { _ = rdb.Close() })
				usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 3)}
				h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{usage: usage, cache: repository.NewGatewayCache(rdb)})
				stream := protocol != "json" && !strings.HasSuffix(protocol, "-json")
				c, rec := newExcelBPSFailoverTestContext(t.Context(), stream)
				model := "gpt-6-astra"
				if failure.status == 403 {
					model = "gpt-6.1-sol"
				}
				body := map[string]any{"model": model, "stream": stream, "input": "original history"}
				path := "/v1/responses"
				if strings.HasPrefix(protocol, "messages") || strings.HasPrefix(protocol, "chat") {
					delete(body, "input")
					body["messages"] = []any{map[string]any{"role": "user", "content": "original history"}}
					if strings.HasPrefix(protocol, "messages") {
						body["max_tokens"], path = 100, "/v1/messages"
					} else {
						path = "/v1/chat/completions"
					}
				}
				if protocol == "ws" {
					body["type"] = "response.create"
				}
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
				c.MustGet(string(middleware2.ContextKeyAPIKey)).(*service.APIKey).Group.AllowMessagesDispatch = true
				if protocol == "ws" {
					wsFailure := make(chan service.OpsStreamError, 1)
					router := gin.New()
					router.GET(path, func(wsCtx *gin.Context) {
						wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
						wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
						h.ResponsesWebSocket(wsCtx)
						if streamErr, ok := service.GetOpsStreamError(wsCtx); ok {
							wsFailure <- streamErr
						}
					})
					server := httptest.NewServer(router)
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, nil)
					require.NoError(t, err)
					defer client.CloseNow()
					require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
					for {
						_, event, err := client.Read(ctx)
						if failure.exhausted {
							require.Equal(t, coderws.StatusTryAgainLater, coderws.CloseStatus(err), "pool exhaustion must not be a client authentication rejection")
							require.NotContains(t, err.Error(), "PRIVATE_UPSTREAM")
							require.NotContains(t, err.Error(), "authentication")
							select {
							case streamErr := <-wsFailure:
								require.Equal(t, failure.status, streamErr.IntendedStatus)
								wantCode := service.ExcelBPSUpstreamFailureReason
								if failure.status == 403 {
									wantCode = service.ExcelBPSModelPermissionReason
								}
								require.Equal(t, string(wantCode), streamErr.Code)
								if failure.status == 403 {
									require.Equal(t, "invalid_request_error", streamErr.ErrType)
									require.Contains(t, err.Error(), "This model is not available")
								} else {
									require.Equal(t, "server_error", streamErr.ErrType)
								}
							case <-ctx.Done():
								t.Fatal("missing exhaustion diagnostic")
							}
							break
						}
						require.NoError(t, err)
						require.NotContains(t, string(event), "resp_rejected_attempt")
						require.NotContains(t, string(event), "PRIVATE_UPSTREAM")
						require.NotEqual(t, "error", gjson.GetBytes(event, "type").String(), string(event))
						if gjson.GetBytes(event, "type").String() == "response.completed" {
							break
						}
					}
					if !failure.exhausted {
						require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
					}
				} else {
					switch {
					case strings.HasPrefix(protocol, "messages"):
						h.Messages(c)
					case strings.HasPrefix(protocol, "chat"):
						h.ChatCompletions(c)
					default:
						h.Responses(c)
					}
					if failure.exhausted {
						require.Equal(t, failure.status, rec.Code, rec.Body.String())
						if failure.status == 403 {
							wantType := "invalid_request_error"
							if strings.HasPrefix(protocol, "messages") {
								wantType = "permission_error"
							}
							require.Equal(t, wantType, gjson.Get(rec.Body.String(), "error.type").String())
							require.Contains(t, rec.Body.String(), "This model is not available")
						} else {
							require.Contains(t, rec.Body.String(), "Excel BPS service temporarily unavailable")
						}
					} else {
						require.Equal(t, 200, rec.Code, rec.Body.String())
						require.Contains(t, rec.Body.String(), "done")
					}
					require.NotContains(t, rec.Body.String(), "resp_rejected_attempt")
					require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
				}
				calls := upstream.calls()
				require.Len(t, calls, 2)
				require.NotEqual(t, calls[0], calls[1])
				require.Equal(t, gjson.GetBytes(upstream.bodies[0], "input").Raw, gjson.GetBytes(upstream.bodies[1], "input").Raw)
				for _, url := range upstream.urls {
					require.Equal(t, basispoints.ResponsesURL, url)
				}
				wantLogs := 1
				if failure.exhausted {
					wantLogs = 0
				}
				if failure.event != "" {
					wantLogs = 2
				}
				logs := map[int64]*service.UsageLog{}
				for range wantLogs {
					select {
					case log := <-usage.created:
						logs[log.AccountID] = log
					case <-time.After(3 * time.Second):
						t.Fatal("each metered attempt must be recorded before failover completes")
					}
				}
				require.Len(t, logs, wantLogs)
				if !failure.exhausted {
					require.Equal(t, 3, logs[calls[1]].InputTokens)
					require.Equal(t, 1, logs[calls[1]].OutputTokens)
				}
				if failure.event != "" {
					require.Equal(t, 7, logs[calls[0]].InputTokens)
					require.Equal(t, 2, logs[calls[0]].OutputTokens)
					require.NotEqual(t, logs[calls[0]].RequestID, logs[calls[1]].RequestID)
					if failure.exhausted {
						require.Equal(t, 7, logs[calls[1]].InputTokens)
						require.Equal(t, 2, logs[calls[1]].OutputTokens)
					}
				}
				require.Empty(t, usage.created, "do not bill the failed attempt twice")
			})
		}
	}
}

func TestExcelBPSAttachment429NeverSwitchesFileOwner(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		t.Run(protocol, func(t *testing.T) {
			var img bytes.Buffer
			require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 3))))
			payload := map[string]any{"model": "gpt-6-astra", "stream": protocol != "json", "input": []any{
				map[string]any{"type": "compaction", "id": "cmp_owner", "encrypted_content": "PRIVATE_BOUND_CONTEXT"},
				map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())}, map[string]any{"type": "input_image", "file_id": "file-bound-owner"}}},
			}}
			if protocol == "ws" {
				payload["type"] = "response.create"
			}
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			upstream := &excelBPSFailoverUpstream{answer: func(int) *http.Response { return excelBPS429("0") }}
			h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{settings: map[string]string{
				service.SettingKeyExcelBPSImageRelayEnabled: "true", service.SettingKeyExcelBPSImageMode: "native",
			}})
			h.cfg.Gateway.OpenAITurnMaxAttempts = 3
			c, rec := newExcelBPSFailoverTestContext(t.Context(), protocol != "json")
			if protocol == "ws" {
				done := make(chan *gin.Context, 1)
				router := gin.New()
				router.GET("/v1/responses", func(wsCtx *gin.Context) {
					wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
					wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
					h.ResponsesWebSocket(wsCtx)
					done <- wsCtx
				})
				server := httptest.NewServer(router)
				defer server.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
				require.NoError(t, err)
				defer client.CloseNow()
				require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
				_, _, err = client.Read(ctx)
				require.Equal(t, coderws.StatusTryAgainLater, coderws.CloseStatus(err))
				select {
				case c = <-done:
				case <-ctx.Done():
					t.Fatal("WS owner-bound rejection did not finish")
				}
			} else {
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
				h.Responses(c)
				require.Equal(t, 429, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), "basispoints_rate_limited")
			}
			calls := upstream.calls()
			require.Len(t, calls, 2, "only one same-owner upload retry, never switch to the second available account")
			require.Equal(t, calls[0], calls[1])
			require.Equal(t, []string{basispoints.AttachmentsURL, basispoints.AttachmentsURL}, upstream.urls)
			events, ok := c.Get(service.OpsUpstreamErrorsKey)
			require.True(t, ok)
			attempts := events.([]*service.OpsUpstreamErrorEvent)
			require.Len(t, attempts, 3)
			require.Equal(t, "retry", attempts[0].Kind)
			require.Equal(t, "failover", attempts[1].Kind)
			require.Equal(t, string(service.OpenAIRetryStopContextNotPortable), attempts[2].Reason)
			var diagnostic map[string]any
			require.NoError(t, json.Unmarshal([]byte(attempts[2].Detail), &diagnostic))
			require.Equal(t, "encrypted_content", diagnostic["context_blocker"])
			require.NoError(t, json.Unmarshal([]byte(attempts[1].Detail), &diagnostic))
			require.Equal(t, "retry_exhausted", diagnostic["attachment_retry_reason"])
			require.Equal(t, float64(1), diagnostic["attachment_retry_attempts"])
			require.NotContains(t, fmt.Sprint(events), "PRIVATE_")
			if protocol == "ws" {
				setupOpsErrorLogTestQueue(t, 1)
				ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
				logOpsStreamError(c, ops, http.StatusSwitchingProtocols)
				require.Equal(t, int64(1), OpsErrorLogQueueLength())
				entry := (<-opsErrorLogQueue).entry
				require.Equal(t, http.StatusTooManyRequests, entry.StatusCode)
				require.NotNil(t, entry.UpstreamStatusCode)
				require.Equal(t, http.StatusTooManyRequests, *entry.UpstreamStatusCode)
				require.NotNil(t, entry.UpstreamErrorMessage)
				require.Equal(t, attempts[1].Message, *entry.UpstreamErrorMessage)
				require.NotNil(t, entry.UpstreamErrorDetail)
				require.JSONEq(t, attempts[1].Detail, *entry.UpstreamErrorDetail)
			}
		})
	}
}

func TestExcelBPS429PoolPrefersBPSBeforeNative(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws", "chat", "messages"} {
		for _, outcome := range []string{"second_bps_succeeds", "all_bps_limited", "native_rejected", "no_native_grant"} {
			t.Run(protocol+"/"+outcome, func(t *testing.T) {
				accounts := []service.Account{}
				for _, id := range []int64{10, 20, 30} {
					if id == 20 && outcome == "no_native_grant" {
						continue
					}
					clientID := openai.ExcelClientID
					if id == 20 {
						clientID = openai.ClientID
					}
					accounts = append(accounts, service.Account{
						ID: id, Name: fmt.Sprintf("account-%d", id), Priority: int(id),
						Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{3131},
						Credentials: map[string]any{"client_id": clientID, "access_token": fmt.Sprintf("token-%d", id), "chatgpt_account_id": fmt.Sprintf("workspace-%d", id)},
						Extra:       map[string]any{"openai_excel_bps": id != 20, "openai_oauth_responses_websockets_v2_mode": "http_bridge"},
					})
				}
				var img bytes.Buffer
				require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 3))))
				payload := map[string]any{"model": "gpt-6-astra", "stream": protocol != "json", "input": []any{
					map[string]any{"type": "compaction", "encrypted_content": "PRIVATE_REPLAYABLE_HISTORY"},
					map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())}}},
				}}
				if protocol == "ws" {
					payload["type"] = "response.create"
				}
				if protocol == "chat" || protocol == "messages" {
					payload = map[string]any{"model": "gpt-6-astra", "stream": true, "max_tokens": 100, "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
				}
				raw, err := json.Marshal(payload)
				require.NoError(t, err)
				upstream := &excelBPSFailoverUpstream{}
				upstream.answer = func(call int) *http.Response {
					id := upstream.calls()[call]
					if id == 20 {
						if outcome == "native_rejected" {
							return &http.Response{StatusCode: 400, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"error\":{\"type\":\"invalid_request_error\",\"message\":\"native rejected\"}}"))}
						}
						return excelBPSCompleted()
					}
					if id == 10 || outcome != "second_bps_succeeds" {
						return excelBPS429("30")
					}
					if upstream.urls[call] == basispoints.AttachmentsURL {
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"openai_file_id\":\"file-second-account\"}"))}
					}
					return excelBPSCompleted()
				}
				h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{accounts: accounts, settings: map[string]string{
					service.SettingKeyExcelBPSImageRelayEnabled: "true", service.SettingKeyExcelBPSImageMode: "native",
				}})
				c, rec := newExcelBPSFailoverTestContext(t.Context(), protocol != "json")
				c.MustGet(string(middleware2.ContextKeyAPIKey)).(*service.APIKey).Group.AllowMessagesDispatch = true
				if protocol == "ws" {
					router := gin.New()
					done := make(chan struct{})
					router.GET("/v1/responses", func(wsCtx *gin.Context) {
						wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
						wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
						h.ResponsesWebSocket(wsCtx)
						close(done)
					})
					server := httptest.NewServer(router)
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					client, _, dialErr := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
					require.NoError(t, dialErr)
					defer client.CloseNow()
					require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
					for {
						_, event, readErr := client.Read(ctx)
						if (outcome == "native_rejected" || outcome == "no_native_grant") && readErr != nil {
							require.NotEqual(t, coderws.StatusNormalClosure, coderws.CloseStatus(readErr))
							break
						}
						require.NoError(t, readErr)
						if gjson.GetBytes(event, "type").String() == "response.completed" || (outcome == "native_rejected" && (gjson.GetBytes(event, "type").String() == "error" || gjson.GetBytes(event, "type").String() == "response.failed")) {
							client.CloseNow()
							break
						}
					}
					select {
					case <-done:
					case <-ctx.Done():
						t.Fatal("WS recovery did not finish")
					}
				} else {
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
					switch protocol {
					case "chat":
						h.ChatCompletions(c)
					case "messages":
						h.Messages(c)
					default:
						h.Responses(c)
					}
					if outcome == "no_native_grant" {
						require.Equal(t, 429, rec.Code)
						if protocol == "messages" {
							require.Contains(t, rec.Body.String(), "rate_limit_error")
						} else {
							require.Contains(t, rec.Body.String(), "basispoints_rate_limited")
						}
					} else if outcome == "native_rejected" {
						require.Equal(t, 400, rec.Code, rec.Body.String())
					} else {
						require.Equal(t, 200, rec.Code, rec.Body.String())
						require.Contains(t, rec.Body.String(), "done")
					}
				}
				calls := upstream.calls()
				if outcome == "no_native_grant" {
					require.Equal(t, []int64{10, 30}, calls)
					return
				}
				if outcome == "second_bps_succeeds" {
					require.NotContains(t, calls, int64(20), "native route must wait until the BPS pool is exhausted")
					require.Equal(t, int64(10), calls[0])
					for _, id := range calls[1:] {
						require.Equal(t, int64(30), id)
					}
				} else {
					require.Equal(t, []int64{10, 30, 20}, calls, "do not loop back to BPS after entering native recovery")
					require.Contains(t, upstream.urls[2], "/backend-api/codex/responses")
				}
				if protocol == "json" || protocol == "sse" || protocol == "ws" {
					if outcome == "second_bps_succeeds" {
						require.Contains(t, string(upstream.bodies[len(calls)-1]), "PRIVATE_REPLAYABLE_HISTORY")
						require.Contains(t, string(upstream.bodies[len(calls)-1]), "file-second-account")
					} else {
						require.JSONEq(t, gjson.GetBytes(raw, "input").Raw, gjson.GetBytes(upstream.bodies[2], "input").Raw)
					}
				}
			})
		}
	}
}
