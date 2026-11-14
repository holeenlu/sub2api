package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.AccountExcelBPSCooldownRepository = (*accountRepository)(nil)

// ExtendExcelBPSCooldown reuses model_rate_limits and its account outbox. The
// conditional update serializes competing writers on the account row, so a
// shorter or delayed 429 cannot replace a later not-before from another instance.
// No protocol switch, native quota, grant or recovery marker is modified.
func (r *accountRepository) ExtendExcelBPSCooldown(ctx context.Context, accountID int64, scope string, until time.Time) (time.Time, error) {
	if !strings.HasPrefix(scope, service.ExcelBPSCooldownScopePrefix) || until.IsZero() {
		return time.Time{}, fmt.Errorf("invalid BPS cooldown")
	}
	if dbent.TxFromContext(ctx) != nil {
		saved, _, err := r.extendExcelBPSCooldownInTx(ctx, accountID, scope, until)
		return saved, err
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		saved, _, err := r.extendExcelBPSCooldownInTx(ctx, accountID, scope, until)
		return saved, err
	}
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = tx.Rollback() }()
	saved, changed, err := r.extendExcelBPSCooldownInTx(dbent.NewTxContext(ctx, tx), accountID, scope, until)
	if err != nil {
		return time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		// The caller must read back after an ambiguous commit; no success receipt.
		return time.Time{}, err
	}
	if changed {
		r.syncSchedulerAccountSnapshot(ctx, accountID)
	}
	return saved, nil
}

func (r *accountRepository) extendExcelBPSCooldownInTx(ctx context.Context, accountID int64, scope string, until time.Time) (time.Time, bool, error) {
	if err := r.guardAccountScope(ctx, accountID); err != nil {
		return time.Time{}, false, err
	}
	raw, err := json.Marshal(map[string]string{
		"rate_limited_at":     time.Now().UTC().Format(time.RFC3339Nano),
		"rate_limit_reset_at": until.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return time.Time{}, false, err
	}
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, `
UPDATE accounts SET
 extra = jsonb_set(
  jsonb_set(COALESCE(extra, '{}'::jsonb), '{model_rate_limits}'::text[],
   COALESCE(extra->'model_rate_limits', '{}'::jsonb), true),
  ARRAY['model_rate_limits', $1]::text[], $2::jsonb, true),
 updated_at = NOW()
WHERE id = $3 AND deleted_at IS NULL AND COALESCE(extra->>'vendor_archived_at','')=''
 AND platform = 'openai' AND type = 'oauth' AND parent_account_id IS NULL
 AND COALESCE(NULLIF(extra->'model_rate_limits'->$1->>'rate_limit_reset_at', '')::timestamptz < $4::timestamptz, TRUE)`,
		scope, raw, accountID, until)
	if err != nil {
		return time.Time{}, false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return time.Time{}, false, err
	}
	if affected == 0 {
		saved, err := r.GetExcelBPSCooldown(ctx, accountID, []string{scope})
		return saved, false, err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return time.Time{}, false, err
	}
	return until, true, nil
}

// GetExcelBPSCooldown reads only the requested BPS scopes, directly from the
// existing authoritative account state. Cache/snapshot misses cannot grant an
// early retry. Missing accounts and malformed persistent timestamps are errors.
func (r *accountRepository) GetExcelBPSCooldown(ctx context.Context, accountID int64, scopes []string) (time.Time, error) {
	if len(scopes) == 0 || len(scopes) > 4 {
		return time.Time{}, fmt.Errorf("invalid BPS cooldown scopes")
	}
	for _, scope := range scopes {
		if !strings.HasPrefix(scope, service.ExcelBPSCooldownScopePrefix) {
			return time.Time{}, fmt.Errorf("invalid BPS cooldown scope")
		}
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
SELECT (SELECT MAX(NULLIF(a.extra->'model_rate_limits'->requested.scope->>'rate_limit_reset_at', '')::timestamptz)
 FROM unnest($2::text[]) AS requested(scope))
FROM accounts a
WHERE a.id=$1 AND a.deleted_at IS NULL AND COALESCE(a.extra->>'vendor_archived_at','')=''
 AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL
 AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`)`, accountID, pq.Array(scopes))
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return time.Time{}, err
		}
		return time.Time{}, service.ErrAccountNotFound
	}
	var until sql.NullTime
	if err := rows.Scan(&until); err != nil {
		return time.Time{}, err
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, err
	}
	return until.Time, nil
}
