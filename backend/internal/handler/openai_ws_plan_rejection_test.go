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
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const wsPlanRejectedA = `{"type":"response.create","stream_id":"plan","event_id":"event-a","model":"public-a","input":[]}`

// A complete tool round trip is portable, but its call ID must survive both
// Native and BPS preparation under B's newly admitted model and identity.
const wsPlanAcceptedB = `{"type":"response.create","stream_id":"plan","event_id":"event-b","model":"public-b","input":[` +
	`{"type":"message","role":"user","content":[{"type":"input_text","text":"list files"}]},` +
	`{"type":"function_call","name":"shell","call_id":"call_plan_b","arguments":"{}"},` +
	`{"type":"function_call_output","call_id":"call_plan_b","output":"b.txt"}],` +
	`"tools":[{"type":"function","name":"shell","parameters":{"type":"object","properties":{}}}]}`

type wsPlanRejectionAcquisition struct {
	ctx   context.Context
	scope *routing.CapacityScope
	user  bool
}

type wsPlanRejectionProbe struct {
	mu           sync.Mutex
	acquisitions []wsPlanRejectionAcquisition
}

func (p *wsPlanRejectionProbe) beforeAcquire(ctx context.Context, user bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acquisitions = append(p.acquisitions, wsPlanRejectionAcquisition{ctx, routing.CapacityScopeFrom(ctx), user})
}

func (p *wsPlanRejectionProbe) assertReleased(t *testing.T, accounts ...int64) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.acquisitions, 2*len(accounts), "each submission must acquire its own user and account slots exactly once")
	seen := make(map[*routing.CapacityScope]bool)
	for i, accountID := range accounts {
		user, account := p.acquisitions[2*i], p.acquisitions[2*i+1]
		require.True(t, user.user)
		require.False(t, account.user)
		require.True(t, service.NativeHTTPOperationBound(user.ctx), "handler must bind the operation before capacity admission")
		require.NotNil(t, user.scope)
		require.Same(t, user.scope, account.scope)
		require.False(t, seen[user.scope], "a new client submission must not reuse the rejected operation's scope")
		seen[user.scope] = true
		for _, target := range []struct {
			user bool
			id   int64
		}{{true, 100}, {false, accountID}} {
			receipt := user.scope.Lookup(target.user, target.id)
			require.NotNil(t, receipt)
			require.NotNil(t, receipt.Lease, "limited capacity must come from the actual Redis lease implementation")
			_, err := receipt.Verify(t.Context(), receipt.Policy())
			require.Error(t, err, "the request owner must release the original lease")
		}
	}
}

// Native consumes the existing one-shot fixture; only BPS uses legacy Do.
// Both feed the same recorder, so an old-model or wrong-mode send is visible.
type wsPlanRejectionTransport struct {
	*nativeHandlerTransport
	bps service.HTTPUpstream
}

func (u *wsPlanRejectionTransport) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	return u.bps.Do(req, proxy, id, concurrency)
}

type wsPlanRejectionSend struct {
	account int64
	url     string
	body    []byte
	scope   *routing.CapacityScope
}

type wsPlanRejectionUpstream struct {
	service.HTTPUpstream
	mu    sync.Mutex
	sends []wsPlanRejectionSend
}

func (u *wsPlanRejectionUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	scope := routing.CapacityScopeFrom(req.Context())
	u.mu.Lock()
	u.sends = append(u.sends, wsPlanRejectionSend{accountID, req.URL.String(), body, scope})
	u.mu.Unlock()
	if scope == nil || !service.NativeHTTPOperationBound(req.Context()) {
		return nil, fmt.Errorf("actual upstream send has no handler operation/capacity scope")
	}
	for _, target := range []struct {
		user bool
		id   int64
	}{{true, 100}, {false, accountID}} {
		receipt := scope.Lookup(target.user, target.id)
		if receipt == nil || receipt.Lease == nil || receipt.Unlimited {
			return nil, fmt.Errorf("actual send lacks a limited lease for user=%t id=%d", target.user, target.id)
		}
		if _, err := receipt.Verify(req.Context(), receipt.Policy()); err != nil {
			return nil, err
		}
	}
	wire := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_plan_b\",\"status\":\"completed\",\"model\":%q,\"output\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":1}}}\n\n", gjson.GetBytes(body, "model").String())
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
}

func (u *wsPlanRejectionUpstream) snapshot() []wsPlanRejectionSend {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]wsPlanRejectionSend(nil), u.sends...)
}

type wsPlanRejectionFixture struct {
	client    *coderws.Conn
	done      chan *gin.Context
	store     *nativeHandlerStore
	keys      *service.APIKeyService
	upstream  *wsPlanRejectionUpstream
	transport *nativeHandlerTransport
	probe     *wsPlanRejectionProbe
	usage     *openAIWSUsageHandlerUsageLogRepoStub
	journal   *wsNativeFixtureJournal
}

func newWSPlanRejectionFixture(t *testing.T, firstBPS, nextBPS bool) *wsPlanRejectionFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, OpenAITurnMaxAttempts: 1}}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
	var accounts []service.Account
	for i, bps := range []bool{firstBPS, nextBPS} {
		channelModel, upstreamModel := "channel-a", "gpt-6-astra"
		if i == 1 {
			channelModel, upstreamModel = "channel-b", "gpt-6.1-sol"
		}
		account := service.Account{ID: int64(10 + i), Name: channelModel, Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
			GroupIDs:    []int64{nativeHandlerFixtureGroupID},
			Credentials: map[string]any{"api_key": "sk-plan-fixture", "model_mapping": map[string]any{channelModel: upstreamModel}},
			Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true,
				"openai_apikey_responses_websockets_v2_mode": service.OpenAIWSIngressModeHTTPBridge}}
		if bps {
			account.Type = service.AccountTypeOAuth
			account.Credentials["client_id"] = openai.ExcelClientID
			account.Credentials["access_token"] = fmt.Sprintf("excel-plan-%d", account.ID)
			account.Credentials["chatgpt_account_id"] = fmt.Sprintf("workspace-plan-%d", account.ID)
			account.Extra["openai_excel_bps"] = true
			account.Extra["openai_excel_bps_models"] = []string{upstreamModel}
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = service.OpenAIWSIngressModeHTTPBridge
		}
		accounts = append(accounts, account)
	}
	authority := newExcelBPSHandlerAuthority(t, cfg, accounts, nativeHandlerFixtureGroupID)
	store := authority.accounts.store
	store.update(func(s *nativeHandlerStore) {
		s.key.User.Concurrency = 1
		s.key.Group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"public-a", "public-b"}}
	})
	probe := &wsPlanRejectionProbe{}
	concurrency := service.NewConcurrencyService(newWSNativeFixtureCapacity(t, nil, probe.beforeAcquire))
	upstream := &wsPlanRejectionUpstream{}
	transport := &nativeHandlerTransport{responder: upstream}
	usage := &openAIWSUsageHandlerUsageLogRepoStub{created: make(chan *service.UsageLog, 4)}
	journal := &wsNativeFixtureJournal{logSink: usage}
	t.Cleanup(func() { journal.assertNoLegacyApply(t) })
	channels := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
		channels: []service.Channel{{ID: 7702, Name: "plan-rejection", Status: service.StatusActive,
			GroupIDs: []int64{nativeHandlerFixtureGroupID}, BillingModelSource: service.BillingModelSourceUpstream,
			ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"public-a": "channel-a", "public-b": "channel-b"}}}},
		groupPlatforms: map[int64]string{nativeHandlerFixtureGroupID: service.PlatformOpenAI},
	}, nil, nil, nil, nil)
	settings := service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyExcelBPSEnabled: "true"}}, cfg)
	gateway := service.NewOpenAIGatewayService(authority.accounts, usage, journal, authority.users, nil, nil, nil, cfg,
		nil, concurrency, service.NewBillingService(cfg, nil), nil, nil,
		&wsPlanRejectionTransport{nativeHandlerTransport: transport, bps: upstream}, &service.DeferredService{},
		nil, nil, nil, channels, nil, settings, nil)
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
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", &coderws.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key}},
	})
	require.NoError(t, err, "real authentication middleware and WS upgrade must succeed")
	f := &wsPlanRejectionFixture{client, done, store, authority.keys, upstream, transport, probe, usage, journal}
	t.Cleanup(func() {
		_ = client.CloseNow()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("handler failed to join after client close")
		}
		admission.assertClosed(t, true)
	})
	return f
}

func wsPlanWrite(t *testing.T, client *coderws.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
}

func wsPlanRead(t *testing.T, client *coderws.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	kind, event, err := client.Read(ctx)
	require.NoError(t, err, "EOF, close and timeout cannot stand in for the expected WS event")
	require.Equal(t, coderws.MessageText, kind)
	return event
}

func wsPlanAssertRejection(t *testing.T, event []byte, streamID, eventID string) {
	t.Helper()
	require.Equal(t, "error", gjson.GetBytes(event, "type").String(), "%s", event)
	require.Equal(t, "missing_required_parameter", gjson.GetBytes(event, "error.code").String())
	require.Equal(t, "invalid_request_error", gjson.GetBytes(event, "error.type").String())
	require.Equal(t, "input", gjson.GetBytes(event, "error.param").String())
	require.Equal(t, streamID, gjson.GetBytes(event, "stream_id").String())
	require.Equal(t, eventID, gjson.GetBytes(event, "event_id").String())
}

func (f *wsPlanRejectionFixture) finish(t *testing.T) *gin.Context {
	t.Helper()
	_ = f.client.CloseNow()
	select {
	case c := <-f.done:
		// Cleanup also waits for the joined handler, including on assertion failure.
		f.done <- c
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish its operation")
		return nil
	}
}

func TestOpenAIWSLocalInputRejectionReadmitsMappedPlan(t *testing.T) {
	for _, firstBPS := range []bool{false, true} {
		for _, nextBPS := range []bool{false, true} {
			t.Run(fmt.Sprintf("first_bps=%t/next_bps=%t", firstBPS, nextBPS), func(t *testing.T) {
				f := newWSPlanRejectionFixture(t, firstBPS, nextBPS)
				wsPlanWrite(t, f.client, wsPlanRejectedA)
				wsPlanAssertRejection(t, wsPlanRead(t, f.client), "plan", "event-a")
				require.Empty(t, f.upstream.snapshot(), "invalid A must dispatch neither its own nor another model")
				require.Empty(t, f.usage.created, "local rejection must not create zero usage")
				reads := f.store.keyReads.Load()
				wsPlanWrite(t, f.client, wsPlanAcceptedB)
				terminal := wsPlanRead(t, f.client)
				require.Equal(t, "response.completed", gjson.GetBytes(terminal, "type").String(), "duplicate error or stale-plan dispatch: %s", terminal)
				require.Equal(t, "plan", gjson.GetBytes(terminal, "stream_id").String())
				require.Equal(t, "resp_plan_b", gjson.GetBytes(terminal, "response.id").String())
				c := f.finish(t)
				require.Greater(t, f.store.keyReads.Load(), reads, "B must cross live authorization again")
				f.probe.assertReleased(t, 10, 11)
				sends := f.upstream.snapshot()
				require.Len(t, sends, 1, "the rejected A must never be sent or replayed")
				require.EqualValues(t, 11, sends[0].account)
				require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(sends[0].body, "model").String(), "apply B's channel and account mappings exactly once")
				require.Contains(t, string(sends[0].body), "call_plan_b", "preserve the client's tool call identity")
				require.Same(t, f.probe.acquisitions[2].scope, sends[0].scope, "send must use B's new operation")
				if nextBPS {
					require.Equal(t, basispoints.ResponsesURL, sends[0].url)
					require.Zero(t, f.transport.opened.Load(), "BPS B cannot leak into Native")
				} else {
					require.Equal(t, "https://api.openai.com/v1/responses", sends[0].url)
					require.EqualValues(t, 1, f.transport.opened.Load())
				}
				require.Zero(t, f.transport.legacy.Load())
				select {
				case log := <-f.usage.created:
					require.EqualValues(t, 11, log.AccountID)
					require.Equal(t, "public-b", log.Model)
					require.Equal(t, 3, log.InputTokens)
					require.Equal(t, 1, log.OutputTokens)
					if !nextBPS {
						f.journal.assertUsageLogs(t, []*service.UsageLog{log})
					}
				case <-time.After(3 * time.Second):
					t.Fatal("B's confirmed usage was not recorded")
				}
				require.Empty(t, f.usage.created, "no usage for A and no duplicate settlement for B")
				failures := service.GetOpsStreamErrors(c)
				require.Len(t, failures, 1)
				require.Equal(t, "missing_required_parameter", failures[0].Code)
				require.Equal(t, http.StatusBadRequest, failures[0].IntendedStatus)
				require.True(t, failures[0].RequestScoped)
				require.False(t, failures[0].CountTowardsSLA, "local input refusal must not become upstream health failure")
			})
		}
	}
}

func TestOpenAIWSLocalInputRejectionRechecksRevocation(t *testing.T) {
	for _, firstBPS := range []bool{false, true} {
		for _, revoke := range []string{"key", "model"} {
			t.Run(fmt.Sprintf("first_bps=%t/revoke=%s", firstBPS, revoke), func(t *testing.T) {
				f := newWSPlanRejectionFixture(t, firstBPS, !firstBPS)
				// The unnamed stream uses the connection policy-close contract;
				// the successful matrix above checks named error envelopes.
				wsPlanWrite(t, f.client, strings.ReplaceAll(wsPlanRejectedA, `"stream_id":"plan",`, ""))
				wsPlanAssertRejection(t, wsPlanRead(t, f.client), "", "event-a")
				f.store.update(func(s *nativeHandlerStore) {
					if revoke == "key" {
						s.key.Status = service.StatusDisabled
					} else {
						// A stays allowed, exposing admission with the stale model.
						s.key.Group.ModelAllowlist.Models = []string{"public-a"}
					}
				})
				f.keys.InvalidateAuthCacheByKey(t.Context(), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID).Key)
				reads := f.store.keyReads.Load()
				wsPlanWrite(t, f.client, strings.ReplaceAll(wsPlanAcceptedB, `"stream_id":"plan",`, ""))
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				_, event, err := f.client.Read(ctx)
				cancel()
				require.Error(t, err, "revocation must close the connection, not emit another response: %s", event)
				var closed coderws.CloseError
				require.ErrorAs(t, err, &closed, "timeout/EOF cannot substitute for a policy close")
				require.Equal(t, coderws.StatusPolicyViolation, closed.Code)
				if revoke == "key" {
					require.Equal(t, "websocket authorization is no longer valid", closed.Reason)
				} else {
					require.Contains(t, closed.Reason, `Model "public-b" is not available for this group`)
				}
				f.finish(t)
				require.Greater(t, f.store.keyReads.Load(), reads)
				f.probe.assertReleased(t, 10)
				require.Empty(t, f.upstream.snapshot(), "neither rejected A nor revoked B may reach either provider mode")
				require.Zero(t, f.transport.opened.Load())
				require.Empty(t, f.usage.created)
				rows, _, _ := f.journal.counts()
				require.Zero(t, rows, "rejections must not fabricate a Native billing record")
			})
		}
	}
}

func TestOpenAIWSLocalInputRejectionCtxPoolUsesNewOperation(t *testing.T) {
	probe := &wsHandlerScopeProbe{}
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.beforeDispatchRead = probe.authorityRead
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: strings.ReplaceAll(wsPlanRejectedA, `"stream_id":"plan",`, ""),
		ingressMode:  service.OpenAIWSIngressModeCtxPool, apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
		channelMapping:        map[string]string{"public-a": "gpt-5.1", "public-b": "gpt-5.6-sol"},
		accountModelMapping:   map[string]any{"gpt-5.1": "gpt-6-astra", "gpt-5.6-sol": "gpt-6.1-sol"},
		beforeCapacityAcquire: probe.beforeAcquire,
		clientScenario: func(t *testing.T, client *coderws.Conn) [][]byte {
			rejected := wsPlanRead(t, client)
			wsPlanAssertRejection(t, rejected, "", "event-a")
			wsPlanWrite(t, client, strings.ReplaceAll(wsPlanAcceptedB, `"stream_id":"plan",`, ""))
			completed := wsPlanRead(t, client)
			require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String(), "no duplicate local error: %s", completed)
			return [][]byte{rejected, completed}
		},
		upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
			scopes := probe.scopes()
			if turn != 1 || len(scopes) != 2 || scopes[0] == scopes[1] {
				return fmt.Errorf("create=%d handler scopes=%d: B reused rejected A's operation", turn, len(scopes))
			}
			if err := verifyWSHandlerLiveScope(scopes[1]); err != nil {
				return err
			}
			if gjson.GetBytes(payload, "model").String() != "gpt-6.1-sol" || !strings.Contains(string(payload), "call_plan_b") {
				return fmt.Errorf("B lost its model mapping or original tool call identity: %s", payload)
			}
			return wsHandlerCompleted(ctx, conn, turn, payload)
		},
	})
	require.Equal(t, 1, got.upstreamAttempts, "invalid A must not even dial a Native upstream")
	require.Len(t, got.upstreamPayloads, 1)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(got.upstreamPayloads[0], "model").String())
	require.Len(t, got.logs, 1)
	require.Equal(t, "public-b", got.logs[0].Model)
	probe.assertTurns(t, 2)
}

// Migrated from the direct-service test: ctx_pool and HTTP bridge now return
// local rejection to this handler owner. Preserve both first/follow-up errors,
// connection reuse, envelope correlation, exact successful sends and usage.
func TestOpenAIWSLocalInputRejectionKeepsConnection(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsPlanRejectedA, secondPayload: wsPlanAcceptedB, ingressMode: mode,
				channelMapping:      map[string]string{"public-a": "gpt-5.1", "public-b": "gpt-5.6-sol"},
				accountModelMapping: map[string]any{"gpt-5.1": "gpt-6-astra", "gpt-5.6-sol": "gpt-6.1-sol"},
				clientScenario: func(t *testing.T, client *coderws.Conn) [][]byte {
					first := wsPlanRead(t, client)
					wsPlanAssertRejection(t, first, "plan", "event-a")
					events := [][]byte{first}
					for turn := 1; turn <= 2; turn++ {
						wsPlanWrite(t, client, wsPlanAcceptedB)
						terminal := wsPlanRead(t, client)
						require.Equal(t, "response.completed", gjson.GetBytes(terminal, "type").String(), "no duplicate error/terminal: %s", terminal)
						require.Equal(t, "plan", gjson.GetBytes(terminal, "stream_id").String())
						require.Equal(t, fmt.Sprintf("resp_usage_e2e_%d", turn), gjson.GetBytes(terminal, "response.id").String())
						events = append(events, terminal)
						if turn == 1 {
							wsPlanWrite(t, client, `{"type":"response.create","stream_id":"plan","event_id":"bad-followup","model":"public-b","input":[]}`)
							rejected := wsPlanRead(t, client)
							wsPlanAssertRejection(t, rejected, "plan", "bad-followup")
							events = append(events, rejected)
						}
					}
					return events
				},
			})
			require.Len(t, got.clientEvents, 4)
			require.Len(t, got.upstreamPayloads, 2, "invalid first/follow-up frames emit no upstream messages")
			for _, payload := range got.upstreamPayloads {
				require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(payload, "model").String())
				require.Contains(t, string(payload), "call_plan_b")
			}
			require.Len(t, got.logs, 2, "only the two successful turns have billable results")
			for _, log := range got.logs {
				require.Equal(t, "public-b", log.Model)
				require.Equal(t, 2, log.InputTokens)
				require.Equal(t, 1, log.OutputTokens)
			}
		})
	}
}
