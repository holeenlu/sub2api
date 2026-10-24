package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVendorAuthorizationPostgresLiveModesAndParentRevocation(t *testing.T) {
	_, db := vendorSupplyPostgres(t)
	_, err := db.Exec(`
UPDATE vendors SET openai_authorization_modes='["native","bps"]' WHERE id=7;
INSERT INTO users VALUES(4,'vendor_level2','active',1,NULL);
INSERT INTO vendors(id,user_id,status,policy_version,platforms,allow_direct,openai_authorization_modes,tier,parent_vendor_id)
 VALUES(8,4,'active',1,'["openai"]',TRUE,'["native","bps"]',2,7);
INSERT INTO vendor_groups VALUES(8,13);
INSERT INTO accounts(id,vendor_id,credentials,extra) VALUES(4,8,'{"access_token":"native-token","plan_type":"plus"}','{"openai_excel_bps":true}');
INSERT INTO account_groups(account_id,group_id) VALUES(4,13);
INSERT INTO openai_excel_grants(account_id,credentials,ciphertext) VALUES(4,'{}','encrypted-test-grant');
`)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, change string
		native, bps  bool
	}{
		{"both_grants", "", true, true},
		{"parent_native_only", `UPDATE vendors SET openai_authorization_modes='["native"]' WHERE id=7`, true, false},
		{"parent_bps_only", `UPDATE vendors SET openai_authorization_modes='["bps"]' WHERE id=7`, false, true},
		{"child_native_only", `UPDATE vendors SET openai_authorization_modes='["native"]' WHERE id=8`, true, false},
		{"child_bps_only", `UPDATE vendors SET openai_authorization_modes='["bps"]' WHERE id=8`, false, true},
		{"parent_mode_revoked", `UPDATE vendors SET openai_authorization_modes='[]' WHERE id=7`, false, false},
		{"parent_platform_revoked", `UPDATE vendors SET platforms='["anthropic"]' WHERE id=7`, false, false},
		{"parent_disabled", `UPDATE vendors SET status='disabled' WHERE id=7`, false, false},
		{"parent_login_disabled", `UPDATE users SET status='disabled' WHERE id=3`, false, false},
		{"parent_group_revoked", `DELETE FROM vendor_groups WHERE vendor_id=7 AND group_id=13`, false, false},
		{"native_grant_revoked", `UPDATE accounts SET extra=extra||'{"vendor_native_disabled":true}' WHERE id=4`, false, true},
		{"bps_switch_revoked", `UPDATE accounts SET extra='{}' WHERE id=4`, true, false},
		{"bps_grant_revoked", `DELETE FROM openai_excel_grants WHERE account_id=4`, true, false},
		{"free_plan_cannot_bps", `UPDATE accounts SET credentials=credentials||'{"plan_type":"free"}' WHERE id=4`, true, false},
		{"excel_token_is_not_native", `UPDATE accounts SET credentials=credentials||'{"client_id":"app_fnr0pYvVwwFDocDumLG3H2Bp"}' WHERE id=4`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(t.Context(), nil)
			require.NoError(t, err)
			defer tx.Rollback()
			if tc.change != "" {
				_, err = tx.Exec(tc.change)
				require.NoError(t, err)
			}
			repo := &accountRepository{sql: tx}
			owner, group := int64(8), int64(13)
			stale := service.Account{ID: 4, VendorID: &owner, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				VendorAuthorizationModesLoaded: true, VendorNativeAllowed: true, VendorBPSAllowed: true}
			for mode, allowed := range map[string]bool{"native": tc.native, "bps": tc.bps} {
				err := repo.ValidateVendorAuthorizationMode(t.Context(), &stale, &group, mode)
				if allowed {
					require.NoError(t, err, mode)
				} else {
					require.ErrorIs(t, err, service.ErrVendorSupplyUnavailable, mode)
				}
			}
			require.ErrorIs(t, repo.ValidateVendorAuthorizationMode(t.Context(), &stale, &group, "unknown"), service.ErrVendorSupplyUnavailable)
			rows, err := repo.FilterVendorSupply(t.Context(), []service.Account{stale}, &group)
			require.NoError(t, err)
			if tc.native || tc.bps {
				require.Len(t, rows, 1)
				require.Equal(t, tc.native, rows[0].VendorOpenAIModeAllowed("native"))
				require.Equal(t, tc.bps, rows[0].VendorOpenAIModeAllowed("bps"))
			} else {
				require.Empty(t, rows)
			}
			require.True(t, stale.VendorNativeAllowed)
			require.True(t, stale.VendorBPSAllowed)
			stale.VendorID = nil
			require.ErrorIs(t, repo.ValidateVendorAuthorizationMode(t.Context(), &stale, &group, "native"), service.ErrVendorSupplyUnavailable, "old sticky snapshots cannot bypass ownership")
		})
	}
}
