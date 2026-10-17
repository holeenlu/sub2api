package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/authz"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAIExcelCredentialRepository = (*accountRepository)(nil)

func (r *accountRepository) GetExcelCredentials(ctx context.Context, id int64) (string, error) {
	var encrypted string
	rows, err := r.sql.QueryContext(ctx, `SELECT g.ciphertext FROM openai_excel_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id IS NULL`, id)
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
	if !account.IsExcelBPSEligible() {
		return false, nil
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
	rows, err := tx.Client().QueryContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL AND vendor_id IS NULL AND parent_account_id IS NULL AND platform='openai' AND type='oauth' AND credentials=$2::jsonb FOR UPDATE`, account.ID, string(primary))
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
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return n == 1, nil
}
