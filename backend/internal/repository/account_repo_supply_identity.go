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

// Configure both native repository instances during composition, before they
// serve requests. Identity verification belongs to the existing vendor service;
// persistence owns the atomic account/claim/outbox transaction.
func (r *accountRepository) SetSupplyIdentityGuard(guard service.NativeSupplyIdentityGuard) {
	r.supplyIdentityGuard = guard
}

func nativeSupplyIdentityRequired(a *service.Account) bool {
	return a != nil && a.VendorID == nil && supplyIdentityRequired(a)
}

func supplyIdentityRequired(a *service.Account) bool {
	return a != nil && a.ParentAccountID == nil &&
		(a.Platform == service.PlatformAnthropic || a.Platform == service.PlatformOpenAI) &&
		(a.Type == service.AccountTypeOAuth || a.Type == service.AccountTypeSetupToken)
}

func (r *accountRepository) prepareNativeSupplyIdentity(ctx context.Context, a *service.Account) (*service.VerifiedSupplyIdentity, error) {
	if !supplyIdentityRequired(a) {
		return nil, nil
	}
	if scope, ok := authz.VendorScopeFromContext(ctx); ok {
		if a.VendorID == nil || scope.VendorID != *a.VendorID {
			return nil, authz.ErrPermissionDenied
		}
	} else if a.VendorID != nil && authz.IsManagementRequest(ctx) {
		return nil, authz.ErrPermissionDenied
	}
	if r.supplyIdentityGuard == nil {
		return nil, service.ErrVendorIdentityUnverified
	}
	copy := *a
	if a.ProxyID != nil {
		proxies, err := r.loadProxies(ctx, []int64{*a.ProxyID})
		if err != nil {
			return nil, err
		}
		copy.Proxy = proxies[*a.ProxyID]
		if copy.Proxy == nil || !service.SameProxyOwner(copy.Proxy.VendorID, a.VendorID) {
			return nil, service.ErrProxyNotFound
		}
	}
	if a.VendorID != nil {
		if err := r.ValidateVendorSupply(ctx, &copy, nil); err != nil {
			return nil, err
		}
	}
	identity, err := r.supplyIdentityGuard.Verify(ctx, &copy)
	if err == nil && identity == nil {
		return nil, service.ErrVendorIdentityUnverified
	}
	return identity, err
}

func (r *accountRepository) prepareNativeSupplyUpdate(ctx context.Context, a *service.Account) (*service.VerifiedSupplyIdentity, error) {
	if !nativeSupplyIdentityRequired(a) {
		return nil, nil
	}
	payload, err := json.Marshal(normalizeJSONMap(a.Credentials))
	if err != nil {
		return nil, err
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, "SELECT platform=$2 AND type=$3 AND credentials=$4::jsonb FROM accounts WHERE id=$1 AND vendor_id IS NULL AND deleted_at IS NULL", a.ID, a.Platform, a.Type, string(payload))
	if err != nil {
		return nil, err
	}
	var unchanged bool
	if !rows.Next() {
		rows.Close()
		return nil, service.ErrAccountNotFound
	}
	err = rows.Scan(&unchanged)
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if unchanged {
		return nil, nil
	}
	return r.prepareNativeSupplyIdentity(ctx, a)
}

func (r *accountRepository) claimNativeSupplyIdentity(ctx context.Context, tx service.VendorProxySQL, a *service.Account, identity *service.VerifiedSupplyIdentity) error {
	if identity == nil {
		return nil
	}
	if r.supplyIdentityGuard == nil {
		return service.ErrVendorIdentityUnverified
	}
	return r.supplyIdentityGuard.Claim(ctx, tx, a, identity)
}

type preparedNativeSupplyIdentity struct {
	account  *service.Account
	identity *service.VerifiedSupplyIdentity
}

func (r *accountRepository) prepareNativeCredentialWrite(ctx context.Context, id int64, credentials map[string]any) (*preparedNativeSupplyIdentity, error) {
	payload, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return nil, err
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, "SELECT platform,type,vendor_id,parent_account_id,proxy_id,credentials=$2::jsonb FROM accounts WHERE id=$1 AND deleted_at IS NULL", id, string(payload))
	if err != nil {
		return nil, err
	}
	a := &service.Account{ID: id, Credentials: credentials}
	var unchanged bool
	if !rows.Next() {
		rows.Close()
		return nil, service.ErrAccountNotFound
	}
	err = rows.Scan(&a.Platform, &a.Type, &a.VendorID, &a.ParentAccountID, &a.ProxyID, &unchanged)
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if unchanged {
		return nil, nil
	}
	// Shadows read credentials from their root. A legacy writer must never
	// install an independently authenticated identity on a supplier shadow.
	if a.VendorID != nil && !supplyIdentityRequired(a) {
		return nil, authz.ErrPermissionDenied
	}
	if !supplyIdentityRequired(a) {
		return nil, nil
	}
	identity, err := r.prepareNativeSupplyIdentity(ctx, a)
	if err != nil {
		return nil, err
	}
	return &preparedNativeSupplyIdentity{account: a, identity: identity}, nil
}

func (r *accountRepository) prepareNativeBulkCredentials(ctx context.Context, ids []int64, patch map[string]any, proxyChange bool) ([]preparedNativeSupplyIdentity, error) {
	if len(patch) == 0 && !proxyChange {
		return nil, nil
	}
	rows, err := txAwareSQLExecutor(ctx, r.sql, r.client).QueryContext(ctx, "/* native_supply_credential_preflight */ SELECT id,platform,type,vendor_id,parent_account_id,proxy_id,credentials FROM accounts WHERE id=ANY($1) AND deleted_at IS NULL", pq.Array(ids))
	if err != nil {
		return nil, err
	}
	var accounts []*service.Account
	for rows.Next() {
		a := new(service.Account)
		var raw []byte
		if err = rows.Scan(&a.ID, &a.Platform, &a.Type, &a.VendorID, &a.ParentAccountID, &a.ProxyID, &raw); err != nil {
			break
		}
		if a.VendorID != nil {
			err = authz.ErrPermissionDenied
			break
		}
		if len(patch) == 0 || !nativeSupplyIdentityRequired(a) {
			continue
		}
		if err = json.Unmarshal(raw, &a.Credentials); err != nil {
			break
		}
		accounts = append(accounts, a)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	var prepared []preparedNativeSupplyIdentity
	for _, a := range accounts {
		if a.Credentials == nil {
			a.Credentials = make(map[string]any)
		}
		for k, v := range patch {
			a.Credentials[k] = v
		}
		identity, err := r.prepareNativeSupplyUpdate(ctx, a)
		if err != nil {
			return nil, err
		}
		if identity != nil {
			prepared = append(prepared, preparedNativeSupplyIdentity{account: a, identity: identity})
		}
	}
	return prepared, nil
}

func (r *accountRepository) createNativeSupplyAccount(ctx context.Context, a *service.Account) error {
	identity, err := r.prepareNativeSupplyIdentity(ctx, a)
	if err != nil {
		return err
	}
	client := clientFromContext(ctx, r.client)
	tx, err := client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}
	if tx != nil {
		defer tx.Rollback()
		client = tx.Client()
	}
	if err := createAccountRecord(ctx, client, a); err != nil {
		return err
	}
	if err := r.claimNativeSupplyIdentity(ctx, client, a, identity); err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &a.ID, nil, buildSchedulerGroupPayload(a.GroupIDs)); err != nil {
		return err
	}
	if tx != nil {
		return tx.Commit()
	}
	return nil
}
