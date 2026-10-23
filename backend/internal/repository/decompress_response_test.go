package repository

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexRequestCompressionWireReplay(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprintf("prepared=%v", prepared), func(t *testing.T) {
			payload := []byte(`{"model":"gpt-5.5","input":"` + strings.Repeat("context ", 8192) + `"}`)
			req, err := newCodexCompressionRequest(t.Context(), payload, prepared)
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
		})
	}
}

func newCodexCompressionRequest(ctx context.Context, payload []byte, prepared bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if prepared {
		req.Body = httputil.NewPrereadBody(payload)
		req.GetBody = func() (io.ReadCloser, error) { return httputil.NewPrereadBody(payload), nil }
	}
	return req, nil
}

type compressionObservedBody struct {
	io.Reader
	reads, closes int
}

func (b *compressionObservedBody) Read(p []byte) (int, error) {
	b.reads++
	return b.Reader.Read(p)
}

func (b *compressionObservedBody) Close() error {
	b.closes++
	return nil
}

type compressionWaitContext struct {
	context.Context
	waiting chan struct{}
}

func (c compressionWaitContext) Done() <-chan struct{} {
	close(c.waiting)
	return c.Context.Done()
}

func TestCodexRequestCompressionBodyOwnership(t *testing.T) {
	for _, outcome := range []string{"success", "read_error", "canceled"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			payload := []byte(`{"input":"original"}`)
			req, err := newCodexCompressionRequest(ctx, payload, false)
			require.NoError(t, err)
			semantic := &compressionObservedBody{Reader: bytes.NewReader(payload)}
			replay := &compressionObservedBody{Reader: bytes.NewReader(payload)}
			req.Body = semantic
			replayCalls := 0
			req.GetBody = func() (io.ReadCloser, error) { replayCalls++; return replay, nil }
			readErr := errors.New("replay read failed")
			if outcome == "read_error" {
				replay.Reader = iotest.ErrReader(readErr)
			} else if outcome == "canceled" {
				cancel()
			}
			sent := false
			client := &http.Client{Transport: upstreamCompressionRoundTripper(func(wire *http.Request) (*http.Response, error) {
				sent = true
				require.NoError(t, wire.Body.Close())
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, nil
			})}
			resp, err := doUpstreamRequest(client, req)
			require.Zero(t, semantic.reads, "compression must read a separate replay")
			require.Same(t, semantic, req.Body)
			require.Empty(t, req.Header.Get("Content-Encoding"))
			switch outcome {
			case "success":
				require.NoError(t, err)
				require.True(t, sent)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, 1, semantic.closes)
				require.Equal(t, 1, replay.closes)
			case "read_error":
				require.ErrorIs(t, err, readErr)
				require.False(t, sent)
				require.Zero(t, semantic.closes)
				require.Equal(t, 1, replay.closes)
			case "canceled":
				require.ErrorIs(t, err, context.Canceled)
				require.False(t, sent)
				require.Zero(t, semantic.closes)
				require.Zero(t, replay.closes)
				require.Zero(t, replayCalls)
			}
			// A failed preparation leaves ownership with the caller.
			if !sent {
				require.NoError(t, req.Body.Close())
			}
		})
	}
}

func TestCodexRequestCompressionConcurrentAdmissionAndCancellation(t *testing.T) {
	compressor, err := newCodexResponsesCompressor(2)
	require.NoError(t, err)
	t.Cleanup(func() { _ = compressor.encoder.Close() })
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan error, 2)
	requests := make([]*http.Request, 2)
	payloads := [][]byte{[]byte(`{"account":"A","input":"first"}`), []byte(`{"account":"B","input":"second"}`)}
	for i, payload := range payloads {
		req, err := newCodexCompressionRequest(t.Context(), payload, true)
		require.NoError(t, err)
		requests[i] = req
		req.GetBody = func() (io.ReadCloser, error) {
			ready <- struct{}{}
			<-release
			return httputil.NewPrereadBody(payload), nil
		}
		go func() { done <- compressor.compress(req) }()
	}
	// Both requests must enter body preparation concurrently. Saturation is
	// held deterministically, without depending on payload size or CPU speed.
	for range requests {
		select {
		case <-ready:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("independent requests were serialized")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waitStarted := make(chan struct{})
	waiting, err := newCodexCompressionRequest(compressionWaitContext{Context: ctx, waiting: waitStarted}, []byte(`{"input":"canceled"}`), false)
	require.NoError(t, err)
	waiting.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("canceled waiter acquired a slot") }
	waitDone := make(chan error, 1)
	go func() { waitDone <- compressor.compress(waiting) }()
	<-waitStarted
	cancel()
	select {
	case err := <-waitDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("compression admission ignored cancellation")
	}
	close(release)
	for range requests {
		require.NoError(t, <-done)
	}
	require.Empty(t, compressor.slots, "success and canceled waits must release all permits")
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	for i, req := range requests {
		wire, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		decoded, err := decoder.DecodeAll(wire, nil)
		require.NoError(t, err)
		require.Equal(t, payloads[i], decoded, "encoder reuse must not share account payloads")
	}
}

// Includes production admission, wire cloning/encoding, and HTTP handoff. The
// transport consumes bytes locally; these numbers do not measure network time.
func BenchmarkCodexRequestCompression(b *testing.B) {
	random := make([]byte, 768<<10)
	_, _ = rand.New(rand.NewSource(1)).Read(random)
	for _, payload := range []struct {
		name string
		body []byte
	}{
		{"text64KiB", []byte(`{"input":"` + strings.Repeat("context ", 8192) + `"}`)},
		{"mixed1MiB", []byte(`{"input":"` + strings.Repeat("context ", 2048) + `","image":"` + base64.StdEncoding.EncodeToString(random) + `"}`)},
	} {
		for _, prepared := range []bool{false, true} {
			for _, parallel := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/prepared=%v/parallel=%v", payload.name, prepared, parallel), func(b *testing.B) {
					if _, err := codexResponsesZstdEncoder(); err != nil {
						b.Fatal(err)
					}
					client := &http.Client{Transport: upstreamCompressionRoundTripper(func(wire *http.Request) (*http.Response, error) {
						_, err := io.Copy(io.Discard, wire.Body)
						_ = wire.Body.Close()
						return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody}, err
					})}
					run := func() {
						req, err := newCodexCompressionRequest(b.Context(), payload.body, prepared)
						if err != nil {
							b.Error(err)
							return
						}
						resp, err := doUpstreamRequest(client, req)
						if err != nil {
							b.Error(err)
							return
						}
						_ = resp.Body.Close()
					}
					b.ReportAllocs()
					b.SetBytes(int64(len(payload.body)))
					b.ResetTimer()
					if parallel {
						b.RunParallel(func(pb *testing.PB) {
							for pb.Next() {
								run()
							}
						})
					} else {
						for range b.N {
							run()
						}
					}
				})
			}
		}
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
