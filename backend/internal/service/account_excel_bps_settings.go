package service

import (
	"maps"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Explicit allowlist: obsolete image-ignore, managed proxy and worker settings
// remain rejected by the shared persistence boundary.
func IsExcelBPSExtraKey(key string) bool {
	switch key {
	case ExcelBPSExpectedDisabledAtKey, "openai_excel_bps", "openai_excel_bps_models", "openai_excel_bps_cache_creation_as_input",
		ExcelBPSIgnoreEncryptedContentKey, ExcelBPSOmitUnsupportedToolsKey,
		"openai_excel_bps_auto_disable_on_403", ExcelBPSAutoRecoverOn403Key,
		ExcelBPSAutoMoveOn403Key, ExcelBPS403TargetGroupIDKey, ExcelBPS403RecoveryIntervalMinutesKey,
		ExcelBPSFailureKey, ExcelBPS403DisabledAtKey, ExcelBPS403LastProbeAtKey, ExcelBPS403LastProbeResultKey, ExcelBPS403MovedAtKey, ExcelBPS403MovedGroupIDKey:
		return true
	}
	return false
}

// ExcelBPSExpectedDisabledAtKey is a request-only condition for explicitly
// re-enabling a system-disabled account. Copy the safe recovery DTO's disabled_at;
// never send the internal failure record, grant fingerprint or policy.
const ExcelBPSExpectedDisabledAtKey = "openai_excel_bps_expected_disabled_at"

var ErrExcelBPSStateConflict = infraerrors.Conflict("OPENAI_EXCEL_BPS_STATE_CONFLICT", "BPS state changed; reload the account before enabling it")

// ValidateExcelBPSSettingsUpdate must run against the current row while holding
// its write lock, in the same transaction as the edit and scheduler outbox.
// False is an explicit stop even after automatic shutdown; it needs no stale
// condition and must still withdraw the old recovery claim.
func ValidateExcelBPSSettingsUpdate(updates, current map[string]any) error {
	if updates["openai_excel_bps"] != true {
		return nil
	}
	expected, present := updates[ExcelBPSExpectedDisabledAtKey]
	if present {
		if _, ok := expected.(string); !ok {
			return infraerrors.BadRequest("OPENAI_EXCEL_BPS_INVALID", ExcelBPSExpectedDisabledAtKey+" must be a string")
		}
	}
	at := ""
	disabled := false
	if current["openai_excel_bps"] != true {
		if legacy, ok := current[ExcelBPS403DisabledAtKey].(string); ok && legacy != "" {
			at, disabled = legacy, true
		}
		if failure, ok := current[ExcelBPSFailureKey].(map[string]any); ok && failure["source"] == "system" && failure["state"] == "disabled" {
			disabled = true
			// Use the same timestamp selection as the safe recovery projection.
			if at == "" {
				at, _ = failure["occurred_at"].(string)
			}
		}
	}
	if disabled {
		// A corrupt/missing timestamp is not permission to overwrite shutdown.
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil || expected != at {
			return ErrExcelBPSStateConflict
		}
	} else if present && expected != "" {
		return ErrExcelBPSStateConflict
	}
	return nil
}

// MergeExcelBPSSettingsUpdate handles the full-account edit under its existing
// repository row lock. Missing switch input preserves current state; an explicit
// false clears recovery authority via the existing marker owner. The condition
// is consumed here and never becomes stored configuration.
func MergeExcelBPSSettingsUpdate(extra, current map[string]any) (map[string]any, error) {
	if err := ValidateExcelBPSSettingsUpdate(extra, current); err != nil {
		return nil, err
	}
	explicit := false
	if _, ok := extra["openai_excel_bps"].(bool); ok {
		explicit = true
	}
	out := maps.Clone(extra)
	// Scoped cooldowns are server state. A stale full-account editor must not
	// erase or shorten them; explicit reset has its own repository operation.
	currentLimits, _ := current["model_rate_limits"].(map[string]any)
	limits, _ := out["model_rate_limits"].(map[string]any)
	limits = maps.Clone(limits)
	for key := range limits {
		if strings.HasPrefix(key, ExcelBPSCooldownScopePrefix) {
			delete(limits, key)
		}
	}
	for key, value := range currentLimits {
		if strings.HasPrefix(key, ExcelBPSCooldownScopePrefix) {
			if limits == nil {
				limits = make(map[string]any)
			}
			limits[key] = value
		}
	}
	if len(limits) > 0 {
		if out == nil {
			out = make(map[string]any)
		}
		out["model_rate_limits"] = limits
	} else {
		delete(out, "model_rate_limits")
	}
	delete(out, ExcelBPSExpectedDisabledAtKey)
	out = MergeExcelBPS403Marker(out, current)
	if !explicit {
		if enabled, exists := current["openai_excel_bps"]; exists {
			if out == nil {
				out = make(map[string]any)
			}
			out["openai_excel_bps"] = enabled
		} else {
			delete(out, "openai_excel_bps")
		}
	}
	return out, nil
}
