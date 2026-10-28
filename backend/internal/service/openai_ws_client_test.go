package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestCoderOpenAIWSClientConnEncodedPayload(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, body, err := conn.Read(r.Context())
		if err == nil {
			received <- body
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	client := &coderOpenAIWSClientConn{conn: conn}
	require.Error(t, client.WriteJSON(ctx, json.RawMessage(`{"incomplete":`)))
	raw := json.RawMessage(`{"type":"response.create","input":[9007199254740993],"unknown":"<中文>","model":"gpt-5.1"}`)
	require.NoError(t, client.WriteJSON(ctx, raw))
	select {
	case body := <-received:
		require.Equal(t, []byte(raw), body, "只发送有效最终字节，保留顺序、精度和未知字段")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCoderOpenAIWSClientDialer_CompressionNegotiationAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode coderws.CompressionMode
	}{
		{"context_takeover", coderws.CompressionContextTakeover},
		{"server_declines", coderws.CompressionDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestHeaders := make(chan http.Header, 1)
			serverDone := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestHeaders <- r.Header.Clone()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: tc.mode})
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.CloseNow()
				for range 2 {
					kind, payload, err := conn.Read(ctx)
					if err == nil && kind != coderws.MessageText {
						err = fmt.Errorf("expected a JSON text message, received type %d", kind)
					}
					if err == nil {
						err = conn.Write(ctx, coderws.MessageText, payload)
					}
					if err != nil {
						serverDone <- err
						return
					}
				}
				serverDone <- nil
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			headers := http.Header{"Openai-Beta": {openAIWSBetaV2Value}, "User-Agent": {"native-compression-test"}}
			client, _, responseHeaders, err := newDefaultOpenAIWSClientDialer().Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), headers, "")
			require.NoError(t, err)
			defer client.(openAIWSForceCloser).CloseNow()
			select {
			case got := <-requestHeaders:
				require.Contains(t, got.Get("Sec-WebSocket-Extensions"), "permessage-deflate")
				require.Equal(t, openAIWSBetaV2Value, got.Get("OpenAI-Beta"))
				require.Equal(t, "native-compression-test", got.Get("User-Agent"))
				require.Empty(t, got.Get("Content-Encoding"), "WS compression is negotiated through extensions")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Equal(t, "websocket", strings.ToLower(responseHeaders.Get("Upgrade")))
			if tc.mode == coderws.CompressionContextTakeover {
				require.Contains(t, responseHeaders.Get("Sec-WebSocket-Extensions"), "permessage-deflate")
			} else {
				require.Empty(t, responseHeaders.Get("Sec-WebSocket-Extensions"))
			}
			for turn := range 2 {
				raw := json.RawMessage(fmt.Sprintf(`{"type":"response.create","nonce":9007199254740993,"turn":%d,"input":%q}`, turn, strings.Repeat("完整上下文 <&> ", 256)))
				require.Greater(t, len(raw), 128, "exercise the production context-takeover compression threshold")
				require.NoError(t, client.WriteJSON(ctx, raw))
				decoded, err := client.ReadMessage(ctx)
				require.NoError(t, err)
				require.Equal(t, []byte(raw), decoded, "both peers must decode each complete JSON message exactly")
			}
			select {
			case err := <-serverDone:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

// Historical encoding is retained only as a benchmark baseline. Both cases
// include recovery-target preparation and send through a real local WS socket.
func BenchmarkOpenAIWSPreparedPayload(b *testing.B) {
	fixtures := []struct {
		name   string
		fields map[string]any
	}{
		{"short", map[string]any{"input": "hello"}},
		{"long", map[string]any{"input": strings.Repeat("long context <&> ", 16384)}},
		{"tools", map[string]any{"input": []any{map[string]any{"type": "function_call_output", "call_id": "call_1", "output": strings.Repeat("result ", 8192)}}, "tools": []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}}}}}},
		{"image", map[string]any{"input": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + strings.Repeat("A", 256*1024)}}}}}},
	}
	for _, fixture := range fixtures {
		b.Run(fixture.name, func(b *testing.B) {
			payload := fixture.fields
			payload["type"] = "response.create"
			payload["model"] = "gpt-5.1"
			for _, raw := range []bool{false, true} {
				name := "former_three_encodings"
				if raw {
					name = "one_encoding_reused"
				}
				b.Run(name, func(b *testing.B) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						conn, err := coderws.Accept(w, r, nil)
						if err != nil {
							return
						}
						defer conn.CloseNow()
						conn.SetReadLimit(2 << 20)
						for {
							if _, _, err = conn.Read(r.Context()); err != nil {
								return
							}
						}
					}))
					defer server.Close()
					conn, _, err := coderws.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
					if err != nil {
						b.Fatal(err)
					}
					defer conn.CloseNow()
					client := &coderOpenAIWSClientConn{conn: conn}
					account := &Account{ID: 1}
					b.SetBytes(int64(len(payloadAsJSONBytes(payload))))
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						var ctx context.Context
						var value any
						if raw {
							encoded := payloadAsJSONBytes(payload)
							ctx = withTurnRecoveryTarget(context.Background(), account, "websocket", "/responses", "", "grant", encoded)
							value = json.RawMessage(encoded)
						} else {
							ctx = withTurnRecoveryTarget(context.Background(), account, "websocket", "/responses", "", "grant", payload)
							encoded := payloadAsJSONBytes(payload)
							if len(encoded) == 0 {
								b.Fatal("empty encoding")
							}
							value = payload
						}
						if err := client.WriteJSON(ctx, value); err != nil {
							b.Fatal(err)
						}
					}
					b.StopTimer()
				})
			}
		})
	}
}

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientReuse(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	c1, err := impl.proxyHTTPClient("http://127.0.0.1:8080")
	require.NoError(t, err)
	c2, err := impl.proxyHTTPClient("http://127.0.0.1:8080")
	require.NoError(t, err)
	require.Same(t, c1, c2, "同一代理地址应复用同一个 HTTP 客户端")

	c3, err := impl.proxyHTTPClient("http://127.0.0.1:8081")
	require.NoError(t, err)
	require.NotSame(t, c1, c3, "不同代理地址应分离客户端")
}

func TestCoderOpenAIWSClientDialer_ProxyHTTPClientInvalidURL(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.proxyHTTPClient("://bad")
	require.Error(t, err)
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		client, err := impl.proxyHTTPClient(scheme + "://127.0.0.1:1080")
		require.NoError(t, err)
		transport := client.Transport.(*http.Transport)
		request, _ := http.NewRequest("GET", "https://api.openai.com/v1/responses", nil)
		proxy, err := transport.Proxy(request)
		require.NoError(t, err)
		require.NotNil(t, proxy)
		require.Equal(t, "127.0.0.1:1080", proxy.Host)
	}
	_, err = impl.proxyHTTPClient("ftp://127.0.0.1:1080")
	require.Error(t, err)
}

func TestCoderOpenAIWSClientDialer_TransportMetricsSnapshot(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	_, err := impl.proxyHTTPClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	_, err = impl.proxyHTTPClient("http://127.0.0.1:18080")
	require.NoError(t, err)
	_, err = impl.proxyHTTPClient("http://127.0.0.1:18081")
	require.NoError(t, err)

	snapshot := impl.SnapshotTransportMetrics()
	require.Equal(t, int64(1), snapshot.ProxyClientCacheHits)
	require.Equal(t, int64(2), snapshot.ProxyClientCacheMisses)
	require.InDelta(t, 1.0/3.0, snapshot.TransportReuseRatio, 0.0001)
}

func TestCoderOpenAIWSClientDialer_ProxyClientCacheCapacity(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	total := openAIWSProxyClientCacheMaxEntries + 32
	for i := 0; i < total; i++ {
		_, err := impl.proxyHTTPClient(fmt.Sprintf("http://127.0.0.1:%d", 20000+i))
		require.NoError(t, err)
	}

	impl.proxyMu.Lock()
	cacheSize := len(impl.proxyClients)
	impl.proxyMu.Unlock()

	require.LessOrEqual(t, cacheSize, openAIWSProxyClientCacheMaxEntries, "代理客户端缓存应受容量上限约束")
}

func TestCoderOpenAIWSClientDialer_ProxyClientCacheIdleTTL(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	oldProxy := "http://127.0.0.1:28080"
	_, err := impl.proxyHTTPClient(oldProxy)
	require.NoError(t, err)

	impl.proxyMu.Lock()
	oldEntry := impl.proxyClients[oldProxy]
	require.NotNil(t, oldEntry)
	oldEntry.lastUsedUnixNano = time.Now().Add(-openAIWSProxyClientCacheIdleTTL - time.Minute).UnixNano()
	impl.proxyMu.Unlock()

	// 触发一次新的代理获取，驱动 TTL 清理。
	_, err = impl.proxyHTTPClient("http://127.0.0.1:28081")
	require.NoError(t, err)

	impl.proxyMu.Lock()
	_, exists := impl.proxyClients[oldProxy]
	impl.proxyMu.Unlock()

	require.False(t, exists, "超过空闲 TTL 的代理客户端应被回收")
}

func TestCoderOpenAIWSClientDialer_ProxyTransportTLSHandshakeTimeout(t *testing.T) {
	dialer := newDefaultOpenAIWSClientDialer()
	impl, ok := dialer.(*coderOpenAIWSClientDialer)
	require.True(t, ok)

	client, err := impl.proxyHTTPClient("http://127.0.0.1:38080")
	require.NoError(t, err)
	require.NotNil(t, client)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport)
	require.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout)
}

func TestCoderOpenAIWSClientConn_DoesNotSupportIdlePingWithoutReader(t *testing.T) {
	require.False(t, (&coderOpenAIWSClientConn{}).SupportsIdlePingWithoutReader())
}
