package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func startBPSWSMemorySession(t *testing.T, svc *OpenAIGatewayService, account *Account, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	return startOpenAIWSMemorySession(t, svc, account, nil, payload, hooks)
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
			svc := excelBPSGatewayTestService(upstream)
			svc.cfg = passthroughLifecycleConfig()
			svc.cfg.Gateway.ForceCodexCLI = true
			svc.cfg.Gateway.CodexImageGenerationBridgeEnabled = true
			svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = mode != "legacy"
			account := excelAccount()
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			require.False(t, account.IsOpenAIWSForceHTTPEnabled())
			turns := make(chan bpsWSTestTurn, 1)
			hooks := &OpenAIWSIngressHooks{MaxReasoningEffort: "high", AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} }}
			client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","input":"hello","stream":false,"reasoning":{"effort":"max"}}`, hooks)
			defer client.CloseNow()
			events := readBPSWSTestTurn(t, client)
			require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
			turn := <-turns
			require.NoError(t, turn.err)
			require.True(t, turn.result.OpenAIWSMode)
			require.NotNil(t, turn.result.LatencyMetrics)
			require.Equal(t, 1, turn.result.LatencyMetrics.Version)
			require.NotNil(t, turn.result.LatencyMetrics.FirstEventMs)
			require.NotNil(t, turn.result.LatencyMetrics.FirstTextMs)
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

func TestOpenAIWSBPSIngressUsesChannelAndAccountMappingOnce(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, `data: {"type":"response.completed","response":{"id":"resp_alias","status":"completed","model":"gpt-6-astra","output":[],"usage":{"input_tokens":1,"output_tokens":2}}}`+"\n\n")}
	svc := excelBPSGatewayTestService(upstream)
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
	svc := excelBPSGatewayTestService(upstream)
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
	require.Nil(t, turn.result, "local prewarm has no billable result")
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
	svc := excelBPSGatewayTestService(upstream)
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

func TestOpenAIWSBPSBridgeErrorsAndDisconnectUsage(t *testing.T) {
	for _, status := range []int{400, 403, 429, 502, 200} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(status, "PRIVATE_UPSTREAM_MESSAGE")}
			svc := excelBPSGatewayTestService(upstream)
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
			if status == 429 || status >= 500 || status == 200 {
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Empty(t, events, "pre-output rejection must remain available for account failover")
				return
			}
			require.NotEmpty(t, events)
			terminal := events[len(events)-1]
			require.NotContains(t, string(terminal), "PRIVATE_UPSTREAM_MESSAGE")
			if status == 200 {
				require.Equal(t, "error", gjson.GetBytes(terminal, "type").String())
				require.Equal(t, "basispoints_stream_incomplete", gjson.GetBytes(terminal, "error.code").String())
				require.False(t, gjson.GetBytes(terminal, "response.id").Exists())
			} else {
				require.Equal(t, "error", gjson.GetBytes(terminal, "type").String())
				// The WS error frame carries the same client status as HTTP: an
				// upstream 403 is reported as 503, never as a client auth failure.
				require.Equal(t, excelBPSUpstreamAuthClientStatus(status), int(gjson.GetBytes(terminal, "status").Int()))
				var forwardErr *excelBPSForwardError
				require.ErrorAs(t, err, &forwardErr)
				require.Equal(t, status == http.StatusBadRequest, forwardErr.RequestScoped())
			}
		})
	}
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_disconnect", "done"))}
	svc := excelBPSGatewayTestService(upstream)
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

func TestOpenAIWSBPSIngressSwitchToNativeRequiresReconnect(t *testing.T) {
	for _, nextPayload := range []string{
		`{"type":"response.create","model":"alias","input":"hello"}`,
	} {
		t.Run(nextPayload, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, bpsCompletedStream("resp_bps", "hello"))}
			svc := excelBPSGatewayTestService(upstream)
			svc.cfg = passthroughLifecycleConfig()
			account := excelAccount()
			account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
			account.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-sol", "gpt-6-sol": "gpt-6-astra"}
			client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","input":"first"}`, nil)
			defer client.CloseNow()
			readBPSWSTestTurn(t, client)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(nextPayload)))
			go func() {
				for {
					if _, _, err := client.Read(context.Background()); err != nil {
						return
					}
				}
			}()
			select {
			case err := <-serverErr:
				require.ErrorIs(t, err, ErrOpenAIWSModelSwitchRequiresReconnect)
			case <-time.After(3 * time.Second):
				t.Fatal("BPS connection did not stop before a native turn")
			}
			require.Len(t, upstream.requests, 1, "only the BPS turn may use HTTP")
			require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
		})
	}
}

func TestOpenAIWSNativeIngressSwitchToBPSModelRequiresReconnect(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(cfg, upstream)
			svc.settingService = excelBPSSettingsTestService()
			pool := newOpenAIWSConnPool(cfg)
			defer pool.Close()
			pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
			svc.openaiWSPool = pool
			account := excelAccount()
			delete(account.Credentials, "client_id")
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

// ctxBlockingBody stalls a generation until its request context ends.
type ctxBlockingBody struct{ ctx context.Context }

func TestOpenAIWSBPS429NativeFallbackKeepsConnectionOwnership(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		bpsCompletionResponse(429, `{"error":{"code":"rate_limit_exceeded"}}`),
		bpsCompletionResponse(200, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native_owner\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[{\"type\":\"reasoning\",\"id\":\"rs_native\",\"summary\":[],\"encrypted_content\":\"native-only-secret\"}],\"usage\":{\"input_tokens\":10,\"output_tokens\":2}}}\n\n"),
		bpsCompletionResponse(200, bpsCompletedStream("resp_native_next", "continued")),
	}}
	svc := excelBPSGatewayTestService(upstream)
	svc.cfg = passthroughLifecycleConfig()
	account := excelBPSNativeFallbackTestAccount(t, svc)
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	account.Credentials["model_mapping"] = map[string]any{"channel-alias": "gpt-6-astra", "gpt-6-astra": "wrong-double-map"}
	turns := make(chan bpsWSTestTurn, 2)
	hooks := &OpenAIWSIngressHooks{
		MapRequestModel: func(_ int, model string) (string, error) { return "channel-alias", nil },
		AfterTurn:       func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} },
	}
	client, serverErr := startBPSWSMemorySession(t, svc, account, `{"type":"response.create","model":"public-alias","input":"hello"}`, hooks)
	defer client.CloseNow()
	events := readBPSWSTestTurn(t, client)
	turn := <-turns
	require.NoError(t, turn.err)
	require.Equal(t, "resp_native_owner", turn.result.RequestID)
	require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
	require.Len(t, upstream.requests, 2)
	// The next response belongs to native Codex even if BPS becomes available.
	svc.excelBPSCooldownUntil.Delete(account.ID)
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, []byte(`{"type":"response.create","model":"public-alias","previous_response_id":"resp_native_owner","input":[{"role":"user","content":"continue"}]}`)))
	events = readBPSWSTestTurn(t, client)
	turn = <-turns
	require.NoError(t, turn.err)
	require.Len(t, upstream.requests, 3)
	require.Equal(t, "chatgpt.com", upstream.lastReq.URL.Host)
	require.Equal(t, "Bearer native-codex-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Contains(t, string(upstream.lastBody), "native-only-secret")
	require.NotContains(t, string(upstream.lastBody), "previous_response_id")
	require.Equal(t, "/v1/responses", turn.result.UpstreamEndpoint)
	require.Equal(t, "public-alias", gjson.GetBytes(events[len(events)-1], "response.model").String())
	requireBPSWSTestServerExit(t, client, serverErr)
}
