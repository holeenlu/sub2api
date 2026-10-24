package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func vendorRefreshPostgres(t *testing.T) (*accountRepository, *sql.DB, *service.Account) {
	t.Helper()
	repo, db := vendorSupplyPostgres(t)
	_, err := db.Exec(`UPDATE vendors SET openai_authorization_modes='["native","bps"]';
UPDATE accounts SET credentials='{"access_token":"native-at","refresh_token":"native-rt","plan_type":"plus"}',extra='{"openai_excel_bps":true,"vendor_authorization_version":"v1"}' WHERE id=1;
UPDATE openai_excel_grants SET ciphertext='preserved-private-grant' WHERE account_id=1;`)
	require.NoError(t, err)
	owner := int64(7)
	a := &service.Account{ID: 1, VendorID: &owner, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Credentials: map[string]any{"access_token": "native-at", "refresh_token": "native-rt", "plan_type": "plus"}, Extra: map[string]any{"openai_excel_bps": true, "vendor_authorization_version": "v1"}}
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
		verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) {
			return new(service.VerifiedSupplyIdentity), nil
		},
		claim: func(context.Context, service.VendorProxySQL, *service.Account, *service.VerifiedSupplyIdentity) error {
			return nil
		},
	})
	return repo, db, a
}

func TestVendorOAuthRefreshPostgresPrimaryModeAdmission(t *testing.T) {
	repo, db, a := vendorRefreshPostgres(t)
	for _, tc := range []struct {
		name, policy, client string
		allowed              bool
	}{
		{"native_both", "[\"native\",\"bps\"]", "", true},
		{"native_bps_only", "[\"bps\"]", "", false},
		{"native_native_only", "[\"native\"]", "", true},
		{"bps_native_only", "[\"native\"]", "app_fnr0pYvVwwFDocDumLG3H2Bp", false},
		{"bps_bps_only", "[\"bps\"]", "app_fnr0pYvVwwFDocDumLG3H2Bp", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec("UPDATE vendors SET openai_authorization_modes=$1::jsonb", tc.policy)
			require.NoError(t, err)
			_, err = db.Exec("UPDATE accounts SET credentials=credentials||jsonb_build_object('client_id',$1::text) WHERE id=1", tc.client)
			require.NoError(t, err)
			a.Credentials["client_id"] = tc.client
			err = repo.ValidateVendorOAuthRefresh(t.Context(), a)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrVendorSupplyUnavailable)
				page, err := repo.ListOAuthRefreshCandidatePage(t.Context(), service.OAuthRefreshPageOptions{Platforms: []string{service.PlatformOpenAI}, Limit: 20, ActiveOnly: true, RequireRefreshToken: true})
				require.NoError(t, err)
				require.Empty(t, page.Accounts)
			}
			var admitted bool
			require.NoError(t, db.QueryRow("SELECT "+vendorOAuthRefreshModeSQL("a")+" FROM accounts a WHERE id=1").Scan(&admitted))
			require.Equal(t, tc.allowed, admitted)
		})
	}
	// Refresh must keep the original non-supplier and paused-account semantics.
	var admitted bool
	require.NoError(t, db.QueryRow("SELECT "+vendorOAuthRefreshModeSQL("a")+" FROM accounts a WHERE id=2").Scan(&admitted))
	require.True(t, admitted)
	_, err := db.Exec("UPDATE accounts SET schedulable=FALSE WHERE id=1")
	require.NoError(t, err)
	require.NoError(t, repo.ValidateVendorOAuthRefresh(t.Context(), a))
}

func TestVendorOAuthRefreshPostgresConditionalPersistenceAndModeDisable(t *testing.T) {
	for _, mode := range []string{"native", "bps"} {
		t.Run(mode, func(t *testing.T) {
			repo, db, a := vendorRefreshPostgres(t)
			if mode == "bps" {
				a.Credentials["client_id"] = "app_fnr0pYvVwwFDocDumLG3H2Bp"
				_, err := db.Exec(`UPDATE accounts SET credentials=credentials||'{"client_id":"app_fnr0pYvVwwFDocDumLG3H2Bp"}' WHERE id=1`)
				require.NoError(t, err)
			}
			next := map[string]any{}
			for k, v := range a.Credentials {
				next[k] = v
			}
			next["access_token"] = "rotated-at"
			changed, err := repo.UpdateVendorOAuthCredentialsIfUnchanged(t.Context(), a, next)
			require.NoError(t, err)
			require.True(t, changed)
			a.Credentials = next
			changed, err = repo.DisableVendorOAuthAuthorizationIfUnchanged(t.Context(), a)
			require.NoError(t, err)
			require.True(t, changed)
			var status string
			var raw []byte
			require.NoError(t, db.QueryRow("SELECT status,extra FROM accounts WHERE id=1").Scan(&status, &raw))
			var extra map[string]any
			require.NoError(t, json.Unmarshal(raw, &extra))
			require.Equal(t, "active", status)
			require.Equal(t, mode == "native", extra["vendor_native_disabled"] == true)
			require.Equal(t, mode == "native", extra["openai_excel_bps"])
			var events, grants, history int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=1").Scan(&events))
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM openai_excel_grants WHERE account_id=1 AND ciphertext='preserved-private-grant'").Scan(&grants))
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM usage_logs WHERE account_id=1").Scan(&history))
			require.Equal(t, 2, events)
			require.Equal(t, 1, grants)
			require.Equal(t, 1, history)
			require.ErrorIs(t, repo.ValidateVendorOAuthRefresh(t.Context(), a), service.ErrVendorSupplyUnavailable)
			if mode == "native" {
				require.NoError(t, repo.ValidateVendorAuthorizationMode(t.Context(), a, nil, "bps"))
			}
		})
	}
}

func TestVendorOAuthRefreshPostgresRejectsStaleResults(t *testing.T) {
	for _, change := range []struct{ name, sql string }{
		{"policy_revocation", `UPDATE vendors SET openai_authorization_modes='["bps"]' WHERE id=7`},
		{"credential_reauthorization", `UPDATE accounts SET credentials=credentials||'{"access_token":"reauthorized-at"}' WHERE id=1`},
		{"mode_edit", `UPDATE accounts SET extra=extra||'{"vendor_authorization_version":"v2"}' WHERE id=1`},
		{"proxy_reconfiguration", `UPDATE proxies SET config_version=2 WHERE id=10`},
		{"parent_revocation", `UPDATE vendors SET openai_authorization_modes='["bps"]' WHERE id=8`},
	} {
		t.Run(change.name, func(t *testing.T) {
			repo, db, a := vendorRefreshPostgres(t)
			proxy := int64(10)
			a.ProxyID = &proxy
			a.Proxy = &service.Proxy{ID: 10, VendorID: a.VendorID, ConfigVersion: 1}
			_, err := db.Exec(`UPDATE accounts SET proxy_id=10 WHERE id=1;
INSERT INTO users VALUES(4,'vendor_level1','active',1,NULL);
INSERT INTO vendors(id,user_id,status,policy_version,platforms,allow_direct,openai_authorization_modes,tier) VALUES(8,4,'active',1,'["openai"]',TRUE,'["native","bps"]',1);
INSERT INTO vendor_groups VALUES(8,13);
UPDATE vendors SET tier=2,parent_vendor_id=8 WHERE id=7;
UPDATE users SET role='vendor_level2' WHERE id=3;`)
			require.NoError(t, err)
			require.NoError(t, repo.ValidateVendorOAuthRefresh(t.Context(), a))
			// Revoke/change after admission, before either success or error writes.
			_, err = db.Exec(change.sql)
			require.NoError(t, err)
			for _, save := range []bool{true, false} {
				var changed bool
				if save {
					changed, err = repo.UpdateVendorOAuthCredentialsIfUnchanged(t.Context(), a, map[string]any{"access_token": "late-at", "refresh_token": "old-rt", "plan_type": "plus"})
				} else {
					changed, err = repo.DisableVendorOAuthAuthorizationIfUnchanged(t.Context(), a)
				}
				require.NoError(t, err)
				require.False(t, changed)
			}
			var events int
			require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM scheduler_outbox").Scan(&events))
			require.Zero(t, events)
		})
	}
}

func TestVendorOAuthRefreshPostgresOutboxFailureRollsBackDisable(t *testing.T) {
	repo, db, a := vendorRefreshPostgres(t)
	_, err := db.Exec("ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_events CHECK(FALSE)")
	require.NoError(t, err)
	changed, err := repo.DisableVendorOAuthAuthorizationIfUnchanged(t.Context(), a)
	require.Error(t, err)
	require.False(t, changed)
	require.NoError(t, repo.ValidateVendorOAuthRefresh(t.Context(), a))
}

func TestVendorOAuthRefreshPostgresRevokedDuringIdentityVerification(t *testing.T) {
	repo, db, a := vendorRefreshPostgres(t)
	verified := false
	repo.SetSupplyIdentityGuard(nativeIdentityGuardStub{
		verify: func(context.Context, *service.Account) (*service.VerifiedSupplyIdentity, error) {
			verified = true
			_, err := db.Exec(`UPDATE vendors SET openai_authorization_modes='["bps"]' WHERE id=7`)
			require.NoError(t, err)
			return new(service.VerifiedSupplyIdentity), nil
		},
		claim: func(context.Context, service.VendorProxySQL, *service.Account, *service.VerifiedSupplyIdentity) error {
			t.Fatal("revoked refresh must not claim or commit")
			return nil
		},
	})
	changed, err := repo.UpdateVendorOAuthCredentialsIfUnchanged(t.Context(), a, map[string]any{"access_token": "late-at", "refresh_token": "rotated-rt", "plan_type": "plus"})
	require.NoError(t, err)
	require.True(t, verified)
	require.False(t, changed)
	var access string
	require.NoError(t, db.QueryRow("SELECT credentials->>'access_token' FROM accounts WHERE id=1").Scan(&access))
	require.Equal(t, "native-at", access)
}
