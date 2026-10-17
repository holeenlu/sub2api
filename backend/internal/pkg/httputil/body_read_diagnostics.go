package httputil

import (
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// bodyReadError preserves the original error chain without retaining partial
// request contents. Capture cancellation at failure, not later when a handler
// or middleware has already returned and canceled its context.
type bodyReadError struct {
	err        error
	bytesRead  int64
	elapsed    time.Duration
	phase      string
	contextErr error
	deadline   time.Time
	failedAt   time.Time
}

func (e *bodyReadError) Error() string { return e.err.Error() }
func (e *bodyReadError) Unwrap() error { return e.err }

func newBodyReadError(req *http.Request, err error, bytesRead int64, started time.Time, phase string) error {
	now := time.Now()
	deadline, _ := req.Context().Deadline()
	return &bodyReadError{err: err, bytesRead: bytesRead, elapsed: now.Sub(started), phase: phase,
		contextErr: req.Context().Err(), deadline: deadline, failedAt: now}
}

// LogRequestBodyReadFailure is shared by pre-read middleware and gateway
// handlers; handler-only logging misses failures before routing. WARN events
// use the existing persisted Ops system-log sink, correlated by request_id.
// Do not log arbitrary err.Error(): decoding errors can include client input.
func LogRequestBodyReadFailure(log *zap.Logger, req *http.Request, err error, stage string) {
	if log == nil || err == nil {
		return
	}
	contentLength := int64(-1)
	encoding := "identity"
	var contextErr error
	var deadline time.Time
	failedAt := time.Now()
	if req != nil {
		contentLength = req.ContentLength
		encoding = requestContentEncodingCategory(req.Header.Get("Content-Encoding"))
		contextErr = req.Context().Err()
		deadline, _ = req.Context().Deadline()
	}
	fields := []zap.Field{zap.String("body_read_stage", stage),
		zap.String("error_kind", requestBodyReadErrorKind(err)),
		zap.String("content_encoding", encoding), zap.Int64("content_length", contentLength)}
	var readErr *bodyReadError
	if errors.As(err, &readErr) {
		contextErr, deadline, failedAt = readErr.contextErr, readErr.deadline, readErr.failedAt
		fields = append(fields, zap.Int64("bytes_read", readErr.bytesRead),
			zap.Float64("body_read_elapsed_ms", float64(readErr.elapsed)/float64(time.Millisecond)),
			zap.String("body_read_phase", readErr.phase))
		err = readErr.err
	}
	// Record bounded type information plus recognized transport causes. Unknown
	// messages stay private; type information still distinguishes read failures.
	var types []string
	leaf := err
	for i := 0; leaf != nil && i < 8; i++ {
		types = append(types, fmt.Sprintf("%T", leaf))
		next := errors.Unwrap(leaf)
		if next == nil {
			break
		}
		leaf = next
	}
	fields = append(fields, zap.Strings("error_types", types),
		zap.String("error_cause", bodyReadCause(leaf)),
		zap.String("context_error", bodyReadContextError(contextErr)))
	var netErr net.Error
	if errors.As(err, &netErr) {
		fields = append(fields, zap.Bool("error_timeout", netErr.Timeout()))
	}
	var limitErr *http.MaxBytesError
	if errors.As(err, &limitErr) {
		fields = append(fields, zap.Int64("body_limit_bytes", limitErr.Limit))
	}
	if !deadline.IsZero() {
		fields = append(fields, zap.String("context_deadline", deadline.UTC().Format(time.RFC3339Nano)),
			zap.Float64("deadline_remaining_ms", float64(deadline.Sub(failedAt))/float64(time.Millisecond)))
	}
	log.Warn("read request body failed", fields...)
}

func bodyReadContextError(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "context canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context deadline exceeded"
	default:
		return "unknown"
	}
}

func bodyReadCause(err error) string {
	var limitErr *http.MaxBytesError
	if errors.As(err, &limitErr) {
		return "http: request body too large"
	}
	switch {
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected EOF"
	case errors.Is(err, context.Canceled):
		return "context canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context deadline exceeded"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset by peer"
	case errors.Is(err, syscall.EPIPE):
		return "broken pipe"
	case errors.Is(err, net.ErrClosed):
		return "use of closed network connection"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "I/O timeout"
	case errors.Is(err, http.ErrBodyReadAfterClose):
		return "http: invalid Read on closed Body"
	case errors.Is(err, gzip.ErrHeader):
		return "invalid gzip header"
	case errors.Is(err, gzip.ErrChecksum):
		return "invalid gzip checksum"
	case errors.Is(err, zlib.ErrHeader):
		return "invalid zlib header"
	case errors.Is(err, zlib.ErrChecksum):
		return "invalid zlib checksum"
	case errors.Is(err, errUnsupportedContentEncoding):
		return "unsupported Content-Encoding"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	return "unclassified (message omitted)"
}

func requestContentEncodingCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "identity":
		return "identity"
	case "gzip", "x-gzip":
		return "gzip"
	case "zstd":
		return "zstd"
	case "deflate":
		return "deflate"
	default:
		return "other"
	}
}

func requestBodyReadErrorKind(err error) string {
	if err == nil {
		return "none"
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return "max_bytes"
	}
	if errors.Is(err, errUnsupportedContentEncoding) {
		return "unsupported_content_encoding"
	}
	var readErr *bodyReadError
	if errors.As(err, &readErr) && readErr.phase == "decode" {
		return "decode_content_encoding"
	}
	if errors.Is(err, context.Canceled) {
		return "context_canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "connection_reset"
	}
	if errors.Is(err, syscall.EPIPE) {
		return "broken_pipe"
	}
	if errors.Is(err, http.ErrBodyReadAfterClose) {
		return "body_closed"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "truncated_body"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout"
		}
		return "transport"
	}
	return "io_read"
}
