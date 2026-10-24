package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type excelBPSRepairBody struct {
	io.Reader
	closed atomic.Bool
}

func (b *excelBPSRepairBody) Close() error { b.closed.Store(true); return nil }

type excelBPSRepairUpstream struct {
	*httpUpstreamRecorder
	bodies []*excelBPSRepairBody
}

func (u *excelBPSRepairUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	if accountID != 300 || concurrency != 1 {
		return nil, fmt.Errorf("correction changed account scheduling")
	}
	if n := len(u.requests); n > 0 && !u.bodies[n-1].closed.Load() {
		return nil, fmt.Errorf("previous response retained its concurrency lease")
	}
	return u.httpUpstreamRecorder.Do(req, proxy, accountID, concurrency)
}

func excelBPSRepairWire(t *testing.T, id, summary, code string, metadata ...string) string {
	t.Helper()
	extra := "{}"
	if len(metadata) > 0 {
		extra = metadata[0]
	}
	args, err := json.Marshal(map[string]any{"summary": summary, "code": code, "extended_summary": extra, "references": []any{}, "destructive": false})
	require.NoError(t, err)
	event, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{
		"id": "resp_" + id, "status": "completed", "model": "gpt-5.6-sol", "usage": map[string]int{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		"output": []any{map[string]any{"type": "function_call", "name": "run_officejs", "id": "fc_" + id, "call_id": "call_" + id, "arguments": string(args), "status": "completed"}},
	}})
	require.NoError(t, err)
	return "event: response.completed\ndata: " + string(event) + "\n\n"
}

func TestExcelBPSUnknownContinuationPreservesRouteAndUsage(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, corrected := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/corrected=%t", protocol, corrected), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{}
				checked := &excelBPSRepairUpstream{httpUpstreamRecorder: upstream}
				for _, id := range []string{"original", "fixed"} {
					code := `{"name":"unknown","arguments":{}}`
					if id == "fixed" && corrected {
						code = `{"name":"shell","arguments":{"cmd":"ls"}}`
					}
					body := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, id, "Run", code))}
					checked.bodies = append(checked.bodies, body)
					upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body})
				}
				svc := excelBPSGatewayTestService(upstream)
				svc.httpUpstream = checked
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"input":[{"type":"function_call","name":"shell","id":"fc_old","call_id":"old","arguments":"{\"cmd\":\"pwd\"}"},{"type":"function_call_output","call_id":"old","output":"/tmp"},{"role":"user","content":"continue"}],"tools":[{"type":"function","name":"shell"}]}`, protocol != "json"))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				account := excelAccount()
				account.Concurrency = 1
				var result *OpenAIForwardResult
				var err error
				var wire strings.Builder
				if !corrected {
					c.Set(OpsUpstreamErrorsKey, []*OpsUpstreamErrorEvent{{Kind: "http_error", UpstreamStatusCode: 429, SkipMonitoring: true}})
					c.Set(OpsUpstreamStatusCodeKey, 429)
					c.Set(OpsUpstreamErrorMessageKey, "earlier rejection")
					c.Set(OpsSkipPassthroughKey, true)
				}
				if protocol == "ws" {
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-5.6-sol", func(event []byte) error { wire.Write(event); return nil })
				} else {
					result, err = svc.Forward(t.Context(), c, account, body)
					wire.WriteString(rec.Body.String())
				}
				if corrected {
					require.NoError(t, err)
					require.Contains(t, wire.String(), `"name":"shell"`)
					require.NotContains(t, wire.String(), "response.failed")
				} else {
					require.Error(t, err)
					require.Contains(t, wire.String(), "basispoints_protocol_error")
					value, _ := c.Get(OpsUpstreamErrorsKey)
					events, _ := value.([]*OpsUpstreamErrorEvent)
					require.Nil(t, LastOpsUpstreamAttempt(events), "local validation is not an upstream rejection")
					require.True(t, c.GetBool(OpsGatewayResponsesFailedKey))
					snapshot, ok := GetOpsStreamError(c)
					require.True(t, ok)
					require.False(t, snapshot.RequestScoped)
					require.False(t, snapshot.SkipMonitoring)
					require.Zero(t, snapshot.UpstreamStatus)
					require.Len(t, snapshot.UpstreamErrors, 2, "retain the earlier attempt as diagnostic evidence")
				}
				require.Len(t, upstream.requests, 2)
				require.NotNil(t, result)
				require.Equal(t, 20, result.Usage.InputTokens)
				require.Equal(t, 4, result.Usage.OutputTokens)
				for _, req := range upstream.requests {
					require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
				}
				require.Contains(t, string(upstream.bodies[1]), "unknown_client_tool")
				require.Contains(t, string(upstream.bodies[1]), `\"executed\":false`)
				for _, response := range checked.bodies {
					require.True(t, response.closed.Load())
				}
			})
		}
	}
}

func TestExcelBPSToolCorrectionPreservesRouteAndUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		for _, corrected := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%t/corrected=%t", stream, corrected), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{}
				checked := &excelBPSRepairUpstream{httpUpstreamRecorder: upstream}
				attempts := 3
				if corrected {
					attempts = 2
				}
				for i := 0; i < attempts; i++ {
					summary := "Run client tool"
					code := "text(42);"
					if corrected && i == attempts-1 {
						summary = "codex2api.custom/functions.exec"
						code = "text(24);"
					}
					body := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, fmt.Sprint(i), summary, code))}
					checked.bodies = append(checked.bodies, body)
					upstream.responses = append(upstream.responses, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body})
				}
				svc := excelBPSGatewayTestService(upstream)
				svc.httpUpstream = checked
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"reasoning":{"effort":"max"},"input":"test correction","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`, stream))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				account := excelAccount()
				account.Concurrency = 1
				result, err := svc.Forward(context.Background(), c, account, body)
				if corrected {
					require.NoError(t, err)
					require.NotContains(t, rec.Body.String(), "response.failed")
					require.Contains(t, rec.Body.String(), `"input":"text(42);"`)
					require.NotContains(t, rec.Body.String(), "text(24);")
					require.Contains(t, rec.Body.String(), `"name":"exec"`)
					if stream {
						require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.output_item.added"))
						require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.completed"))
					}
				} else {
					require.Error(t, err)
					require.Contains(t, rec.Body.String(), "basispoints_protocol_error")
					require.NotContains(t, rec.Body.String(), "custom_tool_call")
				}
				require.Len(t, upstream.requests, attempts)
				require.NotNil(t, result)
				require.Equal(t, attempts*10, result.Usage.InputTokens)
				require.Equal(t, attempts*2, result.Usage.OutputTokens)
				require.Equal(t, "resp_0", result.ResponseID)
				for i, req := range upstream.requests {
					require.Equal(t, basispoints.ResponsesURL, req.URL.String())
					require.Equal(t, "Bearer test-token", req.Header.Get("Authorization"))
					require.Equal(t, "test-account", req.Header.Get("Chatgpt-Account-Id"))
					require.Equal(t, HTTPUpstreamProfileExcelBPS, HTTPUpstreamProfileFromContext(req.Context()))
					require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
					require.True(t, checked.bodies[i].closed.Load())
					require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(upstream.bodies[i], "model").String())
					require.Equal(t, "xhigh", gjson.GetBytes(upstream.bodies[i], "reasoning_effort").String())
					require.False(t, gjson.GetBytes(upstream.bodies[i], "tools").Exists())
					require.Equal(t, fmt.Sprint(i+1), gjson.GetBytes(upstream.bodies[i], "metadata.agent_iteration").String())
					require.Equal(t, gjson.GetBytes(upstream.bodies[0], "metadata.task_id").String(), gjson.GetBytes(upstream.bodies[i], "metadata.task_id").String())
					if i > 0 {
						require.Contains(t, string(upstream.bodies[i]), "invalid_client_tool_transport")
					}
				}
			})
		}
	}
}

func TestExcelBPSUnknownTargetDoesNotRegenerateVisibleStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{true, false} {
		for _, kind := range []string{"response.output_text.delta", "response.reasoning_summary_text.delta"} {
			t.Run(fmt.Sprintf("stream=%t/%s", stream, kind), func(t *testing.T) {
				prefix := fmt.Sprintf("event: %s\ndata: {\"type\":%q,\"delta\":\"Checking tools.\"}\n\n", kind, kind)
				first := &excelBPSRepairBody{Reader: strings.NewReader(prefix + excelBPSRepairWire(t, "original", "Run", `{"name":"missing","arguments":{}}`))}
				fixed := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, "fixed", "Run", `{"name":"shell","arguments":{"cmd":"pwd"}}`))}
				upstream := &httpUpstreamRecorder{responses: []*http.Response{
					{StatusCode: 200, Header: http.Header{}, Body: first},
					{StatusCode: 200, Header: http.Header{}, Body: fixed},
				}}
				svc := excelBPSGatewayTestService(upstream)
				body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"input":"test","tools":[{"type":"function","name":"shell"}]}`, stream))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				result, err := svc.Forward(context.Background(), c, excelAccount(), body)
				require.NotNil(t, result)
				require.Equal(t, http.StatusOK, rec.Code)
				require.True(t, first.closed.Load())
				require.Equal(t, "resp_original", result.ResponseID)
				if stream {
					require.Error(t, err)
					require.Len(t, upstream.requests, 1)
					require.Contains(t, rec.Body.String(), "event: "+kind)
					require.Contains(t, rec.Body.String(), `"delta":"Checking tools."`)
					require.Contains(t, rec.Body.String(), "basispoints_protocol_error")
					require.Contains(t, rec.Body.String(), "event: response.failed")
					require.NotContains(t, rec.Body.String(), "event: response.completed")
					require.NotContains(t, rec.Body.String(), "function_call_arguments")
					require.Equal(t, 10, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
				} else {
					require.NoError(t, err)
					require.Len(t, upstream.requests, 2)
					require.True(t, fixed.closed.Load())
					require.Equal(t, "completed", gjson.Get(rec.Body.String(), "status").String())
					require.Equal(t, "shell", gjson.Get(rec.Body.String(), "output.0.name").String())
					require.Equal(t, 20, result.Usage.InputTokens)
					require.Equal(t, 4, result.Usage.OutputTokens)
				}
			})
		}
	}
}

func TestExcelBPSToolSearchHistoryAcrossTransports(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, outcome := range []string{"discovered", "unavailable", "pending"} {
			t.Run(protocol+"/"+outcome, func(t *testing.T) {
				wire := excelBPSRepairWire(t, "search", "Call client tool", `{"name":"collaboration.send_message","arguments":{"target":"worker","message":"PRIVATE_MESSAGE"}}`)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": {"req_search"}}, Body: io.NopCloser(strings.NewReader(wire))}}
				svc := excelBPSGatewayTestService(upstream)
				svc.cfg.Gateway.LogUpstreamErrorBody = true
				tools := make([]any, 0, 5)
				for _, name := range []string{"shell", "read_file", "write_file", "list_files", "exec"} {
					tools = append(tools, map[string]any{"type": "function", "name": name})
				}
				input := []any{
					map[string]any{"role": "user", "content": "Continue the task"},
					map[string]any{"type": "function_call", "name": "shell", "call_id": "call_prior", "arguments": `{"cmd":"pwd"}`},
					map[string]any{"type": "function_call_output", "call_id": "call_prior", "output": "/workspace"},
					map[string]any{"type": "tool_search_call", "call_id": "call_search", "execution": "client", "status": "completed", "arguments": map[string]any{"query": "collaboration tools"}},
				}
				if outcome != "pending" {
					loaded := []any{}
					if outcome == "discovered" {
						loaded = append(loaded, map[string]any{"type": "namespace", "name": "collaboration", "tools": []any{map[string]any{"type": "function", "name": "send_message"}}})
					}
					input = append(input, map[string]any{"type": "tool_search_output", "call_id": "call_search", "execution": "client", "status": "completed", "tools": loaded})
				}
				body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": protocol != "json", "tools": tools, "input": input})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				var result *OpenAIForwardResult
				var ws bytes.Buffer
				if protocol == "ws" {
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					BeginOpsStreamTurn(c, 1)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error { _, e := ws.Write(event); return e })
				} else {
					result, err = svc.Forward(t.Context(), c, excelAccount(), body)
				}
				client := rec.Body.String() + ws.String()
				if outcome == "pending" {
					require.Error(t, err)
					require.Empty(t, upstream.requests, "reject genuinely missing search results before forwarding")
					require.Contains(t, client, "basispoints_request_invalid")
					require.Contains(t, client, "tool_search_output")
					return
				}
				attempts := 1
				if outcome == "unavailable" {
					attempts = 2
				}
				require.Len(t, upstream.requests, attempts, "allow one bounded continuation, never regenerate history or switch accounts")
				for _, item := range gjson.GetBytes(upstream.bodies[0], "input").Array() {
					require.NotEqual(t, "tool_search_call", item.Get("type").String())
					require.NotEqual(t, "tool_search_output", item.Get("type").String())
				}
				require.Contains(t, string(upstream.bodies[0]), "/workspace")
				require.NotNil(t, result)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				if outcome == "discovered" {
					require.NoError(t, err)
					require.Contains(t, client, `"namespace":"collaboration"`)
					require.Contains(t, client, `"name":"send_message"`)
					require.NotContains(t, client, "basispoints_protocol_error")
				} else {
					require.Error(t, err)
					require.Contains(t, client, "basispoints_protocol_error")
					require.NotContains(t, client, "PRIVATE_MESSAGE")
					require.NotContains(t, client, `"name":"send_message"`)
					snapshot, ok := GetOpsStreamError(c)
					require.True(t, ok)
					require.Zero(t, snapshot.UpstreamStatus)
					require.False(t, snapshot.RequestScoped, "converter errors remain SLA failures")
					require.Len(t, snapshot.UpstreamErrors, 1)
					require.Equal(t, http.StatusOK, snapshot.UpstreamErrors[0].UpstreamStatusCode)
					require.Equal(t, "bridge_error", snapshot.UpstreamErrors[0].Kind)
					detail := gjson.Get(snapshot.UpstreamErrors[0].Detail, "error.param").String()
					require.Equal(t, "collaboration.send_message", gjson.Get(detail, "rejected_tool").String())
					require.Equal(t, int64(5), gjson.Get(detail, "catalog_tool_count").Int())
					require.True(t, gjson.Get(detail, "has_tool_history").Bool())
					require.False(t, gjson.Get(detail, "visible_output").Bool())
					require.True(t, gjson.Get(detail, "regeneration_attempted").Bool())
				}
			})
		}
	}
}

func TestExcelBPSCatalogResolutionAndFailureDiagnosticsAcrossTransports(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, tc := range []struct {
			target         string
			logging, valid bool
		}{
			{"functions.shell", true, true}, {"missing_tool", true, false},
			{"test-token", true, false}, {"missing_tool", false, false},
			{"private\ncontent", true, false}, {strings.Repeat("x", 257), true, false},
		} {
			t.Run(fmt.Sprintf("%s/%s/logging=%t", protocol, tc.target, tc.logging), func(t *testing.T) {
				code, err := json.Marshal(map[string]any{"name": tc.target, "arguments": map[string]string{"cmd": "PRIVATE_TOOL_ARGUMENTS"}})
				require.NoError(t, err)
				wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Working.\"}\n\n" + excelBPSRepairWire(t, "catalog", "Run client tool", string(code))
				wire = strings.ReplaceAll(wire, "gpt-5.6-sol", "gpt-5.6-luna")
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": {"req_catalog"}}, Body: io.NopCloser(strings.NewReader(wire))}}
				svc := excelBPSGatewayTestService(upstream)
				svc.cfg.Gateway.LogUpstreamErrorBody = tc.logging
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"tools":[{"type":"function","name":"shell"}],"input":[{"role":"user","content":"first"},{"type":"function_call","name":"shell","call_id":"prior","arguments":"{}"},{"type":"function_call_output","call_id":"prior","output":"done"},{"role":"user","content":"continue"}]}`, protocol != "json"))
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				var result *OpenAIForwardResult
				var ws bytes.Buffer
				if protocol == "ws" {
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					BeginOpsStreamTurn(c, 1)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error { _, e := ws.Write(event); return e })
				} else {
					result, err = svc.Forward(t.Context(), c, excelAccount(), body)
				}
				require.NotNil(t, result)
				attempts := 1
				if protocol == "json" && !tc.valid {
					attempts = 2
				}
				require.Len(t, upstream.requests, attempts, "visible output prevents correction; buffered continuations are bounded")
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(upstream.bodies[0], "model").String())
				require.Equal(t, "gpt-6-astra", result.UpstreamModel)
				require.Equal(t, "gpt-5.6-luna", result.UpstreamResponseModel, "retain the reported model instead of masking a mismatch")
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				client := rec.Body.String() + ws.String()
				if tc.valid {
					require.NoError(t, err)
					require.Contains(t, client, `"name":"shell"`)
					require.NotContains(t, client, "basispoints_protocol_error")
					return
				}
				require.Error(t, err)
				require.NotContains(t, client, tc.target)
				require.NotContains(t, client, "PRIVATE_TOOL_ARGUMENTS")
				snapshot, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Equal(t, http.StatusBadGateway, snapshot.IntendedStatus)
				require.Zero(t, snapshot.UpstreamStatus, "the terminal error belongs to the converter")
				require.Len(t, snapshot.UpstreamErrors, 1)
				require.Equal(t, http.StatusOK, snapshot.UpstreamErrors[0].UpstreamStatusCode, "retain real HTTP acceptance in the timeline")
				require.Equal(t, "bridge_error", snapshot.UpstreamErrors[0].Kind)
				require.Nil(t, LastOpsUpstreamAttempt(snapshot.UpstreamErrors))
				require.Equal(t, "req_catalog", snapshot.UpstreamErrors[0].UpstreamRequestID)
				diagnostic := snapshot.UpstreamErrors[0].Detail
				if !tc.logging {
					require.Empty(t, diagnostic)
					return
				}
				detail := gjson.Get(diagnostic, "error.param").String()
				require.Equal(t, "gateway_tool_catalog", gjson.Get(detail, "source").String())
				require.Equal(t, true, gjson.Get(detail, "has_tool_history").Bool())
				require.Equal(t, protocol != "json", gjson.Get(detail, "visible_output").Bool())
				require.Equal(t, protocol == "json", gjson.Get(detail, "regeneration_attempted").Bool())
				require.Equal(t, "gpt-5.6-luna", gjson.Get(detail, "response_model").String())
				require.NotContains(t, diagnostic, "PRIVATE_TOOL_ARGUMENTS")
				if tc.target == "test-token" {
					require.NotContains(t, diagnostic, "test-token")
				} else if strings.Contains(tc.target, "\n") {
					require.Equal(t, "[invalid identifier]", gjson.Get(detail, "rejected_tool").String())
					require.NotContains(t, diagnostic, "private")
				} else if len(tc.target) > 256 {
					require.Equal(t, "[unavailable]", gjson.Get(detail, "rejected_tool").String())
					require.NotContains(t, diagnostic, tc.target)
				} else {
					require.Equal(t, tc.target, gjson.Get(detail, "rejected_tool").String())
				}
			})
		}
	}
}

func TestExcelBPSToolCorrectionStopsOnHTTPRejection(t *testing.T) {
	for _, status := range []int{403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			first := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, "http_rejection", "Run", "text(42);"))}
			rejected := &excelBPSRepairBody{Reader: strings.NewReader(`{"error":{"message":"private echoed request text"}}`)}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: 200, Header: http.Header{}, Body: first},
				{StatusCode: status, Header: http.Header{}, Body: rejected},
			}}
			svc := excelBPSGatewayTestService(upstream)
			body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":"test","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			result, err := svc.Forward(context.Background(), c, excelAccount(), body)
			require.Error(t, err)
			require.Len(t, upstream.requests, 2)
			require.True(t, first.closed.Load())
			require.True(t, rejected.closed.Load())
			require.Equal(t, 10, result.Usage.InputTokens)
			require.Contains(t, rec.Body.String(), fmt.Sprintf("HTTP %d", status))
			require.NotContains(t, rec.Body.String(), "private echoed")
			require.NotContains(t, rec.Body.String(), "response.output_item.added")
		})
	}
}

func TestExcelBPS429CorrectionDoesNotChangeCodexState(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			first := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSRepairWire(t, "rate_limit", "Run", "text(42);"))}
			rejected := &excelBPSRepairBody{Reader: strings.NewReader(`{"error":{"type":"usage_limit_reached","resets_in_seconds":7200,"message":"PRIVATE_UPSTREAM"}}`)}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: http.StatusOK, Header: http.Header{}, Body: first},
				{StatusCode: http.StatusTooManyRequests, Header: excelBPSQuotaHeaders("100", "100"), Body: rejected},
			}}
			svc := excelBPSGatewayTestService(upstream)
			repo := &excelBPSQuotaRepo{writes: make(chan excelBPSQuotaWrite, 4)}
			svc.accountRepo = repo
			svc.rateLimitService = NewRateLimitService(repo, nil, svc.cfg, nil, nil)
			svc.rateLimitService.SetAccountRuntimeBlocker(svc)
			account := excelBPSNativeFallbackTestAccount(t, svc)
			body := []byte(fmt.Sprintf(`{"model":"gpt-5.6-sol","stream":%t,"input":"test","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`, stream))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))

			result, err := svc.Forward(context.Background(), c, account, body)

			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.NotErrorAs(t, err, &failover)
			require.Len(t, upstream.requests, 2, "stop after the throttled correction")
			require.True(t, first.closed.Load())
			require.True(t, rejected.closed.Load())
			require.Equal(t, 10, result.Usage.InputTokens)
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
			require.NotContains(t, rec.Body.String(), "response.output_item.added")
			requireNoExcelBPSQuotaWrite(t, repo)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			require.True(t, account.IsSchedulable())
		})
	}
}

func TestExcelBPSMixedToolCorrectionKeepsOriginalOperation(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			wire := func(corrected bool) string {
				summary, code, id := "Run client tool", "text(42);", "initial"
				command := "pwd"
				if corrected {
					summary, id, command = "codex2api.custom/functions.exec", "corrected", "changed command"
				}
				raw := excelBPSRepairWire(t, id, summary, code)
				var event map[string]any
				require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(raw, "data: ", 2)[1])), &event))
				response, ok := event["response"].(map[string]any)
				require.True(t, ok)
				output, ok := response["output"].([]any)
				require.True(t, ok)
				envelope, err := json.Marshal(map[string]any{"name": "shell", "arguments": map[string]any{"cmd": command}})
				require.NoError(t, err)
				args, err := json.Marshal(map[string]any{"code": string(envelope), "summary": "Inspect directory", "extended_summary": "Inspect current directory", "references": []any{}, "destructive": false})
				require.NoError(t, err)
				response["output"] = append(output, map[string]any{"type": "function_call", "name": "run_officejs", "id": "fc_shell_" + id, "call_id": "shell_" + id, "arguments": string(args)})
				encoded, err := json.Marshal(event)
				require.NoError(t, err)
				return "event: response.completed\ndata: " + string(encoded) + "\n\n"
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire(false)))},
				{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire(true)))},
			}}
			svc := excelBPSGatewayTestService(upstream)
			body := []byte(fmt.Sprintf("{\"model\":\"gpt-5.5\",\"stream\":%t,\"input\":\"inspect\",\"tools\":[{\"type\":\"function\",\"name\":\"shell\",\"parameters\":{\"type\":\"object\",\"properties\":{\"cmd\":{\"type\":\"string\"}}}},{\"type\":\"namespace\",\"name\":\"functions\",\"tools\":[{\"type\":\"custom\",\"name\":\"exec\"}]}]}", stream))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			account := excelAccount()
			account.Credentials["model_mapping"] = map[string]any{"gpt-5.5": "gpt-5.6-sol"}
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 2)
			require.Equal(t, 20, result.Usage.InputTokens)
			require.Contains(t, rec.Body.String(), "pwd")
			require.Contains(t, rec.Body.String(), "text(42);")
			require.NotContains(t, rec.Body.String(), "changed command")
			require.NotContains(t, rec.Body.String(), "response.failed")
			for _, sent := range upstream.bodies {
				require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(sent, "model").String())
			}
			if stream {
				require.Equal(t, 2, strings.Count(rec.Body.String(), "event: response.output_item.added"))
				require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.completed"))
			}
		})
	}
}

func TestExcelBPSToolArgumentCorrectionAcrossTransports(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws", "chat", "chat-stream"} {
		for _, tc := range []struct {
			kind    string
			changed bool
		}{
			{"arguments", false}, {"arguments", true},
			{"raw_code", false}, {"raw_code", true},
			{"fenced_arguments", false}, {"fenced_arguments", true},
			{"fenced_code_arguments", false}, {"fenced_code_arguments", true},
			{"missing_separator", false}, {"missing_separator", true},
			{"missing_separator_code", false}, {"missing_separator_code", true},
			{"unverifiable_code", true},
		} {
			rawCode, changed := tc.kind == "raw_code", tc.changed
			codeFunction := rawCode || tc.kind == "fenced_code_arguments" || tc.kind == "missing_separator_code" || tc.kind == "unverifiable_code"
			t.Run(fmt.Sprintf("%s/%s/changed_metadata=%t", protocol, tc.kind, changed), func(t *testing.T) {
				rawArgs := map[string]any{"cmd": "pwd", "workdir": "/private-original", "limit": json.Number("9007199254740993")}
				name, summary, field := "shell", "Run command", "cmd"
				const rawOperation = "text('private-original-code $value');\n"
				if codeFunction {
					name, field = "run_script", "code"
					delete(rawArgs, "cmd")
					rawArgs["code"] = rawOperation
				}
				if rawCode {
					summary = "codex2api.function_code/run_script"
				}
				original, err := json.Marshal(rawArgs)
				require.NoError(t, err)
				metadata := "{}"
				if strings.HasPrefix(tc.kind, "fenced_") {
					original = []byte("```json\n" + string(original) + "\n```")
				}
				if strings.HasPrefix(tc.kind, "missing_separator") {
					original = []byte(strings.Replace(string(original), `,"workdir":`, ` "workdir":`, 1))
				}
				if tc.kind == "unverifiable_code" {
					original = []byte(`{"code":"text("private-original-code")","workdir":"/private-original","limit":9007199254740993}`)
				}
				if rawCode {
					// Duplicate original code is invalid framing, with a fully known operation.
					metadata, original = string(original), []byte(rawOperation)
					rawArgs["code"] = "private-rewritten-code"
				}
				if changed {
					rawArgs["workdir"] = "/private-changed"
				}
				envelope, err := json.Marshal(map[string]any{"name": name, "arguments": rawArgs})
				require.NoError(t, err)
				first := bpsCompletionResponse(200, excelBPSRepairWire(t, "initial", summary, string(original), metadata))
				fixed := bpsCompletionResponse(200, excelBPSRepairWire(t, "fixed", "Run command", string(envelope)))
				first.Header.Set("X-Request-Id", "req_original")
				fixed.Header.Set("X-Request-Id", "req_correction")
				upstream := &httpUpstreamRecorder{responses: []*http.Response{first, fixed}}
				svc := excelBPSGatewayTestService(upstream)
				svc.cfg.Gateway.LogUpstreamErrorBody = true
				schema := map[string]any{"type": "object", "properties": map[string]any{field: map[string]any{"type": "string"}, "workdir": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"}}}
				stream := protocol == "sse" || protocol == "ws" || protocol == "chat-stream"
				source := map[string]any{"model": "gpt-6-astra", "stream": stream, "input": "test", "tools": []any{map[string]any{"type": "function", "name": name, "parameters": schema}}}
				chat := strings.HasPrefix(protocol, "chat")
				path := "/v1/responses"
				if chat {
					path = "/v1/chat/completions"
					delete(source, "input")
					source["messages"] = []any{map[string]any{"role": "user", "content": "test"}}
					source["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": name, "parameters": schema}}}
				}
				body, err := json.Marshal(source)
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				c.Request.Header.Set("session_id", t.Name())
				var result *OpenAIForwardResult
				var events strings.Builder
				account := excelAccount()
				switch {
				case protocol == "ws":
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, account, body, "gpt-6-astra", func(raw []byte) error { events.Write(raw); return nil })
				case chat:
					result, err = svc.forwardOpenAIGrantCompatibility(t.Context(), c, account, body, "", "gpt-6-astra", false)
				default:
					result, err = svc.Forward(t.Context(), c, account, body)
				}
				output := rec.Body.String() + events.String()
				if changed {
					require.Error(t, err)
					var retry *UpstreamFailoverError
					require.NotErrorAs(t, err, &retry)
					value, ok := c.Get(OpsUpstreamErrorsKey)
					require.True(t, ok)
					failures := value.([]*OpsUpstreamErrorEvent)
					require.Len(t, failures, 1)
					require.Equal(t, "req_correction", failures[0].UpstreamRequestID)
					reason := "arguments_changed"
					if rawCode {
						reason = "metadata_changed"
					}
					if tc.kind == "unverifiable_code" {
						reason = "original_payload_unverifiable"
						require.Contains(t, failures[0].Message, "could not be verified")
					}
					require.Contains(t, failures[0].Detail, reason)
					require.NotContains(t, failures[0].Detail, "private-")
					require.NotContains(t, output, "private-")
					require.NotContains(t, output, "tool_calls")
					require.NotContains(t, output, "response.output_item.added")
				} else {
					require.NoError(t, err)
					require.Contains(t, output, name)
					if codeFunction {
						require.NotContains(t, output, "```")
						require.Contains(t, output, "private-original-code")
						require.NotContains(t, output, "private-rewritten-code")
					}
					require.Contains(t, output, "9007199254740993")
				}
				require.Len(t, upstream.requests, 2)
				require.NotNil(t, result)
				require.Equal(t, 20, result.Usage.InputTokens)
				require.Equal(t, 4, result.Usage.OutputTokens)
			})
		}
	}
}
