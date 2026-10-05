package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type codexDiagnosticRepository struct{ db *sql.DB }

const pruneDiagnosticRunsSQL = `DELETE FROM codex_diagnostic_runs WHERE account_id=$1
 AND status NOT IN ('queued','running') AND id NOT IN (
 SELECT id FROM codex_diagnostic_runs WHERE account_id=$1 ORDER BY id DESC LIMIT 10)`

func NewCodexDiagnosticRepository(db *sql.DB) service.CodexDiagnosticRepository {
	return &codexDiagnosticRepository{db}
}

type diagnosticScanner interface{ Scan(...any) error }

const diagnosticPlanColumns = "account_id,owner_id,COALESCE(api_key_id,0),models,enabled,interval_minutes,revision,next_run_at,updated_at"

func scanDiagnosticPlan(row diagnosticScanner) (*service.CodexDiagnosticPlan, error) {
	p := &service.CodexDiagnosticPlan{}
	var models []byte
	err := row.Scan(&p.AccountID, &p.OwnerID, &p.APIKeyID, &models, &p.Enabled, &p.IntervalMinutes, &p.Revision, &p.NextRunAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(models, &p.Models)
	return p, err
}
func (r *codexDiagnosticRepository) GetPlan(ctx context.Context, id int64) (*service.CodexDiagnosticPlan, error) {
	return scanDiagnosticPlan(r.db.QueryRowContext(ctx, "SELECT "+diagnosticPlanColumns+" FROM codex_diagnostic_plans WHERE account_id=$1", id))
}
func (r *codexDiagnosticRepository) SavePlan(ctx context.Context, p *service.CodexDiagnosticPlan) error {
	if p.IntervalMinutes == 0 {
		p.IntervalMinutes = service.CodexDiagnosticDefaultIntervalMinutes
	}
	models, err := json.Marshal(p.Models)
	if err != nil {
		return err
	}
	if p.Models == nil {
		models = []byte("[]")
	}
	// No-op saves preserve the due time and last conclusion. A changed configuration
	// invalidates old run ownership. Timer changes recalculate the next deadline.
	return r.db.QueryRowContext(ctx, `
 INSERT INTO codex_diagnostic_plans(account_id,owner_id,api_key_id,models,enabled,interval_minutes,next_run_at)
 VALUES($1,$2,NULLIF($3,0),$4::jsonb,$5,$6::integer,CASE WHEN $5 THEN NOW()+$6::integer * INTERVAL '1 minute' END)
 ON CONFLICT(account_id) DO UPDATE SET owner_id=EXCLUDED.owner_id,api_key_id=EXCLUDED.api_key_id,
 models=EXCLUDED.models,enabled=EXCLUDED.enabled,interval_minutes=EXCLUDED.interval_minutes,
 revision=codex_diagnostic_plans.revision+CASE WHEN (codex_diagnostic_plans.owner_id,codex_diagnostic_plans.api_key_id,codex_diagnostic_plans.models)
 IS DISTINCT FROM (EXCLUDED.owner_id,EXCLUDED.api_key_id,EXCLUDED.models) THEN 1 ELSE 0 END,
 next_run_at=CASE WHEN NOT EXCLUDED.enabled THEN NULL
 WHEN (codex_diagnostic_plans.owner_id,codex_diagnostic_plans.api_key_id,codex_diagnostic_plans.models,codex_diagnostic_plans.enabled,codex_diagnostic_plans.interval_minutes)
 IS DISTINCT FROM (EXCLUDED.owner_id,EXCLUDED.api_key_id,EXCLUDED.models,EXCLUDED.enabled,EXCLUDED.interval_minutes) THEN NOW()+EXCLUDED.interval_minutes * INTERVAL '1 minute'
 ELSE codex_diagnostic_plans.next_run_at END,updated_at=NOW()
 RETURNING revision,next_run_at,updated_at`, p.AccountID, p.OwnerID, p.APIKeyID, string(models), p.Enabled, p.IntervalMinutes).Scan(&p.Revision, &p.NextRunAt, &p.UpdatedAt)
}

const diagnosticRunColumns = "id,account_id,owner_id,COALESCE(api_key_id,0),api_key_name,plan_revision,models,source,status,reason,items,created_at,started_at,finished_at,COALESCE(worker_token,''),cancel_requested"

func scanDiagnosticRun(row diagnosticScanner) (*service.CodexDiagnosticRun, error) {
	r := &service.CodexDiagnosticRun{}
	var models, items []byte
	err := row.Scan(&r.ID, &r.AccountID, &r.OwnerID, &r.APIKeyID, &r.APIKeyName, &r.PlanRevision, &models, &r.Source, &r.Status, &r.Reason, &items, &r.CreatedAt, &r.StartedAt, &r.FinishedAt, &r.WorkerToken, &r.CancelRequested)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrDiagnosticNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(models, &r.Models); err != nil {
		return nil, err
	}
	err = json.Unmarshal(items, &r.Items)
	return r, err
}
func (r *codexDiagnosticRepository) Enqueue(ctx context.Context, p *service.CodexDiagnosticPlan, source, keyName string) (*service.CodexDiagnosticRun, error) {
	models, _ := json.Marshal(p.Models)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Insert from the current plan, preventing a save racing with Run Now from
	// scheduling a superseded paid request.
	run, err := scanDiagnosticRun(tx.QueryRowContext(ctx, `
 INSERT INTO codex_diagnostic_runs(account_id,owner_id,api_key_id,api_key_name,plan_revision,models,source,status)
 SELECT account_id,owner_id,api_key_id,$6,revision,models,$5,'queued' FROM codex_diagnostic_plans
 WHERE account_id=$1 AND owner_id=$2 AND api_key_id=$3 AND models=$4::jsonb AND revision=$7
 ON CONFLICT(account_id) WHERE status IN ('queued','running') DO NOTHING
 RETURNING `+diagnosticRunColumns, p.AccountID, p.OwnerID, p.APIKeyID, string(models), source, keyName, p.Revision))
	if errors.Is(err, service.ErrDiagnosticNotFound) {
		return nil, service.ErrDiagnosticBusy
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, p.AccountID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}
func (r *codexDiagnosticRepository) EnqueueDue(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// A crashed/expired worker is recorded as failed, never silently replayed.
	expired, err := tx.QueryContext(ctx, `UPDATE codex_diagnostic_runs SET status='failed',reason='worker_interrupted',finished_at=NOW()
 WHERE status='running' AND lease_until<NOW() RETURNING account_id`)
	if err != nil {
		return err
	}
	dirty := map[int64]bool{}
	for expired.Next() {
		var id int64
		if err = expired.Scan(&id); err != nil {
			expired.Close()
			return err
		}
		dirty[id] = true
	}
	err = expired.Err()
	expired.Close()
	if err != nil {
		return err
	}
	queued, err := tx.QueryContext(ctx, `
 WITH due AS (
 SELECT p.account_id FROM codex_diagnostic_plans p JOIN accounts a ON a.id=p.account_id
 WHERE p.enabled AND p.next_run_at<=NOW() AND a.deleted_at IS NULL
 ORDER BY p.next_run_at LIMIT 20 FOR UPDATE OF p SKIP LOCKED
 ), moved AS (
 UPDATE codex_diagnostic_plans p SET next_run_at=NOW()+p.interval_minutes * INTERVAL '1 minute' FROM due
 WHERE p.account_id=due.account_id RETURNING p.*
 )
 INSERT INTO codex_diagnostic_runs(account_id,owner_id,api_key_id,api_key_name,plan_revision,models,source,status)
 SELECT m.account_id,m.owner_id,m.api_key_id,COALESCE(k.name,''),m.revision,m.models,'scheduled','queued'
 FROM moved m LEFT JOIN api_keys k ON k.id=m.api_key_id
 ON CONFLICT(account_id) WHERE status IN ('queued','running') DO NOTHING RETURNING account_id`)
	if err != nil {
		return err
	}
	for queued.Next() {
		var id int64
		if err = queued.Scan(&id); err != nil {
			queued.Close()
			return err
		}
		dirty[id] = true
	}
	err = queued.Err()
	queued.Close()
	if err != nil {
		return err
	}
	for id := range dirty {
		if _, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (r *codexDiagnosticRepository) Claim(ctx context.Context, token string) (*service.CodexDiagnosticRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Global two-run budget, including other replicas. The short transaction lock
	// is released before any billed network request.
	var locked bool
	if err = tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(26450001)").Scan(&locked); err != nil {
		return nil, err
	}
	if !locked {
		return nil, nil
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM codex_diagnostic_runs WHERE status='running' AND lease_until>NOW()").Scan(&count); err != nil {
		return nil, err
	}
	if count >= 2 {
		return nil, nil
	}
	run, err := scanDiagnosticRun(tx.QueryRowContext(ctx, `
 UPDATE codex_diagnostic_runs SET status='running',started_at=NOW(),lease_until=NOW()+INTERVAL '40 minutes',worker_token=$1
 WHERE id=(SELECT id FROM codex_diagnostic_runs WHERE status='queued' ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
 RETURNING `+diagnosticRunColumns, token))
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

func (r *codexDiagnosticRepository) SaveProgress(ctx context.Context, run *service.CodexDiagnosticRun) error {
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
	result, err := tx.ExecContext(ctx, `UPDATE codex_diagnostic_runs SET items=$3::jsonb,status=$4,reason=$5,finished_at=$6,api_key_name=$7
 WHERE id=$1 AND worker_token=$2 AND status='running' AND lease_until>NOW()`, run.ID, run.WorkerToken, string(items), run.Status, run.Reason, run.FinishedAt, run.APIKeyName)
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
	// Keep only ten runs per account, including any currently active run.
	// Never delete work that another worker is still executing.
	_, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, run.AccountID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *codexDiagnosticRepository) GetRun(ctx context.Context, account, id int64) (*service.CodexDiagnosticRun, error) {
	return scanDiagnosticRun(r.db.QueryRowContext(ctx, "SELECT "+diagnosticRunColumns+" FROM codex_diagnostic_runs WHERE account_id=$1 AND id=$2", account, id))
}
func (r *codexDiagnosticRepository) ListRuns(ctx context.Context, account, before int64, limit int) ([]service.CodexDiagnosticRun, error) {
	if limit < 1 || limit > 10 {
		limit = 10
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+diagnosticRunColumns+" FROM (SELECT * FROM codex_diagnostic_runs WHERE account_id=$1 ORDER BY id DESC LIMIT 10) recent WHERE ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3", account, before, limit)
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
func (r *codexDiagnosticRepository) Cancel(ctx context.Context, account, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE codex_diagnostic_runs SET cancel_requested=true,
 status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,
 finished_at=CASE WHEN status='queued' THEN NOW() ELSE finished_at END
 WHERE account_id=$1 AND id=$2 AND status IN ('queued','running')`, account, id)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, pruneDiagnosticRunsSQL, account); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *codexDiagnosticRepository) Summaries(ctx context.Context, ids []int64) (map[int64]service.CodexDiagnosticSummary, error) {
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
	rows, err := r.db.QueryContext(ctx, `SELECT p.account_id,p.enabled,p.interval_minutes,p.next_run_at,COALESCE(r.id,0),COALESCE(r.status,'unknown'),r.finished_at
 FROM codex_diagnostic_plans p LEFT JOIN LATERAL (
 SELECT id,status,finished_at FROM codex_diagnostic_runs WHERE account_id=p.account_id AND plan_revision=p.revision
 ORDER BY id DESC LIMIT 1) r ON TRUE WHERE p.account_id IN (`+strings.Join(marks, ",")+")", args...)
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
		result[id] = s
	}
	return result, rows.Err()
}
