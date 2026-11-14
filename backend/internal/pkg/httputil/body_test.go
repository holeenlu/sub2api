package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const samplePayload = `{"model":"gpt-5.5","input":"hi","stream":false}`

func newRequestWithBody(t *testing.T, body []byte, encoding string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	req.ContentLength = int64(len(body))
	return req
}

func TestReadRequestBodyWithPrealloc_PassesThroughIdentity(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesZstd(t *testing.T) {
	enc, _ := zstd.NewWriter(nil)
	compressed := enc.EncodeAll([]byte(samplePayload), nil)
	_ = enc.Close()

	req := newRequestWithBody(t, compressed, "zstd")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
	if req.Header.Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding should be cleared after decoding")
	}
	if req.ContentLength != int64(len(samplePayload)) {
		t.Fatalf("ContentLength not updated: %d", req.ContentLength)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesGzip(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(samplePayload)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	req := newRequestWithBody(t, buf.Bytes(), "gzip")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_DecodesDeflate(t *testing.T) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(samplePayload)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	req := newRequestWithBody(t, buf.Bytes(), "deflate")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_RejectsUnsupportedEncoding(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "br")
	_, err := ReadRequestBodyWithPrealloc(req)
	if err == nil {
		t.Fatal("expected error for unsupported encoding, got nil")
	}
	if !strings.Contains(err.Error(), "br") {
		t.Fatalf("error should mention encoding, got %v", err)
	}
}

func TestReadRequestBodyWithPrealloc_RejectsCorruptZstd(t *testing.T) {
	req := newRequestWithBody(t, []byte("not actually zstd"), "zstd")
	_, err := ReadRequestBodyWithPrealloc(req)
	if err == nil {
		t.Fatal("expected error for corrupt zstd body, got nil")
	}
}

func TestReadRequestBodyWithPrealloc_NilBody(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "/v1/responses", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil body, got %q", got)
	}
}

func TestReadRequestBodyWithPrealloc_RespectsIdentityEncoding(t *testing.T) {
	req := newRequestWithBody(t, []byte(samplePayload), "identity")
	got, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != samplePayload {
		t.Fatalf("body mismatch: got %q", got)
	}
}

// compressZeros streams n zero bytes through the named encoder so the test
// never holds the decoded payload as its source.
func compressZeros(t *testing.T, encoding string, n int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w io.WriteCloser
	switch encoding {
	case "zstd":
		enc, err := zstd.NewWriter(&buf)
		if err != nil {
			t.Fatalf("zstd writer: %v", err)
		}
		w = enc
	case "gzip":
		w = gzip.NewWriter(&buf)
	default:
		t.Fatalf("unsupported test encoding %q", encoding)
	}
	if _, err := io.CopyN(w, zeroReader{}, n); err != nil {
		t.Fatalf("%s write: %v", encoding, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("%s close: %v", encoding, err)
	}
	return buf.Bytes()
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestReadRequestBodyWithPrealloc_DecodedOverflowReturns413(t *testing.T) {
	for _, encoding := range []string{"zstd", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			exact := newRequestWithBody(t, compressZeros(t, encoding, maxDecompressedBodySize), encoding)
			got, err := ReadRequestBodyWithPrealloc(exact)
			if err != nil {
				t.Fatalf("exactly %d decoded bytes must pass: %v", maxDecompressedBodySize, err)
			}
			if int64(len(got)) != maxDecompressedBodySize {
				t.Fatalf("decoded length = %d, want %d", len(got), maxDecompressedBodySize)
			}
			got = nil

			over := newRequestWithBody(t, compressZeros(t, encoding, maxDecompressedBodySize+1), encoding)
			_, err = ReadRequestBodyWithPrealloc(over)
			if err == nil {
				t.Fatal("expected overflow error, got nil (body was silently truncated)")
			}
			var limitErr *http.MaxBytesError
			if !errors.As(err, &limitErr) || limitErr.Limit != maxDecompressedBodySize {
				t.Fatalf("expected MaxBytesError{Limit: %d}, got %T %v", maxDecompressedBodySize, err, err)
			}
			failure := DescribeRequestBodyReadFailure(over, err)
			status, errType, message := failure.Response()
			if status != http.StatusRequestEntityTooLarge || errType != "invalid_request_error" || message != "Request body too large, limit is 64MB" {
				t.Fatalf("response = %d %q %q, want 413 invalid_request_error 64MB", status, errType, message)
			}
			if failure.Kind != BodyReadKindMaxBytes || failure.Phase != "decoded_size" || failure.ClientAbandoned {
				t.Fatalf("failure = %+v, want max_bytes/decoded_size, not abandoned", failure)
			}
		})
	}
}
