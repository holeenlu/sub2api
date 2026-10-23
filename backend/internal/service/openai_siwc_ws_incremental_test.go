package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Only the test dialer redirects the fixed public endpoint to a local server.
// Both sides otherwise use the production WebSocket framing and reader loop.
type siwcLocalWSDialer struct {
	target    string
	mu        sync.Mutex
	urls      []string
	headers   []http.Header
	afterDial func()
}

func (d *siwcLocalWSDialer) Dial(ctx context.Context, target string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	d.urls = append(d.urls, target)
	d.headers = append(d.headers, headers.Clone())
	d.mu.Unlock()
	conn, status, response, err := newDefaultOpenAIWSClientDialer().Dial(ctx, d.target, headers, "")
	if d.afterDial != nil {
		d.afterDial()
	}
	return conn, status, response, err
}

func siwcIncrementalFixture(t *testing.T, target string) (*OpenAIGatewayService, *OpenAIOAuthService, *Account, *httpUpstreamRecorder, *siwcLocalWSDialer) {
	t.Helper()
	httpUpstream := &httpUpstreamRecorder{}
	svc, oauth, account := siwcWSFixture(t, httpUpstream)
	svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc.cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 10
	svc.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 4
	svc.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 2
	svc.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 4
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
	pool := newOpenAIWSConnPool(svc.cfg)
	dialer := &siwcLocalWSDialer{target: target}
	pool.setClientDialerForTest(dialer)
	svc.openaiWSPool = pool
	t.Cleanup(pool.Close)
	return svc, oauth, account, httpUpstream, dialer
}

func siwcWSJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

// The comparison uses the same local TCP server for HTTP and WS. It measures
// transport overhead only: there is no real model latency or bandwidth shaping.
type siwcLoopbackHTTPUpstream struct {
	httpUpstreamRecorder
	target *url.URL
}

func (u *siwcLoopbackHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, append([]byte(nil), body...))
	out := req.Clone(req.Context())
	endpoint := *req.URL
	endpoint.Scheme, endpoint.Host = u.target.Scheme, u.target.Host
	out.URL, out.Host, out.Body = &endpoint, u.target.Host, io.NopCloser(bytes.NewReader(body))
	return http.DefaultClient.Do(out)
}

func TestSIWCWebSocketSendsOnlyIncrementalContextOnOneConnection(t *testing.T) {
	frames := make(chan []byte, 4)
	var httpTurn atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = io.Copy(io.Discard, r.Body)
			turn := httpTurn.Add(1)
			output := []any{}
			if turn == 1 {
				output = []any{map[string]any{"type": "reasoning", "id": "rs_private", "encrypted_content": "private-cipher", "summary": []any{}}, map[string]any{"type": "function_call", "id": "fc_lookup", "call_id": "call_lookup", "name": "lookup", "arguments": "{}"}}
			}
			payload, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("resp_siwc_%d", turn), "status": "completed", "output": output}})
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintln(w, "data:", string(payload))
			fmt.Fprintln(w)
			return
		}
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(2 << 20)
		for turn := 1; ; turn++ {
			_, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			frames <- raw
			output := []any{}
			if turn == 1 {
				output = []any{
					map[string]any{"type": "reasoning", "id": "rs_private", "encrypted_content": "private-cipher", "summary": []any{}},
					map[string]any{"type": "function_call", "id": "fc_lookup", "call_id": "call_lookup", "name": "lookup", "arguments": "{}"},
				}
			}
			event := map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("resp_siwc_%d", turn), "status": "completed", "output": output, "usage": map[string]int{"input_tokens": 4, "output_tokens": 2}}}
			// Multiline JSON is legal in a WS frame; the stream adapter must
			// preserve the event rather than truncate it at the first line.
			payload, _ := json.MarshalIndent(event, "", "  ")
			if conn.Write(r.Context(), coderws.MessageText, payload) != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	svc, _, account, httpUpstream, dialer := siwcIncrementalFixture(t, "ws"+strings.TrimPrefix(upstream.URL, "http"))
	turns := make(chan bpsWSTestTurn, 4)
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { turns <- bpsWSTestTurn{result, err} }}
	largeInput := strings.Repeat("context ", 2048)
	firstPayload := siwcWSJSON(t, map[string]any{
		"type": "response.create", "model": "gpt-6.1-sol", "instructions": "original policy", "input": []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": largeInput}, map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="}}}},
		"tools": []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object"}}, map[string]any{"type": "web_search"}},
	})
	var finalTiming *openAIForwardTiming
	client, done := startOpenAIWSMemorySession(t, svc, account, http.Header{"Originator": {"must-not-forward"}, "ChatGPT-Account-Id": {"must-not-forward"}}, string(firstPayload), hooks, func(c *gin.Context) { finalTiming = openAIForwardTimingFromContext(c) })
	defer client.CloseNow()
	readNativeWSTestTurn(t, client)
	firstResult := <-turns
	require.NoError(t, firstResult.err)
	require.Equal(t, UsageAuthorizationModeSIWC, firstResult.result.AuthorizationMode)
	first := <-frames
	require.False(t, gjson.GetBytes(first, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(first, "stream").Exists())
	require.False(t, gjson.GetBytes(first, "store").Bool())
	require.Equal(t, "additional_tools", gjson.GetBytes(first, "input.0.type").String())
	require.Contains(t, string(first), "data:image/png;base64,aGVsbG8=")
	require.Equal(t, "web_search", gjson.GetBytes(first, "tools.0.type").String())
	secondPayload := siwcWSJSON(t, map[string]any{"type": "response.create", "previous_response_id": "resp_siwc_1", "instructions": "original policy", "input": []any{map[string]any{"type": "function_call_output", "call_id": "call_lookup", "output": "42"}}})
	wsStarted := time.Now()
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, secondPayload))
	readNativeWSTestTurn(t, client)
	wsRoundTrip := time.Since(wsStarted)
	require.NoError(t, (<-turns).err)
	second := <-frames
	require.Equal(t, "resp_siwc_1", gjson.GetBytes(second, "previous_response_id").String())
	require.Len(t, gjson.GetBytes(second, "input").Array(), 1)
	require.Equal(t, "function_call_output", gjson.GetBytes(second, "input.0.type").String())
	require.NotContains(t, string(second), largeInput)
	require.NotContains(t, string(second), "private-cipher")
	require.Less(t, len(second), len(first)/20)
	thirdPayload := siwcWSJSON(t, map[string]any{"type": "response.create", "previous_response_id": "resp_siwc_2", "instructions": "changed policy", "input": []any{map[string]any{"type": "message", "role": "user", "content": "continue"}}})
	require.NoError(t, client.Write(t.Context(), coderws.MessageText, thirdPayload))
	readNativeWSTestTurn(t, client)
	require.NoError(t, (<-turns).err)
	third := <-frames
	require.False(t, gjson.GetBytes(third, "previous_response_id").Exists(), "changed settings start a complete new response chain before sending")
	require.Contains(t, string(third), largeInput)
	require.Contains(t, string(third), "private-cipher")
	require.Empty(t, httpUpstream.requests, "WS mode must not silently make an HTTP generation")
	requireNativeWSTestServerExit(t, client, done)
	require.NotNil(t, finalTiming)
	require.Equal(t, "websocket", finalTiming.transport)
	require.Equal(t, int64(len(bytes.TrimSpace(third))), finalTiming.semanticBytes) // wsjson framing adds a newline
	require.Contains(t, finalTiming.stages, "wrote_request")
	require.Contains(t, finalTiming.stages, "connection_ready")
	require.Nil(t, finalTiming.observation, "client WS must not be double-counted as passive HTTP observation")
	dialer.mu.Lock()
	require.Equal(t, []string{"wss://api.openai.com/v1/responses"}, dialer.urls)
	require.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, dialer.headers[0].Get("Authorization"))
	require.Equal(t, "Sub2API", dialer.headers[0].Get("User-Agent"))
	require.Len(t, dialer.headers[0], 2, "SIWC must not inherit Codex identity, account or beta headers")
	dialer.mu.Unlock()
	_, _, remaining := svc.openaiWSPool.AccountPoolLoad(account.ID)
	require.Zero(t, remaining, "private grant sockets must close when ingress finishes")
	// Compare the same second turn with the existing forced-HTTP path. The
	// measurement is JSON payload bytes, not compressed wire bytes or billing.
	baselineURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	baselineHTTP := &siwcLoopbackHTTPUpstream{target: baselineURL}
	baselineSvc, _, baselineAccount := siwcWSFixture(t, baselineHTTP)
	baselineTurns := make(chan bpsWSTestTurn, 2)
	baselineClient, baselineDone := startOpenAIWSMemorySession(t, baselineSvc, baselineAccount, nil, string(firstPayload), &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { baselineTurns <- bpsWSTestTurn{r, e} }})
	defer baselineClient.CloseNow()
	readNativeWSTestTurn(t, baselineClient)
	require.NoError(t, (<-baselineTurns).err)
	httpStarted := time.Now()
	require.NoError(t, baselineClient.Write(t.Context(), coderws.MessageText, secondPayload))
	readNativeWSTestTurn(t, baselineClient)
	httpRoundTrip := time.Since(httpStarted)
	require.NoError(t, (<-baselineTurns).err)
	requireNativeWSTestServerExit(t, baselineClient, baselineDone)
	require.Len(t, baselineHTTP.requests, 2)
	baseline := baselineHTTP.bodies[1]
	require.Contains(t, string(baseline), largeInput)
	require.Contains(t, string(baseline), "private-cipher")
	require.False(t, gjson.GetBytes(baseline, "previous_response_id").Exists())
	require.Less(t, len(second), len(baseline)/20)
	t.Logf("same second-turn JSON bytes: HTTP full=%d WS incremental=%d reduction=%.2f%%; WS generations=3 dials=1 HTTP fallback=0", len(baseline), len(second), 100*(1-float64(len(second))/float64(len(baseline))))
	t.Logf("paired loopback second-turn roundtrip, no model inference: HTTP=%d us WS=%d us", httpRoundTrip.Microseconds(), wsRoundTrip.Microseconds())
}

func TestSIWCWebSocketConfiguredHTTPPreservesFullHistory(t *testing.T) {
	scenarios := []struct {
		name      string
		configure func(*OpenAIGatewayService, *Account)
	}{
		{"account off", func(s *OpenAIGatewayService, a *Account) {
			a.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
		}},
		{"http bridge", func(s *OpenAIGatewayService, a *Account) {
			a.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
		}},
		{"account force HTTP", func(s *OpenAIGatewayService, a *Account) { a.Extra["openai_ws_force_http"] = true }},
		{"global force HTTP", func(s *OpenAIGatewayService, a *Account) { s.cfg.Gateway.OpenAIWS.ForceHTTP = true }},
		{"global disabled", func(s *OpenAIGatewayService, a *Account) { s.cfg.Gateway.OpenAIWS.Enabled = false }},
		{"OAuth disabled", func(s *OpenAIGatewayService, a *Account) { s.cfg.Gateway.OpenAIWS.OAuthEnabled = false }},
		{"WS v2 disabled", func(s *OpenAIGatewayService, a *Account) { s.cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = false }},
	}
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			completed := string(siwcWSJSON(t, map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_http_owned", "status": "completed", "output": []any{}}}))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{siwcWSResponse(completed), siwcWSResponse(completed)}}
			svc, _, account := siwcWSFixture(t, upstream)
			svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
			tc.configure(svc, account)
			turns := make(chan bpsWSTestTurn, 2)
			payload := siwcWSJSON(t, map[string]any{"type": "response.create", "model": "gpt-6.1-sol", "input": "first context"})
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, string(payload), &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }})
			defer client.CloseNow()
			readNativeWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)
			require.NoError(t, client.Write(t.Context(), coderws.MessageText, siwcWSJSON(t, map[string]any{"type": "response.create", "previous_response_id": "resp_http_owned", "input": "next context"})))
			readNativeWSTestTurn(t, client)
			require.NoError(t, (<-turns).err)
			require.Len(t, upstream.requests, 2)
			require.Contains(t, string(upstream.bodies[1]), "first context")
			require.Contains(t, string(upstream.bodies[1]), "next context")
			require.False(t, gjson.GetBytes(upstream.bodies[1], "previous_response_id").Exists())
			requireNativeWSTestServerExit(t, client, done)
		})
	}
}

func TestSIWCWebSocketGrantChangedDuringDialSendsNoGeneration(t *testing.T) {
	for _, change := range []string{"revoke", "rebind"} {
		t.Run(change, func(t *testing.T) {
			frames := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_, raw, err := conn.Read(r.Context())
				if err == nil {
					frames <- raw
				}
			}))
			defer upstream.Close()
			svc, oauth, account, httpUpstream, dialer := siwcIncrementalFixture(t, "ws"+strings.TrimPrefix(upstream.URL, "http"))
			dialer.afterDial = func() {
				if change == "revoke" {
					_, err := oauth.DeleteSIWCAuthorization(t.Context(), account)
					require.NoError(t, err)
				} else {
					authorizeSIWC(t, oauth, account)
				}
			}
			payload := siwcWSJSON(t, map[string]any{"type": "response.create", "model": "gpt-6.1-sol", "input": "private context"})
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, string(payload), nil)
			defer client.CloseNow()
			events := readNativeWSTestTurn(t, client)
			require.Equal(t, "error", gjson.GetBytes(events[len(events)-1], "type").String())
			requireNativeWSTestServerExit(t, client, done)
			require.Empty(t, frames, "a changed authorization cannot send even the first generation")
			require.Empty(t, httpUpstream.requests, "revoke must not fall back to another grant or HTTP")
			_, _, remaining := svc.openaiWSPool.AccountPoolLoad(account.ID)
			require.Zero(t, remaining)
		})
	}
}

func TestSIWCWebSocketFailureKeepsHandshakeAndSemanticStatus(t *testing.T) {
	frames := make(chan []byte, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req_ws_diagnostic")
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, raw, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		frames <- raw
		event := map[string]any{"type": "response.failed", "response": map[string]any{"id": "resp_ws_failed", "status": "failed", "error": map[string]any{"type": "rate_limit_error", "code": "subscription_sharing_usage_limit_exceeded", "message": "Plan budget exhausted"}, "usage": map[string]int{"input_tokens": 3, "output_tokens": 1}}}
		payload, _ := json.Marshal(event)
		if conn.Write(r.Context(), coderws.MessageText, payload) != nil {
			return
		}
		_, raw, err = conn.Read(r.Context())
		if err == nil {
			frames <- raw
		}
	}))
	defer upstream.Close()
	svc, _, account, httpUpstream, _ := siwcIncrementalFixture(t, "ws"+strings.TrimPrefix(upstream.URL, "http"))
	turns := make(chan bpsWSTestTurn, 1)
	var diagnostics []OpsStreamError
	payload := siwcWSJSON(t, map[string]any{"type": "response.create", "model": "gpt-6.1-sol", "input": "request"})
	client, done := startOpenAIWSMemorySession(t, svc, account, nil, string(payload), &OpenAIWSIngressHooks{AfterTurn: func(_ int, r *OpenAIForwardResult, e error) { turns <- bpsWSTestTurn{r, e} }}, func(c *gin.Context) { diagnostics = GetOpsStreamErrors(c) })
	defer client.CloseNow()
	events := readNativeWSTestTurn(t, client)
	result := <-turns
	require.Equal(t, "response.failed", gjson.GetBytes(events[len(events)-1], "type").String())
	require.NotNil(t, result.result)
	require.Equal(t, 3, result.result.Usage.InputTokens)
	require.Equal(t, 1, result.result.Usage.OutputTokens)
	requireNativeWSTestServerExit(t, client, done)
	require.Len(t, frames, 1, "accepted upstream failure must not generate again")
	require.Empty(t, httpUpstream.requests)
	require.Len(t, diagnostics, 1)
	require.Equal(t, http.StatusSwitchingProtocols, diagnostics[0].UpstreamStatus)
	require.Equal(t, http.StatusTooManyRequests, diagnostics[0].IntendedStatus)
	attempt := LastOpsUpstreamAttempt(diagnostics[0].UpstreamErrors)
	require.NotNil(t, attempt)
	require.Equal(t, "websocket", gjson.Get(attempt.Detail, "transport").String())
	require.Equal(t, "req_ws_diagnostic", attempt.UpstreamRequestID)
}
