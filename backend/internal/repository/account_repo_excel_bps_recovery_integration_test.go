//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newExcelBPSRecoveryFixture(t *testing.T) (*accountRepository, *service.Account, time.Time) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	a := newExcelBPSAutoDisableAccount()
	a.Extra["openai_excel_bps"] = false
	a.Extra[service.ExcelBPSAutoRecoverOn403Key] = true
	a.Extra[service.ExcelBPS403DisabledAtKey] = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	a.Extra[service.ExcelBPS403MovedAtKey] = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	a.Extra[service.ExcelBPS403MovedGroupIDKey] = float64(0)
	// Seed an existing account; admission now requires real verified OAuth claims.
	a = mustCreateAccount(t, integrationEntClient, a)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", a.ID)
	})
	a, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	return repo, a, now
}

func TestExcelBPS403RecoveryClaimsOnceAcrossInstances(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
			if !assertRecoveryNoError(t, err) {
				return
			}
			if claimed {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), successes.Load())
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, now.Format(time.RFC3339Nano), fresh.Extra[service.ExcelBPS403LastProbeAtKey])
	restarted := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	claimed, err := restarted.ClaimExcelBPS403Probe(t.Context(), fresh, now.Add(time.Hour-time.Second))
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = restarted.ClaimExcelBPS403Probe(t.Context(), fresh, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)
}

func assertRecoveryNoError(t *testing.T, err error) bool {
	t.Helper()
	if err != nil {
		t.Errorf("claim failed: %v", err)
		return false
	}
	return true
}

func TestExcelBPS403RecoveryRestoresProtocolAndSchedulerAtomically(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	snapshot, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	// Usage refreshes are unrelated and must not prevent a successful restoration.
	_, err = integrationDB.ExecContext(t.Context(), `UPDATE accounts SET extra=extra || '{"codex_5h_used_percent":42}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	// Simulate consumption of the creation event; pending events are deliberately
	// coalesced by the scheduler outbox's unique dedup key.
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE scheduler_outbox SET dedup_key=NULL WHERE account_id=$1", a.ID)
	require.NoError(t, err)
	var before int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&before))
	restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot)
	require.NoError(t, err)
	require.True(t, restored)
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.True(t, fresh.IsExcelBPSEnabled())
	require.Equal(t, "restored", fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["status"])
	require.NotContains(t, fresh.Extra, service.ExcelBPS403DisabledAtKey)
	require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeAtKey)
	require.Equal(t, float64(42), fresh.Extra["codex_5h_used_percent"])
	require.Equal(t, a.Extra["openai_excel_bps_models"], fresh.Extra["openai_excel_bps_models"])
	require.Equal(t, a.Extra[service.ExcelBPS403MovedAtKey], fresh.Extra[service.ExcelBPS403MovedAtKey])
	require.Equal(t, a.GroupIDs, fresh.GroupIDs)
	var after int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&after))
	require.Equal(t, before+1, after)
	restored, err = repo.RestoreExcelBPSAfter403(t.Context(), snapshot)
	require.NoError(t, err)
	require.False(t, restored)
}

func TestExcelBPS403RecoveryRejectsChangesDuringProbe(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"interval changed", `extra=extra || '{"openai_excel_bps_403_recovery_interval_minutes":360}'::jsonb`},
		{"opted out", `extra=extra || '{"openai_excel_bps_auto_recover_on_403":false}'::jsonb`},
		{"auto shutdown off", `extra=extra || '{"openai_excel_bps_auto_disable_on_403":false}'::jsonb`},
		{"model changed", `extra=extra || '{"openai_excel_bps_models":["gpt-5.6-sol"]}'::jsonb`},
		{"proxy policy changed", `extra=extra || '{"openai_excel_bps_mihomo":true}'::jsonb`},
		{"credentials changed", `credentials=credentials || '{"access_token":"replacement"}'::jsonb`},
		{"account disabled", `status='disabled'`},
		{"scheduling paused", `schedulable=false`},
		{"new shutdown", `extra=extra || '{"openai_excel_bps_403_disabled_at":"2026-09-28T00:00:00Z"}'::jsonb`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, a, now := newExcelBPSRecoveryFixture(t)
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
			require.NoError(t, err)
			require.True(t, claimed)
			snapshot, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(t.Context(), "UPDATE accounts SET "+tc.mutation+" WHERE id=$1", a.ID)
			require.NoError(t, err)
			restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot)
			require.NoError(t, err)
			require.False(t, restored)
			var extra map[string]any
			var raw []byte
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT extra FROM accounts WHERE id=$1", a.ID).Scan(&raw))
			require.NoError(t, json.Unmarshal(raw, &extra))
			require.NotEqual(t, true, extra["openai_excel_bps"])
		})
	}
}

func TestExcelBPS403RecoveryStopsWhenCloudEnabled(t *testing.T) {
	for _, stage := range []string{"before claim", "before restore"} {
		t.Run(stage, func(t *testing.T) {
			repo, a, now := newExcelBPSRecoveryFixture(t)
			_, err := integrationDB.ExecContext(t.Context(), `UPDATE accounts SET credentials=credentials || '{"chatgpt_user_id":"user"}'::jsonb WHERE id=$1`, a.ID)
			require.NoError(t, err)
			a, err = repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			binding := cloudAuthorizationBindingFixture()
			binding.AccountID = a.GetChatGPTAccountID()
			saved, err := repo.StoreCloudAuthorization(t.Context(), a, "", binding)
			require.NoError(t, err)
			require.True(t, saved)
			if stage == "before restore" {
				claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
				require.NoError(t, err)
				require.True(t, claimed)
			}
			snapshot, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.NoError(t, repo.UpdateExtra(t.Context(), a.ID, map[string]any{service.OpenAICloudEnabledKey: true}))
			var before int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&before))
			var changed bool
			if stage == "before claim" {
				changed, err = repo.ClaimExcelBPS403Probe(t.Context(), snapshot, now)
			} else {
				changed, err = repo.RestoreExcelBPSAfter403(t.Context(), snapshot)
			}
			require.NoError(t, err)
			require.False(t, changed, "the current database protocol must override a stale recovery snapshot")
			fresh, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.True(t, fresh.IsOpenAICloudEnabled())
			require.Equal(t, false, fresh.Extra["openai_excel_bps"])
			for _, key := range []string{service.ExcelBPS403DisabledAtKey, service.ExcelBPS403LastProbeAtKey, "openai_excel_bps_models"} {
				require.Equal(t, snapshot.Extra[key], fresh.Extra[key], key)
			}
			var after int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&after))
			require.Equal(t, before, after)
		})
	}
}

func TestExcelBPS403RecoveryCustomIntervalPersistsAcrossRestart(t *testing.T) {
	for _, minutes := range []int{30, 360} {
		repo, a, now := newExcelBPSRecoveryFixture(t)
		a.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey] = minutes
		a.Extra[service.ExcelBPS403DisabledAtKey] = now.Format(time.RFC3339Nano)
		require.NoError(t, repo.Update(t.Context(), a))
		fresh, err := repo.GetByID(t.Context(), a.ID)
		require.NoError(t, err)
		interval := time.Duration(minutes) * time.Minute
		// Updates cannot forge the server-owned shutdown marker; use its persisted time.
		disabledText, ok := fresh.Extra[service.ExcelBPS403DisabledAtKey].(string)
		require.True(t, ok)
		disabled, err := time.Parse(time.RFC3339Nano, disabledText)
		require.NoError(t, err)
		claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), fresh, disabled.Add(interval-time.Second))
		require.NoError(t, err)
		require.False(t, claimed)
		claimed, err = repo.ClaimExcelBPS403Probe(t.Context(), fresh, disabled.Add(interval))
		require.NoError(t, err)
		require.True(t, claimed)
		fresh, err = repo.GetByID(t.Context(), a.ID)
		require.NoError(t, err)
		require.Equal(t, float64(minutes), fresh.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey])
		restarted := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
		claimed, err = restarted.ClaimExcelBPS403Probe(t.Context(), fresh, disabled.Add(2*interval-time.Second))
		require.NoError(t, err)
		require.False(t, claimed)
		claimed, err = restarted.ClaimExcelBPS403Probe(t.Context(), fresh, disabled.Add(2*interval))
		require.NoError(t, err)
		require.True(t, claimed)
	}
}

func TestExcelBPS403RecoveryOutcomeGuardAndEditProtection(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	snapshot, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	outcome := service.ExcelBPS403ProbeResult{Status: "failed", Stage: "request", Reason: "http_rejected", HTTPStatus: 403, DurationMS: 22, CompletedAt: now.Add(time.Second).Format(time.RFC3339Nano)}
	saved, err := repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.True(t, saved)
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, float64(403), fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["http_status"])
	// A full account save must replace account policy but preserve server state.
	fresh.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey] = 30
	fresh.Extra[service.ExcelBPS403LastProbeResultKey] = map[string]any{"reason": "forged"}
	require.NoError(t, repo.Update(t.Context(), fresh))
	fresh, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, float64(30), fresh.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey])
	require.Equal(t, "http_rejected", fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["reason"])
	saved, err = repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.False(t, saved)
	// A stored legacy template marker cannot break normal saves, and partial
	// updates retire the marker without changing the account's chosen policy.
	_, err = integrationDB.ExecContext(t.Context(), `UPDATE accounts SET extra = extra || '{"openai_excel_bps_config_mode":"defaults"}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	legacy, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Update(t.Context(), legacy))
	legacy, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.NotContains(t, legacy.Extra, "openai_excel_bps_config_mode")
	// Partial editor writes cannot forge the result either.
	require.NoError(t, repo.UpdateExtra(t.Context(), a.ID, map[string]any{service.ExcelBPS403LastProbeResultKey: map[string]any{"reason": "forged"}}))
	fresh, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, "http_rejected", fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["reason"])
	// Explicit manual stop cannot be undone by the in-flight worker; history remains.
	snapshot = fresh
	require.NoError(t, repo.UpdateExtra(t.Context(), a.ID, map[string]any{"openai_excel_bps": false}))
	restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot)
	require.NoError(t, err)
	require.False(t, restored)
	saved, err = repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.False(t, saved)
	fresh, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.False(t, fresh.IsExcelBPS403RecoveryPending())
	require.Contains(t, fresh.Extra, service.ExcelBPS403DisabledAtKey)
	// Explicit re-enable applies the edited policy and acknowledges old probe state.
	fresh.Extra["openai_excel_bps"] = true
	fresh.Extra["openai_excel_bps_auto_disable_on_403"] = true
	fresh.Extra[service.ExcelBPSAutoRecoverOn403Key] = true
	fresh.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey] = 45
	require.NoError(t, repo.Update(t.Context(), fresh))
	fresh, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.True(t, fresh.IsExcelBPSEnabled())
	require.Equal(t, float64(45), fresh.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey])
	require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeResultKey)
}
