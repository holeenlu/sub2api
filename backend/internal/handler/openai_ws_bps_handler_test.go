package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsWSHandlerUpstream struct {
	service.HTTPUpstream
	mu       sync.Mutex
	urls     []string
	terminal string
}

func (u *bpsWSHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.urls = append(u.urls, req.URL.String())
	u.mu.Unlock()
	status := strings.TrimPrefix(u.terminal, "response.")
	wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_bps_handler\",\"model\":\"gpt-6-astra\"}}\n\n" +
		fmt.Sprintf("data: {\"type\":%q,\"response\":{\"id\":\"resp_bps_handler\",\"model\":\"gpt-6-astra\",\"status\":%q,\"output\":[],\"usage\":{\"input_tokens\":7,\"output_tokens\":3}}}\n\n", u.terminal, status)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
}

func (u *bpsWSHandlerUpstream) requests() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.urls...)
}

type bpsWSHandlerHarness struct {
	server   *httptest.Server
	repo     *wsRevalidationKeyRepoStub
	upstream *bpsWSHandlerUpstream
	logs     chan *service.UsageLog
	done     chan struct{}
}

// Mount real auth, turn admission, WS/HTTP handlers and usage recording. Only
// the BPS network and persistence boundaries are stubbed.
func newBPSWSHandlerHarness(t *testing.T, terminal string, configure ...func(*service.Account)) *bpsWSHandlerHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	key := wsRevalidationKey(0, false, nil, true)
	key.Group.RateMultiplier = 1
	price := 0.25
	key.Group.ModelPricing = []service.ChannelModelPricing{{
		Models: []string{"gpt-6-astra"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price,
	}}
	repo := &wsRevalidationKeyRepoStub{current: key}
	account := service.Account{
		ID: 9901, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 4, GroupIDs: []int64{key.Group.ID},
		Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"},
		Extra:       map[string]any{"openai_excel_bps": true, "openai_oauth_responses_websockets_v2_mode": service.OpenAIWSIngressModeOff},
	}
	for _, apply := range configure {
		apply(&account)
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	apiKeyService := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	billing := service.NewBillingService(cfg, nil)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	upstream := &bpsWSHandlerUpstream{terminal: terminal}
	logs := make(chan *service.UsageLog, 8)
	gateway := service.NewOpenAIGatewayService(
		&openAIWSUsageHandlerAccountRepoStub{account: account},
		&openAIWSUsageHandlerUsageLogRepoStub{created: logs},
		nil, nil, nil, nil, nil, cfg, nil, nil, billing, nil, billingCache,
		upstream, &service.DeferredService{}, nil, nil,
		service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil,
	)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := &OpenAIGatewayHandler{
		cfg: cfg, gatewayService: gateway, billingCacheService: billingCache, apiKeyService: apiKeyService,
		concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, time.Second),
	}
	done := make(chan struct{})
	router := gin.New()
	router.Use(gin.HandlerFunc(middleware2.NewAPIKeyAuthMiddleware(apiKeyService, nil, cfg)))
	router.GET("/v1/responses", func(c *gin.Context) { defer close(done); h.ResponsesWebSocket(c) })
	router.POST("/v1/responses", func(c *gin.Context) { defer close(done); h.Responses(c) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &bpsWSHandlerHarness{server: server, repo: repo, upstream: upstream, logs: logs, done: done}
}

func (h *bpsWSHandlerHarness) dial(t *testing.T) *coderws.Conn {
	t.Helper()
	return h.dialWithHeader(t, http.Header{})
}

func (h *bpsWSHandlerHarness) dialWithHeader(t *testing.T, header http.Header) *coderws.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header = header.Clone()
	header.Set("Authorization", "Bearer sk-ws-revalidation-followup")
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(h.server.URL, "http")+"/v1/responses", &coderws.DialOptions{
		HTTPHeader: header,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func writeBPSHandlerTurn(t *testing.T, conn *coderws.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(payload)))
}

func readBPSHandlerTerminal(t *testing.T, conn *coderws.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, payload, err := conn.Read(ctx)
		require.NoError(t, err)
		switch gjson.GetBytes(payload, "type").String() {
		case "response.completed", "response.failed", "response.incomplete", "error":
			return payload
		}
	}
}

func (h *bpsWSHandlerHarness) finishedLogs(t *testing.T) []*service.UsageLog {
	t.Helper()
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not exit")
	}
	var logs []*service.UsageLog
	for {
		select {
		case log := <-h.logs:
			logs = append(logs, log)
		default:
			return logs
		}
	}
}

func TestOpenAIWSBPSHandlerPrewarmIsNotBilled(t *testing.T) {
	h := newBPSWSHandlerHarness(t, "response.completed")
	conn := h.dial(t)
	writeBPSHandlerTurn(t, conn, `{"type":"response.create","model":"gpt-6-astra","generate":false,"input":[]}`)
	prewarm := readBPSHandlerTerminal(t, conn)
	require.Equal(t, "response.completed", gjson.GetBytes(prewarm, "type").String())
	writeBPSHandlerTurn(t, conn, fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":[{"role":"user","content":"hello"}]}`, gjson.GetBytes(prewarm, "response.id").String()))
	readBPSHandlerTerminal(t, conn)
	_ = conn.CloseNow()
	logs := h.finishedLogs(t)
	require.Len(t, h.upstream.requests(), 1)
	require.Len(t, logs, 1, "local prewarm must not create a second per-request bill")
	require.Equal(t, 7, logs[0].InputTokens)
	require.Equal(t, 3, logs[0].OutputTokens)
	require.InDelta(t, 0.25, logs[0].ActualCost, 1e-12)
}

func TestOpenAIWSBPSHandlerBillsPartialUsage(t *testing.T) {
	for _, terminal := range []string{"response.failed", "response.incomplete"} {
		t.Run(terminal, func(t *testing.T) {
			h := newBPSWSHandlerHarness(t, terminal)
			conn := h.dial(t)
			writeBPSHandlerTurn(t, conn, `{"type":"response.create","model":"gpt-6-astra","input":"hello"}`)
			event := readBPSHandlerTerminal(t, conn)
			require.Equal(t, terminal, gjson.GetBytes(event, "type").String())
			_ = conn.CloseNow()
			logs := h.finishedLogs(t)
			require.Len(t, logs, 1, "upstream metered usage must survive a terminal failure")
			require.Equal(t, 7, logs[0].InputTokens)
			require.Equal(t, 3, logs[0].OutputTokens)
			require.InDelta(t, 0.25, logs[0].ActualCost, 1e-12)
		})
	}
}

func TestOpenAIWSBPSHandlerRevalidatesInheritedTools(t *testing.T) {
	for _, clearTools := range []bool{false, true} {
		t.Run(fmt.Sprint(clearTools), func(t *testing.T) {
			h := newBPSWSHandlerHarness(t, "response.completed")
			conn := h.dial(t)
			writeBPSHandlerTurn(t, conn, `{"type":"response.create","model":"gpt-6-astra","generate":false,"input":[],"tools":[{"type":"image_generation"}]}`)
			readBPSHandlerTerminal(t, conn)
			h.repo.set(wsRevalidationKey(0, false, nil, false))
			tools := ""
			if clearTools {
				tools = `,"tools":[]`
			}
			writeBPSHandlerTurn(t, conn, `{"type":"response.create","input":"hello"`+tools+`}`)
			if clearTools {
				readBPSHandlerTerminal(t, conn)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_, _, err := conn.Read(ctx)
				require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
			}
			_ = conn.CloseNow()
			h.finishedLogs(t)
			if clearTools {
				require.Equal(t, []string{"https://bps.openai.com/basispoints/api/responses"}, h.upstream.requests())
			} else {
				require.Empty(t, h.upstream.requests(), "revoked inherited image tool must not reach native or BPS upstream")
			}
		})
	}
}

func TestHTTPBPSHandlerCompatibility(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			h := newBPSWSHandlerHarness(t, "response.completed")
			req, err := http.NewRequest(http.MethodPost, h.server.URL+"/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-6-astra","input":"hello","stream":%t}`, stream)))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer sk-ws-revalidation-followup")
			req.Header.Set("Content-Type", "application/json")
			resp, err := h.server.Client().Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode, "%s", body)
			if stream {
				require.Contains(t, string(body), "response.completed")
			} else {
				require.Equal(t, "completed", gjson.GetBytes(body, "status").String())
			}
			require.Equal(t, []string{"https://bps.openai.com/basispoints/api/responses"}, h.upstream.requests())
			logs := h.finishedLogs(t)
			require.Len(t, logs, 1)
			require.Equal(t, 7, logs[0].InputTokens)
			require.Equal(t, 3, logs[0].OutputTokens)
		})
	}
}

// The same codex_cli_only account answers a non-Codex client identically over
// HTTP and WebSocket: 403 with the shared message, no upstream request and no
// usage. WS adds the policy close after the error event.
func TestCodexCLIOnlyHandlerRejectsNonCodexClientOnBothTransports(t *testing.T) {
	codexCLIOnly := func(a *service.Account) { a.Extra["codex_cli_only"] = true }
	t.Run("websocket", func(t *testing.T) {
		h := newBPSWSHandlerHarness(t, "response.completed", codexCLIOnly)
		conn := h.dialWithHeader(t, http.Header{"User-Agent": {"curl/8.0"}})
		writeBPSHandlerTurn(t, conn, `{"type":"response.create","model":"gpt-6-astra","input":"hello"}`)
		event := readBPSHandlerTerminal(t, conn)
		require.Equal(t, "error", gjson.GetBytes(event, "type").String())
		require.Equal(t, int64(http.StatusForbidden), gjson.GetBytes(event, "status").Int())
		require.Equal(t, service.CodexOfficialClientsOnlyMessage, gjson.GetBytes(event, "error.message").String())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _, err := conn.Read(ctx)
		require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(err))
		require.Empty(t, h.finishedLogs(t))
		require.Empty(t, h.upstream.requests())
	})
	t.Run("http", func(t *testing.T) {
		h := newBPSWSHandlerHarness(t, "response.completed", codexCLIOnly)
		req, err := http.NewRequest(http.MethodPost, h.server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"hello","stream":true}`))
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer sk-ws-revalidation-followup")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "curl/8.0")
		resp, err := h.server.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusForbidden, resp.StatusCode, "%s", body)
		require.Equal(t, service.CodexOfficialClientsOnlyMessage, gjson.GetBytes(body, "error.message").String())
		require.Empty(t, h.finishedLogs(t))
		require.Empty(t, h.upstream.requests())
	})
}
