package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsContinuationIntegrationStateEntry struct {
	data    []byte
	version uint64
}

// Independent gateway instances share both response owner bindings and BPS
// state. This fixture exercises transport integration, not Redis itself.
type bpsContinuationIntegrationCache struct {
	stubGatewayCache
	mu      sync.Mutex
	entries map[string]bpsContinuationIntegrationStateEntry
}

func (s *bpsContinuationIntegrationCache) LoadBPSState(ctx context.Context, scope, kind string) ([]byte, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entries[scope+"\x00"+kind]
	return append([]byte(nil), entry.data...), entry.version, nil
}

func (s *bpsContinuationIntegrationCache) SaveBPSState(ctx context.Context, scope, kind string, expected *uint64, data []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := scope + "\x00" + kind
	entry := s.entries[key]
	if expected != nil && *expected != entry.version {
		return false, nil
	}
	if s.entries == nil {
		s.entries = make(map[string]bpsContinuationIntegrationStateEntry)
	}
	s.entries[key] = bpsContinuationIntegrationStateEntry{data: append([]byte(nil), data...), version: entry.version + 1}
	return true, nil
}

func newBPSContinuationIntegrationService(upstream *httpUpstreamRecorder, cache *bpsContinuationIntegrationCache) *OpenAIGatewayService {
	svc := excelBPSGatewayTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	svc.cfg.Gateway.ExcelBPSStateRedisEnabled = true
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
	svc.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	svc.cache = cache
	return svc
}

func newBPSContinuationIntegrationKey() *APIKey {
	groupID := int64(23)
	return &APIKey{ID: 17, UserID: 19, GroupID: &groupID, User: &User{ID: 19}}
}

func newBPSContinuationIntegrationContext(request *http.Request, key *APIKey) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = request
	c.Set("api_key", key)
	SetOpenAIHTTPResponseOwner(c, key.UserID, key.ID)
	MarkExcelBPSImageAdmission(c)
	return c, recorder
}

// startOpenAIWSMemorySession inspects Gin only after ingress exits; this variant
// initializes each fresh authenticated context before continuation owner checks.
func startBPSContinuationIntegrationSession(t *testing.T, svc *OpenAIGatewayService, account *Account, key *APIKey, payload string) (*coderws.Conn, <-chan error, <-chan bpsWSTestTurn) {
	t.Helper()
	clientPipe, serverPipe := net.Pipe()
	listener := &nativeWSMemoryListener{conn: serverPipe, done: make(chan struct{})}
	serverErr := make(chan error, 1)
	turns := make(chan bpsWSTestTurn, 8)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(ResolveOpenAIWSClientReadLimitBytes(svc.cfg))
		ctx := context.Background()
		_, first, err := ReadOpenAIWSClientMessage(ctx, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first request")
		if err != nil {
			serverErr <- err
			return
		}
		c, _ := newBPSContinuationIntegrationContext(r.Clone(ctx), key)
		SetOpenAIClientTransport(c, OpenAIClientTransportWS)
		BeginOpsStreamTurn(c, 1)
		hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} }}
		serverErr <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "test-token", first, hooks)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = clientPipe.Close(); _ = serverPipe.Close(); _ = server.Close() })
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return clientPipe, nil }}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws://bps-continuation-integration/v1/responses", &coderws.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
	return client, serverErr, turns
}

func requireBPSContinuationIntegrationTurn(t *testing.T, turns <-chan bpsWSTestTurn) *OpenAIForwardResult {
	t.Helper()
	select {
	case turn := <-turns:
		require.NoError(t, turn.err)
		require.NotNil(t, turn.result)
		return turn.result
	case <-time.After(3 * time.Second):
		t.Fatal("BPS continuation turn did not finish")
		return nil
	}
}

func TestExcelBPSContinuationIntegrationReconnectedToolTurnAndOlderFork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &bpsContinuationIntegrationCache{}
	key, account := newBPSContinuationIntegrationKey(), excelAccount()
	producerUpstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "integration_tool", "codex2api.custom/functions.exec", "text(42);"))}
	producer := newBPSContinuationIntegrationService(producerUpstream, cache)
	first, firstDone, firstTurns := startBPSContinuationIntegrationSession(t, producer, account, key, `{"type":"response.create","model":"gpt-6-astra","input":"integration original question","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`)
	events := readBPSWSTestTurn(t, first)
	firstResult := requireBPSContinuationIntegrationTurn(t, firstTurns)
	call := gjson.GetBytes(events[len(events)-1], "response.output.0")
	require.Equal(t, "custom_tool_call", call.Get("type").String())
	require.NotEmpty(t, call.Get("call_id").String())
	requireBPSWSTestServerExit(t, first, firstDone)

	consumerUpstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, bpsCompletedStream("resp_integration_answer", "integration tool answer")),
		bpsCompletionResponse(200, bpsCompletedStream("resp_integration_next", "integration newest answer")),
		bpsCompletionResponse(200, bpsCompletedStream("resp_integration_fork", "integration branch answer")),
	}}
	consumer := newBPSContinuationIntegrationService(consumerUpstream, cache)
	require.NotSame(t, producer.getOpenAIWSStateStore(), consumer.getOpenAIWSStateStore())
	payload := fmt.Sprintf(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":%q,"input":[{"type":"custom_tool_call_output","call_id":%q,"output":"integration tool result 42"}]}`, firstResult.ResponseID, call.Get("call_id").String())
	second, secondDone, secondTurns := startBPSContinuationIntegrationSession(t, consumer, account, key, payload)
	defer second.CloseNow()
	readBPSWSTestTurn(t, second)
	answer := requireBPSContinuationIntegrationTurn(t, secondTurns)
	require.Len(t, consumerUpstream.requests, 1)
	require.Contains(t, string(consumerUpstream.lastBody), "integration original question")
	require.Contains(t, string(consumerUpstream.lastBody), "integration tool result 42")
	require.Contains(t, string(consumerUpstream.lastBody), "Client tool catalog:", "a fresh socket must restore omitted tools")
	require.Contains(t, string(consumerUpstream.lastBody), "run_officejs")
	require.False(t, gjson.GetBytes(consumerUpstream.lastBody, "previous_response_id").Exists())

	require.NoError(t, second.Write(t.Context(), coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":"integration second delta"}`, answer.ResponseID))))
	readBPSWSTestTurn(t, second)
	requireBPSContinuationIntegrationTurn(t, secondTurns)
	require.Len(t, consumerUpstream.requests, 2)
	for _, want := range []string{"integration original question", "integration tool result 42", "integration tool answer", "integration second delta"} {
		require.Contains(t, string(consumerUpstream.lastBody), want, "restoration must seed the ingress history for its next delta")
	}

	require.NoError(t, second.Write(t.Context(), coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"tools":[],"input":"integration alternate branch"}`, answer.ResponseID))))
	readBPSWSTestTurn(t, second)
	requireBPSContinuationIntegrationTurn(t, secondTurns)
	require.Len(t, consumerUpstream.requests, 3)
	wire := string(consumerUpstream.lastBody)
	require.Contains(t, wire, "integration original question")
	require.Contains(t, wire, "integration tool answer")
	require.Contains(t, wire, "integration alternate branch")
	require.NotContains(t, wire, "integration second delta", "an older predecessor must not receive the newest branch")
	require.NotContains(t, wire, "integration newest answer")
	require.NotContains(t, wire, "Client tool catalog:", "explicit tools:[] replaces the saved catalog")
	requireBPSWSTestServerExit(t, second, secondDone)
}

func TestExcelBPSContinuationIntegrationHTTPToFreshWebSocket(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			cache := &bpsContinuationIntegrationCache{}
			key, account := newBPSContinuationIntegrationKey(), excelAccount()
			producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_http", "integration HTTP answer"))}, cache)
			body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":"integration HTTP question"}`, stream))
			c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)), key)
			result, err := producer.Forward(t.Context(), c, account, body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotNil(t, result)
			require.Equal(t, "resp_integration_http", result.ResponseID)

			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_ws", "continued"))}
			consumer := newBPSContinuationIntegrationService(upstream, cache)
			client, done, turns := startBPSContinuationIntegrationSession(t, consumer, account, key, `{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_integration_http","input":"integration WS question"}`)
			defer client.CloseNow()
			readBPSWSTestTurn(t, client)
			requireBPSContinuationIntegrationTurn(t, turns)
			require.Len(t, upstream.requests, 1)
			for _, want := range []string{"integration HTTP question", "integration HTTP answer", "integration WS question"} {
				require.Contains(t, string(upstream.lastBody), want)
			}
			require.False(t, gjson.GetBytes(upstream.lastBody, "previous_response_id").Exists())
			requireBPSWSTestServerExit(t, client, done)
		})
	}
}

func TestExcelBPSContinuationIntegrationRejectsDifferentOwnerOrCredential(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key, account := newBPSContinuationIntegrationKey(), excelAccount()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_private", "private integration answer"))}, cache)
	body := []byte(`{"model":"gpt-6-astra","input":"private integration question"}`)
	c, _ := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)), key)
	_, err := producer.Forward(t.Context(), c, account, body)
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		change func(*APIKey, *Account)
	}{
		{"key", func(key *APIKey, _ *Account) { key.ID++ }},
		{"user", func(key *APIKey, _ *Account) { key.UserID++; key.User = &User{ID: key.UserID} }},
		{"group", func(key *APIKey, _ *Account) { (*key.GroupID)++ }},
		{"account", func(_ *APIKey, account *Account) { account.ID++ }},
		{"credential", func(_ *APIKey, account *Account) {
			account.Credentials["chatgpt_account_id"] = "another-upstream-workspace"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			otherKey, otherAccount := newBPSContinuationIntegrationKey(), excelAccount()
			tc.change(otherKey, otherAccount)
			upstream := &httpUpstreamRecorder{}
			consumer := newBPSContinuationIntegrationService(upstream, cache)
			next := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_integration_private","input":"continue"}`)
			c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(next)), otherKey)
			result, err := consumer.Forward(t.Context(), c, otherAccount, next)
			require.Error(t, err)
			require.Nil(t, result)
			require.Empty(t, upstream.requests, "scope mismatch must be rejected before generation")
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Equal(t, "previous_response_not_found", gjson.Get(recorder.Body.String(), "error.code").String())
			require.NotContains(t, recorder.Body.String(), "private integration")
		})
	}
}

func TestExcelBPSContinuationIntegrationTerminalWriteFailureDoesNotPublish(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key, account := newBPSContinuationIntegrationKey(), excelAccount()
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_undelivered", "undelivered answer"))}
	producer := newBPSContinuationIntegrationService(upstream, cache)
	body := []byte(`{"model":"gpt-6-astra","stream":true,"input":"undelivered question"}`)
	c, _ := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)), key)
	SetOpenAIClientTransport(c, OpenAIClientTransportWS)
	terminalAttempts := 0
	_, _ = producer.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func(event []byte) error {
		if gjson.GetBytes(event, "type").String() == "response.completed" {
			terminalAttempts++
			return io.ErrClosedPipe
		}
		return nil
	})
	require.Equal(t, 1, terminalAttempts)
	require.Len(t, upstream.requests, 1, "delivery failure must not replay generation")

	consumerUpstream := &httpUpstreamRecorder{}
	consumer := newBPSContinuationIntegrationService(consumerUpstream, cache)
	next := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_integration_undelivered","input":"continue"}`)
	nextContext, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(next)), key)
	result, err := consumer.Forward(t.Context(), nextContext, account, next)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "previous_response_not_found", gjson.Get(recorder.Body.String(), "error.code").String())
	require.Empty(t, consumerUpstream.requests)
}

func TestExcelBPSContinuationIntegrationNativeToolChoiceFallbackKeepsRestoredHistory(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key := newBPSContinuationIntegrationKey()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_native_parent", "native fallback prior answer"))}, cache)
	account := excelBPSNativeFallbackTestAccount(t, producer)
	seed := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"native fallback original question"}],"tools":[{"type":"function","name":"inspect","description":"Keep this declaration intact","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}]}`)
	seedContext, seedRecorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(seed)), key)
	seedResult, err := producer.Forward(t.Context(), seedContext, account, seed)
	require.NoError(t, err)
	require.NotNil(t, seedResult)
	require.Equal(t, "resp_integration_native_parent", seedResult.ResponseID)
	require.True(t, gjson.Get(seedRecorder.Body.String(), "output.0").Exists())

	nativeResponse := bpsCompletionResponse(200, bpsCompletedStream("resp_integration_native_result", "native fallback result"))
	nativeResponse.Header.Set("Content-Type", "text/event-stream")
	upstream := &httpUpstreamRecorder{resp: nativeResponse}
	consumer := newBPSContinuationIntegrationService(upstream, cache)
	consumer.openAITokenProvider = producer.openAITokenProvider
	// Omitted tools must come from the authenticated snapshot before BPS's
	// local required-choice rejection enters the same-account native fallback.
	next := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_integration_native_parent","tool_choice":"required","input":[{"role":"user","content":"native fallback current delta"}]}`)
	c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(next)), key)
	result, err := consumer.Forward(t.Context(), c, account, next)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, UsageAuthorizationModeNative, result.AuthorizationMode)
	require.Len(t, upstream.requests, 1, "local BPS tool-choice refusal must not send a BPS generation")
	require.Equal(t, "/backend-api/codex/responses", upstream.lastReq.URL.Path)
	require.Equal(t, "Bearer native-codex-token", upstream.lastReq.Header.Get("Authorization"))
	require.False(t, gjson.GetBytes(upstream.lastBody, "previous_response_id").Exists())
	require.Equal(t, "required", gjson.GetBytes(upstream.lastBody, "tool_choice").String())
	require.JSONEq(t, gjson.GetBytes(seed, "tools").Raw, gjson.GetBytes(upstream.lastBody, "tools").Raw)
	wantInput := fmt.Sprintf("[%s,%s,%s]", gjson.GetBytes(seed, "input.0").Raw, gjson.Get(seedRecorder.Body.String(), "output.0").Raw, gjson.GetBytes(next, "input.0").Raw)
	require.JSONEq(t, wantInput, gjson.GetBytes(upstream.lastBody, "input").Raw, "native dispatch must receive the restored input, output and current delta")
	require.Contains(t, recorder.Body.String(), "native fallback result")
}

func TestExcelBPSContinuationIntegrationPoolFailoverCarriesRestoredHistory(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key := newBPSContinuationIntegrationKey()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_pool_parent", "pool fallback prior answer"))}, cache)
	account := excelBPSNativeFallbackTestAccount(t, producer)
	seed := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"pool fallback original question"}],"tools":[{"type":"function","name":"inspect","parameters":{"type":"object","properties":{"query":{"type":"string"}}}}]}`)
	seedContext, seedRecorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(seed)), key)
	seedResult, err := producer.Forward(t.Context(), seedContext, account, seed)
	require.NoError(t, err)
	require.NotNil(t, seedResult)
	require.Equal(t, "resp_integration_pool_parent", seedResult.ResponseID)
	require.True(t, gjson.Get(seedRecorder.Body.String(), "output.0").Exists())

	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusTooManyRequests, `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"BPS capacity"}}`)}
	consumer := newBPSContinuationIntegrationService(upstream, cache)
	consumer.openAITokenProvider = producer.openAITokenProvider
	ctx := EnsureOpenAITurnBudget(t.Context(), consumer.cfg)
	// Mirror HTTP handler pool ownership: return this attempt to the pool
	// instead of taking an unmanaged same-account native fallback here.
	ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget).bpsRecovery.managed = true
	next := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_integration_pool_parent","input":[{"role":"user","content":"pool fallback current delta"}]}`)
	c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(next)).WithContext(ctx), key)
	result, err := consumer.Forward(ctx, c, account, next)
	require.Nil(t, result)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, http.StatusTooManyRequests, failure.StatusCode)
	require.True(t, failure.RejectedBeforeGeneration)
	require.True(t, failure.ShouldRetryNextAccount())
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
	require.False(t, c.Writer.Written(), "the HTTP handler owns the next attempt and final response")
	require.Empty(t, recorder.Body.String())
	replay := failure.ContinuationReplayBody
	require.NotEmpty(t, replay)
	require.True(t, gjson.ValidBytes(replay))
	require.False(t, gjson.GetBytes(replay, "previous_response_id").Exists())
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(replay, "model").String())
	require.JSONEq(t, gjson.GetBytes(seed, "tools").Raw, gjson.GetBytes(replay, "tools").Raw)
	wantInput := fmt.Sprintf("[%s,%s,%s]", gjson.GetBytes(seed, "input.0").Raw, gjson.Get(seedRecorder.Body.String(), "output.0").Raw, gjson.GetBytes(next, "input.0").Raw)
	require.JSONEq(t, wantInput, gjson.GetBytes(replay, "input").Raw, "pool retry must not reuse the client's unresolved delta")
}

func TestExcelBPSContinuationIntegrationWSFailoverKeepsRestoredTools(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key, account := newBPSContinuationIntegrationKey(), excelAccount()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, excelBPSRepairWire(t, "retry_tool", "codex2api.custom/functions.exec", "text(42);"))}, cache)
	seed := []byte(`{"model":"gpt-6-astra","input":"original question","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`)
	c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(seed)), key)
	_, err := producer.Forward(t.Context(), c, account, seed)
	require.NoError(t, err)
	id := gjson.Get(recorder.Body.String(), "id").String()
	call := gjson.Get(recorder.Body.String(), "output.0.call_id").String()
	require.NotEmpty(t, id)
	require.NotEmpty(t, call)
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(429, `{"error":{"code":"rate_limit_exceeded","type":"rate_limit_error","message":"BPS capacity"}}`)}
	consumer := newBPSContinuationIntegrationService(upstream, cache)
	payload := fmt.Sprintf(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":%q,"input":[{"type":"custom_tool_call_output","call_id":%q,"output":"42"}]}`, id, call)
	client, serverErr, _ := startBPSContinuationIntegrationSession(t, consumer, account, key, payload)
	defer client.CloseNow()
	select {
	case err = <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("recovered first turn did not return failover")
	}
	require.Error(t, err)
	replay, currentTurn := OpenAIWSCurrentTurnRetryPayload(err)
	require.True(t, currentTurn)
	require.NotEmpty(t, replay)
	require.False(t, gjson.GetBytes(replay, "previous_response_id").Exists())
	require.JSONEq(t, gjson.GetBytes(seed, "tools").Raw, gjson.GetBytes(replay, "tools").Raw)
	require.Contains(t, string(replay), "original question")
	require.Contains(t, string(replay), call)
	require.Len(t, upstream.requests, 1)
}

func TestExcelBPSContinuationSnapshotsInheritedCatalog(t *testing.T) {
	cache := new(bpsContinuationIntegrationCache)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, bpsCompletedStream("resp_catalog_first", "first")),
		bpsCompletionResponse(200, bpsCompletedStream("resp_catalog_second", "second")),
		bpsCompletionResponse(200, excelBPSRepairWire(t, "third", "Call", `{"name":"functions.inspect","arguments":{"n":9007199254740993}}`)),
	}}
	svc := newBPSContinuationIntegrationService(upstream, cache)
	account := excelAccount()
	for i, body := range []string{
		`{"model":"gpt-6-astra","input":"first","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"inspect","parameters":{"type":"object","properties":{"n":{"const":9007199254740993}}}}]}]}`,
		`{"model":"gpt-6-astra","input":"second"}`,
	} {
		c := excelBPSContinuationTestContext()
		c.Request.Header.Set("session_id", "inherited-catalog-session")
		_, err := svc.Forward(t.Context(), c, account, []byte(body))
		require.NoError(t, err, "HTTP turn %d", i+1)
	}
	require.Contains(t, string(upstream.bodies[1]), "functions.inspect", "second turn really inherited the live catalog")
	next := excelBPSContinuationTestContext()
	next.Request.Header.Set("session_id", "another-connection-session")
	reader := newBPSContinuationIntegrationService(upstream, cache)
	body := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_catalog_second","input":"third"}`)
	restored, err := reader.restoreExcelBPSContinuation(t.Context(), next, account, body)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", gjson.GetBytes(restored, "tools.0.tools.0.parameters.properties.n.const").Raw)
	result, err := reader.Forward(t.Context(), next, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 3)
	require.Contains(t, string(upstream.bodies[2]), "functions.inspect")
}
