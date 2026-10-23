package service

import (
	"context"
	"fmt"
)

// Refreshing one supplier grant must not use another grant's admission or
// replace a concurrently reauthorized credential. Generic account mutations
// cannot express these mode and snapshot conditions.
type VendorOAuthRefreshRepository interface {
	ValidateVendorOAuthRefresh(context.Context, *Account) error
	UpdateVendorOAuthCredentialsIfUnchanged(context.Context, *Account, map[string]any) (bool, error)
	DisableVendorOAuthAuthorizationIfUnchanged(context.Context, *Account) (bool, error)
}

func isVendorOpenAIRefresh(account *Account) bool {
	return account != nil && account.VendorID != nil && account.Platform == PlatformOpenAI
}

func vendorOAuthRefreshRepository(repo AccountRepository) (VendorOAuthRefreshRepository, error) {
	conditional, ok := repo.(VendorOAuthRefreshRepository)
	if !ok {
		return nil, &providerConfigurationRefreshError{err: fmt.Errorf("supplier OAuth refresh repository is not configured")}
	}
	return conditional, nil
}

func validateVendorOAuthRefresh(ctx context.Context, repo AccountRepository, account *Account) error {
	if !isVendorOpenAIRefresh(account) {
		return nil
	}
	conditional, err := vendorOAuthRefreshRepository(repo)
	if err != nil {
		return err
	}
	return conditional.ValidateVendorOAuthRefresh(ctx, account)
}

func persistVendorOAuthRefresh(ctx context.Context, repo AccountRepository, account *Account, credentials map[string]any) error {
	conditional, err := vendorOAuthRefreshRepository(repo)
	if err != nil {
		return err
	}
	applied, err := conditional.UpdateVendorOAuthCredentialsIfUnchanged(ctx, account, credentials)
	if err != nil {
		// A successful upstream rotation may already have consumed the old RT.
		// Do not turn an ambiguous persistence error into a second refresh.
		return &providerCycleContainmentRefreshError{err: err}
	}
	if !applied {
		return ErrVendorSupplyUnavailable
	}
	account.Credentials = shallowCopyMap(credentials)
	return nil
}

func (s *TokenRefreshService) disableVendorOAuthRefreshGrant(ctx context.Context, account *Account, cause error) error {
	conditional, err := vendorOAuthRefreshRepository(s.accountRepo)
	if err != nil {
		return err
	}
	applied, err := conditional.DisableVendorOAuthAuthorizationIfUnchanged(ctx, account)
	if err != nil {
		return &providerCycleContainmentRefreshError{err: err}
	}
	if !applied {
		return errRefreshSkipped
	}
	cacheFailed := s.cacheInvalidator == nil
	if s.cacheInvalidator != nil {
		cacheFailed = s.cacheInvalidator.InvalidateToken(ctx, account) != nil
	}
	// Repository mutation publishes the durable mode change. Never publish this
	// old snapshot or send the account-wide scheduler-blocked notification.
	return &accountPermanentRefreshError{err: cause, persistentlyBlocked: true, cacheInvalidationFailed: cacheFailed}
}
