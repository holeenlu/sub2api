package service

import (
	"context"
	"encoding/json"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OpsRequestDiagnostics 从已有观测投影可分享的数值和已知错误事实，不导出正文、凭据、
// 用户资料、账号名称或上游地址，也不把未标明丢失的记录宣称为完整执行历史。
type OpsRequestDiagnostics struct {
	Failure          map[string]any            `json:"failure,omitempty"`
	Version          int                       `json:"version"`
	RequestID        string                    `json:"request_id"`
	Status           int                       `json:"status"`
	Timings          map[string]*int64         `json:"timings_ms"`
	Events           []OpsDiagnosticEvent      `json:"events"`
	Scheduling       []OpsDiagnosticScheduling `json:"scheduling"`
	Coverage         string                    `json:"coverage"`
	DroppedAttempts  int                       `json:"dropped_attempts"`
	DroppedDecisions int                       `json:"dropped_decisions"`
	Gaps             []string                  `json:"gaps"`
}

type OpsDiagnosticEvent struct {
	Detail *cloudAdaptationDetail `json:"detail,omitempty"`
	At     int64                  `json:"at_unix_ms"`
	Kind   string                 `json:"kind"`
	Status int                    `json:"upstream_status"`
}

type OpsDiagnosticScheduling struct {
	Stage           string `json:"stage"`
	ElapsedMs       int64  `json:"elapsed_ms"`
	DurationMs      int64  `json:"duration_ms"`
	Candidates      int    `json:"candidates"`
	OldestWaitMs    int64  `json:"oldest_wait_ms"`
	BypassedWaiters int    `json:"bypassed_waiters"`
}

func BuildOpsRequestDiagnostics(detail *OpsErrorLogDetail) *OpsRequestDiagnostics {
	out := &OpsRequestDiagnostics{Version: 1, Coverage: "recorded_events_only", Events: []OpsDiagnosticEvent{}, Scheduling: []OpsDiagnosticScheduling{}, Gaps: []string{"best_effort_capture"}}
	if detail == nil {
		out.Gaps = append(out.Gaps, "missing_record")
		return out
	}
	out.RequestID, out.Status = detail.RequestID, detail.StatusCode
	out.Failure = cloudDiagnosticFailure(detail.ErrorBody)
	if detail.RequestStatusCode != nil {
		out.Status = *detail.RequestStatusCode
	}
	out.Timings = map[string]*int64{"auth": detail.AuthLatencyMs, "routing": detail.RoutingLatencyMs, "upstream": detail.UpstreamLatencyMs, "response": detail.ResponseLatencyMs, "legacy_first_token": detail.TimeToFirstTokenMs}
	events, err := ParseOpsUpstreamErrors(detail.UpstreamErrors)
	if err != nil {
		out.Gaps = append(out.Gaps, "invalid_events")
		return out
	}
	for _, event := range events {
		if event == nil {
			continue
		}
		out.DroppedAttempts += max(0, event.DroppedEarlierAttempts)
		if event.Kind == opsSchedulingTrace {
			var trace struct {
				Dropped           int                  `json:"dropped_events"`
				DecisionsComplete *bool                `json:"decisions_complete"`
				Events            []SchedulingDecision `json:"events"`
			}
			if len(event.Detail) > maxSchedulingTraceBytes || json.Unmarshal([]byte(event.Detail), &trace) != nil {
				out.Gaps = append(out.Gaps, "invalid_scheduling")
				continue
			}
			out.DroppedDecisions += max(0, trace.Dropped)
			// Legacy traces do not assert completeness. Payload clipping can lose
			// facts without dropping an entire decision, so the count alone is insufficient.
			if trace.DecisionsComplete != nil && !*trace.DecisionsComplete {
				out.Gaps = append(out.Gaps, "incomplete_scheduling")
			}
			for _, decision := range trace.Events {
				if len(out.Scheduling) >= maxSchedulingTraceEvents {
					out.DroppedDecisions++
					continue
				}
				stage := "other"
				switch decision.Stage {
				case "selection", "filter", "affinity", "queue", "slot", "retry", "recovery":
					stage = decision.Stage
				}
				out.Scheduling = append(out.Scheduling, OpsDiagnosticScheduling{Stage: stage, ElapsedMs: decision.ElapsedMs, DurationMs: decision.DurationMs, Candidates: decision.Candidates, OldestWaitMs: decision.OldestWaitMs, BypassedWaiters: decision.BypassedWaiters})
			}
			continue
		}
		if len(out.Events) >= opsUpstreamErrorsMaxEvents {
			out.DroppedAttempts++
			continue
		}
		if event.Kind == cloudAdaptationKind {
			safe := cloudSafeAdaptationDetail(event.Detail)
			if safe == nil {
				out.Gaps = append(out.Gaps, "invalid_cloud_adaptation")
				continue
			}
			out.Events = append(out.Events, OpsDiagnosticEvent{At: event.AtUnixMs, Kind: cloudAdaptationKind, Status: event.UpstreamStatusCode, Detail: safe})
			continue
		}
		kind := "other"
		switch event.Kind {
		case "http_error", "request_error", "ws_error", "failover", "retry_exhausted", "retry_stopped":
			kind = event.Kind
		}
		out.Events = append(out.Events, OpsDiagnosticEvent{At: event.AtUnixMs, Kind: kind, Status: event.UpstreamStatusCode})
	}
	if len(out.Events) == 0 {
		out.Gaps = append(out.Gaps, "missing_events")
	}
	if len(out.Scheduling) == 0 {
		out.Gaps = append(out.Gaps, "missing_scheduling")
	}
	if out.DroppedAttempts > 0 || out.DroppedDecisions > 0 {
		out.Gaps = append(out.Gaps, "truncated_capture")
	}
	return out
}

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

// Export only typed, known failure facts, never provider messages or user data.
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
	for _, key := range []string{"code", "param", "reason", "upstream_error_code", "upstream_method", "upstream_error_info", "gateway_stop_reason", "upstream_turn_status"} {
		var value string
		if json.Unmarshal(facts[key], &value) != nil {
			continue
		}
		switch key {
		case "reason":
			value = cloudKnownFailureReason(value)
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
			case "cloud_history_mismatch", "cloud_continuation_settings_changed", "cloud_cached_response_invalid":
			case "cloud_gateway_stopping", "cloud_invalid_request", "cloud_execution_failed", "cloud_upstream_error", "cloud_rpc_rejected", "cloud_method_unavailable", "cloud_protocol_error", "cloud_thread_unavailable", "cloud_stream_disconnected", "cloud_turn_conflict", "cloud_authorization_failed", "cloud_event_queue_overflow", "cloud_transport_closed", "cloud_upstream_timeout", "cloud_message_too_large", "cloud_history_item_too_large", "cloud_history_rejected", "cloud_state_unavailable":
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
