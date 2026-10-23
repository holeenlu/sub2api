package service

import (
	"context"
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OpsRequestDiagnostics 从已有观测投影可分享的数值证据，不导出正文、凭据、
// 用户资料、账号名称或上游地址，也不把未标明丢失的记录宣称为完整执行历史。
type OpsRequestDiagnostics struct {
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
	At     int64  `json:"at_unix_ms"`
	Kind   string `json:"kind"`
	Status int    `json:"upstream_status"`
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
	if detail.RequestStatusCode != nil {
		out.Status = *detail.RequestStatusCode
	}
	out.Timings = map[string]*int64{"auth": detail.AuthLatencyMs, "routing": detail.RoutingLatencyMs, "upstream": detail.UpstreamLatencyMs, "response": detail.ResponseLatencyMs, "legacy_first_token": detail.TimeToFirstTokenMs}
	events, err := ParseOpsUpstreamErrors(detail.UpstreamErrors)
	if err != nil {
		out.Gaps = append(out.Gaps, "invalid_events")
		return out
	}
	if len(events) == 0 {
		out.Gaps = append(out.Gaps, "missing_events")
	}
	for _, event := range events {
		if event == nil {
			continue
		}
		out.DroppedAttempts += max(0, event.DroppedEarlierAttempts)
		if event.Kind == opsSchedulingTrace {
			var trace struct {
				Dropped int                  `json:"dropped_events"`
				Events  []SchedulingDecision `json:"events"`
			}
			if len(event.Detail) > maxSchedulingTraceBytes || json.Unmarshal([]byte(event.Detail), &trace) != nil {
				out.Gaps = append(out.Gaps, "invalid_scheduling")
				continue
			}
			out.DroppedDecisions += max(0, trace.Dropped)
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
		kind := "other"
		switch event.Kind {
		case "http_error", "request_error", "ws_error", "failover", "retry_exhausted", "retry_stopped":
			kind = event.Kind
		}
		out.Events = append(out.Events, OpsDiagnosticEvent{At: event.AtUnixMs, Kind: kind, Status: event.UpstreamStatusCode})
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
