package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSReauthorizationStatusHasNoProbeDeadline(t *testing.T) {
	a := excelAccount()
	a.Extra["openai_excel_bps"] = false
	a.Extra[ExcelBPSFailureKey] = map[string]any{
		"source": "system", "state": "disabled", "reason": "grant_missing",
		"reauth_required": true, "occurred_at": time.Now().UTC().Format(time.RFC3339Nano),
		"next_probe_at": nil,
	}
	status := ExcelBPS403RecoveryStatusFromAccount(a, true, time.Now())
	require.NotNil(t, status)
	require.Equal(t, "reauthorization_required", status.State)
	require.Equal(t, "grant_missing", status.Reason)
	require.Empty(t, status.NextProbeAt)
}

func TestExcelBPSRecoveryPreviousUsageDoesNotDescribeNewClaim(t *testing.T) {
	now := time.Now().UTC()
	a := recoveryTestAccount(now)
	a.Extra[ExcelBPS403LastProbeResultKey] = map[string]any{
		"status": "failed", "stage": "response", "reason": "upstream_rejected",
		"completed_at": now.Add(-24 * time.Hour).Format(time.RFC3339Nano),
		"usage":        map[string]any{"state": "lower_bound", "claim_at": now.Add(-24 * time.Hour).Format(time.RFC3339Nano)},
	}
	state := ExcelBPS403RecoveryStatusFromAccount(a, true, now)
	require.Equal(t, "pending", state.State, "historical usage does not mean a new probe failed")
	require.NotNil(t, state.LastResult.Usage)
	require.Equal(t, "lower_bound", state.LastResult.Usage.State)
}

func TestExcelBPSLegacyAuthorizationFailureRemainsReadOnlyAndUnprobeable(t *testing.T) {
	now := time.Now().UTC()
	a := recoveryTestAccount(now)
	a.Extra["openai_excel_bps"] = true
	a.Extra[ExcelBPSAuthorizationErrorKey] = "account_deactivated"
	require.False(t, a.IsExcelBPSEnabled())
	require.False(t, a.ExcelBPS403RecoveryDue(now.Add(24*time.Hour)))
	view := ExcelBPS403RecoveryStatusFromAccount(a, true, now)
	require.Equal(t, "reauthorization_required", view.State)
	require.Empty(t, view.NextProbeAt)
	merged := MergeExcelBPS403Marker(map[string]any{ExcelBPSAuthorizationErrorKey: "forged", "openai_excel_bps": true}, a.Extra)
	require.Equal(t, "account_deactivated", merged[ExcelBPSAuthorizationErrorKey])
}
