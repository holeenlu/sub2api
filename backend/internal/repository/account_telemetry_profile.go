package repository

import (
	"context"
	"encoding/json"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) ListAccountTelemetry(ctx context.Context, after int64, limit int, pendingOnly bool) ([]*service.Account, error) {
	query := r.scopedAccountQuery(ctx).Where(dbaccount.IDGT(after), dbaccount.PlatformEQ(service.PlatformAnthropic), dbaccount.StatusEQ(service.StatusActive), dbaccount.TypeIn(service.AccountTypeOAuth, service.AccountTypeSetupToken)).Order(dbaccount.ByID()).Limit(limit)
	query.Where(func(s *entsql.Selector) {
		s.Where(sqljson.HasKey(dbaccount.FieldExtra, sqljson.Path(service.AccountTelemetryExtraKey)))
		if pendingOnly {
			s.Where(sqljson.ValueNEQ(dbaccount.FieldExtra, "ready", sqljson.Path(service.AccountTelemetryExtraKey, "status")))
		}
	})
	rows, err := query.All(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := r.accountsToService(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]*service.Account, 0, len(accounts))
	for i := range accounts {
		out = append(out, &accounts[i])
	}
	return out, nil
}

// Revision and credentials guard both manual regeneration and re-imports. The
// account snapshot publication uses the existing transactional scheduler outbox.
func (r *accountRepository) SaveAccountTelemetry(ctx context.Context, id int64, credentials, expected string, state service.AccountTelemetryState) (bool, error) {
	extra := map[string]any{service.AccountTelemetryExtraKey: state}
	if err := service.ValidateAccountTelemetry(extra); err != nil {
		return false, err
	}
	payload, err := json.Marshal(extra)
	if err != nil {
		return false, err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	txCtx := dbent.NewTxContext(ctx, tx)
	result, err := tx.Client().ExecContext(txCtx, `UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) || $1::jsonb, updated_at=NOW()
 WHERE id=$2 AND credentials=$3::jsonb AND COALESCE(extra->'account_telemetry'->>'revision','')=$4
 AND platform='anthropic' AND type IN ('oauth','setup-token') AND status='active'
 AND deleted_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) AND `+accountNotArchivedSQL, string(payload), id, credentials, expected)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if err := enqueueSchedulerOutbox(txCtx, tx.Client(), service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return true, nil
}

var _ service.AccountTelemetryRepository = (*accountRepository)(nil)
