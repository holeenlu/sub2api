//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestCloudEmptyInjectionSkipsUpstreamRPC(t *testing.T) {
	a, _, wire := cloudHostedActorForTest(t)
	a.historyRecoveryItems = nil // also occurs after the only cipher is removed
	done := make(chan bool, 1)
	go func() { done <- a.injectHistory(a.protocol, func() bool { return true }) }()
	select {
	case raw := <-wire.writes:
		cloudTestRead(wire, map[string]any{"id": gjson.GetBytes(raw, "id").Value(), "result": map[string]any{}})
		<-done
		t.Fatal("empty history must not produce thread/inject_items")
	case ok := <-done:
		require.True(t, ok)
	case <-a.ctx.Done():
		t.Fatal(a.ctx.Err())
	}
}

// Exercise the actual gateway/session path, not just the shared history reducer.
func TestCloudExplicitEncryptedHistoryRecovery(t *testing.T) {
	for _, stage := range []string{"thread/inject_items", "turn/start", "turn/completed", "remote_compaction"} {
		t.Run(stage, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			type attempts struct {
				starts, turns int
				wire          *cloudRPCFakeConn
				injections    []string
			}
			done := make(chan attempts, 1)
			go func() {
				wire := h.nextWire()
				got := attempts{wire: wire}
				defer func() { done <- got }()
				thread := ""
				reject := func(msg cloudRPCMessage) {
					cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "message": "rejected", "data": map[string]any{"error": map[string]any{"code": "invalid_encrypted_content"}}}})
				}
				for {
					msg := cloudTestMessage(t, h.ctx, wire)
					switch msg.Method {
					case "thread/start":
						got.starts++
						thread = fmt.Sprintf("encrypted-thread-%d", got.starts)
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": thread}}})
					case "thread/inject_items":
						got.injections = append(got.injections, string(msg.Params))
						if got.starts == 1 && stage == msg.Method {
							reject(msg)
						} else {
							cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
						}
					case "turn/start":
						got.turns++
						if got.starts == 1 && stage == msg.Method {
							reject(msg)
							continue
						}
						turn := fmt.Sprintf("encrypted-turn-%d", got.turns)
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": turn}}})
						if got.starts == 1 && (stage == "turn/completed" || stage == "remote_compaction") {
							failure := map[string]any{"code": "invalid_encrypted_content"}
							if stage == "remote_compaction" {
								cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "auto-compact", "type": "contextCompaction"}}})
								failure = map[string]any{"codexErrorInfo": "other", "message": `Error running remote compact task: {"type":"error","error":{"code":"invalid_encrypted_content"},"status":400}`}
							}
							cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "failed", "error": failure}}})
						} else {
							cloudWireCompleteTurn(wire, thread, turn, 12)
							return
						}
					case "thread/archive":
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
					}
				}
			}()
			body := map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": append(cloudForeignHistory(), cloudUserMessage("continue"))}
			transport := map[string]string{"thread/inject_items": "json", "turn/start": "sse", "turn/completed": "ws", "remote_compaction": "ws"}[stage]
			response, err := cloudRecoveryInvoke(t, h, body, transport)
			require.NoError(t, err, response.Raw)
			require.Equal(t, "completed", response.Get("status").String(), response.Raw)
			got := <-done
			require.Equal(t, 2, got.starts)
			require.Len(t, got.injections, 2)
			require.Contains(t, got.injections[0], "encrypted_content")
			require.NotContains(t, got.injections[1], "encrypted_content")
			require.Equal(t, 4, len(gjson.Get(got.injections[1], "items").Array()))
			require.Contains(t, got.injections[1], "hi there")
			require.Contains(t, got.injections[1], "echoed")
			require.Equal(t, 1, h.store.settlementCount(), "recovery keeps one billing root")
			require.EqualValues(t, 12, response.Get("usage.total_tokens").Int())
			// Keep the original encrypted history for equality checks, and
			// cross transports on the next request without another recovery.
			full := body["input"].([]any)
			for _, item := range response.Get("output").Array() {
				full = append(full, item.Value())
			}
			body["input"] = append(full, cloudUserMessage("next"))
			nextDone := make(chan struct{})
			go func() {
				defer close(nextDone)
				cloudWireResponse(t, h.ctx, got.wire, "turn/start", map[string]any{"turn": map[string]any{"id": "after-recovery"}})
				cloudWireCompleteTurn(got.wire, "encrypted-thread-2", "after-recovery", 22)
			}()
			nextTransport := "json"
			if transport == "json" {
				nextTransport = "ws"
			}
			second, err := cloudRecoveryInvoke(t, h, body, nextTransport)
			require.NoError(t, err)
			require.Equal(t, "completed", second.Get("status").String(), second.Raw)
			require.EqualValues(t, 10, second.Get("usage.total_tokens").Int())
			<-nextDone
			require.Equal(t, 2, h.store.settlementCount())

		})
	}
}

func TestCloudEncryptedRecoverySafetyGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*cloudSessionActor)
	}{
		{"model output", func(a *cloudSessionActor) { a.turnProducedOutput = true }},
		{"observed zero usage", func(a *cloudSessionActor) { a.record.Usage = &OpenAICloudUsage{} }},
		{"metered usage", func(a *cloudSessionActor) { a.record.Usage = &OpenAICloudUsage{TotalTokens: 4} }},
		{"unknown execution", func(a *cloudSessionActor) { a.uncertain = true }},
		{"partial usage", func(a *cloudSessionActor) { a.lowerBound = true }},
		{"model changed", func(a *cloudSessionActor) { a.usageTainted = true }},
		{"upstream compaction", func(a *cloudSessionActor) { a.threadCompacted = true }},
		{"pending tool", func(a *cloudSessionActor) { a.tools = map[string]*cloudPendingTool{"call": {}} }},
		{"public output", func(a *cloudSessionActor) { a.output = []map[string]any{{"type": "message"}} }},
		{"buffered output", func(a *cloudSessionActor) { a.bufferedEvents = []cloudRPCMessage{{Method: "item/agentMessage/delta"}} }},
		{"no active request", func(a *cloudSessionActor) { a.requestID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, _ := cloudHostedActorForTest(t)
			a.record.Usage = nil
			a.historyRecoveryItems = cloudForeignHistory()
			tc.change(a)
			refusal := &cloudUpstreamError{Code: "invalid_encrypted_content"}
			require.False(t, a.encryptedHistoryRejectedBeforeInference(refusal, "turn/completed"))
			require.False(t, a.recoverEncryptedHistory(refusal, "turn/completed", func() bool { t.Fatal("unsafe dispatch"); return true }))
		})
	}
	a, _, _ := cloudHostedActorForTest(t)
	a.record.Usage = nil
	for _, rejection := range []error{errors.New("connection reset"), context.DeadlineExceeded, &cloudUpstreamError{HTTPStatus: 502}, &cloudRPCError{Code: -32600, Upstream: &cloudUpstreamError{Code: "invalid_encrypted_content"}, EventsObserved: true}} {
		require.False(t, a.encryptedHistoryRejectedBeforeInference(rejection, "turn/start"))
	}
}

func TestCloudEncryptedRecoveryDispatchGuards(t *testing.T) {
	for _, mode := range []string{"cancelled", "deadline", "authorization"} {
		t.Run(mode, func(t *testing.T) {
			a, store, _ := cloudHostedActorForTest(t)
			a.record.Usage = nil
			a.historyRecoveryItems = cloudForeignHistory()
			a.historyRecoveryCommand = OpenAICloudCommand{RequestID: a.requestID}
			switch mode {
			case "cancelled":
				a.ctx, a.cancel = context.WithCancel(a.ctx)
				a.cancel()
			case "deadline":
				a.historyRecoveryCommand.Deadline = time.Now().Add(-time.Second)
			case "authorization":
				a.service.validate = func(context.Context, OpenAICloudOwner) error { return errCloudAuthorizationChanged }
			}
			require.False(t, a.recoverEncryptedHistory(&cloudUpstreamError{Code: "invalid_encrypted_content"}, "turn/completed", func() bool { return true }))
			require.Zero(t, a.historyRecoveries)
			require.NotEmpty(t, store.settlements)
		})
	}
}

// The transport observes an event before the RPC error even if the actor has
// not drained Next yet. Such an error cannot prove the request never executed.
func TestCloudEncryptedRecoveryRPCQueuedEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	defer rpc.Close()
	go func() {
		cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
		require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
		msg := cloudTestMessage(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn"}})
		cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "data": map[string]any{"code": "invalid_encrypted_content"}}})
	}()
	require.NoError(t, rpc.Initialize(ctx))
	_, err := rpc.Call(ctx, "turn/start", nil)
	var rejection *cloudRPCError
	require.ErrorAs(t, err, &rejection)
	require.True(t, rejection.EventsObserved)
}

// Reuse the production forwarders and a real local WebSocket connection.
func cloudRecoveryInvoke(t *testing.T, h *cloudHistoryRoutingHarness, body map[string]any, transport string, clientHeaders ...http.Header) (gjson.Result, error) {
	t.Helper()
	if transport == "json" {
		return h.invoke(body, clientHeaders...)
	}
	var events []gjson.Result
	if transport == "ws" {
		ended := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := coderws.Accept(w, r, nil)
			if err != nil {
				ended <- err
				return
			}
			defer conn.CloseNow()
			_, first, err := conn.Read(h.ctx)
			if err != nil {
				ended <- err
				return
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = r
			c.Set("api_key", h.in.APIKey)
			hooks := &OpenAIWSIngressHooks{PrepareCloudTurn: func(ctx context.Context, _ int, _ []byte, _ string) (context.Context, error) {
				return h.svc.CaptureCloudBillingSnapshot(ctx, h.in)
			}}
			ended <- h.svc.ProxyResponsesWebSocketFromClient(h.ctx, c, conn, h.in.Account, "unused", first, hooks)
		}))
		defer server.Close()
		options := &coderws.DialOptions{}
		if len(clientHeaders) > 0 {
			options.HTTPHeader = clientHeaders[0].Clone()
		}
		conn, _, err := coderws.Dial(h.ctx, "ws"+strings.TrimPrefix(server.URL, "http"), options)
		require.NoError(t, err)
		defer conn.CloseNow()
		frame := make(map[string]any, len(body)+1)
		for k, v := range body {
			frame[k] = v
		}
		frame["type"] = "response.create"
		raw, err := json.Marshal(frame)
		require.NoError(t, err)
		require.NoError(t, conn.Write(h.ctx, coderws.MessageText, raw))
		for {
			_, raw, err = conn.Read(h.ctx)
			require.NoError(t, err)
			event := gjson.ParseBytes(raw)
			events = append(events, event)
			if isOpenAIWSTerminalEvent(event.Get("type").String()) || event.Get("type").String() == "error" {
				break
			}
		}
		conn.CloseNow()
		require.NoError(t, <-ended)
	} else {
		rawBody := make(map[string]any, len(body)+1)
		for k, v := range body {
			rawBody[k] = v
		}
		rawBody["stream"] = true
		raw, err := json.Marshal(rawBody)
		require.NoError(t, err)
		ctx, err := h.svc.CaptureCloudBillingSnapshot(h.ctx, h.in)
		require.NoError(t, err)
		ctx, err = h.svc.PrepareCloudContinuation(ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw, clientHeaders...)
		require.NoError(t, err)
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		if len(clientHeaders) > 0 {
			c.Request.Header = clientHeaders[0].Clone()
		}
		c.Set("api_key", h.in.APIKey)
		SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
		_, err = h.svc.Forward(ctx, c, h.in.Account, raw)
		if err != nil {
			return gjson.Result{}, err
		}
		for _, line := range strings.Split(rec.Body.String(), "\n") {
			if strings.HasPrefix(line, "data: ") {
				events = append(events, gjson.Parse(strings.TrimPrefix(line, "data: ")))
			}
		}
	}
	created, completed := 0, 0
	var response gjson.Result
	for _, event := range events {
		switch event.Get("type").String() {
		case "response.created":
			created++
		case "response.completed":
			completed++
			response = event.Get("response")
		case "error", "response.failed":
			t.Fatalf("unexpected public failed attempt: %s", event.Raw)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, 1, completed)
	return response, nil
}

func TestCloudEncryptedRecoveryStagesAndExhaustion(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(fmt.Sprint(exhausted), func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			done := make(chan []string, 1)
			go func() {
				wire := h.nextWire()
				starts := 0
				var injections []string
				defer func() { done <- injections }()
				thread := ""
				for {
					msg := cloudTestMessage(t, h.ctx, wire)
					switch msg.Method {
					case "thread/start":
						starts++
						thread = fmt.Sprintf("staged-thread-%d", starts)
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": thread}}})
						if starts == 3 {
							// A late completion/usage from the refused thread
							// cannot settle the replacement response.
							cloudWireCompleteTurn(wire, "staged-thread-2", "staged-turn-2", 800)
						}
					case "thread/inject_items":
						injections = append(injections, string(msg.Params))
						if starts == 1 {
							cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "data": map[string]any{"code": "invalid_encrypted_content"}}})
						} else {
							cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
						}
					case "turn/start":
						turn := fmt.Sprintf("staged-turn-%d", starts)
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": turn}}})
						if starts == 2 || exhausted {
							code := "invalid_compaction"
							if starts == 3 {
								code = "invalid_encrypted_content"
							}
							cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "failed", "error": map[string]any{"code": code}}}})
						} else {
							cloudWireCompleteTurn(wire, thread, turn, 12)
							return
						}
					case "thread/archive":
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
						if starts == 3 {
							return
						}
					}
				}
			}()
			history := append(cloudForeignHistory(), map[string]any{"type": "compaction", "encrypted_content": "opaque-compaction"}, cloudUserMessage("continue"))
			response, err := h.invoke(map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": history})
			require.NoError(t, err)
			injections := <-done
			require.Len(t, injections, 3, "two reductions across all phases, not two per phase")
			require.Contains(t, injections[1], "opaque-compaction")
			require.NotContains(t, injections[1], "gAAAA-bps-reasoning")
			require.NotContains(t, injections[2], "encrypted_content")
			require.Contains(t, injections[2], openAIOmittedCompactionNotice)
			require.Contains(t, injections[2], "echoed")
			require.Equal(t, 1, h.store.settlementCount())
			if exhausted {
				require.Equal(t, "failed", response.Get("status").String())
				require.Equal(t, "invalid_encrypted_content", response.Get("error.code").String())
				record := h.store.all()[0].snapshot()
				settlement := h.store.all()[0].settlementFor(record.RootRequestID)
				require.False(t, settlement.Unknown)
				require.Zero(t, settlement.Usage.TotalTokens)
			} else {
				require.Equal(t, "completed", response.Get("status").String(), response.Raw)
				require.EqualValues(t, 12, response.Get("usage.total_tokens").Int())
			}
		})
	}
}

func TestCloudEncryptedRecoveryPreviousResponseHistory(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	ready := make(chan *cloudRPCFakeConn, 1)
	go func() {
		wire := h.nextWire()
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "original-thread"}})
		cloudWireResponse(t, h.ctx, wire, "thread/inject_items", map[string]any{})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "original-turn"}})
		cloudWireCompleteTurn(wire, "original-thread", "original-turn", 40)
		ready <- wire
	}()
	first, err := h.invoke(map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": append(cloudForeignHistory(), cloudUserMessage("first-input"))})
	require.NoError(t, err)
	wire := <-ready
	done := make(chan struct{})
	go func() {
		defer close(done)
		rejected := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "turn/start", rejected.Method)
		cloudTestRead(wire, map[string]any{"id": rejected.ID, "error": map[string]any{"code": -32600, "data": map[string]any{"code": "invalid_encrypted_content"}}})
		cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "replacement-thread"}})
		injection := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "thread/inject_items", injection.Method)
		require.Contains(t, string(injection.Params), "first-input")
		require.Contains(t, string(injection.Params), "hi there")
		require.NotContains(t, string(injection.Params), "second-input")
		require.NotContains(t, string(injection.Params), "encrypted_content")
		cloudTestRead(wire, map[string]any{"id": injection.ID, "result": map[string]any{}})
		retry := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "turn/start", retry.Method)
		require.Contains(t, string(retry.Params), "second-input")
		cloudTestRead(wire, map[string]any{"id": retry.ID, "result": map[string]any{"turn": map[string]any{"id": "replacement-turn"}}})
		cloudWireCompleteTurn(wire, "replacement-thread", "replacement-turn", 12)
	}()
	second, err := h.invoke(map[string]any{"model": "client-model", "previous_response_id": first.Get("id").String(), "input": "second-input"})
	require.NoError(t, err)
	require.Equal(t, "completed", second.Get("status").String(), second.Raw)
	require.EqualValues(t, 12, second.Get("usage.total_tokens").Int())
	<-done
	require.Equal(t, 2, h.store.settlementCount())
	require.Len(t, h.store.all(), 1, "the owner and session remain the same")
}
