package server

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// Bound only request-body reads with an idle deadline: timeout is the longest
// pause tolerated between two successful reads, renewed whenever bytes arrive.
// Slow but steady uploads therefore never hit an absolute limit, while a
// stalled upload still fails. A server-wide WriteTimeout would terminate
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
		now := time.Now()
		if err := controller.SetReadDeadline(now.Add(timeout)); err != nil {
			// In-process test writers/custom transports may not expose socket deadlines.
			// Real net/http HTTP/1 and HTTP/2 server writers implement this operation.
			next.ServeHTTP(w, r)
			return
		}
		r.Body = newDeadlineRequestBody(r.Body, timeout, now, controller.SetReadDeadline)
		next.ServeHTTP(w, r)
	})
}

// deadlineRequestBody renews the socket read deadline as the upload makes
// progress. Renewals are throttled to once per timeout/4 so a fast upload does
// not pay a deadline syscall per Read while a stall is still caught within
// timeout..timeout*5/4 of the last byte.
type deadlineRequestBody struct {
	io.ReadCloser
	timeout     time.Duration
	renewEvery  time.Duration
	lastRenewed time.Time
	setDeadline func(time.Time) error
	now         func() time.Time
	once        sync.Once
}

func newDeadlineRequestBody(body io.ReadCloser, timeout time.Duration, started time.Time, setDeadline func(time.Time) error) *deadlineRequestBody {
	return &deadlineRequestBody{
		ReadCloser:  body,
		timeout:     timeout,
		renewEvery:  timeout / 4,
		lastRenewed: started,
		setDeadline: setDeadline,
		now:         time.Now,
	}
}

func (b *deadlineRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.renew()
	}
	if err == io.EOF {
		b.once.Do(b.clear)
	}
	return n, err
}

func (b *deadlineRequestBody) renew() {
	now := b.now()
	if now.Sub(b.lastRenewed) < b.renewEvery {
		return
	}
	if err := b.setDeadline(now.Add(b.timeout)); err != nil {
		return
	}
	b.lastRenewed = now
}

func (b *deadlineRequestBody) clear() { _ = b.setDeadline(time.Time{}) }

func (b *deadlineRequestBody) Close() error {
	// Retain the deadline while net/http drains an unread body on Close.
	defer b.once.Do(b.clear)
	return b.ReadCloser.Close()
}
