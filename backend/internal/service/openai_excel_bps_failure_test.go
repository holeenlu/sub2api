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
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func bpsFailureWire(t *testing.T, kind, code, errorType string, status int) string {
	t.Helper()
	detail := map[string]any{"code": code, "type": errorType, "message": "PRIVATE_UPSTREAM_BODY", "param": "PRIVATE_PARAMETER"}
	event := map[string]any{"type": kind}
	if status != 0 {
		event["status"] = status
	}
	if kind == "error" {
		event["error"] = detail
	} else {
		event["response"] = map[string]any{
			"id": "resp_failure", "status": strings.TrimPrefix(kind, "response."), "error": detail,
			"output": []any{}, "usage": map[string]any{"input_tokens": 7, "output_tokens": 2},
		}
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	return "event: " + kind + "\ndata: " + string(raw) + "\n\n"
}

func TestExcelBPSFailureAfterCompactKeepalive(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK,
		bpsFailureWire(t, "response.failed", "rate_limit_exceeded", "", 0))}
	svc := excelBPSGatewayTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	MarkOpenAICompactClientStream(c)
	stop := StartOpenAICompactSSEKeepalive(c, time.Hour)
	defer stop()
	value, ok := c.Get(openAICompactSSEKeepaliveKey)
	require.True(t, ok)
	keepalive, ok := value.(*openAICompactSSEKeepalive)
	require.True(t, ok)
	require.True(t, keepalive.beat())
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "continue"})
	require.NoError(t, err)
	result, err := svc.Forward(context.Background(), c, excelAccount(), body)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, ExcelBPSRateLimitedReason, failover.Reason)
	require.True(t, failover.SafeToFailoverAfterWrite, "keepalive is not semantic output")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotContains(t, rec.Body.String(), "event: response.failed\n", "leave terminal delivery to the handler after recovery")
	require.NotContains(t, rec.Body.String(), "PRIVATE_")
	require.Equal(t, 7, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Len(t, upstream.requests, 1)
}

// Return final bytes and a transport error together, as a truncated HTTP body can.
type bpsEOFBody struct{ *strings.Reader }

func (r *bpsEOFBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if r.Len() == 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func TestExcelBPSInterruptedStreamKeepsSequenceAndDiagnostics(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, tc := range []struct {
			name, kind, last, reason string
			noID, readError, visible bool
		}{
			{name: "empty body", noID: true, last: "none", reason: "missing_terminal"},
			{name: "metadata only", last: "response.created", reason: "missing_terminal"},
			{name: "text then clean EOF", kind: "response.output_text.delta", last: "response.output_text.delta", reason: "missing_terminal", visible: true},
			{name: "text then transport EOF", kind: "response.output_text.delta", last: "response.output_text.delta", reason: "read_error", visible: true, readError: true},
			{name: "unknown event privacy", kind: "PRIVATE_EVENT_TYPE", last: "other", reason: "missing_terminal", visible: true},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				wire := ""
				if !tc.noID {
					wire = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_interrupted\",\"usage\":{\"input_tokens\":7}}}\n\n"
				}
				if tc.kind != "" {
					payload, err := json.Marshal(map[string]any{"type": tc.kind, "delta": "PRIVATE_PARTIAL_TEXT"})
					require.NoError(t, err)
					wire += "data: " + string(payload) + "\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, wire)}
				upstream.resp.Header.Set("X-Request-Id", "req_interrupted")
				if tc.readError {
					upstream.resp.Body = io.NopCloser(&bpsEOFBody{strings.NewReader(wire)})
				}
				svc := excelBPSGatewayTestService(upstream)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "PRIVATE_PROMPT", "stream": protocol != "json"})
				require.NoError(t, err)
				var result *OpenAIForwardResult
				var events [][]byte
				if protocol == "ws" {
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error {
						events = append(events, append([]byte(nil), event...))
						return nil
					})
				} else {
					result, err = svc.Forward(t.Context(), c, excelAccount(), body)
					for _, line := range strings.Split(rec.Body.String(), "\n") {
						if strings.HasPrefix(line, "data: ") {
							events = append(events, []byte(strings.TrimPrefix(line, "data: ")))
						}
					}
				}
				require.Error(t, err)
				var failover *UpstreamFailoverError
				if tc.noID {
					require.ErrorAs(t, err, &failover)
					require.False(t, c.Writer.Written())
					require.Empty(t, events)
					require.False(t, failover.RequestNotSent, "missing output is not evidence of an unsent request")
					return
				}
				require.NotErrorAs(t, err, &failover)
				require.Len(t, upstream.requests, 1, "an uncertain accepted generation must not be replayed")
				require.Equal(t, !tc.noID, result.ResponseID != "")
				if !tc.noID {
					require.Equal(t, "resp_interrupted", result.ResponseID)
					require.Equal(t, 7, result.Usage.InputTokens)
				}
				require.False(t, result.SucceededForScheduling())
				sequence := int64(-1)
				for _, event := range events {
					next := gjson.GetBytes(event, "sequence_number").Int()
					require.Greater(t, next, sequence, "synthetic failure must continue the delivered stream sequence")
					sequence = next
				}
				if protocol != "json" {
					require.NotEmpty(t, events)
					last := events[len(events)-1]
					require.Contains(t, string(last), "basispoints_stream_incomplete")
					if tc.noID {
						require.Equal(t, "error", gjson.GetBytes(last, "type").String())
						require.False(t, gjson.GetBytes(last, "response.id").Exists())
					}
				}
				snapshot, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Len(t, snapshot.UpstreamErrors, 1)
				diagnostic := snapshot.UpstreamErrors[0]
				require.Equal(t, http.StatusOK, diagnostic.UpstreamStatusCode, "the gateway's 502 is not the upstream HTTP status")
				require.Equal(t, "req_interrupted", diagnostic.UpstreamRequestID)
				require.Equal(t, opsProxyNameDirect, diagnostic.ProxyName)
				require.Equal(t, tc.last, gjson.Get(diagnostic.Detail, "last_event_type").String())
				require.Equal(t, tc.reason, gjson.Get(diagnostic.Detail, "stream_end_reason").String())
				require.Equal(t, tc.visible && protocol != "json", gjson.Get(diagnostic.Detail, "content_committed").Bool())
				require.NotContains(t, diagnostic.Detail, "PRIVATE_")
			})
		}
	}
}

func TestExcelBPSTerminalTailAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failed=%t", protocol, failed), func(t *testing.T) {
				wire := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_tail\"}}\n\n" +
					"data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer\"}\n\n"
				tail := bpsCompletedStream("resp_tail", "answer")
				if failed {
					tail = "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"invalid_value\"}}}"
				}
				wire += strings.TrimRight(tail, "\r\n")
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, "")}
				upstream.resp.Body = io.NopCloser(&bpsEOFBody{strings.NewReader(wire)})
				svc := excelBPSGatewayTestService(upstream)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": "hello", "stream": protocol != "json"})
				require.NoError(t, err)
				var events [][]byte
				var result *OpenAIForwardResult
				if protocol == "ws" {
					SetOpenAIClientTransport(c, OpenAIClientTransportWS)
					result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error {
						events = append(events, append([]byte(nil), event...))
						return nil
					})
				} else {
					result, err = svc.Forward(t.Context(), c, excelAccount(), body)
					for _, line := range strings.Split(rec.Body.String(), "\n") {
						if strings.HasPrefix(line, "data: ") {
							events = append(events, []byte(strings.TrimPrefix(line, "data: ")))
						}
					}
				}
				if failed {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Len(t, upstream.requests, 1)
				require.Equal(t, !failed, result.SucceededForScheduling())
				require.Equal(t, "resp_tail", result.ResponseID)
				lastSequence := int64(-1)
				for _, event := range events {
					require.NotContains(t, string(event), "basispoints_stream_incomplete")
					sequence := gjson.GetBytes(event, "sequence_number").Int()
					require.Greater(t, sequence, lastSequence)
					lastSequence = sequence
				}
				if protocol != "json" {
					require.Len(t, events, 3)
				}
			})
		}
	}
}

func TestExcelBPSFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		name, event, code, errorType string
		explicit, status             int
		wantCode, wantType           string
	}{
		{"invalid input", "response.failed", "invalid_value", "", 0, 400, "invalid_value", "invalid_request_error"},
		{"cyber failed", "response.failed", "cyber_policy", "invalid_request", 0, 400, "cyber_policy", "invalid_request_error"},
		{"cyber error", "error", "cyber_policy", "invalid_request", 0, 400, "cyber_policy", "invalid_request_error"},
		{"cyber wrapped unavailable", "response.failed", "cyber_policy", "invalid_request", 503, 503, "cyber_policy", "server_error"},
		{"native cyber code spelling", "response.failed", " CYBER_POLICY ", "invalid_request", 503, 503, "basispoints_upstream_error", "server_error"},
		{"cyber wrapped rate limit", "response.failed", "cyber_policy", "invalid_request", 429, 429, "cyber_policy", "rate_limit_error"},
		{"authentication", "response.failed", "invalid_api_key", "", 0, 401, "invalid_api_key", "authentication_error"},
		{"permission", "response.failed", "permission_denied", "", 0, 403, "permission_denied", "permission_error"},
		{"rate limit", "error", "rate_limit_exceeded", "", 0, 429, "rate_limit_exceeded", "rate_limit_error"},
		{"type fallback", "response.failed", "", "overloaded_error", 0, 503, "basispoints_upstream_error", "server_error"},
		{"BPS overloaded", "response.failed", "server_is_overloaded", "service_unavailable_error", 0, 503, "server_is_overloaded", "server_error"},
		{"BPS unavailable type", "response.failed", "", "service_unavailable_error", 0, 503, "basispoints_upstream_error", "server_error"},
		{"BPS explicit rejection", "response.failed", "server_is_overloaded", "service_unavailable_error", 403, 403, "server_is_overloaded", "permission_error"},
		{"explicit status", "response.failed", "invalid_value", "invalid_request_error", 429, 429, "invalid_value", "rate_limit_error"},
		{"cancelled", "response.cancelled", "", "", 0, 502, "basispoints_upstream_cancelled", "server_error"},
		{"cancelled rate limit", "response.cancelled", "rate_limit_exceeded", "", 0, 429, "rate_limit_exceeded", "rate_limit_error"},
		{"unknown", "error", "PRIVATE_ERROR_CODE", "PRIVATE_ERROR_TYPE", 0, 502, "basispoints_upstream_error", "server_error"},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				cyber := strings.EqualFold(strings.TrimSpace(tc.code), "cyber_policy")
				wire := bpsFailureWire(t, tc.event, tc.code, tc.errorType, tc.explicit)
				if stream {
					prefix, err := json.Marshal(map[string]any{"type": "response.output_text.delta", "delta": "already delivered"})
					require.NoError(t, err)
					wire = "data: " + string(prefix) + "\n\n" + wire
				}
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, wire)}
				svc := excelBPSGatewayTestService(upstream)
				account := excelAccount()
				account.Extra["openai_excel_bps_auto_disable_on_403"] = true
				body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "stream": stream, "input": "hello"})
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				result, err := svc.Forward(context.Background(), c, account, body)
				require.Error(t, err)
				require.NotNil(t, result)
				if tc.status == http.StatusTooManyRequests && !stream && !cyber && (tc.event == "error" || tc.event == "response.failed") {
					requireExcelBPSRateLimitFailover(t, err, c)
					require.Len(t, upstream.requests, 1)
					require.Empty(t, result.ResponseID, "rejected response IDs must not reach the client")
					require.True(t, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
					if tc.event != "error" {
						require.Equal(t, 7, result.Usage.InputTokens)
						require.Equal(t, 2, result.Usage.OutputTokens)
					}
					return
				}
				if tc.status == http.StatusServiceUnavailable && !stream && !cyber {
					var failover *UpstreamFailoverError
					require.ErrorAs(t, err, &failover)
					require.False(t, c.Writer.Written())
					require.Len(t, upstream.requests, 1)
					require.Equal(t, 7, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					return
				}
				require.Equal(t, tc.event, result.UpstreamTerminalEvent)
				require.Len(t, upstream.requests, 1, "never replay a request after an upstream terminal")
				require.True(t, IsResponseCommitted(c))
				var failover *UpstreamFailoverError
				require.NotErrorAs(t, err, &failover)
				require.NotContains(t, rec.Body.String(), "PRIVATE_")
				require.NotContains(t, err.Error(), "PRIVATE_")
				require.NotContains(t, rec.Body.String(), "basispoints_stream_incomplete")
				require.NotContains(t, rec.Body.String(), "response.completed")
				if tc.event != "error" {
					require.EqualValues(t, 7, result.Usage.InputTokens)
					require.EqualValues(t, 2, result.Usage.OutputTokens)
				}
				if stream {
					require.Equal(t, http.StatusOK, rec.Code)
					require.Contains(t, rec.Body.String(), "already delivered")
					found := 0
					for _, line := range strings.Split(rec.Body.String(), "\n") {
						if !strings.HasPrefix(line, "data: ") {
							continue
						}
						raw := strings.TrimPrefix(line, "data: ")
						if gjson.Get(raw, "type").String() != tc.event {
							continue
						}
						found++
						path := "error"
						if tc.event != "error" {
							path = "response.error"
						}
						require.Equal(t, tc.wantCode, gjson.Get(raw, path+".code").String())
						require.Equal(t, tc.wantType, gjson.Get(raw, path+".type").String())
						require.EqualValues(t, tc.status, gjson.Get(raw, path+".status").Int())
					}
					require.Equal(t, 1, found)
				} else {
					require.Equal(t, tc.status, rec.Code)
					require.Equal(t, tc.wantCode, gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
					require.Equal(t, tc.wantType, gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
				}
				observed, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Equal(t, tc.status, observed.IntendedStatus)
				require.Equal(t, tc.wantCode, observed.Code)
				require.Equal(t, http.StatusOK, c.GetInt(OpsUpstreamStatusCodeKey), "mapped status is not an upstream HTTP status")
				require.Equal(t, tc.status == 429 && !cyber, svc.isExcelBPSCoolingDown(account, "gpt-6-astra"))
				if cyber {
					mark := GetOpsCyberPolicy(c)
					require.NotNil(t, mark)
					require.Equal(t, 200, mark.UpstreamStatus)
					require.Equal(t, result.Usage.InputTokens, mark.UpstreamInTok)
					require.Equal(t, result.Usage.OutputTokens, mark.UpstreamOutTok)
				} else {
					require.Nil(t, GetOpsCyberPolicy(c))
				}
				require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
				require.True(t, account.IsSchedulable())
				require.True(t, account.IsExcelBPSEnabled(), "an in-band 403 must not disable the account")
			})
		}
	}
}

func TestExcelBPS500RecoverySafetyAndOriginalDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, tools, event, code, errorType string
		stream, retry, implicitStatus               bool
		status                                      int
	}{
		{name: "SSE metadata only", stream: true, retry: true},
		{name: "JSON before response", retry: true},
		{name: "committed text", stream: true, prefix: `{"type":"response.output_text.delta","delta":"already delivered"}`},
		{name: "unknown hosted side effects", stream: true, tools: `,"tools":[{"type":"mcp","server_label":"private"}]`},
		{name: "unknown error", stream: true, code: "UNKNOWN_UPSTREAM_CODE"},
		{name: "cancelled response", stream: true, event: "response.cancelled"},
		{name: "explicit 403 overrides server code", stream: true, status: 403},
		{name: "encrypted rejection overrides 503", stream: true, status: 503, code: "invalid_encrypted_content"},
		{name: "signature rejection overrides 503", stream: true, status: 503, code: "thinking_signature_invalid"},
		{name: "BPS overload metadata only", stream: true, retry: true, implicitStatus: true, code: "server_is_overloaded", errorType: "service_unavailable_error"},
		{name: "BPS overload committed text", stream: true, implicitStatus: true, code: "server_is_overloaded", errorType: "service_unavailable_error", prefix: `{"type":"response.output_text.delta","delta":"already delivered"}`},
		{name: "BPS overload unknown side effects", stream: true, implicitStatus: true, code: "server_is_overloaded", errorType: "service_unavailable_error", tools: `,"tools":[{"type":"mcp","server_label":"private"}]`},
		{name: "BPS overload cancellation", stream: true, implicitStatus: true, code: "server_is_overloaded", errorType: "service_unavailable_error", event: "response.cancelled"},
		{name: "BPS overload explicit 403", stream: true, status: 403, code: "server_is_overloaded", errorType: "service_unavailable_error"},
		{name: "BPS unavailable type fallback", stream: true, retry: true, implicitStatus: true, code: "UNKNOWN_UPSTREAM_CODE", errorType: "service_unavailable_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, event := tc.code, tc.event
			if code == "" {
				code = "server_error"
			}
			if event == "" {
				event = "error"
			}
			wire := "data: " + `{"type":"response.created","response":{"id":"resp_failed","usage":{"input_tokens":7,"output_tokens":2}}}` + "\n\n"
			if tc.prefix != "" {
				wire += "data: " + tc.prefix + "\n\n"
			}
			status := tc.status
			if status == 0 && !tc.implicitStatus {
				status = 500
			}
			errorType := tc.errorType
			if errorType == "" {
				errorType = "server_error"
			}
			wire += bpsFailureWire(t, event, code, errorType, status)
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(200, wire)}
			upstream.resp.Header.Set("X-Request-Id", "bps-request-failed")
			svc := excelBPSGatewayTestService(upstream)
			svc.cfg.Gateway.LogUpstreamErrorBody = true
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":"private prompt","stream":%t%s}`, tc.stream, tc.tools))
			result, err := svc.Forward(t.Context(), c, excelAccount(), body)
			require.Error(t, err)
			require.NotNil(t, result)
			var failover *UpstreamFailoverError
			require.Equal(t, tc.retry, errors.As(err, &failover))
			require.Len(t, upstream.requests, 1, "only the handler may switch accounts")
			require.Equal(t, 7, result.Usage.InputTokens)
			require.Equal(t, 2, result.Usage.OutputTokens)
			require.NotContains(t, rec.Body.String(), "PRIVATE_")
			if tc.retry {
				require.False(t, c.Writer.Written())
				require.Empty(t, result.ResponseID, "a failed attempt must not become a response anchor")
				require.False(t, failover.ShouldReportAccountScheduleFailure())
			} else {
				require.True(t, c.Writer.Written())
			}
			events, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			diagnostic := events.([]*OpsUpstreamErrorEvent)[0]
			require.Equal(t, http.StatusOK, diagnostic.UpstreamStatusCode)
			require.Equal(t, "bps-request-failed", diagnostic.UpstreamRequestID)
			require.Contains(t, diagnostic.Detail, "PRIVATE_UPSTREAM_BODY", "retain the real cause only in redacted Ops diagnostics")
			require.NotContains(t, diagnostic.Detail, "private prompt")
			if !tc.retry {
				snapshot, ok := GetOpsStreamError(c)
				require.True(t, ok)
				require.Contains(t, snapshot.UpstreamDetail, "PRIVATE_UPSTREAM_BODY", "the immutable per-turn Ops snapshot must retain the original failure")
				require.Len(t, snapshot.UpstreamErrors, 1)
			}
		})
	}
}

func TestExcelBPSRepairFailureDiagnosticKeepsRequestIdentity(t *testing.T) {
	for _, protocol := range []string{"json", "sse", "ws"} {
		for _, mode := range []string{"known", "unknown"} {
			for _, rejection := range []string{"server stream", "ownership http", "ownership stream", "cyber http", "cyber stream", "http 403", "http 429", "http 500"} {
				t.Run(protocol+"/"+mode+"/"+rejection, func(t *testing.T) {
					code, tools := "text(42);", `[{"type":"custom","name":"exec"}]`
					if mode == "unknown" {
						code, tools = `{"name":"missing","arguments":{}}`, `[{"type":"function","name":"inspect","parameters":{"type":"object"}}]`
					}
					first := bpsCompletionResponse(200, excelBPSRepairWire(t, "original", "Run client tool", code))
					first.Header.Set("X-Request-Id", "req_original")
					repair := bpsCompletionResponse(200, bpsFailureWire(t, "response.failed", "server_error", "server_error", 500))
					wantInput, wantOutput := 17, 4
					if rejection == "cyber http" {
						repair = bpsCompletionResponse(403, "{\"error\":{\"code\":\"cyber_policy\",\"message\":\"rejected test-token file-secret\"}}")
						wantInput, wantOutput = 10, 2
					} else if rejection == "cyber stream" {
						repair = bpsCompletionResponse(200, bpsFailureWire(t, "response.failed", "cyber_policy", "invalid_request", 0))
					}
					if strings.HasPrefix(rejection, "ownership") {
						detail := `{"error":{"message":"Unknown error while validating file ownership.","type":"server_error"}}`
						if rejection == "ownership http" {
							repair = bpsCompletionResponse(500, detail)
							wantInput, wantOutput = 10, 2
						} else {
							repair = bpsCompletionResponse(200, "data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_failure\",\"status\":\"failed\",\"output\":[],\"usage\":{\"input_tokens\":7,\"output_tokens\":2},"+detail[1:]+"}\n\n")
						}
					}
					if strings.HasPrefix(rejection, "http ") {
						status := map[string]int{"http 403": 403, "http 429": 429, "http 500": 500}[rejection]
						repair = bpsCompletionResponse(status, `{"error":{"message":"rejected test-token file-secret"}}`)
						wantInput, wantOutput = 10, 2
					}
					repair.Header.Set("X-Request-Id", "req_repair")
					upstream := &httpUpstreamRecorder{responses: []*http.Response{first, repair}}
					svc := excelBPSGatewayTestService(upstream)
					svc.cfg.Gateway.LogUpstreamErrorBody = true
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
					body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","stream":%t,"input":"calculate","tools":%s}`, protocol != "json", tools))
					var result *OpenAIForwardResult
					var err error
					var output strings.Builder
					if protocol == "ws" {
						SetOpenAIClientTransport(c, OpenAIClientTransportWS)
						result, err = svc.proxyOpenAIWSExcelBPSTurn(t.Context(), c, excelAccount(), body, "gpt-6-astra", func(event []byte) error {
							require.NotContains(t, string(event), "PRIVATE_")
							output.Write(event)
							return nil
						})
					} else {
						result, err = svc.Forward(t.Context(), c, excelAccount(), body)
						output.Write(rec.Body.Bytes())
					}
					require.Error(t, err)
					require.NotNil(t, result)
					require.Len(t, upstream.requests, 2)
					var unexpectedReplay *UpstreamFailoverError
					require.NotErrorAs(t, err, &unexpectedReplay, "correction failure cannot promote the accepted generation to failover")
					require.Equal(t, wantInput, result.Usage.InputTokens)
					require.Equal(t, wantOutput, result.Usage.OutputTokens)
					value, ok := c.Get(OpsUpstreamErrorsKey)
					require.True(t, ok)
					events := value.([]*OpsUpstreamErrorEvent)
					require.Len(t, events, 1)
					require.Equal(t, repair.StatusCode, events[0].UpstreamStatusCode)
					require.NotEqual(t, "req_repair", result.RequestID, "repair identity cannot replace the initial generation billing identity")
					if protocol != "ws" {
						require.Equal(t, "req_original", result.RequestID)
					}
					require.Equal(t, "req_repair", events[0].UpstreamRequestID, "the failure occurred in the correction, not in the original successful HTTP response")
					if strings.HasPrefix(rejection, "ownership") {
						var failover *UpstreamFailoverError
						require.NotErrorAs(t, err, &failover)
						require.Contains(t, output.String(), "basispoints_file_ownership_error")
						require.NotContains(t, output.String(), "response.output_item.added")
						require.Contains(t, events[0].Detail, "Unknown error while validating file ownership.")
						require.Equal(t, repair.StatusCode, events[0].UpstreamStatusCode)
						wantStage := "tool_repair"
						if mode == "unknown" {
							wantStage = "tool_regeneration"
						}
						require.Equal(t, wantStage, gjson.Get(events[0].Detail, "file_ownership_context.stage").String())
					} else if strings.HasPrefix(rejection, "cyber") {
						var failover *UpstreamFailoverError
						require.NotErrorAs(t, err, &failover)
						mark := GetOpsCyberPolicy(c)
						require.NotNil(t, mark)
						require.Equal(t, repair.StatusCode, mark.UpstreamStatus)
						require.Equal(t, wantInput, mark.UpstreamInTok)
						require.Equal(t, wantOutput, mark.UpstreamOutTok)
						require.NotContains(t, mark.Body, "test-token")
						require.NotContains(t, mark.Body, "file-secret")
						require.Contains(t, output.String(), "cyber_policy")
					} else if strings.HasPrefix(rejection, "http ") {
						require.NotContains(t, output.String(), "test-token")
						require.NotContains(t, output.String(), "file-secret")
						require.NotContains(t, events[0].Detail, "test-token")
						require.NotContains(t, output.String(), "response.output_item.added")
					} else {
						require.Contains(t, events[0].Detail, "PRIVATE_UPSTREAM_BODY")
					}
				})
			}
		}
	}
}

func TestExcelBPSHTTPServerFailureRecordsReplayStop(t *testing.T) {
	for _, status := range []int{500, 504} {
		for _, unsafe := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/unsafe=%t", status, unsafe), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(status, "{\"error\":{\"type\":\"server_error\",\"message\":\"PRIVATE_UPSTREAM\"}}")}
				svc := excelBPSGatewayTestService(upstream)
				source := map[string]any{"model": "gpt-6-astra", "input": "test"}
				if unsafe {
					source["input"] = []any{map[string]any{"type": "compaction", "encrypted_content": "PRIVATE_HISTORY"}, map[string]any{"role": "user", "content": "continue"}}
				}
				raw, err := json.Marshal(source)
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				_, err = svc.Forward(t.Context(), c, excelAccount(), raw)
				require.Error(t, err)
				require.Len(t, upstream.requests, 1)
				events, _ := c.Get(OpsUpstreamErrorsKey)
				items := events.([]*OpsUpstreamErrorEvent)
				if unsafe {
					require.Equal(t, status, rec.Code)
					require.Len(t, items, 2)
					require.Equal(t, "http_error", items[0].Kind)
					require.Equal(t, status, items[0].UpstreamStatusCode)
					require.Equal(t, "retry_stopped", items[1].Kind)
					require.Equal(t, "replay_unsafe", items[1].Reason)
					require.Equal(t, "unresolved_tool_context", gjson.Get(items[1].Detail, "blocking_tool_kind").String())
					require.NotContains(t, items[1].Detail, "PRIVATE_")
				} else {
					var failure *UpstreamFailoverError
					require.ErrorAs(t, err, &failure)
					require.Len(t, items, 1)
					require.Equal(t, "failover", items[0].Kind)
					require.False(t, c.Writer.Written(), "the handler retains its bounded recovery opportunity")
				}
			})
		}
	}
}

func TestExcelBPSHTTPServerFailurePreservesExpiredBudgetReason(t *testing.T) {
	svc := excelBPSGatewayTestService(nil)
	svc.cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		// A provider status can race with expiry. It must not make the final
		// first-output timeout look like a different replay-policy rejection.
		<-req.Context().Done()
		return bpsCompletionResponse(504, "{}"), nil
	}}
	raw := []byte("{\"model\":\"gpt-6-astra\",\"input\":[{\"type\":\"compaction\",\"encrypted_content\":\"PRIVATE_HISTORY\"},{\"role\":\"user\",\"content\":\"continue\"}]}")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := svc.Forward(t.Context(), c, excelAccount(), raw)
	require.Error(t, err)
	require.Equal(t, "first_output_timeout", gjson.Get(rec.Body.String(), "error.code").String())
	events, _ := c.Get(OpsUpstreamErrorsKey)
	stopped := 0
	for _, event := range events.([]*OpsUpstreamErrorEvent) {
		if event.Kind == "retry_stopped" {
			stopped++
			require.Equal(t, "turn_budget", event.Reason)
		}
	}
	require.Equal(t, 1, stopped)
}
