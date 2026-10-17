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
	data []byte
	err  error
}

func (b *terminalBody) Read(p []byte) (int, error) {
	n := copy(p, b.data)
	b.data = b.data[n:]
	if len(b.data) == 0 {
		return n, b.err
	}
	return n, nil
}
func (*terminalBody) Close() error { return nil }

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
			// A later cancellation must not be attributed to the read failure.
			cancel()
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
		require.EqualValues(t, req.ContentLength, fields["bytes_read"])
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
