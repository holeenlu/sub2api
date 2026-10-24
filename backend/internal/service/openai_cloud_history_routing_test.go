package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
func (h *cloudHistoryRoutingHarness) invoke(body map[string]any) (gjson.Result, error) {
	h.t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(h.t, err)
	requestCtx, err := h.svc.CaptureCloudBillingSnapshot(h.ctx, h.in)
	require.NoError(h.t, err)
	requestCtx, err = h.svc.PrepareCloudContinuation(requestCtx, h.in.User.ID, h.in.APIKey.ID, *h.in.APIKey.GroupID, raw)
	if err != nil {
		return gjson.Result{}, err
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
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

// A completed session that the owner released (idle expiry or capacity) must
// not turn every later store:false request into a failed continuation. The old
// call IDs are history: start a new thread and inject the complete history.
func TestCloudFullHistoryAfterReleasedSessionStartsNewThread(t *testing.T) {
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
	response, err := h.invoke(map[string]any{"model": "client-model", "input": full, "tools": cloudHistoryTools})
	require.NoError(t, err, response.Raw)
	require.Equal(t, "completed", response.Get("status").String(), response.Raw)
	<-upstream
	require.Equal(t, 2, h.store.settlementCount())
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

// A client may queue user/developer input while a tool runs and send it with
// the tool result. Cloud cannot add messages to a running turn; this is a stable
// explicit error, not a misleading "history changed" rejection or a dropped message.
func TestCloudMessagesAfterPendingToolOutputHaveExplicitError(t *testing.T) {
	for _, role := range []string{"user", "developer"} {
		t.Run(role, func(t *testing.T) {
			h := newCloudHistoryRoutingHarness(t)
			go func() {
				wire := h.nextWire()
				if wire == nil {
					return
				}
				cloudWireResponse(t, h.ctx, wire, "thread/start", map[string]any{"model": "gpt-5.1", "thread": map[string]any{"id": "thread"}})
				cloudWireResponse(t, h.ctx, wire, "turn/start", map[string]any{"turn": map[string]any{"id": "turn"}})
				wire.reads <- []byte(`{"id":"tool-rpc","method":"item/tool/call","params":{"threadId":"thread","turnId":"turn","callId":"up","namespace":"sub2api_client","tool":"echo","arguments":{}}}`)
			}()
			userOne := cloudUserMessage("hello")
			first, err := h.invoke(map[string]any{"model": "client-model", "input": []any{userOne}, "tools": cloudHistoryTools})
			require.NoError(t, err, first.Raw)
			call := first.Get("output.0")
			queued := map[string]any{"type": "message", "role": role, "content": []any{map[string]any{"type": "input_text", "text": "while you work"}}}
			_, err = h.invoke(map[string]any{"model": "client-model", "tools": cloudHistoryTools, "input": []any{userOne, call.Value(),
				map[string]any{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "answer"}, queued}})
			require.Error(t, err)
			status, code, _, ok := cloudPublicError(err)
			require.True(t, ok, "%v", err)
			require.Equal(t, http.StatusBadRequest, status)
			require.Equal(t, "cloud_mid_turn_input_unsupported", code)
		})
	}
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

// Review regression: replaying the exact last tool reply of a completed turn
// is idempotent; after the session is gone it is cloud_tool_call_expired.
func TestCloudReplayedFinalToolReplyIsIdempotentThenExpired(t *testing.T) {
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
	_, err = h.invoke(map[string]any{"model": "client-model", "input": reply, "tools": cloudHistoryTools})
	require.Error(t, err)
	status, code, _, ok := cloudPublicError(err)
	require.True(t, ok, "%v", err)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "cloud_tool_call_expired", code)
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
