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
	go func() { raw, err := c.Call(ctx, "thread/read", nil); secondResult <- cloudRPCReply{raw, err} }()
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

func TestCloudRPCOverflowFailsPendingCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	result := make(chan error, 1)
	go func() { _, err := c.Call(ctx, "turn/start", nil); result <- err }()
	cloudTestMessage(t, ctx, wire)
	for range cloudRPCEventLimit + 1 {
		cloudTestRead(wire, map[string]any{"method": "item/agentMessage/delta", "params": map[string]any{"delta": "x"}})
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

func TestCloudRPCEventByteLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	cloudTestInitialize(t, ctx, c, wire)
	// Three individually valid frames exceed the cumulative byte budget well
	// before the 256-event count limit. Reuse input bytes instead of retaining
	// three unnecessary copies in the test fixture.
	raw := []byte(`{"method":"item/agentMessage/delta","params":{"delta":"` + strings.Repeat("x", int(openAIWSMessageReadLimitBytes)-100) + `"}}`)
	require.Less(t, len(raw), int(openAIWSMessageReadLimitBytes))
	for range 3 {
		wire.reads <- raw
	}
	select {
	case <-c.Done():
		require.ErrorIs(t, c.Err(), errCloudRPCEventOverflow)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestCloudRPCRejectsOversizedIncomingMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wire := newCloudRPCFakeConn()
	c := newCloudRPC(ctx, wire)
	defer c.Close()
	wire.reads <- make([]byte, int(openAIWSMessageReadLimitBytes)+1)
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
