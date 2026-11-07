//go:build integration

package repository

import (
	"context"
	"maps"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSControlPostgresStaleEnableAndExplicitStop(t *testing.T) {
	for _, path := range []string{"full", "extra", "bulk"} {
		t.Run(path, func(t *testing.T) {
			repo, a, now := newExcelBPSRecoveryFixture(t)
			claimed, err := repo.ClaimExcelBPS403Probe(t.Context(), a, now)
			require.NoError(t, err)
			require.True(t, claimed)
			claim, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			write := func(patch map[string]any) error {
				switch path {
				case "full":
					edit := *a
					edit.Extra = maps.Clone(a.Extra)
					maps.Copy(edit.Extra, patch)
					edit.Name = "stale edit"
					return repo.Update(t.Context(), &edit)
				case "extra":
					return repo.UpdateExtra(t.Context(), a.ID, patch)
				default:
					name := "stale edit"
					_, err := repo.BulkUpdate(t.Context(), []int64{a.ID}, service.AccountBulkUpdate{Name: &name, Extra: patch})
					return err
				}
			}
			before, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			var events int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&events))
			require.ErrorIs(t, write(map[string]any{"openai_excel_bps": true}), service.ErrExcelBPSStateConflict)
			after, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.Equal(t, before.Extra, after.Extra)
			require.Equal(t, before.Name, after.Name)
			var eventsAfter int
			require.NoError(t, integrationDB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM scheduler_outbox WHERE account_id=$1", a.ID).Scan(&eventsAfter))
			require.Equal(t, events, eventsAfter)

			// Explicit false must invalidate an already-running probe, even when
			// the persisted switch was false before this command.
			require.NoError(t, write(map[string]any{"openai_excel_bps": false}))
			restored, err := repo.RestoreExcelBPSAfter403(t.Context(), claim, recoveryProbeSuccess(claim))
			require.NoError(t, err)
			require.False(t, restored)
			fresh, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.False(t, fresh.IsExcelBPSEnabled())
			require.NotContains(t, fresh.Extra, service.ExcelBPSFailureKey)
			require.NotContains(t, fresh.Extra, service.ExcelBPS403LastProbeAtKey)
		})
	}
}

func TestExcelBPSControlPostgresConditionalReenable(t *testing.T) {
	for _, path := range []string{"full", "extra", "bulk"} {
		t.Run(path, func(t *testing.T) {
			repo, a, _ := newExcelBPSRecoveryFixture(t)
			at := a.Extra[service.ExcelBPS403DisabledAtKey]
			patch := map[string]any{"openai_excel_bps": true, service.ExcelBPSExpectedDisabledAtKey: at}
			var err error
			switch path {
			case "full":
				maps.Copy(a.Extra, patch)
				err = repo.Update(t.Context(), a)
			case "extra":
				err = repo.UpdateExtra(t.Context(), a.ID, patch)
			case "bulk":
				_, err = repo.BulkUpdate(t.Context(), []int64{a.ID}, service.AccountBulkUpdate{Extra: patch})
			}
			require.NoError(t, err)
			fresh, err := repo.GetByID(t.Context(), a.ID)
			require.NoError(t, err)
			require.True(t, fresh.IsExcelBPSEnabled())
			require.NotContains(t, fresh.Extra, service.ExcelBPSExpectedDisabledAtKey)
			require.NotContains(t, fresh.Extra, service.ExcelBPS403DisabledAtKey)
			require.Contains(t, fresh.Extra, service.ExcelBPS403MovedAtKey, "retain group-action history")
		})
	}
}

func TestExcelBPSControlPostgresEnableWaitsForSystemShutdown(t *testing.T) {
	repo, a, _ := newExcelBPSRecoveryFixture(t)
	// Seed active state, then retain an editor's enabled snapshot.
	_, err := integrationDB.ExecContext(t.Context(), `UPDATE accounts SET extra=(extra-'openai_excel_bps_403_disabled_at')||'{"openai_excel_bps":true}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	a, err = repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	tx, err := integrationEntClient.Tx(t.Context())
	require.NoError(t, err)
	defer tx.Rollback()
	ctx := dbent.NewTxContext(t.Context(), tx)
	grant, err := repo.GetExcelCredentials(ctx, a.ID)
	require.NoError(t, err)
	changed, err := repo.DisableExcelBPSForFailure(ctx, a, service.ExcelBPSFailure{Reason: "account_forbidden", GrantFingerprint: service.ExcelGrantFingerprint(grant)})
	require.NoError(t, err)
	require.True(t, changed)
	rows, err := tx.Client().QueryContext(ctx, "SELECT pg_backend_pid()")
	require.NoError(t, err)
	require.True(t, rows.Next())
	var locker int
	require.NoError(t, rows.Scan(&locker))
	require.NoError(t, rows.Close())
	editCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- repo.UpdateExtra(editCtx, a.ID, map[string]any{"openai_excel_bps": true}) }()
	// Observe the real PostgreSQL lock wait before allowing shutdown to commit;
	// elapsed sleeps are not used as proof that the editor reached the lock.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		require.NoError(t, integrationDB.QueryRowContext(editCtx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", locker).Scan(&waiting))
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("editor did not wait for shutdown: %v", err)
		case <-editCtx.Done():
			t.Fatal("editor did not reach the row lock")
		case <-ticker.C:
		}
	}
	require.NoError(t, tx.Commit())
	select {
	case err := <-done:
		require.ErrorIs(t, err, service.ErrExcelBPSStateConflict)
	case <-editCtx.Done():
		t.Fatal("editor did not finish")
	}
	fresh, err := repo.GetByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.False(t, fresh.IsExcelBPSEnabled())
	require.Contains(t, fresh.Extra, service.ExcelBPSFailureKey)
}
