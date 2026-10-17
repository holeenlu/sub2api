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
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func siwcHTTPTestContext(t *testing.T, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	for k, v := range map[string]string{"Authorization": "Bearer downstream-key", "Cookie": "native-cookie", "ChatGPT-Account-ID": "native-workspace", "Originator": "codex_cli_rs", "OpenAI-Beta": "responses_websockets=2026-02-06", "OpenAI-Organization": "native-org", "OpenAI-Project": "native-project", "User-Agent": "codex_cli/9.9.9"} {
		c.Request.Header.Set(k, v)
	}
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	return rec, c
}
func siwcHTTPBody(model string, stream bool) []byte {
	return []byte(fmt.Sprintf(`{"model":%q,"stream":%t,"store":true,"input":"hello"}`, model, stream))
}

func TestSIWCGatewayTransportSelectionUsesBridge(t *testing.T) {
	svc, _, a := siwcWSFixture(t, &httpUpstreamRecorder{})
	for _, router := range []bool{false, true} {
		svc.cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = router
		for _, mode := range []string{OpenAIWSIngressModeOff, OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
			a.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
			require.Equal(t, mode, a.ResolveOpenAIResponsesWebSocketV2Mode(OpenAIWSIngressModeOff))
			require.False(t, a.UsesOpenAICodexProtocol())
			for _, transport := range []OpenAIUpstreamTransport{OpenAIUpstreamTransportAny, OpenAIUpstreamTransportHTTPSSE, OpenAIUpstreamTransportResponsesWebsocketV2Ingress} {
				require.True(t, svc.isOpenAIAccountTransportCompatible(t.Context(), a, transport, "gpt-6.1-sol"))
			}
			for _, transport := range []OpenAIUpstreamTransport{OpenAIUpstreamTransportResponsesWebsocket, OpenAIUpstreamTransportResponsesWebsocketV2} {
				require.False(t, svc.isOpenAIAccountTransportCompatible(t.Context(), a, transport, "gpt-6.1-sol"))
			}
		}
	}
}

func TestSIWCGatewayForwardSuccessUsesIndependentGrantAndExactModel(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, model := range []string{"gpt-6.1-sol", "public-alias"} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, model), func(t *testing.T) {
				mapped := model
				if model == "public-alias" {
					mapped = "siwc-preview-specific-slug"
				}
				upstream := &httpUpstreamRecorder{resp: siwcWSResponse(`{"type":"response.created","response":{"id":"resp_http","status":"in_progress"}}`, fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_http","object":"response","model":%q,"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello back"}]}],"usage":{"input_tokens":7,"output_tokens":3}}}`, mapped))}
				svc, _, a := siwcWSFixture(t, upstream)
				a.Credentials["model_mapping"] = map[string]any{"public-alias": mapped}
				body := siwcHTTPBody(model, stream)
				rec, c := siwcHTTPTestContext(t, body)
				result, err := svc.Forward(t.Context(), c, a, body)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, upstream.requests, 1)
				timing := openAIForwardTimingFromContext(c)
				require.NotNil(t, timing)
				require.NotNil(t, timing.observation)
				require.True(t, timing.observation.report.Candidate)
				require.Equal(t, "unknown", timing.observation.report.StateReady)
				require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
				require.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, upstream.lastReq.Header.Get("Authorization"))
				for _, h := range []string{"Cookie", "ChatGPT-Account-ID", "Originator", "OpenAI-Beta", "OpenAI-Organization", "OpenAI-Project"} {
					require.Empty(t, upstream.lastReq.Header.Get(h), h)
				}
				require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
				require.Equal(t, mapped, gjson.GetBytes(upstream.lastBody, "model").String())
				require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
				require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
				require.True(t, gjson.GetBytes(upstream.lastBody, "input").IsArray())
				require.Equal(t, UsageAuthorizationModeSIWC, result.AuthorizationMode)
				require.Equal(t, model, result.Model)
				require.Equal(t, mapped, result.UpstreamModel)
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 3, result.Usage.OutputTokens)
				require.Equal(t, 200, rec.Code)
				if stream {
					require.Contains(t, rec.Body.String(), "response.completed")
				} else {
					require.Equal(t, "response", gjson.Get(rec.Body.String(), "object").String())
					require.Equal(t, model, gjson.Get(rec.Body.String(), "model").String())
					require.Equal(t, "hello back", gjson.Get(rec.Body.String(), "output.0.content.0.text").String())
				}
			})
		}
	}
}

func TestSIWCGatewayHTTPFailuresPreserveContractWithoutNativeMutation(t *testing.T) {
	for _, status := range []int{401, 403, 429, 503} {
		for _, shape := range []string{"error", "detail"} {
			t.Run(fmt.Sprintf("%d/%s", status, shape), func(t *testing.T) {
				errorBody := `{"error":{"type":"permission_error","code":"subscription_sharing_usage_limit_exceeded","param":"scope","message":"direct permission rejected","extra_detail":"retained"}}`
				if shape == "detail" {
					errorBody = `{"detail":"The selected grant lacks direct permission"}`
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"req_siwc_failure"}, "Retry-After": []string{"15"}}, Body: io.NopCloser(strings.NewReader(errorBody))}}
				svc, oauth, a := siwcWSFixture(t, upstream)
				svc.accountRepo = oauth.excelAccountRepo
				svc.rateLimitService = NewRateLimitService(oauth.excelAccountRepo, nil, svc.cfg, nil, nil)
				before := siwcHash(a.Credentials)
				body := siwcHTTPBody("gpt-6.1-sol", false)
				rec, c := siwcHTTPTestContext(t, body)
				result, err := svc.Forward(t.Context(), c, a, body)
				require.Error(t, err)
				require.Nil(t, result)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover), "SIWC failure cannot select a different authorization")
				require.Len(t, upstream.requests, 1)
				require.Equal(t, status, rec.Code)
				require.JSONEq(t, errorBody, rec.Body.String())
				require.Equal(t, "req_siwc_failure", rec.Header().Get("x-request-id"))
				require.Equal(t, "15", rec.Header().Get("Retry-After"))
				require.Equal(t, before, siwcHash(a.Credentials))
				require.Equal(t, StatusActive, a.Status)
				require.True(t, a.Schedulable)
				_, _, _, grant, e := oauth.readSIWCRecord(t.Context(), a)
				require.NoError(t, e)
				require.Equal(t, status == 401, grant.Rejected)
			})
		}
	}
}

func TestSIWCGatewayOnlyCompletedIsSuccessAndKeepsUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, ending := range []string{"failed", "incomplete", "EOF", "DONE", "response.done", "error"} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, ending), func(t *testing.T) {
				events := []string{`{"type":"response.created","response":{"id":"resp_partial","status":"in_progress","usage":{"input_tokens":7,"output_tokens":2}}}`, `{"type":"response.output_text.delta","delta":"visible partial"}`}
				switch ending {
				case "failed":
					events = append(events, `{"type":"response.failed","response":{"id":"resp_partial","status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","type":"rate_limit_error","message":"quota exhausted","param":"model"},"usage":{"input_tokens":7,"output_tokens":2}}}`)
				case "incomplete":
					events = append(events, `{"type":"response.incomplete","response":{"id":"resp_partial","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":7,"output_tokens":2}}}`)
				case "DONE":
					events = append(events, "[DONE]")
				case "response.done":
					events = append(events, `{"type":"response.done","response":{"id":"resp_partial","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":2}}}`)
				case "error":
					events = append(events, `{"type":"error","error":{"code":"subscription_sharing_usage_unavailable","type":"server_error","message":"usage unavailable","param":"scope"}}`)
				}
				upstream := &httpUpstreamRecorder{resp: siwcWSResponse(events...)}
				svc, _, a := siwcWSFixture(t, upstream)
				body := siwcHTTPBody("gpt-6.1-sol", stream)
				rec, c := siwcHTTPTestContext(t, body)
				result, err := svc.Forward(t.Context(), c, a, body)
				require.Error(t, err)
				require.Len(t, upstream.requests, 1)
				var failover *UpstreamFailoverError
				require.False(t, errors.As(err, &failover))
				require.NotNil(t, result, "partial usage must survive every non-completed terminal")
				require.Equal(t, 7, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				if stream {
					require.Contains(t, rec.Body.String(), "visible partial")
					require.NotContains(t, rec.Body.String(), `"type":"response.completed"`)
				} else {
					require.GreaterOrEqual(t, rec.Code, 400)
				}
			})
		}
	}
}

func TestSIWCGatewayMissingGrantDoesNotUseNative(t *testing.T) {
	oauth, _, _, a := siwcFixture(t)
	upstream := &httpUpstreamRecorder{}
	svc := &OpenAIGatewayService{httpUpstream: upstream, openAITokenProvider: NewOpenAITokenProvider(nil, nil, oauth)}
	body := siwcHTTPBody("gpt-6.1-sol", true)
	rec, c := siwcHTTPTestContext(t, body)
	result, err := svc.Forward(t.Context(), c, a, body)
	require.Error(t, err)
	require.Nil(t, result)
	require.Empty(t, upstream.requests)
	require.GreaterOrEqual(t, rec.Code, 400)
	require.Equal(t, "native-secret", a.GetCredential("access_token"))
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover))
}

func TestSIWCGatewayNonStreamingFirstOutputDeadlineClosesBody(t *testing.T) {
	pr, pw := io.Pipe()
	body := &firstOutputCloseTrackingBody{ReadCloser: pr, closed: make(chan struct{})}
	t.Cleanup(func() { _ = body.Close(); _ = pw.Close() })
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}}
	svc, _, a := siwcWSFixture(t, upstream)
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	input := siwcHTTPBody("gpt-6.1-sol", false)
	rec, c := siwcHTTPTestContext(t, input)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_waiting\",\"status\":\"in_progress\"}}\n\n"))
		<-body.closed
		_ = pw.Close()
	}()
	done := make(chan error, 1)
	started := time.Now()
	go func() { _, e := svc.Forward(ctx, c, a, input); done <- e }()
	select {
	case err := <-done:
		require.Error(t, err)
		var failover *UpstreamFailoverError
		require.False(t, errors.As(err, &failover))
	case <-time.After(3 * time.Second):
		t.Fatal("non-streaming SIWC SSE did not observe first-output deadline")
	}
	require.Less(t, time.Since(started), 3*time.Second)
	require.Len(t, upstream.requests, 1)
	require.GreaterOrEqual(t, rec.Code, 400)
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("upstream reader/writer did not exit")
	}
}

func TestSIWCNormalizerPublicContract(t *testing.T) {
	t.Run("string input array and required fields", func(t *testing.T) {
		b, e := normalizeSIWCBody([]byte(`{"model":"exact-slug","input":"hello","stream":false,"store":true,"temperature":0.7,"max_output_tokens":50,"metadata":{"user":"private"}}`))
		require.NoError(t, e)
		require.Equal(t, "hello", gjson.GetBytes(b, "input.0.content").String())
		require.Equal(t, "user", gjson.GetBytes(b, "input.0.role").String())
		require.True(t, gjson.GetBytes(b, "stream").Bool())
		require.False(t, gjson.GetBytes(b, "store").Bool())
		for _, k := range []string{"temperature", "max_output_tokens", "metadata"} {
			require.False(t, gjson.GetBytes(b, k).Exists())
		}
	})
	t.Run("array system role precision and additional tools", func(t *testing.T) {
		raw := `{"input":[{"type":"message","role":"system","content":"system guidance"},{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png","detail":"high"}]}],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"n":{"type":"integer","maximum":9007199254740993}}}}],"tool_choice":{"type":"function","name":"lookup"}}`
		b, e := normalizeSIWCBody([]byte(raw))
		require.NoError(t, e)
		require.Equal(t, "additional_tools", gjson.GetBytes(b, "input.0.type").String())
		require.Equal(t, "developer", gjson.GetBytes(b, "input.0.role").String())
		require.Equal(t, "lookup", gjson.GetBytes(b, "input.0.tools.0.name").String())
		require.Contains(t, string(b), "9007199254740993")
		require.Equal(t, "developer", gjson.GetBytes(b, "input.1.role").String())
		require.Equal(t, "high", gjson.GetBytes(b, "input.2.content.0.detail").String())
		require.Equal(t, "lookup", gjson.GetBytes(b, "tool_choice.name").String())
		again, e := normalizeSIWCBody(b)
		require.NoError(t, e)
		require.JSONEq(t, string(b), string(again))
	})
	t.Run("existing namespace and custom tool names unchanged", func(t *testing.T) {
		raw := `{"input":[{"type":"function_call","name":"lookup","namespace":"tools","call_id":"c1","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"42"},{"type":"additional_tools","role":"developer","tools":[{"type":"custom","name":"exec","description":"run"}]}],"tools":[{"type":"namespace","name":"tools","tools":[{"type":"function","name":"lookup"}]}]}`
		b, e := normalizeSIWCBody([]byte(raw))
		require.NoError(t, e)
		require.Equal(t, "tools", gjson.GetBytes(b, "tools.0.name").String())
		require.Equal(t, "tools", gjson.GetBytes(b, "input.0.namespace").String())
		require.Equal(t, "exec", gjson.GetBytes(b, "input.2.tools.0.name").String())
	})
	for _, field := range []string{"conversation", "prompt"} {
		t.Run("reject opaque context "+field, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"input": "continue", field: map[string]any{"id": "stored_context"}})
			require.NoError(t, err)
			_, err = normalizeSIWCBody(raw)
			require.Error(t, err, "SIWC must not silently discard context referenced by the caller")
		})
	}
	for _, raw := range []string{`{"previous_response_id":"resp_old","input":[]}`, `{"input":42}`, `{"input":[42]}`, `{"input":[]}{}`, `{"input":[{"type":"additional_tools","role":"user","tools":[]}]}`, `{"input":[{"role":"user","content":[{"type":"input_audio","data":"abc"}]}]}`, `{"input":[{"type":"input_video"}]}`, `{"input":[{"type":"tool_search_output"}]}`} {
		t.Run("reject "+raw, func(t *testing.T) { _, e := normalizeSIWCBody([]byte(raw)); require.Error(t, e) })
	}
	for _, kind := range []string{"image_generation", "file_search", "code_interpreter", "computer_use_preview", "mcp", "tool_search", "programmatic_tool_calling"} {
		t.Run("reject hosted "+kind, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"input": []any{}, "tools": []any{map[string]any{"type": kind}}})
			_, e := normalizeSIWCBody(raw)
			require.Error(t, e)
		})
	}
}

func TestSIWCGatewayCompatibilityProtocols(t *testing.T) {
	for _, anthropic := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			for _, outcome := range []string{"complete", "failed", "http detail"} {
				t.Run(fmt.Sprintf("anthropic=%t/stream=%t/%s", anthropic, stream, outcome), func(t *testing.T) {
					events := []string{
						`{"type":"response.created","response":{"id":"resp_compat_siwc","object":"response","status":"in_progress","model":"siwc-upstream-exact","output":[]}}`,
						`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_compat","type":"message","role":"assistant","status":"in_progress","content":[]}}`,
						`{"type":"response.content_part.added","output_index":0,"content_index":0,"item_id":"msg_compat","part":{"type":"output_text","text":""}}`,
						`{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_compat","delta":"hello compat"}`,
					}
					if outcome == "complete" {
						events = append(events, `{"type":"response.completed","response":{"id":"resp_compat_siwc","object":"response","status":"completed","model":"siwc-upstream-exact","output":[{"id":"msg_compat","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello compat"}]}],"usage":{"input_tokens":7,"output_tokens":2}}}`)
					} else {
						events = append(events, `{"type":"response.failed","response":{"id":"resp_compat_siwc","status":"failed","error":{"type":"rate_limit_error","code":"subscription_sharing_usage_limit_exceeded","param":"model","message":"SIWC usage exhausted"},"usage":{"input_tokens":7,"output_tokens":2}}}`)
					}
					upstream := &httpUpstreamRecorder{resp: siwcWSResponse(events...)}
					if outcome == "http detail" {
						upstream.resp = &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"15"}, "X-Request-Id": []string{"req_siwc_compat"}}, Body: io.NopCloser(strings.NewReader(`{"detail":"SIWC direct permission is required"}`))}
					}
					svc, _, a := siwcWSFixture(t, upstream)
					a.Credentials["model_mapping"] = map[string]any{"channel-model": "siwc-upstream-exact", "siwc-upstream-exact": "must-not-map-twice"}
					body := []byte(fmt.Sprintf(`{"model":"visible-model","messages":[{"role":"user","content":"hello"}],"max_tokens":100,"stream":%t,"stream_options":{"include_usage":true}}`, stream))
					rec, c := siwcHTTPTestContext(t, body)
					c.Request.URL.Path = "/v1/chat/completions"
					if anthropic {
						c.Request.URL.Path = "/v1/messages"
					}
					var result *OpenAIForwardResult
					var err error
					if anthropic {
						result, err = svc.ForwardAsAnthropic(t.Context(), c, a, body, "cache-id", "channel-model")
					} else {
						result, err = svc.ForwardAsChatCompletions(t.Context(), c, a, body, "cache-id", "channel-model")
					}
					require.Len(t, upstream.requests, 1, "one compatibility request must make only one upstream generation")
					require.Equal(t, "https://api.openai.com/v1/responses", upstream.lastReq.URL.String())
					require.Equal(t, "Bearer "+siwcOpaqueToken().AccessToken, upstream.lastReq.Header.Get("Authorization"))
					require.Empty(t, upstream.lastReq.Header.Get("Cookie"))
					require.Empty(t, upstream.lastReq.Header.Get("ChatGPT-Account-ID"))
					require.Empty(t, upstream.lastReq.Header.Get("Originator"))
					require.Equal(t, "siwc-upstream-exact", gjson.GetBytes(upstream.lastBody, "model").String())
					require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
					require.False(t, gjson.GetBytes(upstream.lastBody, "store").Bool())
					if outcome == "http detail" {
						require.Error(t, err)
						require.Nil(t, result)
						require.Equal(t, 403, rec.Code)
						assert.Equal(t, "SIWC direct permission is required", gjson.Get(rec.Body.String(), "error.message").String(), "the adapter must retain SIWC detail rather than replace it with a BPS error")
						assert.Equal(t, "15", rec.Header().Get("Retry-After"))
						assert.Equal(t, "req_siwc_compat", rec.Header().Get("x-request-id"))
						return
					}
					require.NotNil(t, result)
					require.Equal(t, UsageAuthorizationModeSIWC, result.AuthorizationMode)
					require.Equal(t, "visible-model", result.Model)
					require.Equal(t, stream, result.Stream)
					require.Equal(t, 7, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					if outcome == "failed" {
						require.Error(t, err)
						var failover *UpstreamFailoverError
						require.False(t, errors.As(err, &failover))
						assert.Contains(t, rec.Body.String(), "SIWC usage exhausted")
						assert.NotContains(t, rec.Body.String(), "basispoints_upstream_error")
						if !anthropic {
							assert.Contains(t, rec.Body.String(), "subscription_sharing_usage_limit_exceeded")
							assert.Contains(t, rec.Body.String(), `"param":"model"`)
						}
						require.NotContains(t, rec.Body.String(), "[DONE]")
						require.NotContains(t, rec.Body.String(), "message_stop")
						if stream {
							require.Contains(t, rec.Body.String(), "hello compat")
						} else {
							require.GreaterOrEqual(t, rec.Code, 400)
						}
						return
					}
					require.NoError(t, err)
					require.Equal(t, 200, rec.Code)
					require.Contains(t, rec.Body.String(), "hello compat")
					require.Contains(t, rec.Body.String(), "visible-model")
					if stream {
						if anthropic {
							require.Contains(t, rec.Body.String(), "event: message_start")
							require.Contains(t, rec.Body.String(), "content_block_delta")
							require.Contains(t, rec.Body.String(), "event: message_stop")
							require.NotContains(t, rec.Body.String(), "chat.completion.chunk")
						} else {
							require.Contains(t, rec.Body.String(), "chat.completion.chunk")
							require.Contains(t, rec.Body.String(), "[DONE]")
							require.NotContains(t, rec.Body.String(), "event: message_start")
						}
					} else if anthropic {
						require.Equal(t, "message", gjson.Get(rec.Body.String(), "type").String())
						require.Equal(t, "hello compat", gjson.Get(rec.Body.String(), "content.0.text").String())
						require.Equal(t, "end_turn", gjson.Get(rec.Body.String(), "stop_reason").String())
					} else {
						require.Equal(t, "chat.completion", gjson.Get(rec.Body.String(), "object").String())
						require.Equal(t, "hello compat", gjson.Get(rec.Body.String(), "choices.0.message.content").String())
						require.Equal(t, "stop", gjson.Get(rec.Body.String(), "choices.0.finish_reason").String())
					}
				})
			}
		}
	}
}

func TestSIWCGatewayIncrementalObservationUsesActualGrantAndFullHistory(t *testing.T) {
	for _, renew := range []bool{false, true} {
		t.Run(fmt.Sprintf("new_authorization=%t", renew), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				siwcWSResponse(string(incrementalCompleted(incrementalOutput))),
				siwcWSResponse(string(incrementalCompleted(""))),
			}}
			svc, oauth, account := siwcWSFixture(t, upstream)
			originalGrant, err := oauth.SIWCGrantIdentity(t.Context(), account)
			require.NoError(t, err)
			for turn, items := range []string{incrementalUser, incrementalUser + "," + incrementalOutput + "," + incrementalNext} {
				if turn == 1 && renew {
					authorizeSIWC(t, oauth, account)
					changedGrant, err := oauth.SIWCGrantIdentity(t.Context(), account)
					require.NoError(t, err)
					require.NotEqual(t, originalGrant, changedGrant)
				}
				body := incrementalRequest(items, `"store":false,"stream":true`)
				_, c := siwcHTTPTestContext(t, body)
				c.Set("api_key", &APIKey{ID: 7, UserID: 8})
				c.Request.Header.Set("session_id", "siwc-observation-session")
				_, err := svc.Forward(t.Context(), c, account, body)
				require.NoError(t, err)
				timing := openAIForwardTimingFromContext(c)
				require.NotNil(t, timing)
				require.NotNil(t, timing.observation)
				report := timing.observation.report
				require.True(t, report.Candidate)
				require.Equal(t, "unknown", report.StateReady)
				if turn == 1 {
					if renew {
						require.Equal(t, "boundary_changed", report.Reason)
						require.NotEqual(t, "true", report.PrefixMatch)
					} else {
						require.Equal(t, "prefix_match", report.Reason)
						require.True(t, report.Assessable)
						require.Equal(t, 2, report.CandidateItems)
					}
				}
				require.Len(t, upstream.requests, turn+1, "observation must never send a probe")
				require.JSONEq(t, string(body), string(upstream.bodies[turn]), "send full history unchanged")
				require.False(t, gjson.GetBytes(upstream.bodies[turn], "previous_response_id").Exists())
			}
		})
	}
}
