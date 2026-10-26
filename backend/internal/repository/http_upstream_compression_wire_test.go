package repository

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

type codexCompressionWire struct {
	method, path, encoding, contentType string
	protocolMajor                       int
	contentLength                       int64
	transferEncoding                    []string
	body                                []byte
	err                                 error
}

func readCodexCompressionWire(r *http.Request) codexCompressionWire {
	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	return codexCompressionWire{method: r.Method, path: r.URL.Path, encoding: r.Header.Get("Content-Encoding"),
		contentType: r.Header.Get("Content-Type"), protocolMajor: r.ProtoMajor, contentLength: r.ContentLength,
		transferEncoding: r.TransferEncoding, body: body, err: err}
}

func requireCodexCompressionWire(t *testing.T, got codexCompressionWire, payload []byte, protocolMajor int) {
	t.Helper()
	require.NoError(t, got.err)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, protocolMajor, got.protocolMajor)
	require.Equal(t, "zstd", got.encoding)
	require.Equal(t, "application/json", got.contentType)
	require.Equal(t, int64(len(got.body)), got.contentLength)
	require.Empty(t, got.transferEncoding)
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(got.body, nil)
	require.NoError(t, err)
	require.Equal(t, payload, decoded, "one decompression must recover the semantic request")
}

// Keep the production URL and transport selection while routing TCP solely to
// the local fixture. TLS verifies the fixture's certificate and negotiated ALPN;
// no production transport hook, public DNS lookup or insecure TLS is required.
func localCodexCompressionClient(t *testing.T, server *httptest.Server, mode string) *http.Client {
	t.Helper()
	transport, err := buildUpstreamTransport(poolSettings{maxIdleConns: 2, maxIdleConnsPerHost: 2}, nil, mode)
	require.NoError(t, err)
	localAddress := server.Listener.Addr().String()
	localHost, _, err := net.SplitHostPort(localAddress)
	require.NoError(t, err)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "chatgpt.com:443" {
			return nil, fmt.Errorf("unexpected test destination: %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, localAddress)
	}
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = server.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	transport.TLSClientConfig.ServerName = localHost
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}
}

func TestCodexRequestCompressionTLSWireAndRedirects(t *testing.T) {
	for _, mode := range []string{upstreamProtocolModeOpenAIH1, upstreamProtocolModeOpenAIH2, upstreamProtocolModeOpenAIH1Fallback} {
		for _, redirect := range []int{0, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
			t.Run(fmt.Sprintf("%s/redirect=%d", mode, redirect), func(t *testing.T) {
				observed := make(chan codexCompressionWire, 2)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					observed <- readCodexCompressionWire(r)
					if redirect != 0 && r.URL.Path == "/backend-api/codex/responses" {
						http.Redirect(w, r, "/redirected", redirect)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, "{}")
				}))
				server.EnableHTTP2 = mode == upstreamProtocolModeOpenAIH2
				server.StartTLS()
				t.Cleanup(server.Close)
				client := localCodexCompressionClient(t, server, mode)
				payload := []byte(`{"input":"` + strings.Repeat("complete context ", 1024) + `"}`)
				req, err := newCodexCompressionRequest(t.Context(), payload, true)
				require.NoError(t, err)
				req.Header.Set("Content-Encoding", "identity")
				req.Header.Set("Content-Length", fmt.Sprint(len(payload)))
				req.TransferEncoding = []string{"chunked"}
				originalHeader, originalBody := req.Header.Clone(), req.Body
				resp, err := doUpstreamRequest(client, req)
				require.NoError(t, err)
				_, err = io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, http.StatusOK, resp.StatusCode)
				protocolMajor, wantRequests := 1, 1
				if mode == upstreamProtocolModeOpenAIH2 {
					protocolMajor = 2
				}
				if redirect != 0 {
					wantRequests = 2
				}
				require.Len(t, observed, wantRequests)
				first := <-observed
				requireCodexCompressionWire(t, first, payload, protocolMajor)
				if redirect != 0 {
					next := <-observed
					requireCodexCompressionWire(t, next, payload, protocolMajor)
					require.Equal(t, "/redirected", next.path)
					require.Equal(t, first.body, next.body, "redirects replay the existing encoded body")
				}
				require.Same(t, originalBody, req.Body)
				require.Equal(t, originalHeader, req.Header)
				require.Equal(t, int64(len(payload)), req.ContentLength)
				require.Equal(t, []string{"chunked"}, req.TransferEncoding)
				replay, err := req.GetBody()
				require.NoError(t, err)
				semantic, err := io.ReadAll(replay)
				require.NoError(t, err)
				require.NoError(t, replay.Close())
				require.Equal(t, payload, semantic)
			})
		}
	}
}
