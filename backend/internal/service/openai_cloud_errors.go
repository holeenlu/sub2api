package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

const cloudErrorPayloadLimit = 64 << 10

// Only allowlisted protocol facts survive parsing. Messages, additionalDetails,
// request bodies and arbitrary data are never retained or returned to clients.
// Classification does not authorize a retry: accepted Cloud work stays bound to
// its original thread, credential and billing root even for a 429 or 503.
type cloudUpstreamError struct {
	HTTPStatus     int
	Code           string
	CodexErrorInfo string
}

func (e *cloudUpstreamError) Error() string {
	_, _, message := cloudUpstreamErrorResponse(e)
	return message
}

type cloudErrorNotification struct {
	ThreadID  string
	TurnID    string
	WillRetry bool
	Err       *cloudUpstreamError
}

// The caller must validate thread/turn ownership before using the notification.
// WillRetry describes an upstream retry already in progress, not permission for
// this gateway to replay a request. Such a notice is not a terminal event.
func cloudParseErrorNotification(raw json.RawMessage) (*cloudErrorNotification, error) {
	var value struct {
		ThreadID  string          `json:"threadId"`
		TurnID    string          `json:"turnId"`
		WillRetry *bool           `json:"willRetry"`
		Error     json.RawMessage `json:"error"`
	}
	if len(raw) > cloudErrorPayloadLimit || !cloudUniqueJSON(raw) || json.Unmarshal(raw, &value) != nil ||
		value.ThreadID == "" || len(value.ThreadID) > 512 || value.TurnID == "" || len(value.TurnID) > 512 || value.WillRetry == nil {
		return nil, errCloudRPCProtocol
	}
	upstream := cloudParseTurnError(value.Error)
	if upstream == nil {
		return nil, errCloudRPCProtocol
	}
	return &cloudErrorNotification{ThreadID: value.ThreadID, TurnID: value.TurnID, WillRetry: *value.WillRetry, Err: upstream}, nil
}

// cloudParseTurnError accepts the TurnError object used by error notifications
// and turn/completed. A malformed non-null error remains a generic failure.
func cloudParseTurnError(raw json.RawMessage) *cloudUpstreamError {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if len(raw) > cloudErrorPayloadLimit || !cloudUniqueJSON(raw) {
		return &cloudUpstreamError{}
	}
	return cloudParseErrorFacts(raw, 0)
}

func cloudParseErrorFacts(raw json.RawMessage, depth int) *cloudUpstreamError {
	result := &cloudUpstreamError{}
	var value struct {
		Code           json.RawMessage `json:"code"`
		CodexErrorInfo json.RawMessage `json:"codexErrorInfo"`
		HTTPStatus     *int            `json:"httpStatusCode"`
		Message        string          `json:"message"`
		Data           json.RawMessage `json:"data"`
		Error          json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return result
	}
	var code string
	_ = json.Unmarshal(value.Code, &code)
	result.Code = cloudKnownErrorCode(code)
	if value.HTTPStatus != nil && *value.HTTPStatus >= 400 && *value.HTTPStatus <= 599 {
		result.HTTPStatus = *value.HTTPStatus
	}
	var info string
	if json.Unmarshal(value.CodexErrorInfo, &info) == nil {
		result.CodexErrorInfo = cloudKnownCodexErrorInfo(info)
	} else {
		var variants map[string]json.RawMessage
		if json.Unmarshal(value.CodexErrorInfo, &variants) == nil && len(variants) == 1 {
			for name, fields := range variants {
				switch name {
				case "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts", "activeTurnNotSteerable":
					result.CodexErrorInfo = name
					var details struct {
						HTTPStatus *int `json:"httpStatusCode"`
					}
					if json.Unmarshal(fields, &details) == nil && details.HTTPStatus != nil && *details.HTTPStatus >= 400 && *details.HTTPStatus <= 599 {
						result.HTTPStatus = *details.HTTPStatus
					}
				}
			}
		}
	}
	// Reuse the native exact encrypted-context rejection contract. Only the
	// error's own message is examined; echoed prompts/requests are never scanned.
	// The temporary wrapper is discarded and no provider text is retained.
	encrypted := map[string]any{"message": value.Message}
	if code != "" {
		encrypted["code"] = code
	}
	wrapped, _ := json.Marshal(map[string]any{"error": encrypted})
	if isOpenAIEncryptedContextRejection(wrapped) {
		result.Code = "invalid_encrypted_content"
	}
	// Cloud sometimes wraps the provider's structured Responses error in the
	// top-level TurnError.message string (codexErrorInfo remains "other").
	// Recognize that one observed envelope only; never recursively interpret
	// arbitrary messages, request echoes or free-form JSON strings.
	if depth == 0 && len(value.Message) <= cloudErrorPayloadLimit && cloudUniqueJSON([]byte(value.Message)) {
		var envelope struct {
			Type   string          `json:"type"`
			Error  json.RawMessage `json:"error"`
			Status *int            `json:"status"`
		}
		if json.Unmarshal([]byte(value.Message), &envelope) == nil && envelope.Type == "error" && cloudUniqueJSON(envelope.Error) {
			inner := cloudParseErrorFacts(envelope.Error, 2)
			if result.Code == "" {
				result.Code = inner.Code
			}
			if result.HTTPStatus == 0 && envelope.Status != nil && *envelope.Status >= 400 && *envelope.Status <= 599 {
				result.HTTPStatus = *envelope.Status
			}
		}
	}
	// JSON-RPC data and its error object are the only nested envelopes examined.
	// Never recursively search arbitrary provider payloads for status or text.
	if depth < 2 {
		for _, nested := range []json.RawMessage{value.Error, value.Data} {
			child := cloudParseErrorFacts(nested, depth+1)
			if result.Code == "" {
				result.Code = child.Code
			}
			if result.CodexErrorInfo == "" {
				result.CodexErrorInfo = child.CodexErrorInfo
			}
			if result.HTTPStatus == 0 {
				result.HTTPStatus = child.HTTPStatus
			}
		}
	}
	return result
}

func cloudKnownErrorCode(code string) string {
	switch code {
	case "invalid_encrypted_content", "invalid_compaction", "compaction_decode_error",
		"context_length_exceeded", "context_window_exceeded", "insufficient_quota", "quota_exceeded",
		"rate_limit_exceeded", "server_is_overloaded", "slow_down", "flex_unavailable",
		"unauthorized", "invalid_api_key", "invalid_request_error", "unsupported_parameter",
		"model_not_found", "previous_response_not_found", "cyber_policy", "content_policy_violation":
		return code
	default:
		return ""
	}
}

func cloudKnownCodexErrorInfo(info string) string {
	switch info {
	case "contextWindowExceeded", "sessionBudgetExceeded", "usageLimitExceeded", "rateLimitExceeded",
		"flexUnavailable", "serverOverloaded", "cyberPolicy", "misalignmentPolicyViolation", "tooManyDenials",
		"internalServerError", "unauthorized", "badRequest", "threadRollbackFailed", "sandboxError", "other":
		return info
	default:
		return ""
	}
}

// cloudPublicError returns fixed public diagnostics for recognized Cloud errors.
// It preserves useful status without returning authentication challenges for an
// upstream credential (the customer's Sub2API API key may still be valid).
func cloudPublicError(err error) (status int, code, message string, ok bool) {
	var upstream *cloudUpstreamError
	if errors.As(err, &upstream) {
		status, code, message = cloudUpstreamErrorResponse(upstream)
		return status, code, message, true
	}
	var handshake *cloudHandshakeError
	if errors.As(err, &handshake) {
		status, code, message = cloudUpstreamErrorResponse(&cloudUpstreamError{HTTPStatus: handshake.StatusCode})
		return status, code, message, true
	}
	var rpc *cloudRPCError
	if !errors.As(err, &rpc) {
		return 0, "", "", false
	}
	if rpc.Upstream != nil && (rpc.Upstream.Code != "" || rpc.Upstream.CodexErrorInfo != "" || rpc.Upstream.HTTPStatus != 0) {
		status, code, message = cloudUpstreamErrorResponse(rpc.Upstream)
		return status, code, message, true
	}
	switch rpc.Code {
	case -32602:
		return http.StatusBadRequest, "cloud_invalid_request", "Cloud rejected the request parameters.", true
	case -32601:
		return http.StatusBadGateway, "cloud_method_unavailable", "The Cloud server does not support the required protocol method.", true
	case -32700, -32600:
		return http.StatusBadGateway, "cloud_protocol_error", "The Cloud server rejected the gateway protocol message.", true
	default:
		return http.StatusBadGateway, "cloud_rpc_rejected", "The Cloud server rejected the operation.", true
	}
}

func cloudUpstreamErrorResponse(e *cloudUpstreamError) (int, string, string) {
	if e == nil {
		return http.StatusBadGateway, "cloud_upstream_error", "Cloud execution failed; the request was not replayed."
	}
	classification := e.Code
	if classification == "" {
		classification = e.CodexErrorInfo
	}
	switch classification {
	case "invalid_encrypted_content":
		return http.StatusBadRequest, "invalid_encrypted_content", "Cloud could not verify the encrypted context. Supply valid history for the same account."
	case "invalid_compaction", "compaction_decode_error":
		return http.StatusBadRequest, "invalid_compaction", "Cloud could not verify the compacted context. Supply complete valid history."
	case "context_length_exceeded", "context_window_exceeded", "contextWindowExceeded":
		return http.StatusBadRequest, "context_length_exceeded", "The Cloud context limit was exceeded. Reduce the request context."
	case "sessionBudgetExceeded":
		return http.StatusTooManyRequests, "cloud_session_budget_exhausted", "The Cloud session budget is exhausted."
	case "insufficient_quota", "quota_exceeded", "usageLimitExceeded":
		return http.StatusTooManyRequests, "insufficient_quota", "The Cloud account quota is exhausted."
	case "rate_limit_exceeded", "rateLimitExceeded":
		return http.StatusTooManyRequests, "rate_limit_exceeded", "The Cloud upstream rate limit was reached."
	case "server_is_overloaded", "slow_down", "serverOverloaded":
		return http.StatusServiceUnavailable, "server_is_overloaded", "The selected Cloud model is at capacity."
	case "flex_unavailable", "flexUnavailable":
		return http.StatusServiceUnavailable, "flex_unavailable", "Cloud flex capacity is unavailable."
	case "unauthorized", "invalid_api_key":
		return http.StatusForbidden, "cloud_authorization_failed", "Cloud authorization was rejected. Reauthorize this account."
	case "invalid_request_error", "badRequest":
		return http.StatusBadRequest, "cloud_invalid_request", "Cloud rejected the request parameters."
	case "unsupported_parameter":
		return http.StatusBadRequest, "unsupported_parameter", "Cloud does not support a supplied request parameter."
	case "model_not_found":
		return http.StatusBadRequest, "model_not_found", "The selected model is unavailable for this Cloud account."
	case "previous_response_not_found":
		return http.StatusBadRequest, "previous_response_not_found", "Cloud could not find the referenced previous response."
	case "cyber_policy", "cyberPolicy":
		return http.StatusForbidden, "cyber_policy", "Cloud rejected this request under its cyber safety policy."
	case "content_policy_violation", "misalignmentPolicyViolation":
		return http.StatusForbidden, "content_policy_violation", "Cloud rejected this request under its content policy."
	case "tooManyDenials":
		return http.StatusForbidden, "cloud_approval_denied", "Cloud stopped the turn after repeated approval denials."
	case "activeTurnNotSteerable":
		return http.StatusConflict, "cloud_turn_conflict", "The active Cloud turn cannot accept this continuation."
	}
	if e.HTTPStatus != 0 {
		switch e.HTTPStatus {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return http.StatusBadRequest, "cloud_invalid_request", "Cloud rejected the request parameters."
		case http.StatusUnauthorized:
			return http.StatusForbidden, "cloud_authorization_failed", "Cloud authorization was rejected. Reauthorize this account."
		case http.StatusPaymentRequired:
			return http.StatusPaymentRequired, "cloud_payment_required", "The Cloud account requires payment before it can run this request."
		case http.StatusForbidden:
			return http.StatusForbidden, "cloud_access_denied", "The Cloud account does not have access to this operation."
		case http.StatusConflict:
			return http.StatusConflict, "cloud_turn_conflict", "Cloud rejected a conflicting operation."
		case http.StatusRequestEntityTooLarge:
			return http.StatusRequestEntityTooLarge, "request_too_large", "The request exceeds the Cloud upstream size limit."
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests, "rate_limit_exceeded", "The Cloud upstream rate limit was reached."
		case http.StatusRequestTimeout, http.StatusGatewayTimeout:
			return http.StatusGatewayTimeout, "cloud_upstream_timeout", "The Cloud operation timed out; the request was not replayed."
		case http.StatusServiceUnavailable, 529:
			return http.StatusServiceUnavailable, "server_is_overloaded", "The Cloud service is temporarily unavailable."
		}
	}
	if e.CodexErrorInfo == "responseStreamDisconnected" {
		return http.StatusBadGateway, "cloud_stream_disconnected", "The Cloud event stream disconnected before completion; the request was not replayed."
	}
	return http.StatusBadGateway, "cloud_upstream_error", "Cloud execution failed; the request was not replayed."
}

// Only authoritative rejection categories can prove that a model request was
// refused. The actor additionally requires no observed inference or tool work.
// Network, stream, internal and generic errors never grant this proof.
func cloudRejectedBeforeInference(err *cloudUpstreamError) bool {
	if err == nil {
		return false
	}
	switch err.Code {
	case "invalid_encrypted_content", "invalid_compaction", "compaction_decode_error", "invalid_request_error", "unsupported_parameter", "model_not_found", "context_length_exceeded", "context_window_exceeded", "unauthorized", "invalid_api_key", "insufficient_quota", "quota_exceeded", "rate_limit_exceeded", "server_is_overloaded":
		return true
	}
	switch err.CodexErrorInfo {
	case "badRequest", "unauthorized", "contextWindowExceeded", "usageLimitExceeded", "rateLimitExceeded", "serverOverloaded":
		return true
	}
	return false
}
