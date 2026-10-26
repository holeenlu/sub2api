package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type cloudRPCFakeConn struct {
	reads  chan []byte
	writes chan []byte
	closed chan struct{}
	once   sync.Once
}

func newCloudRPCFakeConn() *cloudRPCFakeConn {
	return &cloudRPCFakeConn{reads: make(chan []byte, 512), writes: make(chan []byte, 512), closed: make(chan struct{})}
}
func (c *cloudRPCFakeConn) WriteJSON(ctx context.Context, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	select {
	case c.writes <- raw:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return errors.New("openai-bearer.fake-secret")
	}
}
func (c *cloudRPCFakeConn) ReadMessage(ctx context.Context) ([]byte, error) {
	select {
	case raw := <-c.reads:
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, errors.New("openai-bearer.fake-secret")
	}
}
func (c *cloudRPCFakeConn) Ping(context.Context) error { return nil }
func (c *cloudRPCFakeConn) Close() error               { c.once.Do(func() { close(c.closed) }); return nil }
func (c *cloudRPCFakeConn) CloseNow() error            { return c.Close() }

func cloudTestMessage(t *testing.T, ctx context.Context, c *cloudRPCFakeConn) cloudRPCMessage {
	t.Helper()
	select {
	case raw := <-c.writes:
		var msg cloudRPCMessage
		require.NoError(t, json.Unmarshal(raw, &msg))
		return msg
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return cloudRPCMessage{}
	}
}

func cloudTestRead(c *cloudRPCFakeConn, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	c.reads <- raw
}

func cloudTestInitialize(t *testing.T, ctx context.Context, c *cloudRPC, wire *cloudRPCFakeConn) {
	t.Helper()
	initialized := make(chan error, 1)
	go func() { initialized <- c.Initialize(ctx) }()
	msg := cloudTestMessage(t, ctx, wire)
	require.Equal(t, "initialize", msg.Method)
	cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"userAgent": "cloud-test"}})
	msg = cloudTestMessage(t, ctx, wire)
	require.Equal(t, "initialized", msg.Method)
	require.Empty(t, msg.ID)
	select {
	case err := <-initialized:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudRPCOutOfOrderRepliesAndServerRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	type outcome struct {
		method string
		raw    json.RawMessage
		err    error
	}
	results := make(chan outcome, 2)
	for _, method := range []string{"model/list", "thread/start"} {
		go func() { raw, err := c.Call(ctx, method, map[string]any{}); results <- outcome{method, raw, err} }()
	}
	first, second := cloudTestMessage(t, ctx, wire), cloudTestMessage(t, ctx, wire)
	cloudTestRead(wire, map[string]any{"method": "thread/started", "params": map[string]any{"thread": map[string]any{"id": "thread-1"}}})
	cloudTestRead(wire, map[string]any{"id": "tool-rpc-1", "method": "item/tool/call", "params": map[string]any{"callId": "call-1", "tool": "echo"}})
	for _, msg := range []cloudRPCMessage{second, first} {
		cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"method": msg.Method}})
	}
	// Both replies complete before the event consumer begins reading. Waiting
	// for a response must neither block on nor steal asynchronous events.
	for range 2 {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(result.raw, &body))
			require.Equal(t, result.method, body["method"])
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	event, err := c.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, "thread/started", event.Method)
	event, err = c.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, "item/tool/call", event.Method)
	require.JSONEq(t, `"tool-rpc-1"`, string(event.ID))
	c.mu.Lock()
	queuedBytes := c.eventBytes
	c.mu.Unlock()
	require.Zero(t, queuedBytes)
	require.NoError(t, c.Reply(ctx, event.ID, map[string]any{"contentItems": []any{map[string]any{"type": "inputText", "text": "ok"}}, "success": true}))
	select {
	case raw := <-wire.writes:
		require.JSONEq(t, `{"id":"tool-rpc-1","result":{"contentItems":[{"type":"inputText","text":"ok"}],"success":true}}`, string(raw))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudRPCShutdownWakesPendingCallsAndEvents(t *testing.T) {
	for _, action := range []string{"close", "parent_cancel", "upstream_close"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			lifetime, cancelLifetime := context.WithCancel(ctx)
			defer cancelLifetime()
			wire := newCloudRPCFakeConn()
			c := newCloudRPC(lifetime, wire)
			defer c.Close()
			cloudTestInitialize(t, ctx, c, wire)
			callResult, eventResult := make(chan error, 1), make(chan error, 1)
			go func() { _, err := c.Call(ctx, "thread/start", map[string]any{}); callResult <- err }()
			cloudTestMessage(t, ctx, wire)
			go func() { _, err := c.Next(ctx); eventResult <- err }()
			switch action {
			case "close":
				_ = c.Close()
			case "parent_cancel":
				cancelLifetime()
			case "upstream_close":
				_ = wire.Close()
			}
			for _, result := range []<-chan error{callResult, eventResult} {
				select {
				case err := <-result:
					require.Error(t, err)
					require.NotContains(t, err.Error(), "fake-secret")
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			select {
			case <-c.Done():
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Error(t, c.Err())
		})
	}
}

func TestCloudRPCInitializeFailureNeverSendsInitialized(t *testing.T) {
	for name, response := range map[string]string{"rpc_error": `{"error":{"code":-32600,"message":"openai-bearer.fake-secret","data":{"token":"fake-secret"}}}`, "invalid_result": `{"result":null}`} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wire := newCloudRPCFakeConn()
			c := newCloudRPC(ctx, wire)
			defer c.Close()
			_, err := c.Call(ctx, "thread/start", nil)
			require.ErrorIs(t, err, errCloudRPCNotReady)
			result := make(chan error, 1)
			go func() { result <- c.Initialize(ctx) }()
			msg := cloudTestMessage(t, ctx, wire)
			var payload map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(response), &payload))
			payload["id"] = msg.ID
			cloudTestRead(wire, payload)
			select {
			case err := <-result:
				require.Error(t, err)
				require.NotContains(t, fmt.Sprintf("%+v", err), "fake-secret")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case raw := <-wire.writes:
				t.Fatalf("unexpected post-failure write: %s", raw)
			default:
			}
		})
	}
}

func TestCloudRPCStructuredErrorKeepsOnlySafeProtocolFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "thread/start", nil); result <- err }()
	msg := cloudTestMessage(t, ctx, wire)
	cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{
		"code": -32000, "message": "openai-bearer.fake-secret", "data": map[string]any{
			"codexErrorInfo": "rateLimitExceeded", "httpStatusCode": 429,
			"token": "fake-secret", "prompt": "private prompt",
		},
	}})
	select {
	case err := <-result:
		var rpcErr *cloudRPCError
		require.ErrorAs(t, err, &rpcErr)
		require.Equal(t, -32000, rpcErr.Code)
		require.Equal(t, "rateLimitExceeded", rpcErr.Upstream.CodexErrorInfo)
		require.Equal(t, 429, rpcErr.Upstream.HTTPStatus)
		actor := &cloudSessionActor{}
		actor.setPublicFailure(err)
		require.Equal(t, "thread/start", actor.publicFailure["upstream_method"])
		require.Equal(t, "rateLimitExceeded", actor.publicFailure["upstream_error_info"])
		require.Equal(t, 429, actor.publicFailure["upstream_http_status"])

		status, code, _, ok := cloudPublicError(err)
		require.True(t, ok)
		require.Equal(t, 429, status)
		require.Equal(t, "rate_limit_exceeded", code)
		require.NotContains(t, fmt.Sprintf("%+v %#v", err, rpcErr.Upstream), "fake-secret")
		require.NotContains(t, fmt.Sprintf("%+v %#v", err, rpcErr.Upstream), "private prompt")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudRPCMissingThreadRedactsUpstreamIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "thread/archive", nil); result <- err }()
	msg := cloudTestMessage(t, ctx, wire)
	const threadID = "b41148ea-7c38-4ae6-8061-74767495e649"
	cloudTestRead(wire, map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "message": "unknown thread: " + threadID}})
	select {
	case err := <-result:
		var rpcErr *cloudRPCError
		require.ErrorAs(t, err, &rpcErr)
		require.Equal(t, -32600, rpcErr.Code)
		status, code, message, ok := cloudPublicError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusBadGateway, status)
		require.Equal(t, "cloud_thread_unavailable", code)
		require.NotContains(t, message, threadID)
		require.NotContains(t, fmt.Sprintf("%+v %#v", err, rpcErr.Upstream), threadID)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudRPCTimedOutReplyIsNotReplayedOrMisdelivered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	callCtx, cancelCall := context.WithCancel(ctx)
	firstResult := make(chan error, 1)
	go func() { _, err := c.Call(callCtx, "turn/start", nil); firstResult <- err }()
	first := cloudTestMessage(t, ctx, wire)
	cancelCall()
	select {
	case err := <-firstResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cloudTestRead(wire, map[string]any{"id": first.ID, "result": map[string]any{"turn": "old"}})
	secondResult := make(chan cloudRPCReply, 1)
	go func() {
		raw, err := c.Call(ctx, "thread/read", nil)
		secondResult <- cloudRPCReply{result: raw, err: err}
	}()
	second := cloudTestMessage(t, ctx, wire)
	require.Equal(t, "thread/read", second.Method)
	require.NotEqual(t, string(first.ID), string(second.ID))
	cloudTestRead(wire, map[string]any{"id": second.ID, "result": map[string]any{"turn": "new"}})
	select {
	case result := <-secondResult:
		require.NoError(t, result.err)
		require.JSONEq(t, `{"turn":"new"}`, string(result.result))
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case raw := <-wire.writes:
		t.Fatalf("unexpected replay: %s", raw)
	default:
	}
}

func TestCloudRPCBurstKeepsReplyAndEventOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "turn/start", nil); result <- err }()
	request := cloudTestMessage(t, ctx, wire)
	for i := range 400 {
		cloudTestRead(wire, map[string]any{"method": "rawResponseItem/completed", "params": map[string]any{"threadId": "thread", "turnId": "auto-compact-0", "item": map[string]any{"type": "message", "id": fmt.Sprint(i)}}})
	}
	// Live output and a server RPC can also precede turn/start acknowledgement.
	cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"threadId": "thread", "turnId": "live", "delta": "ok"}})
	cloudTestRead(wire, map[string]any{"id": "server-call", "method": "item/tool/call", "params": map[string]any{"threadId": "thread", "turnId": "live"}})
	cloudTestRead(wire, map[string]any{"id": request.ID, "result": map[string]any{"turn": map[string]any{"id": "live"}}})
	select {
	case err := <-result:
		require.NoError(t, err, "an undrained history burst must not block its RPC reply")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for i := range 400 {
		event, err := c.Next(ctx)
		require.NoError(t, err)
		require.Equal(t, "rawResponseItem/completed", event.Method)
		require.Contains(t, string(event.Params), `"id":"`+fmt.Sprint(i)+`"`)
	}
	event, err := c.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, "item/agentMessage/delta", event.Method)
	event, err = c.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, "item/tool/call", event.Method)
	require.JSONEq(t, `"server-call"`, string(event.ID))
	c.mu.Lock()
	queuedBytes, pendingCount := c.eventBytes, len(c.pending)
	c.mu.Unlock()
	require.Zero(t, queuedBytes)
	require.Zero(t, pendingCount)
}

func TestCloudRPCEventByteLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	// Three individually valid frames exceed the total queue byte budget.
	// The reader must still wake pending RPCs and release queued data.
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "turn/start", nil); result <- err }()
	cloudTestMessage(t, ctx, wire)
	raw := []byte(`{"method":"item/agentMessage/delta","params":{"delta":"` + strings.Repeat("x", int(OpenAICloudMaxRPCBytes)-100) + `"}}`)
	require.Less(t, len(raw), int(OpenAICloudMaxRPCBytes))
	for range 3 {
		wire.reads <- raw
	}
	select {
	case <-c.Done():
		require.ErrorIs(t, c.Err(), errCloudRPCEventOverflow)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case err := <-result:
		require.ErrorIs(t, err, errCloudRPCEventOverflow)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	_, err := c.Next(ctx)
	require.ErrorIs(t, err, errCloudRPCEventOverflow)
	c.mu.Lock()
	queuedBytes, pendingCount := c.eventBytes, len(c.pending)
	c.mu.Unlock()
	require.Zero(t, queuedBytes)
	require.Zero(t, pendingCount)
}

func TestCloudRPCRejectsOversizedIncomingMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	wire.reads <- make([]byte, int(OpenAICloudMaxRPCBytes)+1)
	select {
	case <-c.Done():
		require.ErrorIs(t, c.Err(), errCloudRPCProtocol)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudDialFixedHandshakeDoesNotExposeCredentials(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	called := false
	rpc, err := dialOpenAICloudWith(ctx, "fake-secret", "workspace-test", "", func(_ context.Context, target string, opts *coderws.DialOptions) (*coderws.Conn, *http.Response, error) {
		called = true
		require.Equal(t, openAICloudWSURL, target)
		require.Equal(t, []string{"codex-app-server", "codex-client.desktop", "openai-bearer.fake-secret"}, opts.Subprotocols)
		require.Equal(t, "codex", opts.HTTPHeader.Get("product-sku"))
		require.Equal(t, "workspace-test", opts.HTTPHeader.Get("chatgpt-account-id"))
		require.Equal(t, "sub2api-cloud/0.1", opts.HTTPHeader.Get("user-agent"))
		require.ErrorIs(t, opts.HTTPClient.CheckRedirect(&http.Request{}, nil), http.ErrUseLastResponse)
		return nil, &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Sec-Websocket-Protocol": {"openai-bearer.fake-secret"}}, Body: io.NopCloser(strings.NewReader("fake-secret"))}, errors.New("failed to negotiate subprotocol openai-bearer.fake-secret")
	})
	require.True(t, called)
	require.Nil(t, rpc)
	var handshake *cloudHandshakeError
	require.ErrorAs(t, err, &handshake)
	require.Equal(t, http.StatusUnauthorized, handshake.StatusCode)
	require.NotContains(t, fmt.Sprintf("%+v", err), "fake-secret")
	require.Nil(t, errors.Unwrap(err))
}

func TestCloudModelCatalogPagination(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(fmt.Sprint(cycle), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			wire := newCloudRPCFakeConn()
			rpc := newCloudRPC(ctx, wire)
			defer rpc.Close()
			cloudTestInitialize(t, ctx, rpc, wire)
			done := make(chan struct{})
			go func() {
				defer close(done)
				msg := cloudTestMessage(t, ctx, wire)
				require.Equal(t, "model/list", msg.Method)
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"data": []any{map[string]any{"model": "first"}}, "nextCursor": "page2"}})
				msg = cloudTestMessage(t, ctx, wire)
				require.Contains(t, string(msg.Params), `"cursor":"page2"`)
				result := map[string]any{"data": []any{map[string]any{"model": "first"}, map[string]any{"model": "second"}}}
				if cycle {
					result["nextCursor"] = "page2"
				}
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": result})
			}()
			models, err := readCloudModelCatalog(ctx, rpc)
			if cycle {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, []string{"first", "second"}, models)
			}
			<-done
		})
	}
}

func TestCloudModelCatalogRejectsMalformedEnvelopes(t *testing.T) {
	for _, raw := range []string{`{}`, `{"data":null}`, `{"data":{}}`, `{"data":[{}]}`, `{"data":[{"model":""}]}`} {
		t.Run(raw, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			wire := newCloudRPCFakeConn()
			rpc := newCloudRPC(ctx, wire)
			defer rpc.Close()
			cloudTestInitialize(t, ctx, rpc, wire)
			go func() {
				msg := cloudTestMessage(t, ctx, wire)
				cloudTestRead(wire, map[string]any{"id": msg.ID, "result": json.RawMessage(raw)})
			}()
			models, err := readCloudModelCatalog(ctx, rpc)
			require.Error(t, err)
			require.Nil(t, models)
		})
	}
}

type cloudPingFailConn struct {
	*cloudRPCFakeConn
	pings chan struct{}
}

func (c *cloudPingFailConn) Ping(ctx context.Context) error {
	select {
	case c.pings <- struct{}{}:
	default:
	}
	<-ctx.Done() // a half-open peer never answers
	return ctx.Err()
}

// A silently dropped connection must be detected by its own heartbeat instead
// of leaving a pending tool wait or idle session attached to a dead socket.
func TestCloudRPCHeartbeatDetectsHalfOpenConnection(t *testing.T) {
	interval, timeout := cloudRPCPingInterval, cloudRPCPingTimeout
	cloudRPCPingInterval, cloudRPCPingTimeout = 20*time.Millisecond, 30*time.Millisecond
	t.Cleanup(func() { cloudRPCPingInterval, cloudRPCPingTimeout = interval, timeout })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn := &cloudPingFailConn{cloudRPCFakeConn: newCloudRPCFakeConn(), pings: make(chan struct{}, 1)}
	rpc := newCloudRPC(ctx, conn)
	defer rpc.Close()
	select {
	case <-conn.pings:
	case <-ctx.Done():
		t.Fatal("no heartbeat was sent")
	}
	_, err := rpc.Next(ctx)
	require.ErrorIs(t, err, errCloudRPCClosed)
}

// A healthy peer that answers pings keeps the connection alive.
func TestCloudRPCHeartbeatKeepsHealthyConnection(t *testing.T) {
	interval, timeout := cloudRPCPingInterval, cloudRPCPingTimeout
	cloudRPCPingInterval, cloudRPCPingTimeout = 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { cloudRPCPingInterval, cloudRPCPingTimeout = interval, timeout })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, conn)
	defer rpc.Close()
	time.Sleep(100 * time.Millisecond)
	conn.reads <- []byte(`{"method":"note","params":{}}`)
	event, err := rpc.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, "note", event.Method)
}

func TestCloudDialUsesNativeDefaultDeadline(t *testing.T) {
	var remaining time.Duration
	_, err := dialOpenAICloudWith(t.Context(), "fake-secret", "workspace-test", "", func(ctx context.Context, _ string, _ *coderws.DialOptions) (*coderws.Conn, *http.Response, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		remaining = time.Until(deadline)
		return nil, nil, errors.New("isolated dial refusal")
	})
	require.Error(t, err)
	var native *OpenAIGatewayService
	require.InDelta(t, native.openAIWSDialTimeout().Seconds(), remaining.Seconds(), 0.5)
}

// Observe the contexts delivered to the real RPC send path without waiting for
// production-length timeouts or introducing a second transport implementation.
type cloudRPCDeadlineConn struct {
	*cloudRPCFakeConn
	writeContexts chan context.Context
}

func (c *cloudRPCDeadlineConn) WriteJSON(ctx context.Context, value any) error {
	c.writeContexts <- ctx
	return c.cloudRPCFakeConn.WriteJSON(ctx, value)
}

func TestCloudRPCUsesNativeConfiguredTimeouts(t *testing.T) {
	gateway := &OpenAIGatewayService{cfg: &config.Config{}}
	gateway.cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	gateway.cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	gateway.cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 7
	ctx := withCloudRPCTimeouts(t.Context(), gateway)
	_, err := dialOpenAICloudWith(ctx, "fake-secret", "workspace-test", "", func(callCtx context.Context, _ string, _ *coderws.DialOptions) (*coderws.Conn, *http.Response, error) {
		deadline, ok := callCtx.Deadline()
		require.True(t, ok)
		require.InDelta(t, gateway.openAIWSDialTimeout().Seconds(), time.Until(deadline).Seconds(), 0.5)
		return nil, nil, errors.New("isolated dial refusal")
	})
	require.Error(t, err)
	wire := &cloudRPCDeadlineConn{cloudRPCFakeConn: newCloudRPCFakeConn(), writeContexts: make(chan context.Context, 8)}
	rpc := newCloudRPC(ctx, wire)
	defer rpc.Close()
	testCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cloudTestInitialize(t, testCtx, rpc, wire.cloudRPCFakeConn)
	for range 2 {
		<-wire.writeContexts
	}
	called := make(chan error, 1)
	go func() {
		_, err := rpc.Call(ctx, "model/list", map[string]any{})
		called <- err
	}()
	message := cloudTestMessage(t, testCtx, wire.cloudRPCFakeConn)
	callCtx := <-wire.writeContexts
	deadline, ok := callCtx.Deadline()
	require.True(t, ok)
	require.InDelta(t, gateway.openAIWSReadTimeout().Seconds(), time.Until(deadline).Seconds(), 0.5, "the RPC response budget bounds its write")
	cloudTestRead(wire.cloudRPCFakeConn, map[string]any{"id": message.ID, "result": map[string]any{}})
	require.NoError(t, <-called)
	require.NoError(t, rpc.Notify(ctx, "notification", nil))
	writeCtx := <-wire.writeContexts
	deadline, ok = writeCtx.Deadline()
	require.True(t, ok)
	require.InDelta(t, gateway.openAIWSWriteTimeout().Seconds(), time.Until(deadline).Seconds(), 0.5)
	shorter, cancelShorter := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancelShorter()
	require.NoError(t, rpc.Notify(shorter, "notification", nil))
	writeCtx = <-wire.writeContexts
	deadline, ok = writeCtx.Deadline()
	require.True(t, ok)
	expected, _ := shorter.Deadline()
	require.Equal(t, expected, deadline, "the original request budget takes priority")
}

func TestCloudRPCInitializationOperationDoesNotOwnSocket(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(t.Context())
	defer cancelLifetime()
	operation, cancelOperation := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancelOperation()
	operation = context.WithValue(operation, cloudRPCLifetimeKey{}, lifetime)
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(operation, wire)
	defer rpc.Close()
	cloudTestInitialize(t, operation, rpc, wire)
	cancelOperation()
	require.NoError(t, rpc.ctx.Err(), "finishing initialization cannot close a persistent tool session")
	require.NoError(t, rpc.Notify(t.Context(), "notification", nil))
	cancelLifetime()
	select {
	case <-wire.closed:
	case <-time.After(time.Second):
		t.Fatal("actor lifetime cancellation did not close its socket")
	}
	require.ErrorIs(t, rpc.ctx.Err(), context.Canceled)
}

func TestCloudRPCInitializationCancellationClosesUnreadySocket(t *testing.T) {
	lifetime, cancelLifetime := context.WithCancel(t.Context())
	defer cancelLifetime()
	operation, cancelOperation := context.WithCancel(t.Context())
	defer cancelOperation()
	operation = context.WithValue(operation, cloudRPCLifetimeKey{}, lifetime)
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(operation, wire)
	defer rpc.Close()
	initialized := make(chan error, 1)
	go func() { initialized <- rpc.Initialize(operation) }()
	wait, cancelWait := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelWait()
	message := cloudTestMessage(t, wait, wire)
	require.Equal(t, "initialize", message.Method)
	cancelOperation()
	select {
	case err := <-initialized:
		require.ErrorIs(t, err, context.Canceled)
	case <-wait.Done():
		t.Fatal("initialization ignored cancellation")
	}
	require.Empty(t, wire.writes, "cancelled initialization must not send initialized or execute a turn")
	select {
	case <-wire.closed:
	case <-wait.Done():
		t.Fatal("the uninitialized socket was left open")
	}
	require.NoError(t, lifetime.Err(), "failed preparation must not cancel its owner's context")
}

// An EOF marker is consumed in read order, unlike closing the fake's separate
// closed channel, which may race with unread frames in ReadMessage's select.
type cloudOrderedEOFConn struct{ *cloudRPCFakeConn }

func (c *cloudOrderedEOFConn) ReadMessage(ctx context.Context) ([]byte, error) {
	raw, err := c.cloudRPCFakeConn.ReadMessage(ctx)
	if err == nil && raw == nil {
		return nil, io.EOF
	}
	return raw, err
}

func cloudWaitDisconnected(t *testing.T, ctx context.Context, rpc *cloudRPC) {
	t.Helper()
	select {
	case <-rpc.Done():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudRPCPeerCloseDrainsReceivedFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	rpc := newCloudRPC(ctx, &cloudOrderedEOFConn{wire})
	defer rpc.Close()
	cloudTestInitialize(t, ctx, rpc, wire)
	for _, method := range []string{"thread/tokenUsage/updated", "turn/completed"} {
		cloudTestRead(wire, map[string]any{"method": method, "params": map[string]any{}})
	}
	wire.reads <- nil
	cloudWaitDisconnected(t, ctx, rpc)
	require.False(t, rpc.isReady(), "a drainable connection cannot be reused")
	require.Error(t, rpc.Reply(ctx, json.RawMessage(`"tool"`), map[string]any{}))
	for _, method := range []string{"thread/tokenUsage/updated", "turn/completed"} {
		event, err := rpc.Next(ctx)
		require.NoError(t, err)
		require.Equal(t, method, event.Method)
	}
	_, err := rpc.Next(ctx)
	require.ErrorIs(t, err, errCloudRPCClosed)
	rpc.mu.Lock()
	require.Zero(t, rpc.eventBytes)
	require.Nil(t, rpc.eventHead)
	require.Nil(t, rpc.eventTail)
	rpc.mu.Unlock()
}

func TestCloudRPCHardFailureDiscardsReceivedFacts(t *testing.T) {
	for _, action := range []string{"close_after_eof", "cancel_after_eof", "protocol_after_event"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			lifetime, stop := context.WithCancel(ctx)
			defer stop()
			wire := newCloudRPCFakeConn()
			rpc := newCloudRPC(lifetime, &cloudOrderedEOFConn{wire})
			defer rpc.Close()
			cloudTestInitialize(t, ctx, rpc, wire)
			cloudTestRead(wire, map[string]any{"method": "turn/completed", "params": map[string]any{}})
			if action == "protocol_after_event" {
				wire.reads <- []byte(`{"invalid"`)
			} else {
				wire.reads <- nil
			}
			cloudWaitDisconnected(t, ctx, rpc)
			switch action {
			case "close_after_eof":
				require.NoError(t, rpc.Close())
			case "cancel_after_eof":
				stop()
			}
			_, err := rpc.Next(ctx)
			require.Error(t, err)
			require.Error(t, rpc.eventError(), "already forwarded actor events must also be invalidated")
			rpc.mu.Lock()
			require.Nil(t, rpc.eventHead)
			require.Zero(t, rpc.eventBytes)
			rpc.mu.Unlock()
		})
	}
}

// Hold the call before its reply select until BOTH the reply and EOF are ready.
// This exercises the race without assuming the scheduler delays the caller.
type cloudReplyBarrierConn struct {
	*cloudOrderedEOFConn
	resume <-chan struct{}
}

func (c *cloudReplyBarrierConn) WriteJSON(ctx context.Context, value any) error {
	if err := c.cloudOrderedEOFConn.WriteJSON(ctx, value); err != nil {
		return err
	}
	raw, _ := json.Marshal(value)
	var message cloudRPCMessage
	_ = json.Unmarshal(raw, &message)
	if message.Method == "turn/start" {
		<-c.resume
	}
	return nil
}

func TestCloudRPCConfirmedReplySurvivesPeerClose(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%v", cancelled), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			wire := newCloudRPCFakeConn()
			resume := make(chan struct{})
			var release sync.Once
			defer release.Do(func() { close(resume) })
			rpc := newCloudRPC(ctx, &cloudReplyBarrierConn{&cloudOrderedEOFConn{wire}, resume})
			defer rpc.Close()
			cloudTestInitialize(t, ctx, rpc, wire)
			callCtx, stop := context.WithCancel(ctx)
			defer stop()
			result := make(chan cloudRPCReply, 1)
			go func() {
				raw, err := rpc.Call(callCtx, "turn/start", map[string]any{})
				result <- cloudRPCReply{result: raw, err: err}
			}()
			msg := cloudTestMessage(t, ctx, wire)
			cloudTestRead(wire, map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": "confirmed"}}})
			wire.reads <- nil
			cloudWaitDisconnected(t, ctx, rpc)
			if cancelled {
				stop()
			}
			release.Do(func() { close(resume) })
			select {
			case reply := <-result:
				if cancelled {
					require.ErrorIs(t, reply.err, context.Canceled)
				} else {
					require.NoError(t, reply.err)
					require.JSONEq(t, `{"turn":{"id":"confirmed"}}`, string(reply.result))
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Empty(t, wire.writes, "the confirmed call must never be replayed")
		})
	}
}

func TestCloudRPCLifetimeCancellationClosesSocketWithoutConsumer(t *testing.T) {
	for range 8 {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		lifetime, stop := context.WithCancel(ctx)
		wire := newCloudRPCFakeConn()
		rpc := newCloudRPC(lifetime, wire)
		cloudTestInitialize(t, ctx, rpc, wire)
		stop()
		select {
		case <-wire.closed:
		case <-ctx.Done():
			t.Error("lifetime cancellation did not release the physical socket")
		}
		rpc.Close()
		cancel()
	}
}
