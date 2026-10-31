package service

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Observe only AFTER the real guarded writer returns. Authorization and write
// deadlines stay owned by the existing writer, never duplicated here.
type nativeOperationWriter struct {
	gin.ResponseWriter
	owner *nativeHTTPOperation
}

func (w *nativeOperationWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if sequence := w.owner.active.Load(); sequence != 0 {
		observed := p
		if err == nil && n >= 0 && n < len(p) {
			observed = p[:n]
		}
		business := nativeClientBytesCommit(observed, w.Header().Get("Content-Type"))
		w.owner.core.Delivered(sequence, model.DeliveryReceipt{BytesWritten: int64(n), HeadersCommitted: w.ResponseWriter.Written(), BusinessCommitted: n > 0 && business, PossiblyCommitted: err != nil && business, Closed: err != nil})
	}
	return n, err
}
func (w *nativeOperationWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *nativeOperationWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
	if sequence := w.owner.active.Load(); sequence != 0 {
		w.owner.core.Delivered(sequence, model.DeliveryReceipt{HeadersCommitted: w.ResponseWriter.Written()})
	}
}
func (w *nativeOperationWriter) Flush() { _ = w.FlushError() }
func (w *nativeOperationWriter) FlushError() error {
	var err error
	confirmed := false
	if f, ok := w.ResponseWriter.(interface{ FlushError() error }); ok {
		err = f.FlushError()
		confirmed = err == nil
	} else {
		w.ResponseWriter.Flush()
	}
	if sequence := w.owner.active.Load(); sequence != 0 {
		w.owner.core.Delivered(sequence, model.DeliveryReceipt{HeadersCommitted: w.ResponseWriter.Written(), FlushAttempted: true, Flushed: confirmed, Closed: err != nil})
	}
	return err
}

// No Unwrap: controller Flush must retain this observer. Deadline control is
// forwarded explicitly, preserving the existing bounded writer's behavior.
func (w *nativeOperationWriter) SetWriteDeadline(t time.Time) error {
	return http.NewResponseController(w.ResponseWriter).SetWriteDeadline(t)
}
func (w *nativeOperationWriter) SetReadDeadline(t time.Time) error {
	return http.NewResponseController(w.ResponseWriter).SetReadDeadline(t)
}
func nativeClientBytesCommit(p []byte, contentType string) bool {
	trimmed := bytes.TrimSpace(p)
	if len(trimmed) == 0 {
		return false
	}
	if !bytes.Contains([]byte(contentType), []byte("text/event-stream")) {
		return true
	}
	for _, line := range bytes.Split(trimmed, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == ':' || bytes.HasPrefix(line, []byte("event:")) {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			return true
		}
		data := bytes.TrimSpace(line[5:])
		if len(data) == 0 {
			continue
		}
		if !gjson.ValidBytes(data) {
			return true
		}
		kind := gjson.GetBytes(data, "type").String()
		// These are real downstream writes, not buffered upstream failure
		// hints. A client-visible terminal/error cannot be replaced by a new
		// generation even if its upstream error class would permit failover.
		switch kind {
		case "error", "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "message_stop":
			return true
		}
		if gjson.GetBytes(data, "response.id").String() != "" || gjson.GetBytes(data, "id").String() != "" {
			return true
		}
		if openAIStreamDataStartsClientOutput(string(data), kind) {
			return true
		}
	}
	return false
}
