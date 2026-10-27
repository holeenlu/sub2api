package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const (
	openAIWSMaxActiveTurns   = 16
	openAIWSMaxNamedStreams  = 32
	openAIWSMaxPendingTurns  = 64
	openAIWSStreamQueueLimit = 16
	openAIWSMaxForkResponses = 256
)

type openAIWSStreamContextKey struct{}

// OpenAIWSFirstMessageError identifies the physical socket's first read. Lane
// failures and later connection cancellation must not be logged as uploads.
type OpenAIWSFirstMessageError struct {
	Err     error
	Elapsed time.Duration
}

func (e *OpenAIWSFirstMessageError) Error() string {
	return "client first websocket message read failed"
}
func (e *OpenAIWSFirstMessageError) Unwrap() error { return e.Err }

type openAIWSStreamIdentity struct{ connection, stream string }
type openAIWSMultiplexer struct {
	connectionID string
	conn         OpenAIWSDownstreamConn
	ctx          context.Context
	mu           sync.Mutex
	lanes        map[string]*openAIWSStreamConn
	pending      int
	queuedBytes  int64 // includes accepted frames until their turn settles
	maxBytes     int64
	active       chan struct{}
	writeGate    chan struct{}
	histories    map[string]*openAIWSForkHistory
	order        []string
	historyBytes int64
	workers      sync.WaitGroup
}
type openAIWSForkHistory struct {
	stream                    string
	accountID, keyID, groupID int64
	credentials               [32]byte
	input                     []json.RawMessage
	size                      int64
	ready                     chan struct{}
	committed                 bool
	publishing                bool
	resolved                  bool
}
type openAIWSStreamConn struct {
	responseID string
	mux        *openAIWSMultiplexer
	id         string
	incoming   chan openAIWSClientReadResult
	done       chan struct{}
	closeOnce  sync.Once
	// Protected by mux.mu. Stream reads and writes run concurrently.
	creates                   []openAIWSClientReadResult
	dispatched                bool
	pending                   int
	reservedBytes             int64
	currentBytes              int64
	running                   bool
	turnDone                  chan struct{}
	request                   []byte
	input                     []json.RawMessage
	complete                  bool
	accountID, keyID, groupID int64
	credentials               [32]byte
	collector                 openAIWSToolCallReplayCollector
	collectorBytes            int64
	terminal                  bool
	lastReadAt                time.Time // written/read only by the downstream read pump
}

// RunOpenAIWSMultiplexer owns the sole physical reader. Every lane reuses the
// existing ingress coordinator, admission, retry, cancellation and settlement.
func RunOpenAIWSMultiplexer(ctx context.Context, conn OpenAIWSDownstreamConn, cfg *config.Config, serve func(context.Context, *OpenAIWSDownstream)) error {
	m := &openAIWSMultiplexer{connectionID: uuid.NewString(), conn: conn, ctx: ctx, lanes: map[string]*openAIWSStreamConn{}, maxBytes: ResolveOpenAIWSClientReadLimitBytes(cfg), active: make(chan struct{}, openAIWSMaxActiveTurns), writeGate: make(chan struct{}, 1), histories: map[string]*openAIWSForkHistory{}}
	if m.maxBytes <= 0 {
		m.maxBytes = 32 << 20
	}
	defer func() {
		_ = conn.CloseNow()
		m.mu.Lock()
		lanes := make([]*openAIWSStreamConn, 0, len(m.lanes))
		for _, l := range m.lanes {
			lanes = append(lanes, l)
		}
		m.mu.Unlock()
		for _, l := range lanes {
			_ = l.CloseNow()
		}
		m.workers.Wait()
	}()
	first := true
	for {
		var kind coderws.MessageType
		var payload []byte
		var err error
		if first {
			started := time.Now()
			kind, payload, err = ReadOpenAIWSClientMessage(ctx, conn, ResolveOpenAIWSClientFirstMessageTimeout(cfg), coderws.StatusPolicyViolation, "missing first response.create message")
			if err != nil {
				return &OpenAIWSFirstMessageError{Err: err, Elapsed: time.Since(started)}
			}
		} else {
			kind, payload, err = conn.Read(ctx)
		}
		if err != nil {
			return err
		}
		first = false
		received := time.Now()
		if !gjson.ValidBytes(payload) || !gjson.ParseBytes(payload).IsObject() {
			_ = m.closeConnection(coderws.StatusPolicyViolation, "invalid JSON payload")
			return errors.New("invalid websocket JSON")
		}
		lane, event, err := openAIWSRequestEnvelope(payload)
		if err != nil {
			_ = m.closeConnection(coderws.StatusPolicyViolation, "invalid websocket envelope")
			return err
		}
		typ := gjson.GetBytes(payload, "type").String()
		if typ == "response.append" || typ == "response.steer" {
			if err := m.writeError(ctx, lane, event, "unsupported_feature", typ+" is not implemented; use response.create", 400); err != nil {
				return err
			}
			continue
		}
		create := typ == "response.create" || typ == ""
		m.mu.Lock()
		l := m.lanes[lane]
		if l != nil {
			select {
			case <-l.done:
				l = nil
			default:
			}
		}
		if l == nil && !create {
			m.mu.Unlock()
			if err := m.writeError(ctx, lane, event, "response_not_active", "no active response for this stream", 400); err != nil {
				return err
			}
			continue
		}
		if l != nil && (typ == "response.cancel" || typ == "response.interrupt") {
			target := gjson.GetBytes(payload, "response_id").String()
			if !l.running || (target != "" && target != l.responseID) {
				m.mu.Unlock()
				if err := m.writeError(ctx, lane, event, "response_not_active", "response does not belong to the active stream turn", 400); err != nil {
					return err
				}
				continue
			}
		}
		if l == nil {
			_, known := m.lanes[lane]
			named := len(m.lanes)
			if _, ok := m.lanes[""]; ok {
				named--
			}
			if lane != "" && !known && named >= openAIWSMaxNamedStreams {
				m.mu.Unlock()
				if err := m.writeError(ctx, lane, event, "stream_limit_exceeded", "at most 32 named streams are allowed", 429); err != nil {
					return err
				}
				continue
			}
			l = &openAIWSStreamConn{mux: m, id: lane, incoming: make(chan openAIWSClientReadResult, openAIWSStreamQueueLimit+openAIWSClientQueueLimit), done: make(chan struct{})}
			m.lanes[lane] = l
			m.workers.Add(1)
			go func(l *openAIWSStreamConn) {
				defer m.workers.Done()
				defer l.completeTurn()
				defer l.CloseNow()
				ds := NewOpenAIWSDownstream(l, m.maxBytes)
				defer ds.CloseNow()
				laneCtx := context.WithValue(ctx, openAIWSStreamContextKey{}, openAIWSStreamIdentity{connection: m.connectionID, stream: l.id})
				serve(laneCtx, ds)
			}(l)
		}
		if create && (l.pending >= openAIWSStreamQueueLimit || m.pending >= openAIWSMaxPendingTurns || m.queuedBytes+int64(len(payload)) > m.maxBytes) {
			m.mu.Unlock()
			if err := m.writeError(ctx, lane, event, "stream_queue_full", "websocket request queue is full", 429); err != nil {
				return err
			}
			continue
		}
		if !create && len(payload) > 16<<10 {
			m.mu.Unlock()
			if err := m.writeError(ctx, lane, event, "invalid_request_error", "control frame is too large", 400); err != nil {
				return err
			}
			continue
		}
		if create {
			l.pending++
			m.pending++
			l.reservedBytes += int64(len(payload))
			m.queuedBytes += int64(len(payload))
		}
		frame := openAIWSClientReadResult{messageType: kind, payload: payload, receivedAt: received}
		if create {
			l.creates = append(l.creates, frame)
			l.dispatchNextLocked()
			m.mu.Unlock()
			continue
		}
		select {
		case l.incoming <- frame:
			m.mu.Unlock()
		default:
			m.mu.Unlock()
			if err := m.writeError(ctx, lane, event, "stream_queue_full", "websocket control queue is full", 429); err != nil {
				return err
			}
		}
	}
}
func (m *openAIWSMultiplexer) write(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	select {
	case m.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-m.writeGate }()
	return m.conn.Write(ctx, kind, payload)
}
func (m *openAIWSMultiplexer) writeError(ctx context.Context, lane, event, code, message string, status int) error {
	payload, _ := json.Marshal(map[string]any{"type": "error", "status": status, "error": map[string]string{"type": "invalid_request_error", "code": code, "message": message}})
	payload, _ = openAIWSEventWithEnvelope(payload, lane, event)
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return m.write(writeCtx, coderws.MessageText, payload)
}
func (l *openAIWSStreamConn) dispatchNextLocked() {
	select {
	case <-l.done:
		return
	default:
	}
	if l.running || l.dispatched || len(l.creates) == 0 {
		return
	}
	select {
	case l.incoming <- l.creates[0]:
		l.creates[0] = openAIWSClientReadResult{}
		l.creates = l.creates[1:]
		l.dispatched = true
	default:
	}
}
func (l *openAIWSStreamConn) Read(ctx context.Context) (coderws.MessageType, []byte, error) {
	l.mux.mu.Lock()
	l.dispatchNextLocked()
	l.mux.mu.Unlock()
	select {
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	case <-l.done:
		return 0, nil, io.EOF
	case frame := <-l.incoming:
		l.lastReadAt = frame.receivedAt
		return frame.messageType, frame.payload, nil
	}
}
func (l *openAIWSStreamConn) acceptCreate(ctx context.Context, payload []byte) error {
	m := l.mux
	m.mu.Lock()
	previous := l.turnDone
	running := l.running
	m.mu.Unlock()
	if running {
		select {
		case <-previous:
		case <-l.done:
			return io.EOF
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case m.active <- struct{}{}:
	case <-l.done:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-l.done:
		<-m.active
		return io.EOF
	default:
	}
	l.pending--
	l.dispatched = false
	m.pending--
	l.currentBytes = int64(len(payload))
	l.running = true
	l.turnDone = make(chan struct{})
	l.request = payload
	l.terminal = false
	l.responseID = ""
	l.input = nil
	l.complete = false
	l.accountID = 0
	l.collector = openAIWSToolCallReplayCollector{}
	l.collectorBytes = 0
	return nil
}
func (l *openAIWSStreamConn) completeTurn() {
	m := l.mux
	m.mu.Lock()
	defer m.mu.Unlock()
	if !l.running {
		return
	}
	l.running = false
	l.reservedBytes -= l.currentBytes
	m.queuedBytes -= l.currentBytes
	l.currentBytes = 0
	l.request = nil
	l.input = nil
	l.collector = openAIWSToolCallReplayCollector{}
	l.collectorBytes = 0
	close(l.turnDone)
	<-m.active
	l.dispatchNextLocked()
}
func (l *openAIWSStreamConn) Write(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	payload, err := openAIWSEventWithEnvelope(payload, l.id, "")
	if err != nil {
		return err
	}
	m := l.mux
	typ := gjson.GetBytes(payload, "type").String()
	id := gjson.GetBytes(payload, "response.id").String()
	m.mu.Lock()
	if id != "" && l.running {
		l.responseID = id
		if _, ok := m.histories[id]; !ok {
			m.evictHistory(0)
			m.histories[id] = &openAIWSForkHistory{stream: l.id, ready: make(chan struct{})}
			m.order = append(m.order, id)
		}
		if h := m.histories[id]; h.stream == l.id && !h.resolved && (typ == "response.completed" || typ == "response.done") {
			h.publishing = true
		}
	}
	m.mu.Unlock()
	if err = m.write(ctx, kind, payload); err != nil {
		m.mu.Lock()
		if h := m.histories[id]; h != nil && h.stream == l.id && h.publishing && !h.resolved {
			h.publishing = false
			h.resolved = true
			close(h.ready)
		}
		m.mu.Unlock()
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !l.running {
		return nil
	}
	l.collectorBytes += int64(len(payload))
	// The collector is bounded independently of raw queued requests. Oversized
	// responses still stream normally but are never advertised as replayable.
	if l.collectorBytes <= m.maxBytes/openAIWSMaxActiveTurns {
		l.collector.AddEvent(typ, payload)
	} else {
		l.complete = false
		l.collector = openAIWSToolCallReplayCollector{}
	}
	if isOpenAIWSTerminalEvent(typ) || typ == "error" {
		l.terminal = true
	}
	h := m.histories[id]
	if h != nil && h.stream == l.id && h.publishing && !h.resolved && (typ == "response.completed" || typ == "response.done") {
		if l.complete && l.accountID != 0 && len(l.collector.pending) == 0 && !l.collector.unidentifiedOutput && (gjson.GetBytes(payload, "response.output").IsArray() || len(l.collector.allItems) > 0) {
			input := combineOpenAIWSReplayItems(l.input, l.collector.AllItems())
			size := int64(0)
			for _, item := range input {
				_, complete := openAIResponsesSelfContainedInputItem(gjson.Parse(openAIWSPayloadStringView(item)))
				if !complete {
					l.complete = false
					break
				}
				size += int64(len(item))
			}
			if l.complete && size <= m.maxBytes {
				m.evictHistory(size)
				h.input = input
				h.size = size
				h.accountID = l.accountID
				h.keyID = l.keyID
				h.groupID = l.groupID
				h.credentials = l.credentials
				h.committed = true
				m.historyBytes += size
			}
		}
		h.publishing = false
		h.resolved = true
		close(h.ready)
	}
	return nil
}
func (m *openAIWSMultiplexer) evictHistory(extra int64) {
	scanned := 0
	for len(m.order) > 0 && scanned < len(m.order) && (len(m.histories) >= openAIWSMaxForkResponses || m.historyBytes+extra > m.maxBytes) {
		id := m.order[0]
		m.order = m.order[1:]
		if old := m.histories[id]; old != nil {
			if old.publishing {
				m.order = append(m.order, id)
				scanned++
				continue
			}
			m.historyBytes -= old.size
			delete(m.histories, id)
		}
	}
}

func (l *openAIWSStreamConn) Close(status coderws.StatusCode, reason string) error {
	if l.id == "" {
		_ = l.mux.closeConnection(status, reason)
	} else if status != coderws.StatusNormalClosure {
		l.mux.mu.Lock()
		terminal := l.terminal
		event := gjson.GetBytes(l.request, "event_id").String()
		l.mux.mu.Unlock()
		if !terminal {
			_ = l.mux.writeError(l.mux.ctx, l.id, event, "stream_closed", reason, 400)
		}
	}
	return l.CloseNow()
}
func (l *openAIWSStreamConn) CloseNow() error {
	l.closeOnce.Do(func() {
		close(l.done)
		l.mux.mu.Lock()
		l.mux.pending -= l.pending
		l.pending = 0
		l.creates = nil
		l.mux.queuedBytes -= l.reservedBytes - l.currentBytes
		l.reservedBytes = l.currentBytes
		for {
			select {
			case <-l.incoming:
			default:
				l.mux.mu.Unlock()
				return
			}
		}
	})
	return nil
}
func (s *OpenAIWSDownstream) completeMultiplexTurn() {
	if l, ok := s.conn.(*openAIWSStreamConn); ok {
		l.completeTurn()
	}
}

// Each lane has its own Gin context as well as independent timing, trace and
// transport state. Authorization values inherited from the handshake stay intact.
func ForkOpenAIWSStreamContext(ctx context.Context) context.Context {
	if trace := schedulingTraceFromContext(ctx); trace != nil {
		ctx = WithSchedulingTrace(ctx, trace.publish)
	}
	return context.WithValue(ctx, openAIRequestTimingKey{}, &openAIRequestTimingState{})
}
func openAIWSAccountGeneration(account *Account) [32]byte {
	raw, _ := json.Marshal(struct {
		ID          int64
		Type        string
		Credentials map[string]any
	}{account.ID, account.Type, account.Credentials})
	return sha256.Sum256(raw)
}
func (s *OpenAIWSDownstream) prepareMultiplexTurn(ctx context.Context, c *gin.Context, account *Account, payload []byte) ([]byte, error) {
	l, ok := s.conn.(*openAIWSStreamConn)
	if !ok {
		return payload, nil
	}
	m := l.mux
	parent := gjson.GetBytes(payload, "previous_response_id").String()
	generation := openAIWSAccountGeneration(account)
	key, group := getAPIKeyIDFromContext(c), getOpenAIGroupIDFromContext(c)
	m.mu.Lock()
	history := m.histories[parent]
	if history != nil && history.publishing {
		ready := history.ready
		m.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-l.done:
			return nil, io.EOF
		}
		m.mu.Lock()
		history = m.histories[parent]
	}
	cross := history != nil && history.stream != l.id
	if cross && (!history.committed || history.accountID != account.ID || history.keyID != key || history.groupID != group || history.credentials != generation) {
		m.mu.Unlock()
		return nil, errors.Join(ErrOpenAIContinuationUnavailable, ErrOpenAIWSReplayUnavailable)
	}
	var previous []json.RawMessage
	known := history != nil && history.committed && history.accountID == account.ID && history.keyID == key && history.groupID == group && history.credentials == generation
	if known {
		previous = history.input
	}
	m.mu.Unlock()
	input, exists, err := buildOpenAIWSReplayInputSequence(previous, known, payload, parent != "")
	if err != nil {
		return nil, err
	}
	// A cross-stream fork uses a full immutable replay on its own connection.
	// Never borrow the source stream's live upstream socket or opaque turn state.
	if cross {
		payload, err = setOpenAIWSPayloadInputSequence(payload, input, exists)
		if err != nil {
			return nil, err
		}
		if int64(len(payload)) > m.maxBytes {
			return nil, NewOpenAIWSClientCloseError(coderws.StatusMessageTooBig, "cross-stream replay exceeds connection memory limit", ErrOpenAIWSReplayUnavailable)
		}
		payload, _, err = dropPreviousResponseIDFromRawPayload(payload)
		if err != nil {
			return nil, err
		}
	}

	m.mu.Lock()
	// Reconstructed forks count against the same outstanding-request budget,
	// so small deltas cannot multiply a full history by sixteen active streams.
	if extra := int64(len(payload)) - l.currentBytes; extra > 0 {
		if m.queuedBytes+extra > m.maxBytes {
			m.mu.Unlock()
			return nil, NewOpenAIWSClientCloseError(coderws.StatusMessageTooBig, "cross-stream replay exceeds connection memory limit", ErrOpenAIWSReplayUnavailable)
		}
		m.queuedBytes += extra
		l.reservedBytes += extra
		l.currentBytes += extra
	}
	l.request = payload
	l.accountID = account.ID
	l.keyID = key
	l.groupID = group
	l.credentials = generation
	l.input = input
	l.complete = exists && (parent == "" || known)
	m.mu.Unlock()
	return payload, nil
}

// Shared authentication failures terminate the physical connection, whereas
// account/model/stream failures use the ordinary lane-local Close method.
func (s *OpenAIWSDownstream) CloseConnection(status coderws.StatusCode, reason string) error {
	if lane, ok := s.conn.(*openAIWSStreamConn); ok {
		return lane.mux.closeConnection(status, reason)
	}
	return s.Close(status, reason)
}

func (m *openAIWSMultiplexer) closeConnection(status coderws.StatusCode, reason string) error {
	timer := time.AfterFunc(openAIWSCloseHandshakeTimeout, func() { _ = m.conn.CloseNow() })
	defer timer.Stop()
	return m.conn.Close(status, reason)
}

// Check before reconstruction removes previous_response_id. A known local
// response cannot become external-unknown after shared-index expiry.
func (s *OpenAIWSDownstream) hasMultiplexHistory(id string) bool {
	l, ok := s.conn.(*openAIWSStreamConn)
	if !ok || id == "" {
		return false
	}
	l.mux.mu.Lock()
	defer l.mux.mu.Unlock()
	return l.mux.histories[id] != nil
}
