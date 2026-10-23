package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAISIWCCredentialRepository = (*accountRepository)(nil)

// SIWC is bound to the native parent identity, not its rotating token values.
// Keep read and locked-write checks identical: a native refresh must not discard
// an independently rotated SIWC refresh token, while rebinding still fails closed.
const siwcParentIdentitySQL = `COALESCE(a.credentials->>'chatgpt_account_id','')=COALESCE(($2::jsonb)->>'chatgpt_account_id','')
 AND COALESCE(a.credentials->>'chatgpt_user_id','')=COALESCE(($2::jsonb)->>'chatgpt_user_id','')
 AND COALESCE(a.credentials->>'email','')=COALESCE(($2::jsonb)->>'email','')
 AND COALESCE(a.credentials->>'client_id','')=COALESCE(($2::jsonb)->>'client_id','')
 AND COALESCE(a.credentials->>'auth_mode','')=COALESCE(($2::jsonb)->>'auth_mode','')
 AND COALESCE(a.credentials->>'openai_auth_mode','')=COALESCE(($2::jsonb)->>'openai_auth_mode','')`

func (r *accountRepository) SIWCInstallationID(ctx context.Context, proposed string) (string, error) {
	// One installation identity across accounts, replicas and administrator signout.
	rows, err := r.sql.QueryContext(ctx, `INSERT INTO openai_siwc_installation(singleton,host_id) VALUES(TRUE,$1) ON CONFLICT(singleton) DO UPDATE SET singleton=TRUE RETURNING host_id`, proposed)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	var host string
	err = rows.Scan(&host)
	return host, err
}
func (r *accountRepository) GetSIWCRecord(ctx context.Context, account *service.Account) (string, string, error) {
	if !account.IsSIWCEligible() {
		return "", "", authz.ErrPermissionDenied
	}
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return "", "", authz.ErrPermissionDenied
	}
	primary, err := json.Marshal(account.Credentials)
	if err != nil {
		return "", "", err
	}
	// Read-side identity checks also reject stale scheduler snapshots after the
	// administrator replaces the native parent identity. Rotating tokens do not
	// change this explicit local-account association.
	rows, err := r.sql.QueryContext(ctx, `SELECT g.registration_ciphertext,g.credentials_ciphertext FROM openai_siwc_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND a.platform='openai' AND a.type='oauth'
 AND (`+siwcParentIdentitySQL+`)
 AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`)`, account.ID, string(primary))
	if err != nil {
		return "", "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", "", rows.Err()
	}
	var registration, grant string
	err = rows.Scan(&registration, &grant)
	return registration, grant, err
}

// Account-row lock, live management authority and double CAS make callbacks,
// refreshes and signout atomic without adding a second credential authority.
func (r *accountRepository) StoreSIWCRecord(ctx context.Context, account *service.Account, oldRegistration, oldGrant, newRegistration, newGrant string) (bool, error) {
	if !account.IsSIWCEligible() {
		return false, authz.ErrPermissionDenied
	}
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return false, authz.ErrPermissionDenied
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
	if err = authz.LockManagementWrite(ctx, tx.Client(), nil, "", "", false); err != nil {
		return false, err
	}
	rows, err := tx.Client().QueryContext(ctx, `SELECT a.id FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND a.platform='openai' AND a.type='oauth' AND (`+siwcParentIdentitySQL+`) FOR UPDATE`, account.ID, string(primary))
	if err != nil {
		return false, err
	}
	exists := rows.Next()
	err = rows.Err()
	_ = rows.Close()
	if err != nil || !exists {
		return false, err
	}
	query := `UPDATE openai_siwc_grants SET registration_ciphertext=$4,credentials_ciphertext=$5,updated_at=NOW() WHERE account_id=$1 AND registration_ciphertext=$2 AND credentials_ciphertext=$3`
	if oldRegistration == "" {
		query = `INSERT INTO openai_siwc_grants(account_id,registration_ciphertext,credentials_ciphertext) VALUES($1,$4,$5) ON CONFLICT(account_id) DO UPDATE SET registration_ciphertext=$4,credentials_ciphertext=$5,updated_at=NOW() WHERE openai_siwc_grants.registration_ciphertext=$2 AND openai_siwc_grants.credentials_ciphertext=$3`
	}
	result, err := tx.Client().ExecContext(ctx, query, account.ID, oldRegistration, oldGrant, newRegistration, newGrant)
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
