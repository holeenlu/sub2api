package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func cloudContractContext(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, ctx context.Context, body []byte) (context.Context, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, err := svc.PrepareCloudContinuation(ctx, in.User.ID, in.APIKey.ID, *in.APIKey.GroupID, body)
	require.NoError(t, err)
	ctx, err = svc.CaptureCloudBillingSnapshot(ctx, in)
	require.NoError(t, err)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body))).WithContext(ctx)
	c.Set("api_key", in.APIKey)
	SetOpenAIHTTPResponseOwner(c, in.User.ID, in.APIKey.ID)
	return ctx, c, recorder
}

func cloudContractResponse(raw string) gjson.Result {
	if json.Valid([]byte(raw)) {
		return gjson.Parse(raw)
	}
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "data: ") {
			event := gjson.Parse(strings.TrimPrefix(line, "data: "))
			if event.Get("type").String() == "response.completed" {
				return event.Get("response")
			}
		}
	}
	return gjson.Result{}
}

func TestCloudCapturedCodexClientEnvelopes(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_0_159_3.json")
	require.NoError(t, err)
	for _, kind := range []string{"http", "ws_warmup", "ws_create", "http_additional", "ws_warmup_additional", "ws_create_additional"} {
		t.Run(kind, func(t *testing.T) {
			transport := strings.TrimSuffix(kind, "_additional")
			body := []byte(gjson.GetBytes(raw, transport).Raw)
			body, err = sjson.SetRawBytes(body, "tools", []byte(gjson.GetBytes(raw, "tools").Raw))
			require.NoError(t, err)
			if transport != kind {
				var request map[string]any
				require.NoError(t, json.Unmarshal(body, &request))
				request["input"] = append([]any{map[string]any{"type": "additional_tools", "role": "developer", "tools": request["tools"]}}, request["input"].([]any)...)
				delete(request, "tools")
				body, err = json.Marshal(request)
				require.NoError(t, err)
			}
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ctx, c, recorder := cloudContractContext(t, svc, in, ctx, body)
			if transport != "ws_warmup" {
				done := make(chan struct{})
				defer func() { cancel(); <-done }()
				go func() {
					defer close(done)
					cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
					require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
					start := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "thread/start", start.Method)
					require.Len(t, gjson.GetBytes(start.Params, "dynamicTools.0.tools").Array(), 106)
					cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
					injected := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "thread/inject_items", injected.Method)
					require.Equal(t, "developer", gjson.GetBytes(injected.Params, "items.0.role").String())
					cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
					turn := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "turn/start", turn.Method)
					require.Equal(t, "auto", gjson.GetBytes(turn.Params, "summary").String())
					cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
					cloudWireComplete(wire)
				}()
			}
			var result *OpenAIForwardResult
			var invokeErr error
			if transport == "http" {
				result, invokeErr = svc.Forward(ctx, c, in.Account, body)
			} else {
				result, invokeErr = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, in.Account, "unused", body, len(body), "gpt-5.1", "", "", "", "", 1, func(event []byte) error { _, err := fmt.Fprintf(recorder, "data: %s\n\n", event); return err })
			}
			require.NoError(t, invokeErr)
			require.NotNil(t, result)
			require.Equal(t, "completed", cloudContractResponse(recorder.Body.String()).Get("status").String())
			if transport == "ws_warmup" {
				require.True(t, result.wsLocalWarmup)
				require.Nil(t, store.snapshot())
				require.Empty(t, wire.writes)
			} else {
				require.Equal(t, 1, store.settlementCount())
			}
		})
	}
}

func TestCloudCompletedTurnInheritsWholeReasoningObject(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
				require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
				cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
				for turn := 1; turn <= 2; turn++ {
					start := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "turn/start", start.Method)
					require.Equal(t, "high", gjson.GetBytes(start.Params, "effort").String())
					require.Equal(t, "auto", gjson.GetBytes(start.Params, "summary").String())
					id := fmt.Sprintf("turn-%d", turn)
					cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": id}}})
					cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": id, "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10 * turn, "outputTokens": 2 * turn, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12 * turn}}}})
					cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": id, "item": map[string]any{"id": "message-" + id, "type": "agentMessage", "text": "ok"}}})
					cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": id, "status": "completed"}}})
				}
			}()
			body := []byte(fmt.Sprintf(`{"model":"client-model","stream":%t,"input":"first","reasoning":{"effort":"high","summary":"auto"},"text":{"verbosity":"low"}}`, stream))
			firstCtx, c, w := cloudContractContext(t, svc, in, ctx, body)
			first, err := svc.Forward(firstCtx, c, in.Account, body)
			require.NoError(t, err)
			require.NotNil(t, first)
			require.True(t, first.SucceededForScheduling(), w.Body.String())
			require.Equal(t, "completed", cloudContractResponse(w.Body.String()).Get("status").String(), w.Body.String())
			id := cloudContractResponse(w.Body.String()).Get("id").String()
			require.NotEmpty(t, id)
			body = []byte(fmt.Sprintf(`{"model":"client-model","stream":%t,"previous_response_id":%q,"input":"next"}`, stream, id))
			nextCtx, c, w := cloudContractContext(t, svc, in, ctx, body)
			next, err := svc.Forward(nextCtx, c, in.Account, body)
			require.NoError(t, err)
			require.NotNil(t, next)
			require.Equal(t, "completed", cloudContractResponse(w.Body.String()).Get("status").String(), w.Body.String())
			require.NotEqual(t, first.RequestID, next.RequestID)
			require.Equal(t, 2, store.settlementCount())
			record := store.snapshot()
			require.Equal(t, "high", gjson.GetBytes(record.BillingSnapshot, "reasoning_effort").String())
		})
	}
}

func TestCloudHistoricalOriginalImageAndPendingToolRetry(t *testing.T) {
	// Reuse the actual image part emitted by codex-cli 0.159.3. The surrounding
	// lifecycle intentionally declares only view_image, not the capture's other tools.
	frames := capturedCodexFrames(t, "http_original")
	var imageURL string
	for _, frame := range frames {
		for _, item := range frame.Get("request.input").Array() {
			if item.Get("type").String() == "function_call_output" && item.Get("output.0.detail").String() == "original" {
				imageURL = item.Get("output.0.image_url").String()
			}
		}
	}
	require.NotEmpty(t, imageURL)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			allowHighReply := make(chan struct{})
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
				require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
				cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
				injected := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/inject_items", injected.Method)
				require.Equal(t, "call_historical", gjson.GetBytes(injected.Params, "items.2.call_id").String())
				require.Equal(t, "original", gjson.GetBytes(injected.Params, "items.2.output.0.detail").String())
				require.Equal(t, imageURL, gjson.GetBytes(injected.Params, "items.2.output.0.image_url").String())
				cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
				cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
				cloudTestRead(wire, map[string]any{"id": "image-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "upstream-call", "namespace": cloudClientToolNamespace, "tool": "view_image", "arguments": map[string]any{"path": "/fixture/current.png"}}})
				select {
				case <-allowHighReply:
				case <-ctx.Done():
					return
				}
				select {
				case reply := <-wire.writes:
					require.Equal(t, "image-rpc", gjson.GetBytes(reply, "id").String())
					require.True(t, gjson.GetBytes(reply, "result.success").Bool())
					require.Len(t, gjson.GetBytes(reply, "result.contentItems").Array(), 1)
					require.Equal(t, "inputImage", gjson.GetBytes(reply, "result.contentItems.0.type").String())
					require.Equal(t, imageURL, gjson.GetBytes(reply, "result.contentItems.0.imageUrl").String())
				case <-ctx.Done():
					t.Error(ctx.Err())
					return
				}
				cloudWireComplete(wire)
			}()
			imagePart := func(detail string) map[string]any {
				return map[string]any{"type": "input_image", "image_url": imageURL, "detail": detail}
			}
			makeBody := func(historicalDetail string, currentCall map[string]any, currentDetail string) []byte {
				t.Helper()
				input := []any{
					map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Read the historical image."}}},
					map[string]any{"type": "function_call", "call_id": "call_historical", "name": "view_image", "arguments": `{"path":"/fixture/history.png"}`},
					map[string]any{"type": "function_call_output", "call_id": "call_historical", "output": []any{imagePart(historicalDetail)}},
					map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "Read the current image too."}}},
				}
				if currentCall != nil {
					input = append(input, currentCall, map[string]any{"type": "function_call_output", "call_id": currentCall["call_id"], "output": []any{imagePart(currentDetail)}})
				}
				body, err := json.Marshal(map[string]any{"model": "client-model", "stream": stream, "input": input, "tools": []any{map[string]any{"type": "function", "name": "view_image", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}}})
				require.NoError(t, err)
				return body
			}
			body := makeBody("original", nil, "")
			requestCtx, c, recorder := cloudContractContext(t, svc, in, ctx, body)
			first, err := svc.Forward(requestCtx, c, in.Account, body)
			require.NoError(t, err)
			require.NotNil(t, first)
			require.False(t, first.SchedulingOutcomeKnown())
			response := cloudContractResponse(recorder.Body.String())
			require.Equal(t, "completed", response.Get("status").String())
			require.Equal(t, "function_call", response.Get("output.0.type").String())
			var currentCall map[string]any
			require.NoError(t, json.Unmarshal([]byte(response.Get("output.0").Raw), &currentCall))
			require.True(t, strings.HasPrefix(currentCall["call_id"].(string), "call_cloud_"))
			pending := store.snapshot()
			require.Equal(t, "awaiting_tools", pending.Status)
			require.Zero(t, store.settlementCount())
			svc.cloudSessions.mu.Lock()
			actor := svc.cloudSessions.actors[pending.SessionID]
			svc.cloudSessions.mu.Unlock()
			require.NotNil(t, actor)
			for _, rejected := range []struct {
				historicalDetail, currentDetail, code string
			}{
				{"original", "original", "cloud_unsupported_parameter"},
				{"high", "high", "cloud_invalid_request"},
			} {
				body = makeBody(rejected.historicalDetail, currentCall, rejected.currentDetail)
				requestCtx, c, recorder = cloudContractContext(t, svc, in, ctx, body)
				result, err := svc.Forward(requestCtx, c, in.Account, body)
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
				require.Equal(t, rejected.code, gjson.Get(recorder.Body.String(), "error.code").String())
				require.Empty(t, wire.writes, "rejection must precede any dynamic-tool RPC reply")
				require.Zero(t, store.settlementCount())
				retained := store.snapshot()
				require.Equal(t, pending.SessionID, retained.SessionID)
				require.Equal(t, pending.RootRequestID, retained.RootRequestID)
				require.Equal(t, "awaiting_tools", retained.Status)
				svc.cloudSessions.mu.Lock()
				retainedActor := svc.cloudSessions.actors[pending.SessionID]
				svc.cloudSessions.mu.Unlock()
				require.Same(t, actor, retainedActor, "a rejected reply must retain the pending actor for correction")
			}
			close(allowHighReply)
			body = makeBody("original", currentCall, "high")
			requestCtx, c, recorder = cloudContractContext(t, svc, in, ctx, body)
			last, err := svc.Forward(requestCtx, c, in.Account, body)
			require.NoError(t, err)
			require.NotNil(t, last)
			require.True(t, last.SucceededForScheduling(), recorder.Body.String())
			require.Equal(t, first.RequestID, last.RequestID)
			require.Equal(t, "completed", cloudContractResponse(recorder.Body.String()).Get("status").String())
			require.Equal(t, 1, store.settlementCount())
			completed := store.snapshot()
			require.Equal(t, pending.SessionID, completed.SessionID)
			require.Equal(t, pending.ThreadID, completed.ThreadID)
			require.Equal(t, "completed", completed.Status)
			require.Empty(t, wire.writes, "correction must not open another upstream turn")
		})
	}
}

func TestCloudCanceledDeliveryDoesNotEmitGatewayFailure(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	in.Account.Credentials["model_mapping"].(map[string]any)["gpt-5.1"] = "gpt-5.1"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reservation := &InflightReservation{ttl: time.Minute}
	reservation.refs.Store(1)
	defer reservation.Release()
	ctx = WithInflightReservation(ctx, reservation)
	body := capturedCodexBody(t, capturedCodexFrames(t, "http_cancel")[0], true)
	ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		cloudCapturedWireStart(t, ctx, wire)
		cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "text", "delta": "partial"}})
	}()
	var events []string
	result, err := svc.proxyOpenAIWSCloudTurn(ctx, c, in.Account, body, "client-model", func(raw []byte) error {
		kind := gjson.GetBytes(raw, "type").String()
		events = append(events, kind)
		if kind == "response.output_text.delta" {
			cancel()
		}
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	require.Contains(t, events, "response.output_text.delta")
	require.NotContains(t, events, "error", "the WS control owner must emit the cancellation, not a 502 error first")
	require.NotContains(t, events, "response.failed")
	require.Empty(t, GetOpsStreamErrors(c))
	require.NotEmpty(t, svc.cloudBillingWorker.holds, "cancellation is not proof of zero consumption")
}

func TestCloudHTTPFlushFailureUsesSharedWriteGuard(t *testing.T) {
	svc, in, store, wire := cloudForwardFixture(t, nil)
	in.Account.Credentials["model_mapping"].(map[string]any)["gpt-5.1"] = "gpt-5.1"
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body := capturedCodexBody(t, capturedCodexFrames(t, "http_cancel")[0], true)
	ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
	writeErr := errors.New("fixture flush failure")
	recorder := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
	replacement, _ := gin.CreateTestContext(recorder)
	c.Writer = replacement.Writer
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() { defer close(done); cloudCapturedWireStart(t, ctx, wire); cloudWireComplete(wire) }()
	result, err := svc.Forward(ctx, c, in.Account, body)
	require.ErrorIs(t, err, writeErr)
	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	require.Empty(t, GetOpsStreamErrors(c), "client delivery failures must not be reported as upstream failures")
	require.Eventually(t, func() bool { return store.settlementCount() == 1 }, 5*time.Second, 5*time.Millisecond)
}

func TestCloudCompletedTurnChangesReasoningAndDoesNotInheritOutputSchema(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			done := make(chan struct{})
			defer func() { cancel(); <-done }()
			go func() {
				defer close(done)
				cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
				require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
				thread := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/start", thread.Method)
				require.True(t, gjson.GetBytes(thread.Params, "ephemeral").Bool())
				cloudTestRead(wire, map[string]any{"id": thread.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
				for turn := 1; turn <= 2; turn++ {
					start := cloudTestMessage(t, ctx, wire)
					require.Equal(t, "turn/start", start.Method)
					if turn == 1 {
						require.Equal(t, "high", gjson.GetBytes(start.Params, "effort").String())
						require.Equal(t, "auto", gjson.GetBytes(start.Params, "summary").String())
						require.Equal(t, "object", gjson.GetBytes(start.Params, "outputSchema.type").String())
					} else {
						require.Equal(t, "low", gjson.GetBytes(start.Params, "effort").String())
						require.Equal(t, "none", gjson.GetBytes(start.Params, "summary").String())
						require.False(t, gjson.GetBytes(start.Params, "outputSchema").Exists())
					}
					id := fmt.Sprintf("turn-%d", turn)
					cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": id}}})
					cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": id, "tokenUsage": map[string]any{"total": map[string]any{"inputTokens": 10 * turn, "outputTokens": 2 * turn, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12 * turn}}}})
					cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": id, "item": map[string]any{"id": "msg-" + id, "type": "agentMessage", "text": "{\"ok\":true}"}}})
					cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": id, "status": "completed"}}})
				}
			}()
			body := []byte(fmt.Sprintf(`{"model":"client-model","stream":%t,"input":"first","reasoning":{"effort":"high","summary":"auto"},"text":{"verbosity":"low","format":{"type":"json_schema","name":"result","strict":true,"schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}}}}`, stream))
			firstCtx, c, w := cloudContractContext(t, svc, in, ctx, body)
			first, err := svc.Forward(firstCtx, c, in.Account, body)
			require.NoError(t, err)
			require.NotNil(t, first)
			response := cloudContractResponse(w.Body.String())
			// Include the response exactly as published, including annotations.
			body = []byte(fmt.Sprintf(`{"model":"client-model","stream":%t,"previous_response_id":%q,"reasoning":{"effort":"low","summary":"none"},"input":[{"role":"user","content":"first"},%s,{"role":"user","content":"next"}]}`, stream, response.Get("id").String(), response.Get("output.0").Raw))
			nextCtx, c, w := cloudContractContext(t, svc, in, ctx, body)
			next, err := svc.Forward(nextCtx, c, in.Account, body)
			require.NoError(t, err, w.Body.String())
			require.NotNil(t, next)
			require.NotEqual(t, first.RequestID, next.RequestID)
			require.Equal(t, 2, store.settlementCount())
			require.Equal(t, "low", gjson.GetBytes(store.snapshot().BillingSnapshot, "reasoning_effort").String())
		})
	}
}

func TestCloudRealAdditionalToolsCatalog(t *testing.T) {
	raw, err := os.ReadFile("testdata/cloud_codex_additional_tools.json")
	require.NoError(t, err)
	for _, client := range []string{"cli159", "desktop"} {
		t.Run(client, func(t *testing.T) {
			request := []byte(gjson.GetBytes(raw, "requests."+client+".request").Raw)
			p, err := parseCloudProtocolRequest(request)
			require.NoError(t, err)
			require.Len(t, p.tools, 13)
			require.True(t, p.customTools["functions__exec"])
			require.Equal(t, "functions", p.names["functions__exec"].Namespace)
			require.Equal(t, "exec", p.names["functions__exec"].Name)
			for _, item := range p.history {
				require.NotEqual(t, "additional_tools", item.(map[string]any)["type"])
			}
			for index, item := range cloudInjectionHistory(p) {
				require.NotContains(t, item, "id", "client IDs must not trigger Cloud persisted-item lookup")
				require.Contains(t, p.history[index], "id", "public history keeps its routing identity")
				require.Equal(t, p.history[index].(map[string]any)["content"], item.(map[string]any)["content"])
			}
			canonical, err := json.Marshal(p.body)
			require.NoError(t, err)
			again, err := parseCloudProtocolRequest(canonical)
			require.NoError(t, err)
			require.Equal(t, p.settings, again.settings)
			require.Equal(t, p.tools, again.tools)
		})
	}
}

func TestCloudAdditionalToolsValidation(t *testing.T) {
	fn := map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "object"}}
	for _, tc := range []struct {
		name  string
		item  map[string]any
		top   any
		valid bool
	}{
		{"matching repeat", map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{fn, fn}}, []any{fn}, true},
		{"empty", map[string]any{"type": "additional_tools", "tools": []any{}}, nil, true},
		{"malformed catalog", map[string]any{"type": "additional_tools", "tools": "bad"}, nil, false},
		{"user role", map[string]any{"type": "additional_tools", "role": "user", "tools": []any{fn}}, nil, false},
		{"extra semantics", map[string]any{"type": "additional_tools", "tools": []any{fn}, "content": "do more"}, nil, false},
		{"malformed top catalog", map[string]any{"type": "additional_tools", "tools": []any{fn}}, "bad", false},
		{"historical declaration superseded", map[string]any{"type": "additional_tools", "tools": []any{map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "string"}}}}, []any{fn}, true},
		{"active carrier conflict", map[string]any{"type": "additional_tools", "tools": []any{fn, map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "string"}}}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"input": []any{tc.item, cloudUserMessage("hello")}}
			if tc.top != nil {
				body["tools"] = tc.top
			}
			p, err := cloudShapeParse(t, body)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Empty(t, p.history)
			if tc.name == "matching repeat" {
				require.Len(t, p.tools, 1)
			}
		})
	}
}

// Keep one tool turn and billing root across both bridge directions, then
// replay the full public history to start a new user turn.
func TestCloudAdditionalCustomToolAcrossTransports(t *testing.T) {
	for _, modes := range [][]string{{"json", "ws", "sse"}, {"ws", "sse", "json"}, {"sse", "json", "ws"}} {
		t.Run(strings.Join(modes, "-"), func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			catalog := []any{map[string]any{"type": "namespace", "name": "functions", "tools": []any{map[string]any{"type": "custom", "name": "exec", "format": map[string]any{"type": "text"}}}}}
			carrier := map[string]any{"type": "additional_tools", "role": "developer", "tools": catalog}
			migrated := cloudCapturedMigratedHistory(t)
			done := make(chan struct{})
			t.Cleanup(func() { cancel(); <-done })
			go func() {
				defer close(done)
				cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
				require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
				start := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "thread/start", start.Method)
				require.Equal(t, "functions__exec", gjson.GetBytes(start.Params, "dynamicTools.0.tools.0.name").String())
				cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}})
				cloudWireResponse(t, ctx, wire, "thread/inject_items", map[string]any{})
				cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
				cloudTestRead(wire, map[string]any{"id": "exec-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn", "callId": "up", "namespace": cloudClientToolNamespace, "tool": "functions__exec", "arguments": map[string]any{"input": "text(7)"}}})
				select {
				case raw := <-wire.writes:
					require.Equal(t, "7", gjson.GetBytes(raw, "result.contentItems.0.text").String())
				case <-ctx.Done():
					return
				}
				cloudWireComplete(wire)
				cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
				cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
			}()
			invoke := func(mode string, items []any, previous string) (*OpenAIForwardResult, gjson.Result) {
				t.Helper()
				body := map[string]any{"model": "gpt-5.1", "input": items, "stream": mode != "json"}
				if previous != "" {
					body["previous_response_id"] = previous
				}
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				requestCtx, c, w := cloudContractContext(t, svc, in, ctx, raw)
				var result *OpenAIForwardResult
				if mode == "ws" {
					result, err = svc.proxyOpenAIWSHTTPBridgeTurn(requestCtx, c, in.Account, "unused", raw, len(raw), "gpt-5.1", "", "", "", "", 1, func(event []byte) error { _, e := fmt.Fprintf(w, "data: %s\n\n", event); return e })
				} else {
					result, err = svc.Forward(requestCtx, c, in.Account, raw)
				}
				require.NoError(t, err, w.Body.String())
				require.NotNil(t, result)
				response := cloudContractResponse(w.Body.String())
				require.Equal(t, "completed", response.Get("status").String(), w.Body.String())
				return result, response
			}
			history := append([]any{carrier}, migrated...)
			first, response := invoke(modes[0], history, "")
			call := response.Get("output.0")
			require.Equal(t, "custom_tool_call", call.Get("type").String())
			require.Equal(t, "functions", call.Get("namespace").String())
			require.Equal(t, "exec", call.Get("name").String())
			require.Equal(t, "text(7)", call.Get("input").String())
			require.Zero(t, store.settlementCount())
			history = append(history, call.Value(), map[string]any{"type": "custom_tool_call_output", "call_id": call.Get("call_id").String(), "output": "7"})
			last, final := invoke(modes[1], history, response.Get("id").String())
			require.Equal(t, first.RequestID, last.RequestID)
			require.Equal(t, 1, store.settlementCount())
			for _, item := range final.Get("output").Array() {
				history = append(history, item.Value())
			}
			history = append(history, cloudUserMessage("Continue"))
			next, _ := invoke(modes[2], history, final.Get("id").String())
			require.NotEqual(t, last.RequestID, next.RequestID)
			require.Equal(t, 2, store.settlementCount())
		})
	}
}
