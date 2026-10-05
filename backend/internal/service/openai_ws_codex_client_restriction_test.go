package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const codexCLIOnlyTestOfficialUA = "codex_cli_rs/0.141.0 (x) (codex_cli_rs; 0.141.0)"

// codexCLIOnlyWSTestSetup builds one codex_cli_only account per ingress path:
// native ctx_pool and passthrough upstream sockets.
type codexCLIOnlyWSTestSetup struct {
	svc     *OpenAIGatewayService
	account *Account
	native  *stagedPassthroughConn
	http    *httpUpstreamRecorder
}

func newCodexCLIOnlyWSTestSetup(t *testing.T, mode string) codexCLIOnlyWSTestSetup {
	t.Helper()

	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(cfg, upstream)
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
	svc.openaiWSPool = pool
	account := nativeWSTestAccount()
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
	account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
	account.Extra["codex_cli_only"] = true
	return codexCLIOnlyWSTestSetup{svc: svc, account: account, native: upstream, http: svc.httpUpstream.(*httpUpstreamRecorder)}
}

// completeTurn answers one forwarded turn and reads it through to its terminal.
func (s codexCLIOnlyWSTestSetup) completeTurn(t *testing.T, client *coderws.Conn, responseID string) {
	t.Helper()
	if s.native != nil {
		requirePassthroughUpstreamWrite(t, s.native, time.Second)
		s.native.Send(`{"type":"response.completed","response":{"id":"` + responseID + `","model":"gpt-6-astra","usage":{"input_tokens":2,"output_tokens":1}}}`)
	}
	events := readNativeWSTestTurn(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(events[len(events)-1], "type").String())
}

// requireRejected expects the 403 event the client must see, then the policy
// close, with no request reaching either upstream.
func (s codexCLIOnlyWSTestSetup) requireRejected(t *testing.T, client *coderws.Conn, serverErr <-chan error, upstreamRequests int) {
	t.Helper()
	event, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(event, "type").String())
	require.Equal(t, int64(http.StatusForbidden), gjson.GetBytes(event, "status").Int())
	require.Equal(t, "forbidden_error", gjson.GetBytes(event, "error.type").String())
	require.Equal(t, CodexOfficialClientsOnlyMessage, gjson.GetBytes(event, "error.message").String())
	// net.Pipe writes are synchronous: keep reading so closing frames cannot block.
	go func() {
		for {
			if _, _, err := client.Read(context.Background()); err != nil {
				return
			}
		}
	}()
	select {
	case err := <-serverErr:
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
		require.ErrorIs(t, err, ErrOpenAIWSCodexClientRestricted)
	case <-time.After(3 * time.Second):
		t.Fatal("WS ingress did not stop at the codex_cli_only rejection")
	}
	require.Len(t, s.http.requests, upstreamRequests)
	if s.native != nil {
		select {
		case payload := <-s.native.writes:
			t.Fatalf("a rejected turn must not reach the native upstream: %s", payload)
		default:
		}
	}
}

func TestOpenAIWSIngressCodexCLIOnlyRejectsNonCodexClient(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, http.Header{"User-Agent": {"curl/8.0"}},
				`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, nil)
			defer client.CloseNow()
			setup.requireRejected(t, client, serverErr, 0)
		})
	}
}

func TestOpenAIWSIngressCodexCLIOnlyAllowsOfficialClient(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			header := http.Header{"User-Agent": {codexCLIOnlyTestOfficialUA}, "Originator": {"codex_cli_rs"}, "X-Codex-Window-Id": {"window-1"}}
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, header,
				`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, nil)
			defer client.CloseNow()
			setup.completeTurn(t, client, "resp_official")
			requireNativeWSTestServerExit(t, client, serverErr)
		})
	}
}

// HTTP checks every request, so a WS connection re-checks every turn: the
// handshake fixes the identity headers, but body fingerprint signals vary.
func TestOpenAIWSIngressCodexCLIOnlyRechecksEachTurn(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			setup.svc.settingService = NewSettingService(&nativeWSSettingsRepo{values: map[string]string{
				SettingKeyCodexCLIOnlyEngineFingerprintSignals: `[{"type":"body_path","match":["client_metadata.x-codex-installation-id"],"required":true}]`,
			}}, setup.svc.cfg)
			header := http.Header{"User-Agent": {codexCLIOnlyTestOfficialUA}, "Originator": {"codex_cli_rs"}}
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, header,
				`{"type":"response.create","model":"gpt-6-astra","input":"first","client_metadata":{"x-codex-installation-id":"install-1"}}`, nil)
			defer client.CloseNow()
			setup.completeTurn(t, client, "resp_first")
			firstTurnRequests := len(setup.http.requests)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","input":"second"}`)))
			setup.requireRejected(t, client, serverErr, firstTurnRequests)
		})
	}
}

type nativeWSTestTurn struct {
	result *OpenAIForwardResult
	err    error
}

// Exercise a real HTTP upgrade and WS framing over net.Pipe. No TCP ports or
// external services are required, including in restricted test environments.
type nativeWSMemoryListener struct {
	conn     net.Conn
	done     chan struct{}
	once     sync.Once
	accepted bool
}

func (l *nativeWSMemoryListener) Accept() (net.Conn, error) {
	if !l.accepted {
		l.accepted = true
		return l.conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *nativeWSMemoryListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }
func (l *nativeWSMemoryListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "native-ws-test", Net: "memory"}
}

func startOpenAIWSMemorySession(t *testing.T, svc *OpenAIGatewayService, account *Account, header http.Header, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	clientPipe, serverPipe := net.Pipe()
	listener := &nativeWSMemoryListener{conn: serverPipe, done: make(chan struct{})}
	serverErr := make(chan error, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.CloseNow()
		ctx := context.Background()
		_, first, err := ReadOpenAIWSClientMessage(ctx, conn, 3*time.Second, coderws.StatusPolicyViolation, "missing first request")
		if err != nil {
			serverErr <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.Clone(ctx)
		serverErr <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "test-token", first, hooks)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = clientPipe.Close(); _ = serverPipe.Close(); _ = server.Close() })
	transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return clientPipe, nil }}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws://native-ws-test/v1/responses", &coderws.DialOptions{HTTPClient: &http.Client{Transport: transport}, HTTPHeader: header})
	require.NoError(t, err)
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
	return client, serverErr
}
func TestOpenAIWSNativeCodexIngressCompatibility(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(cfg, upstream)
			pool := newOpenAIWSConnPool(cfg)
			defer pool.Close()
			pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
			svc.openaiWSPool = pool
			account := nativeWSTestAccount()
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
			turns := make(chan nativeWSTestTurn, 2)
			client, serverErr := startNativeWSMemorySession(t, svc, account, `{"type":"response.create","model":"gpt-6-astra","input":"first"}`, &OpenAIWSIngressHooks{
				AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- nativeWSTestTurn{result, err} },
			})
			defer client.CloseNow()
			for turn := 1; turn <= 2; turn++ {
				payload := requirePassthroughUpstreamWrite(t, upstream, time.Second)
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(payload, "model").String())
				upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_native_%d","model":"gpt-6-astra","usage":{"input_tokens":2,"output_tokens":1}}}`, turn))
				readNativeWSTestTurn(t, client)
				result := <-turns
				require.NoError(t, result.err)
				require.Equal(t, 2, result.result.Usage.InputTokens)
				if turn == 1 {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					err := client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_native_1","input":[{"role":"user","content":"second"}]}`))
					cancel()
					require.NoError(t, err)
				}
			}
			require.Empty(t, svc.httpUpstream.(*httpUpstreamRecorder).requests, "native OAuth accounts must keep their upstream WS transport")
			requireNativeWSTestServerExit(t, client, serverErr)
		})
	}
}
func readNativeWSTestTurn(t *testing.T, conn *coderws.Conn) []json.RawMessage {
	t.Helper()
	var events []json.RawMessage
	for {
		payload, err := readPassthroughLifecycleFrame(t, conn, 3*time.Second)
		require.NoError(t, err)
		require.True(t, json.Valid(payload), "a WS frame must contain JSON, not SSE lines")
		events = append(events, payload)
		kind := gjson.GetBytes(payload, "type").String()
		if isOpenAIWSTerminalEvent(kind) || kind == "error" {
			return events
		}
	}
}
func requireNativeWSTestServerExit(t *testing.T, conn *coderws.Conn, serverErr <-chan error) {
	t.Helper()
	_ = conn.CloseNow()
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Native WS ingress did not exit after client disconnect")
	}
}
func startNativeWSMemorySession(t *testing.T, svc *OpenAIGatewayService, account *Account, payload string, hooks *OpenAIWSIngressHooks) (*coderws.Conn, <-chan error) {
	t.Helper()
	return startOpenAIWSMemorySession(t, svc, account, nil, payload, hooks)
}

type nativeCodexStagedConn struct{ *stagedPassthroughConn }

func (c *nativeCodexStagedConn) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

func nativeWSTestAccount() *Account {
	return &Account{ID: 300, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 10,
		Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"}, Extra: map[string]any{"openai_passthrough": true}}
}

type nativeWSSettingsRepo struct {
	SettingRepository
	mu     sync.Mutex
	values map[string]string
	err    error
}

func (r *nativeWSSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		values[key] = r.values[key]
	}
	return values, r.err
}

func (r *nativeWSSettingsRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, r.err
}

func (r *nativeWSSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	values, err := r.GetMultiple(ctx, []string{key})
	return values[key], err
}

func (r *nativeWSSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}
