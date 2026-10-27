package repository

import (
	"context"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountBPSCooldownRecoveryRepository = (*accountRepository)(nil)

// ClearModelRateLimitsPreservingBPS is the successful-test recovery write. It
// filters the current locked row, not a previously read Account.Extra snapshot,
// so a concurrent BPS extension cannot be lost. Explicit administrative clears
// continue to use ClearModelRateLimits.
func (r *accountRepository) ClearModelRateLimitsPreservingBPS(ctx context.Context, accountID int64) error {
	if dbent.TxFromContext(ctx) != nil {
		return r.clearModelRateLimitsPreservingBPSInTx(ctx, accountID)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.clearModelRateLimitsPreservingBPSInTx(ctx, accountID)
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.clearModelRateLimitsPreservingBPSInTx(dbent.NewTxContext(ctx, tx), accountID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.syncSchedulerAccountSnapshot(ctx, accountID)
	return nil
}

func (r *accountRepository) clearModelRateLimitsPreservingBPSInTx(ctx context.Context, accountID int64) error {
	if err := r.guardAccountScope(ctx, accountID); err != nil {
		return err
	}
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(ctx, `
UPDATE accounts AS a SET
 extra = (COALESCE(a.extra, '{}'::jsonb) - 'model_rate_limits') || COALESCE(
  (SELECT jsonb_build_object('model_rate_limits', jsonb_object_agg(entry.key, entry.value))
   FROM jsonb_each(CASE WHEN jsonb_typeof(a.extra->'model_rate_limits') = 'object'
    THEN a.extra->'model_rate_limits' ELSE '{}'::jsonb END) AS entry
   WHERE left(entry.key, length($2)) = $2
   HAVING COUNT(*) > 0), '{}'::jsonb),
 updated_at = NOW()
WHERE a.id = $1 AND a.deleted_at IS NULL AND COALESCE(a.extra->>'vendor_archived_at', '') = ''`,
		accountID, service.ExcelBPSCooldownScopePrefix)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	return enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil)
}
