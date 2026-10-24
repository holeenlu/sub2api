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

func (r *accountRepository) RecordCloudReconciliation(ctx context.Context, actorID int64, next *service.OpenAICloudSettlement, evidence, reason string) error {
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || actor.Role != authz.SuperAdmin || actor.UserID != actorID {
		return authz.ErrPermissionDenied
	}
	if next == nil || next.Unknown || next.Probe {
		return errors.New("invalid Cloud reconciliation")
	}
	if err := r.guardAccountScope(ctx, next.AccountID); err != nil {
		return err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := authz.LockManagementWrite(txCtx, tx.Client(), nil, "", "", false); err != nil {
		return err
	}
	rows, err := tx.Client().QueryContext(txCtx, `INSERT INTO openai_cloud_reconciliations(root_id,account_id,actor_id,evidence,reason,settlement)
	VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT(root_id) DO UPDATE SET root_id=EXCLUDED.root_id
	WHERE openai_cloud_reconciliations.account_id=EXCLUDED.account_id AND openai_cloud_reconciliations.settlement=EXCLUDED.settlement
	  AND openai_cloud_reconciliations.evidence=EXCLUDED.evidence AND openai_cloud_reconciliations.reason=EXCLUDED.reason
	RETURNING root_id`, next.RootRequestID, next.AccountID, actorID, evidence, reason, string(raw))
	if err != nil {
		return err
	}
	saved := rows.Next()
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if !saved {
		return errors.New("Cloud reconciliation conflicts with an existing audit record")
	}
	if err = authz.Revalidate(txCtx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *accountRepository) ListCloudRuntimeBlockedAccounts(ctx context.Context) ([]int64, error) {
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT g.account_id FROM openai_cloud_grants g WHERE g.runtime_blocked OR EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks b LEFT JOIN openai_cloud_reconciliations r USING(root_id) WHERE b.account_id=g.account_id AND r.settled_at IS NULL) ORDER BY g.account_id LIMIT 256`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *accountRepository) GetCloudAuthorization(ctx context.Context, id int64) (*service.OpenAICloudAuthorizationBinding, error) {
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT g.generation,g.workspace_id,g.user_id,g.subject,g.revoked,(g.runtime_blocked OR EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks b LEFT JOIN openai_cloud_reconciliations r USING(root_id) WHERE b.account_id=g.account_id AND r.settled_at IS NULL)),g.revision,g.updated_at FROM openai_cloud_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`)`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	b := new(service.OpenAICloudAuthorizationBinding)
	if err := rows.Scan(&b.Generation, &b.AccountID, &b.UserID, &b.Subject, &b.Revoked, &b.RuntimeBlocked, &b.Revision, &b.UpdatedAt); err != nil {
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
	rows, err = client.QueryContext(ctx, `SELECT EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks b LEFT JOIN openai_cloud_reconciliations r USING(root_id) WHERE b.account_id=$1 AND r.settled_at IS NULL)`, a.ID)
	if err != nil {
		return false, err
	}
	var unresolved bool
	if rows.Next() {
		err = rows.Scan(&unresolved)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	if unresolved {
		next.RuntimeBlocked = true
	}
	// An unresolved bill or explicit revocation can stop Cloud. Establishing a
	// verified native binding must preserve the operator's saved switch and BPS.
	if next.Revoked || next.RuntimeBlocked {
		if _, err := client.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,true),updated_at=NOW() WHERE id=$1`, a.ID); err != nil {
			return false, mapCloudSettingsError(err)
		}
	}
	_, err = client.ExecContext(ctx, `INSERT INTO openai_cloud_grants(account_id,generation,workspace_id,user_id,subject,revoked,revision,runtime_blocked) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(account_id) DO UPDATE SET generation=EXCLUDED.generation,workspace_id=EXCLUDED.workspace_id,user_id=EXCLUDED.user_id,subject=EXCLUDED.subject,revoked=EXCLUDED.revoked,runtime_blocked=EXCLUDED.runtime_blocked,revision=EXCLUDED.revision,updated_at=NOW()`, a.ID, next.Generation, next.AccountID, next.UserID, next.Subject, next.Revoked, revision+1, next.RuntimeBlocked)
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
		return service.ErrOpenAICloudSettings
	}
	return err
}

func (r *accountRepository) RecordCloudRuntimeBlock(ctx context.Context, root string, accountID int64) error {
	// Lock the account before its grant, matching authorization replacement.
	// The account-wide block and disabled scheduling commit together even when
	// the bill belongs to an older grant or the current grant has been revoked.
	baseCtx := ctx
	client := clientFromContext(ctx, r.client)
	var tx *dbent.Tx
	var err error
	if dbent.TxFromContext(ctx) == nil {
		tx, err = r.client.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		ctx = dbent.NewTxContext(ctx, tx)
		client = tx.Client()
	}
	rows, err := client.QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, accountID)
	if err != nil {
		return err
	}
	exists := rows.Next()
	readErr := rows.Err()
	_ = rows.Close()
	if readErr != nil {
		return readErr
	}
	if !exists {
		return errors.New("Cloud account is missing")
	}
	if _, err = client.ExecContext(ctx, `INSERT INTO openai_cloud_runtime_blocks(root_id,account_id) VALUES($1,$2) ON CONFLICT(root_id) DO NOTHING`, root, accountID); err != nil {
		return err
	}
	if _, err = client.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(COALESCE(extra,'{}'::jsonb),'{openai_cloud}','false'::jsonb,true),updated_at=NOW() WHERE id=$1`, accountID); err != nil {
		return err
	}
	if _, err = client.ExecContext(ctx, `UPDATE openai_cloud_grants SET runtime_blocked=TRUE,revision=revision+1,updated_at=NOW() WHERE account_id=$1`, accountID); err != nil {
		return err
	}
	if err = enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
		return err
	}
	if tx != nil {
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	r.syncSchedulerAccountSnapshot(baseCtx, accountID)
	return nil
}
func (r *accountRepository) PendingCloudReconciliationAcks(ctx context.Context, accountID int64) ([]string, error) {
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT root_id FROM openai_cloud_reconciliations WHERE account_id=$1 AND settled_at IS NULL`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *accountRepository) MarkCloudReconciliationSettled(ctx context.Context, root string) error {
	_, err := txAwareSQLExecutor(ctx, r.sql, r.client).ExecContext(ctx, `UPDATE openai_cloud_reconciliations SET settled_at=COALESCE(settled_at,NOW()) WHERE root_id=$1`, root)
	return err
}
func (r *accountRepository) CloudRuntimeReconciled(ctx context.Context, accountID int64) (bool, error) {
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, `SELECT EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks WHERE account_id=$1) AND NOT EXISTS(SELECT 1 FROM openai_cloud_runtime_blocks b LEFT JOIN openai_cloud_reconciliations r USING(root_id) WHERE b.account_id=$1 AND r.settled_at IS NULL)`, accountID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var complete bool
	if !rows.Next() {
		return false, rows.Err()
	}
	err = rows.Scan(&complete)
	return complete, err
}
