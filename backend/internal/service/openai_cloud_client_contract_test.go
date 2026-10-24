package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	for _, kind := range []string{"http", "ws_warmup", "ws_create"} {
		t.Run(kind, func(t *testing.T) {
			body := []byte(gjson.GetBytes(raw, kind).Raw)
			body, err = sjson.SetRawBytes(body, "tools", []byte(gjson.GetBytes(raw, "tools").Raw))
			require.NoError(t, err)
			svc, in, store, wire := cloudForwardFixture(t, nil)
			in.Account.Credentials["model_mapping"] = map[string]any{"gpt-5.1": "gpt-5.1"}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			ctx, c, recorder := cloudContractContext(t, svc, in, ctx, body)
			if kind != "ws_warmup" {
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
			if kind == "http" {
				result, invokeErr = svc.Forward(ctx, c, in.Account, body)
			} else {
				result, invokeErr = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, in.Account, "unused", body, len(body), "gpt-5.1", "", "", "", "", 1, func(event []byte) error { _, err := fmt.Fprintf(recorder, "data: %s\n\n", event); return err })
			}
			require.NoError(t, invokeErr)
			require.NotNil(t, result)
			require.Equal(t, "completed", cloudContractResponse(recorder.Body.String()).Get("status").String())
			if kind == "ws_warmup" {
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

func TestCloudCanceledDeliveryDoesNotEmitGatewayFailure(t *testing.T) {
	svc, in, _, wire := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reservation := &InflightReservation{ttl: time.Minute}
	reservation.refs.Store(1)
	defer reservation.Release()
	ctx = WithInflightReservation(ctx, reservation)
	body := []byte(`{"model":"gpt-5.1","input":"hello","stream":true}`)
	ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		cloudWireStart(t, ctx, wire)
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
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	body := []byte(`{"model":"client-model","input":"hello","stream":true}`)
	ctx, c, _ := cloudContractContext(t, svc, in, ctx, body)
	writeErr := errors.New("fixture flush failure")
	recorder := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
	replacement, _ := gin.CreateTestContext(recorder)
	c.Writer = replacement.Writer
	done := make(chan struct{})
	defer func() { cancel(); <-done }()
	go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
	result, err := svc.Forward(ctx, c, in.Account, body)
	require.ErrorIs(t, err, writeErr)
	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
	require.Empty(t, GetOpsStreamErrors(c), "client delivery failures must not be reported as upstream failures")
	require.Eventually(t, func() bool { return store.settlementCount() == 1 }, 5*time.Second, 5*time.Millisecond)
}
