package service

import (
	"maps"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSSettingsRejectStaleEnable(t *testing.T) {
	const at = "2026-10-27T01:02:03.000004Z"
	for _, current := range []map[string]any{
		{"openai_excel_bps": false, ExcelBPS403DisabledAtKey: at},
		{"openai_excel_bps": false, ExcelBPSFailureKey: map[string]any{"source": "system", "state": "disabled", "reauth_required": true, "occurred_at": at}},
	} {
		for _, expected := range []any{nil, "", "2026-10-27T01:02:02Z"} {
			updates := map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra"}}
			if expected != nil {
				updates[ExcelBPSExpectedDisabledAtKey] = expected
			}
			_, err := MergeExcelBPSSettingsUpdate(updates, current)
			require.ErrorIs(t, err, ErrExcelBPSStateConflict)
			require.Equal(t, false, current["openai_excel_bps"])
		}
		updates := map[string]any{"openai_excel_bps": true, ExcelBPSExpectedDisabledAtKey: at}
		got, err := MergeExcelBPSSettingsUpdate(updates, current)
		require.NoError(t, err)
		require.Equal(t, map[string]any{"openai_excel_bps": true}, got)
		require.Contains(t, updates, ExcelBPSExpectedDisabledAtKey, "do not mutate caller input")
	}
}

func TestExcelBPSSettingsExplicitStopAndOmittedSwitch(t *testing.T) {
	const at = "2026-10-27T01:02:03Z"
	current := map[string]any{"openai_excel_bps": false, ExcelBPS403DisabledAtKey: at, ExcelBPS403LastProbeAtKey: at,
		ExcelBPSFailureKey: map[string]any{"id": "original-claim"}, ExcelBPS403MovedAtKey: at, ExcelBPS403MovedGroupIDKey: 7}
	for _, updates := range []map[string]any{nil, {"openai_compact_supported": true}, {ExcelBPS403DisabledAtKey: "forged"}} {
		got, err := MergeExcelBPSSettingsUpdate(updates, current)
		require.NoError(t, err)
		require.Equal(t, false, got["openai_excel_bps"])
		for _, key := range excelBPS403MarkerKeys {
			require.Equal(t, current[key], got[key])
		}
	}
	got, err := MergeExcelBPSSettingsUpdate(map[string]any{"openai_excel_bps": false, ExcelBPSExpectedDisabledAtKey: "stale"}, current)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"openai_excel_bps": false, ExcelBPS403MovedAtKey: at, ExcelBPS403MovedGroupIDKey: 7}, got)
	require.Contains(t, current, ExcelBPS403LastProbeAtKey, "merge must not mutate the authoritative input")
}

func TestExcelBPSSettingsConditionCannotAuthorizeAnotherState(t *testing.T) {
	for _, current := range []map[string]any{nil, {"openai_excel_bps": true}, {"openai_excel_bps": false}} {
		require.NoError(t, ValidateExcelBPSSettingsUpdate(map[string]any{"openai_excel_bps": true}, current), "ordinary first enable remains compatible")
		require.ErrorIs(t, ValidateExcelBPSSettingsUpdate(map[string]any{"openai_excel_bps": true, ExcelBPSExpectedDisabledAtKey: "2026-10-27T01:02:03Z"}, current), ErrExcelBPSStateConflict)
	}
	for _, at := range []any{nil, 123, "bad"} {
		current := map[string]any{"openai_excel_bps": false, ExcelBPSFailureKey: map[string]any{"source": "system", "state": "disabled", "occurred_at": at}}
		require.ErrorIs(t, ValidateExcelBPSSettingsUpdate(map[string]any{"openai_excel_bps": true}, current), ErrExcelBPSStateConflict)
	}
	updates := map[string]any{"openai_excel_bps": true, ExcelBPSExpectedDisabledAtKey: 1}
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, ValidateExcelBPSSettingsUpdate(updates, nil), &appErr)
	require.Equal(t, "OPENAI_EXCEL_BPS_INVALID", appErr.Reason)
	current := map[string]any{"openai_excel_bps": true}
	before := maps.Clone(current)
	_, err := MergeExcelBPSSettingsUpdate(nil, current)
	require.NoError(t, err)
	require.Equal(t, before, current)
}
