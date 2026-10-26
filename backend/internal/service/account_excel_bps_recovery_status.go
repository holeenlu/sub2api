package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Only bounded classifications are persisted. Error bodies, credentials and
// request contents must never become account presentation metadata.
type ExcelBPS403ProbeResult struct {
	Status      string `json:"status"`
	Stage       string `json:"stage"`
	Reason      string `json:"reason"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	DurationMS  int64  `json:"duration_ms"`
	CompletedAt string `json:"completed_at"`
}

type excelBPS403ProbeError struct {
	Stage, Reason string
	HTTPStatus    int
	cause         error
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
	State           string                  `json:"state"`
	Reason          string                  `json:"reason,omitempty"`
	IntervalMinutes int                     `json:"interval_minutes"`
	DisabledAt      string                  `json:"disabled_at,omitempty"`
	LastProbeAt     string                  `json:"last_probe_at,omitempty"`
	NextProbeAt     string                  `json:"next_probe_at,omitempty"`
	LastResult      *ExcelBPS403ProbeResult `json:"last_result,omitempty"`
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
			if state.LastResult != nil {
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
			if state.LastResult != nil && state.LastResult.Status == "failed" {
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
