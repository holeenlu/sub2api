//go:build unit

package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

// HTTP/1.1 的中途断连表现为 ECONNRESET / io.ErrUnexpectedEOF，网关改走 h2c
// （Caddy 以 `versions h2c 2` 反代）后同样的中断到读取端变成了 http2.StreamError
// 或连接级的断连哨兵，二者都不能落进 io_read。
func TestRequestBodyReadErrorKind_H2CStreamErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"客户端主动取消", http2.StreamError{StreamID: 1, Code: http2.ErrCodeCancel}, bodyReadKindClientDisconnect},
		{"优雅结束", http2.StreamError{StreamID: 1, Code: http2.ErrCodeNo}, bodyReadKindClientDisconnect},
		{"内部错误", http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}, bodyReadKindTruncatedBody},
		{"流量控制错误", http2.StreamError{StreamID: 1, Code: http2.ErrCodeFlowControl}, bodyReadKindTruncatedBody},
		{"协议错误", http2.StreamError{StreamID: 1, Code: http2.ErrCodeProtocol}, bodyReadKindTruncatedBody},
		// 整条连接断开比 RST_STREAM 更常见：进程被杀、网络中断、代理重启都走
		// closeAllStreamsOnConnClose，读取端拿到的是未导出的 errClientDisconnected。
		{"连接断开", errors.New(h2ClientDisconnectedMessage), bodyReadKindClientDisconnect},
		{"连接断开被包装", fmt.Errorf("read body: %w", errors.New(h2ClientDisconnectedMessage)), bodyReadKindClientDisconnect},
		{"监听器已关闭", net.ErrClosed, bodyReadKindClientDisconnect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, requestBodyReadErrorKind(context.Background(), tc.err))
		})
	}
}

// 端到端守卫：像 Caddy 那样驱动一个真实的 h2c server，确认 handler 实际拿到的
// 错误仍能分类。上面的表驱动用例只覆盖了我们"以为"运行时会给出的错误类型，
// 这里在 Go 运行时换掉错误类型时会先失败。
//
// 生产走 x/net 的 http2.ConfigureServer，标准库另有一份内置 h2 实现，两者的
// StreamError 靠 errors.As 桥接；两种 server、两种中断（RST_STREAM 与直接断开
// 连接）各跑一遍。
func TestRequestBodyReadErrorKind_H2CAbortEndToEnd(t *testing.T) {
	servers := []struct {
		name      string
		configure func(t *testing.T, srv *http.Server)
	}{
		{"std", func(_ *testing.T, _ *http.Server) {}},
		{"x/net", func(t *testing.T, srv *http.Server) {
			require.NoError(t, http2.ConfigureServer(srv, &http2.Server{}))
		}},
	}
	aborts := []struct {
		name  string
		abort func(t *testing.T, conn net.Conn)
	}{
		{"RST_STREAM CANCEL", func(t *testing.T, conn net.Conn) {
			writeH2Frame(t, conn, 0x03, 0x00, 1, []byte{0, 0, 0, 0x08})
		}},
		{"连接直接断开", func(_ *testing.T, conn net.Conn) {
			_ = conn.Close()
		}},
	}

	for _, sv := range servers {
		for _, ab := range aborts {
			t.Run(sv.name+"/"+ab.name, func(t *testing.T) {
				observed := readBodyOverAbortedH2C(t, sv.configure, ab.abort)
				require.Error(t, observed)
				require.Equal(t, bodyReadKindClientDisconnect, requestBodyReadErrorKind(context.Background(), observed),
					"an h2c client abort must not fall through to io_read (got error %T: %v)", observed, observed)
			})
		}
	}
}

// readBodyOverAbortedH2C 起一个 h2c server，发送 HEADERS + 部分 DATA，等 handler
// 进入读取后触发 abort，返回 handler 读请求体时观察到的错误。
func readBodyOverAbortedH2C(
	t *testing.T,
	configure func(t *testing.T, srv *http.Server),
	abort func(t *testing.T, conn net.Conn),
) error {
	t.Helper()

	started := make(chan struct{})
	errCh := make(chan error, 1)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, err := io.ReadAll(r.Body)
		errCh <- err
		w.WriteHeader(http.StatusOK)
	}))
	configure(t, srv.Config)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	srv.Config.Protocols = protocols
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"))
	require.NoError(t, err)
	_, err = conn.Write([]byte{0, 0, 0, 0x04, 0x00, 0, 0, 0, 0}) // empty SETTINGS
	require.NoError(t, err)

	writeH2Frame(t, conn, 0x01, 0x04, 1, encodeLiteralH2Headers(map[string]string{
		":method":        "POST",
		":path":          "/",
		":scheme":        "http",
		":authority":     "example.com",
		"content-length": "1000000",
	}))
	writeH2Frame(t, conn, 0x00, 0x00, 1, make([]byte, 1024)) // partial DATA

	// 等 handler 真正开始而不是 sleep：abort 不能抢在 server 接受流之前。
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started reading")
	}
	abort(t, conn)

	select {
	case observed := <-errCh:
		return observed
	case <-time.After(5 * time.Second):
		t.Fatal("handler never returned")
		return nil
	}
}

func writeH2Frame(t *testing.T, w io.Writer, typ, flags byte, streamID uint32, payload []byte) {
	t.Helper()
	n := len(payload)
	hdr := []byte{
		byte(n >> 16), byte(n >> 8), byte(n),
		typ, flags,
		byte(streamID >> 24), byte(streamID >> 16), byte(streamID >> 8), byte(streamID),
	}
	_, err := w.Write(append(hdr, payload...))
	require.NoError(t, err)
}

// encodeLiteralH2Headers 只用 HPACK 的 "literal header field without indexing —
// new name" 编码，不依赖动态表状态。
func encodeLiteralH2Headers(h map[string]string) []byte {
	order := []string{":method", ":path", ":scheme", ":authority", "content-length"}
	var out []byte
	for _, k := range order {
		v, ok := h[k]
		if !ok {
			continue
		}
		out = append(out, 0x00, byte(len(k)))
		out = append(out, k...)
		out = append(out, byte(len(v)))
		out = append(out, v...)
	}
	return out
}
