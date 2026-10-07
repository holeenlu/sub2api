package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Diagnostics use the existing plan/result repositories. Their JSON fields
// contain only diagnostic-specific configuration/evidence, never credentials.
const diagnosticPlanColumns = "account_id,enabled,next_run_at,updated_at,diagnostic_config"
const diagnosticRunColumns = "r.id,p.account_id,r.status,r.error_message,r.created_at,r.started_at,r.finished_at,r.diagnostic_run,COALESCE(r.diagnostic_run->>'worker_token','')"
const diagnosticRunJoin = " FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id "
const diagnosticPlanPredicate = "diagnostic_config IS NOT NULL"
const pruneDiagnosticRunsSQL = `DELETE FROM scheduled_test_results WHERE plan_id IN
 (SELECT id FROM scheduled_test_plans WHERE account_id=$1 AND diagnostic_config IS NOT NULL)
 AND diagnostic_run IS NOT NULL AND status NOT IN ('queued','running') AND id NOT IN (
 SELECT r.id FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id
 WHERE p.account_id=$1 AND r.diagnostic_run IS NOT NULL ORDER BY r.id DESC LIMIT $2)`

func scanDiagnosticPlan(row scannable) (*service.CodexDiagnosticPlan, error) {
	p := &service.CodexDiagnosticPlan{}
	var raw []byte
	if err := row.Scan(&p.AccountID, &p.Enabled, &p.NextRunAt, &p.UpdatedAt, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var config struct {
		Authorization *authz.Lease `json:"authorization"`
		OwnerID       int64        `json:"owner_id"`
		APIKeyID      int64        `json:"api_key_id"`
		Models        []string     `json:"models"`
		Interval      int          `json:"interval_minutes"`
		Revision      int64        `json:"revision"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	p.Authorization = config.Authorization
	p.OwnerID, p.APIKeyID, p.Models, p.IntervalMinutes, p.Revision = config.OwnerID, config.APIKeyID, config.Models, config.Interval, config.Revision
	return p, nil
}
func (r *scheduledTestPlanRepository) GetPlan(ctx context.Context, account int64) (*service.CodexDiagnosticPlan, error) {
	return scanDiagnosticPlan(r.db.QueryRowContext(ctx, "SELECT "+diagnosticPlanColumns+" FROM scheduled_test_plans WHERE account_id=$1 AND "+diagnosticPlanPredicate, account))
}
func (r *scheduledTestPlanRepository) SavePlan(ctx context.Context, p *service.CodexDiagnosticPlan) error {
	raw, err := json.Marshal(map[string]any{"authorization": p.Authorization, "owner_id": p.OwnerID, "api_key_id": p.APIKeyID, "models": p.Models, "interval_minutes": p.IntervalMinutes, "revision": 1})
	if err != nil {
		return err
	}
	return r.db.QueryRowContext(ctx, `
 INSERT INTO scheduled_test_plans(account_id,enabled,max_results,diagnostic_config,next_run_at)
 VALUES($1,$2,$4,$3::jsonb,CASE WHEN $2 THEN NOW()+($3::jsonb->>'interval_minutes')::integer*INTERVAL '1 minute' END)
 ON CONFLICT(account_id) WHERE diagnostic_config IS NOT NULL DO UPDATE SET
 enabled=EXCLUDED.enabled,
 diagnostic_config=EXCLUDED.diagnostic_config || jsonb_build_object('revision',
 (scheduled_test_plans.diagnostic_config->>'revision')::bigint +
 CASE WHEN (scheduled_test_plans.diagnostic_config - 'revision' - 'interval_minutes')
 IS DISTINCT FROM (EXCLUDED.diagnostic_config - 'revision' - 'interval_minutes') THEN 1 ELSE 0 END),
 next_run_at=CASE WHEN NOT EXCLUDED.enabled THEN NULL
 WHEN scheduled_test_plans.enabled IS DISTINCT FROM EXCLUDED.enabled
 OR (scheduled_test_plans.diagnostic_config - 'revision') IS DISTINCT FROM (EXCLUDED.diagnostic_config - 'revision')
 THEN EXCLUDED.next_run_at ELSE scheduled_test_plans.next_run_at END,updated_at=NOW()
 RETURNING (diagnostic_config->>'revision')::bigint,next_run_at,updated_at`, p.AccountID, p.Enabled, string(raw), service.CodexDiagnosticHistoryLimit).Scan(&p.Revision, &p.NextRunAt, &p.UpdatedAt)
}
func scanDiagnosticRun(row scannable) (*service.CodexDiagnosticRun, error) {
	r := &service.CodexDiagnosticRun{}
	var raw []byte
	if err := row.Scan(&r.ID, &r.AccountID, &r.Status, &r.Reason, &r.CreatedAt, &r.StartedAt, &r.FinishedAt, &raw, &r.WorkerToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrDiagnosticNotFound
		}
		return nil, err
	}
	// Only these evidence fields are read from JSON; row identity/status/timestamps
	// always come from the normal result columns.
	var data struct {
		Authorization *authz.Lease                  `json:"authorization"`
		OwnerID       int64                         `json:"owner_id"`
		APIKeyID      int64                         `json:"api_key_id"`
		KeyName       string                        `json:"api_key_name"`
		Revision      int64                         `json:"plan_revision"`
		Models        []string                      `json:"models"`
		Source        string                        `json:"source"`
		Items         []service.CodexDiagnosticItem `json:"items"`
		Cancel        bool                          `json:"cancel_requested"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	r.Authorization = data.Authorization
	r.OwnerID, r.APIKeyID, r.APIKeyName, r.PlanRevision = data.OwnerID, data.APIKeyID, data.KeyName, data.Revision
	r.Models, r.Source, r.Items, r.CancelRequested = data.Models, data.Source, data.Items, data.Cancel
	if r.Items == nil {
		r.Items = []service.CodexDiagnosticItem{}
	}
	return r, nil
}
func (r *scheduledTestPlanRepository) Enqueue(ctx context.Context, p *service.CodexDiagnosticPlan, source, name string) (*service.CodexDiagnosticRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	run, err := scanDiagnosticRun(tx.QueryRowContext(ctx, `
 WITH inserted AS (
 INSERT INTO scheduled_test_results(plan_id,status,started_at,finished_at,diagnostic_run)
 SELECT id,'queued',NULL,NULL,diagnostic_config || jsonb_build_object(
 'plan_revision',diagnostic_config->'revision','api_key_name',$4::text,'source',$5::text,'items','[]'::jsonb)
 FROM scheduled_test_plans WHERE account_id=$1 AND diagnostic_config IS NOT NULL
 AND (diagnostic_config->>'owner_id')::bigint=$2 AND (diagnostic_config->>'revision')::bigint=$3
 ON CONFLICT(plan_id) WHERE diagnostic_run IS NOT NULL AND status IN ('queued','running') DO NOTHING
 RETURNING *)
 SELECT `+diagnosticRunColumns+` FROM inserted r JOIN scheduled_test_plans p ON p.id=r.plan_id`, p.AccountID, p.OwnerID, p.Revision, name, source))
	if errors.Is(err, service.ErrDiagnosticNotFound) {
		return nil, service.ErrDiagnosticBusy
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, p.AccountID, service.CodexDiagnosticHistoryLimit); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}
func (r *scheduledTestPlanRepository) EnqueueDue(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE scheduled_test_results SET status='failed',error_message='worker_interrupted',finished_at=NOW()
 WHERE diagnostic_run IS NOT NULL AND status='running' AND (diagnostic_run->>'lease_until')::timestamptz<NOW()`)
	if err != nil {
		return err
	}
	queued, err := tx.QueryContext(ctx, `
 WITH due AS (SELECT p.id FROM scheduled_test_plans p JOIN accounts a ON a.id=p.account_id
 WHERE p.diagnostic_config IS NOT NULL AND p.enabled AND p.next_run_at<=NOW() AND a.deleted_at IS NULL
 ORDER BY p.next_run_at LIMIT 20 FOR UPDATE OF p SKIP LOCKED),
 moved AS (UPDATE scheduled_test_plans p SET next_run_at=NOW()+(p.diagnostic_config->>'interval_minutes')::integer*INTERVAL '1 minute'
 FROM due WHERE p.id=due.id RETURNING p.*),
 inserted AS (INSERT INTO scheduled_test_results(plan_id,status,started_at,finished_at,diagnostic_run)
 SELECT m.id,'queued',NULL,NULL,m.diagnostic_config || jsonb_build_object('plan_revision',m.diagnostic_config->'revision',
 'source','scheduled','api_key_name',COALESCE(k.name,''),'items','[]'::jsonb)
 FROM moved m LEFT JOIN api_keys k ON k.id=(m.diagnostic_config->>'api_key_id')::bigint
 ON CONFLICT(plan_id) WHERE diagnostic_run IS NOT NULL AND status IN ('queued','running') DO NOTHING RETURNING plan_id)
 SELECT p.account_id FROM inserted i JOIN scheduled_test_plans p ON p.id=i.plan_id`)
	if err != nil {
		return err
	}
	var ids []int64
	for queued.Next() {
		var id int64
		if err = queued.Scan(&id); err != nil {
			queued.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = queued.Err()
	queued.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, id, service.CodexDiagnosticHistoryLimit); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *scheduledTestPlanRepository) Claim(ctx context.Context, token string) (*service.CodexDiagnosticRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(26450001)").Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, nil
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_test_results WHERE diagnostic_run IS NOT NULL AND status='running' AND (diagnostic_run->>'lease_until')::timestamptz>NOW()").Scan(&count); err != nil {
		return nil, err
	}
	if count >= service.CodexDiagnosticParallelRuns {
		return nil, nil
	}
	run, err := scanDiagnosticRun(tx.QueryRowContext(ctx, `WITH claimed AS (
 UPDATE scheduled_test_results SET status='running',started_at=NOW(),
 diagnostic_run=diagnostic_run || jsonb_build_object('lease_until',NOW()+$2::integer*INTERVAL '1 minute','worker_token',$1::text)
 WHERE id=(SELECT id FROM scheduled_test_results WHERE diagnostic_run IS NOT NULL AND status='queued' ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
 RETURNING *) SELECT `+diagnosticRunColumns+" FROM claimed r JOIN scheduled_test_plans p ON p.id=r.plan_id", token, service.CodexDiagnosticLeaseMinutes))
	if errors.Is(err, service.ErrDiagnosticNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}
func (r *scheduledTestPlanRepository) SaveProgress(ctx context.Context, run *service.CodexDiagnosticRun) error {
	items, err := json.Marshal(run.Items)
	if err != nil {
		return err
	}
	if run.Items == nil {
		items = []byte("[]")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE scheduled_test_results SET
 diagnostic_run=diagnostic_run || jsonb_build_object('items',$3::jsonb,'api_key_name',$7::text),
 status=$4,error_message=$5,finished_at=$6
 WHERE id=$1 AND diagnostic_run->>'worker_token'=$2 AND status='running' AND (diagnostic_run->>'lease_until')::timestamptz>NOW()`, run.ID, run.WorkerToken, string(items), run.Status, run.Reason, run.FinishedAt, run.APIKeyName)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrDiagnosticNotFound
	}
	if _, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, run.AccountID, service.CodexDiagnosticHistoryLimit); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *scheduledTestPlanRepository) GetRun(ctx context.Context, account, id int64) (*service.CodexDiagnosticRun, error) {
	return scanDiagnosticRun(r.db.QueryRowContext(ctx, "SELECT "+diagnosticRunColumns+diagnosticRunJoin+"WHERE p.account_id=$1 AND r.id=$2 AND r.diagnostic_run IS NOT NULL", account, id))
}
func (r *scheduledTestPlanRepository) ListRuns(ctx context.Context, account int64) ([]service.CodexDiagnosticRun, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT "+diagnosticRunColumns+diagnosticRunJoin+"WHERE p.account_id=$1 AND r.diagnostic_run IS NOT NULL ORDER BY r.id DESC LIMIT $2", account, service.CodexDiagnosticHistoryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []service.CodexDiagnosticRun{}
	for rows.Next() {
		run, err := scanDiagnosticRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *run)
	}
	return runs, rows.Err()
}
func (r *scheduledTestPlanRepository) Cancel(ctx context.Context, account, id int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE scheduled_test_results r SET diagnostic_run=diagnostic_run || '{"cancel_requested":true}'::jsonb,
 status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,
 finished_at=CASE WHEN status='queued' THEN NOW() ELSE finished_at END
 WHERE r.id=$2 AND r.diagnostic_run IS NOT NULL AND r.status IN ('queued','running')
 AND EXISTS (SELECT 1 FROM scheduled_test_plans p WHERE p.id=r.plan_id AND p.account_id=$1)`, account, id)
	return err
}
func (r *scheduledTestPlanRepository) Summaries(ctx context.Context, ids []int64) (map[int64]service.CodexDiagnosticSummary, error) {
	result := map[int64]service.CodexDiagnosticSummary{}
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, len(ids))
	marks := make([]string, len(ids))
	for i, id := range ids {
		args[i] = id
		marks[i] = fmt.Sprintf("$%d", i+1)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.account_id,p.enabled,(p.diagnostic_config->>'interval_minutes')::integer,p.next_run_at,
 COALESCE(r.id,0),COALESCE(r.status,'unknown'),r.finished_at
 FROM scheduled_test_plans p LEFT JOIN LATERAL (
 SELECT id,status,finished_at FROM scheduled_test_results WHERE plan_id=p.id
 AND diagnostic_run->'plan_revision'=p.diagnostic_config->'revision' ORDER BY id DESC LIMIT 1) r ON TRUE
 WHERE p.diagnostic_config IS NOT NULL AND p.account_id IN (`+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s service.CodexDiagnosticSummary
		if err := rows.Scan(&id, &s.Enabled, &s.IntervalMinutes, &s.NextRunAt, &s.RunID, &s.Status, &s.CheckedAt); err != nil {
			return nil, err
		}
		s.Stale = service.DiagnosticResultStale(s.CheckedAt, s.IntervalMinutes, time.Now())
		result[id] = s
	}
	return result, rows.Err()
}
