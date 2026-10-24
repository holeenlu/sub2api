package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type cloudForwardCache struct {
	GatewayCache
	*cloudLiveRoutingStore
}

func cloudForwardFixture(t *testing.T, connectErr error) (*OpenAIGatewayService, *OpenAIRecordUsageInput, *cloudLiveRoutingStore, *cloudRPCFakeConn) {
	t.Helper()
	svc, input, _ := cloudBillingFixture(t)
	f := newVendorOpenAIIdentityFixture(t)
	at, id := f.claims(true), f.claims(false)
	at["client_id"] = openai.ClientID
	id["client_id"] = openai.ClientID
	id["aud"] = []string{openai.ClientID}
	tokens := f.tokens(t, at, id)
	a := input.Account
	a.Credentials = map[string]any{"access_token": tokens.AccessToken, "refresh_token": tokens.RefreshToken, "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339), "chatgpt_account_id": "workspace-signed", "chatgpt_user_id": "user-signed", "model_mapping": map[string]any{"client-model": "gpt-5.1"}}
	a.Extra = map[string]any{OpenAICloudEnabledKey: true}
	repo := &cloudAuthorizationRepoFixture{account: a, binding: &OpenAICloudAuthorizationBinding{Generation: "grant", AccountID: "workspace-signed", UserID: "user-signed", Subject: "provider-subject"}}
	cloud := NewOpenAICloudService(repo, NewOpenAITokenProvider(repo, nil, nil), nil)
	cloud.keys = f.keys
	_, err := cloud.verifyAccess(t.Context(), tokens.AccessToken, "http://fixture-proxy.invalid:8080")
	require.NoError(t, err)
	svc.cloudService = cloud
	cloud.gateway = svc
	store := newCloudLiveRoutingStore()
	svc.cache = &cloudForwardCache{GatewayCache: &stubGatewayCache{}, cloudLiveRoutingStore: store}
	wire := newCloudRPCFakeConn()
	svc.cloudSessions = NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		if connectErr != nil {
			return nil, connectErr
		}
		rpc := newCloudRPC(ctx, wire)
		if err := rpc.Initialize(ctx); err != nil {
			return nil, err
		}
		return rpc, nil
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	svc.cloudBillingWorker = &openAICloudBillingWorker{service: svc, holds: map[string]openAICloudHeldReservation{}}
	svc.httpUpstream = &httpUpstreamRecorder{}
	svc.cfg = newOpenAIWSExecutionScopeTestConfig()
	svc.openaiWSResolver = NewOpenAIWSProtocolResolver(svc.cfg)
	svc.openaiWSStateStore = NewOpenAIWSStateStore(nil)
	svc.toolCorrector = NewCodexToolCorrector()
	t.Cleanup(func() { svc.cloudSessions.Close(); wire.Close() })
	return svc, input, store, wire
}
func cloudWireResponse(t *testing.T, ctx context.Context, wire *cloudRPCFakeConn, method string, result any) {
	t.Helper()
	msg := cloudTestMessage(t, ctx, wire)
	require.Equal(t, method, msg.Method)
	cloudTestRead(wire, map[string]any{"id": msg.ID, "result": result})
}
func cloudWireStart(t *testing.T, ctx context.Context, wire *cloudRPCFakeConn) {
	t.Helper()
	cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
	require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
	cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
	cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
}
func cloudWireComplete(wire *cloudRPCFakeConn) {
	for _, event := range []string{
		`{"method":"thread/tokenUsage/updated","params":{"threadId":"thread","turnId":"turn","tokenUsage":{"total":{"inputTokens":10,"outputTokens":2,"cachedInputTokens":0,"cacheWriteInputTokens":0,"totalTokens":12}}}}`,
		`{"method":"item/started","params":{"threadId":"thread","turnId":"turn","item":{"id":"answer","type":"agentMessage"}}}`,
		`{"method":"item/agentMessage/delta","params":{"threadId":"thread","turnId":"turn","itemId":"answer","delta":"ok"}}`,
		`{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"id":"answer","type":"agentMessage","text":"ok"}}}`,
		`{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"completed"}}}`,
	} {
		wire.reads <- []byte(event)
	}
}

func TestCloudImageRejectionsStayBeforeUpstreamAndBilling(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(cloudProtocolTestPNG(t))
	for _, test := range []struct {
		name    string
		part    map[string]any
		message string
	}{
		{"remote", map[string]any{"type": "input_image", "image_url": "https://example.com/private.png"}, "inline base64 image"},
		{"file", map[string]any{"type": "input_image", "file_id": "file_foreign"}, "file_id ownership"},
		{"low", map[string]any{"type": "input_image", "image_url": url, "detail": "low"}, "image detail low"},
		{"tool_original", map[string]any{"type": "input_image", "image_url": url, "detail": "original"}, "dynamic tool replies cannot preserve"},
	} {
		for _, transport := range []string{"json", "sse", "ws"} {
			t.Run(test.name+"/"+transport, func(t *testing.T) {
				svc, in, store, wire := cloudForwardFixture(t, nil)
				in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
				item := map[string]any{"role": "user", "content": []any{test.part}}
				if test.name == "tool_original" {
					item = map[string]any{"type": "function_call_output", "call_id": "call", "output": []any{test.part}}
				}
				body, err := json.Marshal(map[string]any{"model": "gpt-5.1", "input": []any{item}, "stream": transport != "json"})
				require.NoError(t, err)
				ctx, c, rec := cloudContractContext(t, svc, in, t.Context(), body)
				var result *OpenAIForwardResult
				if transport == "ws" {
					result, err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, in.Account, "unused", body, len(body), "gpt-5.1", "", "", "", "", 1, func(event []byte) error { _, e := rec.Write(event); return e })
				} else {
					result, err = svc.Forward(ctx, c, in.Account, body)
					require.Equal(t, http.StatusBadRequest, rec.Code)
				}
				require.Error(t, err)
				require.Nil(t, result)
				require.Contains(t, rec.Body.String(), "cloud_unsupported_parameter")
				require.Contains(t, rec.Body.String(), test.message)
				require.NotContains(t, rec.Body.String(), "example.com")
				require.Empty(t, wire.writes)
				require.Nil(t, store.snapshot())
				require.Zero(t, store.settlementCount())
				require.Empty(t, svc.cloudBillingWorker.holds)
			})
		}
	}
}

func TestCloudForwardPublicEncodingAndQuotaFailure(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, quota := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/quota=%v", stream, quota), func(t *testing.T) {
				var failure error
				if quota {
					failure = &cloudGatewayError{429, "cloud_quota_exhausted", "Cloud account quota is exhausted"}
				}
				svc, in, _, wire := cloudForwardFixture(t, failure)
				in.Account.Extra["openai_excel_bps"] = true
				svc.settingService = excelBPSSettingsTestService()
				ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
				defer cancel()
				ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
				require.NoError(t, err)
				if !quota {
					done := make(chan struct{})
					t.Cleanup(func() { cancel(); <-done })
					go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
				}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				c.Set("api_key", in.APIKey)
				SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
				result, err := svc.Forward(ctx, c, in.Account, []byte(fmt.Sprintf(`{"model":"client-model","input":"hi","stream":%v,"prompt_cache_key":"client-cache-one","client_metadata":{"session_id":"client-session","turn_id":"client-turn","x-codex-turn-metadata":"{\"thread_id\":\"client-thread\"}"}}`, stream)))
				if quota {
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.Equal(t, CloudPreInferenceFailureReason, failure.Reason)
					require.Equal(t, 429, failure.StatusCode)
					require.False(t, failure.RetryableOnSameAccount)
					require.Nil(t, result)
					require.Empty(t, recorder.Body.String(), "failed response must not commit before failover")
					require.Empty(t, svc.cloudBillingWorker.holds)
					return
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, !quota, result.SucceededForScheduling())
				require.True(t, result.SchedulingOutcomeKnown())
				require.NotNil(t, result.LatencyMetrics)
				if stream {
					require.NotNil(t, result.LatencyMetrics.FirstTextMs)
				} else {
					require.Nil(t, result.LatencyMetrics.FirstTextMs)
				}
				require.Contains(t, recorder.Body.String(), `"model":"client-model"`)
				require.Contains(t, recorder.Body.String(), "completed")
				require.Equal(t, 200, recorder.Code)
			})
		}
	}
}

func TestCloudResponsesWebSocketToolRoundTrip(t *testing.T) {
	for _, transports := range [][2]string{{"ws", "ws"}, {"ws", "json"}, {"ws", "sse"}, {"json", "ws"}, {"sse", "ws"}} {
		t.Run(transports[0]+"_to_"+transports[1], func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			upstreamDone := make(chan struct{})
			t.Cleanup(func() { cancel(); <-upstreamDone })
			go func() {
				defer close(upstreamDone)
				cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
				require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
				thread := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/start", thread.Method)
				require.Equal(t, "low", gjson.GetBytes(thread.Params, "config.model_verbosity").String())
				require.NotContains(t, string(thread.Params), "client-cache")
				cloudTestRead(wire, map[string]any{"id": thread.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
				turn := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "turn/start", turn.Method)
				require.Equal(t, "auto", gjson.GetBytes(turn.Params, "summary").String())
				require.NotContains(t, string(turn.Params), "client-cache")
				cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
				wire.reads <- []byte(`{"id":"tool-rpc","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"upstream-call","namespace":"sub2api_client","tool":"echo","arguments":{"text":"one"}}}`)
				wire.reads <- []byte(`{"id":"tool-rpc-two","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"upstream-call-two","namespace":"sub2api_client","tool":"echo","arguments":{"text":"two"}}}`)
				for _, rpcID := range []string{"tool-rpc", "tool-rpc-two"} {
					select {
					case reply := <-wire.writes:
						require.Equal(t, rpcID, gjson.GetBytes(reply, "id").String())
						require.Contains(t, string(reply), "tool-result")
					case <-ctx.Done():
						t.Error(ctx.Err())
						return
					}
				}
				cloudWireComplete(wire)
			}()
			ended := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					ended <- err
					return
				}
				defer conn.CloseNow()
				_, first, err := conn.Read(ctx)
				if err != nil {
					ended <- err
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r
				c.Set("api_key", in.APIKey)
				hooks := &OpenAIWSIngressHooks{PrepareCloudTurn: func(ctx context.Context, _ int, _ []byte, _ string) (context.Context, error) {
					return svc.CaptureCloudBillingSnapshot(ctx, in)
				}}
				ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.CloseNow()
			invoke := func(transport string, body map[string]any) gjson.Result {
				if transport == "ws" {
					body["type"] = "response.create"
					raw, err := json.Marshal(body)
					require.NoError(t, err)
					require.NoError(t, conn.Write(ctx, coderws.MessageText, raw))
					for {
						_, raw, err := conn.Read(ctx)
						require.NoError(t, err)
						kind := gjson.GetBytes(raw, "type").String()
						require.NotEqual(t, "error", kind, string(raw))
						require.NotEqual(t, "response.failed", kind, string(raw))
						if kind == "response.completed" {
							return gjson.GetBytes(raw, "response")
						}
					}
				}
				if output, ok := body["input"].([]any); ok && transport == "json" {
					item := output[0].(map[string]any)
					body["messages"] = []any{map[string]any{"role": "tool", "tool_call_id": item["call_id"], "content": item["output"]}}
					delete(body, "input")
					delete(body, "previous_response_id") // legacy tool ID alone must pin the original owner
				}
				body["stream"] = transport == "sse"
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				requestCtx, err := svc.PrepareCloudContinuation(ctx, in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, raw)
				require.NoError(t, err)
				if gjson.GetBytes(raw, "messages.0.tool_call_id").Exists() {
					binding := cloudContinuationFromContext(requestCtx)
					require.NotNil(t, binding, "legacy tool continuation must bind before account selection")
					require.Equal(t, in.Account.ID, binding.AccountID)
				}
				requestCtx, err = svc.CaptureCloudBillingSnapshot(requestCtx, in)
				require.NoError(t, err)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				c.Set("api_key", in.APIKey)
				SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
				result, err := svc.Forward(requestCtx, c, in.Account, raw)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, 200, recorder.Code)
				if transport == "sse" {
					for _, line := range strings.Split(recorder.Body.String(), "\n") {
						if strings.HasPrefix(line, "data: ") {
							event := gjson.Parse(strings.TrimPrefix(line, "data: "))
							require.NotEqual(t, "error", event.Get("type").String(), line)
							require.NotEqual(t, "response.failed", event.Get("type").String(), line)
							if event.Get("type").String() == "response.completed" {
								return event.Get("response")
							}
						}
					}
					t.Fatal(recorder.Body.String())
				}
				return gjson.Parse(recorder.Body.String())
			}
			first := invoke(transports[0], map[string]any{
				"model": "client-model", "input": "hi", "prompt_cache_key": "client-cache-one", "parallel_tool_calls": false,
				"reasoning": map[string]any{"summary": "auto"}, "text": map[string]any{"verbosity": "low"},
				"client_metadata": map[string]string{"turn_id": "client-turn-one", "x-codex-installation-id": "private-install"},
				"tools":           []any{map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "object"}}},
			})
			require.Equal(t, "client-model", first.Get("model").String())
			call := first.Get("output.0.call_id").String()
			require.NotEmpty(t, call)
			require.Zero(t, store.settlementCount(), "a tool handoff is not a settled model turn")
			last := invoke(transports[1], map[string]any{
				"model": "client-model", "prompt_cache_key": "client-cache-two",
				"client_metadata":      map[string]string{"turn_id": "client-turn-two"},
				"previous_response_id": first.Get("id").String(),
				"input":                []any{map[string]any{"type": "function_call_output", "call_id": call, "output": "tool-result"}},
			})
			require.Len(t, last.Get(`output.#(type=="function_call")#`).Array(), 1)
			require.Zero(t, store.settlementCount())
			last = invoke(transports[1], map[string]any{
				"model": "client-model", "previous_response_id": last.Get("id").String(),
				"input": []any{map[string]any{"type": "function_call_output", "call_id": last.Get("output.0.call_id").String(), "output": "tool-result-two"}},
			})
			require.Equal(t, "client-model", last.Get("model").String())
			require.Equal(t, int64(12), last.Get("usage.total_tokens").Int())
			telemetry := store.snapshot().Telemetry
			require.NotNil(t, telemetry)
			require.NotNil(t, telemetry.FirstTokenMs)
			require.Equal(t, transports[0] == "ws", telemetry.OpenAIWSMode)
			require.False(t, telemetry.UpstreamStartedAt.IsZero())
			require.Equal(t, 1, store.settlementCount())
			require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-ended:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			<-upstreamDone
		})
	}
}

func TestCloudHTTPToolsAndCachedReplayAcrossRequests(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			done := make(chan struct{})
			t.Cleanup(func() { cancel(); <-done })
			go func() {
				defer close(done)
				cloudWireStart(t, ctx, wire)
				wire.reads <- []byte(`{"id":"tool-rpc","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"call","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
				select {
				case reply := <-wire.writes:
					require.Contains(t, string(reply), "answer")
				case <-ctx.Done():
					return
				}
				cloudWireComplete(wire)
			}()
			invoke := func(body []byte) (*OpenAIForwardResult, gjson.Result) {
				t.Helper()
				requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
				require.NoError(t, err)
				requestCtx, err = svc.PrepareCloudContinuation(requestCtx, in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, body)
				require.NoError(t, err)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				c.Set("api_key", in.APIKey)
				SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
				result, err := svc.forwardOpenAICloud(requestCtx, c, in.Account, body)
				require.NoError(t, err)
				require.NotNil(t, result)
				raw := recorder.Body.String()
				if stream {
					for _, line := range strings.Split(raw, "\n") {
						if strings.HasPrefix(line, "data: ") {
							event := gjson.Parse(strings.TrimPrefix(line, "data: "))
							if event.Get("type").String() == "response.completed" {
								return result, event.Get("response")
							}
						}
					}
					t.Fatal(raw)
				}
				return result, gjson.Parse(raw)
			}
			first, response := invoke([]byte(fmt.Sprintf(`{"model":"client-model","stream":%v,"input":"hello","prompt_cache_key":"client-cache-one","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]}`, stream)))
			require.False(t, first.SchedulingOutcomeKnown())
			require.Equal(t, 0, store.settlementCount())
			follow, _ := json.Marshal(map[string]any{"model": "client-model", "stream": stream, "prompt_cache_key": "client-cache-two", "previous_response_id": response.Get("id").String(), "input": []any{map[string]any{"type": "function_call_output", "call_id": response.Get("output.0.call_id").String(), "output": "answer"}}})
			last, final := invoke(follow)
			require.True(t, last.SucceededForScheduling())
			require.Equal(t, first.RequestID, last.RequestID)
			require.Equal(t, 1, store.settlementCount())
			replay, replayed := invoke(follow)
			require.Equal(t, final.Get("id").String(), replayed.Get("id").String())
			require.Equal(t, last.RequestID, replay.RequestID)
			require.False(t, replay.SchedulingOutcomeKnown())
			require.Equal(t, 1, store.settlementCount())
			valid, err := svc.ValidateOpenAIHTTPResponseOwner(ctx, *in.APIKey.GroupID, final.Get("id").String(), in.User.ID, in.APIKey.ID+1)
			require.NoError(t, err)
			require.False(t, valid)
		})
	}
}

func TestCloudToolContinuationWaitsForBufferedMessageCompletion(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	protocol := make(map[string]any, len(a.protocol.body))
	for key, value := range a.protocol.body {
		protocol[key] = value
	}
	protocol["parallel_tool_calls"] = false
	raw, err := json.Marshal(protocol)
	require.NoError(t, err)
	a.protocol, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	for _, id := range []string{"one", "two"} {
		require.NoError(t, cloudHostedEventForTest(t, a, "item/tool/call", map[string]any{"callId": id, "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}, `"`+id+`"`))
	}
	firstCall := gjson.GetBytes(a.record.Result.Response, "output.0.call_id").String()
	for _, id := range []string{"comment-one", "comment-two"} {
		require.NoError(t, cloudHostedEventForTest(t, a, "item/started", map[string]any{"item": map[string]any{"id": id, "type": "agentMessage"}}, ""))
		require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": id, "delta": id}, ""))
	}
	require.Len(t, a.bufferedEvents, 4)
	continueTool := func(requestID, callID string) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"model": "gpt-test", "previous_response_id": a.lastResponseID, "input": []any{map[string]any{"type": "function_call_output", "call_id": callID, "output": "done"}}})
		require.NoError(t, err)
		require.NoError(t, a.handleCommand(OpenAICloudCommand{RequestID: requestID, Request: OpenAICloudRequest{Owner: a.record.OpenAICloudOwner, Body: body}}))
	}
	before := len(store.results)
	continueTool("first-result", firstCall)
	require.JSONEq(t, `"one"`, string(cloudTestMessage(t, a.ctx, wire).ID))
	require.Empty(t, a.bufferedEvents)
	require.Equal(t, "first-result", a.requestID, "an unfinished buffered message keeps its response open")
	require.Equal(t, "awaiting_tools", a.record.Status)
	for i, id := range []string{"comment-one", "comment-two"} {
		for _, event := range store.results[before:] {
			require.NotEqual(t, "response.completed", gjson.GetBytes(event.Event, "type").String(), "all messages must finish before publishing the next tool")
		}
		require.NoError(t, cloudHostedEventForTest(t, a, "item/agentMessage/delta", map[string]any{"itemId": id, "delta": " done"}, ""))
		require.NoError(t, cloudHostedEventForTest(t, a, "item/completed", map[string]any{"item": map[string]any{"id": id, "type": "agentMessage", "text": id + " done"}}, ""))
		require.NoError(t, cloudHostedEventForTest(t, a, "rawResponseItem/completed", map[string]any{"item": map[string]any{"id": id, "type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": id + " done"}}}}, ""))
		if i == 0 {
			require.Equal(t, "first-result", a.requestID, "completing one message cannot close another unfinished message")
		}
	}
	require.Empty(t, a.requestID)
	response := gjson.ParseBytes(a.record.Result.Response)
	require.Len(t, response.Get(`output.#(type=="message")#`).Array(), 2)
	calls := response.Get(`output.#(type=="function_call")#`).Array()
	require.Len(t, calls, 1)
	require.Empty(t, store.settlements)
	continueTool("second-result", calls[0].Get("call_id").String())
	require.JSONEq(t, `"two"`, string(cloudTestMessage(t, a.ctx, wire).ID))
	require.NoError(t, cloudHostedEventForTest(t, a, "turn/completed", map[string]any{"turn": map[string]any{"id": "turn", "status": "completed"}}, ""))
	var text strings.Builder
	for _, event := range store.results {
		parsed := gjson.ParseBytes(event.Event)
		switch parsed.Get("type").String() {
		case "response.output_text.delta":
			text.WriteString(parsed.Get("delta").String())
		case "response.completed":
			for _, item := range parsed.Get("response.output").Array() {
				require.Equal(t, "completed", item.Get("status").String())
			}
		}
	}
	require.Equal(t, "comment-onecomment-two done done", text.String(), "each delta is published once across tool responses")
	require.Len(t, store.settlements, 1)
	require.False(t, store.settlements[0].Unknown)
}

func TestCloudCloseCompletedPreservesSafeArchiveFailure(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprint(timeout), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() {
				defer close(done)
				cloudWireStart(t, ctx, wire)
				cloudWireComplete(wire)
				msg := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/archive", msg.Method)
				if !timeout {
					cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32000, "message": "private-secret", "data": map[string]any{"error": map[string]any{"code": "rate_limit_exceeded", "message": "private-secret"}}}})
				}
			}()
			defer func() { cancel(); <-done }()
			requestCtx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			result, err := svc.forwardOpenAICloud(requestCtx, c, in.Account, []byte(`{"model":"client-model","input":"hi"}`))
			require.NoError(t, err)
			require.NotNil(t, result)
			record := store.snapshot()
			err = svc.cloudSessions.CloseCompleted(ctx, record.OpenAICloudOwner, record.SessionID)
			require.Error(t, err)
			status, code, message, known := cloudPublicError(err)
			require.True(t, known)
			require.NotContains(t, message, "private-secret")
			if timeout {
				require.Equal(t, 504, status)
				require.Equal(t, "cloud_archive_timeout", code)
			} else {
				require.Equal(t, 429, status)
				require.Equal(t, "rate_limit_exceeded", code)
			}
			require.True(t, store.wasReleased(), "local resource release is independent of archive acknowledgement")
			require.Equal(t, 1, store.settlementCount(), "cleanup does not execute or bill another turn")
		})
	}
}

func TestCloudPreInferenceFailureReachesWSScheduler(t *testing.T) {
	svc, in, store, _ := cloudForwardFixture(t, errors.New("connection unavailable"))
	ctx, cancel := context.WithTimeout(EnsureOpenAITurnBudget(t.Context(), svc.cfg), 5*time.Second)
	defer cancel()
	ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	var events [][]byte
	result, err := svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, []byte(`{"model":"gpt-5.1","input":"hi","stream":true}`), "client-model", func(event []byte) error {
		events = append(events, append([]byte(nil), event...))
		return nil
	})
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, CloudPreInferenceFailureReason, failure.Reason)
	require.True(t, failure.RetryableOnSameAccount)
	require.Nil(t, result)
	require.Empty(t, events, "no error/response ID may escape before the retry controller decides")
	require.False(t, TurnOutputCommitted(ctx))
	require.Empty(t, svc.cloudBillingWorker.holds)
	store.mu.Lock()
	settlements := append([]*OpenAICloudSettlement(nil), store.settlements...)
	store.mu.Unlock()
	require.Len(t, settlements, 1)
	require.False(t, settlements[0].Unknown)
	require.Equal(t, "failed", settlements[0].Status)
	require.Equal(t, &OpenAICloudUsage{}, settlements[0].Usage)
	budget := ctx.Value(openAITurnBudgetKey{}).(*OpenAITurnBudget)
	require.Equal(t, 1, budget.attempts)
	ExcludeTurnRecoveryAccount(ctx, in.Account, failure)
	require.False(t, turnAccountEligible(ctx, in.Account, UsageAuthorizationModeCloud))
	require.True(t, turnAccountEligible(ctx, in.Account, "native"), "Cloud exclusion must not poison other mode state")
	require.True(t, in.Account.IsOpenAICloudEnabled(), "recovery must not change the selected protocol")
}

func TestCloudAcceptedTurnFailureIsNeverReplayed(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	in.Account.Credentials["model_mapping"].(map[string]any)["gpt-5.1"] = "gpt-5.1"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudCapturedWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 0, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 10}}}})
		cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": "serverOverloaded"}}}})
	}()
	defer func() { cancel(); <-done }()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	result, err := svc.Forward(ctx, c, in.Account, capturedCodexBody(t, capturedCodexFrames(t, "ws_partial_error")[2], true))
	var failure *UpstreamFailoverError
	require.False(t, errors.As(err, &failure))
	require.NotNil(t, result)
	require.Contains(t, recorder.Body.String(), "response.failed")
	require.Contains(t, recorder.Body.String(), "server_is_overloaded")
}

func TestCloudExplicitPreInferenceRejectionDefersResponseIdentity(t *testing.T) {
	for _, websocket := range []bool{false, true} {
		t.Run(fmt.Sprint(websocket), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ctx, err := svc.CaptureCloudBillingSnapshot(ctx, in)
			require.NoError(t, err)
			done := make(chan struct{})
			go func() {
				defer close(done)
				cloudWireStart(t, ctx, wire)
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "failed", "error": map[string]any{"codexErrorInfo": "serverOverloaded"}}}})
			}()
			defer func() { cancel(); <-done }()
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Set("api_key", in.APIKey)
			SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
			var result *OpenAIForwardResult
			var events [][]byte
			if websocket {
				result, err = svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, []byte(`{"model":"gpt-5.1","input":"hi","stream":true}`), "client-model", func(e []byte) error { events = append(events, append([]byte(nil), e...)); return nil })
			} else {
				result, err = svc.Forward(ctx, c, in.Account, []byte(`{"model":"client-model","input":"hi","stream":true}`))
			}
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.False(t, failure.RequestNotSent, "turn/start was sent; explicit terminal rejection is the proof")
			require.True(t, failure.RejectedBeforeGeneration)
			require.Nil(t, result)
			require.Empty(t, recorder.Body.String())
			require.Empty(t, events)
			require.True(t, store.snapshot().Result.RejectedBeforeInference)
			require.Empty(t, svc.cloudBillingWorker.holds)
		})
	}
}

func TestCloudWebSocketIngressReturnsRecoverableFailureWithoutWritingTerminal(t *testing.T) {
	svc, in, _, _ := cloudForwardFixture(t, errors.New("connection unavailable"))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ended := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			ended <- err
			return
		}
		defer conn.CloseNow()
		_, first, err := conn.Read(ctx)
		if err != nil {
			ended <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		c.Set("api_key", in.APIKey)
		hooks := &OpenAIWSIngressHooks{PrepareCloudTurn: func(c context.Context, _ int, _ []byte, _ string) (context.Context, error) {
			return svc.CaptureCloudBillingSnapshot(c, in)
		}}
		ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
	}))
	defer server.Close()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"client-model","input":"hi"}`)))
	select {
	case err := <-ended:
		var failure *UpstreamFailoverError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, CloudPreInferenceFailureReason, failure.Reason)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, event, readErr := conn.Read(ctx)
	require.Error(t, readErr)
	require.Empty(t, event, "failed startup must not send a terminal before the handler can switch")
}
