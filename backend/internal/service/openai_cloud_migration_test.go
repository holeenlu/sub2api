package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Preserve a migrated conversation and queued message roles across tool rounds,
// a duplicate request and a subsequent user turn on the same Cloud thread.
func TestCloudMigratedHistoryToolRepliesCarryQueuedMessages(t *testing.T) {
	for _, mode := range []string{"http", "sse", "ws", "http_to_ws", "ws_to_http", "http_before", "http_to_sse_retry"} {
		for _, role := range []string{"user", "developer", "system"} {
			t.Run(mode+"/"+role, func(t *testing.T) {
				h := newCloudHistoryRoutingHarness(t)
				done := make(chan struct{})
				ctx, cancel := context.WithCancel(h.ctx)
				h.ctx = ctx
				t.Cleanup(func() { cancel(); <-done })
				invoke := cloudMigrationInvoker(t, h, mode)
				go func() {
					defer close(done)
					wire := h.nextWire()
					if wire == nil {
						return
					}
					cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
					cloudWireResponse(t, h.ctx, wire, "thread/inject_items", map[string]any{})
					cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
					for i := 1; i <= 2; i++ {
						cloudTestRead(wire, map[string]any{"id": "rpc-" + strconv.Itoa(i), "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "up-" + strconv.Itoa(i), "namespace": "sub2api_client", "tool": "echo", "arguments": map[string]any{}}})
						inject := cloudTestMessage(t, h.ctx, wire)
						require.Equal(t, "thread/inject_items", inject.Method)
						require.Equal(t, "thread", gjson.GetBytes(inject.Params, "threadId").String())
						upstreamRole := role
						if upstreamRole == "system" {
							upstreamRole = "developer" // Native Codex normalizes the same role.
						}
						require.Equal(t, upstreamRole, gjson.GetBytes(inject.Params, "items.0.role").String())
						require.Equal(t, "queued-"+strconv.Itoa(i), gjson.GetBytes(inject.Params, "items.0.content.0.text").String())
						require.Equal(t, int64(1), gjson.GetBytes(inject.Params, "items.#").Int())
						cloudTestRead(wire, map[string]any{"id": inject.ID, "result": map[string]any{}})
						reply := cloudTestMessage(t, h.ctx, wire)
						require.JSONEq(t, strconv.Quote("rpc-"+strconv.Itoa(i)), string(reply.ID))
					}
					cloudWireCompleteTurn(wire, "thread", "turn", 12)
					cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "next-turn"}})
					cloudWireCompleteTurn(wire, "thread", "next-turn", 24)
				}()
				history := []any{cloudUserMessage("old question"), map[string]any{"type": "function_call", "call_id": "native-call", "name": "echo", "arguments": "{}"}, map[string]any{"type": "function_call_output", "call_id": "native-call", "output": "old result"}, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "old answer"}}}, cloudUserMessage("continue the existing conversation")}
				result, err := invoke(map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools})
				require.NoError(t, err, result.Raw)
				for i := 1; i <= 2; i++ {
					call := result.Get("output.#(type==\"function_call\")")
					require.True(t, call.Exists(), result.Raw)
					for _, item := range result.Get("output").Array() {
						history = append(history, item.Value())
					}
					history = append(history, map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "answer-" + strconv.Itoa(i)}, map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": "input_text", "text": "queued-" + strconv.Itoa(i)}}})
					if mode == "http_before" {
						n := len(history)
						history[n-1], history[n-2] = history[n-2], history[n-1]
					}
					request := map[string]any{"model": "client-model", "input": append([]any{}, history...), "tools": cloudHistoryTools}
					result, err = invoke(request)
					require.NoError(t, err, result.Raw)
					require.Equal(t, "completed", result.Get("status").String(), result.Raw)
					if i == 1 {
						retry, retryErr := invoke(request)
						require.NoError(t, retryErr, retry.Raw)
						require.Equal(t, result.Get("id").String(), retry.Get("id").String(), "a retry must not inject context or reply twice")
					}
				}
				for _, item := range result.Get("output").Array() {
					history = append(history, item.Value())
				}
				history = append(history, cloudUserMessage("continue again"))
				result, err = invoke(map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools})
				require.NoError(t, err, result.Raw)
				require.Equal(t, "completed", result.Get("status").String(), result.Raw)
				<-done
				require.Len(t, h.store.all(), 1, "both turns share one Cloud thread")
				require.Equal(t, 2, h.store.settlementCount(), "two turns, one settlement each")
			})
		}
	}
}

// Exercise the actual client WS ingress, not a direct call to the RPC adapter.
func cloudMigrationInvoker(t *testing.T, h *cloudHistoryRoutingHarness, mode string) func(map[string]any) (gjson.Result, error) {
	mode = strings.TrimSuffix(mode, "_incremental")
	t.Helper()
	var conn *coderws.Conn
	if strings.Contains(mode, "ws") {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			upstream, err := coderws.Accept(w, r, nil)
			if err != nil {
				return
			}
			defer upstream.CloseNow()
			upstream.SetReadLimit(ResolveOpenAIWSClientReadLimitBytes(h.svc.cfg))
			_, first, err := upstream.Read(h.ctx)
			if err != nil {
				return
			}
			c, _ := gin.CreateTestContext(w)
			c.Request = r
			c.Set("api_key", h.in.APIKey)
			SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
			hooks := &OpenAIWSIngressHooks{PrepareCloudTurn: func(ctx context.Context, _ int, _ []byte, _ string) (context.Context, error) {
				return h.svc.CaptureCloudBillingSnapshot(ctx, h.in)
			}}
			_ = h.svc.ProxyResponsesWebSocketFromClient(openAIWSTestAdmissionContext(h.ctx), c, upstream, h.in.Account, "unused", first, hooks)
		}))
		t.Cleanup(server.Close)
		var err error
		conn, _, err = coderws.Dial(h.ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		require.NoError(t, err)
		t.Cleanup(func() { conn.CloseNow() })
	}
	count := 0
	return func(body map[string]any) (gjson.Result, error) {
		count++
		useWS := mode == "ws" || mode == "http_to_ws" && count > 1 || mode == "ws_to_http" && count == 1
		request := make(map[string]any, len(body))
		for key, value := range body {
			request[key] = value
		}
		if !useWS {
			if mode == "sse" || mode == "ws_to_http" || mode == "http_to_sse_retry" && count == 3 {
				request["stream"] = true
			}
			raw, err := json.Marshal(request)
			if err != nil {
				return gjson.Result{}, err
			}
			ctx, c, w := cloudContractContext(t, h.svc, h.in, h.ctx, raw)
			_, err = h.svc.Forward(ctx, c, h.in.Account, raw)
			return cloudContractResponse(w.Body.String()), err
		}
		request["type"] = "response.create"
		raw, err := json.Marshal(request)
		if err != nil {
			return gjson.Result{}, err
		}
		if err = conn.Write(h.ctx, coderws.MessageText, raw); err != nil {
			return gjson.Result{}, err
		}
		for {
			_, raw, err = conn.Read(h.ctx)
			if err != nil {
				return gjson.Result{}, err
			}
			event := gjson.ParseBytes(raw)
			switch event.Get("type").String() {
			case "response.completed":
				return event.Get("response"), nil
			case "error", "response.failed":
				return event, fmt.Errorf("migration WS failed: %s", event.Raw)
			}
		}
	}
}

func TestCloudMigratedToolDiscoveryKeepsHistoryAndBilling(t *testing.T) {
	for _, mode := range []string{"http", "sse", "ws", "http_to_ws", "ws_to_http", "http_incremental", "ws_incremental", "http_to_ws_incremental", "http_namespace_incremental"} {
		t.Run(mode, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			done := make(chan struct{})
			ctx, cancel := context.WithCancel(h.ctx)
			h.ctx = ctx
			t.Cleanup(func() { cancel(); <-done })
			invoke := cloudMigrationInvoker(t, h, mode)
			namespace := strings.Contains(mode, "namespace")
			go func() {
				defer close(done)
				wire := h.nextWire()
				if wire == nil {
					return
				}
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "old"}})
				cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "search"}})
				cloudTestRead(wire, map[string]any{"id": "search-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "old", "turnId": "search", "callId": "search", "namespace": cloudClientToolNamespace, "tool": "tool_search", "arguments": map[string]any{"query": "echo"}}})
				cloudWireResponse(t, h.ctx, wire, "turn/interrupt", map[string]any{})
				cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "old", "turnId": "search", "itemId": "late-context", "delta": "The catalog context must survive interruption."}})
				cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "old", "turnId": "search", "item": map[string]any{"type": "agentMessage", "id": "late-context", "text": "The catalog context must survive interruption."}}})
				cloudTestRead(wire, map[string]any{"method": "rawResponseItem/completed", "params": map[string]any{"threadId": "old", "turnId": "search", "item": map[string]any{"type": "message", "id": "late-context", "role": "assistant", "phase": "commentary", "content": []any{map[string]any{"type": "output_text", "text": "The catalog context must survive interruption."}}}}})
				cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "old", "turnId": "search", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "totalTokens": 12}}}})
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "old", "turn": map[string]any{"id": "search", "status": "interrupted"}}})
				cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
				start := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "thread/start", start.Method)
				require.Equal(t, int64(2), gjson.GetBytes(start.Params, "dynamicTools.0.tools.#").Int())
				cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "new"}}})
				injected := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "thread/inject_items", injected.Method)
				require.Equal(t, "tool_search", gjson.GetBytes(injected.Params, "items.1.name").String())
				require.Contains(t, gjson.GetBytes(injected.Params, "items.2.output").String(), "echo")
				require.Contains(t, string(injected.Params), "The catalog context must survive interruption.")
				cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
				turn := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "turn/start", turn.Method)
				require.Equal(t, "[]", gjson.GetBytes(turn.Params, "input").Raw)
				cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "execute"}}})
				echoName := gjson.GetBytes(start.Params, "dynamicTools.0.tools.1.name").String()
				cloudTestRead(wire, map[string]any{"id": "echo-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "new", "turnId": "execute", "callId": "echo", "namespace": cloudClientToolNamespace, "tool": echoName, "arguments": map[string]any{}}})
				reply := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "\"echo-rpc\"", string(reply.ID))
				cloudWireCompleteTurn(wire, "new", "execute", 20)
			}()
			history := []any{cloudUserMessage("Continue the existing task: find echo and use it.")}
			tools := []any{map[string]any{"type": "tool_search", "execution": "client"}}
			first, err := invoke(map[string]any{"model": "client-model", "input": history, "tools": tools})
			require.NoError(t, err, first.Raw)
			search := first.Get("output.#(type==\"tool_search_call\")")
			require.True(t, search.Exists(), first.Raw)
			require.Equal(t, "client", search.Get("execution").String())
			for _, item := range first.Get("output").Array() {
				history = append(history, item.Value())
			}
			discoveredTools := cloudHistoryTools
			if namespace {
				discoveredTools = []any{map[string]any{"type": "namespace", "name": "client", "tools": cloudHistoryTools}}
			}
			history = append(history, map[string]any{"type": "tool_search_output", "call_id": search.Get("call_id").String(), "execution": "client", "status": "completed", "tools": discoveredTools})
			request := map[string]any{"model": "client-model", "input": history, "tools": tools}
			if strings.HasSuffix(mode, "_incremental") {
				request = map[string]any{"model": "client-model", "previous_response_id": first.Get("id").String(), "input": []any{history[len(history)-1]}}
			}
			routingBody, _ := json.Marshal(map[string]any{"model": "client-model", "input": []any{history[len(history)-1]}})
			routed, routeErr := h.svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, routingBody)
			require.NoError(t, routeErr)
			require.NotNil(t, cloudContinuationFromContext(routed), "tool_search_output must bind its owner before scheduling, even without previous_response_id")
			second, err := invoke(request)
			require.NoError(t, err, second.Raw)
			call := second.Get("output.#(type==\"function_call\")")
			require.Equal(t, "echo", call.Get("name").String(), second.Raw)
			if namespace {
				require.Equal(t, "client", call.Get("namespace").String(), second.Raw)
			}
			retry, err := invoke(request)
			require.NoError(t, err, retry.Raw)
			require.Equal(t, second.Get("id").String(), retry.Get("id").String())
			for _, item := range second.Get("output").Array() {
				history = append(history, item.Value())
			}
			history = append(history, map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "done"})
			final, err := invoke(map[string]any{"model": "client-model", "input": history, "tools": tools})
			require.NoError(t, err, final.Raw)
			<-done
			require.Len(t, h.store.all(), 1)
			require.Equal(t, 1, h.store.settlementCount())
			require.Equal(t, 32, h.store.all()[0].snapshot().Usage.TotalTokens, "one billing root must retain both measured segments")
		})
	}
}

func TestCloudHistoryComparisonUsesJSONArgumentSemantics(t *testing.T) {
	call := map[string]any{"type": "function_call", "call_id": "call", "name": "tool_search", "arguments": "{ \"query\": \"echo\", \"limit\": 1 }"}
	a := &cloudSessionActor{history: []any{call}, tools: map[string]*cloudPendingTool{}}
	copy := map[string]any{"type": "function_call", "call_id": "call", "name": "tool_search", "arguments": "{\"limit\":1,\"query\":\"echo\"}"}
	p := &cloudProtocolRequest{history: []any{copy}}
	require.NoError(t, a.checkHistory(p), "JSON key order and whitespace are not a history edit")
	copy["arguments"] = "{\"limit\":1,\"query\":\"different\"}"
	require.Error(t, a.checkHistory(p), "changed argument values still reject")
}

func TestCloudHistoryComparisonPreservesExactNumbers(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		equal       bool
	}{
		{"1.0", "1", true}, {"1e0", "1.00", true}, {"0.0100e2", "1", true},
		{"-0.0", "0", true}, {"9007199254740993", "9007199254740993.0", true},
		{"9007199254740993", "9007199254740992", false}, {"1.001", "1", false},
		{"1e999999999", "10e999999998", true},
	} {
		t.Run(tc.left+"_"+tc.right, func(t *testing.T) {
			call := func(number string) map[string]any {
				return map[string]any{"type": "function_call", "call_id": "call", "name": "echo", "arguments": "{\"limit\":" + number + "}"}
			}
			a := &cloudSessionActor{history: []any{call(tc.left)}, tools: map[string]*cloudPendingTool{}}
			err := a.checkHistory(&cloudProtocolRequest{history: []any{call(tc.right)}})
			if tc.equal {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, "{\"limit\":"+tc.left+"}", a.history[0].(map[string]any)["arguments"], "comparison must not mutate stored history")
		})
	}
}

func TestCloudCatalogHandoffFailurePreservesConfirmedUsage(t *testing.T) {
	for _, stage := range []string{"missing_usage", "cancelled", "archive", "inject"} {
		t.Run(stage, func(t *testing.T) {
			a, store, wire := cloudHostedActorForTest(t)
			a.service.validate = func(context.Context, OpenAICloudOwner) error {
				if stage == "cancelled" {
					return context.Canceled
				}
				return nil
			}
			if stage == "missing_usage" {
				a.record.Usage = nil
			}
			events := make(chan cloudRPCDelivery, 2)
			a.upstream = events
			done := make(chan struct{})
			go func() {
				defer close(done)
				cloudWireResponse(t, a.ctx, wire, "turn/interrupt", map[string]any{})
				params, _ := json.Marshal(map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}})
				events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: params}}
				if stage == "cancelled" || stage == "missing_usage" {
					return
				}
				archive := cloudTestMessage(t, a.ctx, wire)
				require.Equal(t, "thread/archive", archive.Method)
				if stage == "archive" {
					cloudTestRead(wire, map[string]any{"id": archive.ID, "error": map[string]any{"code": -32602, "message": "invalid"}})
					return
				}
				cloudTestRead(wire, map[string]any{"id": archive.ID, "result": map[string]any{}})
				cloudWireResponse(t, a.ctx, wire, "thread/start", map[string]any{"model": "gpt-test", "thread": map[string]any{"id": "replacement"}})
				inject := cloudTestMessage(t, a.ctx, wire)
				require.Equal(t, "thread/inject_items", inject.Method)
				cloudTestRead(wire, map[string]any{"id": inject.ID, "error": map[string]any{"code": -32602, "message": "invalid"}})
			}()
			err := a.restartPendingTurn(OpenAICloudCommand{RequestID: "http"}, a.protocol, []any{cloudUserMessage("pending context")})
			require.NoError(t, err)
			<-done
			require.Empty(t, wire.writes, "no tool reply or new turn may escape a failed handoff")
			require.Len(t, store.settlements, 1)
			if stage == "missing_usage" {
				require.True(t, store.settlements[0].Unknown)
				require.Nil(t, store.settlements[0].Usage)
			} else {
				require.False(t, store.settlements[0].Unknown)
				require.True(t, store.settlements[0].LowerBound)
				require.Equal(t, 12, store.settlements[0].Usage.TotalTokens)
			}
		})
	}
}

// A handoff has already ended and metered the old inference. An explicit
// ciphertext rejection belongs to the new, still unstarted segment only.
func TestCloudCatalogHandoffRecoversUnstartedEncryptedHistory(t *testing.T) {
	a, store, wire := cloudHostedActorForTest(t)
	a.service.validate = func(context.Context, OpenAICloudOwner) error { return nil }
	a.record.Telemetry = &OpenAICloudTelemetry{}
	a.history = cloudForeignHistory()
	a.turnProducedOutput = true
	events := make(chan cloudRPCDelivery, 1)
	a.upstream = events
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireResponse(t, a.ctx, wire, "turn/interrupt", map[string]any{})
		params, _ := json.Marshal(map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}})
		events <- cloudRPCDelivery{event: cloudRPCMessage{Method: "turn/completed", Params: params}}
		cloudWireResponse(t, a.ctx, wire, "thread/archive", map[string]any{})
		cloudWireResponse(t, a.ctx, wire, "thread/start", map[string]any{"model": "gpt-test", "thread": map[string]any{"id": "replacement"}})
		rejected := cloudTestMessage(t, a.ctx, wire)
		require.Equal(t, "thread/inject_items", rejected.Method)
		cloudTestRead(wire, map[string]any{"id": rejected.ID, "error": map[string]any{"code": -32602, "data": map[string]any{"code": "invalid_encrypted_content"}}})
		cloudWireResponse(t, a.ctx, wire, "thread/archive", map[string]any{})
		cloudWireResponse(t, a.ctx, wire, "thread/start", map[string]any{"model": "gpt-test", "thread": map[string]any{"id": "recovered"}})
		inject := cloudTestMessage(t, a.ctx, wire)
		require.Equal(t, "thread/inject_items", inject.Method)
		require.Contains(t, string(inject.Params), "confirmed tool result")
		cloudTestRead(wire, map[string]any{"id": inject.ID, "result": map[string]any{}})
		cloudWireResponse(t, a.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "new-turn"}})
	}()
	require.NoError(t, a.restartPendingTurn(OpenAICloudCommand{RequestID: "http"}, a.protocol, []any{cloudUserMessage("confirmed tool result")}))
	<-done
	require.Equal(t, "running", a.record.Status)
	require.Equal(t, "recovered", a.record.ThreadID)
	require.Equal(t, "root", a.record.RootRequestID)
	require.Equal(t, 12, a.record.Usage.TotalTokens)
	require.Empty(t, store.settlements, "a handoff is not a second billing root")
	require.Equal(t, 1, a.historyRecoveries)
	require.True(t, a.turnProducedOutput, "whole-root execution proof must not be erased")
	require.NoError(t, cloudHostedEventForTest(t, a, "thread/tokenUsage/updated", map[string]any{"tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 4, "outputTokens": 1, "cachedInputTokens": 0, "totalTokens": 5}}}, ""))
	require.Equal(t, 17, a.record.Usage.TotalTokens)
	require.False(t, a.encryptedHistoryRejectedBeforeInference(&cloudUpstreamError{Code: "invalid_encrypted_content"}, "turn/completed"), "new-segment usage prevents replay")
}

func TestCloudToolSearchContinuationWithoutTools(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"model": "m", "previous_response_id": "resp_cloud_owner", "input": []any{map[string]any{"type": "tool_search_output", "call_id": "call_cloud_search", "execution": "client", "status": "completed", "tools": cloudHistoryTools}}})
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	require.Contains(t, p.outputs, "call_cloud_search")
	_, supplied := p.body["tools"]
	require.False(t, supplied, "owner must inherit its actual declarations; no synthetic catalog is supplied")
	require.Empty(t, p.tools, "lowering a result must not enable a new tool")
}

func TestCloudOriginalToolImageRestartsWithExactHistory(t *testing.T) {
	for _, mode := range []string{"http", "sse", "ws", "http_to_ws", "ws_to_http", "http_unpublished"} {
		t.Run(mode, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			done := make(chan struct{})
			ready := make(chan *cloudRPCFakeConn)
			ctx, cancel := context.WithCancel(h.ctx)
			h.ctx = ctx
			t.Cleanup(func() { cancel(); <-done })
			invoke := cloudMigrationInvoker(t, h, mode)
			imageURL := "data:image/png;base64,aW1hZ2U="
			go func() {
				defer close(done)
				wire := h.nextWire()
				if wire == nil {
					return
				}
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "old"}})
				cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "inspect"}})
				cloudTestRead(wire, map[string]any{"id": "image-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "old", "turnId": "inspect", "callId": "image", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}})
				ready <- wire
				// No image RPC reply may silently drop original detail. End the old
				// inference first, then reuse the existing confirmed same-root handoff.
				cloudWireResponse(t, h.ctx, wire, "turn/interrupt", map[string]any{})
				cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "old", "turnId": "inspect", "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "totalTokens": 12}}}})
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "old", "turn": map[string]any{"id": "inspect", "status": "interrupted"}}})
				cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "new"}})
				injected := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "thread/inject_items", injected.Method)
				require.Equal(t, "original", gjson.GetBytes(injected.Params, "items.2.output.0.detail").String())
				require.Equal(t, imageURL, gjson.GetBytes(injected.Params, "items.2.output.0.image_url").String())
				cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
				cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "finish"}})
				cloudWireCompleteTurn(wire, "new", "finish", 20)
			}()
			first, err := invoke(map[string]any{"model": "client-model", "input": "inspect the screenshot", "tools": cloudHistoryTools})
			require.NoError(t, err, first.Raw)
			call := first.Get("output.#(type==\"function_call\")")
			require.True(t, call.Exists())
			wire := <-ready
			if mode == "http_unpublished" {
				before := h.store.all()[0].snapshot().Sequence
				cloudTestRead(wire, map[string]any{"id": "not-published-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "old", "turnId": "inspect", "callId": "not-published", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}})
				require.Eventually(t, func() bool { return h.store.all()[0].snapshot().Sequence > before }, time.Second, time.Millisecond)
			}
			request := map[string]any{"model": "client-model", "previous_response_id": first.Get("id").String(), "input": []any{map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": []any{map[string]any{"type": "input_image", "detail": "original", "image_url": imageURL}}}}}
			if mode == "http" {
				large := cloneCloudValue(request).(map[string]any)
				large["input"].([]any)[0].(map[string]any)["output"].([]any)[0].(map[string]any)["image_url"] = "data:image/png;base64," + strings.Repeat("A", 4<<20)
				rejected, err := invoke(large)
				require.Error(t, err)
				require.Equal(t, "cloud_history_item_too_large", rejected.Get("error.code").String())
				require.Equal(t, "awaiting_tools", h.store.all()[0].snapshot().Status, "oversized atomic history must not interrupt the existing owner")
				require.Zero(t, h.store.settlementCount())
			}
			final, err := invoke(request)
			require.NoError(t, err, final.Raw)
			require.Equal(t, "completed", final.Get("status").String())
			retry, err := invoke(request)
			require.NoError(t, err, retry.Raw)
			require.Equal(t, final.Get("id").String(), retry.Get("id").String())
			<-done
			require.Equal(t, 1, h.store.settlementCount())
			require.Len(t, h.store.all(), 1)
			require.Equal(t, 32, h.store.all()[0].snapshot().Usage.TotalTokens)
		})
	}
}

func TestCloudNonStrictSchemaUsesTurnScopedGuidance(t *testing.T) {
	for _, mode := range []string{"http", "sse", "ws"} {
		t.Run(mode, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			done := make(chan struct{})
			ctx, cancel := context.WithCancel(h.ctx)
			h.ctx = ctx
			t.Cleanup(func() { cancel(); <-done })
			invoke := cloudMigrationInvoker(t, h, mode)
			go func() {
				defer close(done)
				wire := h.nextWire()
				if wire == nil {
					return
				}
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "schema"}})
				for i, turn := range []string{"first", "second"} {
					request := cloudTestMessage(t, h.ctx, wire)
					require.Equal(t, "turn/start", request.Method)
					require.False(t, gjson.GetBytes(request.Params, "outputSchema").Exists(), "non-strict must not become strict RPC validation")
					if i == 0 {
						require.Contains(t, gjson.GetBytes(request.Params, "input.0.text").String(), "best-effort")
						require.Contains(t, gjson.GetBytes(request.Params, "input.0.text").String(), "rationale")
						require.Equal(t, "answer", gjson.GetBytes(request.Params, "input.1.text").String())
					} else {
						require.Equal(t, int64(1), gjson.GetBytes(request.Params, "input.#").Int())
						require.Equal(t, "next turn", gjson.GetBytes(request.Params, "input.0.text").String())
					}
					cloudTestRead(wire, map[string]any{"id": request.ID, "result": map[string]any{"turn": map[string]any{"id": turn}}})
					cloudWireCompleteTurn(wire, "schema", turn, 12+i*8)
				}
			}()
			schema := map[string]any{"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}, "rationale": map[string]any{"type": "string"}}, "required": []any{"answer"}, "additionalProperties": false}
			first, err := invoke(map[string]any{"model": "client-model", "input": "answer", "text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "codex_output_schema", "strict": false, "schema": schema}}})
			require.NoError(t, err, first.Raw)
			require.False(t, first.Get("text.format.strict").Bool())
			second, err := invoke(map[string]any{"model": "client-model", "previous_response_id": first.Get("id").String(), "input": "next turn"})
			require.NoError(t, err, second.Raw)
			require.False(t, second.Get("text.format").Exists())
			<-done
			require.Equal(t, 2, h.store.settlementCount())
		})
	}
}

// A migrated call's public namespace identity does not change when discovery
// adds/removes its declaration. Catalog-dependent flattening is an RPC detail.
func TestCloudHistoryCatalogChangesPreserveCallIdentity(t *testing.T) {
	catalog := []any{map[string]any{"type": "namespace", "name": "client", "tools": cloudHistoryTools}}
	history := []any{cloudUserMessage("before migration"), map[string]any{"type": "function_call", "call_id": "call_native", "name": "echo", "namespace": "client", "arguments": "{}"}, map[string]any{"type": "function_call_output", "call_id": "call_native", "output": "ok"}}
	for _, added := range []bool{true, false} {
		t.Run(strconv.FormatBool(added), func(t *testing.T) {
			before, after := []any{}, catalog
			if !added {
				before, after = after, before
			}
			parse := func(tools []any) *cloudProtocolRequest {
				raw, err := json.Marshal(map[string]any{"model": "m", "input": history, "tools": tools})
				require.NoError(t, err)
				p, err := parseCloudProtocolRequest(raw)
				require.NoError(t, err)
				return p
			}
			previous, current := parse(before), parse(after)
			actor := &cloudSessionActor{history: previous.history, protocol: previous, tools: map[string]*cloudPendingTool{}}
			require.NoError(t, actor.checkHistory(current))
			// A real identity change still fails, even when arguments and call ID match.
			current = parse(after)
			current.history[1].(map[string]any)["name"] = "different"
			require.Error(t, actor.checkHistory(current))
		})
	}
}

func TestCloudCatalogOrderDoesNotChangeExecutionSettings(t *testing.T) {
	one := map[string]any{"type": "function", "name": "one", "parameters": map[string]any{"type": "object"}}
	two := map[string]any{"type": "function", "name": "two", "parameters": map[string]any{"type": "object"}}
	parse := func(tools []any) *cloudProtocolRequest {
		raw, err := json.Marshal(map[string]any{"model": "m", "input": "hello", "tools": tools})
		require.NoError(t, err)
		p, err := parseCloudProtocolRequest(raw)
		require.NoError(t, err)
		return p
	}
	a, b := parse([]any{one, two}), parse([]any{two, one})
	require.Equal(t, a.settings, b.settings)
	require.Equal(t, a.threadSettings, b.threadSettings)
	require.Equal(t, "two", b.tools[0].(map[string]any)["name"], "comparison must not reorder the actual declaration")
}
