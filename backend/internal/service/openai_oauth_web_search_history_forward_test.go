//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// TestForward_OAuthWebSearchHistoryDeclaresTool drives the Codex compaction
// shape (web_search_call history, tools:[]) through Forward and asserts the
// body reaching chatgpt.com declares web_search (#7927): top-level for the
// standard wire only. Lite preserves history without adding hosted tools.
func TestForward_OAuthWebSearchHistoryDeclaresTool(t *testing.T) {
	for _, tc := range []struct {
		name        string
		passthrough bool
		lite        bool
		standalone  bool
	}{
		{"transform", false, false, false},
		{"passthrough", true, false, false},
		{"transform lite", false, true, false},
		{"passthrough lite", true, true, false},
		{"transform lite legacy", false, true, false},
		{"passthrough lite legacy", true, true, false},
		{"transform standalone", false, false, true},
		{"passthrough standalone", true, false, true},
		{"transform lite standalone", false, true, true},
		{"passthrough lite standalone", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			s := newAstraOAuthSetup(t, tc.passthrough)
			if tc.lite {
				s.c.Request.Header.Set(responsesLiteHeader, "true")
			}
			inner := `{"id":"resp_test","model":"gpt-5.5","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`
			effectiveLite := tc.lite && !strings.Contains(tc.name, "legacy")
			model := "gpt-5.5"
			if effectiveLite || tc.standalone {
				model = "gpt-6.1-sol"
			}
			inner = strings.ReplaceAll(inner, "gpt-5.5", model)
			s.upstream.resp = &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(codexCompletedSSE(inner))),
			}
			body, err := sjson.SetRawBytes([]byte(openAIWebSearchHistoryCompactionBody), "input.-1", []byte(`{"type":"compaction_trigger"}`))
			require.NoError(t, err)
			const standaloneTools = `[{"type":"namespace","name":"web","tools":[{"type":"function","name":"run","parameters":{"type":"object"}}]}]`
			if tc.standalone {
				body, err = sjson.SetRawBytes(body, "tools", []byte(standaloneTools))
				require.NoError(t, err)
				body, err = sjson.SetBytes(body, "model", "gpt-6-luna")
				require.NoError(t, err)
			}

			body, err = sjson.SetBytes(body, "model", model)
			require.NoError(t, err)

			result, err := s.svc.Forward(context.Background(), s.c, s.account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, s.upstream.lastReq)
			require.Equal(t, astraProCodexResponsesURL, s.upstream.lastReq.URL.String())

			forwarded := s.upstream.lastBody
			require.Equal(t, "web_search_call", gjson.GetBytes(forwarded, "input.1.type").String())
			items := gjson.GetBytes(forwarded, "input").Array()
			require.Equal(t, "compaction_trigger", items[len(items)-1].Get("type").String())
			if tc.standalone {
				if tc.lite {
					// Lite moves namespaces to its existing additional_tools carrier.
					require.False(t, gjson.GetBytes(forwarded, "tools").Exists())
					require.Len(t, items, 6)
					require.JSONEq(t, standaloneTools, items[len(items)-2].Get("tools").Raw)
				} else {
					require.JSONEq(t, standaloneTools, gjson.GetBytes(forwarded, "tools").Raw)
					require.Len(t, items, 5, "do not inject hosted tools into replayed history")
				}
				require.Equal(t, "auto", gjson.GetBytes(forwarded, "tool_choice").String())
				return
			}
			if effectiveLite {
				require.True(t, isOpenAIResponsesLiteHeader(s.upstream.lastReq.Header.Get(responsesLiteHeader)))
				require.Empty(t, gjson.GetBytes(forwarded, "tools").Array())
				require.Len(t, items, 5, "Lite must not receive an injected hosted-tool declaration")
				require.Equal(t, "auto", gjson.GetBytes(forwarded, "tool_choice").String())
				return
			}
			require.False(t, isOpenAIResponsesLiteHeader(s.upstream.lastReq.Header.Get(responsesLiteHeader)))
			require.Equal(t, "none", gjson.GetBytes(forwarded, "tool_choice").String())
			tools := gjson.GetBytes(forwarded, "tools").Array()
			require.Len(t, tools, 1)
			require.Equal(t, "web_search", tools[0].Get("type").String())
			require.Len(t, items, 5, "standard compatibility must not alter search history")
		})
	}
}

// Exercise the real client WS owner and all three transport modes. The fake
// upstream enforces the native Codex Lite contract and returns the production
// rejection when an injected hosted declaration reaches it.
func TestWSOAuthLiteSearchHistoryDoesNotIntroduceHostedTools(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(cfg, upstream)
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(&openAIWSQueueDialer{conns: []openAIWSClientConn{&nativeCodexStagedConn{upstream}}})
			svc.openaiWSPool = pool
			t.Cleanup(pool.Close)
			account := nativeWSTestAccount()
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			captured := make(chan []byte, 2)
			respond := func(payload []byte, turn int) string {
				captured <- append([]byte(nil), payload...)
				toolSets := []gjson.Result{gjson.GetBytes(payload, "tools")}
				for _, item := range gjson.GetBytes(payload, "input").Array() {
					if item.Get("type").String() == "additional_tools" {
						toolSets = append(toolSets, item.Get("tools"))
					}
				}
				for _, tools := range toolSets {
					for _, tool := range tools.Array() {
						if strings.HasPrefix(tool.Get("type").String(), "web_search") {
							return `{"type":"response.failed","response":{"id":"resp_rejected","status":"failed","error":{"code":"unsupported_parameter","type":"invalid_request_error","message":"Hosted tool 'web_search' requires authorization and metering that are not supported by rustponsesapi."}}}`
						}
					}
				}
				return fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_lite_%d","status":"completed","model":"gpt-6.1-sol","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`, turn)
			}
			httpTurn := 0
			svc.httpUpstream = &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, err
				}
				httpTurn++
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + respond(body, httpTurn) + "\n\n"))}, nil
			}}
			payload := `{"type":"response.create","model":"gpt-6.1-sol","stream":true,"store":false,"tool_choice":"auto","client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"},"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"echo","parameters":{"type":"object"}}]},{"type":"message","role":"user","content":"summarize prior search"},{"type":"web_search_call","id":"ws_history","status":"completed","action":{"type":"search","query":"earlier query"}},{"type":"reasoning","id":"rs_history","summary":[],"encrypted_content":"opaque-history"}]}`
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, payload, nil)
			defer client.CloseNow()
			for turn := 1; turn <= 2; turn++ {
				if mode != OpenAIWSIngressModeHTTPBridge {
					wire := requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
					upstream.Send(respond(wire, turn))
				}
				events := readNativeWSTestTurn(t, client)
				last := events[len(events)-1]
				require.Equal(t, "response.completed", gjson.GetBytes(last, "type").String())
				require.Equal(t, fmt.Sprintf("resp_lite_%d", turn), gjson.GetBytes(last, "response.id").String())
				wire := <-captured
				require.Equal(t, "auto", gjson.GetBytes(wire, "tool_choice").String())
				if turn == 1 {
					require.Equal(t, "ws_history", gjson.GetBytes(wire, `input.#(type=="web_search_call").id`).String())
					require.Equal(t, "opaque-history", gjson.GetBytes(wire, `input.#(type=="reasoning").encrypted_content`).String())
					require.Equal(t, "echo", gjson.GetBytes(wire, `input.#(type=="additional_tools").tools.0.name`).String())
					require.Len(t, gjson.GetBytes(wire, `input.#(type=="additional_tools").tools`).Array(), 1)
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					err := client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6.1-sol","stream":true,"store":false,"tool_choice":"auto","previous_response_id":"resp_lite_1","client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"},"input":[{"role":"user","content":"continue"}]}`))
					cancel()
					require.NoError(t, err)
				}
			}
			requireNativeWSTestServerExit(t, client, done)
		})
	}
}
