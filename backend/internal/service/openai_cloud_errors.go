package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Only an actor-confirmed execution terminal enters the common failure policy.
// Public tool handoff completion, local admission errors and cached replies do
// not describe another upstream attempt. The private policy payload retains
// the upstream status separately from the client-facing status translation.
func cloudTerminalFailureFacts(result *OpenAICloudResult) ([]byte, int) {
	if result == nil || !result.Terminal || gjson.GetBytes(result.Response, "status").String() != "failed" {
		return nil, 0
	}
	failure := gjson.GetBytes(result.Response, "error")
	if !failure.IsObject() || (failure.Get("upstream_method").String() == "" && !failure.Get("upstream_http_status").Exists() && failure.Get("upstream_error_info").String() == "") {
		return nil, 0
	}
	code := failure.Get("code").String()
	switch code {
	case "cloud_state_unavailable", "cloud_event_queue_overflow", "cloud_response_too_large", "cloud_first_output_timeout", "cloud_compaction_failed":
		return nil, 0
	}
	status := result.ErrorStatus
	if upstream := int(failure.Get("upstream_http_status").Int()); upstream >= 400 && upstream <= 599 {
		status = upstream
	}
	fields := map[string]any{"code": code, "message": failure.Get("message").String()}
	if code == "cloud_authorization_failed" {
		// A classified credential rejection has native auth semantics even when
		// Cloud's public mapping deliberately avoids blaming the caller's Key.
		fields["code"], fields["type"] = "unauthorized", "authentication_error"
		if !failure.Get("upstream_http_status").Exists() {
			status = http.StatusUnauthorized
		}
	}
	if param := failure.Get("param").String(); param != "" {
		fields["param"] = param
	}
	fields["status_code"] = status
	payload, _ := json.Marshal(map[string]any{"error": fields})
	return payload, status
}

func (s *OpenAIGatewayService) applyCloudTerminalAccountPolicy(ctx context.Context, account *Account, model string, result *OpenAICloudResult) {
	if result == nil || result.Replayed || result.AccountPolicyApplied {
		return
	}
	payload, status := cloudTerminalFailureFacts(result)
	if len(payload) == 0 || gjson.GetBytes(result.Response, "error.code").String() == "cloud_session_budget_exhausted" {
		return // a thread budget is not an account quota or a transient account 429
	}
	s.handleOpenAITerminalAccountFailure(ctx, account, status, payload, extractOpenAISSEErrorMessage(payload), nil, model)
}

func (s *OpenAIGatewayService) applyCloudOwnerTerminalPolicy(ctx context.Context, owner OpenAICloudOwner, result *OpenAICloudResult) bool {
	if result == nil || result.Replayed || result.AccountPolicyApplied {
		return true
	}
	if payload, _ := cloudTerminalFailureFacts(result); len(payload) == 0 {
		return true
	}
	if owner.Probe {
		return true
	}
	if s.cloudService == nil {
		return false
	}
	account, err := s.cloudService.account(ctx, owner.AccountID)
	if err != nil {
		return false
	}
	binding, err := s.cloudService.repo.GetCloudAuthorization(ctx, owner.AccountID)
	if err != nil {
		return false
	}
	if !cloudBindingMatches(account, binding) || binding.Generation != owner.GrantGeneration {
		return true // an old execution cannot penalize replacement credentials
	}
	s.applyCloudTerminalAccountPolicy(ctx, account, owner.Model, result)
	return true
}

// Presentation rules change a delivery copy, never the persisted terminal,
// execution proof, recovery classification or delayed settlement facts.
func projectCloudTerminalError(c *gin.Context, account *Account, result *OpenAICloudResult) *OpenAICloudResult {
	payload, upstreamStatus := cloudTerminalFailureFacts(result)
	if len(payload) == 0 || account == nil {
		return result
	}
	message := gjson.GetBytes(result.Response, "error.message").String()
	status, kind, message, matched := applyErrorPassthroughRule(c, account.Platform, upstreamStatus, payload, result.ErrorStatus, "upstream_error", message)
	if !matched {
		return result
	}
	var fields map[string]any
	if json.Unmarshal([]byte(gjson.GetBytes(result.Response, "error").Raw), &fields) != nil {
		return result
	}
	fields["status_code"], fields["type"], fields["message"] = status, kind, message
	raw, err := json.Marshal(fields)
	if err != nil {
		return result
	}
	body, err := sjson.SetRawBytes(result.Response, "error", raw)
	if err != nil {
		return result
	}
	copy := *result
	copy.Response, copy.ErrorStatus = body, status
	return &copy
}

const cloudErrorPayloadLimit = 64 << 10

// Only allowlisted protocol facts survive parsing. Messages, additionalDetails,
// request bodies and arbitrary data are never retained or returned to clients.
// Classification does not authorize a retry: accepted Cloud work stays bound to
// its original thread, credential and billing root even for a 429 or 503.
type cloudUpstreamError struct {
	HTTPStatus        int
	Code              string
	CodexErrorInfo    string
	GRPCStatus        int
	MessageBytes      int64
	MessageLimitBytes int64
}

// Observed on the real Cloud host, not a model context limit. Match only the
// RPC's own gRPC OUT_OF_RANGE envelope; echoed requests cannot supply facts.
var cloudMessageSizeSignature = regexp.MustCompile("^Operation was attempted past the valid range: Error, decoded message length too large: found ([0-9]+) bytes, the limit is: ([0-9]+) bytes$")

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

// Cloud reports a missing ephemeral thread through these observed JSON-RPC
// signatures. Inspect only the RPC error itself; nested messages and echoed
// requests cannot establish that the upstream thread no longer exists.
func cloudParseRPCErrorFacts(raw json.RawMessage) *cloudUpstreamError {
	result := cloudParseTurnError(raw)
	if result == nil || len(raw) > cloudErrorPayloadLimit || !cloudUniqueJSON(raw) {
		return result
	}
	var value map[string]json.RawMessage
	var code int
	var message string
	if json.Unmarshal(raw, &value) != nil || json.Unmarshal(value["code"], &code) != nil || json.Unmarshal(value["message"], &message) != nil {
		return result
	}
	var data struct {
		GRPCStatus int `json:"grpcStatusCode"`
	}
	if code == -32602 && json.Unmarshal(value["data"], &data) == nil && data.GRPCStatus == 11 {
		if parts := cloudMessageSizeSignature.FindStringSubmatch(message); len(parts) == 3 {
			found, foundErr := strconv.ParseInt(parts[1], 10, 64)
			limit, limitErr := strconv.ParseInt(parts[2], 10, 64)
			if foundErr == nil && limitErr == nil && limit > 0 && found > limit && found <= 1<<40 {
				result.Code, result.GRPCStatus = "cloud_message_too_large", data.GRPCStatus
				result.MessageBytes, result.MessageLimitBytes = found, limit
				return result
			}
		}
	}
	prefix := "unknown thread: "
	switch code {
	case -32600:
	case -32602:
		var data map[string]json.RawMessage
		var grpcStatus int
		if json.Unmarshal(value["data"], &data) != nil || json.Unmarshal(data["grpcStatusCode"], &grpcStatus) != nil || grpcStatus != 3 {
			return result
		}
		prefix = "Client specified an invalid argument: unknown thread: "
	default:
		return result
	}
	id, matched := strings.CutPrefix(message, prefix)
	if !matched || len(id) != 36 {
		return result
	}
	parsed, err := uuid.Parse(id)
	if err == nil && strings.EqualFold(parsed.String(), id) {
		result.Code = "cloud_thread_unavailable"
	}
	return result
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
	// Automatic compaction wraps the same provider envelope in this exact
	// observed prefix. Strip only one such context at the error boundary; never
	// search arbitrary messages or request echoes for embedded JSON.
	providerMessage := value.Message
	if depth == 0 {
		providerMessage = strings.TrimPrefix(providerMessage, "Error running remote compact task: ")
	}
	if depth == 0 && len(providerMessage) <= cloudErrorPayloadLimit && cloudUniqueJSON([]byte(providerMessage)) {
		var envelope struct {
			Type   string          `json:"type"`
			Error  json.RawMessage `json:"error"`
			Status *int            `json:"status"`
		}
		if json.Unmarshal([]byte(providerMessage), &envelope) == nil && envelope.Type == "error" && cloudUniqueJSON(envelope.Error) {
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
	// Queue exhaustion is a local resource failure, not evidence that Cloud
	// refused generation. Preserve this class without enabling replay.
	if errors.Is(err, errCloudRPCEventOverflow) {
		return http.StatusBadGateway, "cloud_event_queue_overflow", "The Cloud event backlog exceeded the gateway memory limit; the request was not replayed.", true
	}
	if errors.Is(err, errCloudRPCClosed) {
		return http.StatusBadGateway, "cloud_transport_closed", "The Cloud connection closed before the operation completed; the request was not replayed.", true
	}
	if errors.Is(err, errCloudRPCProtocol) {
		return http.StatusBadGateway, "cloud_protocol_error", "The Cloud server returned an invalid protocol message; the request was not replayed.", true
	}
	var rejection *cloudRejection
	if errors.As(err, &rejection) {
		return http.StatusBadRequest, rejection.Code, rejection.Error(), true
	}

	var gateway *cloudGatewayError
	if errors.As(err, &gateway) {
		return gateway.status, gateway.code, gateway.message, true
	}
	if errors.Is(err, ErrOpenAICloudAuthorization) {
		return http.StatusForbidden, "cloud_authorization_failed", "Native Codex authorization is unavailable or changed. Reconnect the native account.", true
	}
	if errors.Is(err, ErrOpenAICloudSettings) {
		return http.StatusBadRequest, "cloud_settings_invalid", "Cloud account or model settings do not allow this request.", true
	}
	if errors.Is(err, errCloudAuthorizationChanged) {
		return http.StatusBadRequest, "cloud_settings_invalid", errCloudAuthorizationChanged.Error(), true
	}
	if errors.Is(err, ErrOpenAITurnAttemptBudgetExhausted) || errors.Is(err, ErrTurnPreparationExhausted) || errors.Is(err, ErrTurnRetryWaitExhausted) {
		return http.StatusServiceUnavailable, "retry_budget_exhausted", "The request exhausted its upstream recovery budget.", true
	}
	if errors.Is(err, ErrOpenAIWSFirstOutputBudgetExhausted) {
		return http.StatusGatewayTimeout, "cloud_first_output_timeout", "Cloud did not produce output before the turn deadline.", true
	}
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
	case "cloud_message_too_large":
		return http.StatusRequestEntityTooLarge, "cloud_message_too_large", "A Cloud upstream message exceeded its decoded size limit; no generation was retried."
	case "invalid_encrypted_content":
		return http.StatusBadRequest, "invalid_encrypted_content", "Cloud could not verify the encrypted context. Supply valid history for the same account."
	case "invalid_compaction", "compaction_decode_error":
		return http.StatusBadRequest, "invalid_compaction", "Cloud could not verify the compacted context. Supply complete valid history."
	case "context_length_exceeded", "context_window_exceeded", "contextWindowExceeded":
		return http.StatusBadRequest, "context_length_exceeded", "The Cloud context limit was exceeded. Reduce the request context."
	case "sessionBudgetExceeded", "cloud_session_budget_exhausted":
		return http.StatusTooManyRequests, "cloud_session_budget_exhausted", "The Cloud session budget is exhausted."
	case "insufficient_quota", "quota_exceeded", "usageLimitExceeded":
		return http.StatusTooManyRequests, "insufficient_quota", "The Cloud account quota is exhausted."
	case "rate_limit_exceeded", "rateLimitExceeded":
		return http.StatusTooManyRequests, "rate_limit_exceeded", "The Cloud upstream rate limit was reached."
	case "server_is_overloaded", "slow_down", "serverOverloaded":
		return http.StatusServiceUnavailable, "server_is_overloaded", "The selected Cloud model is at capacity."
	case "flex_unavailable", "flexUnavailable":
		return http.StatusServiceUnavailable, "flex_unavailable", "Cloud flex capacity is unavailable."
	case "unauthorized", "invalid_api_key", "cloud_authorization_failed":
		return http.StatusForbidden, "cloud_authorization_failed", "Native Codex authorization was rejected. Reconnect the native account."
	case "invalid_request_error", "badRequest", "cloud_invalid_request":
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
	case "tooManyDenials", "cloud_approval_denied":
		return http.StatusForbidden, "cloud_approval_denied", "Cloud stopped the turn after repeated approval denials."
	case "activeTurnNotSteerable", "cloud_turn_conflict":
		return http.StatusConflict, "cloud_turn_conflict", "The active Cloud turn cannot accept this continuation."
	case "cloud_stream_disconnected":
		return http.StatusBadGateway, "cloud_stream_disconnected", "The Cloud event stream disconnected before completion; the request was not replayed."
	case "cloud_method_unavailable":
		return http.StatusBadGateway, "cloud_method_unavailable", "The Cloud server does not support the required protocol method."
	case "cloud_thread_unavailable":
		return http.StatusBadGateway, "cloud_thread_unavailable", "The Cloud upstream thread is no longer available; the request was not replayed."
	case "cloud_protocol_error":
		return http.StatusBadGateway, "cloud_protocol_error", "The Cloud server rejected the gateway protocol message."
	case "cloud_rpc_rejected":
		return http.StatusBadGateway, "cloud_rpc_rejected", "The Cloud server rejected the operation."
	}
	if e.HTTPStatus != 0 {
		switch e.HTTPStatus {
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return http.StatusBadRequest, "cloud_invalid_request", "Cloud rejected the request parameters."
		case http.StatusUnauthorized:
			return http.StatusForbidden, "cloud_authorization_failed", "Native Codex authorization was rejected. Reconnect the native account."
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

// CloudPreInferenceFailureReason connects proven pre-inference failures to the
// existing owner/target/turn budgets. It never changes the account's protocol.
const CloudPreInferenceFailureReason GatewayFailureReason = "cloud_pre_inference"

func cloudPreInferenceFailover(ctx context.Context, body []byte, status int, code, message string, cause error, unsent bool) *UpstreamFailoverError {
	if ctx.Err() != nil || IsTurnBudgetError(cause) || errors.Is(cause, context.Canceled) ||
		HasOpenAICloudContinuation(ctx) || TurnOutputCommitted(ctx) || !OpenAIResponsesAccountReplayAllowed(body) {
		return nil
	}
	// Bad input, policy refusal and settings/ownership changes are terminal.
	if status != http.StatusTooManyRequests && status < 500 && code != "cloud_authorization_failed" && code != "cloud_payment_required" && code != "model_not_found" {
		return nil
	}
	if status > 599 || code == "cloud_method_unavailable" || code == "cloud_protocol_error" || code == "cloud_rpc_rejected" {
		return nil
	}
	// A missing upstream thread is an execution-identity failure, not transient
	// capacity. Its 502 classification must not enter generic account replay.
	if code == "cloud_thread_unavailable" || code == "cloud_state_unavailable" {
		return nil
	}
	failure := &UpstreamFailoverError{Cause: cause, StatusCode: status,
		Stage: GatewayFailureStageInference, Scope: GatewayFailureScopeAccount,
		Reason: CloudPreInferenceFailureReason, NextAccountAction: NextAccountRetry,
		RequestNotSent: unsent, RejectedBeforeGeneration: true, SafeToFailoverAfterWrite: true,
		RetryableOnSameAccount: status >= 500,
		ClientStatusCode:       status, ClientMessage: message}
	if code == "cloud_authorization_failed" {
		failure.Stage = GatewayFailureStageAccountAuth
	}
	return failure
}

// Cloud supplies the proof that replay is safe; the ordinary OAuth policy
// owns same-account transient-429 eligibility, delay and its existing window.
// Only an explicit upstream rate-limit category carries that meaning here:
// a quota/session-budget refusal or unclassified usage check must not be
// reinterpreted as transient by fabricating native quota/reset headers.
func (s *OpenAIGatewayService) cloudPreInferenceFailover(ctx context.Context, account *Account, body []byte, status int, code, message string, cause error, unsent bool) *UpstreamFailoverError {
	failure := cloudPreInferenceFailover(ctx, body, status, code, message, cause, unsent)
	if failure == nil || s == nil || account == nil || status != http.StatusTooManyRequests || code != "rate_limit_exceeded" {
		return failure
	}
	failure.RetryableOnSameAccount = s.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, status, false, nil, nil)
	if failure.RetryableOnSameAccount {
		failure.SameAccountRetryDeadline = s.openAIOAuth429RetryDeadline(account)
		failure.SameAccountRetryDelay = openAIOAuth429SameAccountRetryDelay(nil)
	}
	return failure
}

// A failed public response or zero token count alone cannot establish this.
// The fenced actor proves either no turn/start or an explicit pre-inference
// rejection, with no observed model/tool work or usage.
func cloudResultProvesNoInference(result *OpenAICloudResult, root string) bool {
	return result != nil && result.RejectedBeforeInference && result.Terminal && !result.Replayed &&
		!result.AwaitingTools && result.UsageCredible && result.RootRequestID == root &&
		cloudRecoveryUsageEqual(result.Usage, &OpenAICloudUsage{})
}

// cloudThreadDiscarded reports the exact "unknown thread" RPC signature.
func cloudThreadDiscarded(err error) bool {
	var rpc *cloudRPCError
	return errors.As(err, &rpc) && rpc.Upstream != nil && rpc.Upstream.Code == "cloud_thread_unavailable"
}

// Share the same closed vocabulary between RPC capture and diagnostic export.
func cloudDiagnosticMethod(method string) string {
	switch method {
	case "initialize", "thread/start", "thread/inject_items", "thread/archive", "turn/start", "turn/interrupt", "turn/steer", "turn/completed", "error":
		return method
	default:
		return ""
	}
}
