package service

import "context"

type conditionalSettingTestRepo struct{ SettingRepository }

func (r conditionalSettingTestRepo) CompareAndSetMultiple(ctx context.Context, revision string, values map[string]string) (string, error) {
	current, err := r.GetAll(ctx)
	if err != nil {
		return "", err
	}
	if SettingsRevision(current) != revision {
		return "", ErrSettingsRevisionConflict
	}
	if err := r.SetMultiple(ctx, values); err != nil {
		return "", err
	}
	current, err = r.GetAll(ctx)
	return SettingsRevision(current), err
}

func updateSystemSettingsForTest(s *SettingService, ctx context.Context, settings *SystemSettings, authDefaults ...*AuthSourceDefaultSettings) error {
	current, err := s.settingRepo.GetAll(ctx)
	if err != nil {
		return err
	}
	settings.Revision = SettingsRevision(current)
	original := s.settingRepo
	s.settingRepo = conditionalSettingTestRepo{original}
	defer func() { s.settingRepo = original }()
	var defaults *AuthSourceDefaultSettings
	if len(authDefaults) > 0 {
		defaults = authDefaults[0]
	}
	return s.UpdateSettings(ctx, settings, defaults, nil, nil)
}
