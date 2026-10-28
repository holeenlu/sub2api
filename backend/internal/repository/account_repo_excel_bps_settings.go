package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func excelBPSExtraUpdateSQL(expr string, updates map[string]any) string {
	remove := func(keys ...string) {
		expr = "(" + expr + ")"
		for _, key := range keys {
			expr += " - '" + key + "'"
		}
	}
	remove("openai_excel_bps_config_mode", service.ExcelBPSExpectedDisabledAtKey)
	// Server-owned markers cannot be supplied by an editor/import.
	for _, key := range []string{service.ExcelBPSAuthorizationErrorKey, service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, service.ExcelBPS403LastProbeResultKey, service.ExcelBPSFailureKey, service.ExcelBPS403MovedAtKey, service.ExcelBPS403MovedGroupIDKey} {
		if _, exists := updates[key]; exists {
			remove(key)
			expr = "(" + expr + ") || CASE WHEN extra ? '" + key + "' THEN jsonb_build_object('" + key + "', extra -> '" + key + "') ELSE '{}'::jsonb END"
		}
	}
	if enabled, exists := updates["openai_excel_bps"].(bool); exists {
		// Withdraw recovery permission without deleting an in-flight probe's
		// durable identity or its already observed consumption.
		remove(service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, service.ExcelBPSFailureKey)
		if !enabled {
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

// lockExcelBPSSettingsUpdate serializes an enabling editor with automatic
// shutdown. Call only inside the same transaction as UpdateExtra/BulkUpdate.
// The scope predicate is identical to the subsequent write; stable lock order
// avoids cross-account deadlocks between overlapping bulk edits.
func lockExcelBPSSettingsUpdate(ctx context.Context, exec sqlExecutor, ids []int64, updates map[string]any) error {
	if updates["openai_excel_bps"] != true {
		return nil
	}
	rows, err := exec.QueryContext(ctx, "SELECT COALESCE(extra, '{}'::jsonb) FROM accounts WHERE id = ANY($1) AND deleted_at IS NULL AND "+accountNotArchivedSQL+" AND "+accountVisibilitySQL(ctx, "vendor_id")+" ORDER BY id FOR NO KEY UPDATE", pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		var current map[string]any
		if err := json.Unmarshal(raw, &current); err != nil {
			return err
		}
		if err := service.ValidateExcelBPSSettingsUpdate(updates, current); err != nil {
			return err
		}
	}
	return rows.Err()
}
