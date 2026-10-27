package httputil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Return bytes and the terminal error in the same Read, including the last
// incomplete chunk; neither its count nor the original error may be lost.
type terminalBody struct {
	data   []byte
	err    error
	onRead func()
}

func (b *terminalBody) Read(p []byte) (int, error) {
	if b.onRead != nil {
		b.onRead()
	}
	n := copy(p, b.data)
	b.data = b.data[n:]
	if len(b.data) == 0 {
		return n, b.err
	}
	return n, nil
}
func (*terminalBody) Close() error { return nil }

// Trigger a deadline deterministically at the underlying read boundary, rather
// than using a timer that can expire before the reader precheck on a busy host.
type bodyReadFailureContext struct{ context.Context }

func (c bodyReadFailureContext) Err() error {
	if errors.Is(context.Cause(c.Context), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}

func TestBodyReadFailureCountsPartialBytesAndSnapshotsContext(t *testing.T) {
	for _, size := range []int{7, (2 << 20) + 17} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			deadline := time.Now().Add(time.Minute)
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", &terminalBody{
				data: bytes.Repeat([]byte{'x'}, size), err: io.ErrUnexpectedEOF,
			}).WithContext(ctx)
			req.ContentLength = int64(size + 100)
			body, err := ReadRequestBodyWithPrealloc(req)
			require.Nil(t, body)
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
			// The truncated upload is attributed to the client at failure time;
			// a later cancellation changes nothing about the recorded failure.
			cancel()
			failure, ok := RequestBodyReadFailureFromRequest(req)
			require.True(t, ok)
			require.Equal(t, BodyReadKindTruncatedBody, failure.Kind)
			require.EqualValues(t, size, failure.BytesRead)
			require.EqualValues(t, size+100, failure.ContentLength)
			require.True(t, failure.ClientAbandoned)
			require.Equal(t, StatusClientClosedRequest, failure.Status())
			core, logs := observer.New(zap.WarnLevel)
			LogRequestBodyReadFailure(zap.New(core), req, err, "group_model_allowlist")
			require.Len(t, logs.All(), 1)
			fields := logs.All()[0].ContextMap()
			require.EqualValues(t, size, fields["bytes_read"])
			require.EqualValues(t, size+100, fields["content_length"])
			require.Equal(t, "read", fields["body_read_phase"])
			require.Equal(t, "group_model_allowlist", fields["body_read_stage"])
			require.Equal(t, "truncated_body", fields["error_kind"])
			require.Equal(t, "unexpected EOF", fields["error_cause"])
			require.Equal(t, "none", fields["context_error"])
			require.Equal(t, deadline.UTC().Format(time.RFC3339Nano), fields["context_deadline"])
			require.Positive(t, fields["deadline_remaining_ms"])
			require.GreaterOrEqual(t, fields["body_read_elapsed_ms"], float64(0))
		})
	}
}

func TestBodyReadFailureDistinguishesTransportCauses(t *testing.T) {
	cases := []struct {
		err         error
		kind, cause string
	}{
		{context.Canceled, "context_canceled", "context canceled"},
		{context.DeadlineExceeded, "deadline_exceeded", "context deadline exceeded"},
		{&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, "connection_reset", "connection reset by peer"},
		{syscall.EPIPE, "broken_pipe", "broken pipe"},
		{&net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, "timeout", "I/O timeout"},
		{http.ErrBodyReadAfterClose, "body_closed", "http: invalid Read on closed Body"},
		{errors.New("secret-payload-marker"), "io_read", "unclassified (message omitted)"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", &terminalBody{data: []byte("partial"), err: tc.err})
			_, err := ReadRequestBodyWithPrealloc(req)
			require.ErrorIs(t, err, tc.err)
			core, logs := observer.New(zap.WarnLevel)
			LogRequestBodyReadFailure(zap.New(core), req, err, "gateway_handler")
			fields := logs.All()[0].ContextMap()
			require.Equal(t, tc.kind, fields["error_kind"])
			require.Equal(t, tc.cause, fields["error_cause"])
			require.NotEmpty(t, fields["error_types"])
			require.NotContains(t, fmt.Sprint(fields), "secret-payload-marker")
		})
	}
}

func TestBodyReadFailureEncodingAndLimitDiagnostics(t *testing.T) {
	for _, enc := range []string{"gzip", "private-payload-marker"} {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("secret-payload-marker"))
		req.Header.Set("Content-Encoding", enc)
		_, err := ReadRequestBodyWithPrealloc(req)
		core, logs := observer.New(zap.WarnLevel)
		LogRequestBodyReadFailure(zap.New(core), req, err, "gateway_handler")
		fields := logs.All()[0].ContextMap()
		if enc == "gzip" {
			require.EqualValues(t, req.ContentLength, fields["bytes_read"])
		} else {
			require.EqualValues(t, 0, fields["bytes_read"], "unsupported coding is rejected before reading the body")
		}
		require.Equal(t, "decode", fields["body_read_phase"])
		if enc == "gzip" {
			require.Equal(t, "decode_content_encoding", fields["error_kind"])
		} else {
			require.Equal(t, "unsupported_content_encoding", fields["error_kind"])
			require.Equal(t, "other", fields["content_encoding"])
		}
		require.NotContains(t, fmt.Sprint(fields), "payload-marker")
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100)))
	req.Body = http.MaxBytesReader(nil, req.Body, 64)
	_, err := ReadRequestBodyWithPrealloc(req)
	core, logs := observer.New(zap.WarnLevel)
	LogRequestBodyReadFailure(zap.New(core), req, err, "group_model_allowlist")
	fields := logs.All()[0].ContextMap()
	require.Equal(t, "max_bytes", fields["error_kind"])
	require.EqualValues(t, 64, fields["body_limit_bytes"])
	require.EqualValues(t, 64, fields["bytes_read"])
}

func TestRequestBodyReadCanceledSnapshotsOnlyAbandonedUploads(t *testing.T) {
	require.False(t, RequestBodyReadCanceled(nil))
	for _, tc := range []struct {
		name       string
		err        error
		contextErr error
		want       bool
	}{
		{"canceled_truncation", io.ErrUnexpectedEOF, context.Canceled, true},
		{"wrapped_cancellation", fmt.Errorf("read body: %w", context.Canceled), nil, true},
		{"canceled_reset", syscall.ECONNRESET, context.Canceled, true},
		{"truncation_alone", io.ErrUnexpectedEOF, nil, true},
		{"connection_reset_alone", syscall.ECONNRESET, nil, true},
		{"unknown_read_error_alone", errors.New("opaque"), nil, false},
		{"unknown_read_error_canceled", errors.New("opaque"), context.Canceled, true},
		{"server_deadline", io.ErrUnexpectedEOF, context.DeadlineExceeded, false},
		{"read_deadline", context.DeadlineExceeded, context.Canceled, false},
		{"read_timeout", os.ErrDeadlineExceeded, context.Canceled, false},
		{"read_limit", &http.MaxBytesError{Limit: 10}, context.Canceled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			body := &terminalBody{err: tc.err}
			if tc.contextErr != nil {
				canceled, cancel := context.WithCancelCause(ctx)
				defer cancel(nil)
				ctx = bodyReadFailureContext{canceled}
				body.onRead = func() { cancel(tc.contextErr) }
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", body).WithContext(ctx)
			require.NoError(t, ctx.Err(), "context must remain active until the underlying Read")
			require.False(t, RequestBodyReadCanceled(req), "canceled context alone is insufficient")
			_, err := ReadRequestBodyWithPrealloc(req)
			require.ErrorIs(t, err, tc.err)
			if tc.contextErr != nil {
				require.ErrorIs(t, ctx.Err(), tc.contextErr)
			}
			require.Equal(t, tc.want, RequestBodyReadCanceled(req))
		})
	}
}

func TestDescribeRequestBodyReadFailureMapsKindsToResponses(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		encoding   string
		wantKind   string
		wantStatus int
		wantType   string
		wantMsg    string
		abandoned  bool
	}{
		{name: "max_bytes", err: &http.MaxBytesError{Limit: 64 << 20}, wantKind: BodyReadKindMaxBytes, wantStatus: http.StatusRequestEntityTooLarge, wantType: "invalid_request_error", wantMsg: "Request body too large, limit is 64MB"},
		{name: "timeout", err: &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, wantKind: BodyReadKindTimeout, wantStatus: http.StatusRequestTimeout, wantType: "request_timeout", wantMsg: "Request body upload timed out"},
		{name: "bare_deadline", err: os.ErrDeadlineExceeded, wantKind: BodyReadKindTimeout, wantStatus: http.StatusRequestTimeout, wantType: "request_timeout", wantMsg: "Request body upload timed out"},
		{name: "context_deadline", err: context.DeadlineExceeded, wantKind: BodyReadKindDeadlineExceeded, wantStatus: http.StatusRequestTimeout, wantType: "request_timeout", wantMsg: "Request body upload timed out"},
		{name: "truncated", err: io.ErrUnexpectedEOF, wantKind: BodyReadKindTruncatedBody, wantStatus: StatusClientClosedRequest, wantType: "invalid_request_error", wantMsg: "Client closed request before the request body was fully received", abandoned: true},
		{name: "reset", err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, wantKind: BodyReadKindConnectionReset, wantStatus: StatusClientClosedRequest, wantType: "invalid_request_error", wantMsg: "Client closed request before the request body was fully received", abandoned: true},
		{name: "canceled", err: context.Canceled, wantKind: BodyReadKindContextCanceled, wantStatus: StatusClientClosedRequest, wantType: "invalid_request_error", wantMsg: "Client closed request before the request body was fully received", abandoned: true},
		{name: "decode", encoding: "gzip", wantKind: BodyReadKindDecode, wantStatus: http.StatusBadRequest, wantType: "invalid_request_error", wantMsg: "Failed to read request body"},
		{name: "unsupported_encoding", encoding: "br", wantKind: BodyReadKindUnsupportedEncoding, wantStatus: http.StatusBadRequest, wantType: "invalid_request_error", wantMsg: "Failed to read request body"},
		{name: "opaque", err: errors.New("opaque"), wantKind: BodyReadKindIORead, wantStatus: http.StatusBadRequest, wantType: "invalid_request_error", wantMsg: "Failed to read request body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &terminalBody{data: []byte("partial"), err: tc.err}
			if tc.err == nil {
				body.err = io.EOF
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", body)
			req.ContentLength = 42
			if tc.encoding != "" {
				req.Header.Set("Content-Encoding", tc.encoding)
			}
			_, err := ReadRequestBodyWithPrealloc(req)
			require.Error(t, err)
			failure := DescribeRequestBodyReadFailure(req, err)
			require.Equal(t, tc.wantKind, failure.Kind)
			require.Equal(t, tc.abandoned, failure.ClientAbandoned)
			require.Equal(t, tc.abandoned, RequestBodyReadCanceled(req))
			if tc.wantKind == BodyReadKindUnsupportedEncoding {
				require.Zero(t, failure.BytesRead)
			} else {
				require.EqualValues(t, 7, failure.BytesRead)
			}
			status, errType, message := failure.Response()
			require.Equal(t, tc.wantStatus, status)
			require.Equal(t, tc.wantType, errType)
			require.Equal(t, tc.wantMsg, message)
			recorded, ok := RequestBodyReadFailureFromRequest(req)
			require.True(t, ok)
			require.Equal(t, failure, recorded)
		})
	}
	// Errors that bypass the shared reader are still classified by kind.
	plain := DescribeRequestBodyReadFailure(httptest.NewRequest(http.MethodPost, "/", nil), &http.MaxBytesError{Limit: 10})
	require.Equal(t, http.StatusRequestEntityTooLarge, plain.Status())
	require.EqualValues(t, 10, plain.LimitBytes)
	require.Zero(t, plain.BytesRead)
}

func TestLenientReadLimitIsNotAttributedToClient(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"x","text":"`+strings.Repeat("a", 64)+`"}`))
	_, err := ReadLenientJSONRequestBodyWithPrealloc(req, 32)
	var limitErr *http.MaxBytesError
	require.ErrorAs(t, err, &limitErr)
	require.EqualValues(t, 32, limitErr.Limit)
	failure := DescribeRequestBodyReadFailure(req, err)
	require.Equal(t, BodyReadKindMaxBytes, failure.Kind)
	require.Equal(t, "decoded_size", failure.Phase)
	require.False(t, failure.ClientAbandoned)
	require.False(t, RequestBodyReadCanceled(req))
	_, _, message := failure.Response()
	require.Equal(t, "Request body too large, limit is 32B", message)
}
