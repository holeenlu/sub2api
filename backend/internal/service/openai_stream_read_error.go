package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// OpenAIUpstreamHTTP2StreamErrorCode is returned to OpenAI-compatible clients
	// when an upstream HTTP/2 response stream is reset after the request started.
	OpenAIUpstreamHTTP2StreamErrorCode = "upstream_http2_stream_error"
	OpenAIUpstreamStreamReadErrorCode  = "upstream_stream_read_error"
	// OpenAIUpstreamStreamTruncatedCode is returned when an upstream SSE stream
	// closes *cleanly* before delivering any terminal signal. A clean EOF carries
	// no transport error, so without this classification a truncated generation is
	// indistinguishable from a successful one.
	OpenAIUpstreamStreamTruncatedCode = "upstream_stream_truncated"
)

// ErrOpenAIUpstreamStreamTruncated marks an upstream SSE stream that ended at
// EOF — without a read error — before any terminal signal arrived.
var ErrOpenAIUpstreamStreamTruncated = errors.New("upstream stream ended before any terminal chunk")

type openAIUpstreamStreamReadError struct {
	cause         error
	clientCode    string
	clientMessage string
}

func (e *openAIUpstreamStreamReadError) Error() string {
	return fmt.Sprintf("stream usage incomplete: %v", e.cause)
}

func (e *openAIUpstreamStreamReadError) Unwrap() error { return e.cause }

func newOpenAIUpstreamStreamReadError(err error) error {
	code, message := classifyOpenAIUpstreamStreamReadError(err)
	return &openAIUpstreamStreamReadError{
		cause:         err,
		clientCode:    code,
		clientMessage: message,
	}
}

// shouldClassifyOpenAIUpstreamStreamReadError excludes cancellation and
// response-size enforcement from upstream retry.
func shouldClassifyOpenAIUpstreamStreamReadError(err error, contexts ...context.Context) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUpstreamResponseBodyTooLarge) {
		return false
	}
	for _, ctx := range contexts {
		if ctx != nil && ctx.Err() != nil {
			return false
		}
	}
	return true
}

// OpenAIUpstreamStreamReadErrorDetails returns the stable, sanitized client
// classification attached to an upstream stream read failure.
func OpenAIUpstreamStreamReadErrorDetails(err error) (code, message string, ok bool) {
	var streamErr *openAIUpstreamStreamReadError
	if !errors.As(err, &streamErr) || streamErr == nil {
		return "", "", false
	}
	return streamErr.clientCode, streamErr.clientMessage, true
}

func classifyOpenAIUpstreamStreamReadError(err error) (code, message string) {
	if err != nil {
		if errors.Is(err, ErrOpenAIUpstreamStreamTruncated) {
			return OpenAIUpstreamStreamTruncatedCode, "Upstream response stream ended before completion"
		}
		lower := strings.ToLower(err.Error())
		// net/http's HTTP/2 stream error is unexported. Its stable text contains
		// "stream error: stream ID ..."; match only the transport signature and
		// never pass the original text to the client.
		if strings.Contains(lower, "stream error: stream id ") ||
			(strings.Contains(lower, "http2:") && strings.Contains(lower, "stream")) {
			return OpenAIUpstreamHTTP2StreamErrorCode, "Upstream HTTP/2 stream failed"
		}
	}
	return OpenAIUpstreamStreamReadErrorCode, "Upstream response stream was interrupted"
}

// HTTP response readers use detached upstream contexts for final usage. Bound
// that drain independently of upstream pings, just like the native WS reader.
// This guard closes the existing body to unblock both scanners and buffered reads.
type openAIHTTPUsageDrain struct {
	ctx                context.Context
	mu                 sync.Mutex
	closeBody          func() error
	timer              *time.Timer
	stopContexts       []func() bool
	stopped, triggered bool
}

func newOpenAIHTTPUsageDrain(ctx context.Context, closeBody func() error, additional ...context.Context) *openAIHTTPUsageDrain {
	d := &openAIHTTPUsageDrain{closeBody: closeBody, ctx: ctx}
	for _, observed := range append(additional, ctx) {
		if observed != nil {
			d.stopContexts = append(d.stopContexts, context.AfterFunc(observed, d.start))
		}
	}
	return d
}
func (d *openAIHTTPUsageDrain) start() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || d.triggered {
		return
	}
	d.triggered = true
	d.timer = time.AfterFunc(openAIWSUsageDrainTimeout, func() {
		if d.closeBody != nil {
			_ = d.closeBody()
		}
	})
}
func (d *openAIHTTPUsageDrain) disconnected() bool {
	canceled := d.ctx != nil && d.ctx.Err() != nil
	if canceled {
		d.start()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.triggered || canceled
}
func (d *openAIHTTPUsageDrain) stop() {
	for _, stop := range d.stopContexts {
		stop()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
	}
}
