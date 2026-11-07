package handler

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// The HTTP handshake has already been hijacked. A stream needs independent Gin
// status/header bookkeeping; its protocol output uses OpenAIWSDownstream.Write.
type openAIWSLaneWriter struct {
	header       http.Header
	status, size int
	closed       chan bool
}

func newOpenAIWSLaneWriter() *openAIWSLaneWriter {
	return &openAIWSLaneWriter{header: make(http.Header), status: 200, size: -1, closed: make(chan bool)}
}
func (w *openAIWSLaneWriter) Header() http.Header { return w.header }
func (w *openAIWSLaneWriter) WriteHeader(status int) {
	if w.size < 0 {
		w.status = status
	}
}
func (w *openAIWSLaneWriter) WriteHeaderNow() {
	if w.size < 0 {
		w.size = 0
	}
}
func (w *openAIWSLaneWriter) Write(p []byte) (int, error) {
	w.WriteHeaderNow()
	w.size += len(p)
	return len(p), nil
}
func (w *openAIWSLaneWriter) WriteString(p string) (int, error) { return w.Write([]byte(p)) }
func (w *openAIWSLaneWriter) Status() int                       { return w.status }
func (w *openAIWSLaneWriter) Size() int                         { return w.size }
func (w *openAIWSLaneWriter) Written() bool                     { return w.size >= 0 }
func (w *openAIWSLaneWriter) Flush()                            { w.WriteHeaderNow() }
func (w *openAIWSLaneWriter) CloseNotify() <-chan bool          { return w.closed }
func (w *openAIWSLaneWriter) Pusher() http.Pusher               { return nil }
func (w *openAIWSLaneWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("websocket stream is already upgraded")
}
