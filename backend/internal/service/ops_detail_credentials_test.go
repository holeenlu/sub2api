package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func opsDetailTestJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func TestSanitizeOpsDetailCredentialsPreservesRecordedBPSDetails(t *testing.T) {
	accountID, latency, status := int64(91), int64(430), 502
	body := map[string]any{
		"error":  map[string]any{"code": "basispoints_protocol_error", "message": "original BPS response", "stage": "read", "reason": "invalid_envelope"},
		"input":  []any{map[string]any{"type": "function_call", "name": "get_access_token", "arguments": opsDetailTestJSON(t, map[string]any{"count": json.Number("900719925474099312345")})}},
		"schema": map[string]any{"type": "object", "properties": map[string]any{"token": map[string]any{"type": "string"}, "session_key": true}},
	}
	events := []map[string]any{
		{"kind": "bps_error", "stage": "read", "scope": "upstream", "reason": "invalid_envelope", "message": "BPS failed", "detail": opsDetailTestJSON(t, map[string]any{
			"code": "basispoints_protocol_error", "session_id": "session-7", "raw_input": []any{map[string]any{"name": "get_access_token"}},
		}), "unknown_history": map[string]any{"large": json.Number("900719925474099312345")}},
		{"kind": "scheduling_trace", "detail": opsDetailTestJSON(t, map[string]any{
			"summary":                 map[string]any{"candidates": 2, "selected_account_id": 91},
			"events":                  []any{map[string]any{"stage": "retry", "reason": "bps_error", "elapsed_ms": 430}},
			"downstream_token_events": 3, "other_token_events": 1,
		})},
	}
	detail := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			ID: 7, CreatedAt: time.Unix(1730000000, 0), Phase: "upstream", Type: "protocol_error", Owner: "provider", Source: "gateway",
			Severity: "error", StatusCode: status, RequestStatusCode: &status, RequestID: "request-7", ClientRequestID: "client-7",
			AccountID: &accountID, AccountName: "BPS account", UserEmail: "user@example.test", RequestPath: "/v1/responses", Stream: true, Model: "model-test",
			Message: "basispoints_protocol_error: tool get_access_token returned an invalid envelope",
		},
		ErrorBody: opsDetailTestJSON(t, body),
		UpstreamErrorDetail: opsDetailTestJSON(t, map[string]any{"code": "basispoints_protocol_error", "message": "complete upstream message", "unrecognized_history_field": map[string]any{
			"revision": json.Number("900719925474099312345"), "ratio": json.Number("0.1234567890123456789012345"),
		}}),
		UpstreamErrors: opsDetailTestJSON(t, events), UpstreamStatusCode: &status, UpstreamErrorMessage: "full BPS failure",
		AuthLatencyMs: &latency, UpstreamLatencyMs: &latency, APIKeyPrefix: "sk-prefix",
	}
	before := opsDetailTestJSON(t, detail)
	out := sanitizeOpsDetailCredentials(detail)
	require.NotSame(t, detail, out)
	// Inputs already use the canonical encoder: equality also proves every metadata
	// field, nested scheduling fact, schema, unknown key and exact number survives.
	require.Equal(t, *detail, *out)
	require.Contains(t, out.UpstreamErrorDetail, "900719925474099312345")
	require.Contains(t, out.UpstreamErrorDetail, "0.1234567890123456789012345")
	require.Equal(t, before, opsDetailTestJSON(t, detail))
}

func TestSanitizeOpsDetailCredentialsRemovesCredentialsWithoutDroppingFacts(t *testing.T) {
	const fullKey = "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890"
	body := map[string]any{
		"error":   map[string]any{"code": "basispoints_protocol_error", "message": "Bearer nested-bearer"},
		"headers": map[string]any{"Authorization": "Bearer json-bearer", "Cookie": "session=cookie-secret"},
		"input":   map[string]any{"text": "original input diagnostics", "api_key": "input-secret"},
		"tools": []any{map[string]any{"name": "get_access_token", "parameters": map[string]any{"type": "object", "properties": map[string]any{
			"api_key": map[string]any{"type": "string", "description": "API credential", "default": "preset-secret"}, "token": map[string]any{"type": "string"},
		}}}},
	}
	detail := &OpsErrorLogDetail{
		OpsErrorLog: OpsErrorLog{
			Message:     "basispoints_protocol_error Authorization: Bearer bearer-credential x-api-key=plain-key code=retained",
			RequestPath: "https://url-user:url-password@example.test/v1/responses?access_token=url-secret", UserAgent: "debug " + fullKey,
		},
		ErrorBody: opsDetailTestJSON(t, body), UpstreamErrorMessage: "provider said " + fullKey,
		UpstreamErrorDetail: "event: error\r\ndata: " + opsDetailTestJSON(t, map[string]any{"code": "basispoints_protocol_error", "access_token": "sse-secret", "history_revision": json.Number("900719925474099312345")}) + "\r\n\r\ndata: [DONE]\r\n",
		UpstreamErrors: opsDetailTestJSON(t, []any{map[string]any{
			"kind": "bps_error", "reason": "invalid_envelope",
			"detail":                 opsDetailTestJSON(t, map[string]any{"refresh_token": "embedded-secret", "code": "basispoints_protocol_error", "large": json.Number("900719925474099312345")}),
			"upstream_response_body": "Bearer event-bearer", "upstream_url": "https://example.test/v1?api_key=event-url-secret",
		}}),
	}
	before := opsDetailTestJSON(t, detail)
	out := sanitizeOpsDetailCredentials(detail)
	exported := opsDetailTestJSON(t, out)
	for _, secret := range []string{
		"bearer-credential", "plain-key", "url-user", "url-password", "url-secret", fullKey, "nested-bearer", "json-bearer", "cookie-secret", "input-secret",
		"preset-secret", "sse-secret", "embedded-secret", "event-bearer", "event-url-secret",
	} {
		require.NotContains(t, exported, secret)
	}
	for _, fact := range []string{"basispoints_protocol_error", "get_access_token", "original input diagnostics", "900719925474099312345", "invalid_envelope", "retained"} {
		require.Contains(t, exported, fact)
	}
	require.Contains(t, out.UpstreamErrorDetail, "event: error\r\ndata: ")
	require.Contains(t, out.UpstreamErrorDetail, "\r\n\r\ndata: [DONE]\r\n")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out.ErrorBody), &decoded))
	tools := decoded["tools"].([]any)
	parameters := tools[0].(map[string]any)["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	require.Equal(t, "string", properties["api_key"].(map[string]any)["type"])
	require.Equal(t, "[REDACTED]", properties["api_key"].(map[string]any)["default"])
	require.Equal(t, map[string]any{"type": "string"}, properties["token"])
	require.Equal(t, before, opsDetailTestJSON(t, detail))
}

func TestSanitizeOpsDetailCredentialsUsesSharedKeysAndDoesNotTruncate(t *testing.T) {
	require.Nil(t, sanitizeOpsDetailCredentials(nil))
	payload := map[string]any{"code": "basispoints_protocol_error", "max_tokens": 12000, "session_id": "session-7"}
	for _, key := range domain.SensitiveCredentialKeys {
		payload[key] = "private-" + key
	}
	out := sanitizeOpsDetailCredentials(&OpsErrorLogDetail{ErrorBody: opsDetailTestJSON(t, payload)})
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out.ErrorBody), &decoded))
	for _, key := range domain.SensitiveCredentialKeys {
		require.Equal(t, "[REDACTED]", decoded[key], key)
	}
	require.Equal(t, "basispoints_protocol_error", decoded["code"])
	require.Equal(t, float64(12000), decoded["max_tokens"])
	require.Equal(t, "session-7", decoded["session_id"])
	large := strings.Repeat("complete recorded error body ", 10000)
	require.Equal(t, large, sanitizeOpsDetailCredentials(&OpsErrorLogDetail{ErrorBody: large}).ErrorBody)
	pem := "before\n-----BEGIN RSA PRIVATE KEY-----\nprivate-key-line\n-----END RSA PRIVATE KEY-----\nafter"
	require.Equal(t, "before\n[REDACTED]\nafter", sanitizeOpsDetailCredentials(&OpsErrorLogDetail{ErrorBody: pem}).ErrorBody)
}
