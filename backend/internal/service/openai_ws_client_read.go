package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// OpenAIWSDownstreamConn lets every ingress path use the connection's single
// reader and serialized writer, including handler errors and preemption.
type OpenAIWSDownstreamConn interface {
	Read(context.Context) (coderws.MessageType, []byte, error)
	Write(context.Context, coderws.MessageType, []byte) error
	Close(coderws.StatusCode, string) error
	CloseNow() error
}

const openAIWSClientQueueLimit = 8

// validateOpenAIWSInterrupt checks the client envelope without changing the
// control or acknowledging it. Only the upstream can accept an interrupt.
func validateOpenAIWSInterrupt(payload []byte) (string, error) {
	if len(payload) > 16<<10 || !gjson.ValidBytes(payload) || !gjson.ParseBytes(payload).IsObject() {
		return "", errors.New("invalid response.interrupt frame")
	}
	seen := map[string]bool{}
	duplicate := false
	gjson.ParseBytes(payload).ForEach(func(key, value gjson.Result) bool {
		switch key.String() {
		case "type", "response_id", "mode":
			if seen[key.String()] {
				duplicate = true
			}
			seen[key.String()] = true
		}
		return true
	})
	if duplicate {
		return "", errors.New("duplicate response.interrupt field")
	}
	if _, _, err := openAIWSRequestEnvelope(payload); err != nil {
		return "", err
	}
	for _, key := range []string{"response_id", "mode", "event_id", "stream_id"} {
		value := gjson.GetBytes(payload, key)
		if value.Exists() && (value.Type != gjson.String || len(value.String()) > 512) {
			return "", errors.New("invalid response.interrupt " + key)
		}
	}
	target := gjson.GetBytes(payload, "response_id").String()
	if strings.TrimSpace(target) == "" {
		return "", errors.New("response.interrupt requires response_id")
	}
	return target, nil
}

const openAIWSUsageDrainTimeout = 1200 * time.Millisecond
const openAIWSCloseHandshakeTimeout = 250 * time.Millisecond

var errOpenAIWSUsageDrainExpired = errors.New("websocket client usage drain expired")
var ErrOpenAIWSLocalClientPolicy = errors.New("websocket client policy rejection")
var errOpenAIWSTurnCancelled = errors.New("websocket turn cancelled by client")

type openAIWSControl struct {
	payload   []byte
	turn      uint64
	interrupt bool
}

// OpenAIWSDownstream owns one response stream for its complete lifetime,
// using either a direct socket or a multiplexed lane. Account/turn changes
// never start a second reader for that owner.
type OpenAIWSDownstream struct {
	budget          atomic.Pointer[OpenAITurnBudget] // replaced only for a newly accepted client turn
	stagingDeadline time.Time
	responseIDs     map[string]bool
	// publishedResponseID is the response.id this client already received for
	// the current turn; publishedTurnEvents are the metadata-class event types
	// already delivered. A retried attempt after an identity-only publication
	// presents itself under this ID and never repeats those events.
	publishedResponseID string
	publishedTurnEvents map[string]bool
	// responseIDAliases maps a client-visible response.id to the upstream
	// response.id that actually produced it (connection lifetime).
	responseIDAliases map[string]string
	streamID          atomic.Pointer[string] // this owner is scoped to one multiplexed stream
	conn              OpenAIWSDownstreamConn
	frames            chan openAIWSClientReadResult
	controls          chan openAIWSControl
	done              chan struct{}
	err               error // published by done
	queued            atomic.Int64
	maxQueued         int64
	writeGate         chan struct{}
	writeCtx          context.Context
	stopWrites        context.CancelFunc
	closeOnce         sync.Once
	turn              atomic.Uint64
	turnReceivedAt    atomic.Pointer[time.Time]
	active            atomic.Bool
	controlsEnabled   atomic.Bool
	modeSelected      atomic.Bool
	lastTerminalID    atomic.Pointer[string]
	terminalTurn      atomic.Uint64
	requestEventID    string
	currentResponseID atomic.Pointer[string]
	nativeHistory     openAIWSRecoveryHistory
	bridgeHistory     openAIWSRecoveryHistory
}

// Successful history belongs to the socket owner, not an individual upstream
// invocation. The ingress coordinator is its single reader/writer.
type openAIWSRecoveryHistory struct {
	accountID                        int64
	responseID, windowID             string
	payload                          []byte
	strict                           *openAIWSIngressPreviousTurnStrictState
	input, portableInput             []json.RawMessage
	exists, portableExists, complete bool
}

// FinishTurnAndReadNext never creates a second physical socket reader. A failed
// response is not added to history. Only accepting a new create resets budgets.
func (s *OpenAIWSDownstream) FinishTurnAndReadNext(ctx context.Context, cfg *config.Config, code, message string) ([]byte, error) {
	if s.terminalTurn.Load() != s.turn.Load() {
		event := map[string]any{"type": "error", "error": map[string]string{"type": "server_error", "code": code, "message": message}}
		if id := s.currentResponseID.Load(); id != nil && *id != "" {
			event = map[string]any{"type": "response.failed", "response": map[string]any{"id": *id, "object": "response", "status": "failed", "error": event["error"]}}
		} else if s.requestEventID != "" {
			event["event_id"] = s.requestEventID
		}
		payload, _ := json.Marshal(event)
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.Write(writeCtx, coderws.MessageText, payload)
		cancel()
		if err != nil {
			return nil, err
		}
		s.terminalTurn.Store(s.turn.Load())
	}
	s.active.Store(false)
	s.completeMultiplexTurn()
	for {
		kind, payload, err := ReadOpenAIWSClientMessage(ctx, s, openAIWSPassthroughIdleTimeoutDefault, coderws.StatusNormalClosure, "websocket idle")
		if err != nil {
			return nil, err
		}
		if kind != coderws.MessageText || !gjson.ValidBytes(payload) {
			return nil, NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid websocket request", ErrOpenAIWSLocalClientPolicy)
		}
		if typ := gjson.GetBytes(payload, "type").String(); typ != "response.create" && typ != "" {
			if err := s.writeControlError(ctx, payload, "response_not_active", "expected response.create after failed turn"); err != nil {
				return nil, err
			}
			continue
		}
		if err := s.bindRequestEnvelope(payload); err != nil {
			return nil, err
		}
		s.budget.Store(NewOpenAITurnBudget(cfg))
		s.stagingDeadline, s.responseIDs = time.Time{}, nil
		return payload, nil
	}
}

func openAIWSRequestEnvelope(payload []byte) (string, string, error) {
	stream := gjson.GetBytes(payload, "stream_id")
	event := gjson.GetBytes(payload, "event_id")
	lane := stream.String()
	if stream.Exists() {
		valid := stream.Type == gjson.String && len(lane) >= 1 && len(lane) <= 256
		for _, ch := range lane {
			valid = valid && (ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-' || ch == '.')
		}
		if !valid {
			return "", "", NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "invalid stream_id: use 1-256 ASCII letters, digits, underscores, hyphens or periods", ErrOpenAIWSLocalClientPolicy)
		}
	}
	if event.Exists() && event.Type != gjson.String {
		return "", "", NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "event_id must be a string", ErrOpenAIWSLocalClientPolicy)
	}
	seenStream, seenEvent, seenType := false, false, false
	duplicate := false
	gjson.ParseBytes(payload).ForEach(func(key, _ gjson.Result) bool {
		switch key.String() {
		case "type":
			duplicate, seenType = seenType, true
		case "stream_id":
			duplicate, seenStream = seenStream, true
		case "event_id":
			duplicate, seenEvent = seenEvent, true
		}
		return !duplicate
	})
	if duplicate {
		return "", "", NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "duplicate websocket envelope field", ErrOpenAIWSLocalClientPolicy)
	}
	return lane, event.String(), nil
}

func (s *OpenAIWSDownstream) bindRequestEnvelope(payload []byte) error {
	lane, eventID, err := openAIWSRequestEnvelope(payload)
	if err != nil {
		return err
	}
	if previous := s.streamID.Load(); previous != nil {
		if *previous != lane {
			return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "stream_id changed within the assigned response stream", ErrOpenAIWSLocalClientPolicy)
		}
	} else {
		s.streamID.Store(&lane)
	}
	s.requestEventID = eventID
	s.currentResponseID.Store(nil)
	return nil
}

// Preserve explicit control-error correlation; fill only missing request
// envelope fields. Connection metadata stays connection-scoped.
func openAIWSEventWithEnvelope(payload []byte, lane, eventID string) ([]byte, error) {
	typ := gjson.GetBytes(payload, "type").String()
	if typ != "error" && !strings.HasPrefix(typ, "response.") {
		return payload, nil
	}
	var err error
	if lane != "" && !gjson.GetBytes(payload, "stream_id").Exists() {
		payload, err = sjson.SetBytes(payload, "stream_id", lane)
		if err != nil {
			return nil, err
		}
	}
	if typ == "error" && eventID != "" && !gjson.GetBytes(payload, "event_id").Exists() {
		payload, err = sjson.SetBytes(payload, "event_id", eventID)
	}
	return payload, err
}

func NewOpenAIWSDownstream(conn OpenAIWSDownstreamConn, maxQueuedBytes int64) *OpenAIWSDownstream {
	if existing, ok := conn.(*OpenAIWSDownstream); ok {
		return existing
	}
	if maxQueuedBytes <= 0 {
		maxQueuedBytes = 32 << 20
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &OpenAIWSDownstream{conn: conn, frames: make(chan openAIWSClientReadResult, openAIWSClientQueueLimit), controls: make(chan openAIWSControl, openAIWSClientQueueLimit), done: make(chan struct{}), maxQueued: maxQueuedBytes, writeGate: make(chan struct{}, 1), writeCtx: ctx, stopWrites: cancel}
	s.controlsEnabled.Store(true)
	go s.readLoop()
	return s
}

// TurnBudgetContext is called before account selection and reused on retries.
// Only a newly accepted client turn replaces s.budget in the ingress owner.
func (s *OpenAIWSDownstream) TurnBudgetContext(ctx context.Context, cfg *config.Config, payload []byte) context.Context {
	budget := s.budget.Load()
	if budget == nil {
		s.budget.CompareAndSwap(nil, NewOpenAITurnBudget(cfg))
		budget = s.budget.Load()
	}
	return EnsureOpenAITurnBudget(WithOpenAITurnBudget(ctx, budget), cfg, gjson.GetBytes(payload, "reasoning.effort").String())
}

// TurnReceivedAt is the physical receipt time of the last dequeued create.
// Frames waiting behind an active turn retain their original receipt time;
// control frames and read-ahead creates cannot change the accepted turn's clock.
func (s *OpenAIWSDownstream) TurnReceivedAt() time.Time {
	if s != nil {
		if received := s.turnReceivedAt.Load(); received != nil {
			return *received
		}
	}
	return time.Time{}
}

func (s *OpenAIWSDownstream) readLoop() {
	defer close(s.done)
	for {
		kind, payload, err := s.conn.Read(context.Background())
		receivedAt := time.Now()
		if lane, ok := s.conn.(*openAIWSStreamConn); ok {
			receivedAt = lane.lastReadAt
		}
		if err != nil {
			s.err = err
			return
		}
		interrupt := gjson.GetBytes(payload, "type").String() == "response.interrupt"
		controlTurn := s.turn.Load()
		if interrupt {
			target, err := validateOpenAIWSInterrupt(payload)
			if err == nil {
				current, last := s.currentResponseID.Load(), s.lastTerminalID.Load()
				lane := gjson.GetBytes(payload, "stream_id").String()
				activeLane := s.streamID.Load()
				if !s.active.Load() || current == nil || *current != target || (last != nil && *last == target) || (lane != "" && (activeLane == nil || *activeLane != lane)) {
					err = errors.New("response does not belong to the active turn")
				}
			}
			if err != nil {
				if writeErr := s.writeControlError(s.writeCtx, payload, "response_not_active", err.Error()); writeErr != nil {
					s.err = writeErr
					return
				}
				continue
			}
		}
		if interrupt || s.controlsEnabled.Load() && gjson.GetBytes(payload, "type").String() == "response.cancel" {
			turn := controlTurn
			if !s.active.Load() {
				turn = 0
			}
			// Control frames cannot retain arbitrarily large request bodies.
			if len(payload) > 16<<10 {
				s.err = NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "websocket control frame too large", ErrOpenAIWSLocalClientPolicy)
				s.closeTransport(coderws.StatusPolicyViolation, "websocket control frame too large", true)
				return
			}
			select {
			case s.controls <- openAIWSControl{payload: payload, turn: turn, interrupt: interrupt}:
			default:
				s.err = NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "too many pending websocket controls", ErrOpenAIWSLocalClientPolicy)
				s.closeTransport(coderws.StatusPolicyViolation, "websocket control queue limit exceeded", true)
				return
			}
			continue
		}
		if s.queued.Add(int64(len(payload))) > s.maxQueued {
			s.err = NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "websocket request queue byte limit exceeded", ErrOpenAIWSLocalClientPolicy)
			s.closeTransport(coderws.StatusPolicyViolation, "websocket request queue byte limit exceeded", true)
			return
		}
		var turn uint64
		if typ := gjson.GetBytes(payload, "type").String(); (typ == "response.create" || typ == "") && s.active.CompareAndSwap(false, true) {
			turn = s.turn.Add(1)
		}
		select {
		case s.frames <- openAIWSClientReadResult{messageType: kind, payload: payload, turn: turn, receivedAt: receivedAt}:
		case <-s.writeCtx.Done():
			s.err = io.ErrClosedPipe
			return
		}
	}
}

func (s *OpenAIWSDownstream) Read(ctx context.Context) (coderws.MessageType, []byte, error) {
	for {
		controls := s.controls
		// Account selection has not chosen native versus passthrough yet.
		// Preserve early cancel frames while the handler reads the first create.
		if !s.modeSelected.Load() {
			controls = nil
		}
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-s.done:
			return 0, nil, s.err
		case control := <-controls:
			// A control may have arrived during account selection, before the
			// ingress mode was known. Passthrough owns it once selected.
			if !s.controlsEnabled.Load() {
				if control.interrupt && (control.turn != s.turn.Load() || !s.active.Load()) {
					if err := s.writeControlError(ctx, control.payload, "response_not_active", "response does not belong to the active turn"); err != nil {
						return 0, nil, err
					}
					continue
				}
				return coderws.MessageText, control.payload, nil
			}
			if err := s.writeControlError(ctx, control.payload, "response_not_active", "no active response for control"); err != nil {
				return 0, nil, err
			}
		case frame := <-s.frames:
			s.queued.Add(-int64(len(frame.payload)))
			// A buffered create is not authorization to start new work after
			// the physical reader has already observed the client's disconnect.
			if !s.alive() {
				select {
				case <-s.done:
					return 0, nil, s.err
				default:
					return 0, nil, io.ErrClosedPipe
				}
			}
			if kind := gjson.GetBytes(frame.payload, "type").String(); kind == "response.create" || kind == "" {
				if lane, ok := s.conn.(*openAIWSStreamConn); ok {
					// The frame is already read. Waiting for a multiplex permit is
					// admission time, not a missing-message or inter-turn read timeout.
					if received, ok := ctx.Value(openAIWSMessageReceivedKey{}).(chan struct{}); ok {
						select {
						case received <- struct{}{}:
						default:
						}
					}
					if err := lane.acceptCreate(ctx, frame.payload); err != nil {
						return 0, nil, err
					}
				}
				receivedAt := frame.receivedAt
				if receivedAt.IsZero() {
					receivedAt = time.Now()
				}
				s.turnReceivedAt.Store(&receivedAt)
				if frame.turn == 0 {
					s.turn.Add(1)
				}
				s.active.Store(true)
			}
			return frame.messageType, frame.payload, nil
		}
	}
}

func (s *OpenAIWSDownstream) Write(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	select {
	case s.writeGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.writeCtx.Done():
		return io.ErrClosedPipe
	}
	defer func() { <-s.writeGate }()
	if s.writeCtx.Err() != nil {
		return io.ErrClosedPipe
	}
	lane := ""
	if value := s.streamID.Load(); value != nil {
		lane = *value
	}
	var envelopeErr error
	payload, envelopeErr = openAIWSEventWithEnvelope(payload, lane, "")
	if envelopeErr != nil {
		return envelopeErr
	}
	writeCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.writeCtx, cancel)
	defer func() { stop(); cancel() }()
	if id := gjson.GetBytes(payload, "response.id").String(); id != "" {
		s.currentResponseID.Store(&id)
	}
	eventType := gjson.GetBytes(payload, "type").String()
	terminalError := eventType == "error" && gjson.GetBytes(payload, "error.code").String() != "response_not_active"
	if isOpenAIWSTerminalEvent(eventType) || terminalError {
		s.terminalTurn.Store(s.turn.Load())
		if id := gjson.GetBytes(payload, "response.id").String(); id != "" {
			s.lastTerminalID.Store(&id)
		}
		s.active.Store(false)
	}
	return s.conn.Write(writeCtx, kind, payload)
}

func (s *OpenAIWSDownstream) closeTransport(status coderws.StatusCode, reason string, graceful bool) {
	s.closeOnce.Do(func() {
		s.stopWrites()
		s.writeGate <- struct{}{}
		if graceful {
			// A peer that ignores our close frame must not hold preemption or
			// handler cleanup for the websocket library's full handshake wait.
			closed := make(chan struct{})
			timer := time.AfterFunc(openAIWSCloseHandshakeTimeout, func() { _ = s.conn.CloseNow(); close(closed) })
			_ = s.conn.Close(status, reason)
			if !timer.Stop() {
				<-closed
			}
		}
		_ = s.conn.CloseNow()
		<-s.writeGate
	})
}
func (s *OpenAIWSDownstream) close(status coderws.StatusCode, reason string, graceful bool) {
	s.closeTransport(status, reason, graceful)
	<-s.done
}
func (s *OpenAIWSDownstream) Close(status coderws.StatusCode, reason string) error {
	s.close(status, reason, true)
	return nil
}
func (s *OpenAIWSDownstream) CloseNow() error { s.close(0, "", false); return nil }
func (s *OpenAIWSDownstream) alive() bool {
	select {
	case <-s.done:
		return false
	default:
		return s.writeCtx.Err() == nil
	}
}
func (s *OpenAIWSDownstream) writeControlError(ctx context.Context, control []byte, code, message string) error {
	event := map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "code": code, "message": message}}
	if gjson.GetBytes(control, "type").String() == "response.interrupt" {
		event["type"] = "response.interrupt.failed"
		if id := gjson.GetBytes(control, "response_id"); id.Type == gjson.String {
			event["response_id"] = id.String()
		}
	}
	for _, key := range []string{"event_id", "stream_id"} {
		if v := gjson.GetBytes(control, key).String(); v != "" {
			event[key] = v
		}
	}
	b, _ := json.Marshal(event)
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.Write(writeCtx, coderws.MessageText, b)
}

func (s *OpenAIWSDownstream) turnContext(ctx context.Context) (context.Context, context.CancelFunc) {
	turnCtx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-turnCtx.Done():
			return
		case <-s.done:
		}
		timer := time.NewTimer(openAIWSUsageDrainTimeout)
		defer timer.Stop()
		select {
		case <-turnCtx.Done():
		case <-timer.C:
			cancel(errOpenAIWSUsageDrainExpired)
		}
	}()
	return turnCtx, func() { cancel(context.Canceled); <-done }
}

// ClientContext also bounds admission/failover work after the reader observes
// disconnect, while allowing the same brief usage drain as an active turn.
func (s *OpenAIWSDownstream) ClientContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return s.turnContext(ctx)
}

type openAIWSMessageReceivedKey struct{}

type openAIWSClientReadResult struct {
	messageType coderws.MessageType
	payload     []byte
	err         error
	turn        uint64
	receivedAt  time.Time
}

// ReadOpenAIWSClientMessage keeps one reader alive while control events send
// their close frame, then closes the transport and joins that reader.
func ReadOpenAIWSClientMessage(
	controlCtx context.Context,
	conn OpenAIWSDownstreamConn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
) (coderws.MessageType, []byte, error) {
	return readOpenAIWSClientMessageWithTimeoutStart(
		controlCtx,
		conn,
		timeout,
		timeoutStatus,
		timeoutReason,
		nil,
		nil,
	)
}

// readOpenAIWSClientMessageWithTimeoutStart supports readers whose timeout
// starts after a state transition, such as a completed passthrough turn. When
// timeoutActive is nil, a positive timeout starts immediately.
func readOpenAIWSClientMessageWithTimeoutStart(
	controlCtx context.Context,
	conn OpenAIWSDownstreamConn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
	timeoutStart <-chan struct{},
	timeoutActive func() bool,
) (coderws.MessageType, []byte, error) {
	if conn == nil {
		return 0, nil, errors.New("openai websocket client connection is nil")
	}
	if controlCtx == nil {
		controlCtx = context.Background()
	}

	readDone := make(chan openAIWSClientReadResult, 1)
	messageReceived := make(chan struct{}, 1)
	readCtx := context.WithValue(context.Background(), openAIWSMessageReceivedKey{}, messageReceived)
	go func() {
		messageType, payload, err := conn.Read(readCtx)
		readDone <- openAIWSClientReadResult{messageType: messageType, payload: payload, err: err}
	}()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	frameReceived := false
	finishReadTimeout := func() {
		frameReceived = true
		timeoutCh = nil
		messageReceived = nil
		if timer != nil {
			timer.Stop()
		}
	}
	startTimeout := func() {
		if frameReceived || timeout <= 0 || (timeoutActive != nil && !timeoutActive()) {
			return
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		}
		timeoutCh = timer.C
	}
	if timeoutActive == nil || timeoutActive() {
		startTimeout()
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	closeAndJoin := func(status coderws.StatusCode, reason string, cause error) (coderws.MessageType, []byte, error) {
		_ = conn.Close(status, reason)
		_ = conn.CloseNow()
		<-readDone
		return 0, nil, NewOpenAIWSClientCloseError(status, reason, cause)
	}

	for {
		select {
		case result := <-readDone:
			return result.messageType, result.payload, result.err
		case <-messageReceived:
			finishReadTimeout()
		case <-timeoutStart:
			startTimeout()
		case <-timeoutCh:
			// Prefer an already-received frame when its notification and the
			// timer become ready together; cancellation remains active below.
			select {
			case <-messageReceived:
				finishReadTimeout()
				continue
			default:
			}
			return closeAndJoin(timeoutStatus, timeoutReason, context.DeadlineExceeded)
		case <-controlCtx.Done():
			cause := context.Cause(controlCtx)
			if errors.Is(cause, ErrOpenAIWSIngressLeaseLost) {
				return closeAndJoin(
					coderws.StatusTryAgainLater,
					"websocket ingress capacity lease lost; please reconnect",
					cause,
				)
			}
			return closeAndJoin(coderws.StatusGoingAway, "websocket request canceled", cause)
		}
	}
}

func (s *OpenAIWSDownstream) ClientClosed() bool { return !s.alive() }

func (s *OpenAIWSDownstream) CanRetry(accountID int64, response []byte) (bool, OpenAIRetryStopReason) {
	if !s.alive() || len(s.controls) != 0 {
		// Client cancellation is not retry-budget exhaustion.
		return false, ""
	}
	if IsUpstreamContentRefusal(response) {
		return false, OpenAIRetryStopContentRefusal
	}
	if TurnOutputCommitted(WithOpenAITurnBudget(context.Background(), s.budget.Load())) {
		return false, OpenAIRetryStopDownstreamCommitted
	}
	if !OpenAITurnRetryAllowed(WithOpenAITurnBudget(context.Background(), s.budget.Load()), accountID, response) {
		return false, OpenAIRetryStopTurnBudget
	}
	return true, ""
}
