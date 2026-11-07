//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSRecoveryPrimaryRefreshPreservesOnlyOriginalClaim(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	_, err := integrationDB.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials||jsonb_build_object('client_id',$2::text) WHERE id=$1`, a.ID, openai.ExcelClientID)
	require.NoError(t, err)
	a, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	before, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	credentials := make(map[string]any, len(before.Credentials))
	for key, value := range before.Credentials {
		credentials[key] = value
	}
	credentials["access_token"] = "legitimate-primary-rotation"
	saved, err := repo.UpdateExcelPrimaryCredentialsIfUnchanged(t.Context(), before, credentials)
	require.NoError(t, err)
	require.True(t, saved)
	// The old full credential snapshot is deliberately invalid.
	restored, err := repo.RestoreExcelBPSAfter403(t.Context(), before, recoveryProbeSuccess(before))
	require.NoError(t, err)
	require.False(t, restored)
	after, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, before.Extra[service.ExcelBPS403LastProbeAtKey], after.Extra[service.ExcelBPS403LastProbeAtKey])
	require.Equal(t, excelBPSActionID(before), excelBPSActionID(after))
	restored, err = repo.RestoreExcelBPSAfter403(t.Context(), after, recoveryProbeSuccess(after))
	require.NoError(t, err)
	require.True(t, restored, "only the acknowledged refresh snapshot may finish the original claim")
}

func TestExcelBPSFailureMissingGrantIsNot403AndDoesNotProbe(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	_, err := integrationDB.ExecContext(t.Context(), "DELETE FROM openai_excel_grants WHERE account_id=$1", a.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials-'client_id',
 extra=(extra-'openai_excel_bps_403_disabled_at')||'{"openai_excel_bps":true,"openai_excel_bps_auto_disable_on_403":false}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	a, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	changed, err := repo.DisableExcelBPSForFailure(t.Context(), a, service.ExcelBPSFailure{Reason: "grant_missing", ReauthRequired: true})
	require.NoError(t, err)
	require.True(t, changed, "missing authorization does not borrow the optional 403 setting")
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.False(t, fresh.IsExcelBPSEnabled())
	require.Equal(t, a.Credentials, fresh.Credentials)
	require.NotContains(t, fresh.Extra, service.ExcelBPS403DisabledAtKey)
	record := fresh.Extra[service.ExcelBPSFailureKey].(map[string]any)
	require.Equal(t, "grant_missing", record["reason"])
	require.Equal(t, true, record["reauth_required"])
	require.Equal(t, "system", record["source"])
	require.NotEmpty(t, record["id"])
	require.Nil(t, record["next_probe_at"])
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), fresh, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.False(t, claimed)
}

func TestExcelBPSFailureStaleMissingGrantCannotDisableNewAuthorization(t *testing.T) {
	repo, a, _ := newExcelBPSRecoveryFixture(t)
	_, err := integrationDB.ExecContext(t.Context(), "DELETE FROM openai_excel_grants WHERE account_id=$1", a.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials-'client_id',
 extra=extra||'{"openai_excel_bps":true}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	a, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	saved, err := repo.StoreExcelCredentials(t.Context(), a, "", "new-authorization")
	require.NoError(t, err)
	require.True(t, saved)
	changed, err := repo.DisableExcelBPSForFailure(t.Context(), a, service.ExcelBPSFailure{Reason: "grant_missing", ReauthRequired: true})
	require.NoError(t, err)
	require.False(t, changed)
	current, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.True(t, current.IsExcelBPSEnabled())
}

func TestExcelBPSRecoveryIndependentRefreshVersusReplacement(t *testing.T) {
	for _, refresh := range []bool{true, false} {
		t.Run(map[bool]string{true: "token rotation", false: "reauthorization"}[refresh], func(t *testing.T) {
			repo, a, now := newExcelBPSRecoveryFixture(t)
			_, err := integrationDB.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials-'client_id' WHERE id=$1`, a.ID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(t.Context(), `UPDATE openai_excel_grants SET ciphertext='old-grant' WHERE account_id=$1`, a.ID)
			require.NoError(t, err)
			a, err = repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
			require.NoError(t, err)
			require.True(t, claimed)
			snapshot, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			ctx := t.Context()
			if refresh {
				ctx = service.WithExcelCredentialRefresh(ctx)
			}
			saved, err := repo.StoreExcelCredentials(ctx, snapshot, "old-grant", "new-grant")
			require.NoError(t, err)
			require.True(t, saved)
			restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
			require.NoError(t, err)
			require.Equal(t, refresh, restored)
		})
	}
}

func TestExcelBPSRecoveryPolicyChangesCannotBeOverwritten(t *testing.T) {
	for _, change := range []string{"explicit false", "group membership", "global off then on", "raw grant replacement"} {
		t.Run(change, func(t *testing.T) {
			repo, a, now := newExcelBPSRecoveryFixture(t)
			if change == "raw grant replacement" {
				_, err := integrationDB.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials-'client_id' WHERE id=$1`, a.ID)
				require.NoError(t, err)
				_, err = integrationDB.ExecContext(t.Context(), `UPDATE openai_excel_grants SET ciphertext='old-grant' WHERE account_id=$1`, a.ID)
				require.NoError(t, err)
				a, err = repo.GetByID(t.Context(), a.ID)
				require.NoError(t, err)
			}
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
			require.NoError(t, err)
			require.True(t, claimed)
			snapshot, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			switch change {
			case "explicit false":
				require.NoError(t, repo.UpdateExtra(t.Context(), a.ID, map[string]any{"openai_excel_bps": false}))
			case "group membership":
				group, err := integrationEntClient.Group.Create().SetName("bps-recovery-policy").SetPlatform("openai").Save(t.Context())
				require.NoError(t, err)
				t.Cleanup(func() {
					_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", group.ID)
				})
				_, err = integrationDB.ExecContext(t.Context(), "INSERT INTO account_groups(account_id,group_id,priority) VALUES($1,$2,1)", a.ID, group.ID)
				require.NoError(t, err)
			case "global off then on":
				_, err := integrationDB.ExecContext(t.Context(), "UPDATE settings SET value='true',updated_at=updated_at+interval '1 second' WHERE key='excel_bps_enabled'")
				require.NoError(t, err)
			case "raw grant replacement":
				_, err := integrationDB.ExecContext(t.Context(), "UPDATE openai_excel_grants SET ciphertext='replacement' WHERE account_id=$1", a.ID)
				require.NoError(t, err)
			}
			restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
			require.NoError(t, err)
			require.False(t, restored, change)
		})
	}
}
