package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These are gateway round trips against protocol-shaped upstream fixtures.
// Source responses pass through their real adapter before becoming target input.
func TestCloudHistoricalConversationSwitchFourDirections(t *testing.T) {
	for _, direction := range [][2]string{{"native", "cloud"}, {"bps", "cloud"}, {"cloud", "native"}, {"cloud", "bps"}} {
		for _, mode := range []string{"http", "sse", "ws"} {
			t.Run(direction[0]+"_to_"+direction[1]+"/"+mode, func(t *testing.T) {
				h := newCloudHistoryRoutingHarness(t)
				var history []any
				if direction[0] == "cloud" {
					_, history, _ = h.establish()
				} else {
					call := map[string]any{"type": "function_call", "id": "fc_source", "call_id": "call_" + direction[0] + "_source", "name": "echo", "arguments": "{}", "status": "completed"}
					if direction[0] == "bps" {
						envelope, _ := json.Marshal(map[string]any{"name": "echo", "arguments": map[string]any{}})
						wrapped, _ := json.Marshal(map[string]any{"code": string(envelope), "summary": "Recorded call", "extended_summary": "A previously requested client tool", "destructive": false, "references": []any{}})
						call["name"], call["arguments"] = "run_officejs", string(wrapped)
					}
					event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_source", "status": "completed", "output": []any{call}, "usage": map[string]any{"input_tokens": 5, "output_tokens": 2}}})
					upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, "data: "+string(event)+"\n\n")}
					svc, account := cloudSwitchTarget(t, h, direction[0], upstream)
					history = []any{cloudUserMessage("original question")}
					result := cloudSwitchInvoke(t, h, svc, account, map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools}, "http")
					tool := result.Get("output.#(type==\"function_call\")")
					require.Equal(t, "echo", tool.Get("name").String(), result.Raw)
					for _, item := range result.Get("output").Array() {
						history = append(history, item.Value())
					}
					history = append(history, map[string]any{"type": "function_call_output", "call_id": tool.Get("call_id").String(), "output": "preserved tool result"})
				}
				history = append(history, cloudUserMessage("continue the same conversation"))
				request := map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools}
				before, _ := json.Marshal(request)
				settlements := h.store.settlementCount()
				if direction[1] == "cloud" {
					h.in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
					finished := make(chan struct{})
					go func() {
						defer close(finished)
						wire := h.nextWire()
						if wire == nil {
							return
						}
						cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "imported"}})
						injected := cloudTestMessage(t, h.ctx, wire)
						require.Equal(t, "thread/inject_items", injected.Method)
						require.Contains(t, string(injected.Params), "original question")
						require.Contains(t, string(injected.Params), "preserved tool result")
						require.Contains(t, string(injected.Params), "call_"+direction[0]+"_source")
						require.NotContains(t, string(injected.Params), "run_officejs", "BPS wire tools must already have become client Responses history")
						cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
						cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "imported-turn"}})
						cloudWireCompleteTurn(wire, "imported", "imported-turn", 12)
					}()
					result, err := cloudMigrationInvoker(t, h, mode)(request)
					require.NoError(t, err, result.Raw)
					require.Equal(t, "completed", result.Get("status").String(), result.Raw)
					<-finished
					require.Equal(t, settlements+1, h.store.settlementCount())
				} else {
					upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, bpsCompletedStream("resp_target", "continued"))}
					svc, account := cloudSwitchTarget(t, h, direction[1], upstream)
					result := cloudSwitchInvoke(t, h, svc, account, request, mode)
					require.Equal(t, "completed", result.Get("status").String(), result.Raw)
					require.Len(t, upstream.requests, 1, "migration is one new target request, not a replay of old execution")
					coverage := AnalyzeToolCallOutputContextCoverageBytes(upstream.lastBody)
					require.True(t, coverage.ContextCoversAllCallIDs)
					require.Contains(t, string(upstream.lastBody), "call_cloud_")
					require.Contains(t, string(upstream.lastBody), "answer")
					require.Contains(t, string(upstream.lastBody), "continue the same conversation")
					require.Equal(t, settlements, h.store.settlementCount(), "exporting history must not settle the old Cloud root again")
					require.Empty(t, h.wires, "export must not reconnect to Cloud")
				}
				after, _ := json.Marshal(request)
				require.JSONEq(t, string(before), string(after), "source history stays canonical and unchanged")
			})
		}
	}
}

func cloudSwitchTarget(t *testing.T, h *cloudHistoryRoutingHarness, protocol string, upstream *httpUpstreamRecorder) (*OpenAIGatewayService, *Account) {
	t.Helper()
	svc := excelBPSGatewayTestService(upstream)
	svc.cloudSessions = h.svc.cloudSessions
	svc.cache = h.svc.cache
	account := newOpenAIOAuthNamespaceTestAccount()
	if protocol == "bps" {
		account = excelAccount()
		account.Extra["openai_excel_bps_models"] = []string{"gpt-5.1"}
	}
	account.ID = h.in.Account.ID
	return svc, account
}

func cloudSwitchInvoke(t *testing.T, h *cloudHistoryRoutingHarness, svc *OpenAIGatewayService, account *Account, request map[string]any, mode string) gjson.Result {
	t.Helper()
	body := cloneCloudValue(request).(map[string]any)
	body["stream"] = mode != "http"
	if mode == "ws" {
		body["type"] = "response.create"
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	ctx, err := svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
	require.NoError(t, err)
	require.Nil(t, cloudContinuationFromContext(ctx), "completed Cloud history does not select the old protocol")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw))).WithContext(ctx)
	c.Set("api_key", h.in.APIKey)
	SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
	var result *OpenAIForwardResult
	if mode == "ws" {
		result, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "unused", raw, len(raw), "gpt-5.1", "", "", "", "", 1, func(event []byte) error { _, e := fmt.Fprintf(recorder, "data: %s\n\n", event); return e })
	} else {
		result, err = svc.Forward(ctx, c, account, raw)
	}
	require.NoError(t, err, recorder.Body.String())
	require.NotNil(t, result)
	return cloudContractResponse(recorder.Body.String())
}

// Use the real Cloud compaction producer, then cross each actual transport,
// including two tool turns and a later user turn, before returning to Cloud.
// Upstreams are local protocol fixtures; this is not a live provider claim.
func TestCloudSummaryTransportRoundTrip(t *testing.T) {
	for _, route := range []string{"native/http", "native/sse", "native/http_ws", "native/sse_ws", "native/ctx_pool", "native/passthrough", "native/http_bridge", "bps/http", "bps/sse", "bps/ws"} {
		for _, cloudMode := range []string{"http", "sse", "ws"} {
			t.Run(route+"/cloud_"+cloudMode, func(t *testing.T) {
				h := newCloudHistoryRoutingHarness(t)
				h.allow(30 * time.Second)
				script := runCloudScript(func() error {
					wire, err := cloudScriptWire(h.ctx, h)
					if err != nil {
						return err
					}
					for _, step := range []struct {
						method string
						result any
					}{
						{"thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "summary-thread"}}},
						{"thread/inject_items", map[string]any{}},
					} {
						if _, err := cloudExpectReply(h.ctx, wire, step.method, step.result); err != nil {
							return err
						}
					}
					if _, err := cloudExpectSummaryTurn(h.ctx, wire, "summary-thread", "summary-turn"); err != nil {
						return err
					}
					cloudWireSummary(wire, "summary-thread", "summary-turn", 12, "Keep ticket 9007199254740993; run echo next.")
					return nil
				})
				summary, err := h.invoke(cloudCompactionFixture(t, "http_pre_turn_compaction"))
				require.NoError(t, err, summary.Raw)
				script.join(t)
				history := []any{cloudOnlyCompaction(t, summary), cloudUserMessage("continue from the summary")}
				settlements := h.store.settlementCount()
				require.Equal(t, 1, settlements)
				invoke, sent := cloudSwitchTransportClient(t, h, route)
				var sourceCall string
				for turn := 0; turn < 3; turn++ {
					request := map[string]any{"model": "gpt-5.1", "store": false, "input": append([]any{}, history...), "tools": cloudHistoryTools}
					canonical, _ := json.Marshal(request)
					result := invoke(request)
					require.Equal(t, "completed", result.Get("status").String(), result.Raw)
					wire := <-sent
					require.NotContains(t, string(wire), cloudCompactionEnvelope)
					require.Contains(t, string(wire), "Keep ticket 9007199254740993; run echo next.")
					if turn > 0 {
						require.Contains(t, string(wire), "portable tool result")
						require.True(t, AnalyzeToolCallOutputContextCoverageBytes(wire).ContextCoversAllCallIDs)
					}
					if turn == 2 {
						require.Contains(t, string(wire), "continue on the target")
					}
					after, _ := json.Marshal(request)
					require.Equal(t, string(canonical), string(after), "outbound conversion cannot mutate canonical history")
					for _, item := range result.Get("output").Array() {
						history = append(history, item.Value())
					}
					if turn == 0 {
						call := result.Get(`output.#(type=="function_call")`)
						require.Equal(t, "echo", call.Get("name").String(), result.Raw)
						require.Equal(t, "9007199254740993", gjson.Get(call.Get("arguments").String(), "ticket").Raw)
						sourceCall = call.Get("call_id").String()
						history = append(history, map[string]any{"type": "function_call_output", "call_id": sourceCall, "output": "portable tool result"})
					} else {
						history = append(history, cloudUserMessage("continue on the target"))
					}
				}
				require.Equal(t, settlements, h.store.settlementCount(), "export does not settle the Cloud summary again")
				h.in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
				script = runCloudScript(func() error {
					wire, err := cloudScriptWire(h.ctx, h)
					if err != nil {
						return err
					}
					if _, err := cloudExpectReply(h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "return-thread"}}); err != nil {
						return err
					}
					injected, err := cloudExpect(h.ctx, wire, "thread/inject_items")
					if err != nil {
						return err
					}
					text := string(injected.Params)
					for _, want := range []string{"Keep ticket 9007199254740993", "portable tool result", sourceCall, "target answer"} {
						if !strings.Contains(text, want) {
							return fmt.Errorf("missing %q from imported history: %s", want, text)
						}
					}
					if strings.Contains(text, "run_officejs") || strings.Contains(text, cloudCompactionEnvelope) {
						return fmt.Errorf("provider envelope leaked: %s", text)
					}
					cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
					if _, err := cloudExpectReply(h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "return-turn"}}); err != nil {
						return err
					}
					cloudTestRead(wire, map[string]any{"id": "return-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "return-thread", "turnId": "return-turn", "callId": "return-call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}})
					select {
					case reply := <-wire.writes:
						if gjson.GetBytes(reply, "id").String() != "return-rpc" || !strings.Contains(string(reply), "Cloud tool result") {
							return fmt.Errorf("wrong tool reply: %s", reply)
						}
					case <-h.ctx.Done():
						return h.ctx.Err()
					}
					cloudWireCompleteTurn(wire, "return-thread", "return-turn", 12)
					if _, err := cloudExpectReply(h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "next-turn"}}); err != nil {
						return err
					}
					cloudWireCompleteTurn(wire, "return-thread", "next-turn", 24)
					return nil
				})
				cloud := cloudMigrationInvoker(t, h, cloudMode)
				result, err := cloud(map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools})
				require.NoError(t, err, result.Raw)
				call := result.Get(`output.#(type=="function_call")`)
				require.Equal(t, "echo", call.Get("name").String(), result.Raw)
				for _, item := range result.Get("output").Array() {
					history = append(history, item.Value())
				}
				history = append(history, map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "Cloud tool result"})
				request := map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools}
				result, err = cloud(request)
				require.NoError(t, err, result.Raw)
				duplicate, err := cloud(request)
				require.NoError(t, err, duplicate.Raw)
				require.Equal(t, result.Get("id").String(), duplicate.Get("id").String())
				for _, item := range result.Get("output").Array() {
					history = append(history, item.Value())
				}
				history = append(history, cloudUserMessage("next Cloud turn"))
				result, err = cloud(map[string]any{"model": "gpt-5.1", "input": history, "tools": cloudHistoryTools})
				require.NoError(t, err, result.Raw)
				script.join(t)
				require.Equal(t, settlements+2, h.store.settlementCount(), "tool continuation and duplicate request settle the root only once")
			})
		}
	}
}

// This uses public WS ingress for all WS modes, not a direct bridge helper.
func cloudSwitchTransportClient(t *testing.T, h *cloudHistoryRoutingHarness, route string) (func(map[string]any) gjson.Result, <-chan []byte) {
	t.Helper()
	parts := strings.Split(route, "/")
	protocol, mode := parts[0], parts[1]
	svc, account := cloudSwitchTarget(t, h, protocol, nil)
	svc.cfg = passthroughLifecycleConfig()
	svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	svc.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 30
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 10
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Concurrency = 10
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	if mode == "http_ws" || mode == "sse_ws" {
		account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	}
	isWS := mode == "ctx_pool" || mode == "passthrough" || mode == "http_bridge" || mode == "ws"
	if isWS {
		account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
	}
	svc.openaiWSResolver = NewOpenAIWSProtocolResolver(svc.cfg)
	svc.toolCorrector = NewCodexToolCorrector()
	sent := make(chan []byte, 3)
	turn := 0
	respond := func(body []byte) string {
		sent <- append([]byte(nil), body...)
		turn++
		var output []any
		if turn == 1 {
			name, args := "echo", `{"ticket":9007199254740993}`
			if protocol == "bps" {
				code := `{"name":"echo","arguments":` + args + `}`
				raw, err := json.Marshal(map[string]any{"code": code, "summary": "echo", "extended_summary": "echo", "destructive": false, "references": []any{}})
				require.NoError(t, err)
				name, args = "run_officejs", string(raw)
			}
			output = []any{map[string]any{"type": "function_call", "id": "fc_target", "call_id": "call_target", "name": name, "arguments": args, "status": "completed"}}
		} else {
			output = []any{map[string]any{"type": "message", "id": fmt.Sprintf("msg_target_%d", turn), "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "target answer"}}}}
		}
		raw, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("resp_target_%d", turn), "status": "completed", "model": "gpt-5.1", "output": output, "usage": map[string]any{"input_tokens": 5, "output_tokens": 2}}})
		require.NoError(t, err)
		return string(raw)
	}
	upstreamWS := protocol == "native" && (mode == "ctx_pool" || mode == "passthrough" || strings.HasSuffix(mode, "_ws"))
	if upstreamWS {
		upstream := newStagedPassthroughConn()
		dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{&nativeCodexStagedConn{upstream}}}
		pool := newOpenAIWSConnPool(svc.cfg)
		pool.setClientDialerForTest(dialer)
		svc.openaiWSPool, svc.openaiWSPassthroughDialer = pool, dialer
		t.Cleanup(pool.Close)
		go func() {
			for i := 0; i < 3; i++ {
				select {
				case body := <-upstream.writes:
					upstream.Send(respond(body))
				case <-h.ctx.Done():
					return
				}
			}
		}()
	} else {
		svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, err
			}
			return bpsCompletionResponse(http.StatusOK, "data: "+respond(body)+"\n\n"), nil
		}}
	}
	var client *coderws.Conn
	turnResults := make(chan bpsWSTestTurn, 3)
	if isWS {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer conn.CloseNow()
			_, first, err := conn.Read(h.ctx)
			if err != nil {
				return
			}
			c, _ := gin.CreateTestContext(w)
			c.Request = r
			c.Set("api_key", h.in.APIKey)
			SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
			SetOpenAIClientTransport(c, OpenAIClientTransportWS)
			_ = svc.ProxyResponsesWebSocketFromClient(h.ctx, c, conn, account, "test-token", first, &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, err error) { turnResults <- bpsWSTestTurn{r, err} }})
		}))
		t.Cleanup(server.Close)
		var err error
		client, _, err = coderws.Dial(h.ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		require.NoError(t, err)
		t.Cleanup(func() { client.CloseNow() })
	}
	return func(request map[string]any) gjson.Result {
		body := cloneCloudValue(request).(map[string]any)
		body["stream"] = mode == "sse" || mode == "sse_ws" || isWS
		if isWS {
			body["type"] = "response.create"
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		if !isWS {
			ctx, err := svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
			require.NoError(t, err)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(raw))).WithContext(ctx)
			c.Set("api_key", h.in.APIKey)
			SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
			SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
			r, err := svc.Forward(ctx, c, account, raw)
			require.NoError(t, err, rec.Body.String())
			require.NotNil(t, r)
			return cloudContractResponse(rec.Body.String())
		}
		require.NoError(t, client.Write(h.ctx, coderws.MessageText, raw))
		events := readNativeWSTestTurn(t, client)
		last := gjson.ParseBytes(events[len(events)-1])
		require.Equal(t, "response.completed", last.Get("type").String(), last.Raw)
		select {
		case turn := <-turnResults:
			require.NoError(t, turn.err)
			require.NotNil(t, turn.result)
			require.Equal(t, 5, turn.result.Usage.InputTokens)
		case <-h.ctx.Done():
			t.Fatal(h.ctx.Err())
		}
		return last.Get("response")
	}, sent
}
