package service

// Adapted from huoji120/sub4api a3eaf63a. Version 2 shares HTTP/WS markers;
// diagnostics never change legacy first_token_ms, billing or replay decisions.
import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const openAIForwardTimingKey = "openai_forward_timing"

type openAIRequestTimingKey struct{}

// Derived connection contexts share only this current-turn pointer. Each attempt
// retains its immutable root, so late callbacks never write into the next turn.
type openAIRequestTimingState struct {
	current atomic.Pointer[openAIRequestTiming]
}
type openAITransportTimingKey struct{}

// Only bounded timestamps/counters are retained, never request or response bodies.
type openAIRequestTiming struct {
	mu             sync.Mutex
	started        time.Time
	turn, attempts int
	stages         map[string]int64
	log            *zap.Logger
	finished       bool
}

type openAITimingEvent struct {
	valid, semantic, content, text bool
	terminal                       string
	model, tier                    string
	usage                          map[string]int64
}

type openAIForwardTiming struct {
	started                  time.Time
	log                      *zap.Logger
	attempt                  int
	semantic                 atomic.Bool
	flushed                  atomic.Bool
	root                     *openAIRequestTiming
	transport                string
	mu                       sync.Mutex
	stages                   map[string]int64
	terminal                 string
	finished                 bool
	downstreamFailed         bool
	semanticBytes, wireBytes int64
	payloadFull, payloadSent int64
	payloadKnown             bool
	wireKnown                bool
	encoding                 string
	model, effort, tier      string
	actualModel, actualTier  string
	usage                    map[string]int64
	requestShape             string
	clientClass              string
	clientTransport          string
	pendingUp, pendingDown   openAITimingEvent
	readyContent, readyText  bool
	readySemantic            bool
	readyTerminal            bool
	observation              *openAIIncrementalObservation
	observationContext       context.Context
	observationConnID        string
}

// BeginOpenAIRequestTiming captures middleware entry, before routing/slot waits.
// It emits nothing unless an OpenAI-compatible upstream attempt uses the record.
func BeginOpenAIRequestTiming(c *gin.Context, acceptedAt ...time.Time) {
	if c == nil || c.Request == nil {
		return
	}
	state, _ := c.Request.Context().Value(openAIRequestTimingKey{}).(*openAIRequestTimingState)
	if state != nil && len(acceptedAt) == 0 {
		return
	}
	if state == nil {
		state = &openAIRequestTimingState{}
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), openAIRequestTimingKey{}, state))
	}
	started := time.Now()
	if len(acceptedAt) > 0 && !acceptedAt[0].IsZero() {
		started = acceptedAt[0]
	}
	state.current.Store(&openAIRequestTiming{started: started, turn: 1, stages: make(map[string]int64)})
}

func resetOpenAITurnTiming(c *gin.Context, turn int) {
	if c == nil || c.Request == nil || turn <= 1 {
		return
	}
	FinishOpenAIRequestTiming(c)
	root := &openAIRequestTiming{started: time.Now(), turn: turn, stages: make(map[string]int64)}
	BeginOpenAIRequestTiming(c)
	state, _ := c.Request.Context().Value(openAIRequestTimingKey{}).(*openAIRequestTimingState)
	state.current.Store(root)
	c.Set(openAIForwardTimingKey, (*openAIForwardTiming)(nil))
}

func openAIRequestTimingFromContext(ctx context.Context) *openAIRequestTiming {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(openAIRequestTimingKey{}).(*openAIRequestTimingState)
	if state == nil {
		return nil
	}
	return state.current.Load()
}

func (r *openAIRequestTiming) mark(stage string, now time.Time) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finished {
		return
	}
	if _, exists := r.stages[stage]; !exists || stage == "terminal" {
		r.stages[stage] = now.Sub(r.started).Milliseconds()
	}
}

// MarkOpenAIRequestTimingStage accepts only fixed local lifecycle names.
func MarkOpenAIRequestTimingStage(c *gin.Context, stage string) {
	if c == nil || c.Request == nil {
		return
	}
	switch stage {
	case "auth_done", "handler_ready", "user_slot", "account_selected", "account_slot", "routing_done":
	default:
		return
	}
	openAIRequestTimingFromContext(c.Request.Context()).mark(stage, time.Now())
}

func FinishOpenAIRequestTiming(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	if t := openAIForwardTimingFromContext(c); t != nil {
		t.Finish(c.Request.Context().Err())
	}
	r := openAIRequestTimingFromContext(c.Request.Context())
	if r == nil {
		return
	}
	r.finish(c.Writer.Status(), c.Request.Context().Err() != nil)
}
func (r *openAIRequestTiming) finish(status int, canceled bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.finished = true
	fields := []zap.Field{zap.Int("timing_version", 2), zap.Int("turn", r.turn), zap.Int("attempts", r.attempts), zap.Int64("request_duration_ms", time.Since(r.started).Milliseconds()), zap.Any("stages_ms", cloneOpenAITimingStages(r.stages)), zap.Int("http_status", status), zap.Bool("canceled", canceled)}
	log := r.log
	r.mu.Unlock()
	if log != nil {
		log.Info("openai.request_timing", fields...)
	}
}

func openAIForwardTimingFromContext(c *gin.Context) *openAIForwardTiming {
	if c == nil {
		return nil
	}
	value, _ := c.Get(openAIForwardTimingKey)
	timing, _ := value.(*openAIForwardTiming)
	return timing
}

// Call only from the owning request goroutine. Callbacks capture the returned
// synchronized pointer and must not fetch or retain Gin's pooled context.
func newOpenAITransportTiming(c *gin.Context, account *Account, transport string, semanticBytes int64) *openAIForwardTiming {
	if previous := openAIForwardTimingFromContext(c); previous != nil {
		previous.Finish(nil)
	}
	BeginOpenAIRequestTiming(c)
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	root := openAIRequestTimingFromContext(ctx)
	t := newOpenAITransportAttempt(root, openAITimingLogger(ctx, account), transport, string(GetOpenAIClientTransport(c)), openAITimingClientClass(ctx), semanticBytes)
	if c != nil {
		c.Set(openAIForwardTimingKey, t)
	}
	return t
}
func openAITimingLogger(ctx context.Context, account *Account) *zap.Logger {
	var accountID int64
	if account != nil {
		accountID = account.ID
	}
	mode := ""
	if account != nil {
		mode = nativeOpenAIUsageAuthorizationMode(account)
	}
	requestID, _ := ctx.Value(ctxkey.RequestID).(string)
	return logger.FromContext(ctx).With(zap.String("component", "service.openai_gateway"), zap.String("request_id", requestID), zap.Int64("account_id", accountID), zap.String("authorization_mode", mode))
}
func openAITimingClientClass(ctx context.Context) string {
	if d, ok := ctx.Value(gatewayCacheDiagnosticKey{}).(gatewayCacheDiagnostic); ok && d.Client != "" {
		return d.Client
	}
	return "unknown"
}
func newOpenAITransportAttempt(root *openAIRequestTiming, log *zap.Logger, transport, clientTransport, clientClass string, semanticBytes int64) *openAIForwardTiming {
	attempt, turn := 1, 1
	if root != nil {
		root.mu.Lock()
		root.attempts++
		attempt, turn = root.attempts, root.turn
		root.log = log
		root.mu.Unlock()
	}
	switch transport {
	case "http", "http_bridge", "websocket":
	default:
		transport = "unknown"
	}
	t := &openAIForwardTiming{started: time.Now(), log: log.With(zap.Int("attempt", attempt), zap.Int("turn", turn), zap.String("protocol", "responses"), zap.String("timing_scope", "upstream_attempt")), attempt: attempt, root: root, transport: transport, stages: make(map[string]int64), semanticBytes: semanticBytes, wireBytes: -1, clientClass: clientClass, clientTransport: clientTransport}
	t.record("start", zap.String("transport", transport), zap.String("client_transport", clientTransport), zap.String("client_class", clientClass), zap.Int64("semantic_request_bytes", semanticBytes))
	return t
}

// A passthrough relay runs callbacks on two goroutines. Capture immutable
// request identity on its owner goroutine; never retain Gin in these callbacks.
type openAITimingFactory struct {
	state       *openAIRequestTimingState
	lastTurn    int
	mu          sync.Mutex
	root        *openAIRequestTiming
	current     *openAIForwardTiming
	log         *zap.Logger
	clientClass string
}

func newOpenAITimingFactory(c *gin.Context, account *Account) *openAITimingFactory {
	BeginOpenAIRequestTiming(c)
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	state, _ := ctx.Value(openAIRequestTimingKey{}).(*openAIRequestTimingState)
	return &openAITimingFactory{state: state, root: openAIRequestTimingFromContext(ctx), log: openAITimingLogger(ctx, account), clientClass: openAITimingClientClass(ctx)}
}
func (f *openAITimingFactory) BeginTurn(turn int, semanticBytes int64) *openAIForwardTiming {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var published *openAIRequestTiming
	if f.state != nil {
		published = f.state.current.Load()
	}
	changed := f.current != nil && (f.lastTurn != turn || published != f.root)
	if f.current != nil {
		if changed {
			f.current.FinishTurn(nil)
		} else {
			f.current.Finish(nil)
		}
	}
	if f.root == nil || changed {
		// Owner hooks publish the accepted frame before admission/queue waits.
		// Without hooks, start at this callback; a first prewarm explicitly clears root.
		if f.root != nil && published != nil && published != f.root {
			f.root = published
		} else {
			f.root = &openAIRequestTiming{started: time.Now(), turn: turn, stages: make(map[string]int64)}
			if f.state != nil {
				f.state.current.Store(f.root)
			}
		}
	}
	f.lastTurn = turn
	f.current = newOpenAITransportAttempt(f.root, f.log, "websocket", "ws", f.clientClass, semanticBytes)
	return f.current
}
func (f *openAITimingFactory) Finish(err error) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.current != nil {
		f.current.FinishTurn(err)
	}
}
func (t *openAIForwardTiming) FinishTurn(err error) {
	if t == nil {
		return
	}
	t.Finish(err)
	t.root.finish(http.StatusSwitchingProtocols, errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}

func newOpenAIForwardTiming(c *gin.Context, request *http.Request, account *Account) *openAIForwardTiming {
	transport := "http"
	if GetOpenAIClientTransport(c) == OpenAIClientTransportWS {
		transport = "http_bridge"
	}
	size := int64(-1)
	if request != nil {
		size = request.ContentLength
	}
	t := newOpenAITransportTiming(c, account, transport, size)
	if c != nil {
		t.SetTransportReason(c.GetString("openai_ws_transport_reason"))
	}
	return t
}

func (t *openAIForwardTiming) context(ctx context.Context) context.Context {
	if t == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, openAITransportTimingKey{}, t)
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn:           func(string) { t.record("get_conn") },
		GotConn:           func(i httptrace.GotConnInfo) { t.ConnectionReady(i.Reused, -1, -1, -1, i.IdleTime) },
		TLSHandshakeStart: func() { t.record("tls_start") },
		TLSHandshakeDone:  func(_ tls.ConnectionState, err error) { t.record("tls_done", zap.Bool("success", err == nil)) },
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			if i.Err == nil {
				t.MarkRequestWritten(-1)
			} else {
				t.record("request_write_failed")
			}
		},
		GotFirstResponseByte: func() { t.once("first_response_byte") },
	})
}

func (t *openAIForwardTiming) record(stage string, fields ...zap.Field) {
	if t == nil || t.log == nil {
		return
	}
	fields = append(fields, zap.Int("timing_version", 2), zap.String("stage", stage), zap.Int64("elapsed_ms", time.Since(t.started).Milliseconds()))
	if t.root != nil {
		fields = append(fields, zap.Int64("request_elapsed_ms", time.Since(t.root.started).Milliseconds()))
	}
	t.log.Info("openai.forward_timing", fields...)
}

func (t *openAIForwardTiming) once(stage string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	if t.stages == nil {
		t.stages = make(map[string]int64)
	}
	if _, ok := t.stages[stage]; ok {
		t.mu.Unlock()
		return
	}
	t.stages[stage] = now.Sub(t.started).Milliseconds()
	t.mu.Unlock()
	t.root.mark(stage, now)
	t.record(stage)
}

func (t *openAIForwardTiming) markResponse(resp *http.Response, err error) {
	if t == nil {
		return
	}
	if resp != nil {
		t.record("response_headers", zap.Int("status", resp.StatusCode))
		if resp.StatusCode >= 400 {
			t.mu.Lock()
			t.terminal = "http_error"
			t.mu.Unlock()
		}
	}
	if err != nil {
		t.record("upstream_error")
		t.Finish(err)
	}
}

func (t *openAIForwardTiming) MarkSemantic() {
	if t != nil && t.semantic.CompareAndSwap(false, true) {
		t.once("first_semantic")
	}
}
func (t *openAIForwardTiming) MarkFlush() {
	if t == nil {
		return
	}
	t.mu.Lock()
	semantic, content, text := t.readySemantic, t.readyContent, t.readyText
	terminal := t.readyTerminal
	t.readySemantic, t.readyContent, t.readyText = false, false, false
	t.readyTerminal = false
	t.mu.Unlock()
	if semantic && t.flushed.CompareAndSwap(false, true) {
		t.once("first_downstream_flush")
	}
	if content {
		t.once("first_content_flush")
	}
	if text {
		t.once("first_text_flush")
	}
	if terminal {
		// Local Write/Flush success is not proof of client receipt.
		t.once("terminal_output_flush")
	}
}

func classifyOpenAITimingEvent(payload []byte) openAITimingEvent {
	if !gjson.ValidBytes(payload) {
		return openAITimingEvent{}
	}
	typ := gjson.GetBytes(payload, "type").String()
	if typ == "" {
		return openAITimingEvent{}
	}
	if strings.HasPrefix(typ, "message_") || strings.HasPrefix(typ, "content_block_") || typ == "ping" {
		e := openAITimingEvent{valid: true}
		if typ == "message_stop" {
			e.terminal = typ
		}
		e.model = openAITimingModel(gjson.GetBytes(payload, "message.model").String())
		if typ == "content_block_delta" {
			delta := gjson.GetBytes(payload, "delta")
			e.text = delta.Get("type").String() == "text_delta" && delta.Get("text").String() != ""
			e.content = e.text || delta.Get("partial_json").String() != ""
			e.semantic = e.content || delta.Get("thinking").String() != "" || delta.Get("signature").String() != ""
		}
		if typ == "content_block_start" {
			block := gjson.GetBytes(payload, "content_block")
			e.text = block.Get("type").String() == "text" && block.Get("text").String() != ""
			e.content = e.text || block.Get("type").String() == "tool_use"
			e.semantic = e.content || block.Get("thinking").String() != ""
		}
		return e
	}
	e := openAITimingEvent{valid: true}
	switch typ {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "error":
		e.terminal = typ
	}
	e.semantic = typ != "error" && !isOpenAIWSTerminalEvent(typ) && openAIStreamDataStartsClientOutput(string(payload), typ)
	e.content = openAIStreamDataStartsVisibleOutput(string(payload), typ)
	e.text = openAIEventHasOutputText(payload, typ)
	e.model = openAITimingModel(gjson.GetBytes(payload, "response.model").String())
	e.tier = openAITimingTier(gjson.GetBytes(payload, "response.service_tier").String())
	if e.terminal != "" {
		e.usage = openAITimingUsage(gjson.GetBytes(payload, "response.usage"))
	}
	return e
}
func mergeOpenAITimingEvent(dst *openAITimingEvent, src openAITimingEvent) {
	dst.valid = dst.valid || src.valid
	dst.semantic = dst.semantic || src.semantic
	dst.content = dst.content || src.content
	dst.text = dst.text || src.text
	if src.model != "" {
		dst.model = src.model
	}
	if src.tier != "" {
		dst.tier = src.tier
	}
	if src.usage != nil {
		dst.usage = src.usage
	}
	if src.terminal != "" {
		dst.terminal = src.terminal
	}
}
func (t *openAIForwardTiming) observeTimingEvent(e openAITimingEvent) {
	if t == nil || !e.valid {
		return
	}
	t.once("first_event")
	t.mu.Lock()
	if e.model != "" {
		t.actualModel = e.model
	}
	if e.tier != "" {
		t.actualTier = e.tier
	}
	if e.usage != nil {
		t.usage = e.usage
	}
	t.mu.Unlock()
	if e.terminal != "" {
		t.mu.Lock()
		if !t.finished {
			t.terminal = e.terminal
		}
		t.mu.Unlock()
		t.once("terminal")
	}
	if e.semantic {
		t.MarkSemantic()
	}
	if e.content {
		t.once("first_content")
	}
	if e.text {
		t.once("first_text")
	}
}

// ObserveEvent accepts a complete WebSocket event. SSE callers use the same
// classifier but dispatch only at their existing parser's event boundary.
func (t *openAIForwardTiming) ObserveEvent(payload []byte) {
	if t != nil {
		t.observeTimingEvent(classifyOpenAITimingEvent(payload))
		t.observeIncrementalEvent(payload)
	}
}
func (t *openAIForwardTiming) queueSSEEvent(payload []byte) {
	if t == nil {
		return
	}
	e := classifyOpenAITimingEvent(payload)
	t.observeIncrementalEvent(payload)
	t.mu.Lock()
	mergeOpenAITimingEvent(&t.pendingUp, e)
	t.mu.Unlock()
}
func (t *openAIForwardTiming) completeSSEEvent() {
	if t == nil {
		return
	}
	t.mu.Lock()
	e := t.pendingUp
	t.pendingUp = openAITimingEvent{}
	t.mu.Unlock()
	t.observeTimingEvent(e)
}
func (t *openAIForwardTiming) stageSSEDownstream(payload []byte) {
	if t == nil {
		return
	}
	e := classifyOpenAITimingEvent(payload)
	t.mu.Lock()
	mergeOpenAITimingEvent(&t.pendingDown, e)
	t.mu.Unlock()
}
func (t *openAIForwardTiming) completeSSEDownstream() {
	if t == nil {
		return
	}
	t.mu.Lock()
	e := t.pendingDown
	t.pendingDown = openAITimingEvent{}
	t.readySemantic = t.readySemantic || e.semantic
	t.readyContent = t.readyContent || e.content
	t.readyText = t.readyText || e.text
	t.readyTerminal = t.readyTerminal || e.terminal != ""
	t.mu.Unlock()
}

func openAIEventHasOutputText(payload []byte, typ string) bool {
	switch typ {
	case "response.output_text.delta":
		return gjson.GetBytes(payload, "delta").Type == gjson.String && gjson.GetBytes(payload, "delta").String() != ""
	case "response.output_text.done":
		return gjson.GetBytes(payload, "text").Type == gjson.String && gjson.GetBytes(payload, "text").String() != ""
	case "response.content_part.added", "response.content_part.done":
		return gjson.GetBytes(payload, "part.type").String() == "output_text" && gjson.GetBytes(payload, "part.text").String() != ""
	case "response.output_item.added", "response.output_item.done":
		for _, part := range gjson.GetBytes(payload, "item.content").Array() {
			if part.Get("type").String() == "output_text" && part.Get("text").String() != "" {
				return true
			}
		}
	case "response.completed", "response.done":
		for _, item := range gjson.GetBytes(payload, "response.output").Array() {
			for _, part := range item.Get("content").Array() {
				if part.Get("type").String() == "output_text" && part.Get("text").String() != "" {
					return true
				}
			}
		}
	}
	return false
}

// MarkDownstream records only a successful write/flush of this exact event;
// receive-ahead and heartbeats cannot claim that text was delivered locally.
func (t *openAIForwardTiming) MarkDownstream(payload []byte) {
	if t == nil || !gjson.ValidBytes(payload) {
		return
	}
	typ := gjson.GetBytes(payload, "type").String()
	if typ == "" || typ == "response" {
		if gjson.GetBytes(payload, "object").String() == "response" || typ == "response" {
			t.once("buffered_response_written")
			return
		}
	}
	e := classifyOpenAITimingEvent(payload)
	if e.semantic {
		t.MarkSemantic()
		if t.flushed.CompareAndSwap(false, true) {
			t.once("first_downstream_flush")
		}
	}
	if e.content {
		t.once("first_content_flush")
	}
	if e.text {
		t.once("first_text_flush")
	}

}

func (t *openAIForwardTiming) ConnectionReady(reused bool, pick, queue, age, idle time.Duration) {
	if t == nil {
		return
	}
	t.once("connection_ready")
	fields := []zap.Field{zap.Bool("reused", reused)}
	if pick >= 0 {
		fields = append(fields, zap.Int64("conn_pick_ms", pick.Milliseconds()))
	}
	if queue >= 0 {
		fields = append(fields, zap.Int64("queue_wait_ms", queue.Milliseconds()))
	}
	if age >= 0 {
		fields = append(fields, zap.Int64("conn_age_ms", age.Milliseconds()))
	}
	if idle >= 0 {
		fields = append(fields, zap.Int64("conn_idle_ms", idle.Milliseconds()))
	}
	t.record("got_conn", fields...)
}
func (t *openAIForwardTiming) MarkRequestWritten(size int64) {
	if t == nil {
		return
	}
	if size >= 0 {
		t.mu.Lock()
		t.semanticBytes = size
		t.mu.Unlock()
	}
	t.once("wrote_request")
}

// Called only after a successful frame write. Both sizes use the same WS JSON
// envelope before compression; they cannot establish frame/TLS bytes or receipt.
func (t *openAIForwardTiming) MarkIncrementalPayloadWritten(full, sent int64) {
	if t == nil || full < 0 || sent < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.finished && !t.payloadKnown {
		t.payloadFull, t.payloadSent, t.payloadKnown = full, sent, true
	}
}

func (t *openAIForwardTiming) MarkPrewarm(d time.Duration) {
	if t != nil {
		t.record("prewarm", zap.Int64("duration_ms", d.Milliseconds()))
	}
}
func (t *openAIForwardTiming) SetTransportReason(reason string) {
	if t != nil && timingLabel(reason) {
		t.record("transport_decision", zap.String("reason", reason))
	}
}
func timingLabel(s string) bool {
	if len(s) == 0 || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if c != '_' && c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// RecordOpenAIRequestBodyEncoding measures body bytes, not TLS/HTTP framing.
// Repository code receives only this immutable context pointer, never Gin.
func RecordOpenAIRequestBodyEncoding(ctx context.Context, semantic, wire int64, encoding string, elapsed time.Duration) {
	if ctx == nil {
		return
	}
	t, _ := ctx.Value(openAITransportTimingKey{}).(*openAIForwardTiming)
	if t == nil {
		return
	}
	switch encoding {
	case "identity", "zstd", "gzip", "deflate":
	default:
		encoding = "other"
	}
	t.mu.Lock()
	t.semanticBytes = semantic
	t.wireBytes = wire
	t.wireKnown = wire >= 0
	t.encoding = encoding
	t.mu.Unlock()
	t.record("request_encoding", zap.String("encoding", encoding), zap.Int64("semantic_request_bytes", semantic), zap.Int64("body_wire_bytes", wire), zap.Int64("encoding_ms", elapsed.Milliseconds()))
}

func cloneOpenAITimingStages(stages map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(stages))
	for k, v := range stages {
		out[k] = v
	}
	return out
}
func (t *openAIForwardTiming) Finish(err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.finished = true
	outcome := "incomplete"
	switch t.terminal {
	case "response.completed", "response.done", "message_stop", "[DONE]":
		outcome = "completed"
	case "response.failed", "response.incomplete", "error", "http_error":
		outcome = "upstream_failed"
	}
	if t.downstreamFailed {
		outcome = "downstream_failed"
	}
	if err != nil {
		outcome = "error"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			outcome = "canceled"
		}
	}
	fields := []zap.Field{zap.String("transport", t.transport), zap.String("outcome", outcome), zap.Any("stages_ms", cloneOpenAITimingStages(t.stages)), zap.Int64("semantic_request_bytes", t.semanticBytes), zap.Bool("body_wire_bytes_known", t.wireKnown), zap.String("model", t.model), zap.String("reasoning_effort", t.effort), zap.String("requested_service_tier", t.tier), zap.String("actual_service_tier", t.actualTier), zap.String("actual_model", t.actualModel), zap.String("request_shape", t.requestShape), zap.String("client_class", t.clientClass), zap.String("client_transport", t.clientTransport)}
	if t.usage != nil {
		fields = append(fields, zap.Any("upstream_usage", t.usage))
	}
	if t.wireKnown {
		fields = append(fields, zap.Int64("body_wire_bytes", t.wireBytes), zap.String("encoding", t.encoding))
	}
	if t.payloadKnown {
		fields = append(fields, zap.Any("incremental_payload", map[string]int64{"full_json_bytes": t.payloadFull, "sent_json_bytes": t.payloadSent}))
	}
	observation, obsCtx, connID := t.observation, t.observationContext, t.observationConnID
	t.mu.Unlock()
	if observation != nil {
		report := observation.Finish(obsCtx, err == nil && outcome == "completed", connID)
		fields = append(fields, zap.Any("incremental_observation", report))
	}
	t.record("attempt_summary", fields...)
}

// Metadata is captured after the final outbound transform. Unknown fields stay
// absent; upstream token counters are not inferred from billable totals.
func (t *openAIForwardTiming) SetRequestMetadata(body []byte) {
	if t == nil {
		return
	}
	view := parseRawJSONView(body)
	model, effort, tier := openAITimingModel(view.Get("model").String()), view.Get("reasoning.effort").String(), openAITimingTier(view.Get("service_tier").String())
	switch effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		effort = ""
	}
	shape := "full"
	if view.Get("previous_response_id").String() != "" {
		shape = "explicit_continuation"
	}
	if view.Get("generate").Exists() && !view.Get("generate").Bool() {
		shape = "prewarm"
	}
	t.mu.Lock()
	t.model, t.effort, t.tier, t.requestShape = model, strings.Clone(effort), tier, shape
	t.mu.Unlock()
}
func openAITimingModel(s string) string {
	if len(s) > 120 {
		return ""
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '/' || r == ':') {
			return ""
		}
	}
	return strings.Clone(s)
}
func openAITimingTier(s string) string {
	switch s {
	case "auto", "default", "flex", "priority", "scale":
		return strings.Clone(s)
	}
	return ""
}
func openAITimingUsage(usage gjson.Result) map[string]int64 {
	var out map[string]int64
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens", "input_tokens_details.cached_tokens", "output_tokens_details.reasoning_tokens"} {
		value := usage.Get(key)
		if value.Type != gjson.Number || value.Int() < 0 || value.Float() != float64(value.Int()) {
			continue
		}
		if out == nil {
			out = make(map[string]int64)
		}
		out[key] = value.Int()
	}
	return out
}

// Buffered bodies expose only their completion time. Do not label it as first
// stream text: bytes could have arrived earlier inside the buffered read.
func (t *openAIForwardTiming) ObserveBufferedResponse(body []byte) {
	if t == nil || !gjson.ValidBytes(body) {
		return
	}
	t.mu.Lock()
	observation := t.observation
	t.mu.Unlock()
	if observation != nil {
		observation.ObserveResponse(body)
	}
	t.observeBufferedTiming(body)
}

// Buffered SSE lacks the per-item completeness timeline needed by P2.
func (t *openAIForwardTiming) observeBufferedTiming(body []byte) {
	if t == nil || !gjson.ValidBytes(body) {
		return
	}
	view := parseRawJSONView(body)
	status := view.Get("status").String()
	t.mu.Lock()
	t.actualModel = openAITimingModel(view.Get("model").String())
	t.actualTier = openAITimingTier(view.Get("service_tier").String())
	t.usage = openAITimingUsage(view.Get("usage"))
	switch status {
	case "completed":
		t.terminal = "response.completed"
	case "failed", "incomplete":
		t.terminal = "response." + status
	}
	t.mu.Unlock()
	t.once("buffered_response_complete")
}

const openAIIncrementalOriginalScopeKey = "openai_incremental_original_scope"

// Captured before account metadata normalization. Only authenticated execution
// scope is reused; prompt cache keys and generated session affinity are excluded.
func (s *OpenAIGatewayService) attachOpenAIIncrementalObservation(ctx context.Context, c *gin.Context, account *Account, body []byte, endpoint string, headers http.Header, connID string, timing *openAIForwardTiming) *openAIIncrementalObservation {
	if timing == nil || c == nil || c.Request == nil || account == nil || !account.IsOpenAI() || GetOpenAIClientTransport(c) == OpenAIClientTransportWS {
		return nil
	}
	scope, _ := c.Get(openAIIncrementalOriginalScopeKey)
	executionScope, _ := scope.(string)
	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	credential := headers.Get("Authorization")
	if account.IsSIWCEnabled() {
		// Reuse the atomic grant snapshot carried by this actual request. The send
		// boundary validates its bearer/account binding; observation adds no read.
		owner, ok := ctx.Value(siwcRequestGrantKey{}).(siwcRequestGrant)
		credential = ""
		if ok && owner.accountID == account.ID && owner.identity != "" {
			identity, _ := json.Marshal([]string{headers.Get("Authorization"), owner.identity})
			credential = string(identity)
		}
	}
	settings, _ := json.Marshal([]any{timing.transport, account.ProxyID, proxyURL, account.IsTLSFingerprintEnabled(), account.GetTLSFingerprintProfileID()})
	window := c.GetHeader(openAIWSWindowIDHeader)
	if window == "" && account.UsesOpenAICodexProtocol() {
		// The final normalized body owns its window when HTTP callers supplied
		// metadata instead of a header. This never manufactures execution scope.
		window = gjson.GetBytes(body, "client_metadata.x-codex-window-id").String()
	}
	observation := s.beginOpenAIIncrementalObservation(c, account, body, openAIIncrementalObservationOptions{ExecutionScope: executionScope, WindowID: window, Endpoint: endpoint, CredentialGeneration: credential, SettingsIdentity: string(settings), ConnID: connID, ConnectionReady: connID != "", Compaction: isOpenAIResponsesCompactPath(c)})
	timing.mu.Lock()
	timing.observation = observation
	timing.observationContext = c.Request.Context()
	timing.observationConnID = connID
	timing.mu.Unlock()
	return observation
}
func (t *openAIForwardTiming) observeIncrementalEvent(payload []byte) {
	if t == nil {
		return
	}
	t.mu.Lock()
	observation := t.observation
	t.mu.Unlock()
	if observation != nil {
		observation.ObserveEvent(payload)
	}
}

func (t *openAIForwardTiming) discardSSEDownstream() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.pendingDown = openAITimingEvent{}
	t.readySemantic, t.readyContent, t.readyText = false, false, false
	t.mu.Unlock()
}

func (t *openAIForwardTiming) MarkDownstreamFailure() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.downstreamFailed = true
	t.mu.Unlock()
}

// Plugin RPC owns its final upstream payload and transport. Exclude it from
// native HTTP/WS evidence and invalidate the consumed anchor without guessing.
func (t *openAIForwardTiming) markOpaqueTransport() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.transport = "plugin"
	t.wireKnown = false
	t.semanticBytes = -1
	observation, ctx, conn := t.observation, t.observationContext, t.observationConnID
	t.mu.Unlock()
	if observation != nil {
		observation.Finish(ctx, false, conn)
		observation.mu.Lock()
		observation.report = newOpenAIIncrementalReport("opaque_transport")
		observation.mu.Unlock()
	}
	t.SetTransportReason("plugin_outbound_unknown")
}

// Native Claude streams reuse the bounded timing schema. This scope starts at
// response headers; it does not claim to measure connection setup or upload.
func beginAnthropicStreamTiming(c *gin.Context, account *Account, model string) *openAIForwardTiming {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	t := &openAIForwardTiming{started: time.Now(), model: openAITimingModel(model), clientClass: openAITimingClientClass(ctx), log: openAITimingLogger(ctx, account).With(zap.String("protocol", "anthropic"), zap.String("timing_scope", "response_stream")), transport: "http", clientTransport: "http", stages: make(map[string]int64), wireBytes: -1}
	t.once("response_headers")
	return t
}
