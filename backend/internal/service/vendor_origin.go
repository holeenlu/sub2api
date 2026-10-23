package service

import "context"

func (s *SettingService) VendorPortalBoundary(ctx context.Context) (string, bool, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{"vendor_portal_origin", "vendor_console_enabled"})
	if err != nil {
		return "", false, err
	}
	return values["vendor_portal_origin"], values["vendor_console_enabled"] == "true", nil
}
