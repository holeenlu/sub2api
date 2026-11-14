package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsDiagnosticsExportKeepsGapsWithoutSensitivePayloads(t *testing.T) {
	events := []*OpsUpstreamErrorEvent{
		{Kind: "http_error", UpstreamStatusCode: 503, Message: "Bearer private-secret", Detail: `{"api_key":"private-secret"}`, UpstreamURL: "https://private-secret.example", DroppedEarlierAttempts: 2},
		{Kind: opsSchedulingTrace, Detail: `{"dropped_events":3,"events":[{"stage":"queue","account_id":99,"reason":"private-secret","duration_ms":80,"oldest_wait_ms":60}]}`},
	}
	raw, err := json.Marshal(events)
	require.NoError(t, err)
	detail := &OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestID: "request-1", StatusCode: 502, UserEmail: "private-secret@example.test"}, ErrorBody: "private-secret", UpstreamErrors: string(raw)}
	out := BuildOpsRequestDiagnostics(detail)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-secret")
	require.NotContains(t, string(encoded), "account_id")
	require.Equal(t, "recorded_events_only", out.Coverage)
	require.Equal(t, 2, out.DroppedAttempts)
	require.Equal(t, 3, out.DroppedDecisions)
	require.Contains(t, out.Gaps, "truncated_capture")
	require.Equal(t, int64(80), out.Scheduling[0].DurationMs)
	require.Equal(t, string(raw), detail.UpstreamErrors, "导出不改写原始诊断记录")
	detail.UpstreamErrors = "invalid"
	require.Contains(t, BuildOpsRequestDiagnostics(detail).Gaps, "invalid_events")
}

func TestOpsCloudDiagnosticsRetainSafeFailureStageWithoutPayload(t *testing.T) {
	detail := &OpsErrorLogDetail{OpsErrorLog: OpsErrorLog{RequestID: "request-2", StatusCode: 502}, ErrorBody: "{\"error\":{\"code\":\"cloud_invalid_request\",\"message\":\"private-secret\",\"upstream_method\":\"thread/inject_items\",\"upstream_rpc_code\":-32602,\"upstream_error_info\":\"badRequest\"}}", UpstreamErrors: "[{\"kind\":\"scheduling_trace\",\"detail\":\"{}\"}]"}
	out := BuildOpsRequestDiagnostics(detail)
	encoded, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "thread/inject_items")
	require.Contains(t, string(encoded), "cloud_invalid_request")
	require.Contains(t, string(encoded), "badRequest")
	require.NotContains(t, string(encoded), "private-secret")
	require.Contains(t, out.Gaps, "missing_events", "scheduling alone is not upstream failure evidence")
	detail.ErrorBody = "{\"response\":{\"error\":{\"code\":\"private-secret\",\"upstream_method\":\"private-secret\",\"upstream_error_info\":\"private-secret\",\"history_recovery_attempts\":null,\"output_observed\":null,\"usage_observed\":null}}}"
	require.Empty(t, BuildOpsRequestDiagnostics(detail).Failure, "unknown and null facts must not be exported as observed zero or false")
}

func TestOpsCloudDiagnosticsReadOnlyTypedSSETerminalFacts(t *testing.T) {
	data := `{"type":"response.failed","response":{"error":{"code":"cloud_invalid_request","param":"input[12].status","upstream_error_code":"unsupported_parameter","upstream_error_info":"responseStreamDisconnected","message":"private-secret","upstream_http_status":400}}}`
	for _, raw := range []string{data, "event: response.failed\r\ndata: " + data + "\r\n\r\n"} {
		facts := cloudDiagnosticFailure(raw)
		require.Equal(t, "input[12].status", facts["param"])
		require.Equal(t, "unsupported_parameter", facts["upstream_error_code"])
		require.Equal(t, "responseStreamDisconnected", facts["upstream_error_info"])
		require.Equal(t, 400, facts["upstream_http_status"])
		encoded, err := json.Marshal(facts)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-secret")
	}
	for _, raw := range []string{
		"event: response.output_text.delta\ndata: " + data + "\n\n",
		`{"type":"response.output_text.delta","error":{"code":"cloud_invalid_request"}}`,
		`{"error":{"param":"https://private-secret.invalid/path?token=secret","upstream_error_code":"private-secret"}}`,
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"error text\"}\n\n",
	} {
		require.Empty(t, cloudDiagnosticFailure(raw))
	}
}
