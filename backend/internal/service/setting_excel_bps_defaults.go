package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ExcelBPSDefaults is a reusable form template. It never enables accounts by itself.
type ExcelBPSDefaults struct {
	AllModels               bool     `json:"all_models"`
	Models                  []string `json:"models"`
	OmitUnsupportedTools    bool     `json:"omit_unsupported_tools"`
	IgnoreImages            bool     `json:"ignore_images"`
	IgnoreEncryptedContent  bool     `json:"ignore_encrypted_content"`
	AutoDisableOn403        bool     `json:"auto_disable_on_403"`
	AutoRecoverOn403        bool     `json:"auto_recover_on_403"`
	RecoveryIntervalMinutes int      `json:"recovery_interval_minutes"`
	AutoMoveOn403           bool     `json:"auto_move_on_403"`
	TargetGroupID           int64    `json:"target_group_id"`
	CacheCreationAsInput    bool     `json:"cache_creation_as_input"`
}

func DefaultExcelBPSDefaults() ExcelBPSDefaults {
	return ExcelBPSDefaults{
		TargetGroupID:          -1,
		Models:                 []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra"},
		IgnoreEncryptedContent: true, AutoDisableOn403: true, CacheCreationAsInput: true,
		RecoveryIntervalMinutes: DefaultExcelBPS403RecoveryIntervalMinutes,
	}
}

func validateExcelBPSDefaults(b ExcelBPSDefaults) error {
	bad := func(message string) error { return infraerrors.BadRequest("EXCEL_BPS_DEFAULTS_INVALID", message) }
	if len(b.Models) > 100 {
		return bad("at most 100 BPS models allowed")
	}
	for _, model := range b.Models {
		if strings.TrimSpace(model) == "" || len(model) > 200 {
			return bad("invalid BPS model name")
		}
	}
	if !b.AllModels && len(b.Models) == 0 {
		return bad("select at least one BPS model")
	}
	if b.RecoveryIntervalMinutes < 1 || b.RecoveryIntervalMinutes > MaxExcelBPS403RecoveryIntervalMinutes {
		return bad("BPS recovery interval must be between 1 and 10080 minutes")
	}
	if b.AutoRecoverOn403 && !b.AutoDisableOn403 {
		return bad("BPS recovery requires automatic disabling on 403")
	}
	if b.AutoMoveOn403 && b.TargetGroupID < 0 {
		return bad("select a BPS 403 target group")
	}
	return nil
}

const SettingKeyExcelBPSDefaults = "excel_bps_defaults"

// Read only for an explicit administrator form action. This template has no
// gateway, account creation, import or scheduling side effects.
func (s *SettingService) GetExcelBPSDefaults(ctx context.Context) (ExcelBPSDefaults, error) {
	result := DefaultExcelBPSDefaults()
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyExcelBPSDefaults})
	if err != nil {
		return result, err
	}
	if raw := strings.TrimSpace(values[SettingKeyExcelBPSDefaults]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return result, fmt.Errorf("invalid stored BPS template: %w", err)
		}
	}
	if err := validateExcelBPSDefaults(result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *SettingService) SaveExcelBPSDefaults(ctx context.Context, input ExcelBPSDefaults) (ExcelBPSDefaults, error) {
	if err := validateExcelBPSDefaults(input); err != nil {
		return input, err
	}
	models := make([]string, 0, len(input.Models))
	seen := make(map[string]bool)
	for _, value := range input.Models {
		value = strings.TrimSpace(value)
		if !seen[value] {
			models = append(models, value)
			seen[value] = true
		}
	}
	input.Models = models
	raw, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	err = s.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyExcelBPSDefaults: string(raw)})
	return input, err
}
