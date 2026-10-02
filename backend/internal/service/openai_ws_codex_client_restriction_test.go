package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const codexCLIOnlyTestOfficialUA = "codex_cli_rs/0.141.0 (x) (codex_cli_rs; 0.141.0)"

// codexCLIOnlyWSTestSetup builds one codex_cli_only account per ingress path:
// native ctx_pool and passthrough upstream sockets, and the BPS HTTP bridge.
type codexCLIOnlyWSTestSetup struct {
	svc     *OpenAIGatewayService
	account *Account
	native  *stagedPassthroughConn // nil on the BPS path
	http    *httpUpstreamRecorder
}

func newCodexCLIOnlyWSTestSetup(t *testing.T, mode string) codexCLIOnlyWSTestSetup {
	t.Helper()
	if mode == "bps" {
		upstream := &httpUpstreamRecorder{responses: []*http.Response{
			bpsCompletionResponse(200, bpsCompletedStream("resp_bps_1", "first")),
			bpsCompletionResponse(200, bpsCompletedStream("resp_bps_2", "second")),
		}}
		svc := openAIClientToolsTestService(upstream)
		svc.cfg = passthroughLifecycleConfig()
		account := excelAccount()
		account.Extra["codex_cli_only"] = true
		return codexCLIOnlyWSTestSetup{svc: svc, account: account, http: upstream}
	}
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(cfg, upstream)
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
	svc.openaiWSPool = pool
	account := excelAccount()
	account.Extra["openai_excel_bps"] = false
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
	events := readBPSWSTestTurn(t, client)
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
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, "bps"} {
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
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, "bps"} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			header := http.Header{"User-Agent": {codexCLIOnlyTestOfficialUA}, "Originator": {"codex_cli_rs"}, "X-Codex-Window-Id": {"window-1"}}
			client, serverErr := startOpenAIWSMemorySession(t, setup.svc, setup.account, header,
				`{"type":"response.create","model":"gpt-6-astra","input":"hello"}`, nil)
			defer client.CloseNow()
			setup.completeTurn(t, client, "resp_official")
			requireBPSWSTestServerExit(t, client, serverErr)
		})
	}
}

// HTTP checks every request, so a WS connection re-checks every turn: the
// handshake fixes the identity headers, but body fingerprint signals vary.
func TestOpenAIWSIngressCodexCLIOnlyRechecksEachTurn(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, "bps"} {
		t.Run(mode, func(t *testing.T) {
			setup := newCodexCLIOnlyWSTestSetup(t, mode)
			setup.svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{values: map[string]string{
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
