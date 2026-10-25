package service

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpsCloudDiagnosticsReadRecordedSSEFailure(t *testing.T) {
	frame := `data: {"type":"response.failed","response":{"status":"failed","error":{"code":"cloud_execution_failed","message":"private-secret","upstream_method":"initialize","upstream_rpc_code":-32602,"history_recovery_attempts":0,"output_observed":false,"usage_observed":false}}}`
	for _, body := range []string{frame + "\n\n", "event: response.failed\r\n" + frame + "\r\n\r\ndata: [DONE]\r\n\r\n"} {
		out := cloudDiagnosticFailure(body)
		require.Equal(t, "cloud_execution_failed", out["code"])
		require.Equal(t, "initialize", out["upstream_method"])
		require.Equal(t, -32602, out["upstream_rpc_code"])
		require.Equal(t, false, out["output_observed"])
		raw, err := json.Marshal(out)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "private-secret")
	}
	// Data inside model/user text is not an SSE frame or a failure envelope.
	for _, body := range []string{
		`data: {"type":"response.output_text.delta","error":{"code":"cloud_execution_failed"},"delta":"private-secret"}` + "\n\n",
		`data: {"type":"response.completed","response":{"error":{"code":"cloud_execution_failed"}}}` + "\n\n",
		"event: response.completed\n" + frame + "\n\n",
		`data: {"error":{"code":"cloud_execution_failed"}}` + "\n\n",
		`{"message":"{\"error\":{\"code\":\"cloud_execution_failed\"}}"}`,
		strings.Repeat("x", 1<<20) + frame,
	} {
		require.Empty(t, cloudDiagnosticFailure(body))
	}
}

func TestOpsCloudDiagnosticsPreserveKnownCodexObjectVariants(t *testing.T) {
	for _, variant := range []string{"httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts", "activeTurnNotSteerable"} {
		body, err := json.Marshal(map[string]any{"error": map[string]any{"code": "cloud_upstream_error", "upstream_error_info": variant}})
		require.NoError(t, err)
		require.Equal(t, variant, cloudDiagnosticFailure(string(body))["upstream_error_info"])
	}
	for _, code := range []string{"cloud_event_queue_overflow", "cloud_transport_closed", "cloud_protocol_error"} {
		body, err := json.Marshal(map[string]any{"error": map[string]any{"code": code, "upstream_method": "turn/start"}})
		require.NoError(t, err)
		require.Equal(t, code, cloudDiagnosticFailure(string(body))["code"])
	}
}

func TestOpsCloudFailureEventSurvivesCompleteDetails(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	recordCloudFailure(c, []byte(`{"status":"failed","error":{"code":"cloud_upstream_error","message":"private-secret","upstream_method":"turn/start","upstream_error_info":"responseStreamConnectionFailed","upstream_http_status":502,"output_observed":false,"usage_observed":false},"output":[{"text":"private-secret"}]}`))
	events := c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, cloudFailureKind, events[0].Kind)
	require.True(t, events[0].SkipMonitoring)
	require.Nil(t, LastOpsUpstreamAttempt(events), "diagnostic facts must not replace a health attempt")
	for i := 0; i < opsUpstreamErrorsBodyWindow+1; i++ {
		events = append(events, &OpsUpstreamErrorEvent{Kind: "http_error", UpstreamStatusCode: 502})
	}
	entry := &OpsInsertErrorLogInput{UpstreamErrors: events}
	require.NoError(t, sanitizeOpsUpstreamErrors(entry))
	rootBody, err := json.Marshal(map[string]any{"error": map[string]any{"code": "cloud_upstream_error", "message": "failed"}})
	require.NoError(t, err)
	out := sanitizeOpsDetailCredentials(&OpsErrorLogDetail{ErrorBody: string(rootBody), UpstreamErrors: *entry.UpstreamErrorsJSON})
	storedEvents, err := ParseOpsUpstreamErrors(out.UpstreamErrors)
	require.NoError(t, err)
	require.Equal(t, cloudFailureKind, storedEvents[0].Kind)
	facts := cloudDiagnosticFailure(storedEvents[0].Detail)
	require.Equal(t, "turn/start", facts["upstream_method"])
	require.Equal(t, "responseStreamConnectionFailed", facts["upstream_error_info"])
	require.Equal(t, false, facts["usage_observed"])
	require.Equal(t, string(rootBody), out.ErrorBody, "full detail does not combine separate failure records")
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-secret")
	for _, code := range []string{"cloud_invalid_request", "unknown-final-code"} {
		body, err := json.Marshal(map[string]any{"error": map[string]any{"code": code}})
		require.NoError(t, err)
		out = sanitizeOpsDetailCredentials(&OpsErrorLogDetail{ErrorBody: string(body), UpstreamErrors: *entry.UpstreamErrorsJSON})
		require.Equal(t, string(body), out.ErrorBody)
		require.NotContains(t, cloudDiagnosticFailure(out.ErrorBody), "upstream_method")
		require.Contains(t, out.UpstreamErrors, "turn/start", "recorded history remains separate and complete")
	}
}

func TestOpsCloudSemanticFailureRecordsAdaptation(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	recordCloudAdaptation(c, []cloudAdaptationDecision{{Path: "prompt_cache_key", Action: "drop", MatrixID: "CLOUD-META"}}, errors.New("Cloud response failed"))
	events := c.MustGet(OpsUpstreamErrorsKey).([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, cloudAdaptationKind, events[0].Kind)
	require.Nil(t, LastOpsUpstreamAttempt(events))
}
