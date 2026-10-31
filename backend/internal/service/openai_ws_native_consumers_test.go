package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type nativeWSConcurrentAccounts struct {
	*nativeAuthorityAccounts
	mu sync.Mutex
}

func (r *nativeWSConcurrentAccounts) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nativeAuthorityAccounts.GetByID(ctx, id)
}
func (r *nativeWSConcurrentAccounts) ValidateVendorSupply(ctx context.Context, a *Account, g *int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nativeAuthorityAccounts.ValidateVendorSupply(ctx, a, g)
}
func (r *nativeWSConcurrentAccounts) ValidateVendorAuthorizationMode(ctx context.Context, a *Account, g *int64, m string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nativeAuthorityAccounts.ValidateVendorAuthorizationMode(ctx, a, g, m)
}

type nativeWSConcurrentKeys struct {
	*nativeAuthorityKeys
	mu sync.Mutex
}

func (r *nativeWSConcurrentKeys) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nativeAuthorityKeys.GetByID(ctx, id)
}

type nativeWSConcurrentCache struct {
	stubGatewayCache
	mu sync.Mutex
}

func (c *nativeWSConcurrentCache) GetSessionAccountID(ctx context.Context, g int64, s string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stubGatewayCache.GetSessionAccountID(ctx, g, s)
}
func (c *nativeWSConcurrentCache) SetSessionAccountID(ctx context.Context, g int64, s string, a int64, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stubGatewayCache.SetSessionAccountID(ctx, g, s, a, ttl)
}
func (c *nativeWSConcurrentCache) CompareAndRefreshSessionAccountID(ctx context.Context, g int64, s string, a int64, ttl time.Duration, create bool) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stubGatewayCache.CompareAndRefreshSessionAccountID(ctx, g, s, a, ttl, create)
}
func (c *nativeWSConcurrentCache) DeleteSessionAccountID(ctx context.Context, g int64, s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stubGatewayCache.DeleteSessionAccountID(ctx, g, s)
}

// The fixture owns actual in-memory tokens/deadlines behind routing's normal
// receipt/lease implementation. Missing/released tokens cannot be renewed.
type nativeWSCapacityStore struct {
	mu     sync.Mutex
	tokens map[routing.CapacityRequest]time.Time
}

func (s *nativeWSCapacityStore) acquire(ctx context.Context, id int64, limit int, token string, user bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for key, expiry := range s.tokens {
		if key.ID == id && key.User == user && time.Now().Before(expiry) {
			count++
		}
	}
	if count >= limit {
		return false, nil
	}
	if s.tokens == nil {
		s.tokens = make(map[routing.CapacityRequest]time.Time)
	}
	s.tokens[routing.CapacityRequest{ID: id, Token: token, User: user}] = time.Now().Add(30 * time.Second)
	return true, nil
}
func (s *nativeWSCapacityStore) release(id int64, token string, user bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, routing.CapacityRequest{ID: id, Token: token, User: user})
	return nil
}
func (s *nativeWSCapacityStore) AcquireAccountSlot(c context.Context, id int64, n int, k string) (bool, error) {
	return s.acquire(c, id, n, k, false)
}
func (s *nativeWSCapacityStore) AcquireUserSlot(c context.Context, id int64, n int, k string) (bool, error) {
	return s.acquire(c, id, n, k, true)
}
func (s *nativeWSCapacityStore) ReleaseAccountSlot(_ context.Context, id int64, k string) error {
	return s.release(id, k, false)
}
func (s *nativeWSCapacityStore) ReleaseUserSlot(_ context.Context, id int64, k string) error {
	return s.release(id, k, true)
}
func (s *nativeWSCapacityStore) CheckCapacityLease(ctx context.Context, r routing.CapacityRequest) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Limit = 0
	expiry, ok := s.tokens[r]
	if !ok {
		return 0, nil
	}
	return max(time.Until(expiry), 0), nil
}
func (s *nativeWSCapacityStore) RefreshCapacityLease(ctx context.Context, r routing.CapacityRequest) (time.Duration, error) {
	return s.CheckCapacityLease(ctx, r)
}

type nativeWSConsumerFixture struct {
	*nativeOperationFixture
	capacity *nativeWSCapacityStore
}

// These service-level peers bypass the handler. Bind the same explicit
// pricing journal port to each new logical root without changing its permits.
func bindNativeWSConsumerPricing(t *testing.T, f *nativeWSConsumerFixture, ctx context.Context) context.Context {
	t.Helper()
	fixture := *f.nativeOperationFixture
	fixture.ctx, fixture.op = ctx, nativeDispatchFromContext(ctx)
	if nativeHTTPFromContext(ctx).intent == nil {
		bindNativeRuntimeFixture(t, &fixture)
	}
	return fixture.ctx
}

func nativeWSConsumerService(t *testing.T, mode, endpoint string) *nativeWSConsumerFixture {
	t.Helper()
	f := &nativeWSConsumerFixture{nativeOperationFixture: newNativeOperationFixture(t, false, 3), capacity: &nativeWSCapacityStore{}}
	f.resource.Concurrency = 2
	s := f.service
	cfg := s.cfg
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = mode
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 2
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	f.resource.Credentials["base_url"] = endpoint
	f.resource.Extra = map[string]any{"responses_websockets_v2_enabled": true, "openai_apikey_responses_websockets_v2_mode": mode}
	s.cache = &nativeWSConcurrentCache{}
	s.accountRepo = &nativeWSConcurrentAccounts{nativeAuthorityAccounts: f.accounts}
	s.openaiWSResolver = NewOpenAIWSProtocolResolver(cfg)
	s.toolCorrector = NewCodexToolCorrector()
	t.Cleanup(func() {
		if s.openaiWSPool != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, s.openaiWSPool.Stop(ctx))
		}
	})
	return f
}

func TestNativeWSConsumerInternalPrewarmChargesPreparationOnly(t *testing.T) {
	frames := make(chan []byte, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/actual-native" {
			http.Redirect(w, r, "/actual-native", http.StatusTemporaryRedirect)
			return
		}
		c, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			_, body, err := c.Read(r.Context())
			if err != nil {
				return
			}
			frames <- body
			if err = c.Write(r.Context(), coderws.MessageText, []byte("{\"type\":\"response.completed\",\"response\":{\"id\":\"resp_prep\",\"output\":[]}}")); err != nil {
				return
			}
		}
	}))
	t.Cleanup(upstream.Close)
	f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, upstream.URL)
	s := f.service
	user, err := routing.AcquireCapacity(f.ctx, f.capacity, routing.CapacityRequest{ID: f.key.UserID, User: true, Limit: 0}, nil)
	require.NoError(t, err)
	defer user.ReleaseFunc()
	account, err := routing.AcquireCapacity(f.ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: "prewarm-owned-slot", Limit: 2}, nil)
	require.NoError(t, err)
	require.True(t, account.Acquired)
	defer account.ReleaseFunc()
	s.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	endpoint, err := s.buildOpenAIResponsesWSURL(f.resource)
	require.NoError(t, err)
	headers := http.Header{"Authorization": []string{"Bearer synthetic-upstream-key"}}
	lease, err := s.getOpenAIWSConnPool().Acquire(f.ctx, openAIWSAcquireRequest{Account: f.resource, WSURL: endpoint, Headers: headers, HandshakeFactsFactory: func(ctx context.Context, h http.Header) (nativeWSHandshake, error) {
		return s.snapshotNativeWSHandshake(ctx, f.resource, endpoint, "", h)
	}})
	require.NoError(t, err)
	defer lease.Release()
	require.True(t, strings.HasSuffix(lease.conn.nativeHandshake.target.URL, "/actual-native"), "seal the successful upgrade's target, not its pre-redirect URL")
	require.Nil(t, lease.conn.ws.(*coderOpenAIWSClientConn).handshakeRequestHeaders, "do not retain an extra plaintext credential copy after sealing")
	payload := map[string]any{"type": "response.create", "model": "client-model", "input": "warm"}
	err = s.performOpenAIWSGeneratePrewarm(f.ctx, lease, OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, payload, "", payload, f.resource, nil, 9, nil)
	require.NoError(t, err)
	require.Equal(t, gjson.False, gjson.GetBytes(wsLifecycleReceive(t, frames), "generate").Type)
	snapshot := f.op.budget.attemptBudget.Snapshot()
	require.Zero(t, snapshot.Attempts)
	require.Equal(t, 1, snapshot.PreparationRetries)
	facts := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Len(t, facts.Attempts, 1)
	require.Equal(t, model.DispatchInternalPreparation, facts.Attempts[0].Identity.Purpose)
	require.Equal(t, model.SendSent, facts.Attempts[0].Send)
	require.Equal(t, model.TerminalCompleted, facts.Attempts[0].Terminal)
	require.Nil(t, facts.Attempts[0].Usage, "prewarm does not invent generation usage")
	require.True(t, facts.Attempts[0].Closed)
	// The existing once-per-held-connection guard does not issue another permit.
	require.NoError(t, s.performOpenAIWSGeneratePrewarm(f.ctx, lease, OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, payload, "", payload, f.resource, nil, 9, nil))
	require.Empty(t, frames)
	clientFalse := []byte("{\"type\":\"response.create\",\"model\":\"client-model\",\"generate\":false}")
	require.NoError(t, s.sendNativeWS(f.ctx, f.resource, lease, coderws.MessageText, clientFalse))
	wsLifecycleReceive(t, frames)
	snapshot = f.op.budget.attemptBudget.Snapshot()
	require.Equal(t, 1, snapshot.Attempts)
	require.Equal(t, 1, snapshot.PreparationRetries)
	require.Equal(t, "http", f.op.request.Transport)
}

// Real local WS peers exercise the production multiplexer, ctx_pool/passthrough
// consumer, immutable provider and physical writer. Persistent billing and real
// remote OpenAI acceptance remain outside this isolated protocol fixture.
func TestNativeWSConsumerNamedFIFOAndToolFollowup(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			observed := make(chan []byte, 8)
			releaseFirst := make(chan struct{})
			var releaseOnce sync.Once
			releaseFirstTurn := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
			requestTag := func(body []byte) string {
				if tag := gjson.GetBytes(body, "event_id").String(); tag != "" {
					return tag
				}
				// HTTP carries the expanded input, not the client's WS envelope.
				tag := ""
				for _, item := range gjson.GetBytes(body, "input").Array() {
					if item.Get("type").String() == "function_call_output" && item.Get("call_id").String() == "call_a1" {
						return "a2"
					}
					if item.Get("role").String() == "user" {
						content := item.Get("content")
						if content.Type == gjson.String {
							tag = content.String()
						} else if content.IsArray() {
							tag = content.Get("0.text").String()
						}
					}
				}
				return tag
			}
			responseFor := func(ctx context.Context, body []byte) []byte {
				observed <- body
				tag := requestTag(body)
				if tag == "a1" {
					select {
					case <-releaseFirst:
					case <-ctx.Done():
						return nil
					}
				}
				output := []any{}
				if tag == "a1" {
					output = []any{map[string]any{"type": "function_call", "id": "fc_a1", "call_id": "call_a1", "name": "lookup", "arguments": "{}", "status": "completed"}}
				}
				response, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_" + tag, "status": "completed", "model": "mapped-provider-model", "output": output, "usage": map[string]int{"input_tokens": 2, "output_tokens": 1}}})
				return response
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == OpenAIWSIngressModeHTTPBridge {
					body, err := io.ReadAll(r.Body)
					if err != nil {
						return
					}
					response := responseFor(r.Context(), body)
					if response != nil {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: %s\n\n", response)
					}
					return
				}
				c, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				for {
					_, body, err := c.Read(r.Context())
					if err != nil {
						return
					}
					response := responseFor(r.Context(), body)
					if response == nil || c.Write(r.Context(), coderws.MessageText, response) != nil {
						return
					}
				}
			}))
			t.Cleanup(upstream.Close)
			f := nativeWSConsumerService(t, mode, upstream.URL)
			if mode == OpenAIWSIngressModeHTTPBridge {
				transport := &http.Transport{}
				t.Cleanup(transport.CloseIdleConnections)
				f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
					return transport.RoundTrip(request)
				}
			}
			s := f.service
			keys := &APIKeyService{apiKeyRepo: &nativeWSConcurrentKeys{nativeAuthorityKeys: f.keys}}
			finished := make(chan string, 8)
			failures := make(chan error, 8)
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				socket, err := coderws.Accept(w, r, nil)
				if err != nil {
					failures <- err
					return
				}
				defer socket.CloseNow()
				parent := openAIWSTestAdmissionContext(r.Context())
				err = RunOpenAIWSMultiplexer(parent, socket, s.cfg, func(lane context.Context, ds *OpenAIWSDownstream) {
					defer ds.CloseNow()
					_, first, err := ds.Read(lane)
					if err != nil {
						failures <- err
						return
					}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = r.Clone(lane)
					c.Set("api_key", f.key)
					var current context.Context
					var releases []func()
					turn := 0
					release := func() {
						for _, end := range releases {
							end()
						}
						releases = nil
					}
					defer release()
					bind := func(payload []byte) error {
						previous := nativeHTTPFromContext(current)
						if previous != nil && previous.core.Snapshot().State != model.OperationCompleted {
							return errors.New("new turn began before the previous root finished")
						}
						turn++
						base := ds.TurnBudgetContext(lane, s.cfg, payload)
						request := f.op.request
						request.Transport = "ws"
						request.OperationID = fmt.Sprintf("%s-%d", gjson.GetBytes(first, "stream_id").String(), turn)
						request.APIKeyDigest = sha256.Sum256([]byte(f.key.Key))
						current, err = s.BindNativeWSDispatch(base, keys, request)
						if err != nil {
							return err
						}
						current, err = s.BindNativeWSOperation(current, payload)
						if err != nil {
							return err
						}
						current = bindNativeWSConsumerPricing(t, f, current)
						if previous != nil && nativeHTTPFromContext(current) == previous {
							return errors.New("new turn reused the retired operation")
						}
						if err = ds.PublishNativeWSTurn(lane, current); err != nil {
							return err
						}
						c.Request = c.Request.WithContext(current)
						return nil
					}
					acquire := func() error {
						release()
						user, err := routing.AcquireCapacity(current, nil, routing.CapacityRequest{ID: f.key.UserID, User: true, Limit: 0}, nil)
						if err != nil {
							return err
						}
						releases = append(releases, user.ReleaseFunc)
						account, err := routing.AcquireCapacity(current, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: nativeDispatchFromContext(current).request.OperationID, Limit: f.resource.Concurrency}, nil)
						if err != nil {
							return err
						}
						if !account.Acquired {
							return errors.New("fixture account capacity unavailable")
						}
						releases = append(releases, account.ReleaseFunc)
						return nil
					}
					if err = bind(first); err != nil {
						failures <- err
						return
					}
					if err = acquire(); err != nil {
						failures <- err
						return
					}
					var finalizedResponseID string
					hooks := &OpenAIWSIngressHooks{BeforeRequest: func(_ int, payload []byte, _ string) error { return bind(payload) }, BeforeTurn: func(n int) error {
						if n == 1 {
							return nil
						}
						return acquire()
					}, AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) {
						if cause != nil {
							failures <- cause
							return
						}
						facts := nativeHTTPFromContext(current).core.Snapshot()
						if result == nil || len(facts.Attempts) != 1 || facts.Attempts[0].Terminal != model.TerminalCompleted || facts.Attempts[0].ResponseID != result.ResponseID || facts.Attempts[0].Usage == nil {
							failures <- fmt.Errorf("turn facts missing or crossed lanes: %+v", facts)
							return
						}
						snapshot := nativeDispatchFromContext(current).budget.attemptBudget.Snapshot()
						if snapshot.Attempts != 1 || snapshot.PreparationRetries != 0 {
							failures <- fmt.Errorf("turn debited incorrectly: %+v", snapshot)
							return
						}
						if result == nil {
							failures <- errors.New("missing turn result")
							return
						}
						input := &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}
						if _, err := finalizeNativeWSUsage(current, input, cause, nativeWSFixtureJournal(), time.Second); err != nil {
							failures <- err
							return
						}
						finalizedResponseID = result.ResponseID
					}, ReleaseTurn: func(int) {
						facts := nativeHTTPFromContext(current).core.Snapshot()
						if facts.State != model.OperationCompleted || !facts.Billing.Durable {
							failures <- fmt.Errorf("release preceded completion: %+v", facts)
							return
						}
						release()
						finished <- finalizedResponseID
					}}
					err = s.ProxyResponsesWebSocketFromClient(current, c, ds, f.resource, "synthetic-upstream-key", first, hooks)
					if err != nil && !errors.Is(err, context.Canceled) && coderws.CloseStatus(err) < 0 {
						failures <- err
					}
				})
				if err != nil && !errors.Is(err, context.Canceled) && coderws.CloseStatus(err) < 0 {
					failures <- err
				}
			}))
			t.Cleanup(downstream.Close)
			t.Cleanup(releaseFirstTurn)
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http"), nil)
			require.NoError(t, err)
			defer client.CloseNow()
			write := func(body string) { require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(body))) }
			write("{\"type\":\"response.create\",\"model\":\"client-model\",\"stream_id\":\"a\",\"event_id\":\"a1\",\"store\":false,\"input\":[{\"role\":\"user\",\"content\":\"a1\"}],\"tools\":[{\"type\":\"function\",\"name\":\"lookup\",\"parameters\":{\"type\":\"object\"}}]}")
			write("{\"type\":\"response.create\",\"model\":\"client-model\",\"stream_id\":\"a\",\"event_id\":\"a2\",\"store\":false,\"previous_response_id\":\"resp_a1\",\"input\":[{\"type\":\"function_call_output\",\"call_id\":\"call_a1\",\"output\":\"tool result\"}]}")
			write("{\"type\":\"response.create\",\"model\":\"client-model\",\"stream_id\":\"b\",\"event_id\":\"b1\",\"store\":false,\"input\":[{\"role\":\"user\",\"content\":\"b1\"}]}")
			firstTwo := make([][]byte, 0, 2)
			for len(firstTwo) < 2 {
				select {
				case body := <-observed:
					firstTwo = append(firstTwo, body)
				case err := <-failures:
					t.Fatal(err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			tags := []string{requestTag(firstTwo[0]), requestTag(firstTwo[1])}
			require.ElementsMatch(t, []string{"a1", "b1"}, tags)
			for _, body := range firstTwo {
				require.False(t, gjson.GetBytes(body, "stream_id").Exists())
			}
			releaseFirstTurn()
			terminals := []string{}
			for len(terminals) < 3 {
				_, body, err := client.Read(ctx)
				require.NoError(t, err)
				if gjson.GetBytes(body, "type").String() == "response.completed" {
					terminals = append(terminals, gjson.GetBytes(body, "response.id").String())
				}
			}
			require.ElementsMatch(t, []string{"resp_a1", "resp_b1", "resp_a2"}, terminals)
			followup := wsLifecycleReceive(t, observed)
			require.Equal(t, "a2", requestTag(followup))
			require.Contains(t, string(followup), "call_a1")
			require.Contains(t, string(followup), "function_call_output")
			for range 3 {
				select {
				case <-finished:
				case err := <-failures:
					t.Fatal(err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			select {
			case err := <-failures:
				t.Fatal(err)
			default:
			}
			if mode == OpenAIWSIngressModeHTTPBridge {
				require.EqualValues(t, 3, f.transport.sends.Load(), "one actual HTTP request for each independent turn")
				require.Zero(t, f.transport.legacy.Load())
				require.False(t, gjson.GetBytes(followup, "previous_response_id").Exists())
				require.Equal(t, "call_a1", gjson.GetBytes(followup, "input.#(type==\"function_call\").call_id").String(), "HTTP continuation retains the completed call")
				require.Equal(t, "call_a1", gjson.GetBytes(followup, "input.#(type==\"function_call_output\").call_id").String())
				require.Equal(t, "tool result", gjson.GetBytes(followup, "input.#(type==\"function_call_output\").output").String())
			}
			require.False(t, f.resource.modelMappingCacheReady, "lane must not mutate the caller's model-mapping cache")
		})
	}
}

// A real accepted frame followed by a lost read has unknown completion. Run
// both production ingress consumers and HTTP-to-WS against local sockets; no
// replacement HTTP request or second WS create is permitted.
func TestNativeWSConsumerUnsafeHostedReadLossNeverRegenerates(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, "http_to_ws"} {
		t.Run(mode, func(t *testing.T) {
			var creates atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				if _, _, err := c.Read(r.Context()); err == nil {
					creates.Add(1)
				}
			}))
			defer upstream.Close()
			fixtureMode := mode
			if mode == "http_to_ws" {
				fixtureMode = OpenAIWSIngressModeCtxPool
			}
			f := nativeWSConsumerService(t, fixtureMode, upstream.URL)
			s := f.service
			body, bodyErr := json.Marshal(map[string]any{"type": "response.create", "model": "client-model", "input": "plain text", "tools": []any{map[string]any{"type": "code_interpreter", "container": map[string]any{"type": "auto"}}}})
			require.NoError(t, bodyErr)
			var facts model.OperationResult
			if mode == "http_to_ws" {
				user, err := routing.AcquireCapacity(f.ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
				require.NoError(t, err)
				defer user.ReleaseFunc()
				account, err := routing.AcquireCapacity(f.ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: "read-loss-http", Limit: 2}, nil)
				require.NoError(t, err)
				require.True(t, account.Acquired)
				defer account.ReleaseFunc()
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(f.ctx)
				c.Set("api_key", f.key)
				recovered := false
				owner := nativeHTTPFromContext(f.ctx)
				originalDigest := owner.core.Snapshot().OriginalWireDigest
				err = owner.core.Execute(f.ctx, func() NativeOperationStep {
					bound, bindErr := s.BindNativeWSOperation(f.ctx, body)
					require.NoError(t, bindErr)
					require.Same(t, owner, nativeHTTPFromContext(bound))
					_, readErr := s.forwardOpenAIWSV2(bound, c, f.resource, map[string]any{"model": "client-model", "input": "plain text", "tools": []any{map[string]any{"type": "code_interpreter", "container": map[string]any{"type": "auto"}}}}, "", "", "synthetic-upstream-key", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, false, "client-model", "client-model", time.Now(), 1, time.Time{}, "", &recovered)
					require.NoError(t, FinishNativeWSOperation(bound, nil))
					require.Equal(t, model.OperationReceiving, owner.core.Snapshot().State, "WS adaptation must not finish HTTP Execute")
					return NativeOperationStep{Err: readErr}
				})
				require.Equal(t, model.OperationFailed, owner.core.Snapshot().State)
				require.Equal(t, originalDigest, owner.core.Snapshot().OriginalWireDigest)
				require.Error(t, err)
				var fallback *openAIWSFallbackError
				require.False(t, errors.As(err, &fallback), "a read failure cannot request another transport")
				facts = nativeHTTPFromContext(f.ctx).core.Snapshot()
			} else {
				type outcome struct {
					facts model.OperationResult
					err   error
				}
				done := make(chan outcome, 1)
				downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					socket, err := coderws.Accept(w, r, nil)
					if err != nil {
						done <- outcome{err: err}
						return
					}
					defer socket.CloseNow()
					_ = RunOpenAIWSMultiplexer(openAIWSTestAdmissionContext(r.Context()), socket, s.cfg, func(lane context.Context, ds *OpenAIWSDownstream) {
						defer ds.CloseNow()
						_, first, err := ds.Read(lane)
						if err != nil {
							done <- outcome{err: err}
							return
						}
						request := f.op.request
						request.Transport = "ws"
						request.OperationID = "ws-read-loss"
						turn, err := s.BindNativeWSDispatch(ds.TurnBudgetContext(lane, s.cfg, first), &APIKeyService{apiKeyRepo: f.keys}, request)
						if err != nil {
							done <- outcome{err: err}
							return
						}
						turn, err = s.BindNativeWSOperation(turn, first)
						if err != nil {
							done <- outcome{err: err}
							return
						}
						turn = bindNativeWSConsumerPricing(t, f, turn)
						if err = ds.PublishNativeWSTurn(lane, turn); err != nil {
							done <- outcome{err: err}
							return
						}
						user, err := routing.AcquireCapacity(turn, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
						if err != nil {
							done <- outcome{err: err}
							return
						}
						defer user.ReleaseFunc()
						account, err := routing.AcquireCapacity(turn, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: request.OperationID, Limit: 2}, nil)
						if err != nil || !account.Acquired {
							done <- outcome{err: fmt.Errorf("capacity: %v", err)}
							return
						}
						defer account.ReleaseFunc()
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = r.Clone(turn)
						c.Set("api_key", f.key)
						hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, cause error) {
							_, _ = finalizeNativeWSUsage(turn, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, cause, nativeWSFixtureJournal(), time.Second)
						}}
						err = s.ProxyResponsesWebSocketFromClient(turn, c, ds, f.resource, "synthetic-upstream-key", first, hooks)
						done <- outcome{facts: nativeHTTPFromContext(turn).core.Snapshot(), err: err}
					})
				}))
				defer downstream.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(downstream.URL, "http"), nil)
				require.NoError(t, err)
				defer client.CloseNow()
				require.NoError(t, client.Write(ctx, coderws.MessageText, body))
				// Keep the real client's sole read pump alive so it answers the
				// server's close handshake. Waiting only on done forces the
				// transport's two-second close timeout in this fixture.
				readerDone := make(chan struct{})
				go func() {
					defer close(readerDone)
					for {
						if _, _, err := client.Read(ctx); err != nil {
							return
						}
					}
				}()
				defer func() { _ = client.CloseNow(); <-readerDone }()
				out := wsLifecycleReceive(t, done)
				require.Error(t, out.err)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(out.err, &failover))
				facts = out.facts
			}
			require.EqualValues(t, 1, creates.Load())
			require.Zero(t, f.transport.sends.Load())
			require.Len(t, facts.Attempts, 1)
			require.Equal(t, model.SendSent, facts.Attempts[0].Send)
			require.Equal(t, model.TerminalUnknown, facts.Attempts[0].Terminal)
			require.Nil(t, facts.Attempts[0].Usage)
			require.True(t, facts.Attempts[0].Closed)
		})
	}
}

func TestNativeWSConsumerHTTPBridgeReadLossNeverRegenerates(t *testing.T) {
	f := newNativeOperationFixture(t, true, 3)
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(f.ctx)
	c.Set("api_key", f.key)
	_, _, grantErr := f.service.GetAccessToken(f.ctx, f.resource)
	require.NoError(t, grantErr)
	payload := []byte("{\"type\":\"response.create\",\"model\":\"client-model\",\"input\":\"hello\"}")
	_, err := f.service.proxyOpenAIWSHTTPBridgeTurn(f.ctx, c, f.resource, "synthetic-upstream-key", payload, len(payload), "client-model", "", "", "", "", 1, func([]byte) error { return nil })
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	require.EqualValues(t, 1, f.transport.sends.Load())
	facts := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Len(t, facts.Attempts, 1)
	require.Equal(t, model.SendSent, facts.Attempts[0].Send)
	require.Equal(t, model.TerminalUnknown, facts.Attempts[0].Terminal)
	require.Nil(t, facts.Attempts[0].Usage)
	require.True(t, facts.Attempts[0].EndOfStream)
}

func TestNativeWSConsumerExplicitRefusalPreservesRecovery(t *testing.T) {
	for _, code := range []string{"websocket_connection_limit_reached", "previous_response_not_found", "invalid_encrypted_content", "server_error"} {
		t.Run(code, func(t *testing.T) {
			var creates atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer c.CloseNow()
				if _, _, err = c.Read(r.Context()); err != nil {
					return
				}
				creates.Add(1)
				payload, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]string{"code": code, "type": code, "message": code}})
				_ = c.Write(r.Context(), coderws.MessageText, payload)
			}))
			defer upstream.Close()
			f := nativeWSConsumerService(t, OpenAIWSIngressModeCtxPool, upstream.URL)
			user, err := routing.AcquireCapacity(f.ctx, nil, routing.CapacityRequest{ID: f.key.UserID, User: true}, nil)
			require.NoError(t, err)
			defer user.ReleaseFunc()
			account, err := routing.AcquireCapacity(f.ctx, f.capacity, routing.CapacityRequest{ID: f.resource.ID, Token: code, Limit: 2}, nil)
			require.NoError(t, err)
			require.True(t, account.Acquired)
			defer account.ReleaseFunc()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(f.ctx)
			c.Set("api_key", f.key)
			recovered := false
			_, err = f.service.forwardOpenAIWSV2(f.ctx, c, f.resource, map[string]any{"model": "client-model", "input": "hello"}, "", "", "synthetic-upstream-key", OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, false, false, "client-model", "client-model", time.Now(), 1, time.Time{}, "", &recovered)
			require.Error(t, err)
			var fallback *openAIWSFallbackError
			require.Equal(t, code != "server_error", errors.As(err, &fallback))
			require.EqualValues(t, 1, creates.Load())
			require.Zero(t, f.transport.sends.Load())
			facts := nativeHTTPFromContext(f.ctx).core.Snapshot()
			require.Len(t, facts.Attempts, 1)
			require.Equal(t, model.SendSent, facts.Attempts[0].Send)
			require.Equal(t, model.TerminalUnknown, facts.Attempts[0].Terminal, "bare refusal is not an invented response terminal")
			require.Nil(t, facts.Attempts[0].Usage)
		})
	}
}
