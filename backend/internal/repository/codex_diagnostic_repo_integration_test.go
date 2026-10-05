//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexDiagnosticPostgresLifecycle(t *testing.T) {
	ctx := context.Background()
	r := &codexDiagnosticRepository{db: integrationDB}
	var owner, key, account int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO users(email,password_hash,role) VALUES($1,'test','admin') RETURNING id`, fmt.Sprintf("diagnostic-%d@example.invalid", time.Now().UnixNano())).Scan(&owner))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO api_keys(user_id,key,name) VALUES($1,$2,'Test billing key') RETURNING id`, owner, fmt.Sprintf("sk-diagnostic-%d", time.Now().UnixNano())).Scan(&key))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type) VALUES('diagnostic','openai','oauth') RETURNING id`).Scan(&account))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id=$1", key)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1", owner)
	})
	p := &service.CodexDiagnosticPlan{AccountID: account, OwnerID: owner, APIKeyID: key, Models: []string{"gpt-5.4"}, Enabled: true}
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.NotNil(t, p.NextRunAt)
	require.WithinDuration(t, time.Now().Add(time.Hour), *p.NextRunAt, 5*time.Second)
	firstDue := *p.NextRunAt
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.Equal(t, firstDue, *p.NextRunAt)
	p.IntervalMinutes = 300
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.WithinDuration(t, time.Now().Add(5*time.Hour), *p.NextRunAt, 5*time.Second)
	// Multiple instances scanning the same due account may enqueue it once only.
	_, err := integrationDB.ExecContext(ctx, "UPDATE codex_diagnostic_plans SET next_run_at=NOW()-INTERVAL '1 minute' WHERE account_id=$1", account)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- r.EnqueueDue(ctx) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	history, err := r.ListRuns(ctx, account, 0, 20)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "scheduled", history[0].Source)
	scheduledPlan, err := r.GetPlan(ctx, account)
	require.NoError(t, err)
	require.Equal(t, 300, scheduledPlan.IntervalMinutes)
	require.WithinDuration(t, time.Now().Add(5*time.Hour), *scheduledPlan.NextRunAt, 5*time.Second)
	_, err = r.Enqueue(ctx, p, "manual", "Test")
	require.ErrorIs(t, err, service.ErrDiagnosticBusy)
	running, err := r.Claim(ctx, "worker-a")
	require.NoError(t, err)
	require.NotNil(t, running)
	duplicate, err := r.Claim(ctx, "worker-b")
	require.NoError(t, err)
	require.Nil(t, duplicate)
	running.Items = []service.CodexDiagnosticItem{{Model: "gpt-5.4", Status: "normal", Probability: .99, FingerprintCommit: "bank-revision"}}
	require.NoError(t, r.SaveProgress(ctx, running))
	same, err := r.GetRun(ctx, account, running.ID)
	require.NoError(t, err)
	require.Len(t, same.Items, 1)
	_, err = r.GetRun(ctx, account+1, running.ID)
	require.ErrorIs(t, err, service.ErrDiagnosticNotFound)
	finished := time.Now()
	running.Status = "normal"
	running.FinishedAt = &finished
	require.NoError(t, r.SaveProgress(ctx, running))
	summaries, err := r.Summaries(ctx, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "normal", summaries[account].Status)
	// Toggling the timer preserves the interpretation of the last check.
	p.Enabled = false
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 1, p.Revision)
	require.Nil(t, p.NextRunAt)
	summaries, err = r.Summaries(ctx, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "normal", summaries[account].Status)
	require.False(t, summaries[account].Enabled)
	p.Models = []string{"gpt-5.5"}
	require.NoError(t, r.SavePlan(ctx, p))
	require.EqualValues(t, 2, p.Revision)
	summaries, err = r.Summaries(ctx, []int64{account})
	require.NoError(t, err)
	require.Equal(t, "unknown", summaries[account].Status)
	history, err = r.ListRuns(ctx, account, 0, 20)
	require.NoError(t, err)
	require.Len(t, history, 1)
	// A canceled queued run is never claimed, but remains in history.
	queued, err := r.Enqueue(ctx, p, "manual", "Test")
	require.NoError(t, err)
	require.NoError(t, r.Cancel(ctx, account, queued.ID))
	duplicate, err = r.Claim(ctx, "worker-c")
	require.NoError(t, err)
	require.Nil(t, duplicate)
	// A worker lost during a paid call is failed, never reclaimed/replayed.
	_, err = r.Enqueue(ctx, p, "manual", "Test")
	require.NoError(t, err)
	expired, err := r.Claim(ctx, "worker-expired")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE codex_diagnostic_runs SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1", expired.ID)
	require.NoError(t, err)
	require.NoError(t, r.EnqueueDue(ctx))
	require.ErrorIs(t, r.SaveProgress(ctx, expired), service.ErrDiagnosticNotFound)
	stale, err := r.GetRun(ctx, account, expired.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", stale.Status)
	require.Equal(t, "worker_interrupted", stale.Reason)
	duplicate, err = r.Claim(ctx, "worker-restart")
	require.NoError(t, err)
	require.Nil(t, duplicate)
	// Cursor pagination never repeats results.
	page, err := r.ListRuns(ctx, account, 0, 2)
	require.NoError(t, err)
	require.Len(t, page, 2)
	older, err := r.ListRuns(ctx, account, page[1].ID, 2)
	require.NoError(t, err)
	require.Len(t, older, 1)
	// Retention includes the newest queued run and removes only older terminal records.
	for i := 0; i < 12; i++ {
		queued, err := r.Enqueue(ctx, p, "manual", "Test")
		require.NoError(t, err)
		require.NoError(t, r.Cancel(ctx, account, queued.ID))
	}
	history, err = r.ListRuns(ctx, account, 0, 50)
	require.NoError(t, err)
	require.Len(t, history, 10)
	var retained int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_diagnostic_runs WHERE account_id=$1", account).Scan(&retained))
	require.Equal(t, 10, retained)
	_, err = r.GetRun(ctx, account, running.ID)
	require.ErrorIs(t, err, service.ErrDiagnosticNotFound)
	queued, err = r.Enqueue(ctx, p, "manual", "Test")
	require.NoError(t, err)
	history, err = r.ListRuns(ctx, account, 0, 50)
	require.NoError(t, err)
	require.Len(t, history, 10)
	require.Equal(t, queued.ID, history[0].ID)
	require.Equal(t, "queued", history[0].Status)

}
