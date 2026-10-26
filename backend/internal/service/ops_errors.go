package service

import (
	"context"
	"encoding/json"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *OpsService) GetErrorTrend(ctx context.Context, filter *OpsDashboardFilter, bucketSeconds int) (*OpsErrorTrendResponse, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return nil, infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	if filter == nil {
		return nil, infraerrors.BadRequest("OPS_FILTER_REQUIRED", "filter is required")
	}
	if filter.StartTime.IsZero() || filter.EndTime.IsZero() {
		return nil, infraerrors.BadRequest("OPS_TIME_RANGE_REQUIRED", "start_time/end_time are required")
	}
	if filter.StartTime.After(filter.EndTime) {
		return nil, infraerrors.BadRequest("OPS_TIME_RANGE_INVALID", "start_time must be <= end_time")
	}
	filter.QueryMode = s.resolveOpsQueryMode(ctx, filter.QueryMode)

	result, err := s.opsRepo.GetErrorTrend(ctx, filter, bucketSeconds)
	if err != nil && shouldFallbackOpsPreagg(filter, err) {
		rawFilter := cloneOpsFilterWithMode(filter, OpsQueryModeRaw)
		return s.opsRepo.GetErrorTrend(ctx, rawFilter, bucketSeconds)
	}
	return result, err
}

func (s *OpsService) GetErrorDistribution(ctx context.Context, filter *OpsDashboardFilter) (*OpsErrorDistributionResponse, error) {
	if err := s.RequireMonitoringEnabled(ctx); err != nil {
		return nil, err
	}
	if s.opsRepo == nil {
		return nil, infraerrors.ServiceUnavailable("OPS_REPO_UNAVAILABLE", "Ops repository not available")
	}
	if filter == nil {
		return nil, infraerrors.BadRequest("OPS_FILTER_REQUIRED", "filter is required")
	}
	if filter.StartTime.IsZero() || filter.EndTime.IsZero() {
		return nil, infraerrors.BadRequest("OPS_TIME_RANGE_REQUIRED", "start_time/end_time are required")
	}
	if filter.StartTime.After(filter.EndTime) {
		return nil, infraerrors.BadRequest("OPS_TIME_RANGE_INVALID", "start_time must be <= end_time")
	}
	filter.QueryMode = s.resolveOpsQueryMode(ctx, filter.QueryMode)

	result, err := s.opsRepo.GetErrorDistribution(ctx, filter)
	if err != nil && shouldFallbackOpsPreagg(filter, err) {
		rawFilter := cloneOpsFilterWithMode(filter, OpsQueryModeRaw)
		return s.opsRepo.GetErrorDistribution(ctx, rawFilter)
	}
	return result, err
}

// The actor emits only reasons selected from these exact internal stop sites.
// Diagnostic ingestion uses the same vocabulary and never stores free-form
// stop messages supplied by a client or provider.
var cloudGatewayStopReasons = map[string]string{
	"Cloud client cancelled the request":              "client_cancelled",
	context.Canceled.Error():                          "context_cancelled",
	context.DeadlineExceeded.Error():                  "request_deadline_exceeded",
	"Cloud authorization or account settings changed": "authorization_changed",
	errCloudStateUnavailable.Error():                  "authority_check_unavailable",
	"Cloud client tool response idle timeout":         "tool_idle_timeout",
	"Cloud session expired or service stopped":        "service_stopping",
}

func cloudKnownGatewayStopReason(reason string) string {
	for _, known := range cloudGatewayStopReasons {
		if reason == known {
			return reason
		}
	}
	return ""
}

// Extract bounded public failure facts for stored Cloud failure events; raw messages are excluded.
func cloudDiagnosticFailure(raw string) map[string]any {
	if len(raw) > 1<<20 {
		return nil
	}
	if json.Valid([]byte(raw)) {
		return cloudDiagnosticFailureJSON([]byte(raw), "", false)
	}
	// Ops can persist the delivered SSE frame, not just its JSON data. Read
	// actual SSE fields and terminal event types; never search messages or
	// arbitrary embedded JSON for something that resembles an error.
	var failure map[string]any
	var data []string
	var event string
	flush := func() {
		if len(data) > 0 {
			if safe := cloudDiagnosticFailureJSON([]byte(strings.Join(data, "\n")), event, true); safe != nil {
				failure = safe
			}
		}
		data, event = nil, ""
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			flush()
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		} else if value, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimPrefix(value, " ")
		}
	}
	flush()
	return failure
}

func cloudDiagnosticFailureJSON(raw []byte, event string, stream bool) map[string]any {
	var body struct {
		Type     string
		Error    json.RawMessage
		Response struct{ Error json.RawMessage }
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	if stream && (body.Type != "response.failed" && body.Type != "error" || event != "" && event != body.Type) {
		return nil
	}
	if body.Type != "" && body.Type != "response.failed" && body.Type != "error" {
		return nil
	}
	encoded := body.Error
	if len(encoded) == 0 {
		encoded = body.Response.Error
	}
	var facts map[string]json.RawMessage
	if json.Unmarshal(encoded, &facts) != nil {
		return nil
	}
	out := make(map[string]any)
	for _, key := range []string{"code", "param", "upstream_error_code", "upstream_method", "upstream_error_info", "gateway_stop_reason", "upstream_turn_status"} {
		var value string
		if json.Unmarshal(facts[key], &value) != nil {
			continue
		}
		switch key {
		case "param":
			value = cloudSafeParameter(value)
		case "upstream_error_code":
			value = cloudKnownErrorCode(value)
		case "upstream_turn_status":
			if value != "failed" && value != "interrupted" {
				value = ""
			}
		case "gateway_stop_reason":
			value = cloudKnownGatewayStopReason(value)
		case "upstream_method":
			value = cloudDiagnosticMethod(value)
		case "upstream_error_info":
			switch value {
			case "httpConnectionFailed", "responseStreamConnectionFailed", "responseStreamDisconnected", "responseTooManyFailedAttempts", "activeTurnNotSteerable":
				// These variants carry objects on the wire, then the parser keeps
				// only their known variant name for the public error.
			default:
				value = cloudKnownCodexErrorInfo(value)
			}
		case "code":
			switch value {
			case "cloud_invalid_request", "cloud_execution_failed", "cloud_upstream_error", "cloud_rpc_rejected", "cloud_method_unavailable", "cloud_protocol_error", "cloud_thread_unavailable", "cloud_stream_disconnected", "cloud_turn_conflict", "cloud_authorization_failed", "cloud_event_queue_overflow", "cloud_transport_closed", "cloud_upstream_timeout", "cloud_request_timeout", "cloud_message_too_large", "cloud_history_item_too_large", "cloud_history_rejected", "cloud_state_unavailable":
			default:
				value = cloudKnownErrorCode(value)
			}
		}
		if value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"upstream_rpc_code", "upstream_http_status", "history_recovery_attempts"} {
		var fact *int
		if json.Unmarshal(facts[key], &fact) == nil && fact != nil {
			value := *fact
			if key == "upstream_rpc_code" && value >= -32768 && value < 0 || key == "upstream_http_status" && value >= 400 && value <= 599 || key == "history_recovery_attempts" && value >= 0 && value <= 2 {
				out[key] = value
			}
		}
	}
	for _, key := range []string{"upstream_grpc_status", "upstream_message_bytes", "upstream_message_limit_bytes"} {
		var value int64
		if json.Unmarshal(facts[key], &value) == nil && (key == "upstream_grpc_status" && value == 11 || key != "upstream_grpc_status" && value > 0 && value <= 1<<40) {
			out[key] = value
		}
	}
	for _, key := range []string{"output_observed", "usage_observed"} {
		var value *bool
		if json.Unmarshal(facts[key], &value) == nil && value != nil {
			out[key] = *value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
