//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNativeWSMaintenanceHandshake403BoundedProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, challenge, class, format string
		requestScoped                                     bool
	}{
		{name: "empty", class: "handshake_forbidden_unknown", format: "empty"},
		{name: "html_unknown", body: "<html>private-body secret@example.test</html>", contentType: "text/html", class: "handshake_forbidden_unknown", format: "html"},
		{name: "challenge", body: "<html>private-body</html>", contentType: "text/html", challenge: " challenge ", class: "handshake_edge_challenge", format: "html", requestScoped: true},
		{name: "edge_block", body: `{"error":{"cloudflare_error":true,"code":1010,"message":"private-body"}}`, class: "handshake_edge_block", format: "json", requestScoped: true},
		{name: "credential", body: `{"error":{"code":"token_revoked","message":"private-body"}}`, class: "handshake_credentials_rejected", format: "json"},
		{name: "account", body: `{"error":{"code":"workspace_suspended","message":"private-body"}}`, class: "handshake_account_unavailable", format: "json"},
		{name: "request_policy", body: `{"error":{"code":"content_policy_violation","message":"private-body"}}`, class: "handshake_request_policy", format: "json", requestScoped: true},
		{name: "message_is_not_evidence", body: `{"error":{"message":"token_revoked workspace_suspended private-body"}}`, class: "handshake_forbidden_unknown", format: "json"},
		{name: "echo_is_not_evidence", body: `{"input":{"error":{"code":"token_revoked","message":"private-body"}}}`, class: "handshake_forbidden_unknown", format: "json"},
		{name: "malformed_is_not_evidence", body: `{"error":{"code":"token_revoked"}} private-body`, class: "handshake_forbidden_unknown", format: "other"},
		{name: "large_unknown", body: strings.Repeat("private-body ", 1024), class: "handshake_forbidden_unknown", format: "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{"Content-Type": []string{tc.contentType}, "Cf-Mitigated": []string{tc.challenge},
				"Set-Cookie": []string{"private-cookie"}, "X-Logdata": []string{"private-logdata"}}
			dial := &openAIWSDialError{StatusCode: http.StatusForbidden, ResponseHeaders: headers, ResponseBody: []byte(tc.body), Err: errors.New("private-cause")}
			wrapped := fmt.Errorf("dial boundary: %w", dial)
			status, class, detail := OpenAIWSHandshakeFailure(wrapped)
			require.Equal(t, http.StatusForbidden, status)
			require.Equal(t, tc.class, class)
			require.Equal(t, tc.class, classifyOpenAIWSDialError(wrapped))
			require.Equal(t, "handshake_forbidden", classifyOpenAIWSAcquireError(wrapped))
			require.LessOrEqual(t, len(detail), 320, "diagnostics must not scale with captured provider text")
			var fields map[string]any
			require.NoError(t, json.Unmarshal([]byte(detail), &fields))
			require.Len(t, fields, 6)
			require.Equal(t, "websocket", fields["upstream_protocol"])
			require.Equal(t, "handshake", fields["failure_stage"])
			require.EqualValues(t, 403, fields["upstream_status"])
			require.Equal(t, tc.class, fields["classification"])
			require.Equal(t, tc.format, fields["body_format"])
			require.EqualValues(t, len(tc.body), fields["captured_body_bytes"])
			for _, secret := range []string{"private-body", "private-cookie", "private-logdata", "private-cause", "secret@example.test"} {
				require.NotContains(t, detail, secret)
			}
			require.Equal(t, tc.requestScoped, dial.RequestScoped())
			attribution := classifyOpenAIAccountScheduleFailure(t.Context(), wrapped)
			if tc.requestScoped {
				require.Equal(t, "request", attribution.reason, "the actual scheduler consumes the trusted scope interface")
			} else {
				require.NotEqual(t, "request", attribution.reason)
			}
			_, replay := classifyOpenAIWSReconnectReason(wrapOpenAIWSFallback(classifyOpenAIWSAcquireError(wrapped), wrapped))
			require.False(t, replay, "403 classification must not introduce an HTTP fallback")
		})
	}
}

func TestNativeWSMaintenance401AndUnknown403RemainDistinct(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			dial := &openAIWSDialError{StatusCode: status, Err: errors.New("upstream handshake rejected")}
			err := svc.mapOpenAIWSPassthroughDialError(dial, status, nil)
			require.Error(t, err)
			var failure *UpstreamFailoverError
			if status == http.StatusUnauthorized {
				require.Equal(t, "auth_failed", classifyOpenAIWSAcquireError(dial))
				require.ErrorAs(t, err, &failure)
				require.Equal(t, GatewayFailureStageAccountAuth, failure.Stage)
				require.True(t, failure.RequestNotSent)
				require.True(t, failure.RejectedBeforeGeneration)
			} else {
				require.Equal(t, "handshake_forbidden", classifyOpenAIWSAcquireError(dial))
				require.False(t, errors.As(err, &failure), "unknown 403 is not proven credential rejection")
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, err, &closeErr)
				require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
				require.Equal(t, "upstream websocket handshake forbidden", closeErr.Reason())
				require.NotContains(t, closeErr.Reason(), "authentication")
				_, class, _ := OpenAIWSHandshakeFailure(err)
				require.Equal(t, "handshake_forbidden_unknown", class)
			}
		})
	}
}

func TestNativeWSMaintenanceRealHandshakeRedactsCapturedData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Cf-Mitigated", "challenge")
		w.Header().Set("Set-Cookie", "private-cookie")
		w.Header().Set("X-Logdata", "private-logdata")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "<html>"+strings.Repeat("private-response-body ", 1024)+"</html>")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	conn, status, headers, dialErr := newDefaultOpenAIWSClientDialer().Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil, "")
	require.Nil(t, conn)
	require.Equal(t, http.StatusForbidden, status)
	var captured *openAIWSHandshakeError
	require.ErrorAs(t, dialErr, &captured)
	require.LessOrEqual(t, len(captured.Body), 8<<10)
	err := (&OpenAIGatewayService{}).mapOpenAIWSPassthroughDialError(dialErr, status, headers)
	actualStatus, class, detail := OpenAIWSHandshakeFailure(err)
	require.Equal(t, http.StatusForbidden, actualStatus)
	require.Equal(t, "handshake_edge_challenge", class)
	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, err, &closeErr)
	core, entries := observer.New(zap.WarnLevel)
	zap.New(core).Warn("handshake", zap.Int("upstream_status", actualStatus), zap.String("classification", class), zap.String("detail", detail), zap.String("close_reason", closeErr.Reason()), zap.Error(err))
	require.Equal(t, 1, entries.Len())
	encoded, marshalErr := json.Marshal(entries.All()[0].ContextMap())
	require.NoError(t, marshalErr)
	for _, secret := range []string{"private-response-body", "private-cookie", "private-logdata", "<html>"} {
		require.NotContains(t, string(encoded), secret)
	}
	var scoped interface{ RequestScoped() bool }
	require.ErrorAs(t, err, &scoped)
	require.True(t, scoped.RequestScoped())
}

type maintenanceProvenanceReadConn struct {
	frames [][]byte
	err    error
	reads  atomic.Int32
}

func (c *maintenanceProvenanceReadConn) Read(context.Context) (coderws.MessageType, []byte, error) {
	i := int(c.reads.Add(1)) - 1
	if i < len(c.frames) {
		return coderws.MessageText, append([]byte(nil), c.frames[i]...), nil
	}
	return 0, nil, c.err
}
func (*maintenanceProvenanceReadConn) Write(context.Context, coderws.MessageType, []byte) error {
	return nil
}
func (*maintenanceProvenanceReadConn) Close(coderws.StatusCode, string) error { return nil }
func (*maintenanceProvenanceReadConn) CloseNow() error                        { return nil }

func TestNativeWSMaintenancePhysicalFirstReadProvenance(t *testing.T) {
	for _, first := range []bool{true, false} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			cause := fmt.Errorf("private-first-frame-logdata: %w", context.DeadlineExceeded)
			conn := &maintenanceProvenanceReadConn{err: cause}
			if !first {
				// A valid first control frame has already crossed the physical read
				// boundary, even though it does not create a generation lane.
				conn.frames = [][]byte{[]byte(`{"type":"response.append","stream_id":"later"}`)}
			}
			err := RunOpenAIWSMultiplexer(openAIWSTestAdmissionContext(t.Context()), conn, nil, func(context.Context, *OpenAIWSDownstream) {
				t.Error("a control-only stream must not dispatch a generation")
			})
			require.ErrorIs(t, err, context.DeadlineExceeded)
			var phase *OpenAIWSFirstMessageError
			require.Equal(t, first, errors.As(err, &phase))
			if first {
				require.Same(t, cause, phase.Err)
				require.Positive(t, phase.Elapsed)
				require.Equal(t, "client first websocket message read failed", phase.Error())
				require.NotContains(t, phase.Error(), "private-first-frame-logdata")
			} else {
				require.Same(t, cause, err, "a later timeout keeps its original phase")
			}
		})
	}
}

func TestNativeWSMaintenanceLaterLaneTimeoutIsNotFirstUpload(t *testing.T) {
	ctx, cancel := context.WithCancel(openAIWSTestAdmissionContext(t.Context()))
	conn := newQueuedWSDownstreamTestConn()
	conn.writes = make(chan []byte, 16)
	done := make(chan error, 1)
	laneError := make(chan error, 1)
	healthy := make(chan struct{}, 1)
	go func() {
		done <- RunOpenAIWSMultiplexer(ctx, conn, nil, func(laneCtx context.Context, ds *OpenAIWSDownstream) {
			_, first, err := ds.Read(laneCtx)
			if err != nil {
				laneError <- err
				return
			}
			lane, _, _ := openAIWSRequestEnvelope(first)
			if lane == "timeout" {
				_, _, err = ReadOpenAIWSClientMessage(laneCtx, ds, 10*time.Millisecond, coderws.StatusPolicyViolation, "lane read timed out")
				laneError <- err
				return
			}
			healthy <- struct{}{}
			<-laneCtx.Done()
		})
	}()
	t.Cleanup(func() {
		cancel()
		_ = conn.CloseNow()
		select {
		case err := <-done:
			var phase *OpenAIWSFirstMessageError
			require.False(t, errors.As(err, &phase), "later connection teardown is not the physical first read")
		case <-time.After(3 * time.Second):
			t.Error("multiplexer failed to join lane workers")
		}
	})
	conn.input <- multiplexFrame("timeout", "one", "")
	select {
	case err := <-laneError:
		require.ErrorIs(t, err, context.DeadlineExceeded)
		var phase *OpenAIWSFirstMessageError
		require.False(t, errors.As(err, &phase))
	case <-time.After(3 * time.Second):
		t.Fatal("lane read did not time out")
	}
	conn.input <- multiplexFrame("healthy", "two", "")
	select {
	case <-healthy:
	case <-time.After(3 * time.Second):
		t.Fatal("a lane timeout closed the physical socket")
	}
}
