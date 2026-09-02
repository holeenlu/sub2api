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
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// HTTP/1.1 客户端中途放弃上传时内核发的是 FIN，net/http 的 body 在
// Content-Length 未读满的情况下把它翻译成裸 io.ErrUnexpectedEOF——没有任何
// 可供 errors.Is 判定的断连身份。只按错误归类会得到 truncated_body（400 且
// 进 ops_error_logs），而同一个用户操作走 RST 时是 client_disconnect（499 且
// 不记录）。请求上下文在这条路径上已经被 connReader.handleReadError 取消，
// 是唯一可靠的补充判据。
func TestRequestBodyReadErrorKind_UnexpectedEOFWithCancelledContextIsDisconnect(t *testing.T) {
	cancelled := cancelledContext()

	require.Equal(t, bodyReadKindClientDisconnect, requestBodyReadErrorKind(cancelled, io.ErrUnexpectedEOF))
	require.Equal(t, bodyReadKindClientDisconnect, requestBodyReadErrorKind(cancelled,
		fmt.Errorf("read body: %w", io.ErrUnexpectedEOF)))

	// 连接还在（ctx 未取消）时仍是链路截断，值得记录。
	require.Equal(t, bodyReadKindTruncatedBody, requestBodyReadErrorKind(context.Background(), io.ErrUnexpectedEOF))
	require.Equal(t, bodyReadKindTruncatedBody, requestBodyReadErrorKind(nil, io.ErrUnexpectedEOF))
}

// ctx 只是 io.ErrUnexpectedEOF 分支的补充判据，不得替换现有的文案/哨兵匹配，
// 也不得改变 h2 的判定顺序：x/net 的 closeStream 先 CloseWithError 再
// cancelCtx，对 h2 来说 ctx 是竞态锚点，用它当首要规则会把 RST_STREAM 的
// 协议错误也说成客户端断连。
func TestRequestBodyReadErrorKind_CancelledContextDoesNotReclassifyOtherErrors(t *testing.T) {
	cancelled := cancelledContext()

	require.Equal(t, bodyReadKindTruncatedBody,
		requestBodyReadErrorKind(cancelled, http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal}))
	require.Equal(t, bodyReadKindTruncatedBody,
		requestBodyReadErrorKind(cancelled, http2.StreamError{StreamID: 1, Code: http2.ErrCodeProtocol}))
	require.Equal(t, bodyReadKindMaxBytes,
		requestBodyReadErrorKind(cancelled, &http.MaxBytesError{Limit: 1024}))
	require.Equal(t, bodyReadKindTransportTimeout,
		requestBodyReadErrorKind(cancelled, &net.OpError{Op: "read", Err: os.ErrDeadlineExceeded}))
	require.Equal(t, bodyReadKindUnsupportedContentEncoding,
		requestBodyReadErrorKind(cancelled, errors.New(`decode Content-Encoding "br": unsupported Content-Encoding`)))
	require.Equal(t, bodyReadKindIORead,
		requestBodyReadErrorKind(cancelled, errors.New("boom")))
}

// 响应策略也必须跟着走：499 且不进 ops_error_logs。
func TestRespondRequestBodyReadFailure_UnexpectedEOFAfterDisconnectSkipsOpsRecord(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody).WithContext(cancelledContext())

	var gotStatus int
	var gotType string
	RespondRequestBodyReadFailure(c, nil, io.ErrUnexpectedEOF,
		func(_ *gin.Context, status int, errType, _ string) {
			gotStatus, gotType = status, errType
		})

	require.Equal(t, statusClientClosedRequest, gotStatus)
	require.Equal(t, "invalid_request_error", gotType)
	require.True(t, shouldSkipOpsErrorRecord(c))
}

// 端到端守卫：真实 HTTP/1.1 连接上声明 Content-Length 后只发一部分请求体再
// 断开，确认 handler 观察到的错误加上请求上下文能归到 client_disconnect。
// 表驱动用例只覆盖了我们「以为」运行时会给的错误形态，这里在 Go 运行时改变
// 翻译方式时会先失败。
func TestRequestBodyReadErrorKind_HTTP11AbortEndToEnd(t *testing.T) {
	started := make(chan struct{})
	kindCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		_, err := io.ReadAll(r.Body)
		errCh <- err
		kindCh <- requestBodyReadErrorKind(r.Context(), err)
		w.WriteHeader(http.StatusOK)
	}))
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	srv.Config.Protocols = protocols
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte("POST / HTTP/1.1\r\nHost: example.com\r\nContent-Length: 1000000\r\n\r\n"))
	require.NoError(t, err)
	_, err = conn.Write(make([]byte, 1024))
	require.NoError(t, err)

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started reading")
	}
	require.NoError(t, conn.Close())

	var observed error
	var kind string
	select {
	case observed = <-errCh:
		kind = <-kindCh
	case <-time.After(5 * time.Second):
		t.Fatal("handler never returned")
	}

	require.Error(t, observed)
	require.Truef(t,
		errors.Is(observed, io.ErrUnexpectedEOF) || errors.Is(observed, syscall.ECONNRESET),
		"unexpected body read error %T: %v", observed, observed)
	require.Equal(t, bodyReadKindClientDisconnect, kind,
		"an HTTP/1.1 client abort must not be recorded as a truncated body (got error %T: %v)", observed, observed)
}
