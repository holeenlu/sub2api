package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountAuthorizationRepository = (*accountRepository)(nil)

func (r *accountRepository) AccountAuthorizationSnapshot(ctx context.Context, account *service.Account) (*service.AccountAuthorizationCiphertexts, error) {
	if account == nil || account.VendorID != nil || account.IsShadow() {
		return nil, authz.ErrPermissionDenied
	}
	if _, scoped := authz.VendorScopeFromContext(ctx); scoped {
		return nil, authz.ErrPermissionDenied
	}
	if actor, ok := authz.FromContext(ctx); ok && actor.ManagementRequest && actor.Role != authz.SuperAdmin {
		return nil, authz.ErrPermissionDenied
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT a.credentials,a.extra,COALESCE(e.ciphertext,''),COALESCE(s.registration_ciphertext,''),COALESCE(s.credentials_ciphertext,'') FROM accounts a LEFT JOIN openai_excel_grants e ON e.account_id=a.id LEFT JOIN openai_siwc_grants s ON s.account_id=a.id WHERE a.id=$1 AND a.platform=$2 AND a.type=$3 AND a.deleted_at IS NULL AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND (`+accountVisibilitySQL(ctx, "a.vendor_id")+`)`, account.ID, account.Platform, account.Type)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrAccountNotFound
	}
	var credentials, extra []byte
	out := &service.AccountAuthorizationCiphertexts{}
	if err := rows.Scan(&credentials, &extra, &out.Excel, &out.SIWCRegistration, &out.SIWCCredentials); err != nil {
		return nil, err
	}
	var currentCredentials, currentExtra map[string]any
	if err := json.Unmarshal(credentials, &currentCredentials); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(extra, &currentExtra); err != nil {
		return nil, err
	}
	account.Credentials, account.Extra = currentCredentials, currentExtra
	return out, rows.Err()
}

func (r *accountRepository) CreateWithAccountAuthorizations(ctx context.Context, account *service.Account, groups []service.AccountGroup, authorizations *service.AccountAuthorizationCiphertexts) error {
	if authorizations != nil {
		if account == nil || account.VendorID != nil || account.IsShadow() {
			return authz.ErrPermissionDenied
		}
		if actor, ok := authz.FromContext(ctx); ok && actor.ManagementRequest && actor.Role != authz.SuperAdmin {
			return authz.ErrPermissionDenied
		}
		if authorizations.Excel != "" && (!account.IsExcelBPSEligible() || account.IsExcelOAuth()) {
			return errors.New("Account cannot hold an independent Excel grant")
		}
		if (authorizations.SIWCRegistration != "" || authorizations.SIWCCredentials != "") && (!account.IsSIWCEligible() || authorizations.SIWCRegistration == "") {
			return errors.New("Account cannot hold this SIWC grant")
		}
	}
	return r.createWithAccountGroups(ctx, account, groups, authorizations)
}
