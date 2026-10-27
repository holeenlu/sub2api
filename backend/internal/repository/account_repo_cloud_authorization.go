package repository

import (
	"context"
	"encoding/json"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.OpenAICloudAuthorizationRepository = (*accountRepository)(nil)

func (r *accountRepository) GetCloudAuthorization(ctx context.Context, id int64) (*service.OpenAICloudAuthorizationBinding, error) {
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT g.generation,g.workspace_id,g.user_id,g.subject,g.revoked,g.revision,g.updated_at FROM openai_cloud_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	b := new(service.OpenAICloudAuthorizationBinding)
	if err := rows.Scan(&b.Generation, &b.AccountID, &b.UserID, &b.Subject, &b.Revoked, &b.Revision, &b.UpdatedAt); err != nil {
		return nil, err
	}
	return b, rows.Err()
}

// Account then binding locks match every Cloud mutation. Native refresh CAS is
// checked against the complete native credential snapshot before binding an
// execution identity. Identity replacement fences all older Cloud sessions.
func (r *accountRepository) StoreCloudAuthorization(ctx context.Context, a *service.Account, previous string, next *service.OpenAICloudAuthorizationBinding) (bool, error) {
	if !a.IsOpenAICloudEligible() || next == nil || next.Generation == "" || next.AccountID != a.GetChatGPTAccountID() || next.UserID != a.GetChatGPTUserID() {
		return false, service.ErrOpenAICloudAuthorization
	}
	if err := r.guardAccountScope(ctx, a.ID); err != nil {
		return false, err
	}
	primary, err := json.Marshal(a.Credentials)
	if err != nil {
		return false, err
	}
	extra, err := json.Marshal(a.Extra)
	if err != nil {
		return false, err
	}
	baseCtx := ctx
	client := clientFromContext(ctx, r.client)
	var tx *dbent.Tx
	if dbent.TxFromContext(ctx) == nil {
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return false, err
		}
		defer func() { _ = tx.Rollback() }()
		ctx = dbent.NewTxContext(ctx, tx)
		client = tx.Client()
	}
	if err := authz.LockManagementWrite(ctx, client, nil, "", "", false); err != nil {
		return false, err
	}
	rows, err := client.QueryContext(ctx, `SELECT id FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND a.platform='openai' AND a.type='oauth' AND a.credentials=$2::jsonb AND a.proxy_id IS NOT DISTINCT FROM $3::bigint AND COALESCE(a.extra->'openai_cloud','false'::jsonb)=COALESCE($4::jsonb->'openai_cloud','false'::jsonb) AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`) FOR UPDATE`, a.ID, string(primary), a.ProxyID, string(extra))
	if err != nil {
		return false, err
	}
	exists := rows.Next()
	err = rows.Err()
	_ = rows.Close()
	if err != nil || !exists {
		return false, err
	}
	rows, err = client.QueryContext(ctx, `SELECT generation,revision FROM openai_cloud_grants WHERE account_id=$1 FOR UPDATE`, a.ID)
	if err != nil {
		return false, err
	}
	var generation string
	var revision int64
	if rows.Next() {
		err = rows.Scan(&generation, &revision)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	if generation != previous || revision != next.Revision {
		return false, nil
	}
	// Revocation fences native identity changes; usage never changes the saved switch.
	if next.Revoked {
		if _, err := client.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,true),updated_at=NOW() WHERE id=$1`, a.ID); err != nil {
			return false, mapCloudSettingsError(err)
		}
	}
	_, err = client.ExecContext(ctx, `INSERT INTO openai_cloud_grants(account_id,generation,workspace_id,user_id,subject,revoked,revision) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(account_id) DO UPDATE SET generation=EXCLUDED.generation,workspace_id=EXCLUDED.workspace_id,user_id=EXCLUDED.user_id,subject=EXCLUDED.subject,revoked=EXCLUDED.revoked,revision=EXCLUDED.revision,updated_at=NOW()`, a.ID, next.Generation, next.AccountID, next.UserID, next.Subject, next.Revoked, revision+1)
	if err != nil {
		return false, err
	}
	if err := authz.Revalidate(ctx); err != nil {
		return false, err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &a.ID, nil, nil); err != nil {
		return false, err
	}
	if tx != nil {
		if err := tx.Commit(); err != nil {
			return false, err
		}
	}
	r.syncSchedulerAccountSnapshot(baseCtx, a.ID)
	return true, nil
}

// The database trigger is the single settings admission rule, including raw
// JSON patches, imports and bulk writes. Keep its error safe and actionable.
func mapCloudSettingsError(err error) error {
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23514" && pg.Constraint == "accounts_openai_cloud_settings" {
		return service.ErrOpenAICloudAccountSettings
	}
	return err
}
