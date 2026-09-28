package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *scheduledTestPlanRepository) ListQualityPlans(ctx context.Context) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT p.id, p.account_id, p.model_id, p.cron_expression, p.enabled, p.max_results, p.auto_recover, p.last_run_at, p.next_run_at, p.created_at, p.updated_at, p.pelican_config, p.running_until, a.name
 FROM scheduled_test_plans p JOIN accounts a ON a.id=p.account_id
 WHERE p.pelican_config->'quality'->>'action'='enable_bps' AND p.pelican_config->>'question_kind'='state_probe' AND a.deleted_at IS NULL ORDER BY p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows, true)
}
func (r *scheduledTestPlanRepository) TriggerQuality(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE scheduled_test_plans SET next_run_at=NOW(), updated_at=NOW()
 WHERE id=$1 AND enabled AND pelican_config->'quality'->>'action'='enable_bps' AND pelican_config->>'question_kind'='state_probe' AND (running_until IS NULL OR running_until<NOW())`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("plan must be enabled and idle")
	}
	return nil
}

type qualityState struct {
	Action         string    `json:"action"`
	AccountVersion time.Time `json:"account_version"`
	// 以下只用于「降智开 BPS」：连续降智轮数、开启后连续满血轮数，以及开启前 / 开启时 BPS 相关 Extra 的快照。
	FailureStreak int                        `json:"failure_streak,omitempty"`
	PassStreak    int                        `json:"pass_streak,omitempty"`
	BPSPrevious   map[string]json.RawMessage `json:"bps_previous,omitempty"`
	BPSApplied    map[string]json.RawMessage `json:"bps_applied,omitempty"`
}

// Lease/version checks, account mutation, ownership and scheduler invalidation
// commit together. A paused, edited, deleted or expired run cannot change accounts.
func (r *scheduledTestPlanRepository) ApplyQualityOutcome(ctx context.Context, plan *service.ScheduledTestPlan, until time.Time, outcome string) (string, error) {
	if plan == nil || plan.PelicanConfig == nil || plan.PelicanConfig.Quality == nil || plan.PelicanConfig.Quality.Action != service.QualityActionEnableBPS || plan.PelicanConfig.QuestionKind != service.OpenAICodexStateProbeQuestionKind {
		return "unsupported_action", nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT enabled AND updated_at=$2 AND running_until=$3 AND running_until>NOW()
 FROM scheduled_test_plans WHERE id=$1 FOR UPDATE`, plan.ID, plan.UpdatedAt, until).Scan(&valid)
	if err == sql.ErrNoRows || (err == nil && !valid) {
		return "stale_run", nil
	}
	if err != nil {
		return "", err
	}
	var version time.Time
	var schedulable bool
	var status string
	err = tx.QueryRowContext(ctx, `SELECT updated_at, schedulable, status FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, plan.AccountID).Scan(&version, &schedulable, &status)
	if err == sql.ErrNoRows {
		return "account_deleted", nil
	}
	if err != nil {
		return "", err
	}
	if outcome != "passed" && outcome != "failed" {
		return "inconclusive", nil
	}
	if status != "active" || !schedulable {
		return "account_ineligible", nil
	}
	if plan.BPSAccountVersion != nil && !plan.BPSAccountVersion.Equal(version) {
		return "account_changed", nil
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT state FROM account_quality_states WHERE plan_id=$1`, plan.ID).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	var state qualityState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &state); err != nil {
			return "", err
		}
	}
	q := plan.PelicanConfig.Quality
	// Changing a rule's action does not discard its previous ownership. While
	// holding the account lock, keep another rule from taking over that scope
	// until the previous owner's snapshot has been restored.
	if state.Action == "" {
		var owned bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM account_quality_states s JOIN scheduled_test_plans p ON p.id=s.plan_id
 WHERE p.account_id=$1 AND p.id<>$2 AND COALESCE(s.state->>'action','')<>''
 AND (s.state->>'action'='enable_bps')=$3)`, plan.AccountID, plan.ID, q.Action == service.QualityActionEnableBPS).Scan(&owned)
		if err != nil {
			return "", err
		}
		if owned {
			return "action_conflict", nil
		}
	}
	// 已被本规则开过 BPS 的账号即便规则后来改了动作，也由 BPS 分支负责恢复。
	if state.Action == service.QualityActionEnableBPS || (state.Action == "" && q.Action == service.QualityActionEnableBPS) {
		action, err := applyQualityBPSOutcome(ctx, tx, plan, outcome, status, state, len(raw) > 0)
		if err != nil {
			return "", err
		}
		if err = tx.Commit(); err != nil {
			return "", err
		}
		return action, nil
	}
	return "unsupported_action", nil
}

// Global operation history is cursor-paginated independently of account/rule selection.
// Response bodies are loaded through the existing per-result detail endpoint.

func (r *scheduledTestResultRepository) ListQualityHistory(ctx context.Context, beforeID int64, limit int) ([]*service.QualityHistoryResult, error) {
	rows, err := r.db.QueryContext(ctx, `WITH rounds AS (
 SELECT r.*, a.id AS account_id,a.name AS account_name,
 row_number() OVER round_window AS row_in_round,
 count(*) FILTER (WHERE r.status='success') OVER round_window AS passed_count,
 GREATEST(count(*) OVER round_window,COALESCE((r.pelican_config->>'parallel_count')::int,0)) AS total_count,
 array_agg(r.id) OVER round_window AS result_ids,
 min(r.started_at) OVER round_window AS round_started_at,
 max(r.finished_at) OVER round_window AS round_finished_at,
 bool_and(r.status='success') OVER round_window AS all_passed,
 bool_or(r.error_message='answer_mismatch') OVER round_window AS any_wrong
 FROM scheduled_test_results r JOIN scheduled_test_plans p ON p.id=r.plan_id JOIN accounts a ON a.id=p.account_id
 WHERE r.pelican_config->'quality'->>'action'='enable_bps' AND r.pelican_config->>'question_kind'='state_probe' AND a.deleted_at IS NULL
 WINDOW round_window AS (PARTITION BY r.plan_id,COALESCE(NULLIF(r.quality_round_id,''),r.id::text) ORDER BY r.id DESC ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING)
 ) SELECT id,plan_id,CASE WHEN all_passed THEN 'success' ELSE 'failed' END,
 CASE WHEN any_wrong THEN 'answer_mismatch' ELSE error_message END,
 latency_ms,round_started_at,round_finished_at,created_at,pelican_config,quality_action,quality_judgment,account_id,account_name,passed_count,total_count,result_ids
 FROM rounds WHERE row_in_round=1 AND ($1::bigint=0 OR id<$1) ORDER BY id DESC LIMIT $2`, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]*service.QualityHistoryResult, 0)
	for rows.Next() {
		item := &service.QualityHistoryResult{}
		var cfg, judgment []byte
		if err := rows.Scan(&item.ID, &item.PlanID, &item.Status, &item.ErrorMessage, &item.LatencyMs, &item.StartedAt, &item.FinishedAt, &item.CreatedAt, &cfg, &item.QualityAction, &judgment, &item.AccountID, &item.AccountName, &item.PassedCount, &item.TotalCount, pq.Array(&item.ResultIDs)); err != nil {
			return nil, err
		}
		if len(cfg) > 0 {
			if err := json.Unmarshal(cfg, &item.PelicanConfig); err != nil {
				return nil, err
			}
		}
		if len(judgment) > 0 {
			if err := json.Unmarshal(judgment, &item.QualityJudgment); err != nil {
				return nil, err
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
