package server

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// Bound only request-body reads. A server-wide WriteTimeout would terminate
// legitimate SSE responses; leaving the body unbounded permits stalled uploads.
func requestBodyReadDeadline(next http.Handler, timeout time.Duration) http.Handler {
	if timeout <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
			next.ServeHTTP(w, r)
			return
		}
		controller := http.NewResponseController(w)
		if err := controller.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			// In-process test writers/custom transports may not expose socket deadlines.
			// Real net/http HTTP/1 and HTTP/2 server writers implement this operation.
			next.ServeHTTP(w, r)
			return
		}
		r.Body = &deadlineRequestBody{ReadCloser: r.Body, clear: func() { _ = controller.SetReadDeadline(time.Time{}) }}
		next.ServeHTTP(w, r)
	})
}

type deadlineRequestBody struct {
	io.ReadCloser
	once  sync.Once
	clear func()
}

func (b *deadlineRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.once.Do(b.clear)
	}
	return n, err
}
func (b *deadlineRequestBody) Close() error {
	// Retain the deadline while net/http drains an unread body on Close.
	defer b.once.Do(b.clear)
	return b.ReadCloser.Close()
}
