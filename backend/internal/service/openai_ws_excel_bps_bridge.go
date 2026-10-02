package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ErrOpenAIWSModelSwitchRequiresReconnect marks a follow-up turn that the open
// connection cannot carry. It is a routing decision, not an account failure.
var ErrOpenAIWSModelSwitchRequiresReconnect = errors.New("websocket model switch requires reconnect")

// newOpenAIWSBPSModelSwitchError closes a native upstream WS connection whose
// follow-up turn switches to a BPS model. Codex switches models on an open
// socket; closing before admission makes it retry on a new connection, which
// is routed and bridged for the BPS model from its first frame.
func newOpenAIWSBPSModelSwitchError(model string) error {
	return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "model switch requires reconnect",
		fmt.Errorf("%w: Excel BPS model %q needs the HTTP/SSE bridge", ErrOpenAIWSModelSwitchRequiresReconnect, model))
}

// excelBPSWSKeepaliveInterval bounds client-visible silence on a bridged turn.
// Codex abandons a WebSocket turn after five minutes without an event, while
// BPS can hold its first byte longer than that (queueing, uploads, image
// compaction). The forwarder's own SSE comments start only after headers and
// are not WS events. A var so tests can shorten it.
var excelBPSWSKeepaliveInterval = 15 * time.Second

var excelBPSWSKeepaliveEvent = []byte(`{"type":"keepalive"}`)

// excelBPSWSWriter adapts the existing BPS HTTP forwarder to an already
// upgraded client socket. It never writes HTTP bytes to the hijacked writer.
// Only one SSE line is buffered; tool validation and usage accounting remain
// owned by forwardExcelBPS, including image uploads and bounded repair retries.
type excelBPSWSWriter struct {
	gin.ResponseWriter
	header       http.Header
	status, size int
	pending      []byte
	eventType    string
	err          error
	model        string
	write        func([]byte) error
	startDrain   func()
	context      *gin.Context

	// mu serializes client writes between the forwarder and the keepalive
	// ticker, and guards the state both of them read.
	mu           sync.Mutex
	terminal     bool
	disconnected bool
	lastWrite    time.Time
	replay       openAIWSToolCallReplayCollector
}

func (w *excelBPSWSWriter) Header() http.Header { return w.header }
func (w *excelBPSWSWriter) Status() int         { return w.status }
func (w *excelBPSWSWriter) Size() int           { return w.size }
func (w *excelBPSWSWriter) Written() bool       { return w.size >= 0 }
func (w *excelBPSWSWriter) WriteHeader(status int) {
	if !w.Written() && status > 0 {
		w.status = status
	}
}
func (w *excelBPSWSWriter) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
	}
}
func (w *excelBPSWSWriter) Flush()                            {}
func (w *excelBPSWSWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *excelBPSWSWriter) emit(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	kind := gjson.GetBytes(payload, "type").String()
	w.replay.AddEvent(kind, payload)
	w.terminal = w.terminal || isOpenAIWSTerminalEvent(kind) || kind == "error"
	if w.disconnected {
		return nil
	}
	if w.model != "" && gjson.GetBytes(payload, "response.model").Exists() {
		var err error
		payload, err = sjson.SetBytes(payload, "response.model", w.model)
		if err != nil {
			return err
		}
	}
	if err := w.write(payload); err != nil {
		if isOpenAIWSClientDisconnectError(err) {
			w.disconnected = true
			w.startDrain()
			return nil
		}
		return err
	}
	w.lastWrite = time.Now()
	markOpenAIWSClientVisibleFailure(w.context, kind, payload)
	return nil
}

// keepAlive sends a keepalive event once the client has seen nothing for a
// full interval. Write failures other than a disconnect surface on the
// forwarder's next event.
func (w *excelBPSWSWriter) keepAlive(interval time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disconnected || w.terminal || time.Since(w.lastWrite) < interval {
		return
	}
	if err := w.write(excelBPSWSKeepaliveEvent); err != nil {
		if isOpenAIWSClientDisconnectError(err) {
			w.disconnected = true
			w.startDrain()
		}
		return
	}
	w.lastWrite = time.Now()
}

// startKeepalive runs keepAlive until the returned stop, which waits for the
// ticker to exit so no keepalive can follow the turn's last event.
func (w *excelBPSWSWriter) startKeepalive(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(interval / 2)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				w.keepAlive(interval)
			}
		}
	}()
	return func() { close(done); <-exited }
}

func (w *excelBPSWSWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.WriteHeaderNow()
	if len(w.pending)+len(p) > 16<<20 {
		w.err = errors.New("websocket BPS event exceeds buffer limit")
		return 0, w.err
	}
	w.pending = append(w.pending, p...)
	w.size += len(p)
	if !strings.HasPrefix(w.header.Get("Content-Type"), "text/event-stream") {
		// A pre-stream BPS rejection is a JSON body. Delay emitting it until
		// the forwarder returns so its real HTTP status can be sent over WS.
		return len(p), nil
	}
	for {
		n := bytes.IndexByte(w.pending, '\n')
		if n < 0 {
			break
		}
		line := strings.TrimSuffix(string(w.pending[:n]), "\r")
		w.pending = w.pending[n+1:]
		if kind, ok := extractOpenAISSEEventLine(line); ok {
			w.eventType = kind
		} else if data, ok := extractOpenAISSEDataLine(line); ok {
			if data != "[DONE]" && strings.TrimSpace(data) != "" {
				w.err = w.emit([]byte(openAICompatPayloadWithEventType(data, w.eventType)))
				if w.err != nil {
					return 0, w.err
				}
			}
		} else if line == "" {
			w.eventType = ""
		}
	}
	return len(p), nil
}

// proxyOpenAIWSExcelBPSTurn runs one bridged client WS turn through the BPS
// HTTP/SSE forwarder. generate=false prewarms never reach here: the bridge
// answers them locally before choosing BPS or the native fallback.
func (s *OpenAIGatewayService) proxyOpenAIWSExcelBPSTurn(ctx context.Context, c *gin.Context, account *Account, body []byte, originalModel string, write func([]byte) error) (*OpenAIForwardResult, error) {
	start := time.Now()
	upstreamCtx, startDrain, release := openAIWSDrainContext(ctx)
	defer release()
	writer := &excelBPSWSWriter{ResponseWriter: c.Writer, header: make(http.Header), status: http.StatusOK,
		size: -1, model: originalModel, write: write, startDrain: startDrain, context: c, lastWrite: start}
	c.Writer = writer
	defer func() { c.Writer = writer.ResponseWriter }()
	stopKeepalive := writer.startKeepalive(excelBPSWSKeepaliveInterval)
	result, err := s.forwardExcelBPS(upstreamCtx, c, account, body, start)
	stopKeepalive()
	if result != nil {
		result.Model = originalModel
		result.OpenAIWSMode = true
		result.ClientDisconnect = result.ClientDisconnect || writer.disconnected
		// The WS continuation cache uses response.id, not the upstream HTTP
		// request ID. Replay every output item, as the shared bridge does, so
		// incremental turns rebuild the full history on either channel.
		if result.ResponseID != "" {
			result.RequestID = result.ResponseID
		}
		result.wsReplayInput = writer.replay.ReplayItems(s.isOpenAIWSStoreDisabledInRequestRaw(body, account))
		result.wsReplayInputExists = len(result.wsReplayInput) > 0
		result.wsAccountFailoverReplayInput = writer.replay.AllItems()
	}
	if writer.err != nil {
		return result, writer.err
	}
	if upstreamCtx.Err() != nil {
		return result, context.Cause(upstreamCtx)
	}
	// A 429 before any output still reaches the existing account failover
	// loop. Admission/RPM errors likewise retain their typed control path.
	var failover *UpstreamFailoverError
	if errors.As(err, &failover) || IsOpenAITurnAdmissionError(err) || IsOpenAIRPMError(err) {
		return result, err
	}
	if err != nil && !writer.terminal {
		status := writer.status
		var errorBody map[string]any
		_ = json.Unmarshal(writer.pending, &errorBody)
		if status < 400 || errorBody["error"] == nil {
			status = http.StatusBadGateway
			errorBody = map[string]any{"error": gin.H{"type": "server_error", "code": "basispoints_bridge_error", "message": "Excel BPS response could not be forwarded"}}
		}
		errorBody["type"], errorBody["status"], errorBody["sequence_number"] = "error", status, 0
		event, marshalErr := json.Marshal(errorBody)
		if marshalErr != nil {
			return result, fmt.Errorf("encode websocket BPS error: %w", marshalErr)
		}
		if writeErr := writer.emit(event); writeErr != nil {
			return result, writeErr
		}
	}
	return result, err
}
