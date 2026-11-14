//go:build unit

package handler

import (
	"bufio"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/protocol/sse"
	"github.com/Wei-Shaw/sub2api/internal/host"
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
	headers    []http.Header
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
	u.headers = append(u.headers, req.Header.Clone())
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
	journal  *bpsModeHandlerJournal
	cache    service.GatewayCache
	settings map[string]string
	accounts []service.Account
	apiKeys  []*service.APIKey
	// Failure injection only: the normal fixture must bind real request pricing.
	omitIntentRepository bool
	configureAuthority   func(excelBPSHandlerAuthority)
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
			Extra:       map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra", "gpt-6.1-sol"}},
		})
	}
	if opts.accounts != nil {
		accounts = opts.accounts
	}
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 32 << 20}}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	concurrencyService := service.NewConcurrencyService(nil)
	// Both strategies need an actual root-owned account receipt, including
	// explicit unlimited capacity. A nil scheduler service returns only a no-op.
	schedulingConcurrency := concurrencyService
	if loadBatch {
		cfg.Gateway.Scheduling.LoadBatchEnabled = true
	}
	authority := newExcelBPSHandlerAuthority(t, cfg, accounts, nativeHandlerFixtureGroupID, opts.apiKeys...)
	if opts.configureAuthority != nil {
		opts.configureAuthority(authority)
	}
	// The shared operation binds pricing before selecting BPS. Supply the same
	// intent repository as the Native fixtures instead of bypassing that guard;
	// BPS must still use its own usage path and never persist a Native attempt.
	journal := opts.journal
	if journal == nil {
		journal = &bpsModeHandlerJournal{wsNativeFixtureJournal: &wsNativeFixtureJournal{logSink: opts.usage}}
	}
	var intentRepository service.UsageBillingRepository = journal
	if opts.omitIntentRepository {
		intentRepository = nil
	}
	t.Cleanup(func() {
		journal.assertNoLegacyApply(t)
		journal.mu.Lock()
		defer journal.mu.Unlock()
		require.Empty(t, journal.intents, "BPS must not preserve a Native generation intent")
		require.Empty(t, journal.finalizations, "BPS must not finalize Native usage")
		require.Empty(t, journal.records, "BPS must not create Native billing records")
		require.Empty(t, journal.closures, "BPS must not manufacture a Native not-sent closure")
	})
	gatewayService := service.NewOpenAIGatewayService(
		authority.accounts,
		opts.usage, intentRepository, authority.users, nil, nil, opts.cache,
		cfg,
		nil, schedulingConcurrency, service.NewBillingService(cfg, nil), nil, nil,
		&excelBPSHandlerUpstream{HTTPUpstream: upstream, t: t},
		&service.DeferredService{}, nil, nil, nil, nil, nil, service.NewSettingService(&contentModerationHandlerSettingRepo{values: settings}, cfg), nil,
	)
	t.Cleanup(gatewayService.CloseOpenAIWSPool)
	t.Cleanup(func() { require.NoError(t, gatewayService.CloseExcelBPSImages()) })
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	cleanupHandlerBillingCache(t, billingService)
	require.NoError(t, billingService.Start(t.Context()))
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingService,
		authority.keys,
		nil, nil, nil, nil,
		cfg,
	)
	handler.maxAccountSwitches = 10
	return handler
}

// These older routes supply identity directly, but still need the same host
// turn admission as the public WS server. Verify every admitted turn releases.
func excelBPSHistoricalWSAdmission(t *testing.T) gin.HandlerFunc {
	t.Helper()
	admission := &wsNativeFixtureAdmission{}
	joined := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-joined:
			admission.assertClosed(t, true)
		case <-time.After(3 * time.Second):
			t.Error("historical BPS WS fixture did not join")
		}
	})
	return func(c *gin.Context) {
		defer close(joined)
		c.Request = c.Request.WithContext(host.WithTurnAdmission(c.Request.Context(), admission))
		c.Next()
	}
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
				router.Use(excelBPSHistoricalWSAdmission(t))
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
				defer func() { _ = client.CloseNow() }()
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
					require.Equal(t, service.UsageAuthorizationModeBPS, log.AuthorizationMode)
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
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader([]byte(fmt.Sprintf(excelBPSFailoverRequestBody, stream))))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID))
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})
	return c, rec
}

func excelBPSHandlerProtocols() []string {
	return []string{"json", "sse", "ws", "chat", "chat-json", "messages", "messages-json"}
}

func excelBPSHandlerStream(protocol string) bool {
	return protocol != "json" && !strings.HasSuffix(protocol, "-json")
}

type excelBPSHandlerResponse struct {
	status  int
	header  http.Header
	body    []byte
	events  [][]byte
	readErr error
	context *gin.Context
}

// Exercise authentication, pricing and the actual handler on its public path.
// Only persistence and the upstream HTTP transport are local substitutes; a WS
// client still performs the real upgrade and reads the handler's wire frames.
func runExcelBPSHandlerRequest(t *testing.T, h *OpenAIGatewayHandler, protocol string, body []byte) (result excelBPSHandlerResponse) {
	t.Helper()
	path, handler := "/v1/responses", h.Responses
	switch {
	case strings.HasPrefix(protocol, "chat"):
		path, handler = "/v1/chat/completions", h.ChatCompletions
	case strings.HasPrefix(protocol, "messages"):
		path, handler = "/v1/messages", h.Messages
	case protocol == "ws":
		handler = h.ResponsesWebSocket
	}
	finished := make(chan *gin.Context, 1)
	router := gin.New()
	admission := &wsNativeFixtureAdmission{}
	router.Use(func(c *gin.Context) {
		if protocol == "ws" {
			c.Request = c.Request.WithContext(host.WithTurnAdmission(c.Request.Context(), admission))
		}
		defer func() { finished <- c }()
		c.Next()
	}, gin.HandlerFunc(middleware2.NewAPIKeyAuthMiddleware(h.apiKeyService, nil, h.cfg)))
	credential := newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key
	if protocol != "ws" {
		router.POST(path, handler)
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(t.Context())
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, request)
		return excelBPSHandlerResponse{status: rec.Code, header: rec.Header().Clone(), body: bytes.Clone(rec.Body.Bytes()), context: <-finished}
	}
	router.GET(path, handler)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, response, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, &coderws.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + credential}},
	})
	require.NoError(t, err, "fixture authentication and WS upgrade must succeed")
	result.status, result.header = response.StatusCode, response.Header.Clone()
	defer func() {
		_ = client.CloseNow()
		select {
		case result.context = <-finished:
			admission.assertClosed(t, true)
		case <-ctx.Done():
			t.Error("BPS handler did not release its WS request before the deadline")
		}
	}()
	require.NoError(t, client.Write(ctx, coderws.MessageText, body))
	for range 64 {
		_, event, err := client.Read(ctx)
		if err != nil {
			require.NoError(t, ctx.Err(), "a test timeout is not a BPS terminal result")
			result.readErr = err
			return result
		}
		require.True(t, json.Valid(event), "invalid WS event: %s", event)
		result.events = append(result.events, bytes.Clone(event))
		result.body = append(result.body, event...)
		result.body = append(result.body, '\n')
		switch gjson.GetBytes(event, "type").String() {
		case "response.completed", "response.failed", "error":
			return result
		}
	}
	t.Fatal("BPS handler exceeded the fixture's bounded event count without a terminal")
	return result
}

func assertExcelBPSHandlerSuccess(t *testing.T, protocol string, result excelBPSHandlerResponse) {
	t.Helper()
	require.NoError(t, result.readErr, "WS close is not a successful response: %s", result.body)
	require.NotContains(t, string(result.body), "PRIVATE_UPSTREAM")
	if protocol != "ws" {
		require.Equal(t, http.StatusOK, result.status, "%s", result.body)
	}
	if !excelBPSHandlerStream(protocol) {
		require.Contains(t, result.header.Get("Content-Type"), "application/json")
		require.True(t, json.Valid(result.body), "%s", result.body)
		require.False(t, gjson.GetBytes(result.body, "error").Exists(), "%s", result.body)
		switch protocol {
		case "chat-json":
			require.Equal(t, "chat.completion", gjson.GetBytes(result.body, "object").String())
			require.Equal(t, "done", gjson.GetBytes(result.body, "choices.0.message.content").String())
			require.Equal(t, "stop", gjson.GetBytes(result.body, "choices.0.finish_reason").String())
		case "messages-json":
			require.Equal(t, "message", gjson.GetBytes(result.body, "type").String())
			require.Equal(t, "done", gjson.GetBytes(result.body, "content.0.text").String())
			require.Equal(t, "end_turn", gjson.GetBytes(result.body, "stop_reason").String())
		default:
			require.Equal(t, "completed", gjson.GetBytes(result.body, "status").String())
			require.Equal(t, "done", gjson.GetBytes(result.body, "output.0.content.0.text").String())
		}
		return
	}
	events, doneMarkers := result.events, 0
	if protocol == "ws" {
		require.Equal(t, http.StatusSwitchingProtocols, result.status)
	} else {
		require.Contains(t, result.header.Get("Content-Type"), "text/event-stream")
		scanner := bufio.NewScanner(bytes.NewReader(result.body))
		scanner.Split(sse.ScanLines())
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				doneMarkers++
				continue
			}
			require.True(t, json.Valid([]byte(data)), "invalid SSE data: %s", line)
			events = append(events, []byte(data))
		}
		require.NoError(t, scanner.Err())
	}
	require.NotEmpty(t, events, "a heartbeat/empty stream is not completion")
	terminals := 0
	for _, event := range events {
		require.False(t, gjson.GetBytes(event, "error").Exists(), "%s", event)
		kind := gjson.GetBytes(event, "type").String()
		require.NotEqual(t, "error", kind, "%s", event)
		require.NotEqual(t, "response.failed", kind, "%s", event)
		switch protocol {
		case "chat":
			if gjson.GetBytes(event, "choices.0.finish_reason").String() == "stop" {
				terminals++
			}
		case "messages":
			if kind == "message_stop" {
				terminals++
			}
		default:
			if kind == "response.completed" {
				require.Equal(t, "completed", gjson.GetBytes(event, "response.status").String())
				terminals++
			}
		}
	}
	require.Equal(t, 1, terminals, "exactly one protocol completion is required: %s", result.body)
	if protocol == "chat" {
		require.Equal(t, 1, doneMarkers)
	}
	require.Contains(t, string(result.body), "done")
}

// Removing the journal must still fail at the production pricing boundary.
// A fixture that skips binding the operation could otherwise make BPS tests
// green without exercising the guard that caused handler-smoke-v4 to fail.
func TestExcelBPSHandlerRequiresPricingIntentRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range excelBPSHandlerProtocols() {
		t.Run(protocol, func(t *testing.T) {
			upstream := &excelBPSFailoverUpstream{answer: func(int) *http.Response {
				t.Error("missing pricing intent repository must prevent every upstream send")
				return excelBPSCompleted()
			}}
			usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 1)}
			h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{
				usage: usage, omitIntentRepository: true,
			})
			payload := map[string]any{"model": "gpt-6-astra", "stream": excelBPSHandlerStream(protocol), "input": "hello"}
			if strings.HasPrefix(protocol, "chat") || strings.HasPrefix(protocol, "messages") {
				delete(payload, "input")
				payload["messages"] = []any{map[string]any{"role": "user", "content": "hello"}}
				if strings.HasPrefix(protocol, "messages") {
					payload["max_tokens"] = 100
				}
			}
			if protocol == "ws" {
				payload["type"] = "response.create"
			}
			raw, err := json.Marshal(payload)
			require.NoError(t, err)
			result := runExcelBPSHandlerRequest(t, h, protocol, raw)
			require.Empty(t, upstream.calls(), "pricing dependency failure is not an upstream rejection")
			require.Empty(t, usage.created, "no generation occurred")
			if protocol == "ws" {
				require.Equal(t, http.StatusSwitchingProtocols, result.status)
				var closeErr coderws.CloseError
				require.ErrorAs(t, result.readErr, &closeErr, "EOF or timeout is not a pricing rejection")
				require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
				require.Equal(t, "turn pricing could not be established", closeErr.Reason)
				require.Empty(t, result.events, "no turn can start without its pricing dependency")
				return
			}
			require.Equal(t, http.StatusServiceUnavailable, result.status, "%s", result.body)
			require.Contains(t, result.header.Get("Content-Type"), "application/json")
			require.True(t, json.Valid(result.body), "%s", result.body)
			require.Equal(t, "api_error", gjson.GetBytes(result.body, "error.type").String())
			require.Equal(t, "Native pricing intent could not be established", gjson.GetBytes(result.body, "error.message").String())
		})
	}
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
				response := excelBPSCompleted()
				wire, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				response.Body = io.NopCloser(strings.NewReader(strings.ReplaceAll(string(wire), "resp_bps_failover", fmt.Sprintf("resp_bps_failover_%d", call))))
				return response
			}}
			usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
			handler := newExcelBPSFailoverTestHandler(t, upstream, tc.loadBatch, excelBPSFailoverTestOptions{usage: usage})

			protocol := "json"
			if stream {
				protocol = "sse"
			}
			body := []byte(fmt.Sprintf(excelBPSFailoverRequestBody, stream))
			result := runExcelBPSHandlerRequest(t, handler, protocol, body)

			calls := upstream.calls()
			require.Len(t, calls, 2, "must pass pricing/auth and reach BPS: status=%d body=%s", result.status, result.body)
			throttled, healthy := calls[0], calls[1]
			require.NotEqual(t, throttled, healthy, "the throttled account must not be retried in this request")
			assertExcelBPSHandlerSuccess(t, protocol, result)
			require.NotContains(t, string(result.body), "basispoints_rate_limited")
			for _, url := range upstream.urls {
				require.Equal(t, basispoints.ResponsesURL, url)
			}
			require.NotEmpty(t, gjson.GetBytes(upstream.bodies[0], "input").Raw)
			require.Equal(t, gjson.GetBytes(upstream.bodies[0], "input").Raw, gjson.GetBytes(upstream.bodies[1], "input").Raw,
				"messages, tool calls and tool outputs must survive the account switch unchanged")
			rawEvents, ok := result.context.Get(service.OpsUpstreamErrorsKey)
			require.True(t, ok)
			events, ok := rawEvents.([]*service.OpsUpstreamErrorEvent)
			require.True(t, ok)
			require.Len(t, events, 1)
			require.Equal(t, "failover", events[0].Kind)
			require.Equal(t, throttled, events[0].AccountID)
			require.Equal(t, http.StatusTooManyRequests, events[0].UpstreamStatusCode)

			result = runExcelBPSHandlerRequest(t, handler, protocol, body)

			require.Equal(t, []int64{throttled, healthy, healthy}, upstream.calls(), "the cooling account is skipped by the next request")
			assertExcelBPSHandlerSuccess(t, protocol, result)
			seen := make(map[string]bool)
			for range 2 {
				select {
				case log := <-usage.created:
					require.False(t, seen[log.RequestID], "independent generations must settle distinct response identities")
					require.Contains(t, []string{"resp_bps_failover_1", "resp_bps_failover_2"}, log.RequestID)
					seen[log.RequestID] = true
					require.Equal(t, healthy, log.AccountID, "the rejected attempt has no measured usage")
					require.Equal(t, service.UsageAuthorizationModeBPS, log.AuthorizationMode)
					require.Equal(t, 3, log.InputTokens)
					require.Equal(t, 1, log.OutputTokens)
					require.Positive(t, log.InputCost, "measured usage must pass the production pricing resolver")
					require.Positive(t, log.OutputCost)
					require.InDelta(t, log.InputCost+log.OutputCost, log.TotalCost, 1e-12)
				case <-time.After(3 * time.Second):
					t.Fatal("each completed BPS request must record its measured usage")
				}
			}
			require.Empty(t, usage.created, "429 failover must not manufacture or duplicate usage")
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
		{name: "retry after beyond a week", retryAfter: "999999", wantRetryAfter: "999999"},
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
				usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 8)}
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
					router.Use(excelBPSHistoricalWSAdmission(t))
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
					defer func() { _ = client.CloseNow() }()
					require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
					for {
						_, event, err := client.Read(ctx)
						if failure.exhausted {
							require.NoError(t, err, "pool exhaustion ends the turn, not the socket")
							typ := gjson.GetBytes(event, "type").String()
							if typ != "error" && typ != "response.failed" {
								continue
							}
							require.NotContains(t, string(event), "PRIVATE_UPSTREAM")
							require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
							select {
							case streamErr := <-wsFailure:
								require.Equal(t, failure.status, streamErr.IntendedStatus)
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
				wantCalls := 2
				if failure.exhausted && failure.status >= 500 {
					wantCalls = 4
				}
				require.Len(t, calls, wantCalls)
				if failure.status >= 500 {
					require.Equal(t, calls[0], calls[1])
					if failure.exhausted {
						require.Equal(t, calls[2], calls[3])
						require.NotEqual(t, calls[0], calls[2])
					}
				} else {
					require.NotEqual(t, calls[0], calls[1])
				}
				require.Equal(t, gjson.GetBytes(upstream.bodies[0], "input").Raw, gjson.GetBytes(upstream.bodies[1], "input").Raw)
				for _, url := range upstream.urls {
					require.Equal(t, basispoints.ResponsesURL, url)
				}
				wantLogs := 1
				if failure.exhausted {
					wantLogs = 0
				}
				if failure.event != "" {
					wantLogs = wantCalls
				}
				logs := make([]*service.UsageLog, 0, wantLogs)
				for range wantLogs {
					select {
					case log := <-usage.created:
						require.Equal(t, service.UsageAuthorizationModeBPS, log.AuthorizationMode)
						logs = append(logs, log)
					case <-time.After(3 * time.Second):
						t.Fatal("each metered attempt must be recorded before failover completes")
					}
				}
				require.Len(t, logs, wantLogs)
				if !failure.exhausted {
					require.Equal(t, 3, logs[len(logs)-1].InputTokens)
					require.Equal(t, 1, logs[len(logs)-1].OutputTokens)
				}
				if failure.event != "" {
					require.Equal(t, 7, logs[0].InputTokens)
					require.Equal(t, 2, logs[0].OutputTokens)
					require.NotEqual(t, logs[0].RequestID, logs[len(logs)-1].RequestID)
					if failure.exhausted {
						require.Equal(t, 7, logs[len(logs)-1].InputTokens)
						require.Equal(t, 2, logs[len(logs)-1].OutputTokens)
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
				router.Use(excelBPSHistoricalWSAdmission(t))
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
				defer func() { _ = client.CloseNow() }()
				require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
				_, event, err := client.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, "error", gjson.GetBytes(event, "type").String())
				require.Contains(t, string(event), "basispoints_rate_limited")
				require.NoError(t, client.CloseNow())
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

func TestExcelBPS429PoolNeverSelectsNative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range excelBPSHandlerProtocols() {
		for _, outcome := range []string{"second_bps_succeeds", "all_bps_limited", "no_native_grant"} {
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
					extra := map[string]any{"openai_excel_bps": id != 20, "openai_oauth_responses_websockets_v2_mode": "http_bridge"}
					if id != 20 {
						extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
					}
					accounts = append(accounts, service.Account{
						ID: id, Name: fmt.Sprintf("account-%d", id), Priority: int(id),
						Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
						Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{3131},
						Credentials: map[string]any{"client_id": clientID, "access_token": fmt.Sprintf("token-%d", id), "chatgpt_account_id": fmt.Sprintf("workspace-%d", id)},
						Extra:       extra,
					})
				}
				var img bytes.Buffer
				require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 3))))
				stream := excelBPSHandlerStream(protocol)
				payload := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": []any{
					map[string]any{"type": "compaction", "encrypted_content": "PRIVATE_REPLAYABLE_HISTORY"},
					map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())}}},
				}}
				if protocol == "ws" {
					payload["type"] = "response.create"
				}
				compatible := strings.HasPrefix(protocol, "chat") || strings.HasPrefix(protocol, "messages")
				if compatible {
					payload = map[string]any{"model": "gpt-6-astra", "stream": stream, "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
					if strings.HasPrefix(protocol, "messages") {
						payload["max_tokens"] = 100
					}
				}
				raw, err := json.Marshal(payload)
				require.NoError(t, err)
				upstream := &excelBPSFailoverUpstream{}
				upstream.answer = func(call int) *http.Response {
					id := upstream.calls()[call]
					if id == 20 {
						t.Error("the current BPS operation dispatched to a Native candidate")
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
				usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
				var authority excelBPSHandlerAuthority
				h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{
					usage: usage, accounts: accounts,
					configureAuthority: func(current excelBPSHandlerAuthority) { authority = current },
					settings: map[string]string{
						service.SettingKeyExcelBPSImageRelayEnabled: "true", service.SettingKeyExcelBPSImageMode: "native",
					},
				})
				result := runExcelBPSHandlerRequest(t, h, protocol, raw)
				calls := upstream.calls()
				require.NotEmpty(t, calls, "must pass pricing/auth and reach BPS: status=%d body=%s readErr=%v", result.status, result.body, result.readErr)
				require.NotContains(t, calls, int64(20), "Native remains ineligible after BPS pool exhaustion")
				for _, url := range upstream.urls {
					require.Contains(t, url, "/basispoints/api/", "all sends stay in the selected BPS mode")
				}
				require.NotContains(t, string(result.body), "PRIVATE_UPSTREAM")
				for _, id := range []int64{10, 30} {
					account, err := authority.accounts.GetByID(t.Context(), id)
					require.NoError(t, err)
					require.True(t, account.IsExcelBPSEnabled(), "429 must not disable BPS to make Native eligible")
				}
				if outcome == "second_bps_succeeds" {
					assertExcelBPSHandlerSuccess(t, protocol, result)
					if compatible {
						require.Equal(t, []int64{10, 30}, calls, "one rejected BPS generation followed by one successful BPS generation")
						require.Equal(t, []string{basispoints.ResponsesURL, basispoints.ResponsesURL}, upstream.urls)
						require.NotEmpty(t, gjson.GetBytes(upstream.bodies[0], "input").Raw)
						require.JSONEq(t, gjson.GetBytes(upstream.bodies[0], "input").Raw, gjson.GetBytes(upstream.bodies[1], "input").Raw)
					} else {
						require.Equal(t, []int64{10, 30, 30}, calls, "retry the upload on BPS 30, then generate only on that file owner")
						require.Equal(t, []string{basispoints.AttachmentsURL, basispoints.AttachmentsURL, basispoints.ResponsesURL}, upstream.urls)
						require.Contains(t, string(upstream.bodies[len(calls)-1]), "PRIVATE_REPLAYABLE_HISTORY")
						require.Contains(t, string(upstream.bodies[len(calls)-1]), "file-second-account")
					}
					select {
					case log := <-usage.created:
						require.Equal(t, int64(30), log.AccountID)
						require.Equal(t, service.UsageAuthorizationModeBPS, log.AuthorizationMode)
						require.Equal(t, 3, log.InputTokens)
						require.Equal(t, 1, log.OutputTokens)
						require.Positive(t, log.InputCost, "measured usage must pass the production pricing resolver")
						require.Positive(t, log.OutputCost)
						require.InDelta(t, log.InputCost+log.OutputCost, log.TotalCost, 1e-12)
					case <-time.After(3 * time.Second):
						t.Fatal("the completed BPS candidate must record its measured usage")
					}
				} else {
					require.Equal(t, []int64{10, 30}, calls, "pool exhaustion ends the operation after the two BPS candidates")
					endpoint := basispoints.AttachmentsURL
					if compatible {
						endpoint = basispoints.ResponsesURL
					}
					require.Equal(t, []string{endpoint, endpoint}, upstream.urls)
					require.NoError(t, result.readErr, "pool exhaustion must produce a protocol error, not EOF or a socket failure")
					if protocol == "ws" {
						require.Equal(t, http.StatusSwitchingProtocols, result.status)
						require.NotEmpty(t, result.events)
						terminal := result.events[len(result.events)-1]
						require.Equal(t, "error", gjson.GetBytes(terminal, "type").String(), "both attempts rejected before generation")
						require.Equal(t, "basispoints_rate_limited", gjson.GetBytes(terminal, "error.code").String())
						streamErr, ok := service.GetOpsStreamError(result.context)
						require.True(t, ok, "the handler must record the intended upstream failure status")
						require.Equal(t, http.StatusTooManyRequests, streamErr.IntendedStatus)
					} else {
						require.Equal(t, http.StatusTooManyRequests, result.status, "%s", result.body)
						require.True(t, json.Valid(result.body), "%s", result.body)
						require.Equal(t, "rate_limit_error", gjson.GetBytes(result.body, "error.type").String())
						if !strings.HasPrefix(protocol, "messages") {
							require.Equal(t, "basispoints_rate_limited", gjson.GetBytes(result.body, "error.code").String())
						}
					}
					require.NotContains(t, string(result.body), "response.completed")
				}
				require.Empty(t, usage.created, "rejected BPS candidates must not manufacture usage or Native billing")
			})
		}
	}
}

func TestTurnRecoveryWSHandlerExhaustedThenTwoSuccessfulTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var recovered atomic.Bool
	upstream := &excelBPSFailoverUpstream{answer: func(call int) *http.Response {
		if !recovered.Load() {
			return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"server_error","message":"PRIVATE_UPSTREAM"}}`))}
		}
		return excelBPSCompleted()
	}}
	h := newExcelBPSFailoverTestHandler(t, upstream, false)
	c, _ := newExcelBPSFailoverTestContext(t.Context(), true)
	completed := make(chan struct{})
	router := gin.New()
	router.GET("/v1/responses", func(wsCtx *gin.Context) {
		defer close(completed)
		wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
		wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
		h.ResponsesWebSocket(wsCtx)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","input":"failed turn marker","event_id":"failed_request"}`)))
	for {
		_, event, err := client.Read(ctx)
		require.NoError(t, err)
		require.NotContains(t, string(event), "PRIVATE_UPSTREAM")
		typ := gjson.GetBytes(event, "type").String()
		if typ == "error" || typ == "response.failed" {
			break
		}
	}
	firstCalls := len(upstream.calls())
	calls := upstream.calls()
	require.Len(t, calls, 4)
	require.Equal(t, calls[0], calls[1], "retry the actual first owner")
	require.Equal(t, calls[2], calls[3], "retry the actual second owner")
	require.NotEqual(t, calls[0], calls[2], "switch to an independent owner")
	require.ElementsMatch(t, []int64{1, 1, 2, 2}, calls)
	recovered.Store(true)
	for _, marker := range []string{"second fresh request", "third fresh request"} {
		next := map[string]any{"type": "response.create", "model": "gpt-6-astra", "input": marker}
		if marker == "third fresh request" {
			next["previous_response_id"] = "resp_bps_failover"
		}
		payload, _ := json.Marshal(next)
		require.NoError(t, client.Write(ctx, coderws.MessageText, payload))
		for {
			_, event, err := client.Read(ctx)
			require.NoError(t, err)
			require.NotEqual(t, "error", gjson.GetBytes(event, "type").String(), string(event))
			require.NotEqual(t, "response.failed", gjson.GetBytes(event, "type").String(), string(event))
			if gjson.GetBytes(event, "type").String() == "response.completed" {
				break
			}
		}
	}
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-completed:
	case <-ctx.Done():
		t.Fatal("handler did not release connection")
	}
	require.Len(t, upstream.calls(), firstCalls+2)
	for _, body := range upstream.bodies[firstCalls:] {
		require.NotContains(t, string(body), "failed turn marker")
	}
}

func TestExcelBPSManagedAttachmentRecoveryIsBounded(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, mode := range []string{"ownership_recovers", "ownership_exhausted", "attachment_503"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				var img bytes.Buffer
				require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 3))))
				payload := map[string]any{"model": "gpt-6-astra", "stream": protocol != "json", "input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(img.Bytes())}}}}}
				if protocol == "ws" {
					payload["type"] = "response.create"
				}
				raw, err := json.Marshal(payload)
				require.NoError(t, err)
				upstream := &excelBPSFailoverUpstream{}
				uploads, generations := 0, 0
				upstream.answer = func(call int) *http.Response {
					if upstream.urls[call] == basispoints.AttachmentsURL {
						uploads++
						if mode == "attachment_503" && uploads == 1 {
							return &http.Response{StatusCode: 503, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"error\":{\"code\":\"server_is_overloaded\"}}"))}
						}
						return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf("{\"openai_file_id\":\"file-recovery-%d\"}", uploads)))}
					}
					generations++
					if mode == "ownership_exhausted" || (mode == "ownership_recovers" && generations == 1) {
						return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"error\":{\"message\":\"Unknown error while validating file ownership.\",\"type\":\"server_error\"}}"))}
					}
					return excelBPSCompleted()
				}
				h := newExcelBPSFailoverTestHandler(t, upstream, false, excelBPSFailoverTestOptions{settings: map[string]string{service.SettingKeyExcelBPSImageRelayEnabled: "true", service.SettingKeyExcelBPSImageMode: "native"}})
				c, rec := newExcelBPSFailoverTestContext(t.Context(), protocol != "json")
				output := ""
				if protocol == "ws" {
					router := gin.New()
					router.Use(excelBPSHistoricalWSAdmission(t))
					done := make(chan struct{})
					router.GET("/v1/responses", func(wsCtx *gin.Context) {
						defer close(done)
						wsCtx.Set(string(middleware2.ContextKeyAPIKey), c.MustGet(string(middleware2.ContextKeyAPIKey)))
						wsCtx.Set(string(middleware2.ContextKeyUser), c.MustGet(string(middleware2.ContextKeyUser)))
						h.ResponsesWebSocket(wsCtx)
					})
					server := httptest.NewServer(router)
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
					defer cancel()
					client, _, e := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
					require.NoError(t, e)
					defer client.CloseNow()
					require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
					for {
						_, event, e := client.Read(ctx)
						if e != nil {
							require.Equal(t, "ownership_exhausted", mode)
							break
						}
						output += string(event)
						kind := gjson.GetBytes(event, "type").String()
						if kind == "response.completed" || kind == "response.failed" || kind == "error" {
							break
						}
					}
					_ = client.CloseNow()
					select {
					case <-done:
					case <-ctx.Done():
						t.Fatal("attachment recovery did not finish")
					}
				} else {
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
					h.Responses(c)
					output = rec.Body.String()
				}
				require.Equal(t, 2, uploads, "one fresh upload after rejection, never a retry loop")
				if mode == "attachment_503" {
					require.Equal(t, 1, generations)
				} else {
					require.Equal(t, 2, generations)
					ids := upstream.calls()
					for _, id := range ids {
						require.Equal(t, ids[0], id, "ownership recovery cannot move accounts")
					}
				}
				if mode == "ownership_exhausted" {
					require.NotContains(t, output, "response.completed")
				} else {
					require.Contains(t, output, "done")
				}
			})
		}
	}
}
