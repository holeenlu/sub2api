package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
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
const openAIWSUsageDrainTimeout = 1200 * time.Millisecond
const openAIWSCloseHandshakeTimeout = 250 * time.Millisecond

var errOpenAIWSUsageDrainExpired = errors.New("websocket client usage drain expired")
var ErrOpenAIWSLocalClientPolicy = errors.New("websocket client policy rejection")
var errOpenAIWSTurnCancelled = errors.New("websocket turn cancelled by client")

type openAIWSControl struct {
	payload []byte
	turn    uint64
}

// OpenAIWSDownstream owns the physical socket for its complete lifetime, rather
// than starting a new reader each time an account/turn changes.
type OpenAIWSDownstream struct {
	budget          *OpenAITurnBudget // ingress owner only
	stagingDeadline time.Time
	responseIDs     map[string]bool
	streamID        *string // current implementation admits one lane per socket
	conn            OpenAIWSDownstreamConn
	frames          chan openAIWSClientReadResult
	controls        chan openAIWSControl
	done            chan struct{}
	err             error // published by done
	queued          atomic.Int64
	maxQueued       int64
	writeGate       chan struct{}
	writeCtx        context.Context
	stopWrites      context.CancelFunc
	closeOnce       sync.Once
	turn            atomic.Uint64
	active          atomic.Bool
	controlsEnabled atomic.Bool
	modeSelected    atomic.Bool
	lastTerminalID  atomic.Pointer[string]
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

func (s *OpenAIWSDownstream) readLoop() {
	defer close(s.done)
	for {
		kind, payload, err := s.conn.Read(context.Background())
		if err != nil {
			s.err = err
			return
		}
		if s.controlsEnabled.Load() && gjson.GetBytes(payload, "type").String() == "response.cancel" {
			turn := s.turn.Load()
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
			case s.controls <- openAIWSControl{payload: payload, turn: turn}:
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
		case s.frames <- openAIWSClientReadResult{messageType: kind, payload: payload, turn: turn}:
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
				return coderws.MessageText, control.payload, nil
			}
			if err := s.writeControlError(ctx, control.payload, "no active response for cancellation"); err != nil {
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
	writeCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.writeCtx, cancel)
	defer func() { stop(); cancel() }()
	if isOpenAIWSTerminalEvent(gjson.GetBytes(payload, "type").String()) {
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
func (s *OpenAIWSDownstream) writeControlError(ctx context.Context, control []byte, message string) error {
	event := map[string]any{"type": "error", "error": map[string]string{"type": "invalid_request_error", "code": "response_not_active", "message": message}}
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

type openAIWSClientReadResult struct {
	messageType coderws.MessageType
	payload     []byte
	err         error
	turn        uint64
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
	go func() {
		messageType, payload, err := conn.Read(context.Background())
		readDone <- openAIWSClientReadResult{messageType: messageType, payload: payload, err: err}
	}()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	startTimeout := func() {
		if timeout <= 0 || (timeoutActive != nil && !timeoutActive()) {
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
		case <-timeoutStart:
			startTimeout()
		case <-timeoutCh:
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
	if !OpenAITurnRetryAllowed(WithOpenAITurnBudget(context.Background(), s.budget), accountID, response) {
		return false, OpenAIRetryStopTurnBudget
	}
	return true, ""
}
