package service

import "context"

const SettingKeyExcelBPSEnabled = "excel_bps_enabled"

func (s *OpenAIGatewayService) excelBPSGloballyEnabled(ctx context.Context) bool {
	if s == nil || s.settingService == nil {
		return false
	}
	return s.settingService.IsExcelBPSEnabled(ctx)
}
func (s *SettingService) IsExcelBPSEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	value, err := s.settingRepo.GetValue(ctx, SettingKeyExcelBPSEnabled)
	return err == nil && value == "true"
}
