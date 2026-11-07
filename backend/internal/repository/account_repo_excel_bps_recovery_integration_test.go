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
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('excel_bps_enabled','true')
 ON CONFLICT(key) DO UPDATE SET value='true'`)
	require.NoError(t, err)
	a.Extra["openai_excel_bps"] = false
	a.Extra[service.ExcelBPSAutoRecoverOn403Key] = true
	a.Extra[service.ExcelBPS403DisabledAtKey] = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	a.Extra[service.ExcelBPS403MovedAtKey] = now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	a.Extra[service.ExcelBPS403MovedGroupIDKey] = float64(0)
	// Seed an existing account; admission now requires real verified OAuth claims.
	a = mustCreateAccount(t, integrationEntClient, a)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO openai_excel_grants(account_id,ciphertext) VALUES($1,'recovery-grant')`, a.ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", a.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM account_probe_usage WHERE account_id=$1", a.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", a.ID)
	})
	a, err = repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	return repo, a, now
}

func recoveryProbeSuccess(a *service.Account) service.ExcelBPS403ProbeResult {
	claim, _ := a.Extra[service.ExcelBPS403LastProbeAtKey].(string)
	at, _ := time.Parse(time.RFC3339Nano, claim)
	outcome := service.ExcelBPS403ProbeClaimResult(a, at)
	outcome.Status, outcome.Stage, outcome.Reason = "restored", "restore", "success"
	outcome.HTTPStatus, outcome.CompletedAt = 200, time.Now().UTC().Format(time.RFC3339Nano)
	outcome.Usage.SendState, outcome.Usage.Terminal = "sent", "completed"
	return outcome
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
	restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
	require.NoError(t, err)
	require.True(t, restored)
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.True(t, fresh.IsExcelBPSEnabled())
	require.NotContains(t, fresh.Extra, service.ExcelBPS403DisabledAtKey)
	require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeAtKey)
	outcome := fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)
	require.Equal(t, "restored", outcome["status"])
	require.Equal(t, "success", outcome["reason"])
	require.Equal(t, float64(200), outcome["http_status"])
	require.Equal(t, float64(42), fresh.Extra["codex_5h_used_percent"])
	require.Equal(t, a.Extra["openai_excel_bps_models"], fresh.Extra["openai_excel_bps_models"])
	require.Equal(t, a.Extra[service.ExcelBPS403MovedAtKey], fresh.Extra[service.ExcelBPS403MovedAtKey])
	require.Equal(t, a.GroupIDs, fresh.GroupIDs)
	var after int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&after))
	require.Equal(t, before+1, after)
	restored, err = repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
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
			restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
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
				changed, err = repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
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
		repo, a, _ := newExcelBPSRecoveryFixture(t)
		a.Extra[service.ExcelBPS403RecoveryIntervalMinutesKey] = minutes
		// Configure the active account first. An explicit switch edit must
		// acknowledge the old marker, not manufacture a system shutdown.
		a.Extra["openai_excel_bps"] = true
		a.Extra[service.ExcelBPSExpectedDisabledAtKey] = a.Extra[service.ExcelBPS403DisabledAtKey]
		require.NoError(t, repo.Update(t.Context(), a))
		configured, err := repo.GetByID(t.Context(), a.ID)
		require.NoError(t, err)
		require.NotContains(t, configured.Extra, service.ExcelBPS403DisabledAtKey)
		grant, err := repo.GetExcelCredentials(t.Context(), a.ID)
		require.NoError(t, err)
		disabledBySystem, err := repo.DisableExcelBPSForFailure(t.Context(), configured, service.ExcelBPSFailure{
			Reason: "account_forbidden", GrantFingerprint: service.ExcelGrantFingerprint(grant),
		})
		require.NoError(t, err)
		require.True(t, disabledBySystem)
		fresh, err := repo.GetByID(t.Context(), a.ID)
		require.NoError(t, err)
		interval := time.Duration(minutes) * time.Minute
		// Use the actual system action time for both due boundaries and restart.
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

func TestExcelBPS403ProbeResultOnlyUpdatesCurrentClaim(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	old, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	outcome := recoveryProbeSuccess(old)
	outcome.Status, outcome.Stage, outcome.Reason = "failed", "request", "timeout"
	outcome.DurationMS = 45000
	saved, err := repo.RecordExcelBPS403ProbeResult(t.Context(), old, outcome)
	require.NoError(t, err)
	require.True(t, saved)
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, "timeout", fresh.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["reason"])
	claimed, err = repo.ClaimExcelBPS403Probe(t.Context(), fresh, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)
	outcome.Reason = "stale_result"
	saved, err = repo.RecordExcelBPS403ProbeResult(t.Context(), old, outcome)
	require.NoError(t, err)
	require.False(t, saved)
	current, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Hour).Format(time.RFC3339Nano), current.Extra[service.ExcelBPS403LastProbeAtKey])
	require.Equal(t, "pending", current.Extra[service.ExcelBPS403LastProbeResultKey].(map[string]any)["reason"])
}

func TestExcelBPS403ProbeUsagePersistsThroughRestoreAndStatusProjection(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	snapshot, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	pending := service.ExcelBPS403RecoveryStatusFromAccount(snapshot, true, now)
	require.Equal(t, "probing", pending.State)
	require.Equal(t, "unknown", pending.LastResult.Usage.State)
	require.Equal(t, "unknown", pending.LastResult.Usage.SendState)
	require.Nil(t, pending.LastResult.Usage.Tokens)
	outcome := recoveryProbeSuccess(snapshot)
	input, output, zero := int64(9), int64(3), int64(0)
	outcome.Usage.State, outcome.Usage.ResponseID = "measured", "resp_probe"
	outcome.Usage.Tokens = &service.ExcelBPSProbeTokens{Input: &input, Output: &output, Cached: &zero}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	failed := outcome
	failed.Status = "failed"
	saved, err := repo.RecordExcelBPS403ProbeResult(canceled, snapshot, failed)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, saved, "failed persistence cannot return a durable receipt")
	restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.True(t, restored)
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	state := service.ExcelBPS403RecoveryStatusFromAccount(fresh, true, now)
	require.Equal(t, outcome, *state.LastResult)
	require.Nil(t, state.LastResult.Usage.Tokens.Reasoning)
	requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(snapshot, now))
	requireProbeUsageFact(t, repo, "outcome", outcome)
	restored, err = repo.RestoreExcelBPSAfter403(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.False(t, restored)
}

func TestExcelBPS403ProbeUsageResultIsClaimBoundAndIdempotent(t *testing.T) {
	repo, a, now := newExcelBPSRecoveryFixture(t)
	claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
	require.NoError(t, err)
	require.True(t, claimed)
	snapshot, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	outcome := recoveryProbeSuccess(snapshot)
	outcome.Status, outcome.Stage, outcome.Reason = "failed", "response", "upstream_rejected"
	outcome.Usage.State, outcome.Usage.Terminal = "lower_bound", "failed"
	input, output := int64(10), int64(2)
	outcome.Usage.Tokens = &service.ExcelBPSProbeTokens{Input: &input, Output: &output}
	wrong := outcome
	usage := *outcome.Usage
	wrong.Usage = &usage
	wrong.Usage.AccountID++
	saved, err := repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, wrong)
	require.ErrorIs(t, err, errAccountProbeUsageConflict)
	require.False(t, saved)
	// Turning off global recovery permission cannot erase already consumed
	// tokens. Recording facts grants no permission to enable the account.
	_, err = integrationDB.ExecContext(t.Context(), `UPDATE settings SET value='false' WHERE key='excel_bps_enabled'`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), `UPDATE settings SET value='true' WHERE key='excel_bps_enabled'`)
	})
	saved, err = repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
	require.NoError(t, err)
	require.True(t, saved)
	_, err = integrationDB.ExecContext(t.Context(), "UPDATE scheduler_outbox SET dedup_key=NULL WHERE account_id=$1", a.ID)
	require.NoError(t, err)
	var before int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&before))
	conflicting := outcome
	conflictingUsage := *outcome.Usage
	conflicting.Usage = &conflictingUsage
	changedInput := int64(99)
	conflicting.Usage.Tokens = &service.ExcelBPSProbeTokens{Input: &changedInput, Output: &output}
	for _, result := range []service.ExcelBPS403ProbeResult{outcome, conflicting} {
		saved, err = repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, result)
		require.NoError(t, err)
		require.False(t, saved)
	}
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	state := service.ExcelBPS403RecoveryStatusFromAccount(fresh, false, now)
	require.Equal(t, outcome, *state.LastResult)
	require.False(t, fresh.IsExcelBPSEnabled())
	var after int
	require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&after))
	require.Equal(t, before, after)
}

func TestExcelBPS403ProbeUsageSurvivesWithdrawalWithoutRestoringAuthorization(t *testing.T) {
	for _, withdrawal := range []string{"extra edit", "full edit", "bulk edit", "reauthorization required"} {
		t.Run(withdrawal, func(t *testing.T) {
			repo, a, now := newExcelBPSRecoveryFixture(t)
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
			require.NoError(t, err)
			require.True(t, claimed)
			snapshot, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			outcome := recoveryProbeSuccess(snapshot)
			outcome.Status, outcome.Stage, outcome.Reason = "failed", "response", "upstream_rejected"
			outcome.Usage.Terminal, outcome.Usage.State = "failed", "lower_bound"
			input := int64(12)
			outcome.Usage.Tokens = &service.ExcelBPSProbeTokens{Input: &input}
			switch withdrawal {
			case "extra edit":
				require.NoError(t, repo.UpdateExtra(t.Context(), a.ID, map[string]any{"openai_excel_bps": false}))
			case "full edit":
				edited := *snapshot
				edited.Extra = map[string]any{"openai_excel_bps": false}
				require.NoError(t, repo.Update(t.Context(), &edited))
			case "bulk edit":
				_, err := repo.BulkUpdate(t.Context(), []int64{a.ID}, service.AccountBulkUpdate{Extra: map[string]any{"openai_excel_bps": false}})
				require.NoError(t, err)
			case "reauthorization required":
				grant, err := repo.GetExcelCredentials(t.Context(), a.ID)
				require.NoError(t, err)
				changed, err := repo.DisableExcelBPSForFailure(t.Context(), snapshot, service.ExcelBPSFailure{
					Reason: "refresh_grant_missing", ReauthRequired: true, GrantFingerprint: service.ExcelGrantFingerprint(grant),
				})
				require.NoError(t, err)
				require.True(t, changed)
			}
			fresh, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeAtKey, "recovery permission was withdrawn")
			require.Contains(t, fresh.Extra, service.ExcelBPS403LastProbeResultKey, "the claimed observation remains attributable")
			saved, err := repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
			require.NoError(t, err)
			require.True(t, saved)
			requireProbeUsageFact(t, repo, "claim", service.ExcelBPS403ProbeClaimResult(snapshot, now))
			requireProbeUsageFact(t, repo, "outcome", outcome)
			saved, err = repo.RecordExcelBPS403ProbeResult(t.Context(), snapshot, outcome)
			require.NoError(t, err)
			require.False(t, saved)
			restored, err := repo.RestoreExcelBPSAfter403(t.Context(), snapshot, recoveryProbeSuccess(snapshot))
			require.NoError(t, err)
			require.False(t, restored)
			fresh, err = repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.False(t, fresh.IsExcelBPSEnabled())
			state := service.ExcelBPS403RecoveryStatusFromAccount(fresh, true, now)
			require.Equal(t, outcome, *state.LastResult)
			if withdrawal == "reauthorization required" {
				require.Equal(t, "reauthorization_required", state.State)
			} else {
				require.Equal(t, "history", state.State)
			}
			require.Empty(t, state.NextProbeAt)
			claimed, err = repo.ClaimExcelBPS403Probe(t.Context(), fresh, now.Add(24*time.Hour))
			require.NoError(t, err)
			require.False(t, claimed)
		})
	}
}
