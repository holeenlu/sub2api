package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func encodedRequestBody(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	if encoding == "identity" || encoding == "preread" {
		return body
	}
	var buffer bytes.Buffer
	var writer io.WriteCloser
	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&buffer)
	case "deflate":
		var err error
		writer, err = zlib.NewWriterLevel(&buffer, zlib.DefaultCompression)
		require.NoError(t, err)
	case "zstd":
		var err error
		writer, err = zstd.NewWriter(&buffer, zstd.WithEncoderConcurrency(1))
		require.NoError(t, err)
	default:
		t.Fatalf("unsupported test coding %s", encoding)
	}
	_, err := writer.Write(body)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}

func TestRequestBodyRouteBudgetTightensEveryReadPath(t *testing.T) {
	for _, encoding := range []string{"identity", "preread", "gzip", "deflate", "zstd"} {
		for _, explicit := range []int64{0, 64, 256} {
			for _, extra := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s_explicit_%d_extra_%d", encoding, explicit, extra), func(t *testing.T) {
					limit := int64(128)
					if explicit > 0 {
						limit = min(limit, explicit)
					}
					body := bytes.Repeat([]byte("x"), int(limit)+extra)
					encoded := encodedRequestBody(t, encoding, body)
					header := encoding
					if encoding == "preread" {
						header = ""
					}
					req := newRequestWithBody(t, encoded, header)
					ctx := WithRequestBodyLimit(req.Context(), 256)
					ctx = WithRequestBodyLimit(ctx, 128)
					ctx = WithRequestBodyLimit(ctx, 1024)
					ctx = WithRequestBodyLimit(ctx, 0)
					ctx = WithRequestBodyLimit(ctx, -1)
					req = req.WithContext(ctx)
					if encoding == "preread" {
						req.Body = NewPrereadBody(body)
					}
					var got []byte
					var err error
					if explicit == 0 {
						got, err = ReadRequestBodyWithPrealloc(req)
					} else {
						got, err = ReadRequestBodyWithPreallocLimit(req, explicit)
					}
					if extra == 0 {
						require.NoError(t, err)
						require.Equal(t, body, got)
					} else {
						var size *http.MaxBytesError
						require.ErrorAs(t, err, &size)
						require.Equal(t, limit, size.Limit)
						require.Nil(t, got)
						require.Equal(t, http.StatusRequestEntityTooLarge, DescribeRequestBodyReadFailure(req, err).Status())
					}
				})
			}
		}
	}
}

func TestRequestBodyRouteBudgetReplacesGenericDecodedFallback(t *testing.T) {
	const limit = maxDecompressedBodySize + 1
	encoded := compressZeros(t, "gzip", limit)
	req := newRequestWithBody(t, encoded, "gzip")
	req = req.WithContext(WithRequestBodyLimit(req.Context(), limit))
	got, err := ReadRequestBodyWithPrealloc(req)
	require.NoError(t, err)
	require.Len(t, got, limit)
}

// A valid raw-block frame keeps these window tests tiny. Window descriptor
// 0x68 advertises 8 MiB and 0x70 advertises 16 MiB; neither declares an FCS.
func zstdRequestRawFrame(window byte, body []byte) []byte {
	block := uint32(len(body))<<3 | 1
	frame := []byte{0x28, 0xb5, 0x2f, 0xfd, 0, window, byte(block), byte(block >> 8), byte(block >> 16)}
	return append(frame, body...)
}

func TestRequestBodyZstdWindowIsIndependentOfOutputBudget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		encoded     []byte
		limit       int64
		want        string
		windowError bool
		outputError bool
	}{
		{"eight MiB window tiny body", zstdRequestRawFrame(0x68, []byte("ok")), 2, "ok", false, false},
		{"sixteen MiB window tiny body", zstdRequestRawFrame(0x70, []byte("ok")), 64, "", true, false},
		{"large single segment is a window error", []byte{0x28, 0xb5, 0x2f, 0xfd, 0xa0, 0, 0, 0, 1, 17, 0, 0, 'o', 'k'}, 64, "", true, false},
		{"concatenated frames", append(zstdRequestRawFrame(0x68, []byte("ab")), zstdRequestRawFrame(0x68, []byte("cd"))...), 4, "abcd", false, false},
		{"concatenated output overflow", append(zstdRequestRawFrame(0x68, []byte("ab")), zstdRequestRawFrame(0x68, []byte("cd"))...), 3, "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequestWithBody(t, tc.encoded, "zstd")
			req = req.WithContext(WithRequestBodyLimit(req.Context(), tc.limit))
			got, err := ReadRequestBodyWithPrealloc(req)
			var size *http.MaxBytesError
			switch {
			case tc.windowError:
				require.True(t, errors.Is(err, zstd.ErrWindowSizeExceeded) || errors.Is(err, zstd.ErrDecoderSizeExceeded), "%v", err)
				require.False(t, errors.As(err, &size), "window metadata does not prove actual output overflow")
				require.Equal(t, http.StatusBadRequest, DescribeRequestBodyReadFailure(req, err).Status())
			case tc.outputError:
				require.ErrorAs(t, err, &size)
				require.Equal(t, tc.limit, size.Limit)
				require.Equal(t, http.StatusRequestEntityTooLarge, DescribeRequestBodyReadFailure(req, err).Status())
			default:
				require.NoError(t, err)
				require.Equal(t, tc.want, string(got))
			}
		})
	}
}

func TestRequestBodyRejectsAmbiguousContentEncodingBeforeRead(t *testing.T) {
	for _, header := range []http.Header{
		{"Content-Encoding": {"gzip", "zstd"}},
		{"Content-Encoding": {"gzip", "gzip"}},
		{"Content-Encoding": {"gzip, zstd"}},
		{"Content-Encoding": {"identity"}, "content-encoding": {"gzip"}},
	} {
		req := newRequestWithBody(t, []byte(samplePayload), "")
		req.Header = header
		_, err := ReadRequestBodyWithPrealloc(req)
		require.ErrorIs(t, err, errUnsupportedContentEncoding)
		failure := DescribeRequestBodyReadFailure(req, err)
		require.Equal(t, http.StatusBadRequest, failure.Status())
		require.Zero(t, failure.BytesRead)
	}
}

type bodyCancelAfterChecksContext struct {
	context.Context
	cancel    context.CancelFunc
	remaining atomic.Int32
}

func (c *bodyCancelAfterChecksContext) Err() error {
	if c.remaining.Add(-1) == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestRequestBodyCancellationDuringDecodeAndPreread(t *testing.T) {
	for _, encoding := range []string{"gzip", "zstd"} {
		t.Run(encoding, func(t *testing.T) {
			encoded := compressZeros(t, encoding, 32<<10)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			controlled := &bodyCancelAfterChecksContext{Context: ctx, cancel: cancel}
			controlled.remaining.Store(12) // raw upload finishes; later codec reads observe cancellation
			req := newRequestWithBody(t, encoded, encoding).WithContext(controlled)
			got, err := ReadRequestBodyWithPrealloc(req)
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, got)
			failure := DescribeRequestBodyReadFailure(req, err)
			require.EqualValues(t, len(encoded), failure.BytesRead)
			require.Equal(t, BodyReadKindContextCanceled, failure.Kind)
			require.Equal(t, StatusClientClosedRequest, failure.Status())
			require.True(t, RequestBodyReadCanceled(req))
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := newRequestWithBody(t, nil, "").WithContext(ctx)
	req.Body = NewPrereadBody([]byte(samplePayload))
	got, err := ReadRequestBodyWithPrealloc(req)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func TestLenientRouteBudgetPreservesBOMAllowanceAndNormalizationLimit(t *testing.T) {
	for _, encoding := range []string{"identity", "preread", "gzip", "zstd"} {
		for _, tail := range []string{"", " "} {
			t.Run(encoding+fmt.Sprint(len(tail)), func(t *testing.T) {
				body := append([]byte{0xef, 0xbb, 0xbf}, []byte("{}"+tail)...)
				header := encoding
				if encoding == "preread" {
					header = ""
				}
				req := newRequestWithBody(t, encodedRequestBody(t, encoding, body), header)
				req = req.WithContext(WithRequestBodyLimit(req.Context(), 2))
				if encoding == "preread" {
					req.Body = NewPrereadBody(body)
				}
				got, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
				if tail == "" {
					require.NoError(t, err)
					require.Equal(t, "{}", string(got))
				} else {
					var size *http.MaxBytesError
					require.ErrorAs(t, err, &size)
					require.EqualValues(t, 2, size.Limit)
					failure, found := RequestBodyReadFailureFromRequest(req)
					require.True(t, found)
					require.EqualValues(t, 2, failure.LimitBytes)
				}
			})
		}
	}
	body := []byte(`{"input":"raw-control"}`)
	body = bytes.ReplaceAll(body, []byte("raw-control"), []byte{10})
	req := newRequestWithBody(t, body, "")
	req = req.WithContext(WithRequestBodyLimit(req.Context(), int64(len(body))))
	_, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
	var size *http.MaxBytesError
	require.ErrorAs(t, err, &size)
	require.EqualValues(t, len(body), size.Limit)
	require.Equal(t, "normalize", DescribeRequestBodyReadFailure(req, err).Phase)
}
