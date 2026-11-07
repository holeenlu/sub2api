package repository

import (
	"context"
	"encoding/json"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/lib/pq"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Excel's primary OAuth grant uses the same conditional persistence principle
// as the independent grant. Only credentials and its recovery digest advance;
// concurrent manual settings, group edits and disable identities are untouched.
func (r *accountRepository) UpdateExcelPrimaryCredentialsIfUnchanged(ctx context.Context, before *service.Account, credentials map[string]any) (bool, error) {
	if !before.IsExcelOAuth() {
		return false, nil
	}
	oldJSON, err := json.Marshal(before.Credentials)
	if err != nil {
		return false, err
	}
	newJSON, err := json.Marshal(credentials)
	if err != nil {
		return false, err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	locked, err := lockExcelBPSAccount(txCtx, client, before.ID)
	if err != nil || !locked {
		return false, err
	}
	result, err := client.ExecContext(txCtx, `UPDATE accounts SET credentials=$3::jsonb,
 extra=CASE WHEN extra->'openai_excel_bps_failure'->'policy'->>'primary'=md5($2::jsonb::text)
 THEN jsonb_set(extra,'{openai_excel_bps_failure,policy,primary}',to_jsonb(md5($3::jsonb::text)))
 ELSE extra END, updated_at=NOW()
 WHERE id=$1 AND deleted_at IS NULL AND parent_account_id IS NULL AND platform='openai' AND type='oauth'
 AND credentials=$2::jsonb AND proxy_id IS NOT DISTINCT FROM $4::bigint
 AND COALESCE(extra->>'vendor_archived_at','')=''`,
		before.ID, string(oldJSON), string(newJSON), before.ProxyID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if err := enqueueSchedulerOutbox(txCtx, client, service.SchedulerOutboxEventAccountChanged, &before.ID, nil, nil); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if n == 1 {
		r.syncSchedulerAccountSnapshot(ctx, before.ID)
	}
	return n == 1, nil
}

var _ service.OpenAIExcelCredentialRepository = (*accountRepository)(nil)
var _ service.OpenAIExcelAuthorizationPresenceRepository = (*accountRepository)(nil)

// ExcelAuthorizationPresence returns only IDs of visible, nonempty grants.
// It does not load ciphertext or change the account's execution authorization.
func (r *accountRepository) ExcelAuthorizationPresence(ctx context.Context, ids []int64) (map[int64]bool, error) {
	result := make(map[int64]bool)
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.sql.QueryContext(ctx, "SELECT g.account_id FROM openai_excel_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=ANY($1) AND a.deleted_at IS NULL AND g.ciphertext<>'' AND ("+accountVisibilitySQL(ctx, "a.vendor_id")+")", pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (r *accountRepository) GetExcelCredentials(ctx context.Context, id int64) (string, error) {
	var encrypted string
	rows, err := r.sql.QueryContext(ctx, `SELECT g.ciphertext FROM openai_excel_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=$1 AND a.deleted_at IS NULL AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`) AND (a.vendor_id IS NULL OR (`+vendorSupplySQL("a", "NULL")+` AND EXISTS(SELECT 1 FROM vendors v WHERE v.id=a.vendor_id AND `+authz.OpenAIAuthorizationModeSQL("a", "v", "bps")+`)))`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	if err := rows.Scan(&encrypted); err != nil {
		return "", err
	}
	return encrypted, rows.Err()
}

// Serialize on the account row and compare both the primary identity and grant.
// A refresh may never overwrite a concurrent reauthorization or account edit.
func (r *accountRepository) StoreExcelCredentials(ctx context.Context, account *service.Account, previous, next string) (bool, error) {
	if account == nil || !account.IsExcelBPSEligible() {
		return false, nil
	}
	if account.VendorID != nil {
		// Supplier grant admission/revocation is owned by its verified account
		// transaction. This repository path is only the CAS refresh of a grant.
		if previous == "" || next == "" {
			return false, authz.ErrPermissionDenied
		}
		if actor, ok := authz.FromContext(ctx); ok && actor.ManagementRequest {
			scope, scoped := authz.VendorScopeFromContext(ctx)
			if !scoped || scope.ReadOnly || scope.VendorID != *account.VendorID {
				return false, authz.ErrPermissionDenied
			}
		}
		if err := r.guardAccountScope(ctx, account.ID); err != nil {
			return false, err
		}
	}
	primary, err := json.Marshal(account.Credentials)
	if err != nil {
		return false, err
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := authz.LockManagementWrite(ctx, tx.Client(), nil, "", "", false); err != nil {
		return false, err
	}
	lockQuery := `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL AND vendor_id IS NULL AND parent_account_id IS NULL AND platform='openai' AND type='oauth' AND credentials=$2::jsonb FOR UPDATE`
	lockArgs := []any{account.ID, string(primary)}
	if account.VendorID != nil {
		lockQuery = `SELECT a.id FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id=$3 AND a.parent_account_id IS NULL AND a.platform='openai' AND a.type='oauth' AND a.credentials=$2::jsonb AND ` + vendorSupplySQL("a", "NULL") + ` AND EXISTS(SELECT 1 FROM vendors v WHERE v.id=a.vendor_id AND ` + authz.OpenAIAuthorizationModeSQL("a", "v", "bps") + `) FOR UPDATE OF a`
		lockArgs = append(lockArgs, *account.VendorID)
	}
	rows, err := tx.Client().QueryContext(ctx, lockQuery, lockArgs...)
	if err != nil {
		return false, err
	}
	exists := rows.Next()
	err = rows.Err()
	_ = rows.Close()
	if err != nil || !exists {
		return false, err
	}
	query := `INSERT INTO openai_excel_grants(account_id,ciphertext) VALUES($1,$3) ON CONFLICT(account_id) DO UPDATE SET ciphertext=$3,updated_at=NOW() WHERE openai_excel_grants.ciphertext=$2`
	if previous != "" {
		query = `UPDATE openai_excel_grants SET ciphertext=$3,updated_at=NOW() WHERE account_id=$1 AND ciphertext=$2`
	}
	result, err := tx.Client().ExecContext(ctx, query, account.ID, previous, next)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		if service.IsExcelCredentialRefresh(ctx) && previous != "" && next != "" {
			// Token rotation is the same grant. A reauthorization never enters
			// this path, even if its account/workspace identity is unchanged.
			_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra - 'openai_excel_bps_authorization_error',
 '{openai_excel_bps_failure,grant_fingerprint}',to_jsonb(md5($3::text)))
 WHERE id=$1 AND extra->'openai_excel_bps_failure'->>'grant_fingerprint'=md5($2::text)`,
				account.ID, previous, next)
		} else {
			// An explicit replacement/revocation cancels the old claim. Keep the
			// actual failure reason for diagnosis; only an admin may enable it.
			_, err = tx.Client().ExecContext(ctx, `UPDATE accounts SET extra=
 (extra - 'openai_excel_bps_authorization_error' - 'openai_excel_bps_403_disabled_at' - 'openai_excel_bps_403_last_probe_at')
 || CASE WHEN extra ? 'openai_excel_bps_failure' THEN jsonb_build_object('openai_excel_bps_failure', extra->'openai_excel_bps_failure'
 || '{"recovery_cancelled":true,"next_probe_at":null}'::jsonb) ELSE '{}'::jsonb END
 WHERE id=$1`, account.ID)
		}
		if err != nil {
			return false, err
		}
		if err := enqueueSchedulerOutbox(ctx, tx.Client(), service.SchedulerOutboxEventAccountChanged, &account.ID, nil, nil); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if n == 1 {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return n == 1, nil
}
