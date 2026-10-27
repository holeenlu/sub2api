package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"time"
)

// Only bounded classifications are persisted. Error bodies, credentials and
// request contents must never become account presentation metadata.
type ExcelBPS403ProbeResult struct {
	DisabledAt        string `json:"disabled_at,omitempty"`
	AttemptedAt       string `json:"attempted_at,omitempty"`
	UpstreamRequestID string `json:"upstream_request_id,omitempty"`
	ErrorCode         string `json:"error_code,omitempty"`
	ErrorType         string `json:"error_type,omitempty"`
	Status            string `json:"status"`
	Stage             string `json:"stage"`
	Reason            string `json:"reason"`
	HTTPStatus        int    `json:"http_status,omitempty"`
	DurationMS        int64  `json:"duration_ms"`
	CompletedAt       string `json:"completed_at"`
}

type excelBPS403ProbeError struct {
	Stage, Reason                           string
	HTTPStatus                              int
	UpstreamRequestID, ErrorCode, ErrorType string
	cause                                   error
}

func (e *excelBPS403ProbeError) Error() string { return "BPS recovery probe: " + e.Reason }
func (e *excelBPS403ProbeError) Unwrap() error { return e.cause }

func excelBPS403ProbeResult(err error, started time.Time) ExcelBPS403ProbeResult {
	r := ExcelBPS403ProbeResult{Status: "restored", Stage: "restore", Reason: "success", HTTPStatus: 200,
		DurationMS: time.Since(started).Milliseconds(), CompletedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err == nil {
		return r
	}
	r.Status, r.Stage, r.Reason, r.HTTPStatus = "failed", "prepare", "prepare_failed", 0
	var classified *excelBPS403ProbeError
	if errors.As(err, &classified) {
		r.Stage, r.Reason, r.HTTPStatus = classified.Stage, classified.Reason, classified.HTTPStatus
		r.UpstreamRequestID, r.ErrorCode, r.ErrorType = classified.UpstreamRequestID, classified.ErrorCode, classified.ErrorType
	}
	if errors.Is(err, context.DeadlineExceeded) {
		r.Reason = "timeout"
	}
	if errors.Is(err, context.Canceled) {
		r.Reason = "canceled"
	}
	return r
}

// Derived, read-only presentation of the same policy used by the recovery
// worker. Historical 403 actions do not imply an active restriction or a ban.
type ExcelBPS403RecoveryStatus struct {
	State                  string                  `json:"state"`
	Reason                 string                  `json:"reason,omitempty"`
	IntervalMinutes        int                     `json:"interval_minutes"`
	DisabledAt             string                  `json:"disabled_at,omitempty"`
	LastProbeAt            string                  `json:"last_probe_at,omitempty"`
	NextProbeAt            string                  `json:"next_probe_at,omitempty"`
	LastResultCurrentCycle bool                    `json:"last_result_current_cycle"`
	LastResult             *ExcelBPS403ProbeResult `json:"last_result,omitempty"`
}

func ExcelBPS403RecoveryStatusFromAccount(a *Account, globallyEnabled bool, now time.Time) *ExcelBPS403RecoveryStatus {
	if a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth {
		return nil
	}
	disabled, _ := a.Extra[ExcelBPS403DisabledAtKey].(string)
	moved, _ := a.Extra[ExcelBPS403MovedAtKey].(string)
	resultRaw := a.Extra[ExcelBPS403LastProbeResultKey]
	if disabled == "" && moved == "" && resultRaw == nil {
		return nil
	}
	state := &ExcelBPS403RecoveryStatus{State: "history", IntervalMinutes: int(a.ExcelBPS403RecoveryInterval() / time.Minute), DisabledAt: disabled}
	state.LastProbeAt, _ = a.Extra[ExcelBPS403LastProbeAtKey].(string)
	if data, err := json.Marshal(resultRaw); err == nil && resultRaw != nil {
		var result ExcelBPS403ProbeResult
		if json.Unmarshal(data, &result) == nil {
			state.LastResult = &result
			state.LastResultCurrentCycle = state.LastProbeAt != "" && excelBPSResultInCycle(&result, disabled)
		}
	}
	if a.IsExcelBPSEnabled() || disabled == "" {
		return state
	}
	state.State = "paused"
	switch {
	case !globallyEnabled:
		state.Reason = "global_disabled"
	case a.IsOpenAICloudConfigured():
		state.Reason = "cloud_enabled"
	case a.Extra["openai_excel_bps_auto_disable_on_403"] != true:
		state.Reason = "manual_disabled"
	case a.Extra[ExcelBPSAutoRecoverOn403Key] != true:
		state.Reason = "recovery_disabled"
	case excelBPS403RecoveryModel(a) == "":
		state.Reason = "no_text_model"
	case !a.IsExcelBPSEligible():
		state.Reason = "account_ineligible"
	case !a.IsActive():
		state.Reason = "account_inactive"
	case !a.Schedulable:
		state.Reason = "scheduling_paused"
	case a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt):
		state.Reason = "account_expired"
	default:
		last, err := time.Parse(time.RFC3339Nano, disabled)
		if err != nil {
			state.Reason = "invalid_timestamp"
			return state
		}
		if raw, exists := a.Extra[ExcelBPS403LastProbeAtKey]; exists {
			if _, ok := raw.(string); !ok || state.LastProbeAt == "" {
				state.Reason = "invalid_timestamp"
				return state
			}
		}
		if state.LastProbeAt != "" {
			at, err := time.Parse(time.RFC3339Nano, state.LastProbeAt)
			if err != nil {
				state.Reason = "invalid_timestamp"
				return state
			}
			if at.After(last) {
				last = at
			}
			completed := time.Time{}
			if state.LastResult != nil && state.LastResultCurrentCycle {
				completed, _ = time.Parse(time.RFC3339Nano, state.LastResult.CompletedAt)
			}
			if completed.Before(at) {
				state.State = "unknown"
				if now.Before(at.Add(excelBPS403RecoveryTimeout)) {
					state.State = "probing"
				}
			}
		}
		if state.State == "paused" {
			state.State = "pending"
			if state.LastResult != nil && state.LastResultCurrentCycle && state.LastResult.Status == "failed" {
				state.State = "failed"
			}
		}
		state.NextProbeAt = last.Add(a.ExcelBPS403RecoveryInterval()).UTC().Format(time.RFC3339Nano)
	}
	return state
}

// Reuse the existing settings service instead of exposing the full system
// configuration to account-list readers or introducing another settings fetch.
func (s *adminServiceImpl) ExcelBPS403RecoveryStatuses(ctx context.Context, accounts []*Account) map[int64]*ExcelBPS403RecoveryStatus {
	states := make(map[int64]*ExcelBPS403RecoveryStatus)
	enabled := s.settingService != nil && s.settingService.IsExcelBPSEnabled(ctx)
	now := time.Now()
	for _, a := range accounts {
		if state := ExcelBPS403RecoveryStatusFromAccount(a, enabled, now); state != nil {
			states[a.ID] = state
		}
	}
	return states
}

// New results carry the exact cycle start. Legacy results use their completion
// time only when a current claim exists; old history must not mark a new cycle failed.
func excelBPSResultInCycle(result *ExcelBPS403ProbeResult, disabled string) bool {
	if result == nil || disabled == "" {
		return false
	}
	if result.DisabledAt != "" {
		return result.DisabledAt == disabled
	}
	start, err := time.Parse(time.RFC3339Nano, disabled)
	if err != nil {
		return false
	}
	end, err := time.Parse(time.RFC3339Nano, result.CompletedAt)
	return err == nil && !end.Before(start)
}

// Only established protocol identifiers are retained. Arbitrary messages, HTML,
// credentials and unknown identifier values never become persisted diagnostics.
func excelBPSSafeProbeCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "server_error", "invalid_request", "invalid_request_error", "permission_error", "permission_denied", "forbidden", "access_denied", "unauthorized", "authentication_error", "invalid_token", "token_expired", "account_deactivated", "organization_deactivated", "insufficient_quota", "rate_limit_exceeded", "rate_limit_error", "basispoints_model_access_changed", "model_not_allowed", "model_not_found", "cyber_policy", "content_policy_violation", "service_unavailable_error", "server_is_overloaded":
		return strings.ToLower(strings.TrimSpace(value))
	case "":
		return ""
	default:
		return "other"
	}
}
func excelBPSProbeHTTPFailure(resp *http.Response) error {
	failure := &excelBPS403ProbeError{Stage: "request", Reason: "http_rejected", HTTPStatus: resp.StatusCode}
	if id, err := uuid.Parse(strings.TrimSpace(resp.Header.Get("x-request-id"))); err == nil {
		failure.UpstreamRequestID = id.String()
	}
	if resp.Body == nil {
		return failure
	}
	const limit = 16 << 10
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		failure.cause = err
		return failure
	}
	if len(raw) > limit {
		return failure
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &body) == nil {
		failure.ErrorCode = excelBPSSafeProbeCode(body.Error.Code)
		failure.ErrorType = excelBPSSafeProbeCode(body.Error.Type)
	}
	return failure
}
func recordExcelBPSRecoveryEvent(message string, accountID int64, fields map[string]any) {
	fields["account_id"] = accountID
	level := "info"
	if message == "excel_bps.recovery_probe_failed" {
		level = "warn"
	}
	logger.WriteSinkEvent(level, "audit.excel_bps_recovery", message, fields)
}
