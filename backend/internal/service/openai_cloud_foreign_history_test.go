//go:build unit

package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	cloudForeignThreadA = "01a16db5-88ac-75c2-b8f2-b0b05411d0ac"
	cloudForeignThreadB = "01a16db5-a50d-70e4-a931-fe8c84176084"
)

// A conversation that ran on BPS (or another account) before Cloud was
// enabled carries reasoning and message ciphertext Cloud did not issue.
func cloudForeignHistory() []any {
	return []any{
		cloudUserMessage("hello"),
		map[string]any{"type": "reasoning", "id": "rs_bps_1", "summary": []any{}, "encrypted_content": "gAAAA-bps-reasoning-ciphertext"},
		map[string]any{"type": "message", "role": "assistant", "id": "msg_bps_1", "content": []any{map[string]any{"type": "output_text", "text": "hi there"}}, "encrypted_content": "gAAAA-bps-message-ciphertext"},
		map[string]any{"type": "function_call", "id": "fc_bps_1", "call_id": "call_bps_1", "name": "echo", "arguments": `{}`},
		map[string]any{"type": "function_call_output", "call_id": "call_bps_1", "output": "echoed"},
	}
}

// Codex can enter Cloud with a complete native conversation whose latest item
// is not a user message (for example, the result of a client tool). Inject the
// original history and sample it without inventing a new user message.
func TestCloudFullHistoryWithoutFinalUserStartsTurn(t *testing.T) {
	for _, tail := range []string{"tool output", "assistant", "developer", "original tool image"} {
		t.Run(tail, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			requests, _ := cloudCatalogHistoryFixture(t)
			body := requests["standard"]
			body["model"], body["stream"] = "client-model", false
			history := append(body["input"].([]any),
				map[string]any{"type": "function_call", "call_id": "call_native_history", "name": "echo", "arguments": `{}`},
				map[string]any{"type": "function_call_output", "call_id": "call_native_history", "output": "echoed"})
			switch tail {
			case "assistant":
				history = append(history, map[string]any{"type": "message", "role": "assistant", "content": "Continue processing the result."})
			case "developer":
				history = append(history, map[string]any{"type": "message", "role": "developer", "content": "Return only the result."})
			case "original tool image":
				history[len(history)-1].(map[string]any)["output"] = []any{map[string]any{"type": "input_image", "detail": "original", "image_url": "data:image/png;base64,iVBORw0KGgo="}}
			}
			body["input"] = history
			type outcome struct {
				response gjson.Result
				err      error
			}
			done := make(chan outcome, 1)
			go func() { response, err := h.invoke(body); done <- outcome{response, err} }()
			var wire *cloudRPCFakeConn
			select {
			case got := <-done:
				t.Fatalf("complete history was rejected before connecting: %v; %s", got.err, got.response.Raw)
			case wire = <-h.wires:
			case <-h.ctx.Done():
				t.Fatal("request did not reach Cloud")
			}
			cloudWireResponse(t, h.ctx, wire, "initialize", map[string]any{})
			require.Equal(t, "initialized", cloudTestMessage(t, h.ctx, wire).Method)
			cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "history-thread"}})
			inject := cloudTestMessage(t, h.ctx, wire)
			require.Equal(t, "thread/inject_items", inject.Method)
			require.Len(t, gjson.GetBytes(inject.Params, "items").Array(), len(history))
			if tail == "original tool image" {
				require.Contains(t, string(inject.Params), `"detail":"original"`)
			}
			cloudTestRead(wire, map[string]any{"id": inject.ID, "result": map[string]any{}})
			turn := cloudTestMessage(t, h.ctx, wire)
			require.Equal(t, "turn/start", turn.Method)
			require.Equal(t, "[]", gjson.GetBytes(turn.Params, "input").Raw, "an explicit empty input must not synthesize a user message")
			cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "history-turn"}}})
			cloudWireCompleteTurn(wire, "history-thread", "history-turn", 12)
			got := <-done
			require.NoError(t, got.err, got.response.Raw)
			require.Equal(t, "completed", got.response.Get("status").String())
			require.Equal(t, 1, h.store.settlementCount())
			for _, item := range got.response.Get("output").Array() {
				history = append(history, item.Value())
			}
			body["input"] = append(history, cloudUserMessage("next"))
			go func() { response, err := h.invoke(body); done <- outcome{response, err} }()
			cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "next-turn"}})
			cloudWireCompleteTurn(wire, "history-thread", "next-turn", 22)
			next := <-done
			require.NoError(t, next.err, next.response.Raw)
			require.Equal(t, "completed", next.response.Get("status").String())
			require.Equal(t, 2, h.store.settlementCount())
			require.Empty(t, h.wires, "the next turn must reuse the verified history")
		})
	}
}

// Cloud discards an ephemeral thread whose injected history it cannot use and
// answers "unknown thread" before any turn starts (observed in production with
// BPS history). Nothing ran, so the turn retries once on a fresh thread without
// the foreign ciphertext, keeps every visible item, and later full-history
// requests continue that thread.
func TestCloudForeignCiphertextHistoryRetriesOnFreshThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	history := cloudForeignHistory()
	upstream := make(chan *cloudRPCFakeConn, 1)
	go func() {
		wire := h.nextWire()
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": cloudForeignThreadA}})
		inject := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "thread/inject_items", inject.Method)
		require.Contains(t, string(inject.Params), "gAAAA-bps-reasoning-ciphertext", "the first attempt keeps the history intact")
		cloudTestRead(wire, map[string]any{"id": inject.ID, "error": map[string]any{"code": -32600, "message": "unknown thread: " + cloudForeignThreadA}})
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": cloudForeignThreadB}})
		retry := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "thread/inject_items", retry.Method)
		require.Equal(t, cloudForeignThreadB, gjson.GetBytes(retry.Params, "threadId").String())
		require.NotContains(t, string(retry.Params), "encrypted_content", "the retry carries no ciphertext Cloud may not have issued")
		items := gjson.GetBytes(retry.Params, "items").Array()
		require.Len(t, items, 4, "only the unusable reasoning item is omitted")
		require.Equal(t, "hello", items[0].Get("content.0.text").String())
		require.Equal(t, "hi there", items[1].Get("content.0.text").String(), "visible assistant text is kept")
		require.Equal(t, "function_call", items[2].Get("type").String())
		require.Equal(t, "function_call_output", items[3].Get("type").String())
		cloudTestRead(wire, map[string]any{"id": retry.ID, "result": map[string]any{}})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-one"}})
		cloudWireCompleteTurn(wire, cloudForeignThreadB, "turn-one", 40)
		upstream <- wire
	}()
	full := append(append([]any{}, history...), cloudUserMessage("continue"))
	response, err := h.invoke(map[string]any{"model": "client-model", "input": full, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	var wire *cloudRPCFakeConn
	select {
	case wire = <-upstream:
	case <-h.ctx.Done():
		t.Fatal("upstream script did not finish")
	}
	require.Equal(t, 1, h.store.settlementCount())

	// The client replays the original (foreign) history plus Cloud's answer.
	// It still matches the live thread exactly, so no new thread is injected.
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, cloudForeignThreadB, "turn-two", 60)
	}()
	next := append([]any{}, full...)
	for _, item := range response.Get("output").Array() {
		next = append(next, item.Value())
	}
	next = append(next, cloudUserMessage("and again"))
	second, err := h.invoke(map[string]any{"model": "client-model", "input": next, "tools": cloudHistoryTools})
	require.NoError(t, err, second.Raw)
	require.Equal(t, "completed", second.Get("status").String(), second.Raw)
	<-done
	h.noNewWire(200 * time.Millisecond)
	require.Equal(t, 2, h.store.settlementCount())
}

// When Cloud rejects the history even without any ciphertext, the turn fails
// before inference with a 400 that names the history, not a 502 that blames
// the upstream or the account. Each reduction is tried once; a history with no
// ciphertext is not retried at all.
func TestCloudUnusableHistoryIsBadRequestBeforeInference(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		history []any
		starts  int
		code    string
	}{
		{name: "rejected after both reductions", history: append(cloudForeignHistory(), map[string]any{"type": "compaction", "encrypted_content": "gAAAA-bps-compaction"}), starts: 3, code: "cloud_history_rejected"},
		{name: "no ciphertext to remove", history: []any{cloudUserMessage("hello"), map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "hi"}}}}, starts: 1, code: "cloud_history_rejected"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			starts := make(chan int, 1)
			go func() {
				wire := h.nextWire()
				count := 0
				for {
					msg := cloudTestMessage(t, h.ctx, wire)
					switch msg.Method {
					case "thread/start":
						count++
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": fmt.Sprintf("%08d-88ac-75c2-b8f2-b0b05411d0ac", count)}}})
					case "thread/inject_items":
						cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "message": fmt.Sprintf("unknown thread: %08d-88ac-75c2-b8f2-b0b05411d0ac", count)}})
					case "turn/start":
						t.Error("an unusable history must never start a turn")
						return
					case "thread/archive":
						cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
						starts <- count
						return
					}
				}
			}()
			full := append(append([]any{}, scenario.history...), cloudUserMessage("continue"))
			response, err := h.invoke(map[string]any{"model": "client-model", "input": full, "tools": cloudHistoryTools})
			// A failure after response.created is a failed Responses body; one
			// rejected before any public output is a returned error. Both carry
			// the same public status and code.
			status, code, message := int(response.Get("error.status_code").Int()), response.Get("error.code").String(), response.Get("error.message").String()
			if err != nil {
				var ok bool
				status, code, message, ok = cloudPublicError(err)
				require.True(t, ok, "%v", err)
			} else {
				require.Equal(t, "failed", response.Get("status").String(), response.Raw)
			}
			require.Equal(t, http.StatusBadRequest, status, response.Raw)
			require.Equal(t, scenario.code, code, response.Raw)
			require.NotContains(t, message, cloudForeignThreadA)
			select {
			case count := <-starts:
				require.Equal(t, scenario.starts, count)
			case <-h.ctx.Done():
				t.Fatal("the discarded session did not finish")
			}
			sessions := h.store.all()
			require.Len(t, sessions, 1)
			settlement := sessions[0].settlementFor(sessions[0].snapshot().RootRequestID)
			require.NotNil(t, settlement)
			require.False(t, settlement.Unknown, "no turn started: zero consumption is proven")
			require.Zero(t, settlement.Usage.TotalTokens)
		})
	}
}

// Client tool search records from turns served before Cloud (native or BPS)
// are history only: Cloud tools are fixed at thread start and the current
// request declares them directly. They are omitted, as BPS omits them, instead
// of failing the whole conversation with "input.tools".
func TestCloudRequestShapeOmitsToolSearchHistory(t *testing.T) {
	p, err := cloudShapeParse(t, map[string]any{"input": []any{
		cloudUserMessage("find a tool"),
		map[string]any{"type": "tool_search_call", "id": "tsc_1", "call_id": "call_search", "status": "completed", "execution": "client", "arguments": map[string]any{"query": "calendar"}},
		map[string]any{"type": "tool_search_output", "id": "tso_1", "call_id": "call_search", "status": "completed", "execution": "client", "tools": []any{map[string]any{"type": "function", "name": "calendar", "parameters": map[string]any{"type": "object"}}}},
		map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "loaded"}}},
		cloudUserMessage("now"),
	}})
	require.NoError(t, err)
	require.Len(t, p.history, 2, "the search call and output are omitted from the thread history")
	for _, raw := range p.history {
		require.NotContains(t, []any{"tool_search_call", "tool_search_output"}, raw.(map[string]any)["type"])
	}
	p, err = cloudShapeParse(t, map[string]any{"input": []any{
		map[string]any{"type": "additional_tools", "role": "developer", "tools": []any{}},
		cloudUserMessage("now"),
	}})
	require.NoError(t, err, "an empty Codex tool catalog is valid")
	require.Empty(t, p.tools)
	require.Empty(t, p.history)
	require.Len(t, p.input, 1)
}

func cloudCodexHistoryFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/cloud_codex_0_164_history.json")
	require.NoError(t, err)
	var fixture struct {
		Requests map[string]map[string]any `json:"requests"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	body, ok := fixture.Requests[name]
	require.True(t, ok, name)
	return body
}

// Real Codex 0.164 store:false requests replay every earlier item with
// attribution metadata, reasoning with "content": null, and apply_patch as a
// custom tool call. A second turn must parse: these are the client's own
// serializations of items it received, not unsupported semantics.
func TestCloudParsesRealCodexReplayedHistory(t *testing.T) {
	t.Run("reasoning and native web search", func(t *testing.T) {
		raw, err := json.Marshal(cloudCodexHistoryFixture(t, "history_turn"))
		require.NoError(t, err)
		p, err := parseCloudProtocolRequest(raw)
		require.NoError(t, err)
		var types []string
		for _, item := range p.history {
			entry := item.(map[string]any)
			types = append(types, entry["type"].(string))
			if entry["type"] == "reasoning" {
				require.NotContains(t, entry, "content", "an absent raw reasoning content is not forwarded as null")
				require.Equal(t, "gAAAAAfake-reasoning-ciphertext", entry["encrypted_content"])
			}
		}
		require.NotContains(t, types, "web_search_call", "a search record Cloud did not perform is omitted, not rejected")
		require.Contains(t, types, "reasoning")
		require.Len(t, p.input, 1)
	})
	t.Run("function and apply_patch replies", func(t *testing.T) {
		raw, err := json.Marshal(cloudCodexHistoryFixture(t, "tool_replay"))
		require.NoError(t, err)
		p, err := parseCloudProtocolRequest(raw)
		require.NoError(t, err)
		require.Contains(t, p.outputs, "call_plan_1")
		require.Contains(t, p.outputs, "call_patch_1", "the apply_patch result is a tool output like any other")
		injected, err := json.Marshal(cloudInjectionHistory(p))
		require.NoError(t, err)
		require.NotContains(t, string(injected), `"custom_tool_call"`, "custom tool history uses the lowered function form")
	})
}

// Cloud's own reasoning, as Codex replays it (content null, attribution
// metadata), still matches the live thread exactly: the next full-history turn
// continues it instead of starting and re-injecting a new thread.
func TestCloudCodexReplayOfOwnReasoningContinuesThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	upstream := make(chan *cloudRPCFakeConn, 1)
	go func() {
		wire := h.nextWire()
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		cloudTestRead(wire, map[string]any{"method": "rawResponseItem/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"type": "reasoning", "id": "rs_cloud_up", "summary": []any{map[string]any{"type": "summary_text", "text": "thinking"}}, "encrypted_content": "gAAAA-cloud-issued"}}})
		cloudWireCompleteTurn(wire, "thread", "turn", 12)
		upstream <- wire
	}()
	first := []any{cloudUserMessage("hello")}
	response, err := h.invoke(map[string]any{"model": "client-model", "input": first, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	wire := <-upstream
	require.Equal(t, "reasoning", response.Get("output.0.type").String(), response.Raw)

	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	metadata := map[string]any{"turn_id": "01a16dca-f939-71f0-b9b2-39dfd93eee2c"}
	next := []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hello"}}, cloudMessageMetadataKey: metadata}}
	for _, item := range response.Get("output").Array() {
		replayed := item.Value().(map[string]any)
		replayed[cloudMessageMetadataKey] = metadata
		if replayed["type"] == "reasoning" {
			replayed["content"] = nil
			delete(replayed, "status")
		}
		next = append(next, replayed)
	}
	next = append(next, cloudUserMessage("again"))
	second, err := h.invoke(map[string]any{"model": "client-model", "input": next, "tools": cloudHistoryTools})
	require.NoError(t, err, second.Raw)
	require.Equal(t, "completed", second.Get("status").String(), second.Raw)
	<-done
	h.noNewWire(200 * time.Millisecond)
	require.Equal(t, 2, h.store.settlementCount())
}

// A compaction Cloud did not issue keeps a migrated conversation from starting
// even after its reasoning ciphertext is gone. The last reduction replaces it
// with a notice so the model knows earlier context was summarized; every
// visible item stays and the turn runs on that thread.
func TestCloudForeignCompactionIsReplacedWithNoticeOnLastRetry(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	history := append(cloudForeignHistory(), map[string]any{"type": "compaction", "encrypted_content": "gAAAA-bps-compaction"}, cloudUserMessage("after compaction"))
	upstream := make(chan []gjson.Result, 1)
	go func() {
		wire := h.nextWire()
		var accepted []gjson.Result
		starts := 0
		thread := ""
		for {
			msg := cloudTestMessage(t, h.ctx, wire)
			switch msg.Method {
			case "thread/start":
				starts++
				thread = fmt.Sprintf("%08d-a50d-70e4-a931-fe8c84176084", starts)
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": thread}}})
			case "thread/inject_items":
				if gjson.GetBytes(msg.Params, `items.#(type=="compaction")`).Exists() {
					cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "message": "unknown thread: " + thread}})
					continue
				}
				accepted = gjson.GetBytes(msg.Params, "items").Array()
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{}})
			case "turn/start":
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-one"}}})
				cloudWireCompleteTurn(wire, thread, "turn-one", 40)
				upstream <- accepted
				return
			}
		}
	}()
	full := append(append([]any{}, history...), cloudUserMessage("continue"))
	response, err := h.invoke(map[string]any{"model": "client-model", "input": full, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	var accepted []gjson.Result
	select {
	case accepted = <-upstream:
	case <-h.ctx.Done():
		t.Fatal("upstream script did not finish")
	}
	require.Len(t, accepted, 6, "user, assistant, call, output, notice and the later user message")
	require.Equal(t, "developer", accepted[4].Get("role").String())
	require.Equal(t, openAIOmittedCompactionNotice, accepted[4].Get("content.0.text").String())
	require.Equal(t, "after compaction", accepted[5].Get("content.0.text").String())
	for _, item := range accepted {
		require.False(t, item.Get("encrypted_content").Exists())
	}
	require.Equal(t, 1, h.store.settlementCount())
}
