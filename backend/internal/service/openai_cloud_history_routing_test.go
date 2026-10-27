package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// cloudMultiRoutingStore keeps one single-session routing fake per Cloud
// session plus one shared request-event space, mirroring Redis key scoping.
// Release marks the record closed, as the Redis release script does.
type cloudMultiRoutingStore struct {
	OpenAICloudSessionStore
	mu       sync.Mutex
	sessions map[string]*cloudLiveRoutingStore
	order    []string
	events   *cloudLiveRoutingStore
}

func newCloudMultiRoutingStore() *cloudMultiRoutingStore {
	return &cloudMultiRoutingStore{sessions: map[string]*cloudLiveRoutingStore{}, events: newCloudLiveRoutingStore()}
}
func (m *cloudMultiRoutingStore) session(id string) *cloudLiveRoutingStore {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}
func (m *cloudMultiRoutingStore) all() []*cloudLiveRoutingStore {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*cloudLiveRoutingStore, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.sessions[id])
	}
	return out
}
func (m *cloudMultiRoutingStore) CreateCloudSession(ctx context.Context, r *OpenAICloudSessionRecord) error {
	sub := newCloudLiveRoutingStore()
	if err := sub.CreateCloudSession(ctx, r); err != nil {
		return err
	}
	m.mu.Lock()
	m.sessions[r.SessionID] = sub
	m.order = append(m.order, r.SessionID)
	m.mu.Unlock()
	return nil
}
func (m *cloudMultiRoutingStore) LoadCloudSession(ctx context.Context, id string) (*OpenAICloudSessionRecord, error) {
	if sub := m.session(id); sub != nil {
		return sub.LoadCloudSession(ctx, id)
	}
	return nil, nil
}
func (m *cloudMultiRoutingStore) FindCloudSession(ctx context.Context, user, key, group int64, ref string) (*OpenAICloudSessionRecord, error) {
	for _, sub := range m.all() {
		if r, err := sub.FindCloudSession(ctx, user, key, group, ref); err != nil || r != nil {
			return r, err
		}
	}
	return nil, nil
}
func (m *cloudMultiRoutingStore) SaveCloudSession(ctx context.Context, r *OpenAICloudSessionRecord, refs []string, settlement *OpenAICloudSettlement) error {
	if sub := m.session(r.SessionID); sub != nil {
		return sub.SaveCloudSession(ctx, r, refs, settlement)
	}
	return errors.New("unknown Cloud session")
}
func (m *cloudMultiRoutingStore) RenewCloudSession(ctx context.Context, id, owner string, until time.Time) (bool, error) {
	if sub := m.session(id); sub != nil {
		return sub.RenewCloudSession(ctx, id, owner, until)
	}
	return false, nil
}
func (m *cloudMultiRoutingStore) EnqueueCloudCommand(ctx context.Context, id string, command OpenAICloudCommand) error {
	if sub := m.session(id); sub != nil {
		return sub.EnqueueCloudCommand(ctx, id, command)
	}
	return ErrOpenAICloudCommandRejected
}
func (m *cloudMultiRoutingStore) ReadCloudCommands(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	if sub := m.session(id); sub != nil {
		return sub.ReadCloudCommands(ctx, id, after)
	}
	return nil, errors.New("unknown Cloud session")
}
func (m *cloudMultiRoutingStore) ReclaimCloudCommands(ctx context.Context, id, owner, upTo string) error {
	if sub := m.session(id); sub != nil {
		return sub.ReclaimCloudCommands(ctx, id, owner, upTo)
	}
	return errors.New("unknown Cloud session")
}
func (m *cloudMultiRoutingStore) PublishCloudEvent(ctx context.Context, id string, event OpenAICloudStreamEvent) error {
	return m.events.PublishCloudEvent(ctx, id, event)
}
func (m *cloudMultiRoutingStore) ReadCloudEvents(ctx context.Context, id, after string) ([]OpenAICloudStreamMessage, error) {
	return m.events.ReadCloudEvents(ctx, id, after)
}
func (m *cloudMultiRoutingStore) DeleteCloudEvents(ctx context.Context, id string) error {
	return m.events.DeleteCloudEvents(ctx, id)
}
func (m *cloudMultiRoutingStore) CloudRequestClosed(ctx context.Context, id string) (bool, error) {
	return m.events.CloudRequestClosed(ctx, id)
}
func (m *cloudMultiRoutingStore) ReleaseCloudSession(ctx context.Context, id, owner string) error {
	sub := m.session(id)
	if sub == nil {
		return errors.New("unknown Cloud session")
	}
	if err := sub.ReleaseCloudSession(ctx, id, owner); err != nil {
		return err
	}
	sub.mu.Lock()
	sub.record.Status = "closed"
	sub.mu.Unlock()
	return nil
}
func (m *cloudMultiRoutingStore) settlementCount() int {
	total := 0
	for _, sub := range m.all() {
		total += sub.settlementCount()
	}
	return total
}

type cloudMultiForwardCache struct {
	GatewayCache
	*cloudMultiRoutingStore
}

type cloudHistoryRoutingHarness struct {
	t     *testing.T
	ctx   context.Context
	svc   *OpenAIGatewayService
	in    *OpenAIRecordUsageInput
	store *cloudMultiRoutingStore
	wires chan *cloudRPCFakeConn
}

func newCloudHistoryRoutingHarness(t *testing.T) *cloudHistoryRoutingHarness {
	svc, in, _, _ := cloudForwardFixture(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	h := &cloudHistoryRoutingHarness{t: t, ctx: ctx, svc: svc, in: in, store: newCloudMultiRoutingStore(), wires: make(chan *cloudRPCFakeConn, 4)}
	// Response ownership and Cloud sessions share one store in production.
	svc.cache = &cloudMultiForwardCache{GatewayCache: &stubGatewayCache{}, cloudMultiRoutingStore: h.store}
	svc.cloudSessions.Close()
	svc.cloudSessions = NewOpenAICloudSessions(h.store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		wire := newCloudRPCFakeConn()
		t.Cleanup(func() { wire.Close() })
		h.wires <- wire
		rpc := newCloudRPC(ctx, wire)
		if err := rpc.Initialize(ctx); err != nil {
			return nil, err
		}
		return rpc, nil
	}, func(context.Context, OpenAICloudOwner) error { return nil })
	return h
}

// allow replaces the harness deadline for a test that moves tens of MiB of
// JSON through the gateway, which race-detector runs on a busy host need.
func (h *cloudHistoryRoutingHarness) allow(d time.Duration) {
	ctx, cancel := context.WithTimeout(h.t.Context(), d)
	h.t.Cleanup(cancel)
	h.ctx = ctx
}

func (h *cloudHistoryRoutingHarness) nextWire() *cloudRPCFakeConn {
	h.t.Helper()
	select {
	case wire := <-h.wires:
		cloudWireResponse(h.t, h.ctx, wire, "initialize", map[string]any{})
		require.Equal(h.t, "initialized", cloudTestMessage(h.t, h.ctx, wire).Method)
		return wire
	case <-h.ctx.Done():
		h.t.Error("no new Cloud connection was opened")
		return nil
	}
}

func (h *cloudHistoryRoutingHarness) noNewWire(within time.Duration) {
	h.t.Helper()
	select {
	case <-h.wires:
		h.t.Fatal("an unexpected new Cloud connection was opened")
	case <-time.After(within):
	}
}

// invoke runs one HTTP JSON request through the same continuation preparation
// and forwarding entry points the handler uses.
func (h *cloudHistoryRoutingHarness) invoke(body map[string]any, clientHeaders ...http.Header) (gjson.Result, error) {
	h.t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(h.t, err)
	requestCtx, err := h.svc.CaptureCloudBillingSnapshot(h.ctx, h.in)
	require.NoError(h.t, err)
	requestCtx, finishSlots := WithConcurrencySlotLifecycle(requestCtx)
	defer finishSlots()
	requestCtx, err = h.svc.PrepareCloudContinuation(requestCtx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw, clientHeaders...)
	if err != nil {
		return gjson.Result{}, err
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(requestCtx)
	if len(clientHeaders) > 0 {
		c.Request.Header = clientHeaders[0].Clone()
	}
	c.Set("api_key", h.in.APIKey)
	SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
	_, err = h.svc.forwardOpenAICloud(requestCtx, c, h.in.Account, raw)
	return gjson.Parse(recorder.Body.String()), err
}

func cloudWireCompleteTurn(wire *cloudRPCFakeConn, thread, turn string, total int) {
	usage := map[string]any{"inputTokens": total - 2, "outputTokens": 2, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "totalTokens": total}
	cloudTestRead(wire, map[string]any{"method": "thread/tokenUsage/updated", "params": map[string]any{"threadId": thread, "turnId": turn, "tokenUsage": map[string]any{"total": usage}}})
	cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "answer-" + turn, "type": "agentMessage"}}})
	cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": thread, "turnId": turn, "itemId": "answer-" + turn, "delta": "ok"}})
	cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": thread, "turnId": turn, "item": map[string]any{"id": "answer-" + turn, "type": "agentMessage", "text": "ok"}}})
	cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{"threadId": thread, "turn": map[string]any{"id": turn, "status": "completed"}}})
}

var cloudHistoryTools = []any{map[string]any{"type": "function", "name": "echo", "parameters": map[string]any{"type": "object"}}}

// establish runs one tool-using turn on a fresh session and returns the full
// Responses history a store:false client would replay next.
func (h *cloudHistoryRoutingHarness) establish() (*cloudRPCFakeConn, []any, string) {
	h.t.Helper()
	wireReady := make(chan *cloudRPCFakeConn, 1)
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		wire := h.nextWire()
		wireReady <- wire
		cloudWireResponse(h.t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		cloudWireResponse(h.t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		wire.reads <- []byte(`{"id":"tool-rpc","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"upstream-call","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
		reply := cloudTestMessage(h.t, h.ctx, wire)
		require.JSONEq(h.t, `"tool-rpc"`, string(reply.ID))
		cloudWireCompleteTurn(wire, "thread", "turn", 12)
	}()
	userOne := map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "hello"}}}
	first, err := h.invoke(map[string]any{"model": "client-model", "input": []any{userOne}, "tools": cloudHistoryTools})
	require.NoError(h.t, err, first.Raw)
	call := first.Get("output.0")
	require.Equal(h.t, "function_call", call.Get("type").String(), first.Raw)
	output := map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "answer"}
	last, err := h.invoke(map[string]any{"model": "client-model", "previous_response_id": first.Get("id").String(), "input": []any{output}, "tools": cloudHistoryTools})
	require.NoError(h.t, err, last.Raw)
	require.Equal(h.t, "completed", last.Get("status").String(), last.Raw)
	<-upstream
	history := []any{userOne}
	for _, item := range first.Get("output").Array() {
		history = append(history, item.Value())
	}
	history = append(history, output)
	for _, item := range last.Get("output").Array() {
		history = append(history, item.Value())
	}
	return <-wireReady, history, last.Get("id").String()
}

func cloudUserMessage(text string) map[string]any {
	return map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": text}}}
}

func TestCloudCompletedContinuationAppendsClientContext(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		for _, fullHistory := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/full_history=%t", transport, fullHistory), func(t *testing.T) {
				h := newCloudHistoryRoutingHarness(t)
				wire, history, previous := h.establish()
				contextItem := map[string]any{"type": "message", "role": "developer", "content": []any{map[string]any{"type": "input_text", "text": "The client's current workspace is /client/project; use its current workspace-write permissions."}}}
				input := []any{contextItem, cloudUserMessage("Continue with the updated client context.")}
				if fullHistory {
					input = append(append([]any{}, history...), input...)
				}
				model := "client-model"
				if transport == "ws" {
					model = "gpt-5.1" // The WS ingress has already applied the account mapping.
				}
				body, err := json.Marshal(map[string]any{"model": model, "previous_response_id": previous, "input": input, "tools": cloudHistoryTools, "stream": transport != "json"})
				require.NoError(t, err)
				ctx, c, recorder := cloudContractContext(t, h.svc, h.in, h.ctx, body)
				finished := make(chan error, 1)
				go func() {
					if transport == "ws" {
						_, err := h.svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, h.in.Account, "unused", body, len(body), "client-model", "", "", "", "", 2, func(event []byte) error { _, e := fmt.Fprintf(recorder, "data: %s\n\n", event); return e })
						finished <- err
						return
					}
					_, err := h.svc.Forward(ctx, c, h.in.Account, body)
					finished <- err
				}()
				var injected cloudRPCMessage
				select {
				case raw := <-wire.writes:
					require.NoError(t, json.Unmarshal(raw, &injected))
				case err := <-finished:
					require.NoError(t, err, "new client context is additional input, not an edit to prior history")
					t.Fatal("continuation returned without injecting its new context")
				case <-ctx.Done():
					t.Fatal("continuation did not reach the existing Cloud thread")
				}
				require.Equal(t, "thread/inject_items", injected.Method)
				require.Len(t, gjson.GetBytes(injected.Params, "items").Array(), 1, "existing context must not be injected or charged again")
				want, err := json.Marshal(contextItem)
				require.NoError(t, err)
				require.JSONEq(t, string(want), gjson.GetBytes(injected.Params, "items.0").Raw)
				cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
				cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "next-turn"}})
				cloudWireCompleteTurn(wire, "thread", "next-turn", 24)
				require.NoError(t, <-finished)
				require.Equal(t, "completed", cloudContractResponse(recorder.Body.String()).Get("status").String())
				require.Len(t, h.store.all(), 1, "append-only context keeps the live thread")
				require.Equal(t, 2, h.store.settlementCount(), "each user turn settles once")
				actor := h.svc.cloudSessions.localActor(h.store.all()[0].snapshot().SessionID)
				require.NotNil(t, actor)
				require.Contains(t, actor.history, contextItem)
			})
		}
	}
}

func TestCloudCompletedContinuationMayChangeThreadSettings(t *testing.T) {
	for _, setting := range []string{"instructions", "tools", "verbosity", "reused_thread_identity"} {
		t.Run(setting, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			wire, history, previous := h.establish()
			body := map[string]any{"model": "client-model", "previous_response_id": previous, "input": []any{cloudUserMessage("next")}, "tools": cloudHistoryTools}
			switch setting {
			case "instructions", "reused_thread_identity":
				body["instructions"] = "Use the updated instructions for this user turn."
			case "tools":
				body["tools"] = append(append([]any{}, cloudHistoryTools...), map[string]any{"type": "function", "name": "new_tool", "parameters": map[string]any{"type": "object"}})
			case "verbosity":
				body["text"] = map[string]any{"verbosity": "high"}
			}
			finished := make(chan error, 1)
			var response gjson.Result
			go func() { var err error; response, err = h.invoke(body); finished <- err }()
			var archived cloudRPCMessage
			select {
			case raw := <-wire.writes:
				require.NoError(t, json.Unmarshal(raw, &archived))
			case err := <-finished:
				require.NoError(t, err, "a completed latest-parent turn can apply new instructions/tools/verbosity")
				t.Fatal("new settings were not applied")
			case <-h.ctx.Done():
				t.Fatal("new settings did not reach Cloud")
			}
			require.Equal(t, "thread/archive", archived.Method)
			cloudTestRead(wire, map[string]any{"id": archived.ID, "result": map[string]any{}})
			start := cloudTestMessage(t, h.ctx, wire)
			require.Equal(t, "thread/start", start.Method)
			if setting == "instructions" || setting == "reused_thread_identity" {
				require.Equal(t, body["instructions"], gjson.GetBytes(start.Params, "baseInstructions").String())
			} else if setting == "verbosity" {
				require.Equal(t, "high", gjson.GetBytes(start.Params, "config.model_verbosity").String())
			} else {
				require.Len(t, gjson.GetBytes(start.Params, "dynamicTools.0.tools").Array(), 2)
			}
			thread := "updated-thread"
			if setting == "reused_thread_identity" {
				thread = "thread"
			}
			cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": thread}}})
			if setting == "reused_thread_identity" {
				// The actor still archives its failed thread during cleanup. Only
				// duplicate history or a new turn would be an execution violation.
				cleanup := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "thread/archive", cleanup.Method, "a reused identity must not receive duplicate history or another turn")
				cloudTestRead(wire, map[string]any{"id": cleanup.ID, "result": map[string]any{}})
				require.NoError(t, <-finished)
				require.Equal(t, "failed", response.Get("status").String())
				require.Equal(t, "cloud_protocol_error", response.Get("error.code").String())
				require.Empty(t, wire.writes)
				require.Zero(t, h.store.all()[0].snapshot().Usage.TotalTokens)
				return
			}
			injected := cloudTestMessage(t, h.ctx, wire)
			require.Equal(t, "thread/inject_items", injected.Method)
			require.Len(t, gjson.GetBytes(injected.Params, "items").Array(), len(history), "complete authoritative history is materialized once")
			cloudTestRead(wire, map[string]any{"id": injected.ID, "result": map[string]any{}})
			cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "updated-turn"}})
			cloudWireCompleteTurn(wire, "updated-thread", "updated-turn", 20)
			require.NoError(t, <-finished)
			require.Equal(t, "completed", response.Get("status").String())
			require.Len(t, h.store.all(), 1)
			require.Equal(t, 2, h.store.settlementCount())
			require.Equal(t, 20, h.store.all()[0].snapshot().Usage.TotalTokens, "a fresh thread cannot subtract the old thread's usage baseline")
		})
	}
}

// A completed session that the owner released (idle expiry or capacity) must
// not turn every later store:false request into a failed continuation. The old
// call IDs are history: start a new thread and inject the complete history.
func TestCloudFullHistoryAfterReleasedSessionStartsNewThread(t *testing.T) {
	for _, kind := range []string{"user message", "delivered task"} {
		t.Run(kind, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			wire, history, lastID := h.establish()
			record := h.store.all()[0].snapshot()
			archived := make(chan struct{})
			go func() {
				defer close(archived)
				cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
			}()
			require.NoError(t, h.svc.cloudSessions.CloseCompleted(h.ctx, record.OpenAICloudOwner, record.SessionID))
			<-archived

			t.Run("previous_response_id reports not found", func(t *testing.T) {
				_, err := h.invoke(map[string]any{"model": "client-model", "previous_response_id": lastID, "input": []any{cloudUserMessage("again")}, "tools": cloudHistoryTools})
				require.Error(t, err)
				status, code, _, ok := cloudPublicError(err)
				require.True(t, ok, "%v", err)
				require.Equal(t, http.StatusBadRequest, status)
				require.Equal(t, "previous_response_not_found", code)
			})

			upstream := make(chan struct{})
			go func() {
				defer close(upstream)
				next := h.nextWire()
				cloudWireResponse(t, h.ctx, next, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread-two"}})
				inject := cloudTestMessage(t, h.ctx, next)
				require.Equal(t, "thread/inject_items", inject.Method)
				require.Equal(t, "function_call", gjson.GetBytes(inject.Params, "items.1.type").String(), string(inject.Params))
				require.Equal(t, "sub2api_client", gjson.GetBytes(inject.Params, "items.1.namespace").String())
				cloudTestRead(next, map[string]any{"id": inject.ID, "result": map[string]any{}})
				cloudWireResponse(t, h.ctx, next, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
				cloudWireCompleteTurn(next, "thread-two", "turn-two", 30)
			}()
			full := append(append([]any{}, history...), cloudUserMessage("again"))
			if kind == "delivered task" {
				_, samples := cloudCatalogHistoryFixture(t)
				full[len(full)-1] = samples["function_call_output/no_call_id"]
			}
			response, err := h.invoke(map[string]any{"model": "client-model", "input": full, "tools": cloudHistoryTools})
			require.NoError(t, err, response.Raw)
			require.Equal(t, "completed", response.Get("status").String(), response.Raw)
			<-upstream
			require.Equal(t, 2, h.store.settlementCount())

		})
	}
}

// A live completed session is reused when the replayed history matches it.
func TestCloudFullHistoryMatchingLiveSessionContinuesThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, _ := h.establish()
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	full := append(append([]any{}, history...), cloudUserMessage("again"))
	response, err := h.invoke(map[string]any{"model": "client-model", "input": full, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	h.noNewWire(200 * time.Millisecond)
	require.Equal(t, 2, h.store.settlementCount())
}

// continueTurn completes one new turn on the established thread.
func (h *cloudHistoryRoutingHarness) continueTurn(wire *cloudRPCFakeConn, turn int, body map[string]any) gjson.Result {
	h.t.Helper()
	id := "turn-" + strconv.Itoa(turn)
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		cloudWireResponse(h.t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": id}})
		cloudWireCompleteTurn(wire, "thread", id, 20*turn)
	}()
	response, err := h.invoke(body)
	require.NoError(h.t, err, "turn %d: %s", turn, response.Raw)
	require.Equal(h.t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	return response
}

// Review regression: a store:false client resends its whole history with every
// turn. The live thread already holds that history, so only each turn's new
// message is retained; counting every resent copy rejected the conversation
// with a 400 on the same thread, retries included, after about 16 MiB of copies.
func TestCloudFullHistoryLongConversationContinuesThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	h.allow(time.Minute)
	wire, history, _ := h.establish()
	const turns = 12
	for turn := 2; turn <= turns; turn++ {
		// Pasted files or tool output make the history large; short questions
		// follow, each request resending all of it.
		message := cloudUserMessage("question " + strconv.Itoa(turn))
		if turn == 2 {
			message = cloudUserMessage(strings.Repeat("x", 2<<20))
		}
		history = append(history, message)
		response := h.continueTurn(wire, turn, map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools})
		for _, item := range response.Get("output").Array() {
			history = append(history, item.Value())
		}
	}
	h.noNewWire(200 * time.Millisecond)
	require.Equal(t, turns, h.store.settlementCount())
}

// Incremental turns (client WS) do not resend history: each new message counts
// once, and the bound still stops a thread whose own messages reach the retained-history limit,
// before dispatch and without opening another thread.
func TestCloudIncrementalTurnsStillBoundRetainedInput(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	h.allow(time.Minute)
	wire, _, lastID := h.establish()
	message := cloudUserMessage(strings.Repeat("x", OpenAICloudMaxRetainedBytes/8-64<<10))
	for turn := 2; turn <= 9; turn++ {
		response := h.continueTurn(wire, turn, map[string]any{"model": "client-model", "previous_response_id": lastID, "input": []any{message}, "tools": cloudHistoryTools})
		lastID = response.Get("id").String()
	}
	_, err := h.invoke(map[string]any{"model": "client-model", "previous_response_id": lastID, "input": []any{message}, "tools": cloudHistoryTools})
	status, code, _, ok := cloudPublicError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "cloud_invalid_request", code)
	require.Empty(t, wire.writes, "the rejected turn never reached the thread")
	h.noNewWire(100 * time.Millisecond)
	require.Equal(t, 9, h.store.settlementCount())
}

// An edited/forked history or changed thread settings cannot continue the old
// thread, but they are valid full-history requests: start a new thread.
func TestCloudFullHistoryDivergingFromLiveSessionStartsNewThread(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any, []any) []any{
		"edited_history": func(_ map[string]any, history []any) []any {
			edited := append([]any{}, history...)
			edited[0] = cloudUserMessage("edited hello")
			return edited
		},
		"unknown_old_call": func(_ map[string]any, history []any) []any {
			extra := []any{
				map[string]any{"type": "function_call", "call_id": "call_cloud_00000000-0000-4000-8000-000000000000", "name": "echo", "arguments": "{}"},
				map[string]any{"type": "function_call_output", "call_id": "call_cloud_00000000-0000-4000-8000-000000000000", "output": "old"},
			}
			return append(append([]any{}, history...), extra...)
		},
		"changed_instructions": func(body map[string]any, history []any) []any {
			body["instructions"] = "new instructions"
			return history
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			_, history, _ := h.establish()
			upstream := make(chan struct{})
			go func() {
				defer close(upstream)
				next := h.nextWire()
				cloudWireResponse(t, h.ctx, next, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread-two"}})
				cloudWireResponse(t, h.ctx, next, "thread/inject_items", map[string]any{})
				cloudWireResponse(t, h.ctx, next, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
				cloudWireCompleteTurn(next, "thread-two", "turn-two", 30)
			}()
			body := map[string]any{"model": "client-model", "tools": cloudHistoryTools}
			full := append(mutate(body, history), cloudUserMessage("again"))
			body["input"] = full
			response, err := h.invoke(body)
			require.NoError(t, err, response.Raw)
			require.Equal(t, "completed", response.Get("status").String(), response.Raw)
			<-upstream
		})
	}
}

// An incremental tool reply (no matching function_call in the request) can
// only be delivered to its live pending RPC; it must never start a new turn.
func TestCloudOrphanToolReplyWithoutLiveSessionIsRejected(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	_, err := h.invoke(map[string]any{"model": "client-model", "tools": cloudHistoryTools,
		"input": []any{map[string]any{"type": "function_call_output", "call_id": "call_cloud_00000000-0000-4000-8000-000000000000", "output": "late"}}})
	require.Error(t, err)
	status, _, _, ok := cloudPublicError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, http.StatusBadRequest, status)
	h.noNewWire(100 * time.Millisecond)
}

// Over client WS the same stale previous_response_id is a per-turn error event
// with the native code; the connection stays usable for a full-history retry.
func TestCloudWebSocketStalePreviousResponseIsPerTurnError(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, lastID := h.establish()
	record := h.store.all()[0].snapshot()
	archived := make(chan struct{})
	go func() {
		defer close(archived)
		cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
	}()
	require.NoError(t, h.svc.cloudSessions.CloseCompleted(h.ctx, record.OpenAICloudOwner, record.SessionID))
	<-archived

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
	conn, _, err := coderws.Dial(h.ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	send := func(body map[string]any) {
		body["type"] = "response.create"
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		require.NoError(t, conn.Write(h.ctx, coderws.MessageText, raw))
	}
	send(map[string]any{"model": "client-model", "previous_response_id": lastID, "input": []any{cloudUserMessage("again")}, "tools": cloudHistoryTools})
	_, raw, err := conn.Read(h.ctx)
	if err != nil {
		select {
		case serverErr := <-ended:
			t.Fatalf("server ended the connection: %v", serverErr)
		case <-time.After(time.Second):
		}
	}
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(raw, "type").String(), string(raw))
	require.Equal(t, "previous_response_not_found", gjson.GetBytes(raw, "error.code").String(), string(raw))
	send(map[string]any{"model": "client-model", "input": "again", "tool_choice": "required"})
	_, raw, err = conn.Read(h.ctx)
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(raw, "type").String(), string(raw))
	require.Equal(t, "cloud_unsupported_parameter", gjson.GetBytes(raw, "error.code").String(), string(raw))
	require.Equal(t, "tool_choice", gjson.GetBytes(raw, "error.param").String(), string(raw))

	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		next := h.nextWire()
		cloudWireResponse(t, h.ctx, next, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread-two"}})
		cloudWireResponse(t, h.ctx, next, "thread/inject_items", map[string]any{})
		cloudWireResponse(t, h.ctx, next, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(next, "thread-two", "turn-two", 30)
	}()
	send(map[string]any{"model": "client-model", "input": append(append([]any{}, history...), cloudUserMessage("again")), "tools": cloudHistoryTools})
	for {
		_, raw, err := conn.Read(h.ctx)
		require.NoError(t, err)
		kind := gjson.GetBytes(raw, "type").String()
		require.NotEqual(t, "error", kind, string(raw))
		if kind == "response.completed" {
			break
		}
	}
	<-upstream
	require.NoError(t, conn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-ended:
		require.NoError(t, err)
	case <-h.ctx.Done():
		t.Fatal(h.ctx.Err())
	}
}

var cloudHistorySearchTools = []any{map[string]any{"type": "web_search"}}

// establishSearch runs one turn that publishes a hosted web search and text,
// and returns the full history a store:false client replays next.
func (h *cloudHistoryRoutingHarness) establishSearch() (*cloudRPCFakeConn, []any) {
	h.t.Helper()
	wireReady := make(chan *cloudRPCFakeConn, 1)
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		wire := h.nextWire()
		wireReady <- wire
		cloudWireResponse(h.t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		cloudWireResponse(h.t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		search := map[string]any{"id": "search-upstream", "type": "webSearch", "action": map[string]any{"type": "search", "query": "docs"}}
		cloudTestRead(wire, map[string]any{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": search}})
		cloudTestRead(wire, map[string]any{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": search}})
		cloudWireCompleteTurn(wire, "thread", "turn", 12)
	}()
	first, err := h.invoke(map[string]any{"model": "client-model", "input": []any{cloudUserMessage("search")}, "tools": cloudHistorySearchTools})
	require.NoError(h.t, err, first.Raw)
	require.Equal(h.t, "completed", first.Get("status").String(), first.Raw)
	require.Equal(h.t, "web_search_call", first.Get("output.0.type").String(), first.Raw)
	<-upstream
	history := []any{cloudUserMessage("search")}
	for _, item := range first.Get("output").Array() {
		history = append(history, item.Value())
	}
	return <-wireReady, history
}

// Hosted outputs the gateway published are valid history for the next turn:
// the live thread already holds them, so they continue it without re-running.
func TestCloudHostedOutputHistoryContinuesLiveThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history := h.establishSearch()
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		turn := cloudTestMessage(t, h.ctx, wire)
		require.Equal(t, "turn/start", turn.Method)
		cloudTestRead(wire, map[string]any{"id": turn.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-two"}}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "input": append(history, cloudUserMessage("again")), "tools": cloudHistorySearchTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	h.noNewWire(200 * time.Millisecond)
	require.Equal(t, 2, h.store.settlementCount())
}

// When that thread is gone, the published hosted output is injected as
// completed history into the new thread instead of being rejected or dropped.
func TestCloudHostedOutputHistoryIsInjectedIntoNewThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history := h.establishSearch()
	record := h.store.all()[0].snapshot()
	archived := make(chan struct{})
	go func() {
		defer close(archived)
		cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
	}()
	require.NoError(t, h.svc.cloudSessions.CloseCompleted(h.ctx, record.OpenAICloudOwner, record.SessionID))
	<-archived
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		next := h.nextWire()
		cloudWireResponse(t, h.ctx, next, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread-two"}})
		inject := cloudTestMessage(t, h.ctx, next)
		require.Equal(t, "thread/inject_items", inject.Method)
		search := gjson.GetBytes(inject.Params, `items.#(type=="web_search_call")`)
		require.True(t, search.Exists(), string(inject.Params))
		require.Equal(t, "completed", search.Get("status").String())
		require.Equal(t, "docs", search.Get("action.query").String())
		cloudTestRead(next, map[string]any{"id": inject.ID, "result": map[string]any{}})
		cloudWireResponse(t, h.ctx, next, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(next, "thread-two", "turn-two", 30)
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "input": append(history, cloudUserMessage("again")), "tools": cloudHistorySearchTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	require.Equal(t, 2, h.store.settlementCount(), "replayed hosted history is never billed again")
}

// Only hosted items this gateway published can be replayed; a foreign or
// incomplete hosted item is not something Cloud can verify or inject.
func TestCloudHostedHistoryRejectsForeignItems(t *testing.T) {
	// A search or image Cloud did not run (native or BPS turns before Cloud was
	// enabled) is omitted from the thread, never presented as Cloud's own.
	for name, item := range map[string]map[string]any{
		"native_search": {"type": "web_search_call", "id": "ws_native", "status": "completed", "action": map[string]any{"type": "search", "query": "docs"}},
		"native_image":  {"type": "image_generation_call", "id": "ig_native", "status": "completed", "result": "not-a-png"},
	} {
		t.Run("omits_"+name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"model": "gpt-5.1", "input": []any{cloudUserMessage("a"), item, cloudUserMessage("b")}})
			require.NoError(t, err)
			p, err := parseCloudProtocolRequest(raw)
			require.NoError(t, err)
			require.Len(t, p.history, 1)
			injected, _ := json.Marshal(cloudInjectionHistory(p))
			require.NotContains(t, string(injected), item["id"].(string))
		})
	}
	// An item claiming this gateway's identity must be exactly what it published.
	for name, item := range map[string]map[string]any{
		"in_progress":  {"type": "web_search_call", "id": "ws_cloud_x", "status": "in_progress", "action": map[string]any{"type": "search", "query": "docs"}},
		"private_key":  {"type": "web_search_call", "id": "ws_cloud_x", "status": "completed", "action": map[string]any{"type": "search", "query": "docs"}, "results": []any{}},
		"image_no_res": {"type": "image_generation_call", "id": "ig_cloud_x", "status": "completed"},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"model": "gpt-5.1", "input": []any{cloudUserMessage("a"), item, cloudUserMessage("b")}})
			require.NoError(t, err)
			_, err = parseCloudProtocolRequest(raw)
			require.Error(t, err)
		})
	}
	raw, err := json.Marshal(map[string]any{"model": "gpt-5.1", "input": []any{cloudUserMessage("a"),
		map[string]any{"type": "web_search_call", "id": "ws_cloud_x", "status": "completed", "action": map[string]any{"type": "search", "query": "docs", "sources": []any{map[string]any{"type": "url", "url": "https://openai.com"}}}},
		map[string]any{"type": "image_generation_call", "id": "ig_cloud_y", "status": "completed", "result": cloudTinyPNG, "revised_prompt": "p", "output_format": "png", "size": "1x1"},
		cloudUserMessage("b")}})
	require.NoError(t, err)
	p, err := parseCloudProtocolRequest(raw)
	require.NoError(t, err)
	injected, _ := json.Marshal(cloudInjectionHistory(p))
	require.NotContains(t, string(injected), "sources", "public include-only search sources are not upstream history")
	require.NotContains(t, string(injected), "output_format")
	require.Equal(t, cloudTinyPNG, gjson.GetBytes(injected, `#(type=="image_generation_call").result`).String())
}

// Review regression: an earlier submitted tool output followed by commentary
// and a second tool call in the same turn is ordinary full history.
func TestCloudSecondToolReplyAfterCommentaryIsNotMidTurnInput(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		wire := h.nextWire()
		if wire == nil {
			return
		}
		cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
		wire.reads <- []byte(`{"id":"rpc-1","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"up-1","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
		<-wire.writes
		for _, raw := range []map[string]any{
			{"method": "item/started", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "note", "type": "agentMessage"}}},
			{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "turn", "itemId": "note", "delta": "checking"}},
			{"method": "item/completed", "params": map[string]any{"threadId": "thread", "turnId": "turn", "item": map[string]any{"id": "note", "type": "agentMessage", "text": "checking"}}},
		} {
			cloudTestRead(wire, raw)
		}
		wire.reads <- []byte(`{"id":"rpc-2","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"up-2","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
		<-wire.writes
		cloudWireCompleteTurn(wire, "thread", "turn", 12)
	}()
	user := cloudUserMessage("hello")
	first, err := h.invoke(map[string]any{"model": "client-model", "input": []any{user}, "tools": cloudHistoryTools})
	require.NoError(t, err, first.Raw)
	call1 := first.Get("output.0")
	out1 := map[string]any{"type": "function_call_output", "call_id": call1.Get("call_id").String(), "output": "a"}
	second, err := h.invoke(map[string]any{"model": "client-model", "input": []any{user, call1.Value(), out1}, "tools": cloudHistoryTools})
	require.NoError(t, err, second.Raw)
	history := []any{user, call1.Value(), out1}
	for _, item := range second.Get("output").Array() {
		history = append(history, item.Value())
	}
	call2 := second.Get(`output.#(type=="function_call")`)
	require.True(t, call2.Exists(), second.Raw)
	history = append(history, map[string]any{"type": "function_call_output", "call_id": call2.Get("call_id").String(), "output": "b"})
	last, err := h.invoke(map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools})
	require.NoError(t, err, last.Raw)
	require.Equal(t, "completed", last.Get("status").String(), last.Raw)
	<-done
}

// Review regression: an edited last message is a shorter history than the
// thread's. It must not continue the thread that still holds the old answer.
func TestCloudEditedLastMessageDoesNotContinueOldThread(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, _ := h.establish()
	turnTwo := make(chan struct{})
	go func() {
		defer close(turnTwo)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	second, err := h.invoke(map[string]any{"model": "client-model", "input": append(append([]any{}, history...), cloudUserMessage("question two")), "tools": cloudHistoryTools})
	require.NoError(t, err, second.Raw)
	<-turnTwo
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		next := h.nextWire()
		if next == nil {
			return
		}
		cloudWireResponse(t, h.ctx, next, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread-edit"}})
		cloudWireResponse(t, h.ctx, next, "thread/inject_items", map[string]any{})
		cloudWireResponse(t, h.ctx, next, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-edit"}})
		cloudWireCompleteTurn(next, "thread-edit", "turn-edit", 30)
	}()
	edited, err := h.invoke(map[string]any{"model": "client-model", "input": append(append([]any{}, history...), cloudUserMessage("question two, edited")), "tools": cloudHistoryTools})
	require.NoError(t, err, edited.Raw)
	require.Equal(t, "completed", edited.Get("status").String(), edited.Raw)
	<-upstream
	require.Empty(t, wire.writes, "the old thread received nothing for the edited branch")
}

// Review regression: older full-history clients had to strip hosted items.
// Omitting them stays valid; supplying them must match what was published.
func TestCloudHostedHistoryItemsAreOptionalInReplay(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history := h.establishSearch()
	var stripped []any
	for _, item := range history {
		if m, _ := item.(map[string]any); m["type"] != "web_search_call" {
			stripped = append(stripped, item)
		}
	}
	require.Less(t, len(stripped), len(history))
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	response, err := h.invoke(map[string]any{"model": "client-model", "input": append(stripped, cloudUserMessage("again")), "tools": cloudHistorySearchTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	h.noNewWire(200 * time.Millisecond)
}

// An exact completed reply may reuse the selected Cloud owner without billing.
// Once that owner is closed, full paired history remains eligible for scheduling.
func TestCloudReplayedFinalToolReplyIsIdempotentThenPortable(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, _ := h.establish()
	// history = [user, function_call, function_call_output, assistant message]
	reply := history[:3]
	again, err := h.invoke(map[string]any{"model": "client-model", "input": reply, "tools": cloudHistoryTools})
	require.NoError(t, err, again.Raw)
	require.Equal(t, "completed", again.Get("status").String(), again.Raw)
	require.Equal(t, 1, h.store.settlementCount(), "a replay is not another turn")
	record := h.store.all()[0].snapshot()
	archived := make(chan struct{})
	go func() {
		defer close(archived)
		cloudWireResponse(t, h.ctx, wire, "thread/archive", map[string]any{})
	}()
	require.NoError(t, h.svc.cloudSessions.CloseCompleted(h.ctx, record.OpenAICloudOwner, record.SessionID))
	<-archived
	raw, err := json.Marshal(map[string]any{"model": "client-model", "input": reply, "tools": cloudHistoryTools})
	require.NoError(t, err)
	ctx, err := h.svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
	require.NoError(t, err)
	require.False(t, HasOpenAICloudContinuation(ctx), "complete closed history follows ordinary protocol selection")
	require.Empty(t, wire.writes, "migration lookup must not contact the old thread")
	require.Equal(t, 1, h.store.settlementCount())
}

// An earlier reply cannot answer a newer live tool call. Once that work has
// completed, full history may migrate; an explicit previous reference still
// targets the old owner and cannot replay an earlier turn's result.
func TestCloudEarlierTurnToolReplyProtectsPendingOwner(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, lastID := h.establish()
	// history = [user, function_call, function_call_output, assistant message]
	stale := []map[string]any{
		{"model": "client-model", "input": history[:3], "tools": cloudHistoryTools},
		{"model": "client-model", "previous_response_id": lastID, "input": history[2:3], "tools": cloudHistoryTools},
	}
	rejected := func(state string, bodies ...map[string]any) {
		t.Helper()
		for _, body := range bodies {
			_, err := h.invoke(body)
			status, code, _, ok := cloudPublicError(err)
			require.True(t, ok, "%s: %v", state, err)
			require.Equal(t, http.StatusBadRequest, status, state)
			require.Equal(t, "cloud_invalid_request", code, state)
		}
		require.Empty(t, wire.writes, "%s: an earlier turn's reply never reaches the thread", state)
	}
	upstream := make(chan struct{})
	go func() {
		defer close(upstream)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-two"}})
		wire.reads <- []byte(`{"id":"tool-rpc-two","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn-two","callId":"upstream-call-two","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
	}()
	history = append(history, cloudUserMessage("question two"))
	second, err := h.invoke(map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools})
	require.NoError(t, err, second.Raw)
	<-upstream
	call := second.Get("output.0")
	require.Equal(t, "function_call", call.Get("type").String(), second.Raw)
	rejected("explicit previous while turn two waits for its tool", stale[1])
	oldHistory, err := json.Marshal(stale[0])
	require.NoError(t, err)
	independent, err := h.svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, oldHistory)
	require.NoError(t, err)
	require.False(t, HasOpenAICloudContinuation(independent), "a completed old pair is portable history, not a reply to the newer tool")
	upstream = make(chan struct{})
	go func() {
		defer close(upstream)
		reply := cloudTestMessage(t, h.ctx, wire)
		require.JSONEq(t, `"tool-rpc-two"`, string(reply.ID))
		cloudWireCompleteTurn(wire, "thread", "turn-two", 30)
	}()
	history = append(history, call.Value(), map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "answer two"})
	last, err := h.invoke(map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools})
	require.NoError(t, err, last.Raw)
	require.Equal(t, "completed", last.Get("status").String(), last.Raw)
	<-upstream
	rejected("explicit previous after turn two completed", stale[1])
	raw, err := json.Marshal(stale[0])
	require.NoError(t, err)
	ctx, err := h.svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
	require.NoError(t, err)
	require.False(t, HasOpenAICloudContinuation(ctx), "completed paired history may select a different protocol")
	require.Empty(t, wire.writes)
	require.Equal(t, 2, h.store.settlementCount())
}

// Review regression: a replayed generated image is injected only after the
// same inline-image envelope checks as any input image.
func TestCloudReplayedGeneratedImageIsValidated(t *testing.T) {
	for name, result := range map[string]string{"not_base64": "%%%", "not_png": "aGVsbG8="} {
		t.Run(name, func(t *testing.T) {
			_, err := cloudShapeParse(t, map[string]any{"input": []any{cloudUserMessage("a"),
				map[string]any{"type": "image_generation_call", "id": "ig_cloud_x", "status": "completed", "result": result}, cloudUserMessage("b")}})
			require.Error(t, err)
		})
	}
}

// Real Cloud emits hundreds of injected raw items before turn/start replies.
// Preserve live events which precede the reply as well as the historical burst.
func TestCloudLargeInjectedHistoryAcrossIngress(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			history := make([]any, 0, 401)
			for i := range 200 {
				history = append(history, cloudUserMessage(fmt.Sprint(i)), map[string]any{"role": "assistant", "content": "historical text"})
			}
			history = append(history, cloudUserMessage("continue"))
			done := make(chan struct{})
			go func() {
				defer close(done)
				wire := h.nextWire()
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
				inject := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "thread/inject_items", inject.Method)
				require.Equal(t, int64(400), gjson.GetBytes(inject.Params, "items.#").Int())
				echo := func(i int) {
					cloudTestRead(wire, map[string]any{"method": "rawResponseItem/completed", "params": map[string]any{"threadId": "thread", "turnId": "auto-compact-0", "item": map[string]any{"type": "message", "id": fmt.Sprint(i), "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "historical text"}}}}})
				}
				for i := range 100 {
					echo(i)
				}
				cloudTestRead(wire, map[string]any{"id": inject.ID, "result": map[string]any{}})
				start := cloudTestMessage(t, h.ctx, wire)
				require.Equal(t, "turn/start", start.Method)
				for i := 100; i < 400; i++ {
					echo(i)
				}
				cloudWireCompleteTurn(wire, "thread", "turn", 12)
				cloudTestRead(wire, map[string]any{"id": start.ID, "result": map[string]any{"turn": map[string]any{"id": "turn"}}})
			}()
			response, err := cloudRecoveryInvoke(t, h, map[string]any{"model": "client-model", "input": history}, transport)
			require.NoError(t, err)
			require.Equal(t, "completed", response.Get("status").String(), response.Raw)
			require.Equal(t, "ok", response.Get("output.0.content.0.text").String())
			require.Equal(t, int64(1), response.Get("output.#").Int(), "history echoes must not become live output")
			require.Equal(t, int64(12), response.Get("usage.total_tokens").Int())
			<-done
			require.Equal(t, 1, h.store.settlementCount())
			require.Len(t, h.store.all(), 1, "the burst never caused an inference replay")
		})
	}
}

// Codex forks copy the parent's pending call and synthesize its missing output.
// That historical pair must never answer the parent's live tool RPC.
func TestCloudForkConversationPreservesParentPendingTool(t *testing.T) {
	for _, transport := range []string{"json", "sse", "ws"} {
		t.Run(transport, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			ctx, cancel := context.WithCancel(h.ctx)
			h.ctx = ctx
			defer cancel()
			invoke := func(session string, body map[string]any) (gjson.Result, error) {
				raw, err := json.Marshal(body)
				if err != nil {
					return gjson.Result{}, err
				}
				requestCtx, err := h.svc.CaptureCloudBillingSnapshot(ctx, h.in)
				if err != nil {
					return gjson.Result{}, err
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
				c.Request.Header.Set("session_id", session)
				c.Set("api_key", h.in.APIKey)
				SetOpenAIHTTPResponseOwner(c, h.in.User.ID, h.in.APIKey.ID)
				requestCtx, err = h.svc.PrepareCloudContinuation(requestCtx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw, c.Request.Header)
				if err != nil {
					return gjson.Result{}, err
				}
				_, err = h.svc.forwardOpenAICloud(requestCtx, c, h.in.Account, raw)
				return gjson.Parse(rec.Body.String()), err
			}
			parentReady := make(chan *cloudRPCFakeConn, 1)
			go func() {
				wire := h.nextWire()
				cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "parent-thread"}})
				cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "parent-turn"}})
				cloudTestRead(wire, map[string]any{"id": "parent-tool", "method": "item/tool/call", "params": map[string]any{"threadId": "parent-thread", "turnId": "parent-turn", "callId": "parent-call", "namespace": "sub2api_client", "tool": "echo", "arguments": map[string]any{}}})
				parentReady <- wire
			}()
			initial := cloudUserMessage("Parent task")
			first, err := invoke("parent", map[string]any{"model": "client-model", "input": []any{initial}, "tools": cloudHistoryTools})
			require.NoError(t, err)
			require.Equal(t, "function_call", first.Get("output.0.type").String(), first.Raw)
			parent := <-parentReady
			call := first.Get("output.0.call_id").String()
			history := []any{initial}
			for _, x := range first.Get("output").Array() {
				history = append(history, x.Value())
			}
			history = append(history, map[string]any{"type": "function_call_output", "call_id": call, "output": "Tool execution aborted."}, map[string]any{"type": "agent_message", "author": "parent", "recipient": "child", "content": []any{map[string]any{"type": "input_text", "text": "Independent child task"}}})
			var child gjson.Result
			var childErr error
			done := make(chan struct{})
			go func() {
				defer close(done)
				child, childErr = cloudRecoveryInvoke(t, h, map[string]any{"model": "client-model", "input": history, "tools": cloudHistoryTools}, transport, http.Header{"Session_id": []string{"child"}})
			}()
			defer func() { cancel(); <-done }()
			var wire *cloudRPCFakeConn
			select {
			case wire = <-h.wires:
			case raw := <-parent.writes:
				t.Fatalf("child dispatched to parent's pending RPC: %s", gjson.GetBytes(raw, "method").String())
			case <-done:
				t.Fatalf("child rejected instead of receiving its own session: %v %s", childErr, child.Raw)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			cloudWireResponse(t, ctx, wire, "initialize", map[string]any{})
			require.Equal(t, "initialized", cloudTestMessage(t, ctx, wire).Method)
			cloudWireResponse(t, ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "child-thread"}})
			cloudWireResponse(t, ctx, wire, "thread/inject_items", map[string]any{})
			cloudWireResponse(t, ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "child-turn"}})
			cloudWireCompleteTurn(wire, "child-thread", "child-turn", 12)
			<-done
			require.NoError(t, childErr)
			require.Equal(t, "completed", child.Get("status").String(), child.Raw)
			require.Equal(t, "awaiting_tools", h.store.all()[0].snapshot().Status)
			parentDone := make(chan struct{})
			go func() {
				defer close(parentDone)
				reply := cloudTestMessage(t, ctx, parent)
				require.JSONEq(t, `"parent-tool"`, string(reply.ID))
				cloudWireCompleteTurn(parent, "parent-thread", "parent-turn", 17)
			}()
			completed, err := invoke("parent", map[string]any{"model": "client-model", "previous_response_id": first.Get("id").String(), "input": []any{map[string]any{"type": "function_call_output", "call_id": call, "output": "Real parent output"}}, "tools": cloudHistoryTools})
			require.NoError(t, err)
			require.Equal(t, "completed", completed.Get("status").String(), completed.Raw)
			<-parentDone
			require.Len(t, h.store.all(), 2)
			require.Equal(t, 2, h.store.settlementCount())
		})
	}
}

func TestCloudConversationAffinityKeepsReferenceAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, session                                  string
		paired, previous, legacy, wantRoute, wantError bool
	}{
		{"same conversation", "parent", true, false, false, true, false},
		{"fork complete history", "child", true, false, false, false, false},
		{"fork unpaired output", "child", false, false, false, false, true},
		{"explicit response authority", "child", false, true, false, true, false},
		{"legacy record", "child", true, false, true, true, false},
		{"no header", "", true, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newCloudLiveRoutingStore()
			owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4}
			hash := DeriveSessionHashFromSeed("parent")
			if tc.legacy {
				hash = ""
			}
			record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "parent-owner", ConversationHash: hash, Status: "awaiting_tools"}}
			require.NoError(t, store.CreateCloudSession(t.Context(), record))
			store.refs["call_cloud_parent"] = true
			store.refs["resp_cloud_parent"] = true
			svc := &OpenAICloudSessions{store: store}
			input := []any{}
			if tc.paired {
				input = append(input, cloudUserMessage("old"), map[string]any{"type": "function_call", "call_id": "call_cloud_parent", "name": "echo", "arguments": "{}"})
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": "call_cloud_parent", "output": "result"})
			body := map[string]any{"model": "gpt-5.1", "input": input, "prompt_cache_key": "child", "client_metadata": map[string]any{"session_id": "child"}}
			if tc.previous {
				body["previous_response_id"] = "resp_cloud_parent"
			}
			raw, _ := json.Marshal(body)
			p, err := parseCloudProtocolRequest(raw)
			require.NoError(t, err)
			header := http.Header{}
			header.Set("session_id", tc.session)
			ctx := context.WithValue(t.Context(), cloudConversationKey{}, cloudConversationHash(header))
			got, err := svc.resolveProtocol(ctx, 1, 2, 3, p)
			if tc.wantError {
				require.ErrorIs(t, err, errCloudToolCallExpired)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantRoute, got != nil)
			if got != nil {
				require.Equal(t, "parent-owner", got.SessionID)
			}
		})
	}
	t.Run("fork does not excuse unrelated expired current result", func(t *testing.T) {
		store := newCloudLiveRoutingStore()
		owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4}
		record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "parent-owner", ConversationHash: DeriveSessionHashFromSeed("parent"), Status: "awaiting_tools"}}
		require.NoError(t, store.CreateCloudSession(t.Context(), record))
		store.refs["call_cloud_parent"] = true
		svc := &OpenAICloudSessions{store: store}
		body := map[string]any{"model": "gpt-5.1", "input": []any{
			cloudUserMessage("old"),
			map[string]any{"type": "function_call", "call_id": "call_cloud_parent", "name": "echo", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": "call_cloud_parent", "output": "aborted"},
			map[string]any{"type": "function_call_output", "call_id": "call_cloud_missing", "output": "result"},
		}}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		p, err := parseCloudProtocolRequest(raw)
		require.NoError(t, err)
		ctx := context.WithValue(t.Context(), cloudConversationKey{}, DeriveSessionHashFromSeed("child"))
		_, err = svc.resolveProtocol(ctx, 1, 2, 3, p)
		require.ErrorIs(t, err, errCloudToolCallExpired)
	})
	require.Empty(t, cloudConversationHash(http.Header{"X-Request-Id": []string{"rotating"}, "Prompt_cache_key": []string{"cache"}}))
}

// Completed Cloud call IDs are history, not a permanent protocol/account pin.
// The new request chooses its target through normal scheduling; only a live
// pending RPC or an explicit previous_response_id retains the old owner.
func TestCloudCompleteHistoryDoesNotPinDepartedProtocol(t *testing.T) {
	for _, scenario := range []struct {
		name, state          string
		paired, previous     bool
		wantBound, wantError bool
	}{
		{"completed full pair", "completed", true, false, false, false},
		{"closed full pair", "closed", true, false, false, false},
		{"missing historical owner full pair", "", true, false, false, false},
		{"pending full pair", "awaiting_tools", true, false, true, false},
		{"completed explicit previous", "completed", true, true, true, false},
		{"closed explicit previous", "closed", true, true, false, true},
		{"missing explicit previous", "", true, true, false, true},
		{"bare output with missing owner", "", false, false, false, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store := newCloudLiveRoutingStore()
			owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4}
			if scenario.state != "" {
				record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "old-cloud-owner", Status: scenario.state, RootRequestID: "original-billing-root"}}
				require.NoError(t, store.CreateCloudSession(t.Context(), record))
				store.refs["call_cloud_old"] = true
				store.refs["resp_cloud_old"] = true
			}
			svc := &OpenAIGatewayService{cloudSessions: &OpenAICloudSessions{store: store}}
			input := []any{}
			if scenario.paired {
				input = append(input, cloudUserMessage("Earlier task"), map[string]any{"type": "function_call", "call_id": "call_cloud_old", "name": "echo", "arguments": "{}"})
			}
			input = append(input, map[string]any{"type": "function_call_output", "call_id": "call_cloud_old", "output": "already executed"})
			body := map[string]any{"model": "m", "input": input}
			if scenario.previous {
				body["previous_response_id"] = "resp_cloud_old"
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			before, err := json.Marshal(store.snapshot())
			require.NoError(t, err)
			ctx, err := svc.PrepareCloudContinuation(t.Context(), 1, 2, 3, raw)
			if scenario.wantError {
				require.Error(t, err)
				if scenario.previous {
					require.ErrorIs(t, err, errCloudPreviousResponseNotFound)
				} else {
					require.ErrorIs(t, err, errCloudToolCallExpired)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, scenario.wantBound, HasOpenAICloudContinuation(ctx), "full closed history must remain eligible for native or BPS scheduling")
				if scenario.wantBound {
					binding := cloudContinuationFromContext(ctx)
					require.Equal(t, owner, binding.OpenAICloudOwner)
					require.Equal(t, "original-billing-root", binding.RootRequestID)
				}
			}
			after, err := json.Marshal(store.snapshot())
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "routing must not execute, settle, or mutate the old session")
			require.Zero(t, store.settlementCount())
		})
	}
}

// A Cloud marker in old, paired history does not select Cloud for this turn.
// Native hosted history must not acquire Cloud-only capability restrictions
// before the ordinary scheduler has selected the destination protocol.
func TestCloudHistoricalMarkerDoesNotApplyCloudTargetValidation(t *testing.T) {
	store := newCloudLiveRoutingStore()
	record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4}, SessionID: "old-cloud-owner", Status: "completed"}}
	require.NoError(t, store.CreateCloudSession(t.Context(), record))
	store.refs["call_cloud_old"] = true
	svc := &OpenAIGatewayService{cloudSessions: &OpenAICloudSessions{store: store}}
	body, err := json.Marshal(map[string]any{"model": "m", "input": []any{
		cloudUserMessage("Earlier task"),
		map[string]any{"type": "function_call", "call_id": "call_cloud_old", "name": "echo", "arguments": "{}"},
		map[string]any{"type": "function_call_output", "call_id": "call_cloud_old", "output": "already executed"},
		map[string]any{"type": "mcp_call", "id": "mcp_native", "server_label": "docs", "name": "lookup", "arguments": "{}", "output": "native hosted result"},
		cloudUserMessage("Continue with the selected protocol"),
	}})
	require.NoError(t, err)
	ctx, err := svc.PrepareCloudContinuation(t.Context(), 1, 2, 3, body)
	require.NoError(t, err, "ownership projection must not run Cloud target capability validation")
	require.False(t, HasOpenAICloudContinuation(ctx))
	require.Zero(t, store.settlementCount())
}

// A prior completed call must not pin a new full-history turn to a later live
// turn in the same session (including clients that omit conversation headers).
func TestCloudHistoricalCallsDoNotPinLaterPendingTurn(t *testing.T) {
	h := newCloudHistoryRoutingHarness(t)
	wire, history, lastID := h.establish()
	done := make(chan struct{})
	go func() {
		defer close(done)
		cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn-next"}})
		cloudTestRead(wire, map[string]any{"id": "next-rpc", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "turn-next", "callId": "new-call", "namespace": cloudClientToolNamespace, "tool": "echo", "arguments": map[string]any{}}})
	}()
	next, err := h.invoke(map[string]any{"model": "client-model", "previous_response_id": lastID, "input": "parent's next turn", "tools": cloudHistoryTools})
	require.NoError(t, err, next.Raw)
	<-done
	pending := next.Get("output.#(type==\"function_call\")")
	require.True(t, pending.Exists(), next.Raw)
	lookup := func(input []any, previous string) *OpenAICloudBinding {
		t.Helper()
		body := map[string]any{"model": "client-model", "input": input, "tools": cloudHistoryTools}
		if previous != "" {
			body["previous_response_id"] = previous
		}
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		ctx, err := h.svc.PrepareCloudContinuation(h.ctx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
		require.NoError(t, err)
		return cloudContinuationFromContext(ctx)
	}
	require.Nil(t, lookup(append(append([]any{}, history...), cloudUserMessage("a new independent branch")), ""), "old complete pairs are history, not a pending RPC")
	require.NotNil(t, lookup([]any{map[string]any{"type": "function_call_output", "call_id": pending.Get("call_id").String(), "output": "done"}}, ""), "current tool reply remains owner-bound")
	require.NotNil(t, lookup(append(append([]any{}, history...), cloudUserMessage("explicit owner")), next.Get("id").String()), "explicit response identity remains authoritative")
	require.Empty(t, wire.writes, "classification must not execute or interrupt either turn")
}
