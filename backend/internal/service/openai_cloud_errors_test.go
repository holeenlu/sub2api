package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloudErrorsRetainActionableSafeClassification(t *testing.T) {
	for _, test := range []struct {
		name, raw, code string
		status          int
	}{
		{"overload", `{"codexErrorInfo":"serverOverloaded","message":"private prompt"}`, "server_is_overloaded", 503},
		{"quota", `{"codexErrorInfo":"usageLimitExceeded"}`, "insufficient_quota", 429},
		{"rate", `{"codexErrorInfo":"rateLimitExceeded"}`, "rate_limit_exceeded", 429},
		{"session_budget", `{"codexErrorInfo":"sessionBudgetExceeded"}`, "cloud_session_budget_exhausted", 429},
		{"context", `{"codexErrorInfo":"contextWindowExceeded"}`, "context_length_exceeded", 400},
		{"unauthorized", `{"codexErrorInfo":"unauthorized"}`, "cloud_authorization_failed", 403},
		{"cyber", `{"codexErrorInfo":"cyberPolicy"}`, "cyber_policy", 403},
		{"misalignment", `{"codexErrorInfo":"misalignmentPolicyViolation","misalignment":{"message":"private prompt"}}`, "content_policy_violation", 403},
		{"bad_request", `{"codexErrorInfo":"badRequest"}`, "cloud_invalid_request", 400},
		{"flex", `{"codexErrorInfo":"flexUnavailable"}`, "flex_unavailable", 503},
		{"not_steerable", `{"codexErrorInfo":{"activeTurnNotSteerable":{"turnKind":"review"}}}`, "cloud_turn_conflict", 409},
		{"nested_http", `{"codexErrorInfo":{"responseStreamConnectionFailed":{"httpStatusCode":429}}}`, "rate_limit_exceeded", 429},
		{"disconnected", `{"codexErrorInfo":{"responseStreamDisconnected":{"httpStatusCode":null}}}`, "cloud_stream_disconnected", 502},
		{"rpc_provider", `{"code":-32000,"data":{"error":{"code":"model_not_found","message":"private prompt"}}}`, "model_not_found", 400},
		{"encrypted", `{"code":"invalid_encrypted_content","message":"private prompt"}`, "invalid_encrypted_content", 400},
		{"encrypted_signature", `{"code":"thinking_signature_invalid","message":"The encrypted content secret could not be verified because it could not be decrypted or parsed"}`, "invalid_encrypted_content", 400},
		{"encrypted_message", `{"message":"The encrypted content secret could not be verified because it could not be decrypted or parsed"}`, "invalid_encrypted_content", 400},
		{"compaction", `{"code":"invalid_compaction"}`, "invalid_compaction", 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := cloudParseTurnError(json.RawMessage(test.raw))
			require.NotNil(t, upstream)
			status, code, message, ok := cloudPublicError(fmt.Errorf("wrapped: %w", upstream))
			require.True(t, ok)
			require.Equal(t, test.status, status)
			require.Equal(t, test.code, code)
			actor := &cloudSessionActor{}
			actor.setPublicFailure(upstream)
			require.Equal(t, test.status, actor.publicFailure["status_code"], "SSE and WS carry the same safe mapped HTTP status")
			require.Equal(t, message, upstream.Error())
			require.NotContains(t, fmt.Sprintf("%+v %#v", upstream, upstream), "private prompt")
			require.NotContains(t, fmt.Sprintf("%+v %#v", upstream, upstream), "secret")
		})
	}
}

func TestCloudErrorsNeverInferStatusFromEchoedRequestOrRetainRawPayload(t *testing.T) {
	for name, raw := range map[string]string{
		"arbitrary_message": `{"message":"private prompt: invalid_encrypted_content rateLimitExceeded bearer.fake-secret"}`,
		"unknown_enum":      `{"code":"bearer.fake-secret","codexErrorInfo":"private prompt","additionalDetails":"proxy-secret","httpStatusCode":200}`,
		"echoed_request":    `{"data":{"request":{"error":{"code":"invalid_encrypted_content","codexErrorInfo":"rateLimitExceeded"}}}}`,
		"unknown_variant":   `{"codexErrorInfo":{"private prompt":{"httpStatusCode":429,"token":"fake-secret"}}}`,
		"ambiguous_variant": `{"codexErrorInfo":{"httpConnectionFailed":{"httpStatusCode":429},"responseStreamDisconnected":{"httpStatusCode":401}}}`,
		"duplicate_code":    `{"code":"rate_limit_exceeded","code":"invalid_encrypted_content"}`,
		"invalid_status":    `{"httpStatusCode":-429}`,
		"unknown_status":    `{"httpStatusCode":599}`,
		"malformed":         `{broken`,
		"oversized":         `{"message":"` + strings.Repeat("x", cloudErrorPayloadLimit) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			upstream := cloudParseTurnError(json.RawMessage(raw))
			require.NotNil(t, upstream)
			status, code, _, ok := cloudPublicError(upstream)
			require.True(t, ok)
			require.Equal(t, http.StatusBadGateway, status)
			require.Equal(t, "cloud_upstream_error", code)
			for _, secret := range []string{"private prompt", "fake-secret", "proxy-secret"} {
				require.NotContains(t, fmt.Sprintf("%+v %#v", upstream, upstream), secret)
			}
		})
	}
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(" \n null \t")} {
		require.Nil(t, cloudParseTurnError(raw))
	}
	_, _, _, ok := cloudPublicError(errors.New("unknown error"))
	require.False(t, ok)
}

func TestCloudRPCMissingThreadRequiresExactOwnErrorSignature(t *testing.T) {
	const threadID = "b41148ea-7c38-4ae6-8061-74767495e649"
	signature := `{"code":-32600,"message":"unknown thread: ` + threadID + `"}`
	archiveSignature := `{"code":-32602,"data":{"grpcStatusCode":3},"message":"Client specified an invalid argument: unknown thread: ` + threadID + `"}`
	for name, raw := range map[string]string{
		"canonical":              signature,
		"uppercase_uuid":         strings.Replace(signature, threadID, strings.ToUpper(threadID), 1),
		"archive":                archiveSignature,
		"archive_uppercase_uuid": strings.Replace(archiveSignature, threadID, strings.ToUpper(threadID), 1),
	} {
		t.Run(name, func(t *testing.T) {
			upstream := cloudParseRPCErrorFacts(json.RawMessage(raw))
			var rpc struct {
				Code int `json:"code"`
			}
			require.NoError(t, json.Unmarshal([]byte(raw), &rpc))
			status, code, message, ok := cloudPublicError(&cloudRPCError{Code: rpc.Code, Upstream: upstream})
			require.True(t, ok)
			require.Equal(t, http.StatusBadGateway, status)
			require.Equal(t, "cloud_thread_unavailable", code)
			require.NotContains(t, message, threadID)
			require.NotContains(t, fmt.Sprintf("%+v %#v", upstream, upstream), threadID)
			// Generic TurnError payloads cannot establish this RPC-only fact.
			require.NotEqual(t, "cloud_thread_unavailable", cloudParseTurnError(json.RawMessage(raw)).Code)
		})
	}
	for name, raw := range map[string]string{
		"wrong_numeric_code":          strings.Replace(signature, "-32600", "-32602", 1),
		"string_code":                 strings.Replace(signature, "-32600", `"-32600"`, 1),
		"missing_code":                `{"message":"unknown thread: ` + threadID + `"}`,
		"wrong_field_case":            strings.Replace(signature, `"message"`, `"Message"`, 1),
		"message_prefix":              strings.Replace(signature, "unknown thread:", "provider: unknown thread:", 1),
		"message_suffix":              strings.Replace(signature, threadID, threadID+" invalid context", 1),
		"invalid_uuid":                strings.Replace(signature, threadID, "not-a-thread-id", 1),
		"braced_uuid":                 strings.Replace(signature, threadID, "{"+threadID+"}", 1),
		"nested_error":                `{"code":-32600,"error":` + signature + `}`,
		"nested_data":                 `{"code":-32600,"data":` + signature + `}`,
		"echoed_request":              `{"code":-32600,"data":{"request":{"error":` + signature + `}}}`,
		"json_message":                `{"code":-32600,"message":` + fmt.Sprintf("%q", signature) + `}`,
		"duplicate_message":           strings.TrimSuffix(signature, "}") + `,"message":"unknown thread: ` + threadID + `"}`,
		"archive_wrong_rpc_code":      strings.Replace(archiveSignature, "-32602", "-32600", 1),
		"archive_missing_grpc_status": strings.Replace(archiveSignature, `,"data":{"grpcStatusCode":3}`, "", 1),
		"archive_wrong_grpc_status":   strings.Replace(archiveSignature, `"grpcStatusCode":3`, `"grpcStatusCode":5`, 1),
		"archive_string_grpc_status":  strings.Replace(archiveSignature, `"grpcStatusCode":3`, `"grpcStatusCode":"3"`, 1),
		"archive_nested_grpc_status":  strings.Replace(archiveSignature, `"data":{"grpcStatusCode":3}`, `"data":{"error":{"grpcStatusCode":3}}`, 1),
		"archive_wrong_grpc_case":     strings.Replace(archiveSignature, "grpcStatusCode", "GrpcStatusCode", 1),
		"archive_wrong_prefix":        strings.Replace(archiveSignature, "Client specified an invalid argument: ", "", 1),
		"archive_extra_suffix":        strings.Replace(archiveSignature, threadID, threadID+" unavailable", 1),
		"archive_invalid_uuid":        strings.Replace(archiveSignature, threadID, "not-a-thread-id", 1),
		"archive_echoed_request":      `{"code":-32602,"data":{"request":{"error":` + archiveSignature + `}}}`,
		"archive_nested_error":        `{"code":-32602,"error":` + archiveSignature + `}`,
		"archive_duplicate_status":    strings.Replace(archiveSignature, `"grpcStatusCode":3`, `"grpcStatusCode":3,"grpcStatusCode":3`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			upstream := cloudParseRPCErrorFacts(json.RawMessage(raw))
			require.NotNil(t, upstream)
			require.NotEqual(t, "cloud_thread_unavailable", upstream.Code)
		})
	}
}

func TestCloudErrorNotificationRequiresExplicitRetryDisposition(t *testing.T) {
	for _, retry := range []bool{true, false} {
		raw := fmt.Sprintf(`{"threadId":"thread","turnId":"turn","willRetry":%t,"error":{"codexErrorInfo":"serverOverloaded","message":"private prompt"}}`, retry)
		notice, err := cloudParseErrorNotification(json.RawMessage(raw))
		require.NoError(t, err)
		require.Equal(t, "thread", notice.ThreadID)
		require.Equal(t, "turn", notice.TurnID)
		require.Equal(t, retry, notice.WillRetry)
		require.Equal(t, "serverOverloaded", notice.Err.CodexErrorInfo)
		require.NotContains(t, notice.Err.Error(), "private prompt")
	}
	for _, raw := range []string{
		`{"threadId":"thread","turnId":"turn","error":{"codexErrorInfo":"serverOverloaded"}}`,
		`{"threadId":"thread","turnId":"turn","willRetry":null,"error":{}}`,
		`{"threadId":"thread","turnId":"turn","willRetry":"true","error":{}}`,
		`{"threadId":"thread","willRetry":false,"error":{}}`,
		`{"threadId":"thread","turnId":"turn","willRetry":true,"error":null}`,
		`{"threadId":"thread","turnId":"turn","willRetry":true,"willRetry":false,"error":{}}`,
	} {
		_, err := cloudParseErrorNotification(json.RawMessage(raw))
		require.ErrorIs(t, err, errCloudRPCProtocol)
	}
}

func TestCloudHandshakeAndRPCErrorStatusMapping(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{&cloudGatewayError{429, "cloud_quota_exhausted", "Cloud account quota is exhausted"}, 429, "cloud_quota_exhausted"},
		{ErrOpenAICloudAuthorization, 403, "cloud_authorization_failed"},
		{ErrOpenAICloudCapacity, 429, "cloud_session_capacity"},
		{ErrOpenAIWSFirstOutputBudgetExhausted, 504, "cloud_first_output_timeout"},
		{&cloudHandshakeError{StatusCode: 401}, 403, "cloud_authorization_failed"},
		{&cloudHandshakeError{StatusCode: 403}, 403, "cloud_access_denied"},
		{&cloudHandshakeError{StatusCode: 429}, 429, "rate_limit_exceeded"},
		{&cloudHandshakeError{StatusCode: 503}, 503, "server_is_overloaded"},
		{&cloudHandshakeError{StatusCode: 504}, 504, "cloud_upstream_timeout"},
		{&cloudHandshakeError{StatusCode: 0}, 502, "cloud_upstream_error"},
		{&cloudRPCError{Code: -32602}, 400, "cloud_invalid_request"},
		{&cloudRPCError{Code: -32601}, 502, "cloud_method_unavailable"},
		{&cloudRPCError{Code: -32600}, 502, "cloud_protocol_error"},
		{&cloudRPCError{Code: -32099}, 502, "cloud_rpc_rejected"},
		{&cloudRPCError{Code: -32602, Upstream: &cloudUpstreamError{Code: "invalid_encrypted_content"}}, 400, "invalid_encrypted_content"},
	} {
		status, code, message, ok := cloudPublicError(test.err)
		require.True(t, ok)
		require.Equal(t, test.status, status)
		require.Equal(t, test.code, code)
		require.NotEmpty(t, message)
	}
}

func TestCloudErrorMessageRecognizesOnlyOneStructuredProviderEnvelope(t *testing.T) {
	provider := `{"type":"error","error":{"message":"The encrypted content for item rs_probe_invalid could not be verified. Reason: Encrypted content could not be decrypted or parsed.","type":"invalid_request_error","param":"input","code":"invalid_encrypted_content"},"status":400}`
	raw, err := json.Marshal(map[string]any{"message": provider, "codexErrorInfo": "other", "additionalDetails": "private prompt"})
	require.NoError(t, err)
	upstream := cloudParseTurnError(raw)
	status, code, message, ok := cloudPublicError(upstream)
	require.True(t, ok)
	require.Equal(t, 400, status)
	require.Equal(t, 400, upstream.HTTPStatus)
	require.Equal(t, "invalid_encrypted_content", code)
	require.NotContains(t, message, "rs_probe_invalid")
	require.NotContains(t, fmt.Sprintf("%+v %#v", upstream, upstream), "private prompt")
	for name, text := range map[string]string{
		"wrong_type":      `{"type":"message","error":{"code":"invalid_encrypted_content"},"status":400}`,
		"echoed_request":  `{"type":"error","request":{"error":{"code":"invalid_encrypted_content"}},"status":400}`,
		"nested_message":  `{"type":"error","error":{"message":` + fmt.Sprintf("%q", provider) + `}}`,
		"duplicate_field": `{"type":"error","error":{"code":"invalid_encrypted_content","code":"rate_limit_exceeded"},"status":400}`,
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"message": text, "codexErrorInfo": "other"})
			require.NoError(t, err)
			upstream := cloudParseTurnError(raw)
			status, code, _, _ := cloudPublicError(upstream)
			require.Equal(t, 502, status)
			require.Equal(t, "cloud_upstream_error", code)
		})
	}
}

func TestCloudPreInferenceRecoveryBoundaries(t *testing.T) {
	body := []byte(`{"model":"m","input":"hi"}`)
	for _, tc := range []struct {
		code   string
		status int
		retry  bool
	}{
		{"cloud_quota_exhausted", 429, true}, {"cloud_authorization_failed", 403, true},
		{"cloud_payment_required", 402, true}, {"model_not_found", 400, true},
		{"server_is_overloaded", 503, true}, {"cloud_execution_failed", 502, true},
		{"cloud_invalid_request", 400, false}, {"content_policy_violation", 403, false},
		{"cloud_access_denied", 403, false}, {"cloud_settings_invalid", 400, false},
		{"cloud_protocol_error", 502, false}, {"cloud_method_unavailable", 502, false}, {"cloud_thread_unavailable", 502, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			f := cloudPreInferenceFailover(t.Context(), body, tc.status, tc.code, "safe message", nil, true)
			require.Equal(t, tc.retry, f != nil)
			if f != nil {
				require.True(t, f.RequestNotSent && f.RejectedBeforeGeneration && f.SafeToFailoverAfterWrite)
				require.Equal(t, tc.status >= 500, f.RetryableOnSameAccount)
			}
		})
	}
	for _, mode := range []string{"cancel", "committed", "continuation", "reference", "budget"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(EnsureOpenAITurnBudget(t.Context(), nil))
			defer cancel()
			payload := body
			var err error
			switch mode {
			case "cancel":
				cancel()
			case "committed":
				MarkTurnOutputCommitted(ctx)
			case "continuation":
				ctx = context.WithValue(ctx, cloudContinuationKey{}, &OpenAICloudBinding{})
			case "reference":
				payload = []byte(`{"model":"m","previous_response_id":"resp_cloud_old","input":"hi"}`)
			case "budget":
				err = ErrOpenAIWSFirstOutputBudgetExhausted
			}
			require.Nil(t, cloudPreInferenceFailover(ctx, payload, 503, "server_is_overloaded", "busy", err, true))
		})
	}
	for _, mode := range []string{"valid", "legacy", "usage", "root", "uncertain", "replayed", "nonterminal"} {
		t.Run("proof_"+mode, func(t *testing.T) {
			r := &OpenAICloudResult{RejectedBeforeInference: true, Terminal: true, UsageCredible: true, RootRequestID: "root", Usage: &OpenAICloudUsage{}}
			switch mode {
			case "legacy":
				r.RejectedBeforeInference = false
			case "usage":
				r.Usage.WebSearchCalls = 1
			case "root":
				r.RootRequestID = "other"
			case "uncertain":
				r.UsageCredible = false
			case "replayed":
				r.Replayed = true
			case "nonterminal":
				r.Terminal = false
			}
			require.Equal(t, mode == "valid", cloudResultProvesNoInference(r, "root"))
		})
	}
}

func TestCloudAttemptBudgetFailurePreservesRetryLimitStatus(t *testing.T) {
	for _, cause := range []error{ErrOpenAITurnAttemptBudgetExhausted, ErrTurnPreparationExhausted, ErrTurnRetryWaitExhausted} {
		status, code, _, known := cloudPublicError(fmt.Errorf("wrapped: %w", cause))
		require.True(t, known)
		require.Equal(t, http.StatusServiceUnavailable, status)
		require.Equal(t, "retry_budget_exhausted", code)
		require.Nil(t, cloudPreInferenceFailover(t.Context(), []byte(`{"model":"m","input":"x"}`), status, code, "budget exhausted", cause, true))
	}
}

func TestCloudRemoteCompactionErrorEnvelope(t *testing.T) {
	provider := `{"type":"error","error":{"message":"opaque-private","type":"invalid_request_error","code":"invalid_encrypted_content"},"status":400}`
	for _, prefix := range []string{"", "Error running remote compact task: "} {
		raw, _ := json.Marshal(map[string]any{"message": prefix + provider, "codexErrorInfo": "other"})
		got := cloudParseTurnError(raw)
		require.Equal(t, "invalid_encrypted_content", got.Code)
		require.Equal(t, 400, got.HTTPStatus)
		require.True(t, cloudRejectedBeforeInference(got))
		require.NotContains(t, fmt.Sprintf("%+v", got), "opaque-private")
	}
	for _, text := range []string{"quoted: Error running remote compact task: " + provider, "error running remote compact task: " + provider, "Error running remote compact task: Error running remote compact task: " + provider, "Error running remote compact task: " + provider + " trailing"} {
		raw, _ := json.Marshal(map[string]any{"message": text, "codexErrorInfo": "other"})
		got := cloudParseTurnError(raw)
		require.Empty(t, got.Code)
		require.False(t, cloudRejectedBeforeInference(got))
	}
}
