package service

import (
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	ExcelBPS403RecoveryIntervalMinutesKey     = "openai_excel_bps_403_recovery_interval_minutes"
	DefaultExcelBPS403RecoveryIntervalMinutes = 60
	MaxExcelBPS403RecoveryIntervalMinutes     = 10080
)

func excelBPS403RecoveryMinutes(value any) (int, bool) {
	minutes, ok := excelBPS403GroupID(value)
	return int(minutes), ok && minutes >= 1 && minutes <= MaxExcelBPS403RecoveryIntervalMinutes
}

func validateExcelBPS403RecoveryExtra(extra map[string]any) error {
	if raw, exists := extra[ExcelBPS403RecoveryIntervalMinutesKey]; exists {
		if _, ok := excelBPS403RecoveryMinutes(raw); !ok {
			return infraerrors.BadRequest("OPENAI_EXCEL_BPS_INVALID", ExcelBPS403RecoveryIntervalMinutesKey+" must be an integer between 1 and 10080")
		}
	}
	return nil
}

// Missing or invalid legacy values keep the original one-hour behavior.
func (a *Account) ExcelBPS403RecoveryInterval() time.Duration {
	if a != nil {
		if minutes, ok := excelBPS403RecoveryMinutes(a.Extra[ExcelBPS403RecoveryIntervalMinutesKey]); ok {
			return time.Duration(minutes) * time.Minute
		}
	}
	return DefaultExcelBPS403RecoveryIntervalMinutes * time.Minute
}
