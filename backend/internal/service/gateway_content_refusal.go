package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
)

type upstreamRefusalObserverKey struct{}

// The observer belongs to one authenticated request or WebSocket turn. Only
// explicit upstream policy evidence can invoke it, at most once across retries.
type upstreamRefusalObserver struct {
	once     sync.Once
	remember func()
}

func WithUpstreamRefusalObserver(ctx context.Context, remember func()) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if remember == nil {
		return ctx
	}
	return context.WithValue(ctx, upstreamRefusalObserverKey{}, &upstreamRefusalObserver{remember: remember})
}

func upstreamRefusalObserverFromContext(ctx context.Context) *upstreamRefusalObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(upstreamRefusalObserverKey{}).(*upstreamRefusalObserver)
	return observer
}

func (o *upstreamRefusalObserver) observe(payload []byte) {
	if o != nil && IsUpstreamContentRefusal(payload) {
		o.once.Do(o.remember)
	}
}

func observeUpstreamContentRefusal(ctx context.Context, payload []byte) {
	upstreamRefusalObserverFromContext(ctx).observe(payload)
}

// Error bodies are already bounded by the gateway reader. Restore the body so
// the existing protocol-specific error handler keeps ownership of the reply.
func (s *GatewayService) isContentRefusalResponse(resp *http.Response) bool {
	if resp == nil || resp.StatusCode < 400 || resp.Body == nil {
		return false
	}
	body, err := s.readUpstreamErrorBody(resp)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return err == nil && IsUpstreamContentRefusal(body)
}
