package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExcelBPS403RecoveryInterval(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		want  time.Duration
		valid bool
	}{
		{name: "missing", want: time.Hour, valid: true},
		{name: "minimum", value: 1, want: time.Minute, valid: true},
		{name: "maximum", value: float64(MaxExcelBPS403RecoveryIntervalMinutes), want: 7 * 24 * time.Hour, valid: true},
		{name: "zero", value: 0, want: time.Hour},
		{name: "fraction", value: 1.5, want: time.Hour},
		{name: "string", value: "60", want: time.Hour},
		{name: "too large", value: MaxExcelBPS403RecoveryIntervalMinutes + 1, want: time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := &Account{Extra: map[string]any{}}
			if test.value != nil {
				account.Extra[ExcelBPS403RecoveryIntervalMinutesKey] = test.value
			}
			require.Equal(t, test.want, account.ExcelBPS403RecoveryInterval())
			if test.valid {
				require.NoError(t, validateExcelBPS403RecoveryExtra(account.Extra))
			} else {
				require.Error(t, validateExcelBPS403RecoveryExtra(account.Extra))
			}
		})
	}
}

func TestExcelBPS403RecoveryDueUsesLastProbe(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	account := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra: map[string]any{
			ExcelBPSAutoRecoverOn403Key:            true,
			"openai_excel_bps_auto_disable_on_403": true,
			ExcelBPS403DisabledAtKey:               now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			ExcelBPS403LastProbeAtKey:              now.Add(-30 * time.Minute).Format(time.RFC3339Nano),
			ExcelBPS403RecoveryIntervalMinutesKey:  60,
		},
	}
	require.False(t, account.ExcelBPS403RecoveryDue(now))
	account.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-61 * time.Minute).Format(time.RFC3339Nano)
	require.True(t, account.ExcelBPS403RecoveryDue(now))
}
