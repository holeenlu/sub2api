package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

const streamWriteTimeout = 30 * time.Second
const streamWriterKey = "gateway_bounded_stream_writer"

// GuardSSEWrites bounds each write/flush, rather than the duration of a long
// generation. It also interrupts an in-flight socket write on cancellation.
// Nested relay/compatibility calls share the request's existing guard.
func GuardSSEWrites(c *gin.Context) func() {
	if c == nil || c.Request == nil || c.Writer == nil {
		return func() {}
	}
	if value, ok := c.Get(streamWriterKey); ok && value != nil {
		return func() {}
	}
	return guardSSEWrites(c, streamWriteTimeout)
}

func guardSSEWrites(c *gin.Context, timeout time.Duration) func() {
	original := c.Writer
	writeCtx, cancel := context.WithCancel(c.Request.Context())
	var raw http.ResponseWriter = original
	for {
		unwrapper, ok := raw.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		next := unwrapper.Unwrap()
		if next == nil || next == raw {
			break
		}
		raw = next
	}
	w := &boundedStreamWriter{ResponseWriter: original, ctx: writeCtx, cancel: cancel, controller: http.NewResponseController(raw), timeout: timeout}
	c.Set(streamWriterKey, w)
	c.Writer = w
	// Only the deadline controller is touched by this callback: never Gin state.
	done := make(chan struct{})
	stop := context.AfterFunc(w.ctx, func() {
		w.deadlineMu.Lock()
		_ = w.controller.SetWriteDeadline(time.Now())
		w.deadlineMu.Unlock()
		close(done)
	})
	return func() {
		if !stop() {
			<-done
		}
		cancel()
		_ = w.controller.SetWriteDeadline(time.Time{})
		if c.Writer == w {
			c.Writer = original
		}
		c.Set(streamWriterKey, nil)
	}
}

type boundedStreamWriter struct {
	gin.ResponseWriter
	ctx        context.Context
	cancel     context.CancelFunc
	controller *http.ResponseController
	timeout    time.Duration
	deadlineMu sync.Mutex
	failure    atomic.Pointer[streamWriteFailure]
}

func (w *boundedStreamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *boundedStreamWriter) prepare() error {
	if err := w.writeErr(); err != nil {
		return err
	}
	w.deadlineMu.Lock()
	defer w.deadlineMu.Unlock()
	if err := w.ctx.Err(); err != nil {
		w.fail(err)
		return err
	}
	err := w.controller.SetWriteDeadline(time.Now().Add(w.timeout))
	// In-memory recorders have no socket. Real HTTP/1.1 and HTTP/2 writers do.
	if err != nil && !errors.Is(err, http.ErrNotSupported) {
		w.fail(err)
	}
	return w.writeErr()
}

func (w *boundedStreamWriter) Write(p []byte) (int, error) {
	defer w.clearDeadline()
	n := 0
	for len(p) > 0 {
		if err := w.prepare(); err != nil {
			return n, err
		}
		chunk := p[:min(len(p), 32*1024)]
		written, err := w.ResponseWriter.Write(chunk)
		n += written
		if err == nil && written != len(chunk) {
			err = io.ErrShortWrite
		}
		if err != nil {
			w.fail(err)
			return n, err
		}
		p = p[written:]
	}
	return n, nil
}

func (w *boundedStreamWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *boundedStreamWriter) Flush()                            { _ = w.FlushError() }
func (w *boundedStreamWriter) FlushError() error {
	defer w.clearDeadline()
	if err := w.prepare(); err != nil {
		return err
	}
	w.WriteHeaderNow()
	w.fail(w.controller.Flush())
	return w.writeErr()
}

func downstreamStreamError(c *gin.Context) error {
	if value, ok := c.Get(streamWriterKey); ok {
		if w, ok := value.(*boundedStreamWriter); ok {
			return w.writeErr()
		}
	}
	return nil
}

func (w *boundedStreamWriter) clearDeadline() {
	w.deadlineMu.Lock()
	defer w.deadlineMu.Unlock()
	// An idle interval between events is not a blocked write. Do not leave an
	// expired HTTP/2 deadline behind, which cannot be revived by a later write.
	if w.ctx.Err() == nil {
		_ = w.controller.SetWriteDeadline(time.Time{})
	}
}

type streamWriteFailure struct{ err error }

func (w *boundedStreamWriter) fail(err error) {
	if err != nil {
		if w.failure.CompareAndSwap(nil, &streamWriteFailure{err}) && w.cancel != nil {
			w.cancel()
		}
	}
}
func (w *boundedStreamWriter) writeErr() error {
	if failure := w.failure.Load(); failure != nil {
		return failure.err
	}
	return nil
}

func streamWriteContext(c *gin.Context) context.Context {
	if c != nil {
		if value, ok := c.Get(streamWriterKey); ok {
			if w, ok := value.(*boundedStreamWriter); ok {
				return w.ctx
			}
		}
	}
	return nil
}
