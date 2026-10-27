package service

// Explicit allowlist: obsolete image-ignore, managed proxy and worker settings
// remain rejected by the shared persistence boundary.
func IsExcelBPSExtraKey(key string) bool {
	switch key {
	case ExcelBPSAuthorizationErrorKey, "openai_excel_bps", "openai_excel_bps_models", "openai_excel_bps_cache_creation_as_input",
		ExcelBPSIgnoreEncryptedContentKey, ExcelBPSOmitUnsupportedToolsKey,
		"openai_excel_bps_auto_disable_on_403", ExcelBPSAutoRecoverOn403Key,
		ExcelBPSAutoMoveOn403Key, ExcelBPS403TargetGroupIDKey, ExcelBPS403RecoveryIntervalMinutesKey,
		ExcelBPS403DisabledAtKey, ExcelBPS403LastProbeAtKey, ExcelBPS403LastProbeResultKey, ExcelBPS403MovedAtKey, ExcelBPS403MovedGroupIDKey:
		return true
	}
	return false
}
