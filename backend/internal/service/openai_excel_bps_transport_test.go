package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestExcelBPSTransportDiagnosticsAreCredentialFree(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "req-43885")))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	account := excelAccount()
	err := &url.Error{Op: "Post", URL: "https://secret-user:proxy-password@bps.openai.com/?token=secret-token", Err: fmt.Errorf("credential=raw-secret: %w", syscall.ECONNRESET)}
	recordExcelBPSTransportFailure(ctx, c, account, "private-session-key", err, "transport", 1, false)
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	attempts, ok := events.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, attempts, 1)
	require.Equal(t, "connection_reset", attempts[0].Reason)
	require.Equal(t, 0, attempts[0].UpstreamStatusCode)
	require.Equal(t, "req-43885", logs.All()[0].ContextMap()["request_id"])
	all := fmt.Sprint(attempts[0], logs.All()[0].ContextMap())
	for _, secret := range []string{"secret-user", "proxy-password", "secret-token", "raw-secret", "private-session-key"} {
		require.NotContains(t, all, secret)
	}
}
func TestExcelBPSTransportErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{&net.DNSError{Err: "private host"}, "dns_error"}, {syscall.ECONNREFUSED, "connection_refused"},
		{io.ErrUnexpectedEOF, "unexpected_eof"}, {errors.New("net/http: TLS handshake timeout"), "tls_handshake_timeout"},
		{errors.New("http2: connection lost"), "http2_error"}, {nil, "stream_incomplete"},
	} {
		require.Equal(t, tc.want, transportdiag.Classify(tc.err))
	}
}

// Exercise actual net/http tracing without listening sockets or external traffic.
func TestExcelBPSTransportAndStreamFailureAreServerErrors(t *testing.T) {
	for _, streamFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(streamFailure), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{err: io.EOF}
			if streamFailure {
				upstream.err = nil
				upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\",\"status\":\"in_progress\"}}\n\n"))}
			}
			svc := openAIClientToolsTestService(nil)
			svc.httpUpstream = upstream
			body := []byte("{\"model\":\"gpt-6-astra\",\"stream\":true,\"input\":\"test\"}")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			_, err := svc.Forward(context.Background(), c, excelAccount(), body)
			require.Error(t, err)
			require.Contains(t, rec.Body.String(), "server_error")
			if streamFailure {
				require.Equal(t, 200, rec.Code)
				require.Contains(t, rec.Body.String(), "basispoints_stream_incomplete")
				_, marked := GetOpsStreamError(c)
				require.True(t, marked, "HTTP 200 stream failure must enter Ops errors")
			} else {
				require.Equal(t, 502, rec.Code)
				require.Contains(t, rec.Body.String(), "basispoints_transport_error")
			}
		})
	}
}

type bpsTestUpstream struct {
	httpUpstreamRecorder
	send func(*http.Request, string) (*http.Response, error)
}

func (u *bpsTestUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return u.send(req, proxy)
}
