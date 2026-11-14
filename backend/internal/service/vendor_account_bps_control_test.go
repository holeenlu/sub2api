package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVendorBPSRecoveryProjectionMatchesAdmin(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name, global, state, reason string
		policyErr                   error
		mutate                      func(*Account)
	}{
		{name: "pending", global: "true", state: "pending"},
		{name: "global disabled", global: "false", state: "paused", reason: "global_disabled"},
		{name: "policy unavailable", policyErr: errors.New("private database error"), state: "unknown", reason: "policy_unavailable"},
		{name: "reauthorization", global: "true", state: "reauthorization_required", reason: "grant_missing", mutate: func(a *Account) {
			delete(a.Extra, ExcelBPS403DisabledAtKey)
			a.Extra[ExcelBPSFailureKey] = map[string]any{"source": "system", "state": "disabled", "reason": "grant_missing", "reauth_required": true, "occurred_at": now.Format(time.RFC3339Nano), "grant_fingerprint": "private-grant", "policy": map[string]any{"groups": "private-policy"}}
		}},
		{name: "probing", global: "true", state: "probing", mutate: func(a *Account) {
			a.Extra[ExcelBPS403LastProbeAtKey] = now.Format(time.RFC3339Nano)
		}},
		{name: "failed", global: "true", state: "failed", mutate: func(a *Account) {
			a.Extra[ExcelBPS403LastProbeAtKey] = now.Add(-time.Minute).Format(time.RFC3339Nano)
			a.Extra[ExcelBPS403LastProbeResultKey] = map[string]any{"status": "failed", "stage": "probe", "reason": "timeout", "duration_ms": 1000, "completed_at": now.Format(time.RFC3339Nano), "raw_body": "private-body"}
		}},
		{name: "history", global: "true", state: "history", mutate: func(a *Account) { a.Extra["openai_excel_bps"] = true }},
		{name: "no recovery", global: "true", mutate: func(a *Account) {
			delete(a.Extra, ExcelBPS403DisabledAtKey)
			delete(a.Extra, ExcelBPSFailureKey)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := recoveryTestAccount(now)
			a.Extra[ExcelBPSFailureKey] = map[string]any{"source": "system", "state": "disabled", "reason": "account_forbidden", "reauth_required": false, "grant_fingerprint": "private-grant", "policy": map[string]any{"groups": "private-policy"}}
			if tc.mutate != nil {
				tc.mutate(a)
			}
			before, err := json.Marshal(a.Extra)
			require.NoError(t, err)
			settings := &SettingService{settingRepo: &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: tc.global}, err: tc.policyErr}}
			vendor := &VendorService{settings: settings}
			admin := &adminServiceImpl{settingService: settings}
			d := vendorAccountDTO(a, nil)
			vendor.populateVendorBPSRecoveryStatuses(t.Context(), []*Account{a}, []*VendorAccountDTO{d})
			require.Equal(t, admin.ExcelBPS403RecoveryStatuses(t.Context(), []*Account{a})[a.ID], d.ExcelBPS403Recovery)
			if tc.state == "" {
				require.Nil(t, d.ExcelBPS403Recovery)
			} else {
				require.Equal(t, tc.state, d.ExcelBPS403Recovery.State)
				require.Equal(t, tc.reason, d.ExcelBPS403Recovery.Reason)
				if tc.state == "reauthorization_required" || tc.state == "unknown" || tc.state == "paused" {
					require.Empty(t, d.ExcelBPS403Recovery.NextProbeAt)
				}
			}
			raw, err := json.Marshal(d)
			require.NoError(t, err)
			for _, secret := range []string{"private-grant", "private-policy", "private-body", "private database error", "grant_fingerprint", ExcelBPSFailureKey} {
				require.NotContains(t, string(raw), secret)
			}
			if tc.state != "" {
				require.Contains(t, string(raw), `"excel_bps_403_recovery"`)
			}
			after, err := json.Marshal(a.Extra)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestVendorBPSRecoveryProjectionUsesAccountIDs(t *testing.T) {
	a := recoveryTestAccount(time.Now().UTC())
	other := &Account{ID: a.ID + 1}
	dtos := []*VendorAccountDTO{vendorAccountDTO(other, nil), vendorAccountDTO(a, nil)}
	s := &VendorService{}
	s.populateVendorBPSRecoveryStatuses(t.Context(), []*Account{a, other}, dtos)
	require.Nil(t, dtos[0].ExcelBPS403Recovery)
	require.Equal(t, "global_disabled", dtos[1].ExcelBPS403Recovery.Reason)
}

func TestVendorBPSControlPostgresListDetailAndUpdate(t *testing.T) {
	s, ctx := vendorAccountPostgres(t)
	_, err := s.db.Exec(`UPDATE vendors SET openai_authorization_modes='["native","bps"]' WHERE id=1`)
	require.NoError(t, err)
	id, err := vendorAccountTestWrite(ctx, s, vendorAccountTestVendor(1, 10), "bps-control-projection", 0)
	require.NoError(t, err)
	const at = "2026-10-27T01:02:03Z"
	_, err = s.db.Exec(`UPDATE accounts SET extra=extra||jsonb_build_object('openai_excel_bps',false,'openai_excel_bps_403_disabled_at',$2::text,'openai_excel_bps_403_last_probe_at',$2::text,'openai_excel_bps_failure',jsonb_build_object('source','system','state','disabled','reason','grant_missing','reauth_required',true,'occurred_at',$2::text,'grant_fingerprint','private-grant')) WHERE id=$1`, id, at)
	require.NoError(t, err)
	detail, err := s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, detail.ExcelBPS403Recovery)
	require.Equal(t, "reauthorization_required", detail.ExcelBPS403Recovery.State)
	require.Empty(t, detail.ExcelBPS403Recovery.NextProbeAt)
	list, err := s.ListVendorAccounts(ctx, VendorAccountListInput{PageSize: 100})
	require.NoError(t, err)
	found := false
	for _, item := range list.Items {
		if item.ID == id {
			found = true
			require.Equal(t, detail.ExcelBPS403Recovery, item.ExcelBPS403Recovery)
		}
	}
	require.True(t, found, "the real list path must enrich the owned account")
	var before int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", id).Scan(&before))
	enabled, name := true, "stale form name"
	_, err = s.UpdateVendorAccount(ctx, id, VendorAccountUpdate{BPSEnabled: &enabled, Name: &name})
	require.ErrorIs(t, err, ErrExcelBPSStateConflict)
	a, err := s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, false, a.Extra["openai_excel_bps"])
	require.NotEqual(t, name, a.Name, "metadata must roll back with a stale BPS edit")
	var after int
	require.NoError(t, s.db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", id).Scan(&after))
	require.Equal(t, before, after)

	// The normal edit after fetching the system-disabled account must not
	// consume recovery merely because its current state is already false.
	_, err = s.UpdateVendorAccount(ctx, id, VendorAccountUpdate{Name: &name})
	require.NoError(t, err)
	a, err = s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, at, a.Extra[ExcelBPS403LastProbeAtKey])

	expected := detail.ExcelBPS403Recovery.DisabledAt
	updated, err := s.UpdateVendorAccount(ctx, id, VendorAccountUpdate{BPSEnabled: &enabled, BPSExpectedDisabledAt: &expected})
	require.NoError(t, err)
	require.Nil(t, updated.ExcelBPS403Recovery)
	a, err = s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, true, a.Extra["openai_excel_bps"])
	require.NotContains(t, a.Extra, ExcelBPSExpectedDisabledAtKey)
	require.NotContains(t, a.Extra, ExcelBPSFailureKey)

	_, err = s.db.Exec(`UPDATE accounts SET extra=extra||jsonb_build_object('openai_excel_bps',false,'openai_excel_bps_403_disabled_at',$2::text,'openai_excel_bps_403_last_probe_at',$2::text,'openai_excel_bps_failure',jsonb_build_object('id','new-claim')) WHERE id=$1`, id, at)
	require.NoError(t, err)
	_, err = s.db.Exec(`UPDATE accounts SET extra=extra||'{"openai_excel_bps_403_last_probe_result":{"status":"failed","usage":{"state":"lower_bound","tokens":{"input_tokens":9}}}}'::jsonb WHERE id=$1`, id)
	require.NoError(t, err)
	enabled = false
	_, err = s.UpdateVendorAccount(ctx, id, VendorAccountUpdate{BPSEnabled: &enabled})
	require.NoError(t, err)
	a, err = s.accounts.GetByID(ctx, id)
	require.NoError(t, err)
	for _, key := range []string{ExcelBPS403DisabledAtKey, ExcelBPS403LastProbeAtKey, ExcelBPSFailureKey} {
		require.NotContains(t, a.Extra, key)
	}
	detail, err = s.GetVendorAccount(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "history", detail.ExcelBPS403Recovery.State)
	require.Equal(t, "lower_bound", detail.ExcelBPS403Recovery.LastResult.Usage.State)
	require.Equal(t, int64(9), *detail.ExcelBPS403Recovery.LastResult.Usage.Tokens.Input)
}

// A retired all-models flag must never erase an explicit protocol selection.
// nil transaction proves rejection happens before any attempted SQL mutation.
func TestVendorBPSRetiredAllModelsFlagCannotEraseSelection(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(fmt.Sprint(all), func(t *testing.T) {
			models := []string{"gpt-5.1"}
			account := &Account{ID: 9, Platform: PlatformOpenAI, Extra: map[string]any{"openai_excel_bps_models": models}}
			vendor := &Vendor{Platforms: []string{PlatformOpenAI}, OpenAIAuthorizationModes: []string{VendorOpenAIModeBPS}}
			err := (&VendorService{}).updateVendorAuthorizationSettings(t.Context(), nil, vendor, account, VendorAccountUpdate{BPSAllModels: &all})
			require.Error(t, err)
			require.Contains(t, err.Error(), "bps_all_models is no longer supported")
			require.Equal(t, models, account.Extra["openai_excel_bps_models"])
		})
	}
}
