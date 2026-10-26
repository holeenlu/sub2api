//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Codex's own compaction texts (openai/codex rust-v0.164.0-alpha.11,
// codex-rs/prompts/templates/compact/prompt.md and summary_prefix.md).
const (
	testCodexCompactionPrompt = "You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.\n\nInclude:\n- Current progress and key decisions made\n- Important context, constraints, or user preferences\n- What remains to be done (clear next steps)\n- Any critical data, examples, or references needed to continue\n\nBe concise, structured, and focused on helping the next LLM seamlessly continue the work.\n"
	testCodexSummaryPrefix    = "Another language model started to solve this problem and produced a summary of its thinking process. You also have access to the state of the tools that were used by that language model. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:"
)

// cloudCompactionFixture returns one real Codex 0.164 request from the
// loopback capture as an HTTP JSON request for client-model.
func cloudCompactionFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_compaction.json")
	require.NoError(t, err)
	var fixture struct {
		Requests map[string]map[string]any `json:"requests"`
		Tools    []any                     `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	body, ok := fixture.Requests[name]
	require.True(t, ok, name)
	body["tools"] = fixture.Tools
	body["model"] = "client-model"
	body["stream"] = false
	delete(body, "type") // the WS envelope never reaches the Responses adapter
	return body
}

func cloudCompactionInput(body map[string]any) []any { return body["input"].([]any) }

// cloudReplayed returns a published output item the way Codex replays it:
// the public fields plus the attribution metadata of the captured item.
func cloudReplayed(output gjson.Result, captured any) map[string]any {
	item := output.Value().(map[string]any)
	if metadata, ok := captured.(map[string]any)[cloudMessageMetadataKey]; ok {
		item[cloudMessageMetadataKey] = metadata
	}
	return item
}

// cloudOnlyCompaction asserts the Codex v2 acceptance rule (exactly one
// compaction output item) and returns that item as Codex keeps it.
func cloudOnlyCompaction(t *testing.T, response gjson.Result) map[string]any {
	t.Helper()
	var found []gjson.Result
	for _, item := range response.Get("output").Array() {
		if item.Get("type").String() == "compaction" {
			found = append(found, item)
		}
	}
	require.Len(t, found, 1, response.Raw)
	require.NotEmpty(t, found[0].Get("encrypted_content").String(), response.Raw)
	return map[string]any{"type": "compaction", "id": found[0].Get("id").String(), "encrypted_content": found[0].Get("encrypted_content").String()}
}

// A fake App Server script runs beside the request and reports to the test
// only when joined, so a failed request shows its own error instead of a
// script failing after the test ended.
type cloudScript chan error

func runCloudScript(script func() error) cloudScript {
	done := make(cloudScript, 1)
	go func() { done <- script() }()
	return done
}

func (s cloudScript) join(t *testing.T) {
	t.Helper()
	require.NoError(t, <-s)
}

func cloudExpect(ctx context.Context, wire *cloudRPCFakeConn, method string) (cloudRPCMessage, error) {
	select {
	case raw := <-wire.writes:
		var msg cloudRPCMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return msg, err
		}
		if msg.Method != method {
			return msg, fmt.Errorf("expected %s, got %s: %s", method, msg.Method, raw)
		}
		return msg, nil
	case <-ctx.Done():
		return cloudRPCMessage{}, fmt.Errorf("waiting for %s: %w", method, ctx.Err())
	}
}

func cloudExpectReply(ctx context.Context, wire *cloudRPCFakeConn, method string, result any) (cloudRPCMessage, error) {
	msg, err := cloudExpect(ctx, wire, method)
	if err == nil {
		cloudTestRead(wire, map[string]any{"id": msg.ID, "result": result})
	}
	return msg, err
}

func cloudScriptWire(ctx context.Context, h *cloudHistoryRoutingHarness) (*cloudRPCFakeConn, error) {
	select {
	case wire := <-h.wires:
		if _, err := cloudExpectReply(ctx, wire, "initialize", map[string]any{}); err != nil {
			return nil, err
		}
		_, err := cloudExpect(ctx, wire, "initialized")
		return wire, err
	case <-ctx.Done():
		return nil, fmt.Errorf("no new Cloud connection: %w", ctx.Err())
	}
}

func cloudCheck(ok bool, format string, args ...any) error {
	if ok {
		return nil
	}
	return fmt.Errorf(format, args...)
}

func cloudWireSummary(wire *cloudRPCFakeConn, thread, turn string, total int, text string) {
	usage := map[string]any{"inputTokens": total - 2, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": total}
	cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": thread, "turnId": turn, "tokenUsage": map[string]any{"total": usage}}})
	cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "summary-" + turn, "type": "agentMessage"}}})
	cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": thread, "turnId": turn, "itemId": "summary-" + turn, "delta": text}})
	cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "summary-" + turn, "type": "agentMessage", "text": text}}})
	cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "completed"}}})
}

// cloudExpectSummaryTurn accepts the summary turn/start on thread and checks it
// asks with Codex's own compaction prompt as its final input.
func cloudExpectSummaryTurn(ctx context.Context, wire *cloudRPCFakeConn, thread, turn string) (cloudRPCMessage, error) {
	start, err := cloudExpect(ctx, wire, "turn/start")
	if err != nil {
		return start, err
	}
	input := gjson.GetBytes(start.Params, "input").Array()
	if err := cloudCheck(gjson.GetBytes(start.Params, "threadId").String() == thread && len(input) > 0, "summary turn on another thread or without input: %s", start.Params); err != nil {
		return start, err
	}
	last := input[len(input)-1]
	if err := cloudCheck(last.Get("type").String() == "text" && last.Get("text").String() == testCodexCompactionPrompt, "summary turn does not end with Codex's compaction prompt: %s", start.Params); err != nil {
		return start, err
	}
	cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": turn}}})
	return start, nil
}

// cloudExpectFreshTurn opens the next connection's thread, checks the injected
// history, and starts the turn whose first input text is want.
func cloudExpectFreshTurn(ctx context.Context, h *cloudHistoryRoutingHarness, injected func([]gjson.Result) error, want string) (*cloudRPCFakeConn, error) {
	next, err := cloudScriptWire(ctx, h)
	if err != nil {
		return nil, err
	}
	if _, err := cloudExpectReply(ctx, next, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread-two"}}); err != nil {
		return next, err
	}
	inject, err := cloudExpect(ctx, next, "thread/inject_items")
	if err != nil {
		return next, err
	}
	if err := injected(gjson.GetBytes(inject.Params, "items").Array()); err != nil {
		return next, err
	}
	cloudTestRead(next, map[string]any{"id": inject.ID, "result": map[string]any{}})
	start, err := cloudExpect(ctx, next, "turn/start")
	if err != nil {
		return next, err
	}
	if err := cloudCheck(gjson.GetBytes(start.Params, "input.0.text").String() == want, "turn/start input %s, want %q", start.Params, want); err != nil {
		return next, err
	}
	cloudTestRead(next, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-two"}}})
	cloudWireCompleteTurn(next, "thread-two", "turn-two", 30)
	return next, nil
}

// Codex 0.164 compacts before a turn once its context estimate passes the
// limit: the whole history plus a final compaction_trigger over HTTP, or only
// the trigger after the last response on a hot WebSocket. Cloud answers with a
// summary turn on the live thread and returns it as the single compaction item
// Codex requires; the compacted conversation then continues on a new thread
// seeded with that summary, and later turns reuse that thread.
func TestCloudCodexPreTurnCompactionContinuesFromSummary(t *testing.T) {
	for _, shape := range []string{"full_history", "hot_websocket_previous"} {
		t.Run(shape, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			var wire *cloudRPCFakeConn
			script := runCloudScript(func() (err error) {
				if wire, err = cloudScriptWire(h.ctx, h); err != nil {
					return err
				}
				for _, step := range []struct {
					method string
					result any
				}{{"thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}}, {"thread/inject_items", map[string]any{}}, {"turn/start", map[string]any{"turn": map[string]any{"id": "turn"}}}} {
					if _, err = cloudExpectReply(h.ctx, wire, step.method, step.result); err != nil {
						return err
					}
				}
				cloudWireCompleteTurn(wire, "thread", "turn", 12)
				return nil
			})
			first, err := h.invoke(cloudCompactionFixture(t, "http_pre_turn_turn"))
			require.NoError(t, err, first.Raw)
			require.Equal(t, "completed", first.Get("status").String(), first.Raw)
			script.join(t)

			compaction := cloudCompactionFixture(t, "http_pre_turn_compaction")
			input := cloudCompactionInput(compaction)
			input[4] = cloudReplayed(first.Get("output.0"), input[4])
			if shape == "hot_websocket_previous" {
				compaction = cloudCompactionFixture(t, "ws_hot_pre_turn_compaction")
				compaction["previous_response_id"] = first.Get("id").String()
			}
			script = runCloudScript(func() error {
				if _, err := cloudExpectSummaryTurn(h.ctx, wire, "thread", "turn-compact"); err != nil {
					return err
				}
				cloudWireSummary(wire, "thread", "turn-compact", 40, "Greeted the user; nothing is pending.")
				return nil
			})
			compacted, err := h.invoke(compaction)
			require.NoError(t, err, compacted.Raw)
			script.join(t)
			require.Equal(t, "completed", compacted.Get("status").String(), compacted.Raw)
			require.Equal(t, int64(28), compacted.Get("usage.total_tokens").Int(), "the summary turn is billed like any turn")
			h.noNewWire(100 * time.Millisecond)
			require.Equal(t, 2, h.store.settlementCount())
			item := cloudOnlyCompaction(t, compacted)

			// Codex replaces the summarized turns by that item and sends the next
			// user message after it.
			after := cloudCompactionFixture(t, "http_pre_turn_after")
			if shape == "hot_websocket_previous" {
				after = cloudCompactionFixture(t, "ws_hot_after")
			}
			afterInput := cloudCompactionInput(after)
			require.Equal(t, "compaction", afterInput[4].(map[string]any)["type"])
			afterInput[4] = item
			var next *cloudRPCFakeConn
			script = runCloudScript(func() (err error) {
				next, err = cloudExpectFreshTurn(h.ctx, h, func(items []gjson.Result) error {
					if len(items) != 5 {
						return fmt.Errorf("injected %d items: %v", len(items), items)
					}
					summary := items[4]
					return cloudCheck(summary.Get("type").String() == "message" && summary.Get("role").String() == "user" &&
						summary.Get("content.0.text").String() == testCodexSummaryPrefix+"\nGreeted the user; nothing is pending." &&
						!strings.Contains(fmt.Sprint(items), item["encrypted_content"].(string)),
						"the summary is not injected as Codex injects its own summaries: %v", items)
				}, "Second question.")
				return err
			})
			second, err := h.invoke(after)
			require.NoError(t, err, second.Raw)
			script.join(t)
			require.Equal(t, "completed", second.Get("status").String(), second.Raw)
			if shape == "hot_websocket_previous" {
				return
			}

			// The following full-history turn replays the compaction item unchanged
			// and continues the seeded thread instead of injecting it again.
			later := cloudCompactionFixture(t, "http_pre_turn_next")
			laterInput := cloudCompactionInput(later)
			laterInput[4] = item
			laterInput[6] = cloudReplayed(second.Get("output.0"), laterInput[6])
			script = runCloudScript(func() error {
				start, err := cloudExpect(h.ctx, next, "turn/start")
				if err != nil {
					return err
				}
				if err := cloudCheck(gjson.GetBytes(start.Params, "input.0.text").String() == "Third question.", "unexpected turn: %s", start.Params); err != nil {
					return err
				}
				cloudTestRead(next, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-three"}}})
				cloudWireCompleteTurn(next, "thread-two", "turn-three", 50)
				return nil
			})
			third, err := h.invoke(later)
			require.NoError(t, err, third.Raw)
			script.join(t)
			require.Equal(t, "completed", third.Get("status").String(), third.Raw)
			h.noNewWire(100 * time.Millisecond)
		})
	}
}

// Codex also compacts mid-turn, after running a tool: it sends the tool result
// together with the trigger instead of as the reply (full history over HTTP,
// previous_response_id plus the result over WebSocket), then continues the turn
// from the compacted history, whose last item is the compaction. App Server
// cannot add input to a turn that waits for a tool, so Cloud ends that wait and
// summarizes on the same thread with the result as input. The billing root and
// its single settlement span both upstream turns.
func TestCloudCodexMidTurnCompactionSummarizesThePendingTurn(t *testing.T) {
	for _, shape := range []string{"full_history", "websocket_previous"} {
		t.Run(shape, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			var wire *cloudRPCFakeConn
			script := runCloudScript(func() (err error) {
				if wire, err = cloudScriptWire(h.ctx, h); err != nil {
					return err
				}
				for _, step := range []struct {
					method string
					result any
				}{{"thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}}, {"thread/inject_items", map[string]any{}}, {"turn/start", map[string]any{"turn": map[string]any{"id": "turn"}}}} {
					if _, err = cloudExpectReply(h.ctx, wire, step.method, step.result); err != nil {
						return err
					}
				}
				usage := map[string]any{"inputTokens": 10, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 12}
				cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn", "tokenUsage": map[string]any{"total": usage}}})
				wire.reads <- []byte(`{"id":"tool-rpc","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"upstream-call","namespace":"sub2api_client","tool":"exec_command","arguments":{"cmd":"printf compact-capture"}}}`)
				return nil
			})
			call, err := h.invoke(cloudCompactionFixture(t, "http_mid_turn_call"))
			require.NoError(t, err, call.Raw)
			script.join(t)
			require.Equal(t, "function_call", call.Get("output.0.type").String(), call.Raw)
			require.Equal(t, "exec_command", call.Get("output.0.name").String(), call.Raw)
			require.Equal(t, int64(12), call.Get("usage.total_tokens").Int(), "the step confirmed before the call is reported with it")
			callID := call.Get("output.0.call_id").String()

			compaction := cloudCompactionFixture(t, "http_mid_turn_compaction")
			input := cloudCompactionInput(compaction)
			input[4] = cloudReplayed(call.Get("output.0"), input[4])
			input[5].(map[string]any)["call_id"] = callID
			if shape == "websocket_previous" {
				compaction = cloudCompactionFixture(t, "ws_mid_turn_compaction")
				compaction["previous_response_id"] = call.Get("id").String()
				cloudCompactionInput(compaction)[0].(map[string]any)["call_id"] = callID
			}
			script = runCloudScript(func() error {
				// The pending tool RPC is not answered: the turn waiting for it ends.
				interrupt, err := cloudExpectReply(h.ctx, wire, "turn/interrupt", map[string]any{})
				if err != nil {
					return err
				}
				if err := cloudCheck(gjson.GetBytes(interrupt.Params, "turnId").String() == "turn", "interrupted another turn: %s", interrupt.Params); err != nil {
					return err
				}
				cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}}})
				start, err := cloudExpectSummaryTurn(h.ctx, wire, "thread", "turn-compact")
				if err != nil {
					return err
				}
				if err := cloudCheck(strings.Contains(string(start.Params), "compact-capture"), "the client's tool result is not summarized: %s", start.Params); err != nil {
					return err
				}
				cloudWireSummary(wire, "thread", "turn-compact", 40, "Ran printf; it printed compact-capture.")
				return nil
			})
			compacted, err := h.invoke(compaction)
			require.NoError(t, err, compacted.Raw)
			script.join(t)
			require.Equal(t, "completed", compacted.Get("status").String(), compacted.Raw)
			item := cloudOnlyCompaction(t, compacted)
			require.Equal(t, int64(28), compacted.Get("usage.total_tokens").Int(), "the compaction reports its summary step")
			require.Equal(t, 1, h.store.settlementCount(), "no second billing root is opened mid-turn")
			session := h.store.all()[0]
			settlement := session.snapshotSettlement(session.snapshot().RootRequestID)
			require.NotNil(t, settlement)
			require.Equal(t, "completed", settlement.Status, "the pending turn ends completed, not abandoned")
			require.False(t, settlement.Unknown || settlement.LowerBound, "both upstream turns reported their usage")
			require.Equal(t, 40, settlement.Usage.TotalTokens)

			after := cloudCompactionFixture(t, "http_mid_turn_after")
			if shape == "websocket_previous" {
				after = cloudCompactionFixture(t, "ws_mid_turn_after")
			}
			afterInput := cloudCompactionInput(after)
			require.Equal(t, "compaction", afterInput[len(afterInput)-1].(map[string]any)["type"])
			afterInput[len(afterInput)-1] = item
			script = runCloudScript(func() error {
				_, err := cloudExpectFreshTurn(h.ctx, h, func(items []gjson.Result) error {
					return cloudCheck(len(items) == 4, "injected %d items: %v", len(items), items)
				}, testCodexSummaryPrefix+"\nRan printf; it printed compact-capture.")
				return err
			})
			continued, err := h.invoke(after)
			require.NoError(t, err, continued.Raw)
			script.join(t)
			require.Equal(t, "completed", continued.Get("status").String(), continued.Raw)
		})
	}
}

// The summary turn runs on the thread that registered the client's tools.
// Like Codex's local compaction, which offers no tools, a summary turn never
// hands a tool call to the client: Cloud declines it and the model goes on to
// write the summary.
func TestCloudCompactionDeclinesToolCalls(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, _ := h.establish()
	script := runCloudScript(func() error {
		if _, err := cloudExpectSummaryTurn(h.ctx, wire, "thread", "turn-compact"); err != nil {
			return err
		}
		wire.reads <- []byte(`{"id":"compact-tool","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn-compact","callId":"upstream-compact-call","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
		var reply []byte
		select {
		case reply = <-wire.writes:
		case <-h.ctx.Done():
			return fmt.Errorf("the declined tool RPC was never answered: %w", h.ctx.Err())
		}
		if err := cloudCheck(gjson.GetBytes(reply, "id").Raw == `"compact-tool"` && gjson.GetBytes(reply, "result.success").Exists() && !gjson.GetBytes(reply, "result.success").Bool() &&
			strings.Contains(gjson.GetBytes(reply, "result.contentItems.0.text").String(), "summary"), "tool call was not declined: %s", reply); err != nil {
			return err
		}
		cloudWireSummary(wire, "thread", "turn-compact", 40, "Echo was called once.")
		return nil
	})
	body := map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": append(append([]any{}, history...), map[string]any{"type": "compaction_trigger"})}
	compacted, err := h.invoke(body)
	require.NoError(t, err, compacted.Raw)
	script.join(t)
	cloudOnlyCompaction(t, compacted)
	for _, item := range compacted.Get("output").Array() {
		require.NotEqual(t, "function_call", item.Get("type").String(), "a declined call is not handed to the client")
	}
}

// Codex reads compaction over SSE or the client WebSocket, where it keeps the
// output_item.done compaction item and requires response.completed.
func TestCloudCompactionStreamsOneCompactionItem(t *testing.T) {
	for _, transport := range []string{"http_sse", "websocket_bridge"} {
		t.Run(transport, func(t *testing.T) {
			svc, in, store, wire := cloudForwardFixture(t, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			script := runCloudScript(func() error {
				if _, err := cloudExpectReply(ctx, wire, "initialize", map[string]any{}); err != nil {
					return err
				}
				if _, err := cloudExpect(ctx, wire, "initialized"); err != nil {
					return err
				}
				for _, step := range []struct {
					method string
					result any
				}{{"thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}}}, {"thread/inject_items", map[string]any{}}, {"turn/start", map[string]any{"turn": map[string]any{"id": "turn"}}}} {
					if _, err := cloudExpectReply(ctx, wire, step.method, step.result); err != nil {
						return err
					}
				}
				cloudWireCompleteTurn(wire, "thread", "turn", 12)
				if _, err := cloudExpectSummaryTurn(ctx, wire, "thread", "turn-compact"); err != nil {
					return err
				}
				cloudWireSummary(wire, "thread", "turn-compact", 40, "Greeted the user.")
				return nil
			})
			send := func(body map[string]any) string {
				t.Helper()
				body["stream"] = true
				if transport == "websocket_bridge" {
					body["model"] = "gpt-5.1" // the WS ingress maps the model before the bridge
				}
				raw, err := json.Marshal(body)
				require.NoError(t, err)
				requestCtx, c, recorder := cloudContractContext(t, svc, in, ctx, raw)
				if transport == "http_sse" {
					_, err = svc.Forward(requestCtx, c, in.Account, raw)
				} else {
					_, err = svc.proxyOpenAIWSHTTPBridgeTurn(requestCtx, c, in.Account, "unused", raw, len(raw), "client-model", "", "", "", "", 1, func(event []byte) error { _, err := fmt.Fprintf(recorder, "data: %s\n\n", event); return err })
				}
				require.NoError(t, err, recorder.Body.String())
				return recorder.Body.String()
			}
			first := cloudContractResponse(send(cloudCompactionFixture(t, "http_pre_turn_turn")))
			require.Equal(t, "completed", first.Get("status").String(), first.Raw)
			compaction := cloudCompactionFixture(t, "ws_hot_pre_turn_compaction")
			compaction["previous_response_id"] = first.Get("id").String()
			stream := send(compaction)
			script.join(t)
			var done []gjson.Result
			for _, line := range strings.Split(stream, "\n") {
				if event := gjson.Parse(strings.TrimPrefix(line, "data: ")); strings.HasPrefix(line, "data: ") && event.Get("type").String() == "response.output_item.done" && event.Get("item.type").String() == "compaction" {
					done = append(done, event)
				}
			}
			require.Len(t, done, 1, stream)
			require.True(t, strings.HasPrefix(done[0].Get("item.encrypted_content").String(), cloudCompactionEnvelope), stream)
			completed := cloudContractResponse(stream)
			require.Equal(t, "completed", completed.Get("status").String(), stream)
			require.Equal(t, done[0].Get("item.id").String(), cloudOnlyCompaction(t, completed)["id"])
			require.Equal(t, 2, store.settlementCount())
		})
	}
}

// A summary turn that ends without text has nothing for Codex to install: the
// compaction fails visibly instead of returning an empty summary, and the
// usage the turn consumed is still settled.
func TestCloudCompactionWithoutSummaryFailsAndSettles(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, _ := h.establish()
	script := runCloudScript(func() error {
		if _, err := cloudExpectSummaryTurn(h.ctx, wire, "thread", "turn-compact"); err != nil {
			return err
		}
		usage := map[string]any{"inputTokens": 38, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": 40}
		cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": "thread", "turnId": "turn-compact", "tokenUsage": map[string]any{"total": usage}}})
		cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn-compact", "status": "completed"}}})
		return nil
	})
	body := map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": append(append([]any{}, history...), map[string]any{"type": "compaction_trigger"})}
	response, _ := h.invoke(body)
	script.join(t)
	require.Equal(t, "failed", response.Get("status").String(), response.Raw)
	require.Equal(t, "cloud_compaction_failed", response.Get("error.code").String(), response.Raw)
	session := h.store.all()[0]
	settlement := session.snapshotSettlement(session.snapshot().RootRequestID)
	require.NotNil(t, settlement)
	require.False(t, settlement.Unknown)
	require.Equal(t, 28, settlement.Usage.TotalTokens, "the measured summary turn is billed once")
}

// Only the trigger's documented form is a compaction request.
func TestCloudCompactionTriggerShape(t *testing.T) {
	user := cloudUserMessage("hello")
	for name, input := range map[string][]any{
		"not_last":     {map[string]any{"type": "compaction_trigger"}, user},
		"twice":        {user, map[string]any{"type": "compaction_trigger"}, map[string]any{"type": "compaction_trigger"}},
		"extra_fields": {user, map[string]any{"type": "compaction_trigger", "mode": "aggressive"}},
		"nothing":      {map[string]any{"type": "compaction_trigger"}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"model": "gpt-5.1", "input": input})
			require.NoError(t, err)
			p, err := parseCloudProtocolRequest(raw)
			if name == "nothing" || name == "extra_fields" {
				require.Error(t, err)
				require.Contains(t, err.Error(), "compaction")
			} else {
				require.NoError(t, err)
				require.True(t, p.compaction)
			}
		})
	}
	raw, err := json.Marshal(cloudCompactionFixture(t, "http_pre_turn_compaction"))
	require.NoError(t, err)
	_, err = parseCloudProtocolRequest(raw)
	require.NoError(t, err, "the captured Codex request is a valid compaction request")
}

// The legacy unary /responses/compact endpoint has no captured Codex client in
// use; it stays explicitly unsupported rather than guessing its retention.
func TestCloudLegacyCompactEndpointStaysUnsupported(t *testing.T) {
	svc := &OpenAIGatewayService{}
	body := []byte(`{"model":"gpt-5.1","input":[{"type":"message","role":"user","content":"hi"},{"type":"compaction_trigger"}],"stream":true}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses/compact", strings.NewReader(string(body)))
	require.True(t, svc.IsOpenAICloudRequestUnsupported(c, body))
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body)))
	require.False(t, svc.IsOpenAICloudRequestUnsupported(c, body))
}

// HTTP compaction requests are marked as compaction in usage logs, as natively.
func TestCloudCompactionUsageLogKeepsNativeCompactionFlag(t *testing.T) {
	svc, input, ledger := cloudBillingFixture(t)
	input.NativeCompactionV2 = true
	ctx, err := svc.CaptureCloudBillingSnapshot(context.Background(), input)
	require.NoError(t, err)
	settlement := &OpenAICloudSettlement{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 11, APIKeyID: 33, GroupID: 22, AccountID: 44, Model: "gpt-5.1"}, RootRequestID: "compaction-root"},
		BillingSnapshot: CloudBillingSnapshotFromContext(ctx), Status: "completed", Usage: &OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}, ObservedAt: input.PricingAt.Add(time.Second)}
	require.NoError(t, (&openAICloudBillingWorker{service: svc}).settle(ctx, settlement))
	require.True(t, ledger.lastCmd.UsageLog.NativeCompactionV2)
}

func TestCloudCompactionCancellationAfterInterruptDoesNotStartSummary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, wire)
	defer rpc.Close()
	defer wire.Close()
	cloudTestInitialize(t, ctx, rpc, wire)
	store := newCloudLiveRoutingStore()
	record := &OpenAICloudSessionRecord{OwnerToken: "owner", LeaseUntil: time.Now().Add(time.Minute), Usage: &OpenAICloudUsage{InputTokens: 10, OutputTokens: 2, TotalTokens: 12}, OpenAICloudBinding: OpenAICloudBinding{SessionID: "session", RootRequestID: "root", ThreadID: "thread", TurnID: "turn", Status: "awaiting_tools"}}
	require.NoError(t, store.CreateCloudSession(ctx, record))
	events := make(chan cloudRPCMessage, 1)
	actor := &cloudSessionActor{ctx: ctx, rpc: rpc, service: &OpenAICloudSessions{store: store}, record: record, upstream: events, tools: map[string]*cloudPendingTool{}}
	command := OpenAICloudCommand{RequestID: "compaction-request", Deadline: time.Now().Add(time.Minute)}
	protocol := &cloudProtocolRequest{input: []any{map[string]any{"type": "text", "text": "summarize"}}}
	done := make(chan error, 1)
	go func() { done <- actor.compactActiveTurn(command, protocol) }()
	interrupt := cloudTestMessage(t, ctx, wire)
	require.Equal(t, "turn/interrupt", interrupt.Method)
	require.NoError(t, store.DeleteCloudEvents(ctx, command.RequestID))
	cloudTestRead(wire, map[string]any{"id": interrupt.ID, "result": map[string]any{}})
	params, err := json.Marshal(map[string]any{"threadId": "thread", "turn": map[string]any{"id": "turn", "status": "interrupted"}})
	require.NoError(t, err)
	events <- cloudRPCMessage{Method: "turn/completed", Params: params}
	select {
	case err := <-done:
		require.NoError(t, err)
		bill := store.snapshotSettlement("root")
		require.NotNil(t, bill)
		require.Equal(t, 12, bill.Usage.TotalTokens)
		require.True(t, bill.LowerBound)
		require.False(t, bill.Unknown)
	case raw := <-wire.writes:
		var next cloudRPCMessage
		require.NoError(t, json.Unmarshal(raw, &next))
		cloudTestRead(wire, map[string]any{"id": next.ID, "result": map[string]any{"turn": map[string]any{"id": "unexpected-summary"}}})
		<-done
		require.Fail(t, "cancelled compaction dispatched another RPC", next.Method)
	case <-ctx.Done():
		t.Fatal("compaction did not finish")
	}
}
