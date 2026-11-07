//go:build unit

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

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/host"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsNextExecutionSend struct {
	mode, model, id string
	body           []byte
}

// Reuse the existing sealed transport and journal doubles. Only the provider
// wire is scripted; middleware, multiplexing, handler admission and dispatch
// execute normally over one continuously open client WebSocket.
type wsNextExecutionUpstream struct {
	service.HTTPUpstream
	mu      sync.Mutex
	sends   []wsNextExecutionSend
	entered chan wsNextExecutionSend
	release chan struct{}
}

func (u *wsNextExecutionUpstream) complete(ctx context.Context, body []byte, mode string) ([]byte, error) {
	u.mu.Lock()
	n := len(u.sends) + 1
	send := wsNextExecutionSend{mode, gjson.GetBytes(body, "model").String(), fmt.Sprintf("resp_handoff_%d", n), body}
	u.sends = append(u.sends, send)
	u.mu.Unlock()
	u.entered <- send
	if n == 1 && u.release != nil {
		select {
		case <-u.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","model":%q,"output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`, send.id, send.model)), nil
}

func (u *wsNextExecutionUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	mode := service.UsageAuthorizationModeNative
	if req.URL.Hostname() == "bps.openai.com" {
		mode = service.UsageAuthorizationModeBPS
	}
	event, err := u.complete(req.Context(), body, mode)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + string(event) + "\n\n"))}, nil
}

type wsNextExecutionTransport struct{ *nativeHandlerTransport }

func (u *wsNextExecutionTransport) PrepareNativeRequest(req *http.Request, maxBody int64) (*http.Request, error) {
	if req.URL.Hostname() == "bps.openai.com" && maxBody > 0 {
		u.prepared.Add(1)
		return req, nil
	}
	return u.nativeHandlerTransport.PrepareNativeRequest(req, maxBody)
}

type wsNextExecutionFixture struct {
	client    *coderws.Conn
	done      chan *gin.Context
	upstream  *wsNextExecutionUpstream
	probe     *wsPlanRejectionProbe
	admission *wsNativeFixtureAdmission
	usage     *openAIWSUsageHandlerUsageLogRepoStub
	journal   *bpsModeHandlerJournal
	store     *nativeHandlerStore
	keys      *service.APIKeyService
}

func newWSNextExecutionFixture(t *testing.T, mode string, holdFirst bool) *wsNextExecutionFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	u := &wsNextExecutionUpstream{entered: make(chan wsNextExecutionSend, 16)}
	if holdFirst {
		u.release = make(chan struct{})
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			event, err := u.complete(r.Context(), body, service.UsageAuthorizationModeNative)
			if err != nil || conn.Write(r.Context(), coderws.MessageText, event) != nil {
				return
			}
		}
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, OpenAITurnMaxAttempts: 1}}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
	accounts := []service.Account{
		{ID: 10, Name: "native", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 2,
			GroupIDs: []int64{nativeHandlerFixtureGroupID}, Credentials: map[string]any{"api_key": "sk-handoff-native", "model_mapping": map[string]any{"channel-a": "gpt-6.1-sol"}},
			Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": mode}},
		{ID: 11, Name: "bps", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 2,
			GroupIDs: []int64{nativeHandlerFixtureGroupID}, Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": "handoff-excel", "chatgpt_account_id": "handoff-workspace", "model_mapping": map[string]any{"channel-b": "gpt-6-astra"}},
			Extra: map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra"}}},
	}
	if mode != service.OpenAIWSIngressModeHTTPBridge {
		accounts[0].Credentials["base_url"] = upstream.URL
	}
	authority := newExcelBPSHandlerAuthority(t, cfg, accounts, nativeHandlerFixtureGroupID)
	authority.accounts.store.update(func(s *nativeHandlerStore) {
		s.key.User.Concurrency = 2
		s.key.Group.RateMultiplier = 1
		s.key.Group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"public-a", "public-b"}}
	})
	probe := &wsPlanRejectionProbe{}
	concurrency := service.NewConcurrencyService(newWSNativeFixtureCapacity(t, nil, probe.beforeAcquire))
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 16)}
	journal := &bpsModeHandlerJournal{wsNativeFixtureJournal: &wsNativeFixtureJournal{logSink: usage}}
	transport := &wsNextExecutionTransport{&nativeHandlerTransport{responder: u}}
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
		channels: []service.Channel{{ID: 7703, Name: "next-execution", Status: service.StatusActive, GroupIDs: []int64{nativeHandlerFixtureGroupID}, BillingModelSource: service.BillingModelSourceUpstream,
			ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"public-a": "channel-a", "public-b": "channel-b"}}}},
		groupPlatforms: map[int64]string{nativeHandlerFixtureGroupID: service.PlatformOpenAI},
	}, nil, nil, nil, nil)
	settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyExcelBPSEnabled: "true"}}, cfg)
	gateway := service.NewOpenAIGatewayService(authority.accounts, usage, journal, authority.users, nil, nil, nil, cfg,
		nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, transport, &service.DeferredService{}, nil, nil, nil, channels, nil, settings, nil)
	t.Cleanup(gateway.CloseOpenAIWSPool)
	t.Cleanup(func() { require.NoError(t, gateway.CloseExcelBPSImages()) })
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	cleanupHandlerBillingCache(t, billing)
	require.NoError(t, billing.Start(t.Context()))
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, authority.keys, nil, nil, nil, nil, cfg)
	admission := &wsNativeFixtureAdmission{}
	done := make(chan *gin.Context, 1)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(host.WithTurnAdmission(c.Request.Context(), admission))
		defer func() { done <- c }()
		c.Next()
	}, gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(authority.keys, nil, cfg)))
	router.GET("/v1/responses", h.ResponsesWebSocket)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key}}})
	require.NoError(t, err)
	f := &wsNextExecutionFixture{client, done, u, probe, admission, usage, journal, authority.accounts.store, authority.keys}
	t.Cleanup(func() {
		_ = client.CloseNow()
		if u.release != nil {
			select {
			case <-u.release:
			default:
				close(u.release)
			}
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("next-execution handler did not join")
		}
		admission.assertClosed(t, true)
		require.Zero(t, transport.legacy.Load())
		journal.assertNoLegacyApply(t)
	})
	return f
}

func (f *wsNextExecutionFixture) sent(t *testing.T) wsNextExecutionSend {
	t.Helper()
	select {
	case send := <-f.upstream.entered:
		return send
	case <-time.After(3 * time.Second):
		t.Fatal("next create did not reach its selected mode")
		return wsNextExecutionSend{}
	}
}

func wsNextCreate(lane, model, input string) string {
	if lane == "" {
		// The default stream omits stream_id. An explicitly empty ID is an
		// invalid envelope, rejected before even the first host admission.
		return fmt.Sprintf(`{"type":"response.create","model":%q,"input":%q}`, model, input)
	}
	return fmt.Sprintf(`{"type":"response.create","stream_id":%q,"model":%q,"input":%q}`, lane, model, input)
}

func wsNextTerminal(t *testing.T, client *coderws.Conn) []byte {
	t.Helper()
	for i := 0; i < 16; i++ {
		event := wsPlanRead(t, client)
		kind := gjson.GetBytes(event, "type").String()
		require.NotEqual(t, "error", kind, "%s", event)
		require.NotEqual(t, "response.failed", kind, "%s", event)
		if kind == "response.completed" {
			return event
		}
	}
	t.Fatal("missing terminal after bounded provider preamble")
	return nil
}

func (f *wsNextExecutionFixture) assertFinished(t *testing.T, turns int) []*service.UsageLog {
	t.Helper()
	_ = f.client.CloseNow()
	select {
	case c := <-f.done:
		f.done <- c
		require.Empty(t, service.GetOpsStreamErrors(c), "handoffs must not create failure telemetry")
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not join")
	}
	_, accepted, released, live := f.admission.snapshot()
	require.Equal(t, turns, accepted, "handoff must not acquire a second host admission")
	require.Equal(t, turns, released)
	require.Zero(t, live)
	f.probe.mu.Lock()
	acquired := append([]wsPlanRejectionAcquisition(nil), f.probe.acquisitions...)
	f.probe.mu.Unlock()
	require.Len(t, acquired, turns*2)
	counts := make(map[*routing.CapacityScope][2]int)
	prices := make(map[*routing.CapacityScope]time.Time)
	for _, a := range acquired {
		require.True(t, service.NativeHTTPOperationBound(a.ctx))
		require.NotNil(t, a.scope)
		at := service.OpenAIPricingAtFromContext(a.ctx)
		require.False(t, at.IsZero())
		if prior, ok := prices[a.scope]; ok {
			require.Equal(t, prior, at, "a logical turn must retain exactly one pricing snapshot")
		} else {
			prices[a.scope] = at
		}
		n := counts[a.scope]
		if a.user {
			n[0]++
		} else {
			n[1]++
		}
		counts[a.scope] = n
	}
	require.Len(t, counts, turns, "each independent create needs a new operation")
	for scope, n := range counts {
		require.Equal(t, [2]int{1, 1}, n, "one user and account acquisition per operation")
		for _, target := range []struct {
			user bool
			id   int64
		}{{true, 100}, {false, 10}, {false, 11}} {
			if receipt := scope.Lookup(target.user, target.id); receipt != nil {
				_, err := receipt.Verify(t.Context(), receipt.Policy())
				require.Error(t, err, "finished turn retained its capacity lease")
			}
		}
	}
	seen := make(map[string]bool)
	var logs []*service.UsageLog
	for i := 0; i < turns; i++ {
		select {
		case log := <-f.usage.created:
			logs = append(logs, log)
			require.False(t, seen[log.RequestID], "duplicate settlement")
			seen[log.RequestID] = true
			require.Equal(t, 3, log.InputTokens)
			require.Equal(t, 1, log.OutputTokens)
		case <-time.After(3 * time.Second):
			t.Fatal("missing per-turn usage")
		}
	}
	require.Empty(t, f.usage.created)
	return logs
}

func TestOpenAIWSNextExecutionContinuousSocketModes(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeHTTPBridge, service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		for _, lane := range []string{"", "same-lane"} {
			t.Run(mode+"/"+lane, func(t *testing.T) {
				f := newWSNextExecutionFixture(t, mode, false)
				for i, model := range []string{"public-a", "public-b", "public-a"} {
					f.store.update(func(s *nativeHandlerStore) { s.key.Group.RateMultiplier = float64(i + 1) })
					f.keys.InvalidateAuthCacheByKey(t.Context(), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key)
					wsPlanWrite(t, f.client, wsNextCreate(lane, model, fmt.Sprintf("independent-%d", i)))
					send := f.sent(t)
					expectedMode, expectedModel := service.UsageAuthorizationModeNative, "gpt-6.1-sol"
					if i == 1 {
						expectedMode, expectedModel = service.UsageAuthorizationModeBPS, "gpt-6-astra"
					}
					require.Equal(t, expectedMode, send.mode)
					require.Equal(t, expectedModel, send.model, "channel and account mapping apply once")
					event := wsNextTerminal(t, f.client)
					require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), "%s", event)
					require.Equal(t, send.id, gjson.GetBytes(event, "response.id").String())
					require.Equal(t, lane, gjson.GetBytes(event, "stream_id").String())
				}
				logs := f.assertFinished(t, 3)
				for i, log := range logs {
					require.InDelta(t, float64(i+1), log.RateMultiplier, 1e-12, "each mode must bill with its own newly frozen group price exactly once")
				}
			})
		}
	}
}

func TestOpenAIWSNextExecutionNamedLaneFIFO(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			f := newWSNextExecutionFixture(t, mode, true)
			wsPlanWrite(t, f.client, wsNextCreate("a", "public-a", "held"))
			first := f.sent(t)
			f.store.update(func(s *nativeHandlerStore) { s.key.Group.RateMultiplier = 2 })
			f.keys.InvalidateAuthCacheByKey(t.Context(), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key)
			wsPlanWrite(t, f.client, wsNextCreate("a", "public-b", "queued"))
			wsPlanWrite(t, f.client, wsNextCreate("b", "public-b", "parallel"))
			parallel := f.sent(t)
			require.Contains(t, string(parallel.body), "parallel", "same-lane create bypassed the unfinished predecessor")
			event := wsNextTerminal(t, f.client)
			require.Equal(t, "b", gjson.GetBytes(event, "stream_id").String())
			require.Equal(t, parallel.id, gjson.GetBytes(event, "response.id").String())
			close(f.upstream.release)
			event = wsNextTerminal(t, f.client)
			require.Equal(t, "a", gjson.GetBytes(event, "stream_id").String())
			require.Equal(t, first.id, gjson.GetBytes(event, "response.id").String())
			queued := f.sent(t)
			require.Equal(t, service.UsageAuthorizationModeBPS, queued.mode)
			require.Contains(t, string(queued.body), "queued")
			event = wsNextTerminal(t, f.client)
			require.Equal(t, "a", gjson.GetBytes(event, "stream_id").String())
			require.Equal(t, queued.id, gjson.GetBytes(event, "response.id").String())
			for _, log := range f.assertFinished(t, 3) {
				want := 2.0
				if log.RequestID == first.id {
					want = 1
				}
				require.InDelta(t, want, log.RateMultiplier, 1e-12, "another lane or the next turn must not replace the predecessor's frozen price")
			}
		})
	}
}

func TestOpenAIWSNextExecutionRechecksRevocation(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			f := newWSNextExecutionFixture(t, mode, false)
			wsPlanWrite(t, f.client, wsNextCreate("", "public-a", "first"))
			f.sent(t)
			wsNextTerminal(t, f.client)
			f.store.update(func(s *nativeHandlerStore) { s.key.Status = service.StatusDisabled })
			f.keys.InvalidateAuthCacheByKey(t.Context(), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key)
			wsPlanWrite(t, f.client, wsNextCreate("", "public-b", "independent but revoked"))
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			_, event, err := f.client.Read(ctx)
			cancel()
			var closed coderws.CloseError
			require.ErrorAs(t, err, &closed, "%s", event)
			require.Equal(t, coderws.StatusPolicyViolation, closed.Code)
			require.Equal(t, "websocket authorization is no longer valid", closed.Reason)
			select {
			case c := <-f.done:
				f.done <- c
			case <-time.After(5 * time.Second):
				t.Fatal("revoked handler did not join")
			}
			f.probe.assertReleased(t, 10)
			require.Empty(t, f.upstream.entered, "revoked handoff must not dispatch BPS")
			_, admitted, released, live := f.admission.snapshot()
			require.Equal(t, 2, admitted)
			require.Equal(t, admitted, released)
			require.Zero(t, live)
		})
	}
}

func TestOpenAIWSNextExecutionDoesNotDetachExplicitPredecessor(t *testing.T) {
	f := newWSNextExecutionFixture(t, service.OpenAIWSIngressModeCtxPool, false)
	wsPlanWrite(t, f.client, wsNextCreate("a", "public-a", "first"))
	first := f.sent(t)
	wsNextTerminal(t, f.client)
	wsPlanWrite(t, f.client, fmt.Sprintf(`{"type":"response.create","stream_id":"a","model":"public-b","previous_response_id":%q,"input":[{"role":"user","content":"complete-looking history"}]}`, first.id))
	event := wsPlanRead(t, f.client)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String(), "%s", event)
	require.Equal(t, "a", gjson.GetBytes(event, "stream_id").String())
	// A lane-local continuation rejection must not make the other lane reconnect.
	wsPlanWrite(t, f.client, wsNextCreate("b", "public-b", "truly independent"))
	second := f.sent(t)
	require.Equal(t, service.UsageAuthorizationModeBPS, second.mode)
	require.Contains(t, string(second.body), "truly independent", "the explicit predecessor request must never be stripped and replayed")
	event = wsNextTerminal(t, f.client)
	require.Equal(t, "b", gjson.GetBytes(event, "stream_id").String())
	require.Equal(t, second.id, gjson.GetBytes(event, "response.id").String())
}
