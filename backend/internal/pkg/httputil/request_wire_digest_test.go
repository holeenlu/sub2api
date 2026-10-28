package httputil

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func TestRequestWireDigestSurvivesDecodeAndWorkingBodyReplacement(t *testing.T) {
	for _, encoding := range []string{"identity", "gzip", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			original := []byte("{\"model\":\"public-alias\",\"input\":\"a\nb\"}")
			wire := encodedRequestBody(t, encoding, original)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(wire))
			req.Header.Set("Content-Encoding", encoding)
			CaptureRequestWire(req)
			_, known := RequestWireDigest(req)
			require.False(t, known)
			normalized, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
			require.NoError(t, err)
			require.NotEqual(t, original, normalized, "normalization is distinct from original wire")
			digest, known := RequestWireDigest(req)
			require.True(t, known)
			require.Equal(t, sha256.Sum256(wire), digest)
			// The allowlist/Composite pipeline reads normalized input, then
			// replaces its working body. Neither may rehash the replacement.
			replacement := []byte("{\"model\":\"upstream-model\",\"input\":\"changed\"}")
			req.Body = NewPrereadBody(replacement)
			req.ContentLength = int64(len(replacement))
			req.Header.Del("Content-Encoding")
			CaptureRequestWire(req)
			body, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
			require.NoError(t, err)
			require.Equal(t, replacement, body)
			after, known := RequestWireDigest(req)
			require.True(t, known)
			require.Equal(t, digest, after)
			require.NotEqual(t, sha256.Sum256(replacement), after)
		})
	}
}

func TestRequestWireDigestSharedReaderCapturesOnlyUntransformedInput(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{\"input\":\"hello\"}"))
	_, err := ReadRequestBodyWithPrealloc(req)
	require.NoError(t, err)
	digest, known := RequestWireDigest(req)
	require.True(t, known, "the first common bounded read also owns raw input")
	require.Equal(t, sha256.Sum256([]byte("{\"input\":\"hello\"}")), digest)
	req = httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = NewPrereadBody([]byte("{\"input\":\"already transformed\"}"))
	CaptureRequestWire(req)
	_, err = ReadRequestBodyWithPrealloc(req)
	require.NoError(t, err)
	digest, known = RequestWireDigest(req)
	require.False(t, known, "a replacement without original provenance stays unknown")
	require.Equal(t, [32]byte{}, digest)
}

func TestRequestWireDigestPartialAndFailedReadsNeverClaimCompleteness(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("complete body"))
	CaptureRequestWire(req)
	_, err := io.ReadFull(req.Body, make([]byte, 3))
	require.NoError(t, err)
	_, known := RequestWireDigest(req)
	require.False(t, known)
	_, err = io.ReadAll(req.Body)
	require.NoError(t, err)
	digest, known := RequestWireDigest(req)
	require.True(t, known)
	require.Equal(t, sha256.Sum256([]byte("complete body")), digest)

	req = httptest.NewRequest(http.MethodPost, "/", io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(io.ErrUnexpectedEOF)))
	req.ContentLength = 7 // Matching the count is not an EOF proof.
	_, err = ReadRequestBodyWithPrealloc(req)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	_, known = RequestWireDigest(req)
	require.False(t, known)
}

func TestRequestWireDigestLimitCannotManufactureEOF(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("too much wire"))
	CaptureRequestWire(req)
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 4)
	_, err := ReadRequestBodyWithPreallocLimit(req, 4)
	require.Error(t, err)
	_, known := RequestWireDigest(req)
	require.False(t, known)
}
