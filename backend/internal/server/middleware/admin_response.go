package middleware

import (
	"bytes"
	"context"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/gin-gonic/gin"
	"strings"
	"sync"
	"time"
)

// DTO projection lives in response.writeDTO. This writer only watches protected
// streams/downloads. An already admitted ordinary JSON response may complete.
type managementStreamWriter struct {
	gin.ResponseWriter
	context   context.Context
	cancel    context.CancelFunc
	heartbeat sync.Once
	eventOpen bool
	eventTail []byte
}

func (w *managementStreamWriter) check() error {
	contentType := w.Header().Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		return nil
	}
	if strings.Contains(contentType, "text/event-stream") {
		w.heartbeat.Do(func() { go watchManagementStream(w.context, w.cancel, 30*time.Second) })
	}
	if err := authz.Revalidate(w.context); err != nil {
		w.cancel()
		return err
	}
	return w.context.Err()
}
func watchManagementStream(ctx context.Context, cancel context.CancelFunc, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if authz.Revalidate(ctx) != nil {
				cancel()
				return
			}
		}
	}
}
func (w *managementStreamWriter) Write(data []byte) (int, error) {
	stream := strings.Contains(w.Header().Get("Content-Type"), "text/event-stream")
	if !stream || !w.eventOpen {
		if err := w.check(); err != nil {
			return 0, err
		}
	}
	if err := w.context.Err(); err != nil {
		return 0, err
	}
	if stream {
		tail := append(w.eventTail, data...)
		w.eventOpen = !(bytes.HasSuffix(tail, []byte("\n\n")) || bytes.HasSuffix(tail, []byte("\r\n\r\n")))
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		w.eventTail = append(w.eventTail[:0], tail...)
	}
	return w.ResponseWriter.Write(data)
}
func (w *managementStreamWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *managementStreamWriter) WriteHeaderNow() {
	if w.context.Err() == nil {
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *managementStreamWriter) Flush() {
	if w.context.Err() == nil {
		w.ResponseWriter.Flush()
	}
}
