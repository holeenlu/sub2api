package service

import (
	"errors"
	"net/http"
	"sync"
	"time"

	responseswire "github.com/Wei-Shaw/sub2api/internal/gateway/protocol/responses"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var responsesWSKeepaliveEvent = []byte(`{"type":"keepalive"}`)

// responsesEventWriter serializes normalized Responses events for a client
// transport or protocol converter. Write only buffers bounded HTTP error bodies;
// it never decodes an intermediate SSE stream.
type responsesEventWriter struct {
	gin.ResponseWriter
	header        http.Header
	status, size  int
	pending       []byte
	maxFrameBytes int // provided by the actual adapter envelope budget
	err           error
	model         string
	write         func([]byte) error
	startDrain    func()
	context       *gin.Context

	// mu serializes client writes between the forwarder and the keepalive
	// ticker, and guards the state both of them read.
	mu            sync.Mutex
	terminal      bool
	disconnected  bool
	lastWrite     time.Time
	replay        openAIWSToolCallReplayCollector
	collectReplay bool
	failureOrigin openAIWSFailureOrigin
}

func (w *responsesEventWriter) Header() http.Header { return w.header }
func (w *responsesEventWriter) Status() int         { return w.status }
func (w *responsesEventWriter) Size() int           { return w.size }
func (w *responsesEventWriter) Written() bool       { return w.size >= 0 }
func (w *responsesEventWriter) WriteHeader(status int) {
	if !w.Written() && status > 0 {
		w.status = status
	}
}
func (w *responsesEventWriter) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
	}
}
func (w *responsesEventWriter) Flush()                            {}
func (w *responsesEventWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// WriteResponseEvent receives one complete semantic event. The output callback
// alone chooses WS frames or the final client protocol encoding.
func (w *responsesEventWriter) WriteResponseEvent(payload []byte) error {
	if w.err != nil {
		return w.err
	}
	limit := w.maxFrameBytes
	if limit <= 0 {
		w.err = errors.New("response event buffer limit is unavailable")
		return w.err
	}
	if len(payload) > limit {
		w.err = errors.New("websocket response event exceeds buffer limit")
		return w.err
	}
	w.WriteHeaderNow()
	w.size += len(payload)
	w.err = w.emit(payload)
	return w.err
}

func (w *responsesEventWriter) emit(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	kind := gjson.GetBytes(payload, "type").String()
	if w.collectReplay {
		w.replay.AddEvent(kind, payload)
	}
	w.terminal = w.terminal || responseswire.IsTerminalEvent(kind) || kind == "error"
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
	markOpenAIWSClientVisibleFailure(w.context, kind, payload, w.failureOrigin)
	return nil
}

// keepAlive sends a keepalive event once the client has seen nothing for a
// full interval. Write failures other than a disconnect surface on the
// forwarder's next event.
func (w *responsesEventWriter) keepAlive(interval time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disconnected || w.terminal || time.Since(w.lastWrite) < interval {
		return
	}
	if err := w.write(responsesWSKeepaliveEvent); err != nil {
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
func (w *responsesEventWriter) startKeepalive(interval time.Duration) (stop func()) {
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
	var stopOnce sync.Once
	return func() { stopOnce.Do(func() { close(done) }); <-exited }
}

func (w *responsesEventWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.maxFrameBytes <= 0 || len(w.pending)+len(p) > w.maxFrameBytes {
		w.err = errors.New("response error body exceeds buffer limit")
		return 0, w.err
	}
	w.WriteHeaderNow()
	w.pending = append(w.pending, p...)
	w.size += len(p)
	return len(p), nil
}
