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

type bodyReadFailureKey struct{}

func (e *bodyReadError) Error() string { return e.err.Error() }
func (e *bodyReadError) Unwrap() error { return e.err }

func newBodyReadError(req *http.Request, err error, bytesRead int64, started time.Time, phase string) error {
	now := time.Now()
	deadline, _ := req.Context().Deadline()
	readErr := &bodyReadError{err: err, bytesRead: bytesRead, elapsed: now.Sub(started), phase: phase,
		contextErr: req.Context().Err(), deadline: deadline, failedAt: now}
	// Keep only the failure-time classification for outer middleware and the
	// Ops error log, which otherwise see a generic status after the original
	// read error is discarded.
	*req = *req.WithContext(context.WithValue(req.Context(), bodyReadFailureKey{}, DescribeRequestBodyReadFailure(req, readErr)))
	return readErr
}

// StatusClientClosedRequest is the nginx-style status recorded when the client
// abandoned the upload before the request body was fully received.
const StatusClientClosedRequest = 499

// Error kinds reported by RequestBodyReadErrorKind.
const (
	BodyReadKindMaxBytes            = "max_bytes"
	BodyReadKindTimeout             = "timeout"
	BodyReadKindDeadlineExceeded    = "deadline_exceeded"
	BodyReadKindTruncatedBody       = "truncated_body"
	BodyReadKindConnectionReset     = "connection_reset"
	BodyReadKindBrokenPipe          = "broken_pipe"
	BodyReadKindContextCanceled     = "context_canceled"
	BodyReadKindTransport           = "transport"
	BodyReadKindBodyClosed          = "body_closed"
	BodyReadKindDecode              = "decode_content_encoding"
	BodyReadKindUnsupportedEncoding = "unsupported_content_encoding"
	BodyReadKindIORead              = "io_read"
)

// RequestBodyReadFailure is the bounded, content-free classification of a
// failed request-body read, shared by response writers and the Ops error log.
type RequestBodyReadFailure struct {
	// Kind is RequestBodyReadErrorKind of the original error.
	Kind string `json:"error_kind"`
	// Phase is the reader phase that failed: read, decode, decoded_size,
	// cached_size or normalize. Empty when the error did not come from the
	// shared reader.
	Phase string `json:"phase,omitempty"`
	// BytesRead counts raw bytes received before the failure.
	BytesRead int64 `json:"bytes_read"`
	// ContentLength is the declared request Content-Length (-1 when unknown).
	ContentLength int64 `json:"content_length"`
	// LimitBytes is the enforced limit for max_bytes failures.
	LimitBytes int64 `json:"limit_bytes,omitempty"`
	// ClientAbandoned reports that the client gave up the upload (canceled,
	// disconnected or truncated the body); the gateway answers 499 and Ops
	// treats the row as a client cancellation.
	ClientAbandoned bool `json:"client_abandoned"`
}

// Timeout reports a stalled upload cut by the server's read deadline.
func (f RequestBodyReadFailure) Timeout() bool {
	return f.Kind == BodyReadKindTimeout || f.Kind == BodyReadKindDeadlineExceeded
}

// Status maps the failure to the client-facing status code: 413 for size
// limits, 408 for stalled uploads, 499 when the client abandoned the upload
// and 400 for decoding or otherwise malformed bodies.
func (f RequestBodyReadFailure) Status() int {
	switch {
	case f.Kind == BodyReadKindMaxBytes:
		return http.StatusRequestEntityTooLarge
	case f.Timeout():
		return http.StatusRequestTimeout
	case f.ClientAbandoned:
		return StatusClientClosedRequest
	default:
		return http.StatusBadRequest
	}
}

// Response returns the status, error type and message every gateway error
// format writes for a failed body read. Formats keep their own envelope.
func (f RequestBodyReadFailure) Response() (status int, errType, message string) {
	switch status = f.Status(); status {
	case http.StatusRequestEntityTooLarge:
		return status, "invalid_request_error", BodyTooLargeMessage(f.LimitBytes)
	case http.StatusRequestTimeout:
		return status, "request_timeout", "Request body upload timed out"
	case StatusClientClosedRequest:
		return status, "invalid_request_error", "Client closed request before the request body was fully received"
	default:
		return status, "invalid_request_error", "Failed to read request body"
	}
}

// BodyTooLargeMessage formats the 413 message with the enforced limit.
func BodyTooLargeMessage(limit int64) string {
	const mb = 1024 * 1024
	if limit >= mb {
		return fmt.Sprintf("Request body too large, limit is %dMB", limit/mb)
	}
	return fmt.Sprintf("Request body too large, limit is %dB", limit)
}

// DescribeRequestBodyReadFailure classifies err for the request it failed on.
// Errors produced by the shared reader carry byte counts and the cancellation
// state observed at failure time; other errors are classified by kind alone.
func DescribeRequestBodyReadFailure(req *http.Request, err error) RequestBodyReadFailure {
	failure := RequestBodyReadFailure{Kind: RequestBodyReadErrorKind(err), ContentLength: -1}
	if req != nil {
		failure.ContentLength = req.ContentLength
	}
	var limitErr *http.MaxBytesError
	if errors.As(err, &limitErr) {
		failure.LimitBytes = limitErr.Limit
	}
	var readErr *bodyReadError
	if errors.As(err, &readErr) {
		failure.Phase = readErr.phase
		failure.BytesRead = readErr.bytesRead
		failure.ClientAbandoned = readErr.clientAbandoned()
		return failure
	}
	var contextErr error
	if req != nil {
		contextErr = req.Context().Err()
	}
	failure.ClientAbandoned = bodyReadClientAbandoned(err, failure.Kind, "", contextErr)
	return failure
}

func (e *bodyReadError) clientAbandoned() bool {
	return bodyReadClientAbandoned(e.err, requestBodyReadErrorKind(e.err), e.phase, e.contextErr)
}

// bodyReadClientAbandoned reports that the client gave up the upload: the
// request context was canceled, the connection was reset or closed, or the
// body ended before the declared Content-Length. Size limits, read deadlines,
// request deadlines and decoding failures are never attributed to the client.
func bodyReadClientAbandoned(err error, kind, phase string, contextErr error) bool {
	if phase != "" && phase != "read" {
		return false
	}
	if errors.Is(contextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	switch kind {
	case BodyReadKindMaxBytes, BodyReadKindTimeout, BodyReadKindDeadlineExceeded,
		BodyReadKindDecode, BodyReadKindUnsupportedEncoding:
		return false
	case BodyReadKindContextCanceled, BodyReadKindTruncatedBody, BodyReadKindConnectionReset,
		BodyReadKindBrokenPipe, BodyReadKindTransport:
		return true
	}
	return errors.Is(contextErr, context.Canceled)
}

// RequestBodyReadCanceled reports that the shared body reader observed the
// client abandoning the upload: a canceled request context, a reset or closed
// connection, or a body truncated before its declared length. Decoding or size
// errors, read deadlines, and cancellation after the failed read do not
// qualify. No request contents are retained.
func RequestBodyReadCanceled(req *http.Request) bool {
	failure, ok := RequestBodyReadFailureFromRequest(req)
	return ok && failure.ClientAbandoned
}

// RequestBodyReadFailureFromRequest returns the failure the shared body reader
// recorded on req, if any.
func RequestBodyReadFailureFromRequest(req *http.Request) (RequestBodyReadFailure, bool) {
	if req == nil {
		return RequestBodyReadFailure{}, false
	}
	failure, ok := req.Context().Value(bodyReadFailureKey{}).(RequestBodyReadFailure)
	return failure, ok
}

// RequestBodyReadErrorKind returns the bounded classification of a body read
// error (max_bytes, timeout, truncated_body, decode_content_encoding, ...).
func RequestBodyReadErrorKind(err error) string {
	return requestBodyReadErrorKind(err)
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
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return "timeout"
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
