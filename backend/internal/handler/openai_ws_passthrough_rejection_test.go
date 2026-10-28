//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/host"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Keep this selector separate from the preceding 11-case ctx_pool/bridge run.
func TestOpenAIWSPassthroughLocalInputRejectionReadmitsMappedPlan(t *testing.T) {
	probe := &wsHandlerScopeProbe{}
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.beforeDispatchRead = probe.authorityRead
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsPlanRejectedA, ingressMode: service.OpenAIWSIngressModePassthrough,
		apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
		channelMapping:        map[string]string{"public-a": "gpt-5.1", "public-b": "gpt-5.6-sol"},
		accountModelMapping:   map[string]any{"gpt-5.1": "gpt-6-astra", "gpt-5.6-sol": "gpt-6.1-sol"},
		beforeCapacityAcquire: probe.beforeAcquire,
		clientScenario: func(t *testing.T, client *coderws.Conn) [][]byte {
			rejected := wsPlanRead(t, client)
			wsPlanAssertRejection(t, rejected, "plan", "event-a")
			wsPlanWrite(t, client, wsPlanAcceptedB)
			completed := wsPlanRead(t, client)
			require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String(), "no duplicate rejection: %s", completed)
			require.Equal(t, "plan", gjson.GetBytes(completed, "stream_id").String())
			return [][]byte{rejected, completed}
		},
		upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
			scopes := probe.scopes()
			if turn != 1 || len(scopes) != 2 || scopes[0] == scopes[1] {
				return fmt.Errorf("B reused A's owner: sends=%d scopes=%d", turn, len(scopes))
			}
			if err := verifyWSHandlerLiveScope(scopes[1]); err != nil {
				return err
			}
			if gjson.GetBytes(payload, "model").String() != "gpt-6.1-sol" || !strings.Contains(string(payload), "call_plan_b") {
				return fmt.Errorf("passthrough sent the wrong model or lost the original call ID: %s", payload)
			}
			return wsHandlerCompleted(ctx, conn, turn, payload)
		},
	})
	require.Equal(t, 1, got.upstreamAttempts, "invalid A must not dial upstream")
	require.Len(t, got.upstreamPayloads, 1, "only B may be sent")
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(got.upstreamPayloads[0], "model").String())
	require.Len(t, got.logs, 1)
	require.Equal(t, "public-b", got.logs[0].Model)
	probe.assertTurns(t, 2)
}

// Exercise the accepted follow-up inside the relay too: rejection recovery
// re-enters its first-frame path, which alone cannot prove per-frame mapping.
func TestOpenAIWSPassthroughModelMappingForEachAdmittedTurn(t *testing.T) {
	probe := &wsHandlerScopeProbe{}
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.beforeDispatchRead = probe.authorityRead
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:  `{"type":"response.create","model":"public-a","input":"first"}`,
		secondPayload: strings.ReplaceAll(wsPlanAcceptedB, `"stream_id":"plan",`, ""), ingressMode: service.OpenAIWSIngressModePassthrough,
		apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
		channelMapping: map[string]string{"public-a": "gpt-5.1", "public-b": "gpt-5.6-sol"},
		accountModelMapping: map[string]any{
			"gpt-5.1": "gpt-6-astra", "gpt-5.6-sol": "gpt-6.1-sol",
			"gpt-6-astra": "must-not-remap-a", "gpt-6.1-sol": "must-not-remap-b",
		},
		beforeCapacityAcquire: probe.beforeAcquire,
		upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
			expected := []string{"gpt-6-astra", "gpt-6.1-sol"}
			if turn < 1 || turn > len(expected) || gjson.GetBytes(payload, "model").String() != expected[turn-1] {
				return fmt.Errorf("turn %d bypassed or repeated account mapping: %s", turn, payload)
			}
			scopes := probe.scopes()
			if len(scopes) != turn {
				return fmt.Errorf("turn %d has %d admitted scopes", turn, len(scopes))
			}
			if err := verifyWSHandlerLiveScope(scopes[turn-1]); err != nil {
				return err
			}
			return wsHandlerCompleted(ctx, conn, turn, payload)
		},
	})
	require.Equal(t, 1, got.upstreamAttempts, "both admitted turns must traverse the same live relay")
	require.Len(t, got.upstreamPayloads, 2)
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(got.upstreamPayloads[0], "model").String())
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(got.upstreamPayloads[1], "model").String())
	require.Contains(t, string(got.upstreamPayloads[1]), "call_plan_b")
	require.Len(t, got.logs, 2)
	require.Equal(t, "public-a", got.logs[0].Model)
	require.Equal(t, "public-b", got.logs[1].Model)
	probe.assertTurns(t, 2)
}

// This preserves the old direct-service test's same-model recovery assertions
// at the actual request owner, and separately exercises model changes after
// both the first rejection and a rejection inside the running relay filter.
func TestOpenAIWSPassthroughLocalInputRejectionKeepsConnection(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(fmt.Sprintf("different_model=%t", different), func(t *testing.T) {
			probe := &wsHandlerScopeProbe{}
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.beforeDispatchRead = probe.authorityRead
			model, upstreamModel := "public-a", "gpt-6-astra"
			if different {
				model, upstreamModel = "public-b", "gpt-6.1-sol"
			}
			valid := fmt.Sprintf(`{"type":"response.create","stream_id":"plan","model":%q,"input":"original task"}`, model)
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsPlanRejectedA, secondPayload: valid, ingressMode: service.OpenAIWSIngressModePassthrough,
				apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
				channelMapping:        map[string]string{"public-a": "gpt-5.1", "public-b": "gpt-5.6-sol"},
				accountModelMapping:   map[string]any{"gpt-5.1": "gpt-6-astra", "gpt-5.6-sol": "gpt-6.1-sol"},
				beforeCapacityAcquire: probe.beforeAcquire,
				clientScenario: func(t *testing.T, client *coderws.Conn) [][]byte {
					first := wsPlanRead(t, client)
					wsPlanAssertRejection(t, first, "plan", "event-a")
					events := [][]byte{first}
					for turn := 1; turn <= 2; turn++ {
						wsPlanWrite(t, client, valid)
						terminal := wsPlanRead(t, client)
						require.Equal(t, "response.completed", gjson.GetBytes(terminal, "type").String(), "no duplicate error or terminal: %s", terminal)
						require.Equal(t, "plan", gjson.GetBytes(terminal, "stream_id").String())
						require.Equal(t, fmt.Sprintf("resp_usage_e2e_%d", turn), gjson.GetBytes(terminal, "response.id").String())
						events = append(events, terminal)
						if turn == 1 {
							// A remains the rejected model even when the next valid
							// submission is B; no filter-local session model can admit B.
							wsPlanWrite(t, client, `{"type":"response.create","model":"public-a","input":[],"event_id":"bad-followup","stream_id":"plan"}`)
							rejected := wsPlanRead(t, client)
							wsPlanAssertRejection(t, rejected, "plan", "bad-followup")
							events = append(events, rejected)
						}
					}
					return events
				},
			})
			require.Len(t, got.clientEvents, 4)
			require.Len(t, got.upstreamPayloads, 2, "invalid frames must produce zero upstream creates")
			for _, payload := range got.upstreamPayloads {
				require.Equal(t, upstreamModel, gjson.GetBytes(payload, "model").String())
				require.Equal(t, "original task", gjson.GetBytes(payload, "input").String())
			}
			require.Len(t, got.logs, 2, "rejected input must not produce billable results")
			for _, log := range got.logs {
				require.Equal(t, model, log.Model)
				require.Equal(t, 2, log.InputTokens)
				require.Equal(t, 1, log.OutputTokens)
			}
			probe.assertTurns(t, 3)
		})
	}
}

func TestOpenAIWSPassthroughLocalInputRejectionSelectsBPS(t *testing.T) {
	f := newWSPlanRejectionFixture(t, false, true)
	var unexpectedDials atomic.Int32
	trap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		unexpectedDials.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(trap.Close)
	f.store.update(func(s *nativeHandlerStore) {
		a := &s.accounts[0]
		a.Credentials["base_url"] = trap.URL // an erroneous old-plan dial stays local
		a.Extra["openai_apikey_responses_websockets_v2_mode"] = service.OpenAIWSIngressModePassthrough
	})
	wsPlanWrite(t, f.client, wsPlanRejectedA)
	wsPlanAssertRejection(t, wsPlanRead(t, f.client), "plan", "event-a")
	wsPlanWrite(t, f.client, wsPlanAcceptedB)
	terminal := wsPlanRead(t, f.client)
	require.Equal(t, "response.completed", gjson.GetBytes(terminal, "type").String(), "%s", terminal)
	f.finish(t)
	f.probe.assertReleased(t, 10, 11)
	require.Zero(t, unexpectedDials.Load())
	require.Zero(t, f.transport.opened.Load())
	sends := f.upstream.snapshot()
	require.Len(t, sends, 1)
	require.EqualValues(t, 11, sends[0].account)
	require.Equal(t, basispoints.ResponsesURL, sends[0].url)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(sends[0].body, "model").String())
	require.Contains(t, string(sends[0].body), "call_plan_b")
	select {
	case log := <-f.usage.created:
		require.EqualValues(t, 11, log.AccountID)
		require.Equal(t, "public-b", log.Model)
		require.Equal(t, 3, log.InputTokens)
		require.Equal(t, 1, log.OutputTokens)
	case <-time.After(3 * time.Second):
		t.Fatal("BPS B did not record its confirmed usage")
	}
	require.Empty(t, f.usage.created)
}

type wsPassthroughRejectionPeer struct {
	mu       sync.Mutex
	payloads [][]byte
	conns    []*coderws.Conn
	dials    atomic.Int32
	active   sync.WaitGroup
}

func (p *wsPassthroughRejectionPeer) serve(w http.ResponseWriter, r *http.Request) {
	p.active.Add(1)
	defer p.active.Done()
	conn, err := coderws.Accept(w, r, nil)
	if err != nil {
		return
	}
	p.dials.Add(1)
	p.mu.Lock()
	p.conns = append(p.conns, conn)
	p.mu.Unlock()
	defer conn.CloseNow()
	for {
		_, body, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		p.mu.Lock()
		p.payloads = append(p.payloads, append([]byte(nil), body...))
		turn := len(p.payloads)
		p.mu.Unlock()
		if err := wsHandlerCompleted(r.Context(), conn, turn, body); err != nil {
			return
		}
	}
}

func newWSPassthroughRevocationFixture(t *testing.T) (*wsPlanRejectionFixture, *wsPassthroughRejectionPeer) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	peer := &wsPassthroughRejectionPeer{}
	upstreamServer := httptest.NewServer(http.HandlerFunc(peer.serve))
	t.Cleanup(func() {
		upstreamServer.Close()
		peer.mu.Lock()
		for _, conn := range peer.conns {
			_ = conn.CloseNow()
		}
		peer.mu.Unlock()
		done := make(chan struct{})
		go func() { peer.active.Wait(); close(done) }()
		waitWSHandlerFixture(t, done, "passthrough upstream readers")
	})
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20}}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.APIKeyEnabled = true, true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2, cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true, true
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds, cfg.Gateway.OpenAIWS.ReadTimeoutSeconds, cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3, 3, 3
	account := service.Account{ID: 10, Name: "passthrough-revocation", Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		GroupIDs: []int64{nativeHandlerFixtureGroupID},
		Credentials: map[string]any{"api_key": "sk-local-test", "base_url": upstreamServer.URL,
			"model_mapping": map[string]any{"public-a": "gpt-6-astra", "public-b": "gpt-6.1-sol"}},
		Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true,
			"openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModePassthrough}}
	authority := newExcelBPSHandlerAuthority(t, cfg, []service.Account{account}, nativeHandlerFixtureGroupID)
	store := authority.accounts.store
	store.update(func(s *nativeHandlerStore) {
		s.key.User.Concurrency = 1
		s.key.Group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"public-a", "public-b"}}
	})
	probe := &wsPlanRejectionProbe{}
	concurrency := service.NewConcurrencyService(newWSNativeFixtureCapacity(t, nil, probe.beforeAcquire))
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
	journal := &wsNativeFixtureJournal{logSink: usage}
	transport := &nativeHandlerTransport{responder: &wsPlanRejectionUpstream{}}
	t.Cleanup(func() { journal.assertNoLegacyApply(t) })
	gateway := service.NewOpenAIGatewayService(authority.accounts, usage, journal, authority.users, nil, nil, nil, cfg,
		nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, transport, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
	t.Cleanup(gateway.CloseOpenAIWSPool)
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
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.CloseNow()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("passthrough handler failed to join")
		}
		admission.assertClosed(t, true)
	})
	return &wsPlanRejectionFixture{client: client, done: done, store: store, keys: authority.keys,
		transport: transport, probe: probe, usage: usage, journal: journal}, peer
}

func TestOpenAIWSPassthroughLocalInputRejectionRechecksRevocation(t *testing.T) {
	for _, afterSuccessfulTurn := range []bool{false, true} {
		for _, revoke := range []string{"key", "model"} {
			t.Run(fmt.Sprintf("followup=%t/revoke=%s", afterSuccessfulTurn, revoke), func(t *testing.T) {
				f, peer := newWSPassthroughRevocationFixture(t)
				wantSends := 0
				if afterSuccessfulTurn {
					wantSends = 1
					wsPlanWrite(t, f.client, `{"type":"response.create","model":"public-a","input":"seed"}`)
					terminal := wsPlanRead(t, f.client)
					require.Equal(t, "response.completed", gjson.GetBytes(terminal, "type").String(), "%s", terminal)
				}
				wsPlanWrite(t, f.client, strings.ReplaceAll(wsPlanRejectedA, `"stream_id":"plan",`, ""))
				wsPlanAssertRejection(t, wsPlanRead(t, f.client), "", "event-a")
				f.store.update(func(s *nativeHandlerStore) {
					if revoke == "key" {
						s.key.Status = service.StatusDisabled
					} else {
						s.key.Group.ModelAllowlist.Models = []string{"public-a"}
					}
				})
				f.keys.InvalidateAuthCacheByKey(t.Context(), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key)
				reads := f.store.keyReads.Load()
				wsPlanWrite(t, f.client, strings.ReplaceAll(wsPlanAcceptedB, `"stream_id":"plan",`, ""))
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				_, event, err := f.client.Read(ctx)
				cancel()
				require.Error(t, err, "revoked B must not return data: %s", event)
				var closed coderws.CloseError
				require.ErrorAs(t, err, &closed, "EOF/timeout cannot substitute for the expected policy close")
				require.Equal(t, coderws.StatusPolicyViolation, closed.Code)
				if revoke == "key" {
					require.Equal(t, "websocket authorization is no longer valid", closed.Reason)
				} else {
					require.Contains(t, closed.Reason, `Model "public-b" is not available for this group`)
				}
				f.finish(t)
				require.Greater(t, f.store.keyReads.Load(), reads)
				f.probe.assertReleased(t, 10)
				peer.mu.Lock()
				payloads := append([][]byte(nil), peer.payloads...)
				peer.mu.Unlock()
				require.Len(t, payloads, wantSends, "neither rejected input nor revoked B may be sent")
				require.EqualValues(t, wantSends, peer.dials.Load(), "rejected first input must not dial")
				require.Zero(t, f.transport.opened.Load(), "passthrough cannot escape through HTTP")
				if afterSuccessfulTurn {
					require.Equal(t, "gpt-6-astra", gjson.GetBytes(payloads[0], "model").String())
					select {
					case log := <-f.usage.created:
						require.Equal(t, 2, log.InputTokens)
						require.Equal(t, 1, log.OutputTokens)
						f.journal.assertUsageLogs(t, []*service.UsageLog{log})
					case <-time.After(3 * time.Second):
						t.Fatal("the completed seed lost its usage during local rejection")
					}
				}
				require.Empty(t, f.usage.created, "local rejection cannot add usage or settle the prior turn again")
				rows, _, _ := f.journal.counts()
				require.Equal(t, wantSends, rows)
			})
		}
	}
}
