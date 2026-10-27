package service

// ExcelBPS403DisabledAtKey records when an upstream HTTP 403 automatically
// turned Excel BPS off (RFC 3339, UTC). Only DisableExcelBPSOn403 writes it; the
// admin account list shows it as a suspected Excel ban.
const ExcelBPS403DisabledAtKey = "openai_excel_bps_403_disabled_at"

const ExcelBPSAutoRecoverOn403Key = "openai_excel_bps_auto_recover_on_403"

// Server-owned attempt time also acts as a cross-instance claim token.
const ExcelBPS403LastProbeAtKey = "openai_excel_bps_403_last_probe_at"

// Sanitized server-owned outcome, kept compatible with the maintained branch.
const ExcelBPS403LastProbeResultKey = "openai_excel_bps_403_last_probe_result"

// Server-owned failure identity, reason, grant binding and recovery policy.
const ExcelBPSFailureKey = "openai_excel_bps_failure"

// ExcelBPS403MovedAtKey and ExcelBPS403MovedGroupIDKey record the last automatic
// group action after an upstream HTTP 403 (group 0 means all groups were left).
// Only MoveExcelBPSOn403 writes them; the account list flags the suspected ban
// while the account's groups still match that action.
const (
	ExcelBPS403MovedAtKey      = "openai_excel_bps_403_moved_at"
	ExcelBPS403MovedGroupIDKey = "openai_excel_bps_403_moved_group_id"
)

var excelBPS403MarkerKeys = []string{ExcelBPSAuthorizationErrorKey, ExcelBPS403DisabledAtKey, ExcelBPS403LastProbeAtKey, ExcelBPS403LastProbeResultKey, ExcelBPSFailureKey, ExcelBPS403MovedAtKey, ExcelBPS403MovedGroupIDKey}

// MergeExcelBPS403Marker keeps the persisted 403 records across account edits
// and ignores values supplied by the edit. An explicit boolean switch write,
// including false -> false, withdraws the old recovery authorization. Probe
// consumption and group-action history remain; neither authorizes recovery.
// The repository applies this under the row lock.
func MergeExcelBPS403Marker(extra, current map[string]any) map[string]any {
	delete(extra, "openai_excel_bps_config_mode")
	for _, key := range excelBPS403MarkerKeys {
		delete(extra, key)
	}
	_, explicit := extra["openai_excel_bps"].(bool)
	for _, key := range excelBPS403MarkerKeys {
		value, ok := current[key]
		if !ok || (explicit && (key == ExcelBPS403DisabledAtKey || key == ExcelBPS403LastProbeAtKey || key == ExcelBPSFailureKey)) {
			continue
		}
		if extra == nil {
			extra = make(map[string]any, len(excelBPS403MarkerKeys))
		}
		extra[key] = value
	}
	return extra
}
