package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestStreamWriterFailureDoesNotBecomeFirstOutputFailover(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, wire := range []string{
			"",
			`data: {"type":"response.failed","response":{"error":{"code":"server_error","message":"upstream failed"}}}` + "\n\n",
			`data: {"type":"response.completed","response":{"id":"empty","output":[]}}` + "\n\n",
		} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Writer = &openAIChatFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
			finish := GuardSSEWrites(c)
			_, writeErr := c.Writer.Write([]byte(": keepalive\n\n"))
			require.Error(t, writeErr)
			var body io.ReadCloser
			closeWriter := func() {}
			if wire == "" {
				reader, writer := io.Pipe()
				body = reader
				closeWriter = func() { _ = writer.Close() }
			} else {
				body = io.NopCloser(strings.NewReader(wire))
			}
			resp := &http.Response{Body: body, Header: http.Header{}}
			svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{OpenAIFirstOutputTimeoutSeconds: 1}}}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			var err error
			var disconnected bool
			if passthrough {
				result, e := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "m", "m")
				require.NotNil(t, result)
				err, disconnected = e, result.clientDisconnected
			} else {
				result, e := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "m", "m")
				require.NotNil(t, result)
				err, disconnected = e, result.clientDisconnected
			}
			closeWriter()
			finish()
			if !strings.Contains(wire, "response.completed") {
				require.Error(t, err)
			}
			require.True(t, disconnected)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "a downstream failure must not trigger upstream retry: passthrough=%v wire=%s", passthrough, wire)
		}
	}
}

type flushFailureRecorder struct {
	*httptest.ResponseRecorder
	err error
}

func (w *flushFailureRecorder) FlushError() error { return w.err }

func TestKeepaliveFlushFailurePreservesWriteFailure(t *testing.T) {
	for _, heartbeat := range []bool{false, true} {
		writeErr := errors.New("downstream flush failed")
		recorder := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		finish := GuardSSEWrites(c)
		stop := StartSSEKeepalive(c, time.Hour)
		wrapped := c.Writer.(*openAICompactKeepaliveWriter)
		if heartbeat {
			require.False(t, wrapped.k.beat())
			require.False(t, wrapped.k.beat())
		} else {
			require.ErrorIs(t, http.NewResponseController(c.Writer).Flush(), writeErr)
		}
		require.ErrorIs(t, downstreamStreamError(c), writeErr)
		require.ErrorIs(t, streamWriteContext(c).Err(), context.Canceled)
		stop()
		finish()
	}
}

func TestStreamWriterFlushFailureStartsBoundedUsageDrain(t *testing.T) {
	writeErr := errors.New("downstream flush failed")
	w := &flushFailureRecorder{ResponseRecorder: httptest.NewRecorder(), err: writeErr}
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	original := c.Request.Context()
	finish := GuardSSEWrites(c)
	defer finish()
	closed := make(chan struct{})
	drain := newOpenAIHTTPUsageDrain(original, func() error { close(closed); return nil }, streamWriteContext(c))
	defer drain.stop()
	c.Writer.Flush()
	require.ErrorIs(t, downstreamStreamError(c), writeErr)
	require.NoError(t, original.Err(), "write failure must not cancel the parent request owned by another layer")
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("flush failure did not bound the upstream usage drain")
	}
	require.True(t, drain.disconnected())
}

func TestStreamWriterPreservesRequestContextChanges(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	parent := c.Request.Context()
	finish := GuardSSEWrites(c)
	type middlewareValue struct{}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), middlewareValue{}, "kept"))
	finish()
	require.NoError(t, parent.Err())
	require.NoError(t, c.Request.Context().Err())
	require.Equal(t, "kept", c.Request.Context().Value(middlewareValue{}))
}

// Exercise real socket backpressure, including HTTP/2 flow control. Recorder
// tests cannot establish whether a blocked Write is interrupted by cancellation.
func TestStreamWriterSlowConsumer(t *testing.T) {
	for _, h2 := range []bool{false, true} {
		for _, cancelRequest := range []bool{false, true} {
			name := "http1_timeout"
			if h2 {
				name = "http2_timeout"
			}
			if cancelRequest {
				name += "_cancel"
			}
			t.Run(name, func(t *testing.T) {
				finished := make(chan error, 1)
				cancelReady := make(chan context.CancelFunc, 1)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					defer cancel()
					c, _ := gin.CreateTestContext(w)
					c.Request = r.WithContext(ctx)
					timeout := 150 * time.Millisecond
					if cancelRequest {
						timeout = 5 * time.Second
					}
					finish := guardSSEWrites(c, timeout)
					defer finish()
					c.Header("Content-Type", "text/event-stream")
					_, err := io.WriteString(c.Writer, ": ready\n\n")
					c.Writer.Flush()
					cancelReady <- cancel
					chunk := []byte(strings.Repeat("x", 32*1024))
					for i := 0; i < 2048 && err == nil; i++ {
						_, err = c.Writer.Write(chunk)
					}
					finished <- err
				}))
				server.EnableHTTP2 = h2
				server.StartTLS()
				defer server.Close()
				client := server.Client()
				client.Timeout = 3 * time.Second
				resp, err := client.Get(server.URL)
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()
				if h2 {
					require.Equal(t, 2, resp.ProtoMajor)
				} else {
					require.Equal(t, 1, resp.ProtoMajor)
				}
				cancel := <-cancelReady
				if cancelRequest {
					cancel()
				}
				select {
				case err := <-finished:
					require.Error(t, err)
				case <-time.After(2 * time.Second):
					_ = resp.Body.Close()
					cancel()
					t.Fatal("downstream write remained blocked")
				}
			})
		}
	}
}

func TestStreamWriterIdleBetweenEventsDoesNotExpireHTTP2Stream(t *testing.T) {
	finished := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		finish := guardSSEWrites(c, 30*time.Millisecond)
		defer finish()
		c.Header("Content-Type", "text/event-stream")
		_, err := io.WriteString(c.Writer, ": first\n\n")
		c.Writer.Flush()
		time.Sleep(100 * time.Millisecond) // model thinking time is not a blocked write
		if err == nil {
			_, err = io.WriteString(c.Writer, ": second\n\n")
		}
		c.Writer.Flush()
		if err == nil {
			err = downstreamStreamError(c)
		}
		finished <- err
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	for i := 0; i < 2; i++ {
		resp, err := server.Client().Get(server.URL)
		require.NoError(t, err)
		require.Equal(t, 2, resp.ProtoMajor)
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		require.NoError(t, err)
		require.Equal(t, ": first\n\n: second\n\n", string(body))
		require.NoError(t, <-finished)
	}
}
