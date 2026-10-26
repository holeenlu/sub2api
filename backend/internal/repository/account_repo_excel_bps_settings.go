package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

func excelBPSExtraUpdateSQL(expr string, updates map[string]any) string {
	remove := func(keys ...string) {
		expr = "(" + expr + ")"
		for _, key := range keys {
			expr += " - '" + key + "'"
		}
	}
	// Remove the retired template marker when merging account settings.
	remove("openai_excel_bps_config_mode")
	// Server-owned markers cannot be supplied by an editor/import.
	for _, key := range []string{service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, service.ExcelBPS403LastProbeResultKey, service.ExcelBPS403MovedAtKey, service.ExcelBPS403MovedGroupIDKey} {
		if _, exists := updates[key]; exists {
			remove(key)
			expr = "(" + expr + ") || CASE WHEN extra ? '" + key + "' THEN jsonb_build_object('" + key + "', extra -> '" + key + "') ELSE '{}'::jsonb END"
		}
	}
	if enabled, exists := updates["openai_excel_bps"].(bool); exists {
		if enabled {
			remove(service.ExcelBPS403DisabledAtKey)
			remove(service.ExcelBPS403LastProbeAtKey, service.ExcelBPS403LastProbeResultKey)
		} else {
			remove("openai_excel_bps", "openai_excel_bps_models", "openai_excel_bps_cache_creation_as_input", "openai_excel_bps_auto_disable_on_403", service.ExcelBPSAutoRecoverOn403Key, service.ExcelBPSAutoMoveOn403Key, service.ExcelBPS403TargetGroupIDKey, service.ExcelBPSIgnoreEncryptedContentKey, service.ExcelBPSOmitUnsupportedToolsKey)
			return expr
		}
	}
	if value, exists := updates["openai_excel_bps_models"]; exists && value == nil {
		remove("openai_excel_bps_models")
	}
	for _, key := range []string{"openai_excel_bps_cache_creation_as_input", "openai_excel_bps_auto_disable_on_403", service.ExcelBPSAutoRecoverOn403Key} {
		if enabled, exists := updates[key].(bool); exists && !enabled {
			remove(key)
		}
	}
	if enabled, exists := updates[service.ExcelBPSAutoMoveOn403Key].(bool); exists && !enabled {
		remove(service.ExcelBPSAutoMoveOn403Key, service.ExcelBPS403TargetGroupIDKey)
	} else if value, exists := updates[service.ExcelBPS403TargetGroupIDKey]; exists && value == nil {
		remove(service.ExcelBPS403TargetGroupIDKey)
	}
	return expr
}
