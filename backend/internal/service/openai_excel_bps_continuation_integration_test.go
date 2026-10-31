package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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
		ctx := openAIWSTestAdmissionContext(r.Context())
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
	require.Len(t, producerUpstream.requests, 1, "the producer must pass host admission and enter BPS")
	require.Equal(t, "/basispoints/api/responses", producerUpstream.lastReq.URL.Path)
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

func TestExcelBPSContinuationIntegrationToolChoiceRejectsWithoutLosingHistory(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key := newBPSContinuationIntegrationKey()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_choice_parent", "prior BPS answer"))}, cache)
	account := excelBPSDualGrantTestAccount(t, producer)
	seed := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"original BPS question"}],"tools":[{"type":"function","name":"inspect","description":"Keep this declaration intact","parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}}]}`)
	seedContext, seedRecorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(seed)), key)
	seedResult, err := producer.Forward(t.Context(), seedContext, account, seed)
	require.NoError(t, err)
	require.NotNil(t, seedResult)
	require.Equal(t, "resp_integration_choice_parent", seedResult.ResponseID)
	require.True(t, gjson.Get(seedRecorder.Body.String(), "output.0").Exists())

	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_choice_result", "continued BPS answer"))}
	consumer := newBPSContinuationIntegrationService(upstream, cache)
	consumer.openAITokenProvider = producer.openAITokenProvider
	// An inherited tool catalog does not make a forced choice supported.
	next := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_integration_choice_parent","tool_choice":"required","input":[{"role":"user","content":"current BPS delta"}]}`)
	original := bytes.Clone(next)
	c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(next)), key)
	ctx := EnsureOpenAITurnBudget(t.Context(), consumer.cfg)
	result, err := consumer.Forward(ctx, c, account, next)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, "basispoints_request_invalid", gjson.Get(recorder.Body.String(), "error.code").String())
	require.Equal(t, "tool_choice", gjson.Get(recorder.Body.String(), "error.param").String())
	require.Empty(t, upstream.requests, "a local BPS refusal cannot dispatch through a native grant")
	require.Equal(t, original, next, "do not silently downgrade the client's required choice")
	require.True(t, excelBPSExecutionPinned(ctx))

	// The refusal leaves the authenticated snapshot intact for a supported
	// continuation, including the exact prior input, output and declarations.
	restored, err := consumer.restoreExcelBPSContinuation(t.Context(), c, account, next)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(restored, "previous_response_id").Exists())
	require.Equal(t, "required", gjson.GetBytes(restored, "tool_choice").String())
	require.JSONEq(t, gjson.GetBytes(seed, "tools").Raw, gjson.GetBytes(restored, "tools").Raw)
	wantInput := fmt.Sprintf("[%s,%s,%s]", gjson.GetBytes(seed, "input.0").Raw, gjson.Get(seedRecorder.Body.String(), "output.0").Raw, gjson.GetBytes(next, "input.0").Raw)
	require.JSONEq(t, wantInput, gjson.GetBytes(restored, "input").Raw)
	supported := bytes.Replace(next, []byte(`"tool_choice":"required"`), []byte(`"tool_choice":"auto"`), 1)
	supportedContext, supportedRecorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(supported)), key)
	result, err = consumer.Forward(t.Context(), supportedContext, account, supported)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, UsageAuthorizationModeBPS, result.AuthorizationMode)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
	require.Contains(t, string(upstream.lastBody), "Keep this declaration intact")
	require.Contains(t, string(upstream.lastBody), "original BPS question")
	require.Contains(t, string(upstream.lastBody), "prior BPS answer")
	require.Contains(t, string(upstream.lastBody), "current BPS delta")
	require.Contains(t, supportedRecorder.Body.String(), "continued BPS answer")
}

func TestExcelBPSContinuationIntegrationPoolFailoverCarriesRestoredHistory(t *testing.T) {
	cache := &bpsContinuationIntegrationCache{}
	key := newBPSContinuationIntegrationKey()
	producer := newBPSContinuationIntegrationService(&httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_integration_pool_parent", "pool fallback prior answer"))}, cache)
	account := excelBPSDualGrantTestAccount(t, producer)
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
	next := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_integration_pool_parent","input":[{"role":"user","content":"pool fallback current delta"}]}`)
	c, recorder := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(next)).WithContext(ctx), key)
	result, err := consumer.Forward(ctx, c, account, next)
	require.Nil(t, result)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, http.StatusTooManyRequests, failure.StatusCode)
	require.True(t, failure.RejectedBeforeGeneration)
	require.True(t, failure.ShouldRetryNextAccount())
	require.True(t, excelBPSExecutionPinned(ctx), "restoring history cannot reopen Native candidates")
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
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure, "a local admission error is not the intended upstream 429")
	require.Equal(t, http.StatusTooManyRequests, failure.StatusCode)
	require.True(t, failure.RejectedBeforeGeneration)
	require.Len(t, upstream.requests, 1, "prove the BPS attempt occurred before examining retry payload")
	require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
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

// Reproduce the diagnostic structure of #62429/#62430, not their unavailable
// plaintext. The provider rejects old attachment IDs; every opaque-history turn
// must upload first and continue without resubmitting inference or losing history.
func TestExcelBPSOpaqueAttachmentContinuationAvoidsStaleCache(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		for _, images := range []int{1, 5} {
			t.Run(fmt.Sprintf("%s/images=%d", transport, images), func(t *testing.T) {
				cache := &bpsContinuationIntegrationCache{}
				svc := newBPSContinuationIntegrationService(nil, cache)
				enableNativeAttachments(svc)
				account, key := excelAccount(), newBPSContinuationIntegrationKey()
				var parts []any
				for i := 0; i < images; i++ {
					var data bytes.Buffer
					require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, i+2, 3))))
					parts = append(parts, map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())})
				}
				input := []any{map[string]any{"role": "user", "content": parts}}
				warm := map[string]any{"model": "gpt-6-astra", "stream": transport != "json", "prompt_cache_key": t.Name(), "input": input}
				warm["tools"] = []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}
				warmBody, e := json.Marshal(warm)
				require.NoError(t, e)
				hidden := []any{map[string]any{"type": "compaction", "id": "cmp_history", "encrypted_content": "OPAQUE_HISTORY_EXACT"}}
				reasoning := 32
				if images == 5 {
					reasoning = 258
				}
				for i := 0; i < reasoning; i++ {
					hidden = append(hidden, map[string]any{"type": "reasoning", "summary": []any{}, "encrypted_content": fmt.Sprintf("REASONING_%d", i)})
				}
				hidden = append(hidden, map[string]any{"type": "function_call", "name": "lookup", "call_id": "call_previous", "arguments": "{}"}, map[string]any{"type": "function_call_output", "call_id": "call_previous", "output": "COMPLETED_TOOL_RESULT"})
				warm["input"] = append(hidden, input...)
				opaqueBody, e := json.Marshal(warm)
				require.NoError(t, e)
				var uploads atomic.Int64
				generation, rejected := 0, 0
				var oldIDs = map[string]bool{}
				svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
					require.Equal(t, account.ID, accountID)
					if req.URL.String() == basispoints.AttachmentsURL {
						return bpsCompletionResponse(200, fmt.Sprintf(`{"openai_file_id":"file-turn%d"}`, uploads.Add(1))), nil
					}
					require.Equal(t, basispoints.ResponsesURL, req.URL.String())
					generation++
					wire, e := io.ReadAll(req.Body)
					require.NoError(t, e)
					ids := map[string]bool{}
					for _, item := range gjson.GetBytes(wire, "input").Array() {
						for _, part := range item.Get("content").Array() {
							if id := part.Get("file_id").String(); id != "" {
								ids[id] = true
							}
						}
					}
					require.Len(t, ids, images)
					if generation == 1 {
						oldIDs = ids
					} else {
						require.Equal(t, "OPAQUE_HISTORY_EXACT", gjson.GetBytes(wire, "input.#(type==compaction).encrypted_content").String())
						require.Len(t, gjson.GetBytes(wire, "input.#(type==reasoning)#").Array(), reasoning)
						require.Contains(t, string(wire), "COMPLETED_TOOL_RESULT")
						for id := range ids {
							if oldIDs[id] {
								rejected++
								return bpsCompletionResponse(500, excelBPSOwnershipEnvelope), nil
							}
						}
						if generation == 3 {
							require.Contains(t, string(wire), "FOLLOWUP_CONTINUES_SAME_CONVERSATION")
						}
					}
					return bpsCompletionResponse(200, bpsCompletedStream(fmt.Sprintf("resp_fresh_%d", generation), "continued")), nil
				}}
				delta := fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"previous_response_id":"resp_fresh_2","input":"FOLLOWUP_CONTINUES_SAME_CONVERSATION"}`, transport != "json")
				if transport == "ws" {
					client, done, turns := startBPSContinuationIntegrationSession(t, svc, account, key, string(warmBody))
					readBPSWSTestTurn(t, client)
					requireBPSContinuationIntegrationTurn(t, turns)
					require.EqualValues(t, images, uploads.Load())
					require.Equal(t, images, svc.excelBPSAttachmentCache.Len())
					require.NoError(t, client.Write(t.Context(), coderws.MessageText, opaqueBody))
					readBPSWSTestTurn(t, client)
					requireBPSContinuationIntegrationTurn(t, turns)
					requireBPSWSTestServerExit(t, client, done)
					client, done, turns = startBPSContinuationIntegrationSession(t, svc, account, key, delta)
					readBPSWSTestTurn(t, client)
					requireBPSContinuationIntegrationTurn(t, turns)
					requireBPSWSTestServerExit(t, client, done)
				} else {
					for turn, body := range [][]byte{warmBody, opaqueBody, []byte(delta)} {
						c, rec := newBPSContinuationIntegrationContext(httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)), key)
						result, err := svc.Forward(t.Context(), c, account, body)
						require.NoError(t, err, rec.Body.String())
						require.NotNil(t, result)
						require.True(t, result.SucceededForScheduling())
						require.NotContains(t, rec.Body.String(), "basispoints_file_ownership_error")
						if turn == 0 {
							require.Equal(t, images, svc.excelBPSAttachmentCache.Len())
						}
					}
				}
				require.Zero(t, rejected, "prevent stale IDs before inference, not by an unsafe retry")
				require.Equal(t, 3, generation)
				require.EqualValues(t, 3*images, uploads.Load())
			})
		}
	}
}
