package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	coderws "github.com/coder/websocket"
)

const (
	openAICloudWSURL          = "wss://codex-cloud-backend.chatgpt.com/"
	cloudRPCPendingLimit      = 64
	cloudRPCEventLimit        = 256
	cloudRPCEventBytesLimit   = 32 << 20
	cloudRPCOperationTimeout  = 60 * time.Second
	cloudRPCInitializeTimeout = 30 * time.Second
)

var (
	errCloudRPCClosed        = errors.New("Cloud connection closed")
	errCloudRPCProtocol      = errors.New("Cloud sent an invalid RPC message")
	errCloudRPCEventOverflow = errors.New("Cloud event queue exceeded its limit")
	errCloudRPCNotReady      = errors.New("Cloud connection is not initialized")
	cloudProxyClients        = &coderOpenAIWSClientDialer{proxyClients: make(map[string]*openAIWSProxyClientEntry)}
)

// Retain numeric protocol status and allowlisted upstream facts only. Provider
// messages, response headers and dial errors may contain credentials or prompts.
type cloudRPCError struct {
	Code     int
	Upstream *cloudUpstreamError
}

func (e *cloudRPCError) Error() string { return fmt.Sprintf("Cloud RPC rejected (code %d)", e.Code) }

type cloudHandshakeError struct{ StatusCode int }

func (e *cloudHandshakeError) Error() string {
	return fmt.Sprintf("Cloud WebSocket handshake failed (HTTP %d)", e.StatusCode)
}

// ID is present for server requests such as item/tool/call. Pass it unchanged
// to Reply; it is unrelated to the client's monotonically increasing RPC IDs.
type cloudRPCMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	bytes  int
}

type cloudRPCReply struct {
	result json.RawMessage
	err    error
}

// One reader separates replies from ordered notifications/server requests.
// Calls never consume that event stream. The owner must provide a connection
// lifetime context, independent of any individual downstream HTTP request.
// No method in this transport reconnects or replays an RPC.
type cloudRPC struct {
	conn       openAIWSClientConn
	ctx        context.Context
	cancel     context.CancelFunc
	events     chan cloudRPCMessage
	write      *contextMutex
	init       *contextMutex
	mu         sync.Mutex
	ready      bool
	nextID     uint64
	pending    map[string]chan cloudRPCReply
	eventBytes int
	err        error
	closeOnce  sync.Once
}

type cloudWebsocketDial func(context.Context, string, *coderws.DialOptions) (*coderws.Conn, *http.Response, error)

func dialOpenAICloud(ctx context.Context, token, workspace, proxy string) (*cloudRPC, error) {
	return dialOpenAICloudWith(ctx, token, workspace, proxy, coderws.Dial)
}

func dialOpenAICloudWith(ctx context.Context, token, workspace, proxy string, dial cloudWebsocketDial) (*cloudRPC, error) {
	// The token is conveyed in a WebSocket subprotocol, whose grammar is more
	// restrictive than an HTTP header. Reject invalid input without quoting it.
	if len(token) == 0 || len(token) > 32<<10 || strings.IndexFunc(token, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-._~", r))
	}) >= 0 || strings.TrimSpace(workspace) == "" || len(workspace) > 512 || strings.IndexFunc(workspace, func(r rune) bool { return r < 0x21 || r > 0x7e }) >= 0 {
		return nil, errors.New("Cloud authorization is invalid")
	}
	client := &http.Client{}
	if strings.TrimSpace(proxy) != "" {
		shared, err := cloudProxyClients.proxyHTTPClient(proxy)
		if err != nil {
			return nil, errors.New("Cloud proxy configuration is invalid")
		}
		copy := *shared
		client = &copy
	}
	// Sec-WebSocket-Protocol contains a credential. Never forward it through a
	// redirect, including a redirect to another supposedly trusted host.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	dialCtx, cancelDial := context.WithTimeout(ctx, cloudRPCInitializeTimeout)
	defer cancelDial()
	conn, response, err := dial(dialCtx, openAICloudWSURL, &coderws.DialOptions{
		HTTPClient: client,
		HTTPHeader: http.Header{
			"Product-Sku": {"codex"}, "Chatgpt-Account-Id": {workspace},
			"User-Agent": {"sub2api-cloud/0.1"},
		},
		Subprotocols:    []string{"codex-app-server", "codex-client.desktop", "openai-bearer." + token},
		CompressionMode: coderws.CompressionDisabled,
	})
	if err != nil {
		status := 0
		if response != nil {
			status = response.StatusCode
			if response.Body != nil {
				_ = response.Body.Close()
			}
		}
		if conn != nil {
			_ = conn.CloseNow()
		}
		if dialCtx.Err() != nil {
			return nil, dialCtx.Err()
		}
		return nil, &cloudHandshakeError{StatusCode: status}
	}
	if conn == nil {
		return nil, &cloudHandshakeError{}
	}
	if conn.Subprotocol() != "codex-app-server" {
		_ = conn.CloseNow()
		return nil, errors.New("Cloud did not negotiate the app-server protocol")
	}
	conn.SetReadLimit(openAIWSMessageReadLimitBytes)
	rpc := newCloudRPC(ctx, &coderOpenAIWSClientConn{conn: conn})
	if err := rpc.Initialize(dialCtx); err != nil {
		rpc.fail(err)
		return nil, err
	}
	return rpc, nil
}

func newCloudRPC(ctx context.Context, conn openAIWSClientConn) *cloudRPC {
	lifetime, cancel := context.WithCancel(ctx)
	c := &cloudRPC{conn: conn, ctx: lifetime, cancel: cancel, events: make(chan cloudRPCMessage, cloudRPCEventLimit),
		write: newContextMutex(), init: newContextMutex(), pending: make(map[string]chan cloudRPCReply)}
	go c.readLoop()
	go func() { <-lifetime.Done(); c.fail(lifetime.Err()) }()
	return c
}

func (c *cloudRPC) Initialize(ctx context.Context) error {
	if err := c.init.Lock(ctx); err != nil {
		return err
	}
	defer c.init.Unlock()
	if c.isReady() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, cloudRPCInitializeTimeout)
	defer cancel()
	result, err := c.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "sub2api_cloud", "title": "Sub2API Cloud gateway", "version": "0.1"},
		"capabilities": map[string]any{"experimentalApi": true},
	})
	if err == nil && (len(result) == 0 || result[0] != '{') {
		err = errCloudRPCProtocol
	}
	if err == nil {
		err = c.send(ctx, map[string]any{"method": "initialized"})
	}
	if err != nil {
		c.fail(err)
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	c.ready = true
	return nil
}

func (c *cloudRPC) isReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready && c.err == nil && c.ctx.Err() == nil
}

func (c *cloudRPC) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !c.isReady() {
		return nil, c.unreadyError()
	}
	if method == "initialize" || method == "initialized" {
		return nil, errors.New("Cloud initialization is already complete")
	}
	return c.call(ctx, method, params)
}

func (c *cloudRPC) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method == "" || len(method) > 256 {
		return nil, errors.New("Cloud RPC method is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, cloudRPCOperationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan cloudRPCReply, 1)
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return nil, err
	}
	if len(c.pending) >= cloudRPCPendingLimit {
		c.mu.Unlock()
		return nil, errors.New("Cloud has too many pending RPCs")
	}
	c.nextID++
	id := c.nextID
	key := strconv.FormatUint(id, 10)
	c.pending[key] = ch
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if err := c.send(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		return reply.result, reply.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, c.Err()
	}
}

func (c *cloudRPC) Notify(ctx context.Context, method string, params any) error {
	if !c.isReady() {
		return c.unreadyError()
	}
	if method == "" || len(method) > 256 || method == "initialized" || method == "initialize" {
		return errors.New("Cloud notification method is invalid")
	}
	return c.send(ctx, map[string]any{"method": method, "params": params})
}

func (c *cloudRPC) Reply(ctx context.Context, id json.RawMessage, result any) error {
	if !c.isReady() {
		return c.unreadyError()
	}
	if _, ok := cloudRPCIDKey(id); !ok {
		return errors.New("Cloud server request ID is invalid")
	}
	return c.send(ctx, map[string]any{"id": id, "result": result})
}

func (c *cloudRPC) send(ctx context.Context, value any) error {
	ctx, cancel := context.WithTimeout(ctx, cloudRPCOperationTimeout)
	defer cancel()
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	raw, err := json.Marshal(value)
	if err != nil {
		return errors.New("Cloud RPC payload cannot be encoded")
	}
	if len(raw) > int(openAIWSMessageReadLimitBytes) {
		return errors.New("Cloud RPC payload exceeded its limit")
	}
	if err := c.write.Lock(ctx); err != nil {
		return err
	}
	defer c.write.Unlock()
	if err := c.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.conn.WriteJSON(ctx, json.RawMessage(raw)); err != nil {
		safe := errors.New("Cloud RPC write failed; acceptance is unknown")
		c.fail(safe)
		return safe
	}
	return nil
}

// Next preserves event order and accounts for queued bytes as they leave the
// bounded channel. After a transport failure queued output must not be treated
// as proof of a completed turn; the caller gets the failure instead.
func (c *cloudRPC) Next(ctx context.Context) (cloudRPCMessage, error) {
	select {
	case event := <-c.events:
		c.mu.Lock()
		c.eventBytes -= event.bytes
		err := c.err
		if err == nil {
			err = c.ctx.Err()
		}
		c.mu.Unlock()
		if err != nil {
			return cloudRPCMessage{}, err
		}
		return event, nil
	case <-ctx.Done():
		return cloudRPCMessage{}, ctx.Err()
	case <-c.ctx.Done():
		return cloudRPCMessage{}, c.Err()
	}
}

func (c *cloudRPC) Done() <-chan struct{} { return c.ctx.Done() }
func (c *cloudRPC) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return c.ctx.Err()
}
func (c *cloudRPC) unreadyError() error {
	if err := c.Err(); err != nil {
		return err
	}
	return errCloudRPCNotReady
}
func (c *cloudRPC) Close() error { c.fail(errCloudRPCClosed); return nil }

func (c *cloudRPC) fail(err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		c.ready = false
		c.pending = make(map[string]chan cloudRPCReply)
	drain:
		for {
			select {
			case event := <-c.events:
				c.eventBytes -= event.bytes
			default:
				break drain
			}
		}
		c.mu.Unlock()
		c.cancel()
		if closer, ok := c.conn.(openAIWSForceCloser); ok {
			_ = closer.CloseNow()
		} else {
			_ = c.conn.Close()
		}
	})
}

func cloudRPCIDKey(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 4096 {
		return "", false
	}
	if raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return "", false
		}
		return "s:" + value, true
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return strconv.FormatInt(n, 10), err == nil
}

func (c *cloudRPC) readLoop() {
	for {
		raw, err := c.conn.ReadMessage(c.ctx)
		if err != nil {
			if c.ctx.Err() != nil {
				c.fail(c.ctx.Err())
			} else {
				c.fail(errCloudRPCClosed)
			}
			return
		}
		if len(raw) > int(openAIWSMessageReadLimitBytes) {
			c.fail(errCloudRPCProtocol)
			return
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(raw, &message) != nil {
			c.fail(errCloudRPCProtocol)
			return
		}
		if message.Method != "" {
			if len(message.Method) > 256 || len(message.Result) != 0 || len(message.Error) != 0 {
				c.fail(errCloudRPCProtocol)
				return
			}
			if len(message.ID) > 0 {
				if _, ok := cloudRPCIDKey(message.ID); !ok {
					c.fail(errCloudRPCProtocol)
					return
				}
			}
			event := cloudRPCMessage{ID: message.ID, Method: message.Method, Params: message.Params, bytes: len(raw)}
			c.mu.Lock()
			queued := c.err == nil && c.eventBytes+event.bytes <= cloudRPCEventBytesLimit
			if queued {
				select {
				case c.events <- event:
					c.eventBytes += event.bytes
				default:
					queued = false
				}
			}
			c.mu.Unlock()
			if !queued {
				c.fail(errCloudRPCEventOverflow)
				return
			}
			continue
		}
		key, ok := cloudRPCIDKey(message.ID)
		if !ok || (len(message.Result) == 0) == (len(message.Error) == 0) {
			c.fail(errCloudRPCProtocol)
			return
		}
		reply := cloudRPCReply{result: message.Result}
		if len(message.Error) != 0 {
			var rpcErr struct {
				Code *int `json:"code"`
			}
			if json.Unmarshal(message.Error, &rpcErr) != nil || rpcErr.Code == nil {
				c.fail(errCloudRPCProtocol)
				return
			}
			reply.err = &cloudRPCError{Code: *rpcErr.Code, Upstream: cloudParseTurnError(message.Error)}
		}
		c.mu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.mu.Unlock()
		// A timed-out call may reply late. Never replay it or let that reply be
		// mistaken for a notification or another caller's result.
		if ch != nil {
			ch <- reply
		}
	}
}
