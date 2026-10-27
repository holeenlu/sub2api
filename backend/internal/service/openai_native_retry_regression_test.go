package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const nativeRetryMeteringRejection = `{"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"tools","message":"Hosted tool 'web_search' requires authorization and metering that are not supported by rustponsesapi."}}`

func TestOpenAINativeRejectedRetryStopsForUnsafeHistoryAndUsage(t *testing.T) {
	for _, metered := range []bool{false, true} {
		t.Run(fmt.Sprintf("metered_%t", metered), func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 0
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			request := `{"type":"response.create","model":"gpt-5.1","store":false,"input":[{"role":"user","content":"current","status":"completed"}]}`
			failed := []byte(`{"type":"response.failed","response":{"id":"resp_metered","usage":{"input_tokens":9,"output_tokens":2},"error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"input[0].status","message":"Unsupported parameter: input[0].status"}}}`)
			old := &openAIWSCaptureConn{events: [][]byte{failed}}
			if !metered {
				request = `{"type":"response.create","model":"gpt-5.1","store":false,"input":[{"type":"item_reference","id":"msg_remote"}]}`
				old.events = [][]byte{
					[]byte(`{"type":"response.completed","response":{"id":"resp_initial","output":[]}}`),
					[]byte(`{"type":"error","error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"input[0].status","message":"Unsupported parameter: input[0].status"}}`),
				}
			}
			dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{old}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(dialer)
			svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
			svc.openaiWSPool = pool
			account := passthroughLifecycleAccount()
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
			var results []*OpenAIForwardResult
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) { results = append(results, result) }}
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, request, hooks)
			defer client.CloseNow()
			if !metered {
				readNativeWSTestTurn(t, client)
				require.NoError(t, client.Write(context.Background(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","store":false,"previous_response_id":"resp_initial","input":[{"role":"user","content":"current","status":"completed"}]}`)))
			}
			select {
			case err := <-done:
				require.Error(t, err)
				if !metered {
					require.ErrorIs(t, err, ErrOpenAIWSReplayUnavailable)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("unsafe retry did not stop")
			}
			require.Equal(t, 1, dialer.DialCount(), "must not replay unknown history or observed usage")
			if metered {
				require.Len(t, results, 1)
				require.Equal(t, "resp_metered", results[0].RequestID)
				require.EqualValues(t, 9, results[0].Usage.InputTokens)
				require.EqualValues(t, 2, results[0].Usage.OutputTokens)
				require.Len(t, old.writes, 1)
			} else {
				require.Len(t, old.writes, 2)
			}
		})
	}
}

func TestOpenAINativeRetryIsolatesRejectedAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, continuation := range []bool{false, true} {
		t.Run(fmt.Sprintf("continuation_%t", continuation), func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 0
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			old := &openAIWSCaptureConn{events: [][]byte{
				[]byte(`{"type":"error","error":{"type":"invalid_request_error","code":"unsupported_parameter","param":"input[0].status","message":"Unsupported parameter: input[0].status"}}`),
				[]byte(`{"type":"response.failed","response":{"id":"resp_stale","error":{"type":"invalid_request_error","code":"unsupported_parameter"}}}`),
			}}
			if continuation {
				old.events = append([][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_initial","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"earlier answer"}]}]}}`)}, old.events...)
			}
			fresh := &openAIWSCaptureConn{events: [][]byte{
				[]byte(`{"type":"response.completed","response":{"id":"resp_recovered","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`),
				[]byte(`{"type":"response.completed","response":{"id":"resp_next","output":[],"usage":{"input_tokens":3,"output_tokens":1}}}`),
			}}
			dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{old, fresh}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(dialer)
			svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
			svc.openaiWSPool = pool
			account := passthroughLifecycleAccount()
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = OpenAIWSIngressModeCtxPool
			request := `{"type":"response.create","model":"gpt-5.1","store":false,"input":[{"role":"user","content":"current","status":"completed"}]}`
			first := request
			if continuation {
				first = `{"type":"response.create","model":"gpt-5.1","store":false,"input":[{"role":"user","content":"earlier question"}]}`
				request = `{"type":"response.create","model":"gpt-5.1","store":false,"previous_response_id":"resp_initial","input":[{"role":"user","content":"current","status":"completed"}]}`
			}
			var billed []string
			hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
				if result != nil {
					billed = append(billed, result.RequestID)
				}
			}}
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, first, hooks)
			defer client.CloseNow()
			if continuation {
				frames := readNativeWSTestTurn(t, client)
				require.Equal(t, "resp_initial", gjson.GetBytes(frames[len(frames)-1], "response.id").String())
				require.NoError(t, client.Write(context.Background(), coderws.MessageText, []byte(request)))
			}
			frames := readNativeWSTestTurn(t, client)
			require.Equal(t, "response.completed", gjson.GetBytes(frames[len(frames)-1], "type").String())
			require.Equal(t, "resp_recovered", gjson.GetBytes(frames[len(frames)-1], "response.id").String())
			require.NoError(t, client.Write(context.Background(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","store":false,"previous_response_id":"resp_recovered","input":[{"role":"user","content":"next"}]}`)))
			frames = readNativeWSTestTurn(t, client)
			require.Equal(t, "resp_next", gjson.GetBytes(frames[len(frames)-1], "response.id").String())
			_ = client.CloseNow()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("session did not exit")
			}
			require.Equal(t, 2, dialer.DialCount())
			require.Len(t, fresh.writes, 2)
			replayed := requestToJSONString(fresh.writes[0])
			require.False(t, gjson.Get(replayed, "previous_response_id").Exists())
			if continuation {
				require.Equal(t, "earlier question", gjson.Get(replayed, "input.0.content").String())
				require.Equal(t, "earlier answer", gjson.Get(replayed, "input.1.content.0.text").String())
				require.Equal(t, "current", gjson.Get(replayed, "input.2.content").String())
				require.False(t, gjson.Get(replayed, "input.2.status").Exists())
				require.Equal(t, []string{"resp_initial", "resp_recovered", "resp_next"}, billed)
			} else {
				require.False(t, gjson.Get(replayed, "input.0.status").Exists())
				require.Equal(t, []string{"resp_recovered", "resp_next"}, billed)
			}
		})
	}
}

func TestOpenAINativeSearchProvenanceHTTP(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, callerOwned := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough_%t_caller_%t", passthrough, callerOwned), func(t *testing.T) {
				tools := ""
				if callerOwned {
					tools = `,"tools":[{"type":"web_search","external_web_access":false}],"tool_choice":"auto"`
				}
				body := []byte(`{"model":"gpt-5.1","stream":true,"instructions":"test","input":[{"type":"web_search_call","id":"ws_history","status":"completed","action":{"type":"search","query":"history"}},{"role":"user","content":"continue"}]` + tools + `}`)
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, nativeRetryMeteringRejection),
					newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":1}}}\n\n"),
				}}
				svc, account := newOpenAIRejectedFieldTestService(upstream), newOpenAIOAuthNamespaceTestAccount()
				c := newOpenAIRejectedFieldTestContext(body)
				var err error
				if passthrough {
					_, err = svc.forwardOpenAIPassthrough(context.Background(), c, account, body, body, "gpt-5.1", false, nil, true, time.Now())
				} else {
					_, err = svc.Forward(context.Background(), c, account, body)
				}
				if callerOwned {
					require.Error(t, err)
					require.Len(t, upstream.bodies, 1)
					require.Equal(t, "web_search", gjson.GetBytes(upstream.bodies[0], "tools.0.type").String())
				} else {
					require.NoError(t, err)
					require.Len(t, upstream.bodies, 2)
					require.Equal(t, "web_search", gjson.GetBytes(upstream.bodies[0], "tools.0.type").String())
					require.False(t, gjson.GetBytes(upstream.bodies[1], "tools").Exists())
				}
			})
		}
	}
}

func TestOpenAINativeSearchProvenanceWebSocket(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModeHTTPBridge} {
		t.Run(mode, func(t *testing.T) {
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 0
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
			account := newOpenAIOAuthNamespaceTestAccount()
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": mode}
			bare := []byte(`{"type":"error",` + nativeRetryMeteringRejection[1:])
			completed := []byte(`{"type":"response.completed","response":{"id":"resp_injected","output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`)
			old := &openAIWSCaptureConn{events: [][]byte{bare}}
			fresh := &openAIWSCaptureConn{events: [][]byte{completed, bare}}
			dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{old, fresh}}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(dialer)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, nativeRetryMeteringRejection),
				newOpenAIRejectedFieldTestResponse(http.StatusOK, "data: "+string(completed)+"\n\n"),
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, nativeRetryMeteringRejection),
			}}
			svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
			svc.openaiWSPool, svc.httpUpstream = pool, upstream
			client, done := startOpenAIWSMemorySession(t, svc, account, nil, `{"type":"response.create","model":"gpt-5.1","input":[{"type":"web_search_call","id":"ws_history","status":"completed"},{"role":"user","content":"continue"}]}`, nil)
			defer client.CloseNow()
			frames := readNativeWSTestTurn(t, client)
			require.Equal(t, "resp_injected", gjson.GetBytes(frames[len(frames)-1], "response.id").String())
			// Provenance must reset for a later caller-owned cached search.
			require.NoError(t, client.Write(context.Background(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1","input":"search cached material","tools":[{"type":"web_search","external_web_access":false}],"tool_choice":"auto"}`)))
			frames = readNativeWSTestTurn(t, client)
			require.Equal(t, "error", gjson.GetBytes(frames[len(frames)-1], "type").String())
			_ = client.CloseNow()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("session did not exit")
			}
			if mode == OpenAIWSIngressModeCtxPool {
				require.Equal(t, 2, dialer.DialCount())
				require.Len(t, fresh.writes, 2)
				require.False(t, gjson.Get(requestToJSONString(fresh.writes[0]), "tools").Exists())
				require.Equal(t, "web_search", gjson.Get(requestToJSONString(fresh.writes[1]), "tools.0.type").String())
			} else {
				require.Len(t, upstream.bodies, 3)
				require.False(t, gjson.GetBytes(upstream.bodies[1], "tools").Exists())
				require.Equal(t, "web_search", gjson.GetBytes(upstream.bodies[2], "tools.0.type").String())
			}
		})
	}
}
