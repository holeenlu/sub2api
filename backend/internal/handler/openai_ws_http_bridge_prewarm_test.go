package handler

import (
	"context"
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

// bridgePrewarmUpstream records each bridged Responses request and answers it
// with one completed response.
type bridgePrewarmUpstream struct {
	service.HTTPUpstream
	mu     sync.Mutex
	bodies []string
}

func (u *bridgePrewarmUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.bodies = append(u.bodies, string(body))
	u.mu.Unlock()
	wire := `data: {"type":"response.completed","response":{"id":"resp_bridge_upstream","model":"gpt-5.1","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":3}}}` + "\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
}

func (u *bridgePrewarmUpstream) requests() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.bodies...)
}

// Codex opens every WebSocket connection with a generate=false prewarm. An HTTP
// bridge acknowledges it without an upstream request, so it must not be billed
// either: a per-request price is charged once, for the business request that
// continues from the prewarm response.
func TestOpenAIResponsesWebSocketHTTPBridgePrewarmIsNotBilled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	key := wsRevalidationKey(0, false, nil, true)
	key.Group.RateMultiplier = 1
	price := 0.25
	key.Group.ModelPricing = []service.ChannelModelPricing{{
		Models: []string{"gpt-5.1"}, BillingMode: service.BillingModePerRequest, PerRequestPrice: &price,
	}}
	repo := &wsRevalidationKeyRepoStub{current: key}
	account := service.Account{
		ID: 9902, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 4, GroupIDs: []int64{key.Group.ID},
		Credentials: map[string]any{"api_key": "sk-upstream"},
		Extra:       map[string]any{"openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Default.RateMultiplier = 1
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	apiKeyService := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	billing := service.NewBillingService(cfg, nil)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	upstream := &bridgePrewarmUpstream{}
	logs := make(chan *service.UsageLog, 4)
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
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + key.Key}},
	})
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	readCompleted := func() []byte {
		for {
			_, event, err := conn.Read(ctx)
			require.NoError(t, err)
			eventType := gjson.GetBytes(event, "type").String()
			require.NotContains(t, []string{"error", "response.failed"}, eventType, string(event))
			if eventType == "response.completed" {
				return event
			}
		}
	}

	question := `{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}`
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","generate":false,"input":[`+question+`]}`)))
	prewarmID := gjson.GetBytes(readCompleted(), "response.id").String()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"`+prewarmID+`","input":[]}`)))
	require.Equal(t, "resp_bridge_upstream", gjson.GetBytes(readCompleted(), "response.id").String())
	require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not exit")
	}

	bodies := upstream.requests()
	require.Len(t, bodies, 1, "the prewarm must not reach upstream")
	require.Equal(t, "hello", gjson.Get(bodies[0], "input.0.content.0.text").String())
	require.Len(t, logs, 1, "the prewarm must not be billed")
	log := <-logs
	require.Equal(t, 7, log.InputTokens)
	require.Equal(t, 3, log.OutputTokens)
	require.InDelta(t, 0.25, log.ActualCost, 1e-12)
}
