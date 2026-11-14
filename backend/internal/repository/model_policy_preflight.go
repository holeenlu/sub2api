package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Wei-Shaw/sub2api/migrations"
)

// CheckModelPolicyMigration runs the shipped migration against session-local
// copies. No application rows, triggers, outbox events or migration records are
// written. The migration remains the single owner of conversion rules.
func CheckModelPolicyMigration(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return checkModelPolicyMigration(ctx, tx)
}

func checkModelPolicyMigration(ctx context.Context, tx *sql.Tx) error {
	var accountsExist bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('public.accounts') IS NOT NULL`).Scan(&accountsExist); err != nil {
		return err
	}
	if !accountsExist {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
CREATE TEMP TABLE accounts ON COMMIT DROP AS
SELECT id, platform, type, parent_account_id, credentials, extra, updated_at
FROM public.accounts WHERE extra ? 'model_catalog_policy';
CREATE TEMP TABLE scheduler_outbox(event_type TEXT, account_id BIGINT) ON COMMIT DROP;
SET LOCAL search_path = pg_temp, public;`)
	if err != nil {
		return fmt.Errorf("prepare model policy check: %w", err)
	}
	body, err := migrations.FS.ReadFile("263_model_catalog_candidates_only.sql")
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("model policy migration preflight failed: %w", err)
	}
	return nil
}
