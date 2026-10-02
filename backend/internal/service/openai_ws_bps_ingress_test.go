package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bpsWSTestTurn struct {
	result *OpenAIForwardResult
	err    error
}

// Exercise a real HTTP upgrade and WS framing over net.Pipe. No TCP ports or
// external services are required, including in restricted test environments.
type bpsWSMemoryListener struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *bpsWSMemoryListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *bpsWSMemoryListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (l *bpsWSMemoryListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "bps-ws-test", Net: "memory"}
}

func startBPSWSMemorySession(t *testing.T, svc *OpenAIGatewayService, account *Account, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	clientPipe, serverPipe := net.Pipe()
	listener := &bpsWSMemoryListener{conn: serverPipe, done: make(chan struct{})}
	serverErr := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()
		ctx := context.Background()
		_, first, err := ReadOpenAIWSClientMessage(ctx, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first request")
		if err != nil {
			serverErr <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.Clone(ctx)
		serverErr <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "test-token", first, hooks)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = clientPipe.Close(); _ = serverPipe.Close(); _ = server.Close() })
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return clientPipe, nil }}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws://bps-ws-test/v1/responses", &coderws.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
	return client, serverErr
}

func readBPSWSTestTurn(t *testing.T, conn *coderws.Conn) []json.RawMessage {
	t.Helper()
	var events []json.RawMessage
	for {
		payload, err := readPassthroughLifecycleFrame(t, conn, 3*time.Second)
		require.NoError(t, err)
		require.True(t, json.Valid(payload), "a WS frame must contain JSON, not SSE lines")
		events = append(events, payload)
		kind := gjson.GetBytes(payload, "type").String()
		if isOpenAIWSTerminalEvent(kind) || kind == "error" {
			return events
		}
	}
}

func requireBPSWSTestServerExit(t *testing.T, conn *coderws.Conn, serverErr <-chan error) {
	t.Helper()
	_ = conn.CloseNow()
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("BPS WS ingress did not exit after client disconnect")
	}
}

func TestOpenAIWSBPSIngressBridgesRegardlessOfNativeWSMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{OpenAIWSIngressModeOff, OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, "legacy"} {
		t.Run(mode, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_bps", "hello"))}
			svc := openAIClientToolsTestService(upstream)
			svc.cfg = passthroughLifecycleConfig()
			svc.cfg.Gateway.ForceCodexCLI = true
			svc.cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = mode != "legacy"
			account := excelAccount()
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			require.True(t, account.IsOpenAIWSForceHTTPEnabled())
			turns := make(chan bpsWSTestTurn, 1)
			hooks := &OpenAIWSIngressHooks{MaxReasoningEffort: "high", AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} }}
			client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","input":"hello","stream":false,"reasoning":{"effort":"max"}}`, hooks)
			defer client.CloseNow()
			events := readBPSWSTestTurn(t, client)
			require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
			turn := <-turns
			require.NoError(t, turn.err)
			require.True(t, turn.result.OpenAIWSMode)
			require.Equal(t, "resp_bps", turn.result.ResponseID)
			require.Equal(t, 10, turn.result.Usage.InputTokens)
			require.Equal(t, 2, turn.result.Usage.OutputTokens)
			require.Equal(t, "max", *turn.result.RequestedReasoningEffort)
			require.Equal(t, "high", *turn.result.ReasoningEffort)
			require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
			require.Equal(t, "/basispoints/api/responses", upstream.lastReq.URL.Path)
			require.Equal(t, "Bearer test-token", upstream.lastReq.Header.Get("Authorization"))
			require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
			require.False(t, gjson.GetBytes(upstream.lastBody, "type").Exists())
			requireBPSWSTestServerExit(t, client, serverErr)
		})
	}
}

func TestOpenAIWSBPSBridgePreservesHostedToolPolicyAndPrewarm(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(fmt.Sprint(omit), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_policy", "hosted answer"))}
			upstream.resp.Header.Set("Content-Type", "text/event-stream")
			svc := openAIClientToolsTestService(upstream)
			svc.cfg = passthroughLifecycleConfig()
			account := excelAccount()
			account.Extra[ExcelBPSOmitUnsupportedToolsKey] = omit
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			payload := []byte(`{"type":"response.create","model":"gpt-6-astra","input":"test","reasoning":{"effort":"none"},"tools":[{"type":"web_search","external_web_access":true}]}`)
			var events [][]byte
			write := func(event []byte) error { events = append(events, append([]byte(nil), event...)); return nil }
			prewarm := append([]byte(`{"generate":false,`), payload[1:]...)
			result, err := svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", prewarm, len(prewarm), "gpt-6-astra", true, "", "", "", "", 1, write)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.LocalPrewarm)
			require.Empty(t, upstream.requests, "hosted tools must not turn generate=false into real generation")
			require.Len(t, events, 2)
			events = nil
			result, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", payload, len(payload), "gpt-6-astra", true, "", "", "", "", 2, write)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, result.LocalPrewarm)
			require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
			require.True(t, result.wsReplayInputExists)
			require.Len(t, result.wsReplayInput, 1)
			require.Contains(t, string(result.wsReplayInput[0]), "hosted answer")
			if omit {
				require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
				require.Equal(t, "low", gjson.GetBytes(upstream.lastBody, "reasoning_effort").String(), "BPS none is normalized to low, not omitted")
			} else {
				require.Equal(t, "chatgpt.com", upstream.lastReq.URL.Host)
				require.Equal(t, "none", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
			}
		})
	}
}

func TestOpenAIWSBPSIngressUsesChannelAndAccountMappingOnce(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, `data: {"type":"response.completed","response":{"id":"resp_alias","status":"completed","model":"gpt-6-astra","output":[],"usage":{"input_tokens":1,"output_tokens":2}}}`+"\n\n")}
	svc := openAIClientToolsTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	account := excelAccount()
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModePassthrough
	account.Credentials["model_mapping"] = map[string]any{"channel-alias": "gpt-6-astra", "gpt-6-astra": "wrong-double-map"}
	turns := make(chan bpsWSTestTurn, 1)
	mapCalls := 0
	hooks := &OpenAIWSIngressHooks{
		MapRequestModel: func(_ int, model string) (string, error) {
			mapCalls++
			if model != "public-alias" {
				return "", fmt.Errorf("unexpected model %s", model)
			}
			return "channel-alias", nil
		},
		AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
	}
	client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"public-alias","input":"test"}`, hooks)
	defer client.CloseNow()
	events := readBPSWSTestTurn(t, client)
	turn := <-turns
	require.NoError(t, turn.err)
	require.Equal(t, 1, mapCalls)
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "public-alias", turn.result.Model)
	require.Equal(t, "gpt-6-astra", turn.result.UpstreamModel)
	require.Equal(t, "public-alias", gjson.GetBytes(events[len(events)-1], "response.model").String())
	requireBPSWSTestServerExit(t, client, serverErr)
}

func TestOpenAIWSBPSIngressPrewarmAndIncrementalToolRoundTrip(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, excelBPSRepairWire(t, "tool", "codex2api.custom/functions.exec", "text(42);")),
		bpsCompletionResponse(200, bpsCompletedStream("resp_answer", "42")),
		bpsCompletionResponse(200, bpsCompletedStream("resp_clear", "done")),
	}}
	svc := openAIClientToolsTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 4)
	beforeRequests := make(chan int, 4)
	hooks := &OpenAIWSIngressHooks{
		BeforeRequest: func(turn int, _ []byte, _ string) error { beforeRequests <- turn; return nil },
		AfterTurn:     func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
	}
	client, serverErr := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","generate":false,"input":[],"tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`, hooks)
	defer client.CloseNow()
	prewarm := readBPSWSTestTurn(t, client)
	turn := <-turns
	require.NoError(t, turn.err)
	require.Empty(t, upstream.requests, "generate=false must not generate upstream")
	require.Zero(t, turn.result.Usage.InputTokens)
	prewarmID := gjson.GetBytes(prewarm[len(prewarm)-1], "response.id").String()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":[{"role":"user","content":"calculate 42"}]}`, prewarmID))))
	events := readBPSWSTestTurn(t, client)
	turn = <-turns
	require.NoError(t, turn.err)
	require.Equal(t, 2, <-beforeRequests)
	output := gjson.GetBytes(events[len(events)-1], "response.output.0")
	require.Equal(t, "custom_tool_call", output.Get("type").String())
	require.Equal(t, "exec", output.Get("name").String())
	require.Equal(t, "text(42);", output.Get("input").String())
	toolID := output.Get("call_id").String()
	responseID := turn.result.ResponseID
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":[{"type":"custom_tool_call_output","call_id":%q,"output":"tool returned 42"}]}`, responseID, toolID))))
	events = readBPSWSTestTurn(t, client)
	turn = <-turns
	require.NoError(t, turn.err)
	require.Equal(t, 3, <-beforeRequests)
	require.Contains(t, string(upstream.bodies[1]), "calculate 42")
	require.Contains(t, string(upstream.bodies[1]), "tool returned 42")
	require.Contains(t, string(upstream.bodies[1]), "run_officejs", "translated tool-call context must accompany its result")
	require.False(t, gjson.GetBytes(upstream.bodies[1], "previous_response_id").Exists())
	require.Equal(t, "42", gjson.GetBytes(events[len(events)-1], "response.output.0.content.0.text").String())
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","tools":[],"input":"done"}`)))
	readBPSWSTestTurn(t, client)
	turn = <-turns
	require.NoError(t, turn.err)
	require.Equal(t, 4, <-beforeRequests)
	require.Len(t, upstream.requests, 3)
	require.Contains(t, string(upstream.bodies[1]), "functions.exec")
	require.NotContains(t, string(upstream.bodies[2]), "functions.exec", "explicit tools:[] must clear the inherited catalog")
	requireBPSWSTestServerExit(t, client, serverErr)
}

func TestOpenAIWSBPSIngressStreamsBeforeCompletion(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: make(http.Header), Body: reader}}
	svc := openAIClientToolsTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 1)
	client, serverErr := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, &OpenAIWSIngressHooks{
		AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
	})
	defer client.CloseNow()
	go func() {
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.created","response":{"id":"resp_stream","status":"in_progress","output":[]}}`+"\n\ndata: "+`{"type":"response.output_text.delta","delta":"live"}`+"\n\n")
	}()
	created, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.created", gjson.GetBytes(created, "type").String())
	delta, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "live", gjson.GetBytes(delta, "delta").String())
	go func() {
		_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"id":"resp_stream","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`+"\n\n")
		_ = writer.Close()
	}()
	readBPSWSTestTurn(t, client)
	turn := <-turns
	require.NoError(t, turn.err)
	require.Equal(t, 3, turn.result.Usage.InputTokens)
	requireBPSWSTestServerExit(t, client, serverErr)
}

func TestOpenAIWSBPSIngressContinuationFailoverCarriesInheritedTools(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(200, excelBPSRepairWire(t, "before_limit", "codex2api.custom/functions.exec", "text(42);")),
		bpsCompletionResponse(429, "PRIVATE_RATE_LIMIT"),
		bpsCompletionResponse(200, bpsCompletedStream("resp_replacement", "42")),
	}}
	svc := openAIClientToolsTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	turns := make(chan bpsWSTestTurn, 2)
	account := excelAccount()
	client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","input":"calculate 42","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`, &OpenAIWSIngressHooks{
		AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
	})
	defer client.CloseNow()
	events := readBPSWSTestTurn(t, client)
	turn := <-turns
	require.NoError(t, turn.err)
	callID := gjson.GetBytes(events[len(events)-1], "response.output.0.call_id").String()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.create","previous_response_id":%q,"input":[{"type":"custom_tool_call_output","call_id":%q,"output":"tool returned 42"}]}`, turn.result.ResponseID, callID))))
	_, unexpected, readErr := client.Read(ctx)
	require.Error(t, readErr, "pre-output failover must not send a client error: %s", unexpected)
	select {
	case err := <-serverErr:
		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
		retry, safe := OpenAIWSCurrentTurnRetryPayload(err)
		require.True(t, safe)
		require.False(t, gjson.GetBytes(retry, "previous_response_id").Exists())
		require.Equal(t, "functions", gjson.GetBytes(retry, "tools.0.name").String())
		require.Contains(t, string(retry), "calculate 42")
		require.Contains(t, string(retry), "tool returned 42")
		require.Len(t, upstream.requests, 2)
		// Retry the complete payload on another BPS account, without relying
		// on the old account's catalog or replay cache.
		replacement := excelAccount()
		replacement.ID++
		replacementClient, replacementErr := startBPSWSMemorySession(t, svc, replacement, string(retry), nil)
		defer replacementClient.CloseNow()
		answered := readBPSWSTestTurn(t, replacementClient)
		require.Equal(t, "42", gjson.GetBytes(answered[len(answered)-1], "response.output.0.content.0.text").String())
		requireBPSWSTestServerExit(t, replacementClient, replacementErr)
		require.Len(t, upstream.requests, 3)
	case <-time.After(3 * time.Second):
		t.Fatal("BPS pre-output 429 did not return a current-turn failover")
	}
}

func TestOpenAIWSBPSBridgeErrorsAndDisconnectUsage(t *testing.T) {
	for _, status := range []int{400, 403, 429, 502, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(status, "PRIVATE_UPSTREAM_MESSAGE")}
			svc := openAIClientToolsTestService(upstream)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			originalWriter := c.Writer
			var events [][]byte
			_, err := svc.proxyOpenAIWSExcelBPSTurn(context.Background(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"test"}`), "gpt-6-astra", func(payload []byte) error {
				events = append(events, append([]byte(nil), payload...))
				return nil
			})
			require.Error(t, err)
			require.Same(t, originalWriter, c.Writer)
			require.False(t, originalWriter.Written(), "HTTP bytes must not reach an upgraded client")
			if status == 429 {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Empty(t, events, "pre-output 429 must remain available for account failover")
				return
			}
			require.NotEmpty(t, events)
			terminal := events[len(events)-1]
			require.NotContains(t, string(terminal), "PRIVATE_UPSTREAM_MESSAGE")
			if status == 200 {
				require.Equal(t, "response.failed", gjson.GetBytes(terminal, "type").String())
				require.Equal(t, "basispoints_stream_incomplete", gjson.GetBytes(terminal, "response.error.code").String())
			} else {
				require.Equal(t, "error", gjson.GetBytes(terminal, "type").String())
				require.Equal(t, status, int(gjson.GetBytes(terminal, "status").Int()))
			}
		})
	}
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_disconnect", "done"))}
	svc := openAIClientToolsTestService(upstream)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	writes := 0
	result, err := svc.proxyOpenAIWSExcelBPSTurn(context.Background(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"test"}`), "gpt-6-astra", func([]byte) error {
		writes++
		return coderws.CloseError{Code: coderws.StatusNormalClosure}
	})
	require.NoError(t, err)
	require.Equal(t, 1, writes)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
}

func TestOpenAIWSBPSIngressSchedulerAllowsAllBPSAccountPool(t *testing.T) {
	for _, advanced := range []bool{true, false} {
		t.Run(fmt.Sprint(advanced), func(t *testing.T) {
			account := excelAccount()
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*account}},
				cache:       &schedulerTestGatewayCache{}, cfg: passthroughLifecycleConfig(),
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
			}
			groupID := int64(42)
			selected, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportResponsesWebsocketV2Ingress, false)
			require.NoError(t, err)
			require.Equal(t, account.ID, selected.Account.ID)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
			_, _, err = svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportResponsesWebsocketV2, false)
			require.ErrorIs(t, err, ErrNoAvailableAccounts, "BPS still cannot use native upstream WS")
		})
	}
}

func TestOpenAIWSBPSIngressSwitchToNativeMapsModelOnce(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_native", "hello"))}
	svc := openAIClientToolsTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	account := excelAccount()
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-sol", "gpt-6-sol": "gpt-6-astra"}
	turns := make(chan bpsWSTestTurn, 2)
	client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","generate":false,"input":[]}`, &OpenAIWSIngressHooks{
		AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
	})
	defer client.CloseNow()
	readBPSWSTestTurn(t, client)
	prewarm := <-turns
	require.NoError(t, prewarm.err)
	require.True(t, prewarm.result.LocalPrewarm)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"alias","input":"hello"}`)))
	readBPSWSTestTurn(t, client)
	turn := <-turns
	require.NoError(t, turn.err)
	require.Equal(t, "chatgpt.com", upstream.lastReq.URL.Host)
	require.Equal(t, "gpt-6-sol", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "gpt-6-sol", turn.result.UpstreamModel)
	requireBPSWSTestServerExit(t, client, serverErr)
}

type nativeCodexStagedConn struct{ *stagedPassthroughConn }

func (c *nativeCodexStagedConn) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

func TestOpenAIWSNativeCodexIngressCompatibility(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(cfg, upstream)
			pool := newOpenAIWSConnPool(cfg)
			defer pool.Close()
			pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
			svc.openaiWSPool = pool
			account := excelAccount()
			account.Extra["openai_excel_bps"] = false
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
			turns := make(chan bpsWSTestTurn, 2)
			client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","input":"first"}`, &OpenAIWSIngressHooks{
				AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
			})
			defer client.CloseNow()
			for turn := 1; turn <= 2; turn++ {
				payload := requirePassthroughUpstreamWrite(t, upstream, time.Second)
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(payload, "model").String())
				upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_native_%d","model":"gpt-6-astra","usage":{"input_tokens":2,"output_tokens":1}}}`, turn))
				readBPSWSTestTurn(t, client)
				result := <-turns
				require.NoError(t, result.err)
				require.Equal(t, 2, result.result.Usage.InputTokens)
				if turn == 1 {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					err := client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_native_1","input":[{"role":"user","content":"second"}]}`))
					cancel()
					require.NoError(t, err)
				}
			}
			require.Empty(t, svc.httpUpstream.(*httpUpstreamRecorder).requests, "native OAuth accounts must keep their upstream WS transport")
			requireBPSWSTestServerExit(t, client, serverErr)
		})
	}
}

// bpsDelayedUpstream holds the BPS response until released, like an upstream
// that queues the request before sending headers.
type bpsDelayedUpstream struct {
	*httpUpstreamRecorder
	release chan struct{}
}

func (u *bpsDelayedUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	<-u.release
	return u.httpUpstreamRecorder.Do(req, proxyURL, accountID, accountConcurrency)
}

func shortenExcelBPSWSKeepalive(t *testing.T, interval time.Duration) {
	t.Helper()
	previous := excelBPSWSKeepaliveInterval
	excelBPSWSKeepaliveInterval = interval
	t.Cleanup(func() { excelBPSWSKeepaliveInterval = previous })
}

func TestOpenAIWSBPSTurnKeepsClientAliveWhileBPSIsSilent(t *testing.T) {
	const interval = 20 * time.Millisecond
	shortenExcelBPSWSKeepalive(t, interval)
	reader, writer := io.Pipe()
	defer writer.Close()
	upstream := &bpsDelayedUpstream{
		httpUpstreamRecorder: &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}},
		release:              make(chan struct{}),
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream, cfg: passthroughLifecycleConfig()}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	var mu sync.Mutex
	var kinds []string
	written := make(chan string, 64)
	write := func(payload []byte) error {
		kind := gjson.GetBytes(payload, "type").String()
		mu.Lock()
		kinds = append(kinds, kind)
		mu.Unlock()
		written <- kind
		return nil
	}
	waitKeepalives := func(n int) {
		t.Helper()
		for seen := 0; seen < n; {
			select {
			case kind := <-written:
				require.Equal(t, "keepalive", kind, "only keepalives may precede upstream output")
				seen++
			case <-time.After(time.Second):
				t.Fatalf("expected %d keepalive events while BPS was silent", n)
			}
		}
	}

	type turnResult struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan turnResult, 1)
	go func() {
		result, err := svc.proxyOpenAIWSExcelBPSTurn(context.Background(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"input":"hello"}`), "gpt-6-astra", write)
		done <- turnResult{result, err}
	}()
	waitKeepalives(2) // no response headers yet
	close(upstream.release)
	// The forwarder holds lifecycle events until the first output (its start
	// rate-limit window), so keepalives may still precede response.created.
	_, _ = io.WriteString(writer, "data: "+`{"type":"response.created","response":{"id":"resp_slow","status":"in_progress","output":[]}}`+"\n\n"+
		"data: "+`{"type":"response.output_text.delta","delta":"la"}`+"\n\n")
	var started []string
	for len(started) == 0 || started[len(started)-1] != "response.output_text.delta" {
		select {
		case kind := <-written:
			if kind != "keepalive" {
				started = append(started, kind)
			}
		case <-time.After(time.Second):
			t.Fatal("upstream output was not forwarded")
		}
	}
	require.Equal(t, []string{"response.created", "response.output_text.delta"}, started)
	waitKeepalives(2) // output started, stream stalled
	_, _ = io.WriteString(writer, "data: "+`{"type":"response.completed","response":{"id":"resp_slow","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"late"}]}],"usage":{"input_tokens":4,"output_tokens":1}}}`+"\n\n")
	_ = writer.Close()

	turn := <-done
	require.NoError(t, turn.err)
	require.Equal(t, 4, turn.result.Usage.InputTokens)
	require.Len(t, turn.result.wsReplayInput, 1, "keepalives are not conversation items")
	require.Contains(t, string(turn.result.wsReplayInput[0]), "late")
	mu.Lock()
	settled := append([]string(nil), kinds...)
	mu.Unlock()
	require.Equal(t, "response.completed", settled[len(settled)-1])
	time.Sleep(5 * interval)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, settled, kinds, "no keepalive may follow the terminal event")
}

func TestOpenAIWSBPSIngressKeepaliveReachesWebSocketClient(t *testing.T) {
	shortenExcelBPSWSKeepalive(t, 20*time.Millisecond)
	upstream := &bpsDelayedUpstream{
		httpUpstreamRecorder: &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_queued", "ready"))},
		release:              make(chan struct{}),
	}
	svc := openAIClientToolsTestService(upstream.httpUpstreamRecorder)
	svc.httpUpstream = upstream
	svc.cfg = passthroughLifecycleConfig()
	client, serverErr := startBPSWSMemorySession(t, svc, excelAccount(), `{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, nil)
	defer client.CloseNow()
	keepalive, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"keepalive"}`, string(keepalive))
	close(upstream.release)
	events := readBPSWSTestTurn(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
	requireBPSWSTestServerExit(t, client, serverErr)
}

func TestOpenAIWSBPSTurnReplayFollowsStoreMode(t *testing.T) {
	wire := "data: " + `{"type":"response.output_item.done","item":{"type":"reasoning","id":"rs_bps_1","summary":[],"encrypted_content":"enc-1"}}` + "\n\n" +
		"data: " + `{"type":"response.completed","response":{"id":"resp_reasoned","status":"completed","output":[{"type":"reasoning","id":"rs_bps_1","summary":[],"encrypted_content":"enc-1"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":3,"output_tokens":2}}}` + "\n\n"
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, wire)}
	svc := openAIClientToolsTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	result, err := svc.proxyOpenAIWSExcelBPSTurn(context.Background(), c, excelAccount(), []byte(`{"model":"gpt-6-astra","stream":true,"store":false,"input":"think"}`), "gpt-6-astra", func([]byte) error { return nil })
	require.NoError(t, err)
	require.Len(t, result.wsReplayInput, 2)
	reasoning := gjson.ParseBytes(result.wsReplayInput[0])
	require.Equal(t, "reasoning", reasoning.Get("type").String())
	require.False(t, reasoning.Get("id").Exists(), "store=false history must not reference rs_ ids, even on a later native fallback turn")
	require.Equal(t, "enc-1", reasoning.Get("encrypted_content").String())
	require.Equal(t, "message", gjson.GetBytes(result.wsReplayInput[1], "type").String())
	require.Equal(t, "rs_bps_1", gjson.GetBytes(result.wsAccountFailoverReplayInput[0], "id").String())
}

func TestOpenAIWSNativeIngressSwitchToBPSModelRequiresReconnect(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(cfg, upstream)
			pool := newOpenAIWSConnPool(cfg)
			defer pool.Close()
			pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
			svc.openaiWSPool = pool
			account := excelAccount()
			account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
			turns := make(chan bpsWSTestTurn, 2)
			client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-sol","input":"first"}`, &OpenAIWSIngressHooks{
				AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
			})
			defer client.CloseNow()
			require.Equal(t, "gpt-6-sol", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, time.Second), "model").String())
			upstream.Send(`{"type":"response.completed","response":{"id":"resp_native_1","model":"gpt-6-sol","usage":{"input_tokens":2,"output_tokens":1}}}`)
			readBPSWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)

			// Codex switches models on the open socket and resends full context.
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","input":[{"role":"user","content":"first"},{"role":"user","content":"second"}]}`)))
			// net.Pipe writes are synchronous: keep reading so the server's
			// closing frames cannot block on this test.
			go func() {
				for {
					if _, _, err := client.Read(context.Background()); err != nil {
						return
					}
				}
			}()
			select {
			case err := <-serverErr:
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
				require.ErrorIs(t, err, ErrOpenAIWSModelSwitchRequiresReconnect)
			case <-time.After(3 * time.Second):
				t.Fatal("native ingress did not stop at the BPS model switch")
			}
			require.Empty(t, svc.httpUpstream.(*httpUpstreamRecorder).requests, "the BPS turn must not reach BPS on a native socket")
			select {
			case payload := <-upstream.writes:
				t.Fatalf("the BPS turn must not reach the native upstream: %s", payload)
			default:
			}
		})
	}
}
