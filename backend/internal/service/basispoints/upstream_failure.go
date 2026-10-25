package basispoints

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const FileOwnershipFailureCode = "basispoints_file_ownership_error"

// ParseHTTPFailure shares terminal classification while keeping the actual HTTP
// status authoritative. A successful HTTP stream must use ParseUpstreamFailure.
func ParseHTTPFailure(status int, raw []byte) *UpstreamFailure {
	if status < 400 || status > 599 {
		return nil
	}
	var payload object
	if decode(raw, &payload) != nil {
		return nil
	}
	return classifyUpstreamFailure("error", object{"error": payload["error"], "status": json.Number(strconv.Itoa(status))})
}

// UpstreamFailure is a safe interpretation of an in-band or bridge protocol
// error. Status is semantic, not evidence of the upstream HTTP status.
type UpstreamFailure struct {
	Status  int
	Code    string
	Type    string
	Message string
	// Diagnostic holds error fields or bounded bridge metadata, never output.
	// The gateway must redact it before storage; it is not client-facing.
	Diagnostic json.RawMessage `json:"-"`
	// Local is trusted converter provenance, never parsed from upstream JSON.
	Local bool `json:"-"`
}

func (f *UpstreamFailure) Error() string { return f.Message }

func (f *UpstreamFailure) Details() map[string]any {
	return object{"status": f.Status, "code": f.Code, "type": f.Type, "message": f.Message}
}

// ParseUpstreamFailure recognizes failure terminals only. Unknown identifiers
// and free-form upstream messages never enter client responses. Diagnostics
// remain separate so the gateway can apply its existing redaction policy.
func ParseUpstreamFailure(raw []byte) *UpstreamFailure {
	var payload object
	if decode(raw, &payload) != nil {
		return nil
	}
	return classifyUpstreamFailure(text(payload["type"]), payload)
}

func classifyUpstreamFailure(kind string, payload object) *UpstreamFailure {
	switch kind {
	case "error", "response.failed", "response.cancelled":
	default:
		return nil
	}
	response, _ := payload["response"].(object)
	detail, _ := response["error"].(object)
	if detail == nil {
		detail, _ = payload["error"].(object)
	}
	if detail == nil && kind == "error" {
		detail = payload
	}
	code := text(detail["code"])
	if failureIdentifierStatus(code) == 0 && isContextWindowFailure(code, text(detail["message"])) {
		// Codex compacts only on this identifier; unknown spellings would otherwise
		// collapse to basispoints_upstream_error. Known codes keep their meaning.
		code = "context_length_exceeded"
	}
	status := failureIdentifierStatus(code)
	if status == 0 {
		code = "basispoints_upstream_error"
		if kind == "response.cancelled" {
			code = "basispoints_upstream_cancelled"
		}
		status = failureIdentifierStatus(text(detail["type"]))
	}
	// An explicit error status takes precedence over an inconsistent code/type.
	for _, value := range []any{payload["status"], payload["status_code"], detail["status"], detail["status_code"], response["status_code"]} {
		if explicit := failureStatus(value); explicit != 0 {
			status = explicit
			break
		}
	}
	if status == 0 {
		status = http.StatusBadGateway
	}
	// This endpoint currently supplies no machine code for ownership failures.
	// Match its exact error envelope, never user prose or substring matches.
	// It does not prove that replaying an accepted generation would be safe.
	if status >= 500 && text(detail["type"]) == "server_error" &&
		text(detail["message"]) == "Unknown error while validating file ownership." {
		switch text(detail["code"]) {
		case "", "server_error", "internal_server_error":
			code = FileOwnershipFailureCode
		}
	}
	errorType := "server_error"
	switch status {
	case http.StatusUnauthorized:
		errorType = "authentication_error"
	case http.StatusForbidden:
		errorType = "permission_error"
	case http.StatusTooManyRequests:
		errorType = "rate_limit_error"
	default:
		if status < 500 {
			errorType = "invalid_request_error"
		}
	}
	fields := object{}
	for _, key := range []string{"code", "type", "message", "param"} {
		if value, ok := detail[key].(string); ok {
			if len(value) > 8192 {
				value = value[:8192]
			}
			fields[key] = value
		}
	}
	diagnostic, _ := json.Marshal(object{"error": fields, "status": status})
	message := "Excel BPS upstream failure: " + code + "; request was not replayed"
	if code == FileOwnershipFailureCode {
		message = "Excel BPS could not validate file ownership; resend the original image data if available, or use the channel that owns the files; request was not replayed and account scheduling was not changed"
	} else if code == "first_output_timeout" {
		message = "Excel BPS produced no semantic output before the deadline; request was not replayed"
	}
	return &UpstreamFailure{Status: status, Code: code, Type: errorType, Diagnostic: diagnostic, Message: message}
}

// isContextWindowFailure applies the gateway's shared context-window phrase
// rules to the error code and message fields. The matched text is used for
// classification only, never echoed.
func isContextWindowFailure(values ...string) bool {
	return openai.IsContextWindowErrorText(values...)
}

func failureStatus(value any) int {
	number, ok := value.(json.Number)
	if !ok {
		return 0
	}
	status, err := number.Int64()
	if err != nil || status < 400 || status > 599 {
		return 0
	}
	return int(status)
}

func failureIdentifierStatus(identifier string) int {
	switch identifier {
	case "invalid_request", "invalid_request_error", "bad_request_error", "invalid_argument", "invalid_value",
		"unsupported_value", "invalid_prompt", "context_length_exceeded", "string_above_max_length",
		"previous_response_not_found", "thinking_signature_invalid", "invalid_encrypted_content",
		"cyber_policy", "content_policy_violation":
		return http.StatusBadRequest
	case "authentication_error", "invalid_api_key", "token_expired":
		return http.StatusUnauthorized
	case "permission_error", "permission_denied", "basispoints_model_access_changed", "model_not_allowed":
		return http.StatusForbidden
	case "model_not_found", "model_not_found_error":
		return http.StatusNotFound
	case "message_too_big":
		return http.StatusRequestEntityTooLarge
	case "rate_limit_error", "rate_limit_exceeded", "insufficient_quota", "usage_limit_reached":
		return http.StatusTooManyRequests
	case "server_error", "internal_server_error", FileOwnershipFailureCode:
		return http.StatusInternalServerError
	case "overloaded_error", "server_is_overloaded", "service_unavailable", "service_unavailable_error":
		return http.StatusServiceUnavailable
	case "first_output_timeout":
		return http.StatusGatewayTimeout
	case "api_error", "basispoints_upstream_error", "basispoints_upstream_cancelled", "basispoints_protocol_error":
		return http.StatusBadGateway
	default:
		return 0
	}
}

// normalizeFailureEvent preserves response identity/output/usage after the
// bridge has withheld tools, but replaces error details with safe identifiers.
func normalizeFailureEvent(kind string, payload object) object {
	failure := classifyUpstreamFailure(kind, payload)
	if failure == nil {
		return payload
	}
	out := object{"status": failure.Status}
	if usage, ok := payload["usage"].(object); ok {
		out["usage"] = usage
	}
	if kind == "error" {
		out["error"] = failure.Details()
	} else {
		response, _ := payload["response"].(object)
		if response == nil {
			response = object{"status": "failed", "output": []any{}}
			if kind == "response.cancelled" {
				response["status"] = "cancelled"
			}
		}
		response["error"] = failure.Details()
		out["response"] = response
	}
	return out
}
