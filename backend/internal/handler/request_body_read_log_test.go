//go:build unit

package handler

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/stretchr/testify/require"
)

func TestLogRequestBodyReadFailureClassifiesWithoutPayload(t *testing.T) {
	log, logs := newObservedLogger(t)
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader("secret-payload-marker"))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "gzip")
	req.ContentLength = int64(len("secret-payload-marker"))

	_, readErr := httputil.ReadRequestBodyWithPrealloc(req)
	require.Error(t, readErr)
	logRequestBodyReadFailure(log, req, readErr)

	entries := logs.All()
	require.Len(t, entries, 1)
	require.Equal(t, "read request body failed", entries[0].Message)
	fields := entries[0].ContextMap()
	require.Equal(t, "decode_content_encoding", fields["error_kind"])
	require.Equal(t, "gzip", fields["content_encoding"])
	require.EqualValues(t, req.ContentLength, fields["content_length"])
	require.EqualValues(t, req.ContentLength, fields["bytes_read"])
	require.Equal(t, "gateway_handler", fields["body_read_stage"])
	require.Equal(t, "decode", fields["body_read_phase"])
	require.NotContains(t, entries[0].Message+fmt.Sprint(fields), "secret-payload-marker")
}
