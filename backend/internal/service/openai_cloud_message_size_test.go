package service

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloudRPCMessageSizeRetainsFactsWithoutHistoryReduction(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"code": -32602, "message": "Operation was attempted past the valid range: Error, decoded message length too large: found 11715065 bytes, the limit is: 4194304 bytes", "data": map[string]any{"grpcStatusCode": 11}})
	require.NoError(t, err)
	rpc := &cloudRPCError{Code: -32602, Method: "thread/inject_items", Upstream: cloudParseRPCErrorFacts(raw)}
	status, code, _, known := cloudPublicError(rpc)
	require.True(t, known)
	require.Equal(t, http.StatusRequestEntityTooLarge, status)
	require.Equal(t, "cloud_message_too_large", code)
	require.False(t, cloudInjectRejected(rpc, "thread/inject_items"), "message size is not evidence for removing encrypted history")
	a := &cloudSessionActor{}
	a.setPublicFailure(rpc)
	failure, err := json.Marshal(map[string]any{"error": a.publicFailure})
	require.NoError(t, err)
	facts := cloudDiagnosticFailure(string(failure))
	require.Equal(t, "thread/inject_items", facts["upstream_method"])
	require.EqualValues(t, 11, facts["upstream_grpc_status"])
	require.EqualValues(t, 11715065, facts["upstream_message_bytes"])
	require.EqualValues(t, 4194304, facts["upstream_message_limit_bytes"])
	require.NotContains(t, facts, "message")
}

func TestCloudRPCMessageSizeRequiresExactOwnEnvelope(t *testing.T) {
	message := "Operation was attempted past the valid range: Error, decoded message length too large: found 11715065 bytes, the limit is: 4194304 bytes"
	for name, envelope := range map[string]map[string]any{
		"wrong grpc": {"code": -32602, "message": message, "data": map[string]any{"grpcStatusCode": 3}},
		"echo":       {"code": -32602, "message": "invalid request", "data": map[string]any{"grpcStatusCode": 11, "request": map[string]any{"message": message}}},
		"quoted":     {"code": -32602, "message": "Client said: " + message, "data": map[string]any{"grpcStatusCode": 11}},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(envelope)
			require.NoError(t, err)
			_, code, _ := cloudUpstreamErrorResponse(cloudParseRPCErrorFacts(raw))
			require.NotEqual(t, "cloud_message_too_large", code)
		})
	}
}
