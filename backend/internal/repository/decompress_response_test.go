package repository

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexRequestCompressionWireReplay(t *testing.T) {
	payload := []byte(`{"model":"gpt-5.5","input":"` + strings.Repeat("context ", 8192) + `"}`)
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	var first []byte
	client := &http.Client{Transport: upstreamCompressionRoundTripper(func(wire *http.Request) (*http.Response, error) {
		require.Equal(t, "zstd", wire.Header.Get("Content-Encoding"))
		encoded, readErr := io.ReadAll(wire.Body)
		require.NoError(t, readErr)
		require.Equal(t, int64(len(encoded)), wire.ContentLength)
		require.Less(t, len(encoded), len(payload)/10)
		decoder, decodeErr := zstd.NewReader(nil)
		require.NoError(t, decodeErr)
		defer decoder.Close()
		decoded, decodeErr := decoder.DecodeAll(encoded, nil)
		require.NoError(t, decodeErr)
		require.Equal(t, payload, decoded)
		replay, replayErr := wire.GetBody()
		require.NoError(t, replayErr)
		replayed, replayErr := io.ReadAll(replay)
		require.NoError(t, replayErr)
		require.NoError(t, replay.Close())
		require.Equal(t, encoded, replayed)
		if first == nil {
			first = encoded
		} else {
			require.Equal(t, first, encoded)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for i := 0; i < 2; i++ {
		if i > 0 {
			req.Body, err = req.GetBody()
			require.NoError(t, err)
		}
		resp, sendErr := doUpstreamRequest(client, req)
		require.NoError(t, sendErr)
		require.NoError(t, resp.Body.Close())
		require.Empty(t, req.Header.Get("Content-Encoding"), "semantic request must remain uncompressed")
		require.Equal(t, int64(len(payload)), req.ContentLength)
	}
}

type upstreamCompressionRoundTripper func(*http.Request) (*http.Response, error)

func (f upstreamCompressionRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCodexRequestCompressionScopeAndReadFailure(t *testing.T) {
	for _, tc := range []struct {
		name, target, method, contentType, encoding string
		noReplay                                    bool
	}{
		{"public", "https://api.openai.com/v1/responses", "POST", "application/json", "", false},
		{"compact", "https://chatgpt.com/backend-api/codex/responses/compact", "POST", "application/json", "", false},
		{"http", "http://chatgpt.com/backend-api/codex/responses", "POST", "application/json", "", false},
		{"lookalike", "https://chatgpt.com.example.org/backend-api/codex/responses", "POST", "application/json", "", false},
		{"get", "https://chatgpt.com/backend-api/codex/responses", "GET", "application/json", "", false},
		{"nonjson", "https://chatgpt.com/backend-api/codex/responses", "POST", "text/plain", "", false},
		{"encoded", "https://chatgpt.com/backend-api/codex/responses", "POST", "application/json", "gzip", false},
		{"no_replay", "https://chatgpt.com/backend-api/codex/responses", "POST", "application/json", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.target, strings.NewReader("payload"))
			require.NoError(t, err)
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Content-Encoding", tc.encoding)
			if tc.noReplay {
				req.GetBody = nil
			}
			originalBody := req.Body
			require.NoError(t, compressCodexResponsesRequest(req))
			require.Equal(t, originalBody, req.Body)
			require.Equal(t, tc.encoding, req.Header.Get("Content-Encoding"))
		})
	}
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	sentinel := errors.New("replay unavailable")
	req.GetBody = func() (io.ReadCloser, error) { return nil, sentinel }
	client := &http.Client{Transport: upstreamCompressionRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("must not send after compression read failure")
		return nil, nil
	})}
	_, err = doUpstreamRequest(client, req)
	require.ErrorIs(t, err, sentinel)
	for _, mode := range []string{upstreamProtocolModeOpenAIH1, upstreamProtocolModeOpenAIH2, upstreamProtocolModeOpenAIH1Fallback, upstreamProtocolModeLongStreamH2} {
		transport, err := buildUpstreamTransport(poolSettings{}, nil, mode)
		require.NoError(t, err)
		require.Equal(t, mode != upstreamProtocolModeLongStreamH2, transport.DisableCompression)
		transport.CloseIdleConnections()
	}
}

func TestDecompressResponseBodyZstdUsage(t *testing.T) {
	payload := []byte(`{"usage":{"input_tokens":123,"output_tokens":45,"cache_read_input_tokens":67}}`)
	compressed := compressZstd(t, payload)
	resp := newEncodedResponse("zstd", compressed)

	decompressResponseBody(resp)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, body)
	require.Equal(t, int64(123), gjson.GetBytes(body, "usage.input_tokens").Int())
	require.Equal(t, int64(45), gjson.GetBytes(body, "usage.output_tokens").Int())
	require.Equal(t, int64(67), gjson.GetBytes(body, "usage.cache_read_input_tokens").Int())
	require.Empty(t, resp.Header.Get("Content-Encoding"))
	require.Empty(t, resp.Header.Get("Content-Length"))
	require.Equal(t, int64(-1), resp.ContentLength)
	require.NoError(t, resp.Body.Close())
}

func TestDecompressResponseBodyExistingEncodings(t *testing.T) {
	payload := []byte(`{"ok":true}`)
	tests := []struct {
		name     string
		encoding string
		compress func(*testing.T, []byte) []byte
	}{
		{name: "gzip", encoding: "gzip", compress: compressGzip},
		{name: "brotli", encoding: "br", compress: compressBrotli},
		{name: "deflate", encoding: "deflate", compress: compressDeflate},
		{name: "deflate_zlib", encoding: "deflate", compress: func(t *testing.T, payload []byte) []byte {
			var buf bytes.Buffer
			zw := zlib.NewWriter(&buf)
			_, err := zw.Write(payload)
			require.NoError(t, err)
			require.NoError(t, zw.Close())
			return buf.Bytes()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := newEncodedResponse(tt.encoding, tt.compress(t, payload))

			decompressResponseBody(resp)

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, payload, body)
			require.Empty(t, resp.Header.Get("Content-Encoding"))
			require.Empty(t, resp.Header.Get("Content-Length"))
			require.Equal(t, int64(-1), resp.ContentLength)
			require.NoError(t, resp.Body.Close())
		})
	}
}

func TestDecompressResponseBodyWithoutEncodingLeavesBodyUntouched(t *testing.T) {
	originalBody := &responseTestBody{Reader: bytes.NewReader([]byte("plain"))}
	resp := &http.Response{
		Header:        make(http.Header),
		Body:          originalBody,
		ContentLength: 5,
	}

	decompressResponseBody(resp)

	require.Same(t, originalBody, resp.Body)
	require.Equal(t, int64(5), resp.ContentLength)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "plain", string(body))
	require.NoError(t, resp.Body.Close())
}

func TestDecompressResponseBodyInvalidZstdWarnsAndPreservesBody(t *testing.T) {
	previousLogger := slog.Default()
	var logOutput bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	payload := []byte("not a zstd response")
	resp := newEncodedResponse("zstd", payload)

	require.NotPanics(t, func() {
		decompressResponseBody(resp)
	})

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, body)
	require.Equal(t, "zstd", resp.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(payload)), resp.ContentLength)
	require.Contains(t, logOutput.String(), "msg=zstd_decompress_failed")
	require.NoError(t, resp.Body.Close())
}

func TestDecompressResponseBodyEmptyZstdWarnsAndPreservesBody(t *testing.T) {
	previousLogger := slog.Default()
	var logOutput bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logOutput, nil)))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	resp := newEncodedResponse("zstd", nil)

	require.NotPanics(t, func() {
		decompressResponseBody(resp)
	})

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Empty(t, body)
	require.Equal(t, "zstd", resp.Header.Get("Content-Encoding"))
	require.Equal(t, int64(0), resp.ContentLength)
	require.Contains(t, logOutput.String(), "msg=zstd_decompress_failed")
	require.NoError(t, resp.Body.Close())
}

type responseTestBody struct {
	io.Reader
}

func (b *responseTestBody) Close() error {
	return nil
}

func newEncodedResponse(encoding string, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Encoding", encoding)
	header.Set("Content-Length", "123")
	return &http.Response{
		Header:        header,
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func compressZstd(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	require.NoError(t, err)
	_, err = zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func compressGzip(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func compressBrotli(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := brotli.NewWriter(&buf)
	_, err := zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func compressDeflate(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = zw.Write(payload)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
