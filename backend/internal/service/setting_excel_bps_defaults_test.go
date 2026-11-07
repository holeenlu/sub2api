package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSDefaultsPersistWithoutChangingOtherSettings(t *testing.T) {
	repo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSImageBodyLimitMiB: "64", "site_name": "existing"}}
	settings := NewSettingService(repo, nil)
	initial, err := settings.GetExcelBPSDefaults(t.Context())
	require.NoError(t, err)
	require.Equal(t, DefaultExcelBPSDefaults(), initial)
	require.Len(t, repo.values, 2, "reading defaults does not save or enable anything")
	initial.Models = []string{" custom-model ", "custom-model"}
	initial.AutoMoveOn403, initial.TargetGroupID = true, 0
	saved, err := settings.SaveExcelBPSDefaults(t.Context(), initial)
	require.NoError(t, err)
	require.Equal(t, []string{"custom-model"}, saved.Models)
	loaded, err := settings.GetExcelBPSDefaults(t.Context())
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
	require.Len(t, repo.values, 3)
	require.Equal(t, "64", repo.values[SettingKeyExcelBPSImageBodyLimitMiB])
	require.Equal(t, "existing", repo.values["site_name"])
	require.NotContains(t, repo.values[SettingKeyExcelBPSDefaults], "mihomo")
	require.NotContains(t, repo.values[SettingKeyExcelBPSDefaults], "openai_excel_bps")
}

func TestExcelBPSDefaultsRejectInvalidSaveAtomically(t *testing.T) {
	repo := &excelBPSImageSettingsRepo{}
	settings := NewSettingService(repo, nil)
	_, err := settings.SaveExcelBPSDefaults(t.Context(), DefaultExcelBPSDefaults())
	require.NoError(t, err)
	before := repo.values[SettingKeyExcelBPSDefaults]
	for _, mutate := range []func(*ExcelBPSDefaults){
		func(b *ExcelBPSDefaults) { b.Models = nil },
		func(b *ExcelBPSDefaults) { b.Models = []string{" "} },
		func(b *ExcelBPSDefaults) { b.Models = []string{strings.Repeat("a", 201)} },
		func(b *ExcelBPSDefaults) { b.Models = make([]string, 101) },
		func(b *ExcelBPSDefaults) { b.RecoveryIntervalMinutes = 0 },
		func(b *ExcelBPSDefaults) { b.RecoveryIntervalMinutes = 10081 },
		func(b *ExcelBPSDefaults) { b.AutoDisableOn403 = false; b.AutoRecoverOn403 = true },
		func(b *ExcelBPSDefaults) { b.AutoMoveOn403 = true; b.TargetGroupID = -1 },
	} {
		input := DefaultExcelBPSDefaults()
		mutate(&input)
		_, err := settings.SaveExcelBPSDefaults(t.Context(), input)
		require.Error(t, err)
		require.Equal(t, before, repo.values[SettingKeyExcelBPSDefaults])
	}
	repo.err = errors.New("settings unavailable")
	_, err = settings.GetExcelBPSDefaults(t.Context())
	require.Error(t, err, "do not silently replace stored settings with defaults")
	repo.err = nil
	repo.values[SettingKeyExcelBPSDefaults] = "broken"
	_, err = settings.GetExcelBPSDefaults(t.Context())
	require.Error(t, err)
}
